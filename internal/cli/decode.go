package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/gopager/pagewire/internal/audio"
	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	_ "github.com/gopager/pagewire/internal/decoder/mmng"
	"github.com/gopager/pagewire/internal/dsp"
	"github.com/gopager/pagewire/internal/message"
	"github.com/gopager/pagewire/internal/pipeline"
	"github.com/gopager/pagewire/internal/sdr/iqfile"
)

// DecodeCmd decodes a recording offline: either a raw IQ capture through the
// full channel pipeline or a demodulated audio file straight into the
// decoders. It is the basis of the differential tests against multimon-ng.
type DecodeCmd struct {
	IQ     string `help:"Raw unsigned 8-bit IQ capture (.cu8)." type:"existingfile" xor:"input" required:""`
	Audio  string `help:"Demodulated audio file (raw PCM or WAV)." type:"existingfile" xor:"input" required:""`
	Rate   uint32 `help:"Sample rate of the IQ capture or raw audio file in S/s."`
	Center uint32 `help:"Centre frequency of the IQ capture in Hz (required with --iq)."`
	Format string `help:"Raw audio sample format." enum:"s16le,f32le" default:"s16le"`

	Frequency []config.Frequency `help:"Channel frequency within the capture (repeatable, default: capture centre + 50 kHz)." name:"frequency" short:"f"`
	Protocol  []string           `help:"Decoder protocol for every channel (repeatable); none = only --stats/--dump-audio." short:"a"`
	Output    string             `help:"Output format." enum:"text,json" default:"text"`
	DumpAudio string             `help:"Also write each channel's audio as <name>.s16le into this directory." type:"existingdir"`
	Stats     bool               `help:"Print per-second channel statistics (RSSI, noise, SNR, quieting, gate) to stderr."`
	NoGate    bool               `help:"Deliver audio continuously instead of only while the squelch is open."`
}

// Run implements the command.
func (c DecodeCmd) Run(cctx *Context) error {
	log := slog.New(slog.NewTextHandler(cctx.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	var decoders []config.DecoderConfig
	for _, p := range c.Protocol {
		decoders = append(decoders, config.DecoderConfig{Protocol: p})
	}
	handler := c.printer(cctx.Stdout)

	var channels []config.ChannelConfig
	if c.IQ != "" {
		if c.Rate == 0 || c.Center == 0 {
			return fmt.Errorf("--iq needs --rate and --center")
		}
		freqs := c.Frequency
		if len(freqs) == 0 {
			freqs = []config.Frequency{config.Frequency(c.Center + 50000)}
		}
		gate := !c.NoGate
		for i, f := range freqs {
			channels = append(channels, config.ChannelConfig{Name: fmt.Sprintf("ch%d", i), Source: "file", Frequency: f, Squelch: config.Level{Auto: true}, PowerGate: &gate, Decoders: decoders})
		}
	} else {
		channels = []config.ChannelConfig{{Name: "audio", Source: "file", Decoders: decoders}}
	}
	cfg := config.Config{Instance: "decode", Sources: []config.SourceConfig{{Name: "file"}}, Channels: channels}
	if len(decoders) == 0 && !c.Stats && c.DumpAudio == "" {
		return fmt.Errorf("nothing to do: pass --protocol, --stats or --dump-audio")
	}
	var sink pipeline.AudioSink = fanout{}
	if len(decoders) > 0 {
		ds, err := pipeline.NewDecoderSink(cfg.Channels, handler, nil, log)
		if err != nil {
			return err
		}
		defer ds.Close()
		sink = ds
	}
	if c.Stats {
		sink = fanout{sink, &statsSink{w: cctx.Stderr}}
	}
	if c.DumpAudio != "" {
		writers := map[string]io.Writer{}
		for _, ch := range channels {
			f, err := os.Create(filepath.Join(c.DumpAudio, ch.Name+".s16le"))
			if err != nil {
				return err
			}
			defer f.Close()
			writers[ch.Name] = f
		}
		sink = fanout{sink, pipeline.NewRecorder(writers)}
	}

	ctx := context.Background()
	if c.IQ != "" {
		dev, err := iqfile.Open(c.IQ, c.Rate, c.Center)
		if err != nil {
			return err
		}
		defer dev.Close()
		src := config.SourceConfig{Name: "file", Type: config.SourceRTLSDR, RTLSDR: config.RTLSDRConfig{SampleRate: c.Rate}}
		ps, err := pipeline.NewSource(src, channels, dev, decoder.DefaultRate, sink, nil, log)
		if err != nil {
			return err
		}
		return ps.Run(ctx)
	}
	format, err := audio.ParseFormat(c.Format)
	if err != nil {
		return err
	}
	in, err := audio.OpenFile(c.Audio, format, 1, c.Rate)
	if err != nil {
		return err
	}
	defer in.Close()
	src := config.SourceConfig{Name: "file", Type: config.SourceAudio}
	as, err := pipeline.NewAudioSource(src, channels, in, decoder.DefaultRate, sink, log)
	if err != nil {
		return err
	}
	return as.Run(ctx)
}

// printer returns a message handler for the chosen output format.
func (c DecodeCmd) printer(w io.Writer) pipeline.MessageHandler {
	if c.Output == "json" {
		enc := json.NewEncoder(w)
		return func(msg *message.Message) { _ = enc.Encode(msg) }
	}
	return func(msg *message.Message) {
		fn := ""
		if msg.Function >= 0 {
			fn = fmt.Sprintf(" fn=%d", msg.Function)
		}
		fmt.Fprintf(w, "%s %s addr=%s%s %s: %s\n", msg.ReceivedAt.Format("15:04:05.000"), msg.Protocol, msg.Address, fn, msg.Kind, msg.Text)
	}
}

// fanout delivers audio to several sinks.
type fanout []pipeline.AudioSink

// Audio implements pipeline.AudioSink.
func (f fanout) Audio(ch *pipeline.Channel, s []float32) {
	for _, sink := range f {
		sink.Audio(ch, s)
	}
}

// statsSink prints one line of channel statistics per second of input,
// whether or not the gate is open.
type statsSink struct {
	w       io.Writer
	samples int
	sum     float32
	n       int
	openN   int
	sec     int
}

// Audio implements pipeline.AudioSink.
func (s *statsSink) Audio(*pipeline.Channel, []float32) {}

// Block implements pipeline.BlockObserver.
func (s *statsSink) Block(ch *pipeline.Channel, audio []float32, open bool) {
	s.samples += len(audio)
	s.sum += dsp.Quieting(audio)
	s.n++
	if open {
		s.openN++
	}
	if s.samples >= int(ch.Rate) {
		st := ch.Status()
		fmt.Fprintf(s.w, "t=%3ds rssi=%6.1f dBFS noise=%6.1f dBFS snr=%5.1f dB quieting=%.3f offset=%+6.0f Hz open=%d/%d\n",
			s.sec, st.RSSIdBFS, st.NoiseDBFS, st.SNRdB, s.sum/float32(s.n), st.FreqOffsetHz, s.openN, s.n)
		s.samples -= int(ch.Rate)
		s.sum, s.n, s.openN = 0, 0, 0
		s.sec++
	}
}

// Block forwards to every member that observes blocks.
func (f fanout) Block(ch *pipeline.Channel, audio []float32, open bool) {
	for _, sink := range f {
		if obs, ok := sink.(pipeline.BlockObserver); ok {
			obs.Block(ch, audio, open)
		}
	}
}
