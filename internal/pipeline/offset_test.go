//go:build cgo

package pipeline

import (
	"bytes"
	"context"
	"log/slog"
	"math/rand"
	"testing"

	"github.com/gopager/pagewire/internal/config"
	_ "github.com/gopager/pagewire/internal/decoder/mmng"
	"github.com/gopager/pagewire/internal/message"
	"github.com/gopager/pagewire/internal/sdr/iqfile"
	"github.com/gopager/pagewire/internal/testsignal"
)

// TestDecodesWithCarrierOffset sends a POCSAG page whose carrier sits
// 3 kHz off the channel frequency (about 20 ppm at 150 MHz), which shifts
// the discriminator output by 0.27 of full scale; the DC tracking must
// keep the embedded decoder working.
func TestDecodesWithCarrierOffset(t *testing.T) {
	const (
		rate    = 22050 * 46
		center  = 150000000
		chanOff = 150000
		carrier = 3000
	)
	bits, err := testsignal.EncodePOCSAG([]testsignal.POCSAGMessage{
		{Address: 1234567, Function: 3, Text: "OFFSET TEST ONE"},
		{Address: 1234568, Function: 3, Text: "OFFSET TEST TWO"},
	})
	if err != nil {
		t.Fatal(err)
	}
	base := testsignal.FSKAudio(bits, testsignal.POCSAGFSK(1200), 22050, 1.0, 0.5, 0, nil)
	wide := testsignal.Upsample(base, 46)
	for i := range wide {
		wide[i] /= 46
	}
	iq := testsignal.FMModulateIQ(wide, rate, chanOff+carrier, 0.3)
	cu8 := testsignal.ToCU8(iq, 0.15, rand.New(rand.NewSource(4)).Float64)

	var got []*message.Message
	sink, err := NewDecoderSink([]config.ChannelConfig{{Name: "p", Decoders: []config.DecoderConfig{{Protocol: "pocsag1200", POCSAG: &config.POCSAGOptions{Mode: "alpha", Charset: "US", Polarity: "auto"}}}}},
		func(m *message.Message) { got = append(got, m) }, nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	chCfg := []config.ChannelConfig{{Name: "p", Source: "f", Frequency: center + chanOff, Decoders: []config.DecoderConfig{{Protocol: "pocsag1200"}}}}
	dev := iqfile.New(bytes.NewReader(cu8), rate, center)
	src, err := NewSource(config.SourceConfig{Name: "f", Type: config.SourceRTLSDR, RTLSDR: config.RTLSDRConfig{SampleRate: rate}}, chCfg, dev, 22050, sink, nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := src.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	texts := map[string]bool{}
	for _, m := range got {
		texts[m.Address] = true
	}
	if !texts["1234567"] || !texts["1234568"] {
		t.Fatalf("pages not decoded with %d Hz carrier offset: %d messages (%v); status %+v", carrier, len(got), texts, src.Channels[0].Status())
	}
	if st := src.Channels[0].Status(); st.FreqOffsetHz < carrier-400 || st.FreqOffsetHz > carrier+400 {
		t.Errorf("offset estimate %.0f Hz, want about %d", st.FreqOffsetHz, carrier)
	}
}
