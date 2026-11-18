package dsp

import (
	"fmt"
	"math"
)

// KaiserLowPass designs a linear-phase low-pass FIR with a Kaiser window.
// cutoff and transition are in cycles per sample (0 < cutoff, cutoff +
// transition <= 0.5); attenuationDB is the required stop-band attenuation.
// The filter has unity DC gain.
func KaiserLowPass(cutoff, transition, attenuationDB float64) []float32 {
	beta := 0.0
	switch {
	case attenuationDB > 50:
		beta = 0.1102 * (attenuationDB - 8.7)
	case attenuationDB >= 21:
		beta = 0.5842*math.Pow(attenuationDB-21, 0.4) + 0.07886*(attenuationDB-21)
	}
	taps := int(math.Ceil((attenuationDB-8)/(2.285*2*math.Pi*transition))) + 1
	if taps < 5 {
		taps = 5
	}
	if taps%2 == 0 {
		taps++
	}
	h := make([]float32, taps)
	mid := float64(taps-1) / 2
	i0beta := besselI0(beta)
	var sum float64
	for n := range h {
		x := float64(n) - mid
		var sinc float64
		if x == 0 {
			sinc = 2 * cutoff
		} else {
			sinc = math.Sin(2*math.Pi*cutoff*x) / (math.Pi * x)
		}
		r := 2 * x / float64(taps-1)
		w := besselI0(beta*math.Sqrt(1-r*r)) / i0beta
		v := sinc * w
		h[n] = float32(v)
		sum += v
	}
	for n := range h {
		h[n] = float32(float64(h[n]) / sum)
	}
	return h
}

// besselI0 is the zeroth-order modified Bessel function of the first kind.
func besselI0(x float64) float64 {
	sum, term := 1.0, 1.0
	half := x / 2
	for k := 1; k < 50; k++ {
		term *= (half / float64(k)) * (half / float64(k))
		sum += term
		if term < 1e-12*sum {
			break
		}
	}
	return sum
}

// Decimator low-pass filters a complex signal and keeps every factor-th
// sample. It keeps the filter history between calls so blocks can be fed
// continuously.
type Decimator struct {
	taps   []float32
	factor int
	work   []complex64
	phase  int // input samples to skip before the next output
	maxIn  int
	outBuf []complex64
}

// NewDecimator creates a decimator for blocks of at most maxIn input samples.
func NewDecimator(taps []float32, factor, maxIn int) *Decimator {
	if factor < 1 {
		panic("dsp: decimation factor must be >= 1")
	}
	d := &Decimator{taps: taps, factor: factor, maxIn: maxIn}
	d.work = make([]complex64, len(taps)-1, len(taps)-1+maxIn)
	d.outBuf = make([]complex64, maxIn/factor+1)
	return d
}

// Factor returns the decimation factor.
func (d *Decimator) Factor() int { return d.factor }

// Process filters and decimates src and returns the produced output samples.
// The returned slice is owned by the decimator and valid until the next call.
func (d *Decimator) Process(src []complex64) []complex64 {
	if len(src) > d.maxIn {
		panic(fmt.Sprintf("dsp: block of %d exceeds decimator capacity %d", len(src), d.maxIn))
	}
	hist := len(d.taps) - 1
	work := append(d.work[:hist], src...)
	taps := d.taps
	out := d.outBuf[:0]
	i := d.phase
	for ; i+hist < len(work); i += d.factor {
		win := work[i : i+len(taps)]
		var re, im float32
		for k, h := range taps {
			re += real(win[k]) * h
			im += imag(win[k]) * h
		}
		out = append(out, complex(re, im))
	}
	// i now points past the last consumed input; the distance to the end
	// of work is how many inputs to skip at the start of the next block.
	d.phase = i - (len(work) - hist)
	copy(d.work[:hist], work[len(work)-hist:])
	d.work = d.work[:hist]
	return out
}

// DecimationChain cascades decimators so that a wide-band IQ stream is
// brought down to a channel rate with modest filter lengths.
type DecimationChain struct {
	stages []*Decimator
}

// factorise splits a decimation factor into stages, large factors last so
// the expensive narrow filter runs at the lowest possible rate.
func factorise(d int) []int {
	var out []int
	for _, p := range []int{2, 3, 5, 7} {
		for d%p == 0 {
			out = append(out, p)
			d /= p
		}
	}
	if d > 1 {
		out = append(out, d)
	}
	// Merge tiny leading factors into pairs so the chain stays short.
	for len(out) > 3 {
		out[1] *= out[0]
		out = out[1:]
	}
	return out
}

// NewDecimationChain builds a chain from inRate to outRate (inRate must be an
// integer multiple of outRate) that passes ±passHz flat and attenuates
// aliases by at least attenuationDB. maxIn is the largest input block.
func NewDecimationChain(inRate, outRate uint32, passHz, attenuationDB float64, maxIn int) (*DecimationChain, error) {
	if inRate%outRate != 0 {
		return nil, fmt.Errorf("dsp: %d is not an integer multiple of %d", inRate, outRate)
	}
	if 2*passHz >= float64(outRate) {
		return nil, fmt.Errorf("dsp: pass band ±%.0f Hz does not fit into %d S/s", passHz, outRate)
	}
	c := &DecimationChain{}
	rate := float64(inRate)
	blk := maxIn
	for _, f := range factorise(int(inRate / outRate)) {
		next := rate / float64(f)
		// Anything above next-passHz would alias onto the pass band after
		// this stage, so the transition band ends there.
		cutoff := passHz / rate
		transition := (next - 2*passHz) / rate
		taps := KaiserLowPass(cutoff, transition, attenuationDB)
		c.stages = append(c.stages, NewDecimator(taps, f, blk))
		rate = next
		blk = blk/f + 1
	}
	return c, nil
}

// Taps returns the total number of filter taps for reporting and tests.
func (c *DecimationChain) Taps() []int {
	out := make([]int, len(c.stages))
	for i, s := range c.stages {
		out[i] = len(s.taps)
	}
	return out
}

// Process runs the block through every stage. The result is owned by the
// chain and valid until the next call.
func (c *DecimationChain) Process(src []complex64) []complex64 {
	out := src
	for _, s := range c.stages {
		out = s.Process(out)
	}
	return out
}
