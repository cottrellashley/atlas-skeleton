package builtins

import (
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

// whoami is the caller's effective context (ATLAS-CORE §2, Orient): who
// they are, their roles, the environment, the data they may read, the
// domains with something available to them and the house rules that reach
// them. Its members beyond §9.3.2's are additive: whoami_result is open.
func (p *Provider) whoami(v *view) map[string]any {
	domains := []string{}
	for _, c := range v.listed() {
		if d := grammar.Domain(c.Pinned.ID); !slices.Contains(domains, d) {
			domains = append(domains, d)
		}
	}
	slices.Sort(domains)
	roles := v.s.Roles
	if roles == nil {
		roles = []string{}
	}
	out := map[string]any{
		"principal": v.s.Identity.Principal, "name": v.s.Identity.Name, "kind": v.s.Identity.Kind, "roles": roles,
		"environment": p.opt.Environment, "available_domains": domains, "house_rules": v.houseRules(nil),
		"scope": scopeOf(v.s.Scope),
	}
	if t := v.s.Identity.Tenant; t != "" {
		out["tenant"] = t
	}
	if a := v.s.Identity.Application; a != "" {
		out["application"] = a
	}
	return out
}

// dimensions are the data dimensions scope is kept in, sorted: one for each
// of atlas/1.0's code types (§9.2.3), as the ports name them. The protocol
// fixes the code types, so the list changes only with it; a test holds it
// to the protocol's own list of logical types.
func dimensions() []string {
	var out []string
	for _, logical := range []string{"IndexCode", "PortfolioCode"} {
		if d, ok := ports.DimensionOf(logical); ok && !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	slices.Sort(out)
	return out
}

// scopeOf is the caller's data scope (R05) as whoami says it: every
// dimension scope is kept in, each with its values sorted, ["*"] when it is
// open whole, and none when the caller may read nothing in it, as the
// gateway enforces it (ScopeFor: "a dimension they have no scope in as
// none"). So no dimension reads as unrestricted by being left out, and one
// a policy names that is no dimension, which nothing enforces, is not shown.
func scopeOf(s ports.Scope) map[string]any {
	out := map[string]any{}
	for _, dim := range dimensions() {
		if s.Open(dim) {
			out[dim] = []string{ports.AllValues}
			continue
		}
		vals := slices.Clone(s[dim])
		slices.Sort(vals)
		out[dim] = nonNil(slices.Compact(vals))
	}
	return out
}

// item is a capability as a catalogue page shows it: the interactions the
// caller holds, with their effects (§2.6 rule 1).
func (v *view) item(c *registry.Capability) map[string]any {
	p := c.Current
	interactions := map[string]any{}
	for _, name := range p.Order {
		if v.s.Decide(p, name).Allowed {
			interactions[name] = map[string]any{"effect": p.Interactions[name].Effect}
		}
	}
	out := map[string]any{
		"id": p.ID, "version": p.Version, "exact": p.Exact().String(), "summary": identity(p, "summary"),
		"family": p.Family, "lifecycle": p.Lifecycle, "interactions": interactions,
	}
	if t := identity(p, "title"); t != "" {
		out["title"] = t
	}
	return out
}

// identity is a member of a profile's identity.
func identity(p *profile.Profile, member string) string {
	id, _ := p.Doc.Get("identity")
	o, _ := id.(*ijson.Object)
	if o == nil {
		return ""
	}
	v, _ := o.Get(member)
	s, _ := v.(string)
	return s
}

func (p *Provider) catalog(v *view, req *ijson.Object) (*ports.Answer, error) {
	domain, family, effect := strMember(req, "domain"), strMember(req, "family"), strMember(req, "effect")
	v.previews = boolMember(req, "previews")
	var caps []*registry.Capability
	for _, c := range v.listed() {
		if domain != "" && grammar.Domain(c.Pinned.ID) != domain || family != "" && c.Current.Family != family {
			continue
		}
		if effect != "" && !slices.ContainsFunc(c.Current.Order, func(n string) bool {
			return c.Current.Interactions[n].Effect == effect && v.s.Decide(c.Current, n).Allowed
		}) {
			continue
		}
		caps = append(caps, c)
	}
	return p.pageOf(v, req, caps, nil)
}

// pageOf is one page of capabilities, with the house rules naming them.
func (p *Provider) pageOf(v *view, req *ijson.Object, caps []*registry.Capability, extra func(i int, it map[string]any)) (*ports.Answer, error) {
	ids := make([]string, len(caps))
	for i, c := range caps {
		ids[i] = c.Pinned.String()
	}
	from, to, next, err := p.window(v, req, ids, intMember(req, "page_size", 100))
	if err != nil {
		return nil, err
	}
	items := []any{}
	var onPage []string
	for i := from; i < to; i++ {
		it := v.item(caps[i])
		if extra != nil {
			extra(i, it)
		}
		items = append(items, it)
		onPage = append(onPage, caps[i].Pinned.ID)
	}
	out := page(items, next)
	out["house_rules"] = v.houseRules(func(id string) bool { return slices.Contains(onPage, id) })
	return record(out)
}

// search ranks the caller's working view by the words asked for (§2.6
// Find: "short, ranked"). Each content word of the request, every word but
// a stopword or a single letter, matches a whole word of a capability's
// id, title, summary or when-to-use, or the start of one; the id weighs
// most. A capability's mention of another (portfolio.list@v1) is not its
// own word. Relevance orders the caller's view; it never widens it, and a
// preview is never in it (ATLAS-CORE §3).
func (p *Provider) search(v *view, req *ijson.Object) (*ports.Answer, error) {
	words := contentWords(strMember(req, "words"))
	type hit struct {
		c        *registry.Capability
		score    float64
		coverage float64 // the share of its id's words matched: the more specific id first
		why      string
	}
	var hits []hit
	for _, c := range v.listed() {
		fields := []struct {
			name   string
			weight float64
			words  []string
		}{
			{"id", 3, stems(c.Pinned.ID)},
			{"title", 2, stems(mention.ReplaceAllString(identity(c.Current, "title"), " "))},
			{"summary", 1, stems(mention.ReplaceAllString(identity(c.Current, "summary"), " "))},
			{"when", 1, stems(mention.ReplaceAllString(usageText(c.Current, "when"), " "))},
		}
		var score float64
		var why []string
		idMatched := map[string]bool{}
		for _, w := range words {
			q := stem(w)
			for _, f := range fields {
				if i := slices.IndexFunc(f.words, func(fw string) bool { return matches(q, fw) }); i >= 0 {
					score += f.weight
					why = append(why, f.name+" has "+w)
					if f.name == "id" {
						idMatched[f.words[i]] = true
					}
					break
				}
			}
		}
		if score > 0 {
			hits = append(hits, hit{c, score / (3 * float64(len(words))), float64(len(idMatched)) / float64(len(fields[0].words)), strings.Join(why, "; ")})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		switch {
		case a.score != b.score:
			return a.score > b.score
		case a.coverage != b.coverage:
			return a.coverage > b.coverage
		}
		return a.c.Pinned.String() < b.c.Pinned.String()
	})
	caps := make([]*registry.Capability, len(hits))
	for i, h := range hits {
		caps[i] = h.c
	}
	return p.pageOf(v, req, caps, func(i int, it map[string]any) {
		n, _ := ijson.ECMAScriptNumber(hits[i].score)
		it["score"] = ijson.Number(n)
		it["why"] = hits[i].why
	})
}

func usageText(p *profile.Profile, member string) string {
	if p.Usage == nil {
		return ""
	}
	v, _ := p.Usage.Get(member)
	s, _ := v.(string)
	return s
}

// words are a text's words, lowercased: runs of letters and digits, with
// the marks that belong to them (an accent written apart from its letter).
// Anything else parts them, in any script: a space of any width, an
// apostrophe or a dash, so "fund’s" is fund and s.
func words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r)
	})
}

