// Package decoder defines the interface between the demodulated audio of a
// channel and the protocol decoders, plus the registry that creates decoders
// from configuration.
package decoder

import (
	"fmt"
	"sort"
	"sync"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/message"
)

// DefaultRate is the audio sample rate most decoders expect.
const DefaultRate = 22050

// Decoder consumes demodulated audio and emits messages and quality events.
// Implementations are driven from a single goroutine per channel.
type Decoder interface {
	// Protocol is the configured protocol name, e.g. "pocsag1200".
	Protocol() string
	// SampleRate is the audio rate the decoder expects.
	SampleRate() uint32
	// Process consumes one block of audio samples in [-1, 1].
	Process(samples []float32)
	// Reset clears all decoder state, e.g. after a long gap.
	Reset()
	// Close releases resources held by the decoder.
	Close() error
}

// Events receives what decoders produce. Methods are called synchronously
// from Process and must return quickly.
type Events interface {
	Message(msg *message.Message)
	Event(ev Event)
}

// EventKind classifies quality events.
type EventKind int

// Quality event kinds.
const (
	EventSyncAcquired EventKind = iota
	EventSyncLost
	EventBitsCorrected
	EventUncorrectable
	EventInvalidFrame
)

// Event is a decoder quality event; Count carries the number of corrected
// bits for EventBitsCorrected.
type Event struct {
	Kind  EventKind
	Count int
}

// Factory creates a decoder for a configured protocol.
type Factory func(cfg config.DecoderConfig, ev Events) (Decoder, error)

var (
	mu       sync.RWMutex
	registry = map[string]Factory{}
)

// Register makes a decoder available under a protocol name. It panics on
// duplicate registration because that is a programming error.
func Register(protocol string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[protocol]; dup {
		panic("decoder: duplicate registration of " + protocol)
	}
	registry[protocol] = f
}

// New creates the decoder for cfg.Protocol.
func New(cfg config.DecoderConfig, ev Events) (Decoder, error) {
	mu.RLock()
	f, ok := registry[cfg.Protocol]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("decoder: protocol %q is not available in this build (available: %v)", cfg.Protocol, Available())
	}
	return f(cfg, ev)
}

// Available lists the registered protocols in sorted order.
func Available() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for p := range registry {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
