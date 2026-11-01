package edgehttp_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/gateway/adapter/snowflake"
	"github.com/jhgonline/fi-app-atlas/gateway/adapter/snowflake/snowfake"
	"github.com/jhgonline/fi-app-atlas/gateway/core/catalog"
	"github.com/jhgonline/fi-app-atlas/gateway/core/pipeline"
	edgehttp "github.com/jhgonline/fi-app-atlas/gateway/edge/http"
	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/gateway/porttest"
	"github.com/jhgonline/fi-app-atlas/gateway/svc/harbour"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

type gateway struct {
	url    string
	issuer *fakes.Issuer
	audit  *fakes.Audit
	snap   *catalog.Snapshot
	client *http.Client
	mem    memory.Allocator
	calls  int
}

func start(t *testing.T, pins map[string]int) *gateway {
	t.Helper()
	mem := memory.NewGoAllocator()
	wh := snowfake.New(mem, snowfake.Harbour(snowfake.Small)...)
	wh.ChunkRows = 11
	storage := httptest.NewServer(wh.Storage())
	t.Cleanup(storage.Close)
	wh.SetStorageURL(storage.URL)
	pub, priv, _ := ed25519.GenerateKey(nil)
	svc, err := harbour.New(snowflake.New(wh, storage.Client(), mem), pub)
	if err != nil {
		t.Fatal(err)
	}
	gov := &catalog.Governance{Revision: "g-test-1", Roles: []catalog.Role{
		{Name: "fi-pm", Members: []string{"fi-pm"}, Grants: []string{"portfolio", "index"}},
		{Name: "fi-analysts", Members: []string{"fi-analysts"}, Grants: []string{"portfolio.holdings@v1:fetch"}, RatePerMinute: 3},
	}}
	snap, err := catalog.Build([]ports.Service{svc}, pins, gov)
	if err != nil {
		t.Fatal(err)
	}
	issuer, _ := fakes.NewIssuer("https://issuer.test", "api://atlas-gateway", map[string]fakes.Persona{
		"pm":       {Subject: "pm-0001", Groups: []string{"fi-pm"}},
		"analyst":  {Subject: "analyst-0001", Groups: []string{"fi-analysts"}},
		"outsider": {Subject: "outsider-0001"},
	})
	audit := fakes.NewAudit(nil)
	edge := &edgehttp.Edge{Mem: mem, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Pipeline: &pipeline.Pipeline{
		Verifier: issuer.Verifier(), Snapshot: func() *catalog.Snapshot { return snap },
		Limits: fakes.NewLimits(), Audit: audit, Key: priv, Environment: "test", Now: time.Now,
	}, Bootstrap: protocol.Bootstrap{Protocol: protocol.Version, Environment: "test"}}
	srv := httptest.NewServer(edge.Handler())
	t.Cleanup(srv.Close)
	return &gateway{url: srv.URL, issuer: issuer, audit: audit, snap: snap, client: storage.Client(), mem: mem}
}

type answer struct {
	status int
	header http.Header
	body   []byte
}

func (g *gateway) call(t *testing.T, persona, path, body string, headers ...string) answer {
	t.Helper()
	g.calls++
	req, _ := http.NewRequest(http.MethodPost, g.url+path, strings.NewReader(body))
	if persona != "" {
		token, err := g.issuer.Token(persona)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return answer{resp.StatusCode, resp.Header, b}
}

func (a answer) problem(t *testing.T) protocol.Problem {
	t.Helper()
	var p protocol.Problem
	if err := json.Unmarshal(a.body, &p); err != nil || a.header.Get(protocol.HeaderErrorCode) != p.Code {
		t.Fatalf("not a problem document (%d): %s", a.status, a.body)
	}
	return p
}

const holdings = "/v1/data/portfolio.holdings@v1:fetch"

func TestBootstrapNeedsNoSignIn(t *testing.T) {
	g := start(t, nil)
	resp, err := http.Get(g.url + "/.well-known/atlas")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("bootstrap: %v %v", err, resp.Status)
	}
}

func TestNoTokenIsUnauthenticatedAndAudited(t *testing.T) {
	g := start(t, nil)
	a := g.call(t, "", holdings, `{"portfolios":["147HYC"]}`)
	if a.status != 401 || a.problem(t).Code != protocol.CodeUnauthenticated || a.header.Get("WWW-Authenticate") == "" {
		t.Fatalf("no token: %d %s", a.status, a.body)
	}
	if recs := g.audit.Records(); len(recs) != 1 || recs[0].RefusedAt != "authenticate" {
		t.Fatalf("audit: %+v", recs)
	}
}

