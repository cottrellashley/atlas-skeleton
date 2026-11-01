package edge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"atlas.local/gateway/core/pipeline"
	"atlas.local/gateway/core/registry"
	"atlas.local/gateway/fakes"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/clock"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/documents"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/profile"
)

// The rig is the real pipeline and registry over the journey's own
// profiles, read from their services' files, behind the edge: fakes at
// every port, and a provider the test scripts, so the edge is tested at
// the seam a real provider uses (AGENTS.md: never fake below the code
// under test).

const (
	holdingsAt = "/v1/data/portfolio.holdings@v1:fetch"
	listAt     = "/v1/data/portfolio.list@v1:fetch"
	secretAt   = "/v1/data/portfolio.secret@v1:fetch"
	legacyAt   = "/v1/data/portfolio.legacy@v1:fetch"
	nowhereAt  = "/v1/data/portfolio.nowhere@v1:fetch"
)

// serve is a provider written as a function.
type serve func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error)

func (f serve) Serve(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
	return f(ctx, inv)
}

// people are the rig's callers: an analyst of four portfolios, one of one,
// one who holds nothing.
var people = map[string]ports.Identity{
	"t-alice": {Principal: "alice", Name: "Alice", Kind: "person", Groups: []string{"credit"}},
	"t-bob":   {Principal: "bob", Name: "Bob", Kind: "person", Groups: []string{"hy"}},
	"t-carol": {Principal: "carol", Name: "Carol", Kind: "person"},
}

func testPolicy(limits ...ports.Limit) *ports.Policy {
	return &ports.Policy{
		Revision: "r1",
		Roles:    map[string][]string{"credit": {"analyst"}, "hy": {"analyst"}},
		Grants:   []ports.Grant{{ID: "grant-read", Role: "analyst", Capabilities: []string{"portfolio.holdings@v1", "portfolio.list@v1", "portfolio.secret@v1", "portfolio.legacy@v1", "portfolio.moved@v1", "portfolio.echo@v1"}}},
		Scopes: map[string]ports.Scope{
			"credit": {"portfolio": {"147HYC", "GLOBAL-CREDIT", "EM-RATES", "EURO-IG"}},
			"hy":     {"portfolio": {"147HYC"}},
		},
		Limits: limits,
	}
}

// authenticator is the fake token issuer, counting the calls that reach it.
type authenticator struct {
	tokens *fakes.Tokens
	calls  atomic.Int64
}

func (a *authenticator) Authenticate(ctx context.Context, token string) (ports.Identity, error) {
	a.calls.Add(1)
	return a.tokens.Authenticate(ctx, token)
}

type rig struct {
	t     testing.TB
	edge  *Server
	pipe  *pipeline.Pipeline
	reg   *registry.Registry
	audit *fakes.Audit
	auth  *authenticator
	clock clock.Clock
	srv   *httptest.Server
	log   *logBuffer

	mu    sync.Mutex
	serve serve
}

// rigOptions shape a rig: the clock, the edge's serving settings, the
// policy's limits.
type rigOptions struct {
	clock   clock.Clock
	serving Serving
	limits  []ports.Limit
	maxBody int64
	// noServer leaves the rig without an HTTP server, for a test that
	// serves the edge itself.
	noServer bool
}

func newRig(t testing.TB, o rigOptions) *rig {
	t.Helper()
	if o.clock == nil {
		o.clock = clock.NewManual(time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))
	}
	r := &rig{t: t, reg: registry.New(), audit: &fakes.Audit{}, clock: o.clock, log: &logBuffer{}}
	tokens := fakes.NewTokens(o.clock)
	for token, id := range people {
		tokens.Issue(token, id, time.Time{})
	}
	r.auth = &authenticator{tokens: tokens}
	r.serve = r.standard
	provider := serve(func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		r.mu.Lock()
		f := r.serve
		r.mu.Unlock()
		return f(ctx, inv)
	})
	var entries []registry.Entry
	for _, chain := range [][]*profile.Profile{
		loadChain(t, "portfolio.holdings@v1", nil),
		loadChain(t, "portfolio.list@v1", nil),
		loadChain(t, "portfolio.list@v1", rename("portfolio.secret", func(props *ijson.Object) { props.Set("visibility", "confidential") })),
		loadChain(t, "portfolio.list@v1", rename("portfolio.legacy", func(props *ijson.Object) {
			props.Set("lifecycle", "deprecated")
			props.Set("deprecated", "2026-10-01")
			props.Set("sunset", "2027-01-31")
		})),
	} {
		entries = append(entries, registry.Entry{Profiles: chain, Provider: provider})
	}
	if err := r.reg.Register(entries...); err != nil {
		t.Fatal(err)
	}
	var err error
	r.pipe, err = pipeline.New(pipeline.Options{Environment: "local", Registry: r.reg, Authenticator: r.auth, Policy: fakes.NewPolicy(testPolicy(o.limits...)), Audit: r.audit, Clock: o.clock})
	if err != nil {
		t.Fatal(err)
	}
	r.edge, err = Open(Options{
		Pipeline: r.pipe, URL: "http://gateway.test", Clock: o.clock, MaxBody: o.maxBody,
		SignIn: SignIn{Authority: "http://gateway.test", ClientID: "atlas-test", Scopes: []string{"api://atlas/.default"}, TokenEndpoint: "http://gateway.test/oauth/token"},
		Log:    slog.New(slog.NewJSONHandler(r.log, nil)), Serving: o.serving,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !o.noServer {
		r.srv = httptest.NewServer(r.edge)
		t.Cleanup(r.srv.Close)
	}
	return r
}

// provide scripts the provider.
func (r *rig) provide(f serve) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.serve = f
}

