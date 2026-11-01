// Package edge is the gateway's HTTP binding (§9.6): the bootstrap, a POST
// to every interaction's binding, each Atlas header in its Structured Field
// type, the answer's encoding chosen from Accept and the client's declared
// levels, and each delivery framed as §9.6.5 frames it. In front of it are
// the transport middleware of §6.7.2, declared in one list in their order
// (ATLAS-MECH-001): nothing there decides access. The edge reads the wire
// and writes the wire; every decision is the pipeline's. Every failure, here
// or in the pipeline, is one problem document (§4.1), and nothing the edge
// answers before the pipeline has decided access depends on whether an
// address is bound (ATLAS-CORE §8, PR08).
package edge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/core/pipeline"
	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/health"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/lifecycle"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/middleware"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/problem"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/telemetry"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// SignIn is how a client signs in (bootstrap.schema.json's sign_in).
type SignIn struct {
	Authority, ClientID, TokenEndpoint                 string
	AuthorizationEndpoint, DeviceAuthorizationEndpoint string
	Scopes                                             []string
}

// Options configure the edge.
type Options struct {
	Pipeline *pipeline.Pipeline
	// URL is the gateway server's public base URL, without a trailing /.
	URL    string
	SignIn SignIn
	// MaxBody is the largest request body (default 1 MiB, §6.7.2 row 8).
	MaxBody int64
	Clock   clock.Clock
	Log     *slog.Logger
	// Serving is how the deployment serves: readiness, the checks behind
	// it, the origins and proxies it trusts, and the limits that keep one
	// replica's resources bounded.
	Serving
}

// Serving are a deployment's settings for the edge (§6.7.2, §6.7.15). The
// zero value is a development edge: always ready, no browser origins, the
// peer's address as the client's, and the default limits.
type Serving struct {
	// Ready is the process's readiness. /readyz reports it, and while it is
	// false every call is answered unavailable with Retry-After 2 and
	// Connection: close (draining, §6.7.2 row 7, ATLAS-MECH-016). Nil is
	// always ready.
	Ready *lifecycle.Readiness
	// Checks are /readyz's further named checks (the audit sink, the policy
	// source), each bounded to 2 s (§6.7.15).
	Checks []health.Check
	// Origins are the web origins a browser may call the gateway from, each
	// scheme://host[:port]: https, or http on a loopback host. Never a
	// wildcard (§6.2.9). None lets no browser read an answer.
	Origins []string
	// TrustedProxies are the ingress's address ranges. ClientIPHeader
	// (default X-Azure-ClientIP) names the client only when the peer is one
	// of them (§6.7.2 row 3).
	TrustedProxies []netip.Prefix
	ClientIPHeader string
	// BodyTimeout bounds how long a request body may take to arrive, and the
	// call's deadline bounds it further (default 30 s, R066).
	BodyTimeout time.Duration
	// WriteGrace is how long after a call's deadline its answer may still be
	// written: the end line or final batch that reports the deadline
	// (default 5 s, R066).
	WriteGrace time.Duration
	// MaxAnswer is the largest JSON answer the edge holds to send whole
	// (default 16 MiB). A table larger than this is refused in JSON, never
	// cut (R109).
	MaxAnswer int
	// MaxInFlight is how many requests one replica serves at once; a stream
	// holds its place until it ends (default 512, §6.7.2 row 11).
	MaxInFlight int
	// PreAuthRate and PreAuthBurst are each client address's allowance of
	// calls refused as unauthenticated: a burst, then a rate a second
	// (defaults 100 and 10, §6.7.2 row 10). PreAuthClients bounds how many
	// addresses are tracked at once (default 65536).
	PreAuthRate    float64
	PreAuthBurst   int
	PreAuthClients int
	// Metrics receives each request's rate, errors and duration by address
	// (§6.7.2 row 6). Nil discards them.
	Metrics telemetry.Recorder
}

// The defaults of Options and Serving.
const (
	DefaultMaxBody        = 1 << 20
	DefaultBodyTimeout    = 30 * time.Second
	DefaultWriteGrace     = 5 * time.Second
	DefaultMaxAnswer      = 16 << 20
	DefaultMaxInFlight    = 512
	DefaultPreAuthRate    = 10
	DefaultPreAuthBurst   = 100
	DefaultPreAuthClients = 1 << 16
	// DefaultClientIPHeader is the header Azure Front Door names the
	// client in.
	DefaultClientIPHeader = "X-Azure-ClientIP"
	// readyCheckTimeout bounds each of /readyz's checks (§6.7.15).
	readyCheckTimeout = 2 * time.Second
	// fallbackCeiling caps a call's outer deadline when the catalogue
	// declares no interaction maximum at all.
	fallbackCeiling = 2 * time.Minute
)

