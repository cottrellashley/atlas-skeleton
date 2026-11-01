package atlas

import (
	"context"
	"net/http"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/engine/fakegw"
)

// R029, R087, §9.7.7, CORE-PROB-001: retries follow the problem's
// retryable, never the HTTP status: a retryable 502 or 409 is repeated
// three times, a capability's own 503 that is not retryable is not.
func TestR029RetriesFollowTheProblemsRetryable(t *testing.T) {
	isolate(t)
	for _, tc := range []struct {
		name     string
		status   int
		code     string
		extra    map[string]any
		requests int
	}{
		{"upstream_failed, retryable", http.StatusBadGateway, "upstream_failed", map[string]any{"retryable": true, "retry_after": 0}, 4},
		{"in_progress, retryable", http.StatusConflict, "in_progress", map[string]any{"retryable": true, "retry_after": 0}, 4},
		{"a capability's 503, not retryable", http.StatusServiceUnavailable, "warehouse_suspended", map[string]any{"retryable": false}, 1},
		{"denied", http.StatusForbidden, "denied", nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				gw := fakegw.New(t, fakegw.Options{})
				gw.Handle("conformance.echo@v1:fetch", func(c *fakegw.Call) { c.Problem(tc.status, tc.code, "scripted", tc.extra) })
				c := connect(t, gw, signedIn(t))
				_, err := c.Call(context.Background(), "conformance.echo@v1:fetch", map[string]any{"text": "x"})
				wantProblem(t, err, tc.code)
				if n := len(gw.To("conformance.echo@v1:fetch")); n != tc.requests {
					t.Fatalf("%d requests, want %d", n, tc.requests)
				}
			})
		})
	}
}

// R087, R133, §9.7.7: without a retry_after, retry n waits min(30, 0.5 ×
// 2^n) seconds plus a jitter of up to 25%, on the timers a synctest bubble
// controls, so the waits are exact; every retry of a write keeps its
// Idempotency-Key and its Atlas-Deadline (CORE-CALL-004).
func TestR087BackoffWithJitterKeepsTheKey(t *testing.T) {
	isolate(t)
	for _, tc := range []struct {
		jitter float64
		waits  []time.Duration
	}{
		{0, []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}},
		{0.999, []time.Duration{624875 * time.Microsecond, 1249750 * time.Microsecond, 2499500 * time.Microsecond}},
	} {
		synctest.Test(t, func(t *testing.T) {
			gw := fakegw.New(t, fakegw.Options{})
			gw.Handle("conformance.thing@v1:create", func(c *fakegw.Call) {
				if c.N <= 3 {
					c.Problem(http.StatusBadGateway, "upstream_failed", "transient", map[string]any{"retryable": true})
					return
				}
				thing := c.Body
				thing.Set("metadata", mustObject(t, `{"uid": "u1", "etag": "\"1\"", "generation": 1, "created": "2026-01-01T00:00:00.000000Z", "updated": "2026-01-01T00:00:00.000000Z", "owner": "00000000-0000-4000-8000-000000000001", "labels": {}, "deleting": null}`))
				c.JSON(thing)
			})
			c := connect(t, gw, signedIn(t))
			c.jitter = func() float64 { return tc.jitter }
			if _, err := c.Call(context.Background(), "conformance.thing@v1:create", map[string]any{"name": "conformance/things/t1", "colour": "red", "size": 3}); err != nil {
				t.Fatal(err)
			}
			reqs := gw.To("conformance.thing@v1:create")
			if len(reqs) != 4 {
				t.Fatalf("%d requests, want 4", len(reqs))
			}
			for i := 1; i < 4; i++ {
				if got := reqs[i].At.Sub(reqs[i-1].At); got != tc.waits[i-1] {
					t.Fatalf("jitter %v: retry %d waited %v, want %v", tc.jitter, i, got, tc.waits[i-1])
				}
				for _, h := range []string{"Idempotency-Key", "Atlas-Deadline"} {
					if reqs[i].Header.Get(h) != reqs[0].Header.Get(h) || reqs[0].Header.Get(h) == "" {
						t.Fatalf("retry %d changed %s: %q, then %q", i, h, reqs[0].Header.Get(h), reqs[i].Header.Get(h))
					}
				}
			}
		})
	}
}

