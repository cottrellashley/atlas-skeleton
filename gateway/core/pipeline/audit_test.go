package pipeline

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// R108, ATLAS-MECH-003 (every refused call's audit record names the step
// that refused it): two refusals for the same code, at different steps,
// leave different facts.
func TestR108EachRefusalNamesItsStep(t *testing.T) {
	r := newRig(t, rows(listRows("147HYC")...), Options{})
	withLimit(r, ports.Limit{Subject: "alice", Capability: "portfolio.list@v1", PerMinute: 1})
	if err := r.whole(r.call("t-alice", listAt, `{}`)); err != nil {
		t.Fatal(err)
	}
	old := r.call("t-alice", listAt, `{}`)
	old.Client = &protocol.Client{Name: "old", Version: "0.1", Protocol: "atlas/0.9"}
	late := r.call("t-alice", listAt, `{}`)
	late.Deadline = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name string
		call *Call
		code string
		step ports.Step
	}{
		{"an old client", old, protocol.CodeUpgradeRequired, ports.StepTransport},
		{"a deadline already passed", late, protocol.CodeDeadlineExceeded, ports.StepTransport},
		{"no token", r.call("forged", listAt, `{}`), protocol.CodeUnauthenticated, ports.StepIdentity},
		{"no capability", r.call("t-alice", "/v1/data/nothing@v1:fetch", `{}`), protocol.CodeNotFound, ports.StepAddress},
		{"no grant", r.call("t-carol", holdingsAt, `{"portfolios": ["147HYC"]}`), protocol.CodeNotGranted, ports.StepAccess},
		{"a deny", r.call("t-dave", holdingsAt, `{"portfolios": ["147HYC"]}`), protocol.CodeDenied, ports.StepAccess},
		{"an invalid request", r.call("t-alice", holdingsAt, `{}`), protocol.CodeInvalidRequest, ports.StepRequest},
		{"outside the scope", r.call("t-bob", holdingsAt, `{"portfolios": ["GLOBAL-CREDIT"]}`), protocol.CodeNotGranted, ports.StepScope},
		{"a limit", r.call("t-alice", listAt, `{}`), protocol.CodeLimitExceeded, ports.StepLimits},
	} {
		if _, err := r.run(c.call); code(err) != c.code {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		facts := r.audit.Of(c.call.RequestID)
		if len(facts) != 1 || facts[0].Kind != ports.Refusal || facts[0].Code != c.code || facts[0].Step != c.step {
			t.Errorf("%s: %+v", c.name, facts)
		}
	}
}

// R108, §6.2.8 (the writer and expected fingerprints; the client),
// ATLAS-ACCESS-007 (an identity header changes nothing and is recorded):
// every fact of a call records the profile its request was written under,
// its client, and any identity a header claimed, which decides nothing.
func TestR108FactsRecordWhatTheCallerClaimed(t *testing.T) {
	r := newRig(t, serve(func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		if inv.Caller.Principal != "alice" || len(inv.Scope["portfolio"]) != 4 {
			return nil, errors.New("the claimed identity decided something")
		}
		return &ports.Answer{Rows: ports.SliceRows(listRows("147HYC", "GLOBAL-CREDIT"))}, nil
	}), Options{})
	exact := r.reg.Snapshot().Lookup("portfolio.list@v1").Current.Exact().String()
	c := r.call("t-alice", listAt, `{}`)
	c.Expect, c.Claimed = exact, "bob"
	res, err := r.run(c)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := drain(res); err != nil || len(got) != 2 {
		t.Fatalf("%d rows: %v", len(got), err)
	}
	facts := r.audit.Of(c.RequestID)
	if len(facts) != 2 {
		t.Fatalf("%+v", facts)
	}
	for _, f := range facts {
		if f.Principal != "alice" || f.Claimed != "bob" || f.Expected != exact || f.Exact != exact || f.Client.Name != "rig" || f.Client.Protocol != "atlas/1.0" {
			t.Fatalf("%s: %+v", f.Kind, f)
		}
	}
}

// settling answers rows, and settles them as settle says.
func settling(settle func(ctx context.Context) (ports.Usage, error)) serve {
	return func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		return &ports.Answer{Rows: ports.SliceRows(listRows("147HYC")), Settle: settle}, nil
	}
}

