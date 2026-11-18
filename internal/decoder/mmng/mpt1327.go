//go:build cgo

package mmng

/*
#include "pagewire_hooks.h"
#include "pagewire_mpt1327.h"
*/
import "C"

import (
	"runtime/cgo"
	"strconv"
	"unsafe"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	"github.com/gopager/pagewire/internal/message"
)

func init() {
	decoder.Register("mpt1327", newMPT1327)
}

// MPT1327 decodes the 1200 bit/s FFSK signalling of MPT 1327 trunked radio
// systems with the embedded multimon-ng decoder. Every validated codeword is
// a signalling record, delivered as a message of kind status: Address is the
// decimal IDENT1 where the codeword has one, Text the decoder's message name
// (GTC, ALH, ACK, ...), and the payload carries all fields and the raw
// codeword. The gateway does not interpret the fields.
type MPT1327 struct {
	c    *cdecoder
	opts config.MPT1327Options
	ev   decoder.Events
}

func newMPT1327(cfg config.DecoderConfig, ev decoder.Events) (decoder.Decoder, error) {
	d := &MPT1327{ev: ev}
	if cfg.MPT1327 != nil {
		d.opts = *cfg.MPT1327
	}
	c, err := newCDecoder("MPT1327", d)
	if err != nil {
		return nil, err
	}
	d.c = c
	c.init()
	return d, nil
}

// Protocol implements decoder.Decoder.
func (d *MPT1327) Protocol() string { return "mpt1327" }

// SampleRate implements decoder.Decoder.
func (d *MPT1327) SampleRate() uint32 { return d.c.sampleRate() }

// Process implements decoder.Decoder.
func (d *MPT1327) Process(samples []float32) { d.c.process(samples) }

// Reset implements decoder.Decoder.
func (d *MPT1327) Reset() { d.c.reset() }

// Close implements decoder.Decoder.
func (d *MPT1327) Close() error {
	d.c.free()
	return nil
}

// goMMNGMpt1327 receives MPT1327 records from the C decoder.
//
//export goMMNGMpt1327
func goMMNGMpt1327(ctx unsafe.Pointer, m *C.struct_pagewire_mpt1327_message) {
	d, ok := cgo.Handle(uintptr(ctx)).Value().(*MPT1327)
	if !ok {
		return
	}
	name := C.GoString(m.message)
	if !d.opts.IncludeSystem && (name == "CCSC" || name == "DCSC") {
		return
	}
	fields := make([]message.MPT1327Field, int(m.nfields))
	for i := range fields {
		fields[i] = message.MPT1327Field{Name: C.GoString(m.field_name[i]), Value: uint64(m.field_value[i])}
	}
	p := message.NewMPT1327Payload(name, uint64(m.codeword), fields)
	msg := &message.Message{Function: -1, Kind: message.KindStatus, Text: name, Payload: p}
	if id, ok := p.Field("ident1"); ok {
		msg.Address = strconv.FormatUint(id, 10)
	}
	if d.ev != nil {
		d.ev.Message(msg)
	}
}
