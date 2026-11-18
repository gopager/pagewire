// Package sink defines delivery targets. A sink takes one decoded message
// and delivers it to an external system; the delivery worker handles
// queuing, retries and back-off, so sinks stay simple and stateless.
package sink

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/message"
)

// Outcome classifies a delivery attempt that did not fail.
type Outcome string

// Outcomes. Only errors are retried; every outcome acknowledges the entry.
const (
	// OutcomeDelivered means the target accepted the message.
	OutcomeDelivered Outcome = "delivered"
	// OutcomeDuplicate means the target reported it as a duplicate.
	OutcomeDuplicate Outcome = "duplicate"
	// OutcomeFiltered means the target filtered the message out.
	OutcomeFiltered Outcome = "filtered"
	// OutcomeSkipped means the sink itself chose not to send it (e.g.
	// tone-only page without a placeholder text).
	OutcomeSkipped Outcome = "skipped"
	// OutcomeRejected means the target refused the message permanently
	// (malformed); retrying would not help.
	OutcomeRejected Outcome = "rejected"
)

// Class says what kind of problem a failed delivery attempt hit, which
// decides whether the message or the target is at fault.
type Class string

// Failure classes.
const (
	// ClassNetwork: the target could not be reached (DNS, connect, timeout).
	ClassNetwork Class = "network"
	// ClassAuth: the target rejected the credentials.
	ClassAuth Class = "auth"
	// ClassServer: the target answered with a server error.
	ClassServer Class = "server"
)

// Error is a classified delivery failure.
type Error struct {
	Class Class
	Err   error
}

func (e *Error) Error() string { return string(e.Class) + ": " + e.Err.Error() }

// Unwrap returns the underlying error.
func (e *Error) Unwrap() error { return e.Err }

// Classify returns the class of err, defaulting to ClassServer for
// unclassified errors.
func Classify(err error) Class {
	var e *Error
	if errors.As(err, &e) {
		return e.Class
	}
	return ClassServer
}

// Sink delivers messages to one target.
type Sink interface {
	// Name is the target name from the configuration.
	Name() string
	// Type is the sink type, e.g. "pagermon".
	Type() string
	// Deliver sends one message. A returned error means the attempt should
	// be retried later; an Outcome is final.
	Deliver(ctx context.Context, msg *message.Message) (Outcome, error)
	// Check probes reachability of the target.
	Check(ctx context.Context) error
}

// Env carries gateway-wide values sinks may use in templates.
type Env struct {
	Instance string
	Hostname string
}

// Factory builds a sink from its target configuration.
type Factory func(cfg config.TargetConfig, env Env) (Sink, error)

var (
	mu       sync.RWMutex
	registry = map[string]Factory{}
)

// Register adds a sink type. Duplicate registration is a programming error.
func Register(typ string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[typ]; dup {
		panic("sink: duplicate registration of " + typ)
	}
	registry[typ] = f
}

// New builds the sink for a target.
func New(cfg config.TargetConfig, env Env) (Sink, error) {
	mu.RLock()
	f, ok := registry[cfg.Type]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("sink: target %s: type %q is not available (available: %v)", cfg.Name, cfg.Type, Types())
	}
	return f(cfg, env)
}

// Types lists the registered sink types.
func Types() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for t := range registry {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
