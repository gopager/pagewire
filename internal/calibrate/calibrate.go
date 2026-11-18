// Package calibrate measures the frequency correction and the tuner gain a
// source should use, from real transmissions on the configured channels or
// from a reference carrier.
package calibrate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	"github.com/gopager/pagewire/internal/pipeline"
	"github.com/gopager/pagewire/internal/sdr"
)

// Burst is one transmission seen on a channel.
type Burst struct {
	Channel  string
	Start    time.Duration
	Blocks   int
	OffsetHz float64 // mean carrier offset over the burst
	SNRdB    float64 // mean SNR over the burst
}

// OffsetResult is the outcome of the frequency measurement.
type OffsetResult struct {
	Bursts         []Burst
	MedianOffsetHz float64
	SpreadHz       float64 // median absolute deviation of the burst offsets
	// PPMDelta is the correction to add to the configured ppm. A positive
	// ppm raises the frequency the dongle actually receives at, so a
	// carrier seen below its nominal frequency needs a larger ppm.
	PPMDelta float64
	// ReferenceHz is the frequency the offsets refer to.
	ReferenceHz float64
}

// GainResult is one step of the gain sweep.
type GainResult struct {
	GainTenthDB int
	NoiseDBFS   float64 // median noise floor
	Clipping    float64 // mean share of clipped ADC samples
	Bursts      int
	SNRdB       float64 // mean burst SNR, NaN without bursts
	Messages    int
}

// observer collects per-block channel status from the pipeline.
type observer struct {
	start  time.Time
	blocks map[string]*chanState
}

type chanState struct {
	bursts []Burst
	cur    *Burst
	offSum float64
	snrSum float64
	floors []float64
}

func newObserver() *observer { return &observer{start: time.Now(), blocks: map[string]*chanState{}} }

// Audio implements pipeline.AudioSink.
func (o *observer) Audio(*pipeline.Channel, []float32) {}

// Block implements pipeline.BlockObserver.
func (o *observer) Block(ch *pipeline.Channel, audio []float32, open bool) {
	st := o.blocks[ch.Name]
	if st == nil {
		st = &chanState{}
		o.blocks[ch.Name] = st
	}
	s := ch.Status()
	st.floors = append(st.floors, s.NoiseDBFS)
	// A burst spans the held gate; only blocks that carry signal themselves
	// contribute to the offset and SNR averages.
	switch {
	case open && st.cur == nil:
		st.cur = &Burst{Channel: ch.Name, Start: time.Since(o.start)}
		st.offSum, st.snrSum = 0, 0
		fallthrough
	case open:
		if s.Signal {
			st.cur.Blocks++
			st.offSum += s.FreqOffsetHz
			st.snrSum += s.SNRdB
		}
	case !open && st.cur != nil:
		o.closeBurst(st)
	}
}

func (o *observer) closeBurst(st *chanState) {
	b := st.cur
	st.cur = nil
	if b.Blocks < 2 {
		return // too short to trust
	}
	b.OffsetHz = st.offSum / float64(b.Blocks)
	b.SNRdB = st.snrSum / float64(b.Blocks)
	st.bursts = append(st.bursts, *b)
}

func (o *observer) finish() {
	for _, st := range o.blocks {
		if st.cur != nil {
			o.closeBurst(st)
		}
	}
}

