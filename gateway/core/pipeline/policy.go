package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/conc"
)

// errStalePolicy is a policy older than the gateway may decide by.
var errStalePolicy = errors.New("pipeline: the policy cannot be renewed and is older than its bound")

// errNoPolicy is a gateway that has not read a policy it may decide by.
var errNoPolicy = errors.New("pipeline: the gateway has read no policy it may decide by")

// policyCache holds the last policy read, and keeps deciding by it while
// its source is down only until maxAge (R13): a gateway that cannot renew
// its policy stops deciding rather than promise a revocation it cannot
// keep.
//
// The request path reads a snapshot (constitution rule 3, R018, R044):
// once a renewal is due, one read of the source runs in the background,
// under its own deadline, and every call meanwhile decides by the policy
// held. A call waits on the source only when nothing may decide it and a
// read begun since then may yet answer: the first read, and the first
// after the gateway was idle past the bound. Past the bound with a read
// failed or hung, it is refused at once. A failed read is tried again a
// refresh later, so a store that is down is not read once per call.
type policyCache struct {
	src             ports.PolicySource
	clock           clock.Clock
	refresh, maxAge time.Duration

	held atomic.Pointer[heldPolicy] // what the request path reads
	mu   sync.Mutex                 // guards read and abandoned; never held across one
	read *policyRead                // the read in flight, or nil
	// abandoned counts reads given up at their deadline that have not
	// returned yet: a source that ignores its context (a read stuck on a
	// mount) holds a goroutine each, so no more than maxAbandoned are left
	// waiting before the cache stops starting reads until one returns.
	abandoned int
}

// maxAbandoned bounds the reads left waiting on a source that ignores its
// context.
const maxAbandoned = 2

// heldPolicy is one state of the cache. It never changes once stored.
type heldPolicy struct {
	policy   *ports.Policy // the last policy read, nil before the first
	read     time.Time     // when the read that answered it began
	err      error         // why the reads since then failed, or nil
	failedAt time.Time
}

// policyRead is one read of the source in flight.
type policyRead struct {
	began time.Time
	ctx   context.Context // done at the read's deadline, answered or not
	done  chan struct{}   // closed once its answer is held
}

func (c *policyCache) state() *heldPolicy {
	if h := c.held.Load(); h != nil {
		return h
	}
	return &heldPolicy{}
}

// maxPolicyRead is the longest one read of the policy store may take,
// however long the bound: a read slower than this has failed.
const maxPolicyRead = 30 * time.Second

// readTimeout is how long one read of the source may take: a quarter of
// the bound, so a read that hangs fails with time for others before the
// policy it renews is past its bound, and at most maxPolicyRead, so no
// call waits long on a store that hangs.
func (c *policyCache) readTimeout() time.Duration { return min(c.maxAge/4, maxPolicyRead) }

// get is the policy a call is decided by, or why there is none.
func (c *policyCache) get(ctx context.Context) (*ports.Policy, error) {
	now := c.clock.Now()
	if h := c.state(); h.policy != nil && now.Sub(h.read) < c.refresh {
		return h.policy, nil
	}
	h, r := c.renew(ctx, now)
	if h.policy != nil && now.Sub(h.read) <= c.maxAge {
		return h.policy, nil
	}
	// Nothing may decide the call. Wait only for a read begun since the
	// policy went past its bound, and only while no read has failed since
	// the last that succeeded: a store known to be down, or a read begun
	// earlier that has not answered yet, refuses the call at once.
	if r != nil && h.err == nil && (h.policy == nil || r.began.Sub(h.read) > c.maxAge) {
		var cause error
		select {
		case <-r.done:
		case <-r.ctx.Done():
			cause = fmt.Errorf("the policy source did not answer within %v: %w", c.readTimeout(), r.ctx.Err())
		case <-ctx.Done():
			cause = ctx.Err()
		}
		after := c.state()
		if after.policy != nil && c.clock.Now().Sub(after.read) <= c.maxAge {
			return after.policy, nil
		}
		return nil, c.stale(after, cause)
	}
	return nil, c.stale(h, nil)
}

