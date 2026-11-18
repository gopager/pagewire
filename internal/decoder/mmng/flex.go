//go:build cgo

package mmng

/*
#include "pagewire_hooks.h"
*/
import "C"

import (
	"fmt"
	"runtime/cgo"
	"strconv"
	"unsafe"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	"github.com/gopager/pagewire/internal/message"
)

func init() {
	decoder.Register("flex", newFLEX)
}

// FLEX decodes FLEX pages (1600/3200/6400 bps, 2 and 4 level) with the
// embedded multimon-ng FLEX_NEXT decoder, which also reassembles fragments
// and tracks group pages.
type FLEX struct {
	c    *cdecoder
	opts config.FLEXOptions
	ev   decoder.Events
}

func newFLEX(cfg config.DecoderConfig, ev decoder.Events) (decoder.Decoder, error) {
	d := &FLEX{ev: ev}
	if cfg.FLEX != nil {
		d.opts = *cfg.FLEX
	}
	c, err := newCDecoder("FLEX_NEXT", d)
	if err != nil {
		return nil, err
	}
	d.c = c
	c.init()
	return d, nil
}

// Protocol implements decoder.Decoder.
func (d *FLEX) Protocol() string { return "flex" }

// SampleRate implements decoder.Decoder.
func (d *FLEX) SampleRate() uint32 { return d.c.sampleRate() }

// Process implements decoder.Decoder.
func (d *FLEX) Process(samples []float32) { d.c.process(samples) }

// Reset implements decoder.Decoder.
func (d *FLEX) Reset() { d.c.reset() }

// Close implements decoder.Decoder.
func (d *FLEX) Close() error {
	d.c.free()
	return nil
}

// FLEX message type codes as used by the decoder.
const (
	flexTypeSecure          = 0
	flexTypeInstruction     = 1
	flexTypeShortMessage    = 2
	flexTypeNumeric         = 3
	flexTypeSpecialNumeric  = 4
	flexTypeAlphanumeric    = 5
	flexTypeBinary          = 6
	flexTypeNumberedNumeric = 7
	flexTypeToneOnly        = 8
)

var flexTypeNames = map[int]string{
	flexTypeSecure: "secure", flexTypeInstruction: "instruction", flexTypeShortMessage: "short_msg",
	flexTypeNumeric: "numeric", flexTypeSpecialNumeric: "special_numeric", flexTypeAlphanumeric: "alphanumeric",
	flexTypeBinary: "binary", flexTypeNumberedNumeric: "numbered_numeric", flexTypeToneOnly: "tone_only",
}

// flexPage is the Go view of one decoder record.
type flexPage struct {
	capcode       int64
	addrType      byte
	phase         byte
	baud, levels  int
	cycle, frame  int
	msgType       int
	typeTag       string
	fragment      string
	msgN, msgR    int
	maildrop      bool
	kOK, sigOK    bool
	priority      bool
	group         bool
	message       string
	groupCapcodes []int64
	flextime      string
}

//export goMMNGFlex
func goMMNGFlex(ctx unsafe.Pointer, m *C.struct_pagewire_flex_message) {
	d, ok := cgo.Handle(uintptr(ctx)).Value().(*FLEX)
	if !ok {
		return
	}
	p := flexPage{
		capcode:  int64(m.capcode),
		addrType: byte(m.addr_type),
		phase:    byte(m.phase),
		baud:     int(m.baud),
		levels:   int(m.levels),
		cycle:    int(m.cycle),
		frame:    int(m.frame),
		msgType:  int(m.msg_type),
		typeTag:  C.GoString(m.type_tag),
		fragment: C.GoString(m.fragment),
		msgN:     int(m.msg_n),
		msgR:     int(m.msg_r),
		maildrop: m.msg_m != 0,
		kOK:      m.k_ok != 0,
		sigOK:    m.sig_ok != 0,
		priority: m.is_priority != 0,
		group:    m.is_group != 0,
		message:  C.GoString(m.message),
		flextime: C.GoString(m.flextime),
	}
	if m.group_count > 0 && m.group_capcodes != nil {
		caps := unsafe.Slice((*C.int64_t)(unsafe.Pointer(m.group_capcodes)), int(m.group_count))
		p.groupCapcodes = make([]int64, len(caps))
		for i, c := range caps {
			p.groupCapcodes[i] = int64(c)
		}
	}
	d.handle(p)
}

// handle maps one decoder record to zero or more messages.
func (d *FLEX) handle(p flexPage) {
	system := p.typeTag == "SMSG"
	if system && !d.opts.IncludeSystem {
		return
	}
	switch p.fragment {
	case "", "complete", "reassembled":
	default:
		if !d.opts.ShowPartial {
			return
		}
	}
	typeName := flexTypeNames[p.msgType]
	if typeName == "" {
		typeName = p.typeTag
	}
	payload := &message.FLEXPayload{
		Baud:          p.baud,
		Levels:        p.levels,
		Cycle:         p.cycle,
		Frame:         p.frame,
		Phase:         string(p.phase),
		MessageType:   typeName,
		Fragment:      p.fragment,
		Reassembled:   p.fragment == "reassembled" || p.fragment == "reassembled_partial",
		Group:         p.group || len(p.groupCapcodes) > 0,
		Capcodes:      p.groupCapcodes,
		MessageNumber: p.msgN,
		Retrieval:     p.msgR,
		Maildrop:      p.maildrop,
		Priority:      p.priority,
		ChecksumOK:    p.kOK,
		SignatureOK:   p.sigOK,
		NetworkTime:   p.flextime,
	}
	// p.message holds the received 7-bit characters with NUL carried as
	// 0x80 by the C decoder (see add_ch in demod_flex_next.c); restored
	// here, the bytes are both the raw record and, as ASCII, the text.
	raw := []byte(p.message)
	for i, c := range raw {
		if c == 0x80 {
			raw[i] = 0
		}
	}
	payload.Raw = raw
	text := string(raw)
	kind := message.KindAlpha
	switch {
	case system:
		kind = message.KindAlert
	case p.msgType == flexTypeToneOnly || p.typeTag == "TON":
		kind = message.KindTone
	case p.msgType == flexTypeNumeric || p.msgType == flexTypeSpecialNumeric || p.msgType == flexTypeNumberedNumeric:
		kind = message.KindNumeric
	case p.message == "" && p.msgType < 0:
		kind = message.KindTone
	}
	if system && text == "" {
		text = fmt.Sprintf("%s %s", p.typeTag, typeName)
	}
	emit := func(addr int64) {
		msg := &message.Message{
			Address:  strconv.FormatInt(addr, 10),
			Function: -1,
			Kind:     kind,
			Text:     text,
			Payload:  payload,
		}
		if d.ev != nil {
			d.ev.Message(msg)
		}
	}
	if payload.Group && len(p.groupCapcodes) > 0 && d.opts.ExpandGroupsEnabled() {
		for _, cc := range p.groupCapcodes {
			emit(cc)
		}
		return
	}
	emit(p.capcode)
}
