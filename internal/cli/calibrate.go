package cli

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/gopager/pagewire/internal/calibrate"
	"github.com/gopager/pagewire/internal/config"
	_ "github.com/gopager/pagewire/internal/decoder/mmng"
	"github.com/gopager/pagewire/internal/observe"
	"github.com/gopager/pagewire/internal/source"
)

// CalibrateCmd measures ppm and gain for an SDR source.
type CalibrateCmd struct {
	Source      string        `help:"Source to calibrate (default: the only one)."`
	PPM         bool          `help:"Measure the frequency correction."`
	Gain        bool          `help:"Sweep the tuner gain."`
	All         bool          `help:"Measure everything (default when nothing is selected)."`
	Duration    time.Duration `help:"Listening time per ppm measurement pass." default:"3m"`
	Passes      int           `help:"Measurement passes; the correction is applied between passes until the residual is small." default:"3"`
	StepTime    time.Duration `help:"Listening time per gain step." default:"20s"`
	ReferenceHz uint32        `help:"Use an unmodulated carrier at this frequency instead of the channels (ppm only)."`
	GSM         string        `help:"Measure ppm against GSM base stations in this band instead (gsm850, gsm900, egsm900, dcs1800, pcs1900); needs 2G service in reach." enum:",gsm850,gsm900,egsm900,dcs1800,pcs1900" default:""`
	GSMChannels int           `help:"How many GSM carriers to measure." default:"3"`
	MaxClipping float64       `help:"Reject gain steps with a higher share of clipped samples." default:"0.001"`
	Apply       bool          `help:"Write the results into the configuration file."`
}

