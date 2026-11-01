package edge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/clock"
	"atlas.local/sdk/go/kit/conc"
	"atlas.local/sdk/go/kit/health"
	"atlas.local/sdk/go/kit/lifecycle"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/ijson"
)

// ATLAS-MECH-001, R128: the edge's middleware is declared in one list, in
// §6.7.2's order (CORS, §6.2.9, after the drain).
func TestMECH001MiddlewareOrder(t *testing.T) {
	r := newRig(t, rigOptions{noServer: true})
	var names []string
	for _, l := range r.edge.layers {
		names = append(names, l.name)
	}
	want := []string{"recover", "request-id", "client-address", "trace-context", "access-log", "metrics", "draining", "cors", "body-size", "outer-deadline", "pre-auth-limit", "in-flight"}
	if !slices.Equal(names, want) {
		t.Fatalf("the middleware %v, §6.7.2 %v", names, want)
	}
}

func get(t testing.TB, r *rig, path string) answer {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, r.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r.do(req)
}

// R059, R065, ATLAS-MECH-015, ATLAS-MECH-016, §6.7.15: /livez and /healthz
// are liveness, /readyz names every check and is ready only while the
// process is and every check passes; while the process is not ready, a
// call is unavailable with Retry-After 2 and Connection: close, and the
// probes still answer. No probe is in the access log.
func TestR065ProbesAndDraining(t *testing.T) {
	ready := &lifecycle.Readiness{}
	ready.Set(true)
	var auditDown error
	r := newRig(t, rigOptions{serving: Serving{Ready: ready, Checks: []health.Check{health.NamedCheck("audit", func(context.Context) error { return auditDown })}}})
	for _, path := range []string{"/livez", "/healthz"} {
		if a := get(t, r, path); a.status != 200 || string(a.body) != "ok" || a.header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %q", path, a.status, a.body)
		}
	}
	a := get(t, r, "/readyz")
	for _, check := range []string{"[+]ready ok", "[+]snapshot ok", "[+]audit ok"} {
		if a.status != 200 || !strings.Contains(string(a.body), check) {
			t.Fatalf("/readyz names %s: %d %q", check, a.status, a.body)
		}
	}
	auditDown = io.ErrClosedPipe
	if a := get(t, r, "/readyz?exclude=audit"); a.status != 503 || !strings.Contains(string(a.body), "[-]audit failed: reason withheld") || strings.Contains(string(a.body), "closed pipe") {
		t.Fatalf("a failed check, which no query excludes: %d %q", a.status, a.body)
	}
	auditDown = nil

	ready.Set(false)
	call := r.call("t-alice", listAt, `{}`)
	if p := call.problem(t); p.Code != protocol.CodeUnavailable || call.header.Get("Retry-After") != "2" || !call.close {
		t.Fatalf("draining: %d %v %s", call.status, call.header, call.body)
	}
	if a := get(t, r, "/readyz"); a.status != 503 || !strings.Contains(string(a.body), "[-]ready failed") {
		t.Fatalf("/readyz while draining: %d %q", a.status, a.body)
	}
	if a := get(t, r, "/livez"); a.status != 200 {
		t.Fatalf("/livez while draining: %d", a.status)
	}
	ready.Set(true)
	if a := r.call("t-alice", listAt, `{}`); a.status != 200 {
		t.Fatalf("ready again: %d %s", a.status, a.body)
	}
	for _, line := range r.log.lines("call") {
		if a := line["address"].(string); a == "livez" || a == "healthz" || a == "readyz" {
			t.Fatalf("a probe in the access log: %v", line)
		}
	}
}

// ATLAS-MECH-016: once readiness turns false, a call already running
// finishes, and a new one is unavailable with Retry-After 2.
func TestMECH016DrainLetsRunningCallsFinish(t *testing.T) {
	ready := &lifecycle.Readiness{}
	ready.Set(true)
	r := newRig(t, rigOptions{serving: Serving{Ready: ready}})
	started, release := make(chan struct{}), make(chan struct{})
	r.provide(func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		r.provide(r.standard)
		close(started)
		<-release
		return r.standard(ctx, inv)
	})
	first := make(chan answer, 1)
	wait := conc.Go(func() error {
		first <- r.call("t-alice", listAt, `{}`)
		return nil
	})
	select {
	case <-started:
	case a := <-first:
		t.Fatalf("the call ended before its work started: %d %s", a.status, a.body)
	}
	ready.Set(false)
	if a := r.call("t-alice", listAt, `{}`); a.status != 503 || a.header.Get("Retry-After") != "2" {
		t.Fatalf("a new call while draining: %d %v", a.status, a.header)
	}
	close(release)
	if err := wait(); err != nil {
		t.Fatal(err)
	}
	if a := <-first; a.status != 200 {
		t.Fatalf("the running call: %d %s", a.status, a.body)
	}
}

