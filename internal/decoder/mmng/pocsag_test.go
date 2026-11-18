//go:build cgo

package mmng

import (
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	"github.com/gopager/pagewire/internal/message"
	"github.com/gopager/pagewire/internal/testsignal"
)

type sink struct {
	msgs   []*message.Message
	events []decoder.Event
}

func (s *sink) Message(m *message.Message) { s.msgs = append(s.msgs, m) }
func (s *sink) Event(e decoder.Event)      { s.events = append(s.events, e) }

func newDec(t *testing.T, proto string, opts *config.POCSAGOptions) (decoder.Decoder, *sink) {
	t.Helper()
	s := &sink{}
	d, err := decoder.New(config.DecoderConfig{Protocol: proto, POCSAG: opts}, s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, s
}

func feed(t *testing.T, d decoder.Decoder, bits []byte, baud int, invert bool, noise float64) {
	t.Helper()
	audio := testsignal.FSKAudio(bits, testsignal.POCSAGFSK(baud), 22050, 0.3, 0.3, noise, rand.New(rand.NewSource(3)).Float64)
	if invert {
		for i := range audio {
			audio[i] = -audio[i]
		}
	}
	// Feed in odd-sized chunks to exercise the carry buffer.
	for i := 0; i < len(audio); i += 1000 {
		end := i + 1000
		if end > len(audio) {
			end = len(audio)
		}
		d.Process(audio[i:end])
	}
}

func encode(t *testing.T, msgs ...testsignal.POCSAGMessage) []byte {
	t.Helper()
	bits, err := testsignal.EncodePOCSAG(msgs)
	if err != nil {
		t.Fatal(err)
	}
	return bits
}

func TestAlphaRoundTrip(t *testing.T) {
	for _, proto := range []string{"pocsag512", "pocsag1200", "pocsag2400"} {
		d, s := newDec(t, proto, &config.POCSAGOptions{Mode: "auto", Charset: "US", Polarity: "auto"})
		feed(t, d, encode(t, testsignal.POCSAGMessage{Address: 1234567, Function: 3, Text: "HELLO WORLD 123"}), map[string]int{"pocsag512": 512, "pocsag1200": 1200, "pocsag2400": 2400}[proto], false, 0.02)
		if len(s.msgs) != 1 {
			t.Fatalf("%s: got %d messages", proto, len(s.msgs))
		}
		m := s.msgs[0]
		// The padding bits of the last codeword render as NUL characters
		// that stay in the text.
		if m.Address != "1234567" || m.Function != 3 || m.Kind != message.KindAlpha || !strings.HasPrefix(m.Text, "HELLO WORLD 123\x00") || strings.TrimRight(m.Text, "\x00") != "HELLO WORLD 123" {
			t.Logf("text = %q", m.Text)
			t.Errorf("%s: message = %+v", proto, m)
		}
		if p := m.Payload.(*message.POCSAGPayload); p.RawBits == 0 || p.Partial {
			t.Errorf("%s: payload = %+v", proto, p)
		}
	}
}

func TestNumericStandardModeAndTone(t *testing.T) {
	d, s := newDec(t, "pocsag1200", &config.POCSAGOptions{Mode: "standard", Charset: "US", Polarity: "auto"})
	feed(t, d, encode(t,
		testsignal.POCSAGMessage{Address: 54321, Function: 0, Numeric: true, Text: "112-4711"},
		testsignal.POCSAGMessage{Address: 777, Function: 2},
	), 1200, false, 0)
	if len(s.msgs) != 2 {
		t.Fatalf("got %d messages: %+v", len(s.msgs), s.msgs)
	}
	if m := s.msgs[0]; m.Kind != message.KindNumeric || m.Text != "112-4711  " {
		t.Errorf("numeric = %+v", m)
	}
	if m := s.msgs[1]; m.Kind != message.KindTone || m.Address != "777" || m.Function != 2 || m.Text != "" {
		t.Errorf("tone = %+v", m)
	}
}

func TestPruneEmpty(t *testing.T) {
	d, s := newDec(t, "pocsag1200", &config.POCSAGOptions{Mode: "auto", Charset: "US", Polarity: "auto", PruneEmpty: true})
	feed(t, d, encode(t, testsignal.POCSAGMessage{Address: 777, Function: 2}), 1200, false, 0)
	if len(s.msgs) != 0 {
		t.Errorf("tone-only page should be pruned, got %+v", s.msgs)
	}
}

func TestPolarity(t *testing.T) {
	bits := encode(t, testsignal.POCSAGMessage{Address: 1000, Function: 1, Text: "INV"})
	// Noise on the tail matters: digital silence decodes as all-zero
	// codewords, which are valid POCSAG addresses, exactly as in multimon-ng.
	d, s := newDec(t, "pocsag1200", &config.POCSAGOptions{Mode: "alpha", Charset: "US", Polarity: "auto"})
	feed(t, d, bits, 1200, true, 0.05)
	if len(s.msgs) != 1 || strings.TrimRight(s.msgs[0].Text, "\x00") != "INV" {
		t.Errorf("auto polarity should decode an inverted signal: %+v", s.msgs)
	}
	d2, s2 := newDec(t, "pocsag1200", &config.POCSAGOptions{Mode: "alpha", Charset: "US", Polarity: "normal"})
	feed(t, d2, bits, 1200, true, 0.05)
	if len(s2.msgs) != 0 {
		t.Errorf("normal-only polarity must not decode an inverted signal: %+v", s2.msgs)
	}
}

func TestBCHCorrectionAndEvents(t *testing.T) {
	bits := encode(t, testsignal.POCSAGMessage{Address: 2000, Function: 1, Text: "BCH TEST"})
	// Flip one bit inside the address codeword (first codeword of frame 0
	// after the sync word) and two bits in the first message codeword.
	start := 576 + 32
	bits[start+5] ^= 1
	bits[start+32+7] ^= 1
	bits[start+32+20] ^= 1
	d, s := newDec(t, "pocsag1200", &config.POCSAGOptions{Mode: "alpha", Charset: "US", Polarity: "auto"})
	feed(t, d, bits, 1200, false, 0)
	if len(s.msgs) != 1 || strings.TrimRight(s.msgs[0].Text, "\x00") != "BCH TEST" || s.msgs[0].Address != "2000" {
		t.Fatalf("expected corrected decode, got %+v", s.msgs)
	}
	corrected := 0
	for _, e := range s.events {
		if e.Kind == decoder.EventBitsCorrected {
			corrected += e.Count
		}
	}
	if corrected < 3 {
		t.Errorf("corrected bits = %d, want >= 3", corrected)
	}
	lvl := 0
	d0, s0 := newDec(t, "pocsag1200", &config.POCSAGOptions{Mode: "alpha", Charset: "US", Polarity: "auto", BCHLevel: &lvl})
	feed(t, d0, bits, 1200, false, 0)
	for _, m := range s0.msgs {
		if strings.TrimRight(m.Text, "\x00") == "BCH TEST" && m.Address == "2000" {
			t.Errorf("with bch_level 0 the corrupted page must not decode cleanly: %+v", m)
		}
	}
}

func TestCharsetDE(t *testing.T) {
	d, s := newDec(t, "pocsag1200", &config.POCSAGOptions{Mode: "alpha", Charset: "DE", Polarity: "auto"})
	feed(t, d, encode(t, testsignal.POCSAGMessage{Address: 3000, Function: 3, Text: "[]{"}), 1200, false, 0)
	if len(s.msgs) != 1 || strings.TrimRight(s.msgs[0].Text, "\x00") != "ÄÜä" {
		t.Errorf("DE charset text = %+v", s.msgs)
	}
}

// TestRealRecordingMatchesMultimon decodes a real 22050 Hz mono s16le POCSAG
// recording and compares addresses and numeric renderings with the
// multimon-ng binary. The recording location comes from PAGEWIRE_POCSAG_FIXTURE
// because real captures cannot be shipped with the repository.
func TestRealRecordingMatchesMultimon(t *testing.T) {
	fixture := os.Getenv("PAGEWIRE_POCSAG_FIXTURE")
	if fixture == "" {
		t.Skip("PAGEWIRE_POCSAG_FIXTURE not set")
	}
	raw, err := os.ReadFile(fixture)
	if err != nil {
		t.Skip("fixture not available:", err)
	}
	bin, err := exec.LookPath("multimon-ng")
	if err != nil {
		t.Skip("multimon-ng not installed")
	}
	out, err := exec.Command(bin, "-q", "-t", "raw", "-a", "POCSAG1200", "-f", "numeric", fixture).Output()
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, ln := range strings.Split(string(out), "\n") {
		if i := strings.Index(ln, "Address:"); i >= 0 {
			want = append(want, ln[i:])
		}
	}
	audio := make([]float32, len(raw)/2)
	for i := range audio {
		audio[i] = float32(int16(uint16(raw[2*i])|uint16(raw[2*i+1])<<8)) / 32768
	}
	d, s := newDec(t, "pocsag1200", &config.POCSAGOptions{Mode: "numeric", Charset: "DE", Polarity: "auto"})
	d.Process(audio)
	var got []string
	for _, m := range s.msgs {
		got = append(got, "Address: "+padLeft(m.Address, 7)+"  Function: "+padLeft(itoa(m.Function), 1)+"  Numeric: "+m.Text)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("mismatch with multimon-ng\n--- ours\n%s\n--- multimon-ng\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func padLeft(s string, n int) string {
	for len(s) < n {
		s = " " + s
	}
	return s
}

func itoa(i int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+i))) }

