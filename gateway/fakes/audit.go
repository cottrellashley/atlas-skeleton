package fakes

import (
	"context"
	"slices"
	"sync"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
)

// Audit keeps facts in memory, in the order they were recorded.
type Audit struct {
	mu      sync.Mutex
	facts   []ports.Fact
	err     error
	stalled bool
}

// Record keeps the fact, unless the sink was told to fail. A stalled sink
// answers only when ctx ends, as a sink writing to a stuck disk or a
// blocked pipe does, so a caller that does not bound it waits for ever.
func (a *Audit) Record(ctx context.Context, f ports.Fact) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	stalled := a.stalled
	a.mu.Unlock()
	if stalled {
		<-ctx.Done()
		return ctx.Err()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return a.err
	}
	a.facts = append(a.facts, f)
	return nil
}

// Fail makes every Record fail with err (nil heals it).
func (a *Audit) Fail(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.err = err
}

// Stall makes every Record wait for its context to end (false heals it).
func (a *Audit) Stall(on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stalled = on
}

// Facts are every fact recorded.
func (a *Audit) Facts() []ports.Fact {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.facts)
}

// Of are the facts of one call, in order.
func (a *Audit) Of(requestID string) []ports.Fact {
	var out []ports.Fact
	for _, f := range a.Facts() {
		if f.RequestID == requestID {
			out = append(out, f)
		}
	}
	return out
}
