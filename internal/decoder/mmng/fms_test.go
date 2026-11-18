//go:build cgo

package mmng

import (
	"encoding/binary"
	"fmt"
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

var fmsCases = []testsignal.FMSMessage{
	{Service: 6, State: 9, Location: 0x4B, Vehicle: 0x2143, Status: 3, Model: 1, Direction: 0, ShortInfo: 2},
	{Service: 1, State: 0xE, Location: 0x12, Vehicle: 0xFFFF, Status: 0xF, Model: 0, Direction: 1, ShortInfo: 3},
	{Service: 0xD, State: 0, Location: 0x00, Vehicle: 0x0000, Status: 0, Model: 0, Direction: 0, ShortInfo: 0},
	{Service: 9, State: 3, Location: 0xA5, Vehicle: 0x8001, Status: 7, Model: 1, Direction: 1, ShortInfo: 1},
}

func newFMSDec(t *testing.T) (decoder.Decoder, *sink) {
	t.Helper()
	s := &sink{}
	d, err := decoder.New(config.DecoderConfig{Protocol: "fms"}, s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, s
}

func feedFMS(d decoder.Decoder, bits []byte, noise float64) {
	audio := testsignal.FMSAudio(bits, 22050, 0.2, 0.2, noise, rand.New(rand.NewSource(5)).Float64)
	for i := 0; i < len(audio); i += 1000 {
		end := min(i+1000, len(audio))
		d.Process(audio[i:end])
	}
}

// fmsAddress computes the expected station identifier from the transmitted
// values: nibbles of the location swapped, vehicle nibbles in on-air order.
func fmsAddress(m testsignal.FMSMessage) string {
	loc := m.Location<<4 | m.Location>>4
	v := m.Vehicle
	return fmt.Sprintf("%x%x%02x%x%x%x%x", m.Service, m.State, loc, v&15, v>>4&15, v>>8&15, v>>12)
}

func TestFMSRoundTrip(t *testing.T) {
	d, s := newFMSDec(t)
	bits, err := testsignal.EncodeFMS(fmsCases)
	if err != nil {
		t.Fatal(err)
	}
	feedFMS(d, bits, 0)
	if len(s.msgs) != len(fmsCases) {
		t.Fatalf("got %d messages, want %d", len(s.msgs), len(fmsCases))
	}
	for i, m := range fmsCases {
		got := s.msgs[i]
		p, ok := got.Payload.(*message.FMSPayload)
		if !ok {
			t.Fatalf("payload type %T", got.Payload)
		}
		if got.Kind != message.KindStatus || got.Function != -1 || got.Address != fmsAddress(m) {
			t.Errorf("%d: kind %q function %d address %q, want %q", i, got.Kind, got.Function, got.Address, fmsAddress(m))
		}
		if !strings.HasPrefix(got.Text, "FMS: ") || !strings.Contains(got.Text, "CRC correct") {
			t.Errorf("%d: text %q", i, got.Text)
		}
		want := message.FMSPayload{
			ServiceID: int(m.Service), StateID: int(m.State), LocationRaw: int(m.Location),
			LocationID: int(m.Location<<4 | m.Location>>4), VehicleID: int(m.Vehicle),
			Vehicle: fmsAddress(m)[4:], Status: int(m.Status), Model: int(m.Model),
			Direction: int(m.Direction), ShortInfo: int(m.ShortInfo), CRCOK: true,
		}
		bits, _ := testsignal.FMSTelegramBits(m)
		want.Raw = make([]byte, 6)
		for j, b := range bits {
			want.Raw[j/8] |= b << (7 - j%8)
		}
		for j := 0; j < 7; j++ {
			want.CRC |= int(bits[40+j]) << j
		}
		want.Received = want.Raw
		if p.CRC != want.CRC || p.CRCOK != want.CRCOK || p.Corrected {
			t.Errorf("%d: crc %d ok %v corrected %v, want %d", i, p.CRC, p.CRCOK, p.Corrected, want.CRC)
		}
		p2 := *p
		p2.CRC, p2.Corrected = want.CRC, false
		if fmt.Sprint(p2) != fmt.Sprint(want) {
			t.Errorf("%d: payload\n got %+v\nwant %+v", i, p2, want)
		}
	}
}

func TestFMSSingleBitCorrection(t *testing.T) {
	d, s := newFMSDec(t)
	m := fmsCases[0]
	tg, _ := testsignal.FMSTelegramBits(m)
	tg[40] ^= 1 // a high bit: repairable since the shift fix in fms.c
	feedFMS(d, testsignal.FMSFrame(tg), 0)
	if len(s.msgs) != 1 {
		t.Fatalf("got %d messages, want 1 (the unrepaired attempt must not be delivered)", len(s.msgs))
	}
	p := s.msgs[0].Payload.(*message.FMSPayload)
	if !p.CRCOK || !p.Corrected {
		t.Fatalf("crc ok %v corrected %v", p.CRCOK, p.Corrected)
	}
	if p.ServiceID != int(m.Service) {
		t.Errorf("service %d not repaired, want %d", p.ServiceID, m.Service)
	}
	if string(p.Raw) == string(p.Received) {
		t.Error("raw and received are equal for a repaired telegram")
	}
	var wantRecv [6]byte
	for j, b := range tg {
		if b == 1 {
			wantRecv[j/8] |= 0x80 >> (j % 8)
		}
	}
	if string(p.Received) != string(wantRecv[:]) {
		t.Errorf("received % x, want % x", p.Received, wantRecv)
	}
}

func TestFMSBadCRCIsDelivered(t *testing.T) {
	d, s := newFMSDec(t)
	tg, _ := testsignal.FMSTelegramBits(fmsCases[1])
	for _, i := range []int{20, 33, 44} { // more than one bit error
		tg[i] ^= 1
	}
	feedFMS(d, testsignal.FMSFrame(tg), 0)
	// Telegrams with failed CRC are delivered too, flagged. Whether the
	// decoder's one-bit repair finds a (wrong) neighbour depends on its
	// bit-shift arithmetic, so only the invariants are asserted.
	if len(s.msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(s.msgs))
	}
	p := s.msgs[0].Payload.(*message.FMSPayload)
	if p.Corrected && !p.CRCOK {
		t.Error("corrected telegram flagged as CRC failure")
	}
	if !p.CRCOK && !strings.Contains(s.msgs[0].Text, "CRC INCORRECT") {
		t.Errorf("text %q does not report the CRC failure", s.msgs[0].Text)
	}
}

func TestFMSNoise(t *testing.T) {
	d, s := newFMSDec(t)
	bits, _ := testsignal.EncodeFMS(fmsCases[:2])
	feedFMS(d, bits, 0.15)
	if len(s.msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(s.msgs))
	}
}

