// Package fakegw is a stand-in Atlas gateway for the tests of the Go
// client, the engine and the CLI. It serves the references' fixture
// (spec/protocol/references/scenarios/fixtures/gateway.json, written from
// the spec's text alone, D22): the bootstrap, every profile by exact id
// through atlas.describe, the built-ins a client needs, a token endpoint,
// and the default answers of conformance.echo and conformance.rows
// (README, "the conformance gateway"). It records every request, so a test
// asserts what was sent, and a test scripts any answer by address.
//
// It listens on an in-memory network, never on a port: the gateways of one
// test and the clients it builds share a Network, whose transport dials a
// gateway by its host. Everything here works inside a testing/synctest
// bubble, so a test of deadlines and retries moves the bubble's clock
// instead of sleeping.
package fakegw

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/conc"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/arrowschema"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/frame"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

// Network is the in-memory network a test's gateways listen on.
type Network struct {
	mu        sync.Mutex
	hosts     map[string]*listener
	transport *http.Transport
}

// NewNetwork is an empty network. Its idle connections are closed when the
// test ends.
func NewNetwork(t testing.TB) *Network {
	n := &Network{hosts: map[string]*listener{}}
	n.transport = &http.Transport{DialContext: n.dial, MaxIdleConnsPerHost: 8}
	t.Cleanup(n.transport.CloseIdleConnections)
	return n
}

// Client is an HTTP client that reaches every gateway of the network, and
// nothing else.
func (n *Network) Client() *http.Client { return &http.Client{Transport: n.transport} }

func (n *Network) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	n.mu.Lock()
	l := n.hosts[addr]
	n.mu.Unlock()
	if l == nil {
		return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connection refused: no gateway at " + addr)}
	}
	return l.dial(ctx)
}

type listener struct {
	addr  string
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func (l *listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *listener) Close() error { l.once.Do(func() { close(l.done) }); return nil }

func (l *listener) Addr() net.Addr { return fakeAddr(l.addr) }

func (l *listener) dial(ctx context.Context) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-l.done:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type fakeAddr string

func (a fakeAddr) Network() string { return "fake" }
func (a fakeAddr) String() string  { return string(a) }

// Options configure a gateway. Every member is optional.
type Options struct {
	// Host is the host:port it answers at; "127.0.0.1:<n>" by default.
	Host string
	// Environment is its bootstrap's environment; "conformance" by default.
	Environment string
	// Network is the network it listens on; one of its own by default.
	Network *Network
}

// Request is one request the gateway received.
type Request struct {
	Method, Path string
	// Address is the interaction the path is bound to ("atlas.whoami:fetch",
	// "conformance.echo@v1:fetch"), or "" for the bootstrap and the token
	// endpoint.
	Address string
	Header  http.Header
	Body    []byte
	At      time.Time
}

// Grant is what the token endpoint answers for a refresh token.
type Grant struct {
	AccessToken, RefreshToken string
	ExpiresIn                 int64
}

// Call is one interaction request, as a handler sees it.
type Call struct {
	W       http.ResponseWriter
	R       *http.Request
	Address string
	Body    *ijson.Object
	// N is the request's ordinal among those to its address, from 1.
	N  int
	gw *Gateway
}

// Handler answers an interaction.
type Handler func(c *Call)

// Gateway is one stand-in gateway.
type Gateway struct {
	URL  string
	Host string
	Env  string

	t   testing.TB
	net *Network
	srv *http.Server

	mu       sync.Mutex
	fixture  *ijson.Object
	profiles map[string]*ijson.Object
	parsed   map[string]*profile.Profile
	current  map[string]string
	routes   map[string]string
	handlers map[string]Handler
	writers  map[string]string
	access   map[string]bool
	grants   map[string]Grant
	// client is the application its token endpoint redeems refresh tokens
	// for, as Entra does: the fixture's sign_in client_id unless Application
	// names another.
	client   *string
	counts   map[string]int
	requests []Request
	edit     func(*ijson.Object)
}

var hosts struct {
	sync.Mutex
	n int
}

// New starts a gateway serving the references' fixture. It stops when the
// test ends.
func New(t testing.TB, opt Options) *Gateway {
	t.Helper()
	if opt.Network == nil {
		opt.Network = NewNetwork(t)
	}
	if opt.Host == "" {
		hosts.Lock()
		hosts.n++
		opt.Host = "127.0.0.1:" + strconv.Itoa(18000+hosts.n)
		hosts.Unlock()
	}
	if opt.Environment == "" {
		opt.Environment = "conformance"
	}
	g := &Gateway{
		URL: "http://" + opt.Host, Host: opt.Host, Env: opt.Environment, t: t, net: opt.Network,
		profiles: map[string]*ijson.Object{}, parsed: map[string]*profile.Profile{}, current: map[string]string{},
		routes: map[string]string{}, handlers: map[string]Handler{}, writers: map[string]string{},
		access: map[string]bool{}, grants: map[string]Grant{}, counts: map[string]int{},
	}
	g.load(t)
	l := &listener{addr: opt.Host, conns: make(chan net.Conn), done: make(chan struct{})}
	g.net.mu.Lock()
	if g.net.hosts[opt.Host] != nil {
		g.net.mu.Unlock()
		t.Fatalf("fakegw: a gateway already answers at %s", opt.Host)
	}
	g.net.hosts[opt.Host] = l
	g.net.mu.Unlock()
	g.srv = &http.Server{Handler: http.HandlerFunc(g.serve), ReadHeaderTimeout: time.Minute}
	wait := conc.Go(func() error {
		if err := g.srv.Serve(l); !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			return err
		}
		return nil
	})
	t.Cleanup(func() {
		g.net.mu.Lock()
		delete(g.net.hosts, opt.Host)
		g.net.mu.Unlock()
		_ = g.srv.Close()
		if err := wait(); err != nil {
			t.Errorf("fakegw: %v", err)
		}
		g.net.transport.CloseIdleConnections()
	})
	return g
}