func TestChooseAuto(t *testing.T) {
	cases := []struct{ numeric, alpha, want string }{
		{"8 22399[40-5[92562114139- 0000", "HELLO WORLD 123", "alpha"},
		{"112-4711  ", "\x11\x12\x17Q", "numeric"},
		{"U07 ]]-5170.756]4]9.[6U636- 5..970.34U", "\x0b\x0e;o\x15\x0eh+fI'", "numeric"},
		{"123", "ABC", "numeric"},
		{"12.5", "ABC", "alpha"},
	}
	for _, c := range cases {
		if got := chooseAuto(c.numeric, c.alpha, c.alpha); got != c.want {
			t.Errorf("chooseAuto(%q, %q) = %s, want %s", c.numeric, c.alpha, got, c.want)
		}
	}
}

func TestControlCharactersAreRaw(t *testing.T) {
	// EOT, ETB and a newline inside the text plus trailing spaces arrive
	// unchanged; NUL (inside and as padding) arrives as U+2400.
	const sent = "A\x00B\x04C\x17D\nE  "
	const want = sent
	d, s := newDec(t, "pocsag1200", &config.POCSAGOptions{Mode: "alpha", Charset: "DE", Polarity: "auto"})
	feed(t, d, encode(t, testsignal.POCSAGMessage{Address: 4000, Function: 3, Text: sent}), 1200, false, 0)
	if len(s.msgs) != 1 || !strings.HasPrefix(s.msgs[0].Text, want) || strings.Contains(s.msgs[0].Text, "<") {
		t.Fatalf("control characters must be raw and complete: %q", s.msgs[0].Text)
	}
	if rest := strings.TrimPrefix(s.msgs[0].Text, want); strings.Trim(rest, "\x00") != "" {
		t.Errorf("unexpected trailer %q", rest)
	}
	if p := s.msgs[0].Payload.(*message.POCSAGPayload); p.Alpha != s.msgs[0].Text || p.RawBits == 0 {
		t.Errorf("payload renderings must match: alpha=%q rawbits=%d", p.Alpha, p.RawBits)
	}
}

