package builtins

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/jhgonline/fi-app-atlas/gateway/core/access"
	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

// view is the caller as one built-in call answers them: who they are under
// the policy, what their client implements, and the catalogue as it
// stands. It holds the one availability rule every built-in applies, so
// whoami, the catalogue, search, describe, the guide and the house rules
// never disagree about what the caller can use (§2.6 rule 2).
type view struct {
	s        *access.Subject
	snap     *registry.Snapshot
	features protocol.Features
	// previews is whether the caller asked for previews (atlas.catalog's
	// previews); nothing else lists them.
	previews bool
	// limited are the policy's limits, each with a subject holding only it,
	// once a call needs to say who set a limit.
	limited []limitHolder
	// grantors are the caller holding one more role each, once a call
	// needs to say which role would grant what they lack.
	grantors []grantor
	caps     []*registry.Capability
}

// all are the catalogue's capabilities, by pinned id, read once a call.
func (v *view) all() []*registry.Capability {
	if v.caps == nil {
		v.caps = v.snap.Capabilities()
	}
	return v.caps
}

// available is §2.6 rule 2: the caller holds an interaction of it and no
// deny refuses it; it is neither retired nor, unless asked for, a preview;
// and the caller's client implements every trait it marks critical.
func (v *view) available(c *registry.Capability) bool {
	p := c.Current
	// The lifecycle it is served with: an unready profile is a preview,
	// whatever it declares (R3-12).
	switch lifecycle := c.Lifecycle(); {
	case lifecycle == "retired", lifecycle == "preview" && !v.previews:
		return false
	case len(v.unsupported(p)) > 0:
		return false
	}
	return v.s.Holds(p)
}

// unsupported are the traits a profile marks critical that the caller's
// client does not implement, lowercased as Atlas-Client-Features names
// them (§9.3.2).
func (v *view) unsupported(p *profile.Profile) []string {
	var out []string
	for _, t := range p.Traits {
		if name := lowerASCII(t.Name); t.Critical && !v.features.Understands(name) {
			out = append(out, name)
		}
	}
	return out
}

// listed are the capabilities of the caller's working view, by pinned id:
// not the built-ins, which the bootstrap names (§2.6).
func (v *view) listed() []*registry.Capability {
	var out []*registry.Capability
	for _, c := range v.all() {
		if !c.Builtin() && v.available(c) {
			out = append(out, c)
		}
	}
	return out
}

// capability is what an id names: a pinned id its version; a capability id
// the highest version the caller can use, else its highest version.
func (v *view) capability(id string) *registry.Capability {
	if pinned, err := grammar.ParsePinned(id); err == nil {
		return v.snap.Lookup(pinned.String())
	}
	var best, usable *registry.Capability
	for _, c := range v.all() {
		if c.Pinned.ID != id {
			continue
		}
		if best == nil || c.Pinned.Version > best.Pinned.Version {
			best = c
		}
		if (usable == nil || c.Pinned.Version > usable.Pinned.Version) && v.available(c) {
			usable = c
		}
	}
	if usable != nil {
		return usable
	}
	return best
}

// mayName reports whether a guide may name what a dotted word in it names
// (ATLAS-DISCOVERY-010): anything that is not a capability; a built-in,
// which is every caller's; a capability the caller can use; and an address
// whose interaction they hold. A pinned id or an address is always a
// capability; a bare id is one only in a domain this gateway serves, so a
// word such as e.g or a source's name is left alone.
func (v *view) mayName(word string) bool {
	id, interaction, _ := strings.Cut(word, ":")
	bare, _, pinned := strings.Cut(id, "@")
	if grammar.Builtin(bare) {
		return true
	}
	if !pinned && !v.serves(grammar.Domain(bare)) {
		return true
	}
	c := v.capability(id)
	switch {
	case c == nil || !v.available(c):
		return false
	case interaction == "":
		return true
	}
	_, ok := c.Current.Interactions[interaction]
	return ok && v.s.Decide(c.Current, interaction).Allowed
}

