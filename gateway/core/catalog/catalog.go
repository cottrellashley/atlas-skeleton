// Package catalog is the snapshot the request path reads (§6.1.14, §6.7.10):
// every profile the gateway serves, by exact id; which profile is current for
// each capability version; the service behind it; and the governance served
// beside it. A snapshot is built once, checked before it is used, and read
// with one atomic load per call; the request path never reads a store.
package catalog

import (
	"fmt"
	"slices"
	"sort"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// Entry is one served profile.
type Entry struct {
	Exact   string
	Profile *protocol.Profile
	Service ports.Service
}

// Snapshot is one immutable catalogue.
type Snapshot struct {
	Governance *Governance
	Revision   string
	byExact    map[string]*Entry
	current    map[string]*Entry
}

// Build admits every service's profiles, as registration does (§6.1.6), and
// returns the snapshot. pins chooses an earlier profile as current for a
// capability version (id@vN to its 1-based position in the service's
// declaration), as if the later ones were not yet deployed; 0 leaves the
// capability out altogether. It is how a test changes a contract, or adds a
// capability, "from the back".
func Build(services []ports.Service, pins map[string]int, gov *Governance) (*Snapshot, error) {
	s := &Snapshot{Governance: gov, byExact: map[string]*Entry{}, current: map[string]*Entry{}}
	for _, svc := range services {
		chains := map[string][]*protocol.Profile{}
		var order []string
		for _, p := range svc.Profiles() {
			if err := admit(svc, p); err != nil {
				return nil, err
			}
			k := p.Pinned().String()
			if _, seen := chains[k]; !seen {
				order = append(order, k)
			}
			chains[k] = append(chains[k], p)
		}
		for _, k := range order {
			chain := chains[k]
			if pin, ok := pins[k]; ok {
				if pin < 0 || pin > len(chain) {
					return nil, fmt.Errorf("pin %s=%d: the service declares %d profiles", k, pin, len(chain))
				}
				chain = chain[:pin] // 0: not deployed at all
			}
			prev := ""
			for _, p := range chain {
				e, err := protocol.ExactOf(p)
				if err != nil {
					return nil, err
				}
				exact := e.String()
				if p.Previous() != prev {
					return nil, fmt.Errorf("%s: evolution.previous is %q, the profile before it is %q", exact, p.Previous(), prev)
				}
				if _, dup := s.byExact[exact]; dup {
					return nil, fmt.Errorf("%s is declared twice", exact)
				}
				entry := &Entry{Exact: exact, Profile: p, Service: svc}
				s.byExact[exact] = entry
				s.current[k] = entry
				prev = exact
			}
		}
	}
	s.Revision = gov.Revision
	return s, nil
}

// admit is the registration check, provisionally: the domain claim, the id,
// and that every table result maps to Arrow.
func admit(svc ports.Service, p *protocol.Profile) error {
	if err := protocol.ValidID(p.ID); err != nil {
		return err
	}
	if protocol.IsBuiltin(p.ID) {
		return fmt.Errorf("%s: only the gateway serves atlas.*", p.ID)
	}
	if !slices.Contains(svc.Domains(), p.Domain) || protocol.Domain(p.ID) != p.Domain {
		return fmt.Errorf("%s: a service claiming %v may not serve the domain %s (ATLAS-GATEWAY-007)", p.ID, svc.Domains(), p.Domain)
	}
	if p.Result != nil {
		if _, err := protocol.ArrowSchema(p.Result, ""); err != nil {
			return fmt.Errorf("%s: %w", p.ID, err)
		}
	}
	for name, i := range p.Interactions {
		if len(i.Bindings) == 0 {
			return fmt.Errorf("%s:%s has no binding (ATLAS-BINDING-001)", p.ID, name)
		}
	}
	return nil
}

// Current returns the current profile of a capability version.
func (s *Snapshot) Current(p protocol.Pinned) (*Entry, bool) {
	e, ok := s.current[p.String()]
	return e, ok
}

// Exact returns any served profile by its exact id.
func (s *Snapshot) Exact(exact string) (*Entry, bool) {
	e, ok := s.byExact[exact]
	return e, ok
}

// Source lets resolution walk the snapshot's profiles.
func (s *Snapshot) Source() protocol.ProfileSource {
	return func(exact string) (*protocol.Profile, error) {
		if e, ok := s.byExact[exact]; ok {
			return e.Profile, nil
		}
		return nil, protocol.NewProblem(protocol.CodeResolutionFailed, "the gateway does not serve the profile "+exact)
	}
}

// CurrentEntries lists every current profile, sorted by id.
func (s *Snapshot) CurrentEntries() []*Entry {
	out := make([]*Entry, 0, len(s.current))
	for _, e := range s.current {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Exact < out[j].Exact })
	return out
}
