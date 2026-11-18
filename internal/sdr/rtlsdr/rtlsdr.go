//go:build cgo && (linux || darwin || windows)

// Package rtlsdr drives RTL2832U based dongles through the vendored
// librtlsdr library.
package rtlsdr

/*
#cgo CFLAGS: -I${SRCDIR}/../../../third_party/librtlsdr/include -Drtlsdr_STATIC
#include <stdint.h>
#include <rtl-sdr.h>

extern void goRtlsdrBlock(unsigned char *buf, uint32_t len, void *ctx);

static void rtlsdr_block_trampoline(unsigned char *buf, uint32_t len, void *ctx) {
	goRtlsdrBlock(buf, len, ctx);
}

static int rtlsdr_read_async_go(rtlsdr_dev_t *dev, uintptr_t handle, uint32_t buf_num, uint32_t buf_len) {
	return rtlsdr_read_async(dev, rtlsdr_block_trampoline, (void *)handle, buf_num, buf_len);
}
*/
import "C"

import (
	"context"
	"fmt"
	"runtime/cgo"
	"slices"
	"sync"

	_ "github.com/gopager/pagewire/third_party/librtlsdr"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/sdr"
)

// DriverName identifies this driver in sdr.Info and configuration.
const DriverName = "rtlsdr"

// Error is a negative status code returned by librtlsdr (libusb error codes
// in most cases).
type Error struct {
	Op   string
	Code int
}

func (e *Error) Error() string {
	return fmt.Sprintf("rtlsdr: %s failed with code %d", e.Op, e.Code)
}

func check(op string, rc C.int) error {
	if rc < 0 {
		return &Error{Op: op, Code: int(rc)}
	}
	return nil
}

