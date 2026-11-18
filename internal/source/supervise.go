package source

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/gopager/pagewire/internal/sdr"
)

// Supervisor keeps an SDR source running across device failures: when the
// stream ends with an error, the device disappears from USB, or no data
// arrives for StallTimeout, the device is closed, reopened with back-off and
// the source rebuilt, while the rest of the gateway keeps running.
type Supervisor struct {
	// Name of the source, for logs.
	Name string
	// Open opens the device; it is called again after every failure.
	Open func(ctx context.Context) (sdr.Device, error)
	// Run builds and runs the pipeline on dev until ctx ends or dev fails.
	// It must return promptly when the device breaks.
	Run func(ctx context.Context, dev sdr.Device, alive *atomic.Int64) error
	// StallTimeout marks the device dead when alive is not updated for
	// this long (0 disables).
	StallTimeout time.Duration
	// MinBackoff and MaxBackoff bound the reopen delay.
	MinBackoff, MaxBackoff time.Duration
	// OnState is called with true when streaming and false when the source
	// is down; it may be nil.
	OnState func(up bool)
	// Restarts counts recoveries.
	Restarts atomic.Int64
	Log      *slog.Logger
}

// Serve runs the supervision loop until ctx ends. It returns nil on
// cancellation and never gives up on its own.
func (s *Supervisor) Serve(ctx context.Context) error {
	backoff := s.MinBackoff
	if backoff <= 0 {
		backoff = 2 * time.Second
	}
	maxBackoff := s.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = 30 * time.Second
	}
	delay := backoff
	for ctx.Err() == nil {
		dev, err := s.Open(ctx)
		if err != nil {
			s.setState(false)
			s.Log.Warn("source unavailable, retrying", "source", s.Name, "error", err, "retry_in", delay)
			if !sleep(ctx, delay) {
				return nil
			}
			delay = min(delay*2, maxBackoff)
			continue
		}
		var alive atomic.Int64
		alive.Store(time.Now().UnixNano())
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- s.Run(runCtx, dev, &alive) }()
		s.setState(true)
		if s.Restarts.Load() > 0 {
			s.Log.Info("source recovered", "source", s.Name)
		}
		delay = backoff
		err, finished := s.watch(runCtx, &alive, done)
		cancel()
		if !finished {
			// The run notices the cancelled context and returns; its error
			// was already handled by watch.
			if rerr := <-done; rerr != nil && !errors.Is(rerr, context.Canceled) {
				s.Log.Debug("source run ended", "source", s.Name, "error", rerr)
			}
		}
		if cerr := dev.Close(); cerr != nil {
			s.Log.Debug("closing failed device", "source", s.Name, "error", cerr)
		}
		if ctx.Err() != nil {
			return nil
		}
		s.setState(false)
		s.Restarts.Add(1)
		s.Log.Warn("source failed, reopening", "source", s.Name, "error", err, "retry_in", delay)
		if !sleep(ctx, delay) {
			return nil
		}
		delay = min(delay*2, maxBackoff)
	}
	return nil
}

// watch waits for the run to end or the stall timeout to pass. finished
// reports whether the run's result was consumed from done.
func (s *Supervisor) watch(ctx context.Context, alive *atomic.Int64, done <-chan error) (err error, finished bool) {
	var tick <-chan time.Time
	if s.StallTimeout > 0 {
		t := time.NewTicker(s.StallTimeout / 4)
		defer t.Stop()
		tick = t.C
	}
	for {
		select {
		case err := <-done:
			if err == nil {
				err = errors.New("stream ended")
			}
			return err, true
		case <-ctx.Done():
			return ctx.Err(), false
		case <-tick:
			if since := time.Since(time.Unix(0, alive.Load())); since > s.StallTimeout {
				return fmt.Errorf("no samples for %s", since.Round(time.Second)), false
			}
		}
	}
}

func (s *Supervisor) setState(up bool) {
	if s.OnState != nil {
		s.OnState(up)
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
