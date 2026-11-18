package dsp

import "math"

// NoiseFloor estimates the noise power of a channel from block powers. While
// the squelch reports no signal the estimate follows the block power with an
// exponential average; while a signal is present it only creeps upwards very
// slowly, so long transmissions do not lift it.
type NoiseFloor struct {
	floor float64
	rise  float64
	alpha float64
	init  bool
}

// NewNoiseFloor returns a tracker that averages noise-only blocks with the
// given smoothing factor (0 < alpha <= 1) and lets the estimate rise by
// risePerBlockDB decibels per block while a signal is present.
func NewNoiseFloor(alpha, risePerBlockDB float64) *NoiseFloor {
	return &NoiseFloor{alpha: alpha, rise: math.Pow(10, risePerBlockDB/10)}
}

// Observe feeds one block power and whether a signal was detected in it,
// returning the current floor estimate.
func (n *NoiseFloor) Observe(power float64, signal bool) float64 {
	switch {
	case !n.init:
		n.floor, n.init = power, true
	case !signal:
		n.floor += n.alpha * (power - n.floor)
	case power < n.floor:
		n.floor = power
	default:
		n.floor *= n.rise
		if n.floor > power {
			n.floor = power
		}
	}
	return n.floor
}

// Floor returns the current estimate.
func (n *NoiseFloor) Floor() float64 { return n.floor }

// Quieting measures how noisy a block of discriminator output is: the mean
// absolute difference between successive samples. Pure noise on an empty
// channel gives about 0.67 (phase differences are uniformly distributed),
// while a narrow-band FSK signal gives values well below 0.2. Unlike a
// power-based squelch it does not depend on a noise floor estimate and keeps
// working on channels that are keyed continuously.
func Quieting(audio []float32) float32 {
	if len(audio) < 2 {
		return 1
	}
	var acc float32
	prev := audio[0]
	for _, v := range audio[1:] {
		d := v - prev
		if d < 0 {
			d = -d
		}
		acc += d
		prev = v
	}
	return acc / float32(len(audio)-1)
}

// Gate decides whether a channel carries a signal, with hysteresis and a
// hold time so short fades inside a transmission do not interrupt decoding.
// It works on a score where larger means "more signal"; Update returns the
// state after observing one block.
type Gate struct {
	openAt     float64
	closeAt    float64
	holdBlocks int
	quiet      int
	open       bool
}

// NewGate opens when the score reaches openAt and closes when it stays
// below closeAt for holdBlocks consecutive blocks.
func NewGate(openAt, closeAt float64, holdBlocks int) *Gate {
	return &Gate{openAt: openAt, closeAt: closeAt, holdBlocks: holdBlocks}
}

// Update feeds one block score and returns whether the gate is open.
func (g *Gate) Update(score float64) bool {
	switch {
	case score >= g.openAt:
		g.open, g.quiet = true, 0
	case score < g.closeAt && g.open:
		g.quiet++
		if g.quiet >= g.holdBlocks {
			g.open = false
		}
	default:
		g.quiet = 0
	}
	return g.open
}

// Open reports the current state.
func (g *Gate) Open() bool { return g.open }
