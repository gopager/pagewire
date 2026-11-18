package audio

import (
	"context"
	"fmt"
	"net"
)

// DialTCP connects to a host that streams raw PCM over TCP.
func DialTCP(ctx context.Context, address string, f Format, channels int, rate uint32) (Input, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("audio: %w", err)
	}
	return NewReaderInput(conn, conn, f, channels, rate), nil
}

// ListenUDP receives raw PCM datagrams on address. Datagram boundaries are
// ignored; the payload is treated as a continuous stream.
func ListenUDP(address string, f Format, channels int, rate uint32) (Input, error) {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, fmt.Errorf("audio: %w", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("audio: %w", err)
	}
	return &udpInput{conn: conn, format: f, channels: channels, rate: rate}, nil
}

type udpInput struct {
	conn     *net.UDPConn
	format   Format
	channels int
	rate     uint32
}

// SampleRate implements Input.
func (u *udpInput) SampleRate() uint32 { return u.rate }

// Stream implements Input.
func (u *udpInput) Stream(ctx context.Context, blockLen int, fn BlockFunc) error {
	frame := u.format.bytesPerSample() * u.channels
	stop := context.AfterFunc(ctx, func() { u.conn.Close() })
	defer stop()
	pkt := make([]byte, 65536)
	pending := make([]byte, 0, blockLen*frame+65536)
	out := make([]float32, blockLen)
	for {
		n, _, err := u.conn.ReadFromUDP(pkt)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("audio: udp: %w", err)
		}
		pending = append(pending, pkt[:n]...)
		for len(pending) >= blockLen*frame {
			decode(out, pending[:blockLen*frame], u.format, u.channels)
			fn(out)
			pending = append(pending[:0], pending[blockLen*frame:]...)
		}
	}
}

// Close implements Input.
func (u *udpInput) Close() error { return u.conn.Close() }
