// Package pipeline connects an IQ source to its channels: it converts the
// raw samples, shifts and decimates each channel, demodulates FM and hands
// the audio to the channel's consumers.
package pipeline

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/dsp"
	"github.com/gopager/pagewire/internal/observe"
)

// AudioSink receives the demodulated audio of a channel. Blocks arrive from
// one goroutine per source; the slice is reused after the call returns.
type AudioSink interface {
	Audio(ch *Channel, samples []float32)
}

// BlockObserver is optionally implemented by sinks that want to see every
// processed block, including the gated ones, e.g. for statistics.
type BlockObserver interface {
	Block(ch *Channel, audio []float32, open bool)
}

// Pass band half-width of a channel in Hz: wide enough for ±4.8 kHz FSK
// deviation plus the fastest pager symbol rates.
const channelPassHz = 8000

// Stop-band attenuation of the channel filters in dB.
const channelAttenuationDB = 60

// Quieting thresholds for the automatic squelch. Measured on an RTL-SDR
// after the ±8 kHz channel filter, an empty channel sits at about 0.47 and
// a clean FSK signal well below 0.2 (see dsp.Quieting).
const (
	quietingOpen  = 0.33
	quietingClose = 0.42
)

// Channel is one receive frequency within a source.
type Channel struct {
	Name   string
	FreqHz uint32
	Rate   uint32
	Source string

	nco    *dsp.NCO
	chain  *dsp.DecimationChain
	fm     *dsp.FMDemod
	noise  *dsp.NoiseFloor
	gate   *dsp.Gate
	openAt float64
	fixed  bool
	gated  bool
	// dc is the tracked mean of the discriminator output while a signal is
	// present, i.e. the carrier offset in audio units; it is removed before
	// the decoders so their zero-crossing slicers see a centred signal.
	dc    float32
	shift []complex64
	audio []float32

	// Latest measurements, written by the processing goroutine.
	status Status

	mRSSI, mNoise, mSNR, mGate, mOffset prometheus.Gauge
}

// Status is a snapshot of a channel's reception state.
type Status struct {
	// Unavailable is set for audio sources where RF levels cannot be known.
	Unavailable bool
	RSSIdBFS    float64
	NoiseDBFS   float64
	SNRdB       float64
	// GateOpen is the held squelch state; Signal says whether this very
	// block carried a signal (no hold), which measurements should use.
	GateOpen     bool
	Signal       bool
	FreqOffsetHz float64
}

// Status returns the latest measurements. It is meant for attaching quality
// data to messages from the same goroutine that processes the channel.
func (c *Channel) Status() Status { return c.status }

func newChannel(cfg config.ChannelConfig, plan dsp.CapturePlan, maxIn int, m *observe.Metrics) (*Channel, error) {
	chain, err := dsp.NewDecimationChain(plan.SampleRate, plan.ChannelRate, channelPassHz, channelAttenuationDB, maxIn)
	if err != nil {
		return nil, fmt.Errorf("channel %s: %w", cfg.Name, err)
	}
	offset := float64(cfg.Frequency) - float64(plan.CenterHz)
	// Automatic squelch scores blocks by discriminator quieting (1 - Quieting,
	// so noise scores about 0.33 and a clean signal above 0.9); a fixed
	// squelch scores by SNR against the noise floor in dB.
	openAt, closeAt := 1-quietingOpen, 1-quietingClose
	if !cfg.Squelch.IsAuto() {
		openAt = cfg.Squelch.DB
		closeAt = openAt - 4
	}
	// Noise-only blocks are averaged with alpha 0.05 (about a second at 15
	// to 60 blocks per second); during a signal the floor may rise by
	// 0.0005 dB per block, under 2 dB per minute, so a transmission lasting
	// minutes barely moves the SNR reference.
	c := &Channel{
		Name:   cfg.Name,
		FreqHz: uint32(cfg.Frequency),
		Rate:   plan.ChannelRate,
		Source: cfg.Source,
		nco:    dsp.NewNCO(offset, float64(plan.SampleRate)),
		chain:  chain,
		fm:     dsp.NewFMDemod(),
		noise:  dsp.NewNoiseFloor(0.05, 0.0005),
		gate:   dsp.NewGate(openAt, closeAt, 12),
		openAt: openAt,
		fixed:  !cfg.Squelch.IsAuto(),
		gated:  cfg.PowerGateEnabled(),
		shift:  make([]complex64, maxIn),
		audio:  make([]float32, maxIn/plan.Decimation+2),
	}
	if m != nil {
		c.mRSSI = m.ChannelRSSI.WithLabelValues(cfg.Name)
		c.mNoise = m.ChannelNoise.WithLabelValues(cfg.Name)
		c.mSNR = m.ChannelSNR.WithLabelValues(cfg.Name)
		c.mGate = m.ChannelGate.WithLabelValues(cfg.Name)
		c.mOffset = m.ChannelOffset.WithLabelValues(cfg.Name)
	}
	return c, nil
}

// process runs one block of capture-rate IQ through the channel and
// delivers audio to sink when the channel is active (or gating is off).
func (c *Channel) process(iq []complex64, sink AudioSink) {
	shifted := c.shift[:len(iq)]
	c.nco.Mix(shifted, iq)
	narrow := c.chain.Process(shifted)
	if len(narrow) == 0 {
		return
	}
	p := dsp.Power(narrow)
	audio := c.audio[:len(narrow)]
	c.fm.Demod(audio, narrow)
	c.status.RSSIdBFS = dsp.DB(p)

	var open bool
	if c.fixed {
		// Fixed squelch: compare against the floor learnt while closed.
		floor := c.noise.Observe(p, c.gate.Open())
		c.status.NoiseDBFS = dsp.DB(floor)
		c.status.SNRdB = c.status.RSSIdBFS - c.status.NoiseDBFS
		c.status.Signal = c.status.SNRdB >= c.openAt
		open = c.gate.Update(c.status.SNRdB)
	} else {
		score := 1 - float64(dsp.Quieting(audio))
		c.status.Signal = score >= c.openAt
		open = c.gate.Update(score)
		floor := c.noise.Observe(p, open)
		c.status.NoiseDBFS = dsp.DB(floor)
		c.status.SNRdB = c.status.RSSIdBFS - c.status.NoiseDBFS
	}
	c.status.GateOpen = open
	if c.status.Signal {
		var mean float32
		for _, v := range audio {
			mean += v
		}
		mean /= float32(len(audio))
		c.status.FreqOffsetHz = float64(mean) * float64(c.Rate) / 2
		// Follow the carrier offset with a time constant of a few blocks so
		// data patterns within a block do not pull the estimate.
		c.dc += 0.2 * (mean - c.dc)
	}
	if c.dc != 0 {
		for i := range audio {
			audio[i] -= c.dc
		}
	}
	if c.mRSSI != nil {
		c.mRSSI.Set(c.status.RSSIdBFS)
		c.mNoise.Set(c.status.NoiseDBFS)
		c.mSNR.Set(c.status.SNRdB)
		if open {
			c.mGate.Set(1)
			c.mOffset.Set(c.status.FreqOffsetHz)
		} else {
			c.mGate.Set(0)
		}
	}
	if obs, ok := sink.(BlockObserver); ok {
		obs.Block(c, audio, open)
	}
	if c.gated && !open {
		return
	}
	if sink != nil {
		sink.Audio(c, audio)
	}
}
