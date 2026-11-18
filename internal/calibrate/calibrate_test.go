package calibrate

import (
	"context"
	"log/slog"
	"math"
	"math/cmplx"
	"testing"
	"time"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/sdr/mock"
)

// fmBurstDevice simulates a channel at chanFreq whose carrier sits offHz
// away from nominal, keyed on for burstS seconds every periodS seconds with
// a 1200 baud square FSK of ±4.5 kHz, and noise otherwise.
func fmBurstDevice(chanFreq uint32, offHz float64, burstS, periodS float64) *mock.Replay {
	const rate = 22050.0 * 11
	center := chanFreq + 60000
	var phase float64
	seed := uint32(5)
	return mock.NewReplay(func(dst []byte, start int64) bool {
		base := float64(chanFreq) - float64(center) + offHz
		for i := 0; i < len(dst)/2; i++ {
			n := start + int64(i)
			t := float64(n) / rate
			seed = seed*1664525 + 1013904223
			noise := (float64(seed>>8)/float64(1<<24) - 0.5) * 0.02
			var re, im float64
			if math.Mod(t, periodS) < burstS {
				bit := int(t*1200) % 2
				dev := 4500.0
				if bit == 1 {
					dev = -4500
				}
				phase += 2 * math.Pi * (base + dev) / rate
				z := cmplx.Rect(0.4, phase)
				re, im = real(z), imag(z)
			}
			dst[2*i] = byte(127.5 + 127.5*(re+noise))
			dst[2*i+1] = byte(127.5 + 127.5*(im+noise))
		}
		return true
	}, uint32(rate), center)
}

func TestMeasureOffsetFindsCarrier(t *testing.T) {
	const f = 150000000
	dev := fmBurstDevice(f, 1500, 0.8, 2.0)
	src := config.SourceConfig{Name: "s", Type: config.SourceRTLSDR}
	chans := []config.ChannelConfig{{Name: "c", Source: "s", Frequency: f, Decoders: []config.DecoderConfig{{Protocol: "pocsag1200"}}}}
	// The mock streams as fast as it can; a wall-clock budget of a few
	// seconds covers many simulated bursts.
	res, err := MeasureOffset(context.Background(), src, chans, dev, 0, 3*time.Second, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Bursts) < 3 {
		t.Fatalf("only %d bursts", len(res.Bursts))
	}
	if math.Abs(res.MedianOffsetHz-1500) > 150 {
		t.Errorf("median offset = %.0f Hz, want about 1500", res.MedianOffsetHz)
	}
	// The carrier is seen 1500 Hz too high, so the ppm must go down.
	if want := -1500.0 / f * 1e6; math.Abs(res.PPMDelta-want) > 1 {
		t.Errorf("ppm delta = %.2f, want about %.2f", res.PPMDelta, want)
	}
}

func TestRecommendGain(t *testing.T) {
	results := []GainResult{
		{GainTenthDB: 0, NoiseDBFS: -60, Clipping: 0, Bursts: 2, SNRdB: 10},
		{GainTenthDB: 200, NoiseDBFS: -45, Clipping: 0, Bursts: 2, SNRdB: 18},
		{GainTenthDB: 400, NoiseDBFS: -30, Clipping: 0.0005, Bursts: 2, SNRdB: 19},
		{GainTenthDB: 496, NoiseDBFS: -22, Clipping: 0.05, Bursts: 2, SNRdB: 20},
	}
	best, why := RecommendGain(results, 0.001)
	if best.GainTenthDB != 400 || why == "" {
		t.Errorf("got %d (%s), want 400", best.GainTenthDB, why)
	}
	// Without bursts: floor rises 15 dB per 20 dB, then only 2 dB -> knee.
	quiet := []GainResult{
		{GainTenthDB: 0, NoiseDBFS: -60, SNRdB: math.NaN()},
		{GainTenthDB: 200, NoiseDBFS: -45, SNRdB: math.NaN()},
		{GainTenthDB: 400, NoiseDBFS: -43, SNRdB: math.NaN()},
	}
	best, why = RecommendGain(quiet, 0.001)
	if best.GainTenthDB != 200 {
		t.Errorf("knee pick = %d (%s), want 200", best.GainTenthDB, why)
	}
}
