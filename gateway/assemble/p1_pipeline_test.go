package assemble

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"atlas.local/gateway/fakes"
	"atlas.local/gateway/fixtures"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/frame"
)

// R105, R126, R006, STREAM-004, over the wire through the view model: the
// views provider asks the warehouse for one row past the cap, and the
// pipeline reads it, so 147HYC's four positions under a cap of four end
// whole, and under a cap of three end truncated with output_limit_exceeded.
func TestR105TheEndLineTellsTheTruth(t *testing.T) {
	for _, c := range []struct {
		cap       int
		rows      int64
		truncated bool
	}{
		{cap: 4, rows: 4},
		{cap: 3, rows: 3, truncated: true},
	} {
		r := newRig(t)
		pol, err := fixtures.Policy()
		if err != nil {
			t.Fatal(err)
		}
		pol.Limits = append(pol.Limits, ports.Limit{Subject: "a11ce000-0000-4000-8000-000000000001", Capability: "portfolio.holdings@v1", MaxRows: c.cap})
		r.gw.Pipeline = mustPipeline(t, r, fakes.NewPolicy(pol))
		a := r.call("dev-alice", "/v1/data/portfolio.holdings@v1:fetch", `{"portfolios": ["147HYC"]}`, "Accept", protocol.MediaNDJSON)
		if a.status != 200 {
			t.Fatalf("cap %d: %d %s", c.cap, a.status, a.body)
		}
		rd := frame.NewNDJSONReader(bytes.NewReader(a.body))
		var rows int64
		for {
			_, err := rd.Next()
			if err != nil {
				if !errors.Is(err, io.EOF) && !(c.truncated && strings.Contains(err.Error(), protocol.CodeOutputLimitExceeded)) {
					t.Fatalf("cap %d: %v\n%s", c.cap, err, a.body)
				}
				break
			}
			rows++
		}
		end := rd.End()
		if end == nil || rows != c.rows || end.Rows != c.rows || end.Truncated != c.truncated {
			t.Fatalf("cap %d: %d rows, end %+v\n%s", c.cap, rows, end, a.body)
		}
		if got := end.Problem != nil && end.Problem.Code == protocol.CodeOutputLimitExceeded; got != c.truncated {
			t.Fatalf("cap %d: the end line's problem %+v", c.cap, end.Problem)
		}
	}
}

// R081, 02-6 rule 4: a refusal of a listed capability, as a client reads
// it, names atlas.access and the capability to ask about.
func TestR081ARefusalOverTheWireNamesAtlasAccess(t *testing.T) {
	r := newRig(t)
	a := r.call("dev-dave", "/v1/data/portfolio.holdings@v1:fetch", `{"portfolios": ["147HYC"]}`)
	if code, _ := a.problem(t); code != protocol.CodeDenied {
		t.Fatal(code)
	}
	if next := str(a.json(t), "next"); !strings.Contains(next, "atlas.access@v1:fetch") || !strings.Contains(next, "portfolio.holdings@v1") {
		t.Fatalf("next %q", next)
	}
}
