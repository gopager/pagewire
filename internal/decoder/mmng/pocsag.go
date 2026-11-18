//go:build cgo

package mmng

/*
#include <stdlib.h>
#include "pagewire_hooks.h"
*/
import "C"

import (
	"fmt"
	"runtime/cgo"
	"strconv"
	"strings"
	"unsafe"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	"github.com/gopager/pagewire/internal/message"
)

// pocsagVariants maps configuration protocol names to multimon-ng names.
var pocsagVariants = map[string]struct {
	name string
	baud int
}{
	"pocsag512":  {"POCSAG512", 512},
	"pocsag1200": {"POCSAG1200", 1200},
	"pocsag2400": {"POCSAG2400", 2400},
}

func init() {
	for proto := range pocsagVariants {
		decoder.Register(proto, newPOCSAG)
	}
}

// guessThreshold is multimon-ng's certainty threshold for format guessing.
const guessThreshold = 20

// POCSAG decodes POCSAG pages at one of the three standard baud rates.
type POCSAG struct {
	c        *cdecoder
	protocol string
	baud     int
	opts     config.POCSAGOptions
	ev       decoder.Events
	prev     [C.PAGEWIRE_POCSAG_STATS]uint32
}

func newPOCSAG(cfg config.DecoderConfig, ev decoder.Events) (decoder.Decoder, error) {
	v, ok := pocsagVariants[cfg.Protocol]
	if !ok {
		return nil, fmt.Errorf("mmng: %q is not a POCSAG variant", cfg.Protocol)
	}
	opts := config.POCSAGOptions{Mode: "alpha", Charset: "US", Polarity: "auto"}
	if cfg.POCSAG != nil {
		opts = *cfg.POCSAG
	}
	d := &POCSAG{protocol: cfg.Protocol, baud: v.baud, opts: opts, ev: ev}
	c, err := newCDecoder(v.name, d)
	if err != nil {
		return nil, err
	}
	d.c = c
	h := C.pagewire_decoder_hooks(c.dec)
	h.pocsag.error_correction = 2
	if opts.BCHLevel != nil {
		h.pocsag.error_correction = C.int(*opts.BCHLevel)
	}
	switch opts.Polarity {
	case "normal":
		h.pocsag.polarity = 1
	case "inverted":
		h.pocsag.polarity = 2
	default:
		h.pocsag.polarity = 0
	}
	c.init()
	cs := C.CString(strings.ToUpper(opts.Charset))
	rc := C.pagewire_pocsag_set_charset(c.dec, cs)
	C.free(unsafe.Pointer(cs))
	if rc != 0 {
		c.free()
		return nil, fmt.Errorf("mmng: unsupported POCSAG charset %q", opts.Charset)
	}
	return d, nil
}

// Protocol implements decoder.Decoder.
func (d *POCSAG) Protocol() string { return d.protocol }

// SampleRate implements decoder.Decoder.
func (d *POCSAG) SampleRate() uint32 { return d.c.sampleRate() }

// Process implements decoder.Decoder.
func (d *POCSAG) Process(samples []float32) {
	d.c.process(samples)
	d.emitStats()
}

// Reset implements decoder.Decoder.
func (d *POCSAG) Reset() {
	d.c.reset()
	d.prev = [C.PAGEWIRE_POCSAG_STATS]uint32{}
}

// Close implements decoder.Decoder.
func (d *POCSAG) Close() error {
	d.c.free()
	return nil
}

// emitStats turns counter deltas into quality events.
func (d *POCSAG) emitStats() {
	var cur [C.PAGEWIRE_POCSAG_STATS]C.uint32_t
	C.pagewire_pocsag_stats(d.c.dec, &cur[0])
	var now [C.PAGEWIRE_POCSAG_STATS]uint32
	for i := range cur {
		now[i] = uint32(cur[i])
	}
	if d.ev != nil {
		// While hunting for sync the decoder runs BCH on noise, which
		// produces meaningless corrections; only count them while synced.
		synced := now[8] == 1 || d.prev[8] == 1
		if bits := (now[2] - d.prev[2]) + 2*(now[3]-d.prev[3]); bits > 0 && synced {
			d.ev.Event(decoder.Event{Kind: decoder.EventBitsCorrected, Count: int(bits)})
		}
		if n := now[4] - d.prev[4]; n > 0 && synced {
			d.ev.Event(decoder.Event{Kind: decoder.EventUncorrectable, Count: int(n)})
		}
		switch {
		case now[8] == 1 && d.prev[8] == 0:
			d.ev.Event(decoder.Event{Kind: decoder.EventSyncAcquired, Count: 1})
		case now[8] == 0 && d.prev[8] == 1:
			d.ev.Event(decoder.Event{Kind: decoder.EventSyncLost, Count: 1})
		}
	}
	d.prev = now
}

// page is the Go view of one received page.
type page struct {
	address, function int32
	sync              bool
	nibbles           []byte
	numNibbles        int
	numeric, alpha    string
	skyper            string
	guessNum          int
	guessAlpha        int
	guessSkyper       int
}

