package edge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/telemetry"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

// R127, ATLAS-MECH-002: a body over 1 MiB is invalid_request before step 1
// runs: no token is verified, whoever sends it.
func TestR127BodyOverTheLimitBeforeStepOne(t *testing.T) {
	r := newRig(t, rigOptions{})
	big := `{"portfolios": ["` + strings.Repeat("x", 1<<20) + `"]}`
	for _, token := range []string{"", "t-alice"} {
		a := r.call(token, holdingsAt, big)
		if p := a.problem(t); p.Code != protocol.CodeInvalidRequest || !strings.Contains(p.Detail, "larger than 1048576 bytes") {
			t.Fatalf("%d %s", a.status, a.body)
		}
	}
	// Sent without a Content-Length, the body is cut at the limit as it is read.
	req := r.request("", holdingsAt, "")
	req.Body = io.NopCloser(io.LimitReader(spaces{}, 2<<20))
	req.ContentLength = -1
	if p := r.do(req).problem(t); p.Code != protocol.CodeInvalidRequest {
		t.Fatalf("a chunked body over the limit: %s", p.Code)
	}
	if n := r.auth.calls.Load(); n != 0 {
		t.Fatalf("%d tokens were verified for bodies over the limit", n)
	}
}

// spaces reads as JSON whitespace, for ever.
type spaces struct{}

func (spaces) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

// R127, STREAM-003 (§9.6.5): a failure after the first row is the end
// line's problem in NDJSON and the final batch's in Arrow; before any row
// is sent, it is a problem with its own status.
func TestR127AFailureAfterTheFirstRowIsTheEnds(t *testing.T) {
	r := newRig(t, rigOptions{})
	r.provide(func(_ context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		return &ports.Answer{Rows: &lazy{n: 10, make: func(i int) *ijson.Object { return holdingRow("147HYC", i) }, before: func(_ context.Context, i int) error {
			if i == 3 {
				return fmt.Errorf("the warehouse went away: %w", ports.ErrWarehouseUnavailable)
			}
			return nil
		}}}, nil
	})
	body := `{"portfolios": ["147HYC"]}`
	rows, end, err := ndjsonRows(t, r.call("t-alice", holdingsAt, body, "Accept", protocol.MediaNDJSON))
	if len(rows) != 3 || codeOf(err) != protocol.CodeUpstreamFailed || end.Problem == nil || end.Problem.RequestID == "" {
		t.Fatalf("NDJSON: %d rows, %+v, %v", len(rows), end, err)
	}
	n, end, err := arrowAnswer(t, r.call("t-alice", holdingsAt, body, "Accept", protocol.MediaArrowStream))
	if n != 3 || codeOf(err) != protocol.CodeUpstreamFailed || end.Problem == nil {
		t.Fatalf("Arrow: %d rows, %+v, %v", n, end, err)
	}
	a := r.call("t-alice", holdingsAt, body, "Accept", protocol.MediaJSON)
	if p := a.problem(t); p.Code != protocol.CodeUpstreamFailed || a.status != 502 {
		t.Fatalf("JSON: %d %s", a.status, a.body)
	}
	r.provide(func(_ context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		return &ports.Answer{Rows: &lazy{n: 10, make: func(i int) *ijson.Object { return holdingRow("147HYC", i) }, before: func(context.Context, int) error {
			return fmt.Errorf("the warehouse went away: %w", ports.ErrWarehouseUnavailable)
		}}}, nil
	})
	if p := r.call("t-alice", holdingsAt, body, "Accept", protocol.MediaNDJSON).problem(t); p.Code != protocol.CodeUpstreamFailed {
		t.Fatalf("before the first row: %s", p.Code)
	}
}

