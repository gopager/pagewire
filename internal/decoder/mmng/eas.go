//go:build cgo

package mmng

/*
#include "pagewire_hooks.h"
#include "pagewire_eas.h"
*/
import "C"

import (
	"runtime/cgo"
	"unsafe"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	"github.com/gopager/pagewire/internal/message"
)

func init() {
	decoder.Register("eas", newEAS)
}

// EAS decodes EAS/SAME (US Emergency Alert System) headers and end-of-message
// markers.
//
// Every accepted header becomes a message of kind alert: Address is the
// originator and event code joined by a dash ("WXR-TOR"), Text is the SAME
// header exactly as received ("ZCZC-WXR-TOR-039173+0030-1591829-KCLE/NWS-")
// and the payload (message.EASPayload) holds its fields and the burst copies
// the decoder compared. When the option end_of_message is set, each received
// "NNNN" marker is delivered as a message with Address and Text "NNNN".
type EAS struct {
	c    *cdecoder
	opts config.EASOptions
	ev   decoder.Events
}

func newEAS(cfg config.DecoderConfig, ev decoder.Events) (decoder.Decoder, error) {
	d := &EAS{ev: ev}
	if cfg.EAS != nil {
		d.opts = *cfg.EAS
	}
	c, err := newCDecoder("EAS", d)
	if err != nil {
		return nil, err
	}
	d.c = c
	c.init()
	return d, nil
}

// Protocol implements decoder.Decoder.
func (d *EAS) Protocol() string { return "eas" }

// SampleRate implements decoder.Decoder.
func (d *EAS) SampleRate() uint32 { return d.c.sampleRate() }

// Process implements decoder.Decoder.
func (d *EAS) Process(samples []float32) { d.c.process(samples) }

// Reset implements decoder.Decoder.
func (d *EAS) Reset() { d.c.reset() }

// Close implements decoder.Decoder.
func (d *EAS) Close() error {
	d.c.free()
	return nil
}

// goMMNGEas receives EAS/SAME records from the C decoder.
//
//export goMMNGEas
func goMMNGEas(ctx unsafe.Pointer, m *C.struct_pagewire_eas_message) {
	d, ok := cgo.Handle(uintptr(ctx)).Value().(*EAS)
	if !ok {
		return
	}
	header := C.GoStringN(m.header, C.int(m.header_len))
	if m.kind == C.PAGEWIRE_EAS_EOM {
		d.handleEOM(header)
		return
	}
	p := message.ParseSAME(header)
	p.Burst = int(m.burst)
	p.Votes = int(m.agree)
	for i := 0; i < int(m.ncopies); i++ {
		cp := &m.copies[i]
		p.Copies = append(p.Copies, C.GoStringN(&cp.text[0], C.int(cp.len)))
	}
	p.AllCopiesAgree = len(p.Copies) == 3
	for _, c := range p.Copies {
		p.AllCopiesAgree = p.AllCopiesAgree && c == header
	}
	d.emit(&message.Message{Address: easAddress(p), Function: -1, Kind: message.KindAlert, Text: header, Payload: p})
}

// easAddress joins originator and event code ("WXR-TOR"); a header that
// could not be split keeps the address empty rather than a made-up value.
func easAddress(p *message.EASPayload) string {
	if p.Originator == "" || p.EventCode == "" {
		return ""
	}
	return p.Originator + "-" + p.EventCode
}

func (d *EAS) handleEOM(marker string) {
	if !d.opts.EndOfMessage {
		return
	}
	d.emit(&message.Message{
		Address: marker, Function: -1, Kind: message.KindAlert, Text: marker,
		Payload: &message.EASPayload{Kind: message.EASEOM, Header: marker, Valid: true},
	})
}

func (d *EAS) emit(msg *message.Message) {
	if d.ev != nil {
		d.ev.Message(msg)
	}
}
