//go:build cgo

package mmng

/*
#include "pagewire_hooks.h"
#include "pagewire_selcall.h"
*/
import "C"

import (
	"fmt"
	"runtime/cgo"
	"time"
	"unsafe"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	"github.com/gopager/pagewire/internal/message"
)

// defaultSequenceTimeout is the DTMF inter-key timeout when none is configured.
const defaultSequenceTimeout = time.Second

// selcallTable holds the nominal tone frequencies in Hz of a sequential
// tone system, indexed by tone number. The values are those of the
// multimon-ng demodulator tables; they describe the tone indices, the
// decoder itself does not report frequencies for these variants.
type selcallTable [16]float64

var selcallVariants = map[string]struct {
	name  string // multimon-ng demodulator name
	table *selcallTable
}{
	"zvei1":    {"ZVEI1", &selcallTable{2400, 1060, 1160, 1270, 1400, 1530, 1670, 1830, 2000, 2200, 2800, 810, 970, 885, 2600, 680}},
	"zvei2":    {"ZVEI2", &selcallTable{2400, 1060, 1160, 1270, 1400, 1530, 1670, 1830, 2000, 2200, 885, 825, 740, 680, 970, 2600}},
	"zvei3":    {"ZVEI3", &selcallTable{2400, 1060, 1160, 1270, 1400, 1530, 1670, 1830, 2000, 2200, 885, 810, 2800, 680, 970, 2600}},
	"dzvei":    {"DZVEI", &selcallTable{2200, 970, 1060, 1160, 1270, 1400, 1530, 1670, 1830, 2000, 825, 740, 2600, 885, 2400, 680}},
	"pzvei":    {"PZVEI", &selcallTable{2400, 1060, 1160, 1270, 1400, 1530, 1670, 1830, 2000, 2200, 970, 810, 2800, 885, 2400, 680}},
	"ccir":     {"CCIR", &selcallTable{1981, 1124, 1197, 1275, 1358, 1446, 1540, 1640, 1747, 1860, 2400, 930, 2247, 991, 2110, 1055}},
	"eea":      {"EEA", &selcallTable{1981, 1124, 1197, 1275, 1358, 1446, 1540, 1640, 1747, 1860, 1055, 930, 2400, 991, 2110, 2247}},
	"eia":      {"EIA", &selcallTable{600, 741, 882, 1023, 1164, 1305, 1446, 1587, 1728, 1869, 2151, 2433, 2010, 2292, 459, 1091}},
	"dtmf":     {"DTMF", nil},
	"dualtone": {"DUALTONE", nil},
}

func init() {
	for proto := range selcallVariants {
		decoder.Register(proto, newSelcall)
	}
}

// pendingSeq is a DTMF sequence that is still being assembled.
type pendingSeq struct {
	symbols []byte
	tones   []int
	freqs   [][]float64
	lastPos uint64 // audio position (samples) of the last key
}

// Selcall decodes the selective-call tone systems of multimon-ng: ZVEI1/2/3,
// DZVEI, PZVEI, CCIR, EEA, EIA, DTMF and DUALTONE.
//
// The sequential tone systems and DUALTONE arrive from the decoder as one
// record per sequence. multimon-ng reports DTMF per key, so the host
// assembles keys into one message per dialled sequence: a sequence ends when
// no further key follows within the configured timeout, which is measured in
// audio time (samples consumed), not wall-clock time, so that recordings and
// live input behave the same. Close and Reset deliver a sequence in progress.
type Selcall struct {
	c        *cdecoder
	protocol string
	variant  string
	table    *selcallTable
	ev       decoder.Events

	assemble       bool // DTMF: keys are joined into sequences
	timeoutSamples uint64
	fed            uint64 // samples handed to the decoder since the last reset
	pending        *pendingSeq
}

