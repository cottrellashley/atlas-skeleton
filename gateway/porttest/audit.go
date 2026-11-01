package porttest

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/conc"
)

// Audit is an AuditSink under test, with what the suite needs to check it.
type Audit struct {
	// Sink is the implementation.
	Sink ports.AuditSink
	// Read reads back, through the store's own read path and never through
	// the sink, every fact the store holds, in the order they were recorded.
	// n is how many the suite expects: a store read as a stream (a pipe to a
	// collector) waits until n have arrived, and one read at rest may
	// ignore it.
	Read func(t *testing.T, n int) []ports.Fact
	// Break makes the store behind the sink fail from now on, as a full
	// disk, a closed pipe or an unreachable account does.
	Break func(t *testing.T)
}

// AuditSink is the suite of ports.AuditSink (ATLAS-CORE §8 step 7, R09,
// ATLAS-ACCESS-004): a fact Record accepts is held, whole, before Record
// returns, so an admission is durable before its call is dispatched; many
// callers at once each leave their own fact, once; a failure of the store
// is reported, never swallowed; and a fact whose context has ended is not
// recorded. open makes a fresh, empty store for each case.
func AuditSink(t *testing.T, open func(t *testing.T) Audit) {
	for _, c := range auditCases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.check(t, open(t)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// An audit case returns how the implementation broke the contract, or nil.
type auditCase struct {
	name  string
	check func(t *testing.T, a Audit) error
}

var auditCases = []auditCase{
	{"a recorded fact is held, whole, when Record returns", func(t *testing.T, a Audit) error {
		want := auditFacts()
		for _, f := range want {
			if err := a.Sink.Record(t.Context(), f); err != nil {
				return fmt.Errorf("record %s: %w", f.Kind, err)
			}
		}
		return sameFacts(a.Read(t, len(want)), want, true)
	}},
	{"many callers at once each leave their own fact, once", func(t *testing.T, a Audit) error {
		const callers = 100
		want := make([]ports.Fact, callers)
		var g conc.Group
		for i := range want {
			f := auditFacts()[i%4]
			f.RequestID = "request-" + strconv.Itoa(i)
			want[i] = f
			g.Go(func() error { return a.Sink.Record(t.Context(), f) })
		}
		if err := g.Wait(); err != nil {
			return fmt.Errorf("record: %w", err)
		}
		return sameFacts(a.Read(t, callers), want, false)
	}},
	{"a fact whose context has ended is refused and not recorded", func(t *testing.T, a Audit) error {
		ended, cancel := context.WithCancel(t.Context())
		cancel()
		dropped, kept := auditFacts()[0], auditFacts()[1]
		dropped.RequestID = "the-caller-went-away"
		if err := a.Sink.Record(ended, dropped); !errors.Is(err, context.Canceled) {
			return fmt.Errorf("a fact with an ended context: %v, want context.Canceled", err)
		}
		if err := a.Sink.Record(t.Context(), kept); err != nil {
			return fmt.Errorf("record: %w", err)
		}
		return sameFacts(a.Read(t, 1), []ports.Fact{kept}, true)
	}},
	{"a failing store is reported, never swallowed", func(t *testing.T, a Audit) error {
		if err := a.Sink.Record(t.Context(), auditFacts()[0]); err != nil {
			return fmt.Errorf("record before the store fails: %w", err)
		}
		a.Break(t)
		for _, f := range auditFacts() {
			if err := a.Sink.Record(t.Context(), f); err == nil {
				return fmt.Errorf("the %s fact was accepted by a store that had failed", f.Kind)
			}
		}
		return nil
	}},
}

// auditFacts are one of each kind, with every member set to what a store
// must keep as it was given: times in UTC to the nanosecond, durations to the
// millisecond.
func auditFacts() []ports.Fact {
	at := time.Date(2026, 10, 31, 12, 0, 0, 123456789, time.UTC)
	call := ports.Fact{
		At: at, RequestID: "6f1c2a6e-3b9d-4a7e-9c1d-2f8e5b7a9c10", Attempt: 1,
		Principal: "a11ce000-0000-4000-8000-000000000001", CallerKind: "person", Application: "atlas-cli",
		Environment: "dv", PolicyRevision: "policy-7", Address: "portfolio.holdings@v1:fetch", Exact: "portfolio.holdings@1.2.0",
		RequestDigest: "9f2b5c0e8d7a6b4c3e1f0a9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d",
	}
	admission, refusal, outcome, settlement := call, call, call, call
	admission.Kind = ports.Admission
	refusal.Kind, refusal.Code, refusal.Attempt, refusal.Operation = ports.Refusal, "not_granted", 2, "operations/7"
	outcome.Kind, outcome.Code, outcome.Rows, outcome.Truncated, outcome.Duration = ports.Outcome, "upstream_failed", 1000, true, 1500*time.Millisecond
	outcome.At = at.Add(1500 * time.Millisecond)
	settlement.Kind, settlement.Usage = ports.Settlement, map[string]string{"rows": "1000", "credits": "0.002"}
	return []ports.Fact{admission, refusal, outcome, settlement}
}

// sameFacts reports how got differs from want: in order when ordered,
// otherwise as a multiset, so a fact lost, torn, invented or doubled shows.
func sameFacts(got, want []ports.Fact, ordered bool) error {
	if len(got) != len(want) {
		return fmt.Errorf("the store holds %d facts, want %d", len(got), len(want))
	}
	if ordered {
		for i := range want {
			if !sameFact(got[i], want[i]) {
				return fmt.Errorf("fact %d is %+v, want %+v", i, got[i], want[i])
			}
		}
		return nil
	}
	left := make([]ports.Fact, len(want))
	copy(left, want)
next:
	for _, g := range got {
		for i, w := range left {
			if sameFact(g, w) {
				left = append(left[:i], left[i+1:]...)
				continue next
			}
		}
		return fmt.Errorf("the store holds a fact nobody recorded, or one twice: %+v", g)
	}
	return nil
}

func sameFact(a, b ports.Fact) bool {
	return a.Kind == b.Kind && a.At.Equal(b.At) && a.RequestID == b.RequestID && a.Attempt == b.Attempt && a.Operation == b.Operation &&
		a.Principal == b.Principal && a.CallerKind == b.CallerKind && a.Application == b.Application &&
		a.Environment == b.Environment && a.PolicyRevision == b.PolicyRevision && a.Address == b.Address && a.Exact == b.Exact &&
		a.RequestDigest == b.RequestDigest && a.Code == b.Code && a.Rows == b.Rows && a.Truncated == b.Truncated &&
		maps.Equal(a.Usage, b.Usage) && a.Duration == b.Duration
}