// Enumerate lists the attached RTL-SDR devices.
func Enumerate() ([]sdr.Info, error) {
	n := int(C.rtlsdr_get_device_count())
	infos := make([]sdr.Info, 0, n)
	for i := 0; i < n; i++ {
		info := sdr.Info{Driver: DriverName, Index: i, Name: C.GoString(C.rtlsdr_get_device_name(C.uint32_t(i)))}
		var mfg, prod, serial [256]C.char
		if rc := C.rtlsdr_get_device_usb_strings(C.uint32_t(i), &mfg[0], &prod[0], &serial[0]); rc == 0 {
			info.Manufacturer = C.GoString(&mfg[0])
			info.Product = C.GoString(&prod[0])
			info.Serial = C.GoString(&serial[0])
		}
		var bus C.uint8_t
		var ports [8]C.uint8_t
		if n := C.rtlsdr_get_device_usb_location(C.uint32_t(i), &bus, &ports[0], C.int(len(ports))); n >= 0 {
			info.Bus = int(bus)
			info.Ports = make([]int, n)
			for p := 0; p < int(n); p++ {
				info.Ports[p] = int(ports[p])
			}
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// Device is an opened RTL-SDR dongle.
type Device struct {
	dev  *C.rtlsdr_dev_t
	info sdr.Info

	mu        sync.Mutex
	streaming bool
}

// Open opens the device with the given enumeration index.
func Open(index int) (*Device, error) {
	infos, err := Enumerate()
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(infos) {
		return nil, fmt.Errorf("rtlsdr: device index %d out of range (%d devices)", index, len(infos))
	}
	var dev *C.rtlsdr_dev_t
	if err := check("open", C.rtlsdr_open(&dev, C.uint32_t(index))); err != nil {
		return nil, err
	}
	d := &Device{dev: dev, info: infos[index]}
	d.info.Tuner = tunerName(C.rtlsdr_get_tuner_type(dev))
	return d, nil
}

// OpenSelector opens the device matched by a configuration selector such as
// "serial:00000002" or "usb:1-1.3". An empty selector requires exactly one
// attached device.
func OpenSelector(sel config.DeviceSelector) (*Device, error) {
	infos, err := Enumerate()
	if err != nil {
		return nil, err
	}
	var matches []sdr.Info
	for _, info := range infos {
		switch sel.Kind {
		case config.SelectAny:
			matches = append(matches, info)
		case config.SelectIndex:
			if info.Index == sel.Index {
				matches = append(matches, info)
			}
		case config.SelectSerial:
			if info.Serial == sel.Serial {
				matches = append(matches, info)
			}
		case config.SelectUSBPath:
			if info.Bus == sel.Bus && slices.Equal(info.Ports, sel.Ports) {
				matches = append(matches, info)
			}
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("rtlsdr: no device matches selector (%d attached)", len(infos))
	case 1:
		return Open(matches[0].Index)
	default:
		if sel.Kind == config.SelectSerial {
			return nil, fmt.Errorf("rtlsdr: %d devices share serial %q; set unique serials or select by usb: path", len(matches), sel.Serial)
		}
		return nil, fmt.Errorf("rtlsdr: %d devices attached; select one with a serial: or usb: selector", len(matches))
	}
}

func tunerName(t C.enum_rtlsdr_tuner) string {
	switch t {
	case C.RTLSDR_TUNER_E4000:
		return "E4000"
	case C.RTLSDR_TUNER_FC0012:
		return "FC0012"
	case C.RTLSDR_TUNER_FC0013:
		return "FC0013"
	case C.RTLSDR_TUNER_FC2580:
		return "FC2580"
	case C.RTLSDR_TUNER_R820T:
		return "R820T"
	case C.RTLSDR_TUNER_R828D:
		return "R828D"
	default:
		return "unknown"
	}
}

// Info implements sdr.Device.
func (d *Device) Info() sdr.Info { return d.info }

// SetCenterFreq implements sdr.Device.
func (d *Device) SetCenterFreq(hz uint32) error {
	return check("set_center_freq", C.rtlsdr_set_center_freq(d.dev, C.uint32_t(hz)))
}

// CenterFreq implements sdr.Device.
func (d *Device) CenterFreq() uint32 { return uint32(C.rtlsdr_get_center_freq(d.dev)) }

// SetSampleRate implements sdr.Device.
func (d *Device) SetSampleRate(hz uint32) error {
	return check("set_sample_rate", C.rtlsdr_set_sample_rate(d.dev, C.uint32_t(hz)))
}

// SampleRate implements sdr.Device.
func (d *Device) SampleRate() uint32 { return uint32(C.rtlsdr_get_sample_rate(d.dev)) }

// SetPPM implements sdr.Device. librtlsdr reports -2 when the value is
// unchanged, which is not an error.
func (d *Device) SetPPM(ppm int) error {
	rc := C.rtlsdr_set_freq_correction(d.dev, C.int(ppm))
	if rc == -2 {
		return nil
	}
	return check("set_freq_correction", rc)
}

// Gains implements sdr.Device.
func (d *Device) Gains() []int {
	n := int(C.rtlsdr_get_tuner_gains(d.dev, nil))
	if n <= 0 {
		return nil
	}
	buf := make([]C.int, n)
	n = int(C.rtlsdr_get_tuner_gains(d.dev, &buf[0]))
	gains := make([]int, n)
	for i := range gains {
		gains[i] = int(buf[i])
	}
	return gains
}

// SetGain implements sdr.Device.
func (d *Device) SetGain(tenthDB int) error {
	if err := check("set_tuner_gain_mode", C.rtlsdr_set_tuner_gain_mode(d.dev, 1)); err != nil {
		return err
	}
	return check("set_tuner_gain", C.rtlsdr_set_tuner_gain(d.dev, C.int(nearestGain(d.Gains(), tenthDB))))
}

// nearestGain returns the supported gain step closest to want.
func nearestGain(gains []int, want int) int {
	if len(gains) == 0 {
		return want
	}
	best := gains[0]
	for _, g := range gains[1:] {
		if abs(g-want) < abs(best-want) {
			best = g
		}
	}
	return best
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// SetAGC implements sdr.Device. Only the tuner's own AGC is enabled; the
// RTL2832 digital AGC is left off because it pumps the noise floor and is
// not used by rtl_fm either.
func (d *Device) SetAGC() error {
	return check("set_tuner_gain_mode", C.rtlsdr_set_tuner_gain_mode(d.dev, 0))
}

// SetBiasTee implements sdr.Device.
func (d *Device) SetBiasTee(on bool) error {
	v := C.int(0)
	if on {
		v = 1
	}
	return check("set_bias_tee", C.rtlsdr_set_bias_tee(d.dev, v))
}

// SetBandwidth implements sdr.TunerExtras. librtlsdr returns -1 when the
// tuner has no bandwidth control (FC0012, FC0013).
func (d *Device) SetBandwidth(hz uint32) error {
	if rc := C.rtlsdr_set_tuner_bandwidth(d.dev, C.uint32_t(hz)); rc != 0 {
		return fmt.Errorf("%w: tuner %s has no bandwidth control", sdr.ErrUnsupported, d.info.Tuner)
	}
	return nil
}

// SetIFGain implements sdr.TunerExtras; only the E4000 has IF gain stages.
func (d *Device) SetIFGain(stage, tenthDB int) error {
	if d.info.Tuner != "E4000" {
		return fmt.Errorf("%w: tuner %s has no IF gain stages", sdr.ErrUnsupported, d.info.Tuner)
	}
	return check("set_tuner_if_gain", C.rtlsdr_set_tuner_if_gain(d.dev, C.int(stage), C.int(tenthDB)))
}

// streamState is what the C callback gets back through its cgo.Handle.
type streamState struct {
	fn sdr.BlockFunc
}

// Stream implements sdr.Device using librtlsdr's asynchronous reader. The
// callback runs on librtlsdr's USB thread; blockLen must be a multiple of
// 512 as required by the USB bulk transfers.
func (d *Device) Stream(ctx context.Context, blockLen int, fn sdr.BlockFunc) error {
	if blockLen <= 0 || blockLen%512 != 0 {
		return fmt.Errorf("rtlsdr: block length %d must be a positive multiple of 512", blockLen)
	}
	d.mu.Lock()
	if d.streaming {
		d.mu.Unlock()
		return fmt.Errorf("rtlsdr: device is already streaming")
	}
	d.streaming = true
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.streaming = false
		d.mu.Unlock()
	}()

	if err := check("reset_buffer", C.rtlsdr_reset_buffer(d.dev)); err != nil {
		return err
	}
	h := cgo.NewHandle(&streamState{fn: fn})
	defer h.Delete()

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			C.rtlsdr_cancel_async(d.dev)
		case <-done:
		}
	}()
	rc := C.rtlsdr_read_async_go(d.dev, C.uintptr_t(h), 0, C.uint32_t(blockLen))
	close(done)
	if ctx.Err() != nil {
		return nil
	}
	if rc != 0 {
		return &Error{Op: "read_async", Code: int(rc)}
	}
	return nil
}

// Close implements sdr.Device.
func (d *Device) Close() error {
	if d.dev == nil {
		return nil
	}
	err := check("close", C.rtlsdr_close(d.dev))
	d.dev = nil
	return err
}
