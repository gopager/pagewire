// Package testsignal generates synthetic pager transmissions for tests and
// fixtures: baseband bit streams from the protocol specifications and FM
// modulated IQ or audio from those bit streams. It is written from the
// public POCSAG specification (ITU-R M.584) and shares no code with
// multimon-ng, so decoders can be verified against an independent source.
package testsignal

import (
	"fmt"
	"strings"
)

// POCSAG constants per ITU-R M.584.
const (
	pocsagSync     = 0x7CD215D8
	pocsagIdle     = 0x7A89C197
	pocsagPreamble = 576 // bits of alternating 1/0 before the first batch
)

// POCSAGMessage is one page to encode.
type POCSAGMessage struct {
	Address  uint32 // 21-bit RIC
	Function int    // 0–3
	// Numeric selects the 4-bit numeric encoding; otherwise text is sent
	// as 7-bit alphanumeric. Empty text with either flag false is a tone
	// only page.
	Numeric bool
	Text    string
}

// bch3121 computes the 10 BCH check bits for the 21 data bits of a POCSAG
// codeword (generator x^10+x^9+x^8+x^6+x^5+x^3+1 = 0x769).
func bch3121(data uint32) uint32 {
	const gen = 0x769
	v := data << 10
	for i := 30; i >= 10; i-- {
		if v&(1<<i) != 0 {
			v ^= gen << (i - 10)
		}
	}
	return v & 0x3FF
}

// codeword builds a 32-bit POCSAG codeword from 21 data bits: BCH and even
// parity are appended.
func codeword(data uint32) uint32 {
	w := data<<11 | bch3121(data)<<1
	var parity uint32
	for v := w; v != 0; v >>= 1 {
		parity ^= v & 1
	}
	return w | parity
}

// Codeword exposes codeword generation for tests of decoders' BCH logic.
func Codeword(data21 uint32) uint32 { return codeword(data21) }

// EncodePOCSAG returns the bit stream (one bit per element, 0/1) of a
// transmission carrying the given pages: preamble, then batches of sync
// word plus eight frames of two codewords, padded with idle codewords.
func EncodePOCSAG(msgs []POCSAGMessage) ([]byte, error) {
	var words []uint32
	for _, m := range msgs {
		if m.Address >= 1<<21 {
			return nil, fmt.Errorf("address %d exceeds 21 bits", m.Address)
		}
		if m.Function < 0 || m.Function > 3 {
			return nil, fmt.Errorf("function %d out of range", m.Function)
		}
		// The frame (0–7) is the low three address bits; pad with idle
		// codewords until the address codeword lands in its frame.
		frame := int(m.Address & 7)
		for (len(words)/2)%8 != frame {
			words = append(words, pocsagIdle)
		}
		addr := (m.Address>>3)<<2 | uint32(m.Function)
		words = append(words, codeword(addr))
		var payload []byte
		switch {
		case m.Numeric:
			payload = numericBits(m.Text)
		case m.Text != "":
			payload = alphaBits(m.Text)
		}
		for len(payload) > 0 {
			chunk := make([]byte, 20)
			n := copy(chunk, payload)
			payload = payload[n:]
			// Alphanumeric padding bits are zero; a receiver sees any
			// complete padding character as NUL and strips it.
			if n < 20 && m.Numeric {
				// Numeric padding uses spaces: nibble 0xC sent least
				// significant bit first, i.e. 0 0 1 1 on air.
				for i := n; i < 20; i += 4 {
					chunk[i], chunk[i+1], chunk[i+2], chunk[i+3] = 0, 0, 1, 1
				}
			}
			var data uint32 = 1 << 20 // message codeword flag
			for i, b := range chunk {
				data |= uint32(b) << (19 - i)
			}
			words = append(words, codeword(data))
		}
	}
	// Pad to full batches (16 codewords each).
	for len(words)%16 != 0 {
		words = append(words, pocsagIdle)
	}
	bits := make([]byte, 0, pocsagPreamble+len(words)/16*17*32)
	for i := 0; i < pocsagPreamble; i++ {
		bits = append(bits, byte((i+1)&1))
	}
	for i := 0; i < len(words); i += 16 {
		bits = appendWord(bits, pocsagSync)
		for _, w := range words[i : i+16] {
			bits = appendWord(bits, w)
		}
	}
	return bits, nil
}

func appendWord(bits []byte, w uint32) []byte {
	for i := 31; i >= 0; i-- {
		bits = append(bits, byte(w>>i&1))
	}
	return bits
}

// alphaBits encodes text as 7-bit ASCII, least significant bit first.
func alphaBits(text string) []byte {
	var out []byte
	for _, c := range []byte(text) {
		for i := 0; i < 7; i++ {
			out = append(out, c>>i&1)
		}
	}
	return out
}

// numericBits encodes digits and the symbols U, space, hyphen, bracket as
// 4-bit nibbles, least significant bit first.
func numericBits(text string) []byte {
	const symbols = "0123456789*U -)("
	var out []byte
	for _, c := range strings.ToUpper(text) {
		v := strings.IndexRune(symbols, c)
		if v < 0 {
			v = 0xC // space
		}
		for i := 0; i < 4; i++ {
			out = append(out, byte(v>>i&1))
		}
	}
	return out
}
