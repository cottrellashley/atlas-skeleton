// Package assemble puts a gateway together from the parts a deployable
// chose: its authenticator, policy source, audit sink and warehouse, and
// the domains whose capabilities it serves. It is the one place that wires
// adapters to the core, so cmd/gateway (real systems) and the development
// gateway (fakes and DuckDB) differ only in the parts they pass (§6.5.3:
// only a deployable chooses an adapter).
package assemble

import (
	"context"
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
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
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

	// Domains are warehouse view-model capabilities, served through
	// DataSource as one module (views.Module). Modules are every other
	// integration the deployment registers: a company's providers, each
	// with its capabilities and its upstream (HANDOFF.md C1). Exclude
	// withholds pinned ids among them all.
	Domains []Domain
	Modules []ports.Module
	Exclude []string
	// Dimensions are the access dimensions the deployment defines beyond
	// the protocol's (A-200): what x-atlas-scope may name and a policy's
	// scopes may grant. Defining one grants nobody anything.
	Dimensions []ports.Dimension
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
	modules  []ports.Module
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
	dims, err := ports.NewDimensions(p.Dimensions...)
	if err != nil {
		return nil, fmt.Errorf("assemble: %w", err)
	}
	reg := registry.NewWith(dims)
	policy := checkedPolicy(p, reg)
	var files []views.Files
	for _, d := range p.Domains {
		fs, err := d()
		if err != nil {
			return nil, err
		}
		for _, f := range fs {
			// Left out before its model is built: what is withheld because
			// it is not ready never stops the start.
			if !slices.Contains(p.Exclude, f.Name) {
				files = append(files, f)
			}
		}
	}
	modules := slices.Clone(p.Modules)
	if len(files) > 0 {
		if p.DataSource == nil {
			return nil, errors.New("assemble: view-model capabilities need a data source")
		}
		m, err := views.Module(views.Warehouse, p.DataSource, views.Options{Clock: p.Clock}, p.WarehouseConcurrency, files...)
		if err != nil {
			return nil, err
		}
		modules = append([]ports.Module{m}, modules...)
	}
	upstreams, err := upstreamsOf(p, modules)
	if err != nil {
		return nil, err
	}
	more, err := entriesOf(modules, p.Exclude)
	if err != nil {
		return nil, err
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
	// One generation: every module's capabilities with the built-ins, held to
	// the gate together, before the policy is checked against them.
	if err := reg.Register(append(entries, more...)...); err != nil {
		return nil, err
	}
	if err := startPolicy(policy); err != nil {
		return nil, err
	}
	g := &Gateway{Registry: reg, Pipeline: pipe, parts: p, modules: modules}
	if g.Edge, err = edge.Open(edge.Options{Pipeline: pipe, URL: p.URL, SignIn: p.SignIn, Clock: p.Clock, Log: p.Log, Serving: p.Serving}); err != nil {
		return nil, err
	}
	return g, nil
}

// Add serves more view-model capabilities while the gateway runs
// (constitution rule 4): the next snapshot holds them, and no client is
// released to use them.
func (g *Gateway) Add(files ...views.Files) error {
	if len(files) == 0 {
		return nil
	}
	if g.parts.DataSource == nil {
		return errors.New("assemble: view-model capabilities need a data source")
	}
	m, err := views.Module(views.Warehouse, g.parts.DataSource, views.Options{Clock: g.parts.Clock}, g.parts.WarehouseConcurrency, files...)
	if err != nil {
		return err
	}
	entries, err := entriesOf([]ports.Module{m}, nil)
	if err != nil {
		return err
	}
	return g.Registry.Register(entries...)
}

// Health is each module's own health, by name: a module without a Health
// check is absent. A failing module costs its own capabilities, never the
// gateway's readiness (constitution rule 13).
func (g *Gateway) Health(ctx context.Context) map[string]error {
	out := map[string]error{}
	for _, m := range g.modules {
		if m.Health != nil {
			out[m.Name] = m.Health(ctx)
		}
	}
	return out
}

// Close releases what every module holds.
func (g *Gateway) Close() error {
	var errs []error
	for _, m := range g.modules {
		if m.Close != nil {
			if err := m.Close(); err != nil {
				errs = append(errs, fmt.Errorf("assemble: closing %s: %w", m.Name, err))
			}
		}
	}
	return errors.Join(errs...)
}

// upstreamsOf are the bulkheads the deployment's upstreams need: the
// warehouse's, whenever the deployment sets it, so view-model capabilities
// added later keep it, and each module's. Two that give one upstream
// different concurrencies are refused.
func upstreamsOf(p Parts, modules []ports.Module) (map[string]int, error) {
	upstreams := map[string]int{}
	if p.WarehouseConcurrency > 0 {
		upstreams[views.Warehouse] = p.WarehouseConcurrency
	}
	for _, m := range modules {
		if m.Concurrency < 0 {
			return nil, fmt.Errorf("assemble: the module %s's concurrency is negative", m.Name)
		}
		if m.Concurrency == 0 {
			continue
		}
		upstream := m.Upstream
		if upstream == "" {
			upstream = m.Name
		}
		if n, set := upstreams[upstream]; set && n != m.Concurrency {
			return nil, fmt.Errorf("assemble: the upstream %s is given a concurrency of %d and of %d", upstream, n, m.Concurrency)
		}
		upstreams[upstream] = m.Concurrency
	}
	if len(upstreams) == 0 {
		return nil, nil
	}
	return upstreams, nil
}

// entriesOf are the registry entries of modules, each capability with the
// profiles it replaced within its version, so the gate classifies its
// change against them and a request written under one is still resolved
// forward. A capability excluded by its pinned id is left out. A module
// with no name, one named twice, or one with capabilities and no provider,
// is refused.
func entriesOf(modules []ports.Module, exclude []string) ([]registry.Entry, error) {
	var entries []registry.Entry
	names := map[string]bool{}
	for _, m := range modules {
		switch {
		case m.Name == "":
			return nil, errors.New("assemble: a module has a name")
		case names[m.Name]:
			return nil, fmt.Errorf("assemble: two modules are named %q", m.Name)
		case m.Provider == nil && len(m.Capabilities) > 0:
			return nil, fmt.Errorf("assemble: the module %s has capabilities and no provider", m.Name)
		}
		names[m.Name] = true
		upstream := m.Upstream
		if upstream == "" {
			upstream = m.Name
		}
		reach := registry.AsGateway
		if m.AsCaller {
			reach = registry.AsCaller
		}
		provider := pipeline.Through(upstream, m.Provider)
		for _, c := range m.Capabilities {
			var chain []*profile.Profile
			for _, doc := range append(slices.Clone(c.Previous), c.Profile) {
				p, err := registry.Load(doc)
				if err != nil {
					return nil, fmt.Errorf("assemble: %s: %s: %w", m.Name, c.Name, err)
				}
				chain = append(chain, p)
			}
			cur := chain[len(chain)-1]
			if slices.Contains(exclude, grammar.Pinned{ID: cur.ID, Version: cur.Version}.String()) {
				continue
			}
			entries = append(entries, registry.Entry{Profiles: chain, Provider: provider, Reach: reach})
		}
	}
	return entries, nil
}
