//go:build cgo && (linux || darwin || windows)

// Package libusb compiles the vendored libusb 1.0 sources into the binary so
// that no system libusb is needed at build or run time. It exports nothing to
// Go; packages that need libusb link against it by importing it for its side
// effects and including libusb.h from this directory.
package libusb

/*
#cgo CFLAGS: -I${SRCDIR} -I${SRCDIR}/os -fvisibility=hidden -Wno-unused-parameter
#cgo linux CFLAGS: -I${SRCDIR}/config/linux
#cgo darwin CFLAGS: -I${SRCDIR}/config/darwin
#cgo windows CFLAGS: -I${SRCDIR}/config/windows
#cgo linux LDFLAGS: -lpthread
#cgo darwin LDFLAGS: -framework IOKit -framework CoreFoundation -framework Security -lobjc
#cgo windows LDFLAGS: -lsetupapi -lole32 -ladvapi32
*/
import "C"
