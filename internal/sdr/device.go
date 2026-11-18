// Package sdr defines the hardware-independent interface to an IQ sample
// source such as an RTL-SDR dongle or an rtl_tcp server.
package sdr

import (
	"context"
	"errors"
)

// ErrUnsupported is returned by drivers that are compiled out on the current
// platform or build configuration.
var ErrUnsupported = errors.New("sdr: driver not available in this build")

// Info describes an attached device before or after it is opened.
type Info struct {
	// Driver is the name of the driver that owns the device, e.g. "rtlsdr".
	Driver string
	// Index is the driver-specific enumeration index.
	Index int
	// Name is the human-readable device name reported by the driver.
	Name string
	// Manufacturer, Product and Serial are the USB string descriptors.
	Manufacturer string
	Product      string
	Serial       string
	// Bus and Ports give the USB bus number and port chain, which identify
	// the physical socket and stay stable across reboots.
	Bus   int
	Ports []int
	// Tuner names the tuner chip once the device has been opened.
	Tuner string
}

// BlockFunc receives one block of interleaved unsigned 8-bit IQ samples
// (I0, Q0, I1, Q1, …). The slice is owned by the driver and is reused for
// the next block, so the callee must process or copy it before returning.
type BlockFunc func(iq []byte)

// Device is an opened IQ sample source. Implementations are not safe for
// concurrent use except where documented; the pipeline drives a device from
// a single goroutine and only cancels Stream through its context.
type Device interface {
	// Info returns the device description including the tuner type.
	Info() Info
	// SetCenterFreq tunes the device to the given frequency in Hz.
	SetCenterFreq(hz uint32) error
	// CenterFreq returns the frequency the device is actually tuned to.
	CenterFreq() uint32
	// SetSampleRate sets the IQ sample rate in samples per second.
	SetSampleRate(hz uint32) error
	// SampleRate returns the sample rate the device is actually using.
	SampleRate() uint32
	// SetPPM sets the frequency correction in parts per million.
	SetPPM(ppm int) error
	// Gains returns the supported manual gain steps in tenths of a dB,
	// in ascending order.
	Gains() []int
	// SetGain selects a manual tuner gain in tenths of a dB. The driver
	// picks the nearest supported step.
	SetGain(tenthDB int) error
	// SetAGC enables the tuner's automatic gain control.
	SetAGC() error
	// SetBiasTee switches the antenna bias voltage, where supported.
	SetBiasTee(on bool) error
	// Stream delivers IQ blocks of blockLen bytes to fn until ctx is done
	// or the device fails. It blocks for the duration of the stream and
	// returns nil after a clean cancellation.
	Stream(ctx context.Context, blockLen int, fn BlockFunc) error
	// Close releases the device.
	Close() error
}

// FixedTuning is implemented by devices whose sample rate and centre
// frequency are given, such as recordings; the pipeline plans around them
// instead of choosing its own.
type FixedTuning interface {
	FixedTuning() (sampleRate, centerHz uint32)
}

// TunerExtras is implemented by drivers whose tuners accept settings beyond
// the common gain: an explicit IF filter bandwidth and per-stage IF gains
// (E4000). Drivers report ErrUnsupported when the attached tuner has no
// such setting.
type TunerExtras interface {
	// SetBandwidth sets the tuner IF filter width in Hz; 0 selects the
	// automatic choice derived from the sample rate.
	SetBandwidth(hz uint32) error
	// SetIFGain sets the gain of IF stage (1-based) in tenths of a dB.
	SetIFGain(stage, tenthDB int) error
}
