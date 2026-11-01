package assemble

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"atlas.local/gateway/edge"
	"atlas.local/gateway/fakes"
	"atlas.local/gateway/fixtures"
	"atlas.local/gateway/ports"
	"atlas.local/gateway/svc/datastatus"
	"atlas.local/gateway/svc/portfolio"
	"atlas.local/gateway/views"
	"atlas.local/sdk/go/kit/clock"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/documents"
	"atlas.local/sdk/go/protocol/frame"
	"atlas.local/sdk/go/protocol/ijson"
)

type rig struct {
	t      *testing.T
	gw     *Gateway
	srv    *httptest.Server
	audit  *fakes.Audit
	source *fakes.DataSource
	clock  *clock.Manual
}

func newRig(t *testing.T, exclude ...string) *rig {
	t.Helper()
	tables, err := fixtures.Harbour()
	if err != nil {
		t.Fatal(err)
	}
	pol, err := fixtures.Policy()
	if err != nil {
		t.Fatal(err)
	}
	ids, err := fixtures.Identities()
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, audit: &fakes.Audit{}, source: fakes.NewDataSource(tables...), clock: clock.NewManual(time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))}
	tokens := fakes.NewTokens(r.clock)
	for _, id := range ids {
		tokens.Issue(id.Token, id.Identity, time.Time{})
	}
	tokens.Issue("dev-expired", ids[0].Identity, r.clock.Now().Add(-time.Minute))
	r.srv = httptest.NewUnstartedServer(nil)
	url := "http://" + r.srv.Listener.Addr().String()
	r.gw, err = New(Parts{
		Environment: "local", URL: url,
		SignIn:        edge.SignIn{Authority: url, ClientID: "atlas-cli", Scopes: []string{"api://atlas/.default"}, TokenEndpoint: url + "/oauth/token"},
		Authenticator: tokens, Policy: fakes.NewPolicy(pol), Audit: r.audit, DataSource: r.source, Clock: r.clock,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Domains: []Domain{portfolio.Capabilities, datastatus.Capabilities}, Exclude: exclude,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.srv.Config.Handler = r.gw.Edge
	r.srv.Start()
	t.Cleanup(r.srv.Close)
	return r
}

type answer struct {
	status int
	header http.Header
	body   []byte
}

func (a answer) json(t *testing.T) *ijson.Object {
	t.Helper()
	v, err := ijson.Parse(a.body)
	if err != nil {
		t.Fatalf("%d %s: %v", a.status, a.body, err)
	}
	return v.(*ijson.Object)
}

func (a answer) problem(t *testing.T) (code, target string) {
	t.Helper()
	o := a.json(t)
	if fs := documents.Validate("problem", o); len(fs) > 0 {
		t.Fatalf("not a problem document: %v\n%s", fs, a.body)
	}
	c, _ := o.Get("code")
	tg, _ := o.Get("target")
	s, _ := tg.(string)
	if a.header.Get(protocol.HeaderErrorCode) != c {
		t.Fatalf("Atlas-Error-Code %q, code %v", a.header.Get(protocol.HeaderErrorCode), c)
	}
	return c.(string), s
}

const client = `name="journey-test", version="1.0.0", protocol="atlas/1.0"`

func (r *rig) call(token, path, body string, headers ...string) answer {
	r.t.Helper()
	req, err := http.NewRequest(http.MethodPost, r.srv.URL+path, strings.NewReader(body))
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(protocol.HeaderClient, client)
	req.Header.Set(protocol.HeaderClientFeatures, "levels=(core streaming columnar), deliveries=(single chunked operation), encodings=(json ndjson arrow)")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i+1] == "" {
			req.Header.Del(headers[i])
		} else {
			req.Header.Set(headers[i], headers[i+1])
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.Header.Get(protocol.HeaderRequestID) == "" {
		r.t.Fatalf("no X-Request-Id on %s", path)
	}
	return answer{res.StatusCode, res.Header, b}
}

func holdings(t *testing.T, a answer) ([]*ijson.Object, *frame.End) {
	t.Helper()
	if a.status != 200 {
		t.Fatalf("%d %s", a.status, a.body)
	}
	rd := frame.NewNDJSONReader(bytes.NewReader(a.body))
	var rows []*ijson.Object
	for {
		row, err := rd.Next()
		if err == io.EOF {
			return rows, rd.End()
		}
		if err != nil {
			t.Fatalf("%v\n%s", err, a.body)
		}
		rows = append(rows, row)
	}
}

func str(o *ijson.Object, path ...string) string {
	var v ijson.Value = o
	for _, p := range path {
		v, _ = v.(*ijson.Object).Get(p)
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := ijson.Marshal(v)
	return string(b)
}

func TestBootstrap(t *testing.T) {
	r := newRig(t)
	res, err := http.Get(r.srv.URL + protocol.WellKnownPath)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	v, err := ijson.Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	if fs := documents.Validate("bootstrap", v); len(fs) > 0 {
		t.Fatalf("AP-DOC-001: the bootstrap: %v\n%s", fs, body)
	}
	etag := res.Header.Get("ETag")
	req, _ := http.NewRequest(http.MethodGet, r.srv.URL+protocol.WellKnownPath, nil)
	req.Header.Set("If-None-Match", etag)
	res2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if !strings.HasPrefix(etag, `"`) || res2.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidation: %q %d", etag, res2.StatusCode)
	}
}

func TestDiscovery(t *testing.T) {
	r := newRig(t)
	who := r.call("dev-alice", "/v1/atlas/atlas.whoami:fetch", `{}`).json(t)
	if str(who, "principal") != "a11ce000-0000-4000-8000-000000000001" || str(who, "available_domains") != `["data","portfolio"]` || !strings.Contains(str(who, "house_rules"), "confidential") {
		t.Fatalf("whoami: %s", str(who))
	}
	cat := r.call("dev-alice", "/v1/atlas/atlas.catalog:fetch", `{"page_size": 2}`)
	page := cat.json(t)
	if fs := documents.Validate("catalog-page", page); len(fs) > 0 {
		t.Fatalf("catalog page: %v\n%s", fs, cat.body)
	}
	var ids []string
	for {
		items, _ := page.Get("items")
		for _, it := range items.([]ijson.Value) {
			ids = append(ids, str(it.(*ijson.Object), "id"))
		}
		next, _ := page.Get("next_cursor")
		if next == nil {
			break
		}
		page = r.call("dev-alice", "/v1/atlas/atlas.catalog:fetch", `{"page_size": 2, "cursor": "`+next.(string)+`"}`).json(t)
	}
	if want := []string{"data.status", "portfolio.holdings", "portfolio.holdings-dates", "portfolio.list", "portfolio.search"}; !slices.Equal(ids, want) {
		t.Fatalf("catalogue %v", ids)
	}
	if code, target := r.call("dev-bob", "/v1/atlas/atlas.catalog:fetch", `{"page_size": 2, "cursor": "`+str(r.call("dev-alice", "/v1/atlas/atlas.catalog:fetch", `{"page_size": 2}`).json(t), "next_cursor")+`"}`).problem(t); code != "invalid_request" || target != "/cursor" {
		t.Fatalf("a cursor is the caller's own: %s %s", code, target)
	}
	carol := r.call("dev-carol", "/v1/atlas/atlas.catalog:fetch", `{}`).json(t)
	if str(carol, "items") != "[]" {
		t.Fatalf("carol holds nothing: %s", str(carol))
	}
	found := r.call("dev-alice", "/v1/atlas/atlas.search:fetch", `{"words": "portfolio holdings dates"}`).json(t)
	items, _ := found.Get("items")
	if first := items.([]ijson.Value)[0].(*ijson.Object); str(first, "id") != "portfolio.holdings-dates" {
		t.Fatalf("search: %s", str(found))
	}
	// R078: the handbook's query (CAPABILITY-HANDBOOK, "one journey") finds the capability it means.
	hb := r.call("dev-alice", "/v1/atlas/atlas.search:fetch", `{"words": "holdings and weights for a portfolio"}`).json(t)
	if ranked, _ := hb.Get("items"); len(ranked.([]ijson.Value)) == 0 || str(ranked.([]ijson.Value)[0].(*ijson.Object), "id") != "portfolio.holdings" {
		t.Fatalf("R078: the handbook's search: %s", str(hb))
	}
	d := r.call("dev-alice", "/v1/atlas/atlas.describe:fetch", `{"by": "id", "id": "portfolio.holdings@v1"}`)
	desc := d.json(t)
	if fs := documents.Validate("describe", desc); len(fs) > 0 {
		t.Fatalf("describe: %v", fs)
	}
	// R075: held_by is how the caller holds it (governance.schema.json), never a policy's own grant id.
	if fp := str(desc, "profile", "fingerprint"); fp == "" || str(desc, "governance", "for_caller", "interactions", "fetch", "held_by") != "role:portfolio-analyst" {
		t.Fatalf("describe: %s", str(desc, "governance"))
	}
	if code, _ := r.call("dev-carol", "/v1/atlas/atlas.describe:fetch", `{"by": "id", "id": "nothing.here@v1"}`).problem(t); code != "not_found" {
		t.Fatal(code)
	}
	g := r.call("dev-alice", "/v1/atlas/atlas.guide:fetch", `{"by": "id", "id": "portfolio.holdings", "client": "python"}`).json(t)
	if md := str(g, "markdown"); !strings.Contains(md, `atlas.call("portfolio.holdings@v1:fetch", {"portfolios": ["147HYC"]})`) || !strings.Contains(md, "| `weight` | Fraction") {
		t.Fatalf("guide:\n%s", md)
	}
	acc := r.call("dev-dave", "/v1/atlas/atlas.access:fetch", `{"id": "portfolio.holdings@v1"}`).json(t)
	if !strings.Contains(str(acc, "items"), `"status":"denied"`) {
		t.Fatalf("access: %s", str(acc))
	}
}

// The acceptance result's third point: a second person with narrower scope
// receives a narrower result, or a clear refusal (R05).
func TestTwoIdentitiesTwoScopes(t *testing.T) {
	r := newRig(t)
	rows, end := holdings(t, r.call("dev-alice", "/v1/data/portfolio.holdings@v1:fetch", `{"portfolios": ["147HYC", "GLOBAL-CREDIT"]}`, "Accept", protocol.MediaNDJSON))
	if len(rows) != 8 || end.Rows != 8 || end.Problem != nil {
		t.Fatalf("alice: %d rows, end %+v", len(rows), end)
	}
	if str(rows[0], "portfolio") != "147HYC" || str(rows[0], "as_of") != "2026-10-30" || str(rows[4], "as_of") != "2026-10-29" {
		t.Fatalf("each portfolio from its own latest valuation: %s %s", str(rows[0]), str(rows[4]))
	}
	for _, row := range rows {
		if strings.HasPrefix(str(row, "security_id"), "XS-CORP-BETA") && (!strings.HasPrefix(str(row, "quantity"), "-") || !strings.HasPrefix(str(row, "market_value", "amount"), "-")) {
			t.Fatalf("a short is negative: %s", str(row))
		}
	}
	// The fixture's own value, written at Money's scale: 1000500 × 1.014345.
	acme := rows[slices.IndexFunc(rows, func(o *ijson.Object) bool { return str(o, "security_id") == "US-HY-ACME-29" })]
	if str(acme, "market_value", "amount") != "1014852.172500" || str(acme, "quantity") != "1000500.000000" {
		t.Fatalf("an exact decimal at its scale: %s", str(acme))
	}
	if code, target := r.call("dev-bob", "/v1/data/portfolio.holdings@v1:fetch", `{"portfolios": ["147HYC", "GLOBAL-CREDIT"]}`).problem(t); code != "not_granted" || target != "/portfolios/1" {
		t.Fatalf("bob: %s %s", code, target)
	}
	bob := r.call("dev-bob", "/v1/data/portfolio.list@v1:fetch", `{}`)
	if bob.status != 200 || strings.Count(string(bob.body), `"code"`) != 1 || !strings.Contains(string(bob.body), `"code":"147HYC"`) {
		t.Fatalf("bob's list is his scope: %s", bob.body)
	}
	alice := r.call("dev-alice", "/v1/data/portfolio.list@v1:fetch", `{"asset_class": "credit"}`)
	if strings.Count(string(alice.body), `"code"`) != 3 {
		t.Fatalf("alice's credit portfolios: %s", alice.body)
	}
	if code, _ := r.call("dev-dave", "/v1/data/portfolio.holdings@v1:fetch", `{"portfolios": ["147HYC"]}`).problem(t); code != "denied" {
		t.Fatal(code)
	}
	if code, _ := r.call("dev-carol", "/v1/data/portfolio.holdings@v1:fetch", `{"portfolios": ["147HYC"]}`).problem(t); code != "not_granted" {
		t.Fatal(code)
	}
	search := r.call("dev-alice", "/v1/data/portfolio.search@v1:fetch", `{"words": "global credit"}`)
	if !strings.HasPrefix(string(search.body), `[{"code":"GLOBAL-CREDIT"`) {
		t.Fatalf("search: %s", search.body)
	}
	if s := r.call("dev-bob", "/v1/data/portfolio.search@v1:fetch", `{"words": "global credit"}`); strings.Contains(string(s.body), "GLOBAL-CREDIT") || !strings.Contains(string(s.body), `"code":"147HYC"`) {
		t.Fatalf("relevance ranks within scope, and never widens it: %s", s.body)
	}
}

func TestRequestsAndHeaders(t *testing.T) {
	r := newRig(t)
	a := r.call("dev-alice", "/v1/data/portfolio.holdings@v1:fetch", `{"portfolios": ["147HYC"], "as_of": "2026-10-28", "columns": ["portfolio", "isin", "as_of", "weight"]}`, "Accept", protocol.MediaNDJSON)
	rows, _ := holdings(t, a)
	if len(rows) != 3 || rows[0].Len() != 4 || str(rows[0], "as_of") != "2026-10-28" {
		t.Fatalf("projected rows on a date: %v", str(rows[0]))
	}
	if a.header.Get(protocol.HeaderProfile) == "" || a.header.Get(protocol.HeaderGovernanceRevision) != `"dev-2026-10-31.1"` || a.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("headers: %v", a.header)
	}
	for _, c := range []struct {
		name, body, code, target string
		headers                  []string
	}{
		{"a missing member", `{}`, "invalid_request", "/portfolios", nil},
		{"a column the result lacks", `{"portfolios": ["147HYC"], "columns": ["nope"]}`, "invalid_request", "/columns/0", nil},
		{"an unknown portfolio", `{"portfolios": ["NOPE"]}`, "not_granted", "/portfolios/0", nil},
		{"duplicate members", `{"portfolios": ["147HYC"], "portfolios": []}`, "invalid_document", "", nil},
		{"no token", `{"portfolios": ["147HYC"]}`, "unauthenticated", "", []string{"Authorization", ""}},
		{"an old client", `{"portfolios": ["147HYC"]}`, "upgrade_required", "Atlas-Client", []string{protocol.HeaderClient, `name="old", version="0.1", protocol="atlas/0.9"`}},
		{"an Accept the binding does not serve", `{"portfolios": ["147HYC"]}`, "invalid_request", "Accept", []string{"Accept", "text/csv"}},
		{"a deadline already passed", `{"portfolios": ["147HYC"]}`, "deadline_exceeded", "Atlas-Deadline", []string{protocol.HeaderDeadline, "@1000"}},
	} {
		code, target := r.call("dev-alice", "/v1/data/portfolio.holdings@v1:fetch", c.body, c.headers...).problem(t)
		if code != c.code || target != c.target {
			t.Errorf("%s: %s %q", c.name, code, target)
		}
	}
	nf := r.call("dev-nightly-risk", "/v1/data/portfolio.holdings@v1:fetch", `{"portfolios": ["NOPE"]}`)
	if code, target := nf.problem(t); code != "portfolio_not_found" || target != "/portfolios/0" || nf.status != 404 {
		t.Fatalf("a declared error: %d %s %s", nf.status, code, target)
	}
	if e := r.call("dev-expired", "/v1/atlas/atlas.whoami:fetch", `{}`); e.status != 401 || !strings.Contains(e.header.Get("WWW-Authenticate"), `error="invalid_token"`) {
		t.Fatalf("an expired token: %d %v", e.status, e.header)
	}
	if code, _ := r.call("dev-alice", "/v1/data/nothing@v1:fetch", `{}`).problem(t); code != "not_found" {
		t.Fatal(code)
	}
}

