package atlas

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/engine/fakegw"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// PR06, R028, CORE-DEADLINE-001: a gateway that accepts a call and holds
// it ends the call at its deadline, deadline_exceeded, before any header
// arrives.
func TestPR06TheDeadlineBoundsAStalledAnswer(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		gw.Handle("conformance.echo@v1:fetch", func(c *fakegw.Call) { c.Hold(release) })
		c := connect(t, gw, signedIn(t))
		start := time.Now()
		_, err := c.Call(context.Background(), "conformance.echo@v1:fetch", map[string]any{"text": "slow"}, WithDeadline(300*time.Millisecond))
		wantProblem(t, err, "deadline_exceeded")
		if got := time.Since(start); got != 300*time.Millisecond {
			t.Fatalf("the call ended after %v, want its deadline, 300ms", got)
		}
	})
}

// PR06, R061: the deadline bounds the answer to the end of its body. Rows
// that stop arriving end the read at the deadline, deadline_exceeded, not
// a success and not a wait without end.
func TestPR06TheDeadlineBoundsAStalledBody(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		gw.Handle("conformance.rows@v1:fetch", func(c *fakegw.Call) {
			c.W.Header().Set("Content-Type", protocol.MediaNDJSON)
			c.W.WriteHeader(http.StatusOK)
			for i := int64(0); i < 10; i++ {
				b, _ := ijson.Marshal(fakegw.Row(i))
				_, _ = c.W.Write(append(b, '\n'))
			}
			c.W.(http.Flusher).Flush()
			c.Hold(release)
		})
		c := connect(t, gw, signedIn(t))
		start := time.Now()
		r, err := c.Call(context.Background(), "conformance.rows@v1:fetch", map[string]any{"n": 1000}, WithDeadline(2*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		rows := 0
		for {
			_, err = r.Next()
			if err != nil {
				break
			}
			rows++
		}
		wantProblem(t, err, "deadline_exceeded")
		if got := time.Since(start); rows != 10 || got != 2*time.Second {
			t.Fatalf("read %d rows, ended after %v; want 10 rows and the 2s deadline", rows, got)
		}
		if r.Complete() {
			t.Fatal("a cut table reports itself complete")
		}
	})
}

// R061, CORE-DEADLINE-001: with no deadline given, the interaction's
// default (conformance.echo's PT30S) is sent as Atlas-Deadline, and it
// bounds the call; a deadline past the maximum (PT2M) is the maximum.
func TestR061TheDefaultDeadlineIsSentAndBoundsTheCall(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		gw.Handle("conformance.echo@v1:fetch", func(c *fakegw.Call) {
			if text, _ := c.Body.Get("text"); text == "slow" {
				c.Hold(release)
				return
			}
			c.JSON(fakegw.Echo(c.Body))
		})
		c := connect(t, gw, signedIn(t))
		start := time.Now()
		for _, tc := range []struct {
			text string
			opts []CallOption
			want time.Duration
		}{{"a", nil, 30 * time.Second}, {"b", []CallOption{WithDeadline(10 * time.Minute)}, 2 * time.Minute}} {
			if _, err := c.Call(context.Background(), "conformance.echo@v1:fetch", map[string]any{"text": tc.text}, tc.opts...); err != nil {
				t.Fatal(err)
			}
			reqs := gw.To("conformance.echo@v1:fetch")
			at, err := protocol.ParseDeadline(reqs[len(reqs)-1].Header.Get("Atlas-Deadline"))
			if err != nil || at.Sub(start) != tc.want {
				t.Fatalf("%s: Atlas-Deadline %v after the call began, want %v (%v)", tc.text, at.Sub(start), tc.want, err)
			}
		}
		start = time.Now()
		_, err := c.Call(context.Background(), "conformance.echo@v1:fetch", map[string]any{"text": "slow"})
		wantProblem(t, err, "deadline_exceeded")
		if got := time.Since(start); got != 30*time.Second {
			t.Fatalf("a held call ended after %v, want the default 30s", got)
		}
	})
}

// R061: what has no call deadline of its own is bounded too: the
// bootstrap, and a token refresh within its call's deadline.
func TestR061TheBootstrapAndTheRefreshAreBounded(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		gw.Handle("/.well-known/atlas", func(c *fakegw.Call) { c.Hold(release) })
		start := time.Now()
		_, err := Connect(context.Background(), Options{URL: gw.URL, Home: t.TempDir(), HTTP: gw.Client()})
		wantProblem(t, err, "deadline_exceeded")
		if got := time.Since(start); got != BootstrapTimeout {
			t.Fatalf("the bootstrap was waited for %v, want %v", got, BootstrapTimeout)
		}
	})
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		gw.Handle("/oauth/token", func(c *fakegw.Call) { c.Hold(release) })
		home := t.TempDir()
		if err := SignIn(home, "conformance", "ct-old", "rt-1", 30*time.Second); err != nil {
			t.Fatal(err)
		}
		c := connect(t, gw, home)
		start := time.Now()
		_, err := c.Call(context.Background(), "atlas.whoami:fetch", nil, WithDeadline(2*time.Second))
		wantProblem(t, err, "deadline_exceeded")
		if got := time.Since(start); got != 2*time.Second {
			t.Fatalf("the refresh was waited for %v, want the call's 2s", got)
		}
	})
}

// R026: a request JSON cannot carry (a NaN) is refused before anything is
// sent, invalid_request; the next call still succeeds.
func TestR026ANaNIsRefusedBeforeSending(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	c := connect(t, gw, signedIn(t))
	ctx := context.Background()
	_, err := c.Call(ctx, "conformance.echo@v1:fetch", map[string]any{"text": "x", "ratio": math.NaN()})
	wantProblem(t, err, "invalid_request")
	if n := len(gw.To("conformance.echo@v1:fetch")); n != 0 {
		t.Fatalf("%d requests were sent", n)
	}
	r, err := c.Call(ctx, "conformance.echo@v1:fetch", map[string]any{"text": "x", "ratio": 0.5})
	if err != nil || r.Record == nil {
		t.Fatalf("the next call: %v", err)
	}
}

// PR06: a caller's own cancellation ends the call at once, and is told
// apart from the deadline.
func TestPR06ACancelledCallEndsAtOnce(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		gw.Handle("conformance.echo@v1:fetch", func(c *fakegw.Call) { c.Hold(release) })
		c := connect(t, gw, signedIn(t))
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(time.Second, cancel)
		start := time.Now()
		_, err := c.Call(ctx, "conformance.echo@v1:fetch", map[string]any{"text": "x"})
		p := wantProblem(t, err, "deadline_exceeded")
		if got := time.Since(start); got != time.Second || p.Detail != "the call was cancelled" {
			t.Fatalf("ended after %v: %v", got, p)
		}
		if errors.Is(err, io.EOF) {
			t.Fatal("unreachable")
		}
	})
}
