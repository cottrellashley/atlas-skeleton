package porttest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/conc"
	"atlas.local/sdk/go/protocol"
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
	if err := everyField(auditFacts()); err != nil {
		t.Fatal(err)
	}
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
	settlement.Source = &ports.Usage{QueryID: "01bf-0000-1234-0001", Rows: 1000, Bytes: 2048, Cost: map[string]string{"credits": "0.002"}}
	settlement.Step = ports.StepSettle
	refusal.Step, outcome.Step = ports.StepAccess, ports.StepAnswer
	for _, f := range []*ports.Fact{&admission, &refusal, &outcome, &settlement} {
		f.Claimed = "someone-else@example.com"
		f.Client = protocol.Client{Name: "atlas-cli", Version: "0.2.0", Protocol: "atlas/1.0"}
		f.Expected = "portfolio.holdings@v1#219b68a11867acd4"
	}
	return []ports.Fact{admission, refusal, outcome, settlement}
}

// everyField reports a member of ports.Fact that no fact of auditFacts
// sets (R3-04): the suite compares every member, so each must be set by
// one of them, and a member added to Fact is added here too.
func everyField(facts []ports.Fact) error {
	t := reflect.TypeFor[ports.Fact]()
	for i := range t.NumField() {
		set := false
		for _, f := range facts {
			if !reflect.ValueOf(f).Field(i).IsZero() {
				set = true
			}
		}
		if !set {
			return fmt.Errorf("porttest: no fact of the suite sets ports.Fact.%s, so no store is checked to keep it", t.Field(i).Name)
		}
	}
	return nil
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

// sameFact reports whether a store kept every member of a fact (R3-04):
// times compare as instants, and an empty map as an absent one.
func sameFact(a, b ports.Fact) bool {
	if !a.At.Equal(b.At) {
		return false
	}
	a.At = b.At
	if len(a.Usage) == 0 && len(b.Usage) == 0 {
		a.Usage, b.Usage = nil, nil
	}
	if a.Source != nil && b.Source != nil && len(a.Source.Cost) == 0 && len(b.Source.Cost) == 0 {
		as, bs := *a.Source, *b.Source
		as.Cost, bs.Cost = nil, nil
		a.Source, b.Source = &as, &bs
	}
	return reflect.DeepEqual(a, b)
}
