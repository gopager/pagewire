package audio

import (
	"context"
	"fmt"
	"io"

	"github.com/jfreymuth/pulse"
)

// pulseInput records from a PulseAudio or PipeWire server through the
// native protocol, without libpulse.
type pulseInput struct {
	client *pulse.Client
	source *pulse.Source
	rate   uint32
}

// OpenPulse connects to the default PulseAudio server and selects the
// source named by device (empty for the default source). The stream is
// requested at rate Hz mono.
func OpenPulse(device string, rate uint32) (Input, error) {
	client, err := pulse.NewClient(pulse.ClientApplicationName("pagewire"))
	if err != nil {
		return nil, fmt.Errorf("audio: pulse: %w", err)
	}
	var src *pulse.Source
	if device == "" {
		src, err = client.DefaultSource()
	} else {
		src, err = client.SourceByID(device)
	}
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("audio: pulse source %q: %w", device, err)
	}
	return &pulseInput{client: client, source: src, rate: rate}, nil
}

// SampleRate implements Input.
func (p *pulseInput) SampleRate() uint32 { return p.rate }

// Stream implements Input.
func (p *pulseInput) Stream(ctx context.Context, blockLen int, fn BlockFunc) error {
	out := make([]float32, blockLen)
	pending := 0
	errc := make(chan error, 1)
	writer := pulse.Float32Writer(func(in []float32) (int, error) {
		for len(in) > 0 {
			n := copy(out[pending:], in)
			pending += n
			in = in[n:]
			if pending == blockLen {
				fn(out)
				pending = 0
			}
		}
		return len(in), nil
	})
	stream, err := p.client.NewRecord(writer,
		pulse.RecordSource(p.source),
		pulse.RecordSampleRate(int(p.rate)),
		pulse.RecordMono,
		pulse.RecordLatency(0.05),
	)
	if err != nil {
		return fmt.Errorf("audio: pulse record: %w", err)
	}
	stream.Start()
	go func() {
		<-ctx.Done()
		stream.Stop()
		stream.Close()
		errc <- nil
	}()
	return <-errc
}

// Close implements Input.
func (p *pulseInput) Close() error {
	p.client.Close()
	return nil
}

var _ = io.EOF
