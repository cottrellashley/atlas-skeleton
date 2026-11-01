package edge

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/clock"
	"atlas.local/sdk/go/kit/conc"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/frame"
	"atlas.local/sdk/go/protocol/ijson"
)

// These tests serve the edge over in-memory pipes inside a testing/synctest
// bubble: the pipes' deadlines, the server's timers and the pipeline's
// clock all run on the bubble's clock, so a deadline passes exactly when
// every goroutine is waiting on it, and a goroutine left running fails
// the test.

// pipes is a listener of in-memory connections.
type pipes struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newPipes() *pipes { return &pipes{conns: make(chan net.Conn), done: make(chan struct{})} }

func (p *pipes) Accept() (net.Conn, error) {
	select {
	case c := <-p.conns:
		return c, nil
	case <-p.done:
		return nil, net.ErrClosed
	}
}

func (p *pipes) Close() error { p.once.Do(func() { close(p.done) }); return nil }

func (p *pipes) Addr() net.Addr { return pipeAddr{} }

// dial is a client's end of a new connection.
func (p *pipes) dial() net.Conn {
	server, client := net.Pipe()
	p.conns <- server
	return client
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }

// overPipes serves a rig's edge over pipes for one bubble.
func overPipes(t *testing.T, o rigOptions) (*rig, *pipes) {
	t.Helper()
	o.clock, o.noServer = clock.System, true
	r := newRig(t, o)
	ln := newPipes()
	srv := &http.Server{Handler: r.edge, ReadHeaderTimeout: 10 * time.Second}
	wait := conc.Go(func() error { return srv.Serve(ln) })
	t.Cleanup(func() {
		_ = srv.Close()
		if err := wait(); !errors.Is(err, http.ErrServerClosed) {
			t.Error(err)
		}
	})
	return r, ln
}

// raw is a request's bytes: a POST with a full client's headers, extra
// headers as name, value pairs, and the body, which the Content-Length
// header may promise more of than is sent.
func raw(path, token, body string, extra ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "POST %s HTTP/1.1\r\nHost: gateway\r\n", path)
	if !slices.Contains(extra, "Content-Type") {
		b.WriteString("Content-Type: application/json\r\n")
	}
	b.WriteString(protocol.HeaderClient + ": " + client + "\r\n")
	b.WriteString(protocol.HeaderClientFeatures + ": " + fullFeatures + "\r\n")
	if token != "" {
		b.WriteString("Authorization: Bearer " + token + "\r\n")
	}
	hasLength := false
	for i := 0; i+1 < len(extra); i += 2 {
		hasLength = hasLength || extra[i] == "Content-Length"
		b.WriteString(extra[i] + ": " + extra[i+1] + "\r\n")
	}
	if !hasLength {
		fmt.Fprintf(&b, "Content-Length: %d\r\n", len(body))
	}
	b.WriteString("\r\n" + body)
	return b.String()
}

// exchangeOver sends a request and reads its answer's status and headers.
func exchangeOver(t *testing.T, conn net.Conn, req string) (*http.Response, *bufio.Reader) {
	t.Helper()
	if _, err := io.WriteString(conn, req); err != nil {
		t.Fatal(err)
	}
	br := bufio.NewReader(conn)
	res, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res, br
}

// R066, R048, §6.7.2 rows 8-9, §6.7.15 (no read timeout: the call's
// deadline bounds it): a body that trickles is cut at BodyTimeout, or at
// the call's own deadline when that is sooner, whether or not the caller
// signed in, and no token is verified for it.
func TestR066SlowBodyIsCutAtItsDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, ln := overPipes(t, rigOptions{})
		for _, c := range []struct {
			name     string
			token    string
			deadline time.Duration // from now, or none
			extra    []string
			after    time.Duration
			status   int
			code     string
		}{
			{"unauthenticated, no deadline", "", 0, nil, DefaultBodyTimeout, 400, protocol.CodeInvalidRequest},
			{"signed in, a deadline sooner than the body timeout", "t-alice", 5 * time.Second, nil, 5 * time.Second, 504, protocol.CodeDeadlineExceeded},
			// Answered before the body is read: the connection closes, and
			// net/http never waits on what is left of it.
			{"a deadline already passed", "t-alice", -time.Second, nil, 0, 504, protocol.CodeDeadlineExceeded},
			{"a body that is not JSON", "", 0, []string{"Content-Type", "text/plain"}, 0, 400, protocol.CodeInvalidRequest},
		} {
			extra := append([]string{"Content-Length", "1000"}, c.extra...)
			if c.deadline != 0 {
				extra = append(extra, protocol.HeaderDeadline, protocol.FormatDeadline(time.Now().Add(c.deadline)))
			}
			conn := ln.dial()
			start := time.Now()
			res, _ := exchangeOver(t, conn, raw(listAt, c.token, `{"a`, extra...))
			if took := time.Since(start); res.StatusCode != c.status || res.Header.Get(protocol.HeaderErrorCode) != c.code || took != c.after || !res.Close {
				t.Fatalf("%s: %d %s after %s, close %v", c.name, res.StatusCode, res.Header.Get(protocol.HeaderErrorCode), took, res.Close)
			}
			conn.Close()
		}
		if n := r.auth.calls.Load(); n != 0 {
			t.Fatalf("%d tokens verified for bodies that never arrived", n)
		}
	})
}

