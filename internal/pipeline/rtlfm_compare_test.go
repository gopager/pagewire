//go:build cgo

package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	_ "github.com/gopager/pagewire/internal/decoder/mmng"
	"github.com/gopager/pagewire/internal/message"
	"github.com/gopager/pagewire/internal/sdr/iqfile"
	"github.com/gopager/pagewire/internal/testsignal"
)

// rtlFMBlockBytes is rtl_fm's DEFAULT_BUF_LENGTH: bytes per callback block.
const rtlFMBlockBytes = 16384

// rtlFMPasses is the number of fifth_order passes rtl_fm derives for
// "-s 22050 -F 0": (int)log2(1000000/22050+1) + 1 = 6 (optimal_settings,
// rtl_fm.c:866-870).
const rtlFMPasses = 6

// rtlFMState carries the per-stream state of rtl_fm's demod_state that
// survives between blocks.
type rtlFMState struct {
	iHist, qHist [rtlFMPasses][6]int16 // lp_i_hist / lp_q_hist
	preR, preJ   int16                 // pre_r / pre_j
	dcAvg        int                   // dc_avg
}

// rtlFMRotate90 ports rotate_90 (rtl_fm.c:284-303): it multiplies every
// sample by j^k, which moves a signal at -rate/4 to 0 Hz.
func rtlFMRotate90(buf []byte) {
	for i := 0; i+7 < len(buf); i += 8 {
		tmp := 255 - buf[i+3]
		buf[i+3] = buf[i+2]
		buf[i+2] = tmp
		buf[i+4] = 255 - buf[i+4]
		buf[i+5] = 255 - buf[i+5]
		tmp = 255 - buf[i+6]
		buf[i+6] = buf[i+7]
		buf[i+7] = tmp
	}
}

// rtlFMFifthOrder ports fifth_order (rtl_fm.c:365-392) on one half of the
// interleaved data. Intermediates are int, stores wrap to int16 like C.
func rtlFMFifthOrder(data []int16, length int, hist *[6]int16) {
	a, b, c, d, e, f := hist[1], hist[2], hist[3], hist[4], hist[5], data[0]
	data[0] = int16((int(a) + (int(b)+int(e))*5 + (int(c)+int(d))*10 + int(f)) >> 4)
	for i := 4; i < length; i += 4 {
		a, b, c, d = c, d, e, f
		e = data[i-2]
		f = data[i]
		data[i/2] = int16((int(a) + (int(b)+int(e))*5 + (int(c)+int(d))*10 + int(f)) >> 4)
	}
	hist[0], hist[1], hist[2], hist[3], hist[4], hist[5] = a, b, c, d, e, f
}

// rtlFMFastAtan2 ports fast_atan2 (rtl_fm.c:439-460), scaled so pi = 1<<14.
func rtlFMFastAtan2(y, x int32) int32 {
	const pi4, pi34 = 1 << 12, 3 * (1 << 12)
	if x == 0 && y == 0 {
		return 0
	}
	yabs := y
	if yabs < 0 {
		yabs = -yabs
	}
	var angle int32
	if x >= 0 {
		angle = pi4 - pi4*(x-yabs)/(x+yabs)
	} else {
		angle = pi34 - pi4*(x+yabs)/(yabs-x)
	}
	if y < 0 {
		return -angle
	}
	return angle
}

// rtlFMDisc ports multiply() plus polar_disc_fast() (rtl_fm.c:431-467) when
// fast is true, else polar_discriminant() (rtl_fm.c:433-438).
func rtlFMDisc(ar, aj, br, bj int32, fast bool) int32 {
	cr := ar*br - aj*(-bj)
	cj := aj*br + ar*(-bj)
	if fast {
		return rtlFMFastAtan2(cj, cr)
	}
	return int32(math.Atan2(float64(cj), float64(cr)) / 3.14159 * (1 << 14))
}

