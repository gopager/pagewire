package dsp

import "math"

// FMDemod is a polar discriminator: the phase difference between successive
// samples is proportional to the instantaneous frequency. Output samples lie
// in [-1, 1] where ±1 corresponds to ±sampleRate/2 of deviation.
type FMDemod struct {
	prev complex64
	// Fast selects the polynomial atan2 approximation (max error about
	// 0.01 rad) instead of math.Atan2.
	Fast bool
}

// NewFMDemod returns a demodulator using the fast approximation.
func NewFMDemod() *FMDemod { return &FMDemod{Fast: true} }

// Demod writes len(src) audio samples into dst.
func (f *FMDemod) Demod(dst []float32, src []complex64) {
	prev := f.prev
	const invPi = float32(1 / math.Pi)
	if f.Fast {
		for i, z := range src {
			// z * conj(prev)
			re := real(z)*real(prev) + imag(z)*imag(prev)
			im := imag(z)*real(prev) - real(z)*imag(prev)
			dst[i] = fastAtan2(im, re) * invPi
			prev = z
		}
	} else {
		for i, z := range src {
			re := real(z)*real(prev) + imag(z)*imag(prev)
			im := imag(z)*real(prev) - real(z)*imag(prev)
			dst[i] = float32(math.Atan2(float64(im), float64(re))) * invPi
			prev = z
		}
	}
	f.prev = prev
}

// fastAtan2 approximates atan2 with a cubic polynomial on the octant-reduced
// ratio; the maximum error is about 0.01 rad (0.3 % of full deviation),
// far below the noise of any narrow-band FM channel.
func fastAtan2(y, x float32) float32 {
	const (
		pi4  = float32(math.Pi / 4)
		pi34 = float32(3 * math.Pi / 4)
	)
	absY := y
	if absY < 0 {
		absY = -absY
	}
	absY += 1e-20
	var angle float32
	if x >= 0 {
		r := (x - absY) / (x + absY)
		angle = pi4 + (0.1963*r*r-0.9817)*r
	} else {
		r := (x + absY) / (absY - x)
		angle = pi34 + (0.1963*r*r-0.9817)*r
	}
	if y < 0 {
		return -angle
	}
	return angle
}
