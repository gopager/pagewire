package message

import (
	"encoding/gob"
	"encoding/hex"
)

func init() {
	gob.Register(&GSCPayload{})
}

// GSCPayload is the protocol record of a GSC (Golay Sequential Code) page or
// voice event, as the decoder reports it.
type GSCPayload struct {
	// MsgType is the decoder's classification: alpha, numeric, tone or
	// voice.
	MsgType string `json:"msg_type"`
	// Inverted is set when the transmission was received with inverted
	// polarity.
	Inverted bool `json:"inverted"`
	// AltType and AltText are the other rendering of the same data blocks
	// (a numeric page also has an alpha rendering and vice versa); Score
	// and AltScore are the decoder's content scores for MsgType and
	// AltType. Message.Text holds the rendering named by MsgType.
	AltType  string `json:"alt_type,omitempty"`
	AltText  string `json:"alt_text,omitempty"`
	Score    int    `json:"score,omitempty"`
	AltScore int    `json:"alt_score,omitempty"`
	// Blocks is the number of data blocks, -1 when not applicable.
	Blocks int `json:"blocks"`
	// ErrorCount and UncorrectableCount are the decoder's running counters
	// of corrected and uncorrectable BCH words at the time of the page, -1
	// when not applicable.
	ErrorCount         int `json:"error_count"`
	UncorrectableCount int `json:"uncorrectable_count"`
	// VoiceState is "start" or "end" for voice events.
	VoiceState string `json:"voice_state,omitempty"`
	// Raw holds the data blocks after BCH correction: 8 bytes per block,
	// seven data words followed by the checksum word, 7 bits each (0 for an
	// uncorrectable word). Empty for tone and voice events.
	Raw []byte `json:"raw,omitempty"`
}

// RawHex returns Raw as hex digits.
func (p *GSCPayload) RawHex() string { return hex.EncodeToString(p.Raw) }
