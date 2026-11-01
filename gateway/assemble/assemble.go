// Package assemble puts a gateway together from the parts a deployable
// chose: its authenticator, policy source, audit sink and warehouse, and
// the domains whose capabilities it serves. It is the one place that wires
// adapters to the core, so cmd/gateway (real systems) and the development
// gateway (fakes and DuckDB) differ only in the parts they pass (§6.5.3:
// only a deployable chooses an adapter).
package assemble

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/core/builtins"
	"github.com/jhgonline/fi-app-atlas/gateway/core/pipeline"
	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/gateway/edge"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/gateway/views"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

// Domain gives a domain's capabilities, as their owners wrote them.
type Domain func() ([]views.Files, error)

// Parts are what a deployable chose.
type Parts struct {
	Environment string
	// URL is the gateway's public base URL.
	URL    string
	SignIn edge.SignIn

	Authenticator ports.Authenticator
	Policy        ports.PolicySource
	Audit         ports.AuditSink
	// DataSource is the engine the view-model capabilities read through
	// (§6.4.1): Snowflake in production, DuckDB in development, memory in
	// tests.
	DataSource ports.DataSource
	Clock      clock.Clock
	Log        *slog.Logger

	// Domains are the capabilities served; Exclude withholds pinned ids
	// among them.
	Domains []Domain
	Exclude []string
	// CursorKey signs listing cursors, shared by every replica: at least
	// builtins.MinCursorKey bytes. PreviousCursorKey is the key it replaced,
	// accepted for the hour its cursors live after a rotation (§6.7.9).
	CursorKey, PreviousCursorKey []byte
	// Clients are what atlas.clients lists, with how this deployment hands
	// each one out: its own package registry, artifact store or checkout.
	// Nil lists DefaultClients.
	Clients []builtins.Client
	// PolicyRefresh and PolicyMaxAge bound the policy's staleness (R13).
	PolicyRefresh, PolicyMaxAge time.Duration
	// WarehouseConcurrency is how many calls may reach the warehouse at
	// once, its connection pool's size: the warehouse's bulkhead (06-7
	// §6.7.5). MaxRows is the output cap of a capability no policy limit
	// caps (§6.2.6), and MaxInFlight how many calls the replica runs at once
	// (06-7 §6.7.2). Zero is the pipeline's default for each.
	WarehouseConcurrency, MaxRows, MaxInFlight int
	// Serving is how the deployment serves the edge: its readiness and the
	// checks behind /readyz, its trusted proxies and origins, its limits
	// (§6.7.2, §6.7.15). The zero value is a development edge.
	Serving edge.Serving
}

// Gateway is an assembled gateway.
type Gateway struct {
	Registry *registry.Registry
	Pipeline *pipeline.Pipeline
	Edge     *edge.Server
	parts    Parts
}

// DefaultClients are what atlas.clients lists when the deployment names
// none: each is built from a checkout of the Atlas repository. Nothing here
// points at a public package registry, where these names are not published
// and could be anyone's (dependency confusion), or at the repository's
// private host, which a caller cannot and must not reach. A deployment that
// publishes its clients to its own registry lists them in Parts.Clients.
var DefaultClients = []builtins.Client{
	{Name: "atlas", Language: "shell", BuiltFor: "people and agents exploring and trying work", Version: "0.2.0", Install: "From a checkout of the Atlas repository: go -C clients/cli install ./cmd/atlas", Roles: []string{"explore", "try"}, Levels: []string{"core", "streaming"}},
	{Name: "fiatlas", Language: "python", BuiltFor: "programs and notebooks that repeat the work", Version: "0.2.0", Install: "From a checkout of the Atlas repository: uv pip install ./clients/python", Roles: []string{"build"}, Levels: []string{"core", "streaming"}, Extras: []string{"pandas"}},
	{Name: "@atlas/sdk", Language: "typescript", BuiltFor: "Node programs and services", Version: "0.2.0", Install: "From a checkout of the Atlas repository: npm install ./clients/node", Roles: []string{"build"}, Levels: []string{"core", "streaming"}},
	{Name: "atlas-go", Language: "go", BuiltFor: "Go programs, and the engine under fiatlas", Version: "0.2.0", Install: "From a checkout of the Atlas repository: add ./sdk/go to your go.work, or require it with a replace to that directory", Roles: []string{"build"}, Levels: []string{"core", "streaming", "columnar"}},
}