// R066, §6.7.2 row 11: a caller that stops reading its stream holds the
// connection, its goroutine and its concurrency slot only until the call's
// deadline and the grace to write its end: then the answer stops, its
// outcome is recorded, and the slot is free.
func TestR066AStalledReaderReleasesItsCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, ln := overPipes(t, rigOptions{limits: []ports.Limit{{Subject: "alice", Capability: "portfolio.holdings@v1", Concurrency: 1}}})
		r.provide(func(_ context.Context, inv *ports.Invocation) (*ports.Answer, error) {
			return &ports.Answer{Rows: &lazy{n: 1 << 30, make: func(i int) *ijson.Object { return holdingRow("147HYC", i) }}}, nil
		})
		conn := ln.dial()
		defer conn.Close()
		deadline := time.Now().Add(3 * time.Second)
		res, _ := exchangeOver(t, conn, raw(holdingsAt, "t-alice", `{"portfolios": ["147HYC"]}`, "Accept", protocol.MediaNDJSON, protocol.HeaderDeadline, protocol.FormatDeadline(deadline)))
		if res.StatusCode != 200 {
			t.Fatalf("%d", res.StatusCode)
		}
		id := res.Header.Get(protocol.HeaderRequestID)
		// The caller reads no further.
		time.Sleep(time.Until(deadline) + DefaultWriteGrace + time.Second)
		synctest.Wait()
		if out := outcomeFacts(r, id); len(out) != 1 {
			t.Fatalf("the stalled call's outcome: %+v", r.audit.Of(id))
		}
		next := ln.dial()
		defer next.Close()
		again, _ := exchangeOver(t, next, raw(holdingsAt, "t-alice", `{"portfolios": ["147HYC"]}`, "Accept", protocol.MediaNDJSON, protocol.HeaderDeadline, protocol.FormatDeadline(time.Now().Add(time.Second))))
		if again.StatusCode != 200 {
			t.Fatalf("the slot was not released: %d %s", again.StatusCode, again.Header.Get(protocol.HeaderErrorCode))
		}
	})
}

func outcomeFacts(r *rig, id string) []ports.Fact {
	var out []ports.Fact
	for _, f := range r.audit.Of(id) {
		if f.Kind == ports.Outcome {
			out = append(out, f)
		}
	}
	return out
}