// maxQueryWords bounds a query's content words: the request's 200
// characters hold no more (builtins.schema.json search_request).
const maxQueryWords = 100

// contentWords are a query's words that can match: not a stopword, not a
// single letter, each once.
func contentWords(text string) []string {
	var out []string
	for _, w := range words(text) {
		if len(out) == maxQueryWords {
			break
		}
		if utf8.RuneCountInString(w) > 1 && !isStopword(w) && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

func stems(text string) []string {
	ws := words(text)
	for i, w := range ws {
		ws[i] = stem(w)
	}
	return ws
}

// stem folds a plural into its singular, the same on both sides of a
// match: holdings and holding, dates and date.
func stem(w string) string {
	if len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") {
		return w[:len(w)-1]
	}
	return w
}

// matches reports whether a query word's stem matches a field's word: the
// same word, or, for three letters or more, the start of it.
func matches(q, w string) bool {
	return q == w || len(q) >= 3 && strings.HasPrefix(w, q)
}

// isStopword reports whether a word is one a query carries that says
// nothing of a capability.
func isStopword(w string) bool {
	switch w {
	case "about", "after", "all", "am", "an", "and", "any", "are", "as", "at", "be", "been", "before", "but", "by",
		"can", "could", "did", "do", "does", "each", "for", "from", "get", "give", "had", "has", "have", "how",
		"i", "if", "in", "into", "is", "it", "its", "me", "my", "no", "not", "of", "on", "or", "our", "please",
		"show", "so", "some", "than", "that", "the", "their", "them", "then", "there", "these", "this", "those",
		"to", "was", "we", "were", "what", "when", "where", "which", "who", "why", "will", "with", "would", "you", "your":
		return true
	}
	return false
}
