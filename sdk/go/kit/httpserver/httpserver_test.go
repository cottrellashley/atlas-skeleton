// Includes MIT-licensed serving utilities; see sdk/go/THIRD_PARTY_NOTICES.md.
// Changes: import paths; later edits are marked ATLAS.

package httpserver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"atlas.local/sdk/go/kit/conc"
	"atlas.local/sdk/go/kit/lifecycle"
)

// ATLAS: every test below runs in a synctest bubble over an in-memory
// listener. The bubble's clock is fake, so the shutdown delay, the grace
// period and the body bound pass at once and exactly; and the bubble fails
// the test if a goroutine it started is still running when it ends, which
// is the leak check (AGENTS.md: never sleep to wait, check for leaks).

var discard = slog.New(slog.DiscardHandler)

// R123, R016: Defaults are §6.7.15's values, which are requirements, not
// defaults to tune.
func TestDefaultsAreTheSpecsServingValues(t *testing.T) {
	got := Defaults()
	want := Config{
		Addr:              ":8000",
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       5 * time.Minute,
		MaxHeaderBytes:    64 << 10,
		BodyReadTimeout:   30 * time.Second,
		EnableH2C:         true,
		ShutdownDelay:     5 * time.Second,
		GracePeriod:       30 * time.Second,
	}
	if got != want {
		t.Fatalf("Defaults() = %+v\nwant         %+v", got, want)
	}
}

// R123: the server is built with the configuration's values and both
// protocols, h2c among them (§6.7.15).
func TestNewSetsProtocolsAndTimeouts(t *testing.T) {
	cfg := Defaults()
	srv := New(context.Background(), cfg, http.NotFoundHandler(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if srv.ReadHeaderTimeout != 10*time.Second || srv.IdleTimeout != 5*time.Minute || srv.MaxHeaderBytes != 64<<10 || srv.ReadTimeout != 0 || srv.WriteTimeout != 0 {
		t.Fatalf("timeouts not applied: %+v", srv)
	}
	if !srv.Protocols.UnencryptedHTTP2() || !srv.Protocols.HTTP2() || !srv.Protocols.HTTP1() {
		t.Fatalf("protocols: %v", srv.Protocols)
	}
}

// R016, ATLAS-MECH-016: a call running when the server is told to stop
// finishes. Readiness turns false at the signal, the server keeps serving
// through the shutdown delay, then stops accepting and waits for the call;
// and the call's own context is never cancelled by the signal.
func TestR016ServeLetsRunningCallsFinish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := Defaults()
		ln := newPipeListener()
		started, release := make(chan struct{}), make(chan struct{})
		var atRelease error
		mux := http.NewServeMux()
		mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-release
			atRelease = r.Context().Err()
			_, _ = io.WriteString(w, "done")
		})
		mux.HandleFunc("GET /quick", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "quick") })
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		var ready lifecycle.Readiness
		srv := New(ctx, cfg, mux, discard)
		served := conc.Go(func() error { return Serve(ctx, srv, ln, cfg, &ready, discard) })
		synctest.Wait()
		if !ready.Ready() {
			t.Fatal("not ready once serving")
		}
		slow := conc.Go(func() error { return expect(ln.client(), "/slow", http.StatusOK, "done") })
		<-started
		stop() // the signal
		synctest.Wait()
		if ready.Ready() {
			t.Fatal("readiness is still true after the signal")
		}
		// During the delay the load balancer has not noticed yet: a new call
		// is still served.
		if err := expect(ln.client(), "/quick", http.StatusOK, "quick"); err != nil {
			t.Fatalf("a call during the shutdown delay: %v", err)
		}
		time.Sleep(cfg.ShutdownDelay)
		synctest.Wait()
		if c, err := ln.DialContext(context.Background(), "tcp", "atlas"); err == nil {
			c.Close()
			t.Fatal("the server accepted a connection while draining")
		}
		close(release)
		if err := slow(); err != nil {
			t.Fatalf("the running call: %v", err)
		}
		if atRelease != nil {
			t.Fatalf("the running call's context ended at the signal: %v", atRelease)
		}
		if err := served(); err != nil {
			t.Fatalf("Serve: %v", err)
		}
	})
}

// R016, ATLAS-MECH-016: a call still running when the grace period ends is
// ended: its context is cancelled and its connection closed, and Serve
// returns. The drain is bounded.
func TestR016ServeEndsWhatOutlivesTheGracePeriod(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := Defaults()
		ln := newPipeListener()
		started, ended := make(chan struct{}), make(chan error, 1)
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
			ended <- r.Context().Err()
		})
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		srv := New(ctx, cfg, h, discard)
		served := conc.Go(func() error { return Serve(ctx, srv, ln, cfg, nil, discard) })
		call := conc.Go(func() error { return expect(ln.client(), "/forever", http.StatusOK, "") })
		<-started
		begun := time.Now()
		stop()
		if err := <-ended; !errors.Is(err, context.Canceled) {
			t.Fatalf("the call's context: %v", err)
		}
		if took, bound := time.Since(begun), cfg.ShutdownDelay+cfg.GracePeriod; took != bound {
			t.Fatalf("the call was ended after %v, not at the bound %v", took, bound)
		}
		if err := call(); err == nil {
			t.Fatal("the client was answered by a call that never finished")
		}
		if err := served(); err != nil {
			t.Fatalf("Serve: %v", err)
		}
	})
}