// standard answers the journey's capabilities from generated rows:
// holdings four rows a portfolio, a list row for each portfolio in scope.
func (r *rig) standard(_ context.Context, inv *ports.Invocation) (*ports.Answer, error) {
	switch inv.Capability {
	case "portfolio.holdings@v1":
		return &ports.Answer{Rows: ports.SliceRows(holdingRows(requested(inv), 4))}, nil
	default:
		var rows []*ijson.Object
		for _, code := range inv.Scope["portfolio"] {
			rows = append(rows, listRow(code))
		}
		return &ports.Answer{Rows: ports.SliceRows(rows)}, nil
	}
}

// requested are the portfolios a holdings request names.
func requested(inv *ports.Invocation) []string {
	v, _ := inv.Request.Get("portfolios")
	var out []string
	for _, p := range v.([]ijson.Value) {
		out = append(out, p.(string))
	}
	return out
}

func listRow(code string) *ijson.Object {
	o := ijson.NewObject()
	o.Set("code", code)
	o.Set("name", "Portfolio "+code)
	o.Set("region", "emea")
	o.Set("asset_class", "credit")
	o.Set("base_currency", "USD")
	o.Set("status", "active")
	return o
}

func holdingRow(portfolio string, i int) *ijson.Object {
	o := ijson.NewObject()
	o.Set("portfolio", portfolio)
	o.Set("security_id", fmt.Sprintf("S-%06d", i))
	o.Set("as_of", "2026-10-30")
	o.Set("isin", nil)
	o.Set("title", nil)
	o.Set("asset_class", "credit")
	o.Set("quantity", "1000.500000")
	mv := ijson.NewObject()
	mv.Set("amount", "1014852.172500")
	mv.Set("currency", "USD")
	o.Set("market_value", mv)
	o.Set("weight", ijson.Number("0.25"))
	o.Set("oas", nil)
	o.Set("status", "open")
	return o
}

func holdingRows(portfolios []string, each int) []*ijson.Object {
	var rows []*ijson.Object
	for _, p := range portfolios {
		for i := range each {
			rows = append(rows, holdingRow(p, i))
		}
	}
	return rows
}

// lazy is a row stream made a row at a time: n rows of make, with a hook
// before each, so a test can fail, block or panic a stream mid-way.
type lazy struct {
	n      int
	i      int
	make   func(i int) *ijson.Object
	before func(ctx context.Context, i int) error
	closed atomic.Bool
}

func (l *lazy) Next(ctx context.Context) (*ijson.Object, error) {
	if l.i >= l.n {
		return nil, io.EOF
	}
	if l.before != nil {
		if err := l.before(ctx, l.i); err != nil {
			return nil, err
		}
	}
	l.i++
	return l.make(l.i - 1), nil
}

func (l *lazy) Close() error { l.closed.Store(true); return nil }

// rename makes a variant of a profile under another id, its binding
// following it, with its properties changed by edit.
func rename(id string, edit func(props *ijson.Object)) func(doc *ijson.Object) {
	return func(doc *ijson.Object) {
		ident, _ := doc.Get("identity")
		old, _ := ident.(*ijson.Object).Get("id")
		ident.(*ijson.Object).Set("id", id)
		props, _ := doc.Get("properties")
		edit(props.(*ijson.Object))
		b, _ := ijson.Marshal(doc)
		v, _ := ijson.Parse(bytes.ReplaceAll(b, []byte(old.(string)+"@v1"), []byte(id+"@v1")))
		for _, name := range v.(*ijson.Object).Names() {
			x, _ := v.(*ijson.Object).Get(name)
			doc.Set(name, x)
		}
	}
}

