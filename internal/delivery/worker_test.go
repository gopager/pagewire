package delivery

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/message"
	"github.com/gopager/pagewire/internal/queue"
	"github.com/gopager/pagewire/internal/sink"
)

type fakeSink struct {
	mu       sync.Mutex
	fail     int
	class    sink.Class
	checkErr error
	got      []string
	attempts int
}

func (f *fakeSink) Name() string { return "t" }
func (f *fakeSink) Type() string { return "fake" }
func (f *fakeSink) Deliver(_ context.Context, m *message.Message) (sink.Outcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if f.fail > 0 {
		f.fail--
		class := f.class
		if class == "" {
			class = sink.ClassServer
		}
		return "", &sink.Error{Class: class, Err: errors.New("down")}
	}
	f.got = append(f.got, m.Address)
	return sink.OutcomeDelivered, nil
}
func (f *fakeSink) Check(context.Context) error { return f.checkErr }

func TestWorkerRetriesInOrder(t *testing.T) {
	q, err := queue.Open(filepath.Join(t.TempDir(), "q.db"), 100)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	fs := &fakeSink{fail: 2}
	w := New("t", q, fs, config.QueueConfig{MaxAttempts: 10, MaxBackoff: 50 * time.Millisecond}, nil, slog.Default())
	w.minBackoff = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	for _, a := range []string{"1", "2", "3"} {
		if err := q.Enqueue("t", &message.Message{Address: a}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		fs.mu.Lock()
		n := len(fs.got)
		fs.mu.Unlock()
		if n == 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if len(fs.got) != 3 || fs.got[0] != "1" || fs.got[2] != "3" {
		t.Errorf("delivered = %v", fs.got)
	}
	if fs.attempts != 5 {
		t.Errorf("attempts = %d, want 5 (2 failures + 3 deliveries)", fs.attempts)
	}
	if p, _ := q.Depth("t"); p != 0 {
		t.Errorf("queue not drained: %d", p)
	}
}

func TestWorkerDeadLetters(t *testing.T) {
	q, _ := queue.Open(filepath.Join(t.TempDir(), "q.db"), 100)
	defer q.Close()
	fs := &fakeSink{fail: 100}
	w := New("t", q, fs, config.QueueConfig{MaxAttempts: 2, MaxBackoff: 10 * time.Millisecond}, nil, slog.Default())
	w.minBackoff = 5 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := q.Enqueue("t", &message.Message{Address: "x"}); err != nil {
		t.Fatal(err)
	}
	go func() { _ = w.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, d := q.Depth("t"); d == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("message was not moved to dead letters")
}

func TestAutoReplayOnStartAndRecovery(t *testing.T) {
	q, _ := queue.Open(filepath.Join(t.TempDir(), "q.db"), 100)
	defer q.Close()
	// Two dead letters from an earlier life.
	for _, a := range []string{"d1", "d2"} {
		if err := q.Enqueue("t", &message.Message{Address: a}); err != nil {
			t.Fatal(err)
		}
		e, _ := q.Peek("t")
		if _, err := q.Fail("t", e.Seq, "old", "server", true, 1); err != nil {
			t.Fatal(err)
		}
	}
	if _, d := q.Depth("t"); d != 2 {
		t.Fatalf("setup: dead = %d", d)
	}
	fs := &fakeSink{}
	w := New("t", q, fs, config.QueueConfig{MaxAttempts: 10, MaxBackoff: 10 * time.Millisecond}, nil, slog.Default())
	w.minBackoff = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fs.mu.Lock()
		n := len(fs.got)
		fs.mu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if len(fs.got) != 2 {
		t.Errorf("dead letters were not replayed at startup: %v", fs.got)
	}
	off := false
	w2 := New("t", q, fs, config.QueueConfig{MaxAttempts: 10, MaxBackoff: 10 * time.Millisecond, AutoReplay: &off}, nil, slog.Default())
	w2.replayDead("test")
	if p, _ := q.Depth("t"); p != 0 {
		t.Errorf("auto_replay=false must not replay")
	}
}

func TestNetworkFailuresDoNotConsumeAttempts(t *testing.T) {
	q, _ := queue.Open(filepath.Join(t.TempDir(), "q.db"), 100)
	defer q.Close()
	fs := &fakeSink{fail: 5, class: sink.ClassNetwork}
	w := New("t", q, fs, config.QueueConfig{MaxAttempts: 2, MaxBackoff: 10 * time.Millisecond}, nil, slog.Default())
	w.minBackoff = 2 * time.Millisecond
	if err := q.Enqueue("t", &message.Message{Address: "n"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fs.mu.Lock()
		n := len(fs.got)
		fs.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if len(fs.got) != 1 {
		t.Fatalf("message should be delivered once the network is back, got %v", fs.got)
	}
	if _, d := q.Depth("t"); d != 0 {
		t.Errorf("network failures must not create dead letters, dead=%d", d)
	}
}

func TestServerErrorWithFailingCheckIsTargetLevel(t *testing.T) {
	q, _ := queue.Open(filepath.Join(t.TempDir(), "q.db"), 100)
	defer q.Close()
	fs := &fakeSink{fail: 4, class: sink.ClassServer, checkErr: &sink.Error{Class: sink.ClassServer, Err: errors.New("503")}}
	w := New("t", q, fs, config.QueueConfig{MaxAttempts: 2, MaxBackoff: 10 * time.Millisecond}, nil, slog.Default())
	w.minBackoff = 2 * time.Millisecond
	if err := q.Enqueue("t", &message.Message{Address: "s"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fs.mu.Lock()
		n := len(fs.got)
		fs.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if len(fs.got) != 1 {
		t.Fatalf("message should be delivered once the server is back, got %v", fs.got)
	}
	if _, d := q.Depth("t"); d != 0 {
		t.Errorf("a down server must not create dead letters, dead=%d", d)
	}
}
