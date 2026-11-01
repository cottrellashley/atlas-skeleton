package assemble

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"atlas.local/gateway/fakes"
	"atlas.local/gateway/fixtures"
	"atlas.local/sdk/go/kit/conc"
	"atlas.local/sdk/go/protocol"
)

// Many callers at once, under -race: every answer is its own caller's, and
// no row of one caller's scope reaches another (R05, R18: results are
// private to their caller).
func TestConcurrentCallersNeverSeeEachOthersData(t *testing.T) {
	r := newRig(t)
	var g conc.Group
	g.SetLimit(32)
	var mu sync.Mutex
	leaks := 0
	for i := range 200 {
		token := []string{"dev-alice", "dev-bob", "dev-dave"}[i%3]
		g.Go(func() error {
			a := r.call(token, "/v1/data/portfolio.list@v1:fetch", `{}`)
			if a.status != 200 {
				return errors.New(string(a.body))
			}
			n := strings.Count(string(a.body), `"code"`)
			want := map[string]int{"dev-alice": 4, "dev-bob": 1, "dev-dave": 1}[token]
			if n != want || token != "dev-alice" && strings.Contains(string(a.body), "GLOBAL-CREDIT") {
				mu.Lock()
				leaks++
				mu.Unlock()
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}
	if leaks > 0 {
		t.Fatalf("%d answers were not their caller's", leaks)
	}
}

// R13: a gateway whose policy store stops answering keeps deciding by the
// policy it holds only for its bound, then refuses rather than promise a
// revocation it cannot keep.
func TestAStalePolicyStopsDeciding(t *testing.T) {
	r := newRig(t)
	pol, _ := fixtures.Policy()
	src := fakes.NewPolicy(pol)
	r.gw.Pipeline = mustPipeline(t, r, src)
	if a := r.call("dev-alice", "/v1/data/portfolio.list@v1:fetch", `{}`); a.status != 200 {
		t.Fatalf("%d %s", a.status, a.body)
	}
	src.Fail(errors.New("the control store is down"))
	r.clock.Advance(30 * time.Second)
	if a := r.call("dev-alice", "/v1/data/portfolio.list@v1:fetch", `{}`); a.status != 200 {
		t.Fatalf("within its bound the held policy decides: %d %s", a.status, a.body)
	}
	r.clock.Advance(31 * time.Second)
	if code, _ := r.call("dev-alice", "/v1/data/portfolio.list@v1:fetch", `{}`).problem(t); code != protocol.CodeUnavailable {
		t.Fatalf("past its bound a stale policy refuses: %s", code)
	}
}

// The protective limits hold under contention: a caller at their
// concurrency limit is refused with limit_exceeded and Retry-After.
func TestConcurrencyLimit(t *testing.T) {
	r := newRig(t)
	pol, _ := fixtures.Policy()
	pol.Limits = append(pol.Limits, portsLimit("a11ce000-0000-4000-8000-000000000001", "portfolio.list@v1", 1))
	src := fakes.NewPolicy(pol)
	r.gw.Pipeline = mustPipeline(t, r, src)
	held := make(chan struct{}, 1)
	r.source.Hold(func() {
		select {
		case held <- struct{}{}:
		default:
		}
	})
	first := make(chan answer, 1)
	wait := conc.Go(func() error {
		first <- r.call("dev-alice", "/v1/data/portfolio.list@v1:fetch", `{}`, protocol.HeaderDeadline, protocol.FormatDeadline(r.clock.Now().Add(2*time.Second)))
		return nil
	})
	<-held
	second := r.call("dev-alice", "/v1/data/portfolio.list@v1:fetch", `{}`)
	if code, _ := second.problem(t); code != protocol.CodeLimitExceeded || second.header.Get("Retry-After") == "" {
		t.Fatalf("over the limit: %s %v", code, second.header)
	}
	r.source.Hold(nil)
	_ = wait()
	if a := <-first; a.status != 504 {
		t.Fatalf("the blocked call ends at its deadline: %d", a.status)
	}
}
