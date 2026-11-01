package access

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

// Catalogue is what a policy is checked against before it may decide
// (06-7 §6.7.10): the capabilities the gateway serves, and the pinned ids
// the deployment knows but withholds for now. A policy may name a
// withheld capability already: a capability reached as the gateway's own
// identity is granted by name (06-3 rule 3), so its grant is in place the
// moment it is added (A-180).
type Catalogue struct {
	Served   *registry.Snapshot
	Withheld []string
}

// Check holds a policy to the catalogue it will decide in (06-7 §6.7.10,
// R040). Every grant and deny names a capability the gateway serves or
// withholds, by id or pinned id, or a namespace that has one, and every
// interaction it names is one a named capability has; a family grant
// reaches something 06-3 rule 3 lets it; no grant or deny names a
// built-in, which is every authenticated caller's; every limit names what
// exists; every house rule's capabilities exist. It is every reason the
// policy may not decide, none when it may.
func Check(p *ports.Policy, cat Catalogue) []string {
	var out []string
	bad := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }
	var served []*registry.Capability
	if cat.Served != nil {
		served = cat.Served.Capabilities()
	}
	for i, g := range p.Grants {
		at := fmt.Sprintf("grants[%d] (%s)", i, g.ID)
		matched := entries(at, g.Capabilities, g.Interactions, served, cat.Withheld, true, bad)
		for _, e := range g.Capabilities {
			if e != "*" && !strings.HasSuffix(e, ".*") {
				continue
			}
			if !slices.ContainsFunc(matched[e], func(c *registry.Capability) bool {
				return slices.ContainsFunc(c.Current.Order, func(n string) bool {
					return names(g.Interactions, n) && FamilyReaches(c.Current, n, c.Reach)
				})
			}) && len(matched[e]) > 0 {
				bad("%s: %q reaches none of the %d capabilities it names: a family grant reaches only a read of a capability whose system is reached as the caller (ATLAS-ACCESS-002), and each of these is reached as the gateway's own identity, or has no read, so is granted by name only (06-3 rule 3)", at, e, len(matched[e]))
			}
		}
	}
	for i, d := range p.Denies {
		entries(fmt.Sprintf("denies[%d] (%s)", i, d.ID), d.Capabilities, d.Interactions, served, cat.Withheld, true, bad)
	}
	for i, l := range p.Limits {
		entries(fmt.Sprintf("limits[%d]", i), []string{l.Capability}, nil, served, cat.Withheld, false, bad)
	}
	for i, r := range p.HouseRules {
		entries(fmt.Sprintf("house_rules[%d]", i), r.Capabilities, nil, served, cat.Withheld, false, bad)
	}
	return out
}

// entries checks one list of capability entries against the catalogue,
// and the interactions the list's owner names; it is what each entry
// matched among the capabilities served. Grants and denies never name a
// built-in.
func entries(at string, list, interactions []string, served []*registry.Capability, withheld []string, decides bool, bad func(string, ...any)) map[string][]*registry.Capability {
	matched := map[string][]*registry.Capability{}
	for _, e := range list {
		if decides && grammar.Builtin(strings.TrimSuffix(e, ".*")) {
			bad("%s: %q names a built-in: the atlas.* capabilities are every authenticated caller's, and a policy neither grants nor denies them", at, e)
			continue
		}
		for _, c := range served {
			if nameOf(e, c.Current) != unnamed && !(decides && c.Builtin()) {
				matched[e] = append(matched[e], c)
			}
		}
		if len(matched[e]) == 0 && !slices.ContainsFunc(withheld, func(w string) bool { return withheldNamed(e, w) }) {
			bad("%s: %q names no capability this gateway serves", at, e)
			continue
		}
		for _, n := range interactions {
			if len(matched[e]) > 0 && !slices.ContainsFunc(matched[e], func(c *registry.Capability) bool { return c.Current.Interactions[n] != nil }) {
				bad("%s: %q is not an interaction of %s", at, n, pinnedList(matched[e]))
			}
		}
	}
	return matched
}

// withheldNamed reports whether an entry names a withheld pinned id.
func withheldNamed(entry, pinned string) bool {
	p, err := grammar.ParsePinned(pinned)
	if err != nil {
		return false
	}
	return nameOf(entry, &profile.Profile{ID: p.ID, Version: p.Version}) != unnamed
}

func pinnedList(cs []*registry.Capability) string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Pinned.String()
	}
	return strings.Join(out, ", ")
}

// Refused is a policy revision that may not decide, with every reason.
type Refused struct {
	Revision string
	Reasons  []string
}

func (e *Refused) Error() string {
	return fmt.Sprintf("access: the policy %q is refused: %s", e.Revision, strings.Join(e.Reasons, "; "))
}

// Checked is src with every policy it answers held to the catalogue
// (Check) before the gateway may decide by it (06-7 §6.7.10). A policy that
// fails is answered as an error: the gateway keeps deciding by the last
// that passed only for its bound (R13), and the error says what to fix. A
// policy whose content changes under a revision it already used is
// refused too, since the audit would name two policies alike. A change of
// state is logged once, to log when it is not nil.
func Checked(src ports.PolicySource, catalogue func() Catalogue, log *slog.Logger) ports.PolicySource {
	if src == nil {
		return nil
	}
	return &checked{src: src, catalogue: catalogue, log: log}
}

type checked struct {
	src       ports.PolicySource
	catalogue func() Catalogue
	log       *slog.Logger

	mu       sync.Mutex
	revision string            // the last revision that passed
	digest   [sha256.Size]byte // its content
	last     string            // the last outcome logged
}

func (c *checked) Policy(ctx context.Context) (*ports.Policy, error) {
	p, err := c.src.Policy(ctx)
	if err == nil && p == nil {
		err = errors.New("access: the policy source answered no policy")
	}
	if err != nil {
		c.report(ctx, "", err)
		return nil, err
	}
	reasons := Check(p, c.catalogue())
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("access: the policy %q: %w", p.Revision, err)
	}
	digest := sha256.Sum256(b)
	c.mu.Lock()
	if c.revision == p.Revision && c.digest != digest {
		reasons = append(reasons, fmt.Sprintf("the policy %q changed, but kept its revision: give every change a new revision, so the audit names one policy by it", p.Revision))
	}
	if len(reasons) == 0 {
		c.revision, c.digest = p.Revision, digest
	}
	c.mu.Unlock()
	if len(reasons) > 0 {
		err := &Refused{Revision: p.Revision, Reasons: reasons}
		c.report(ctx, p.Revision, err)
		return nil, err
	}
	c.report(ctx, p.Revision, nil)
	return p, nil
}

// report logs an outcome once, when it differs from the last.
func (c *checked) report(ctx context.Context, revision string, err error) {
	if c.log == nil {
		return
	}
	outcome := "deciding by " + revision
	if err != nil {
		outcome = err.Error()
	}
	c.mu.Lock()
	changed := outcome != c.last
	c.last = outcome
	c.mu.Unlock()
	switch {
	case !changed:
	case err != nil:
		c.log.WarnContext(ctx, "policy: refused; the last policy that passed keeps deciding until its bound", "revision", revision, "error", err)
	default:
		c.log.InfoContext(ctx, "policy: deciding", "revision", revision)
	}
}
