package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/adapter/auditlog"
	"github.com/jhgonline/fi-app-atlas/gateway/fixtures"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/conc"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/lifecycle"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// testDriver is the warehouse engine the settings name; the tests open its
// sessions with testConnector, which connects unless its DSN is "down", and
// runs nothing.
const testDriver = engineDriver

// TestMain starts os/signal's one watcher outside every synctest bubble:
// the serving group registers for SIGTERM inside them, and a goroutine of
// the bubble must end with it.
func TestMain(m *testing.M) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGUSR2)
	signal.Stop(c)
	os.Exit(m.Run())
}

var discard = slog.New(slog.DiscardHandler)

// R016, ATLAS-MECH-016, with main.go's own composition: a signal turns
// readiness false and starts the drain, and the call running when it came
// finishes, its context never cancelled by it. Before, the server's base
// context was the one the signal cancelled, so every call in flight was cut
// off at the signal: a 500 that clients do not retry, on every deploy.
func TestR016ASignalLetsTheRunningCallFinish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ln := newPipeListener()
		started, release := make(chan struct{}), make(chan struct{})
		var atRelease error
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-release
			atRelease = r.Context().Err()
			_, _ = io.WriteString(w, "done")
		})
		cfg := (&Settings{Listen: ":8000"}).serving()
		ready := &lifecycle.Readiness{}
		ctx, signalled := context.WithCancel(context.Background())
		served := conc.Go(func() error { return serve(ctx, h, ln, cfg, ready, discard) })
		call := conc.Go(func() error {
			res, err := ln.client().Get("http://atlas/v1/data/portfolio.holdings@v1:fetch")
			if err != nil {
				return err
			}
			defer res.Body.Close()
			b, _ := io.ReadAll(res.Body)
			if res.StatusCode != http.StatusOK || string(b) != "done" {
				return fmt.Errorf("%d %q", res.StatusCode, b)
			}
			return nil
		})
		<-started
		signalled()
		synctest.Wait()
		if ready.Ready() {
			t.Fatal("readiness is true after the signal")
		}
		time.Sleep(cfg.ShutdownDelay)
		synctest.Wait()
		close(release)
		if err := call(); err != nil {
			t.Fatalf("the call running at the signal: %v", err)
		}
		if atRelease != nil {
			t.Fatalf("its context ended at the signal: %v", atRelease)
		}
		if err := served(); !errors.Is(err, context.Canceled) {
			t.Fatalf("serve: %v", err)
		}
	})
}

// The production root, end to end over its real adapters (Entra over a JWKS
// endpoint, the policy file, the JSON-lines audit): it starts ready, serves
// a signed-in call and keeps that call's facts (R047, R056); and the server
// is the deployable's, so a person's 48 KiB bearer token reaches the edge
// and gets a problem with its request id, where 16 KiB of headers used to
// be refused as text/plain before the edge ran (R123).
func TestTheProductionRootServesWhatItWasBuiltWith(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRoot(t)
		g := r.start(t, r.settings(t))
		if code, body := g.get(t, "/readyz"); code != http.StatusOK {
			t.Fatalf("/readyz %d %s", code, body)
		}
		a := g.call(t, r.token(t, "k1"))
		if a.status != http.StatusOK || !strings.Contains(string(a.body), porttestAlice) {
			t.Fatalf("whoami: %d %s", a.status, a.body)
		}
		facts := r.facts(t)
		if len(facts) != 2 || facts[0].Kind != ports.Admission || facts[1].Kind != ports.Outcome || facts[0].RequestID != a.header.Get(protocol.HeaderRequestID) || facts[0].Principal != porttestAlice {
			t.Fatalf("the call's facts: %+v", facts)
		}
		big := g.call(t, strings.Repeat("e", 48<<10))
		if big.status != http.StatusUnauthorized || big.header.Get(protocol.HeaderErrorCode) != protocol.CodeUnauthenticated || big.header.Get(protocol.HeaderRequestID) == "" {
			t.Fatalf("a 48 KiB token: %d %v", big.status, big.header)
		}
		if huge := g.call(t, strings.Repeat("e", 80<<10)); huge.status != http.StatusRequestHeaderFieldsTooLarge {
			t.Fatalf("an 80 KiB token: %d", huge.status)
		}
		g.stop(t)
	})
}

