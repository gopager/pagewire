// Package mock provides an sdr.Device backed by a sample generator for tests
// and offline runs.
package mock

import (
	"context"
	"sort"

	"github.com/gopager/pagewire/internal/sdr"
)

// Generator fills raw interleaved uint8 IQ for the block starting at sample
// index start; it returns false when the stream should end.
type Generator func(dst []byte, start int64) bool

// Device is a mock sdr.Device.
type Device struct {
	gen   Generator
	freq  uint32
	rate  uint32
	gain  int
	ppm   int
	Calls []string
}

// New creates a mock device driven by gen.
func New(gen Generator) *Device { return &Device{gen: gen} }

// Info implements sdr.Device.
func (d *Device) Info() sdr.Info { return sdr.Info{Driver: "mock", Name: "mock", Tuner: "mock"} }

// SetCenterFreq implements sdr.Device.
func (d *Device) SetCenterFreq(hz uint32) error { d.freq = hz; return nil }

// CenterFreq implements sdr.Device.
func (d *Device) CenterFreq() uint32 { return d.freq }

// SetSampleRate implements sdr.Device.
func (d *Device) SetSampleRate(hz uint32) error { d.rate = hz; return nil }

// SampleRate implements sdr.Device.
func (d *Device) SampleRate() uint32 { return d.rate }

// SetPPM implements sdr.Device.
func (d *Device) SetPPM(ppm int) error { d.ppm = ppm; return nil }

// Gains implements sdr.Device.
func (d *Device) Gains() []int { g := []int{0, 100, 200, 300, 400}; sort.Ints(g); return g }

// SetGain implements sdr.Device.
func (d *Device) SetGain(tenthDB int) error {
	d.gain = tenthDB
	d.Calls = append(d.Calls, "gain")
	return nil
}

// SetAGC implements sdr.Device.
func (d *Device) SetAGC() error { d.gain = -1; d.Calls = append(d.Calls, "agc"); return nil }

// SetBiasTee implements sdr.Device.
func (d *Device) SetBiasTee(bool) error { d.Calls = append(d.Calls, "biastee"); return nil }

// Stream implements sdr.Device by calling the generator until it ends or
// ctx is cancelled.
func (d *Device) Stream(ctx context.Context, blockLen int, fn sdr.BlockFunc) error {
	buf := make([]byte, blockLen)
	var pos int64
	for ctx.Err() == nil {
		if !d.gen(buf, pos) {
			return nil
		}
		fn(buf)
		pos += int64(blockLen / 2)
	}
	return nil
}

// Close implements sdr.Device.
func (d *Device) Close() error { return nil }

// Replay wraps a Device so the pipeline treats it like a recording: blocks
// are handed over with back-pressure instead of being dropped when
// processing is slower than the generator, and the capture rate and centre
// are fixed.
type Replay struct {
	*Device
	Rate   uint32
	Center uint32
}

// NewReplay creates a replay-style mock at a fixed rate and centre.
func NewReplay(gen Generator, rate, centerHz uint32) *Replay {
	d := New(gen)
	d.rate, d.freq = rate, centerHz
	return &Replay{Device: d, Rate: rate, Center: centerHz}
}

// FixedTuning implements sdr.FixedTuning.
func (r *Replay) FixedTuning() (uint32, uint32) { return r.Rate, r.Center }
