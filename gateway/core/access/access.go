// Package access decides what a caller may do and read (§6.2, ATLAS-CORE
// §3): one resolver shared by discovery, guides and execution, so what the
// catalogue shows a caller and what a call lets them do never disagree.
// Relevance is never permission: a role grants interactions, and data
// scope (R05) is a separate, typed rule over the values a request names and
// a result returns.
//
// A grant names a capability by its id or pinned id, or a family by its
// namespace ("portfolio.*", or "*" for every one). A family grant reaches
// only an interaction whose effect is read (ATLAS-ACCESS-002), of a
// capability whose system is reached as the caller: a system reached as
// the gateway's own identity cannot narrow by person, so its capabilities
// are granted by name only (06-3 rule 3, R041). What it does not know of a
// capability's reach, it takes to be the gateway's identity. A deny may
// name a family, and always wins. Anything but a listed visibility is
// confidential (R102).
package access

import (
	"context"
	"slices"
	"strings"

	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

// Subject is a caller under one policy: who they are, their roles and the
// data they may read.
type Subject struct {
	Identity ports.Identity
	Policy   *ports.Policy
	Roles    []string
	Scope    ports.Scope
	names    []string           // the principal, its groups and roles: who grants and denies can name
	catalog  *registry.Snapshot // how each capability's system is reached, or nil
}

// Resolve is the caller under policy. Without a catalogue to say how each
// capability's system is reached, a family grant reaches no capability
// but the built-ins, which are every caller's (06-3 rule 3).
func Resolve(policy *ports.Policy, id ports.Identity) *Subject { return ResolveIn(policy, id, nil) }

// ResolveIn is the caller under policy, in a catalogue that says how each
// capability's system is reached, so a family grant reaches the read
// interactions of one reached as the caller (ATLAS-ACCESS-002).
func ResolveIn(policy *ports.Policy, id ports.Identity, catalog *registry.Snapshot) *Subject {
	s := &Subject{Identity: id, Policy: policy, Scope: ports.Scope{}, catalog: catalog}
	holders := append([]string{id.Principal}, id.Groups...)
	for _, h := range holders {
		for _, role := range policy.Roles[h] {
			if !slices.Contains(s.Roles, role) {
				s.Roles = append(s.Roles, role)
			}
		}
	}
	slices.Sort(s.Roles)
	s.names = append(holders, s.Roles...)
	for _, n := range s.names {
		for dim, values := range policy.Scopes[n] {
			for _, v := range values {
				if !slices.Contains(s.Scope[dim], v) {
					s.Scope[dim] = append(s.Scope[dim], v)
				}
			}
		}
	}
	return s
}

// Caller is the subject as a provider sees it.
func (s *Subject) Caller() ports.Caller {
	return ports.Caller{Principal: s.Identity.Principal, Name: s.Identity.Name, Kind: s.Identity.Kind, Application: s.Identity.Application, Roles: s.Roles}
}

// naming is how a policy entry names a capability.
type naming int

const (
	unnamed  naming = iota
	byName          // its id, every version, or its pinned id
	byFamily        // its namespace, "portfolio.*", or every capability, "*"
)

func nameOf(entry string, p *profile.Profile) naming {
	switch {
	case entry == p.ID || entry == grammar.Pinned{ID: p.ID, Version: p.Version}.String():
		return byName
	case entry == "*":
		return byFamily
	case strings.HasSuffix(entry, ".*") && strings.HasPrefix(p.ID, strings.TrimSuffix(entry, "*")):
		return byFamily
	}
	return unnamed
}

// covers reports whether a deny's or limit's capability list names p, by
// name or by family.
func covers(list []string, p *profile.Profile) bool {
	return slices.ContainsFunc(list, func(e string) bool { return nameOf(e, p) != unnamed })
}

func names(list []string, interaction string) bool {
	return len(list) == 0 || slices.Contains(list, interaction)
}

// Decision is whether a caller may use one interaction, and if not, the
// problem code: not_found keeps a confidential capability undisclosed.
type Decision struct {
	Allowed bool
	Code    string
	Reason  string
	HeldBy  string
}

// Decide is §8 step 3 for one interaction: visibility, then denies, then
// grants. A built-in is every authenticated caller's.
func (s *Subject) Decide(p *profile.Profile, interaction string) Decision {
	if grammar.Builtin(p.ID) {
		return Decision{Allowed: true, HeldBy: "atlas"}
	}
	if p.Visibility != "listed" && !s.holdsAny(p) {
		return Decision{Code: protocol.CodeNotFound, Reason: "no such capability"}
	}
	if p.Interactions[interaction] == nil {
		return Decision{Code: protocol.CodeNotFound, Reason: "no such interaction"}
	}
	for _, d := range s.Policy.Denies {
		if (d.Subject == "*" || slices.Contains(s.names, d.Subject)) && covers(d.Capabilities, p) && names(d.Interactions, interaction) {
			return Decision{Code: protocol.CodeDenied, Reason: d.Reason}
		}
	}
	if held := s.grant(p, interaction); held != "" {
		return Decision{Allowed: true, HeldBy: held}
	}
	return Decision{Code: protocol.CodeNotGranted, Reason: "no grant covers " + p.ID + "@v" + itoa(p.Version) + ":" + interaction}
}

// grant is the id of a grant the caller holds that reaches the
// interaction, or "".
func (s *Subject) grant(p *profile.Profile, interaction string) string {
	for _, g := range s.Policy.Grants {
		if slices.Contains(s.Roles, g.Role) && names(g.Interactions, interaction) && s.reaches(g.Capabilities, p, interaction) {
			return g.ID
		}
	}
	return ""
}

// reaches reports whether a grant's capability list reaches one
// interaction of p: by name, any interaction; by family, only what 06-3
// rule 3 and ATLAS-ACCESS-002 let a family grant reach.
func (s *Subject) reaches(list []string, p *profile.Profile, interaction string) bool {
	for _, e := range list {
		switch nameOf(e, p) {
		case byName:
			return true
		case byFamily:
			if FamilyReaches(p, interaction, s.reach(p)) {
				return true
			}
		}
	}
	return false
}

// FamilyReaches reports whether a family grant ("ns.*" or "*") may reach
// one interaction of p, whose system is reached as reach: only a read
// (ATLAS-ACCESS-002), and only of a system that narrows by the caller
// (06-3 rule 3).
func FamilyReaches(p *profile.Profile, interaction string, reach registry.Reach) bool {
	in := p.Interactions[interaction]
	return in != nil && in.Effect == "read" && reach == registry.AsCaller
}

// reach is how p's system is reached, as the catalogue says; the gateway's
// own identity when it does not know.
func (s *Subject) reach(p *profile.Profile) registry.Reach {
	if s.catalog == nil {
		return registry.AsGateway
	}
	if c := s.catalog.Lookup(grammar.Pinned{ID: p.ID, Version: p.Version}.String()); c != nil {
		return c.Reach
	}
	return registry.AsGateway
}

// holdsAny reports whether a grant the caller holds reaches some
// interaction of p.
func (s *Subject) holdsAny(p *profile.Profile) bool {
	return slices.ContainsFunc(p.Order, func(name string) bool { return s.grant(p, name) != "" })
}

// Visible reports whether the caller's catalogue lists the capability: a
// listed one they hold any interaction of, or one with no grant they may
// still ask access about (§2.6), but never a confidential one they do not
// hold. Anything but listed is confidential (R102).
func (s *Subject) Visible(p *profile.Profile) bool {
	if grammar.Builtin(p.ID) || p.Visibility == "listed" {
		return true
	}
	return s.holdsAny(p)
}

// Holds reports whether the caller holds some interaction of p.
func (s *Subject) Holds(p *profile.Profile) bool {
	for _, name := range p.Order {
		if s.Decide(p, name).Allowed {
			return true
		}
	}
	return false
}

// Limit is the caller's limit for a capability: the most restrictive of
// every limit that names them.
func (s *Subject) Limit(p *profile.Profile) ports.Limit {
	var out ports.Limit
	tighter := func(a, b int) int {
		switch {
		case a == 0:
			return b
		case b == 0:
			return a
		}
		return min(a, b)
	}
	for _, l := range s.Policy.Limits {
		if (l.Subject == "*" || slices.Contains(s.names, l.Subject)) && covers([]string{l.Capability}, p) {
			out.PerMinute = tighter(out.PerMinute, l.PerMinute)
			out.Concurrency = tighter(out.Concurrency, l.Concurrency)
			out.MaxRows = tighter(out.MaxRows, l.MaxRows)
		}
	}
	return out
}

// ScopeFor is the caller's scope over the dimensions a schema uses: open
// dimensions as AllValues, and a dimension they have no scope in as none.
func (s *Subject) ScopeFor(schemas ...*schema.Schema) ports.Scope {
	out := ports.Scope{}
	for _, sc := range schemas {
		for _, dim := range dimensionsOf(sc) {
			if _, done := out[dim]; done {
				continue
			}
			if s.Scope.Open(dim) {
				out[dim] = []string{ports.AllValues}
			} else {
				out[dim] = slices.Clone(s.Scope[dim])
				if out[dim] == nil {
					out[dim] = []string{}
				}
			}
		}
	}
	return out
}

// Value is a value of a scoped dimension at a pointer of a record.
type Value struct {
	Pointer   string
	Dimension string
	Value     string
}

// Values are every scoped value in v, read through s: what a request names,
// or a row returns.
func Values(s *schema.Schema, v any) []Value {
	var out []Value
	walk(s, v, "", &out)
	return out
}

func walk(s *schema.Schema, v any, ptr string, out *[]Value) {
	if s == nil || v == nil {
		return
	}
	if name, _, _, ok := s.Logical(); ok {
		if dim, scoped := ports.DimensionOf(name); scoped {
			if str, isString := v.(string); isString {
				*out = append(*out, Value{ptr, dim, str})
			}
			return
		}
	}
	switch x := v.(type) {
	case []any:
		if it := s.Items(); it != nil {
			for i, e := range x {
				walk(it, e, ptr+"/"+itoa(i), out)
			}
		}
	case interface {
		Get(string) (any, bool)
		Names() []string
	}:
		for _, name := range s.Properties() {
			if e, ok := x.Get(name); ok {
				ps, _ := s.Property(name)
				walk(ps, e, ptr+"/"+name, out)
			}
		}
	}
}

func dimensionsOf(s *schema.Schema) []string {
	var dims []string
	var visit func(*schema.Schema, int)
	visit = func(s *schema.Schema, depth int) {
		if s == nil || depth > 32 {
			return
		}
		if name, _, _, ok := s.Logical(); ok {
			if dim, scoped := ports.DimensionOf(name); scoped && !slices.Contains(dims, dim) {
				dims = append(dims, dim)
			}
		}
		visit(s.Items(), depth+1)
		for _, n := range s.Properties() {
			ps, _ := s.Property(n)
			visit(ps, depth+1)
		}
	}
	visit(s, 0)
	return dims
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}

type subjectKey struct{}

// WithSubject is ctx carrying the caller, for the built-ins, which answer
// about the caller themself (whoami, catalog, access).
func WithSubject(ctx context.Context, s *Subject) context.Context {
	return context.WithValue(ctx, subjectKey{}, s)
}

// SubjectFrom is the caller ctx carries, or nil.
func SubjectFrom(ctx context.Context) *Subject {
	s, _ := ctx.Value(subjectKey{}).(*Subject)
	return s
}
