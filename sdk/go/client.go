// Package atlas is the Go client of the Atlas Protocol (§9.7): connect to a
// gateway from its bootstrap, sign in from the shared token store, find and
// describe capabilities, and call any interaction by its address with a
// request record, the same record the CLI, Python and Node send
// (ATLAS-CORE §6). Every document received is checked against the
// protocol's schemas and every answer against the capability's contract
// before it is used (§9.3 rule 1); an answer without its end is never
// complete (AP-HTTP-002). Every request is bounded by its call's deadline,
// to the end of its body.
package atlas

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/documents"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// Version is this client's version, sent in Atlas-Client.
const Version = "0.3.0"

// The client's own bounds on what has no call deadline of its own.
const (
	// BootstrapTimeout bounds reading the bootstrap at Connect.
	BootstrapTimeout = 30 * time.Second
	// RefreshTimeout bounds one redemption of a refresh token, within its
	// call's deadline.
	RefreshTimeout = 30 * time.Second
	// BuiltinDeadline is a built-in's deadline when the call names none
	// (each built-in's deadline.default, §9.3.2).
	BuiltinDeadline = 30 * time.Second

	maxBootstrap = 4 << 20
	maxProblem   = 1 << 20
	maxAnswer    = 256 << 20
	maxTokenFile = 64 << 10
	lockPoll     = 25 * time.Millisecond
	staleLock    = 2 * RefreshTimeout
)

// Options configure a connection. Every member is optional.
type Options struct {
	// URL is the gateway. Without it the gateway is Env's, else ATLAS_URL,
	// else ATLAS_ENV's, else config.toml's env's (§9.7.2).
	URL string
	// Env names an environment: its gateway is config.toml's
	// [environments.<name>] url.
	Env string
	// Home is the client's directory (ATLAS_HOME, else ~/.atlas): its
	// sign-in under state/, its caches under cache/, config.toml.
	Home string
	// Token is an access token to use instead of the token store. It is
	// sent only to the gateway connected to.
	Token string
	// Name and Version name the client in Atlas-Client.
	Name, Version string
	// Features are what the client implements (Atlas-Client-Features).
	Features *protocol.Features
	// HTTP is the HTTP client. It needs no timeout: every request is
	// bounded by its call's deadline, to the end of its body. Its redirects
	// are never followed.
	HTTP  *http.Client
	Clock clock.Clock
	// Interactive says a person is at the client (X-Atlas-Interactive).
	Interactive bool
	// Warn is the client's warning hook (§9.7.4 step 11): each warning an
	// answer carries, a deprecation or a truncation, as it is read. Every
	// warning is also on the call's Result.
	Warn func(Warning)
	// Environment, when given, is read in place of the process's own
	// variables (ATLAS_URL, ATLAS_ENV, ATLAS_HOME, ATLAS_TOKEN): an engine
	// connects with its face's.
	Environment map[string]string
}

// DefaultFeatures are what this client implements.
func DefaultFeatures() protocol.Features {
	return protocol.Features{
		Levels:     []string{protocol.LevelCore, "streaming", "columnar"},
		Deliveries: []string{protocol.DeliverySingle, "chunked", protocol.DeliveryOperation},
		Encodings:  []string{protocol.EncodingJSON, "ndjson", "arrow"},
		Lens:       []string{"rename", "alias_value", "default"},
	}
}

// Client is a connection to one gateway.
type Client struct {
	opt       Options
	origin    string // the gateway connected to: scheme://host[:port]
	features  string
	clientHdr string
	bootstrap *ijson.Object
	env       string
	servers   map[string]string
	tokens    *tokenStore
	jitter    func() float64 // in [0, 1): a retry's share of its 25% jitter

	mu       sync.Mutex
	profiles map[string]*describedProfile // by exact id, forever (gaps/2.md D4.1)
	current  map[string]string            // pinned id → the exact id the gateway last named current (D4.3)
	bare     map[string]string            // capability id → its current pinned id, for this process only
}

