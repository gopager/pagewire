package pipeline

import (
	"encoding/binary"
	"io"
	"sync"
)

// Recorder is an AudioSink that writes each channel's audio as signed 16-bit
// little-endian PCM to a writer, the format multimon-ng reads with -t raw.
type Recorder struct {
	mu      sync.Mutex
	writers map[string]io.Writer
	buf     []byte
	// Err holds the first write error.
	Err error
}

// NewRecorder records the channels named in writers; others are ignored.
func NewRecorder(writers map[string]io.Writer) *Recorder {
	return &Recorder{writers: writers}
}

// Audio implements AudioSink.
func (r *Recorder) Audio(ch *Channel, samples []float32) {
	w, ok := r.writers[ch.Name]
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if cap(r.buf) < 2*len(samples) {
		r.buf = make([]byte, 2*len(samples))
	}
	buf := r.buf[:2*len(samples)]
	for i, v := range samples {
		switch {
		case v > 1:
			v = 1
		case v < -1:
			v = -1
		}
		binary.LittleEndian.PutUint16(buf[2*i:], uint16(int16(v*32767)))
	}
	if r.Err == nil {
		if _, err := w.Write(buf); err != nil {
			r.Err = err
		}
	}
}