// R123: §6.7.15's 64 KiB of header bytes, for a person's bearer token with
// many groups. 48 KiB reaches the handler; beyond the bound net/http
// refuses the request before any handler runs.
func TestR123HeaderBytesAre64KiB(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := Defaults()
		ln := newPipeListener()
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, len(r.Header.Get("Authorization")))
		})
		ctx, stop := context.WithCancel(context.Background())
		srv := New(ctx, cfg, h, discard)
		served := conc.Go(func() error { return Serve(ctx, srv, ln, cfg, nil, discard) })
		get := func(size int) (*http.Response, error) {
			req, _ := http.NewRequest(http.MethodGet, "http://atlas/", nil)
			req.Header.Set("Authorization", "Bearer "+strings.Repeat("a", size))
			return ln.client().Do(req)
		}
		res, err := get(48 << 10)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK || string(body) != fmt.Sprint(7+48<<10) {
			t.Fatalf("48 KiB of headers: %d %q", res.StatusCode, body)
		}
		res, err = get(80 << 10)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
			t.Fatalf("80 KiB of headers: %d", res.StatusCode)
		}
		stop()
		if err := served(); err != nil {
			t.Fatal(err)
		}
	})
}

// R048 (the server's part): a body that trickles in is cut off once the
// body bound has passed, before any token is read, so an unauthenticated
// client cannot hold a connection and its goroutine open by sending a byte
// at a time.
func TestR048ATricklingBodyIsCutOff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := Defaults()
		ln := newPipeListener()
		read := make(chan error, 1)
		var took time.Duration
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			begun := time.Now()
			_, err := io.ReadAll(r.Body)
			took = time.Since(begun)
			read <- err
			w.WriteHeader(http.StatusRequestTimeout)
		})
		ctx, stop := context.WithCancel(context.Background())
		srv := New(ctx, cfg, h, discard)
		served := conc.Go(func() error { return Serve(ctx, srv, ln, cfg, nil, discard) })
		c, err := ln.DialContext(context.Background(), "tcp", "atlas")
		if err != nil {
			t.Fatal(err)
		}
		sent := conc.Go(func() error {
			_, err := io.WriteString(c, "POST /v1/data/portfolio.holdings@v1:fetch HTTP/1.1\r\nHost: atlas\r\nContent-Length: 1000000\r\n\r\n{")
			return err
		})
		if err := sent(); err != nil {
			t.Fatal(err)
		}
		drained := conc.Go(func() error { _, err := io.Copy(io.Discard, c); return err })
		if err := <-read; !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("the trickling body was read to %v", err)
		}
		if took != cfg.BodyReadTimeout {
			t.Fatalf("the body was cut off after %v, not at the bound %v", took, cfg.BodyReadTimeout)
		}
		c.Close()
		_ = drained()
		stop()
		if err := served(); err != nil {
			t.Fatal(err)
		}
	})
}

// R048 (the server's part): the body bound never bounds the answer. Once the
// body is read, a call may stream for as long as its deadline allows, and
// its context is not cancelled when the bound passes (§6.7.15: no read or
// write timeouts, for streams).
func TestR048TheBodyBoundNeverCutsTheAnswer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := Defaults()
		ln := newPipeListener()
		var after error
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := io.ReadAll(r.Body); err != nil {
				t.Errorf("the body: %v", err)
			}
			for i := range 6 {
				time.Sleep(cfg.BodyReadTimeout)
				_, _ = fmt.Fprintf(w, "line %d\n", i)
				w.(http.Flusher).Flush()
			}
			after = r.Context().Err()
		})
		ctx, stop := context.WithCancel(context.Background())
		srv := New(ctx, cfg, h, discard)
		served := conc.Go(func() error { return Serve(ctx, srv, ln, cfg, nil, discard) })
		res, err := ln.client().Post("http://atlas/v1/data/portfolio.holdings@v1:fetch", "application/json", strings.NewReader(`{"portfolios":["147HYC"]}`))
		if err != nil {
			t.Fatal(err)
		}
		lines := 0
		for sc := bufio.NewScanner(res.Body); sc.Scan(); {
			lines++
		}
		res.Body.Close()
		if lines != 6 || after != nil {
			t.Fatalf("the stream: %d lines, its context %v", lines, after)
		}
		stop()
		if err := served(); err != nil {
			t.Fatal(err)
		}
	})
}

// A server that cannot listen says so (Run binds the configured address).
func TestRunReportsAnAddressItCannotListenOn(t *testing.T) {
	cfg := Defaults()
	cfg.Addr = "127.0.0.1:99999"
	srv := New(context.Background(), cfg, http.NotFoundHandler(), discard)
	if err := Run(context.Background(), srv, cfg, nil, discard); err == nil {
		t.Fatal("Run listened on an address that does not exist")
	}
}

// expect makes one call and checks its answer.
func expect(c *http.Client, path string, status int, body string) error {
	res, err := c.Get("http://atlas" + path)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode != status || body != "" && string(b) != body {
		return fmt.Errorf("%s: %d %q", path, res.StatusCode, b)
	}
	return nil
}

// pipeListener is an in-memory listener: a dial hands the server one end of
// a net.Pipe. Everything in it blocks on channels, so a synctest bubble can
// tell when the server, its handlers and the client are all waiting.
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

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr{} }

// DialContext connects to the listener, or is refused once it is closed.
func (l *pipeListener) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
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

// client is an HTTP/1.1 client of the listener. It keeps no idle
// connection, so nothing of it outlives a call.
func (l *pipeListener) client() *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: l.DialContext, DisableKeepAlives: true}}
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "atlas" }