// ATLAS-ACCESS-009 and -010: a caller without the grant learns nothing about
// the request's shape; one with it is told what is wrong, and spends no limit.
func TestAuthorisationComesBeforeValidation(t *testing.T) {
	g := start(t, nil)
	invalid := `{"portfolio":"147HYC"}`
	if p := g.call(t, "outsider", holdings, invalid).problem(t); p.Code != protocol.CodeNotGranted || p.Next == "" {
		t.Fatalf("outsider: %+v", p)
	}
	p := g.call(t, "analyst", holdings, invalid).problem(t)
	if p.Code != protocol.CodeInvalidRequest || p.Target == "" {
		t.Fatalf("analyst with an invalid request: %+v", p)
	}
	// The analyst's limit is 3 a minute; three invalid calls spent none of it.
	g.call(t, "analyst", holdings, invalid)
	g.call(t, "analyst", holdings, invalid)
	for i := range 3 {
		if a := g.call(t, "analyst", holdings, `{"portfolios":["147HYC"]}`); a.status != 200 {
			t.Fatalf("valid call %d: %d %s", i, a.status, a.body)
		}
	}
	if p := g.call(t, "analyst", holdings, `{"portfolios":["147HYC"]}`).problem(t); p.Code != protocol.CodeLimitExceeded || p.RetryAfter == 0 {
		t.Fatalf("the fourth valid call should hit the limit: %+v", p)
	}
	// The analyst holds fetch, not explain.
	if p := g.call(t, "analyst", "/v1/data/portfolio.holdings@v1:explain", `{"portfolios":["147HYC"]}`).problem(t); p.Code != protocol.CodeNotGranted {
		t.Fatalf("explain without its grant: %+v", p)
	}
	if recs := g.audit.Records(); len(recs) != g.calls {
		t.Fatalf("every call leaves exactly one audit record: %d records for %d calls", len(recs), g.calls)
	}
}

func TestStreamedAndHandedOffRowsAreIdentical(t *testing.T) {
	g := start(t, nil)
	body := `{"portfolios":["147HYC","200IG"]}`
	cur, _ := g.snap.Current(protocol.Pinned{ID: "portfolio.holdings", Version: 1})

	nd := g.call(t, "pm", holdings, body)
	if nd.status != 200 || nd.header.Get(protocol.HeaderProfile) != cur.Exact {
		t.Fatalf("ndjson: %d profile %q: %s", nd.status, nd.header.Get(protocol.HeaderProfile), nd.body)
	}
	lines := bytes.Split(bytes.TrimSpace(nd.body), []byte("\n"))
	var end protocol.EndLine
	if err := json.Unmarshal(lines[len(lines)-1], &end); err != nil || end.Atlas != "end" || end.Rows != int64(len(lines)-1) {
		t.Fatalf("end line: %s", lines[len(lines)-1])
	}

	ar := g.call(t, "pm", holdings, body, "Accept", protocol.MediaArrowStream)
	rd, err := ipc.NewReader(bytes.NewReader(ar.body), ipc.WithAllocator(g.mem))
	if err != nil {
		t.Fatal(err)
	}
	schema, _ := protocol.ArrowSchema(cur.Profile.Result, cur.Exact)
	streamed := porttest.ReadRows(t, rd, schema)
	rd.Release()
	if int64(len(streamed)) != end.Rows {
		t.Fatalf("arrow gave %d rows, ndjson %d", len(streamed), end.Rows)
	}
	if last := finalBatch(t, g.mem, ar.body); last == nil || last.Rows != end.Rows || last.Problem != nil {
		t.Fatalf("the arrow answer must end with its final batch (§9.6.5), got %+v", last)
	}

	features := protocol.Features{Levels: []string{"core", "columnar", "handoff"}, Handoff: []string{"snowflake-arrow/1"}}.String()
	ho := g.call(t, "pm", holdings, body, protocol.HeaderClientFeatures, features, protocol.HeaderExecution, "handoff")
	if ho.status != 200 || ho.header.Get("Content-Type") != protocol.MediaHandoff || ho.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("handoff: %d %s %s", ho.status, ho.header.Get("Content-Type"), ho.body)
	}
	var h protocol.Handoff
	if err := json.Unmarshal(ho.body, &h); err != nil {
		t.Fatal(err)
	}
	if handed := porttest.ReadHandoff(t, context.Background(), g.mem, g.client, &h, schema); !slices.Equal(handed, streamed) {
		t.Fatal("handed-off rows differ from streamed rows (ATLAS-CORE-005)")
	}

	// Only the caller whose call produced a result may renew its links.
	renew := []string{protocol.HeaderClientFeatures, features, protocol.HeaderRenew, h.QueryID}
	if p := g.call(t, "analyst", holdings, body, renew...).problem(t); p.Code != protocol.CodeNotFound {
		t.Fatalf("another caller renewed the links: %+v", p)
	}
	if a := g.call(t, "pm", holdings, body, renew...); a.status != 200 {
		t.Fatalf("renew: %d %s", a.status, a.body)
	}
}

