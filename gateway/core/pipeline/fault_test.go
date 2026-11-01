package pipeline

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/ijson"
)

// runRecovering is Run, with a panic that escapes it caught and returned:
// a pipeline that lets one through has failed the test, not crashed it.
func (r *rig) runRecovering(c *Call) (res *Result, escaped any, err error) {
	defer func() { escaped = recover() }()
	res, err = r.run(c)
	return res, nil, err
}

// onlyFirst panics on the first n calls, and answers rows after them.
func onlyFirst(n int64, rs ...*ijson.Object) serve {
	var calls atomic.Int64
	return func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		if calls.Add(1) <= n {
			panic(fmt.Sprintf("the driver fell over on call %d", calls.Load()))
		}
		return &ports.Answer{Rows: ports.SliceRows(rs)}, nil
	}
}

func withLimit(r *rig, l ports.Limit) {
	pol := testPolicy("r1")
	pol.Limits = append(pol.Limits, l)
	r.policy.Set(pol)
}

// R043, R058, ATLAS-MECH-007: a provider that panics costs its own call
// only. The call answers internal, and the process lives; its concurrency
// slot comes back, so the caller is not locked out; and its admission has
// an outcome fact.
func TestR043ProviderPanicReleasesItsSlot(t *testing.T) {
	r := newRig(t, onlyFirst(2, listRows("147HYC")...), Options{})
	withLimit(r, ports.Limit{Subject: "alice", Capability: "portfolio.list@v1", Concurrency: 1})
	for i := range 2 {
		c := r.call("t-alice", listAt, `{}`)
		_, escaped, err := r.runRecovering(c)
		if escaped != nil {
			t.Fatalf("call %d: the panic escaped the pipeline: %v", i+1, escaped)
		}
		if code(err) != protocol.CodeInternal {
			t.Fatalf("call %d: %v, want internal (not limit_exceeded: the slot came back)", i+1, err)
		}
		admitted, outcomes := r.facts(c.RequestID, ports.Admission), r.facts(c.RequestID, ports.Outcome)
		if len(admitted) != 1 || len(outcomes) != 1 || outcomes[0].Code != protocol.CodeInternal {
			t.Fatalf("call %d: an admission without its outcome: %+v", i+1, r.audit.Of(c.RequestID))
		}
	}
	res, err := r.run(r.call("t-alice", listAt, `{}`))
	if err != nil {
		t.Fatalf("after two panics the caller is admitted again: %v", err)
	}
	if got, err := drain(res); err != nil || len(got) != 1 {
		t.Fatalf("%d rows, %v", len(got), err)
	}
}

// panicky is a row stream that panics after its first row.
type panicky struct{ n int }

func (p *panicky) Next(ctx context.Context) (*ijson.Object, error) {
	if p.n++; p.n > 1 {
		panic("the stream fell over")
	}
	return listRow("147HYC"), nil
}

func (p *panicky) Close() error { return nil }

// R058, ATLAS-MECH-007: a stream that panics after its first row ends the
// answer with internal, releases the slot, and leaves its outcome.
func TestR058StreamPanicEndsTheAnswer(t *testing.T) {
	r := newRig(t, serve(func(context.Context, *ports.Invocation) (*ports.Answer, error) {
		return &ports.Answer{Rows: &panicky{}}, nil
	}), Options{})
	withLimit(r, ports.Limit{Subject: "alice", Capability: "portfolio.list@v1", Concurrency: 1})
	for i := range 2 {
		c := r.call("t-alice", listAt, `{}`)
		res, err := r.run(c)
		if err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
		var got []*ijson.Object
		var escaped any
		func() {
			defer func() { escaped = recover() }()
			got, err = drain(res)
		}()
		if escaped != nil {
			t.Fatalf("call %d: the panic escaped the answer: %v", i+1, escaped)
		}
		if len(got) != 1 || code(err) != protocol.CodeInternal {
			t.Fatalf("call %d: %d rows, %v", i+1, len(got), err)
		}
		if out := r.facts(c.RequestID, ports.Outcome); len(out) != 1 || out[0].Code != protocol.CodeInternal || out[0].Rows != 1 {
			t.Fatalf("call %d: %+v", i+1, r.audit.Of(c.RequestID))
		}
	}
}

// failing is a provider whose source fails with err.
func failing(err error) serve {
	return func(context.Context, *ports.Invocation) (*ports.Answer, error) { return nil, err }
}
