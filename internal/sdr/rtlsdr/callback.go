//go:build cgo && (linux || darwin || windows)

package rtlsdr

/*
#include <stdint.h>
*/
import "C"

import (
	"runtime/cgo"
	"unsafe"
)

// goRtlsdrBlock is invoked by librtlsdr for every completed USB transfer.
// The buffer is aliased without copying; librtlsdr reuses it after return.
//
//export goRtlsdrBlock
func goRtlsdrBlock(buf *C.uchar, n C.uint32_t, ctx unsafe.Pointer) {
	if n == 0 || buf == nil {
		return
	}
	st := cgo.Handle(uintptr(ctx)).Value().(*streamState)
	st.fn(unsafe.Slice((*byte)(unsafe.Pointer(buf)), int(n)))
}