// R013, R064, GATEWAY-012 (§6.1.16's Entra row), through the production
// root: while Entra's key endpoint is down, calls with tokens already held
// keep working, verified with the keys the gateway holds, past the key
// document's max-age, and the built-ins serve. A token under a key the
// gateway never held is the gateway's fault: unavailable with Retry-After,
// never 401, which tells people and agents to sign in again.
func TestR064TheEntraRowWhileItsKeysAreDown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRoot(t)
		g := r.start(t, r.settings(t))
		if a := g.call(t, r.token(t, "k1")); a.status != http.StatusOK {
			t.Fatalf("before the outage: %d %s", a.status, a.body)
		}
		r.jwks.fail()
		time.Sleep(2 * time.Hour) // past the key document's max-age
		if a := g.call(t, r.token(t, "k1")); a.status != http.StatusOK {
			t.Fatalf("a held token during the outage: %d %s", a.status, a.body)
		}
		if code, body := g.get(t, "/readyz"); code != http.StatusOK {
			t.Fatalf("not ready while it can verify held tokens: %d %s", code, body)
		}
		a := g.call(t, r.token(t, "k2"))
		if a.status != http.StatusServiceUnavailable || a.header.Get(protocol.HeaderErrorCode) != protocol.CodeUnavailable || a.header.Get("Retry-After") == "" {
			t.Fatalf("a token under a key never held, during the outage: %d %v %s", a.status, a.header, a.body)
		}
		g.stop(t)
	})
}

// R013: a replica that starts while Entra cannot be reached starts, but is
// not ready, and answers every call unavailable with Retry-After, never 401;
// once the keys can be fetched, it is ready and serves.
func TestR013ARootThatStartsWhileEntraIsDown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRoot(t)
		r.jwks.fail()
		g := r.start(t, r.settings(t))
		if code, _ := g.get(t, "/readyz"); code != http.StatusServiceUnavailable {
			t.Fatalf("/readyz without the tenant's keys: %d", code)
		}
		a := g.call(t, r.token(t, "k1"))
		if a.status != http.StatusServiceUnavailable || a.header.Get("Retry-After") == "" {
			t.Fatalf("a valid token while the keys cannot be fetched: %d %v %s", a.status, a.header, a.body)
		}
		r.jwks.heal()
		if code, body := g.get(t, "/readyz"); code != http.StatusOK {
			t.Fatalf("/readyz once the keys can be fetched: %d %s", code, body)
		}
		if a := g.call(t, r.token(t, "k1")); a.status != http.StatusOK {
			t.Fatalf("once the keys can be fetched: %d %s", a.status, a.body)
		}
		g.stop(t)
	})
}

// R047, R012: the audit on standard output, when it is a pipe a collector
// reads (a container's), keeps every fact, and nothing else is written
// there: the logs are on standard error. Standard output that keeps nothing
// (a terminal, /dev/null) refuses the start, rather than every call.
func TestR047TheAuditOnStandardOutput(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRoot(t)
		pr, pw, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer pr.Close()
		r.stdout = pw
		s := r.settings(t)
		s["audit_file"] = "-"
		g := r.start(t, s)
		if a := g.call(t, r.token(t, "k1")); a.status != http.StatusOK {
			t.Fatalf("%d %s", a.status, a.body)
		}
		// The facts are in the pipe when the call has answered: Record
		// returned once each was written.
		out := bufio.NewReader(pr)
		for _, want := range []string{"admission", "outcome"} {
			line, err := out.ReadString('\n')
			var f map[string]any
			if err != nil || json.Unmarshal([]byte(line), &f) != nil || f["kind"] != want || f["principal"] != porttestAlice {
				t.Fatalf("standard output carried %q, %v; want the %s fact", line, err, want)
			}
		}
		g.stop(t)
		pw.Close()
		if rest, err := io.ReadAll(out); err != nil || len(rest) > 0 {
			t.Fatalf("standard output carried more than the facts: %q %v", rest, err)
		}

		null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer null.Close()
		r.stdout = null
		cfg, err := load(writeSettings(t, s))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := open(t.Context(), cfg, r.host(), discard); err == nil || !strings.Contains(err.Error(), `audit_file "-"`) || !strings.Contains(err.Error(), "keeps no record") {
			t.Fatalf("standard output to /dev/null: %v", err)
		}
	})
}

