package calibrate

import (
	"log/slog"
	"math"
	"math/cmplx"
	"testing"

	"github.com/gopager/pagewire/internal/dsp"
)

func TestFFT(t *testing.T) {
	n := 1024
	x := make([]complex128, n)
	for i := range x {
		x[i] = cmplx.Rect(1, 2*math.Pi*100*float64(i)/float64(n))
	}
	dsp.FFT(x)
	if m := cmplx.Abs(x[100]); math.Abs(m-float64(n)) > 1e-6*float64(n) {
		t.Errorf("bin 100 = %v, want %d", m, n)
	}
	if m := cmplx.Abs(x[101]); m > 1e-6*float64(n) {
		t.Errorf("leakage at bin 101 = %v", m)
	}
}

// TestDetectFCCH synthesises a GSM-like signal: FCCH tone bursts at the
// expected position plus a 320 Hz carrier error, separated by noise-like
// modulated data, and expects the offset back.
func TestDetectFCCH(t *testing.T) {
	const fs = gsmCaptureHz
	expected := -gsmLOOffsetHz + fcchOffsetHz
	seed := uint32(9)
	n := fs / 2 // half a second
	iq := make([]complex64, n)
	rate := float64(fs)
	burstLen := int(float64(fcchBits) / gsmBitRate * rate)
	period := int(0.0461538 * rate) // one 51-multiframe is 235 ms; use a shorter period for the test
	phase := 0.0
	for i := range iq {
		var z complex128
		if i%period < burstLen {
			phase += 2 * math.Pi * (expected + 320) / rate
			z = cmplx.Rect(0.5, phase)
		} else {
			// pseudo-random GMSK-ish data: random frequency steps
			seed = seed*1664525 + 1013904223
			f := -gsmLOOffsetHz + (float64(seed>>8)/float64(1<<24)-0.5)*gsmBitRate
			phase += 2 * math.Pi * f / rate
			z = cmplx.Rect(0.5, phase)
		}
		seed = seed*1664525 + 1013904223
		noise := complex((float64(seed>>8)/float64(1<<24)-0.5)*0.05, 0)
		iq[i] = complex64(z + noise)
	}
	off, bursts := detectFCCH(iq, rate, expected)
	if bursts < 5 {
		t.Fatalf("found %d bursts", bursts)
	}
	if math.Abs(off-320) > 40 {
		t.Errorf("offset = %.0f Hz, want 320", off)
	}
	_ = slog.Default()
}