// deriveAlpha and deriveNumeric implement the documented reading of
// POCSAGPayload.RawHex independently of the decoder, so the documentation
// is proven against the decoder output.
func deriveAlpha(rawHex string) string {
	var bits []byte
	for _, h := range rawHex {
		v, _ := strconv.ParseUint(string(h), 16, 8)
		for i := 3; i >= 0; i-- {
			bits = append(bits, byte(v>>uint(i)&1))
		}
	}
	var out []byte
	for k := 0; k+7 <= len(bits); k += 7 {
		var c byte
		for i := 0; i < 7; i++ {
			c |= bits[k+i] << uint(i)
		}
		out = append(out, c)
	}
	return string(out)
}

func deriveNumeric(rawHex string) string {
	const symbols = "0123456789*U -)("
	var out []byte
	for _, h := range rawHex {
		v, _ := strconv.ParseUint(string(h), 16, 8)
		rev := (v&1)<<3 | (v&2)<<1 | (v&4)>>1 | (v&8)>>3
		out = append(out, symbols[rev])
	}
	return string(out)
}

func TestRawHexDocumentationHolds(t *testing.T) {
	d, s := newDec(t, "pocsag1200", &config.POCSAGOptions{Mode: "alpha", Charset: "US", Polarity: "auto"})
	feed(t, d, encode(t, testsignal.POCSAGMessage{Address: 4100, Function: 3, Text: "Raw\x04bytes\n ok"}), 1200, false, 0)
	if len(s.msgs) != 1 {
		t.Fatal("no message")
	}
	p := s.msgs[0].Payload.(*message.POCSAGPayload)
	want := s.msgs[0].Text
	if got := deriveAlpha(p.RawHex()); got != want {
		t.Errorf("alpha derived from raw_hex = %q, decoder text = %q", got, want)
	}
	d2, s2 := newDec(t, "pocsag1200", &config.POCSAGOptions{Mode: "numeric", Charset: "US", Polarity: "auto"})
	feed(t, d2, encode(t, testsignal.POCSAGMessage{Address: 4101, Function: 0, Numeric: true, Text: "112-4711 U"}), 1200, false, 0)
	p2 := s2.msgs[0].Payload.(*message.POCSAGPayload)
	// multimon-ng renders the symbols for A and F as "[" and "]" while the
	// standard names them "(" and ")"; everything else must agree.
	norm := func(x string) string { return strings.NewReplacer("[", "(", "]", ")").Replace(x) }
	if got := deriveNumeric(p2.RawHex()); norm(got) != norm(s2.msgs[0].Text) {
		t.Errorf("numeric derived from raw_hex = %q, decoder text = %q", got, s2.msgs[0].Text)
	}
}