// Every attempt leaves its facts (R09): an admission, an outcome and the
// settlement of what the engine recorded, or a refusal; and an admission
// that cannot be recorded is not run.
func TestAuditFacts(t *testing.T) {
	r := newRig(t)
	ok := r.call("dev-alice", "/v1/data/portfolio.list@v1:fetch", `{}`)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	facts, _ := r.audit.Await(ctx, ok.header.Get(protocol.HeaderRequestID), 3)
	if len(facts) != 3 || facts[0].Kind != ports.Admission || facts[1].Kind != ports.Outcome || facts[1].Rows != 4 || facts[0].RequestDigest == "" || facts[0].PolicyRevision != "dev-2026-10-31.1" {
		t.Fatalf("facts %+v", facts)
	}
	if s := facts[2]; s.Kind != ports.Settlement || s.Source == nil || s.Source.QueryID == "" || s.Source.Rows != facts[1].Rows {
		t.Fatalf("the settlement holds the engine's own record of the rows it sent: %+v", s)
	}
	denied := r.call("dev-dave", "/v1/data/portfolio.holdings@v1:fetch", `{"portfolios": ["147HYC"]}`)
	facts = r.audit.Of(denied.header.Get(protocol.HeaderRequestID))
	if len(facts) != 1 || facts[0].Kind != ports.Refusal || facts[0].Code != "denied" || facts[0].Principal != "da7e0000-0000-4000-8000-000000000004" {
		t.Fatalf("refusal %+v", facts)
	}
	anon := r.call("forged", "/v1/data/portfolio.list@v1:fetch", `{}`)
	if facts = r.audit.Of(anon.header.Get(protocol.HeaderRequestID)); len(facts) != 1 || facts[0].Principal != "" || facts[0].Code != "unauthenticated" {
		t.Fatalf("a failed sign-in trusts no claimed identity: %+v", facts)
	}
	r.audit.Fail(io.ErrClosedPipe)
	before := r.source.Calls()
	if code, _ := r.call("dev-alice", "/v1/data/portfolio.list@v1:fetch", `{}`).problem(t); code != "unavailable" || r.source.Calls() != before {
		t.Fatalf("an unrecorded admission ran: %s", code)
	}
}

