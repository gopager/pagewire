package queue

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gopager/pagewire/internal/message"
)

func msg(addr string) *message.Message {
	return &message.Message{ID: 1, ReceivedAt: time.Now(), Protocol: "pocsag1200", Address: addr, Kind: message.KindAlpha, Text: "t", Payload: &message.POCSAGPayload{Baud: 1200, Raw: []byte{0xab}, RawBits: 8}}
}

func TestFIFOAckFailDeadReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.db")
	q, err := Open(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"1", "2", "3"} {
		if err := q.Enqueue("t", msg(a)); err != nil {
			t.Fatal(err)
		}
	}
	e, err := q.Peek("t")
	if err != nil || e.Message.Address != "1" {
		t.Fatalf("peek = %+v, %v", e, err)
	}
	if p, ok := e.Message.Payload.(*message.POCSAGPayload); !ok || p.RawHex() != "ab" {
		t.Errorf("typed payload lost: %#v", e.Message.Payload)
	}
	if err := q.Ack("t", e.Seq); err != nil {
		t.Fatal(err)
	}
	e, _ = q.Peek("t")
	if e.Message.Address != "2" {
		t.Fatalf("after ack peek = %s", e.Message.Address)
	}
	dead, err := q.Fail("t", e.Seq, "boom", "server", true, 2)
	if err != nil || dead {
		t.Fatalf("first fail: dead=%v err=%v", dead, err)
	}
	e, _ = q.Peek("t")
	if e.Attempts != 1 || e.LastError != "boom" || e.Class != "server" || e.Message.Address != "2" {
		t.Fatalf("after fail: %+v", e)
	}
	// A target-level failure records the error but does not consume attempts.
	if _, err := q.Fail("t", e.Seq, "down", "network", false, 2); err != nil {
		t.Fatal(err)
	}
	e, _ = q.Peek("t")
	if e.Attempts != 1 || e.Class != "network" {
		t.Fatalf("after target-level fail: %+v", e)
	}
	dead, _ = q.Fail("t", e.Seq, "boom", "server", true, 2)
	if !dead {
		t.Fatal("second counted fail should move to dead letters")
	}
	if p, d := q.Depth("t"); p != 1 || d != 1 {
		t.Errorf("depth = %d/%d", p, d)
	}
	// Persistence across reopen.
	q.Close()
	q, err = Open(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	if p, d := q.Depth("t"); p != 1 || d != 1 {
		t.Errorf("depth after reopen = %d/%d", p, d)
	}
	n, err := q.Replay("t", "", 0)
	if err != nil || n != 1 {
		t.Fatalf("replay = %d, %v", n, err)
	}
	if p, d := q.Depth("t"); p != 2 || d != 0 {
		t.Errorf("depth after replay = %d/%d", p, d)
	}
	entries, _ := q.List("t", false, 10)
	if len(entries) != 2 || entries[0].Message.Address != "3" || entries[1].Message.Address != "2" || entries[1].Attempts != 0 {
		t.Errorf("list = %+v", entries)
	}
	if _, err := q.Purge("t", false); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Peek("t"); err != ErrEmpty {
		t.Errorf("after purge peek err = %v", err)
	}
}

func TestBoundDropsOldest(t *testing.T) {
	q, err := Open(filepath.Join(t.TempDir(), "q.db"), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	for _, a := range []string{"1", "2", "3"} {
		if err := q.Enqueue("t", msg(a)); err != nil {
			t.Fatal(err)
		}
	}
	e, _ := q.Peek("t")
	if e.Message.Address != "2" || q.Dropped("t") != 1 {
		t.Errorf("oldest should be dropped: peek=%s dropped=%d", e.Message.Address, q.Dropped("t"))
	}
	if p, _ := q.Depth("t"); p != 2 {
		t.Errorf("depth = %d", p)
	}
}

func TestNotify(t *testing.T) {
	q, _ := Open(filepath.Join(t.TempDir(), "q.db"), 10)
	defer q.Close()
	ch := q.Notify("t")
	if err := q.Enqueue("t", msg("1")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no notification")
	}
}

func TestSecondOpenFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.db")
	q, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	if _, err := Open(path, 10); err == nil {
		t.Fatal("second instance on the same file must fail (lock)")
	}
}

func TestReplayLimitsAndClass(t *testing.T) {
	q, _ := Open(filepath.Join(t.TempDir(), "q.db"), 100)
	defer q.Close()
	for i, class := range []string{"server", "server", "auth"} {
		if err := q.Enqueue("t", msg(string(rune('a'+i)))); err != nil {
			t.Fatal(err)
		}
		e, _ := q.Peek("t")
		if _, err := q.Fail("t", e.Seq, "x", class, true, 1); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := q.Replay("t", "server", 1); n != 2 {
		t.Errorf("first automatic replay of server class = %d, want 2", n)
	}
	for i := 0; i < 2; i++ {
		e, _ := q.Peek("t")
		if e.Replays != 1 {
			t.Errorf("replays = %d", e.Replays)
		}
		if _, err := q.Fail("t", e.Seq, "x", "server", true, 1); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := q.Replay("t", "server", 1); n != 0 {
		t.Errorf("entries past max_replays must stay dead, replayed %d", n)
	}
	if n, _ := q.Replay("t", "", 0); n != 3 {
		t.Errorf("manual replay moves everything, got %d", n)
	}
	e, _ := q.Peek("t")
	if e.Replays != 0 {
		t.Errorf("manual replay resets counters, got %d", e.Replays)
	}
}
