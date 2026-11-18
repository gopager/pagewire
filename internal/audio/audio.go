// Package audio provides already-demodulated audio inputs (sound servers,
// files, pipes, sockets) as an alternative to the SDR front end.
package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Format is a raw PCM sample encoding.
type Format int

// Supported encodings.
const (
	FormatS16LE Format = iota
	FormatF32LE
)

// ParseFormat maps the configuration string to a Format.
func ParseFormat(s string) (Format, error) {
	switch s {
	case "s16le":
		return FormatS16LE, nil
	case "f32le":
		return FormatF32LE, nil
	default:
		return 0, fmt.Errorf("audio: unsupported format %q", s)
	}
}

// bytesPerSample returns the frame size of the encoding.
func (f Format) bytesPerSample() int {
	if f == FormatF32LE {
		return 4
	}
	return 2
}

// BlockFunc receives one block of mono samples in [-1, 1]. The slice is
// reused after the call returns.
type BlockFunc func(samples []float32)

// Input is a source of demodulated audio.
type Input interface {
	// SampleRate is the native rate of the input in Hz.
	SampleRate() uint32
	// Stream delivers blocks of blockLen samples to fn until ctx is done or
	// the input ends. A clean end of input returns io.EOF.
	Stream(ctx context.Context, blockLen int, fn BlockFunc) error
	// Close releases the input.
	Close() error
}

// decode converts raw PCM bytes into samples. channels > 1 are averaged
// down to mono.
func decode(dst []float32, src []byte, f Format, channels int) int {
	bps := f.bytesPerSample()
	frames := len(src) / (bps * channels)
	for i := 0; i < frames; i++ {
		var acc float32
		for c := 0; c < channels; c++ {
			off := (i*channels + c) * bps
			switch f {
			case FormatS16LE:
				acc += float32(int16(binary.LittleEndian.Uint16(src[off:]))) / 32768
			case FormatF32LE:
				acc += math.Float32frombits(binary.LittleEndian.Uint32(src[off:]))
			}
		}
		dst[i] = acc / float32(channels)
	}
	return frames
}

// readerInput streams PCM from an io.Reader (pipe, file, socket).
type readerInput struct {
	r        io.Reader
	closer   io.Closer
	format   Format
	channels int
	rate     uint32
}

// NewReaderInput wraps a raw PCM stream.
func NewReaderInput(r io.Reader, closer io.Closer, f Format, channels int, rate uint32) Input {
	return &readerInput{r: r, closer: closer, format: f, channels: channels, rate: rate}
}

// SampleRate implements Input.
func (in *readerInput) SampleRate() uint32 { return in.rate }

// Stream implements Input.
func (in *readerInput) Stream(ctx context.Context, blockLen int, fn BlockFunc) error {
	frame := in.format.bytesPerSample() * in.channels
	raw := make([]byte, blockLen*frame)
	out := make([]float32, blockLen)
	for {
		if ctx.Err() != nil {
			return nil
		}
		n, err := io.ReadFull(in.r, raw)
		if n > 0 {
			got := decode(out, raw[:n-n%frame], in.format, in.channels)
			fn(out[:got])
		}
		if err != nil {
			if err == io.ErrUnexpectedEOF {
				return io.EOF
			}
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

// Close implements Input.
func (in *readerInput) Close() error {
	if in.closer != nil {
		return in.closer.Close()
	}
	return nil
}
