//go:build cgo

package mmng

/*
#include "pagewire_hooks.h"
#include "pagewire_gsc.h"
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
	decoder.Register("gsc", newGSC)
}

// GSC decodes Golay Sequential Code pages (600 baud) with the embedded
// multimon-ng decoder: alphanumeric, numeric and tone pages, and the start
// and end of voice messages.
type GSC struct {
	c    *cdecoder
	opts config.GSCOptions
	ev   decoder.Events
}

func newGSC(cfg config.DecoderConfig, ev decoder.Events) (decoder.Decoder, error) {
	d := &GSC{ev: ev}
	if cfg.GSC != nil {
		d.opts = *cfg.GSC
	}
	c, err := newCDecoder("GSC", d)
	if err != nil {
		return nil, err
	}
	d.c = c
	c.init()
	return d, nil
}

// Protocol implements decoder.Decoder.
func (d *GSC) Protocol() string { return "gsc" }

// SampleRate implements decoder.Decoder.
func (d *GSC) SampleRate() uint32 { return d.c.sampleRate() }

// Process implements decoder.Decoder.
func (d *GSC) Process(samples []float32) { d.c.process(samples) }

// Reset implements decoder.Decoder.
func (d *GSC) Reset() { d.c.reset() }

// Close implements decoder.Decoder.
func (d *GSC) Close() error {
	d.c.free()
	return nil
}

// goMMNGGsc receives GSC page records from the C decoder.
//
//export goMMNGGsc
func goMMNGGsc(ctx unsafe.Pointer, m *C.struct_pagewire_gsc_message) {
	d, ok := cgo.Handle(uintptr(ctx)).Value().(*GSC)
	if !ok {
		return
	}
	msgType := C.GoString(m.msg_type)
	isVoice := msgType == "voice"
	if isVoice && !d.opts.IncludeVoice {
		return
	}
	p := &message.GSCPayload{
		MsgType:            msgType,
		Inverted:           m.polarity_inverted != 0,
		Score:              int(m.score),
		AltScore:           int(m.alt_score),
		Blocks:             int(m.blocks),
		ErrorCount:         int(m.error_count),
		UncorrectableCount: int(m.uncorrectable_count),
	}
	if m.alt_type != nil {
		p.AltType = C.GoString(m.alt_type)
		if m.alt_message != nil {
			p.AltText = C.GoStringN(m.alt_message, C.int(m.alt_message_len))
		}
	}
	if m.voice_state != nil {
		p.VoiceState = C.GoString(m.voice_state)
	}
	if m.raw_len > 0 {
		p.Raw = C.GoBytes(unsafe.Pointer(m.raw), C.int(m.raw_len))
	}
	msg := &message.Message{
		Address:  C.GoString(m.address),
		Function: int(m.function),
		Kind:     message.KindTone,
		Payload:  p,
	}
	if m.message != nil {
		msg.Text = C.GoStringN(m.message, C.int(m.message_len))
	}
	switch msgType {
	case "alpha":
		msg.Kind = message.KindAlpha
	case "numeric":
		msg.Kind = message.KindNumeric
	}
	if d.ev != nil {
		d.ev.Message(msg)
	}
}