func TestSourceFailures(t *testing.T) {
	r := newRig(t)
	r.source.Fail(ports.ErrSourceUnavailable)
	a := r.call("dev-alice", "/v1/data/portfolio.list@v1:fetch", `{}`)
	if code, _ := a.problem(t); code != "warehouse_suspended" || a.status != 503 || a.header.Get("Retry-After") != "5" {
		t.Fatalf("%d %s %v", a.status, code, a.header)
	}
}

// The acceptance result's fourth point: a capability added after the
// clients were built is served without releasing them.
func TestACapabilityAddedLater(t *testing.T) {
	r := newRig(t, "portfolio.holdings-dates@v1")
	if code, _ := r.call("dev-alice", "/v1/data/portfolio.holdings-dates@v1:fetch", `{"portfolios": ["147HYC"]}`).problem(t); code != "not_found" {
		t.Fatal(code)
	}
	fs, err := portfolio.Capabilities()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(fs, func(f views.Files) bool { return f.Name == "portfolio.holdings-dates@v1" })
	if err := r.gw.Add(fs[i]); err != nil {
		t.Fatal(err)
	}
	a := r.call("dev-alice", "/v1/data/portfolio.holdings-dates@v1:fetch", `{"portfolios": ["147HYC"], "from": "2026-10-29"}`)
	var rows []map[string]any
	if err := json.Unmarshal(a.body, &rows); err != nil || len(rows) != 2 || rows[0]["as_of"] != "2026-10-29" || rows[1]["positions"] != 4.0 {
		t.Fatalf("%d %s", a.status, a.body)
	}
}

