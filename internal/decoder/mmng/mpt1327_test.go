//go:build cgo

package mmng

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	"github.com/gopager/pagewire/internal/message"
	"github.com/gopager/pagewire/internal/testsignal"
)

func mptSignal() []float32 {
	infos := []uint64{
		testsignal.MPT1327GoToChannel(5, 1234, 1, 100, 4321, 2),
		testsignal.MPT1327Aloha(9, 777, 0, 3, 2, 17, 5),
		testsignal.MPT1327Ack(12, 8000, 1, 4096, 1, 6),
	}
	bits := testsignal.EncodeMPT1327(infos, 0x1234)
	return testsignal.MPT1327Audio(bits, 22050, 0.05, 0.05)
}

func newMPT(t *testing.T, opts *config.MPT1327Options) (decoder.Decoder, *sink) {
	t.Helper()
	s := &sink{}
	d, err := decoder.New(config.DecoderConfig{Protocol: "mpt1327", MPT1327: opts}, s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, s
}

func field(t *testing.T, p *message.MPT1327Payload, name string, want uint64) {
	t.Helper()
	if v, ok := p.Field(name); !ok || v != want {
		t.Errorf("%s: field %s = %d (present %v), want %d", p.Message, name, v, ok, want)
	}
}

// TestMPT1327RoundTrip decodes a synthetic control channel transmission
// built from the standard and checks the delivered records.
func TestMPT1327RoundTrip(t *testing.T) {
	d, s := newMPT(t, nil)
	decodeAll(t, d, mptSignal())
	if len(s.msgs) != 3 {
		t.Fatalf("got %d records, want 3: %+v", len(s.msgs), s.msgs)
	}
	for i, name := range []string{"GTC", "ALH", "ACKI"} {
		m := s.msgs[i]
		p := m.Payload.(*message.MPT1327Payload)
		if p.Message != name || m.Text != name || m.Kind != message.KindStatus || m.Function != -1 {
			t.Errorf("record %d: %+v / %+v", i, m, p)
		}
		if len(p.Raw) != 8 || p.Codeword != testsignalCodeword(i) {
			t.Errorf("record %d codeword %#x raw %x", i, p.Codeword, p.Raw)
		}
	}
	gtc := s.msgs[0].Payload.(*message.MPT1327Payload)
	if s.msgs[0].Address != "1234" {
		t.Errorf("GTC address = %q", s.msgs[0].Address)
	}
	for k, v := range map[string]uint64{"pfix": 5, "ident1": 1234, "d": 1, "chan": 100, "ident2": 4321, "n": 2} {
		field(t, gtc, k, v)
	}
	alh := s.msgs[1].Payload.(*message.MPT1327Payload)
	for k, v := range map[string]uint64{"pfix": 9, "ident1": 777, "chan4": 3, "wt": 2, "m": 17, "n": 5} {
		field(t, alh, k, v)
	}
	ack := s.msgs[2].Payload.(*message.MPT1327Payload)
	for k, v := range map[string]uint64{"pfix": 12, "ident1": 8000, "ident2": 4096, "qual": 1, "n": 6} {
		field(t, ack, k, v)
	}
}

func testsignalCodeword(i int) uint64 {
	return testsignal.MPT1327Codeword([]uint64{
		testsignal.MPT1327GoToChannel(5, 1234, 1, 100, 4321, 2),
		testsignal.MPT1327Aloha(9, 777, 0, 3, 2, 17, 5),
		testsignal.MPT1327Ack(12, 8000, 1, 4096, 1, 6),
	}[i])
}

// TestMPT1327SystemOptional checks that the system codeword is only
// delivered when requested.
func TestMPT1327SystemOptional(t *testing.T) {
	d, s := newMPT(t, &config.MPT1327Options{IncludeSystem: true})
	decodeAll(t, d, mptSignal())
	var sys []*message.MPT1327Payload
	for _, m := range s.msgs {
		if p := m.Payload.(*message.MPT1327Payload); p.Message == "CCSC" {
			sys = append(sys, p)
		}
	}
	if len(sys) == 0 {
		t.Fatalf("no CCSC delivered: %+v", s.msgs)
	}
	field(t, sys[0], "sys", 0x1234)
	if sys[0].Codeword != testsignal.MPT1327Codeword(testsignal.MPT1327CCSC(0x1234)) {
		t.Errorf("CCSC codeword %#x", sys[0].Codeword)
	}
}

// TestMPT1327MatchesMultimon compares the records with the multimon-ng
// binary decoding the same synthetic signal.
func TestMPT1327MatchesMultimon(t *testing.T) {
	bin := referenceBinary(t, "MPT1327")
	audio := mptSignal()
	raw := make([]byte, 2*len(audio))
	for i, v := range audio {
		u := uint16(int16(v * 32767))
		raw[2*i], raw[2*i+1] = byte(u), byte(u>>8)
	}
	path := filepath.Join(t.TempDir(), "mpt.s16le")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "-q", "--json", "-t", "raw", "-a", "MPT1327", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	var want []map[string]any
	for _, ln := range bytes.Split(out, []byte("\n")) {
		if len(bytes.TrimSpace(ln)) == 0 {
			continue
		}
		var r map[string]any
		if err := json.Unmarshal(ln, &r); err != nil {
			t.Fatal(err)
		}
		if r["message"] != "CCSC" {
			want = append(want, r)
		}
	}
	d, s := newMPT(t, nil)
	decodeAll(t, d, audio)
	if len(s.msgs) != len(want) || len(want) == 0 {
		t.Fatalf("got %d records, reference %d", len(s.msgs), len(want))
	}
	for i, w := range want {
		p := s.msgs[i].Payload.(*message.MPT1327Payload)
		if p.Message != w["message"] {
			t.Errorf("record %d: %s, reference %v", i, p.Message, w["message"])
		}
		for _, f := range p.Fields {
			if rv, ok := w[f.Name].(float64); ok && uint64(rv) != f.Value {
				t.Errorf("record %d field %s = %d, reference %v", i, f.Name, f.Value, rv)
			}
		}
	}
}
