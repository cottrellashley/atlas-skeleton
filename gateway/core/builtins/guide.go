package builtins

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

// guideReader is who a guide is written for (§2.6): what they hold of the
// capability, what else it may name to them, the house rules that reach
// them and their limits. Everything a guide says comes from the profile
// and the reader: the mechanical tables are generated, the prose is the
// owner's. view.reader is the one that writes a caller's.
//
// It fails closed: a reader left unset holds nothing and may be told of
// nothing but the built-ins, so code that writes a guide can never name a
// capability to someone it has not been told may use it.
type guideReader struct {
	// Holds reports whether the reader holds an interaction. Nil holds none.
	Holds func(interaction string) bool
	// MayName reports whether the guide may name what a dotted word names:
	// false for a capability the reader cannot use, which no guide names
	// (ATLAS-DISCOVERY-010). Nil names nothing but the built-ins.
	MayName func(word string) bool
	// HouseRules are the rules that name the capability and reach the reader.
	HouseRules []string
	// Limits are the reader's limits on it, each a sentence.
	Limits []string
	// ReferredBy are the capabilities the reader may use that refer to it.
	ReferredBy []string
}

// tokenCount estimates a text's tokens: four bytes each.
func tokenCount(s string) int { return (len(s) + 3) / 4 }

// minTokens and maxTokens bound a guide's max_tokens (builtins.schema.json).
const (
	minTokens = 200
	maxTokens = 20000
)

const shortened = "*Shortened to fit max_tokens: ask atlas.guide with a larger max_tokens for the whole guide.*\n"

// fitGuide joins a guide's sections within budget tokens (0 is no budget),
// by shortening sections from the last, saying so, and never dropping
// sections 1 to 4 (§2.6). Each section is its forms, fullest first; the
// last form of sections 5 to 9 is empty. truncated reports whether any
// section was shortened.
func fitGuide(sections [][]string, budget int) (md string, truncated bool) {
	choice := make([]int, len(sections))
	body := func() string {
		var b strings.Builder
		for i, forms := range sections {
			if len(forms) > 0 {
				b.WriteString(forms[choice[i]])
			}
		}
		return b.String()
	}
	md = body()
	if budget <= 0 {
		return md, false
	}
	budget = min(max(budget, minTokens), maxTokens)
	for tokenCount(md) > budget {
		i := len(sections) - 1
		for i >= 0 && choice[i] >= len(sections[i])-1 {
			i--
		}
		if i < 0 {
			break
		}
		choice[i]++
		md, truncated = body()+shortened, true
	}
	if tokenCount(md) > budget {
		// Even the shortest forms are too long: keep the whole lines that
		// fit, and say so.
		cut := body()
		cut = cut[:min(len(cut), budget*4-len(shortened))]
		for !utf8.ValidString(cut) {
			cut = cut[:len(cut)-1]
		}
		if i := strings.LastIndexByte(cut, '\n'); i > 0 {
			cut = cut[:i+1]
		}
		md, truncated = cut+shortened, true
	}
	return md, truncated
}

// sectionsOf are a profile's guide for a reader, in §2.6's nine sections,
// in their order, each with its forms, fullest first. Its examples are in
// the client's syntax: cli, python, node, go or mcp.
func sectionsOf(p *profile.Profile, client string, r guideReader) [][]string {
	g := &guide{p: p, client: client, r: r, pinned: grammar.Pinned{ID: p.ID, Version: p.Version}.String()}
	for _, name := range p.Order {
		if r.Holds != nil && r.Holds(name) {
			g.held = append(g.held, name)
		}
	}
	return [][]string{g.whatItIs(), g.whenToUse(), g.whatYouMayDo(), g.parameters(), g.whatComesBack(), g.examples(), g.problems(), g.limits(), g.related()}
}

type guide struct {
	p      *profile.Profile
	client string
	r      guideReader
	pinned string
	held   []string
}

