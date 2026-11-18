//go:build cgo

package mmng

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	"github.com/gopager/pagewire/internal/message"
)

// sampleFile returns the path of a multimon-ng sample recording below the
// directory named by PAGEWIRE_SAMPLES and skips the test when that variable
// is unset, because the recordings are not part of this repository.
func sampleFile(t *testing.T, name string) string {
	t.Helper()
	dir := os.Getenv("PAGEWIRE_SAMPLES")
	if dir == "" {
		t.Skip("PAGEWIRE_SAMPLES not set")
	}
	return filepath.Join(dir, name)
}

func loadS16(t *testing.T, path string) []float32 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip("fixture not available:", err)
	}
	audio := make([]float32, len(raw)/2)
	for i := range audio {
		audio[i] = float32(int16(uint16(raw[2*i])|uint16(raw[2*i+1])<<8)) / 32768
	}
	return audio
}

// TestFLEXP2000Sample decodes the P2000 test alarm sample shipped with
// multimon-ng. Expected pages come from multimon-ng's own output; the page
// split over two frames must come back reassembled.
func TestFLEXP2000Sample(t *testing.T) {
	audio := loadS16(t, sampleFile(t, "FLEX_1600_2fsk_P2000_proef_alarm.s16le"))
	s := &sink{}
	d, err := decoder.New(config.DecoderConfig{Protocol: "flex", FLEX: &config.FLEXOptions{}}, s)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for i := 0; i < len(audio); i += 4096 {
		end := i + 4096
		if end > len(audio) {
			end = len(audio)
		}
		d.Process(audio[i:end])
	}
	want := map[string]string{
		"1120103": "test",
		"1523020": "Passage Ambulance Leiderdorpsebrug Leiderdorp",
		"120901":  "A1 (DIA) 13991 Schmidtstraat 2064 Spaarndam 82106 regio 12",
		"1123102": "Fijne dienst",
		"1180000": "TESTOPROEP MOB",
		"1400521": "Test: Proefalarm Ochtend Brandweer Veiligheidsregio Rotterdam Rijnmond.",
		"1400141": "Test: Proefalarm Ochtend Brandweer Veiligheidsregio Rotterdam Rijnmond.",
	}
	// FLEX fills the last word with ETX (0x03); like POCSAG's NUL padding it
	// is delivered untouched, so the comparison strips it and checks that
	// at least one page carries it.
	got := map[string]string{}
	sawFill := false
	for _, m := range s.msgs {
		if strings.HasSuffix(m.Text, "\x03") {
			sawFill = true
		}
		got[m.Address] = strings.TrimRight(m.Text, "\x03")
		if m.Kind != message.KindAlpha || m.Protocol != "" {
			t.Errorf("unexpected message shape: %+v", m)
		}
		p := m.Payload.(*message.FLEXPayload)
		if p.Baud != 1600 || p.Levels != 2 {
			t.Errorf("payload = %+v", p)
		}
		if m.Address == "1400141" && !p.Reassembled {
			t.Errorf("page 1400141 should be reassembled, got fragment %q", p.Fragment)
		}
	}
	for addr, text := range want {
		if got[addr] != text {
			t.Errorf("capcode %s: got %q, want %q", addr, got[addr], text)
		}
	}
	if len(s.msgs) < 10 {
		t.Errorf("only %d messages decoded", len(s.msgs))
	}
	if !sawFill {
		t.Errorf("ETX fill characters must be passed through untouched")
	}
}

func TestFLEXRecordMapping(t *testing.T) {
	s := &sink{}
	d := &FLEX{ev: s}
	d.handle(flexPage{capcode: 1400141, msgType: flexTypeAlphanumeric, typeTag: "ALN", fragment: "fragment_start", message: "half"})
	if len(s.msgs) != 0 {
		t.Errorf("fragments must be suppressed by default")
	}
	d.handle(flexPage{capcode: 1400141, msgType: flexTypeToneOnly, typeTag: "TON", fragment: "complete"})
	if len(s.msgs) != 1 || s.msgs[0].Kind != message.KindTone {
		t.Errorf("tone mapping: %+v", s.msgs)
	}
	d.handle(flexPage{capcode: 2000001, msgType: flexTypeAlphanumeric, typeTag: "ALN", fragment: "complete", message: "group", group: true, groupCapcodes: []int64{11, 22}})
	if len(s.msgs) != 3 || s.msgs[1].Address != "11" || s.msgs[2].Address != "22" {
		t.Errorf("group expansion: %+v", s.msgs)
	}
	d.handle(flexPage{capcode: 5, typeTag: "SMSG", msgType: flexTypeShortMessage})
	if len(s.msgs) != 3 {
		t.Errorf("system records must be ignored by default")
	}
	d.handle(flexPage{capcode: 7, msgType: flexTypeNumberedNumeric, typeTag: "NNUM", fragment: "complete", message: "123"})
	if m := s.msgs[len(s.msgs)-1]; m.Kind != message.KindNumeric || m.Payload.(*message.FLEXPayload).MessageType != "numbered_numeric" {
		t.Errorf("numeric mapping: %+v", m)
	}
}

func TestFLEXRawBytes(t *testing.T) {
	s := &sink{}
	d := &FLEX{ev: s}
	d.handle(flexPage{capcode: 7, msgType: flexTypeAlphanumeric, typeTag: "ALN", fragment: "complete", message: "A\x80B\n\x04\x03"})
	if len(s.msgs) != 1 {
		t.Fatal("no message")
	}
	m := s.msgs[0]
	if m.Text != "A\x00B\n\x04\x03" {
		t.Errorf("text = %q", m.Text)
	}
	if p := m.Payload.(*message.FLEXPayload); p.RawHex() != "4100420a0403" {
		t.Errorf("raw_hex = %q", p.RawHex())
	}
}
