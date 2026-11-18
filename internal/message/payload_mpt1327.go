package message

import (
	"encoding/binary"
	"encoding/gob"
	"encoding/hex"
)

func init() {
	gob.Register(&MPT1327Payload{})
}

// MPT1327Field is one named field of an MPT 1327 record as the decoder
// extracts it from the codeword.
type MPT1327Field struct {
	Name  string `json:"name"`
	Value uint64 `json:"value"`
}

// MPT1327Payload is the protocol record of one MPT 1327 codeword (message
// kind status). Message and the field names are those of the decoder:
// Message is one of CCSC, DCSC, DATA, GTC, the ALH, ACK, AHY and MARK/MAINT/
// CLEAR/MOVE/BCAST families, or UNDECODED for codewords of an unparsed
// category or function. Typical fields are pfix, ident1, ident2, chan, sys,
// params (the raw parameter bits of an undecoded codeword) and cat, type and
// func for undecoded codewords; the full set per message is defined by
// demod_mpt1327.c. Idents and prefixes are numbers; the special idents of
// the standard (e.g. 8101 PSTNGI) are not renamed.
type MPT1327Payload struct {
	Message string         `json:"message"`
	Fields  []MPT1327Field `json:"fields"`
	// Codeword is the 64 bits after error correction; bit 1 (sent first)
	// is the most significant bit. Raw is the same as 8 big-endian bytes.
	Codeword uint64 `json:"codeword"`
	Raw      []byte `json:"raw"`
}

// NewMPT1327Payload builds a payload and its Raw bytes from a codeword.
func NewMPT1327Payload(message string, codeword uint64, fields []MPT1327Field) *MPT1327Payload {
	raw := make([]byte, 8)
	binary.BigEndian.PutUint64(raw, codeword)
	return &MPT1327Payload{Message: message, Fields: fields, Codeword: codeword, Raw: raw}
}

// Field returns the value of the named field.
func (p *MPT1327Payload) Field(name string) (uint64, bool) {
	for _, f := range p.Fields {
		if f.Name == name {
			return f.Value, true
		}
	}
	return 0, false
}

// RawHex returns Raw as hex digits.
func (p *MPT1327Payload) RawHex() string { return hex.EncodeToString(p.Raw) }