// R127, CORE-DEPR-001 (§9.6.3): a deprecated version's answers carry
// Deprecation, the date it was deprecated, and Sunset, an HTTP-date.
func TestR127DeprecationAndSunset(t *testing.T) {
	r := newRig(t, rigOptions{})
	a := r.call("t-alice", legacyAt, `{}`)
	if a.status != 200 || a.header.Get(protocol.HeaderDeprecation) != "@1790812800" || a.header.Get(protocol.HeaderSunset) != "Sun, 31 Jan 2027 00:00:00 GMT" {
		t.Fatalf("%d %v", a.status, a.header)
	}
	if a := r.call("t-alice", listAt, `{}`); a.header.Get(protocol.HeaderDeprecation) != "" || a.header.Get(protocol.HeaderSunset) != "" {
		t.Fatalf("a stable version: %v", a.header)
	}
}

// R129, §6.7.2 row 1, problem.schema.json: a panic answers internal with
// the request's id; a panic after streaming began still ends the stream
// with its end line or final batch, holding internal, which a client
// reads as internal, not as a cut stream to retry.
func TestR129PanicsAnswerInternal(t *testing.T) {
	r := newRig(t, rigOptions{})
	r.provide(func(context.Context, *ports.Invocation) (*ports.Answer, error) { panic("a provider's bug") })
	a := r.call("t-alice", listAt, `{}`)
	if p := a.problem(t); p.Code != protocol.CodeInternal || a.status != 500 {
		t.Fatalf("a panic before the answer: %d %s", a.status, a.body)
	}
	panicking := func(at int) serve {
		return func(_ context.Context, inv *ports.Invocation) (*ports.Answer, error) {
			return &ports.Answer{Rows: &lazy{n: 5000, make: func(i int) *ijson.Object { return holdingRow("147HYC", i) }, before: func(_ context.Context, i int) error {
				if i == at {
					panic(fmt.Sprintf("a stream's bug at row %d", i))
				}
				return nil
			}}}, nil
		}
	}
	body := `{"portfolios": ["147HYC"]}`
	r.provide(panicking(600))
	rows, end, err := ndjsonRows(t, r.call("t-alice", holdingsAt, body, "Accept", protocol.MediaNDJSON))
	if len(rows) != 600 || codeOf(err) != protocol.CodeInternal || end == nil || end.Problem.RequestID == "" {
		t.Fatalf("NDJSON: %d rows, %+v, %v", len(rows), end, err)
	}
	n, end, err := arrowAnswer(t, r.call("t-alice", holdingsAt, body, "Accept", protocol.MediaArrowStream))
	if n != 600 || codeOf(err) != protocol.CodeInternal || end == nil {
		t.Fatalf("Arrow, the panic inside the first batch: %d rows, %+v, %v", n, end, err)
	}
	r.provide(panicking(4500))
	n, end, err = arrowAnswer(t, r.call("t-alice", holdingsAt, body, "Accept", protocol.MediaArrowStream))
	if n != 4500 || codeOf(err) != protocol.CodeInternal || end == nil {
		t.Fatalf("Arrow, the panic after a batch: %d rows, %+v, %v", n, end, err)
	}

	// The edge's own steps are guarded too: a panic in one is internal,
	// logged with its request id and stack.
	req := r.request("t-alice", listAt, `{}`)
	x := &exchange{id: "req-guard"}
	req = req.WithContext(context.WithValue(req.Context(), exchangeKey{}, x))
	_, err = guarded(r.edge, req, func() (*ijson.Object, error) { panic("the edge's own bug") })
	if codeOf(err) != protocol.CodeInternal || x.failed == nil || !strings.Contains(r.log.String(), "the edge's own bug") || !strings.Contains(r.log.String(), "req-guard") {
		t.Fatalf("a panic in the edge's own step: %v; %s", err, r.log.String())
	}
}

