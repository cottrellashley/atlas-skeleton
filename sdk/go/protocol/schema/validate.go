package schema

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"atlas.local/sdk/go/protocol/ijson"
)

// Failure is one way an instance is invalid (§9.2.5): the instance's JSON
// Pointer and the keyword that failed. Failures sort by pointer, then
// keyword, in code-point order, each pair once, so the first is the same
// in every implementation (assumption A-009). A failure whose pointer
// would pass MaxPointerBytes is reported at ReportedPointer's ancestor of
// it (assumption A-193).
type Failure struct {
	Pointer string `json:"pointer"`
	Keyword string `json:"keyword"`
	Message string `json:"message"`
}

// The bounds of what a validation reports (R049, assumptions A-190 and
// A-193). A request inside the body limit may fail in every value it
// holds; a validation still reads it in time linear in its size, and
// reports at most MaxFailures failures, the first in the order of
// assumption A-009, while their pointers and keywords come to at most
// MaxFailureBytes (the first is always reported, and always fits). A
// pointer is at most MaxPointerBytes, and a message at most
// MaxMessageBytes, then "…" where it is cut. The bounds count only what
// the contract carries, pointers and keywords, so every implementation
// can cut the same list in the same place: a message is prose.
const (
	MaxFailures     = 100
	MaxFailureBytes = 64 << 10
	MaxPointerBytes = 1 << 10
	MaxMessageBytes = 256
)

// Validate returns the instance's failures, sorted: every one, or the
// first of them within the bounds. None means valid. It does not say
// whether it left any out, so build invalid_request from Failures, which
// does.
func (s *Schema) Validate(v ijson.Value) []Failure {
	fs, _ := s.Failures(v)
	return fs
}

// Failures is Validate, and reports whether the instance fails in more
// ways than the failures returned.
func (s *Schema) Failures(v ijson.Value) ([]Failure, bool) {
	fs, more, _ := s.n.failures(v, MaxFailures, MaxFailureBytes, MaxPointerBytes)
	return fs, more
}

// Valid reports whether an instance has no failure. It stops at the
// first.
func (s *Schema) Valid(v ijson.Value) bool { return s.n.valid(v) }

// failures validates in full, keeping at most limit failures within
// budget bytes, at pointers of at most pointer bytes, and reports the
// work done.
func (n *node) failures(v ijson.Value, limit, budget, pointer int) ([]Failure, bool, int) {
	c := collector{max: limit, pointer: pointer}
	n.validate(v, nil, &c)
	fs, more := c.report(budget)
	return fs, more, c.steps
}

func (n *node) valid(v ijson.Value) bool { return n.validIn(v, &collector{}) }

// validIn is valid inside another validation, whose work it counts.
func (n *node) validIn(v ijson.Value, outer *collector) bool {
	c := collector{first: true}
	n.validate(v, nil, &c)
	outer.steps += c.steps
	return len(c.kept) == 0
}

// validate applies 2020-12's semantics: every keyword on its own, each
// keyword only to the instances of its type.
func (n *node) validate(v ijson.Value, at *path, c *collector) {
	c.steps++
	if n.boolean != nil {
		if !*n.boolean {
			c.add(at, "false", "no value is allowed here")
		}
		return
	}
	if n.target != nil {
		n.target.validate(v, at, c)
	}
	if !n.admits(v) {
		c.add(at, "type", "%s is not %s", describe(v), n.typeName())
	}
	if n.enum != nil && !slices.ContainsFunc(n.enum, func(e ijson.Value) bool { return ijson.Equal(e, v) }) {
		c.add(at, "enum", "%s is not one of the enum", describe(v))
	}
	if n.hasConst && !ijson.Equal(n.constV, v) {
		c.add(at, "const", "%s is not the const", describe(v))
	}
	if c.done() {
		return
	}
	switch x := v.(type) {
	case ijson.Number:
		n.number(x, at, c)
	case string:
		n.str(x, at, c)
	case []ijson.Value:
		n.array(x, at, c)
	case *ijson.Object:
		n.object(x, at, c)
	}
	for _, b := range n.allOf {
		b.validate(v, at, c)
	}
	if c.done() {
		return
	}
	if n.ifN != nil {
		branch := n.elseN
		if n.ifN.validIn(v, c) {
			branch = n.thenN
		}
		if branch != nil {
			branch.validate(v, at, c)
		}
	}
	if n.anyOf != nil && !slices.ContainsFunc(n.anyOf, func(b *node) bool { return b.validIn(v, c) }) {
		c.add(at, "anyOf", "the value matches none of anyOf")
	}
	if n.oneOf != nil {
		matched := 0
		for _, b := range n.oneOf {
			if b.validIn(v, c) {
				matched++
			}
		}
		if matched != 1 {
			c.add(at, "oneOf", "the value matches %d of oneOf, not exactly one", matched)
		}
	}
}

func (n *node) typeName() string {
	names := slices.Clone(n.types)
	if n.nullable {
		names = append(names, "null")
	}
	if len(names) == 1 {
		return article(names[0]) + " " + names[0]
	}
	return strings.Join(names, " or ")
}

