package message

import (
	"encoding/gob"
	"encoding/hex"
)

func init() {
	// Payload types travel through the persistent queue as gob values.
	gob.Register(&POCSAGPayload{})
	gob.Register(&FLEXPayload{})
}

// POCSAGPayload is the protocol record of a POCSAG page. It has two layers:
// the raw content exactly as received (Raw/RawBits), from which every
// interpretation can be derived later, and the two renderings the protocol
// defines, computed deterministically from the raw content.
type POCSAGPayload struct {
	Baud int `json:"baud"`
	// Partial is set when the page was cut off by sync loss.
	Partial bool `json:"partial"`
	// Raw holds the message content bit stream in transmission order: the
	// 20 data bits of every message codeword that followed the address
	// codeword, including the padding bits of the last codeword. Bits are
	// packed most significant bit first; RawBits says how many are valid.
	// The content is interpreted as follows:
	//   numeric: 4 bits per symbol; the on-air order is the reverse of the
	//            value, so symbol = "0123456789*U -)("[reverse4(nibble)].
	//   alpha:   7 bits per character, least significant bit first, ISO 646
	//            with the national variant of the configured charset.
	Raw     []byte `json:"raw"`
	RawBits int    `json:"raw_bits"`
	// Numeric and Alpha are the renderings of Raw as multimon-ng produces
	// them (Alpha with the configured charset). Message.Text holds the one
	// selected by the decoder mode.
	Numeric string `json:"numeric"`
	Alpha   string `json:"alpha"`
}

// RawHex returns Raw as hex digits, one digit per 4 bits in transmission
// order, trimmed to RawBits.
func (p *POCSAGPayload) RawHex() string {
	h := hex.EncodeToString(p.Raw)
	if n := (p.RawBits + 3) / 4; n < len(h) {
		h = h[:n]
	}
	return h
}

// FLEXPayload is the protocol record of a FLEX page.
type FLEXPayload struct {
	Baud   int    `json:"baud"`
	Levels int    `json:"levels"`
	Cycle  int    `json:"cycle"`
	Frame  int    `json:"frame"`
	Phase  string `json:"phase"`
	// MessageType is the FLEX page type (alphanumeric, numeric, …).
	MessageType string `json:"message_type"`
	// Fragment is the decoder's assembly state: complete, reassembled,
	// reassembled_partial, fragment_start/mid/end/orphan.
	Fragment string `json:"fragment"`
	// Reassembled is set when the text was joined from several frames.
	Reassembled bool `json:"reassembled"`
	// Group is set for group pages; Capcodes lists all member capcodes.
	Group    bool    `json:"group"`
	Capcodes []int64 `json:"capcodes,omitempty"`
	// MessageNumber and Retrieval are the FLEX numbering fields; Maildrop
	// and Priority are the corresponding flags.
	MessageNumber int  `json:"message_number"`
	Retrieval     int  `json:"retrieval"`
	Maildrop      bool `json:"maildrop"`
	Priority      bool `json:"priority"`
	// ChecksumOK and SignatureOK report the FLEX integrity checks.
	ChecksumOK  bool `json:"checksum_ok"`
	SignatureOK bool `json:"signature_ok"`
	// NetworkTime is the over-the-air time string when the network sends it.
	NetworkTime string `json:"network_time,omitempty"`
	// Raw holds the page content as received: for alphanumeric pages one
	// byte per 7-bit character in order, fill characters (ETX) included;
	// for numeric pages one byte per digit character; for binary pages the
	// decoder's hex rendering as bytes.
	Raw []byte `json:"raw"`
}

// RawHex returns Raw as hex digits.
func (p *FLEXPayload) RawHex() string { return hex.EncodeToString(p.Raw) }
