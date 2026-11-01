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
	changed chan struct{} // closed, and replaced, when a fact is kept
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
	if a.changed != nil {
		close(a.changed)
		a.changed = nil
	}
	return nil
}

// Await is the facts of one call once it has at least n, or ctx's error:
// a settlement is recorded after its call's answer is sent, so a test that
// reads it waits here, never by sleeping.
func (a *Audit) Await(ctx context.Context, requestID string, n int) ([]ports.Fact, error) {
	for {
		a.mu.Lock()
		if a.changed == nil {
			a.changed = make(chan struct{})
		}
		changed := a.changed
		a.mu.Unlock()
		if facts := a.Of(requestID); len(facts) >= n {
			return facts, nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return a.Of(requestID), ctx.Err()
		}
	}
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
