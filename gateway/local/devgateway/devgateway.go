// Package devgateway assembles the development gateway: the real gateway
// over DuckDB holding the Harbour fixture, development sign-in and the
// fixture's policy. cmd/gateway-local serves it; the end-to-end tests run
// it in process.
package devgateway

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/adapter/auditlog"
	"github.com/jhgonline/fi-app-atlas/gateway/adapter/snowflake"
	"github.com/jhgonline/fi-app-atlas/gateway/assemble"
	"github.com/jhgonline/fi-app-atlas/gateway/edge"
	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/gateway/fixtures"
	"github.com/jhgonline/fi-app-atlas/gateway/local/duckwh"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/gateway/svc/datastatus"
	"github.com/jhgonline/fi-app-atlas/gateway/svc/portfolio"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
)

// Options configure the development gateway.
type Options struct {
	URL     string
	Exclude []string
	Policy  ports.PolicySource // the fixture's when nil
	Audit   io.Writer          // discarded when nil
	Log     *slog.Logger
}

// Gateway is a running development gateway's parts.
type Gateway struct {
	*assemble.Gateway
	Handler http.Handler
	Tokens  *fakes.Tokens
	engine  *duckwh.Engine
}

// New assembles the development gateway.
func New(ctx context.Context, opt Options) (*Gateway, error) {
	tables, err := fixtures.Harbour()
	if err != nil {
		return nil, err
	}
	source, engine, err := duckwh.OpenSource(ctx, tables, snowflake.Options{})
	if err != nil {
		return nil, err
	}
	ids, err := fixtures.Identities()
	if err != nil {
		engine.Close()
		return nil, err
	}
	tokens := fakes.NewTokens(clock.System)
	refresh := map[string]string{}
	for _, id := range ids {
		tokens.Issue(id.Token, id.Identity, time.Time{})
		refresh["refresh-"+id.Token] = id.Token
	}
	if opt.Policy == nil {
		p, err := fixtures.Policy()
		if err != nil {
			engine.Close()
			return nil, err
		}
		opt.Policy = fakes.NewPolicy(p)
	}
	if opt.Audit == nil {
		opt.Audit = io.Discard
	}
	if opt.Log == nil {
		opt.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	g, err := assemble.New(assemble.Parts{
		Environment: "local", URL: opt.URL,
		SignIn:        edge.SignIn{Authority: opt.URL, ClientID: "atlas-dev", Scopes: []string{"api://atlas-dev/.default"}, TokenEndpoint: opt.URL + "/oauth/token"},
		Authenticator: tokens, Policy: opt.Policy, Audit: auditlog.New(opt.Audit), DataSource: source, Log: opt.Log,
		Domains: []assemble.Domain{portfolio.Capabilities, datastatus.Capabilities}, Exclude: opt.Exclude, PolicyRefresh: time.Second,
	})
	if err != nil {
		engine.Close()
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", TokenEndpoint(refresh))
	mux.Handle("/", g.Edge)
	return &Gateway{Gateway: g, Handler: mux, Tokens: tokens, engine: engine}, nil
}

// Close releases the warehouse.
func (g *Gateway) Close() error { return g.engine.Close() }

// TokenEndpoint redeems a development refresh token (RFC 6749 §6).
func TokenEndpoint(refresh map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "refresh_token" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unsupported_grant_type"})
			return
		}
		rt := r.PostForm.Get("refresh_token")
		access, ok := refresh[rt]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "refresh_token": rt, "token_type": "Bearer", "expires_in": 3600})
	}
}