// The review of C1: a capability the deployment excludes is left out
// before its view model is built or checked against the warehouse, as it
// was before modules: what is withheld because it is not ready never stops
// the start.
func TestC1AnExcludedCapabilityIsNotBuilt(t *testing.T) {
	broken := func() ([]views.Files, error) {
		fs, err := portfolio.Capabilities()
		if err != nil {
			return nil, err
		}
		for i := range fs {
			if fs[i].Name == "portfolio.holdings-dates@v1" {
				fs[i].Model = []byte("not a view model")
			}
		}
		return fs, nil
	}
	pol, err := fixtures.Policy()
	if err != nil {
		t.Fatal(err)
	}
	tables, err := fixtures.Harbour()
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(Parts{
		Environment: "local", URL: "http://127.0.0.1:1",
		SignIn:        edge.SignIn{Authority: "http://127.0.0.1:1", ClientID: "atlas-cli", Scopes: []string{"api://atlas/.default"}, TokenEndpoint: "http://127.0.0.1:1/oauth/token"},
		Authenticator: fakes.NewTokens(clock.System), Policy: fakes.NewPolicy(pol), Audit: &fakes.Audit{}, DataSource: fakes.NewDataSource(tables...),
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Domains: []Domain{broken, datastatus.Capabilities}, Exclude: []string{"portfolio.holdings-dates@v1"},
	})
	if err != nil {
		t.Fatalf("an excluded capability's model stopped the start: %v", err)
	}
}

// The review of C1: the warehouse's bulkhead is the deployment's whether
// its view-model capabilities come at start or are added later.
func TestC1TheWarehouseBulkheadIsKeptForLaterCapabilities(t *testing.T) {
	got, err := upstreamsOf(Parts{WarehouseConcurrency: 3}, nil)
	if err != nil || got[views.Warehouse] != 3 {
		t.Fatalf("upstreams %v, %v", got, err)
	}
	if _, err := upstreamsOf(Parts{WarehouseConcurrency: 3}, []ports.Module{{Name: "w2", Upstream: views.Warehouse, Concurrency: 5}}); err == nil {
		t.Fatal("a module gave the warehouse another concurrency")
	}
}