// Client is an HTTP client on the gateway's network.
func (g *Gateway) Client() *http.Client { return g.net.Client() }

// Network is the network the gateway listens on.
func (g *Gateway) Network() *Network { return g.net }

func references(t testing.TB) string {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		refs := filepath.Join(dir, "spec", "protocol", "references")
		if fi, err := os.Stat(refs); err == nil && fi.IsDir() {
			return refs
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("fakegw: spec/protocol/references is not above " + dir)
		}
		dir = parent
	}
}

func (g *Gateway) load(t testing.TB) {
	b, err := os.ReadFile(filepath.Join(references(t), "scenarios", "fixtures", "gateway.json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := ijson.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	g.fixture = v.(*ijson.Object)
	profiles, _ := g.fixture.Get("profiles")
	profiles.(*ijson.Object).Each(func(exact string, doc ijson.Value) bool {
		g.serveProfile(t, exact, doc.(*ijson.Object))
		return true
	})
	current, _ := g.fixture.Get("current")
	current.(*ijson.Object).Each(func(pinned string, exact ijson.Value) bool {
		g.current[pinned] = exact.(string)
		return true
	})
	tokens, _ := g.fixture.Get("tokens")
	access, _ := tokens.(*ijson.Object).Get("access")
	for _, a := range access.([]ijson.Value) {
		g.access[a.(string)] = true
	}
	refresh, _ := tokens.(*ijson.Object).Get("refresh")
	refresh.(*ijson.Object).Each(func(rt string, v ijson.Value) bool {
		o := v.(*ijson.Object)
		at, _ := o.Get("access_token")
		next, _ := o.Get("refresh_token")
		in, _ := o.Get("expires_in")
		n, _ := in.(ijson.Number).Int64()
		g.grants[rt] = Grant{AccessToken: at.(string), RefreshToken: next.(string), ExpiresIn: n}
		return true
	})
	b2, _ := g.fixture.Get("bootstrap")
	builtins, _ := b2.(*ijson.Object).Get("builtins")
	builtins.(*ijson.Object).Each(func(id string, entry ijson.Value) bool {
		in, _ := entry.(*ijson.Object).Get("interactions")
		in.(*ijson.Object).Each(func(name string, i ijson.Value) bool {
			bs, _ := i.(*ijson.Object).Get("bindings")
			for _, b := range bs.([]ijson.Value) {
				addr, _ := b.(*ijson.Object).Get("address")
				g.routes[addr.(string)] = id + ":" + name
			}
			return true
		})
		return true
	})
}

// serveProfile adds one profile document, routing every binding of it
// (gaps/2.md D5: every binding of every live profile).
func (g *Gateway) serveProfile(t testing.TB, exact string, doc *ijson.Object) {
	p, err := profile.Parse(doc)
	if err != nil {
		t.Fatalf("fakegw: %s: %v", exact, err)
	}
	g.profiles[exact] = doc
	g.parsed[exact] = p
	pinned := grammar.Pinned{ID: p.ID, Version: p.Version}.String()
	for _, name := range p.Order {
		for _, b := range p.Interactions[name].Bindings {
			if b.Protocol == "https" {
				g.routes[b.Address] = pinned + ":" + name
			}
		}
	}
}

// Serve adds a profile document (with its fingerprint) to what the
// gateway serves by exact id.
func (g *Gateway) Serve(doc *ijson.Object) string {
	g.t.Helper()
	fp, err := protocol.CheckFingerprint(doc)
	if err != nil {
		g.t.Fatalf("fakegw: %v", err)
	}
	p, err := profile.Parse(doc)
	if err != nil {
		g.t.Fatal(err)
	}
	exact := grammar.Exact{Pinned: grammar.Pinned{ID: p.ID, Version: p.Version}, Fingerprint: fp.Value}.String()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.serveProfile(g.t, exact, doc)
	return exact
}

// Profile is a profile document of the fixture, by exact id, as a copy a
// test may change.
func (g *Gateway) Profile(exact string) *ijson.Object {
	g.mu.Lock()
	defer g.mu.Unlock()
	doc, ok := g.profiles[exact]
	if !ok {
		g.t.Fatalf("fakegw: no profile %s", exact)
	}
	return ijson.Clone(doc).(*ijson.Object)
}

// Current makes exact the profile served for its pinned id.
func (g *Gateway) Current(exact string) {
	e, err := grammar.ParseExact(exact)
	if err != nil {
		g.t.Fatal(err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.current[e.Pinned.String()] = exact
}

// WriteUnder makes the default answers of address be written under the
// profile exact, as a gateway that moved on writes them.
func (g *Gateway) WriteUnder(address, exact string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.writers[address] = exact
}

// Handle scripts an interaction: h answers every request to address
// ("conformance.echo@v1:fetch", "atlas.whoami:fetch"), or, for an address
// that begins with "/", every request to that path (the bootstrap's, the
// token endpoint's), before any check.
func (g *Gateway) Handle(address string, h Handler) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.handlers[address] = h
}

// Bootstrap edits the bootstrap the gateway serves.
func (g *Gateway) Bootstrap(edit func(*ijson.Object)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.edit = edit
}

// Application replaces the application its token endpoint redeems refresh
// tokens for. A refresh that names no client_id, or another, is refused
// with invalid_client, as Entra refuses one (R3-03).
func (g *Gateway) Application(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.client = &id
}

// Tokens replaces the access tokens it accepts and the refresh tokens its
// token endpoint redeems.
func (g *Gateway) Tokens(access []string, grants map[string]Grant) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.access = map[string]bool{}
	for _, a := range access {
		g.access[a] = true
	}
	g.grants = grants
}

// Requests is every request received so far, in order.
func (g *Gateway) Requests() []Request {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.requests)
}

// To is the requests received for an address, or for a path when address
// begins with "/".
func (g *Gateway) To(address string) []Request {
	var out []Request
	for _, r := range g.Requests() {
		if r.Address == address || strings.HasPrefix(address, "/") && r.Path == address {
			out = append(out, r)
		}
	}
	return out
}

func (g *Gateway) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		return
	}
	g.mu.Lock()
	address := g.routes[r.URL.Path]
	g.requests = append(g.requests, Request{Method: r.Method, Path: r.URL.Path, Address: address, Header: r.Header.Clone(), Body: body, At: time.Now()})
	n := 0
	if address != "" {
		g.counts[address]++
		n = g.counts[address]
	}
	g.mu.Unlock()
	w.Header().Set(protocol.HeaderRequestID, fmt.Sprintf("req-%d", len(g.Requests())))
	g.mu.Lock()
	byPath := g.handlers[r.URL.Path]
	g.mu.Unlock()
	if byPath != nil {
		c := &Call{W: w, R: r, Address: address, N: n, gw: g}
		if v, err := ijson.Parse(body); err == nil {
			c.Body, _ = v.(*ijson.Object)
		}
		byPath(c)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == protocol.WellKnownPath:
		g.bootstrap(w, r)
		return
	case r.Method == http.MethodPost && r.URL.Path == "/oauth/token":
		g.token(w, body)
		return
	case address == "":
		WriteProblem(w, http.StatusNotFound, protocol.CodeNotFound, "no binding at "+r.URL.Path, nil)
		return
	}
	if !g.authorised(r) {
		w.Header().Set(protocol.HeaderWWWAuthenticate, `Bearer realm="atlas", error="invalid_token"`)
		WriteProblem(w, http.StatusUnauthorized, protocol.CodeUnauthenticated, "the token is not valid", nil)
		return
	}
	c := &Call{W: w, R: r, Address: address, N: n, gw: g}
	if v, err := ijson.Parse(body); err == nil {
		c.Body, _ = v.(*ijson.Object)
	}
	if c.Body == nil {
		WriteProblem(w, http.StatusBadRequest, protocol.CodeInvalidDocument, "the body is not a JSON object", nil)
		return
	}
	g.mu.Lock()
	h := g.handlers[address]
	g.mu.Unlock()
	if h != nil {
		h(c)
		return
	}
	c.Serve()
}

