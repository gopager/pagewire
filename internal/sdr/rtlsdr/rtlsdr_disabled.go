//go:build !cgo || !(linux || darwin || windows)

package rtlsdr

import (
	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/sdr"
)

// DriverName identifies this driver in sdr.Info and configuration.
const DriverName = "rtlsdr"

// Enumerate reports that the driver is unavailable in this build.
func Enumerate() ([]sdr.Info, error) { return nil, sdr.ErrUnsupported }

// Open reports that the driver is unavailable in this build.
func Open(index int) (sdr.Device, error) { return nil, sdr.ErrUnsupported }

// OpenSelector reports that the driver is unavailable in this build.
func OpenSelector(sel config.DeviceSelector) (sdr.Device, error) { return nil, sdr.ErrUnsupported }
