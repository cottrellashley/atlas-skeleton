// Package e2e drives the development gateway, DuckDB and all, through the
// Go client's public interface only: the journey as a program meets it.
package e2e

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/local/devgateway"
	atlas "github.com/jhgonline/fi-app-atlas/sdk/go"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

type counter struct {
	calls atomic.Int64
	next  http.RoundTripper
}

func (c *counter) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodPost {
		c.calls.Add(1)
	}
	return c.next.RoundTrip(r)
}

func start(t *testing.T) string {
	t.Helper()
	srv := httptest.NewUnstartedServer(nil)
	url := "http://" + srv.Listener.Addr().String()
	g, err := devgateway.New(context.Background(), devgateway.Options{URL: url})
	if err != nil {
		t.Fatal(err)
	}
	srv.Config.Handler = g.Handler
	srv.Start()
	t.Cleanup(func() { srv.Close(); g.Close() })
	return url
}

func connect(t *testing.T, url, token string) (*atlas.Client, *counter, string) {
	t.Helper()
	home := t.TempDir()
	if token != "" {
		if err := atlas.SignIn(home, "local", token, "refresh-"+token, 0); err != nil {
			t.Fatal(err)
		}
	}
	n := &counter{next: http.DefaultTransport}
	c, err := atlas.Connect(context.Background(), atlas.Options{URL: url, Home: home, HTTP: &http.Client{Transport: n}})
	if err != nil {
		t.Fatal(err)
	}
	return c, n, home
}

func code(err error) string {
	var p *protocol.Problem
	if errors.As(err, &p) {
		return p.Code + " " + p.Target
	}
	return "not a problem: " + err.Error()
}

func TestTheJourney(t *testing.T) {
	url := start(t)
	ctx := context.Background()
	alice, _, _ := connect(t, url, "dev-alice")
	who, err := alice.Whoami(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if name, _ := who.Get("name"); name != "Alice Analyst" {
		t.Fatalf("whoami %v", name)
	}
	cat, err := alice.Catalog(ctx, nil)
	if err != nil || len(cat) != 5 {
		t.Fatalf("catalogue: %d %v", len(cat), err)
	}
	found, err := alice.Search(ctx, "portfolio holdings")
	if err != nil || len(found) == 0 {
		t.Fatalf("search %v", err)
	}
	if _, err := alice.Describe(ctx, "portfolio.holdings@v1"); err != nil {
		t.Fatal(err)
	}
	md, err := alice.Guide(ctx, "portfolio.holdings", "go")
	if err != nil || !strings.Contains(md, "client.Call(ctx,") {
		t.Fatalf("guide: %v\n%s", err, md)
	}
	req := `{"portfolios": ["147HYC", "GLOBAL-CREDIT"], "columns": ["portfolio", "security_id", "as_of", "market_value", "weight"]}`
	ndjson, err := alice.Call(ctx, "portfolio.holdings@v1:fetch", atlas.Request(req), atlas.WithAccept(protocol.MediaNDJSON))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ndjson.All()
	if err != nil || len(rows) != 8 || ndjson.End() == nil || ndjson.End().Rows != 8 {
		t.Fatalf("ndjson: %d rows, %v, %+v", len(rows), err, ndjson.End())
	}
	// COL-001: Arrow gives the same records as NDJSON, decimals exact.
	arrow, err := alice.Call(ctx, "portfolio.holdings@v1:fetch", atlas.Request(req), atlas.WithAccept(protocol.MediaArrowStream))
	if err != nil {
		t.Fatal(err)
	}
	columnar, err := arrow.All()
	if err != nil || len(columnar) != len(rows) {
		t.Fatalf("arrow: %d rows, %v", len(columnar), err)
	}
	for i := range rows {
		a, _ := ijson.Canonical(rows[i])
		b, _ := ijson.Canonical(columnar[i])
		if string(a) != string(b) {
			t.Fatalf("row %d:\n ndjson %s\n arrow  %s", i, a, b)
		}
	}
	if !strings.Contains(string(mustCanon(rows[1])), `"amount":"1014852.172500"`) {
		t.Fatalf("an exact amount: %s", mustCanon(rows[1]))
	}
}

func mustCanon(o *ijson.Object) []byte { b, _ := ijson.Canonical(o); return b }

func TestScopeAndRefusals(t *testing.T) {
	url := start(t)
	ctx := context.Background()
	bob, calls, _ := connect(t, url, "dev-bob")
	_, err := bob.Call(ctx, "portfolio.holdings@v1:fetch", atlas.Request(`{"portfolios": ["147HYC", "GLOBAL-CREDIT"]}`))
	if code(err) != "not_granted /portfolios/1" {
		t.Fatalf("bob: %v", code(err))
	}
	list, err := bob.Call(ctx, "portfolio.list@v1:fetch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rows, _ := list.All(); len(rows) != 1 {
		t.Fatalf("bob's list: %d", len(rows))
	}
	before := calls.calls.Load()
	// CORE-CALL-002: an invalid request fails before anything is sent.
	_, err = bob.Call(ctx, "portfolio.holdings@v1:fetch", atlas.Request(`{"portfolios": [], "as_of": "yesterday"}`))
	var p *protocol.Problem
	if !errors.As(err, &p) || p.Code != "invalid_request" || p.Target != "/as_of" || len(p.Details) != 2 || calls.calls.Load() != before {
		t.Fatalf("invalid: %v %v, %d calls", err, p, calls.calls.Load()-before)
	}
	dave, _, _ := connect(t, url, "dev-dave")
	if _, err := dave.Call(ctx, "portfolio.holdings@v1:fetch", atlas.Request(`{"portfolios": ["147HYC"]}`)); code(err) != "denied " {
		t.Fatalf("dave: %s", code(err))
	}
	if protocol.ExitCode("denied") != 4 {
		t.Fatal("§9.7.6: denied exits 4")
	}
}

func TestSignIn(t *testing.T) {
	url := start(t)
	ctx := context.Background()
	home := t.TempDir()
	// CORE-SIGNIN-002: a token about to expire is refreshed first, and the
	// file rewritten whole, readable by its owner only.
	if err := atlas.SignIn(home, "local", "stale", "refresh-dev-alice", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	c, err := atlas.Connect(ctx, atlas.Options{URL: url, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Whoami(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "state", "token-local.json")
	b, _ := os.ReadFile(path)
	st, _ := os.Stat(path)
	if !strings.Contains(string(b), `"access_token":"dev-alice"`) || st.Mode().Perm() != 0o600 {
		t.Fatalf("refreshed: %s %v", b, st.Mode())
	}
	// CORE-SIGNIN-005: a token file others can read is refused.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	c2, _ := atlas.Connect(ctx, atlas.Options{URL: url, Home: home})
	if _, err := c2.Whoami(ctx); !strings.HasPrefix(code(err), "unauthenticated") {
		t.Fatalf("a readable token file: %s", code(err))
	}
}
