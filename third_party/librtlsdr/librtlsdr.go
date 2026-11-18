//go:build cgo && (linux || darwin || windows)

// Package librtlsdr compiles the vendored rtl-sdr library sources into the
// binary. It exports nothing to Go; the Go binding lives in
// internal/sdr/rtlsdr and includes rtl-sdr.h from this directory.
package librtlsdr

/*
#cgo CFLAGS: -I${SRCDIR}/include -I${SRCDIR}/../libusb -Drtlsdr_STATIC -DDETACH_KERNEL_DRIVER=1 -fvisibility=hidden -Wno-unused-parameter
*/
import "C"

import _ "github.com/gopager/pagewire/third_party/libusb"