// serves reports whether the catalogue has a capability in a domain.
func (v *view) serves(domain string) bool {
	return slices.ContainsFunc(v.all(), func(c *registry.Capability) bool { return grammar.Domain(c.Pinned.ID) == domain })
}

// heldBy is how the caller holds an interaction they may use, as
// governance writes it (governance.schema.json held_by): role:<name>, or
// everyone for the built-ins.
func (v *view) heldBy(p *profile.Profile, d access.Decision) string {
	if grammar.Builtin(p.ID) {
		return "everyone"
	}
	for _, g := range v.s.Policy.Grants {
		if g.ID == d.HeldBy {
			return "role:" + g.Role
		}
	}
	return d.HeldBy
}

// probe begins the name of a group no policy names: the group through
// which a grantor holds the role it is the caller with.
const probe = "\x00probe:"

// grantor is the caller as they would be holding one more role.
type grantor struct {
	role string
	s    *access.Subject
}

// granting are the roles, none of which the caller holds, that would let
// them use one of the interactions named of p: what the access view names
// for what they lack (§2.6, not_granted: "which role would grant it"). The
// access resolver itself decides each, for the caller holding that role as
// well, under the whole policy and in the call's catalogue. So a family
// grant counts where it reaches (ATLAS-ACCESS-002, 06-3 rule 3), a role
// whose holders a deny refuses grants nothing (ATLAS-ACCESS-001), and the
// rule that refuses a call is the rule that names who grants it.
func (v *view) granting(p *profile.Profile, interactions []string) []string {
	var roles []string
	for _, g := range v.holding() {
		if slices.ContainsFunc(interactions, func(name string) bool { return g.s.Decide(p, name).Allowed }) {
			roles = append(roles, g.role)
		}
	}
	return roles
}

// holding are the grantors, by role: the caller holding each role a grant
// names that they do not hold. Each holds it through a group of its own,
// in one copy of the policy's roles, so nothing else of the policy, or of
// who the caller is, changes.
func (v *view) holding() []grantor {
	if v.grantors != nil {
		return v.grantors
	}
	v.grantors = []grantor{}
	var lacking []string
	for _, g := range v.s.Policy.Grants {
		if !slices.Contains(lacking, g.Role) && !slices.Contains(v.s.Roles, g.Role) {
			lacking = append(lacking, g.Role)
		}
	}
	if len(lacking) == 0 {
		return v.grantors
	}
	slices.Sort(lacking)
	with := *v.s.Policy
	with.Roles = make(map[string][]string, len(v.s.Policy.Roles)+len(lacking))
	maps.Copy(with.Roles, v.s.Policy.Roles)
	for _, role := range lacking {
		with.Roles[probe+role] = []string{role}
	}
	for _, role := range lacking {
		id := v.s.Identity
		id.Groups = append(slices.Clip(id.Groups), probe+role)
		v.grantors = append(v.grantors, grantor{role, access.ResolveIn(&with, id, v.snap)})
	}
	return v.grantors
}

// limitHolder is the caller under one of the policy's limits alone.
type limitHolder struct {
	limit ports.Limit
	s     *access.Subject
}

// limits are the caller's limits on a capability, as governance lists them
// (governance.schema.json limit): the access resolver's own, each with who
// set it (§6.2.4: "who set each limit").
func (v *view) limits(p *profile.Profile) []any {
	if v.limited == nil {
		v.limited = []limitHolder{}
		for _, l := range v.s.Policy.Limits {
			one := *v.s.Policy
			one.Limits = []ports.Limit{l}
			v.limited = append(v.limited, limitHolder{l, access.ResolveIn(&one, v.s.Identity, v.snap)})
		}
	}
	all := v.s.Limit(p)
	setBy := func(value func(ports.Limit) int) string {
		for _, h := range v.limited {
			if value(h.s.Limit(p)) == value(all) {
				return v.subjectOf(h.limit.Subject)
			}
		}
		return ""
	}
	out := []any{}
	add := func(kind, member string, value func(ports.Limit) int) {
		if n := value(all); n > 0 {
			l := map[string]any{"kind": kind, member: n}
			if by := setBy(value); by != "" {
				l["set_by"] = by
			}
			out = append(out, l)
		}
	}
	add("rate", "per_minute", func(l ports.Limit) int { return l.PerMinute })
	add("concurrency", "max", func(l ports.Limit) int { return l.Concurrency })
	add("output", "max_rows", func(l ports.Limit) int { return l.MaxRows })
	return out
}

