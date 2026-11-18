// Package queue is the persistent per-target delivery queue. Messages are
// stored in a bbolt file so that nothing is lost while a target is down or
// the gateway restarts; each target has its own FIFO and dead-letter
// bucket. Delivery is at-least-once.
package queue

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/gopager/pagewire/internal/message"
)

var (
	bucketTargets = []byte("targets")
	keyPending    = []byte("pending")
	keyDead       = []byte("dead")
)

// ErrEmpty is returned by Peek when a target has nothing pending.
var ErrEmpty = errors.New("queue: empty")

// Entry is a queued message with its delivery bookkeeping.
type Entry struct {
	Seq      uint64
	Attempts int
	// LastError is the error of the most recent failed attempt and Class
	// its classification (network, auth, server).
	LastError string
	Class     string
	// Replays counts how often the entry came back from the dead letters.
	Replays int
	Message *message.Message
}

// stored is the on-disk form of an entry, gob-encoded. Payload types are
// registered with gob in the message package.
type stored struct {
	Attempts  int
	LastError string
	Class     string
	Replays   int
	Enqueued  time.Time
	Message   *message.Message
}

func encode(st *stored) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(st); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decode(v []byte) (*stored, error) {
	var st stored
	if err := gob.NewDecoder(bytes.NewReader(v)).Decode(&st); err != nil {
		return nil, err
	}
	return &st, nil
}

// Queue is the bbolt-backed queue.
type Queue struct {
	db     *bolt.DB
	maxPer int

	mu      sync.Mutex
	pending map[string]int
	dead    map[string]int
	dropped map[string]uint64
	notify  map[string]chan struct{}
}

// Open opens or creates the queue file. maxPerTarget bounds the pending
// entries per target; the oldest entry is dropped when it is exceeded.
func Open(path string, maxPerTarget int) (*Queue, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second, NoFreelistSync: true})
	if err != nil {
		return nil, fmt.Errorf("queue: open %s: %w", path, err)
	}
	q := &Queue{db: db, maxPer: maxPerTarget, pending: map[string]int{}, dead: map[string]int{}, dropped: map[string]uint64{}, notify: map[string]chan struct{}{}}
	err = db.Update(func(tx *bolt.Tx) error {
		root, err := tx.CreateBucketIfNotExists(bucketTargets)
		if err != nil {
			return err
		}
		return root.ForEachBucket(func(name []byte) error {
			tb := root.Bucket(name)
			q.pending[string(name)] = count(tb.Bucket(keyPending))
			q.dead[string(name)] = count(tb.Bucket(keyDead))
			return nil
		})
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("queue: init: %w", err)
	}
	return q, nil
}

func count(b *bolt.Bucket) int {
	if b == nil {
		return 0
	}
	n := 0
	_ = b.ForEach(func(_, _ []byte) error { n++; return nil })
	return n
}

// Close closes the file.
func (q *Queue) Close() error { return q.db.Close() }

// Targets lists the targets that have buckets.
func (q *Queue) Targets() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, 0, len(q.pending))
	for t := range q.pending {
		out = append(out, t)
	}
	return out
}

func targetBuckets(tx *bolt.Tx, target string, create bool) (pending, dead *bolt.Bucket, err error) {
	root := tx.Bucket(bucketTargets)
	var tb *bolt.Bucket
	if create {
		if tb, err = root.CreateBucketIfNotExists([]byte(target)); err != nil {
			return nil, nil, err
		}
		if pending, err = tb.CreateBucketIfNotExists(keyPending); err != nil {
			return nil, nil, err
		}
		if dead, err = tb.CreateBucketIfNotExists(keyDead); err != nil {
			return nil, nil, err
		}
		return pending, dead, nil
	}
	if tb = root.Bucket([]byte(target)); tb == nil {
		return nil, nil, nil
	}
	return tb.Bucket(keyPending), tb.Bucket(keyDead), nil
}