// R133, CORE-RETRY-001: Retry-After is waited for exactly; a retry that
// would end past the deadline is not made, and the problem held is the
// answer; a problem that is not retryable is not retried.
func TestR133RetryAfterAndTheDeadline(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		gw.Handle("conformance.echo@v1:fetch", func(c *fakegw.Call) {
			text, _ := c.Body.Get("text")
			switch {
			case text == "x" && c.N == 1:
				c.Problem(http.StatusTooManyRequests, "limit_exceeded", "slow down", map[string]any{"retryable": true, "retry_after": 1})
			case text == "x" && c.N == 2:
				c.Problem(http.StatusServiceUnavailable, "unavailable", "warming", map[string]any{"retryable": true, "retry_after": 2})
			case text == "y":
				c.Problem(http.StatusServiceUnavailable, "unavailable", "down", map[string]any{"retryable": true, "retry_after": 0})
			case text == "z":
				c.Problem(http.StatusServiceUnavailable, "unavailable", "down", map[string]any{"retryable": true, "retry_after": 2})
			case text == "w":
				c.Problem(http.StatusForbidden, "denied", "no", nil)
			default:
				c.JSON(fakegw.Echo(c.Body))
			}
		})
		c := connect(t, gw, signedIn(t))
		ctx := context.Background()
		count := func(text string) []fakegw.Request {
			var out []fakegw.Request
			for _, r := range gw.To("conformance.echo@v1:fetch") {
				if string(r.Body) == `{"text":"`+text+`"}` {
					out = append(out, r)
				}
			}
			return out
		}
		if _, err := c.Call(ctx, "conformance.echo@v1:fetch", map[string]any{"text": "x"}); err != nil {
			t.Fatal(err)
		}
		x := count("x")
		if len(x) != 3 || x[1].At.Sub(x[0].At) != time.Second || x[2].At.Sub(x[1].At) != 2*time.Second {
			t.Fatalf("x: %d requests, at %v", len(x), x)
		}
		_, err := c.Call(ctx, "conformance.echo@v1:fetch", map[string]any{"text": "y"})
		wantProblem(t, err, "unavailable")
		if n := len(count("y")); n != 4 {
			t.Fatalf("y: %d requests, want 4", n)
		}
		_, err = c.Call(ctx, "conformance.echo@v1:fetch", map[string]any{"text": "z"}, WithDeadline(3*time.Second))
		wantProblem(t, err, "unavailable")
		if z := count("z"); len(z) != 2 || z[0].Header.Get("Atlas-Deadline") != z[1].Header.Get("Atlas-Deadline") {
			t.Fatalf("z: %d requests, want 2 with one Atlas-Deadline", len(z))
		}
		_, err = c.Call(ctx, "conformance.echo@v1:fetch", map[string]any{"text": "w"})
		wantProblem(t, err, "denied")
		if n := len(count("w")); n != 1 {
			t.Fatalf("w: %d requests, want 1", n)
		}
	})
}

// §9.6.6, CORE-SIGNIN-003: a 401 invalid_token refreshes the token and
// repeats the call once; a second 401 is unauthenticated.
func TestR029InvalidTokenRefreshesAndRepeatsOnce(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	gw.Tokens([]string{"ct-valid", "ct-refreshed"}, map[string]fakegw.Grant{"rt-valid": {AccessToken: "ct-refreshed", RefreshToken: "rt-2", ExpiresIn: 3600}, "rt-2": {AccessToken: "ct-refreshed", RefreshToken: "rt-3", ExpiresIn: 3600}})
	gw.Handle("atlas.whoami:fetch", func(c *fakegw.Call) {
		if c.N == 1 || c.N >= 3 {
			c.W.Header().Set("WWW-Authenticate", `Bearer realm="atlas", error="invalid_token"`)
			c.Problem(http.StatusUnauthorized, "unauthenticated", "expired", nil)
			return
		}
		c.Serve()
	})
	c := connect(t, gw, signedIn(t))
	ctx := context.Background()
	if _, err := c.Whoami(ctx); err != nil {
		t.Fatal(err)
	}
	_, err := c.Whoami(ctx)
	wantProblem(t, err, "unauthenticated")
	who := gw.To("atlas.whoami:fetch")
	if len(who) != 4 || len(gw.To("/oauth/token")) != 2 || who[1].Header.Get("Authorization") != "Bearer ct-refreshed" {
		t.Fatalf("%d whoami and %d refreshes, want 4 and 2", len(who), len(gw.To("/oauth/token")))
	}
}

func mustObject(t *testing.T, s string) *ijson.Object {
	t.Helper()
	v, err := ijson.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return v.(*ijson.Object)
}
