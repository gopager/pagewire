//go:build cgo

package mmng

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	"github.com/gopager/pagewire/internal/message"
)

// referenceBinary returns a multimon-ng executable that knows the given
// demodulator: $MULTIMON_NG_REF, else multimon-ng from PATH. Releases
// before the GSC and MPT1327 decoders do not list them, so the test skips.
func referenceBinary(t *testing.T, demod string) string {
	t.Helper()
	candidates := []string{os.Getenv("MULTIMON_NG_REF")}
	if p, err := exec.LookPath("multimon-ng"); err == nil {
		candidates = append(candidates, p)
	}
	for _, bin := range candidates {
		if bin == "" {
			continue
		}
		out, _ := exec.Command(bin, "-h").CombinedOutput()
		if bytes.Contains(out, []byte(" "+demod+" ")) {
			return bin
		}
	}
	t.Skipf("no multimon-ng with %s support (set MULTIMON_NG_REF)", demod)
	return ""
}

// refRecord is one line of multimon-ng's --json output for GSC.
type refRecord struct {
	Address string `json:"address"`
	Func    int    `json:"function"`
	Type    string `json:"msg_type"`
	Message string `json:"message"`
	Alt     string `json:"alt_type"`
	AltMsg  string `json:"alt_message"`
	Blocks  *int   `json:"blocks"`
	State   string `json:"voice_state"`
}

func decodeAll(t *testing.T, d decoder.Decoder, audio []float32) {
	t.Helper()
	for i := 0; i < len(audio); i += 4096 {
		end := min(i+4096, len(audio))
		d.Process(audio[i:end])
	}
}

// TestGSCCaptureMatchesMultimon decodes the GSC capture shipped with
// multimon-ng and compares every record with the output of the multimon-ng
// binary: address, function, type, text, other rendering and block count.
func TestGSCCaptureMatchesMultimon(t *testing.T) {
	fixture := sampleFile(t, "gsc_capture.s16le")
	audio := loadS16(t, fixture)
	bin := referenceBinary(t, "GSC")
	out, err := exec.Command(bin, "-q", "--json", "-t", "raw", "-a", "GSC", fixture).Output()
	if err != nil {
		t.Fatal(err)
	}
	var want []refRecord
	for _, ln := range bytes.Split(out, []byte("\n")) {
		if len(bytes.TrimSpace(ln)) == 0 {
			continue
		}
		var r refRecord
		if err := json.Unmarshal(ln, &r); err != nil {
			t.Fatalf("reference line %q: %v", ln, err)
		}
		want = append(want, r)
	}
	if len(want) == 0 {
		t.Fatal("reference decoded nothing")
	}

	s := &sink{}
	d, err := decoder.New(config.DecoderConfig{Protocol: "gsc", GSC: &config.GSCOptions{IncludeVoice: true}}, s)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	decodeAll(t, d, audio)

	if len(s.msgs) != len(want) {
		t.Fatalf("got %d records, reference has %d", len(s.msgs), len(want))
	}
	for i, w := range want {
		m := s.msgs[i]
		p, ok := m.Payload.(*message.GSCPayload)
		if !ok {
			t.Fatalf("record %d payload %T", i, m.Payload)
		}
		if m.Address != w.Address || m.Function != w.Func || p.MsgType != w.Type || m.Text != w.Message ||
			p.AltType != w.Alt || p.AltText != w.AltMsg || p.VoiceState != w.State {
			t.Errorf("record %d: got %q f%d %s %q alt %s %q voice %q; want %q f%d %s %q alt %s %q voice %q",
				i, m.Address, m.Function, p.MsgType, m.Text, p.AltType, p.AltText, p.VoiceState,
				w.Address, w.Func, w.Type, w.Message, w.Alt, w.AltMsg, w.State)
		}
		if w.Blocks != nil && p.Blocks != *w.Blocks {
			t.Errorf("record %d blocks = %d, want %d", i, p.Blocks, *w.Blocks)
		}
		wantKind := map[string]message.Kind{"alpha": message.KindAlpha, "numeric": message.KindNumeric}[w.Type]
		if wantKind == "" {
			wantKind = message.KindTone
		}
		if m.Kind != wantKind {
			t.Errorf("record %d kind = %s, want %s (address %s)", i, m.Kind, wantKind, m.Address)
		}
		if len(p.Raw) != 8*max(p.Blocks, 0) {
			t.Errorf("record %d: %d raw bytes for %d blocks", i, len(p.Raw), p.Blocks)
		}
	}
	t.Logf("%d records identical to multimon-ng", len(want))
}

// TestGSCVoiceOptional checks that voice events are only delivered when
// requested and that pages are unaffected.
func TestGSCVoiceOptional(t *testing.T) {
	fixture := sampleFile(t, "gsc_capture.s16le")
	audio := loadS16(t, fixture)
	count := func(opts *config.GSCOptions) (voice, total int) {
		s := &sink{}
		d, err := decoder.New(config.DecoderConfig{Protocol: "gsc", GSC: opts}, s)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		decodeAll(t, d, audio)
		for _, m := range s.msgs {
			if m.Payload.(*message.GSCPayload).MsgType == "voice" {
				voice++
			}
		}
		return voice, len(s.msgs)
	}
	vOff, nOff := count(nil)
	vOn, nOn := count(&config.GSCOptions{IncludeVoice: true})
	if vOff != 0 || vOn == 0 || nOn-nOff != vOn {
		t.Errorf("voice events off/on = %d/%d, total %d/%d", vOff, vOn, nOff, nOn)
	}
}