// R128, §9.6.3, §6.7.2 row 4, constitution rule 14: the caller's
// traceparent is continued: the answer carries the gateway's span of the
// same trace, the access log its trace id, and the provider gets it in its
// context. A traceparent that does not parse is absent, and a new trace
// starts.
func TestR128TraceContextIsContinued(t *testing.T) {
	r := newRig(t, rigOptions{})
	var seen string
	r.provide(func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		if tp, ok := telemetry.TraceFrom(ctx); ok {
			seen = tp.TraceIDString()
		}
		return r.standard(ctx, inv)
	})
	const in = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	a := r.call("t-alice", listAt, `{}`, "traceparent", in, "tracestate", "atlas=1,other=2")
	tp, err := protocol.ParseTraceparent(a.header.Get("traceparent"))
	if err != nil || tp.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" || tp.ParentID == "00f067aa0ba902b7" || tp.Flags != 1 || a.header.Get("tracestate") != "atlas=1,other=2" {
		t.Fatalf("the answer's traceparent %q (%v), tracestate %q", a.header.Get("traceparent"), err, a.header.Get("tracestate"))
	}
	if seen != tp.TraceID {
		t.Fatalf("the provider's trace %q", seen)
	}
	logged := false
	for _, line := range r.log.lines("call") {
		if line["request_id"] == a.header.Get(protocol.HeaderRequestID) {
			logged = line["trace_id"] == tp.TraceID
		}
	}
	if !logged {
		t.Fatalf("the access log has no trace id: %s", r.log.String())
	}
	for _, bad := range []string{"00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01", "00-00000000000000000000000000000000-00f067aa0ba902b7-01", "garbage"} {
		a := r.call("t-alice", listAt, `{}`, "traceparent", bad, "tracestate", "atlas=1")
		tp, err := protocol.ParseTraceparent(a.header.Get("traceparent"))
		if err != nil || strings.EqualFold(tp.TraceID, "4bf92f3577b34da6a3ce929d0e0e4736") || a.header.Get("tracestate") != "" {
			t.Fatalf("%q: a fresh trace %q, tracestate %q", bad, a.header.Get("traceparent"), a.header.Get("tracestate"))
		}
	}
	if a := r.call("t-alice", listAt, `{}`); a.header.Get("traceparent") == "" {
		t.Fatal("a call without a traceparent starts a trace")
	}
}

// R137, §9.6.3 (a header that does not parse as its type is absent),
// Q-HDR-1, §6.7.8: a malformed Idempotency-Key on a read is absent, never
// a refusal.
func TestR137MalformedIdempotencyKeyIsAbsent(t *testing.T) {
	r := newRig(t, rigOptions{})
	for _, key := range []string{"run_2026.10.31", `"` + strings.Repeat("k", 256) + `"`, `"a b"`, `?1`} {
		if a := r.call("t-alice", listAt, `{}`, protocol.HeaderIdempotencyKey, key); a.status != 200 {
			t.Fatalf("%q: %d %s", key, a.status, a.body)
		}
	}
	req := r.request("t-alice", listAt, `{}`, protocol.HeaderIdempotencyKey, "token-not-string")
	if c := r.edge.read(req, "id", ijson.NewObject()); c.IdempotencyKey != "" {
		t.Fatalf("a Token key was read as %q", c.IdempotencyKey)
	}
	req = r.request("t-alice", listAt, `{}`, protocol.HeaderIdempotencyKey, `"run-1"`)
	if c := r.edge.read(req, "id", ijson.NewObject()); c.IdempotencyKey != "run-1" {
		t.Fatalf("a well-formed key was read as %q", c.IdempotencyKey)
	}
}

// interaction is a profile interaction with one https binding at at.
func interaction(delivery, result string, encodings ...string) *profile.Interaction {
	return &profile.Interaction{Name: "fetch", Delivery: delivery, Result: result, Bindings: []profile.Binding{{Protocol: "https", Address: "/at", Encodings: encodings}}}
}