// Connect reads the gateway's bootstrap (§9.7.2). It does not sign in: the
// first call does.
func Connect(ctx context.Context, opt Options) (*Client, error) {
	env, err := readEnvironment(opt.Home, opt.Environment)
	if err != nil {
		return nil, err
	}
	if opt.Home == "" {
		opt.Home = env.Home
	}
	if opt.Token == "" {
		opt.Token = env.Token
	}
	gateway, err := chooseGateway(opt, env)
	if err != nil {
		return nil, err
	}
	origin, err := Origin(gateway)
	if err != nil {
		return nil, err
	}
	if opt.Name == "" {
		opt.Name = "atlas-go"
	}
	if opt.Version == "" {
		opt.Version = Version
	}
	// A call goes to its binding and nowhere else (CORE-CALL-003, R26): the
	// client follows no redirect, which could carry a token and a body to
	// another place. The caller's client is copied, never changed.
	h := http.Client{}
	if opt.HTTP != nil {
		h = *opt.HTTP
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	opt.HTTP = &h
	if opt.Clock == nil {
		opt.Clock = clock.System
	}
	features := DefaultFeatures()
	if opt.Features != nil {
		features = *opt.Features
	}
	c := &Client{opt: opt, origin: origin, jitter: mrand.Float64, profiles: map[string]*describedProfile{}, current: map[string]string{}, bare: map[string]string{}}
	if c.features, err = features.Format(); err != nil {
		return nil, fmt.Errorf("atlas: the client's features: %w", err)
	}
	c.clientHdr = fmt.Sprintf(`name=%q, version=%q, protocol=%q`, opt.Name, opt.Version, protocol.Version)
	if err := c.readBootstrap(ctx, strings.TrimRight(gateway, "/")); err != nil {
		return nil, err
	}
	c.tokens = &tokenStore{dir: filepath.Join(opt.Home, "state"), env: c.env, origin: origin, signIn: c.signInOf(), explicit: opt.Token, clock: opt.Clock, http: opt.HTTP}
	c.loadPointers()
	return c, nil
}

// chooseGateway is §9.7.2's order: the URL or environment given to
// connect, ATLAS_URL, ATLAS_ENV, config.toml's env. With none of them the
// client refuses to choose (ATLAS-CORE §3: a deliberate selection, never a
// silent default), and this build names no Production URL of its own.
func chooseGateway(opt Options, env environment) (string, error) {
	named := func(name, from string) (string, error) {
		if u := env.Environments[name]; u != "" {
			return u, nil
		}
		return "", protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("no gateway for the environment %q (%s): config.toml names none", name, from)).WithTarget("env").WithNext(fmt.Sprintf("Add [environments.%s] url = \"https://…\" to %s, or name the gateway: atlas --gateway <url>.", name, filepath.Join(env.Home, "config.toml")))
	}
	switch {
	case opt.URL != "":
		return opt.URL, nil
	case opt.Env != "":
		return named(opt.Env, "given to connect")
	case env.URL != "":
		return env.URL, nil
	case env.Env != "":
		return named(env.Env, "ATLAS_ENV")
	case env.ConfigEnv != "":
		return named(env.ConfigEnv, "config.toml's env")
	}
	return "", protocol.NewProblem(protocol.CodeInvalidRequest, "no gateway: name one, or set ATLAS_URL or ATLAS_ENV").WithTarget("url").WithNext("atlas --gateway <url>, export ATLAS_URL, or set env in config.toml.")
}

// Origin is a gateway URL's origin, the identity a sign-in is bound to:
// its scheme, host and port, lowercased, the default port left out.
func Origin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Opaque != "" {
		return "", protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("%q is not a gateway URL: an http or https URL with a host", raw)).WithTarget("url")
	}
	scheme, host, port := strings.ToLower(u.Scheme), strings.ToLower(u.Hostname()), u.Port()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port == "" || scheme == "http" && port == "80" || scheme == "https" && port == "443" {
		return scheme + "://" + host, nil
	}
	return scheme + "://" + host + ":" + port, nil
}

