package edge

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"

	"atlas.local/sdk/go/kit/middleware"
	"atlas.local/sdk/go/kit/telemetry"
	"atlas.local/sdk/go/protocol"
)

// layer is one of the edge's middleware, by its name.
type layer struct {
	name string
	wrap func(http.Handler) http.Handler
}

// chain is the edge's middleware, outermost first, in the order of
// §6.7.2's table (ATLAS-MECH-001). Transport only: nothing here decides
// access, and nothing here answers a bound address differently from an
// unbound one. CORS (§6.2.9) is not in that table; it stands after the
// drain, so a preflight is answered before any body or token work.
func (s *Server) chain() []layer {
	return []layer{
		{"recover", s.recovering},
		{"request-id", s.requestID},
		{"client-address", middleware.RealIP(s.opt.TrustedProxies, s.opt.ClientIPHeader)},
		{"trace-context", s.traceContext},
		{"access-log", s.accessLog},
		{"metrics", s.metrics},
		{"draining", s.draining},
		{"cors", s.cors},
		{"body-size", middleware.MaxBytes(s.opt.MaxBody)},
		{"outer-deadline", s.outerDeadline},
		{"pre-auth-limit", s.preAuth},
		{"in-flight", s.inFlight},
	}
}

// recovering is row 1: a panic answers internal, with the request's id.
// After an answer began, the stream writers recover a panic in the rows
// themselves and end the stream with internal (stream.go), so a panic that
// reaches here after the status was sent is only logged.
func (s *Server) recovering(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler { //nolint:errorlint // net/http's sentinel, compared by identity as net/http does
				panic(v)
			}
			x := exchangeOf(r)
			if x.id == "" {
				x.id = newID()
				w.Header().Set(protocol.HeaderRequestID, x.id)
			}
			s.opt.Log.Error("panic", "request_id", x.id, "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
			x.failed = protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
			s.problem(w, r, x.failed)
		}()
		next.ServeHTTP(w, r)
	})
}

// requestID is row 2: every answer carries X-Request-Id, errors included.
func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		x := exchangeOf(r)
		x.id = newID()
		w.Header().Set(protocol.HeaderRequestID, x.id)
		next.ServeHTTP(w, r)
	})
}

// maxTracestate is the longest tracestate the edge carries on (W3C Trace
// Context §3.3.1.5: at least 512 characters are kept).
const maxTracestate = 512

// traceContext is row 4: the caller's traceparent is continued, a fresh one
// started when it has none that parses (§9.6.3), and the answer carries the
// gateway's own span of the trace. The trace reaches what follows through
// kit/telemetry's context, so the logs, the pipeline and every outbound
// call carry the same trace id (constitution rule 14).
func (s *Server) traceContext(next http.Handler) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		x := exchangeOf(r)
		if tp, ok := telemetry.TraceFrom(r.Context()); ok {
			x.trace = tp.TraceIDString()
			w.Header().Set(protocol.HeaderTraceparent, tp.String())
			if ts, ok := tracestate(r.Header); ok {
				w.Header().Set(protocol.HeaderTracestate, ts)
			}
		}
		next.ServeHTTP(w, r)
	})
	propagate := telemetry.Propagate(true)(inner)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := traceparent(r.Header); !ok && (r.Header.Get(protocol.HeaderTraceparent) != "" || r.Header.Get(protocol.HeaderTracestate) != "") {
			// One that does not parse is absent (§9.6.3): a new trace
			// starts, and its tracestate goes with it.
			r = r.Clone(r.Context())
			r.Header.Del(protocol.HeaderTraceparent)
			r.Header.Del(protocol.HeaderTracestate)
		}
		propagate.ServeHTTP(w, r)
	})
}

// traceparent is the request's traceparent, when it has exactly one that
// parses (W3C Trace Context §3.2: several are invalid).
func traceparent(h http.Header) (protocol.Traceparent, bool) {
	vs := h.Values(protocol.HeaderTraceparent)
	if len(vs) != 1 {
		return protocol.Traceparent{}, false
	}
	tp, err := protocol.ParseTraceparent(vs[0])
	return tp, err == nil
}

// tracestate is the request's tracestate, carried on unchanged when it is
// short enough and printable; otherwise it is dropped.
func tracestate(h http.Header) (string, bool) {
	vs := h.Values(protocol.HeaderTracestate)
	if len(vs) == 0 {
		return "", false
	}
	ts := strings.Join(vs, ",")
	if len(ts) > maxTracestate {
		return "", false
	}
	for i := 0; i < len(ts); i++ {
		if ts[i] < 0x20 || ts[i] > 0x7e {
			return "", false
		}
	}
	return ts, strings.TrimSpace(ts) != ""
}

