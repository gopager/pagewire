package source

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gopager/pagewire/internal/sdr"
	"github.com/gopager/pagewire/internal/sdr/mock"
)

func TestSupervisorReopensAfterFailure(t *testing.T) {
	var opens, failsLeft atomic.Int32
	failsLeft.Store(2) // the first two devices "unplug" after a few blocks
	states := make(chan bool, 16)
	sup := &Supervisor{
		Name: "t",
		Open: func(context.Context) (sdr.Device, error) {
			n := opens.Add(1)
			if n == 2 {
				return nil, errors.New("not found yet") // simulate the dongle still enumerating
			}
			blocks := 0
			return mock.New(func(dst []byte, _ int64) bool {
				blocks++
				if failsLeft.Load() > 0 && blocks > 3 {
					failsLeft.Add(-1)
					return false // stream ends like a lost USB device
				}
				time.Sleep(2 * time.Millisecond)
				return true
			}), nil
		},
		Run: func(ctx context.Context, dev sdr.Device, alive *atomic.Int64) error {
			return dev.Stream(ctx, 4096, func([]byte) { alive.Store(time.Now().UnixNano()) })
		},
		MinBackoff: 5 * time.Millisecond, MaxBackoff: 20 * time.Millisecond,
		OnState: func(up bool) { states <- up },
		Log:     slog.Default(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { _ = sup.Serve(ctx); close(done) }()
	// Expect: up, down, down(open failed), up, down, up (third device keeps streaming).
	var seen []bool
	deadline := time.After(2 * time.Second)
collect:
	for {
		select {
		case st := <-states:
			seen = append(seen, st)
			if len(seen) >= 6 {
				break collect
			}
		case <-deadline:
			break collect
		}
	}
	cancel()
	<-done
	if len(seen) < 5 || !seen[0] || seen[1] || !seen[len(seen)-1] {
		t.Fatalf("state sequence %v", seen)
	}
	if sup.Restarts.Load() < 2 || opens.Load() < 4 {
		t.Errorf("restarts=%d opens=%d", sup.Restarts.Load(), opens.Load())
	}
}

func TestSupervisorDetectsStall(t *testing.T) {
	var opens atomic.Int32
	sup := &Supervisor{
		Name: "t",
		Open: func(context.Context) (sdr.Device, error) {
			opens.Add(1)
			return mock.New(func(dst []byte, _ int64) bool {
				time.Sleep(500 * time.Millisecond) // a device that stops delivering
				return true
			}), nil
		},
		Run: func(ctx context.Context, dev sdr.Device, alive *atomic.Int64) error {
			return dev.Stream(ctx, 4096, func([]byte) { alive.Store(time.Now().UnixNano()) })
		},
		StallTimeout: 100 * time.Millisecond, MinBackoff: 5 * time.Millisecond, MaxBackoff: 10 * time.Millisecond,
		Log: slog.Default(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	_ = sup.Serve(ctx)
	if sup.Restarts.Load() < 1 {
		t.Errorf("stall not detected: restarts=%d opens=%d", sup.Restarts.Load(), opens.Load())
	}
}