// 1. What it is: the summary, the family, its stability and lifecycle.
func (g *guide) whatItIs() []string {
	title := oneLine(identity(g.p, "title"))
	if title == "" || !g.mayNameAll(title) {
		title = g.pinned
	}
	head := fmt.Sprintf("# %s: `%s`\n\n", title, g.pinned)
	summary := ""
	if s := g.prose(identity(g.p, "summary")); s != "" {
		summary = s + "\n\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Owner: %s. A %s capability, %s and %s.", identity(g.p, "owner"), g.p.Family, g.p.Lifecycle, g.p.Stability)
	if g.p.Lifecycle == "deprecated" {
		b.WriteString(" **Deprecated**")
		if sunset := property(g.p, "sunset"); sunset != "" {
			b.WriteString(": its sunset is " + sunset)
		}
		if to := property(g.p, "replaced_by"); to != "" && g.mayName(to) {
			b.WriteString(", and `" + to + "` replaces it")
		}
		b.WriteString(".")
	}
	return []string{head + summary + b.String() + "\n\n", head + summary, fmt.Sprintf("# `%s`\n\n", g.pinned)}
}

// 2. When to use it, and when not, with the house rules that name it.
func (g *guide) whenToUse() []string {
	const head = "## When to use it\n\n"
	when, whenNot, skill := g.prose(usageText(g.p, "when")), g.prose(usageText(g.p, "when_not")), g.prose(usageText(g.p, "skill"))
	var body strings.Builder
	if when != "" {
		body.WriteString(when + "\n\n")
	}
	if whenNot != "" {
		body.WriteString("When not to: " + whenNot + "\n\n")
	}
	if skill != "" {
		body.WriteString(skill + "\n\n")
	}
	if body.Len() == 0 {
		body.WriteString("Its owner has not said.\n\n")
	}
	rules := ""
	if len(g.r.HouseRules) > 0 {
		rules = "House rules:\n\n"
		for _, t := range g.r.HouseRules {
			rules += "- " + t + "\n"
		}
		rules += "\n"
	}
	first := ""
	if c := clauses(when); len(c) > 0 {
		first = strings.TrimRight(c[0], ":;") + "\n\n"
	}
	return []string{head + body.String() + rules, head + first + rules, head + first}
}

// effects say what each effect class means for asking the person first
// (§2.1.D).
var effects = map[string]string{
	"read":         "Run it; a metered call whose estimate passes your threshold asks the person first.",
	"write":        "Confirm with the person first.",
	"external":     "Plan it, then confirm with the person.",
	"irreversible": "Plan it, then get approval from someone other than the person asking.",
}

// 3. What you may do: each interaction the reader holds, with its effect
// class and what that means for asking the person first.
func (g *guide) whatYouMayDo() []string {
	const head = "## What you may do\n\n"
	if len(g.held) == 0 {
		s := head + "You hold none of its interactions.\n\n"
		return []string{s}
	}
	var full, list, names strings.Builder
	full.WriteString(head + "| Interaction | Effect | Before you call it | Result | Delivery |\n|---|---|---|---|---|\n")
	list.WriteString(head)
	names.WriteString(head)
	for i, name := range g.held {
		in := g.p.Interactions[name]
		meaning, ok := effects[in.Effect]
		if !ok {
			meaning = "Ask the person first."
		}
		fmt.Fprintf(&full, "| `%s` | %s | %s | %s | %s |\n", name, in.Effect, meaning, in.Result, in.Delivery)
		fmt.Fprintf(&list, "- `%s` (%s): %s\n", name, in.Effect, meaning)
		if i > 0 {
			names.WriteString(", ")
		}
		fmt.Fprintf(&names, "`%s` (%s)", name, in.Effect)
	}
	return []string{full.String() + "\n", list.String() + "\n", names.String() + "\n\n"}
}

