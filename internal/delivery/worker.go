// Package delivery drains the per-target queues into their sinks with
// retries, exponential back-off and health reporting.
package delivery

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"time"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/observe"
	"github.com/gopager/pagewire/internal/queue"
	"github.com/gopager/pagewire/internal/sink"
)

// Worker delivers one target's queue.
type Worker struct {
	target  string
	queue   *queue.Queue
	sink    sink.Sink
	cfg     config.QueueConfig
	metrics *observe.Metrics
	log     *slog.Logger

	// minBackoff is the first retry delay; it doubles up to cfg.MaxBackoff.
	minBackoff time.Duration
	healthy    bool
}

// New creates a worker; it does not start it.
func New(target string, q *queue.Queue, s sink.Sink, cfg config.QueueConfig, m *observe.Metrics, log *slog.Logger) *Worker {
	return &Worker{target: target, queue: q, sink: s, cfg: cfg, metrics: m, log: log.With("target", target), minBackoff: 2 * time.Second, healthy: true}
}

// Run delivers until ctx is done. It sleeps on the queue's notification
// channel while nothing is pending.
func (w *Worker) Run(ctx context.Context) error {
	notify := w.queue.Notify(w.target)
	failures := 0
	w.replayDead("startup")
	w.updateGauges()
	for {
		e, err := w.queue.Peek(w.target)
		switch {
		case errors.Is(err, queue.ErrEmpty):
			select {
			case <-ctx.Done():
				return nil
			case <-notify:
			case <-time.After(5 * time.Second):
			}
			continue
		case err != nil:
			w.log.Error("queue read failed", "error", err)
			if !sleep(ctx, 5*time.Second) {
				return nil
			}
			continue
		}

		start := time.Now()
		dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		outcome, derr := w.sink.Deliver(dctx, e.Message)
		cancel()
		if w.metrics != nil {
			w.metrics.SinkLatency.WithLabelValues(w.target).Observe(time.Since(start).Seconds())
		}
		if derr == nil {
			if failures > 0 || !w.healthy {
				w.replayDead("target reachable again")
			}
			failures = 0
			w.setHealthy(true)
			w.count(string(outcome))
			if err := w.queue.Ack(w.target, e.Seq); err != nil {
				w.log.Error("ack failed", "error", err)
			}
			if outcome != sink.OutcomeDelivered {
				w.log.Info("message not delivered", "outcome", outcome, "address", e.Message.Address, "protocol", e.Message.Protocol)
			} else {
				w.log.Debug("message delivered", "address", e.Message.Address, "protocol", e.Message.Protocol, "attempts", e.Attempts+1)
			}
			w.updateGauges()
			continue
		}
		if ctx.Err() != nil {
			return nil
		}
		failures++
		w.setHealthy(false)
		w.count("error")
		class := sink.Classify(derr)
		// A server error can be the message's fault or the server's. If the
		// target does not even pass its health check, the server is down
		// and the message must not pay for it with an attempt.
		countAttempt := class == sink.ClassServer
		if countAttempt {
			cctx, ccancel := context.WithTimeout(ctx, 10*time.Second)
			if err := w.sink.Check(cctx); err != nil {
				countAttempt = false
				class = sink.Classify(err)
			}
			ccancel()
		}
		dead, err := w.queue.Fail(w.target, e.Seq, derr.Error(), string(class), countAttempt, w.cfg.MaxAttempts)
		if err != nil {
			w.log.Error("recording failure", "error", err)
		}
		switch {
		case dead:
			w.count("dead")
			w.log.Warn("message moved to dead letters", "address", e.Message.Address, "attempts", e.Attempts+1, "class", class, "error", derr)
		case failures == 1 || failures%10 == 0:
			w.log.Warn("delivery failed, will retry", "class", class, "error", derr, "attempt", e.Attempts+1, "consecutive_failures", failures)
		}
		w.updateGauges()
		if !sleep(ctx, w.backoff(failures)) {
			return nil
		}
	}
}

// replayDead moves dead letters back into the queue when auto replay is on,
// each at most max_replays times.
func (w *Worker) replayDead(reason string) {
	if !w.cfg.AutoReplayEnabled() {
		return
	}
	if _, dead := w.queue.Depth(w.target); dead == 0 {
		return
	}
	n, err := w.queue.Replay(w.target, "", w.cfg.MaxReplays)
	if err != nil {
		w.log.Error("replaying dead letters failed", "error", err)
		return
	}
	if n > 0 {
		w.log.Info("dead letters re-queued", "count", n, "reason", reason)
	}
}

// backoff returns the retry delay after n consecutive failures.
func (w *Worker) backoff(n int) time.Duration {
	d := time.Duration(float64(w.minBackoff) * math.Pow(2, float64(n-1)))
	if max := w.cfg.MaxBackoff; max > 0 && (d > max || d <= 0) {
		d = max
	}
	return d
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func (w *Worker) count(outcome string) {
	if w.metrics != nil {
		w.metrics.SinkDeliveries.WithLabelValues(w.target, outcome).Inc()
	}
}

func (w *Worker) setHealthy(h bool) {
	if h != w.healthy {
		if h {
			w.log.Info("target reachable again")
		}
		w.healthy = h
	}
	if w.metrics != nil {
		v := 0.0
		if h {
			v = 1
		}
		w.metrics.SinkHealthy.WithLabelValues(w.target).Set(v)
	}
}

func (w *Worker) updateGauges() {
	if w.metrics == nil {
		return
	}
	p, d := w.queue.Depth(w.target)
	w.metrics.QueueDepth.WithLabelValues(w.target).Set(float64(p))
	w.metrics.QueueDead.WithLabelValues(w.target).Set(float64(d))
}

// Healthy reports the state after the last attempt.
func (w *Worker) Healthy() bool { return w.healthy }
