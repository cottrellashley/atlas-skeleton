package views

import (
	"fmt"

	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
)

// Warehouse is the upstream every view-model capability reaches, so they
// share its bulkhead, those added later included (06-7 §6.7.5).
const Warehouse = "warehouse"

// Module is warehouse view models as a module (HANDOFF.md C1): one
// provider reading through src serves every capability of files. Each view
// model is checked against its profile here, so a model that cannot answer
// its contract stops the gateway at start, never on a call. concurrency is
// the warehouse's bulkhead, 0 for the pipeline's default.
func Module(name string, src ports.DataSource, opt Options, concurrency int, files ...Files) (ports.Module, error) {
	var bindings []Binding
	caps := make([]ports.Capability, 0, len(files))
	for _, f := range files {
		p, err := registry.Load(f.Profile)
		if err != nil {
			return ports.Module{}, fmt.Errorf("views: %s: %w", f.Name, err)
		}
		m, err := ParseModel(f.Model)
		if err != nil {
			return ports.Module{}, fmt.Errorf("views: %s: %w", f.Name, err)
		}
		bindings = append(bindings, Binding{Profile: p, Model: m})
		caps = append(caps, ports.Capability{Name: f.Name, Profile: f.Profile, Previous: f.Previous})
	}
	m := ports.Module{Name: name, Capabilities: caps, Upstream: Warehouse, Concurrency: concurrency}
	if len(files) == 0 {
		return m, nil
	}
	provider, err := Build(src, opt, bindings...)
	if err != nil {
		return ports.Module{}, err
	}
	m.Provider = provider
	return m, nil
}
