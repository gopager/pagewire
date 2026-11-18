package pipeline

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/sdr/iqfile"
	"github.com/gopager/pagewire/internal/testsignal"
)

// runMultimon decodes raw 22050 Hz s16le audio with the multimon-ng binary
// on PATH and returns its stdout. Tests skip when it is not installed.
func runMultimon(t *testing.T, pcm []byte, demod string) string {
	t.Helper()
	bin, err := exec.LookPath("multimon-ng")
	if err != nil {
		t.Skip("multimon-ng not installed")
	}
	path := filepath.Join(t.TempDir(), "audio.raw")
	if err := os.WriteFile(path, pcm, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "-q", "-t", "raw", "-a", demod, path).CombinedOutput()
	if err != nil {
		t.Fatalf("multimon-ng: %v\n%s", err, out)
	}
	return string(out)
}

func toS16LE(audio []float32) []byte {
	out := make([]byte, 2*len(audio))
	for i, v := range audio {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(int16(v*32767)))
	}
	return out
}

// TestEncoderAgainstMultimon checks the independent POCSAG encoder: its
// discriminator-style audio must decode in multimon-ng.
func TestEncoderAgainstMultimon(t *testing.T) {
	bits, err := testsignal.EncodePOCSAG([]testsignal.POCSAGMessage{{Address: 1234567, Function: 3, Text: "HELLO WORLD 123"}})
	if err != nil {
		t.Fatal(err)
	}
	audio := testsignal.FSKAudio(bits, testsignal.POCSAGFSK(1200), 22050, 0.5, 0.5, 0.02, rand.New(rand.NewSource(1)).Float64)
	out := runMultimon(t, toS16LE(audio), "POCSAG1200")
	if !strings.Contains(out, "Address: 1234567") || !strings.Contains(out, "Function: 3") || !strings.Contains(out, "HELLO WORLD 123") {
		t.Fatalf("multimon-ng did not decode the synthetic page:\n%s", out)
	}
}

// TestIQPipelineAgainstMultimon modulates the page onto a carrier 150 kHz
// off the capture centre at 1,014,300 S/s with noise, runs it through the
// channel pipeline and lets multimon-ng decode the resulting audio.
func TestIQPipelineAgainstMultimon(t *testing.T) {
	if _, err := exec.LookPath("multimon-ng"); err != nil {
		t.Skip("multimon-ng not installed")
	}
	const (
		rate    = 22050 * 46
		center  = 150000000
		chanOff = 150000
	)
	bits, err := testsignal.EncodePOCSAG([]testsignal.POCSAGMessage{{Address: 54321, Function: 0, Numeric: true, Text: "112-4711"}})
	if err != nil {
		t.Fatal(err)
	}
	base := testsignal.FSKAudio(bits, testsignal.POCSAGFSK(1200), 22050, 0.3, 0.3, 0, nil)
	wide := testsignal.Upsample(base, 46)
	// The baseband is scaled to ±1 = ±11025 Hz; at 46× the rate the same
	// deviation is 46× smaller in normalised units.
	for i := range wide {
		wide[i] /= 46
	}
	rnd := rand.New(rand.NewSource(2)).Float64
	iq := testsignal.FMModulateIQ(wide, rate, chanOff, 0.3)
	cu8 := testsignal.ToCU8(iq, 0.15, rnd)

	dev := iqfile.New(bytes.NewReader(cu8), rate, center)
	chCfg := []config.ChannelConfig{{Name: "p", Source: "f", Frequency: center + chanOff, Squelch: config.Level{Auto: true},
		Decoders: []config.DecoderConfig{{Protocol: "pocsag1200"}}}}
	var pcm bytes.Buffer
	rec := NewRecorder(map[string]io.Writer{"p": &pcm})
	src, err := NewSource(config.SourceConfig{Name: "f", Type: config.SourceRTLSDR, RTLSDR: config.RTLSDRConfig{SampleRate: rate}}, chCfg, dev, 22050, rec, nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := src.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pcm.Len() == 0 {
		t.Fatalf("pipeline produced no audio; status %+v", src.Channels[0].Status())
	}
	st := src.Channels[0].Status()
	t.Logf("channel status: rssi=%.1f dBFS noise=%.1f dBFS snr=%.1f dB offset=%.0f Hz, %d audio bytes", st.RSSIdBFS, st.NoiseDBFS, st.SNRdB, st.FreqOffsetHz, pcm.Len())
	out := runMultimon(t, pcm.Bytes(), "POCSAG1200")
	if !strings.Contains(out, "Address:   54321") || !strings.Contains(out, "Numeric: 112-4711") {
		t.Fatalf("multimon-ng did not decode the page from pipeline audio:\n%s", out)
	}
}
