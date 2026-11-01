package atlas

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"atlas.local/sdk/go/engine/fakegw"
)

// R091, CORE-CALL-002, Q-DOC-9: a request that fails its schema is
// refused before it is sent, and each of the problem's details carries the
// failure's pointer and keyword.
func TestR091InvalidRequestDetailsCarryTheirKeyword(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	c := connect(t, gw, signedIn(t))
	_, err := c.Call(context.Background(), "conformance.echo@v1:fetch", map[string]any{"text": "", "count": 101, "extra": 1})
	p := wantProblem(t, err, "invalid_request")
	var got [][2]string
	for _, d := range p.Details {
		got = append(got, [2]string{d.Target, d.Keyword})
	}
	for _, want := range [][2]string{{"/count", "maximum"}, {"/extra", "additionalProperties"}, {"/text", "minLength"}} {
		if !slices.Contains(got, want) {
			t.Fatalf("details %v lack %v", got, want)
		}
	}
	if n := len(gw.To("conformance.echo@v1:fetch")); n != 0 {
		t.Fatalf("%d requests were sent", n)
	}
}

// R092, CORE-SIGNIN-004: a claims challenge is unauthenticated at once: a
// refreshed token cannot satisfy it, so nothing is refreshed and the call
// is not repeated.
func TestR092AClaimsChallengeIsNotRefreshed(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	challenge := `Bearer realm="atlas", claims="eyJhY2Nlc3NfdG9rZW4iOnsiYWNycyI6eyJ2YWx1ZXMiOlsiYzEiXX19fQ"`
	gw.Handle("atlas.whoami:fetch", func(c *fakegw.Call) {
		c.W.Header().Set("WWW-Authenticate", challenge)
		c.Problem(http.StatusUnauthorized, "unauthenticated", "step up", nil)
	})
	c := connect(t, gw, signedIn(t))
	_, err := c.Whoami(context.Background())
	wantProblem(t, err, "unauthenticated")
	if w, r := len(gw.To("atlas.whoami:fetch")), len(gw.To("/oauth/token")); w != 1 || r != 0 {
		t.Fatalf("%d calls and %d refreshes, want 1 and 0", w, r)
	}
}
