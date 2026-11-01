package porttest

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
)

var errBroken = errors.New("the store has failed")

// R073, R056: the fake passes the AuditSink suite.
func TestTheFakeAuditPassesTheSuite(t *testing.T) {
	AuditSink(t, func(t *testing.T) Audit {
		a := &fakes.Audit{}
		return Audit{Sink: a, Read: func(*testing.T, int) []ports.Fact { return a.Facts() }, Break: func(*testing.T) { a.Fail(errBroken) }}
	})
}

// R073: the suite fails a sink that breaks the contract, each in the case
// that is there to catch it. A suite that passes everything proves nothing.
func TestTheAuditSuiteCatchesWhatItShould(t *testing.T) {
	for _, c := range []struct {
		name, fails string
		sink        func() *flawed
	}{
		{"a sink that swallows its store's failure", "a failing store is reported, never swallowed", func() *flawed { return &flawed{swallow: true} }},
		{"a sink that answers before the fact is held", "a recorded fact is held, whole, when Record returns", func() *flawed { return &flawed{lag: true} }},
		{"a sink that records past its context", "a fact whose context has ended is refused and not recorded", func() *flawed { return &flawed{ignoreContext: true} }},
		{"a sink that records a fact twice", "many callers at once each leave their own fact, once", func() *flawed { return &flawed{double: true} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			caught := false
			for _, ac := range auditCases {
				f := c.sink()
				err := ac.check(t, Audit{Sink: f, Read: f.read, Break: f.brk})
				if ac.name == c.fails {
					caught = err != nil
				}
			}
			if !caught {
				t.Fatalf("the case %q let it through", c.fails)
			}
		})
	}
}

// flawed is a sink with one defect.
type flawed struct {
	swallow, lag, ignoreContext, double bool

	mu     sync.Mutex
	facts  []ports.Fact
	broken bool
	calls  int
}

func (f *flawed) Record(ctx context.Context, fact ports.Fact) error {
	if err := ctx.Err(); err != nil && !f.ignoreContext {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	switch {
	case f.broken && f.swallow:
		return nil
	case f.broken:
		return errBroken
	case f.lag && f.calls%2 == 0:
		return nil // kept for a later write that never comes in time
	}
	f.facts = append(f.facts, fact)
	if f.double && f.calls == 7 {
		f.facts = append(f.facts, fact)
	}
	return nil
}

func (f *flawed) read(*testing.T, int) []ports.Fact {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ports.Fact(nil), f.facts...)
}

func (f *flawed) brk(*testing.T) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broken = true
}
