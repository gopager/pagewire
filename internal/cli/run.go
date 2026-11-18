package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder"
	_ "github.com/gopager/pagewire/internal/decoder/mmng"
	"github.com/gopager/pagewire/internal/delivery"
	"github.com/gopager/pagewire/internal/message"
	"github.com/gopager/pagewire/internal/observe"
	"github.com/gopager/pagewire/internal/pipeline"
	"github.com/gopager/pagewire/internal/queue"
	"github.com/gopager/pagewire/internal/sdr"
	"github.com/gopager/pagewire/internal/sink"
	_ "github.com/gopager/pagewire/internal/sink/pagermon"
	_ "github.com/gopager/pagewire/internal/sink/pagermoningest"
	"github.com/gopager/pagewire/internal/source"
)

// RunCmd runs the gateway until interrupted.
type RunCmd struct{}

// Run implements the command: it opens all sources, decodes every channel,
// routes messages into the persistent queues and delivers them.
func (RunCmd) Run(cctx *Context, root *CLI) error {
	cfg, err := config.Load(root.Config)
	if err != nil {
		return err
	}
	log := observe.NewLogger(cfg.Observability.Log, cfg.Instance, cctx.Stderr)
	obs := observe.NewServer(cfg.Observability, cfg.Instance)
	metrics := observe.NewMetrics(obs.Registry())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hostname, _ := os.Hostname()
	env := sink.Env{Instance: cfg.Instance, Hostname: hostname}
	sinks := map[string]sink.Sink{}
	for _, t := range cfg.Targets {
		s, err := sink.New(t, env)
		if err != nil {
			return err
		}
		sinks[t.Name] = s
	}

	q, err := queue.Open(cfg.Queue.Path, cfg.Queue.MaxPerTarget)
	if err != nil {
		return fmt.Errorf("%w (another instance using the same queue file?)", err)
	}
	defer q.Close()

	handler := func(msg *message.Message) {
		n := 0
		for _, t := range cfg.Targets {
			if err := q.Enqueue(t.Name, msg); err != nil {
				log.Error("enqueue failed", "target", t.Name, "error", err)
				continue
			}
			n++
		}
		log.Info("message", "channel", msg.Channel.Name, "protocol", msg.Protocol, "address", msg.Address, "function", msg.Function, "kind", msg.Kind, "text", msg.Text, "snr_db", fmt.Sprintf("%.1f", msg.Quality.SNRdB), "bits_corrected", msg.Quality.BitsCorrected, "targets", n)
	}
	dsink, err := pipeline.NewDecoderSink(cfg.Channels, handler, metrics, log)
	if err != nil {
		return err
	}
	defer dsink.Close()

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return obs.Run(ctx) })
	for name, s := range sinks {
		w := delivery.New(name, q, s, cfg.Queue, metrics, log)
		g.Go(func() error { return w.Run(ctx) })
	}

	// Sources that end by themselves (recordings, pipes) finish the run:
	// once all of them are done and the queues are drained, the gateway
	// stops, so a recording can be replayed into a server as a batch.
	srcCtx, srcCancel := context.WithCancel(ctx)
	defer srcCancel()
	var sources errgroup.Group

	var closers []func() error
	defer func() {
		for _, c := range closers {
			if err := c(); err != nil {
				log.Warn("closing source", "error", err)
			}
		}
	}()
	for _, src := range cfg.Sources {
		switch src.Type {
		case config.SourceAudio:
			in, err := pipeline.OpenAudio(ctx, src.Audio, os.Stdin)
			if err != nil {
				return fmt.Errorf("source %s: %w", src.Name, err)
			}
			closers = append(closers, in.Close)
			as, err := pipeline.NewAudioSource(src, cfg.Channels, in, decoder.DefaultRate, dsink, log)
			if err != nil {
				return err
			}
			sources.Go(func() error { return as.Run(srcCtx) })
		default:
			// The first open must succeed so configuration errors surface at
			// start; afterwards the supervisor reopens the device after USB
			// disconnects or stalls while delivery keeps running.
			dev, err := source.OpenSDR(ctx, src)
			if err != nil {
				return err
			}
			if _, err := pipeline.NewSource(src, cfg.Channels, dev, decoder.DefaultRate, dsink, nil, log); err != nil {
				dev.Close()
				return err
			}
			dev.Close()
			sup := &source.Supervisor{
				Name:         src.Name,
				Open:         func(ctx context.Context) (sdr.Device, error) { return source.OpenSDR(ctx, src) },
				StallTimeout: 10 * time.Second,
				Log:          log,
				Run: func(ctx context.Context, dev sdr.Device, alive *atomic.Int64) error {
					ps, err := pipeline.NewSource(src, cfg.Channels, dev, decoder.DefaultRate, dsink, metrics, log)
					if err != nil {
						return err
					}
					ps.Alive = alive
					return ps.Run(ctx)
				},
			}
			up := metrics.SourceUp.WithLabelValues(src.Name)
			restarts := metrics.SourceRestarts.WithLabelValues(src.Name)
			var wasUp bool
			sup.OnState = func(isUp bool) {
				if isUp {
					up.Set(1)
				} else {
					up.Set(0)
					if wasUp {
						restarts.Inc()
					}
				}
				wasUp = isUp
				obs.SetReady(isUp, "source "+src.Name+" down")
			}
			sources.Go(func() error { return sup.Serve(srcCtx) })
		}
	}
	// The waiter must start after every source is registered, otherwise an
	// empty group would report completion at once.
	g.Go(func() error {
		if err := sources.Wait(); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		log.Info("all sources ended, draining queues")
		drain(ctx, q, cfg, cfg.Queue.DrainTimeout)
		return errAllSourcesEnded
	})
	obs.SetReady(true, "")
	log.Info("gateway started", "sources", len(cfg.Sources), "channels", len(cfg.Channels), "targets", len(cfg.Targets), "listen", cfg.Observability.Listen, "decoders", decoder.Available(), "sinks", sink.Types(), "queue", cfg.Queue.Path)
	err = g.Wait()
	if errors.Is(err, context.Canceled) || errors.Is(err, errAllSourcesEnded) {
		err = nil
	}
	log.Info("gateway stopped")
	return err
}

// errAllSourcesEnded stops the run group once finite sources are done.
var errAllSourcesEnded = errors.New("all sources ended")

// drain waits until every target queue is empty or the timeout passes.
func drain(ctx context.Context, q *queue.Queue, cfg *config.Config, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		pending := 0
		for _, t := range cfg.Targets {
			p, _ := q.Depth(t.Name)
			pending += p
		}
		if pending == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