func features(t testing.TB, f string) protocol.Features {
	t.Helper()
	if f == "" {
		return protocol.CoreFeatures()
	}
	out, err := protocol.ParseFeatures(f)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// R131, §9.B, §9.C, §9.6.4, §9.6.5, A-005, A-152: the encoding is chosen
// from what the binding lists, what the edge frames for the delivery, and
// what the client's levels let it read, in Accept's order; a client that
// lacks a level is served the next way down, never refused.
func TestR131NegotiationRespectsDeclaredFeatures(t *testing.T) {
	holdings := interaction("chunked", "table", "ndjson", "json", "arrow")
	for _, c := range []struct {
		name     string
		in       *profile.Interaction
		features string
		accept   []string
		want     string // a media type, or a problem code
	}{
		{"a Core client asking */*", holdings, "", []string{"*/*"}, protocol.MediaJSON},
		{"a Core client asking nothing", holdings, "", nil, protocol.MediaJSON},
		{"a Core client asking for Arrow", holdings, "", []string{protocol.MediaArrowStream}, protocol.MediaJSON},
		{"no Columnar, asking Arrow then NDJSON", holdings, "levels=(core streaming)", []string{protocol.MediaArrowStream + ", " + protocol.MediaNDJSON}, protocol.MediaNDJSON},
		{"no Columnar, asking Arrow", holdings, "levels=(core streaming)", []string{protocol.MediaArrowStream}, protocol.MediaNDJSON},
		{"Columnar, asking Arrow", holdings, fullFeatures, []string{protocol.MediaArrowStream}, protocol.MediaArrowStream},
		{"a declared list without Arrow", holdings, "levels=(core streaming columnar), encodings=(json ndjson)", []string{protocol.MediaArrowStream}, protocol.MediaNDJSON},
		{"application/*", holdings, fullFeatures, []string{"application/*"}, protocol.MediaNDJSON},
		{"Accept over two field lines", holdings, fullFeatures, []string{"text/html", protocol.MediaArrowStream}, protocol.MediaArrowStream},
		{"q values ignored", holdings, fullFeatures, []string{protocol.MediaJSON + ";q=0.1, " + protocol.MediaNDJSON + ";q=1"}, protocol.MediaJSON},
		{"nothing it serves", holdings, fullFeatures, []string{"text/csv"}, protocol.CodeInvalidRequest},
		{"an Accept that is no media range", holdings, fullFeatures, []string{";;;"}, protocol.CodeInvalidRequest},
		{"a single table", interaction("single", "table", "json"), fullFeatures, []string{protocol.MediaNDJSON}, protocol.CodeInvalidRequest},
		{"a single table, */*", interaction("single", "table", "json"), fullFeatures, nil, protocol.MediaJSON},
		{"a binding that lists csv, which the edge does not frame", interaction("chunked", "table", "csv", "json"), fullFeatures, []string{"text/csv"}, protocol.CodeInvalidRequest},
		{"only encodings the edge does not frame", interaction("chunked", "table", "csv", "parquet"), fullFeatures, nil, protocol.CodeInternal},
		{"an operation", interaction("operation", "record", "json"), fullFeatures, nil, protocol.CodeNotInEnvironment},
		{"a subscription", interaction("subscription", "record", "json"), fullFeatures, nil, protocol.CodeNotInEnvironment},
	} {
		h := http.Header{}
		for _, a := range c.accept {
			h.Add("Accept", a)
		}
		ch, prob := choose(h, features(t, c.features), c.in, "/at")
		got := ch.media
		if prob != nil {
			got = prob.Code
		}
		if got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
		if prob != nil && prob.Code == protocol.CodeInvalidRequest && (prob.Status != http.StatusNotAcceptable || prob.Target != "Accept") {
			t.Errorf("%s: %d %s", c.name, prob.Status, prob.Target)
		}
	}

	// Over HTTP: a Core client calling holdings, as curl or a browser
	// would, with */*, gets a JSON array, not NDJSON it cannot read.
	r := newRig(t, rigOptions{})
	a := r.call("t-alice", holdingsAt, `{"portfolios": ["147HYC"]}`, protocol.HeaderClientFeatures, "", "Accept", "*/*")
	if a.status != 200 || a.header.Get("Content-Type") != protocol.MediaJSON || !strings.HasPrefix(string(a.body), "[") {
		t.Fatalf("a Core client: %d %s %s", a.status, a.header.Get("Content-Type"), a.body)
	}
}
