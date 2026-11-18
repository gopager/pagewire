package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/dsp"
	"github.com/gopager/pagewire/internal/observe"
	"github.com/gopager/pagewire/internal/sdr"
)

// BlockSamples is the number of IQ samples per processing block. At
// 1 MS/s a block is about 16 ms; at 250 kS/s about 65 ms.
const BlockSamples = 16384

// queueDepth is how many blocks may wait between the USB thread and the
// processing goroutine before blocks are dropped.
const queueDepth = 8

// Source drives one SDR device and its channels.
type Source struct {
	Name     string
	Plan     dsp.CapturePlan
	Channels []*Channel

	dev   sdr.Device
	sink  AudioSink
	log   *slog.Logger
	iq    []complex64
	free  chan []byte
	queue chan []byte

	mBlocks, mDropped prometheus.Counter
	mClip, mLoad      prometheus.Gauge

	clipSum   float64
	clipCount int

	// Alive, when set, receives the time of the last received block (unix
	// nanoseconds) so a supervisor can detect a silent device.
	Alive *atomic.Int64
}

// ClippingRatio returns the mean share of ADC samples at full scale over
// all processed blocks (0-1).
func (s *Source) ClippingRatio() float64 {
	if s.clipCount == 0 {
		return 0
	}
	return s.clipSum / float64(s.clipCount)
}

// NewSource plans the capture for the channels bound to cfg, configures the
// opened device accordingly and prepares the channel processors. The device
// must already have gain, ppm and bias tee applied.
func NewSource(cfg config.SourceConfig, channels []config.ChannelConfig, dev sdr.Device, channelRate uint32, sink AudioSink, m *observe.Metrics, log *slog.Logger) (*Source, error) {
	var freqs []uint32
	var mine []config.ChannelConfig
	for _, ch := range channels {
		if ch.Source == cfg.Name {
			freqs = append(freqs, uint32(ch.Frequency))
			mine = append(mine, ch)
		}
	}
	forced := uint32(0)
	switch cfg.Type {
	case config.SourceRTLSDR:
		forced = cfg.RTLSDR.SampleRate
	case config.SourceRTLTCP:
		forced = cfg.RTLTCP.SampleRate
	}
	var plan dsp.CapturePlan
	var err error
	if fixed, ok := dev.(sdr.FixedTuning); ok {
		rate, center := fixed.FixedTuning()
		plan, err = dsp.PlanFixed(freqs, channelRate, rate, center)
	} else {
		plan, err = dsp.PlanCapture(freqs, channelRate, forced)
	}
	if err != nil {
		return nil, fmt.Errorf("source %s: %w", cfg.Name, err)
	}
	if err := dev.SetSampleRate(plan.SampleRate); err != nil {
		return nil, fmt.Errorf("source %s: set sample rate %d: %w", cfg.Name, plan.SampleRate, err)
	}
	if err := dev.SetCenterFreq(plan.CenterHz); err != nil {
		return nil, fmt.Errorf("source %s: tune to %d Hz: %w", cfg.Name, plan.CenterHz, err)
	}
	if got := dev.SampleRate(); got != 0 && got != plan.SampleRate {
		// The RTL2832 resampler rounds; the NCOs are built from the real rate.
		plan.SampleRate = got
	}
	s := &Source{
		Name:  cfg.Name,
		Plan:  plan,
		dev:   dev,
		sink:  sink,
		log:   log.With("source", cfg.Name),
		iq:    make([]complex64, BlockSamples),
		free:  make(chan []byte, queueDepth),
		queue: make(chan []byte, queueDepth),
	}
	for i := 0; i < queueDepth; i++ {
		s.free <- make([]byte, 2*BlockSamples)
	}
	for _, ch := range mine {
		c, err := newChannel(ch, plan, BlockSamples, m)
		if err != nil {
			return nil, err
		}
		s.Channels = append(s.Channels, c)
	}
	if m != nil {
		info := dev.Info()
		m.SourceInfo.WithLabelValues(cfg.Name, info.Driver, info.Tuner, info.Serial, config.USBPath(info.Bus, info.Ports),
			strconv.FormatUint(uint64(plan.CenterHz), 10), strconv.FormatUint(uint64(plan.SampleRate), 10)).Set(1)
		s.mBlocks = m.IQBlocks.WithLabelValues(cfg.Name)
		s.mDropped = m.IQDropped.WithLabelValues(cfg.Name)
		s.mClip = m.IQClipping.WithLabelValues(cfg.Name)
		s.mLoad = m.PipelineLoad.WithLabelValues(cfg.Name)
	}
	return s, nil
}

// Run streams from the device and processes blocks until ctx is done or
// the device fails.
func (s *Source) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.log.Info("source streaming", "center_hz", s.Plan.CenterHz, "sample_rate", s.Plan.SampleRate, "decimation", s.Plan.Decimation, "channels", len(s.Channels))

	procErr := make(chan error, 1)
	go func() { procErr <- s.processLoop(ctx) }()

	// A live device must never be stalled, so blocks are dropped when the
	// processing goroutine falls behind. A recording can wait instead.
	_, replay := s.dev.(sdr.FixedTuning)
	var dropped uint64
	err := s.dev.Stream(ctx, 2*BlockSamples, func(raw []byte) {
		if s.Alive != nil {
			s.Alive.Store(time.Now().UnixNano())
		}
		var buf []byte
		if replay {
			select {
			case buf = <-s.free:
			case <-ctx.Done():
				return
			}
		} else {
			select {
			case buf = <-s.free:
			default:
				dropped++
				if s.mDropped != nil {
					s.mDropped.Inc()
				}
				return
			}
		}
		copy(buf, raw)
		s.queue <- buf
	})
	if replay && err == nil {
		// Let the processing goroutine drain what the replay queued.
		for len(s.queue) > 0 && ctx.Err() == nil {
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	if perr := <-procErr; perr != nil && !errors.Is(perr, context.Canceled) && err == nil {
		err = perr
	}
	if dropped > 0 {
		s.log.Warn("blocks dropped because processing fell behind", "blocks", dropped)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("source %s: %w", s.Name, err)
	}
	return nil
}

// processLoop runs the DSP for every queued block on one goroutine.
func (s *Source) processLoop(ctx context.Context) error {
	blockDur := float64(BlockSamples) / float64(s.Plan.SampleRate)
	for {
		select {
		case <-ctx.Done():
			return nil
		case buf := <-s.queue:
			start := time.Now()
			clipped := dsp.ConvertIQ(s.iq, buf)
			s.free <- buf
			s.clipSum += float64(clipped) / float64(2*BlockSamples)
			s.clipCount++
			for _, ch := range s.Channels {
				ch.process(s.iq, s.sink)
			}
			if s.mBlocks != nil {
				s.mBlocks.Inc()
				s.mClip.Set(float64(clipped) / float64(2*BlockSamples))
				s.mLoad.Set(time.Since(start).Seconds() / blockDur)
			}
		}
	}
}
