package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// OpenFile opens a raw PCM or WAV file. WAV files are detected by their
// RIFF header and their format chunk overrides f, channels and rate.
func OpenFile(path string, f Format, channels int, rate uint32) (Input, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	var hdr [12]byte
	if _, err := io.ReadFull(fh, hdr[:]); err != nil {
		fh.Close()
		return nil, fmt.Errorf("audio: %s: %w", path, err)
	}
	if string(hdr[:4]) == "RIFF" && string(hdr[8:12]) == "WAVE" {
		wf, wch, wrate, err := readWAVHeader(fh)
		if err != nil {
			fh.Close()
			return nil, fmt.Errorf("audio: %s: %w", path, err)
		}
		return NewReaderInput(fh, fh, wf, wch, wrate), nil
	}
	if _, err := fh.Seek(0, io.SeekStart); err != nil {
		fh.Close()
		return nil, err
	}
	if rate == 0 {
		fh.Close()
		return nil, fmt.Errorf("audio: %s: raw file needs a sample rate", path)
	}
	return NewReaderInput(fh, fh, f, channels, rate), nil
}

// readWAVHeader parses the chunks after the RIFF header up to and including
// the start of the data chunk, leaving r positioned at the first sample.
func readWAVHeader(r io.Reader) (Format, int, uint32, error) {
	var (
		format   Format
		channels int
		rate     uint32
		haveFmt  bool
	)
	for {
		var ch [8]byte
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			return 0, 0, 0, fmt.Errorf("wav: %w", err)
		}
		size := binary.LittleEndian.Uint32(ch[4:])
		switch string(ch[:4]) {
		case "fmt ":
			body := make([]byte, size)
			if _, err := io.ReadFull(r, body); err != nil {
				return 0, 0, 0, err
			}
			tag := binary.LittleEndian.Uint16(body[0:])
			channels = int(binary.LittleEndian.Uint16(body[2:]))
			rate = binary.LittleEndian.Uint32(body[4:])
			bits := binary.LittleEndian.Uint16(body[14:])
			switch {
			case tag == 1 && bits == 16:
				format = FormatS16LE
			case tag == 3 && bits == 32:
				format = FormatF32LE
			default:
				return 0, 0, 0, fmt.Errorf("wav: unsupported format tag %d with %d bits", tag, bits)
			}
			haveFmt = true
		case "data":
			if !haveFmt {
				return 0, 0, 0, fmt.Errorf("wav: data chunk before fmt chunk")
			}
			return format, channels, rate, nil
		default:
			if _, err := io.CopyN(io.Discard, r, int64(size+size%2)); err != nil {
				return 0, 0, 0, err
			}
		}
	}
}
