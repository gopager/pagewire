package pipeline

import (
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	"github.com/gopager/pagewire/internal/message"
	"github.com/gopager/pagewire/internal/observe"
)

// MessageHandler receives finished messages from all channels. It is called
// from the processing goroutine of the channel's source.
type MessageHandler func(msg *message.Message)

// DecoderSink runs the configured decoders of every channel on its audio
// and forwards messages to a handler.
type DecoderSink struct {
	channels map[string]*channelDecoders
	handler  MessageHandler
	metrics  *observe.Metrics
	log      *slog.Logger
	nextID   atomic.Uint64
}

type channelDecoders struct {
	decoders []decoder.Decoder
	events   []*channelEvents
}

// channelEvents adapts decoder events for one channel and decoder to the
// message handler and metrics.
type channelEvents struct {
	sink     *DecoderSink
	channel  *Channel
	protocol string
	msgs     *prometheus.CounterVec
	events   *prometheus.CounterVec
	// corrected accumulates BCH corrections until the next message so they
	// can be attached to it.
	corrected     int
	uncorrectable int
	syncLost      bool
}

// NewDecoderSink instantiates the decoders for all channels.
func NewDecoderSink(channels []config.ChannelConfig, handler MessageHandler, m *observe.Metrics, log *slog.Logger) (*DecoderSink, error) {
	s := &DecoderSink{channels: map[string]*channelDecoders{}, handler: handler, metrics: m, log: log}
	for _, ch := range channels {
		cd := &channelDecoders{}
		for _, dc := range ch.Decoders {
			ev := &channelEvents{sink: s, protocol: dc.Protocol}
			if m != nil {
				ev.msgs = m.DecoderMessages
				ev.events = m.DecoderEvents
			}
			d, err := decoder.New(dc, ev)
			if err != nil {
				cd.close()
				return nil, fmt.Errorf("channel %s: %w", ch.Name, err)
			}
			if d.SampleRate() != decoder.DefaultRate {
				cd.close()
				return nil, fmt.Errorf("channel %s: decoder %s wants %d Hz, pipeline delivers %d Hz", ch.Name, dc.Protocol, d.SampleRate(), decoder.DefaultRate)
			}
			cd.decoders = append(cd.decoders, d)
			cd.events = append(cd.events, ev)
		}
		s.channels[ch.Name] = cd
	}
	return s, nil
}

func (cd *channelDecoders) close() {
	for _, d := range cd.decoders {
		d.Close()
	}
}

// Audio implements AudioSink.
func (s *DecoderSink) Audio(ch *Channel, samples []float32) {
	cd, ok := s.channels[ch.Name]
	if !ok {
		return
	}
	for i, d := range cd.decoders {
		cd.events[i].channel = ch
		d.Process(samples)
	}
}

// Close releases all decoders.
func (s *DecoderSink) Close() {
	for _, cd := range s.channels {
		cd.close()
	}
}

// Message implements decoder.Events.
func (e *channelEvents) Message(msg *message.Message) {
	ch := e.channel
	msg.ID = e.sink.nextID.Add(1)
	if msg.ReceivedAt.IsZero() {
		msg.ReceivedAt = time.Now()
	}
	msg.Protocol = e.protocol
	msg.Channel = message.Channel{Name: ch.Name, Source: ch.Source, FreqHz: ch.FreqHz}
	st := ch.Status()
	msg.Quality = message.Quality{
		Unavailable:   st.Unavailable,
		RSSIdBFS:      st.RSSIdBFS,
		NoiseDBFS:     st.NoiseDBFS,
		SNRdB:         st.SNRdB,
		FreqOffsetHz:  st.FreqOffsetHz,
		BitsCorrected: e.corrected,
		Uncorrectable: e.uncorrectable,
		SyncLost:      e.syncLost,
	}
	e.corrected, e.uncorrectable, e.syncLost = 0, 0, false
	if e.msgs != nil {
		e.msgs.WithLabelValues(ch.Name, e.protocol, string(msg.Kind)).Inc()
	}
	if e.sink.handler != nil {
		e.sink.handler(msg)
	}
}

// Event implements decoder.Events.
func (e *channelEvents) Event(ev decoder.Event) {
	name := "invalid_frame"
	switch ev.Kind {
	case decoder.EventSyncAcquired:
		name = "sync_acquired"
	case decoder.EventSyncLost:
		name, e.syncLost = "sync_lost", true
	case decoder.EventBitsCorrected:
		name = "bits_corrected"
		e.corrected += ev.Count
	case decoder.EventUncorrectable:
		name = "uncorrectable"
		e.uncorrectable++
	}
	if e.events != nil && e.channel != nil {
		c := e.events.WithLabelValues(e.channel.Name, e.protocol, name)
		if ev.Count > 1 {
			c.Add(float64(ev.Count))
		} else {
			c.Inc()
		}
	}
}
