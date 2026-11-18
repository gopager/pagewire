package testsignal

import (
	"math"
	"testing"
)

func TestEASBurstBits(t *testing.T) {
	bits := EASBurstBits("ZC")
	if len(bits) != (16+2)*8 {
		t.Fatalf("len = %d", len(bits))
	}
	// 0xAB sent least significant bit first: 1 1 0 1 0 1 0 1.
	pre := []byte{1, 1, 0, 1, 0, 1, 0, 1}
	for i := 0; i < 16; i++ {
		for k, b := range pre {
			if bits[i*8+k] != b {
				t.Fatalf("preamble byte %d bit %d = %d", i, k, bits[i*8+k])
			}
		}
	}
	// 'Z' = 0x5A: 0 1 0 1 1 0 1 0 LSB first.
	for k, b := range []byte{0, 1, 0, 1, 1, 0, 1, 0} {
		if bits[128+k] != b {
			t.Fatalf("'Z' bit %d = %d", k, bits[128+k])
		}
	}
}

func TestEASAudioTonesAndTiming(t *testing.T) {
	rate := 22050
	// A run of mark bits is a 2083.3 Hz tone, a run of space bits 1562.5 Hz.
	for _, tc := range []struct {
		bit  byte
		freq float64
	}{{1, EASMarkHz}, {0, EASSpaceHz}} {
		bits := make([]byte, 100)
		for i := range bits {
			bits[i] = tc.bit
		}
		a := easModulate(bits, float64(rate))
		if want := int(100 / EASBaud * float64(rate)); len(a) < want-1 || len(a) > want+1 {
			t.Errorf("length = %d, want about %d", len(a), want)
		}
		cross := 0
		for i := 1; i < len(a); i++ {
			if (a[i-1] < 0) != (a[i] < 0) {
				cross++
			}
		}
		got := float64(cross) / 2 / (float64(len(a)) / float64(rate))
		if math.Abs(got-tc.freq) > 30 {
			t.Errorf("bit %d: frequency %.1f, want %.1f", tc.bit, got, tc.freq)
		}
	}
}

func TestEASAudioLayout(t *testing.T) {
	rate := 22050
	a := EASAudio([]string{"ZCZC-A-", "ZCZC-A-"}, float64(rate), EASOptions{Pause: 1, LeadIn: 0.5, Tail: 0.5, Amplitude: 0.5})
	burst := int(float64((16+7)*8) / EASBaud * float64(rate))
	want := burst*2 + rate + rate/2 + rate/2
	if d := len(a) - want; d < -4 || d > 4 {
		t.Errorf("length = %d, want about %d", len(a), want)
	}
	for _, v := range a[:rate/2-10] {
		if v != 0 {
			t.Fatal("lead-in is not silent")
		}
	}
}
