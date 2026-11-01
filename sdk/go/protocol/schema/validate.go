package schema

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// Failure is one way an instance is invalid (§9.2.5): the instance's JSON
// Pointer and the keyword that failed. Failures sort by pointer, then
// keyword, in code-point order, so the first is the same in every
// implementation (assumption A-009).
type Failure struct {
	Pointer string `json:"pointer"`
	Keyword string `json:"keyword"`
	Message string `json:"message"`
}

// Validate returns every failure of an instance, sorted; none means valid.
func (s *Schema) Validate(v ijson.Value) []Failure {
	var fs []Failure
	s.n.validate(v, "", &fs)
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Pointer != fs[j].Pointer {
			return fs[i].Pointer < fs[j].Pointer
		}
		return fs[i].Keyword < fs[j].Keyword
	})
	return slices.CompactFunc(fs, func(a, b Failure) bool { return a.Pointer == b.Pointer && a.Keyword == b.Keyword })
}

// Valid reports whether an instance has no failure.
func (s *Schema) Valid(v ijson.Value) bool {
	var fs []Failure
	s.n.validate(v, "", &fs)
	return len(fs) == 0
}

func add(out *[]Failure, ptr, keyword, format string, args ...any) {
	*out = append(*out, Failure{ptr, keyword, fmt.Sprintf(format, args...)})
}

// validate applies 2020-12's semantics: every keyword on its own, each
// keyword only to the instances of its type.
func (n *node) validate(v ijson.Value, ptr string, out *[]Failure) {
	if n.boolean != nil {
		if !*n.boolean {
			add(out, ptr, "false", "no value is allowed here")
		}
		return
	}
	if n.target != nil {
		n.target.validate(v, ptr, out)
	}
	if !n.admits(v) {
		add(out, ptr, "type", "%s is not %s", describe(v), n.typeName())
	}
	if n.enum != nil && !slices.ContainsFunc(n.enum, func(e ijson.Value) bool { return ijson.Equal(e, v) }) {
		add(out, ptr, "enum", "%s is not one of the enum", describe(v))
	}
	if n.hasConst && !ijson.Equal(n.constV, v) {
		add(out, ptr, "const", "%s is not the const", describe(v))
	}
	switch x := v.(type) {
	case ijson.Number:
		n.number(x, ptr, out)
	case string:
		n.str(x, ptr, out)
	case []ijson.Value:
		n.array(x, ptr, out)
	case *ijson.Object:
		n.object(x, ptr, out)
	}
	if n.anyOf != nil && !slices.ContainsFunc(n.anyOf, func(b *node) bool { return b.valid(v) }) {
		add(out, ptr, "anyOf", "the value matches none of anyOf")
	}
	if n.oneOf != nil {
		matched := 0
		for _, b := range n.oneOf {
			if b.valid(v) {
				matched++
			}
		}
		if matched != 1 {
			add(out, ptr, "oneOf", "the value matches %d of oneOf, not exactly one", matched)
		}
	}
}

func (n *node) valid(v ijson.Value) bool {
	var fs []Failure
	n.validate(v, "", &fs)
	return len(fs) == 0
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

func describe(v ijson.Value) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(x)
	case ijson.Number:
		return string(x)
	case string:
		if len(x) > 40 {
			return strconv.Quote(x[:40]) + "…"
		}
		return strconv.Quote(x)
	case []ijson.Value:
		return "an array"
	case *ijson.Object:
		return "an object"
	}
	return "a value"
}

func (n *node) number(x ijson.Number, ptr string, out *[]Failure) {
	f, err := x.Float64()
	if err != nil {
		add(out, ptr, "type", "%s is not a binary64 number", x)
		return
	}
	if n.minimum != nil && f < *n.minimum {
		add(out, ptr, "minimum", "%s is below %v", x, *n.minimum)
	}
	if n.maximum != nil && f > *n.maximum {
		add(out, ptr, "maximum", "%s is above %v", x, *n.maximum)
	}
	if n.exMinimum != nil && f <= *n.exMinimum {
		add(out, ptr, "exclusiveMinimum", "%s is not above %v", x, *n.exMinimum)
	}
	if n.exMaximum != nil && f >= *n.exMaximum {
		add(out, ptr, "exclusiveMaximum", "%s is not below %v", x, *n.exMaximum)
	}
	if n.multipleOf > 0 && math.Mod(f, n.multipleOf) != 0 {
		add(out, ptr, "multipleOf", "%s is not a multiple of %v", x, n.multipleOf)
	}
}

func (n *node) str(s, ptr string, out *[]Failure) {
	if n.minLength >= 0 || n.maxLength >= 0 {
		l := utf8.RuneCountInString(s)
		if n.minLength >= 0 && l < n.minLength {
			add(out, ptr, "minLength", "%d characters, fewer than %d", l, n.minLength)
		}
		if n.maxLength >= 0 && l > n.maxLength {
			add(out, ptr, "maxLength", "%d characters, more than %d", l, n.maxLength)
		}
	}
	for _, re := range n.patterns {
		if !re.MatchString(s) {
			add(out, ptr, "pattern", "%s does not match its pattern", describe(s))
		}
	}
	for _, f := range n.formats {
		if why := checkFormat(f, s, n.decimal); why != "" {
			add(out, ptr, "format", "not a %s: %s", f, why)
		}
	}
}

func (n *node) array(a []ijson.Value, ptr string, out *[]Failure) {
	for i, e := range a {
		at := ptr + "/" + strconv.Itoa(i)
		switch {
		case i < len(n.prefix):
			n.prefix[i].validate(e, at, out)
		case n.items != nil && n.items.boolean != nil && !*n.items.boolean:
			add(out, at, "items", "no item is allowed here")
		case n.items != nil:
			n.items.validate(e, at, out)
		}
	}
	if n.minItems >= 0 && len(a) < n.minItems {
		add(out, ptr, "minItems", "%d items, fewer than %d", len(a), n.minItems)
	}
	if n.maxItems >= 0 && len(a) > n.maxItems {
		add(out, ptr, "maxItems", "%d items, more than %d", len(a), n.maxItems)
	}
	if n.uniqueItems {
		for i := range a {
			for j := 0; j < i; j++ {
				if ijson.Equal(a[i], a[j]) {
					add(out, ptr, "uniqueItems", "items %d and %d are equal", j, i)
					return
				}
			}
		}
	}
}

func (n *node) object(o *ijson.Object, ptr string, out *[]Failure) {
	o.Each(func(name string, m ijson.Value) bool {
		at := ptr + "/" + escape(name)
		if p, ok := n.props[name]; ok {
			p.validate(m, at, out)
			return true
		}
		if n.additional == nil {
			return true
		}
		if n.additional.boolean != nil && !*n.additional.boolean {
			add(out, at, "additionalProperties", "%q is not a member this object takes", name)
			return true
		}
		n.additional.validate(m, at, out)
		return true
	})
	for _, r := range n.required {
		if _, ok := o.Get(r); !ok {
			add(out, ptr+"/"+escape(r), "required", "%q is required", r)
		}
	}
	if n.minProps >= 0 && o.Len() < n.minProps {
		add(out, ptr, "minProperties", "%d members, fewer than %d", o.Len(), n.minProps)
	}
	if n.maxProps >= 0 && o.Len() > n.maxProps {
		add(out, ptr, "maxProperties", "%d members, more than %d", o.Len(), n.maxProps)
	}
}
