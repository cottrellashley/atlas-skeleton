package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/conc"
)

// Every test here runs in a synctest bubble: its clock is the bubble's, so
// a policy ages and a read times out exactly when the test says, and the
// bubble fails the test if a read is left running at its end.

// policySource is a PolicySource the test scripts. When held, a read waits
// until it is released; deaf, it waits even when its context ends, as a
// stat on a hung file system does.
type policySource struct {
	mu    sync.Mutex
	p     *ports.Policy
	err   error
	held  chan struct{}
	deaf  bool
	boom  bool
	reads atomic.Int64
}

func (s *policySource) Policy(ctx context.Context) (*ports.Policy, error) {
	s.reads.Add(1)
	s.mu.Lock()
	held, deaf, boom := s.held, s.deaf, s.boom
	s.mu.Unlock()
	if boom {
		panic("the policy store's client crashed")
	}
	if held != nil {
		if deaf {
			<-held
		} else {
			select {
			case <-held:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return s.p, nil
}

func (s *policySource) set(p *ports.Policy, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.p, s.err = p, err
}

func (s *policySource) hold(deaf bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held, s.deaf = make(chan struct{}), deaf
}

func (s *policySource) release(p *ports.Policy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.p = p
	close(s.held)
	s.held = nil
}

// detach leaves the reads already waiting stuck, and lets every later read
// answer p at once: a read hung on a mount that recovered for new opens. The
// channel it gives back frees the stuck reads when closed.
func (s *policySource) detach(p *ports.Policy) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.held
	s.p, s.held = p, nil
	return held
}

func revision(r string) *ports.Policy { return &ports.Policy{Revision: r} }

func newPolicyCache(src ports.PolicySource) *policyCache {
	return &policyCache{src: src, clock: clock.System, refresh: 5 * time.Second, maxAge: 60 * time.Second}
}

func mustDecideBy(t *testing.T, c *policyCache, want string) {
	t.Helper()
	p, err := c.get(context.Background())
	if err != nil || p.Revision != want {
		t.Fatalf("want %s: %v %v", want, p, err)
	}
}

// R018, R044, ATLAS-MECH-010, constitution rule 3: the request path reads
// a snapshot. Once a refresh is due, one read of the store runs, off the
// request path, and every call meanwhile decides by the policy held,
// without waiting on the store or on each other.
func TestR018ARefreshNeverHoldsACall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := &policySource{p: revision("r1")}
		c := newPolicyCache(src)
		mustDecideBy(t, c, "r1")
		src.hold(false)
		time.Sleep(6 * time.Second)
		start := time.Now()
		var g conc.Group
		for range 50 {
			g.Go(func() error {
				p, err := c.get(context.Background())
				if err != nil || p.Revision != "r1" {
					return fmt.Errorf("%v %v", p, err)
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			t.Fatal(err)
		}
		if waited := time.Since(start); waited != 0 {
			t.Fatalf("calls waited %v on a refresh", waited)
		}
		synctest.Wait() // the read in flight has reached the store
		if n := src.reads.Load(); n != 2 {
			t.Fatalf("one read in flight, not %d", n-1)
		}
		src.release(revision("r2"))
		synctest.Wait()
		mustDecideBy(t, c, "r2")
	})
}

// R044, R13: past its bound a policy that could not be renewed refuses at
// once, never blocking a call on a store that hangs, even one that ignores
// its read's deadline.
func TestR044PastItsBoundAStalePolicyRefusesAtOnce(t *testing.T) {
	for _, deaf := range []bool{false, true} {
		t.Run(fmt.Sprintf("deaf=%v", deaf), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				src := &policySource{p: revision("r1")}
				c := newPolicyCache(src)
				mustDecideBy(t, c, "r1")
				src.hold(deaf)
				time.Sleep(6 * time.Second)
				mustDecideBy(t, c, "r1")
				time.Sleep(55 * time.Second)
				start := time.Now()
				for range 20 {
					if _, err := c.get(context.Background()); !errors.Is(err, errStalePolicy) {
						t.Fatalf("past its bound: %v", err)
					}
				}
				if waited := time.Since(start); waited != 0 {
					t.Fatalf("a refusal waited %v", waited)
				}
				if s := c.status(); s.Deciding || s.Revision != "r1" {
					t.Fatalf("status %+v", s)
				}
				src.release(revision("r2"))
				synctest.Wait()
			})
		})
	}
}

