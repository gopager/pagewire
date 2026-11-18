package dsp

import (
	"fmt"
	"math"
	"sort"
)

// RTL-SDR sample rate limits as enforced by librtlsdr.
const (
	rtlLowMin  = 225001
	rtlLowMax  = 300000
	rtlHighMin = 900001
	rtlHighMax = 3200000
)

// CapturePlan describes how one SDR covers a set of channel frequencies.
type CapturePlan struct {
	// SampleRate is the capture rate; it is ChannelRate times Decimation.
	SampleRate uint32
	// CenterHz is the tuner frequency.
	CenterHz uint32
	// Decimation is the integer factor from SampleRate to ChannelRate.
	Decimation int
	// ChannelRate is the complex sample rate delivered per channel.
	ChannelRate uint32
}

// usableFraction of the capture bandwidth that channels may occupy; the
// edges are left free for the tuner's own roll-off.
const usableFraction = 0.8

// dcGuardHz keeps channels away from the capture centre where RTL-SDR
// dongles have a DC spike.
const dcGuardHz = 25000

// PlanCapture picks the lowest valid RTL-SDR sample rate that is an integer
// multiple of channelRate and covers all frequencies, and a centre frequency
// that keeps every channel out of the DC guard. forcedRate, if non-zero,
// must itself be a valid multiple and is used instead.
func PlanCapture(freqs []uint32, channelRate, forcedRate uint32) (CapturePlan, error) {
	if len(freqs) == 0 {
		return CapturePlan{}, fmt.Errorf("dsp: no channel frequencies")
	}
	sorted := append([]uint32(nil), freqs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	lo, hi := sorted[0], sorted[len(sorted)-1]
	span := float64(hi - lo)

	candidates := []uint32{}
	if forcedRate != 0 {
		if forcedRate%channelRate != 0 {
			return CapturePlan{}, fmt.Errorf("dsp: sample rate %d is not a multiple of the channel rate %d", forcedRate, channelRate)
		}
		candidates = append(candidates, forcedRate)
	} else {
		for d := 1; ; d++ {
			r := channelRate * uint32(d)
			if r > rtlHighMax {
				break
			}
			if validRTLRate(r) {
				candidates = append(candidates, r)
			}
		}
	}
	for _, rate := range candidates {
		usable := float64(rate) * usableFraction
		if span+2*float64(dcGuardHz) > usable {
			continue
		}
		center, ok := chooseCenter(sorted, float64(rate))
		if !ok {
			continue
		}
		return CapturePlan{SampleRate: rate, CenterHz: center, Decimation: int(rate / channelRate), ChannelRate: channelRate}, nil
	}
	return CapturePlan{}, fmt.Errorf("dsp: channels spanning %.0f Hz do not fit into one capture (max %.0f Hz usable); use a second source", span, float64(rtlHighMax)*usableFraction-2*dcGuardHz)
}

func validRTLRate(r uint32) bool {
	return (r >= rtlLowMin && r <= rtlLowMax) || (r >= rtlHighMin && r <= rtlHighMax)
}

// chooseCenter places the centre at the middle of the span and nudges it if
// a channel would fall onto the DC spike, as long as every channel stays
// inside the usable band.
func chooseCenter(sorted []uint32, rate float64) (uint32, bool) {
	lo, hi := float64(sorted[0]), float64(sorted[len(sorted)-1])
	mid := (lo + hi) / 2
	half := rate * usableFraction / 2
	fits := func(c float64) bool {
		for _, f := range sorted {
			off := float64(f) - c
			if off < -half || off > half || (off > -dcGuardHz && off < dcGuardHz) {
				return false
			}
		}
		return true
	}
	for _, c := range []float64{mid, mid + 2*dcGuardHz, mid - 2*dcGuardHz, mid + 4*dcGuardHz, mid - 4*dcGuardHz, mid + 8*dcGuardHz, mid - 8*dcGuardHz} {
		if fits(c) {
			return uint32(c + 0.5), true
		}
	}
	return 0, false
}

// PlanFixed builds a plan for a device whose rate and centre cannot change,
// such as a recording. Channels must lie inside the usable band; the DC
// guard is not enforced because the capture already exists (a channel on
// the capture centre would sit on the DC spike, which a recording made by
// the gateway never does).
func PlanFixed(freqs []uint32, channelRate, rate, centerHz uint32) (CapturePlan, error) {
	if rate%channelRate != 0 {
		return CapturePlan{}, fmt.Errorf("dsp: capture rate %d is not a multiple of the channel rate %d", rate, channelRate)
	}
	half := float64(rate) * usableFraction / 2
	for _, f := range freqs {
		if off := math.Abs(float64(f) - float64(centerHz)); off > half {
			return CapturePlan{}, fmt.Errorf("dsp: channel %d Hz is %.0f Hz from the capture centre %d Hz, outside the usable ±%.0f Hz", f, off, centerHz, half)
		}
	}
	return CapturePlan{SampleRate: rate, CenterHz: centerHz, Decimation: int(rate / channelRate), ChannelRate: channelRate}, nil
}
