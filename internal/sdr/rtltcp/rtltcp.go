// Package rtltcp implements a client for the rtl_tcp protocol so that a
// dongle attached to another host (or exposed into a VM) can be used as a
// sample source without cgo.
package rtltcp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/gopager/pagewire/internal/sdr"
)

// DriverName identifies this driver in sdr.Info and configuration.
const DriverName = "rtltcp"

// Protocol command codes as implemented by rtl_tcp.c.
const (
	cmdSetFreq       = 0x01
	cmdSetSampleRate = 0x02
	cmdSetGainMode   = 0x03
	cmdSetGain       = 0x04
	cmdSetFreqCorr   = 0x05
	cmdSetBiasTee    = 0x0e
)

// tunerGains lists the manual gain steps (tenths of dB) per tuner type as
// defined in librtlsdr, because rtl_tcp only transmits the number of steps.
var tunerGains = map[uint32][]int{
	1: {-10, 15, 40, 65, 90, 115, 140, 165, 190, 215, 240, 290, 340, 420},                                                                       // E4000
	2: {-99, -40, 71, 179, 192},                                                                                                                 // FC0012
	3: {-99, -73, -65, -63, -60, -58, -54, 58, 61, 63, 65, 67, 68, 70, 71, 179, 181, 182, 184, 186, 188, 191, 197},                              // FC0013
	4: {0},                                                                                                                                      // FC2580
	5: {0, 9, 14, 27, 37, 77, 87, 125, 144, 157, 166, 197, 207, 229, 254, 280, 297, 328, 338, 364, 372, 386, 402, 421, 434, 439, 445, 480, 496}, // R820T
	6: {0, 9, 14, 27, 37, 77, 87, 125, 144, 157, 166, 197, 207, 229, 254, 280, 297, 328, 338, 364, 372, 386, 402, 421, 434, 439, 445, 480, 496}, // R828D
}

var tunerNames = map[uint32]string{0: "unknown", 1: "E4000", 2: "FC0012", 3: "FC0013", 4: "FC2580", 5: "R820T", 6: "R828D"}

// Device is a connected rtl_tcp server.
type Device struct {
	conn   net.Conn
	info   sdr.Info
	gains  []int
	freq   uint32
	rate   uint32
	mu     sync.Mutex
	closed bool
}

// Dial connects to an rtl_tcp server and reads its greeting.
func Dial(ctx context.Context, address string) (*Device, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("rtltcp: %w", err)
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		// Commands are tiny and latency matters more than packet count.
		_ = tcp.SetNoDelay(true)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("rtltcp: %w", err)
	}
	var hdr [12]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		conn.Close()
		return nil, fmt.Errorf("rtltcp: reading greeting: %w", err)
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("rtltcp: %w", err)
	}
	if string(hdr[:4]) != "RTL0" {
		conn.Close()
		return nil, fmt.Errorf("rtltcp: %s is not an rtl_tcp server (magic %q)", address, hdr[:4])
	}
	tuner := binary.BigEndian.Uint32(hdr[4:8])
	gainCount := int(binary.BigEndian.Uint32(hdr[8:12]))
	gains := tunerGains[tuner]
	if len(gains) != gainCount && gainCount > 0 && len(gains) > gainCount {
		gains = gains[:gainCount]
	}
	return &Device{
		conn:  conn,
		gains: gains,
		info:  sdr.Info{Driver: DriverName, Name: "rtl_tcp " + address, Tuner: tunerNames[tuner]},
	}, nil
}

func (d *Device) send(cmd byte, param uint32) error {
	var buf [5]byte
	buf[0] = cmd
	binary.BigEndian.PutUint32(buf[1:], param)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return errors.New("rtltcp: connection closed")
	}
	_, err := d.conn.Write(buf[:])
	return err
}

// Info implements sdr.Device.
func (d *Device) Info() sdr.Info { return d.info }

// SetCenterFreq implements sdr.Device.
func (d *Device) SetCenterFreq(hz uint32) error {
	if err := d.send(cmdSetFreq, hz); err != nil {
		return err
	}
	d.freq = hz
	return nil
}

// CenterFreq implements sdr.Device. rtl_tcp does not report the actual
// value, so the last requested frequency is returned.
func (d *Device) CenterFreq() uint32 { return d.freq }

// SetSampleRate implements sdr.Device.
func (d *Device) SetSampleRate(hz uint32) error {
	if err := d.send(cmdSetSampleRate, hz); err != nil {
		return err
	}
	d.rate = hz
	return nil
}

// SampleRate implements sdr.Device.
func (d *Device) SampleRate() uint32 { return d.rate }

// SetPPM implements sdr.Device.
func (d *Device) SetPPM(ppm int) error { return d.send(cmdSetFreqCorr, uint32(int32(ppm))) }

// Gains implements sdr.Device.
func (d *Device) Gains() []int {
	out := append([]int(nil), d.gains...)
	sort.Ints(out)
	return out
}

// SetGain implements sdr.Device.
func (d *Device) SetGain(tenthDB int) error {
	if err := d.send(cmdSetGainMode, 1); err != nil {
		return err
	}
	best := tenthDB
	if len(d.gains) > 0 {
		best = d.gains[0]
		for _, g := range d.gains {
			if abs(g-tenthDB) < abs(best-tenthDB) {
				best = g
			}
		}
	}
	return d.send(cmdSetGain, uint32(int32(best)))
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// SetAGC implements sdr.Device (tuner AGC only, see the rtlsdr driver).
func (d *Device) SetAGC() error {
	return d.send(cmdSetGainMode, 0)
}

// SetBiasTee implements sdr.Device.
func (d *Device) SetBiasTee(on bool) error {
	v := uint32(0)
	if on {
		v = 1
	}
	return d.send(cmdSetBiasTee, v)
}

// Stream implements sdr.Device. It reads blockLen bytes at a time into a
// single reused buffer.
func (d *Device) Stream(ctx context.Context, blockLen int, fn sdr.BlockFunc) error {
	if blockLen <= 0 {
		return fmt.Errorf("rtltcp: invalid block length %d", blockLen)
	}
	buf := make([]byte, blockLen)
	// Cancelling the context unblocks the pending read through a deadline in
	// the past; the error it produces is ignored because Close follows.
	stop := context.AfterFunc(ctx, func() { _ = d.conn.SetReadDeadline(time.Now()) })
	defer stop()
	for {
		if _, err := io.ReadFull(d.conn, buf); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("rtltcp: stream: %w", err)
		}
		fn(buf)
	}
}

// Close implements sdr.Device.
func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	return d.conn.Close()
}