// New assembles a gateway: every capability passes the gate as it is
// registered, and a view model that cannot answer its profile fails here.
func New(p Parts) (*Gateway, error) {
	if err := checkBootstrap(p); err != nil {
		return nil, err
	}
	if p.Clock == nil {
		p.Clock = clock.System
	}
	if p.Log == nil {
		p.Log = slog.Default()
	}
	reg := registry.New()
	policy := checkedPolicy(p, reg)
	var upstreams map[string]int
	if p.WarehouseConcurrency > 0 {
		upstreams = map[string]int{warehouse: p.WarehouseConcurrency}
	}
	pipe, err := pipeline.New(pipeline.Options{
		Environment: p.Environment, Registry: reg, Authenticator: p.Authenticator, Policy: policy, Audit: p.Audit, Clock: p.Clock, Log: p.Log,
		PolicyRefresh: p.PolicyRefresh, PolicyMaxAge: p.PolicyMaxAge,
		MaxRows: p.MaxRows, MaxInFlight: p.MaxInFlight, Upstreams: upstreams,
	})
	if err != nil {
		return nil, err
	}
	clients := p.Clients
	if clients == nil {
		clients = DefaultClients
	}
	b, err := builtins.New(builtins.Options{Registry: reg, Environment: p.Environment, Clock: p.Clock, CursorKey: p.CursorKey, PreviousCursorKey: p.PreviousCursorKey, Clients: clients})
	if err != nil {
		return nil, err
	}
	entries, err := b.Entries()
	if err != nil {
		return nil, err
	}
	var files []views.Files
	for _, d := range p.Domains {
		fs, err := d()
		if err != nil {
			return nil, err
		}
		for _, f := range fs {
			if !slices.Contains(p.Exclude, f.Name) {
				files = append(files, f)
			}
		}
	}
	more, err := capabilities(p.DataSource, p.Clock, files)
	if err != nil {
		return nil, err
	}
	if err := reg.Register(append(entries, more...)...); err != nil {
		return nil, err
	}
	if err := startPolicy(policy); err != nil {
		return nil, err
	}
	g := &Gateway{Registry: reg, Pipeline: pipe, parts: p}
	if g.Edge, err = edge.Open(edge.Options{Pipeline: pipe, URL: p.URL, SignIn: p.SignIn, Clock: p.Clock, Log: p.Log, Serving: p.Serving}); err != nil {
		return nil, err
	}
	return g, nil
}

// Add serves more capabilities while the gateway runs (constitution rule 4):
// the next snapshot holds them, and no client is released to use them.
func (g *Gateway) Add(files ...views.Files) error {
	entries, err := capabilities(g.parts.DataSource, g.parts.Clock, files)
	if err != nil {
		return err
	}
	return g.Registry.Register(entries...)
}

// capabilities are view-model capabilities, one provider serving them all.
// Each is registered with the profiles it replaced within its version, so
// the gate classifies its change against them and a request written under
// one is still resolved forward.
func capabilities(src ports.DataSource, clk clock.Clock, files []views.Files) ([]registry.Entry, error) {
	if len(files) == 0 {
		return nil, nil
	}
	if src == nil {
		return nil, errors.New("assemble: view-model capabilities need a data source")
	}
	var bindings []views.Binding
	var chains [][]*profile.Profile
	for _, f := range files {
		var chain []*profile.Profile
		for _, doc := range append(slices.Clone(f.Previous), f.Profile) {
			p, err := registry.Load(doc)
			if err != nil {
				return nil, fmt.Errorf("assemble: %s: %w", f.Name, err)
			}
			chain = append(chain, p)
		}
		m, err := views.ParseModel(f.Model)
		if err != nil {
			return nil, fmt.Errorf("assemble: %s: %w", f.Name, err)
		}
		bindings = append(bindings, views.Binding{Profile: chain[len(chain)-1], Model: m})
		chains = append(chains, chain)
	}
	provider, err := views.Build(src, views.Options{Clock: clk}, bindings...)
	if err != nil {
		return nil, err
	}
	entries := make([]registry.Entry, len(chains))
	for i, chain := range chains {
		entries[i] = registry.Entry{Profiles: chain, Provider: pipeline.Through(warehouse, provider)}
	}
	return entries, nil
}

// warehouse is the upstream every view-model capability reaches, so they
// share its bulkhead, those added later included (06-7 §6.7.5).
const warehouse = "warehouse"
