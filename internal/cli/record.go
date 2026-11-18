package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	"github.com/gopager/pagewire/internal/observe"
	"github.com/gopager/pagewire/internal/pipeline"
	"github.com/gopager/pagewire/internal/sdr"
	"github.com/gopager/pagewire/internal/source"
)

// RecordCmd captures raw IQ and/or demodulated channel audio to files for
// test fixtures, bug reports and verification with multimon-ng.
type RecordCmd struct {
	Source   string        `help:"Name of the source to record (default: the only one)."`
	Duration time.Duration `help:"Stop after this long (0 = until interrupted)." default:"30s"`
	Dir      string        `help:"Output directory." default:"." type:"existingdir"`
	IQ       bool          `help:"Write <source>.cu8 with the raw 8-bit IQ capture."`
	Audio    bool          `help:"Write <channel>.s16le with 22050 Hz demodulated audio per channel." default:"true"`
}

// Run implements the command.
func (c RecordCmd) Run(cctx *Context, root *CLI) error {
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
		return fmt.Errorf("source %s is an audio source; record only applies to SDR sources", src.Name)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if c.Duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Duration)
		defer cancel()
	}

	dev, err := source.OpenSDR(ctx, *src)
	if err != nil {
		return err
	}
	defer dev.Close()

	var closers []io.Closer
	defer func() {
		for _, cl := range closers {
			cl.Close()
		}
	}()
	open := func(name string) (*bufio.Writer, error) {
		f, err := os.Create(filepath.Join(c.Dir, name))
		if err != nil {
			return nil, err
		}
		w := bufio.NewWriterSize(f, 1<<20)
		closers = append(closers, flushCloser{w, f})
		return w, nil
	}

	streamDev := sdr.Device(dev)
	var tee *sdr.Tee
	if c.IQ {
		w, err := open(src.Name + ".cu8")
		if err != nil {
			return err
		}
		tee = &sdr.Tee{Device: dev, W: w}
		streamDev = tee
	}
	writers := map[string]io.Writer{}
	if c.Audio {
		for _, ch := range cfg.Channels {
			if ch.Source != src.Name {
				continue
			}
			w, err := open(ch.Name + ".s16le")
			if err != nil {
				return err
			}
			writers[ch.Name] = w
		}
	}
	rec := pipeline.NewRecorder(writers)
	ps, err := pipeline.NewSource(*src, cfg.Channels, streamDev, decoder.DefaultRate, rec, nil, log)
	if err != nil {
		return err
	}
	fmt.Fprintf(cctx.Stdout, "recording source %s (center %d Hz, %d S/s) for %s into %s\n", src.Name, ps.Plan.CenterHz, ps.Plan.SampleRate, c.Duration, c.Dir)
	if c.IQ {
		fmt.Fprintf(cctx.Stdout, "  %s.cu8: raw IQ, replay with: pagewire decode --iq %s.cu8 --rate %d --center %d\n", src.Name, src.Name, ps.Plan.SampleRate, ps.Plan.CenterHz)
	}
	for name := range writers {
		fmt.Fprintf(cctx.Stdout, "  %s.s16le: audio, verify with: multimon-ng -t raw -a POCSAG1200 %s.s16le\n", name, name)
	}
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return ps.Run(ctx) })
	if err := g.Wait(); err != nil {
		return err
	}
	if tee != nil && tee.Err != nil {
		return fmt.Errorf("writing IQ: %w", tee.Err)
	}
	if rec.Err != nil {
		return fmt.Errorf("writing audio: %w", rec.Err)
	}
	return nil
}

// pickSource returns the named source, or the only one when name is empty.
func pickSource(cfg *config.Config, name string) (*config.SourceConfig, error) {
	if name == "" {
		if len(cfg.Sources) != 1 {
			return nil, fmt.Errorf("config has %d sources; pass --source", len(cfg.Sources))
		}
		return &cfg.Sources[0], nil
	}
	for i := range cfg.Sources {
		if cfg.Sources[i].Name == name {
			return &cfg.Sources[i], nil
		}
	}
	return nil, fmt.Errorf("source %q is not configured", name)
}

type flushCloser struct {
	w *bufio.Writer
	f *os.File
}

// Close implements io.Closer: flush the writer, then close the file.
func (fc flushCloser) Close() error {
	if err := fc.w.Flush(); err != nil {
		fc.f.Close()
		return err
	}
	return fc.f.Close()
}