// R048, §6.7.2 row 11: one replica serves at most MaxInFlight requests at
// once; past it, unavailable with Retry-After, and the probes still answer.
func TestR048InFlightLimit(t *testing.T) {
	r := newRig(t, rigOptions{serving: Serving{MaxInFlight: 1}})
	started, release := make(chan struct{}), make(chan struct{})
	r.provide(func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		r.provide(r.standard)
		close(started)
		<-release
		return r.standard(ctx, inv)
	})
	first := make(chan answer, 1)
	wait := conc.Go(func() error {
		first <- r.call("t-alice", listAt, `{}`)
		return nil
	})
	select {
	case <-started:
	case a := <-first:
		t.Fatalf("the call ended before its work started: %d %s", a.status, a.body)
	}
	a := r.call("t-bob", listAt, `{}`)
	if p := a.problem(t); p.Code != protocol.CodeUnavailable || a.header.Get("Retry-After") != "1" {
		t.Fatalf("past the in-flight limit: %d %v %s", a.status, a.header, a.body)
	}
	if a := get(t, r, "/livez"); a.status != 200 {
		t.Fatalf("a probe past the limit: %d", a.status)
	}
	close(release)
	if err := wait(); err != nil {
		t.Fatal(err)
	}
	if a := <-first; a.status != 200 {
		t.Fatalf("the call in flight: %d", a.status)
	}
	if a := r.call("t-bob", listAt, `{}`); a.status != 200 {
		t.Fatalf("its place is free again: %d %s", a.status, a.body)
	}
}

// R048, §6.7.2 row 10: an address may have only so many calls refused as
// unauthenticated; past them it is refused limit_exceeded before any
// token is read, until its allowance refills. A call that signs in costs
// its address nothing, however many it has open at once (an office behind
// one address).
func TestR048PreAuthLimit(t *testing.T) {
	c := clock.NewManual(time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))
	r := newRig(t, rigOptions{clock: c, serving: Serving{PreAuthBurst: 2, PreAuthRate: 1}})
	release := make(chan struct{})
	var running sync.WaitGroup
	running.Add(3)
	r.provide(func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		running.Done()
		<-release
		return r.standard(ctx, inv)
	})
	var open conc.Group
	for range 3 {
		open.Go(func() error {
			if a := r.call("t-alice", listAt, `{}`); a.status != 200 {
				return fmt.Errorf("an open call from the address: %d %s", a.status, a.body)
			}
			return nil
		})
	}
	running.Wait() // three calls open at once, over the burst of two
	close(release)
	if err := open.Wait(); err != nil {
		t.Fatal(err)
	}
	r.provide(r.standard)
	if n := r.edge.unauth.size(); n != 0 {
		t.Fatalf("signed-in calls left %d buckets", n)
	}
	for i := range 2 {
		if p := r.call("forged", listAt, `{}`).problem(t); p.Code != protocol.CodeUnauthenticated {
			t.Fatalf("refusal %d: %s", i+1, p.Code)
		}
	}
	verified := r.auth.calls.Load()
	a := r.call("forged", listAt, `{}`)
	if p := a.problem(t); p.Code != protocol.CodeLimitExceeded || a.header.Get("Retry-After") != "1" || r.auth.calls.Load() != verified {
		t.Fatalf("past the allowance: %d %v %s; %d tokens verified", a.status, a.header, a.body, r.auth.calls.Load()-verified)
	}
	if a := r.call("t-alice", listAt, `{}`); a.status != 429 {
		t.Fatalf("the address is refused whole until it refills: %d", a.status)
	}
	if a := get(t, r, protocol.WellKnownPath); a.status != 200 {
		t.Fatalf("the bootstrap needs no token: %d", a.status)
	}
	c.Advance(time.Second)
	if a := r.call("t-alice", listAt, `{}`); a.status != 200 {
		t.Fatalf("after a second: %d %s", a.status, a.body)
	}
}

// R048: the pre-auth limit's memory is bounded by the addresses it may
// track, however many a flood comes from.
func TestR048PreAuthMemoryIsBounded(t *testing.T) {
	b := newBuckets(1, 1, 4)
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for i := range 100 {
		b.charge(clientKey("10.0.0."+strconv.Itoa(i)), at)
	}
	if n := b.size(); n > 4 {
		t.Fatalf("%d addresses tracked, bound 4", n)
	}
	if _, ok := b.allow(clientKey("10.0.1.1"), at); ok {
		t.Fatal("past the bound, new addresses share one spent bucket")
	}
	if _, ok := b.allow(clientKey("10.0.0.1"), at.Add(time.Second)); !ok {
		t.Fatal("an address refills")
	}
	// Once its own have refilled, a later sweep makes room for others.
	b.charge(clientKey("10.0.2.1"), at.Add(time.Hour))
	if _, ok := b.allow(clientKey("10.0.3.1"), at.Add(time.Hour)); !ok || b.size() != 1 {
		t.Fatalf("after a sweep: %d tracked", b.size())
	}
	if clientKey("2001:db8::1") != clientKey("2001:db8::2") || clientKey("2001:db8::1") == clientKey("2001:db8:0:1::1") {
		t.Fatal("an IPv6 subscriber is its /64")
	}
}

