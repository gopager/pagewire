//go:build cgo

package mmng

/*
#include "pagewire_hooks.h"
#include "pagewire_fms.h"
*/
import "C"

import (
	"fmt"
	"runtime/cgo"
	"unsafe"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	"github.com/gopager/pagewire/internal/message"
)

func init() {
	decoder.Register("fms", newFMS)
}

// FMS decodes FMS (Funkmeldesystem) status telegrams, 1200 baud AFSK.
type FMS struct {
	c  *cdecoder
	ev decoder.Events
}

func newFMS(cfg config.DecoderConfig, ev decoder.Events) (decoder.Decoder, error) {
	d := &FMS{ev: ev}
	c, err := newCDecoder("FMSFSK", d)
	if err != nil {
		return nil, err
	}
	d.c = c
	c.init()
	return d, nil
}

// Protocol implements decoder.Decoder.
func (d *FMS) Protocol() string { return "fms" }

// SampleRate implements decoder.Decoder.
func (d *FMS) SampleRate() uint32 { return d.c.sampleRate() }

// Process implements decoder.Decoder.
func (d *FMS) Process(samples []float32) { d.c.process(samples) }

// Reset implements decoder.Decoder.
func (d *FMS) Reset() { d.c.reset() }

// Close implements decoder.Decoder.
func (d *FMS) Close() error {
	d.c.free()
	return nil
}

// goMMNGFms receives FMS telegram records from the C decoder.
//
//export goMMNGFms
func goMMNGFms(ctx unsafe.Pointer, m *C.struct_pagewire_fms_message) {
	d, ok := cgo.Handle(uintptr(ctx)).Value().(*FMS)
	if !ok || d.ev == nil {
		return
	}
	p := &message.FMSPayload{
		ServiceID:   int(m.service_id),
		StateID:     int(m.state_id),
		LocationRaw: int(m.loc_id_raw),
		LocationID:  int(m.loc_id),
		VehicleID:   int(m.vehicle_id),
		Vehicle:     C.GoString(&m.vehicle[0]),
		Status:      int(m.status),
		Model:       int(m.model),
		Direction:   int(m.direction),
		ShortInfo:   int(m.short_info),
		CRC:         int(m.crc),
		CRCOK:       m.crc_ok != 0,
		Corrected:   m.corrected != 0,
		Raw:         C.GoBytes(unsafe.Pointer(&m.raw[0]), C.int(len(m.raw))),
		Received:    C.GoBytes(unsafe.Pointer(&m.received[0]), C.int(len(m.received))),
	}
	d.ev.Message(&message.Message{
		Address:  fmt.Sprintf("%x%x%02x%s", p.ServiceID, p.StateID, p.LocationID, p.Vehicle),
		Function: -1,
		Kind:     message.KindStatus,
		Text:     C.GoString(m.text),
		Payload:  p,
	})
}