func (g *Gateway) authorised(r *http.Request) bool {
	token, err := protocol.ParseBearer(r.Header.Get(protocol.HeaderAuthorization))
	if err != nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.access[token]
}

func (g *Gateway) bootstrapDoc() *ijson.Object {
	g.mu.Lock()
	defer g.mu.Unlock()
	b, _ := g.fixture.Get("bootstrap")
	doc := substitute(ijson.Clone(b), "{gateway}", g.URL).(*ijson.Object)
	doc.Set("environment", g.Env)
	if g.edit != nil {
		g.edit(doc)
	}
	return doc
}

func substitute(v ijson.Value, from, to string) ijson.Value {
	switch x := v.(type) {
	case string:
		return strings.ReplaceAll(x, from, to)
	case []ijson.Value:
		for i := range x {
			x[i] = substitute(x[i], from, to)
		}
	case *ijson.Object:
		x.Each(func(name string, m ijson.Value) bool {
			x.Set(name, substitute(m, from, to))
			return true
		})
	}
	return v
}

func (g *Gateway) bootstrap(w http.ResponseWriter, r *http.Request) {
	body, err := ijson.Marshal(g.bootstrapDoc())
	if err != nil {
		g.t.Errorf("fakegw: %v", err)
		return
	}
	sum := sha256.Sum256(body)
	etag := protocol.StrongETag(hex.EncodeToString(sum[:8]))
	w.Header().Set(protocol.HeaderETag, etag)
	w.Header().Set(protocol.HeaderCacheControl, "no-cache")
	if protocol.IfNoneMatch(r.Header.Get(protocol.HeaderIfNoneMatch), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set(protocol.HeaderContentType, protocol.MediaJSON)
	_, _ = w.Write(body)
}

func (g *Gateway) token(w http.ResponseWriter, body []byte) {
	form, err := url.ParseQuery(string(body))
	g.mu.Lock()
	client := ""
	if g.client != nil {
		client = *g.client
	} else {
		b, _ := g.fixture.Get("bootstrap")
		if bo, ok := b.(*ijson.Object); ok {
			si, _ := bo.Get("sign_in")
			if o, ok := si.(*ijson.Object); ok {
				id, _ := o.Get("client_id")
				client, _ = id.(string)
			}
		}
	}
	if err == nil && (form.Get("client_id") == "" || form.Get("client_id") != client) {
		g.mu.Unlock()
		w.Header().Set(protocol.HeaderContentType, protocol.MediaJSON)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
		return
	}
	grant, ok := g.grants[form.Get("refresh_token")]
	if ok {
		g.access[grant.AccessToken] = true
	}
	g.mu.Unlock()
	w.Header().Set(protocol.HeaderContentType, protocol.MediaJSON)
	if err != nil || form.Get("grant_type") != "refresh_token" || !ok {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": grant.AccessToken, "refresh_token": grant.RefreshToken, "expires_in": grant.ExpiresIn, "token_type": "Bearer"})
}

// WriteProblem answers a problem document, as the gateway's kit/problem
// writes one: its code in Atlas-Error-Code, and Retry-After when the
// problem has one. extra adds or replaces members (retryable,
// retry_after, target, details).
func WriteProblem(w http.ResponseWriter, status int, code, detail string, extra map[string]any) {
	p := map[string]any{"type": protocol.ProblemTypeBase + code, "title": code, "status": status, "code": code, "detail": detail, "next": "See the problem's detail.", "request_id": w.Header().Get(protocol.HeaderRequestID)}
	if r, ok := protocol.Lookup(code); ok {
		p["retryable"] = r.Retryable
	} else {
		p["retryable"] = false
	}
	for k, v := range extra {
		p[k] = v
	}
	if ra, ok := p["retry_after"]; ok {
		w.Header().Set(protocol.HeaderRetryAfter, fmt.Sprint(ra))
	}
	w.Header().Set(protocol.HeaderContentType, protocol.MediaProblem)
	w.Header().Set(protocol.HeaderErrorCode, code)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

// Problem answers the call with a problem.
func (c *Call) Problem(status int, code, detail string, extra map[string]any) {
	WriteProblem(c.W, status, code, detail, extra)
}

// Profile is the profile the call's answer is written under: the one the
// test chose with WriteUnder, else the current one.
func (c *Call) Profile() *profile.Profile {
	g := c.gw
	g.mu.Lock()
	defer g.mu.Unlock()
	exact := g.writers[c.Address]
	if exact == "" {
		head, _, _ := strings.Cut(c.Address, ":")
		exact = g.current[head]
	}
	return g.parsed[exact]
}

// JSON answers one JSON document under the call's profile.
func (c *Call) JSON(v ijson.Value) {
	b, err := ijson.Marshal(v)
	if err != nil {
		c.gw.t.Errorf("fakegw: %v", err)
		return
	}
	c.header(protocol.MediaJSON)
	c.W.WriteHeader(http.StatusOK)
	_, _ = c.W.Write(b)
}

func (c *Call) header(media string) {
	h := c.W.Header()
	h.Set(protocol.HeaderContentType, media)
	h.Set(protocol.HeaderCacheControl, protocol.NoStore)
	if p := c.Profile(); p != nil {
		if f, err := protocol.FormatExact(p.Exact()); err == nil {
			h.Set(protocol.HeaderProfile, f)
		}
	}
}

// Serve answers the call by the gateway's default behaviour.
func (c *Call) Serve() {
	switch c.Address {
	case "atlas.whoami:fetch":
		c.JSON(c.gw.whoami())
	case "atlas.describe:fetch":
		c.describe()
	case "atlas.catalog:fetch", "atlas.search:fetch":
		c.JSON(c.gw.catalog())
	case "atlas.guide:fetch":
		c.guide()
	case "atlas.access:fetch":
		c.JSON(mustParse(`{"items": [], "next_cursor": null}`))
	case "conformance.echo@v1:fetch":
		c.JSON(Echo(c.Body))
	case "conformance.rows@v1:fetch":
		n, _ := c.Body.Get("n")
		count, _ := n.(ijson.Number).Int64()
		c.Rows(count, nil, frame.End{})
	default:
		c.Problem(http.StatusNotImplemented, protocol.CodeInternal, "fakegw has no default answer for "+c.Address, nil)
	}
}

func mustParse(s string) *ijson.Object {
	v, err := ijson.Parse([]byte(s))
	if err != nil {
		panic(err)
	}
	return v.(*ijson.Object)
}

func (g *Gateway) whoami() *ijson.Object {
	o := mustParse(`{"principal": "00000000-0000-4000-8000-000000000001", "name": "Conformance", "kind": "person", "roles": [], "environment": "", "available_domains": ["conformance"], "house_rules": {"revision": "h1", "rules": []}}`)
	o.Set("environment", g.Env)
	return o
}

func (g *Gateway) catalog() *ijson.Object {
	g.mu.Lock()
	defer g.mu.Unlock()
	var items []ijson.Value
	pinned := make([]string, 0, len(g.current))
	for p := range g.current {
		pinned = append(pinned, p)
	}
	slices.Sort(pinned)
	for _, id := range pinned {
		p := g.parsed[g.current[id]]
		if p == nil || grammar.Builtin(p.ID) {
			continue
		}
		it := ijson.NewObject()
		it.Set("id", p.ID)
		it.Set("version", ijson.Number(strconv.Itoa(p.Version)))
		it.Set("exact", p.Exact().String())
		it.Set("summary", p.ID)
		it.Set("family", p.Family)
		ins := ijson.NewObject()
		for _, name := range p.Order {
			e := ijson.NewObject()
			e.Set("effect", p.Interactions[name].Effect)
			ins.Set(name, e)
		}
		it.Set("interactions", ins)
		items = append(items, it)
	}
	page := mustParse(`{"items": [], "next_cursor": null, "house_rules": {"revision": "h1", "rules": []}}`)
	page.Set("items", items)
	return page
}

func (c *Call) describe() {
	g := c.gw
	by, _ := c.Body.Get("by")
	g.mu.Lock()
	var exact string
	switch by {
	case "exact":
		e, _ := c.Body.Get("exact")
		exact, _ = e.(string)
	case "id":
		v, _ := c.Body.Get("id")
		id, _ := v.(string)
		if strings.Contains(id, "@") {
			exact = g.current[id]
		} else {
			best := 0
			for pinned, e := range g.current {
				p, err := grammar.ParsePinned(pinned)
				if err == nil && p.ID == id && p.Version > best {
					best, exact = p.Version, e
				}
			}
		}
	}
	doc := g.profiles[exact]
	p := g.parsed[exact]
	g.mu.Unlock()
	if doc == nil {
		c.Problem(http.StatusNotFound, protocol.CodeNotFound, "no capability "+exact, nil)
		return
	}
	interactions := ijson.NewObject()
	var eligibility []ijson.Value
	for _, name := range p.Order {
		held := ijson.NewObject()
		held.Set("held_by", "everyone")
		interactions.Set(name, held)
		e := mustParse(`{"interaction": "", "eligible": [], "ineligible": []}`)
		e.Set("interaction", name)
		var eligible []ijson.Value
		for _, b := range p.Interactions[name].Bindings {
			eb := ijson.NewObject()
			eb.Set("binding", b.Address)
			eligible = append(eligible, eb)
		}
		if eligible == nil {
			eligible = []ijson.Value{}
		}
		e.Set("eligible", eligible)
		eligibility = append(eligibility, e)
	}
	if eligibility == nil {
		eligibility = []ijson.Value{}
	}
	gov := mustParse(`{"capability": "", "governance_revision": "g1", "for_caller": {"interactions": {}, "limits": [], "approval": null}}`)
	gov.Set("capability", grammar.Pinned{ID: p.ID, Version: p.Version}.String())
	fc, _ := gov.Get("for_caller")
	fc.(*ijson.Object).Set("interactions", interactions)
	answer := ijson.NewObject()
	answer.Set("profile", ijson.Clone(doc))
	answer.Set("governance", gov)
	answer.Set("availability", "available")
	answer.Set("eligibility", eligibility)
	c.JSON(answer)
}

func (c *Call) guide() {
	id, _ := c.Body.Get("id")
	client, _ := c.Body.Get("client")
	s, _ := client.(string)
	if s == "" {
		s = "cli"
	}
	o := mustParse(`{"exact": "conformance.echo@v1#f17339273f0617c2", "client": "", "markdown": "", "tokens": 3, "truncated": false, "revisions": {}}`)
	o.Set("client", s)
	o.Set("markdown", fmt.Sprintf("# %v\n\nHow to call it from %s.\n", id, s))
	c.JSON(o)
}

// Echo is conformance.echo's default answer: the request, with count 1,
// kind plain, ratio and when null and tags [] when absent (README).
func Echo(req *ijson.Object) *ijson.Object {
	out := ijson.NewObject()
	for _, m := range []struct {
		name string
		def  ijson.Value
	}{{"text", ""}, {"count", ijson.Number("1")}, {"ratio", nil}, {"when", nil}, {"kind", "plain"}, {"tags", []ijson.Value{}}} {
		if v, ok := req.Get(m.name); ok {
			out.Set(m.name, v)
		} else {
			out.Set(m.name, m.def)
		}
	}
	return out
}

// Row is conformance.rows' row i (README): i, label row-<i>, amount i × 1.5,
// at 2026-01-01T00:00:00Z plus i seconds and i microseconds, day 2026-01-01
// plus i days, and share i/100, or null when i mod 5 is 4.
func Row(i int64) *ijson.Object {
	o := ijson.NewObject()
	o.Set("i", ijson.Number(strconv.FormatInt(i, 10)))
	o.Set("label", "row-"+strconv.FormatInt(i, 10))
	o.Set("amount", fmt.Sprintf("%d.%06d", i*3/2, i*3%2*500000))
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i)*time.Second + time.Duration(i)*time.Microsecond)
	o.Set("at", at.Format("2006-01-02T15:04:05.000000Z"))
	o.Set("day", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(i)).Format(time.DateOnly))
	if i%5 == 4 {
		o.Set("share", nil)
	} else {
		o.Set("share", ijson.Number(strconv.FormatFloat(float64(i)/100, 'g', -1, 64)))
	}
	return o
}

