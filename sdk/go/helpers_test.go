package atlas

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/engine/fakegw"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// isolate keeps the machine's own Atlas settings out of a test.
func isolate(t *testing.T) {
	t.Helper()
	for _, k := range []string{"ATLAS_URL", "ATLAS_ENV", "ATLAS_TOKEN", "ATLAS_HOME", "ATLAS_CONFORMANCE"} {
		t.Setenv(k, "")
	}
}

// signedIn is a home whose unbound conformance sign-in the fixture's
// gateway accepts (CORE-SIGNIN-001's setup).
func signedIn(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := SignIn(home, "conformance", "ct-valid", "rt-valid", time.Hour); err != nil {
		t.Fatal(err)
	}
	return home
}

func connect(t *testing.T, gw *fakegw.Gateway, home string, edit ...func(*Options)) *Client {
	t.Helper()
	opt := Options{URL: gw.URL, Home: home, HTTP: gw.Client()}
	for _, e := range edit {
		e(&opt)
	}
	c, err := Connect(context.Background(), opt)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return c
}

func wantProblem(t *testing.T, err error, code string) *protocol.Problem {
	t.Helper()
	var p *protocol.Problem
	if !errors.As(err, &p) || p.Code != code {
		t.Fatalf("got %v, want the problem %s", err, code)
	}
	return p
}

// carried reports whether any request bore the token in its
// Authorization, or the refresh token in its body.
func carried(reqs []fakegw.Request, secret string) bool {
	for _, r := range reqs {
		if strings.Contains(r.Header.Get("Authorization"), secret) || strings.Contains(string(r.Body), secret) {
			return true
		}
	}
	return false
}