// process ports one rtlsdr_callback + full_demod run (rtl_fm.c:733-777,
// 783-806) for a block of cu8 bytes and returns the 22.05 kHz int16 audio.
// The first discriminator sample uses the accurate atan2 like fm_demod does
// (rtl_fm.c:524-526); the rest use the fast one (-A fast).
func (s *rtlFMState) process(block []byte) []int16 {
	buf := append([]byte(nil), block...)
	rtlFMRotate90(buf)
	lp := make([]int16, len(buf))
	for i, v := range buf {
		lp[i] = int16(v) - 127
	}
	n := len(lp)
	for p := 0; p < rtlFMPasses; p++ {
		rtlFMFifthOrder(lp, n>>p, &s.iHist[p])
		rtlFMFifthOrder(lp[1:], (n>>p)-1, &s.qHist[p])
	}
	n >>= rtlFMPasses
	res := make([]int16, n/2)
	res[0] = int16(rtlFMDisc(int32(lp[0]), int32(lp[1]), int32(s.preR), int32(s.preJ), false))
	for i := 2; i < n-1; i += 2 {
		res[i/2] = int16(rtlFMDisc(int32(lp[i]), int32(lp[i+1]), int32(lp[i-2]), int32(lp[i-1]), true))
	}
	s.preR, s.preJ = lp[n-2], lp[n-1]
	// dc_block_filter (rtl_fm.c:616-630)
	var sum int64
	for _, v := range res {
		sum += int64(v)
	}
	avg := int(sum / int64(len(res)))
	avg = (avg + s.dcAvg*9) / 10
	for i := range res {
		res[i] -= int16(avg)
	}
	s.dcAvg = avg
	return res
}

// rtlFMAudio runs the whole cu8 capture through the rtl_fm emulation and
// returns float32 audio as multimon-ng would read the int16 pipe.
func rtlFMAudio(cu8 []byte) []float32 {
	var st rtlFMState
	out := make([]float32, 0, len(cu8)/rtlFMBlockBytes*128)
	for off := 0; off+rtlFMBlockBytes <= len(cu8); off += rtlFMBlockBytes {
		for _, v := range st.process(cu8[off : off+rtlFMBlockBytes]) {
			out = append(out, float32(v)/32768)
		}
	}
	return out
}

// rtlFMCollector gathers decoder messages; it implements decoder.Events.
type rtlFMCollector struct{ msgs []*message.Message }

// Message stores a decoded message.
func (c *rtlFMCollector) Message(m *message.Message) { c.msgs = append(c.msgs, m) }

// Event ignores quality events.
func (c *rtlFMCollector) Event(decoder.Event) {}

// pocsagCompareOpts are the decoder options used by both chains.
func pocsagCompareOpts() config.DecoderConfig {
	return config.DecoderConfig{Protocol: "pocsag1200", POCSAG: &config.POCSAGOptions{Mode: "alpha", Charset: "US", Polarity: "auto"}}
}

// score counts messages whose address and text both match want and returns
// the first three mismatching texts.
func score(msgs []*message.Message, want map[string]string) (int, []string) {
	ok := map[string]bool{}
	var bad []string
	for _, m := range msgs {
		text := strings.TrimRight(m.Text, "\x00")
		switch exp, known := want[m.Address]; {
		case known && exp == text:
			ok[m.Address] = true
		case len(bad) < 3:
			bad = append(bad, fmt.Sprintf("addr %s: %q", m.Address, text))
		}
	}
	return len(ok), bad
}

// decodeRTLFM runs chain A: rtl_fm emulation feeding the embedded decoder.
func decodeRTLFM(t *testing.T, cu8 []byte) []*message.Message {
	t.Helper()
	col := &rtlFMCollector{}
	d, err := decoder.New(pocsagCompareOpts(), col)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	audio := rtlFMAudio(cu8)
	for i := 0; i < len(audio); i += 4096 {
		d.Process(audio[i:min(i+4096, len(audio))])
	}
	return col.msgs
}