func newSelcall(cfg config.DecoderConfig, ev decoder.Events) (decoder.Decoder, error) {
	v, ok := selcallVariants[cfg.Protocol]
	if !ok {
		return nil, fmt.Errorf("mmng: %q is not a selective-call variant", cfg.Protocol)
	}
	timeout := defaultSequenceTimeout
	if cfg.Selcall != nil && cfg.Selcall.SequenceTimeout != 0 {
		timeout = cfg.Selcall.SequenceTimeout
	}
	if timeout < 0 {
		return nil, fmt.Errorf("mmng: selcall sequence_timeout must not be negative, got %v", timeout)
	}
	d := &Selcall{
		protocol: cfg.Protocol,
		variant:  v.name,
		table:    v.table,
		ev:       ev,
		assemble: v.name == "DTMF",
	}
	c, err := newCDecoder(v.name, d)
	if err != nil {
		return nil, err
	}
	d.c = c
	d.timeoutSamples = uint64(timeout.Seconds() * float64(c.sampleRate()))
	c.init()
	return d, nil
}

// Protocol implements decoder.Decoder.
func (d *Selcall) Protocol() string { return d.protocol }

// SampleRate implements decoder.Decoder.
func (d *Selcall) SampleRate() uint32 { return d.c.sampleRate() }

// Process implements decoder.Decoder.
func (d *Selcall) Process(samples []float32) {
	d.c.process(samples)
	d.fed += uint64(len(samples))
	if d.pending != nil && d.fed-d.pending.lastPos >= d.timeoutSamples {
		d.flush()
	}
}

// Reset implements decoder.Decoder. A sequence in progress is delivered.
func (d *Selcall) Reset() {
	d.c.reset()
	d.flush()
	d.fed = 0
}

// Close implements decoder.Decoder. A sequence in progress is delivered.
func (d *Selcall) Close() error {
	d.c.free()
	d.flush()
	return nil
}

// flush emits the DTMF sequence in progress, if any.
func (d *Selcall) flush() {
	p := d.pending
	if p == nil {
		return
	}
	d.pending = nil
	d.emit(string(p.symbols), p.tones, p.freqs)
}

// emit delivers one sequence exactly as decoded.
func (d *Selcall) emit(sequence string, tones []int, freqs [][]float64) {
	if d.ev == nil {
		return
	}
	d.ev.Message(&message.Message{
		Address:  sequence,
		Function: -1,
		Kind:     message.KindSelcall,
		Text:     sequence,
		Payload: &message.SelcallPayload{
			Variant:     d.variant,
			Sequence:    sequence,
			Tones:       tones,
			Frequencies: freqs,
		},
	})
}

// handle processes one record from the C decoder.
func (d *Selcall) handle(m *C.struct_pagewire_selcall_message) {
	n := int(m.count)
	symbols := C.GoStringN(&m.symbols[0], C.int(m.symbols_len))
	tones := make([]int, n)
	freqs := make([][]float64, n)
	for i := range tones {
		tones[i] = int(m.tones[i])
		switch {
		case d.table != nil:
			freqs[i] = []float64{d.table[tones[i]&15]}
		case m.freq_count == 2 && n == 1: // DTMF key: row and column
			freqs[i] = []float64{float64(m.freq_hz[0]), float64(m.freq_hz[1])}
		case int(m.freq_count) > i: // DUALTONE: one frequency per tone
			freqs[i] = []float64{float64(m.freq_hz[i])}
		}
	}
	if !d.assemble {
		d.emit(symbols, tones, freqs)
		return
	}
	pos := uint64(m.sample_pos)
	if d.pending != nil && pos-d.pending.lastPos >= d.timeoutSamples {
		d.flush()
	}
	if d.pending == nil {
		d.pending = &pendingSeq{}
	}
	p := d.pending
	p.symbols = append(p.symbols, symbols...)
	p.tones = append(p.tones, tones...)
	p.freqs = append(p.freqs, freqs...)
	p.lastPos = pos
}

// goMMNGSelcall receives selective-call tone sequence records from the C decoder.
//
//export goMMNGSelcall
func goMMNGSelcall(ctx unsafe.Pointer, m *C.struct_pagewire_selcall_message) {
	if d, ok := cgo.Handle(uintptr(ctx)).Value().(*Selcall); ok {
		d.handle(m)
	}
}
