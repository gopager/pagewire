package dsp

import "math"

// Resampler converts a real signal between arbitrary sample rates with a
// windowed-sinc interpolation filter. It targets demodulated pager audio,
// where modest filter lengths are more than adequate.
type Resampler struct {
	ratio   float64 // output samples per input sample
	taps    []float32
	phases  int
	history []float32
	pos     float64 // position of the next output in input samples, relative to history start
	out     []float32
}

// NewResampler creates a converter from inRate to outRate for input blocks
// of at most maxIn samples.
func NewResampler(inRate, outRate uint32, maxIn int) *Resampler {
	const phases = 64
	ratio := float64(outRate) / float64(inRate)
	cutoff := 0.45 * math.Min(1, ratio) / float64(phases)
	proto := KaiserLowPass(cutoff, cutoff*0.5, 60)
	// Normalise each phase to unity gain.
	tapsPerPhase := (len(proto) + phases - 1) / phases
	taps := make([]float32, phases*tapsPerPhase)
	for ph := 0; ph < phases; ph++ {
		var sum float32
		for k := 0; k < tapsPerPhase; k++ {
			idx := k*phases + ph
			if idx < len(proto) {
				taps[ph*tapsPerPhase+k] = proto[idx]
				sum += proto[idx]
			}
		}
		for k := 0; k < tapsPerPhase; k++ {
			taps[ph*tapsPerPhase+k] /= sum
		}
	}
	r := &Resampler{ratio: ratio, taps: taps, phases: phases}
	r.history = make([]float32, tapsPerPhase, tapsPerPhase+maxIn)
	r.out = make([]float32, int(float64(maxIn)*ratio)+2)
	return r
}

// Process resamples src and returns the output, valid until the next call.
func (r *Resampler) Process(src []float32) []float32 {
	tapsPerPhase := len(r.taps) / r.phases
	work := append(r.history, src...)
	out := r.out[:0]
	step := 1 / r.ratio
	limit := float64(len(work) - tapsPerPhase)
	for r.pos < limit {
		i := int(r.pos)
		frac := r.pos - float64(i)
		ph := int(frac * float64(r.phases))
		h := r.taps[ph*tapsPerPhase : (ph+1)*tapsPerPhase]
		win := work[i : i+tapsPerPhase]
		var acc float32
		for k, c := range h {
			acc += win[k] * c
		}
		out = append(out, acc)
		r.pos += step
	}
	consumed := len(work) - tapsPerPhase
	r.pos -= float64(consumed)
	copy(r.history[:tapsPerPhase], work[consumed:])
	r.history = r.history[:tapsPerPhase]
	return out
}
