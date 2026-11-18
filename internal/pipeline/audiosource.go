package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/gopager/pagewire/internal/audio"
	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/dsp"
)

// AudioSource feeds one channel from an already demodulated audio input,
// resampling to the channel rate when needed. Reception quality cannot be
// measured on audio, so the channel reports Status with no RSSI.
type AudioSource struct {
	Name    string
	Channel *Channel

	in   audio.Input
	rs   *dsp.Resampler
	sink AudioSink
	log  *slog.Logger
}

// audioBlock is the number of input samples per block (about 93 ms at
// 22050 Hz).
const audioBlock = 2048

// NewAudioSource binds the single channel configured for cfg to in.
func NewAudioSource(cfg config.SourceConfig, channels []config.ChannelConfig, in audio.Input, channelRate uint32, sink AudioSink, log *slog.Logger) (*AudioSource, error) {
	var mine *config.ChannelConfig
	for i := range channels {
		if channels[i].Source == cfg.Name {
			mine = &channels[i]
		}
	}
	if mine == nil {
		return nil, fmt.Errorf("source %s: no channel bound", cfg.Name)
	}
	s := &AudioSource{
		Name:    cfg.Name,
		Channel: &Channel{Name: mine.Name, Source: cfg.Name, Rate: channelRate, status: Status{Unavailable: true}},
		in:      in,
		sink:    sink,
		log:     log.With("source", cfg.Name),
	}
	if in.SampleRate() != channelRate {
		s.rs = dsp.NewResampler(in.SampleRate(), channelRate, audioBlock)
	}
	return s, nil
}

// Run streams audio until ctx is done or the input ends.
func (s *AudioSource) Run(ctx context.Context) error {
	s.log.Info("audio source streaming", "input_rate", s.in.SampleRate(), "channel_rate", s.Channel.Rate, "channel", s.Channel.Name)
	err := s.in.Stream(ctx, audioBlock, func(samples []float32) {
		if s.rs != nil {
			samples = s.rs.Process(samples)
		}
		if len(samples) == 0 {
			return
		}
		s.Channel.status.GateOpen = true
		s.sink.Audio(s.Channel, samples)
	})
	if errors.Is(err, io.EOF) {
		s.log.Info("audio input ended")
		return nil
	}
	return err
}

// OpenAudio opens the input described by an audio source configuration.
func OpenAudio(ctx context.Context, cfg config.AudioConfig, stdin io.Reader) (audio.Input, error) {
	format, err := audio.ParseFormat(cfg.Format)
	if err != nil {
		return nil, err
	}
	switch cfg.Backend {
	case config.AudioStdin:
		return audio.NewReaderInput(stdin, nil, format, 1, cfg.Rate), nil
	case config.AudioFile:
		return audio.OpenFile(cfg.Device, format, 1, cfg.Rate)
	case config.AudioTCP:
		return audio.DialTCP(ctx, cfg.Device, format, 1, cfg.Rate)
	case config.AudioUDP:
		return audio.ListenUDP(cfg.Device, format, 1, cfg.Rate)
	case config.AudioPulse:
		return audio.OpenPulse(cfg.Device, cfg.Rate)
	default:
		return nil, fmt.Errorf("audio backend %q is not supported", cfg.Backend)
	}
}
