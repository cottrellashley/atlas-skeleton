// Package registry is the gateway's catalogue of admitted capabilities
// (§6.1, constitution rule 4: new behaviour is a new module, registered in
// one list). A deployment registers each capability's profiles and the
// provider that serves them; every profile passes the gate again here
// (§3.3.12), so a breaking change can never reach callers even if it
// escaped review. A registration is one generation: admitted whole, or
// discarded whole with every reason. The request path reads an immutable
// snapshot (constitution rule 3), and a capability added while the gateway
// runs is served from the next snapshot without a restart.
//
// A version keeps every profile it served (§3.1: "a profile is served by
// fingerprint for as long as anything may hold it"; R045, PR05). Each
// profile new to its version's chain is classified against the one before
// it, whether it arrives in a deploy, which registers the version's
// history oldest first, or in a hot replace, so a deploy cannot skip R03.
// A profile that names a previous profile the gateway does not hold is
// refused, and every binding of every profile of the chain stays routed
// (D5, gaps/2.md).
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/documents"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/gate"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

// MaxChain is the most profiles one version may hold. Past it a change is
// a new version: so many changes to one contract are what a version is for.
const MaxChain = 64

// Load reads a profile as its author wrote it, and gives it the
// fingerprint it is served with.
func Load(doc []byte) (*profile.Profile, error) {
	v, err := ijson.Parse(doc)
	if err != nil {
		return nil, err
	}
	o, ok := v.(*ijson.Object)
	if !ok {
		return nil, errors.New("registry: a profile is a JSON object")
	}
	o.Delete("fingerprint")
	p, err := profile.Parse(o)
	if err != nil {
		return nil, err
	}
	o.Set("fingerprint", p.FP.Value)
	return p, nil
}

// Reach is the identity a capability's provider reaches its system as
// (06-3 §6.3.2).
type Reach int

const (
	// AsGateway is the gateway's own identity: a service identity, a
	// vendor's key. The system cannot narrow by person, so the gateway's
	// grant is the only limit, and the capability is granted by name only,
	// never by a family grant or everyone (06-3 rule 3). It is the zero
	// value: a registration that says nothing is held to rule 3.
	AsGateway Reach = iota
	// AsCaller is the caller's own identity (On-Behalf-Of for a person, a
	// workload's own), so the system's own permissions apply after
	// Atlas's, and a family grant may reach its read interactions
	// (ATLAS-ACCESS-002).
	AsCaller
)

func (r Reach) String() string {
	if r == AsCaller {
		return "the caller's own identity"
	}
	return "the gateway's own identity"
}

// Entry is one capability a deployment registers: its profiles within one
// version, oldest first, the last being the one served; its provider; and
// how that provider reaches its system.
type Entry struct {
	Profiles []*profile.Profile
	Provider ports.Provider
	Reach    Reach
}

// Capability is an admitted capability.
type Capability struct {
	Pinned   grammar.Pinned
	Current  *profile.Profile
	Chain    []*profile.Profile // every profile the version served, oldest first, ending with Current
	Provider ports.Provider
	Reach    Reach
	// Unready are the reasons Current fails the agent-readiness check
	// (§3.1.4a), none when it passes. A built-in is the protocol's, and is
	// not checked (A-016, A-183).
	Unready []string
}

// Builtin reports whether it is one of the atlas.* capabilities (§9.3.2).
func (c *Capability) Builtin() bool { return grammar.Builtin(c.Pinned.ID) }

// Lifecycle is the lifecycle the capability is served with: its current
// profile's, or preview when that profile is not ready for agents
// (§3.1.4a: "a profile that fails is served only as preview").
func (c *Capability) Lifecycle() string {
	if len(c.Unready) > 0 && c.Current.Lifecycle != "deprecated" && c.Current.Lifecycle != "retired" {
		return "preview"
	}
	return c.Current.Lifecycle
}

// Route is where one https binding leads: the interaction, and the
// binding the address is, from the newest profile that has it.
type Route struct {
	Capability  *Capability
	Interaction string
	Binding     profile.Binding
}

// Snapshot is one admitted generation of the catalogue. It never changes.
type Snapshot struct {
	Generation int
	ETag       string // the catalogue's strong entity tag, with its quotes
	caps       []*Capability
	byPinned   map[string]*Capability
	byExact    map[string]*profile.Profile
	byPath     map[string]Route
}

