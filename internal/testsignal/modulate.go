package testsignal

import (
	"math"
	"math/cmplx"
)

// FSKParams describes the 2-level FSK modulation of a bit stream.
type FSKParams struct {
	Baud        float64
	DeviationHz float64
	// Inverted sends a 1 bit as the lower frequency, as POCSAG does.
	Inverted bool
}

// POCSAGFSK returns the standard POCSAG parameters for a baud rate.
func POCSAGFSK(baud int) FSKParams {
	return FSKParams{Baud: float64(baud), DeviationHz: 4500, Inverted: true}
}

// FSKAudio renders the bit stream as the discriminator output a receiver
// would produce: a rectangular ±deviation waveform at sampleRate, scaled so
// that ±sampleRate/2 maps to ±1 (the pipeline's convention). amplitude
// scales the result and noise adds uniform noise of that peak level.
func FSKAudio(bits []byte, p FSKParams, sampleRate float64, leadInSeconds, tailSeconds float64, noise float64, rnd func() float64) []float32 {
	full := p.DeviationHz / (sampleRate / 2)
	n := int(float64(len(bits))/p.Baud*sampleRate) + int((leadInSeconds+tailSeconds)*sampleRate)
	out := make([]float32, n)
	lead := int(leadInSeconds * sampleRate)
	for i := range out {
		v := 0.0
		if i >= lead {
			bit := int(float64(i-lead) / sampleRate * p.Baud)
			if bit < len(bits) {
				v = full
				if (bits[bit] == 1) == p.Inverted {
					v = -full
				}
			}
		}
		if noise > 0 && rnd != nil {
			v += (rnd()*2 - 1) * noise
		}
		out[i] = float32(v)
	}
	return out
}

// FMModulateIQ turns a discriminator-style baseband (±1 = ±sampleRate/2) into
// complex IQ at the same sample rate, centred at offsetHz, with the given
// amplitude (1 = full scale of one component).
func FMModulateIQ(baseband []float32, sampleRate, offsetHz, amplitude float64) []complex64 {
	out := make([]complex64, len(baseband))
	phase := 0.0
	for i, v := range baseband {
		freq := float64(v)*sampleRate/2 + offsetHz
		phase += 2 * math.Pi * freq / sampleRate
		if phase > math.Pi {
			phase -= 2 * math.Pi
		} else if phase < -math.Pi {
			phase += 2 * math.Pi
		}
		out[i] = complex64(cmplx.Rect(amplitude, phase))
	}
	return out
}

// Upsample repeats each baseband sample factor times (zero-order hold); for
// a rectangular FSK waveform that is exact.
func Upsample(src []float32, factor int) []float32 {
	out := make([]float32, len(src)*factor)
	for i, v := range src {
		for k := 0; k < factor; k++ {
			out[i*factor+k] = v
		}
	}
	return out
}

// ToCU8 converts IQ to the interleaved unsigned 8-bit format of RTL-SDR
// captures, adding uniform noise of the given peak level (in units of full
// scale) when noise > 0.
func ToCU8(iq []complex64, noise float64, rnd func() float64) []byte {
	out := make([]byte, 2*len(iq))
	for i, z := range iq {
		re, im := float64(real(z)), float64(imag(z))
		if noise > 0 && rnd != nil {
			re += (rnd()*2 - 1) * noise
			im += (rnd()*2 - 1) * noise
		}
		out[2*i] = clampByte(127.5 + 127.5*re)
		out[2*i+1] = clampByte(127.5 + 127.5*im)
	}
	return out
}

func clampByte(v float64) byte {
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	default:
		return byte(v + 0.5)
	}
}