// loopback reports whether an origin is this machine.
func loopback(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	h := u.Hostname()
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// secure reports whether a credential may travel to an origin: HTTPS, or
// HTTP to this machine (R26; RFC 8252 §8.3's loopback exception).
func secure(origin string) bool {
	return strings.HasPrefix(origin, "https://") || strings.HasPrefix(origin, "http://") && loopback(origin)
}

// Environment is the gateway's environment, from its bootstrap.
func (c *Client) Environment() string { return c.env }

// Gateway is the origin of the gateway connected to.
func (c *Client) Gateway() string { return c.origin }

// Bootstrap is the gateway's bootstrap document.
func (c *Client) Bootstrap() *ijson.Object { return c.bootstrap }

func (c *Client) signInOf() signIn {
	v, _ := c.bootstrap.Get("sign_in")
	o, _ := v.(*ijson.Object)
	str := func(name string) string {
		if o == nil {
			return ""
		}
		s, _ := o.Get(name)
		t, _ := s.(string)
		return t
	}
	s := signIn{Authority: str("authority"), ClientID: str("client_id"), TokenEndpoint: str("token_endpoint")}
	if o != nil {
		scopes, _ := o.Get("scopes")
		for _, x := range scopes.([]ijson.Value) {
			s.Scopes = append(s.Scopes, x.(string))
		}
	}
	return s
}

// readBootstrap is GET /.well-known/atlas, without a token, revalidated
// against the copy cached under cache/ (§9.6.1), within BootstrapTimeout.
func (c *Client) readBootstrap(ctx context.Context, base string) error {
	ctx, cancel := context.WithTimeout(ctx, BootstrapTimeout)
	defer cancel()
	cache := filepath.Join(c.opt.Home, "cache", "bootstrap-"+url.PathEscape(c.origin)+".json")
	var cached struct {
		ETag string          `json:"etag"`
		Body json.RawMessage `json:"body"`
	}
	if b, err := readBounded(cache, maxBootstrap+1024); err == nil {
		if json.Unmarshal(b, &cached) != nil {
			cached.ETag, cached.Body = "", nil // a damaged cache is no cache
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+protocol.WellKnownPath, nil)
	if err != nil {
		return protocol.NewProblem(protocol.CodeInvalidRequest, "the gateway URL: "+err.Error()).WithTarget("url")
	}
	if cached.ETag != "" && len(cached.Body) > 0 {
		req.Header.Set(protocol.HeaderIfNoneMatch, cached.ETag)
	}
	res, err := c.opt.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return protocol.NewProblem(protocol.CodeDeadlineExceeded, fmt.Sprintf("the gateway's bootstrap did not answer within %s", BootstrapTimeout)).WithNext("Check the gateway's URL and your network.")
		}
		return protocol.NewProblem(protocol.CodeUnavailable, "the gateway cannot be reached: "+err.Error()).WithRetryAfter(5).WithNext("Check the gateway's URL and your network.")
	}
	defer drain(res)
	var body []byte
	switch res.StatusCode {
	case http.StatusNotModified:
		body = cached.Body
	case http.StatusOK:
		if body, err = io.ReadAll(io.LimitReader(res.Body, maxBootstrap+1)); err != nil {
			if ctx.Err() != nil {
				return protocol.NewProblem(protocol.CodeDeadlineExceeded, fmt.Sprintf("the gateway's bootstrap did not arrive within %s", BootstrapTimeout))
			}
			return protocol.NewProblem(protocol.CodeUpstreamFailed, "the bootstrap was cut short: "+err.Error())
		}
		if len(body) > maxBootstrap {
			return protocol.NewProblem(protocol.CodeInvalidDocument, fmt.Sprintf("the bootstrap is larger than %d bytes", maxBootstrap))
		}
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return protocol.NewProblem(protocol.CodeUnavailable, fmt.Sprintf("the gateway's bootstrap redirects (%d) to %s, which is not followed", res.StatusCode, res.Header.Get("Location"))).WithRetryAfter(5).WithNext("Connect to the gateway's own URL.")
	default:
		return protocol.NewProblem(protocol.CodeUnavailable, fmt.Sprintf("the gateway's bootstrap answered %d", res.StatusCode)).WithRetryAfter(5)
	}
	v, err := ijson.Parse(body)
	if err != nil {
		return protocol.NewProblem(protocol.CodeInvalidDocument, "the bootstrap is not I-JSON: "+err.Error())
	}
	if p := checkDocument("bootstrap", v); p != nil {
		return p
	}
	b := v.(*ijson.Object)
	env, _ := b.Get("environment")
	servers := map[string]string{}
	var bad error
	sv, _ := b.Get("servers")
	sv.(*ijson.Object).Each(func(name string, s ijson.Value) bool {
		u, _ := s.(*ijson.Object).Get("url")
		if _, err := Origin(u.(string)); err != nil {
			bad = protocol.NewProblem(protocol.CodeInvalidDocument, fmt.Sprintf("the bootstrap's server %s has no http or https URL", name)).WithTarget("/servers/" + name + "/url")
			return false
		}
		servers[name] = strings.TrimRight(u.(string), "/")
		return true
	})
	if bad != nil {
		return bad
	}
	if res.StatusCode == http.StatusOK {
		if etag := res.Header.Get(protocol.HeaderETag); etag != "" {
			if saved, err := json.Marshal(map[string]any{"etag": etag, "body": json.RawMessage(body)}); err == nil {
				_ = writeFileAtomic(cache, saved, 0o600) // a cache that cannot be written is only slower
			}
		}
	}
	c.bootstrap, c.env, c.servers = b, env.(string), servers
	return nil
}