//export goMMNGPocsag
func goMMNGPocsag(ctx unsafe.Pointer, m *C.struct_pagewire_pocsag_message) {
	d, ok := cgo.Handle(uintptr(ctx)).Value().(*POCSAG)
	if !ok {
		return
	}
	p := page{
		address:     int32(m.address),
		function:    int32(m.function),
		sync:        m.sync != 0,
		numNibbles:  int(m.numnibbles),
		numeric:     C.GoString(m.numeric),
		alpha:       C.GoStringN(m.alpha, C.int(m.alpha_len)),
		skyper:      C.GoStringN(m.skyper, C.int(m.skyper_len)),
		guessNum:    int(m.guess_numeric),
		guessAlpha:  int(m.guess_alpha),
		guessSkyper: int(m.guess_skyper),
	}
	if p.numNibbles > 0 {
		p.nibbles = C.GoBytes(unsafe.Pointer(m.nibbles), C.int((p.numNibbles+1)/2))
	}
	d.handlePage(p)
}

// handlePage applies the host-side output rules that multimon-ng used to
// apply in pocsag_printmessage and emits a message.
func (d *POCSAG) handlePage(p page) {
	partial := p.address == -2 || p.function == -2 || !p.sync
	if partial && !d.opts.ShowPartial {
		return
	}
	if p.numNibbles == 0 && d.opts.PruneEmpty {
		return
	}
	address := ""
	if p.address >= 0 {
		address = strconv.FormatInt(int64(p.address), 10)
	}
	function := -1
	if p.function >= 0 {
		function = int(p.function)
	}
	payload := &message.POCSAGPayload{Baud: d.baud, Partial: partial}
	if p.numNibbles > 0 {
		// The decoder keeps two nibbles per byte in transmission order; an
		// odd count leaves the low nibble of the last byte unused.
		payload.Raw = p.nibbles[:(p.numNibbles+1)/2]
		payload.RawBits = 4 * p.numNibbles
		if p.numNibbles%2 == 1 {
			payload.Raw[len(payload.Raw)-1] &= 0xf0
		}
	}
	msg := &message.Message{Address: address, Function: function, Kind: message.KindTone, Payload: payload}

	format := d.opts.Mode
	if p.numNibbles > 0 {
		unsure := p.guessNum < guessThreshold && p.guessAlpha < guessThreshold && p.guessSkyper < guessThreshold
		if d.opts.Heuristics && unsure && format == "auto" {
			return
		}
		// Renderings are passed on untouched, including the padding of the
		// last codeword (spaces in numeric, NUL characters in alpha) and
		// any control characters: the gateway does not interpret content.
		numeric, alpha, skyper := p.numeric, p.alpha, p.skyper
		payload.Numeric, payload.Alpha = numeric, alpha
		switch format {
		case "numeric":
			msg.Kind, msg.Text = message.KindNumeric, numeric
		case "alpha":
			msg.Kind, msg.Text = message.KindAlpha, alpha
		case "skyper":
			msg.Kind, msg.Text = message.KindAlpha, skyper
		case "standard":
			if function == 0 {
				msg.Kind, msg.Text = message.KindNumeric, numeric
			} else {
				msg.Kind, msg.Text = message.KindAlpha, alpha
			}
		default: // auto
			switch chooseAuto(numeric, alpha, skyper) {
			case "numeric":
				msg.Kind, msg.Text = message.KindNumeric, numeric
			case "skyper":
				msg.Kind, msg.Text = message.KindAlpha, skyper
			default:
				msg.Kind, msg.Text = message.KindAlpha, alpha
			}
		}
	}
	if d.ev != nil {
		d.ev.Message(msg)
	}
}

// chooseAuto picks the rendering with the smallest share of implausible
// characters. Text rendered as numeric shows up as the symbols U, [ and ],
// numeric data rendered as text shows up as control and special characters;
// ciphertext is implausible in both, where numeric (the lossless container)
// wins. multimon-ng prints several renderings in its auto mode; a message
// needs exactly one.
func chooseAuto(numeric, alpha, skyper string) string {
	nb := numericBadness(numeric)
	ab := alphaBadness(alpha)
	sb := alphaBadness(skyper)
	if sb < ab && ab > 0.1 {
		ab, alpha = sb, "skyper"
	} else {
		alpha = "alpha"
	}
	switch {
	case nb < ab:
		return "numeric"
	case ab < nb:
		return alpha
	case strings.Trim(numeric, "0123456789 ") == "":
		return "numeric"
	default:
		return alpha
	}
}

// numericBadness is the share of symbols that a numeric page would not
// normally carry.
func numericBadness(s string) float64 {
	s = strings.TrimRight(s, " ")
	if s == "" {
		return 1
	}
	bad := 0
	for _, r := range s {
		if r == 'U' || r == '[' || r == ']' {
			bad++
		}
	}
	return float64(bad) / float64(len([]rune(s)))
}

// alphaBadness is the share of control characters and special characters
// in an alphanumeric rendering (the trailing NUL padding is ignored).
func alphaBadness(s string) float64 {
	s = strings.TrimRight(s, "\x00")
	if s == "" {
		return 1
	}
	bad, n := 0, 0
	for _, r := range s {
		n++
		switch {
		case r < 0x20 && r != '\n' && r != '\r' && r != '\t', r == 0x7f:
			bad++
		case r < 0x80 && strings.ContainsRune("!\"#$%&'()*+,/:;<=>?@\\^_`{|}~", r):
			bad++
		}
	}
	return float64(bad) / float64(n)
}