func TestAnOldRequestIsResolvedForward(t *testing.T) {
	g := start(t, nil)
	first := firstExact(t, g)
	a := g.call(t, "pm", holdings, `{"portfolios":["147HYC"],"columns":["mv","oas"]}`, protocol.HeaderProfileExpect, first)
	if a.status != 200 {
		t.Fatalf("an old request: %d %s", a.status, a.body)
	}
	row := bytes.SplitN(a.body, []byte("\n"), 2)[0]
	if !bytes.Contains(row, []byte(`"market_value":`)) || bytes.Contains(row, []byte(`"mv":`)) {
		t.Fatalf("the gateway writes under its current profile; the client resolves: %s", row)
	}
	if bytes.Contains(row, []byte(`"sector"`)) {
		t.Fatalf("only the requested columns travel: %s", row)
	}
}

func TestPinningServesAnEarlierProfile(t *testing.T) {
	g := start(t, map[string]int{"portfolio.holdings@v1": 1})
	a := g.call(t, "pm", holdings, `{"portfolios":["147HYC"]}`)
	if row := bytes.SplitN(a.body, []byte("\n"), 2)[0]; !bytes.Contains(row, []byte(`"mv":`)) || bytes.Contains(row, []byte("Supranational")) {
		t.Fatalf("pinned to the first profile: %s", row)
	}
}

func TestTheWorkingViewShowsOnlyWhatTheCallerCanUse(t *testing.T) {
	g := start(t, nil)
	catalogOf := func(persona string) protocol.CatalogPage {
		var page protocol.CatalogPage
		json.Unmarshal(g.call(t, persona, "/v1/atlas/atlas.catalog@v1:fetch", `{}`).body, &page)
		return page
	}
	if items := catalogOf("outsider").Items; len(items) != 0 {
		t.Fatalf("the outsider sees %d capabilities", len(items))
	}
	analyst := catalogOf("analyst").Items
	if len(analyst) != 1 || analyst[0].ID != "portfolio.holdings" || !slices.Equal(analyst[0].Interactions, []string{"fetch"}) {
		t.Fatalf("the analyst should see holdings with fetch only: %+v", analyst)
	}
	if n := len(catalogOf("pm").Items); n != 2 {
		t.Fatalf("the PM should see both capabilities, sees %d", n)
	}
	var found protocol.CatalogPage
	json.Unmarshal(g.call(t, "pm", "/v1/atlas/atlas.search@v1:fetch", `{"words":"portfolio holdings market value"}`).body, &found)
	if len(found.Items) == 0 || found.Items[0].ID != "portfolio.holdings" {
		t.Fatalf("search: %+v", found.Items)
	}
	var d protocol.DescribeAnswer
	a := g.call(t, "pm", "/v1/atlas/atlas.describe@v1:fetch", `{"exact":"`+firstExact(t, g)+`"}`)
	if json.Unmarshal(a.body, &d); a.status != 200 || d.Profile.Result.Columns[3].Name != "mv" || d.Current == d.Exact {
		t.Fatalf("describe an earlier profile by exact id: %d %s", a.status, a.body)
	}
	if p := g.call(t, "outsider", "/v1/atlas/atlas.describe@v1:fetch", `{"id":"portfolio.holdings@v1"}`).problem(t); p.Code != protocol.CodeNotGranted {
		t.Fatalf("describe without a grant: %+v", p)
	}
}

func firstExact(t *testing.T, g *gateway) string {
	t.Helper()
	cur, _ := g.snap.Current(protocol.Pinned{ID: "portfolio.holdings", Version: 1})
	exact := cur.Exact
	for {
		e, _ := g.snap.Exact(exact)
		if e.Profile.Previous() == "" {
			return exact
		}
		exact = e.Profile.Previous()
	}
}

// finalBatch returns the end line an Arrow answer's last batch carries, or nil
// when its last batch is not the final batch.
func finalBatch(t *testing.T, mem memory.Allocator, body []byte) *protocol.EndLine {
	t.Helper()
	rd, err := ipc.NewReader(bytes.NewReader(body), ipc.WithAllocator(mem))
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Release()
	var end *protocol.EndLine
	for rd.Next() {
		e, final, err := protocol.EndOf(rd.RecordBatch())
		if err != nil {
			t.Fatal(err)
		}
		end = nil
		if final {
			end = e
		}
	}
	return end
}
