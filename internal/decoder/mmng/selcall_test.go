//go:build cgo

package mmng

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	"github.com/gopager/pagewire/internal/message"
	"github.com/gopager/pagewire/internal/testsignal"
)

const selRate = 22050

func newSel(t *testing.T, proto string, opts *config.SelcallOptions) (decoder.Decoder, *sink) {
	t.Helper()
	s := &sink{}
	d, err := decoder.New(config.DecoderConfig{Protocol: proto, Selcall: opts}, s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, s
}

// feedChunks passes audio in odd-sized blocks to exercise the carry buffer.
func feedChunks(d decoder.Decoder, audio []float32, chunk int) {
	for i := 0; i < len(audio); i += chunk {
		end := min(i+chunk, len(audio))
		d.Process(audio[i:end])
	}
}

// selAudio renders tones with a silent lead-in and tail.
func selAudio(t *testing.T, tones []testsignal.Tone, err error) []float32 {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	a := testsignal.Silence(200*time.Millisecond, selRate)
	a = append(a, testsignal.ToneAudio(tones, selRate, 0.5)...)
	return append(a, testsignal.Silence(400*time.Millisecond, selRate)...)
}

func TestSelcallRegistered(t *testing.T) {
	have := map[string]bool{}
	for _, p := range decoder.Available() {
		have[p] = true
	}
	for _, p := range []string{"zvei1", "zvei2", "zvei3", "dzvei", "pzvei", "ccir", "eea", "eia", "dtmf", "dualtone"} {
		if !have[p] {
			t.Errorf("%s is not registered", p)
		}
	}
}

func TestZVEI1RoundTrip(t *testing.T) {
	d, s := newSel(t, "zvei1", nil)
	tones, err := testsignal.SelcallSequence(testsignal.ZVEI1, "12345", testsignal.SelcallDefaultTone)
	feedChunks(d, selAudio(t, tones, err), 1000)
	if len(s.msgs) != 1 {
		t.Fatalf("got %d messages: %+v", len(s.msgs), s.msgs)
	}
	m := s.msgs[0]
	if m.Address != "12345" || m.Text != "12345" || m.Kind != message.KindSelcall || m.Function != -1 {
		t.Errorf("message = %+v", m)
	}
	p, ok := m.Payload.(*message.SelcallPayload)
	if !ok {
		t.Fatalf("payload = %T", m.Payload)
	}
	if p.Variant != "ZVEI1" || p.Sequence != "12345" || len(p.Tones) != 5 || p.Tones[0] != 1 || p.Tones[4] != 5 {
		t.Errorf("payload = %+v", p)
	}
	if len(p.Frequencies) != 5 || len(p.Frequencies[0]) != 1 || p.Frequencies[0][0] != 1060 || p.Frequencies[4][0] != 1530 {
		t.Errorf("frequencies = %v", p.Frequencies)
	}
}

func TestCCIRRoundTrip(t *testing.T) {
	d, s := newSel(t, "ccir", nil)
	tones, err := testsignal.SelcallSequence(testsignal.CCIR, "9A07BCDEF", 100*time.Millisecond)
	feedChunks(d, selAudio(t, tones, err), 777)
	if len(s.msgs) != 1 || s.msgs[0].Text != "9A07BCDEF" {
		t.Fatalf("messages = %+v", s.msgs)
	}
}

// TestSelcallAllToneTables sends every tone of every variant once; tones
// whose frequency appears twice in a table (PZVEI 0 and 14) are ambiguous
// for the decoder and are left out.
func TestSelcallAllToneTables(t *testing.T) {
	tables := map[string]testsignal.SelcallTable{
		"zvei1": testsignal.ZVEI1, "zvei2": testsignal.ZVEI2, "zvei3": testsignal.ZVEI3,
		"dzvei": testsignal.DZVEI, "pzvei": testsignal.PZVEI, "ccir": testsignal.CCIR,
		"eea": testsignal.EEA, "eia": testsignal.EIA,
	}
	for proto, table := range tables {
		count := map[float64]int{}
		for _, f := range table {
			count[f]++
		}
		digits := ""
		for i, f := range table {
			if count[f] == 1 {
				digits += strings.ToUpper(string("0123456789abcdef"[i]))
			}
		}
		d, s := newSel(t, proto, nil)
		tones, err := testsignal.SelcallSequence(table, digits, 100*time.Millisecond)
		feedChunks(d, selAudio(t, tones, err), 4096)
		if len(s.msgs) != 1 || s.msgs[0].Text != digits {
			t.Errorf("%s: want %q, got %d msgs %q", proto, digits, len(s.msgs), func() (o []string) {
				for _, m := range s.msgs {
					o = append(o, m.Text)
				}
				return
			}())
			continue
		}
		p := s.msgs[0].Payload.(*message.SelcallPayload)
		if p.Variant != strings.ToUpper(proto) {
			t.Errorf("%s: variant %q", proto, p.Variant)
		}
		for i, tone := range p.Tones {
			if p.Frequencies[i][0] != table[tone] {
				t.Errorf("%s: tone %d frequency %v, want %v", proto, tone, p.Frequencies[i], table[tone])
			}
		}
	}
}

func TestSelcallTwoSequences(t *testing.T) {
	d, s := newSel(t, "zvei1", nil)
	a, _ := testsignal.SelcallSequence(testsignal.ZVEI1, "12345", testsignal.SelcallDefaultTone)
	b, _ := testsignal.SelcallSequence(testsignal.ZVEI1, "67890", testsignal.SelcallDefaultTone)
	audio := selAudio(t, a, nil)
	audio = append(audio, selAudio(t, b, nil)...)
	feedChunks(d, audio, 1000)
	if len(s.msgs) != 2 || s.msgs[0].Text != "12345" || s.msgs[1].Text != "67890" {
		t.Fatalf("messages = %+v", s.msgs)
	}
}

// TestSelcallLongSequence checks that a run longer than one record loses no
// tone.
func TestSelcallLongSequence(t *testing.T) {
	d, s := newSel(t, "zvei1", nil)
	digits := strings.Repeat("1234567890", 7) // 70 tones, no tone repeats directly
	tones, err := testsignal.SelcallSequence(testsignal.ZVEI1, digits, testsignal.SelcallDefaultTone)
	feedChunks(d, selAudio(t, tones, err), 1000)
	var got strings.Builder
	for _, m := range s.msgs {
		got.WriteString(m.Text)
	}
	if got.String() != digits || len(s.msgs) != 2 {
		t.Errorf("got %d messages, concatenated %q", len(s.msgs), got.String())
	}
}

func dtmf(t *testing.T, keys string) []testsignal.Tone {
	t.Helper()
	tones, err := testsignal.DTMFSequence(keys, 80*time.Millisecond, 80*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	return tones
}

func TestDTMFRoundTrip(t *testing.T) {
	d, s := newSel(t, "dtmf", &config.SelcallOptions{SequenceTimeout: 500 * time.Millisecond})
	audio := selAudio(t, dtmf(t, "0123456789*#ABCD"), nil)
	// A longer pause than the timeout separates two sequences.
	audio = append(audio, testsignal.Silence(time.Second, selRate)...)
	audio = append(audio, selAudio(t, dtmf(t, "42"), nil)...)
	feedChunks(d, audio, 1000)
	if len(s.msgs) != 2 {
		t.Fatalf("got %d messages: %+v", len(s.msgs), s.msgs)
	}
	if s.msgs[0].Text != "0123456789*#ABCD" || s.msgs[0].Address != s.msgs[0].Text || s.msgs[0].Kind != message.KindSelcall || s.msgs[1].Text != "42" {
		t.Errorf("messages = %+v %+v", s.msgs[0], s.msgs[1])
	}
	p := s.msgs[1].Payload.(*message.SelcallPayload)
	// Key 4 = row 770 Hz, column 1209 Hz (index 4); key 2 = row 697, column 1336 (index 1).
	if p.Variant != "DTMF" || p.Tones[0] != 4 || p.Tones[1] != 1 || p.Frequencies[0][0] != 770 || p.Frequencies[0][1] != 1209 {
		t.Errorf("payload = %+v", p)
	}
}

// TestDTMFOneBlock decodes everything in one Process call: the pauses are
// taken from the audio position, not from the call pattern.
func TestDTMFOneBlock(t *testing.T) {
	d, s := newSel(t, "dtmf", &config.SelcallOptions{SequenceTimeout: 500 * time.Millisecond})
	audio := selAudio(t, dtmf(t, "123"), nil)
	audio = append(audio, testsignal.Silence(time.Second, selRate)...)
	audio = append(audio, selAudio(t, dtmf(t, "456"), nil)...)
	d.Process(audio)
	if len(s.msgs) != 2 || s.msgs[0].Text != "123" || s.msgs[1].Text != "456" {
		t.Fatalf("messages = %+v", s.msgs)
	}
}

func TestDTMFFlushOnClose(t *testing.T) {
	s := &sink{}
	d, err := decoder.New(config.DecoderConfig{Protocol: "dtmf", Selcall: &config.SelcallOptions{SequenceTimeout: 10 * time.Second}}, s)
	if err != nil {
		t.Fatal(err)
	}
	a := testsignal.ToneAudio(dtmf(t, "789"), selRate, 0.5)
	feedChunks(d, a, 1000)
	if len(s.msgs) != 0 {
		t.Fatalf("sequence completed before the timeout: %+v", s.msgs)
	}
	d.Close()
	if len(s.msgs) != 1 || s.msgs[0].Text != "789" {
		t.Errorf("close must deliver the pending sequence: %+v", s.msgs)
	}
}

func TestSelcallBadTimeout(t *testing.T) {
	_, err := decoder.New(config.DecoderConfig{Protocol: "dtmf", Selcall: &config.SelcallOptions{SequenceTimeout: -time.Second}}, &sink{})
	if err == nil {
		t.Error("negative sequence_timeout must be rejected")
	}
}

func TestDualtoneRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		second float64
		want   string
		tone   int
	}{{testsignal.DualtoneFS5, "fs4,fs5", 5}, {testsignal.DualtoneFS7, "fs4,fs7", 7}} {
		d, s := newSel(t, "dualtone", nil)
		a := testsignal.Silence(200*time.Millisecond, selRate)
		a = append(a, testsignal.ToneAudio([]testsignal.Tone{{Freqs: []float64{testsignal.DualtoneFS4, tc.second}, Duration: 3500 * time.Millisecond}}, selRate, 0.5)...)
		feedChunks(d, a, 2048)
		if len(s.msgs) != 1 || s.msgs[0].Text != tc.want {
			t.Fatalf("%s: messages = %+v", tc.want, s.msgs)
		}
		p := s.msgs[0].Payload.(*message.SelcallPayload)
		if p.Variant != "DUALTONE" || len(p.Tones) != 2 || p.Tones[0] != 4 || p.Tones[1] != tc.tone || p.Frequencies[0][0] != 675 || p.Frequencies[1][0] != tc.second {
			t.Errorf("payload = %+v", p)
		}
	}
}