func seqKey(seq uint64) []byte {
	var k [8]byte
	binary.BigEndian.PutUint64(k[:], seq)
	return k[:]
}

// Enqueue appends msg to the target's FIFO.
func (q *Queue) Enqueue(target string, msg *message.Message) error {
	val, err := encode(&stored{Enqueued: time.Now(), Message: msg})
	if err != nil {
		return fmt.Errorf("queue: encode message: %w", err)
	}
	dropped := 0
	err = q.db.Update(func(tx *bolt.Tx) error {
		pending, _, err := targetBuckets(tx, target, true)
		if err != nil {
			return err
		}
		q.mu.Lock()
		n := q.pending[target]
		q.mu.Unlock()
		for n >= q.maxPer {
			c := pending.Cursor()
			k, _ := c.First()
			if k == nil {
				break
			}
			if err := pending.Delete(k); err != nil {
				return err
			}
			n--
			dropped++
		}
		seq, err := pending.NextSequence()
		if err != nil {
			return err
		}
		return pending.Put(seqKey(seq), val)
	})
	if err != nil {
		return fmt.Errorf("queue: enqueue: %w", err)
	}
	q.mu.Lock()
	q.pending[target] += 1 - dropped
	q.dropped[target] += uint64(dropped)
	ch := q.notify[target]
	q.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return nil
}

// Notify returns a channel that receives a token whenever something is
// enqueued for target, for workers that sleep while the queue is empty.
func (q *Queue) Notify(target string) <-chan struct{} {
	q.mu.Lock()
	defer q.mu.Unlock()
	ch, ok := q.notify[target]
	if !ok {
		ch = make(chan struct{}, 1)
		q.notify[target] = ch
	}
	return ch
}

// Peek returns the oldest pending entry of target without removing it.
func (q *Queue) Peek(target string) (*Entry, error) {
	var e *Entry
	err := q.db.View(func(tx *bolt.Tx) error {
		pending, _, err := targetBuckets(tx, target, false)
		if err != nil || pending == nil {
			return err
		}
		k, v := pending.Cursor().First()
		if k == nil {
			return nil
		}
		e, err = decodeEntry(k, v)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("queue: peek: %w", err)
	}
	if e == nil {
		return nil, ErrEmpty
	}
	return e, nil
}

func decodeEntry(k, v []byte) (*Entry, error) {
	st, err := decode(v)
	if err != nil {
		return nil, err
	}
	return &Entry{Seq: binary.BigEndian.Uint64(k), Attempts: st.Attempts, LastError: st.LastError, Class: st.Class, Replays: st.Replays, Message: st.Message}, nil
}

// Ack removes a delivered entry.
func (q *Queue) Ack(target string, seq uint64) error {
	err := q.db.Update(func(tx *bolt.Tx) error {
		pending, _, err := targetBuckets(tx, target, false)
		if err != nil || pending == nil {
			return err
		}
		return pending.Delete(seqKey(seq))
	})
	if err != nil {
		return fmt.Errorf("queue: ack: %w", err)
	}
	q.mu.Lock()
	q.pending[target]--
	q.mu.Unlock()
	return nil
}

// Fail records a failed attempt of class class. When countAttempt is set
// the entry's attempt counter grows and the entry moves to the dead-letter
// bucket once it reaches maxAttempts (dead is then true); otherwise only
// the error is recorded, for failures that are the target's fault rather
// than the message's.
func (q *Queue) Fail(target string, seq uint64, reason, class string, countAttempt bool, maxAttempts int) (dead bool, err error) {
	err = q.db.Update(func(tx *bolt.Tx) error {
		pending, deadB, err := targetBuckets(tx, target, false)
		if err != nil || pending == nil {
			return err
		}
		k := seqKey(seq)
		v := pending.Get(k)
		if v == nil {
			return nil
		}
		st, err := decode(v)
		if err != nil {
			return err
		}
		if countAttempt {
			st.Attempts++
		}
		st.LastError = reason
		st.Class = class
		nv, err := encode(st)
		if err != nil {
			return err
		}
		if countAttempt && st.Attempts >= maxAttempts {
			dead = true
			if err := pending.Delete(k); err != nil {
				return err
			}
			dseq, err := deadB.NextSequence()
			if err != nil {
				return err
			}
			return deadB.Put(seqKey(dseq), nv)
		}
		return pending.Put(k, nv)
	})
	if err != nil {
		return false, fmt.Errorf("queue: fail: %w", err)
	}
	if dead {
		q.mu.Lock()
		q.pending[target]--
		q.dead[target]++
		q.mu.Unlock()
	}
	return dead, nil
}

// Depth returns the pending and dead-letter counts of target.
func (q *Queue) Depth(target string) (pending, dead int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.pending[target], q.dead[target]
}

// Dropped returns how many entries were discarded because the target's
// bound was exceeded.
func (q *Queue) Dropped(target string) uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.dropped[target]
}

