// Package source turns source configuration into opened sample sources.
package source

import (
	"context"
	"fmt"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/sdr"
	"github.com/gopager/pagewire/internal/sdr/rtlsdr"
	"github.com/gopager/pagewire/internal/sdr/rtltcp"
)

// OpenSDR opens the SDR device described by an rtlsdr or rtltcp source and
// applies gain, ppm and bias tee settings. Frequency and sample rate are set
// by the pipeline once the channel plan is known.
func OpenSDR(ctx context.Context, cfg config.SourceConfig) (sdr.Device, error) {
	switch cfg.Type {
	case config.SourceRTLSDR:
		sel, err := config.ParseDeviceSelector(cfg.RTLSDR.Device)
		if err != nil {
			return nil, err
		}
		dev, err := rtlsdr.OpenSelector(sel)
		if err != nil {
			return nil, fmt.Errorf("source %s: %w", cfg.Name, err)
		}
		if err := applyTuning(dev, cfg.RTLSDR.Gain, cfg.RTLSDR.PPM, cfg.RTLSDR.BiasTee); err != nil {
			dev.Close()
			return nil, fmt.Errorf("source %s: %w", cfg.Name, err)
		}
		if err := applyTunerExtras(dev, cfg.RTLSDR); err != nil {
			dev.Close()
			return nil, fmt.Errorf("source %s: %w", cfg.Name, err)
		}
		return dev, nil
	case config.SourceRTLTCP:
		dev, err := rtltcp.Dial(ctx, cfg.RTLTCP.Address)
		if err != nil {
			return nil, fmt.Errorf("source %s: %w", cfg.Name, err)
		}
		if err := applyTuning(dev, cfg.RTLTCP.Gain, cfg.RTLTCP.PPM, false); err != nil {
			dev.Close()
			return nil, fmt.Errorf("source %s: %w", cfg.Name, err)
		}
		return dev, nil
	default:
		return nil, fmt.Errorf("source %s: type %q is not an SDR source", cfg.Name, cfg.Type)
	}
}

func applyTuning(dev sdr.Device, gain config.Gain, ppm int, biasTee bool) error {
	if gain.Auto {
		if err := dev.SetAGC(); err != nil {
			return fmt.Errorf("enable AGC: %w", err)
		}
	} else if err := dev.SetGain(gain.TenthDB); err != nil {
		return fmt.Errorf("set gain: %w", err)
	}
	if err := dev.SetPPM(ppm); err != nil {
		return fmt.Errorf("set ppm: %w", err)
	}
	if biasTee {
		if err := dev.SetBiasTee(true); err != nil {
			return fmt.Errorf("enable bias tee: %w", err)
		}
	}
	return nil
}

// applyTunerExtras passes optional tuner settings on when configured. A
// tuner that lacks the setting is an error, because the operator asked for
// something the hardware cannot do.
func applyTunerExtras(dev sdr.Device, cfg config.RTLSDRConfig) error {
	if cfg.Bandwidth == 0 && len(cfg.IFGains) == 0 {
		return nil
	}
	extras, ok := dev.(sdr.TunerExtras)
	if !ok {
		return fmt.Errorf("%w: driver has no tuner extras", sdr.ErrUnsupported)
	}
	if cfg.Bandwidth != 0 {
		if err := extras.SetBandwidth(cfg.Bandwidth); err != nil {
			return fmt.Errorf("set bandwidth: %w", err)
		}
	}
	for i, g := range cfg.IFGains {
		if err := extras.SetIFGain(i+1, int(g*10+0.5)); err != nil {
			return fmt.Errorf("set if_gains[%d]: %w", i, err)
		}
	}
	return nil
}