func (o *observer) allBursts() []Burst {
	var out []Burst
	for _, st := range o.blocks {
		out = append(out, st.bursts...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

func (o *observer) medianFloor() float64 {
	var all []float64
	for _, st := range o.blocks {
		all = append(all, st.floors...)
	}
	return median(all)
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	if n := len(s); n%2 == 1 {
		return s[n/2]
	} else {
		return (s[n/2-1] + s[n/2]) / 2
	}
}

// run streams the source for d (or until ctx ends) with the given observer.
func run(ctx context.Context, src config.SourceConfig, channels []config.ChannelConfig, dev sdr.Device, obs *observer, d time.Duration, log *slog.Logger) (*pipeline.Source, error) {
	ps, err := pipeline.NewSource(src, channels, dev, decoder.DefaultRate, obs, nil, log)
	if err != nil {
		return nil, err
	}
	rctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	err = ps.Run(rctx)
	obs.finish()
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return ps, err
	}
	return ps, nil
}

// referenceChannels returns the channel plan for the measurement: the
// configured channels, or a single channel on referenceHz.
func referenceChannels(src config.SourceConfig, channels []config.ChannelConfig, referenceHz uint32) []config.ChannelConfig {
	if referenceHz == 0 {
		var mine []config.ChannelConfig
		for _, ch := range channels {
			if ch.Source == src.Name {
				mine = append(mine, ch)
			}
		}
		return mine
	}
	return []config.ChannelConfig{{Name: "reference", Source: src.Name, Frequency: config.Frequency(referenceHz), Squelch: config.Level{Auto: true, Set: true},
		Decoders: []config.DecoderConfig{{Protocol: "pocsag1200"}}}}
}

// MeasureOffset listens for d and estimates the remaining carrier offset
// from every transmission (or from the reference carrier). The offset is
// the mean discriminator output while the squelch is open, so a balanced
// FSK burst or an unmodulated carrier both yield the carrier position.
func MeasureOffset(ctx context.Context, src config.SourceConfig, channels []config.ChannelConfig, dev sdr.Device, referenceHz uint32, d time.Duration, log *slog.Logger) (*OffsetResult, error) {
	chans := referenceChannels(src, channels, referenceHz)
	if len(chans) == 0 {
		return nil, fmt.Errorf("source %s has no channels", src.Name)
	}
	obs := newObserver()
	if _, err := run(ctx, src, chans, dev, obs, d, log); err != nil {
		return nil, err
	}
	res := &OffsetResult{Bursts: obs.allBursts(), ReferenceHz: float64(chans[0].Frequency)}
	if len(res.Bursts) == 0 {
		return res, nil
	}
	offs := make([]float64, len(res.Bursts))
	for i, b := range res.Bursts {
		offs[i] = b.OffsetHz
	}
	res.MedianOffsetHz = median(offs)
	deviations := make([]float64, len(offs))
	for i, o := range offs {
		deviations[i] = math.Abs(o - res.MedianOffsetHz)
	}
	res.SpreadHz = median(deviations)
	res.PPMDelta = -res.MedianOffsetHz / res.ReferenceHz * 1e6
	return res, nil
}

// MaxTrustedOffsetHz bounds the carrier offset for which the burst method
// is unbiased: further out, the channel filter's edge attenuates one FSK
// tone more than the other and skews the discriminator mean.
const MaxTrustedOffsetHz = 1000

// MeasureOffsetIterative repeats MeasureOffset, applying the estimate to
// the device's ppm between passes, until the remaining offset is within
// MaxTrustedOffsetHz or passes are exhausted. It returns the final result
// and the total ppm delta relative to the starting correction.
func MeasureOffsetIterative(ctx context.Context, src config.SourceConfig, channels []config.ChannelConfig, dev sdr.Device, startPPM int, referenceHz uint32, d time.Duration, passes int, log *slog.Logger) (*OffsetResult, int, error) {
	ppm := startPPM
	var last *OffsetResult
	for pass := 1; pass <= passes; pass++ {
		res, err := MeasureOffset(ctx, src, channels, dev, referenceHz, d, log)
		if err != nil {
			return last, ppm, err
		}
		last = res
		if len(res.Bursts) == 0 {
			break
		}
		step := int(math.Round(res.PPMDelta))
		log.Info("ppm pass", "pass", pass, "offset_hz", fmt.Sprintf("%.0f", res.MedianOffsetHz), "bursts", len(res.Bursts), "ppm", ppm, "delta", step)
		if math.Abs(res.MedianOffsetHz) <= MaxTrustedOffsetHz || step == 0 {
			ppm += step
			break
		}
		ppm += step
		if err := dev.SetPPM(ppm); err != nil {
			return last, ppm, fmt.Errorf("set ppm %d: %w", ppm, err)
		}
	}
	return last, ppm, nil
}

// SweepGain tries every gain step for perStep and reports noise floor,
// clipping and burst SNR per step. gains are tenths of a dB.
func SweepGain(ctx context.Context, src config.SourceConfig, channels []config.ChannelConfig, dev sdr.Device, gains []int, perStep time.Duration, log *slog.Logger) ([]GainResult, error) {
	chans := referenceChannels(src, channels, 0)
	if len(chans) == 0 {
		return nil, fmt.Errorf("source %s has no channels", src.Name)
	}
	var out []GainResult
	for _, g := range gains {
		if ctx.Err() != nil {
			break
		}
		if err := dev.SetGain(g); err != nil {
			return out, fmt.Errorf("set gain %d: %w", g, err)
		}
		obs := newObserver()
		ps, err := run(ctx, src, chans, dev, obs, perStep, log)
		if err != nil {
			return out, err
		}
		r := GainResult{GainTenthDB: g, NoiseDBFS: obs.medianFloor(), SNRdB: math.NaN()}
		r.Clipping = ps.ClippingRatio()
		bursts := obs.allBursts()
		r.Bursts = len(bursts)
		if len(bursts) > 0 {
			sum := 0.0
			for _, b := range bursts {
				sum += b.SNRdB
			}
			r.SNRdB = sum / float64(len(bursts))
		}
		out = append(out, r)
		log.Info("gain step measured", "gain_db", float64(g)/10, "noise_dbfs", fmt.Sprintf("%.1f", r.NoiseDBFS), "clipping", fmt.Sprintf("%.4f", r.Clipping), "bursts", r.Bursts, "snr_db", fmt.Sprintf("%.1f", r.SNRdB))
	}
	return out, nil
}

// RecommendGain picks the gain with the best burst SNR among steps whose
// clipping stays below maxClipping; without bursts it picks the highest
// gain before the noise floor stops rising with gain (the front end is
// then noise limited and more gain only adds distortion).
func RecommendGain(results []GainResult, maxClipping float64) (GainResult, string) {
	var best *GainResult
	for i := range results {
		r := &results[i]
		if r.Clipping > maxClipping || r.Bursts == 0 {
			continue
		}
		if best == nil || r.SNRdB > best.SNRdB {
			best = r
		}
	}
	if best != nil {
		return *best, "best burst SNR without clipping"
	}
	// No bursts: find the knee of the noise floor curve.
	var pick *GainResult
	for i := 1; i < len(results); i++ {
		if results[i].Clipping > maxClipping {
			break
		}
		rise := results[i].NoiseDBFS - results[i-1].NoiseDBFS
		gainStep := float64(results[i].GainTenthDB-results[i-1].GainTenthDB) / 10
		if gainStep > 0 && rise < 0.5*gainStep {
			pick = &results[i-1]
			break
		}
		pick = &results[i]
	}
	if pick == nil && len(results) > 0 {
		pick = &results[0]
	}
	if pick == nil {
		return GainResult{}, "no measurements"
	}
	return *pick, "no transmissions seen; noise-floor knee (verify with traffic)"
}
