package porttest

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/conc"
)

// PolicyHarness is the suite's hold on a PolicySource's store: what the
// people and systems behind it do to it.
type PolicyHarness interface {
	// Publish makes the store hold p, a new revision.
	Publish(t *testing.T, p *ports.Policy)
	// Break makes the store unable to give its policy: unreachable,
	// unreadable or invalid, as the implementation's store can fail.
	Break(t *testing.T)
}

// SuitePolicy is the policy the PolicySource suite starts from, at a
// revision: a role, a grant, a deny, a scope, a limit and a house rule.
func SuitePolicy(revision string) *ports.Policy {
	return &ports.Policy{
		Revision: revision,
		Roles:    map[string][]string{"analysts": {"analyst"}},
		Grants:   []ports.Grant{{ID: "grant-read", Role: "analyst", Capabilities: []string{"portfolio.holdings@v1", "portfolio.list@v1"}}},
		Denies:   []ports.Deny{{ID: "deny-dave", Subject: "dave", Capabilities: []string{"portfolio.holdings@v1"}, Reason: "Dave is on the deal team."}},
		Scopes:   map[string]ports.Scope{"analysts": {"portfolio": {"147HYC"}}},
		Limits:   []ports.Limit{{Subject: "*", Capability: "portfolio.*", PerMinute: 60, Concurrency: 4, MaxRows: 1000}},
		HouseRules: []ports.HouseRule{
			{Text: "Holdings stay inside the firm.", Capabilities: []string{"portfolio.holdings@v1"}},
		},
		HouseRulesRevision: "house-1",
	}
}

// PolicySource is the PolicySource port's suite (R13, R057). open starts a
// source whose store holds initial, and the harness that changes it. A
// source answers the policy its store holds now, or an error when it
// cannot: never an older policy as if it were current, because the gateway
// bounds only what it knows to be stale (ports.PolicySource).
func PolicySource(t *testing.T, open func(t *testing.T, initial *ports.Policy) (ports.PolicySource, PolicyHarness)) {
	ctx := context.Background()
	read := func(t *testing.T, src ports.PolicySource) *ports.Policy {
		t.Helper()
		p, err := src.Policy(ctx)
		if err != nil || p == nil {
			t.Fatalf("a source that can read its store answers its policy: %v %v", p, err)
		}
		return p
	}
	same := func(t *testing.T, got, want *ports.Policy) {
		t.Helper()
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(want)
		if string(a) != string(b) {
			t.Fatalf("the policy answered is not the one its store holds:\n got  %s\n want %s", a, b)
		}
	}
	t.Run("it answers the policy its store holds", func(t *testing.T) {
		want := SuitePolicy("r1")
		src, _ := open(t, want)
		same(t, read(t, src), want)
		same(t, read(t, src), want)
	})
	t.Run("a change is seen at the next read", func(t *testing.T) {
		src, h := open(t, SuitePolicy("r1"))
		read(t, src)
		next := SuitePolicy("r2")
		next.Denies = append(next.Denies, ports.Deny{ID: "deny-alice", Subject: "alice", Capabilities: []string{"portfolio.list@v1"}, Reason: "Alice moved desks."})
		h.Publish(t, next)
		same(t, read(t, src), next)
	})
	t.Run("a broken store is an error, never the last policy", func(t *testing.T) {
		src, h := open(t, SuitePolicy("r1"))
		read(t, src)
		h.Break(t)
		for range 3 {
			if p, err := src.Policy(ctx); err == nil || p != nil {
				t.Fatalf("a broken store answered %v, %v: an old policy as if it were current hides a revocation that never took hold (R13)", p, err)
			}
		}
	})
	t.Run("it answers again when its store mends", func(t *testing.T) {
		src, h := open(t, SuitePolicy("r1"))
		read(t, src)
		h.Break(t)
		_, _ = src.Policy(ctx)
		mended := SuitePolicy("r3")
		h.Publish(t, mended)
		same(t, read(t, src), mended)
	})
	t.Run("a cancelled context is an error", func(t *testing.T) {
		src, _ := open(t, SuitePolicy("r1"))
		gone, cancel := context.WithCancel(ctx)
		cancel()
		if p, err := src.Policy(gone); err == nil || p != nil {
			t.Fatalf("a read under a cancelled context answered %v, %v", p, err)
		}
	})
	t.Run("a policy answered never changes afterwards", func(t *testing.T) {
		src, h := open(t, SuitePolicy("r1"))
		first := read(t, src)
		before, _ := json.Marshal(first)
		h.Publish(t, SuitePolicy("r2"))
		read(t, src)
		if after, _ := json.Marshal(first); string(after) != string(before) {
			t.Fatalf("a held policy changed under its reader:\n before %s\n after  %s", before, after)
		}
	})
	t.Run("a policy that names no revision is an error", func(t *testing.T) {
		src, h := open(t, SuitePolicy("r1"))
		read(t, src)
		h.Publish(t, &ports.Policy{Grants: SuitePolicy("").Grants})
		if p, err := src.Policy(ctx); err == nil || p != nil {
			t.Fatalf("a policy with no revision answered %v, %v: nothing could record what decided a call", p, err)
		}
	})
	t.Run("reads are safe at once, and never torn", func(t *testing.T) {
		revisions := []string{"r0", "r1", "r2", "r3", "r4", "r5"}
		src, h := open(t, SuitePolicy(revisions[0]))
		var g conc.Group
		for range 16 {
			g.Go(func() error {
				for range 25 {
					p, err := src.Policy(ctx)
					if err != nil {
						return err
					}
					if !slices.Contains(revisions, p.Revision) || len(p.Grants) != 1 || p.Grants[0].ID != "grant-read" {
						return fmt.Errorf("a torn read: %+v", p)
					}
				}
				return nil
			})
		}
		for _, rev := range revisions[1:] {
			h.Publish(t, SuitePolicy(rev))
		}
		if err := g.Wait(); err != nil {
			t.Fatal(err)
		}
		if p := read(t, src); p.Revision != "r5" {
			t.Fatalf("after every change, the last: %s", p.Revision)
		}
	})
}