// Capabilities are every admitted capability, by pinned id.
func (s *Snapshot) Capabilities() []*Capability { return slices.Clone(s.caps) }

// Lookup is the capability a pinned id names, or nil.
func (s *Snapshot) Lookup(pinned string) *Capability { return s.byPinned[pinned] }

// Profile is a profile by its exact id: the current one or any the
// version served before (§3.1, rule 3), its newest document (D2).
func (s *Snapshot) Profile(exact string) *profile.Profile { return s.byExact[exact] }

// Route is the interaction a request path is bound to.
func (s *Snapshot) Route(path string) (Route, bool) {
	r, ok := s.byPath[path]
	return r, ok
}

// Has reports whether the catalogue serves a pinned id (gate.Catalogue).
func (s *Snapshot) Has(p grammar.Pinned) bool { return s.byPinned[p.String()] != nil }

// Registry holds the current snapshot.
type Registry struct {
	mu  sync.Mutex // one registration at a time
	cur atomic.Pointer[Snapshot]
}

// New is an empty registry.
func New() *Registry {
	r := &Registry{}
	r.cur.Store(build(0, nil))
	return r
}

// Snapshot is the catalogue as it stands.
func (r *Registry) Snapshot() *Snapshot { return r.cur.Load() }

// Refused is a registration the gate refused: every reason, for every
// profile, so one attempt shows the author everything to fix.
type Refused struct{ Reasons []string }

func (e *Refused) Error() string {
	return "registry: the generation was refused:\n" + strings.Join(e.Reasons, "\n")
}

// Register admits entries as one generation. Each entry's profiles join
// the chain its version already has: a profile new to the chain is
// classified against the one before it, and a profile already there, by
// fingerprint, is a newer document of it (D2). The profile an entry serves
// is its last, and is the newest of the chain: a version's profile only
// moves forward. Every https address of every profile leads to one
// interaction only, and every reference a schema makes must name a
// capability the new generation serves.
func (r *Registry) Register(entries ...Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old := r.cur.Load()
	byPinned := map[string]*Capability{}
	for _, c := range old.caps {
		byPinned[c.Pinned.String()] = c
	}
	var reasons []string
	refuse := func(format string, a ...any) { reasons = append(reasons, fmt.Sprintf(format, a...)) }
	type admitted struct {
		c     *Capability
		fresh []int // the chain's positions new or rewritten in this generation
	}
	var added []admitted
	for i, e := range entries {
		if len(e.Profiles) == 0 || e.Provider == nil {
			refuse("entry %d: a capability has a profile and a provider", i)
			continue
		}
		cur := e.Profiles[len(e.Profiles)-1]
		pinned := grammar.Pinned{ID: cur.ID, Version: cur.Version}
		ok := true
		for _, p := range e.Profiles {
			if p.ID != cur.ID || p.Version != cur.Version {
				refuse("%s: its chain holds %s@v%d, another capability", pinned, p.ID, p.Version)
				ok = false
			}
			if !grammar.Builtin(p.ID) {
				for _, f := range documents.Validate("profile", p.Doc) {
					refuse("%s: %s (%s): not a profile document (profile.schema.json, §9.3 rule 1)", p.Exact(), f.Pointer, f.Message)
					ok = false
				}
			}
		}
		if !ok {
			continue
		}
		var prev []*profile.Profile
		if c := byPinned[pinned.String()]; c != nil {
			prev = c.Chain
		}
		chain, fresh, why := merge(prev, e.Profiles)
		if why != "" {
			refuse("%s: %s", pinned, why)
			continue
		}
		if len(chain) > MaxChain {
			refuse("%s: its version would hold %d profiles, more than %d: make the change a new version", pinned, len(chain), MaxChain)
			continue
		}
		c := &Capability{Pinned: pinned, Current: chain[len(chain)-1], Chain: chain, Provider: e.Provider, Reach: e.Reach}
		if !grammar.Builtin(pinned.ID) {
			c.Unready = gate.Ready(c.Current)
		}
		byPinned[pinned.String()] = c
		added = append(added, admitted{c, fresh})
	}
	next := build(old.Generation+1, values(byPinned))
	for _, a := range added {
		for _, i := range a.fresh {
			p := a.c.Chain[i]
			var previous *profile.Profile
			if i > 0 {
				previous = a.c.Chain[i-1]
			} else if pp := p.Evolution.PreviousProfile; pp != nil {
				refuse("%s: it names its previous profile %s, which this gateway does not hold: register the version's profiles oldest first, from its first (R045)", p.Exact(), pp)
				continue
			}
			if rep := gate.Check(p, previous, next); rep.Refused() {
				refuse("%s: %s", p.Exact(), strings.ReplaceAll(rep.Reason(), "\n", "\n  "))
			}
		}
	}
	paths := map[string]string{}
	for _, c := range next.caps {
		for _, p := range c.Chain {
			for _, name := range p.Order {
				if c.Current.Interactions[name] == nil {
					refuse("%s: %s's interaction %s is not its current profile's", c.Pinned, p.Exact(), name)
					continue
				}
				for _, b := range p.Interactions[name].Bindings {
					if b.Protocol != "https" {
						continue
					}
					at := c.Pinned.String() + ":" + name
					if other, taken := paths[b.Address]; taken && other != at {
						refuse("%s: its binding %s is already %s's", at, b.Address, other)
					}
					paths[b.Address] = at
				}
			}
		}
	}
	if len(reasons) > 0 {
		return &Refused{reasons}
	}
	r.cur.Store(next)
	return nil
}