// checkDocument validates a document the gateway sent against its schema,
// first mapping each enum value this client does not know to UNKNOWN where
// the enum has it (§9.7.10, Q-DOC-1): a newer gateway never fails an older
// client.
func checkDocument(name string, v ijson.Value) *protocol.Problem {
	fs := documents.Validate(name, v)
	for tries := 0; len(fs) > 0 && tries < 64; tries++ {
		f := fs[0]
		if f.Keyword != "enum" || !setPointer(v, f.Pointer, "UNKNOWN") {
			break
		}
		next := documents.Validate(name, v)
		if len(next) > 0 && next[0].Pointer == f.Pointer && next[0].Keyword == "enum" {
			break // UNKNOWN is not in this enum either
		}
		fs = next
	}
	if len(fs) > 0 {
		return protocol.NewProblem(protocol.CodeInvalidDocument, fmt.Sprintf("the %s breaks its schema at %s (%s)", name, fs[0].Pointer, fs[0].Keyword)).WithTarget(fs[0].Pointer)
	}
	return nil
}

// setPointer sets the string at a JSON Pointer, when one is there.
func setPointer(v ijson.Value, pointer string, to string) bool {
	if pointer == "" {
		return false
	}
	parts := strings.Split(pointer[1:], "/")
	for i, p := range parts {
		p = strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")
		last := i == len(parts)-1
		switch x := v.(type) {
		case *ijson.Object:
			m, ok := x.Get(p)
			if !ok {
				return false
			}
			if last {
				if _, isString := m.(string); !isString {
					return false
				}
				x.Set(p, to)
				return true
			}
			v = m
		case []ijson.Value:
			n, err := strconv.Atoi(p)
			if err != nil || n < 0 || n >= len(x) {
				return false
			}
			if last {
				if _, isString := x[n].(string); !isString {
					return false
				}
				x[n] = to
				return true
			}
			v = x[n]
		default:
			return false
		}
	}
	return false
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func traceparent() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return "00-" + hex.EncodeToString(b[:16]) + "-" + hex.EncodeToString(b[16:]) + "-01"
}

// readBounded reads a file of at most n bytes.
func readBounded(path string, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, n+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > n {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, n)
	}
	return b, nil
}