func TestFMSReset(t *testing.T) {
	d, s := newFMSDec(t)
	tg, _ := testsignal.FMSTelegramBits(fmsCases[0])
	frame := testsignal.FMSFrame(tg)
	// Cut off mid-telegram, without trailing silence (silence would be
	// decoded as zero bits and complete the telegram).
	d.Process(testsignal.FMSAudio(frame[:30], 22050, 0.05, 0, 0, nil))
	d.Reset()
	feedFMS(d, frame, 0)
	if len(s.msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(s.msgs))
	}
}

// TestFMSAgainstMultimon decodes the synthetic audio with the multimon-ng
// binary (when installed) and compares its hex telegram with ours.
func TestFMSAgainstMultimon(t *testing.T) {
	bin, err := exec.LookPath("multimon-ng")
	if err != nil {
		t.Skip("multimon-ng not installed")
	}
	bits, _ := testsignal.EncodeFMS(fmsCases)
	audio := testsignal.FMSAudio(bits, 22050, 0.2, 0.2, 0, nil)
	pcm := make([]byte, 2*len(audio))
	for i, v := range audio {
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(v*32767)))
	}
	path := filepath.Join(t.TempDir(), "fms.raw")
	if err := os.WriteFile(path, pcm, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "-q", "-t", "raw", "-a", "FMSFSK", path).CombinedOutput()
	if err != nil {
		t.Fatalf("multimon-ng: %v\n%s", err, out)
	}
	d, s := newFMSDec(t)
	feedFMS(d, bits, 0)
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "FMS:") {
			lines = append(lines, l)
		}
	}
	if len(lines) != len(s.msgs) || len(lines) != len(fmsCases) {
		t.Fatalf("multimon-ng decoded %d telegrams, embedded decoder %d, sent %d\n%s", len(lines), len(s.msgs), len(fmsCases), out)
	}
	for i, l := range lines {
		if l != s.msgs[i].Text {
			t.Errorf("telegram %d differs:\n binary:   %q\n embedded: %q", i, l, s.msgs[i].Text)
		}
	}
}
