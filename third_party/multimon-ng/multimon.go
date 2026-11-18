//go:build cgo

// Package multimon compiles the vendored multimon-ng decoder sources into
// the binary. It exports nothing to Go; the binding lives in
// internal/decoder/mmng and includes pagewire_hooks.h from this directory.
package multimon

/*
#cgo CFLAGS: -I${SRCDIR} -DMAX_VERBOSE_LEVEL=10 -DCHARSET_UTF8 -fvisibility=hidden -Wno-unused-parameter -Wno-unused-function
#cgo LDFLAGS: -lm
*/
import "C"
