package pipeline

import (
	"context"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/clock"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/ijson"
)

// R105, R126, R006, 06-2 §6.2.6, STREAM-004: an answer is truncated only
// when the source holds a row past the caller's cap. An answer of exactly
// the cap is whole; one row over it is cut at the cap and says so.
func TestR105OutputCapIsExact(t *testing.T) {
	for _, c := range []struct {
		cap, rows int
		truncated bool
	}{
		{cap: 3, rows: 2},
		{cap: 3, rows: 3},
		{cap: 3, rows: 4, truncated: true},
		{cap: 1, rows: 4, truncated: true},
	} {
		codes := []string{"147HYC", "GLOBAL-CREDIT", "EM-RATES", "EURO-IG"}[:c.rows]
		r := newRig(t, rows(listRows(codes...)...), Options{})
		withLimit(r, ports.Limit{Subject: "alice", Capability: "portfolio.list@v1", MaxRows: c.cap})
		call := r.call("t-alice", listAt, `{}`)
		res, err := r.run(call)
		if err != nil {
			t.Fatal(err)
		}
		got, err := drain(res)
		want := min(c.rows, c.cap)
		if err != nil || len(got) != want || res.Rows.Count() != int64(want) || res.Rows.Truncated() != c.truncated {
			t.Errorf("cap %d over %d rows: %d rows, truncated %v, %v", c.cap, c.rows, len(got), res.Rows.Truncated(), err)
		}
		if out := r.facts(call.RequestID, ports.Outcome); len(out) != 1 || out[0].Truncated != c.truncated || out[0].Rows != int64(want) {
			t.Errorf("cap %d over %d rows: outcome %+v", c.cap, c.rows, out)
		}
	}
}

// leaky answers rows and records with members the contract does not
// declare, one of them a portfolio outside the caller's scope.
func leaky(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
	if inv.Capability == "demo.echo@v1" {
		rec := ijson.NewObject()
		rec.Set("text", "hello")
		rec.Set("sister_portfolio", "GLOBAL-CREDIT")
		return &ports.Answer{Record: rec}, nil
	}
	row := listRow("147HYC")
	row.Set("sister_portfolio", "GLOBAL-CREDIT")
	row.Set("manager_comp", "1250000.00")
	return &ports.Answer{Rows: ports.SliceRows([]*ijson.Object{row})}, nil
}

// R106, R05 (returned-data scope through every delivery path): a member the
// contract does not declare never reaches the caller, so a provider cannot
// leak another portfolio under a name the scope check does not know.
func TestR106UndeclaredMembersNeverLeave(t *testing.T) {
	r := newRig(t, serve(leaky), Options{})
	res, err := r.run(r.call("t-bob", listAt, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := drain(res)
	if err != nil || len(got) != 1 {
		t.Fatalf("%d rows, %v", len(got), err)
	}
	if names := got[0].Names(); !slices.Equal(names, []string{"code", "name", "region", "asset_class", "base_currency", "status"}) {
		t.Fatalf("a row holds only the declared columns: %v", names)
	}
	res, err = r.run(r.call("t-bob", echoAt, `{"text": "hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	if names := res.Record.Names(); !slices.Equal(names, []string{"text"}) {
		t.Fatalf("a record holds only the declared members: %v", names)
	}
}

// holdingsRow is a row of portfolio.holdings.
func holdingsRow(portfolio, security string) *ijson.Object {
	o := ijson.NewObject()
	o.Set("portfolio", portfolio)
	o.Set("security_id", security)
	o.Set("as_of", "2026-10-30")
	o.Set("isin", nil)
	o.Set("title", "A bond")
	o.Set("asset_class", "credit")
	o.Set("quantity", "100.000000")
	mv := ijson.NewObject()
	mv.Set("amount", "101.500000")
	mv.Set("currency", "USD")
	o.Set("market_value", mv)
	o.Set("weight", ijson.Number("0.5"))
	o.Set("oas", nil)
	o.Set("status", "open")
	return o
}

// endless is a stream of holdings rows that never ends by itself.
type endless struct{ portfolio string }

func (e endless) Next(ctx context.Context) (*ijson.Object, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return holdingsRow(e.portfolio, "US-HY-ACME-29"), nil
}

func (endless) Close() error { return nil }

// R104, 06-2 rule 3, R13 (an active-connection freshness bound): a grant
// revoked while a stream is open ends it with a problem, within the bound,
// and its outcome says so. The test gives the policy cache every chance to
// renew between reads, however it renews.
func TestR104RevocationEndsAnOpenStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, serve(func(context.Context, *ports.Invocation) (*ports.Answer, error) {
			return &ports.Answer{Rows: endless{"147HYC"}}, nil
		}), Options{Clock: clock.System})
		c := r.call("t-alice", holdingsAt, `{"portfolios": ["147HYC"]}`)
		c.Deadline = time.Now().Add(10 * time.Minute)
		res, err := r.run(c)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Close()
		if _, err := res.Rows.Next(); err != nil {
			t.Fatal(err)
		}
		revoked := testPolicy("r2")
		revoked.Denies = append(revoked.Denies, ports.Deny{ID: "deny-alice", Subject: "alice", Capabilities: []string{"portfolio.holdings@v1"}, Reason: "Alice moved to the deal team."})
		r.policy.Set(revoked)
		start := time.Now()
		for err == nil && time.Since(start) < time.Minute {
			time.Sleep(r.p.opt.StreamRecheck + time.Second)
			synctest.Wait()
			_, err = res.Rows.Next()
		}
		if code(err) != protocol.CodeDenied {
			t.Fatalf("after %v: %v, want denied", time.Since(start), err)
		}
		if took := time.Since(start); took > 2*(r.p.opt.PolicyRefresh+r.p.opt.StreamRecheck) {
			t.Fatalf("the stream outlived its revocation by %v", took)
		}
		if out := r.facts(c.RequestID, ports.Outcome); len(out) != 1 || out[0].Code != protocol.CodeDenied || out[0].Step != ports.StepAnswer {
			t.Fatalf("outcome %+v", out)
		}
	})
}

// R104, R05: access to a portfolio withdrawn while an answer is open ends
// it with not_granted once a row names it, not with a fault of the
// provider, which was given the scope as it stood.
func TestR104AWithdrawnPortfolioEndsTheAnswer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, serve(func(context.Context, *ports.Invocation) (*ports.Answer, error) {
			return &ports.Answer{Rows: endless{"GLOBAL-CREDIT"}}, nil
		}), Options{Clock: clock.System})
		c := r.call("t-alice", holdingsAt, `{"portfolios": ["147HYC", "GLOBAL-CREDIT"]}`)
		c.Deadline = time.Now().Add(10 * time.Minute)
		res, err := r.run(c)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Close()
		if _, err := res.Rows.Next(); err != nil {
			t.Fatal(err)
		}
		narrowed := testPolicy("r2")
		narrowed.Scopes["credit"] = ports.Scope{"portfolio": {"147HYC"}}
		r.policy.Set(narrowed)
		start := time.Now()
		for err == nil && time.Since(start) < time.Minute {
			time.Sleep(r.p.opt.StreamRecheck + time.Second)
			synctest.Wait()
			_, err = res.Rows.Next()
		}
		if p := problemOf(t, err); p.Code != protocol.CodeNotGranted || !strings.Contains(p.Next, "atlas.access@v1:fetch") {
			t.Fatalf("%+v", p)
		}
	})
}
