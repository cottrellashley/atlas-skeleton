package pipeline

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/clock"
	"atlas.local/sdk/go/kit/conc"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/ijson"
)

// held is a provider that answers once released, honouring its context
// while it waits, and counts the calls it has begun.
type held struct {
	release chan struct{}
	began   atomic.Int64
}

func newHeld() *held { return &held{release: make(chan struct{})} }

func (h *held) Serve(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
	h.began.Add(1)
	select {
	case <-h.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &ports.Answer{Rows: ports.SliceRows(listRows("147HYC"))}, nil
}

// whole runs a call and reads its answer to the end.
func (r *rig) whole(c *Call) error {
	res, err := r.run(c)
	if err != nil {
		return err
	}
	_, err = drain(res)
	return err
}

// R103, 06-7 §6.7.5 (a bulkhead per upstream, its queue twice its
// concurrency; a full one answers at once, naming the upstream), 06-1
// §6.1.8 (load shedding): two calls reach the warehouse, four wait, and the
// seventh is shed at once, unavailable with Retry-After; as places free,
// the queue drains, and nothing is left running.
func TestR103AFullBulkheadShedsAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHeld()
		r := newRig(t, Through("warehouse", h), Options{Clock: clock.System, Upstreams: map[string]int{"warehouse": 2}})
		var g conc.Group
		for range 6 {
			g.Go(func() error { return r.whole(r.call("t-alice", listAt, `{}`)) })
		}
		synctest.Wait()
		if n := h.began.Load(); n != 2 {
			t.Fatalf("%d calls reached the warehouse at once, not 2", n)
		}
		start := time.Now()
		c := r.call("t-alice", listAt, `{}`)
		_, err := r.run(c)
		p := problemOf(t, err)
		if p.Code != protocol.CodeUnavailable || p.RetryAfter <= 0 || !strings.Contains(p.Detail, "warehouse") || time.Since(start) != 0 {
			t.Fatalf("%+v after %v", p, time.Since(start))
		}
		if out := r.facts(c.RequestID, ports.Outcome); len(out) != 1 || out[0].Code != protocol.CodeUnavailable || out[0].Step != ports.StepDispatch {
			t.Fatalf("a shed call's outcome: %+v", r.audit.Of(c.RequestID))
		}
		close(h.release)
		if err := g.Wait(); err != nil {
			t.Fatal(err)
		}
		if n := h.began.Load(); n != 6 {
			t.Fatalf("the queue drained %d calls, not 6", n)
		}
		if err := r.whole(r.call("t-alice", listAt, `{}`)); err != nil {
			t.Fatalf("after the queue drained: %v", err)
		}
	})
}

// R103: a call waiting in a bulkhead's queue waits no longer than its
// deadline.
func TestR103AQueuedCallEndsAtItsDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHeld()
		r := newRig(t, Through("warehouse", h), Options{Clock: clock.System, Upstreams: map[string]int{"warehouse": 1}})
		wait := conc.Go(func() error { return r.whole(r.call("t-alice", listAt, `{}`)) })
		synctest.Wait()
		c := r.call("t-alice", listAt, `{}`)
		c.Deadline = time.Now().Add(3 * time.Second)
		start := time.Now()
		_, err := r.run(c)
		if code(err) != protocol.CodeDeadlineExceeded || time.Since(start) != 3*time.Second {
			t.Fatalf("%v after %v", err, time.Since(start))
		}
		close(h.release)
		if err := wait(); err != nil {
			t.Fatal(err)
		}
	})
}

// counting is an authenticator that counts the tokens it is asked about.
type counting struct {
	ports.Authenticator
	n atomic.Int64
}

func (c *counting) Authenticate(ctx context.Context, token string) (ports.Identity, error) {
	c.n.Add(1)
	return c.Authenticator.Authenticate(ctx, token)
}

// R103, 06-7 §6.7.2 (one in-flight limit per replica; a stream counts by
// connection; past it, unavailable with Retry-After): a call past the
// replica's limit is shed before any work, a token check or an audit write
// among it, and the replica takes calls again as answers end.
func TestR103TheReplicaShedsPastItsInFlightLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHeld()
		r := newRig(t, h, Options{Clock: clock.System, MaxInFlight: 2})
		auth := &counting{Authenticator: r.tokens}
		r.p.opt.Authenticator = auth
		var g conc.Group
		for range 2 {
			g.Go(func() error { return r.whole(r.call("t-alice", listAt, `{}`)) })
		}
		synctest.Wait()
		checked, facts := auth.n.Load(), len(r.audit.Facts())
		c := r.call("t-alice", listAt, `{}`)
		_, err := r.run(c)
		if p := problemOf(t, err); p.Code != protocol.CodeUnavailable || p.RetryAfter <= 0 {
			t.Fatalf("%+v", p)
		}
		if auth.n.Load() != checked || len(r.audit.Facts()) != facts {
			t.Fatal("a shed call did work: a token check or an audit write")
		}
		close(h.release)
		if err := g.Wait(); err != nil {
			t.Fatal(err)
		}
		if err := r.whole(r.call("t-alice", listAt, `{}`)); err != nil {
			t.Fatalf("after the answers ended: %v", err)
		}
	})
}

// statusRow is a row of data.status.
func statusRow(source string) *ijson.Object {
	o := ijson.NewObject()
	o.Set("source", source)
	o.Set("latest_date", "2026-10-30")
	o.Set("expected_date", "2026-10-30")
	o.Set("status", "current")
	o.Set("checked_at", "2026-10-31T12:00:00.000000Z")
	return o
}

// R109, 06-2 §6.2.6 (output: rows per answer): a capability no policy
// limit names still has an output cap, the pipeline's default, which its
// provider is told so it can push the cap down, and which truncates the
// answer and says so.
func TestR109EveryCapabilityHasAnOutputCap(t *testing.T) {
	for _, c := range []struct {
		name      string
		limit     int // a policy's max_rows, or 0 for none
		rows      int
		truncated bool
	}{
		{name: "the default cap", rows: 2, truncated: true},
		{name: "a policy's cap replaces it", limit: 5, rows: 3},
	} {
		var told int
		r := newRig(t, serve(func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
			told = inv.MaxRows
			return &ports.Answer{Rows: ports.SliceRows([]*ijson.Object{statusRow("harbour.holdings"), statusRow("harbour.prices"), statusRow("harbour.ratings")})}, nil
		}), Options{MaxRows: 2})
		want := 2
		if c.limit > 0 {
			withLimit(r, ports.Limit{Subject: "alice", Capability: "data.status@v1", MaxRows: c.limit})
			want = c.limit
		}
		res, err := r.run(r.call("t-alice", statusAt, `{}`))
		if err != nil {
			t.Fatal(err)
		}
		got, err := drain(res)
		if err != nil || len(got) != c.rows || res.Rows.Truncated() != c.truncated || told != want {
			t.Errorf("%s: %d rows, truncated %v, the provider told %d: %v", c.name, len(got), res.Rows.Truncated(), told, err)
		}
	}
}