// R044, R13 (the review's regression): a read that ignores its deadline,
// as a read stuck on a hung mount does, is given up at the deadline. Its
// failure is reported, so readiness can say why calls are refused, and once
// the source answers again the cache decides again, with no restart.
func TestR044AStuckReadNeverStopsTheRenewal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := &policySource{p: revision("r1")}
		c := newPolicyCache(src)
		mustDecideBy(t, c, "r1")
		src.hold(true)
		time.Sleep(6 * time.Second)
		mustDecideBy(t, c, "r1") // within the bound, while the read hangs
		time.Sleep(time.Minute)  // past the read's deadline, and past the bound
		if _, err := c.get(context.Background()); !errors.Is(err, errStalePolicy) {
			t.Fatalf("past its bound: %v", err)
		}
		if s := c.status(); s.Err == nil || s.Deciding {
			t.Fatalf("a stuck read is reported: %+v", s)
		}
		src.release(revision("r2"))
		synctest.Wait()
		time.Sleep(6 * time.Second)
		_, _ = c.get(context.Background()) // the store is known down: refused, and a read begins
		synctest.Wait()
		mustDecideBy(t, c, "r2")
		if s := c.status(); s.Err != nil || !s.Deciding || s.Revision != "r2" {
			t.Fatalf("recovered: %+v", s)
		}
	})
}

// R044 (the review's exact case): the read in flight never returns, while
// the store answers every new read. The cache gives the stuck read up at its
// deadline and renews from a new one, so the gateway decides again without
// a restart.
func TestR044ANewReadRecoversFromOneThatNeverReturns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := &policySource{p: revision("r1")}
		c := newPolicyCache(src)
		mustDecideBy(t, c, "r1")
		src.hold(true)
		time.Sleep(6 * time.Second)
		mustDecideBy(t, c, "r1") // a read begins, and hangs
		synctest.Wait()
		stuck := src.detach(revision("r2"))
		time.Sleep(time.Minute)
		_, _ = c.get(context.Background()) // the stuck read is given up
		time.Sleep(6 * time.Second)
		_, _ = c.get(context.Background()) // a new read begins, and answers
		synctest.Wait()
		mustDecideBy(t, c, "r2")
		close(stuck)
		synctest.Wait()
	})
}

// R044: a source that ignores its context holds a goroutine for each read
// it never answers, so the cache leaves at most maxAbandoned of them
// waiting, and starts no read until one returns.
func TestR044AbandonedReadsAreBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := &policySource{p: revision("r1")}
		c := newPolicyCache(src)
		mustDecideBy(t, c, "r1")
		src.hold(true)
		for range 10 {
			time.Sleep(time.Minute)
			_, _ = c.get(context.Background())
		}
		if n := src.reads.Load(); n != 1+maxAbandoned {
			t.Fatalf("%d reads began against a source that never answers; want %d", n, 1+maxAbandoned)
		}
		src.release(revision("r2"))
		synctest.Wait()
	})
}

// R13, PR02: the bound runs from the last read that succeeded, however
// many have failed since. A failure is kept with the policy, so a refusal
// says why, and the policy decides again once its store answers.
func TestR13TheBoundRunsFromTheLastSuccessfulRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		down := errors.New("the control store is down")
		src := &policySource{p: revision("r1")}
		c := newPolicyCache(src)
		mustDecideBy(t, c, "r1")
		src.set(nil, down)
		for range 6 {
			time.Sleep(10 * time.Second)
			mustDecideBy(t, c, "r1")
			synctest.Wait()
		}
		time.Sleep(time.Second)
		_, err := c.get(context.Background())
		if !errors.Is(err, errStalePolicy) || !errors.Is(err, down) {
			t.Fatalf("past its bound, with its cause: %v", err)
		}
		if s := c.status(); s.Deciding || !errors.Is(s.Err, down) || s.Revision != "r1" {
			t.Fatalf("status %+v", s)
		}
		src.set(revision("r2"), nil)
		time.Sleep(5 * time.Second)
		_, _ = c.get(context.Background())
		synctest.Wait()
		mustDecideBy(t, c, "r2")
		if s := c.status(); !s.Deciding || s.Err != nil {
			t.Fatalf("mended %+v", s)
		}
	})
}

