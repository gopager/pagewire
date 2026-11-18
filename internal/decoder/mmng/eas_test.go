//go:build cgo

package mmng

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	"github.com/gopager/pagewire/internal/message"
	"github.com/gopager/pagewire/internal/testsignal"
)

const easHeader = "ZCZC-WXR-TOR-039173-039051+0030-1591829-KCLE/NWS-"

func newEASDec(t *testing.T, opts *config.EASOptions) (decoder.Decoder, *sink) {
	t.Helper()
	s := &sink{}
	d, err := decoder.New(config.DecoderConfig{Protocol: "eas", EAS: opts}, s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, s
}

func easAudio(msgs []string, noise float64) []float32 {
	return testsignal.EASAudio(msgs, 22050, testsignal.EASOptions{
		Pause: 1, LeadIn: 0.3, Tail: 0.5, Amplitude: 0.5, Noise: noise, Rand: rand.New(rand.NewSource(5)).Float64,
	})
}

func feedEAS(d decoder.Decoder, audio []float32) {
	for i := 0; i < len(audio); i += 1000 {
		end := min(i+1000, len(audio))
		d.Process(audio[i:end])
	}
}

func TestEASRoundTrip(t *testing.T) {
	d, s := newEASDec(t, nil)
	if d.SampleRate() != 22050 {
		t.Fatalf("sample rate = %d", d.SampleRate())
	}
	feedEAS(d, easAudio(testsignal.EASTransmission(easHeader, true), 0.02))
	if len(s.msgs) != 1 {
		t.Fatalf("got %d messages, want 1 (end of message is off): %+v", len(s.msgs), s.msgs)
	}
	m := s.msgs[0]
	if m.Kind != message.KindAlert || m.Address != "WXR-TOR" || m.Text != easHeader || m.Function != -1 {
		t.Errorf("message = %+v", m)
	}
	p := m.Payload.(*message.EASPayload)
	if p.Kind != message.EASHeader || p.Header != easHeader || !p.Valid || p.Originator != "WXR" || p.EventCode != "TOR" ||
		len(p.Locations) != 2 || p.Locations[0] != "039173" || p.Locations[1] != "039051" ||
		p.Purge != "0030" || p.PurgeMinutes != 30 || p.Issued != "1591829" || p.IssuedDay != 159 || p.Callsign != "KCLE/NWS" {
		t.Errorf("payload = %+v", p)
	}
	// The decoder accepts the header at the second agreeing burst.
	if p.Votes != 2 || p.Burst != 2 || len(p.Copies) != 2 || p.AllCopiesAgree {
		t.Errorf("vote info = votes %d burst %d copies %q all %v", p.Votes, p.Burst, p.Copies, p.AllCopiesAgree)
	}
	for _, c := range p.Copies {
		if c != easHeader {
			t.Errorf("copy = %q", c)
		}
	}
}

func TestEASEndOfMessageOption(t *testing.T) {
	d, s := newEASDec(t, &config.EASOptions{EndOfMessage: true})
	feedEAS(d, easAudio(testsignal.EASTransmission(easHeader, true), 0.02))
	if len(s.msgs) < 2 {
		t.Fatalf("got %d messages", len(s.msgs))
	}
	if s.msgs[0].Text != easHeader {
		t.Errorf("first = %+v", s.msgs[0])
	}
	for _, m := range s.msgs[1:] {
		p, ok := m.Payload.(*message.EASPayload)
		if !ok || p.Kind != message.EASEOM || m.Text != "NNNN" || m.Address != "NNNN" || m.Kind != message.KindAlert {
			t.Errorf("end-of-message record = %+v / %+v", m, m.Payload)
		}
	}
}

// One damaged copy does not prevent delivery of the two that agree, and the
// record shows the copies as they were received.
func TestEASDamagedCopy(t *testing.T) {
	damaged := strings.Replace(easHeader, "039051", "039052", 1)
	d, s := newEASDec(t, nil)
	feedEAS(d, easAudio([]string{easHeader, damaged, easHeader}, 0.02))
	if len(s.msgs) != 1 {
		t.Fatalf("got %d messages", len(s.msgs))
	}
	p := s.msgs[0].Payload.(*message.EASPayload)
	if s.msgs[0].Text != easHeader || p.Votes != 2 || p.Burst != 3 || len(p.Copies) != 3 || p.Copies[1] != damaged || p.AllCopiesAgree {
		t.Errorf("message %q payload %+v", s.msgs[0].Text, p)
	}
}

func TestEASSingleCopyIsNotAccepted(t *testing.T) {
	d, s := newEASDec(t, nil)
	feedEAS(d, easAudio([]string{easHeader}, 0.02))
	if len(s.msgs) != 0 {
		t.Errorf("one copy must not be accepted: %+v", s.msgs)
	}
}

func TestEASSecondAlertIsDelivered(t *testing.T) {
	other := "ZCZC-CIV-CEM-048453+0100-2001200-KXYZ/NWS-"
	d, s := newEASDec(t, nil)
	feedEAS(d, easAudio(testsignal.EASTransmission(easHeader, false), 0.02))
	feedEAS(d, easAudio(testsignal.EASTransmission(other, false), 0.02))
	if len(s.msgs) != 2 || s.msgs[1].Text != other || s.msgs[1].Address != "CIV-CEM" {
		t.Errorf("got %+v", s.msgs)
	}
}

// TestEASAgainstMultimonBinary decodes the same recording with the embedded
// decoder and the multimon-ng binary and compares the delivered headers.
func TestEASAgainstMultimonBinary(t *testing.T) {
	bin, err := exec.LookPath("multimon-ng")
	if err != nil {
		t.Skip("multimon-ng not installed")
	}
	other := "ZCZC-CIV-CEM-048453+0100-2001200-KXYZ/NWS-"
	audio := append(easAudio(testsignal.EASTransmission(easHeader, true), 0.02), easAudio(testsignal.EASTransmission(other, true), 0.02)...)
	var raw bytes.Buffer
	for _, v := range audio {
		_ = binary.Write(&raw, binary.LittleEndian, int16(v*32767))
	}
	file := filepath.Join(t.TempDir(), "same.s16le")
	if err := os.WriteFile(file, raw.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "-t", "raw", "-a", "EAS", file).CombinedOutput()
	if err != nil {
		t.Fatalf("multimon-ng: %v\n%s", err, out)
	}
	var want []string
	for _, line := range strings.Split(string(out), "\n") {
		if h, ok := strings.CutPrefix(line, "EAS: "); ok && strings.HasPrefix(h, "ZCZC") {
			want = append(want, strings.TrimSpace(h))
		}
	}
	d, s := newEASDec(t, nil)
	feedEAS(d, audio)
	var got []string
	for _, m := range s.msgs {
		got = append(got, m.Text)
	}
	if len(want) != 2 || strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("embedded decoder %q, multimon-ng %q\n%s", got, want, out)
	}
}
