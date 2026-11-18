package testsignal

import "testing"

func TestCodewordBCH(t *testing.T) {
	// The sync and idle words are valid codewords; re-encoding their data
	// bits must reproduce them exactly.
	for _, w := range []uint32{pocsagSync, pocsagIdle} {
		if got := codeword(w >> 11); got != w {
			t.Errorf("codeword(%#x>>11) = %#x", w, got)
		}
	}
}

func TestEncodePOCSAGStructure(t *testing.T) {
	bits, err := EncodePOCSAG([]POCSAGMessage{{Address: 1234567, Function: 3, Text: "HELLO"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(bits) < pocsagPreamble+17*32 || (len(bits)-pocsagPreamble)%(17*32) != 0 {
		t.Fatalf("length %d is not preamble plus whole batches", len(bits))
	}
	for i := 0; i < pocsagPreamble; i++ {
		if bits[i] != byte((i+1)&1) {
			t.Fatalf("preamble bit %d = %d", i, bits[i])
		}
	}
	word := func(idx int) uint32 {
		var w uint32
		for _, b := range bits[pocsagPreamble+idx*32 : pocsagPreamble+(idx+1)*32] {
			w = w<<1 | uint32(b)
		}
		return w
	}
	if word(0) != pocsagSync {
		t.Errorf("first word %#x is not sync", word(0))
	}
	// Address 1234567 has frame 7: words 1..14 idle, word 15 address.
	for i := 1; i <= 14; i++ {
		if word(i) != pocsagIdle {
			t.Errorf("word %d = %#x, want idle", i, word(i))
		}
	}
	addr := word(15)
	if addr>>31 != 0 || (addr>>13)&0x3FFFF != 1234567>>3 || (addr>>11)&3 != 3 {
		t.Errorf("address word %#x does not carry RIC 1234567 function 3", addr)
	}
	if word(16)>>31 != 1 {
		t.Errorf("word 16 should be a message codeword")
	}
}
