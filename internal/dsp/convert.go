// Package dsp contains the signal processing blocks between the IQ source and
// the protocol decoders: sample conversion, frequency shifting, decimation,
// FM demodulation and signal level estimation. All blocks process fixed-size
// blocks into caller-provided buffers and allocate nothing in steady state.
package dsp

import "math"

// iqLUT maps an unsigned 8-bit ADC value to a float in (-1, 1).
var iqLUT = func() (t [256]float32) {
	for i := range t {
		t[i] = float32((float64(i) - 127.5) / 127.5)
	}
	return t
}()

// ConvertIQ converts interleaved unsigned 8-bit IQ bytes into complex
// samples and returns how many bytes sat at the ADC limits (0 or 255). dst
// must hold len(src)/2 samples.
func ConvertIQ(dst []complex64, src []byte) (clipped int) {
	n := len(src) / 2
	dst = dst[:n]
	for i := 0; i < n; i++ {
		ib, qb := src[2*i], src[2*i+1]
		dst[i] = complex(iqLUT[ib], iqLUT[qb])
		if ib == 0 || ib == 255 {
			clipped++
		}
		if qb == 0 || qb == 255 {
			clipped++
		}
	}
	return clipped
}

// Power returns the mean power of the block relative to a full-scale
// component amplitude of 1.0.
func Power(src []complex64) float64 {
	if len(src) == 0 {
		return 0
	}
	var acc float64
	for _, z := range src {
		re, im := float64(real(z)), float64(imag(z))
		acc += re*re + im*im
	}
	return acc / float64(len(src))
}

// DB converts a power ratio to decibels, clamping zero to -200 dB.
func DB(power float64) float64 {
	if power <= 0 {
		return -200
	}
	return 10 * math.Log10(power)
}