// R108, R09 (an invocation's history: admission, outcome and settlement),
// §6.2.8 (the source's own record of the work, copied in at settlement),
// ATLAS-CORE §8 step 9: once its answer has ended, a call is settled from
// its source's record, off the call's path, and its settlement fact holds
// that record; a settlement that fails says so, and is never lost.
func TestR108TheSettlementFactHoldsTheSourcesRecord(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		record := ports.Usage{QueryID: "01b8-snowflake-query", Rows: 2, Bytes: 4096, Cost: map[string]string{"credits": "0.002"}}
		var settledAfter time.Duration
		r := newRig(t, settling(func(ctx context.Context) (ports.Usage, error) {
			if err := ctx.Err(); err != nil {
				return ports.Usage{}, err
			}
			start := time.Now()
			time.Sleep(time.Second) // the source's history answers slowly
			settledAfter = time.Since(start)
			return record, nil
		}), Options{Clock: clock.System})
		c := r.call("t-alice", listAt, `{}`)
		ctx, gone := context.WithCancel(context.Background())
		res, err := r.p.Run(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		if _, err := drain(res); err != nil {
			t.Fatal(err)
		}
		if waited := time.Since(start); waited != 0 {
			t.Fatalf("the answer waited %v for its settlement", waited)
		}
		gone() // the caller has its answer and leaves
		if err := r.p.Drain(); err != nil {
			t.Fatal(err)
		}
		s := r.facts(c.RequestID, ports.Settlement)
		if len(s) != 1 || s[0].Source == nil || s[0].Source.QueryID != record.QueryID || s[0].Source.Bytes != 4096 || s[0].Usage["credits"] != "0.002" || s[0].Code != "" || settledAfter != time.Second {
			t.Fatalf("%+v", r.audit.Of(c.RequestID))
		}
		if kinds := kindsOf(r.audit.Of(c.RequestID)); kinds != "admission outcome settlement" {
			t.Fatalf("the history: %s", kinds)
		}
	})
}

func kindsOf(facts []ports.Fact) string {
	s := ""
	for i, f := range facts {
		if i > 0 {
			s += " "
		}
		s += string(f.Kind)
	}
	return s
}

// R108, R09: a settlement that fails, or that cannot begin, is a fact with
// its code, so the call is visibly unsettled rather than silently so.
func TestR108AnUnsettledCallSaysSo(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, settling(func(ctx context.Context) (ports.Usage, error) {
			return ports.Usage{}, errors.Join(ports.ErrSourceUnavailable, errors.New("query history: 503"))
		}), Options{Clock: clock.System})
		failed := r.call("t-alice", listAt, `{}`)
		if err := r.whole(failed); err != nil {
			t.Fatal(err)
		}
		if err := r.p.Drain(); err != nil {
			t.Fatal(err)
		}
		late := r.call("t-alice", listAt, `{}`)
		if err := r.whole(late); err != nil {
			t.Fatal(err)
		}
		for id, want := range map[string]string{failed.RequestID: protocol.CodeUpstreamFailed, late.RequestID: protocol.CodeUnavailable} {
			s := r.facts(id, ports.Settlement)
			if len(s) != 1 || s[0].Code != want || s[0].Step != ports.StepSettle || s[0].Source != nil {
				t.Errorf("%s: %+v", want, r.audit.Of(id))
			}
		}
	})
}

// R042, R09: a fact written for a refused or ended call is bounded too,
// so a stalled sink holds a refusal no longer than the audit's own bound.
func TestR042AStalledSinkCannotHoldARefusal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, rows(listRows("147HYC")...), Options{Clock: clock.System, AuditTimeout: 2 * time.Second})
		r.audit.Stall(true)
		start := time.Now()
		if _, err := r.run(r.call("t-dave", holdingsAt, `{"portfolios": ["147HYC"]}`)); code(err) != protocol.CodeDenied {
			t.Fatal(err)
		}
		if waited := time.Since(start); waited != 2*time.Second {
			t.Fatalf("the refusal waited %v on the sink", waited)
		}
	})
}

// R108, R09 (the history exposes what is known rather than inventing a
// completed outcome), R017: an answer the edge cannot send ends with that
// failure as its outcome, its cause logged, never as a success; and one the
// edge stops reading before its end is a cancellation.
func TestR108AnAnswerTheEdgeCannotSendIsNoSuccess(t *testing.T) {
	var l logs
	r := newRig(t, serve(valid), Options{Log: l.logger()})
	record := r.call("t-alice", echoAt, `{"text": "hi"}`)
	res, err := r.run(record)
	if err != nil {
		t.Fatal(err)
	}
	res.Fail(errors.New("json: unsupported value: NaN"))
	res.Close()
	table := r.call("t-alice", listAt, `{}`)
	if res, err = r.run(table); err != nil {
		t.Fatal(err)
	}
	if _, err := res.Rows.Next(); err != nil {
		t.Fatal(err)
	}
	res.Rows.Fail(errors.New("arrow/ipc: the stream's writer failed"))
	res.Close()
	stopped := r.call("t-alice", listAt, `{}`)
	if res, err = r.run(stopped); err != nil {
		t.Fatal(err)
	}
	res.Close()
	for id, want := range map[string]string{record.RequestID: protocol.CodeInternal, table.RequestID: protocol.CodeInternal, stopped.RequestID: ports.CodeCancelled} {
		if out := r.facts(id, ports.Outcome); len(out) != 1 || out[0].Code != want || out[0].Step != ports.StepAnswer {
			t.Errorf("want %s: %+v", want, r.audit.Of(id))
		}
	}
	for _, cause := range []string{"unsupported value: NaN", "the stream's writer failed"} {
		if found := l.with(t, cause); len(found) != 1 {
			t.Errorf("the cause %q is logged once: %v", cause, found)
		}
	}
}