// writeFileAtomic writes a file whole or not at all: a temporary file in
// the same directory, synced, then renamed over it.
func writeFileAtomic(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// signIn is a bootstrap's sign_in: who issues the gateway's tokens, and
// where a refresh token is redeemed.
type signIn struct {
	Authority, ClientID, TokenEndpoint string
	Scopes                             []string
}

// Binding is what a stored sign-in was issued for (R26): the gateway it
// was made at, and that gateway's sign_in then. The client sends the
// credential only to that gateway, and redeems its refresh token only at
// that token endpoint, so an unauthenticated bootstrap can never redirect
// it. The authority names the tenant (Entra's authority is per tenant),
// and the scopes name the audience.
type Binding struct {
	Gateway       string   `json:"gateway"`
	Environment   string   `json:"environment"`
	Authority     string   `json:"authority"`
	ClientID      string   `json:"client_id"`
	Audience      string   `json:"audience,omitempty"`
	Scopes        []string `json:"scopes"`
	TokenEndpoint string   `json:"token_endpoint"`
}

// audience is the resource the scopes are for ("api://atlas-dev" for
// "api://atlas-dev/.default"), or "" when they name none or differ.
func audience(scopes []string) string {
	var out string
	for i, s := range scopes {
		scheme, rest, ok := strings.Cut(s, "://")
		j := strings.LastIndex(rest, "/")
		if !ok || scheme == "" || j <= 0 {
			return ""
		}
		a := scheme + "://" + rest[:j]
		if i > 0 && a != out {
			return ""
		}
		out = a
	}
	return out
}

func (s signIn) binding(gateway, env string) *Binding {
	return &Binding{Gateway: gateway, Environment: env, Authority: s.Authority, ClientID: s.ClientID, Audience: audience(s.Scopes), Scopes: slices.Clone(s.Scopes), TokenEndpoint: s.TokenEndpoint}
}

// tokenStore is the shared sign-in: <home>/state/token-<environment>.json,
// readable by its owner only (§9.7.3, CORE-SIGNIN-005), each bound to the
// gateway it was made at (R26). Refreshes are serialised in the process
// and, by a lock file beside the token, between processes.
type tokenStore struct {
	dir, env, explicit string
	origin             string // the gateway connected to
	signIn             signIn // its bootstrap's sign_in
	clock              clock.Clock
	http               *http.Client
	mu                 sync.Mutex
}

type tokenFile struct {
	AccessToken  string   `json:"access_token"`
	ExpiresAt    int64    `json:"expires_at"` // 0 when the token's life is not known
	RefreshToken string   `json:"refresh_token,omitempty"`
	Binding      *Binding `json:"binding,omitempty"`
}

func (t *tokenStore) path() string { return filepath.Join(t.dir, "token-"+t.env+".json") }

func signInAgain(origin string) string {
	return "Sign in to this gateway: atlas --gateway " + origin + " login --token <token>."
}

// token is the access token to send: the stored one, refreshed first when
// it ends within a minute or is the one the gateway refused (failed).
func (t *tokenStore) token(ctx context.Context, failed string) (string, error) {
	if t.explicit != "" {
		return t.explicit, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	f, err := t.usable()
	if err != nil {
		return "", err
	}
	if !t.due(f, failed) {
		return f.AccessToken, nil
	}
	unlock, err := t.lock(ctx)
	if err != nil {
		return "", err
	}
	defer unlock()
	if f, err = t.usable(); err != nil { // another program may have refreshed it while this one waited
		return "", err
	}
	if !t.due(f, failed) {
		return f.AccessToken, nil
	}
	if f, err = t.refresh(ctx, f); err != nil {
		return "", err
	}
	return f.AccessToken, nil
}

// due reports whether a stored token is to be refreshed first.
func (t *tokenStore) due(f *tokenFile, failed string) bool {
	if f.RefreshToken == "" {
		return false
	}
	return failed != "" && f.AccessToken == failed || f.ExpiresAt != 0 && t.clock.Now().Add(time.Minute).Unix() >= f.ExpiresAt
}

// usable is the stored sign-in, when it may be used with this gateway.
func (t *tokenStore) usable() (*tokenFile, error) {
	f, err := t.read()
	if err != nil {
		return nil, err
	}
	if err := t.trusted(f); err != nil {
		return nil, err
	}
	return f, nil
}

// trusted reports whether a stored sign-in may be sent to the gateway
// connected to (R26). A bound sign-in is used only at the gateway it was
// made at, while that gateway's sign_in is the one it was issued under. An
// unbound one (written by SignIn, an older client or the conformance
// suite) is used only by a development or conformance gateway on this
// machine. Neither is ever sent in the clear off this machine.
func (t *tokenStore) trusted(f *tokenFile) error {
	if !secure(t.origin) {
		return protocol.NewProblem(protocol.CodeUnauthenticated, t.origin+" is plain HTTP and not on this machine, so no sign-in is sent to it").WithNext("Use the gateway's https URL.")
	}
	b := f.Binding
	if b == nil {
		if loopback(t.origin) && (t.env == "local" || t.env == "conformance") {
			return nil
		}
		return protocol.NewProblem(protocol.CodeUnauthenticated, fmt.Sprintf("the saved sign-in for %s is not bound to a gateway, so it is not sent to %s", t.env, t.origin)).WithNext(signInAgain(t.origin))
	}
	if b.Gateway != t.origin {
		return protocol.NewProblem(protocol.CodeUnauthenticated, fmt.Sprintf("the saved sign-in for %s was made at %s, not at %s, so it is not sent there", t.env, b.Gateway, t.origin)).WithNext(signInAgain(t.origin) + " Or connect to " + b.Gateway + ".")
	}
	now := t.signIn.binding(t.origin, t.env)
	for _, m := range []struct{ name, was, is string }{
		{"environment", b.Environment, now.Environment}, {"authority", b.Authority, now.Authority}, {"client_id", b.ClientID, now.ClientID},
		{"audience", b.Audience, now.Audience}, {"token_endpoint", b.TokenEndpoint, now.TokenEndpoint},
		{"scopes", strings.Join(sorted(b.Scopes), " "), strings.Join(sorted(now.Scopes), " ")},
	} {
		if m.was != m.is {
			return protocol.NewProblem(protocol.CodeUnauthenticated, fmt.Sprintf("%s's sign-in has changed since you signed in (its %s), so the saved sign-in is not used", t.origin, m.name)).WithNext("Sign in again: atlas login --token <token>.")
		}
	}
	return nil
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}

func (t *tokenStore) read() (*tokenFile, error) {
	st, err := os.Stat(t.path())
	if err != nil {
		return nil, errNoSignIn(t.origin)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, protocol.NewProblem(protocol.CodeUnauthenticated, t.path()+" can be read by others, so its token is not used").WithNext("chmod 600 the file, or sign in again.")
	}
	b, err := readBounded(t.path(), maxTokenFile)
	if err != nil {
		return nil, errNoSignIn(t.origin)
	}
	var f tokenFile
	if err := json.Unmarshal(b, &f); err != nil || f.AccessToken == "" {
		return nil, protocol.NewProblem(protocol.CodeUnauthenticated, t.path()+" holds no token").WithNext("Sign in again: atlas login --token <token>.")
	}
	return &f, nil
}

func errNoSignIn(origin string) error {
	return protocol.NewProblem(protocol.CodeUnauthenticated, "you are not signed in to "+origin).WithNext(signInAgain(origin))
}

// lock serialises refreshes between programs sharing the store: a lock
// file beside the token, made exclusively, removed when done. A lock older
// than staleLock is a crashed holder's and is broken.
func (t *tokenStore) lock(ctx context.Context) (func(), error) {
	path := t.path() + ".lock"
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, werr := fmt.Fprintf(f, "%d\n", os.Getpid())
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				_ = os.Remove(path)
				return nil, protocol.NewProblem(protocol.CodeUnavailable, "the sign-in's lock cannot be written: "+werr.Error()).WithRetryAfter(1)
			}
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, protocol.NewProblem(protocol.CodeUnavailable, "the sign-in cannot be locked: "+err.Error()).WithRetryAfter(1)
		}
		if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) > staleLock {
			_ = os.Remove(path)
			continue
		}
		timer := time.NewTimer(lockPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, protocol.NewProblem(protocol.CodeDeadlineExceeded, "the call's deadline passed while another program renewed the sign-in")
		case <-timer.C:
		}
	}
}