// R135, §6.2.9: only the named origins may call from a browser; never a
// wildcard; the token is read from Authorization alone, never a cookie.
func TestR135CORSNamedOriginsOnly(t *testing.T) {
	if _, err := Open(Options{Pipeline: newRig(t, rigOptions{noServer: true}).pipe, Serving: Serving{Origins: []string{"*"}}}); err == nil {
		t.Fatal("a wildcard origin was accepted")
	}
	for _, bad := range []string{"https://nautilus.example.com/app", "http://nautilus.example.com", "ftp://x", "nautilus.example.com", "https://*.example.com"} {
		if _, err := Open(Options{Pipeline: newRig(t, rigOptions{noServer: true}).pipe, Serving: Serving{Origins: []string{bad}}}); err == nil {
			t.Fatalf("%q was accepted as an origin", bad)
		}
	}
	const named = "https://nautilus.example.com"
	r := newRig(t, rigOptions{serving: Serving{Origins: []string{named, "http://localhost:5173"}}})
	preflight := func(origin string) answer {
		req, _ := http.NewRequest(http.MethodOptions, r.srv.URL+listAt, nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "authorization, atlas-client")
		return r.do(req)
	}
	ok := preflight(named)
	if ok.status != 204 || ok.header.Get("Access-Control-Allow-Origin") != named || !strings.Contains(ok.header.Get("Access-Control-Allow-Headers"), "Authorization") || ok.header.Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("a named origin's preflight: %d %v", ok.status, ok.header)
	}
	refused := preflight("https://evil.example.com")
	if p := refused.problem(t); p.Code != protocol.CodeNotFound || refused.header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("another origin's preflight: %d %v", refused.status, refused.header)
	}
	call := r.call("t-alice", listAt, `{}`, "Origin", named)
	if call.status != 200 || call.header.Get("Access-Control-Allow-Origin") != named || !strings.Contains(call.header.Get("Access-Control-Expose-Headers"), protocol.HeaderRequestID) {
		t.Fatalf("a named origin's call: %d %v", call.status, call.header)
	}
	other := r.call("t-alice", listAt, `{}`, "Origin", "https://evil.example.com")
	if v := other.header.Get("Access-Control-Allow-Origin"); v != "" {
		t.Fatalf("another origin was allowed: %q", v)
	}
	cookie := r.call("", listAt, `{}`, "Cookie", "token=t-alice; access_token=t-alice", "Origin", named)
	if p := cookie.problem(t); p.Code != protocol.CodeUnauthenticated {
		t.Fatalf("a token in a cookie was taken: %s", p.Code)
	}
	for _, a := range []answer{ok, refused, call, other, cookie} {
		if a.header.Get("Access-Control-Allow-Origin") == "*" {
			t.Fatal("a wildcard was sent")
		}
	}
}

// D3 (gaps/2.md §3), R01, §9.6.7: the bootstrap is no-cache with a strong
// ETag and a 304 on If-None-Match; every other answer is no-store and
// carries no ETag, and a POST is never answered 304.
func TestD3Caching(t *testing.T) {
	r := newRig(t, rigOptions{})
	boot := get(t, r, protocol.WellKnownPath)
	etag := boot.header.Get("ETag")
	if boot.status != 200 || boot.header.Get("Cache-Control") != "no-cache" || !strings.HasPrefix(etag, `"`) || strings.HasPrefix(etag, "W/") {
		t.Fatalf("the bootstrap: %d %v", boot.status, boot.header)
	}
	req, _ := http.NewRequest(http.MethodGet, r.srv.URL+protocol.WellKnownPath, nil)
	req.Header.Set("If-None-Match", etag)
	again := r.do(req)
	if again.status != 304 || len(again.body) != 0 || again.header.Get("ETag") != etag || again.header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("revalidated: %d %v %q", again.status, again.header, again.body)
	}
	for _, a := range []answer{
		r.call("t-alice", listAt, `{}`, "If-None-Match", "*"),
		r.call("t-alice", listAt, `{}`, "If-None-Match", etag),
		r.call("t-alice", holdingsAt, `{"portfolios": ["147HYC"]}`, "If-None-Match", "*", "Accept", protocol.MediaNDJSON),
		r.call("", listAt, `{}`),
		get(t, r, "/livez"),
		get(t, r, "/readyz"),
	} {
		if a.status == 304 || a.header.Get("Cache-Control") != "no-store" || a.header.Get("ETag") != "" {
			t.Fatalf("%d %v", a.status, a.header)
		}
	}
	v, err := ijson.Parse(boot.body)
	if err != nil {
		t.Fatal(err)
	}
	if env, _ := v.(*ijson.Object).Get("environment"); env != "local" {
		t.Fatalf("the bootstrap: %s", boot.body)
	}
}
