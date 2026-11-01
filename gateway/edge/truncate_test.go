package edge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/frame"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// ndjsonRows reads an NDJSON answer whole: its rows, its end line, and the
// error its reader ended with (io.EOF on a whole answer).
func ndjsonRows(t testing.TB, a answer) ([]*ijson.Object, *frame.End, error) {
	t.Helper()
	if a.status != 200 || a.header.Get("Content-Type") != protocol.MediaNDJSON {
		t.Fatalf("%d %s: %s", a.status, a.header.Get("Content-Type"), a.body)
	}
	rd := frame.NewNDJSONReader(bytes.NewReader(a.body))
	var rows []*ijson.Object
	for {
		row, err := rd.Next()
		if err != nil {
			return rows, rd.End(), err
		}
		rows = append(rows, row)
	}
}

// codeOf is an error's problem code, or "" when it is not a problem.
func codeOf(err error) string {
	var p *protocol.Problem
	if errors.As(err, &p) {
		return p.Code
	}
	return ""
}

// arrowAnswer reads an Arrow answer whole: how many rows its batches held,
// its end line, and the error its reader ended with.
func arrowAnswer(t testing.TB, a answer) (int64, *frame.End, error) {
	t.Helper()
	if a.status != 200 || a.header.Get("Content-Type") != protocol.MediaArrowStream {
		t.Fatalf("%d %s: %s", a.status, a.header.Get("Content-Type"), a.body)
	}
	rd, err := frame.NewArrowReader(bytes.NewReader(a.body), memory.NewGoAllocator())
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Release()
	var n int64
	for {
		b, err := rd.Next()
		if err != nil {
			return n, rd.End(), err
		}
		n += b.NumRows()
		b.Release()
	}
}

// PR04, R007, R009, R011; 06-2 §6.2.6 ("the answer is truncated and says
// so"), ATLAS-CORE §10 ("a truncated result must be marked incomplete"),
// Q-DOC-15, STREAM-004, A-150: an answer cut at the caller's output cap is
// never a complete-looking 200. NDJSON and Arrow end with truncated and
// output_limit_exceeded; a JSON array, which has no end, is refused, and
// the refusal names the encoding that would say where it was cut.
func TestPR04OutputCapIsNeverSilent(t *testing.T) {
	r := newRig(t, rigOptions{limits: []ports.Limit{
		{Subject: "alice", Capability: "portfolio.holdings@v1", MaxRows: 3},
		{Subject: "alice", Capability: "portfolio.list@v1", MaxRows: 2},
	}})
	body := `{"portfolios": ["147HYC", "GLOBAL-CREDIT"]}`

	rows, end, err := ndjsonRows(t, r.call("t-alice", holdingsAt, body, "Accept", protocol.MediaNDJSON))
	if codeOf(err) != protocol.CodeOutputLimitExceeded || len(rows) != 3 || !end.Truncated || end.Problem == nil || end.Problem.Code != protocol.CodeOutputLimitExceeded || end.Rows != 3 {
		t.Fatalf("NDJSON: %d rows, end %+v, %v", len(rows), end, err)
	}

	n, end, err := arrowAnswer(t, r.call("t-alice", holdingsAt, body, "Accept", protocol.MediaArrowStream))
	if codeOf(err) != protocol.CodeOutputLimitExceeded || n != 3 || !end.Truncated || end.Problem == nil || end.Problem.Code != protocol.CodeOutputLimitExceeded {
		t.Fatalf("Arrow: %d rows, end %+v, %v", n, end, err)
	}

	for _, c := range []struct {
		name, path, body, accept, features, next string
	}{
		{"holdings asked for as JSON", holdingsAt, body, protocol.MediaJSON, fullFeatures, "Ask for " + protocol.MediaNDJSON},
		{"holdings asked for by a Core client", holdingsAt, body, "*/*", "levels=(core)", "Narrow the request"},
		{"a single-delivery table", listAt, `{}`, protocol.MediaJSON, fullFeatures, "Narrow the request"},
	} {
		a := r.call("t-alice", c.path, c.body, "Accept", c.accept, protocol.HeaderClientFeatures, "", protocol.HeaderClientFeatures, c.features)
		if a.status == 200 {
			t.Fatalf("%s: a cut table was sent as a 200: %s", c.name, a.body)
		}
		p := a.problem(t)
		if p.Code != protocol.CodeInvalidRequest || p.Status != 400 || len(p.Details) != 1 || p.Details[0].Code != protocol.CodeOutputLimitExceeded || !strings.HasPrefix(p.Next, c.next) {
			t.Fatalf("%s: %d %s", c.name, a.status, a.body)
		}
		if a.header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: Cache-Control %q", c.name, a.header.Get("Cache-Control"))
		}
		out := r.facts(a, ports.Outcome)
		if len(out) != 1 || !out[0].Truncated {
			t.Fatalf("%s: outcome %+v", c.name, out)
		}
	}

	// Within the cap, JSON is a plain array: Bob's one portfolio is one
	// row of the list, under Alice's cap of two.
	whole := r.call("t-bob", listAt, `{}`)
	if whole.status != 200 || whole.header.Get("Content-Type") != protocol.MediaJSON || !strings.HasPrefix(string(whole.body), `[{"code":"147HYC"`) {
		t.Fatalf("within the cap: %d %s", whole.status, whole.body)
	}
}

// R109, R060, AGENTS.md "no unbounded input": a JSON answer is held to
// MaxAnswer bytes. A table larger than that is refused, never cut, and
// NDJSON streams the same table whole.
func TestR109JSONAnswersAreBounded(t *testing.T) {
	r := newRig(t, rigOptions{serving: Serving{MaxAnswer: 4096}})
	r.provide(func(_ context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		return &ports.Answer{Rows: &lazy{n: 1000, make: func(i int) *ijson.Object { return holdingRow("147HYC", i) }}}, nil
	})
	body := `{"portfolios": ["147HYC"]}`
	a := r.call("t-alice", holdingsAt, body, "Accept", protocol.MediaJSON)
	if p := a.problem(t); p.Code != protocol.CodeInvalidRequest || len(p.Details) != 1 || p.Details[0].Code != protocol.CodeOutputLimitExceeded || !strings.Contains(p.Detail, "4096 bytes") {
		t.Fatalf("%d %s", a.status, a.body)
	}
	rows, end, err := ndjsonRows(t, r.call("t-alice", holdingsAt, body, "Accept", protocol.MediaNDJSON))
	if !errors.Is(err, io.EOF) || len(rows) != 1000 || end.Truncated || end.Problem != nil {
		t.Fatalf("NDJSON: %d rows, %+v, %v", len(rows), end, err)
	}
}