// refresh redeems the refresh token at the token endpoint the sign-in was
// bound to (RFC 6749 §6), within RefreshTimeout, and rewrites the file
// whole, binding kept. An unbound sign-in is redeemed only at a token
// endpoint on the gateway's own origin.
func (t *tokenStore) refresh(ctx context.Context, f *tokenFile) (*tokenFile, error) {
	endpoint := t.signIn.TokenEndpoint
	if f.Binding != nil {
		endpoint = f.Binding.TokenEndpoint
	}
	if endpoint == "" {
		return f, nil
	}
	eo, err := Origin(endpoint)
	if err != nil || !secure(eo) || f.Binding == nil && eo != t.origin {
		return nil, protocol.NewProblem(protocol.CodeUnauthenticated, "the sign-in cannot be renewed at "+endpoint+": a refresh token goes only to the token endpoint it was issued for").WithNext("Sign in again: atlas login --token <token>.")
	}
	ctx, cancel := context.WithTimeout(ctx, RefreshTimeout)
	defer cancel()
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {f.RefreshToken}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeUnauthenticated, "the token endpoint: "+err.Error())
	}
	req.Header.Set(protocol.HeaderContentType, "application/x-www-form-urlencoded")
	res, err := t.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, protocol.NewProblem(protocol.CodeDeadlineExceeded, "the token endpoint did not answer before the call's deadline")
		}
		return nil, protocol.NewProblem(protocol.CodeUnavailable, "the token endpoint cannot be reached: "+err.Error()).WithRetryAfter(5)
	}
	defer drain(res)
	var got struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, maxProblem)).Decode(&got) != nil || got.AccessToken == "" {
		if ctx.Err() != nil {
			return nil, protocol.NewProblem(protocol.CodeDeadlineExceeded, "the token endpoint did not answer before the call's deadline")
		}
		return nil, protocol.NewProblem(protocol.CodeUnauthenticated, fmt.Sprintf("the sign-in could not be renewed (the token endpoint answered %d)", res.StatusCode)).WithNext("Sign in again: atlas login --token <token>.")
	}
	next := &tokenFile{AccessToken: got.AccessToken, RefreshToken: got.RefreshToken, Binding: f.Binding}
	if next.RefreshToken == "" {
		next.RefreshToken = f.RefreshToken
	}
	if got.ExpiresIn > 0 {
		next.ExpiresAt = t.clock.Now().Unix() + got.ExpiresIn
	}
	b, err := json.Marshal(next)
	if err != nil {
		return nil, fmt.Errorf("atlas: the token file: %w", err)
	}
	if err := writeFileAtomic(t.path(), b, 0o600); err != nil {
		return nil, protocol.NewProblem(protocol.CodeUnavailable, "the renewed sign-in cannot be saved: "+err.Error()).WithRetryAfter(1)
	}
	return next, nil
}

