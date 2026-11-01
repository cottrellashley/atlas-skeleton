package ports

import "context"

// A Module is what a deployment registers: capabilities and the provider
// that serves them (HANDOFF.md C1). A company's integration is a Module:
// its profiles, its provider, the upstream it reaches and how many calls
// may reach it at once. Warehouse view models are one constructor of it
// (views.Module); a provider over an HTTP service, a job scheduler or any
// other system is another. The assembly registers every module's
// capabilities at once, with the gate and the policy check over them all,
// so a module never copies the assembly's work.
type Module struct {
	// Name names the module in errors, in health, and as its upstream
	// when Upstream is empty.
	Name string
	// Capabilities are its capabilities, each its profile and the history
	// of its version.
	Capabilities []Capability
	// Provider serves every interaction of every capability.
	Provider Provider
	// Upstream is the system the provider reaches: the bulkhead its calls
	// share with every other module that names it (06-7 §6.7.5), so an
	// outage of one upstream never takes another's places. Empty is Name.
	Upstream string
	// Concurrency is how many calls may reach the upstream at once; 0 is
	// the pipeline's default. Modules that share an upstream and both set
	// it must agree.
	Concurrency int
	// AsCaller is whether the provider reaches its system as the caller's
	// own identity, so the system's permissions apply after Atlas's and a
	// family grant may reach its reads (ATLAS-ACCESS-002). False, the
	// default, is the gateway's own identity: each capability is granted
	// by name only (06-3 rule 3).
	AsCaller bool
	// Health, when set, reports whether the upstream answers, through the
	// gateway's Health. It is the module's own, and never feeds the
	// gateway's readiness (constitution rule 13: a dependency's failure
	// costs its own capabilities, never the catalogue).
	Health func(ctx context.Context) error
	// Close, when set, releases what the module holds, once, when the
	// gateway closes. If the gateway fails to assemble, the module is still
	// its maker's to close.
	Close func() error
}

// Capability is one capability of a module: the profile it serves, and the
// profiles that profile replaced within its version, oldest first.
type Capability struct {
	// Name is its pinned id, id@vN, for errors.
	Name     string
	Profile  []byte
	Previous [][]byte
}