// R124, §6.7.9, ATLAS-MECH-009: replicas started from one settings file
// sign cursors with one key, so a listing paged on one replica continues on
// another; and after a rotation, a replica given the old key as
// cursor_key_previous still accepts the cursors it signed. Before, each
// replica made a random key, and page 2 behind a load balancer was refused
// half the time.
func TestR124ReplicasAcceptEachOthersCursors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, b := newRoot(t), newRoot(t)
		ga, gb := a.start(t, a.settings(t)), b.start(t, b.settings(t))
		page := func(g *started, body string) (int, string, string) {
			t.Helper()
			ans := g.post(t, "/v1/atlas/atlas.catalog:fetch", a.token(t, "k1"), body)
			var p struct {
				NextCursor *string `json:"next_cursor"`
			}
			_ = json.Unmarshal(ans.body, &p)
			next := ""
			if p.NextCursor != nil {
				next = *p.NextCursor
			}
			return ans.status, next, string(ans.body)
		}
		status, cursor, body := page(ga, `{"page_size": 2}`)
		if status != http.StatusOK || cursor == "" {
			t.Fatalf("page 1 on replica A: %d %s", status, body)
		}
		if status, _, body := page(gb, `{"page_size": 2, "cursor": "`+cursor+`"}`); status != http.StatusOK {
			t.Fatalf("page 2 on replica B: %d %s", status, body)
		}
		gb.stop(t)

		// The key is rotated: a replica with the new key and the old one as
		// its previous accepts the old cursor; one without the old key
		// refuses it.
		t.Setenv("ATLAS_TEST_NEW_CURSOR_KEY", strings.Repeat("n", 32))
		rotated := b.settings(t)
		rotated["cursor_key"] = map[string]any{"env": "ATLAS_TEST_NEW_CURSOR_KEY"}
		rotated["cursor_key_previous"] = map[string]any{"env": "ATLAS_TEST_CURSOR_KEY"}
		gr := b.start(t, rotated)
		if status, _, body := page(gr, `{"page_size": 2, "cursor": "`+cursor+`"}`); status != http.StatusOK {
			t.Fatalf("an old cursor after the rotation: %d %s", status, body)
		}
		gr.stop(t)
		delete(rotated, "cursor_key_previous")
		gn := b.start(t, rotated)
		ans := gn.post(t, "/v1/atlas/atlas.catalog:fetch", a.token(t, "k1"), `{"page_size": 2, "cursor": "`+cursor+`"}`)
		if ans.status != http.StatusBadRequest || ans.header.Get(protocol.HeaderErrorCode) != protocol.CodeInvalidRequest {
			t.Fatalf("a cursor of a key the replica does not hold: %d %s", ans.status, ans.body)
		}
		gn.stop(t)
		ga.stop(t)
	})
}

// R054, R069 (the settings' part), through openWarehouse: the settings'
// pool bounds reach the gosnowflake engine, whose own tests prove that it
// keeps them (gosf.TestR054DeadSessionsAreRetired), and the Source over it
// is the deployment's data source.
func TestR054ThePoolTakesTheSettingsBounds(t *testing.T) {
	w := warehouse{Driver: engineDriver, DSN: envSecret{Env: "ATLAS_TEST_DSN"}, Registered: [][]string{{"HARBOUR", "HOLDINGS"}}, MaxOpen: 6, MaxIdleSeconds: 600}
	opened, err := openWarehouse(t.Context(), w, "", discard, testConnector{dsn: "warehouse"})
	if err != nil {
		t.Fatal(err)
	}
	defer opened.engine.Close()
	cfg := opened.engine.Config()
	if cfg.MaxOpen != 6 || cfg.MaxIdleTime != 10*time.Minute || opened.engine.Stats().MaxOpenConnections != 6 || opened.source == nil || opened.pool.maxOpen != 6 {
		t.Fatalf("the pool: %+v, %d open at most", cfg, opened.engine.Stats().MaxOpenConnections)
	}
	if opened.source.Capabilities().Engine != "snowflake" {
		t.Fatalf("the data source: %+v", opened.source.Capabilities())
	}
}

