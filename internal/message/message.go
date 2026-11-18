// Package message defines the gateway's own representation of a decoded
// transmission. Sinks map it to their target's format.
package message

import "time"

// Kind classifies the payload of a message.
type Kind string

// Message kinds.
const (
	KindAlpha   Kind = "alpha"   // free text
	KindNumeric Kind = "numeric" // digits and numeric pager symbols
	KindTone    Kind = "tone"    // address only, no payload
	KindStatus  Kind = "status"  // FMS status telegram
	KindAlert   Kind = "alert"   // EAS / selective call alert
	KindSelcall Kind = "selcall" // tone sequence
)

// Channel identifies where a message was received.
type Channel struct {
	Name   string
	Source string
	FreqHz uint32
}

// Quality describes reception conditions at the time of the message. RSSI
// and SNR are zero and Unavailable is true for audio sources.
type Quality struct {
	Unavailable   bool
	RSSIdBFS      float64
	NoiseDBFS     float64
	SNRdB         float64
	FreqOffsetHz  float64
	BitsCorrected int
	Uncorrectable int
	SyncLost      bool
}

// Message is one decoded transmission.
type Message struct {
	// ID is unique per gateway instance (monotonic counter at runtime).
	ID uint64
	// ReceivedAt is when the decoder emitted the message.
	ReceivedAt time.Time
	Protocol   string
	Channel    Channel
	// Address is the pager address (RIC, capcode) or equivalent as a
	// decimal string without padding.
	Address string
	// Function is the POCSAG function bits (0–3) or -1 when not applicable.
	Function int
	// Kind names the rendering that Text holds.
	Kind Kind
	// Text is the rendering of the payload selected by the decoder's mode:
	// the protocol's character set mapped to Unicode code points one to
	// one, control characters and NUL included, nothing trimmed, nothing
	// substituted. Transports that cannot carry some code points adapt it
	// in their sink, never here.
	Text string
	// Payload carries the protocol-specific record (see payloads.go),
	// including the byte-exact raw content and every rendering.
	Payload any
	Quality Quality
}