// R138, §6.7.3 ("a caller that went away is logged as 499 and answered
// nothing"): a caller who hangs up mid-stream, before its deadline, is
// logged as 499, not as the 200 already sent; one who stops reading until
// the deadline passes ends as deadline_exceeded.
func TestR138ACallerWhoHangsUpIs499(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r, ln := overPipes(t, rigOptions{})
		r.provide(func(_ context.Context, inv *ports.Invocation) (*ports.Answer, error) {
			return &ports.Answer{Rows: &lazy{n: 1 << 30, make: func(i int) *ijson.Object { return holdingRow("147HYC", i) }}}, nil
		})
		status := func(id string) any {
			for _, line := range r.log.lines("call") {
				if line["request_id"] == id {
					return line["status"]
				}
			}
			return nil
		}
		conn := ln.dial()
		res, br := exchangeOver(t, conn, raw(holdingsAt, "t-alice", `{"portfolios": ["147HYC"]}`, "Accept", protocol.MediaNDJSON, protocol.HeaderDeadline, protocol.FormatDeadline(time.Now().Add(time.Minute))))
		if _, err := br.ReadString('\n'); err != nil || res.StatusCode != 200 {
			t.Fatalf("%d %v", res.StatusCode, err)
		}
		conn.Close()
		synctest.Wait()
		id := res.Header.Get(protocol.HeaderRequestID)
		if got := status(id); got != float64(499) {
			t.Fatalf("a caller who hung up was logged as %v", got)
		}
		if out := outcomeFacts(r, id); len(out) != 1 || out[0].Code != ports.CodeCancelled {
			t.Fatalf("a caller who hung up: outcome %+v", out)
		}

		stalled := ln.dial()
		defer stalled.Close()
		deadline := time.Now().Add(2 * time.Second)
		res, _ = exchangeOver(t, stalled, raw(holdingsAt, "t-alice", `{"portfolios": ["147HYC"]}`, "Accept", protocol.MediaNDJSON, protocol.HeaderDeadline, protocol.FormatDeadline(deadline)))
		time.Sleep(time.Until(deadline) + DefaultWriteGrace + time.Second)
		synctest.Wait()
		id = res.Header.Get(protocol.HeaderRequestID)
		for _, line := range r.log.lines("call") {
			if line["request_id"] == id && (line["status"] == float64(499) || line["code"] != protocol.CodeDeadlineExceeded) {
				t.Fatalf("a caller who stopped reading past its deadline: %v", line)
			}
		}
		if out := outcomeFacts(r, id); len(out) != 1 || out[0].Code != protocol.CodeDeadlineExceeded {
			t.Fatalf("a caller who stopped reading past its deadline: outcome %+v", out)
		}
	})
}

// R060, ATLAS-GATEWAY-005, ATLAS-SNOWFLAKE-002 (memory stays flat while
// streaming): the edge holds a stream's rows only until its next flush,
// never the whole answer. The source stops after a few hundred rows until
// the client has read what was sent before it, in NDJSON and in Arrow; an
// edge that held the answer whole would deadlock the bubble.
func TestR060StreamsAreNeverHeldWhole(t *testing.T) {
	for _, c := range []struct {
		name, accept string
		waitAt       int // the row the source waits at
		read         func(t *testing.T, br *bufio.Reader) error
	}{
		{"NDJSON", protocol.MediaNDJSON, flushEvery + 44, func(t *testing.T, br *bufio.Reader) error {
			for range flushEvery {
				if _, err := br.ReadString('\n'); err != nil {
					return err
				}
			}
			return nil
		}},
		{"Arrow", protocol.MediaArrowStream, arrowBatch + 44, func(t *testing.T, br *bufio.Reader) error {
			rd, err := frame.NewArrowReader(br, memory.NewGoAllocator())
			if err != nil {
				return err
			}
			b, err := rd.Next()
			if err != nil {
				return err
			}
			defer b.Release()
			if b.NumRows() != arrowBatch {
				return fmt.Errorf("the first batch holds %d rows", b.NumRows())
			}
			return nil
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r, ln := overPipes(t, rigOptions{})
				sent := make(chan struct{})
				r.provide(func(_ context.Context, inv *ports.Invocation) (*ports.Answer, error) {
					return &ports.Answer{Rows: &lazy{n: 10 * c.waitAt, make: func(i int) *ijson.Object { return holdingRow("147HYC", i) }, before: func(ctx context.Context, i int) error {
						if i == c.waitAt {
							select {
							case <-sent:
							case <-ctx.Done():
								return ctx.Err()
							}
						}
						return nil
					}}}, nil
				})
				conn := ln.dial()
				defer conn.Close()
				res, br := exchangeOver(t, conn, raw(holdingsAt, "t-alice", `{"portfolios": ["147HYC"]}`, "Accept", c.accept))
				if res.StatusCode != 200 {
					t.Fatalf("%d", res.StatusCode)
				}
				body := httputilChunked(br)
				if err := c.read(t, body); err != nil {
					t.Fatal(err)
				}
				close(sent) // the client has what was sent before the source waited
			})
		})
	}
}

// httputilChunked reads an HTTP/1.1 chunked body.
func httputilChunked(br *bufio.Reader) *bufio.Reader {
	return bufio.NewReader(httputil.NewChunkedReader(br))
}
