package pipeline

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/clock"
	"atlas.local/sdk/go/kit/conc"
	"atlas.local/sdk/go/protocol"
)

// Every test here runs in a synctest bubble: its clock is the bubble's,
// time moves only when every goroutine in it waits, and a goroutine still
// running when the test ends fails it.

// slowAuth is an authenticator that takes d to answer, as a token check
// that fetches keys does, and honours its context while it waits.
type slowAuth struct {
	ports.Authenticator
	d time.Duration
}

func (s slowAuth) Authenticate(ctx context.Context, token string) (ports.Identity, error) {
	select {
	case <-time.After(s.d):
	case <-ctx.Done():
		return ports.Identity{}, ctx.Err()
	}
	return s.Authenticator.Authenticate(ctx, token)
}

// R042, R17, AGENTS.md (every outbound call is bounded by the call's
// deadline): an audit sink that stalls on the admission cannot hold a call
// past its deadline. The call answers deadline_exceeded at its deadline,
// and nothing is dispatched.
func TestR042AStalledAdmissionEndsAtTheDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dispatched := false
		r := newRig(t, serve(func(context.Context, *ports.Invocation) (*ports.Answer, error) {
			dispatched = true
			return &ports.Answer{Rows: ports.SliceRows(listRows("147HYC"))}, nil
		}), Options{Clock: clock.System})
		r.audit.Stall(true)
		c := r.call("t-alice", listAt, `{}`)
		start := time.Now()
		c.Deadline = start.Add(time.Second)
		_, err := r.run(c)
		if code(err) != protocol.CodeDeadlineExceeded || dispatched {
			t.Fatalf("%v, dispatched %v", err, dispatched)
		}
		if took := time.Since(start); took > time.Second {
			t.Fatalf("answered %v after the call's deadline", took-time.Second)
		}
	})
}

// R042: the deadline bounds the steps before dispatch too. A call whose
// authentication took 2s of its 3s reaches its provider with at most 1s
// left, never a fresh 3s.
func TestR042TheProviderHasOnlyWhatIsLeft(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var left time.Duration
		r := newRig(t, serve(func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
			d, _ := ctx.Deadline()
			left = time.Until(d)
			return &ports.Answer{Rows: ports.SliceRows(listRows("147HYC"))}, nil
		}), Options{Clock: clock.System})
		r.p.opt.Authenticator = slowAuth{r.tokens, 2 * time.Second}
		c := r.call("t-alice", listAt, `{}`)
		c.Deadline = time.Now().Add(3 * time.Second)
		res, err := r.run(c)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := drain(res); err != nil {
			t.Fatal(err)
		}
		if left <= 0 || left > time.Second {
			t.Fatalf("the provider had %v of a deadline 1s away", left)
		}
	})
}

// R042, R17, 06-7 §6.7.3 (a caller that went away is logged as 499): a
// caller that goes away cancels the work, and the outcome records a
// cancellation (A-171), not a fault of the gateway nor a deadline.
func TestR042ACallerThatGoesAwayIsCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{})
		r := newRig(t, serve(func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}), Options{Clock: clock.System})
		ctx, cancel := context.WithCancel(context.Background())
		c := r.call("t-alice", listAt, `{}`)
		wait := conc.Go(func() error {
			_, err := r.p.Run(ctx, c)
			return err
		})
		<-started
		cancel()
		_ = wait()
		out := r.facts(c.RequestID, ports.Outcome)
		if len(out) != 1 || out[0].Code != ports.CodeCancelled || out[0].Step != ports.StepDispatch {
			t.Fatalf("outcome %+v", out)
		}
	})
}