// groupBy groups the interactions the reader holds by the schema each
// takes or returns, in their order: interactions sharing one are shown once.
func (g *guide) groupBy(schemaOf func(*profile.Interaction) string) (names []string, groups [][]string) {
	for _, name := range g.held {
		s := schemaOf(g.p.Interactions[name])
		if i := slices.Index(names, s); i >= 0 {
			groups[i] = append(groups[i], name)
			continue
		}
		names, groups = append(names, s), append(groups, []string{name})
	}
	return names, groups
}

func ticked(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "`" + n + "`"
	}
	return strings.Join(out, " and ")
}

// 4. Parameters: each with its type, whether required, its default, its
// meaning and unit, its allowed values or the capability that lists them.
func (g *guide) parameters() []string {
	const head = "## Parameters\n\n"
	var full, compact, names strings.Builder
	full.WriteString(head)
	compact.WriteString(head)
	names.WriteString(head)
	schemas, groups := g.groupBy(func(in *profile.Interaction) string { return in.Takes })
	for i, takes := range schemas {
		var shapes [][]param
		if s := g.p.Schemas[takes]; takes != "" && s != nil {
			shapes = g.params(s)
		}
		if len(shapes) == 0 {
			line := ticked(groups[i]) + " takes no parameters: send `{}`.\n\n"
			full.WriteString(line)
			compact.WriteString(line)
			names.WriteString(line)
			continue
		}
		fmt.Fprintf(&full, "### %s\n\n", ticked(groups[i]))
		fmt.Fprintf(&compact, "%s:\n\n", ticked(groups[i]))
		if len(shapes) > 1 {
			fmt.Fprintf(&full, "A request is one of %d shapes.\n\n", len(shapes))
		}
		var all []string
		for j, shape := range shapes {
			if len(shapes) > 1 {
				fmt.Fprintf(&full, "Shape %d:\n\n", j+1)
			}
			full.WriteString("| Field | Type | Required | Default | Meaning |\n|---|---|---|---|---|\n")
			var line []string
			for _, f := range shape {
				req, required := "no", ""
				if f.required {
					req, required = "yes", ", required"
				}
				fmt.Fprintf(&full, "| %s | %s | %s | %s | %s |\n", escapePipes(codeSpan(f.name)), cell(f.typ), req, escapePipes(f.def), cell(f.meaning))
				line = append(line, codeSpan(f.name)+" ("+f.typ+required+")")
				if n := codeSpan(f.name); !slices.Contains(all, n) {
					all = append(all, n)
				}
			}
			full.WriteString("\n")
			compact.WriteString("- " + strings.Join(line, ", ") + "\n")
		}
		compact.WriteString("\n")
		fmt.Fprintf(&names, "%s: %s.\n\n", ticked(groups[i]), strings.Join(all, ", "))
	}
	return []string{full.String(), compact.String(), names.String()}
}

// param is one request member as a guide shows it.
type param struct {
	name, typ, def, meaning string
	required                bool
}

// params are a request schema's members, as one shape, or one shape for
// each alternative of a request that is a union (the built-ins' describe
// and guide requests are: by id or by exact).
func (g *guide) params(s *schema.Schema) [][]param {
	if len(s.Properties()) == 0 && s.Union() {
		return g.unionParams(s)
	}
	var shape []param
	for _, prop := range s.Properties() {
		ps, _ := s.Property(prop)
		meaning := g.prose(describe(ps))
		if ref := reference(ps); ref != "" && g.mayName(ref) {
			meaning = strings.TrimSpace(meaning + " Valid values come from `" + ref + "`.")
		}
		shape = append(shape, param{name: prop, typ: typeOf(ps), def: defaultOf(ps.Default()), meaning: meaning, required: s.Required(prop)})
	}
	if len(shape) == 0 {
		return nil
	}
	return [][]param{shape}
}

// defaultOf is a parameter's default as a guide shows it: its JSON, as
// code, or a dash when it has none.
func defaultOf(v ijson.Value, ok bool) string {
	if !ok {
		return "—"
	}
	b, err := ijson.Marshal(v)
	if err != nil {
		return "—"
	}
	return codeSpan(string(b))
}

