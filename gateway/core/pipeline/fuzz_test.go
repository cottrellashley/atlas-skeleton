package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

// valid answers every capability of the rig with rows, or a record, its
// contract holds, whatever the request.
func valid(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
	switch inv.Capability {
	case "portfolio.holdings@v1":
		return &ports.Answer{Rows: ports.SliceRows([]*ijson.Object{holdingsRow("147HYC", "US-HY-ACME-29")})}, nil
	case "data.status@v1":
		return &ports.Answer{Rows: ports.SliceRows([]*ijson.Object{statusRow("harbour.holdings")})}, nil
	case "demo.echo@v1":
		return said(ctx, inv)
	}
	return &ports.Answer{Rows: ports.SliceRows(listRows("147HYC"))}, nil
}

// FuzzPR07Request is the request step's fuzz target (AGENTS.md: fuzz every
// parser; PR07, R04, ATLAS-ACCESS-010): whatever body a caller sends, under
// whatever profile it says it wrote it, the call ends with a request the
// current contract holds or with the caller's own problem, never a fault
// of the gateway, and never a panic.
func FuzzPR07Request(f *testing.F) {
	r := newRig(f, serve(valid), Options{})
	v1 := r.reg.Snapshot().Lookup("demo.echo@v1").Current
	v2 := loadDoc(f, echo("request2", `, "loud": {"type": ["boolean", "null"], "default": null, "description": "Shout."}`, v1.Exact().String()))
	if err := r.reg.Register(registry.Entry{Profiles: []*profile.Profile{v1, v2}, Provider: serve(valid)}); err != nil {
		f.Fatal(err)
	}
	expects := []string{"", v1.Exact().String(), v2.Exact().String(), r.reg.Snapshot().Lookup("portfolio.holdings@v1").Current.Exact().String(), "demo.echo@v1#0000000000000000", "not an exact id"}
	paths := []string{holdingsAt, listAt, statusAt, echoAt}
	for _, seed := range []string{
		`{}`, `{"portfolios": ["147HYC"]}`, `{"portfolios": ["147HYC"], "as_of": "2026-10-28", "columns": ["portfolio", "isin"]}`,
		`{"portfolios": ["147HYC"], "columns": ["nope"]}`, `{"text": "hi"}`, `{"text": "hi", "loud": true}`, `[1]`, `null`, `"x"`,
		`{"portfolios": [1, "x", "x"], "nope": true}`, `{"region": "emea", "asset_class": "credit"}`, `{"sources": ["harbour.holdings"]}`,
		`{"portfolios": ["GLOBAL-CREDIT", "EM-RATES", "EURO-IG", "147HYC"], "asset_class": null, "columns": null}`,
	} {
		for i := range paths {
			f.Add([]byte(seed), uint8(i), uint8(i))
		}
	}
	f.Fuzz(func(t *testing.T, body []byte, path, expect uint8) {
		v, err := ijson.ParseLimits(body, ijson.Limits{MaxBytes: 1 << 16, MaxDepth: 64})
		if err != nil {
			return // the edge refuses it as invalid_document
		}
		c := r.callOf("t-alice", paths[int(path)%len(paths)], v)
		c.Expect = expects[int(expect)%len(expects)]
		res, err := r.run(c)
		if err != nil {
			var p *protocol.Problem
			if !errors.As(err, &p) || p.Code == protocol.CodeInternal || p.Check() != nil {
				t.Fatalf("%s under %q: %v", body, c.Expect, err)
			}
			return
		}
		defer res.Close()
		if res.Rows != nil {
			if _, err := drain(res); err != nil {
				t.Fatalf("%s under %q: the answer ended %v", body, c.Expect, err)
			}
		}
	})
}