// R069, constitution rule 13: a warehouse that cannot be reached when the
// gateway starts costs its own capabilities, never the gateway: the start
// goes on, and says so. Any engine but the one this build links is refused.
func TestR069AWarehouseUnreachableAtStartIsReported(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	w := warehouse{Driver: engineDriver, Registered: [][]string{{"HARBOUR", "HOLDINGS"}}}
	opened, err := openWarehouse(t.Context(), w, "", log, testConnector{dsn: "down"})
	if err != nil {
		t.Fatal(err)
	}
	defer opened.engine.Close()
	if !strings.Contains(logs.String(), "the warehouse cannot be reached yet") || !strings.Contains(logs.String(), "390100") {
		t.Fatalf("logs: %s", logs.String())
	}
	if _, err := openWarehouse(t.Context(), warehouse{Driver: "postgres"}, "", log, nil); err == nil || !strings.Contains(err.Error(), "snowflake") {
		t.Fatalf("an engine this build does not link: %v", err)
	}
	if _, err := openWarehouse(t.Context(), warehouse{Driver: engineDriver, Registered: [][]string{{"HARBOUR", "HOLDINGS"}}}, "not a dsn", log, nil); err == nil || strings.Contains(err.Error(), "not a dsn") {
		t.Fatalf("a DSN that does not parse is refused, and never quoted: %v", err)
	}
}