// TestMatchesMultimon feeds the same synthetic audio to the multimon-ng
// binary and compares the sequences it prints with the gateway's.
func TestMatchesMultimon(t *testing.T) {
	bin, err := exec.LookPath("multimon-ng")
	if err != nil {
		t.Skip("multimon-ng not installed")
	}
	zvei, _ := testsignal.SelcallSequence(testsignal.ZVEI1, "12345", testsignal.SelcallDefaultTone)
	zvei2, _ := testsignal.SelcallSequence(testsignal.ZVEI1, "9E0A7", testsignal.SelcallDefaultTone)
	ccir, _ := testsignal.SelcallSequence(testsignal.CCIR, "246813", 100*time.Millisecond)
	cases := []struct {
		proto, demod string
		audio        []float32
		prefix       string // multimon-ng line prefix before the sequence
		perLine      bool   // one line per key (DTMF)
	}{
		{"zvei1", "ZVEI1", append(selAudio(t, zvei, nil), selAudio(t, zvei2, nil)...), "ZVEI1: ", false},
		{"ccir", "CCIR", selAudio(t, ccir, nil), "CCIR: ", false},
		{"dtmf", "DTMF", selAudio(t, dtmf(t, "0*#9A"), nil), "DTMF: ", true},
	}
	for _, c := range cases {
		raw := new(bytes.Buffer)
		for _, v := range c.audio {
			_ = binary.Write(raw, binary.LittleEndian, int16(v*32767))
		}
		file := filepath.Join(t.TempDir(), c.proto+".s16le")
		if err := os.WriteFile(file, raw.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(bin, "-q", "-t", "raw", "-a", c.demod, file).Output()
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		var dtmfSeq strings.Builder
		for _, ln := range strings.Split(string(out), "\n") {
			if rest, ok := strings.CutPrefix(ln, c.prefix); ok {
				if c.perLine {
					dtmfSeq.WriteString(rest)
				} else {
					want = append(want, rest)
				}
			}
		}
		if c.perLine {
			want = []string{dtmfSeq.String()}
		}
		d, s := newSel(t, c.proto, nil)
		feedChunks(d, c.audio, 1024)
		d.Close() // delivers a DTMF sequence that is still within its timeout
		var got []string
		for _, m := range s.msgs {
			got = append(got, m.Text)
		}
		if strings.Join(got, "|") != strings.Join(want, "|") || len(want) == 0 {
			t.Errorf("%s: gateway %q, multimon-ng %q", c.proto, got, want)
		}
	}
}