// unionParams are a union request's alternatives, read from its definition
// as written: the schema package compiles alternatives but does not expose
// them.
func (g *guide) unionParams(s *schema.Schema) [][]param {
	src, _ := s.Definition().Source().(*ijson.Object)
	if src == nil {
		return nil
	}
	var alts []ijson.Value
	for _, k := range []string{"oneOf", "anyOf"} {
		if v, ok := src.Get(k); ok {
			alts, _ = v.([]ijson.Value)
			break
		}
	}
	var out [][]param
	for _, a := range alts {
		o, _ := a.(*ijson.Object)
		props, _ := at(o, "properties").(*ijson.Object)
		if props == nil {
			continue
		}
		required := map[string]bool{}
		list, _ := at(o, "required").([]ijson.Value)
		for _, r := range list {
			if name, ok := r.(string); ok {
				required[name] = true
			}
		}
		var shape []param
		props.Each(func(name string, v ijson.Value) bool {
			p, _ := v.(*ijson.Object)
			def := "—"
			if p != nil {
				def = defaultOf(p.Get("default"))
			}
			desc, _ := at(p, "description").(string)
			shape = append(shape, param{name: name, typ: rawType(p), def: def, meaning: g.prose(desc), required: required[name]})
			return true
		})
		if len(shape) > 0 {
			out = append(out, shape)
		}
	}
	return out
}

// rawType is a schema's type as written, as a person reads it.
func rawType(p *ijson.Object) string {
	if p == nil {
		return "any"
	}
	strOf := func(name string) string { s, _ := at(p, name).(string); return s }
	t := ""
	switch {
	case at(p, "const") != nil:
		b, _ := ijson.Marshal(at(p, "const"))
		t = codeSpan(string(b))
	case at(p, "enum") != nil:
		var vals []string
		list, _ := at(p, "enum").([]ijson.Value)
		for _, v := range list {
			if v != nil {
				b, _ := ijson.Marshal(v)
				vals = append(vals, codeSpan(string(b)))
			}
		}
		t = "one of " + strings.Join(vals, ", ")
	case strOf("x-atlas-logical") != "":
		t = strOf("x-atlas-logical")
	case strOf("format") != "":
		t = strOf("format")
	case at(p, "anyOf") != nil || at(p, "oneOf") != nil:
		alts, _ := at(p, "anyOf").([]ijson.Value)
		if alts == nil {
			alts, _ = at(p, "oneOf").([]ijson.Value)
		}
		var parts []string
		for _, a := range alts {
			o, _ := a.(*ijson.Object)
			parts = append(parts, rawType(o))
		}
		t = strings.Join(parts, " or ")
	default:
		switch x := at(p, "type").(type) {
		case string:
			t = x
		case []ijson.Value:
			var parts []string
			for _, e := range x {
				parts = append(parts, fmt.Sprint(e))
			}
			t = strings.Join(parts, " or ")
		default:
			t = "any"
		}
	}
	if u := strOf("x-atlas-unit"); u != "" {
		t += " (" + u + ")"
	}
	return t
}

// at is an object's member, or nil when there is no object or no member.
func at(v ijson.Value, name string) ijson.Value {
	o, ok := v.(*ijson.Object)
	if !ok || o == nil {
		return nil
	}
	m, _ := o.Get(name)
	return m
}