// renew is the state held and the read in flight, seen together: a read
// stores its answer before it leaves, under the same lock. It starts a
// read unless one is in flight, the policy was renewed since now, or a
// read failed less than a refresh ago.
func (c *policyCache) renew(ctx context.Context, now time.Time) (*heldPolicy, *policyRead) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := c.state()
	if r := c.read; r != nil && r.ctx.Err() != nil {
		// The read is past its deadline and has not answered: give it up,
		// say so in the state readiness and the audit see, and let a later
		// read start, so one stuck read never stops the renewal (R13, R044).
		c.abandoned++
		c.read = nil
		c.held.Store(&heldPolicy{policy: h.policy, read: h.read, failedAt: now,
			err: fmt.Errorf("pipeline: the policy source did not answer within %v: %w", c.readTimeout(), context.DeadlineExceeded)})
		h = c.state()
	}
	switch {
	case c.read != nil:
		return h, c.read
	case c.abandoned >= maxAbandoned:
		return h, nil
	case h.policy != nil && now.Sub(h.read) < c.refresh:
		return h, nil
	case h.err != nil && now.Sub(h.failedAt) < c.refresh:
		return h, nil
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.readTimeout())
	r := &policyRead{began: now, ctx: rctx, done: make(chan struct{})}
	c.read = r
	conc.Go(func() (err error) {
		var p *ports.Policy
		defer func() {
			if v := recover(); v != nil {
				p, err = nil, fmt.Errorf("pipeline: the policy source panicked: %v", v)
			}
			c.answer(r, p, err)
			cancel() // after the answer is held, so no waiter wakes to the old state
		}()
		p, err = c.src.Policy(rctx)
		return err
	})
	return h, r
}

// answer holds what a read answered: a policy that may decide calls from
// when the read began, or the read's failure beside the policy held.
func (c *policyCache) answer(r *policyRead, p *ports.Policy, err error) {
	switch {
	case err == nil && p == nil:
		err = errors.New("pipeline: the policy source answered no policy")
	case err == nil && p.Revision == "":
		err = errors.New("pipeline: the policy source answered a policy with no revision")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	defer close(r.done)
	if c.read != r {
		// An abandoned read, answering at last. Its answer is older than
		// what the cache has said since, so it is dropped; the source is
		// answering again, so the next refresh reads it anew.
		c.abandoned--
		return
	}
	c.read = nil
	h := c.state()
	next := &heldPolicy{policy: p, read: r.began}
	if err != nil {
		next = &heldPolicy{policy: h.policy, read: h.read, err: err, failedAt: c.clock.Now()}
	}
	c.held.Store(next)
}

// stale is why h may not decide a call: no policy yet, or one past its
// bound, with the failure that kept it from being renewed: the source's
// own, or else the waiting call's cause.
func (c *policyCache) stale(h *heldPolicy, cause error) error {
	if h.err != nil {
		cause = h.err
	}
	if h.policy == nil {
		if cause == nil {
			return errNoPolicy
		}
		return fmt.Errorf("%w: %w", errNoPolicy, cause)
	}
	if cause == nil {
		cause = errors.New("no read has answered since")
	}
	return fmt.Errorf("%w: revision %q was read at %s, more than %v before %s: %w",
		errStalePolicy, h.policy.Revision, h.read.UTC().Format(time.RFC3339), c.maxAge, c.clock.Now().UTC().Format(time.RFC3339), cause)
}

// PolicyStatus is what the pipeline knows of the policy it decides by:
// for readiness (§6.7.15, "a snapshot is loaded ... and its source") and
// for the operator who must learn why a policy file stopped deciding.
type PolicyStatus struct {
	// Revision is the policy held, "" before the first read.
	Revision string
	// Read is when the read that answered it began.
	Read time.Time
	// Deciding reports whether it may still decide calls: it is no older
	// than PolicyMaxAge.
	Deciding bool
	// Err is why the reads since Read failed, nil when none has.
	Err      error
	FailedAt time.Time
}

func (c *policyCache) status() PolicyStatus {
	h := c.state()
	s := PolicyStatus{Read: h.read, Err: h.err, FailedAt: h.failedAt}
	if h.policy != nil {
		s.Revision = h.policy.Revision
		s.Deciding = c.clock.Now().Sub(h.read) <= c.maxAge
	}
	return s
}

// PolicyStatus is the policy the pipeline decides by, its age and why it
// could not be renewed. It never reads the source.
func (p *Pipeline) PolicyStatus() PolicyStatus { return p.policy.status() }