// Server is the HTTP binding.
type Server struct {
	opt      Options
	origins  map[string]bool
	layers   []layer
	handler  http.Handler
	live     http.Handler
	ready    http.Handler
	inflight chan struct{}
	unauth   *buckets
	boot     atomic.Pointer[bootstrapDoc]
	ceil     atomic.Pointer[ceiling]
}

// Open is the edge, or why its options cannot serve.
func Open(opt Options) (*Server, error) {
	if opt.Pipeline == nil {
		return nil, errors.New("edge: a pipeline is required")
	}
	if opt.MaxBody <= 0 {
		opt.MaxBody = DefaultMaxBody
	}
	if opt.Clock == nil {
		opt.Clock = clock.System
	}
	if opt.Log == nil {
		opt.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opt.ClientIPHeader == "" {
		opt.ClientIPHeader = DefaultClientIPHeader
	}
	if opt.BodyTimeout <= 0 {
		opt.BodyTimeout = DefaultBodyTimeout
	}
	if opt.WriteGrace <= 0 {
		opt.WriteGrace = DefaultWriteGrace
	}
	if opt.MaxAnswer <= 0 {
		opt.MaxAnswer = DefaultMaxAnswer
	}
	if opt.MaxInFlight <= 0 {
		opt.MaxInFlight = DefaultMaxInFlight
	}
	if opt.PreAuthRate <= 0 {
		opt.PreAuthRate = DefaultPreAuthRate
	}
	if opt.PreAuthBurst <= 0 {
		opt.PreAuthBurst = DefaultPreAuthBurst
	}
	if opt.PreAuthClients <= 0 {
		opt.PreAuthClients = DefaultPreAuthClients
	}
	if opt.Metrics == nil {
		opt.Metrics = telemetry.NoopRecorder{}
	}
	opt.URL = strings.TrimRight(opt.URL, "/")
	origins, err := parseOrigins(opt.Origins)
	if err != nil {
		return nil, err
	}
	s := &Server{
		opt:      opt,
		origins:  origins,
		inflight: make(chan struct{}, opt.MaxInFlight),
		unauth:   newBuckets(opt.PreAuthRate, opt.PreAuthBurst, opt.PreAuthClients),
	}
	s.live = health.Handler("livez", opt.Log, health.Ping)
	s.ready = s.readiness()
	s.layers = s.chain()
	h := http.Handler(http.HandlerFunc(s.route))
	for i := len(s.layers) - 1; i >= 0; i-- {
		h = s.layers[i].wrap(h)
	}
	s.handler = h
	return s, nil
}

// New is Open for options a deployment's code fixes. It panics when they
// cannot serve; a deployment that reads them from its settings calls Open.
func New(opt Options) *Server {
	s, err := Open(opt)
	if err != nil {
		panic(err)
	}
	return s
}

// ServeHTTP serves every request through the edge's middleware (§6.7.2).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	x := &exchange{start: s.opt.Clock.Now(), probe: isProbe(r)}
	x.w = &writer{ResponseWriter: middleware.Wrap(w), x: x, r: r}
	s.handler.ServeHTTP(x.w, r.WithContext(context.WithValue(r.Context(), exchangeKey{}, x)))
}

// writer is the edge's response writer: kit's, which records the status
// and the bytes sent, made to close an HTTP/1 connection after an answer
// sent before the request's body was read to its end. net/http would
// otherwise read what is left of the body before it sends the answer, and
// a client that trickles its body would hold the connection and its
// goroutine for as long as it liked (R048, R066). HTTP/2 never reads a
// body it was not asked to, and there Connection: close would end every
// other stream of the connection.
type writer struct {
	*middleware.ResponseWriter
	x *exchange
	r *http.Request
}

