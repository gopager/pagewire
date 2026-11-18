package calibrate

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/gopager/pagewire/internal/dsp"
	"github.com/gopager/pagewire/internal/sdr"
)

// GSM base stations transmit a frequency correction burst (FCCH) that is a
// pure tone exactly fcchOffsetHz above the carrier; carriers are accurate to
// 0.05 ppm, which makes them the classic reference for RTL-SDR calibration
// (kalibrate-rtl does the same).
const (
	gsmBitRate    = 1625000.0 / 6  // 270.833 kbit/s
	fcchOffsetHz  = gsmBitRate / 4 // 67708.33 Hz
	fcchBits      = 148
	gsmChannelHz  = 200000
	gsmCaptureHz  = 1024000
	gsmLOOffsetHz = -256000 // keep the carrier and the tone away from DC
)

// Band is a GSM downlink band.
type Band struct {
	Name    string
	FirstHz float64
	ARFCNs  int
	first   int
}

// Bands lists the downlink bands by region. Which ones carry live GSM
// depends on the country and the year (2G is being switched off in many
// places), so the scan is an optional precision reference; the pager
// channels themselves remain the universal method. PCS 1900 lies above the
// R820T tuner range and needs a tuner such as the E4000.
var Bands = map[string]Band{
	"gsm850":  {Name: "GSM 850 (Americas)", FirstHz: 869.2e6, ARFCNs: 124, first: 128},
	"gsm900":  {Name: "GSM 900", FirstHz: 935.2e6, ARFCNs: 124, first: 1},
	"egsm900": {Name: "E-GSM 900", FirstHz: 925.2e6, ARFCNs: 50, first: 975},
	"dcs1800": {Name: "DCS 1800", FirstHz: 1805.2e6, ARFCNs: 374, first: 512},
	"pcs1900": {Name: "PCS 1900 (Americas)", FirstHz: 1930.2e6, ARFCNs: 299, first: 512},
}

// BandNames lists the band keys in a stable order.
func BandNames() []string {
	return []string{"gsm850", "gsm900", "egsm900", "dcs1800", "pcs1900"}
}

// Channel is a GSM carrier found during the scan.
type Channel struct {
	ARFCN     int
	CarrierHz float64
	PowerDB   float64
	// OffsetHz is the measured carrier offset (received minus nominal);
	// Bursts says from how many FCCH bursts it was averaged.
	OffsetHz float64
	Bursts   int
	// PPMDelta is the correction to add to the configured ppm.
	PPMDelta float64
}

// ScanGSM looks for base stations in band and measures their offset. It
// first ranks all carriers by power, then runs the FCCH detector on the
// strongest ones until maxChannels have yielded a measurement.
func ScanGSM(ctx context.Context, dev sdr.Device, band Band, maxChannels int, log *slog.Logger) ([]Channel, error) {
	if err := dev.SetSampleRate(gsmCaptureHz); err != nil {
		return nil, err
	}
	// Power survey: one capture per 1 MHz covers five ARFCNs.
	type cand struct {
		arfcn int
		power float64
	}
	var cands []cand
	span := float64(band.ARFCNs) * gsmChannelHz
	for center := band.FirstHz + 400e3; center < band.FirstHz+span; center += 1e6 {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		iq, err := capture(ctx, dev, uint32(center+0.5), 64*1024)
		if err != nil {
			return nil, err
		}
		spec := powerSpectrum(iq, 1024)
		for k := -2; k <= 2; k++ {
			f := center + float64(k)*gsmChannelHz
			arfcn := band.first + int((f-band.FirstHz)/gsmChannelHz+0.5)
			if arfcn < band.first || arfcn >= band.first+band.ARFCNs {
				continue
			}
			p := bandPower(spec, gsmCaptureHz, float64(k)*gsmChannelHz, gsmChannelHz*0.8)
			cands = append(cands, cand{arfcn, dsp.DB(p)})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].power > cands[j].power })
	var out []Channel
	tried := 0
	for _, c := range cands {
		if len(out) >= maxChannels || tried >= 3*maxChannels || ctx.Err() != nil {
			break
		}
		tried++
		carrier := band.FirstHz + float64(c.arfcn-band.first)*gsmChannelHz
		iq, err := capture(ctx, dev, uint32(carrier+gsmLOOffsetHz+0.5), 1024*1024)
		if err != nil {
			return out, err
		}
		off, n := detectFCCH(iq, gsmCaptureHz, -gsmLOOffsetHz+fcchOffsetHz)
		ch := Channel{ARFCN: c.arfcn, CarrierHz: carrier, PowerDB: c.power}
		if n >= 3 {
			ch.OffsetHz, ch.Bursts = off, n
			ch.PPMDelta = -off / carrier * 1e6
			out = append(out, ch)
			log.Info("gsm carrier", "band", band.Name, "arfcn", c.arfcn, "carrier_hz", fmt.Sprintf("%.0f", carrier), "power_db", fmt.Sprintf("%.1f", c.power), "offset_hz", fmt.Sprintf("%.0f", off), "bursts", n)
		} else {
			log.Debug("no fcch", "arfcn", c.arfcn, "power_db", fmt.Sprintf("%.1f", c.power), "bursts", n)
		}
	}
	return out, nil
}