// loadChain is a portfolio capability's profiles within its version,
// oldest first, as its service keeps them: the one served, and before it
// each profile its history holds that the served one's chain reaches. With
// edit, it is instead a capability of its own made from the chain's first
// profile, changed by edit.
func loadChain(t testing.TB, name string, edit func(doc *ijson.Object)) []*profile.Profile {
	t.Helper()
	dir := filepath.Join("..", "svc", "portfolio")
	cur := loadProfile(t, filepath.Join(dir, "profiles", name+".json"), nil)
	files, err := filepath.Glob(filepath.Join(dir, "history", name, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	held := map[string]string{}
	for _, f := range files {
		held[loadProfile(t, f, nil).Exact().String()] = f
	}
	chain, paths := []*profile.Profile{cur}, []string{filepath.Join(dir, "profiles", name+".json")}
	for p := cur; p.Evolution.PreviousProfile != nil; {
		f, ok := held[p.Evolution.PreviousProfile.String()]
		if !ok {
			t.Fatalf("%s names %s, which its history does not hold", p.Exact(), p.Evolution.PreviousProfile)
		}
		p = loadProfile(t, f, nil)
		chain, paths = append([]*profile.Profile{p}, chain...), append([]string{f}, paths...)
	}
	if edit != nil {
		return []*profile.Profile{loadProfile(t, paths[0], edit)}
	}
	return chain
}

// loadProfile reads a profile's file, changed by edit when one is given.
func loadProfile(t testing.TB, path string, edit func(doc *ijson.Object)) *profile.Profile {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		v, err := ijson.Parse(b)
		if err != nil {
			t.Fatal(err)
		}
		edit(v.(*ijson.Object))
		if b, err = ijson.Marshal(v); err != nil {
			t.Fatal(err)
		}
	}
	p, err := registry.Load(b)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return p
}

// The client headers a full client sends.
const (
	client       = `name="edge-test", version="1.0.0", protocol="atlas/1.0"`
	fullFeatures = "levels=(core streaming columnar), deliveries=(single chunked operation), encodings=(json ndjson arrow)"
)

type answer struct {
	status int
	header http.Header
	body   []byte
	// close is Connection: close, which the client takes out of header.
	close bool
}

// request builds a call: a POST with a full client's headers, the token
// as the bearer, and headers given as name, value pairs (an empty value
// removes the header).
func (r *rig) request(token, path, body string, headers ...string) *http.Request {
	r.t.Helper()
	req, err := http.NewRequest(http.MethodPost, r.srv.URL+path, strings.NewReader(body))
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(protocol.HeaderClient, client)
	req.Header.Set(protocol.HeaderClientFeatures, fullFeatures)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i+1] == "" {
			req.Header.Del(headers[i])
		} else {
			req.Header.Add(headers[i], headers[i+1])
		}
	}
	return req
}

func (r *rig) do(req *http.Request) answer {
	r.t.Helper()
	res, err := r.srv.Client().Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		r.t.Fatal(err)
	}
	if res.Header.Get(protocol.HeaderRequestID) == "" {
		r.t.Fatalf("no X-Request-Id on %s %s", req.Method, req.URL.Path)
	}
	return answer{status: res.StatusCode, header: res.Header, body: b, close: res.Close}
}

func (r *rig) call(token, path, body string, headers ...string) answer {
	r.t.Helper()
	return r.do(r.request(token, path, body, headers...))
}

// problem is an answer's problem document, held to problem.schema.json and
// to its Atlas-Error-Code and X-Request-Id.
func (a answer) problem(t testing.TB) *protocol.Problem {
	t.Helper()
	v, err := ijson.Parse(a.body)
	if err != nil {
		t.Fatalf("%d %s: %v", a.status, a.body, err)
	}
	if fs := documents.Validate("problem", v); len(fs) > 0 {
		t.Fatalf("not a problem document: %v\n%s", fs, a.body)
	}
	var p protocol.Problem
	if err := json.Unmarshal(a.body, &p); err != nil {
		t.Fatal(err)
	}
	if a.header.Get(protocol.HeaderErrorCode) != p.Code || a.header.Get("Content-Type") != protocol.MediaProblem {
		t.Fatalf("Atlas-Error-Code %q, Content-Type %q, code %s", a.header.Get(protocol.HeaderErrorCode), a.header.Get("Content-Type"), p.Code)
	}
	if p.RequestID != a.header.Get(protocol.HeaderRequestID) {
		t.Fatalf("request_id %q, X-Request-Id %q", p.RequestID, a.header.Get(protocol.HeaderRequestID))
	}
	if p.Status != a.status {
		t.Fatalf("status %d, the problem's %d", a.status, p.Status)
	}
	return &p
}

// facts are the audit facts of an answered call, by kind.
func (r *rig) facts(a answer, kind ports.FactKind) []ports.Fact {
	var out []ports.Fact
	for _, f := range r.audit.Of(a.header.Get(protocol.HeaderRequestID)) {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

// logBuffer collects the edge's log lines, one JSON object each.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// lines are the log's records with the message msg.
func (l *logBuffer) lines(msg string) []map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]any
	for _, line := range bytes.Split(l.buf.Bytes(), []byte("\n")) {
		var m map[string]any
		if json.Unmarshal(line, &m) == nil && m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}