func (w *writer) WriteHeader(code int) {
	if !w.Written() && w.r.ProtoMajor < 2 && w.r.ContentLength != 0 && !w.x.read {
		w.Header().Set("Connection", "close")
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *writer) Write(b []byte) (int, error) {
	if !w.Written() {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Flush sends what is buffered.
func (w *writer) Flush() { _ = w.FlushError() }

// FlushError sends what is buffered, and says whether it reached the
// connection: http.ResponseController asks for it before Flush, so a
// stream learns at once that its caller has gone.
func (w *writer) FlushError() error {
	if !w.Written() {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter.Unwrap()).Flush()
}

// Unwrap lets http.ResponseController reach the connection's deadlines.
func (w *writer) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// route sends a request to what serves it. GET serves only the bootstrap
// and the probes (§9.6.2); every other request is an interaction call,
// and anything but a POST is not_found wherever it is sent (PR08).
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	get := r.Method == http.MethodGet || r.Method == http.MethodHead
	switch {
	case get && r.URL.Path == protocol.WellKnownPath:
		s.bootstrap(w, r)
	case get && (r.URL.Path == "/livez" || r.URL.Path == "/healthz"):
		s.live.ServeHTTP(w, r)
	case get && r.URL.Path == "/readyz":
		s.ready.ServeHTTP(w, r)
	default:
		s.call(w, r)
	}
}

// isProbe reports whether a request is a liveness or readiness probe:
// exempt from the access log, the drain, the body limit and the limits
// (§6.7.2), so a probe always answers.
func isProbe(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	switch r.URL.Path {
	case "/livez", "/healthz", "/readyz":
		return true
	}
	return false
}

// exchange is one request's state, which the edge's layers share: its id,
// its trace, how it was answered, and whether the caller stayed to hear it.
type exchange struct {
	w     *writer
	start time.Time
	probe bool

	id      string
	trace   string
	address string    // the address the call was bound to, once known
	outer   time.Time // the call's outer deadline (§6.7.2 row 9)
	code    string    // the problem the answer carried, its end line's included
	// failed is the problem the edge ended the answer with in place of the
	// pipeline's answer: the pipeline records it as the call's outcome.
	failed *protocol.Problem
	gone   bool // the caller went away before its answer was written (499)
	// read is a body read to its end; unread a body read that failed,
	// after which net/http ends the request's context, which says nothing
	// of whether the caller is still there.
	read, unread bool
}

type exchangeKey struct{}

// exchangeOf is the request's exchange. A request that did not come
// through ServeHTTP gets a fresh one, so a handler never meets a nil.
func exchangeOf(r *http.Request) *exchange {
	if x, ok := r.Context().Value(exchangeKey{}).(*exchange); ok {
		return x
	}
	x := &exchange{}
	x.w = &writer{ResponseWriter: middleware.Wrap(nil), x: x, r: r}
	return x
}

// status is what the access log records: 499 for a caller that went away
// (§6.7.3), else the status sent.
func (x *exchange) status() int {
	if x.gone {
		return problem.StatusClientClosedRequest
	}
	if s := x.w.Status(); s != 0 {
		return s
	}
	return http.StatusOK
}

// newID is a request id: a random UUID (§9.6.3 X-Request-Id). The edge
// always mints its own and never adopts one a caller sends, so no caller
// can choose the id another call's audit facts are joined by.
func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails (Go 1.24)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// nexts are the next steps of the protocol's own codes, for a problem
// raised without one: every problem tells the caller what to do.
var nexts = map[string]string{
	protocol.CodeInvalidDocument:     "Send the body as I-JSON: one object, no duplicate members.",
	protocol.CodeInvalidRequest:      "Correct the request at target; atlas.describe@v1 gives its schema.",
	protocol.CodeUnauthenticated:     "Sign in, then repeat the call.",
	protocol.CodeNotFound:            "Call atlas.catalog@v1:fetch for what you may use.",
	protocol.CodeNotInEnvironment:    "Call atlas.describe@v1 for how this environment serves it.",
	protocol.CodeInternal:            "Repeat the call; if it fails again, report its request_id.",
	protocol.CodeUpstreamFailed:      "Wait retry_after seconds, then repeat the call.",
	protocol.CodeUnavailable:         "Wait retry_after seconds, then repeat the call.",
	protocol.CodeDeadlineExceeded:    "Repeat the call with a later deadline, or narrow it.",
	protocol.CodeLimitExceeded:       "Wait retry_after seconds, then repeat the call.",
	protocol.CodeUpgradeRequired:     "Upgrade the client.",
	protocol.CodeOutputLimitExceeded: "Narrow the request, or ask for a higher output limit.",
}

// complete gives a problem everything problem.schema.json requires.
func complete(p *protocol.Problem, id string) *protocol.Problem {
	out := *p
	out.RequestID = id
	if out.Next == "" {
		out.Next = nexts[out.Code]
		if out.Next == "" {
			out.Next = "See the problem's detail."
		}
	}
	return &out
}

// problem answers a failure: one problem document through kit/problem,
// never cached (D3). A cause that is not a problem is logged before it
// becomes internal, so no failure is swallowed and none leaks. A caller
// that went away is answered nothing (§6.7.3).
func (s *Server) problem(w http.ResponseWriter, r *http.Request, err error) {
	x := exchangeOf(r)
	var known *protocol.Problem
	if !errors.As(err, &known) {
		s.opt.Log.Error("a failure became internal", "request_id", x.id, "err", err)
	}
	p := complete(problem.From(err), x.id)
	if x.w.Written() {
		s.opt.Log.Error("a failure after the answer began", "request_id", x.id, "code", p.Code, "detail", p.Detail)
		return
	}
	x.code = p.Code
	if x.gone || !x.unread && errors.Is(r.Context().Err(), context.Canceled) {
		x.gone = true
		return
	}
	h := w.Header()
	if p.Code == protocol.CodeUnauthenticated {
		c := protocol.Challenge{Realm: "atlas"}
		if pipeline.Expired(p) {
			c.Error = "invalid_token"
		}
		h.Set(protocol.HeaderWWWAuthenticate, c.Format())
	}
	h.Set(protocol.HeaderCacheControl, protocol.NoStore)
	problem.Write(w, p.RequestID, p)
}

// real is t on the clock a connection's deadlines are kept by: the time
// left until t, measured by the injected clock, from the runtime's now.
// Connection deadlines are the runtime's timers (kit/clock), and under
// testing/synctest both clocks are the bubble's.
func (s *Server) real(t time.Time) time.Time { return time.Now().Add(t.Sub(s.opt.Clock.Now())) }

// ceiling caches the largest interaction maximum of one snapshot: the cap
// of every call's outer deadline (§6.7.2 row 9).
type ceiling struct {
	snap *registry.Snapshot
	max  time.Duration
}

func (s *Server) ceiling() time.Duration {
	snap := s.opt.Pipeline.Registry().Snapshot()
	if c := s.ceil.Load(); c != nil && c.snap == snap {
		return c.max
	}
	var m time.Duration
	for _, c := range snap.Capabilities() {
		for _, in := range c.Current.Interactions {
			m = max(m, in.Deadline.Max, in.Deadline.Default)
		}
	}
	if m <= 0 {
		m = fallbackCeiling
	}
	s.ceil.Store(&ceiling{snap: snap, max: m})
	return m
}

// address is what the access log and the metrics name a request by: the
// address it was bound to, a fixed name for the gateway's own routes, or
// "unmatched", never a raw path a caller chose (MECH-014, MECH-015).
func (s *Server) address(r *http.Request, x *exchange) string {
	if x.address != "" {
		return x.address
	}
	switch r.URL.Path {
	case protocol.WellKnownPath:
		return "bootstrap"
	case "/livez", "/healthz", "/readyz":
		return strings.TrimPrefix(r.URL.Path, "/")
	}
	if rt, ok := s.opt.Pipeline.Registry().Snapshot().Route(r.URL.Path); ok {
		return rt.Capability.Pinned.String() + ":" + rt.Interaction
	}
	return "unmatched"
}

// parseOrigins reads the origins a browser may call from: each
// scheme://host[:port], https or loopback http, never a wildcard (§6.2.9).
func parseOrigins(list []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, o := range list {
		u, err := url.Parse(o)
		bad := err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.Contains(o, "*")
		if !bad {
			switch u.Scheme {
			case "https":
			case "http":
				bad = !loopback(u.Hostname())
			default:
				bad = true
			}
		}
		if bad {
			return nil, fmt.Errorf("edge: %q is not a web origin a browser may call from: scheme://host[:port], https or loopback http, never a wildcard (§6.2.9)", o)
		}
		out[strings.ToLower(o)] = true
	}
	return out, nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && a.IsLoopback()
}