// 5. What comes back: for a table its grain and keys, then each column
// with its type, unit and meaning; for a record, its fields.
func (g *guide) whatComesBack() []string {
	const head = "## What comes back\n\n"
	var full, compact strings.Builder
	full.WriteString(head)
	compact.WriteString(head)
	schemas, groups := g.groupBy(func(in *profile.Interaction) string { return in.Returns })
	for i, returns := range schemas {
		s := g.p.Schemas[returns]
		if returns == "" || s == nil {
			continue
		}
		row, kind := s, "Field"
		if it := s.Items(); it != nil {
			row, kind = it, "Column"
		}
		result := g.p.Interactions[groups[i][0]].Result
		what := "a " + result
		if result == "content" {
			what = "content"
		}
		fmt.Fprintf(&full, "### %s: %s\n\n", ticked(groups[i]), what)
		fmt.Fprintf(&compact, "%s, %s:", ticked(groups[i]), what)
		if d := g.prose(describe(s)); d != "" {
			full.WriteString(d + "\n\n")
		}
		if result == "table" {
			if grain := g.meaning("grain"); grain != "" {
				full.WriteString("Grain: " + grain + ".\n\n")
			}
			if keys := g.meaning("keys"); keys != "" {
				full.WriteString("Keys: " + keys + ".\n\n")
			}
		}
		if len(row.Properties()) > 0 {
			fmt.Fprintf(&full, "| %s | Type | Can be null | Key | Meaning |\n|---|---|---|---|---|\n", kind)
			var cols []string
			for _, prop := range row.Properties() {
				ps, _ := row.Property(prop)
				null, key := "no", ""
				if ps.Nullable() {
					null = "yes"
				}
				if ps.Key() {
					key = "key"
				}
				fmt.Fprintf(&full, "| %s | %s | %s | %s | %s |\n", escapePipes(codeSpan(prop)), cell(typeOf(ps)), null, key, cell(g.prose(describe(ps))))
				cols = append(cols, codeSpan(prop)+" ("+typeOf(ps)+")")
			}
			full.WriteString("\n")
			compact.WriteString(" " + strings.Join(cols, ", "))
		}
		compact.WriteString(".\n\n")
	}
	if g.p.Meaning != nil && g.p.Meaning.Len() > 0 {
		var meant strings.Builder
		g.p.Meaning.Each(func(name string, v ijson.Value) bool {
			if name == "grain" || name == "keys" {
				return true
			}
			if s := g.meaningOf(v); s != "" {
				fmt.Fprintf(&meant, "- **%s**: %s\n", strings.ReplaceAll(name, "_", " "), s)
			}
			return true
		})
		if meant.Len() > 0 {
			full.WriteString("What it means:\n\n" + meant.String() + "\n")
		}
	}
	return []string{full.String(), compact.String(), ""}
}

// meaning is a member of the profile's meaning, as prose.
func (g *guide) meaning(name string) string {
	if g.p.Meaning == nil {
		return ""
	}
	v, _ := g.p.Meaning.Get(name)
	if name == "keys" {
		if list, ok := v.([]ijson.Value); ok {
			var keys []string
			for _, k := range list {
				keys = append(keys, codeSpan(fmt.Sprint(k)))
			}
			return strings.Join(keys, ", ")
		}
	}
	return g.meaningOf(v)
}