// Replay moves dead-letter entries of target back to pending with their
// attempt counter cleared and returns how many were moved. With
// maxReplays > 0 only entries replayed fewer times are moved (automatic
// replay); 0 moves everything and resets the replay counters (manual
// replay). class restricts the replay to one failure class when not empty.
func (q *Queue) Replay(target, class string, maxReplays int) (int, error) {
	n := 0
	err := q.db.Update(func(tx *bolt.Tx) error {
		pending, deadB, err := targetBuckets(tx, target, false)
		if err != nil || pending == nil {
			return err
		}
		c := deadB.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			st, err := decode(v)
			if err != nil {
				return err
			}
			if class != "" && st.Class != class {
				continue
			}
			if maxReplays > 0 && st.Replays >= maxReplays {
				continue
			}
			if maxReplays > 0 {
				st.Replays++
			} else {
				st.Replays = 0
			}
			st.Attempts, st.LastError = 0, ""
			nv, err := encode(st)
			if err != nil {
				return err
			}
			seq, err := pending.NextSequence()
			if err != nil {
				return err
			}
			if err := pending.Put(seqKey(seq), nv); err != nil {
				return err
			}
			if err := c.Delete(); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("queue: replay: %w", err)
	}
	q.mu.Lock()
	q.pending[target] += n
	q.dead[target] -= n
	ch := q.notify[target]
	q.mu.Unlock()
	if ch != nil && n > 0 {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return n, nil
}

// List returns up to limit entries of the pending or dead-letter bucket.
func (q *Queue) List(target string, dead bool, limit int) ([]*Entry, error) {
	var out []*Entry
	err := q.db.View(func(tx *bolt.Tx) error {
		pending, deadB, err := targetBuckets(tx, target, false)
		if err != nil || pending == nil {
			return err
		}
		b := pending
		if dead {
			b = deadB
		}
		c := b.Cursor()
		for k, v := c.First(); k != nil && len(out) < limit; k, v = c.Next() {
			e, err := decodeEntry(k, v)
			if err != nil {
				return err
			}
			out = append(out, e)
		}
		return nil
	})
	return out, err
}

// Purge deletes all entries of the pending or dead-letter bucket.
func (q *Queue) Purge(target string, dead bool) (int, error) {
	n := 0
	err := q.db.Update(func(tx *bolt.Tx) error {
		root := tx.Bucket(bucketTargets)
		tb := root.Bucket([]byte(target))
		if tb == nil {
			return nil
		}
		key := keyPending
		if dead {
			key = keyDead
		}
		n = count(tb.Bucket(key))
		if err := tb.DeleteBucket(key); err != nil {
			return err
		}
		_, err := tb.CreateBucket(key)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("queue: purge: %w", err)
	}
	q.mu.Lock()
	if dead {
		q.dead[target] = 0
	} else {
		q.pending[target] = 0
	}
	q.mu.Unlock()
	return n, nil
}