// subjectOf is a limit's subject as the caller is told of it: their own
// principal, one of their roles or groups, or everyone.
func (v *view) subjectOf(subject string) string {
	switch {
	case subject == "*":
		return "everyone"
	case subject == v.s.Identity.Principal:
		return "user:" + subject
	case slices.Contains(v.s.Roles, subject):
		return "role:" + subject
	case slices.Contains(v.s.Identity.Groups, subject):
		return "group:" + subject
	}
	return subject
}

// limitSentences are the caller's limits on a capability, as a guide says
// them (§2.6, section 8).
func (v *view) limitSentences(p *profile.Profile) []string {
	var out []string
	for _, l := range v.limits(p) {
		m := l.(map[string]any)
		switch m["kind"] {
		case "rate":
			out = append(out, "At most "+strconv.Itoa(m["per_minute"].(int))+" calls a minute.")
		case "concurrency":
			out = append(out, "At most "+strconv.Itoa(m["max"].(int))+" calls at once.")
		case "output":
			out = append(out, "At most "+strconv.Itoa(m["max_rows"].(int))+" rows an answer: a longer one is cut there, and says so.")
		}
	}
	return out
}

// houseRules are the rules whose capabilities the caller can all use
// (§2.6 rule 5, §9.3.2); with onPage, only those naming a capability it
// reports on the page.
func (v *view) houseRules(onPage func(id string) bool) map[string]any {
	rules := []any{}
	for _, r := range v.s.Policy.HouseRules {
		if !v.reaches(r) || onPage != nil && !slices.ContainsFunc(r.Capabilities, func(id string) bool { return onPage(bareID(id)) }) {
			continue
		}
		caps := make([]string, len(r.Capabilities))
		for i, id := range r.Capabilities {
			caps[i] = bareID(id)
		}
		rules = append(rules, map[string]any{"text": r.Text, "capabilities": caps})
	}
	return map[string]any{"revision": revisionOr(v.s.Policy.HouseRulesRevision), "rules": rules}
}

// reaches reports whether a house rule reaches the caller: they can use
// every capability it names, in its list and in its words, so it reveals
// none they cannot (§2.6).
func (v *view) reaches(r ports.HouseRule) bool {
	if len(r.Capabilities) == 0 {
		return false
	}
	for _, id := range r.Capabilities {
		if c := v.capability(id); c == nil || !c.Builtin() && !v.available(c) {
			return false
		}
	}
	for _, m := range mention.FindAllString(r.Text, -1) {
		if !v.mayName(m) {
			return false
		}
	}
	return true
}

// rulesNaming are the texts of the house rules that reach the caller and
// name a capability (§2.6, section 2 of a guide).
func (v *view) rulesNaming(id string) []string {
	var out []string
	for _, r := range v.s.Policy.HouseRules {
		if v.reaches(r) && slices.ContainsFunc(r.Capabilities, func(c string) bool { return bareID(c) == id }) {
			out = append(out, r.Text)
		}
	}
	return out
}

// revisionOr is a revision as an answer writes it: none when there is
// none, since a revision is never empty (catalog-page.schema.json).
func revisionOr(revision string) string {
	if revision == "" {
		return "none"
	}
	return revision
}

func bareID(id string) string {
	bare, _, _ := strings.Cut(id, "@")
	return bare
}

// lowerASCII lowers A to Z only, as trait tokens are compared (A-008).
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}
