// Package builtins answers the atlas.* capabilities from the snapshot alone
// (§2.6): who the caller is, the working view (catalogue and search) and
// describe. The working view shows only what the caller can use, here and
// now; nothing in it says that anything else exists.
package builtins

import (
	"sort"
	"strings"
	"unicode"

	"github.com/jhgonline/fi-app-atlas/gateway/core/catalog"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// WhoAmI answers atlas.whoami.
func WhoAmI(s *catalog.Snapshot, p catalog.Principal, env string) protocol.WhoAmI {
	out := protocol.WhoAmI{Principal: p.Subject, Name: p.Name, Environment: env, Roles: s.Governance.RoleNames(p), Available: map[string]int{}}
	for _, e := range Catalog(s, p).Items {
		out.Available[protocol.Domain(e.ID)]++
	}
	return out
}

// Catalog answers atlas.catalog: every current capability the caller holds
// at least one interaction of, with only the interactions they hold.
func Catalog(s *catalog.Snapshot, p catalog.Principal) protocol.CatalogPage {
	page := protocol.CatalogPage{Items: []protocol.CatalogEntry{}}
	for _, e := range s.CurrentEntries() {
		held := Held(s, p, e)
		if len(held) == 0 || e.Profile.Lifecycle == "retired" {
			continue
		}
		page.Items = append(page.Items, protocol.CatalogEntry{
			ID: e.Profile.ID, Version: e.Profile.Version, Exact: e.Exact, Family: e.Profile.Family,
			Title: e.Profile.Title, Summary: e.Profile.Summary, Interactions: held,
		})
	}
	return page
}

// Held lists the interactions of a profile the principal may use.
func Held(s *catalog.Snapshot, p catalog.Principal, e *catalog.Entry) []string {
	var held []string
	for name, i := range e.Profile.Interactions {
		addr := protocol.Address{Pinned: e.Profile.Pinned(), Interaction: name}
		if s.Governance.Decide(p, addr, i.Effect).Code == "" {
			held = append(held, name)
		}
	}
	sort.Strings(held)
	return held
}

// Search answers atlas.search: the working view ranked against words, by
// where each word appears: the id, the title, the summary, a column.
func Search(s *catalog.Snapshot, p catalog.Principal, words string) protocol.CatalogPage {
	terms := tokens(words)
	page := protocol.CatalogPage{Items: []protocol.CatalogEntry{}}
	for _, item := range Catalog(s, p).Items {
		e, _ := s.Exact(item.Exact)
		var score float64
		for _, t := range terms {
			score += 3 * count(tokens(e.Profile.ID), t)
			score += 2 * count(tokens(e.Profile.Title), t)
			score += count(tokens(e.Profile.Summary), t)
			if e.Profile.Result != nil {
				for _, c := range e.Profile.Result.Columns {
					score += 0.5 * count(tokens(c.Name+" "+c.Description), t)
				}
			}
		}
		if score > 0 {
			item.Score = score
			page.Items = append(page.Items, item)
		}
	}
	sort.SliceStable(page.Items, func(i, j int) bool { return page.Items[i].Score > page.Items[j].Score })
	return page
}

// Describe answers atlas.describe, by id@vN for the current profile or by an
// exact id for any profile a reader may hold in its lock. A capability the
// caller holds nothing of is not described: it is listed in the access view
// only (§2.6, rule 4).
func Describe(s *catalog.Snapshot, p catalog.Principal, req protocol.DescribeRequest) (protocol.DescribeAnswer, error) {
	var e *catalog.Entry
	switch {
	case req.Exact != "":
		if _, err := protocol.ParseExact(req.Exact); err != nil {
			return protocol.DescribeAnswer{}, protocol.NewProblem(protocol.CodeInvalidRequest, err.Error()).WithTarget("/exact")
		}
		found, ok := s.Exact(req.Exact)
		if !ok {
			return protocol.DescribeAnswer{}, protocol.NewProblem(protocol.CodeNotFound, "no profile "+req.Exact)
		}
		e = found
	case req.ID != "":
		pinned, err := pinnedOrCurrent(s, req.ID)
		if err != nil {
			return protocol.DescribeAnswer{}, err
		}
		found, ok := s.Current(pinned)
		if !ok {
			return protocol.DescribeAnswer{}, protocol.NewProblem(protocol.CodeNotFound, "no capability "+req.ID)
		}
		e = found
	default:
		return protocol.DescribeAnswer{}, protocol.NewProblem(protocol.CodeInvalidRequest, "name a capability by id or exact").WithTarget("/id")
	}
	cur, _ := s.Current(e.Profile.Pinned())
	held := Held(s, p, cur)
	if len(held) == 0 {
		return protocol.DescribeAnswer{}, protocol.NewProblem(protocol.CodeNotGranted, "you hold no interaction of "+e.Profile.Pinned().String()).
			WithNext("call atlas.access for " + e.Profile.Pinned().String() + " to see who can grant it")
	}
	d := s.Governance.Decide(p, protocol.Address{Pinned: e.Profile.Pinned(), Interaction: held[0]}, e.Profile.Interactions[held[0]].Effect)
	return protocol.DescribeAnswer{
		Exact: e.Exact, Current: cur.Exact, Profile: *e.Profile,
		Governance:   protocol.Governance{Revision: s.Revision, Interactions: held, HeldBy: d.HeldBy},
		Availability: "available",
	}, nil
}

// pinnedOrCurrent reads id@vN, or a bare id as its highest current version,
// which a person at a terminal may use; a program pins (§3.3.10).
func pinnedOrCurrent(s *catalog.Snapshot, id string) (protocol.Pinned, error) {
	if strings.Contains(id, "@v") {
		p, err := protocol.ParsePinned(id)
		if err != nil {
			return p, protocol.NewProblem(protocol.CodeInvalidRequest, err.Error()).WithTarget("/id")
		}
		return p, nil
	}
	best := protocol.Pinned{}
	for _, e := range s.CurrentEntries() {
		if e.Profile.ID == id && e.Profile.Version > best.Version {
			best = e.Profile.Pinned()
		}
	}
	if best.Version == 0 {
		return best, protocol.NewProblem(protocol.CodeNotFound, "no capability "+id)
	}
	return best, nil
}

func tokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func count(list []string, t string) float64 {
	n := 0.0
	for _, w := range list {
		if w == t || (len(t) > 3 && strings.HasPrefix(w, t)) {
			n++
		}
	}
	return n
}
