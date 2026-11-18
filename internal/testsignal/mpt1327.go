package testsignal

import "math"

// MPT 1327 per "A Signalling Standard for Trunked Private Land Mobile Radio
// Systems" (June 1997): 1200 bit/s FFSK with 1200 Hz for 1 and 1800 Hz for
// 0 (3.1), control channel codewords of 64 bits (48 information bits, 15
// check bits with the last one inverted, even parity; 3.2.3), preceded by a
// bit reversal preamble and the sync sequence 0xC4D7 (3.2.1).

const (
	mptSync      = 0xC4D7
	mptGenerator = 0x6815 // x^15+x^14+x^13+x^11+x^4+x^2+1 without the x^15 term
)

// MPT1327Field is a value of the given width in bits.
type MPT1327Field struct {
	Value uint64
	Bits  uint
}

// MPT1327Info concatenates fields, most significant first, into the 48
// information bits of a codeword (bit 1 of the standard is the MSB). The
// widths must add up to 48.
func MPT1327Info(fields ...MPT1327Field) uint64 {
	var info uint64
	var n uint
	for _, f := range fields {
		info = info<<f.Bits | f.Value&(1<<f.Bits-1)
		n += f.Bits
	}
	if n != 48 {
		panic("testsignal: MPT1327 information field is not 48 bits")
	}
	return info
}

func mptRemainder(info uint64) uint64 {
	var r uint64
	for i := 47; i >= 0; i-- {
		fb := (info>>uint(i) ^ r>>14) & 1
		r = (r << 1) & 0x7FFF
		if fb == 1 {
			r ^= mptGenerator
		}
	}
	return r
}

// MPT1327Codeword returns the 64-bit codeword for 48 information bits.
func MPT1327Codeword(info uint64) uint64 {
	cw := info<<15 | (mptRemainder(info) ^ 1)
	var parity uint64
	for v := cw; v != 0; v >>= 1 {
		parity ^= v & 1
	}
	return cw<<1 | parity
}

// MPT1327CCSC returns the information bits of the control channel system
// codeword for a 15-bit system identity (5.1.1). Its 16 completion bits are
// chosen so that the codeword ends in the sync sequence.
func MPT1327CCSC(sys uint16) uint64 {
	rotated := uint64(0xAAAA)<<32 | uint64(0xC4D4)<<16 | uint64(sys)<<1
	cw := MPT1327Codeword(rotated)
	if cw&1 == 0 {
		cw = MPT1327Codeword(rotated | 1)
	}
	ccs := (cw >> 1 & 0xFFFF) ^ 1
	return uint64(sys)<<32 | ccs<<16 | 0xAAAA
}

// MPT1327GoToChannel builds a GTC address codeword (5.4).
func MPT1327GoToChannel(pfix, ident1, d, channel, ident2, n uint64) uint64 {
	return MPT1327Info(
		MPT1327Field{1, 1}, MPT1327Field{pfix, 7}, MPT1327Field{ident1, 13}, MPT1327Field{0, 1},
		MPT1327Field{d, 1}, MPT1327Field{channel, 10}, MPT1327Field{ident2, 13}, MPT1327Field{n, 2})
}

// MPT1327Aloha builds an ALH-family codeword (5.5.1); function 0 is ALH.
func MPT1327Aloha(pfix, ident1, function, chan4, wt, m, n uint64) uint64 {
	return MPT1327Info(
		MPT1327Field{1, 1}, MPT1327Field{pfix, 7}, MPT1327Field{ident1, 13}, MPT1327Field{1, 1},
		MPT1327Field{0, 3}, MPT1327Field{0, 2}, MPT1327Field{function, 3},
		MPT1327Field{chan4, 4}, MPT1327Field{wt, 3}, MPT1327Field{0, 2}, MPT1327Field{m, 5}, MPT1327Field{n, 4})
}

// MPT1327Ack builds an ACK-family codeword (5.5.2); function 0 is ACK.
func MPT1327Ack(pfix, ident1, function, ident2, qual, n uint64) uint64 {
	return MPT1327Info(
		MPT1327Field{1, 1}, MPT1327Field{pfix, 7}, MPT1327Field{ident1, 13}, MPT1327Field{1, 1},
		MPT1327Field{0, 3}, MPT1327Field{1, 2}, MPT1327Field{function, 3},
		MPT1327Field{ident2, 13}, MPT1327Field{qual, 1}, MPT1327Field{n, 4})
}

// EncodeMPT1327 returns the bit stream of a control channel transmission:
// preamble and sync, then the given information words as codewords. As a
// real control channel does, it sends a system codeword for sys before every
// address codeword after the first and at the end, and a filler data
// codeword after an odd number of data codewords.
func EncodeMPT1327(infos []uint64, sys uint16) []byte {
	var bits []byte
	put := func(v uint64, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, byte(v>>uint(i)&1))
		}
	}
	endMessage := func(dataWords int) {
		if dataWords%2 == 1 {
			put(MPT1327Codeword(0), 64)
		}
		put(MPT1327Codeword(MPT1327CCSC(sys)), 64)
	}
	put(0xAAAAAAAA, 32)
	put(mptSync, 16)
	dataWords := 0
	for i, info := range infos {
		if info>>47&1 == 1 {
			if i > 0 {
				endMessage(dataWords)
			}
			dataWords = 0
		} else {
			dataWords++
		}
		put(MPT1327Codeword(info), 64)
	}
	endMessage(dataWords)
	put(0xAAAA, 16)
	return bits
}

// MPT1327Audio renders bits as continuous-phase FFSK audio in [-1, 1] at
// sampleRate, with silence of the given lengths before and after.
func MPT1327Audio(bits []byte, sampleRate float64, leadInSeconds, tailSeconds float64) []float32 {
	lead := int(leadInSeconds * sampleRate)
	n := int(float64(len(bits))*sampleRate/1200) + lead + int(tailSeconds*sampleRate)
	out := make([]float32, n)
	phase := 0.0
	for i := lead; i < n; i++ {
		bit := int(float64(i-lead) * 1200 / sampleRate)
		if bit >= len(bits) {
			break
		}
		f := 1800.0
		if bits[bit] == 1 {
			f = 1200
		}
		phase += 2 * math.Pi * f / sampleRate
		out[i] = float32(0.8 * math.Sin(phase))
	}
	return out
}