// Run implements the command.
func (c CalibrateCmd) Run(cctx *Context, root *CLI) error {
	cfg, err := config.Load(root.Config)
	if err != nil {
		return err
	}
	log := observe.NewLogger(cfg.Observability.Log, cfg.Instance, cctx.Stderr)
	src, err := pickSource(cfg, c.Source)
	if err != nil {
		return err
	}
	if src.Type == config.SourceAudio {
		return fmt.Errorf("source %s is an audio source; nothing to calibrate", src.Name)
	}
	if !c.PPM && !c.Gain {
		c.All = true
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dev, err := source.OpenSDR(ctx, *src)
	if err != nil {
		return err
	}
	defer dev.Close()
	info := dev.Info()
	fmt.Fprintf(cctx.Stdout, "calibrating source %s (%s, tuner %s, serial %s)\n", src.Name, info.Driver, info.Tuner, info.Serial)

	var newPPM *int
	var newGain *config.Gain
	if c.All || c.PPM {
		switch {
		case c.GSM != "":
			band := calibrate.Bands[c.GSM]
			fmt.Fprintf(cctx.Stdout, "\nfrequency correction against %s base stations (current ppm %d)\n", band.Name, src.RTLSDRPPM())
			chans, err := calibrate.ScanGSM(ctx, dev, band, c.GSMChannels, log)
			if err != nil {
				return err
			}
			if len(chans) == 0 {
				fmt.Fprintln(cctx.Stdout, "  no GSM carriers with FCCH found in this band; try another band, the channel method, or --reference-hz")
			} else {
				tw := tabwriter.NewWriter(cctx.Stdout, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "  ARFCN\tCARRIER MHz\tPOWER dB\tFCCH BURSTS\tOFFSET Hz\tPPM DELTA")
				deltas := make([]float64, 0, len(chans))
				for _, g := range chans {
					fmt.Fprintf(tw, "  %d\t%.1f\t%.1f\t%d\t%+.0f\t%+.2f\n", g.ARFCN, g.CarrierHz/1e6, g.PowerDB, g.Bursts, g.OffsetHz, g.PPMDelta)
					deltas = append(deltas, g.PPMDelta)
				}
				tw.Flush()
				sort.Float64s(deltas)
				delta := deltas[len(deltas)/2]
				v := src.RTLSDRPPM() + int(math.Round(delta))
				newPPM = &v
				fmt.Fprintf(cctx.Stdout, "  median ppm delta %+.2f, recommended ppm: %d\n", delta, v)
			}
		default:
			fmt.Fprintf(cctx.Stdout, "\nfrequency correction: up to %d passes of %s", c.Passes, c.Duration)
			if c.ReferenceHz != 0 {
				fmt.Fprintf(cctx.Stdout, " on the reference carrier at %d Hz", c.ReferenceHz)
			} else {
				fmt.Fprint(cctx.Stdout, " on the configured channels")
			}
			fmt.Fprintf(cctx.Stdout, " (current ppm %d)\n", src.RTLSDRPPM())
			res, ppm, err := calibrate.MeasureOffsetIterative(ctx, *src, cfg.Channels, dev, src.RTLSDRPPM(), c.ReferenceHz, c.Duration, c.Passes, log)
			if err != nil {
				return err
			}
			if res != nil {
				tw := tabwriter.NewWriter(cctx.Stdout, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "  BURST\tCHANNEL\tAT\tBLOCKS\tOFFSET Hz\tSNR dB")
				for i, b := range res.Bursts {
					fmt.Fprintf(tw, "  %d\t%s\t%s\t%d\t%+.0f\t%.1f\n", i+1, b.Channel, b.Start.Round(time.Second), b.Blocks, b.OffsetHz, b.SNRdB)
				}
				tw.Flush()
			}
			switch {
			case res == nil || len(res.Bursts) == 0:
				fmt.Fprintln(cctx.Stdout, "  no transmissions seen in the last pass; extend --duration, use --reference-hz or --gsm")
			default:
				conf := "ok"
				if len(res.Bursts) < 3 {
					conf = "low confidence, few bursts"
				} else if math.Abs(res.MedianOffsetHz) > calibrate.MaxTrustedOffsetHz {
					conf = "residual still large, run again"
				}
				fmt.Fprintf(cctx.Stdout, "  last pass: %d bursts, median offset %+.0f Hz (spread ±%.0f Hz); recommended ppm: %d (%s)\n", len(res.Bursts), res.MedianOffsetHz, res.SpreadHz, ppm, conf)
				newPPM = &ppm
			}
		}
	}
	if c.All || c.Gain {
		gains := dev.Gains()
		if len(gains) == 0 {
			fmt.Fprintln(cctx.Stdout, "\ngain: the device reports no manual gain steps; keep auto")
		} else {
			fmt.Fprintf(cctx.Stdout, "\ngain sweep: %d steps, %s each\n", len(gains), c.StepTime)
			results, err := calibrate.SweepGain(ctx, *src, cfg.Channels, dev, gains, c.StepTime, log)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(cctx.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "  GAIN dB\tNOISE dBFS\tCLIPPING\tBURSTS\tSNR dB")
			for _, r := range results {
				snr := "-"
				if !math.IsNaN(r.SNRdB) {
					snr = fmt.Sprintf("%.1f", r.SNRdB)
				}
				fmt.Fprintf(tw, "  %.1f\t%.1f\t%.4f\t%d\t%s\n", float64(r.GainTenthDB)/10, r.NoiseDBFS, r.Clipping, r.Bursts, snr)
			}
			tw.Flush()
			best, why := calibrate.RecommendGain(results, c.MaxClipping)
			if why != "no measurements" {
				g := config.Gain{TenthDB: best.GainTenthDB}
				newGain = &g
				fmt.Fprintf(cctx.Stdout, "  recommended gain: %.1f dB (%s)\n", float64(best.GainTenthDB)/10, why)
			}
		}
	}

	fmt.Fprintln(cctx.Stdout, "\nconfiguration:")
	fmt.Fprintf(cctx.Stdout, "  sources[%s].rtlsdr:\n", src.Name)
	if newPPM != nil {
		fmt.Fprintf(cctx.Stdout, "    ppm: %d\n", *newPPM)
	}
	if newGain != nil {
		fmt.Fprintf(cctx.Stdout, "    gain: %s\n", newGain)
	}
	if !c.Apply || (newPPM == nil && newGain == nil) {
		if !c.Apply {
			fmt.Fprintln(cctx.Stdout, "  (run with --apply to write these values)")
		}
		return nil
	}
	if newPPM != nil {
		if err := config.SetSourceRTLSDR(root.Config, src.Name, "ppm", *newPPM); err != nil {
			return err
		}
	}
	if newGain != nil {
		if err := config.SetSourceRTLSDR(root.Config, src.Name, "gain", newGain.String()); err != nil {
			return err
		}
	}
	fmt.Fprintf(cctx.Stdout, "  written to %s\n", root.Config)
	return nil
}