// capture tunes dev and collects n IQ samples after a settling period.
func capture(ctx context.Context, dev sdr.Device, centerHz uint32, n int) ([]complex64, error) {
	if err := dev.SetCenterFreq(centerHz); err != nil {
		return nil, err
	}
	out := make([]complex64, 0, n)
	skip := 2 * 16384 // let the PLL and the AGC settle
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := dev.Stream(cctx, 2*16384, func(raw []byte) {
		if skip > 0 {
			skip -= len(raw) / 2
			return
		}
		blk := make([]complex64, len(raw)/2)
		dsp.ConvertIQ(blk, raw)
		out = append(out, blk...)
		if len(out) >= n {
			cancel()
		}
	})
	if err != nil && cctx.Err() == nil {
		return nil, err
	}
	if len(out) < n/2 {
		return nil, fmt.Errorf("capture at %d Hz delivered %d of %d samples", centerHz, len(out), n)
	}
	return out, nil
}

// powerSpectrum averages |FFT|² over consecutive blocks of size n and
// returns it with DC in the middle.
func powerSpectrum(iq []complex64, n int) []float64 {
	spec := make([]float64, n)
	blocks := 0
	for i := 0; i+n <= len(iq); i += n {
		buf := make([]complex128, n)
		for k := 0; k < n; k++ {
			w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(k)/float64(n-1))
			buf[k] = complex128(iq[i+k]) * complex(w, 0)
		}
		dsp.FFT(buf)
		for k := 0; k < n; k++ {
			spec[(k+n/2)%n] += real(buf[k])*real(buf[k]) + imag(buf[k])*imag(buf[k])
		}
		blocks++
	}
	for k := range spec {
		spec[k] /= float64(blocks) * float64(n) * float64(n)
	}
	return spec
}

// bandPower sums the spectrum around offsetHz over widthHz.
func bandPower(spec []float64, fs float64, offsetHz, widthHz float64) float64 {
	n := len(spec)
	binHz := fs / float64(n)
	lo := int((offsetHz-widthHz/2)/binHz) + n/2
	hi := int((offsetHz+widthHz/2)/binHz) + n/2
	p := 0.0
	for k := lo; k <= hi; k++ {
		if k >= 0 && k < n {
			p += spec[k]
		}
	}
	return p
}

// detectFCCH finds pure-tone bursts of FCCH length near expectedHz (within
// ±maxDevHz) from the instantaneous frequency of the signal and returns the
// median tone frequency relative to expectedHz and the burst count.
func detectFCCH(iq []complex64, fs, expectedHz float64) (offsetHz float64, bursts int) {
	const (
		maxDevHz = 60000.0 // covers ±60 ppm at 1 GHz
		smooth   = 32
		toleranc = 2500.0 // frequency jitter allowed inside a burst
	)
	minLen := int(float64(fcchBits) / gsmBitRate * fs * 0.8)
	// Instantaneous frequency, smoothed.
	inst := make([]float64, len(iq))
	prev := iq[0]
	for i := 1; i < len(iq); i++ {
		z := iq[i]
		re := real(z)*real(prev) + imag(z)*imag(prev)
		im := imag(z)*real(prev) - real(z)*imag(prev)
		inst[i] = math.Atan2(float64(im), float64(re)) * fs / (2 * math.Pi)
		prev = z
	}
	var tones []float64
	run, sum := 0, 0.0
	for i := smooth; i < len(inst); i++ {
		// moving average over the last `smooth` samples
		var avg float64
		for k := i - smooth; k < i; k++ {
			avg += inst[k]
		}
		avg /= smooth
		inBand := math.Abs(avg-expectedHz) <= maxDevHz
		stable := run > 0 && math.Abs(avg-sum/float64(run)) <= toleranc
		switch {
		case inBand && (run == 0 || stable):
			run++
			sum += avg
		default:
			if run >= minLen {
				tones = append(tones, sum/float64(run)-expectedHz)
			}
			run, sum = 0, 0
		}
	}
	if run >= minLen {
		tones = append(tones, sum/float64(run)-expectedHz)
	}
	if len(tones) == 0 {
		return 0, 0
	}
	return median(tones), len(tones)
}
