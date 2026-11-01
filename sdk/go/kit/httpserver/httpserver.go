// Adapted from github.com/cottrellashley/cloud-gateway-patterns-go at 44ba81a
// (patterns/httpserver/httpserver.go), MIT licence: see sdk/go/THIRD_PARTY_NOTICES.md.
// Changes: import paths; later edits are marked ATLAS.

// Package httpserver builds and runs an http.Server the way Caddy, Traefik and
// the Kubernetes API server do: explicit timeouts, HTTP/2 on, a readiness flip
// before drain, a shutdown delay so load balancers notice, a bounded graceful
// period, then a hard close.
//
// Evidence (see docs/02-server/http-server-lifecycle.md):
//   - net/http Server.Shutdown closes listeners, then polls idle connections
//     with exponential backoff capped at 500ms until ctx expires.
//   - Caddy app.go: MaxHeaderBytes defaults to 16 KiB, IdleTimeout to 5
//     minutes, ShutdownDelay then GracePeriod.
//   - Traefik server_entrypoint_tcp.go Shutdown: RequestAcceptGraceTimeout
//     sleep, Shutdown with GraceTimeOut, and Close() when the deadline hits.
//   - Kubernetes genericapiserver RunWithContext: /readyz goes red first,
//     sleeps ShutdownDelayDuration, then stops accepting and drains.
//
// ATLAS: the defaults are §6.7.15's values, which are requirements; the
// signal that stops a server never cancels the requests it is running (they
// end by their own deadline, by their client, or when the grace period
// ends); and a request's body has a bound of its own, so a client cannot
// hold a connection by trickling one (R016, R048, R123).
package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/conc"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/lifecycle"
)

// Config is everything the server needs that is not the handler.
type Config struct {
	Addr string

	// ReadHeaderTimeout bounds slow-loris clients. Always set it.
	ReadHeaderTimeout time.Duration
	// ReadTimeout bounds the whole request read. Keep it 0 for a gateway that
	// accepts uploads and set per-route body limits and deadlines instead.
	ReadTimeout time.Duration
	// WriteTimeout bounds the whole response write. Keep it 0 when the server
	// serves SSE, WebSockets or large downloads; use per-request deadlines.
	WriteTimeout time.Duration
	// IdleTimeout closes keep-alive connections that sit idle.
	IdleTimeout time.Duration
	// MaxHeaderBytes caps request headers. Caddy uses 16 KiB; stdlib 1 MiB.
	MaxHeaderBytes int
	// BodyReadTimeout bounds how long a request's body may take to arrive,
	// from the moment its handler starts until the body's end is read; 0 is
	// no bound. ATLAS: it is not a read timeout in http.Server's sense: it
	// never bounds a response, so a call may stream for as long as its
	// deadline allows (R048).
	BodyReadTimeout time.Duration

	// EnableH2C serves unencrypted HTTP/2 (behind a TLS-terminating front
	// door such as Azure Front Door or App Gateway). Go 1.24+ stdlib.
	EnableH2C bool

	// ShutdownDelay is how long to keep serving after readiness goes false so
	// that load balancers stop routing to this instance before it drains.
	ShutdownDelay time.Duration
	// GracePeriod bounds Shutdown; after it the server is closed hard.
	GracePeriod time.Duration
}

// Defaults are production values for a gateway behind a cloud load balancer.
// ATLAS: they are §6.7.15's: the address :8000, no read or write timeout
// (every call is bounded by its deadline instead), 10 s to read the headers,
// 5 minutes idle, 64 KiB of headers for a person's bearer token with many
// groups, h2c on, and readiness false, 5 s, then 30 s to finish what runs.
// A body has 30 s to arrive, as at the edge, which narrows it to the
// call's deadline (ASSUMPTIONS.md A-100).
func Defaults() Config {
	return Config{
		Addr:              ":8000",
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       0,
		WriteTimeout:      0,
		IdleTimeout:       5 * time.Minute,
		MaxHeaderBytes:    64 << 10,
		BodyReadTimeout:   30 * time.Second,
		EnableH2C:         true,
		ShutdownDelay:     5 * time.Second,
		GracePeriod:       30 * time.Second,
	}
}

