package rtltcp

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

// fakeServer speaks just enough rtl_tcp to test the client: it sends the
// greeting, records commands and streams a counter pattern.
func fakeServer(t *testing.T, tuner uint32, payload []byte) (addr string, cmds <-chan [5]byte) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan [5]byte, 16)
	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		hdr := make([]byte, 12)
		copy(hdr, "RTL0")
		binary.BigEndian.PutUint32(hdr[4:], tuner)
		binary.BigEndian.PutUint32(hdr[8:], uint32(len(tunerGains[tuner])))
		if _, err := conn.Write(hdr); err != nil {
			return
		}
		if _, err := conn.Write(payload); err != nil {
			return
		}
		for {
			var c [5]byte
			if _, err := io.ReadFull(conn, c[:]); err != nil {
				return
			}
			ch <- c
		}
	}()
	return ln.Addr().String(), ch
}

func TestDialAndCommands(t *testing.T) {
	addr, cmds := fakeServer(t, 5, nil)
	dev, err := Dial(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	if dev.Info().Tuner != "R820T" || len(dev.Gains()) != 29 {
		t.Errorf("info = %+v gains=%d", dev.Info(), len(dev.Gains()))
	}
	if err := dev.SetCenterFreq(150000000); err != nil {
		t.Fatal(err)
	}
	c := <-cmds
	if c[0] != cmdSetFreq || binary.BigEndian.Uint32(c[1:]) != 150000000 {
		t.Errorf("freq cmd = %v", c)
	}
	if err := dev.SetGain(300); err != nil { // nearest step is 29.7 dB
		t.Fatal(err)
	}
	<-cmds // gain mode
	c = <-cmds
	if c[0] != cmdSetGain || binary.BigEndian.Uint32(c[1:]) != 297 {
		t.Errorf("gain cmd = %v", c)
	}
	if err := dev.SetPPM(-3); err != nil {
		t.Fatal(err)
	}
	c = <-cmds
	if c[0] != cmdSetFreqCorr || int32(binary.BigEndian.Uint32(c[1:])) != -3 {
		t.Errorf("ppm cmd = %v", c)
	}
}

func TestStream(t *testing.T) {
	payload := make([]byte, 4096)
	for i := range payload {
		payload[i] = byte(i)
	}
	addr, _ := fakeServer(t, 5, payload)
	dev, err := Dial(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	ctx, cancel := context.WithCancel(context.Background())
	var got []byte
	err = dev.Stream(ctx, 1024, func(iq []byte) {
		got = append(got, iq...)
		if len(got) == len(payload) {
			cancel()
		}
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	for i := range got {
		if got[i] != byte(i) {
			t.Fatalf("byte %d = %d", i, got[i])
		}
	}
}

func TestDialRejectsNonRTL(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	go func() {
		c, _ := ln.Accept()
		_, _ = c.Write([]byte("HTTP/1.0 200 OK\r\n\r\n"))
		time.Sleep(100 * time.Millisecond)
		c.Close()
	}()
	if _, err := Dial(context.Background(), ln.Addr().String()); err == nil {
		t.Fatal("expected error for non rtl_tcp server")
	}
}
