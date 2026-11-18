package dsp

import (
	"math"
	"math/cmplx"
)

// NCO is a numerically controlled oscillator that shifts a signal in
// frequency by multiplying it with a complex exponential. The rotation is
// advanced incrementally and renormalised periodically so no trigonometric
// function is evaluated per sample.
type NCO struct {
	rot   complex64
	step  complex64
	count int
}

// NewNCO returns an oscillator producing exp(-j·2π·shiftHz/sampleRate·n), so
// that a signal at +shiftHz lands at 0 Hz.
func NewNCO(shiftHz float64, sampleRate float64) *NCO {
	w := -2 * math.Pi * shiftHz / sampleRate
	return &NCO{rot: 1, step: complex64(cmplx.Exp(complex(0, w)))}
}

// Mix multiplies src by the oscillator into dst (which may alias src).
func (n *NCO) Mix(dst, src []complex64) {
	rot, step := n.rot, n.step
	for i, z := range src {
		dst[i] = z * rot
		rot *= step
	}
	n.count += len(src)
	if n.count >= 4096 {
		// Renormalise to stop the magnitude drifting away from 1.
		mag := float32(math.Hypot(float64(real(rot)), float64(imag(rot))))
		rot = complex(real(rot)/mag, imag(rot)/mag)
		n.count = 0
	}
	n.rot = rot
}
