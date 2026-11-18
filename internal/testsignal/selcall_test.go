package testsignal

import (
	"testing"
	"time"
)

func TestSelcallSequence(t *testing.T) {
	tones, err := SelcallSequence(ZVEI1, "1aF", SelcallDefaultTone)
	if err != nil || len(tones) != 3 || tones[0].Freqs[0] != 1060 || tones[1].Freqs[0] != 2800 || tones[2].Freqs[0] != 680 {
		t.Errorf("tones = %+v, err = %v", tones, err)
	}
	if _, err := SelcallSequence(ZVEI1, "1G", SelcallDefaultTone); err == nil {
		t.Error("G is not a selcall digit")
	}
}

func TestDTMFSequence(t *testing.T) {
	tones, err := DTMFSequence("5D", 50*time.Millisecond, 20*time.Millisecond)
	if err != nil || len(tones) != 4 {
		t.Fatalf("tones = %+v, err = %v", tones, err)
	}
	if f := tones[0].Freqs; f[0] != 770 || f[1] != 1336 {
		t.Errorf("key 5 = %v", f)
	}
	if f := tones[2].Freqs; f[0] != 941 || f[1] != 1633 {
		t.Errorf("key D = %v", f)
	}
	if _, err := DTMFSequence("E", time.Second, time.Second); err == nil {
		t.Error("E is not a DTMF key")
	}
}

func TestToneAudioLength(t *testing.T) {
	a := ToneAudio([]Tone{{Freqs: []float64{1000}, Duration: 100 * time.Millisecond}, {Duration: 50 * time.Millisecond}}, 22050, 0.5)
	if len(a) != 2205+1102 {
		t.Errorf("len = %d", len(a))
	}
}
