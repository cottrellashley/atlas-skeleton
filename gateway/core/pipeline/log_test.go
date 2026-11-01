package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// logs is a log the test reads back: one JSON record a line.
type logs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logs) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(b)
}

func (l *logs) logger() *slog.Logger { return slog.New(slog.NewJSONHandler(l, nil)) }

// with are the records whose text holds s.
func (l *logs) with(t *testing.T, s string) []map[string]any {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(l.buf.String()), "\n") {
		if !strings.Contains(line, s) {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("%v: %s", err, line)
		}
		out = append(out, rec)
	}
	return out
}

func (l *logs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// failingAuth is an authenticator whose own machinery fails.
type failingAuth struct{ err error }

func (f failingAuth) Authenticate(ctx context.Context, token string) (ports.Identity, error) {
	return ports.Identity{}, f.err
}

// R017, 06-7 §6.7.3 (causes kept for the log, never in the body), §6.7.16
// (the request id on every log line), MECH-014: whatever fails, the caller
// gets its problem with no detail of the cause, and the log holds the
// cause once, with the call's request id, and never the token or the body.
func TestR017TheCauseIsLoggedNeverSent(t *testing.T) {
	for _, c := range []struct {
		name, cause, code string
		opt               func(cause error, r *Options)
		provider          func(cause error) serve
	}{
		{name: "a provider's fault", cause: "permission denied on HARBOUR.REF.PORTFOLIOS_V", code: protocol.CodeInternal,
			provider: func(cause error) serve { return failing(cause) }},
		{name: "a source that may come back", cause: "dial tcp 10.0.0.7:443: connection refused", code: protocol.CodeUpstreamFailed,
			provider: func(cause error) serve { return failing(errors.Join(ports.ErrWarehouseUnavailable, cause)) }},
		{name: "the authenticator", cause: "jwks: fetching the keys: 503 Service Unavailable", code: protocol.CodeUnavailable,
			opt: func(cause error, o *Options) { o.Authenticator = failingAuth{cause} }},
		{name: "the policy source", cause: "cosmos: the control store timed out", code: protocol.CodeUnavailable,
			opt: func(cause error, o *Options) { pol := fakes.NewPolicy(nil); pol.Fail(cause); o.Policy = pol }},
		{name: "the audit sink", cause: "write /var/log/atlas/audit.jsonl: no space left on device", code: protocol.CodeUnavailable,
			opt: func(cause error, o *Options) { a := &fakes.Audit{}; a.Fail(cause); o.Audit = a }},
	} {
		t.Run(c.name, func(t *testing.T) {
			var l logs
			cause := errors.New(c.cause)
			opt := Options{Log: l.logger()}
			if c.opt != nil {
				c.opt(cause, &opt)
			}
			provider := rows(listRows("147HYC")...)
			if c.provider != nil {
				provider = c.provider(cause)
			}
			r := newRig(t, provider, opt)
			call := r.call("t-alice", listAt, `{"region": "emea", "asset_class": "credit"}`)
			call.Trace = "4bf92f3577b34da6a3ce929d0e0e4736"
			_, err := r.run(call)
			p := problemOf(t, err)
			if p.Code != c.code || strings.Contains(p.Detail, c.cause) || strings.Contains(p.Next, c.cause) {
				t.Fatalf("%s: %q, next %q", p.Code, p.Detail, p.Next)
			}
			found := l.with(t, c.cause)
			if len(found) != 1 || found[0]["request_id"] != call.RequestID || found[0]["trace_id"] != call.Trace {
				t.Fatalf("the cause is logged once, with the request and trace ids: %v\n%s", found, &l)
			}
			for _, secret := range []string{"t-alice", "asset_class", `\"credit\"`} {
				if strings.Contains(l.String(), secret) {
					t.Fatalf("the log holds %s:\n%s", secret, &l)
				}
			}
		})
	}
}