func project(row *ijson.Object, columns []string) *ijson.Object {
	if columns == nil {
		return row
	}
	out := ijson.NewObject()
	for _, c := range columns {
		if v, ok := row.Get(c); ok {
			out.Set(c, v)
		}
	}
	return out
}

func (c *Call) columns() []string {
	v, ok := c.Body.Get("columns")
	if !ok || v == nil {
		return nil
	}
	var out []string
	for _, x := range v.([]ijson.Value) {
		out = append(out, x.(string))
	}
	return out
}

// Rows answers n rows in the encoding Accept chooses, as they are made:
// NDJSON or Arrow end with end (its rows counted), a JSON array has no
// end. Without rows they are conformance.rows' own, projected to the
// request's columns; rows, when given, makes row i as the gateway sends it.
func (c *Call) Rows(n int64, rows func(i int64) *ijson.Object, end frame.End) {
	var columns []string
	if rows == nil {
		rows, columns = Row, c.columns()
	}
	media, ok := protocol.ChooseMedia(c.R.Header.Get(protocol.HeaderAccept), []string{protocol.MediaArrowStream, protocol.MediaNDJSON, protocol.MediaJSON})
	if !ok {
		c.Problem(http.StatusNotAcceptable, protocol.CodeInvalidRequest, "no encoding the client accepts", nil)
		return
	}
	c.header(media)
	flusher, _ := c.W.(http.Flusher)
	switch media {
	case protocol.MediaNDJSON:
		c.W.WriteHeader(http.StatusOK)
		nw := frame.NewNDJSONWriter(c.W)
		for i := int64(0); i < n; i++ {
			if err := nw.Row(project(rows(i), columns)); err != nil {
				return
			}
			if i%1024 == 1023 && nw.Flush() == nil && flusher != nil {
				flusher.Flush()
			}
		}
		_ = nw.End(end)
	case protocol.MediaArrowStream:
		p := c.Profile()
		_, name, _ := strings.Cut(c.Address, ":")
		in := p.Interactions[name]
		row := p.Schemas[in.Returns].Items()
		if columns != nil {
			row = row.Project(columns)
		} else if first := rows(0); n > 0 && first.Len() < len(row.Properties()) {
			row = row.Project(first.Names()) // scripted rows, projected as the gateway sends them
		}
		fields, err := arrowschema.Fields(row)
		if err != nil {
			c.Problem(http.StatusInternalServerError, protocol.CodeInternal, err.Error(), nil)
			return
		}
		md := arrow.NewMetadata([]string{arrowschema.MetaProfile, arrowschema.MetaProtocol}, []string{p.Exact().String(), protocol.Version})
		sch := arrow.NewSchema(fields, &md)
		c.W.WriteHeader(http.StatusOK)
		mem := memory.NewGoAllocator()
		aw, err := frame.NewArrowWriter(c.W, sch, mem)
		if err != nil {
			return
		}
		var batch []*ijson.Object
		write := func() bool {
			if len(batch) == 0 {
				return true
			}
			b, err := arrowschema.ToBatch(mem, sch, row, batch)
			if err != nil {
				return false
			}
			defer b.Release()
			batch = batch[:0]
			return aw.Write(b) == nil
		}
		for i := int64(0); i < n; i++ {
			batch = append(batch, project(rows(i), columns))
			if len(batch) == 4096 && !write() {
				return
			}
		}
		if write() {
			_ = aw.End(end)
		}
	default:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i := int64(0); i < n; i++ {
			if i > 0 {
				buf.WriteByte(',')
			}
			b, _ := ijson.Marshal(project(rows(i), columns))
			buf.Write(b)
		}
		buf.WriteByte(']')
		c.W.WriteHeader(http.StatusOK)
		_, _ = c.W.Write(buf.Bytes())
	}
}

// Hold answers nothing until the call's context ends or release is
// closed: a gateway that accepted a call and never answers.
func (c *Call) Hold(release <-chan struct{}) {
	select {
	case <-c.R.Context().Done():
	case <-release:
	}
}
