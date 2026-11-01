package atlas

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/conc"
)

// stalled is a token endpoint that answers nothing until its request's
// context ends.
type stalled struct{}

func (stalled) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

// R3-06, HANDOFF.md C2 ("waiting on shared work is cancellable alone"): a
// call waiting behind another call's refresh waits no longer than its own
// deadline, however long the refresh it waits behind may run.
func TestR3_06AWaiterBehindARefreshKeepsItsOwnDeadline(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		home := t.TempDir()
		if err := SignIn(home, "conformance", "ct-old", "rt-1", 30*time.Second); err != nil {
			t.Fatal(err)
		}
		origin := "http://127.0.0.1:8740"
		ts := &tokenStore{dir: filepath.Join(home, "state"), env: "conformance", origin: origin, clock: clock.System,
			signIn: signIn{ClientID: "atlas-conformance", TokenEndpoint: origin + "/oauth/token"}, http: &http.Client{Transport: stalled{}}}
		long := conc.Go(func() error { _, err := ts.token(context.Background(), ""); return err })
		synctest.Wait() // the first call holds the refresh
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		start := time.Now()
		_, err := ts.token(ctx, "")
		if waited := time.Since(start); waited > time.Second {
			t.Fatalf("a call with a 1s deadline waited %v behind another's refresh", waited)
		}
		wantProblem(t, err, "deadline_exceeded")
		_ = long() // the refresh ends at its own bound
	})
}
