package testsignal

import "math"

// SAME (EAS) modulation parameters from the specification: AFSK at
// 520.83 baud, mark (1) 2083.3 Hz, space (0) 1562.5 Hz, bytes sent least
// significant bit first, every header preceded by 16 bytes 0xAB.
const (
	EASBaud        = 520.83
	EASMarkHz      = 2083.3
	EASSpaceHz     = 1562.5
	easPreamble    = 0xAB
	easPreambleLen = 16
)

// EASOptions shapes the audio of a SAME transmission.
type EASOptions struct {
	// Pause is the silence between bursts in seconds (SAME: 1).
	Pause float64
	// LeadIn and Tail are silence before the first and after the last burst.
	LeadIn, Tail float64
	// Amplitude is the peak level of the tones (0..1).
	Amplitude float64
	// Noise is the peak level of added uniform noise; Rand supplies values in
	// [0,1) and must be set when Noise > 0.
	Noise float64
	Rand  func() float64
}

// EASBurstBits returns the bits of one burst: the preamble followed by msg
// (a "ZCZC-..." header or "NNNN"), each byte least significant bit first.
func EASBurstBits(msg string) []byte {
	data := make([]byte, 0, easPreambleLen+len(msg))
	for i := 0; i < easPreambleLen; i++ {
		data = append(data, easPreamble)
	}
	data = append(data, msg...)
	bits := make([]byte, 0, 8*len(data))
	for _, b := range data {
		for k := 0; k < 8; k++ {
			bits = append(bits, (b>>k)&1)
		}
	}
	return bits
}

// EASTransmission returns the bursts of a SAME transmission: the header
// three times and, when eom is set, the end-of-message marker three times.
// The caller sends a pause between all of them (EASAudio does).
func EASTransmission(header string, eom bool) []string {
	out := []string{header, header, header}
	if eom {
		out = append(out, "NNNN", "NNNN", "NNNN")
	}
	return out
}

// easModulate renders bits as phase-continuous AFSK at sampleRate with
// amplitude 1.
func easModulate(bits []byte, sampleRate float64) []float32 {
	n := int(float64(len(bits)) / EASBaud * sampleRate)
	out := make([]float32, n)
	phase := 0.0
	for i := range out {
		bit := int(float64(i) / sampleRate * EASBaud)
		if bit >= len(bits) {
			bit = len(bits) - 1
		}
		f := EASSpaceHz
		if bits[bit] == 1 {
			f = EASMarkHz
		}
		out[i] = float32(math.Sin(phase))
		phase += 2 * math.Pi * f / sampleRate
	}
	return out
}

// EASAudio renders one burst per element of msgs (each a header or "NNNN")
// with pauses between them, as 22050 Hz style audio at sampleRate in [-1, 1].
func EASAudio(msgs []string, sampleRate float64, o EASOptions) []float32 {
	amp := o.Amplitude
	if amp == 0 {
		amp = 0.5
	}
	pause := int(o.Pause * sampleRate)
	out := make([]float32, int(o.LeadIn*sampleRate))
	for i, m := range msgs {
		if i > 0 {
			out = append(out, make([]float32, pause)...)
		}
		for _, v := range easModulate(EASBurstBits(m), sampleRate) {
			out = append(out, v*float32(amp))
		}
	}
	out = append(out, make([]float32, int(o.Tail*sampleRate))...)
	if o.Noise > 0 && o.Rand != nil {
		for i := range out {
			out[i] += float32((o.Rand()*2 - 1) * o.Noise)
		}
	}
	return out
}