func (g *guide) meaningOf(v ijson.Value) string {
	switch x := v.(type) {
	case string:
		return g.prose(x)
	case []ijson.Value:
		var parts []string
		for _, e := range x {
			if s := g.meaningOf(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case ijson.Number:
		return string(x)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

// 6. Examples: each of the profile's examples, in the calling client's
// syntax, for an interaction the reader holds and valid against its
// request schema (ATLAS-DISCOVERY-009).
func (g *guide) examples() []string {
	const head = "## Examples\n\n"
	var shown []string
	list, _ := usageValue(g.p, "examples").([]ijson.Value)
	for _, e := range list {
		o, _ := e.(*ijson.Object)
		if o == nil {
			continue
		}
		name, _ := o.Get("interaction")
		interaction, _ := name.(string)
		if interaction == "" && len(g.p.Order) > 0 {
			interaction = g.p.Order[0]
		}
		in, ok := g.p.Interactions[interaction]
		if !ok || !slices.Contains(g.held, interaction) {
			continue
		}
		req, _ := o.Get("request")
		if req == nil {
			req = ijson.NewObject()
		}
		if s := g.p.Schemas[in.Takes]; in.Takes != "" && (s == nil || !s.Valid(req)) {
			continue
		}
		title, _ := o.Get("title")
		t, _ := title.(string)
		if t = oneLine(t); t == "" {
			t = "`" + interaction + "`"
		}
		if !g.mayNameAll(t) {
			t = "`" + interaction + "`"
		}
		shown = append(shown, fmt.Sprintf("### %s\n\n%s\n", t, example(g.client, g.pinned+":"+interaction, req)))
	}
	if len(shown) == 0 {
		return []string{head + "Its owner gives no example of what you hold.\n\n", ""}
	}
	return []string{head + strings.Join(shown, ""), head + shown[0], ""}
}

// 7. Problems: each problem it can return, with its next step.
func (g *guide) problems() []string {
	const head = "## Problems\n\n"
	if len(g.p.Errors) == 0 {
		return []string{head + "Only the protocol's own: each says what to do in its `next`.\n\n", ""}
	}
	var full, compact strings.Builder
	full.WriteString(head + "Beyond the protocol's own, each of which says what to do in its `next`:\n\n| Code | Status | Retry | What it means | What to do |\n|---|---|---|---|---|\n")
	compact.WriteString(head)
	for _, e := range g.p.Errors {
		retry := "no"
		if e.Retryable {
			retry = "yes"
		}
		next := g.prose(e.Next)
		if next == "" {
			next = "Ask " + identity(g.p, "owner") + ", its owner."
		}
		fmt.Fprintf(&full, "| %s | %d | %s | %s | %s |\n", escapePipes(codeSpan(e.Code)), e.Status, retry, cell(g.prose(e.Description)), cell(next))
		fmt.Fprintf(&compact, "- %s (%d): %s\n", codeSpan(e.Code), e.Status, next)
	}
	return []string{full.String() + "\n", compact.String() + "\n", ""}
}

// 8. Your limits and its cost: what is left for the reader, and the cost
// of a call where it is metered.
func (g *guide) limits() []string {
	var b strings.Builder
	b.WriteString("## Your limits and its cost\n\n")
	if len(g.r.Limits) == 0 {
		b.WriteString("No limit names you.\n")
	}
	for _, l := range g.r.Limits {
		b.WriteString("- " + l + "\n")
	}
	b.WriteString("\n")
	switch c := usageValue(g.p, "cost").(type) {
	case string:
		if s := g.prose(c); s != "" {
			b.WriteString("Cost: " + s + "\n\n")
		}
	case *ijson.Object:
		var parts []string
		c.Each(func(name string, v ijson.Value) bool {
			if s := g.meaningOf(v); s != "" {
				parts = append(parts, strings.ReplaceAll(name, "_", " ")+" "+s)
			}
			return true
		})
		if len(parts) > 0 {
			b.WriteString("Cost: " + strings.Join(parts, "; ") + ".\n\n")
		}
	}
	return []string{b.String(), ""}
}

// 9. Related: the capabilities it refers to and those that refer to it.
func (g *guide) related() []string {
	var ids []string
	for _, r := range append(references(g.p), g.r.ReferredBy...) {
		if r != g.pinned && r != g.p.ID && g.mayName(r) && !slices.Contains(ids, r) {
			ids = append(ids, r)
		}
	}
	if len(ids) == 0 {
		return []string{"## Related\n\nNone.\n\n", ""}
	}
	var b strings.Builder
	b.WriteString("## Related\n\n")
	for _, id := range ids {
		b.WriteString("- `" + id + "`\n")
	}
	return []string{b.String() + "\n", ""}
}

// references are the capabilities a profile refers to: its related ones,
// then those its request schemas take values from.
func references(p *profile.Profile) []string {
	var out []string
	list, _ := usageValue(p, "related").([]ijson.Value)
	for _, r := range list {
		if s, ok := r.(string); ok && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	for _, name := range p.Order {
		in := p.Interactions[name]
		s := p.Schemas[in.Takes]
		if in.Takes == "" || s == nil {
			continue
		}
		for _, prop := range s.Properties() {
			ps, _ := s.Property(prop)
			if ref := reference(ps); ref != "" && !slices.Contains(out, ref) {
				out = append(out, ref)
			}
		}
	}
	return out
}

// reference is the capability a property's values come from.
func reference(ps *schema.Schema) string {
	if ps == nil {
		return ""
	}
	if r := ps.Reference(); r != "" {
		return r
	}
	if it := ps.Items(); it != nil {
		return it.Reference()
	}
	return ""
}

// mayName reports whether the guide may name what a dotted word names to
// its reader: what MayName says, or, with no MayName, a built-in only.
func (g *guide) mayName(word string) bool {
	if g.r.MayName == nil {
		id, _, _ := strings.Cut(word, ":")
		return grammar.Builtin(bareID(id))
	}
	return g.r.MayName(word)
}

// mayNameAll reports whether a text names nothing the reader may not be
// told of.
func (g *guide) mayNameAll(text string) bool {
	for _, m := range mention.FindAllString(text, -1) {
		if !g.mayName(m) {
			return false
		}
	}
	return true
}

// mention is a dotted word that may name a capability: a capability-id,
// pinned-id or address (grammar.abnf).
var mention = regexp.MustCompile(`[a-z](?:-?[a-z0-9])*(?:\.[a-z](?:-?[a-z0-9])*)+(?:@v[1-9][0-9]*(?::[a-z](?:-?[a-z0-9])*)?)?`)

// prose is owner prose without the sentences or clauses that name what the
// reader may not be told of (ATLAS-DISCOVERY-010, ATLAS-CORE §3: "metadata
// and examples referring to other capabilities follow the same visibility
// rules"). Prose that names nothing hidden is returned as written.
func (g *guide) prose(s string) string {
	if s == "" || g.mayNameAll(s) {
		return s
	}
	parts := clauses(s)
	keep := make([]bool, len(parts))
	for i, c := range parts {
		keep[i] = g.mayNameAll(c)
	}
	var kept []string
	for i, c := range parts {
		if !keep[i] {
			continue
		}
		// A clause that led into one left out ends its sentence.
		if (strings.HasSuffix(c, ":") || strings.HasSuffix(c, ";")) && (i+1 == len(parts) || !keep[i+1]) {
			c = c[:len(c)-1] + "."
		}
		kept = append(kept, c)
	}
	out := strings.Join(kept, " ")
	if r, _ := utf8.DecodeRuneInString(s); unicode.IsUpper(r) {
		if first, n := utf8.DecodeRuneInString(out); unicode.IsLower(first) {
			out = string(unicode.ToUpper(first)) + out[n:]
		}
	}
	return out
}

// clauses are a text's sentences and clauses, split after each '.', '!',
// '?', ';' or ':' that ends a word: "You need X: use Y." is "You need X:"
// and "use Y.", so a hidden Y leaves "You need X." standing.
func clauses(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '.', '!', '?', ';', ':':
			if i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\n' || s[i+1] == '\t' {
				if part := strings.TrimSpace(s[start : i+1]); part != "" {
					out = append(out, part)
				}
				start = i + 1
			}
		}
	}
	if part := strings.TrimSpace(s[start:]); part != "" {
		out = append(out, part)
	}
	return out
}

// cell is text for a Markdown table's cell: one line, its bars escaped.
func cell(s string) string { return escapePipes(oneLine(s)) }

// escapePipes escapes a table cell's bars, code spans' too, so none ends
// the cell (GFM: "include a pipe in a cell's content by escaping it,
// including inside other inline spans").
func escapePipes(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

// oneLine is text on one line, its runs of space each one space: a table
// row and a heading end at a line's end.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// codeSpan is text as a Markdown code span, whatever it holds (CommonMark
// §6.1): fenced by one more backtick than its longest run of them, and set
// off by a space where it begins or ends with a backtick, or has a space at
// both ends, which a renderer would strip. Its line endings are spaces, as
// a renderer shows them, so it never ends a table row.
func codeSpan(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
	longest, run := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] != '`' {
			run = 0
			continue
		}
		run++
		longest = max(longest, run)
	}
	fence := strings.Repeat("`", longest+1)
	if s == "" || s[0] == '`' || s[len(s)-1] == '`' || s[0] == ' ' && s[len(s)-1] == ' ' && strings.Trim(s, " ") != "" {
		s = " " + s + " "
	}
	return fence + s + fence
}

// property is a member of the profile's properties, as text.
func property(p *profile.Profile, member string) string {
	props, _ := p.Doc.Get("properties")
	o, _ := props.(*ijson.Object)
	if o == nil {
		return ""
	}
	v, _ := o.Get(member)
	s, _ := v.(string)
	return s
}

func usageValue(p *profile.Profile, member string) ijson.Value {
	if p.Usage == nil {
		return nil
	}
	v, _ := p.Usage.Get(member)
	return v
}

func describe(s *schema.Schema) string {
	if src, ok := s.Source().(*ijson.Object); ok && src != nil {
		if d, ok := src.Get("description"); ok {
			if t, isString := d.(string); isString {
				return t
			}
		}
	}
	return ""
}

// typeOf is a property's type as a person reads it.
func typeOf(s *schema.Schema) string {
	var t string
	switch name, p, sc, ok := s.Logical(); {
	case ok && name == "Decimal":
		t = fmt.Sprintf("decimal (%d digits, %d after the point), as a string", p, sc)
	case ok && name == "Money":
		t = "Money: `{amount, currency}`, the amount an exact decimal string"
	case ok:
		t = name
	case s.Enum() != nil:
		var vals []string
		for _, v := range s.Enum() {
			if v != nil {
				vals = append(vals, codeSpan(fmt.Sprint(v)))
			}
		}
		t = "one of " + strings.Join(vals, ", ")
	case s.Type() == "array" && s.Items() != nil:
		t = "array of " + typeOf(s.Items())
	case s.Format() != "":
		t = s.Format()
	default:
		t = s.Type()
	}
	if u, ok := s.Unit(); ok && !strings.Contains(strings.ToLower(t), string(u)) {
		t += " (" + string(u) + ")"
	}
	return t
}

// example is one request in a client's syntax.
func example(client, address string, req ijson.Value) string {
	if req == nil {
		req = ijson.NewObject()
	}
	jsonText, _ := ijson.Marshal(req)
	switch client {
	case "python":
		return fmt.Sprintf("```python\nfrom fiatlas import FIAtlas\n\natlas = FIAtlas()\nresult = atlas.call(%q, %s)\n```\n", address, python(req))
	case "node":
		return fmt.Sprintf("```ts\nimport { atlas } from '@atlas/sdk';\n\nconst client = await atlas.connect();\nconst result = await client.call('%s', %s);\n```\n", address, jsonText)
	case "go":
		// A raw string reads best, but cannot hold a backtick.
		literal := "`" + string(jsonText) + "`"
		if bytes.ContainsRune(jsonText, '`') {
			literal = strconv.Quote(string(jsonText))
		}
		return fmt.Sprintf("```go\nclient, err := atlas.Connect(ctx)\nresult, err := client.Call(ctx, %q, atlas.Request(%s))\n```\n", address, literal)
	case "mcp":
		return fmt.Sprintf("```json\n{\"tool\": \"atlas_call\", \"arguments\": {\"address\": %q, \"request\": %s}}\n```\n", address, jsonText)
	}
	return fmt.Sprintf("```sh\natlas call %s --request '%s'\n```\n", address, strings.ReplaceAll(string(jsonText), "'", `'\''`))
}

// python writes a JSON value as a Python literal.
func python(v ijson.Value) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case string:
		return strconv.Quote(x)
	case ijson.Number:
		return string(x)
	case []ijson.Value:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = python(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *ijson.Object:
		if x == nil {
			return "None"
		}
		var parts []string
		x.Each(func(k string, e ijson.Value) bool {
			parts = append(parts, strconv.Quote(k)+": "+python(e))
			return true
		})
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return "None"
}
