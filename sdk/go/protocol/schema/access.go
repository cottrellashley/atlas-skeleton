package schema

import (
	"slices"
	"strings"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/units"
)

// What a schema says, for resolution (§9.5) and the Arrow mapping (§9.2.7).
// A $ref is followed to the end of its chain, as validation follows it: a
// property defined by reference answers with its definition's type and
// members, and with the prose and Atlas keywords of the first node in the
// chain that writes them.

func (s *Schema) def() *node { return s.n.resolved() }

// first is the first node of the chain for which has holds, or nil.
func (s *Schema) first(has func(*node) bool) *node {
	for _, n := range s.n.chain() {
		if has(n) {
			return n
		}
	}
	return nil
}

// any reports whether has holds for any node of the chain.
func (s *Schema) any(has func(*node) bool) bool { return s.first(has) != nil }

func wrap(n *node) *Schema {
	if n == nil {
		return nil
	}
	return &Schema{n}
}

// Pointer is where the schema stands in its root.
func (s *Schema) Pointer() string { return s.n.ptr }

// Source is the schema as written.
func (s *Schema) Source() ijson.Value { return s.n.source }

// Definition is the schema its $ref chain ends at, or the schema itself:
// what holds its type and its constraints.
func (s *Schema) Definition() *Schema { return wrap(s.def()) }

// Type is the schema's type without null: boolean, object, array, number,
// string, integer, or "" for a union (anyOf, oneOf) or a boolean schema.
func (s *Schema) Type() string {
	if d := s.def(); len(d.types) == 1 {
		return d.types[0]
	}
	return ""
}

// Nullable reports whether the schema allows null.
func (s *Schema) Nullable() bool { return s.n.chainNullable() }

// Union reports whether the schema is anyOf or oneOf.
func (s *Schema) Union() bool { d := s.def(); return d.anyOf != nil || d.oneOf != nil }

// Properties are an object schema's property names, in order.
func (s *Schema) Properties() []string { return slices.Clone(s.def().order) }

// Property returns one property's schema.
func (s *Schema) Property(name string) (*Schema, bool) {
	p, ok := s.def().props[name]
	return wrap(p), ok
}

// Required reports whether a property is required.
func (s *Schema) Required(name string) bool { return slices.Contains(s.def().required, name) }

// Items is an array schema's items, or nil.
func (s *Schema) Items() *Schema {
	if it := s.def().items; it != nil && it.boolean == nil {
		return wrap(it)
	}
	return nil
}

// Default is the schema's default, if it declares one.
func (s *Schema) Default() (ijson.Value, bool) {
	if d := s.first(func(n *node) bool { return n.hasDefault }); d != nil {
		return d.defaultV, true
	}
	return nil, false
}

// Aliases are the property's earlier names, oldest first.
func (s *Schema) Aliases() []string {
	if d := s.first(func(n *node) bool { return n.aliases != nil }); d != nil {
		return slices.Clone(d.aliases)
	}
	return nil
}

// Enum is the schema's enum, or nil.
func (s *Schema) Enum() []ijson.Value { return s.def().enum }

// EnumHas reports whether a string is one of the enum's values.
func (s *Schema) EnumHas(v string) bool {
	return slices.ContainsFunc(s.def().enum, func(e ijson.Value) bool { return e == v })
}

// EnumAlias returns the value an earlier spelling stands for, by
// x-atlas-enum's aliases.
func (s *Schema) EnumAlias(spelling string) (string, bool) {
	v, ok := s.def().enumAliases[spelling]
	return v, ok
}

// Format is the schema's own format, or "".
func (s *Schema) Format() string {
	d := s.def()
	if len(d.formats) > 0 {
		return d.formats[0]
	}
	return ""
}

// Logical is the schema's logical type: its name (Bp, Decimal, Money, …)
// and, for a Decimal, its precision and scale.
func (s *Schema) Logical() (name string, precision, scale int, ok bool) {
	l := s.def().logical
	if l == nil {
		return "", 0, 0, false
	}
	return l.name, l.precision, l.scale, true
}

// Unit is the unit of a number, from its logical type or x-atlas-unit.
func (s *Schema) Unit() (units.Unit, bool) {
	u := s.def().unit
	return u, u != ""
}

// Key reports whether the property is part of its row's key.
func (s *Schema) Key() bool { return s.any(func(n *node) bool { return n.key }) }

// Deprecated reports whether the property is deprecated.
func (s *Schema) Deprecated() bool { return s.any(func(n *node) bool { return n.deprecated }) }

// Reference is the pinned-id of the capability that lists the property's
// valid values, or "".
func (s *Schema) Reference() string {
	if d := s.first(func(n *node) bool { return n.reference != "" }); d != nil {
		return d.reference
	}
	return ""
}

// Project keeps only the named properties of an object schema, in
// properties and in required (§9.5.2 step 5). A name it does not have is
// left out; the caller has already refused it.
func (s *Schema) Project(columns []string) *Schema {
	d := s.def()
	p := *d
	p.props = map[string]*node{}
	p.order, p.required = nil, nil
	for _, name := range d.order {
		if slices.Contains(columns, name) {
			p.props[name] = d.props[name]
			p.order = append(p.order, name)
			if slices.Contains(d.required, name) {
				p.required = append(p.required, name)
			}
		}
	}
	p.target = nil
	return &Schema{&p}
}

// Keyword is one Atlas keyword on a property, as written.
type Keyword struct {
	Name  string // x-atlas-logical, x-atlas-key, …
	Value ijson.Value
}

// AtlasKeywords are the Atlas keywords on a property, in the order it
// writes them: its own, then each definition's along its $ref chain where
// nothing before it wrote them.
func (s *Schema) AtlasKeywords() []Keyword {
	var out []Keyword
	seen := map[string]bool{}
	for _, n := range s.n.chain() {
		o, ok := n.sourceObject()
		if !ok {
			continue
		}
		o.Each(func(k string, v ijson.Value) bool {
			if strings.HasPrefix(k, "x-atlas-") && !seen[k] {
				seen[k] = true
				out = append(out, Keyword{k, v})
			}
			return true
		})
	}
	return out
}

func (n *node) sourceObject() (*ijson.Object, bool) {
	if n == nil {
		return nil, false
	}
	o, ok := n.source.(*ijson.Object)
	return o, ok
}
