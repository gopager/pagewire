package pipeline

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

type collector struct {
	audio map[string][]float32
	done  chan struct{}
	want  int
}

func (c *collector) Audio(ch *Channel, s []float32) {
	c.audio[ch.Name] = append(c.audio[ch.Name], s...)
	if len(c.audio[ch.Name]) >= c.want {
		select {
		case <-c.done:
		default:
			close(c.done)
		}
	}
}

// TestFMToneThroughPipeline generates a carrier at the channel frequency,
// frequency-modulated with a 1 kHz tone at ±3 kHz deviation, and checks that
// the channel delivers that tone.
func TestFMToneThroughPipeline(t *testing.T) {
	const chanFreq = 150000000
	chCfg := []config.ChannelConfig{{Name: "t", Source: "s", Frequency: chanFreq, Squelch: config.Level{Auto: true},
		Decoders: []config.DecoderConfig{{Protocol: "pocsag1200"}}}}
	srcCfg := config.SourceConfig{Name: "s", Type: config.SourceRTLSDR}

	// A replay-style mock: the generator runs as fast as the pipeline
	// consumes, so no blocks are dropped and the audio stays contiguous.
	const rate, center = 22050 * 11, chanFreq + 60000
	var phase float64
	dev := mock.NewReplay(func(dst []byte, start int64) bool {
		off := float64(chanFreq) - float64(center)
		for i := 0; i < len(dst)/2; i++ {
			n := float64(start + int64(i))
			deviation := 3000 * math.Sin(2*math.Pi*1000*n/rate)
			phase += 2 * math.Pi * (off + deviation) / rate
			z := cmplx.Rect(0.5, phase)
			dst[2*i] = byte(127.5 + 127.5*real(z))
			dst[2*i+1] = byte(127.5 + 127.5*imag(z))
		}
		return true
	}, rate, center)
	col := &collector{audio: map[string][]float32{}, done: make(chan struct{}), want: 22050}
	src, err := NewSource(srcCfg, chCfg, dev, 22050, col, nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if src.Plan.SampleRate != rate || src.Plan.CenterHz != center {
		t.Fatalf("plan = %+v", src.Plan)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() {
		<-col.done
		cancel()
	}()
	if err := src.Run(ctx); err != nil {
		t.Fatal(err)
	}
	audio := col.audio["t"]
	if len(audio) < 22050 {
		t.Fatalf("got %d audio samples", len(audio))
	}
	seg := audio[len(audio)/2:]
	// Expected amplitude: deviation / (rate/2).
	var acc complex128
	for i, v := range seg {
		acc += complex(float64(v), 0) * cmplx.Rect(1, -2*math.Pi*1000*float64(i)/22050)
	}
	amp := 2 * cmplx.Abs(acc) / float64(len(seg))
	want := 3000.0 / 11025
	if math.Abs(amp-want) > 0.03 {
		t.Errorf("1 kHz amplitude = %.3f, want %.3f", amp, want)
	}
	st := src.Channels[0].Status()
	if !st.GateOpen {
		t.Errorf("status = %+v", st)
	}
}

// TestNoiseIsGated feeds ADC noise and expects the gate to stay closed so
// no audio reaches the decoders.
func TestNoiseIsGated(t *testing.T) {
	chCfg := []config.ChannelConfig{{Name: "n", Source: "s", Frequency: 150000000, Squelch: config.Level{Auto: true},
		Decoders: []config.DecoderConfig{{Protocol: "pocsag1200"}}}}
	seed := uint32(7)
	blocks := 0
	dev := mock.New(func(dst []byte, start int64) bool {
		for i := range dst {
			seed = seed*1664525 + 1013904223
			dst[i] = byte(120 + seed>>29) // low-level noise around mid scale
		}
		blocks++
		return blocks < 200
	})
	col := &collector{audio: map[string][]float32{}, done: make(chan struct{}), want: 1 << 30}
	src, err := NewSource(config.SourceConfig{Name: "s", Type: config.SourceRTLSDR}, chCfg, dev, 22050, col, nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := src.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(col.audio["n"]); n != 0 {
		t.Errorf("noise produced %d audio samples, gate should be closed", n)
	}
	if st := src.Channels[0].Status(); st.GateOpen {
		t.Errorf("gate open on noise: %+v", st)
	}
}
