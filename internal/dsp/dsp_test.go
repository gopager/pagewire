package dsp

import (
	"math"
	"math/cmplx"
	"testing"
)

// tone fills dst with a complex exponential at freq for the given sample rate.
func tone(dst []complex64, freq, rate, amp float64, start int) {
	for i := range dst {
		ph := 2 * math.Pi * freq * float64(start+i) / rate
		dst[i] = complex64(cmplx.Rect(amp, ph))
	}
}

// goertzelPower measures the power of a single frequency in a block.
func goertzelPower(x []complex64, freq, rate float64) float64 {
	var acc complex128
	for i, z := range x {
		ph := -2 * math.Pi * freq * float64(i) / rate
		acc += complex128(z) * cmplx.Rect(1, ph)
	}
	acc /= complex(float64(len(x)), 0)
	return real(acc)*real(acc) + imag(acc)*imag(acc)
}

func TestConvertIQ(t *testing.T) {
	src := []byte{0, 255, 128, 127, 255, 0}
	dst := make([]complex64, 3)
	clipped := ConvertIQ(dst, src)
	if clipped != 4 {
		t.Errorf("clipped = %d, want 4", clipped)
	}
	if real(dst[0]) != -1 || imag(dst[0]) != 1 {
		t.Errorf("dst[0] = %v", dst[0])
	}
	if math.Abs(float64(real(dst[1]))) > 0.005 || math.Abs(float64(imag(dst[1]))) > 0.005 {
		t.Errorf("mid-scale should be near zero: %v", dst[1])
	}
	if p := Power(dst[:1]); math.Abs(p-2) > 1e-6 {
		t.Errorf("power of full-scale sample = %v, want 2", p)
	}
}

func TestNCOShiftsTone(t *testing.T) {
	const rate, f0, shift = 100000.0, 20000.0, 20000.0
	n := 8192
	src := make([]complex64, n)
	tone(src, f0, rate, 1, 0)
	nco := NewNCO(shift, rate)
	dst := make([]complex64, n)
	// Feed in two halves to exercise phase continuity.
	nco.Mix(dst[:n/2], src[:n/2])
	nco.Mix(dst[n/2:], src[n/2:])
	at0 := goertzelPower(dst, 0, rate)
	atF := goertzelPower(dst, f0, rate)
	if at0 < 0.99 || atF > 1e-4 {
		t.Errorf("after shift: power at 0 Hz = %.3f, at f0 = %.2e", at0, atF)
	}
}

func TestDecimationChainPassesInBandRejectsAlias(t *testing.T) {
	const in, out = 1014300, 22050
	chain, err := NewDecimationChain(in, out, 8000, 60, 65536)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("stages taps=%v", chain.Taps())
	gen := func(freq float64) []complex64 {
		var got []complex64
		pos := 0
		for pos < 10*in/out*2048 {
			blk := make([]complex64, 16384)
			tone(blk, freq, in, 1, pos)
			got = append(got, chain.Process(blk)...)
			pos += len(blk)
		}
		return got[len(got)/2:] // skip filter settling
	}
	// In-band tone at 5 kHz must survive with ~unity gain.
	if p := goertzelPower(gen(5000), 5000, out); p < 0.9 {
		t.Errorf("in-band gain = %.3f", p)
	}
	// A tone at out+5k aliases onto 5 kHz; it must be attenuated by >= 55 dB.
	chain, _ = NewDecimationChain(in, out, 8000, 60, 65536)
	if p := goertzelPower(gen(out+5000), 5000, out); DB(p) > -55 {
		t.Errorf("alias leakage = %.1f dB", DB(p))
	}
}