// decodeGateway runs chain B: the gateway pipeline on the same cu8 bytes
// with the channel tuned to freq.
func decodeGateway(t *testing.T, cu8 []byte, rate, center, freq int) []*message.Message {
	t.Helper()
	var got []*message.Message
	sink, err := NewDecoderSink([]config.ChannelConfig{{Name: "p", Decoders: []config.DecoderConfig{pocsagCompareOpts()}}},
		func(m *message.Message) { got = append(got, m) }, nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	chCfg := []config.ChannelConfig{{Name: "p", Source: "f", Frequency: config.Frequency(freq), Decoders: []config.DecoderConfig{pocsagCompareOpts()}}}
	dev := iqfile.New(bytes.NewReader(cu8), uint32(rate), uint32(center))
	src, err := NewSource(config.SourceConfig{Name: "f", Type: config.SourceRTLSDR, RTLSDR: config.RTLSDRConfig{SampleRate: uint32(rate)}}, chCfg, dev, 22050, sink, nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := src.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	return got
}

// TestCompareRTLFM compares the decoding sensitivity of an rtl_fm port
// (rtl_fm -s 22050 -E dc -F 0 -A fast | multimon-ng) with the gateway
// pipeline on identical synthetic IQ at increasing noise. It is an
// experiment and only runs with PAGEWIRE_COMPARE_RTLFM=1.
func TestCompareRTLFM(t *testing.T) {
	if os.Getenv("PAGEWIRE_COMPARE_RTLFM") != "1" {
		t.Skip("set PAGEWIRE_COMPARE_RTLFM=1 to run the rtl_fm comparison experiment")
	}
	const (
		audioRate = 22050
		factor    = 64 // rtl_fm capture rate = 22050 * 64
		rate      = audioRate * factor
		center    = 150000000
		sigOff    = -rate / 4 // rtl_fm tunes freq + rate/4, rotate_90 shifts back
		amp       = 0.3
		pages     = 25
	)
	var msgs []testsignal.POCSAGMessage
	want := map[string]string{}
	for i := 0; i < pages; i++ {
		addr := 1000001 + i
		text := fmt.Sprintf("EINSATZ %02d FEUER HALLE %c ETAGE %02d", i+1, 'A'+i%26, (i*7)%40)
		msgs = append(msgs, testsignal.POCSAGMessage{Address: uint32(addr), Function: 3, Text: text})
		want[fmt.Sprint(addr)] = text
	}
	bits, err := testsignal.EncodePOCSAG(msgs)
	if err != nil {
		t.Fatal(err)
	}
	base := testsignal.FSKAudio(bits, testsignal.POCSAGFSK(1200), audioRate, 1.0, 1.0, 0, nil)
	wide := testsignal.Upsample(base, factor)
	for i := range wide {
		wide[i] /= factor
	}
	iq := testsignal.FMModulateIQ(wide, rate, sigOff, amp)

	levels := []float64{0, 0.5, 1, 1.25, 1.4, 1.5, 1.6, 1.75, 2}
	// Several noise realisations per level keep single-page flukes from
	// dominating the comparison.
	seeds := []int64{42, 7, 1234, 99}
	var rows []string
	var mismatches []string
	for _, noise := range levels {
		var a, b int
		var badA, badB []string
		for _, seed := range seeds {
			cu8 := testsignal.ToCU8(iq, noise, rand.New(rand.NewSource(seed)).Float64)
			na, ba := score(decodeRTLFM(t, cu8), want)
			nb, bb := score(decodeGateway(t, cu8, rate, center, center+sigOff), want)
			a, b = a+na, b+nb
			badA, badB = append(badA, ba...), append(badB, bb...)
		}
		total := pages * len(seeds)
		// ToCU8 adds uniform noise of peak `noise`, so sigma per I/Q
		// component is noise/sqrt(3). SNR in a 12.5 kHz channel is
		// A^2*fs / (2*sigma^2*12500) with A the complex amplitude.
		snr := "clean"
		if noise > 0 {
			sigma2 := noise * noise / 3
			snr = fmt.Sprintf("%.1f", 10*math.Log10(amp*amp*rate/(2*sigma2*12500)))
		}
		rows = append(rows, fmt.Sprintf("%6.2f | %8s | %3d/%d | %3d/%d", noise, snr, a, total, b, total))
		for _, s := range badA {
			mismatches = append(mismatches, fmt.Sprintf("noise %.2f rtl_fm: %s", noise, s))
		}
		for _, s := range badB {
			mismatches = append(mismatches, fmt.Sprintf("noise %.2f gateway: %s", noise, s))
		}
		if noise == 0 && (a != total || b != total) {
			t.Errorf("clean level: rtl_fm %d/%d, gateway %d/%d", a, total, b, total)
		}
	}
	t.Logf("\nnoise  | SNR dB   | rtl_fm  | gateway\n%s", strings.Join(rows, "\n"))
	for _, m := range mismatches {
		t.Log(m)
	}
}
