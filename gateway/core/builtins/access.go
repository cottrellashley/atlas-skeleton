package builtins

import (
	"slices"
	"strings"

	"atlas.local/gateway/core/registry"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/grammar"
	"atlas.local/sdk/go/protocol/ijson"
)

// access explains the caller's access to each listed capability (or one, or
// a domain's): its status from §2.6's table, the reason, how to get it, and
// how they hold it with their limits where they do (ATLAS-DISCOVERY-004).
// It is the one view that shows what the caller cannot use (§2.6 rule 3),
// and it says nothing about anyone else (rule 6). A confidential
// capability they do not hold stays undisclosed (ATLAS-DISCOVERY-005).
func (p *Provider) access(v *view, req *ijson.Object) (*ports.Answer, error) {
	id, domain := strMember(req, "id"), strMember(req, "domain")
	var caps []*registry.Capability
	var ids []string
	for _, c := range v.all() {
		if c.Builtin() || !v.s.Visible(c.Current) || c.Current.Lifecycle == "retired" {
			continue
		}
		if id != "" && id != c.Pinned.ID && id != c.Pinned.String() || domain != "" && grammar.Domain(c.Pinned.ID) != domain {
			continue
		}
		caps, ids = append(caps, c), append(ids, c.Pinned.String())
	}
	from, to, next, err := p.window(v, req, ids, 100)
	if err != nil {
		return nil, err
	}
	items := []any{}
	for _, c := range caps[from:to] {
		items = append(items, v.entry(c))
	}
	return record(page(items, next))
}

// entry is one capability's access entry for the caller. What they lack is
// of two kinds, and each is said apart (§2.6: partial gives "which, and
// why"): an interaction a deny refuses, whatever else they hold, for which
// no grant is ever offered (ATLAS-ACCESS-001); and one no grant they hold
// covers, for which the roles that would grant it are named.
func (v *view) entry(c *registry.Capability) map[string]any {
	prof := c.Current
	interactions := map[string]any{}
	var held, refused, lacking, reasons []string
	var heldBy string
	for _, name := range prof.Order {
		d := v.s.Decide(prof, name)
		switch {
		case d.Allowed:
			held = append(held, name)
			heldBy = v.heldBy(prof, d)
			interactions[name] = map[string]any{"status": "available", "held_by": heldBy}
		case d.Code == protocol.CodeDenied:
			refused = append(refused, name)
			if d.Reason != "" && !slices.Contains(reasons, d.Reason) {
				reasons = append(reasons, d.Reason)
			}
			interactions[name] = map[string]any{"status": "denied", "reason": d.Reason}
		default:
			lacking = append(lacking, name)
			interactions[name] = map[string]any{"status": "not_granted"}
		}
	}
	e := map[string]any{"id": c.Pinned.String(), "interactions": interactions}
	owner := identity(prof, "owner")
	// which names interactions, or the capability when they are all of it.
	which := func(names []string) string {
		if len(names) == len(prof.Order) {
			return "it"
		}
		return ticked(names)
	}
	// ask is how to get what no grant the caller holds covers.
	ask := func(names []string) string {
		roles := v.granting(prof, names)
		if len(roles) == 0 {
			if len(names) == len(prof.Order) {
				return "Ask " + owner + ", its owner, for a grant."
			}
			return "Ask " + owner + ", its owner, for a grant of " + which(names) + "."
		}
		return "Ask " + owner + ", its owner, for the role " + strings.Join(roles, " or the role ") + ", which grants " + which(names) + "."
	}
	deny := func() string {
		return "A deny refuses " + which(refused) + ", whatever else you hold: ask " + owner + ", its owner."
	}
	why := func() string {
		if len(reasons) == 0 {
			return "A deny refuses " + which(refused) + "."
		}
		return strings.Join(reasons, " ")
	}
	switch {
	case len(held) > 0 && prof.Lifecycle == "deprecated":
		e["status"], e["reason"], e["next"] = "deprecated", "You hold it, and it is deprecated.", "Ask "+owner+", its owner, what replaces it."
		sunset := property(prof, "sunset")
		if sunset != "" {
			e["reason"], e["next"] = "You hold it, and it is deprecated: its sunset is "+sunset+".", "Plan for its sunset on "+sunset+": ask "+owner+", its owner, what replaces it."
		}
		if to := property(prof, "replaced_by"); to != "" {
			if r := v.capability(to); r != nil && v.s.Visible(r.Current) {
				e["next"] = "Move to " + to + " before its sunset."
				if sunset != "" {
					e["next"] = "Move to " + to + " before its sunset on " + sunset + "."
				}
			}
		}
	case len(held) == len(prof.Order):
		e["status"], e["reason"], e["next"] = "available", "You hold every interaction, through "+heldBy+".", "Call it."
	case len(held) > 0:
		reason, next := "You hold "+ticked(held), []string{}
		if len(lacking) > 0 {
			reason += "; no grant you hold covers " + ticked(lacking)
			next = append(next, ask(lacking))
		}
		if len(refused) > 0 {
			reason += "; a deny refuses " + ticked(refused) + ": " + why()
			next = append(next, deny())
		}
		if !strings.HasSuffix(reason, ".") && !strings.HasSuffix(reason, "!") && !strings.HasSuffix(reason, "?") {
			reason += "."
		}
		e["status"], e["reason"], e["next"] = "partial", reason, strings.Join(next, " ")
	case len(refused) > 0:
		next := deny()
		if len(lacking) > 0 {
			next += " " + ask(lacking)
		}
		e["status"], e["reason"], e["next"] = "denied", why(), next
	default:
		e["status"], e["reason"], e["next"] = "not_granted", "No grant you hold covers it.", ask(lacking)
	}
	if len(held) > 0 {
		e["held_by"] = heldBy
		e["limits"] = v.limits(prof)
	}
	return e
}

// setup is what the caller's client still needs (§9.3.2).
func (p *Provider) setup(v *view) map[string]any {
	n := len(v.listed())
	return map[string]any{"steps": []any{
		map[string]any{"name": "sign in", "done": true, "detail": "Signed in as " + v.s.Identity.Name + " (" + v.s.Identity.Kind + ") in " + p.opt.Environment + "."},
		map[string]any{"name": "hold a capability", "done": n > 0, "command": "atlas catalog", "detail": map[bool]string{true: "You hold capabilities in your catalogue.", false: "You hold no capability yet: atlas access says who grants them."}[n > 0]},
	}}
}

// clients are the Atlas clients this gateway's callers use (§9.3.2).
func (p *Provider) clients() map[string]any {
	items := []any{}
	for _, c := range p.opt.Clients {
		items = append(items, map[string]any{"name": c.Name, "language": c.Language, "built_for": c.BuiltFor, "roles": nonNil(c.Roles), "levels": nonNil(c.Levels), "extras": nonNil(c.Extras), "version": c.Version, "install": c.Install})
	}
	return page(items, nil)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
