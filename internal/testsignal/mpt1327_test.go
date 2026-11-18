package testsignal

import "testing"

func parity64(v uint64) uint64 {
	var p uint64
	for ; v != 0; v >>= 1 {
		p ^= v & 1
	}
	return p
}

func TestMPT1327CodewordStructure(t *testing.T) {
	cw := MPT1327Codeword(MPT1327GoToChannel(5, 1234, 1, 100, 4321, 2))
	if parity64(cw) != 0 {
		t.Errorf("codeword %#x has odd parity", cw)
	}
	if cw>>63 != 1 {
		t.Errorf("address codeword must start with bit 1 set: %#x", cw)
	}
	if got := (cw >> 1) & 0x7FFF; got != mptRemainder(cw>>16)^1 {
		t.Errorf("check bits %#x do not match remainder", got)
	}
}

func TestMPT1327CCSCEndsInSync(t *testing.T) {
	for _, sys := range []uint16{0x0001, 0x1234, 0x5A5A} {
		cw := MPT1327Codeword(MPT1327CCSC(sys))
		if got := cw & 0xFFFF; got != mptSync {
			t.Errorf("sys %#x: CCSC ends in %#x, want %#x", sys, got, mptSync)
		}
		if cw>>63 != 0 || cw>>16&0xFFFF != 0xAAAA {
			t.Errorf("sys %#x: not a system codeword layout: %#x", sys, cw)
		}
	}
}

func TestMPT1327InfoWidthChecked(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic for wrong width")
		}
	}()
	MPT1327Info(MPT1327Field{1, 1})
}