// SignIn writes the token store for env, bound to no gateway: what a test
// or a tool that predates the binding writes. Such a sign-in is used only
// by a development or conformance gateway on this machine (R26); a person
// signs in with Client.SignIn.
func SignIn(home, env, access, refresh string, expiresIn time.Duration) error {
	if err := grammar.Segment(env); err != nil {
		return protocol.NewProblem(protocol.CodeInvalidRequest, "not an environment name: "+err.Error()).WithTarget("env")
	}
	return writeToken(home, env, &tokenFile{AccessToken: access, RefreshToken: refresh}, expiresIn, time.Now())
}

// SignIn writes the token store for the gateway connected to, bound to it
// and to its bootstrap's sign_in (R26): what `atlas login` does with a
// token it was given. The token is then sent to this gateway only, and its
// refresh token redeemed only at this sign_in's token endpoint.
func (c *Client) SignIn(access, refresh string, expiresIn time.Duration) error {
	if !secure(c.origin) {
		return protocol.NewProblem(protocol.CodeInvalidRequest, c.origin+" is plain HTTP and not on this machine: sign in only over https").WithTarget("url").WithNext("Use the gateway's https URL.")
	}
	if access == "" {
		return protocol.NewProblem(protocol.CodeInvalidRequest, "a sign-in needs an access token").WithTarget("--token")
	}
	return writeToken(c.opt.Home, c.env, &tokenFile{AccessToken: access, RefreshToken: refresh, Binding: c.signInOf().binding(c.origin, c.env)}, expiresIn, c.opt.Clock.Now())
}

// SignOut forgets the stored sign-in for the gateway connected to: one
// bound to it, or an unbound one this gateway would use. A sign-in bound to
// another gateway is left as it is, and so is none at all.
func (c *Client) SignOut() error {
	f, err := c.tokens.read()
	if err != nil {
		return nil
	}
	if f.Binding != nil && f.Binding.Gateway != c.origin || f.Binding == nil && c.tokens.trusted(f) != nil {
		return protocol.NewProblem(protocol.CodeInvalidRequest, "the saved sign-in for "+c.env+" is not this gateway's, so it is kept").WithTarget("url")
	}
	if err := os.Remove(c.tokens.path()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("atlas: the sign-in could not be removed: %w", err)
	}
	return nil
}

func writeToken(home, env string, f *tokenFile, expiresIn time.Duration, now time.Time) error {
	if expiresIn > 0 {
		f.ExpiresAt = now.Add(expiresIn).Unix()
	}
	b, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("atlas: the token file: %w", err)
	}
	dir := filepath.Join(home, "state")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("atlas: the token store: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("atlas: the token store: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(dir, "token-"+env+".json"), b, 0o600); err != nil {
		return fmt.Errorf("atlas: the token store: %w", err)
	}
	return nil
}