// accessLog is row 5: one line per request, probes skipped. It names the
// call by its bound address, never its path or query string, and holds no
// token, key or body (MECH-014). A caller that went away is 499 (§6.7.3).
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		x := exchangeOf(r)
		if x.probe {
			return
		}
		code := x.code
		if code == "" {
			code = x.w.Header().Get(protocol.HeaderErrorCode)
		}
		status := x.status()
		attrs := []any{
			"request_id", x.id, "trace_id", x.trace, "method", method(r),
			"address", s.address(r, x), "status", status, "code", code,
			"bytes", x.w.BytesWritten(), "ms", s.opt.Clock.Now().Sub(x.start).Milliseconds(),
			"client", middleware.ClientIPFrom(r.Context()),
		}
		if status >= 500 {
			s.opt.Log.Error("call", attrs...)
			return
		}
		s.opt.Log.Info("call", attrs...)
	})
}

// method is a request's method from a bounded set, for labels.
func method(r *http.Request) string {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return r.Method
	}
	return "OTHER"
}

// metrics is row 6: the rate, errors and duration of each address, every
// label from a bounded set (MECH-015).
func (s *Server) metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		x := exchangeOf(r)
		if x.probe {
			next.ServeHTTP(w, r)
			return
		}
		s.opt.Metrics.InFlight(1)
		defer s.opt.Metrics.InFlight(-1)
		next.ServeHTTP(w, r)
		s.opt.Metrics.ObserveRequest(method(r), s.address(r, x), x.status(), s.opt.Clock.Now().Sub(x.start), max(r.ContentLength, 0), x.w.BytesWritten())
	})
}

// draining is row 7: while the process is not ready (shutting down, or not
// yet started) every call is unavailable with Retry-After 2 and
// Connection: close, so the client takes another replica (ATLAS-MECH-016).
// The probes still answer: /readyz says why.
func (s *Server) draining(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.opt.Ready == nil || s.opt.Ready.Ready() || exchangeOf(r).probe {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Connection", "close")
		s.problem(w, r, protocol.NewProblem(protocol.CodeUnavailable, "this replica of the gateway is not serving: it is starting or shutting down").WithRetryAfter(2).WithNext("Repeat the call after retry_after seconds; another replica answers it."))
	})
}

// outerDeadline is row 9: before any I/O, the call is bounded by its
// Atlas-Deadline, capped by the largest interaction maximum, and the
// pipeline narrows it after step 2. One that has already passed is refused
// at once, the same wherever it is sent. Atlas-Deadline is absent when it
// does not parse (§9.6.3), and the cap bounds the call.
func (s *Server) outerDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		x := exchangeOf(r)
		now := s.opt.Clock.Now()
		deadline := now.Add(s.ceiling())
		if f, ok := protocol.Field(r.Header, protocol.HeaderDeadline); ok {
			if d, err := protocol.ParseDeadline(f); err == nil {
				if !d.After(now) {
					s.problem(w, r, protocol.NewProblem(protocol.CodeDeadlineExceeded, "the call's deadline had passed when it arrived").WithTarget(protocol.HeaderDeadline).WithNext("Repeat the call with a later deadline."))
					return
				}
				deadline = minTime(deadline, d)
			}
		}
		x.outer = deadline
		ctx, cancel := context.WithDeadline(r.Context(), s.real(deadline))
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// preAuth is row 10: each client address may have only so many calls
// refused as unauthenticated, so a flood of bad or missing tokens cannot
// keep token verification busy. Every refusal as unauthenticated spends one
// of the address's allowance; an address that has spent it is refused
// limit_exceeded before any token is read, until it refills. A call that
// signs in costs nothing, however long it runs. The bootstrap and the
// probes need no token and are exempt.
func (s *Server) preAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		key := clientKey(middleware.ClientIPFrom(r.Context()))
		if retry, ok := s.unauth.allow(key, s.opt.Clock.Now()); !ok {
			s.problem(w, r, protocol.NewProblem(protocol.CodeLimitExceeded, "too many calls from this address were refused as unauthenticated").WithRetryAfter(retry).WithNext("Sign in, then repeat the call after retry_after seconds."))
			return
		}
		next.ServeHTTP(w, r)
		if exchangeOf(r).code == protocol.CodeUnauthenticated {
			s.unauth.charge(key, s.opt.Clock.Now())
		}
	})
}

// inFlight is row 11: one replica serves at most MaxInFlight requests at
// once, a stream holding its place until it ends; past it, unavailable
// with Retry-After. The probes are exempt, so a busy replica still answers
// its load balancer.
func (s *Server) inFlight(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exchangeOf(r).probe {
			next.ServeHTTP(w, r)
			return
		}
		select {
		case s.inflight <- struct{}{}:
			defer func() { <-s.inflight }()
			next.ServeHTTP(w, r)
		default:
			s.problem(w, r, protocol.NewProblem(protocol.CodeUnavailable, "this replica of the gateway is serving as many calls as it can").WithRetryAfter(1).WithNext("Repeat the call after retry_after seconds."))
		}
	})
}