// An exclude that names nothing a domain serves is a typo that would serve
// what it meant to withhold: refused at start.
func TestAnExcludeThatNamesNothingIsRefused(t *testing.T) {
	r := newRoot(t)
	s := r.settings(t)
	s["exclude"] = []string{"portfolio.holdingz@v1"}
	cfg, err := load(writeSettings(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := open(t.Context(), cfg, r.host(), discard); err == nil || !strings.Contains(err.Error(), "portfolio.holdingz@v1") {
		t.Fatalf("got %v", err)
	}
	s["exclude"] = []string{"portfolio.holdings-dates@v1"}
	if cfg, err = load(writeSettings(t, s)); err != nil {
		t.Fatal(err)
	}
	d, err := open(t.Context(), cfg, r.host(), discard)
	if err != nil {
		t.Fatalf("an exclude of a served capability: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
}

// porttestAlice is the fixture's Alice, whom the root's tokens name.
const porttestAlice = "a11ce000-0000-4000-8000-000000000001"

const issuer = "https://login.microsoftonline.com/t1/v2.0"

var (
	keyOnce sync.Once
	key     *rsa.PrivateKey
)

// root is a production root's surroundings: a policy file, an audit file,
// the cursor key's variable, and Entra's key endpoint in memory.
type root struct {
	dir    string
	jwks   *jwksEndpoint
	stdout *os.File
}

func newRoot(t *testing.T) *root {
	t.Helper()
	keyOnce.Do(func() { key, _ = rsa.GenerateKey(rand.Reader, 2048) })
	if key == nil {
		t.Fatal("no key")
	}
	r := &root{dir: t.TempDir(), jwks: &jwksEndpoint{keys: map[string]*rsa.PublicKey{"k1": &key.PublicKey}}}
	pol, err := fixtures.Policy()
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(pol)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, "policy.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return r
}

// settings serve the journey's domains over the test warehouse, as the
// fixture policy, which grants them, requires.
func (r *root) settings(t *testing.T) map[string]any {
	s := withWarehouse(goodSettings(t), nil)
	s["policy_file"] = filepath.Join(r.dir, "policy.json")
	s["audit_file"] = filepath.Join(r.dir, "audit.jsonl")
	s["listen"] = "127.0.0.1:0"
	s["domains"] = []string{"portfolio", "data"}
	return s
}

func (r *root) host() host {
	return host{stdout: r.stdout, stderr: io.Discard, http: &http.Client{Transport: r.jwks, Timeout: startTimeout}, warehouse: testConnector{dsn: "warehouse"}}
}

// facts are what the audit file holds.
func (r *root) facts(t *testing.T) []ports.Fact {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	facts, err := auditlog.Read(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

// token is Alice's Entra access token, signed under kid.
func (r *root) token(t *testing.T, kid string) string {
	t.Helper()
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	now := time.Now()
	claims := map[string]any{"iss": issuer, "aud": "api://atlas", "exp": now.Add(time.Hour).Unix(), "iat": now.Add(-time.Minute).Unix(), "nbf": now.Add(-time.Minute).Unix(),
		"oid": porttestAlice, "tid": "t1", "name": "Alice Analyst", "azp": "atlas-cli", "groups": []string{"credit-analysts"}}
	signing := enc(map[string]any{"alg": "RS256", "kid": kid, "typ": "JWT"}) + "." + enc(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// started is a production root, opened from its settings and serving on an
// in-memory listener.
type started struct {
	d      *deployment
	ln     *pipeListener
	cancel context.CancelFunc
	served func() error
}

func (r *root) start(t *testing.T, s map[string]any) *started {
	t.Helper()
	cfg, err := load(writeSettings(t, s))
	if err != nil {
		t.Fatal(err)
	}
	d, err := open(t.Context(), cfg, r.host(), discard)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ln := newPipeListener()
	g := &started{d: d, ln: ln, cancel: cancel}
	g.served = conc.Go(func() error { return serve(ctx, d.gateway.Edge, ln, cfg.serving(), d.ready, discard) })
	if !synctestReady(d.ready) {
		t.Fatal("the root is not serving")
	}
	return g
}

// synctestReady waits, in a bubble, for the server to start serving.
func synctestReady(r *lifecycle.Readiness) bool {
	synctest.Wait()
	return r.Ready()
}

// stop is the signal: the root drains, and closes what it opened.
func (g *started) stop(t *testing.T) {
	t.Helper()
	g.cancel()
	if err := g.served(); !errors.Is(err, context.Canceled) {
		t.Fatalf("serve: %v", err)
	}
	if err := g.d.Close(); err != nil {
		t.Fatal(err)
	}
}

func (g *started) get(t *testing.T, path string) (int, string) {
	t.Helper()
	res, err := g.ln.client().Get("http://atlas" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

type answer struct {
	status int
	header http.Header
	body   []byte
}

// call is atlas.whoami@v1:fetch with a bearer token.
func (g *started) call(t *testing.T, token string) answer {
	t.Helper()
	return g.post(t, "/v1/atlas/atlas.whoami:fetch", token, "{}")
}

// post is a call to a binding's address.
func (g *started) post(t *testing.T, path, token, body string) answer {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://atlas"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(protocol.HeaderClient, `name="root-test", version="1.0.0", protocol="atlas/1.0"`)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := g.ln.client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return answer{status: res.StatusCode, header: res.Header, body: b}
}

// jwksEndpoint is the tenant's key endpoint, in memory: an outage and the
// passing of time are the test's to decide.
type jwksEndpoint struct {
	mu   sync.Mutex
	keys map[string]*rsa.PublicKey
	down bool
}

func (e *jwksEndpoint) fail() { e.mu.Lock(); e.down = true; e.mu.Unlock() }
func (e *jwksEndpoint) heal() { e.mu.Lock(); e.down = false; e.mu.Unlock() }

func (e *jwksEndpoint) RoundTrip(r *http.Request) (*http.Response, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.down {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}
	var doc struct {
		Keys []map[string]string `json:"keys"`
	}
	for kid, k := range e.keys {
		doc.Keys = append(doc.Keys, map[string]string{"kty": "RSA", "use": "sig", "kid": kid,
			"n": base64.RawURLEncoding.EncodeToString(k.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes())})
	}
	b, _ := json.Marshal(doc)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}, "Cache-Control": {"max-age=3600"}},
		Body: io.NopCloser(bytes.NewReader(b)), Request: r}, nil
}

// testWarehouse is a database/sql driver that connects and runs nothing:
// enough for the pool's bounds and the start's checks.
type testWarehouse struct{}

// testConnector opens testWarehouse sessions, as gosf's connector.
type testConnector struct{ dsn string }

func (c testConnector) Connect(context.Context) (driver.Conn, error) {
	return testWarehouse{}.Open(c.dsn)
}
func (c testConnector) Driver() driver.Driver { return testWarehouse{} }

func (testWarehouse) Open(dsn string) (driver.Conn, error) {
	if dsn == "down" {
		return nil, errors.New("390100 (08004): Incorrect username or password was specified")
	}
	return testConn{}, nil
}

type testConn struct{}

func (testConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("the test warehouse runs nothing")
}
func (testConn) Close() error { return nil }
func (testConn) Begin() (driver.Tx, error) {
	return nil, errors.New("the test warehouse has no transactions")
}

// pipeListener is an in-memory listener: a dial hands the server one end of
// a net.Pipe, so a synctest bubble sees every wait.
type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *pipeListener) Addr() net.Addr { return pipeAddr{} }

func (l *pipeListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	server, client := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-l.closed:
		return nil, errors.New("connection refused")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (l *pipeListener) client() *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: l.dial, DisableKeepAlives: true}}
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "atlas" }
