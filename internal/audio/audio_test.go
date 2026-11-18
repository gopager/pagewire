package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeS16Stereo(t *testing.T) {
	raw := make([]byte, 8)
	for i, v := range []int16{16384, -16384, 32767, 32767} {
		binary.LittleEndian.PutUint16(raw[2*i:], uint16(v))
	}
	out := make([]float32, 2)
	if n := decode(out, raw, FormatS16LE, 2); n != 2 {
		t.Fatalf("frames = %d", n)
	}
	if out[0] != 0 || math.Abs(float64(out[1])-0.99997) > 1e-4 {
		t.Errorf("decoded = %v", out)
	}
}

func TestReaderInputBlocksAndEOF(t *testing.T) {
	raw := make([]byte, 2*1000)
	for i := 0; i < 1000; i++ {
		binary.LittleEndian.PutUint16(raw[2*i:], uint16(int16(i)))
	}
	in := NewReaderInput(bytes.NewReader(raw), nil, FormatS16LE, 1, 8000)
	var total int
	err := in.Stream(context.Background(), 300, func(s []float32) { total += len(s) })
	if err != io.EOF || total != 1000 {
		t.Errorf("total=%d err=%v", total, err)
	}
}

func TestOpenWAV(t *testing.T) {
	// 16-bit mono 22050 Hz WAV with a LIST chunk before data.
	var b bytes.Buffer
	pcm := make([]byte, 2*10)
	for i := range pcm[:10] {
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(1000*i)))
	}
	fmtChunk := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtChunk[0:], 1)
	binary.LittleEndian.PutUint16(fmtChunk[2:], 1)
	binary.LittleEndian.PutUint32(fmtChunk[4:], 22050)
	binary.LittleEndian.PutUint32(fmtChunk[8:], 44100)
	binary.LittleEndian.PutUint16(fmtChunk[12:], 2)
	binary.LittleEndian.PutUint16(fmtChunk[14:], 16)
	le := func(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
	b.WriteString("RIFF")
	b.Write(le(0))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	b.Write(le(16))
	b.Write(fmtChunk)
	b.WriteString("LIST")
	b.Write(le(3))
	b.Write([]byte{1, 2, 3, 0}) // padded to even
	b.WriteString("data")
	b.Write(le(uint32(len(pcm))))
	b.Write(pcm)
	path := filepath.Join(t.TempDir(), "t.wav")
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	in, err := OpenFile(path, FormatF32LE, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.SampleRate() != 22050 {
		t.Errorf("rate = %d", in.SampleRate())
	}
	var got []float32
	if err := in.Stream(context.Background(), 4, func(s []float32) { got = append(got, s...) }); err != io.EOF {
		t.Fatal(err)
	}
	if len(got) != 10 || math.Abs(float64(got[3])-3000.0/32768) > 1e-6 {
		t.Errorf("samples = %v", got)
	}
}