// describe names a value in a message, quoting at most 40 bytes of it.
func describe(v ijson.Value) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(x)
	case ijson.Number:
		return clip(string(x), 40)
	case string:
		if len(x) > 40 {
			return strconv.Quote(x[:cut(x, 40)]) + "…"
		}
		return strconv.Quote(x)
	case []ijson.Value:
		return "an array"
	case *ijson.Object:
		return "an object"
	}
	return "a value"
}

func (n *node) number(x ijson.Number, at *path, c *collector) {
	f, err := x.Float64()
	if err != nil {
		c.add(at, "type", "%s is not a binary64 number", describe(x))
		return
	}
	if n.minimum != nil && f < *n.minimum {
		c.add(at, "minimum", "%s is below %v", describe(x), *n.minimum)
	}
	if n.maximum != nil && f > *n.maximum {
		c.add(at, "maximum", "%s is above %v", describe(x), *n.maximum)
	}
	if n.exMinimum != nil && f <= *n.exMinimum {
		c.add(at, "exclusiveMinimum", "%s is not above %v", describe(x), *n.exMinimum)
	}
	if n.exMaximum != nil && f >= *n.exMaximum {
		c.add(at, "exclusiveMaximum", "%s is not below %v", describe(x), *n.exMaximum)
	}
	if n.multipleOf > 0 && math.Mod(f, n.multipleOf) != 0 {
		c.add(at, "multipleOf", "%s is not a multiple of %v", describe(x), n.multipleOf)
	}
}

func (n *node) str(s string, at *path, c *collector) {
	if n.minLength >= 0 || n.maxLength >= 0 {
		l := utf8.RuneCountInString(s)
		if n.minLength >= 0 && l < n.minLength {
			c.add(at, "minLength", "%d characters, fewer than %d", l, n.minLength)
		}
		if n.maxLength >= 0 && l > n.maxLength {
			c.add(at, "maxLength", "%d characters, more than %d", l, n.maxLength)
		}
	}
	for _, re := range n.patterns {
		if c.done() {
			return
		}
		if !re.MatchString(s) {
			c.add(at, "pattern", "%s does not match its pattern", describe(s))
		}
	}
	for _, f := range n.formats {
		if c.done() {
			return
		}
		if why := checkFormat(f, s, n.decimal); why != "" {
			c.add(at, "format", "not a %s: %s", f, why)
		}
	}
}

func (n *node) array(a []ijson.Value, at *path, c *collector) {
	if n.prefix != nil || n.items != nil {
		for i, e := range a {
			if c.done() {
				return
			}
			sub, extra := n.items, i >= len(n.prefix)
			if !extra {
				sub = n.prefix[i]
			}
			if sub == nil {
				break // nothing applies to the items that remain
			}
			item, visit := c.enter(at, "", i)
			if !visit {
				continue
			}
			if extra && sub.boolean != nil && !*sub.boolean {
				c.add(item, "items", "no item is allowed here")
				continue
			}
			sub.validate(e, item, c)
		}
	}
	if n.minItems >= 0 && len(a) < n.minItems {
		c.add(at, "minItems", "%d items, fewer than %d", len(a), n.minItems)
	}
	if n.maxItems >= 0 && len(a) > n.maxItems {
		c.add(at, "maxItems", "%d items, more than %d", len(a), n.maxItems)
	}
	if n.uniqueItems && !c.done() && !c.skip(at) {
		if j, i, ok := repeated(a, c); ok {
			c.add(at, "uniqueItems", "items %d and %d are equal", j, i)
		}
	}
}

func (n *node) object(o *ijson.Object, at *path, c *collector) {
	if n.props != nil || n.additional != nil || n.names != nil {
		o.Each(func(name string, m ijson.Value) bool {
			if c.done() {
				return false
			}
			member, visit := c.enter(at, name, -1)
			if !visit {
				return true
			}
			if n.names != nil && !n.names.validIn(name, c) {
				c.add(member, "propertyNames", "the member name %s is not one this object takes", describe(name))
			}
			if p, ok := n.props[name]; ok {
				p.validate(m, member, c)
				return true
			}
			switch {
			case n.additional == nil:
			case n.additional.boolean != nil && !*n.additional.boolean:
				c.add(member, "additionalProperties", "%s is not a member this object takes", describe(name))
			default:
				n.additional.validate(m, member, c)
			}
			return true
		})
	}
	for _, r := range n.required {
		if _, ok := o.Get(r); !ok {
			c.add(c.member(at, r), "required", "%q is required", r)
		}
	}
	if n.minProps >= 0 && o.Len() < n.minProps {
		c.add(at, "minProperties", "%d members, fewer than %d", o.Len(), n.minProps)
	}
	if n.maxProps >= 0 && o.Len() > n.maxProps {
		c.add(at, "maxProperties", "%d members, more than %d", o.Len(), n.maxProps)
	}
}
