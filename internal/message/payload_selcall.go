package message

import "encoding/gob"

func init() {
	gob.Register(&SelcallPayload{})
}

// SelcallPayload is the protocol record of a selective-call tone sequence
// (ZVEI1/2/3, DZVEI, PZVEI, CCIR, EEA, EIA, DTMF, DUALTONE). Message.Address
// and Message.Text hold Sequence.
type SelcallPayload struct {
	// Variant is the multimon-ng demodulator name, e.g. "ZVEI1".
	Variant string `json:"variant"`
	// Sequence is the tone sequence as the decoder renders it: one
	// character per tone (hex digit, DTMF key) or "fs4,fs5" for DUALTONE.
	Sequence string `json:"sequence"`
	// Tones are the raw tone indices in order: 0-15 for the sequential
	// tone systems, row*4+column for DTMF keys, the tone numbers 4 and 5
	// or 7 for DUALTONE.
	Tones []int `json:"tones"`
	// Frequencies lists the nominal frequencies in Hz of each tone: one
	// for the sequential systems and DUALTONE, row and column frequency for
	// a DTMF key.
	Frequencies [][]float64 `json:"frequencies"`
}
