package testsignal

import (
	"fmt"
	"math"
)

// FMS constants per TR-BOS FMS: 1200 baud, "1" is a 1200 Hz tone, "0" a
// 1800 Hz tone (audio frequency shift keying on the FM channel). Every
// telegram is a bit sync of eleven 1 bits and the frame sync 0001 1010
// (the 19 bit pattern 0x7FF1A), followed by 48 bits sent least significant
// bit first per field. Written from the specification; shares no code with
// multimon-ng.
const (
	fmsSyncBits = 19
	fmsSync     = 0x7FF1A
	// fmsBaud and the tone frequencies are the FMS line parameters.
	fmsBaud  = 1200
	fmsTone1 = 1200
	fmsTone0 = 1800
	// fmsCRCPoly is x^7+x^6+x^2+1 without the leading term.
	fmsCRCPoly = 0x45
)

// FMSMessage is one telegram to encode; fields hold the values as they
// appear on the air (first transmitted bit = least significant bit).
type FMSMessage struct {
	Service   uint8  // 4 bits, BOS-Kennung
	State     uint8  // 4 bits, Landeskennung
	Location  uint8  // 8 bits, Ortskennung as transmitted
	Vehicle   uint16 // 16 bits, Fahrzeugkennung
	Status    uint8  // 4 bits
	Model     uint8  // 1 bit, Baustufe
	Direction uint8  // 1 bit, 0 FZG->LST, 1 LST->FZG
	ShortInfo uint8  // 2 bits, taktische Kurzinformation
}

// fmsCRC returns the 7-bit CRC of the 40 information bits (generator
// x^7+x^6+x^2+1, register initialised to zero, no final xor).
func fmsCRC(info []byte) uint8 {
	var reg uint8
	for _, b := range info {
		top := (reg >> 6) & 1
		reg = (reg << 1) & 0x7F
		if top^b == 1 {
			reg ^= fmsCRCPoly
		}
	}
	return reg
}

// putLSB appends the low n bits of v, least significant first.
func putLSB(bits []byte, v uint32, n int) []byte {
	for i := 0; i < n; i++ {
		bits = append(bits, byte(v>>i)&1)
	}
	return bits
}

// FMSTelegramBits returns the 48 telegram bits (without sync): 40
// information bits, the 7 CRC bits most significant first and a final 0
// bit that closes the block.
func FMSTelegramBits(m FMSMessage) ([]byte, error) {
	if m.Service > 15 || m.State > 15 || m.Status > 15 || m.Model > 1 || m.Direction > 1 || m.ShortInfo > 3 {
		return nil, fmt.Errorf("FMS field out of range: %+v", m)
	}
	var b []byte
	b = putLSB(b, uint32(m.Service), 4)
	b = putLSB(b, uint32(m.State), 4)
	b = putLSB(b, uint32(m.Location), 8)
	b = putLSB(b, uint32(m.Vehicle), 16)
	b = putLSB(b, uint32(m.Status), 4)
	b = putLSB(b, uint32(m.Model), 1)
	b = putLSB(b, uint32(m.Direction), 1)
	b = putLSB(b, uint32(m.ShortInfo), 2)
	crc := fmsCRC(b)
	for i := 6; i >= 0; i-- {
		b = append(b, (crc>>i)&1)
	}
	return append(b, 0), nil
}

// EncodeFMS returns the bit stream of the telegrams, each preceded by the
// sync pattern and followed by idle (alternating) bits.
func EncodeFMS(msgs []FMSMessage) ([]byte, error) {
	var out []byte
	for _, m := range msgs {
		t, err := FMSTelegramBits(m)
		if err != nil {
			return nil, err
		}
		out = append(out, FMSFrame(t)...)
	}
	return out, nil
}

// FMSFrame wraps 48 telegram bits with the sync pattern and idle bits; it
// lets tests corrupt a telegram before framing it.
func FMSFrame(telegram []byte) []byte {
	var out []byte
	for i := fmsSyncBits - 1; i >= 0; i-- {
		out = append(out, byte(uint32(fmsSync)>>i)&1)
	}
	out = append(out, telegram...)
	for i := 0; i < 24; i++ {
		out = append(out, byte(i&1))
	}
	return out
}

// FMSAudio renders bits as the continuous-phase AFSK audio an FM receiver
// outputs (amplitude 0.5, silence for the lead-in and tail), with optional
// uniform noise of the given peak level.
func FMSAudio(bits []byte, sampleRate float64, leadInSeconds, tailSeconds float64, noise float64, rnd func() float64) []float32 {
	lead := int(leadInSeconds * sampleRate)
	n := int(float64(len(bits))/fmsBaud*sampleRate) + lead + int(tailSeconds*sampleRate)
	out := make([]float32, n)
	phase := 0.0
	for i := range out {
		v := 0.0
		if bit := int(float64(i-lead) / sampleRate * fmsBaud); i >= lead && bit < len(bits) {
			f := float64(fmsTone0)
			if bits[bit] == 1 {
				f = fmsTone1
			}
			phase += 2 * math.Pi * f / sampleRate
			v = 0.5 * math.Sin(phase)
		}
		if noise > 0 && rnd != nil {
			v += (rnd()*2 - 1) * noise
		}
		out[i] = float32(v)
	}
	return out
}