func TestDecimatorPhaseAcrossBlocks(t *testing.T) {
	// Odd block sizes must not change the output compared to one big block.
	taps := KaiserLowPass(0.1, 0.1, 40)
	src := make([]complex64, 1000)
	tone(src, 1000, 10000, 1, 0)
	whole := NewDecimator(taps, 3, 1000).Process(src)
	ref := append([]complex64(nil), whole...)
	d := NewDecimator(taps, 3, 1000)
	var pieces []complex64
	for i := 0; i < len(src); {
		n := 7 + i%13
		if i+n > len(src) {
			n = len(src) - i
		}
		pieces = append(pieces, d.Process(src[i:i+n])...)
		i += n
	}
	if len(pieces) != len(ref) {
		t.Fatalf("len = %d, want %d", len(pieces), len(ref))
	}
	for i := range ref {
		if cmplx.Abs(complex128(ref[i]-pieces[i])) > 1e-5 {
			t.Fatalf("sample %d differs: %v vs %v", i, ref[i], pieces[i])
		}
	}
}

func TestFMDemod(t *testing.T) {
	const rate = 22050.0
	n := 2048
	for _, dev := range []float64{1000, -4500, 11025 * 0.9} {
		src := make([]complex64, n)
		tone(src, dev, rate, 0.5, 0)
		for _, fast := range []bool{true, false} {
			d := &FMDemod{Fast: fast}
			dst := make([]float32, n)
			d.Demod(dst, src)
			want := dev / (rate / 2)
			for _, v := range dst[1:] {
				if math.Abs(float64(v)-want) > 0.005 {
					t.Fatalf("fast=%v dev=%v: got %v want %v", fast, dev, v, want)
				}
			}
		}
	}
}

func TestFastAtan2Error(t *testing.T) {
	maxErr := 0.0
	for i := 0; i < 100000; i++ {
		a := float64(i) / 100000 * 2 * math.Pi
		y, x := float32(math.Sin(a)), float32(math.Cos(a))
		e := math.Abs(float64(fastAtan2(y, x)) - math.Atan2(float64(y), float64(x)))
		if e > math.Pi {
			e = 2*math.Pi - e
		}
		maxErr = math.Max(maxErr, e)
	}
	if maxErr > 0.011 {
		t.Errorf("max atan2 error = %.4f rad", maxErr)
	}
}

func TestNoiseFloorAndGate(t *testing.T) {
	nf := NewNoiseFloor(0.1, 0.01)
	noise := 1e-4
	nf.Observe(1e-9, false) // filter still settling: far too low
	for i := 0; i < 100; i++ {
		nf.Observe(noise, false)
	}
	if f := nf.Floor(); math.Abs(DB(f)-DB(noise)) > 0.5 {
		t.Errorf("floor = %.1f dB, want %.1f", DB(f), DB(noise))
	}
	// A 20 dB burst flagged as signal must not lift the floor by more than
	// the creep rate allows.
	for i := 0; i < 20; i++ {
		nf.Observe(noise*100, true)
	}
	if DB(nf.Floor())-DB(noise) > 0.5 {
		t.Errorf("floor rose to %.1f dB during burst", DB(nf.Floor()))
	}

	g := NewGate(8, 4, 3)
	if g.Update(0) {
		t.Error("gate should start closed")
	}
	if !g.Update(10) {
		t.Error("gate should open at 10")
	}
	g.Update(0)
	g.Update(0)
	if !g.Open() {
		t.Error("gate closed before hold expired")
	}
	if g.Update(0) {
		t.Error("gate should close after hold")
	}
}

func TestQuieting(t *testing.T) {
	// Discriminator output of pure noise: uniform in [-1, 1].
	noise := make([]float32, 20000)
	seed := uint32(1)
	for i := range noise {
		seed = seed*1664525 + 1013904223
		noise[i] = float32(seed)/float32(math.MaxUint32)*2 - 1
	}
	if q := Quieting(noise); q < 0.6 || q > 0.75 {
		t.Errorf("quieting of noise = %.3f, want about 0.67", q)
	}
	// 1200 baud FSK at ±4.5 kHz deviation, 22050 Hz: square wave of ±0.41.
	fsk := make([]float32, 20000)
	for i := range fsk {
		if (i/18)%2 == 0 {
			fsk[i] = 0.41
		} else {
			fsk[i] = -0.41
		}
	}
	if q := Quieting(fsk); q > 0.1 {
		t.Errorf("quieting of FSK = %.3f, want < 0.1", q)
	}
}

