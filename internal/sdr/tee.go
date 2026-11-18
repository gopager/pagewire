package sdr

import (
	"context"
	"io"
)

// Tee wraps a Device and copies every streamed IQ block to w, e.g. to record
// a capture while it is being processed.
type Tee struct {
	Device
	W io.Writer
	// Err holds the first write error; streaming continues regardless.
	Err error
}

// Stream implements Device.
func (t *Tee) Stream(ctx context.Context, blockLen int, fn BlockFunc) error {
	return t.Device.Stream(ctx, blockLen, func(iq []byte) {
		if t.Err == nil {
			if _, err := t.W.Write(iq); err != nil {
				t.Err = err
			}
		}
		fn(iq)
	})
}
