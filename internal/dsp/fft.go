package dsp

import (
	"math"
	"math/cmplx"
)

// FFT computes the in-place radix-2 discrete Fourier transform of x, whose
// length must be a power of two.
func FFT(x []complex128) {
	n := len(x)
	if n&(n-1) != 0 {
		panic("dsp: FFT length must be a power of two")
	}
	// bit reversal
	for i, j := 0, 0; i < n; i++ {
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
		m := n >> 1
		for m >= 1 && j&m != 0 {
			j ^= m
			m >>= 1
		}
		j |= m
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			wk := complex(1, 0)
			for k := 0; k < size/2; k++ {
				a, b := x[start+k], x[start+k+size/2]*wk
				x[start+k], x[start+k+size/2] = a+b, a-b
				wk *= w
			}
		}
	}
}