// merge is the chain a version holds once profiles join it: the profiles
// it held, oldest first, then each profile new to it. A profile it holds
// already, by fingerprint, replaces its document there, when its
// projection is the same (D2, §9.4.2 rule 2). fresh are the positions new
// or rewritten; why says what makes the chain impossible.
func merge(held, profiles []*profile.Profile) (chain []*profile.Profile, fresh []int, why string) {
	chain = slices.Clone(held)
	at := map[string]int{}
	for i, p := range chain {
		at[p.FP.Value] = i
	}
	for _, p := range profiles {
		i, ok := at[p.FP.Value]
		if !ok {
			at[p.FP.Value] = len(chain)
			fresh = append(fresh, len(chain))
			chain = append(chain, p)
			continue
		}
		same, err := sameProjection(chain[i], p)
		if err != nil {
			return nil, nil, err.Error()
		}
		if !same {
			return nil, nil, fmt.Sprintf("%s names two contracts: the fingerprint of one projection is another's (§9.4.2 rule 2)", p.Exact())
		}
		if chain[i] != p && !slices.Contains(fresh, i) {
			fresh = append(fresh, i)
		}
		chain[i] = p
	}
	served := profiles[len(profiles)-1]
	if chain[len(chain)-1].FP.Value != served.FP.Value {
		return nil, nil, fmt.Sprintf("%s is older than %s, which its version already serves: a version's profile only moves forward (R03)", served.Exact(), chain[len(chain)-1].Exact())
	}
	slices.Sort(fresh)
	return chain, fresh, ""
}

// sameProjection reports whether two profiles are one contract: the same
// canonical projection (§9.4.1), whatever their prose. It reads the
// documents themselves, never the fingerprints they were given.
func sameProjection(a, b *profile.Profile) (bool, error) {
	fa, err := protocol.Fingerprint(a.Doc)
	if err != nil {
		return false, err
	}
	fb, err := protocol.Fingerprint(b.Doc)
	if err != nil {
		return false, err
	}
	return string(fa.Canonical) == string(fb.Canonical), nil
}

func values(m map[string]*Capability) []*Capability {
	out := make([]*Capability, 0, len(m))
	for _, c := range m {
		out = append(out, c)
	}
	return out
}

func build(generation int, caps []*Capability) *Snapshot {
	sort.Slice(caps, func(i, j int) bool {
		a, b := caps[i].Pinned, caps[j].Pinned
		return a.ID < b.ID || a.ID == b.ID && a.Version < b.Version
	})
	s := &Snapshot{Generation: generation, caps: caps, byPinned: map[string]*Capability{}, byExact: map[string]*profile.Profile{}, byPath: map[string]Route{}}
	h := sha256.New()
	for _, c := range caps {
		s.byPinned[c.Pinned.String()] = c
		for _, p := range c.Chain {
			s.byExact[p.Exact().String()] = p
			// Oldest first, so an address a later profile keeps is that
			// profile's binding.
			for _, name := range p.Order {
				for _, b := range p.Interactions[name].Bindings {
					if b.Protocol == "https" {
						s.byPath[b.Address] = Route{Capability: c, Interaction: name, Binding: b}
					}
				}
			}
		}
		fmt.Fprintln(h, c.Current.Exact().String())
	}
	s.ETag = `"` + hex.EncodeToString(h.Sum(nil))[:16] + `"`
	return s
}
