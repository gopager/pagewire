// Package iqfile replays a raw unsigned 8-bit IQ capture (the .cu8 format of
// rtl_sdr and the record command) as an sdr.Device.
package iqfile

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gopager/pagewire/internal/sdr"
)

// Device replays a capture file.
type Device struct {
	r      io.Reader
	closer io.Closer
	rate   uint32
	center uint32
	// Realtime paces the replay at the sample rate instead of running as
	// fast as possible.
	Realtime bool
}

// Open opens path for replay; rate and centre describe how it was captured.
func Open(path string, rate, centerHz uint32) (*Device, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &Device{r: f, closer: f, rate: rate, center: centerHz}, nil
}

// New replays IQ from an arbitrary reader.
func New(r io.Reader, rate, centerHz uint32) *Device {
	return &Device{r: r, rate: rate, center: centerHz}
}

// FixedTuning implements sdr.FixedTuning.
func (d *Device) FixedTuning() (uint32, uint32) { return d.rate, d.center }

// Info implements sdr.Device.
func (d *Device) Info() sdr.Info {
	return sdr.Info{Driver: "iqfile", Name: "IQ replay", Tuner: "file"}
}

// SetCenterFreq implements sdr.Device; the recorded centre cannot change,
// so anything else is an error.
func (d *Device) SetCenterFreq(hz uint32) error {
	if hz != d.center {
		return fmt.Errorf("iqfile: capture was taken at %d Hz, cannot tune to %d Hz", d.center, hz)
	}
	return nil
}

// CenterFreq implements sdr.Device.
func (d *Device) CenterFreq() uint32 { return d.center }

// SetSampleRate implements sdr.Device; only the recorded rate is accepted.
func (d *Device) SetSampleRate(hz uint32) error {
	if hz != d.rate {
		return fmt.Errorf("iqfile: capture was taken at %d S/s, cannot use %d S/s (set the source sample_rate to match)", d.rate, hz)
	}
	return nil
}

// SampleRate implements sdr.Device.
func (d *Device) SampleRate() uint32 { return d.rate }

// SetPPM implements sdr.Device (no effect on a recording).
func (d *Device) SetPPM(int) error { return nil }

// Gains implements sdr.Device.
func (d *Device) Gains() []int { return nil }

// SetGain implements sdr.Device (no effect on a recording).
func (d *Device) SetGain(int) error { return nil }

// SetAGC implements sdr.Device (no effect on a recording).
func (d *Device) SetAGC() error { return nil }

// SetBiasTee implements sdr.Device (no effect on a recording).
func (d *Device) SetBiasTee(bool) error { return nil }

// Stream implements sdr.Device. It returns nil at end of file.
func (d *Device) Stream(ctx context.Context, blockLen int, fn sdr.BlockFunc) error {
	buf := make([]byte, blockLen)
	blockDur := time.Duration(float64(blockLen/2) / float64(d.rate) * float64(time.Second))
	next := time.Now()
	for ctx.Err() == nil {
		n, err := io.ReadFull(d.r, buf)
		if n > 0 {
			// Keep whole samples and a length librtlsdr would also deliver.
			fn(buf[:n-n%2])
		}
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return err
		}
		if d.Realtime {
			next = next.Add(blockDur)
			if wait := time.Until(next); wait > 0 {
				select {
				case <-time.After(wait):
				case <-ctx.Done():
				}
			}
		}
	}
	return nil
}

// Close implements sdr.Device.
func (d *Device) Close() error {
	if d.closer != nil {
		return d.closer.Close()
	}
	return nil
}