// New builds the http.Server. ATLAS: every request's context carries ctx's
// values but not its cancellation, so the context that stops the server
// (Serve's) may be the same one: a signal never cancels a running call.
func New(ctx context.Context, cfg Config, h http.Handler, log *slog.Logger) *http.Server {
	base := context.WithoutCancel(ctx)
	if cfg.BodyReadTimeout > 0 {
		h = bodyBound(h, cfg.BodyReadTimeout)
	}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           h,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
		BaseContext:       func(net.Listener) context.Context { return base },
		Protocols:         new(http.Protocols),
	}
	srv.Protocols.SetHTTP1(true)
	srv.Protocols.SetHTTP2(true)
	if cfg.EnableH2C {
		srv.Protocols.SetUnencryptedHTTP2(true)
	}
	if log != nil {
		srv.ErrorLog = slog.NewLogLogger(log.Handler(), slog.LevelWarn)
	}
	return srv
}

// Run listens on srv.Addr and serves until ctx is done, then drains: see
// Serve. Use it as a lifecycle.Group actor: execute = Run, interrupt =
// cancel ctx.
func Run(ctx context.Context, srv *http.Server, cfg Config, ready *lifecycle.Readiness, log *slog.Logger) error {
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	return Serve(ctx, srv, ln, cfg, ready, log)
}

// Serve serves on ln until ctx is done, then performs the drain sequence
// (§6.7.15, ATLAS-MECH-016):
//
//  1. readiness.Set(false)            // /readyz turns red
//  2. wait cfg.ShutdownDelay          // LB health checks observe it; calls are still served
//  3. srv.Shutdown(grace)             // stop listening, let running calls finish
//  4. srv.Close() if grace expired    // end the calls still running: their contexts are cancelled
//
// It returns nil on a clean stop and the serve error otherwise.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, cfg Config, ready *lifecycle.Readiness, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	stopped := make(chan struct{})
	served := conc.Go(func() error {
		defer close(stopped)
		return srv.Serve(ln)
	})
	if ready != nil {
		ready.Set(true)
	}
	log.Info("http server listening", "addr", ln.Addr().String())

	select {
	case <-stopped:
		return closed(served())
	case <-ctx.Done():
	}

	if ready != nil {
		ready.Set(false)
	}
	if cfg.ShutdownDelay > 0 {
		log.Info("shutdown: readiness off, waiting for load balancer", "delay", cfg.ShutdownDelay)
		delay := time.NewTimer(cfg.ShutdownDelay)
		select {
		case <-delay.C:
		case <-stopped:
		}
		delay.Stop()
	}
	// Shutdown must not inherit the already-cancelled ctx.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.GracePeriod)
	defer cancel()
	log.Info("shutdown: draining", "grace", cfg.GracePeriod)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("shutdown: grace period exceeded, closing connections", "err", err)
		if err := srv.Close(); err != nil {
			log.Warn("shutdown: closing", "err", err)
		}
	}
	<-stopped
	log.Info("shutdown: complete")
	return closed(served())
}

// closed is Serve's answer for what srv.Serve returned: nil once the server
// was shut down or closed.
func closed(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// bodyBound bounds how long a request's body may take to arrive (ATLAS,
// R048): the connection's read deadline is set when the handler starts and
// cleared once the body's end has been read, so a client that trickles its
// body is cut off with os.ErrDeadlineExceeded, and a response is never
// bounded by it. A request with no body is left alone.
func bodyBound(h http.Handler, d time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Body == http.NoBody {
			h.ServeHTTP(w, r)
			return
		}
		rc := http.NewResponseController(w)
		if err := rc.SetReadDeadline(time.Now().Add(d)); err != nil {
			// The writer cannot bound reads: not net/http's own.
			h.ServeHTTP(w, r)
			return
		}
		// Clearing cannot be refused where setting was not; a connection that
		// has gone has no reads left to bound.
		r.Body = &boundBody{ReadCloser: r.Body, end: func() { _ = rc.SetReadDeadline(time.Time{}) }}
		h.ServeHTTP(w, r)
	})
}

// boundBody clears the body's bound when the body ends. After that the
// connection is read only to see whether the client went away, which must
// never time out while a call streams its answer.
type boundBody struct {
	io.ReadCloser
	end  func()
	once sync.Once
}

func (b *boundBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) {
		b.once.Do(b.end)
	}
	return n, err
}
