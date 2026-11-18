package testsignal

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// SelcallTable holds the 16 tone frequencies in Hz of a selective-call
// variant, indexed by the tone number (digit 0-9, A-F). The values are the
// nominal frequencies of the multimon-ng demodulator tables.
type SelcallTable [16]float64

// Tone tables of the sequential tone systems.
var (
	ZVEI1 = SelcallTable{2400, 1060, 1160, 1270, 1400, 1530, 1670, 1830, 2000, 2200, 2800, 810, 970, 885, 2600, 680}
	ZVEI2 = SelcallTable{2400, 1060, 1160, 1270, 1400, 1530, 1670, 1830, 2000, 2200, 885, 825, 740, 680, 970, 2600}
	ZVEI3 = SelcallTable{2400, 1060, 1160, 1270, 1400, 1530, 1670, 1830, 2000, 2200, 885, 810, 2800, 680, 970, 2600}
	DZVEI = SelcallTable{2200, 970, 1060, 1160, 1270, 1400, 1530, 1670, 1830, 2000, 825, 740, 2600, 885, 2400, 680}
	PZVEI = SelcallTable{2400, 1060, 1160, 1270, 1400, 1530, 1670, 1830, 2000, 2200, 970, 810, 2800, 885, 2400, 680}
	CCIR  = SelcallTable{1981, 1124, 1197, 1275, 1358, 1446, 1540, 1640, 1747, 1860, 2400, 930, 2247, 991, 2110, 1055}
	EEA   = SelcallTable{1981, 1124, 1197, 1275, 1358, 1446, 1540, 1640, 1747, 1860, 1055, 930, 2400, 991, 2110, 2247}
	EIA   = SelcallTable{600, 741, 882, 1023, 1164, 1305, 1446, 1587, 1728, 1869, 2151, 2433, 2010, 2292, 459, 1091}
)

// DTMF row (low group) and column (high group) frequencies in Hz.
var (
	dtmfRows = [4]float64{697, 770, 852, 941}
	dtmfCols = [4]float64{1209, 1336, 1477, 1633}
)

// dtmfKeys lays the keypad out row by row.
const dtmfKeys = "123A456B789C*0#D"

// Dualtone frequencies in Hz (TR-BOS C 4.6): fs4 with fs5 or fs7.
const (
	DualtoneFS4 = 675
	DualtoneFS5 = 825
	DualtoneFS7 = 1240
)

// Tone is a segment of a tone sequence: the frequencies sounded together
// for the duration; no frequencies is silence.
type Tone struct {
	Freqs    []float64
	Duration time.Duration
}

// SelcallDefaultTone is the tone duration of ZVEI.
const SelcallDefaultTone = 70 * time.Millisecond

// SelcallSequence returns the tones of a digit string (0-9, A-F) of one
// variant, each toneDur long and back to back.
func SelcallSequence(table SelcallTable, digits string, toneDur time.Duration) ([]Tone, error) {
	out := make([]Tone, 0, len(digits))
	for _, r := range strings.ToUpper(digits) {
		var idx int
		switch {
		case r >= '0' && r <= '9':
			idx = int(r - '0')
		case r >= 'A' && r <= 'F':
			idx = int(r-'A') + 10
		default:
			return nil, fmt.Errorf("testsignal: %q is not a selcall digit", r)
		}
		out = append(out, Tone{Freqs: []float64{table[idx]}, Duration: toneDur})
	}
	return out, nil
}

// DTMFSequence returns the tones of a key string, each key sounded for
// toneDur followed by gap of silence.
func DTMFSequence(keys string, toneDur, gap time.Duration) ([]Tone, error) {
	out := make([]Tone, 0, 2*len(keys))
	for _, r := range keys {
		idx := strings.IndexRune(dtmfKeys, r)
		if idx < 0 {
			return nil, fmt.Errorf("testsignal: %q is not a DTMF key", r)
		}
		out = append(out,
			Tone{Freqs: []float64{dtmfRows[idx/4], dtmfCols[idx%4]}, Duration: toneDur},
			Tone{Duration: gap})
	}
	return out, nil
}

// ToneAudio renders the tones as audio at sampleRate. Each sounded
// frequency has amplitude/len(freqs) so that the sum never exceeds
// amplitude; the phase is continuous within a tone and restarts with each
// tone.
func ToneAudio(tones []Tone, sampleRate, amplitude float64) []float32 {
	var out []float32
	for _, t := range tones {
		n := int(t.Duration.Seconds() * sampleRate)
		for i := 0; i < n; i++ {
			v := 0.0
			for _, f := range t.Freqs {
				v += amplitude / float64(len(t.Freqs)) * math.Sin(2*math.Pi*f*float64(i)/sampleRate)
			}
			out = append(out, float32(v))
		}
	}
	return out
}

// Silence returns a pause of the given length for use around tone audio.
func Silence(d time.Duration, sampleRate float64) []float32 {
	return make([]float32, int(d.Seconds()*sampleRate))
}