// R13: a gateway that was idle past the bound renews before it decides,
// rather than refuse a call to a store that answers.
func TestR13AnIdleGatewayRenewsBeforeItDecides(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := &policySource{p: revision("r1")}
		c := newPolicyCache(src)
		mustDecideBy(t, c, "r1")
		src.set(revision("r2"), nil)
		time.Sleep(2 * time.Minute)
		var g conc.Group
		for range 10 {
			g.Go(func() error {
				p, err := c.get(context.Background())
				if err != nil || p.Revision != "r2" {
					return fmt.Errorf("%v %v", p, err)
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			t.Fatal(err)
		}
		if n := src.reads.Load(); n != 2 {
			t.Fatalf("one renewal for every waiting call, not %d", n-1)
		}
	})
}

// The first read is waited for, bounded by the read's own deadline and the
// call's; a store that is down then refuses at once, read again only after
// a refresh, so a failing store is never read once per call.
func TestTheFirstReadIsWaitedForOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		down := errors.New("down")
		src := &policySource{err: down}
		c := newPolicyCache(src)
		for range 5 {
			if _, err := c.get(context.Background()); !errors.Is(err, errNoPolicy) || !errors.Is(err, down) {
				t.Fatalf("no policy: %v", err)
			}
		}
		if n := src.reads.Load(); n != 1 {
			t.Fatalf("%d reads of a store that is down", n)
		}
		src.set(revision("r1"), nil)
		time.Sleep(5 * time.Second)
		_, _ = c.get(context.Background())
		synctest.Wait()
		mustDecideBy(t, c, "r1")

		hung := &policySource{p: revision("r1")}
		hung.hold(false)
		c = newPolicyCache(hung)
		start := time.Now()
		if _, err := c.get(context.Background()); !errors.Is(err, errNoPolicy) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("a store that never answers: %v", err)
		}
		if waited := time.Since(start); waited != c.readTimeout() {
			t.Fatalf("waited %v for the first read, not its deadline %v", waited, c.readTimeout())
		}
		gone, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := c.get(gone); err == nil {
			t.Fatal("no policy for a caller that left")
		}
		hung.release(revision("r1"))
		synctest.Wait()
	})
}

// A source that panics, answers nothing, or answers a policy with no
// revision has failed its read: the failure is kept, and the process lives
// (ATLAS-MECH-012).
func TestASourceThatMisbehavesHasFailed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for name, src := range map[string]*policySource{
			"panics":      {boom: true},
			"nothing":     {},
			"no revision": {p: &ports.Policy{}},
		} {
			c := newPolicyCache(src)
			_, err := c.get(context.Background())
			if !errors.Is(err, errNoPolicy) || c.status().Err == nil {
				t.Fatalf("a source that %s: %v", name, err)
			}
		}
	})
}

// R13, R018: a read of the policy store has its own deadline, a quarter of
// the bound, and never more than half a minute however long the bound, so
// no call waits long on a store that hangs.
func TestAReadsDeadlineIsBounded(t *testing.T) {
	for maxAge, want := range map[time.Duration]time.Duration{
		60 * time.Second: 15 * time.Second,
		time.Hour:        30 * time.Second,
		time.Second:      250 * time.Millisecond,
	} {
		c := &policyCache{refresh: time.Second, maxAge: maxAge}
		if got := c.readTimeout(); got != want {
			t.Fatalf("a bound of %v: a read's deadline %v, want %v", maxAge, got, want)
		}
	}
}
