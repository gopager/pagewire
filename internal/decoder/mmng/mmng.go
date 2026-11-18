//go:build cgo

// Package mmng binds the embedded multimon-ng decoders to the decoder
// interface. Each decoder instance owns one C decoder; callbacks from C
// arrive on the goroutine that called Process.
package mmng

/*
#cgo CFLAGS: -I${SRCDIR}/../../../third_party/multimon-ng
#include <stdint.h>
#include <stdlib.h>
#include "pagewire_hooks.h"

extern void goMMNGLog(void *ctx, int level, char *text);
extern void goMMNGPocsag(void *ctx, struct pagewire_pocsag_message *msg);
extern void goMMNGFlex(void *ctx, struct pagewire_flex_message *msg);
extern void goMMNGFms(void *ctx, struct pagewire_fms_message *msg);
extern void goMMNGSelcall(void *ctx, struct pagewire_selcall_message *msg);
extern void goMMNGEas(void *ctx, struct pagewire_eas_message *msg);
extern void goMMNGMpt1327(void *ctx, struct pagewire_mpt1327_message *msg);
extern void goMMNGGsc(void *ctx, struct pagewire_gsc_message *msg);

static void mmng_log_tramp(void *ctx, int level, const char *text) {
	goMMNGLog(ctx, level, (char *)text);
}
static void mmng_pocsag_tramp(void *ctx, const struct pagewire_pocsag_message *m) {
	goMMNGPocsag(ctx, (struct pagewire_pocsag_message *)m);
}
static void mmng_flex_tramp(void *ctx, const struct pagewire_flex_message *m) {
	goMMNGFlex(ctx, (struct pagewire_flex_message *)m);
}
static void mmng_fms_tramp(void *ctx, const struct pagewire_fms_message *m) { goMMNGFms(ctx, (struct pagewire_fms_message *)m); }
static void mmng_selcall_tramp(void *ctx, const struct pagewire_selcall_message *m) { goMMNGSelcall(ctx, (struct pagewire_selcall_message *)m); }
static void mmng_eas_tramp(void *ctx, const struct pagewire_eas_message *m) { goMMNGEas(ctx, (struct pagewire_eas_message *)m); }
static void mmng_mpt1327_tramp(void *ctx, const struct pagewire_mpt1327_message *m) { goMMNGMpt1327(ctx, (struct pagewire_mpt1327_message *)m); }
static void mmng_gsc_tramp(void *ctx, const struct pagewire_gsc_message *m) { goMMNGGsc(ctx, (struct pagewire_gsc_message *)m); }
static void mmng_install(struct pagewire_hooks *h, uintptr_t handle, int verbosity) {
	h->ctx = (void *)handle;
	h->verbosity = verbosity;
	h->log = mmng_log_tramp;
	h->pocsag_message = mmng_pocsag_tramp;
	h->flex_message = mmng_flex_tramp;
	h->fms_message = mmng_fms_tramp;
	h->selcall_message = mmng_selcall_tramp;
	h->eas_message = mmng_eas_tramp;
	h->mpt1327_message = mmng_mpt1327_tramp;
	h->gsc_message = mmng_gsc_tramp;
}
*/
import "C"

import (
	"log/slog"
	"runtime/cgo"
	"sync"
	"unsafe"

	_ "github.com/gopager/pagewire/third_party/multimon-ng"
)

var bchOnce sync.Once

// Verbosity controls how much of multimon-ng's diagnostic output reaches the
// log (0 = none, 10 = everything). It applies to decoders created afterwards.
var Verbosity = 0

// logger receives multimon-ng diagnostics.
var logger = slog.Default()

// SetLogger routes multimon-ng diagnostics to l.
func SetLogger(l *slog.Logger) { logger = l }

// cdecoder wraps one C decoder instance.
type cdecoder struct {
	dec    *C.struct_pagewire_decoder
	handle cgo.Handle
	name   string
}

func newCDecoder(name string, owner any) (*cdecoder, error) {
	bchOnce.Do(func() { C.pagewire_bch_init() })
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	dec := C.pagewire_decoder_new(cname)
	if dec == nil {
		return nil, errUnknown(name)
	}
	c := &cdecoder{dec: dec, name: name, handle: cgo.NewHandle(owner)}
	C.mmng_install(C.pagewire_decoder_hooks(dec), C.uintptr_t(c.handle), C.int(Verbosity))
	return c, nil
}

func (c *cdecoder) sampleRate() uint32 { return uint32(C.pagewire_decoder_samplerate(c.dec)) }

func (c *cdecoder) init() { C.pagewire_decoder_init(c.dec) }

func (c *cdecoder) process(samples []float32) {
	if len(samples) == 0 {
		return
	}
	C.pagewire_decoder_process(c.dec, (*C.float)(unsafe.Pointer(&samples[0])), C.int(len(samples)))
}

func (c *cdecoder) reset() { C.pagewire_decoder_reset(c.dec) }

func (c *cdecoder) free() {
	if c.dec != nil {
		C.pagewire_decoder_free(c.dec)
		c.dec = nil
		c.handle.Delete()
	}
}

type errUnknown string

func (e errUnknown) Error() string { return "mmng: unknown multimon-ng decoder " + string(e) }

//export goMMNGLog
func goMMNGLog(ctx unsafe.Pointer, level C.int, text *C.char) {
	owner := cgo.Handle(uintptr(ctx)).Value()
	name := ""
	if n, ok := owner.(interface{ Protocol() string }); ok {
		name = n.Protocol()
	}
	logger.Debug("multimon-ng", "decoder", name, "level", int(level), "text", C.GoString(text))
}