func TestPlanCapture(t *testing.T) {
	// Single channel: lowest valid rate band (225–300 kS/s) and the channel
	// moved off the DC spike.
	p, err := PlanCapture([]uint32{150000000}, 22050, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.SampleRate != 22050*11 || p.Decimation != 11 {
		t.Errorf("single channel plan = %+v", p)
	}
	if off := math.Abs(float64(p.CenterHz) - 150000000); off < dcGuardHz || off > float64(p.SampleRate)*0.4 {
		t.Errorf("centre offset %.0f Hz leaves channel on DC or outside band", off)
	}
	// Two channels 700 kHz apart need the ~1 MS/s band.
	p, err = PlanCapture([]uint32{150000000, 150700000}, 22050, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.SampleRate < 900000 || p.SampleRate > 1100000 {
		t.Errorf("two channel plan = %+v", p)
	}
	// Channels 5 MHz apart cannot share one dongle.
	if _, err := PlanCapture([]uint32{150000000, 155000000}, 22050, 0); err == nil {
		t.Error("expected error for 5 MHz span")
	}
	// Forced rate must be a multiple.
	if _, err := PlanCapture([]uint32{150000000}, 22050, 1024000); err == nil {
		t.Error("expected error for non-multiple forced rate")
	}
	if p, err := PlanCapture([]uint32{150000000}, 22050, 1014300); err != nil || p.Decimation != 46 {
		t.Errorf("forced plan = %+v, %v", p, err)
	}
}

// BenchmarkChannel measures the per-channel cost at 1,014,300 S/s: mixer,
// decimation to 22050 and FM demodulation of one 16384-sample block.
func BenchmarkChannel(b *testing.B) {
	const in, out = 1014300, 22050
	raw := make([]byte, 32768)
	for i := range raw {
		raw[i] = byte(128 + 60*math.Sin(float64(i)/7))
	}
	iq := make([]complex64, 16384)
	mixed := make([]complex64, 16384)
	nco := NewNCO(150000, in)
	chain, _ := NewDecimationChain(in, out, 8000, 60, 16384)
	fm := NewFMDemod()
	audio := make([]float32, 16384/46+2)
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ConvertIQ(iq, raw)
		nco.Mix(mixed, iq)
		ch := chain.Process(mixed)
		fm.Demod(audio[:len(ch)], ch)
	}
}

func BenchmarkConvertIQ(b *testing.B) {
	raw := make([]byte, 32768)
	iq := make([]complex64, 16384)
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ConvertIQ(iq, raw)
	}
}

func TestResampler(t *testing.T) {
	const in, out, f = 48000.0, 22050.0, 1000.0
	r := NewResampler(uint32(in), uint32(out), 4800)
	var got []float32
	for pos := 0; pos < 48000; pos += 4800 {
		blk := make([]float32, 4800)
		for i := range blk {
			blk[i] = float32(math.Sin(2 * math.Pi * f * float64(pos+i) / in))
		}
		got = append(got, r.Process(blk)...)
	}
	if n := len(got); math.Abs(float64(n)-out) > 10 {
		t.Fatalf("output samples = %d, want about %.0f", n, out)
	}
	// The second half must be a clean 1 kHz tone at the output rate.
	seg := got[len(got)/2:]
	x := make([]complex64, len(seg))
	for i, v := range seg {
		x[i] = complex(v, 0)
	}
	p := goertzelPower(x, f, out)
	if p < 0.2 || p > 0.3 { // a real sine of amplitude 1 has 0.25 at +f
		t.Errorf("tone power after resampling = %.3f", p)
	}
	if p2 := goertzelPower(x, 3*f, out); DB(p2)-DB(p) > -50 {
		t.Errorf("harmonic distortion %.1f dB", DB(p2)-DB(p))
	}
}
