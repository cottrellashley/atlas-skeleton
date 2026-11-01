// Package schema is the Atlas Schema profile of JSON Schema 2020-12 (§9.2):
// Compile holds a schema to the profile and compiles it, Validate applies
// 2020-12's semantics with formats as assertions and logical types as their
// own keywords, and the accessors give resolution (§9.5) and the Arrow
// mapping (§9.2.7) what a schema says.
//
// A compiled Schema is immutable and safe for concurrent use.
package schema

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/units"
)

// Kind is which side of a contract a schema describes. A request schema is
// closed and accepts no UNKNOWN; every other schema is open, and its string
// enums carry UNKNOWN (§9.2.1 rule 6, §9.2.3).
type Kind int

// The kinds.
const (
	Open Kind = iota
	Request
)

// Limits of §9.2.1 rule 5.
const (
	MaxDepth = 32
	MaxSize  = 1 << 20
)

// Dialect is every schema's $schema.
const Dialect = "https://json-schema.org/draft/2020-12/schema"

// Vocabulary is the Atlas vocabulary's URI.
const Vocabulary = "https://atlas.jhg.com/vocab/2026-10"

// vocabularies is every schema's $vocabulary, exactly (§9.2.1 rule 2).
var vocabularies = map[string]bool{
	"https://json-schema.org/draft/2020-12/vocab/core":             true,
	"https://json-schema.org/draft/2020-12/vocab/applicator":       true,
	"https://json-schema.org/draft/2020-12/vocab/validation":       true,
	"https://json-schema.org/draft/2020-12/vocab/meta-data":        true,
	"https://json-schema.org/draft/2020-12/vocab/format-assertion": true,
	Vocabulary: false,
}

// keywords are the profile's keywords (§9.2.1 rule 3, §9.2.3).
var keywords = map[string]bool{
	"$schema": true, "$vocabulary": true, "$id": true, "$defs": true, "$ref": true, "$comment": true,
	"properties": true, "additionalProperties": true, "items": true, "prefixItems": true, "anyOf": true, "oneOf": true,
	"type": true, "enum": true, "const": true, "required": true,
	"minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true, "multipleOf": true,
	"minLength": true, "maxLength": true, "pattern": true,
	"minItems": true, "maxItems": true, "uniqueItems": true, "minProperties": true, "maxProperties": true,
	"title": true, "description": true, "default": true, "deprecated": true, "examples": true, "readOnly": true, "writeOnly": true,
	"format":          true,
	"x-atlas-logical": true, "x-atlas-key": true, "x-atlas-aliases": true, "x-atlas-reference": true,
	"x-atlas-deprecated": true, "x-atlas-unit": true, "x-atlas-enum": true,
}

// refSiblings are what may stand beside $ref: prose, and the Atlas keywords
// that describe a property rather than its type (assumption A-012).
var refSiblings = map[string]bool{
	"$ref": true, "$comment": true, "title": true, "description": true, "default": true, "deprecated": true,
	"examples": true, "readOnly": true, "writeOnly": true,
	"x-atlas-key": true, "x-atlas-aliases": true, "x-atlas-reference": true, "x-atlas-deprecated": true,
}

var jsonTypes = map[string]bool{"boolean": true, "object": true, "array": true, "number": true, "string": true, "integer": true}

// Violation is one way a schema leaves the profile.
type Violation struct {
	Pointer string `json:"pointer"` // into the schema (RFC 6901)
	Rule    string `json:"rule"`    // the keyword, or "profile" for a rule of §9.2.1
	Message string `json:"message"`
}

// Error is a schema that leaves the profile: every violation, in order.
type Error struct{ Violations []Violation }

func (e *Error) Error() string {
	var b strings.Builder
	for i, v := range e.Violations {
		if i == 3 {
			fmt.Fprintf(&b, "; and %d more", len(e.Violations)-3)
			break
		}
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s at %q: %s", v.Rule, v.Pointer, v.Message)
	}
	return "not an Atlas schema: " + b.String()
}

// Schema is a compiled schema, or one of its subschemas.
type Schema struct {
	n *node
}

type node struct {
	ptr     string
	boolean *bool

	typed    bool     // the schema has a type keyword
	types    []string // its types besides "null"
	nullable bool

	enum     []ijson.Value
	constV   ijson.Value
	hasConst bool

	props      map[string]*node
	order      []string
	required   []string
	additional *node
	items      *node
	prefix     []*node
	anyOf      []*node
	oneOf      []*node

	minimum, maximum, exMinimum, exMaximum *float64
	multipleOf                             float64

	minLength, maxLength int
	patterns             []*regexp.Regexp
	formats              []string
	decimal              *logical

	minItems, maxItems int
	uniqueItems        bool
	minProps, maxProps int

	ref    string
	target *node

	logical     *logical
	unit        units.Unit
	key         bool
	aliases     []string
	enumAliases map[string]string
	enumValues  []string // x-atlas-enum's values, in its order
	deprecated  bool
	defaultV    ijson.Value
	hasDefault  bool
	readOnly    bool
	writeOnly   bool
	reference   string

	source ijson.Value
}

func newNode(ptr string) *node {
	return &node{ptr: ptr, minLength: -1, maxLength: -1, minItems: -1, maxItems: -1, minProps: -1, maxProps: -1}
}

// Options are what a schema's place adds to its rules.
type Options struct {
	// FreeForm are the pointers of members a request leaves open by nature:
	// atlas.compare's request and atlas.settings' settings (Q-PROF-9).
	FreeForm []string
	// Unchecked compiles a schema without holding it to the profile's
	// rules, for validating with 2020-12's semantics alone: every keyword
	// still applies, and only a schema that cannot be applied is refused.
	Unchecked bool
}

type compiler struct {
	kind       Kind
	opt        Options
	violations []Violation
	defs       map[string]*node
	defSources map[string]ijson.Value
	tooDeep    bool
}

// fail records a rule of the profile the schema breaks.
func (c *compiler) fail(ptr, rule, format string, args ...any) {
	if c.opt.Unchecked {
		return
	}
	c.violations = append(c.violations, Violation{ptr, rule, fmt.Sprintf(format, args...)})
}

// fatal records what makes a schema impossible to apply at all.
func (c *compiler) fatal(ptr, rule, format string, args ...any) {
	c.violations = append(c.violations, Violation{ptr, rule, fmt.Sprintf(format, args...)})
}

// Compile holds a schema to the Atlas Schema profile and compiles it. A
// schema that leaves the profile returns an *Error with every violation,
// sorted by pointer, so its first is the problem's target.
func Compile(v ijson.Value, kind Kind) (*Schema, error) { return CompileWith(v, kind, Options{}) }

// CompileWith is Compile for a schema whose place exempts some of its
// members.
func CompileWith(v ijson.Value, kind Kind, opt Options) (*Schema, error) {
	c := &compiler{kind: kind, opt: opt, defs: map[string]*node{}, defSources: map[string]ijson.Value{}}
	if b, err := ijson.Canonical(v); err != nil || len(b) > MaxSize {
		c.fail("", "size", "a schema is at most %d bytes as RFC 8785 writes it (Q-PROF-12)", MaxSize)
		return nil, &Error{c.violations}
	}
	root, ok := v.(*ijson.Object)
	if !ok {
		c.fatal("", "profile", "a schema's root is an object")
		return nil, &Error{c.violations}
	}
	if s, _ := root.Get("$schema"); s != Dialect {
		c.fail("/$schema", "$schema", "a schema declares %q", Dialect)
	}
	c.checkVocabulary(root)
	if id, ok := root.Get("$id"); ok {
		if s, isString := id.(string); !isString || checkURI(s) != nil {
			c.fail("/$id", "$id", "$id is an absolute URI")
		}
	}
	if defs, ok := root.Get("$defs"); ok {
		m, isObject := defs.(*ijson.Object)
		if !isObject {
			c.fail("/$defs", "$defs", "$defs is an object of schemas")
		} else {
			m.Each(func(name string, d ijson.Value) bool {
				c.defSources[name] = d
				return true
			})
			m.Each(func(name string, d ijson.Value) bool {
				c.defs[name] = c.walk(d, "/$defs/"+escape(name), 2, false)
				return true
			})
		}
	}
	n := c.walk(root, "", 1, false)
	c.resolveRefs(n)
	if len(c.violations) == 0 {
		c.checkDefaults(n, map[*node]bool{})
	}
	if len(c.violations) > 0 {
		sort.SliceStable(c.violations, func(i, j int) bool { return c.violations[i].Pointer < c.violations[j].Pointer })
		return nil, &Error{c.violations}
	}
	return &Schema{n}, nil
}

// MustCompile is Compile for schemas known to be valid: a defect panics.
func MustCompile(v ijson.Value, kind Kind) *Schema {
	s, err := Compile(v, kind)
	if err != nil {
		panic(err)
	}
	return s
}

func (c *compiler) checkVocabulary(root *ijson.Object) {
	v, _ := root.Get("$vocabulary")
	m, ok := v.(*ijson.Object)
	if ok && m.Len() == len(vocabularies) {
		m.Each(func(uri string, b ijson.Value) bool {
			want, known := vocabularies[uri]
			ok = ok && known && b == want
			return ok
		})
	}
	if !ok {
		c.fail("/$vocabulary", "$vocabulary", "a schema declares exactly the vocabularies of §9.2.1")
	}
}

func escape(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(name, "~", "~0"), "/", "~1")
}

// walk compiles one schema position. boolOK says whether a boolean schema
// may stand there: only as items or additionalProperties.
func (c *compiler) walk(v ijson.Value, ptr string, depth int, boolOK bool) *node {
	n := newNode(ptr)
	n.source = v
	if depth > MaxDepth {
		if !c.tooDeep {
			c.fail(ptr, "depth", "a schema nests at most %d levels", MaxDepth)
			c.tooDeep = true
		}
		return n
	}
	if b, ok := v.(bool); ok {
		if !boolOK {
			c.fail(ptr, "boolean-schema", "a boolean schema stands only as items or additionalProperties")
		}
		n.boolean = &b
		return n
	}
	o, ok := v.(*ijson.Object)
	if !ok {
		c.fatal(ptr, "profile", "a schema is an object")
		return n
	}
	root := ptr == ""
	o.Each(func(k string, _ ijson.Value) bool {
		switch {
		case !keywords[k]:
			c.fail(ptr+"/"+escape(k), k, "%q is not a keyword of the Atlas Schema profile", k)
		case !root && (k == "$schema" || k == "$vocabulary" || k == "$id" || k == "$defs"):
			c.fail(ptr+"/"+escape(k), k, "%s stands only at a schema's root", k)
		}
		return true
	})
	if r, ok := o.Get("$ref"); ok {
		c.ref(n, o, r)
	}
	c.typeOf(n, o)
	c.meta(n, o)
	c.validation(n, o)
	c.applicators(n, o, depth)
	c.atlas(n, o)
	c.closedness(n, o)
	return n
}

func (c *compiler) ref(n *node, o *ijson.Object, r ijson.Value) {
	s, _ := r.(string)
	name, ok := strings.CutPrefix(s, "#/$defs/")
	if !ok || name == "" || strings.ContainsAny(name, "/~") {
		c.fatal(n.ptr+"/$ref", "$ref", "$ref is #/$defs/<name>")
		return
	}
	if _, defined := c.defSources[name]; !defined {
		c.fatal(n.ptr+"/$ref", "$ref", "no $defs entry %q", name)
		return
	}
	n.ref = name
	o.Each(func(k string, _ ijson.Value) bool {
		if !refSiblings[k] {
			c.fail(n.ptr+"/"+escape(k), "$ref", "%s does not stand beside $ref: the definition holds it (Q-PROF-2)", k)
		}
		return true
	})
}

// typeOf reads type. The profile allows one type, or [T, "null"] (§9.2.1
// rule 4); 2020-12 allows any list, which an unchecked schema keeps.
func (c *compiler) typeOf(n *node, o *ijson.Object) {
	t, ok := o.Get("type")
	if !ok {
		_, hasAny := o.Get("anyOf")
		_, hasOne := o.Get("oneOf")
		if n.ref == "" && !hasAny && !hasOne {
			c.fail(n.ptr+"/type", "type", "every schema has a type")
		}
		return
	}
	var names []string
	switch x := t.(type) {
	case string:
		names = []string{x}
	case []ijson.Value:
		for _, e := range x {
			s, _ := e.(string)
			names = append(names, s)
		}
	default:
		c.fatal(n.ptr+"/type", "type", `type is a type name or a list of them`)
		return
	}
	n.typed = true
	for _, name := range names {
		switch {
		case name == "null":
			n.nullable = true
		case jsonTypes[name]:
			n.types = append(n.types, name)
		default:
			c.fatal(n.ptr+"/type", "type", "%q is not a type", name)
		}
	}
	switch x := t.(type) {
	case string:
		if x == "null" {
			c.fail(n.ptr+"/type", "type", "null alone is not a type of the profile")
		}
	case []ijson.Value:
		if len(x) != 2 || x[1] != "null" || x[0] == "null" {
			c.fail(n.ptr+"/type", "type", `a type array is [T, "null"]`)
		}
	}
}

// is reports whether the schema's one type, besides null, is t.
func (n *node) is(t string) bool { return len(n.types) == 1 && n.types[0] == t }

func (n *node) isNumeric() bool { return n.is("number") || n.is("integer") }

func (c *compiler) meta(n *node, o *ijson.Object) {
	for _, k := range []string{"title", "description", "$comment"} {
		if v, ok := o.Get(k); ok {
			if _, isString := v.(string); !isString {
				c.fail(n.ptr+"/"+k, k, "%s is a string", k)
			}
		}
	}
	for _, k := range []string{"deprecated", "readOnly", "writeOnly"} {
		if v, ok := o.Get(k); ok {
			b, isBool := v.(bool)
			if !isBool {
				c.fail(n.ptr+"/"+k, k, "%s is a boolean", k)
			}
			switch k {
			case "deprecated":
				n.deprecated = b
			case "readOnly":
				n.readOnly = b
			case "writeOnly":
				n.writeOnly = b
			}
		}
	}
	if v, ok := o.Get("examples"); ok {
		if _, isArray := v.([]ijson.Value); !isArray {
			c.fail(n.ptr+"/examples", "examples", "examples is an array")
		}
	}
	if v, ok := o.Get("default"); ok {
		n.defaultV, n.hasDefault = v, true
	}
}

func (c *compiler) validation(n *node, o *ijson.Object) {
	if v, ok := o.Get("enum"); ok {
		list, isArray := v.([]ijson.Value)
		if !isArray || len(list) == 0 {
			c.fail(n.ptr+"/enum", "enum", "enum is a non-empty array")
		} else {
			for i, e := range list {
				for _, prev := range list[:i] {
					if ijson.Equal(prev, e) {
						c.fail(n.ptr+"/enum", "enum", "enum's values are unique: %d repeats one", i)
					}
				}
				if !n.admits(e) {
					c.fail(fmt.Sprintf("%s/enum/%d", n.ptr, i), "enum", "an enum value is of the schema's type")
				}
			}
			n.enum = list
		}
	}
	if v, ok := o.Get("const"); ok {
		if !n.admits(v) {
			c.fail(n.ptr+"/const", "const", "const is of the schema's type")
		}
		n.constV, n.hasConst = v, true
	}
	for _, k := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum"} {
		v, ok := o.Get(k)
		if !ok {
			continue
		}
		num, isNumber := v.(ijson.Number)
		f, err := num.Float64()
		if !isNumber || err != nil {
			c.fail(n.ptr+"/"+k, k, "%s is a number", k)
			continue
		}
		if !n.isNumeric() {
			c.fail(n.ptr+"/"+k, k, "%s stands only on a number or an integer", k)
		}
		switch k {
		case "minimum":
			n.minimum = &f
		case "maximum":
			n.maximum = &f
		case "exclusiveMinimum":
			n.exMinimum = &f
		case "exclusiveMaximum":
			n.exMaximum = &f
		}
	}
	if v, ok := o.Get("multipleOf"); ok {
		num, isNumber := v.(ijson.Number)
		f, err := num.Float64()
		switch {
		case !isNumber || err != nil || f <= 0:
			c.fatal(n.ptr+"/multipleOf", "multipleOf", "multipleOf is a positive number")
		case !num.SafeInteger() || !n.is("integer"):
			c.fail(n.ptr+"/multipleOf", "multipleOf", "multipleOf is a positive integer, on an integer (Q-PROF-8)")
			n.multipleOf = f
		default:
			n.multipleOf = f
		}
	}
	n.minLength = c.count(n, o, "minLength", "string")
	n.maxLength = c.count(n, o, "maxLength", "string")
	n.minItems = c.count(n, o, "minItems", "array")
	n.maxItems = c.count(n, o, "maxItems", "array")
	n.minProps = c.count(n, o, "minProperties", "object")
	n.maxProps = c.count(n, o, "maxProperties", "object")
	if v, ok := o.Get("uniqueItems"); ok {
		b, isBool := v.(bool)
		if !isBool || !n.is("array") {
			c.fail(n.ptr+"/uniqueItems", "uniqueItems", "uniqueItems is a boolean, on an array")
		}
		n.uniqueItems = b
	}
	if v, ok := o.Get("pattern"); ok {
		s, isString := v.(string)
		re, err := compilePattern(s)
		switch {
		case !isString:
			c.fatal(n.ptr+"/pattern", "pattern", "pattern is a string")
		case err != nil:
			c.fatal(n.ptr+"/pattern", "pattern", "%v", err)
		default:
			n.patterns = append(n.patterns, re)
			if !n.is("string") {
				c.fail(n.ptr+"/pattern", "pattern", "pattern stands only on a string")
			}
		}
	}
	if v, ok := o.Get("format"); ok {
		s, _ := v.(string)
		switch {
		case !formats[s]:
			c.fail(n.ptr+"/format", "format", "%q is not a format of §9.2.2", s)
		default:
			n.formats = append(n.formats, s)
			if !n.is("string") {
				c.fail(n.ptr+"/format", "format", "format stands only on a string")
			}
		}
	}
	if v, ok := o.Get("required"); ok {
		list, isArray := v.([]ijson.Value)
		if !isArray {
			c.fail(n.ptr+"/required", "required", "required is an array of names")
		}
		for i, e := range list {
			s, isString := e.(string)
			if !isString || slices.Contains(n.required, s) {
				c.fail(fmt.Sprintf("%s/required/%d", n.ptr, i), "required", "required holds unique names")
				continue
			}
			n.required = append(n.required, s)
		}
		if !n.is("object") {
			c.fail(n.ptr+"/required", "required", "required stands only on an object")
		}
	}
}

func (c *compiler) count(n *node, o *ijson.Object, k, on string) int {
	v, ok := o.Get(k)
	if !ok {
		return -1
	}
	num, isNumber := v.(ijson.Number)
	i, isInt := num.Int64()
	if !isNumber || !isInt || i < 0 || i > 1<<31 {
		c.fail(n.ptr+"/"+k, k, "%s is a non-negative integer", k)
		return -1
	}
	if !n.is(on) {
		c.fail(n.ptr+"/"+k, k, "%s stands only on %s %s", k, article(on), on)
	}
	return int(i)
}

func article(t string) string {
	if t == "array" || t == "object" || t == "integer" {
		return "an"
	}
	return "a"
}

// admits reports whether a value is of one of the schema's declared types.
func (n *node) admits(v ijson.Value) bool {
	if !n.typed {
		return true
	}
	if v == nil {
		return n.nullable
	}
	return slices.ContainsFunc(n.types, func(t string) bool { return typeMatches(t, v) })
}

func typeMatches(t string, v ijson.Value) bool {
	switch x := v.(type) {
	case bool:
		return t == "boolean"
	case string:
		return t == "string"
	case ijson.Number:
		return t == "number" || t == "integer" && x.SafeInteger()
	case []ijson.Value:
		return t == "array"
	case *ijson.Object:
		return t == "object"
	}
	return false
}

func (c *compiler) applicators(n *node, o *ijson.Object, depth int) {
	if v, ok := o.Get("properties"); ok {
		m, isObject := v.(*ijson.Object)
		if isObject && !n.is("object") {
			c.fail(n.ptr+"/properties", "properties", "properties stands only on an object")
		}
		if !isObject {
			c.fatal(n.ptr+"/properties", "properties", "properties is an object of schemas")
		} else {
			n.props = map[string]*node{}
			m.Each(func(name string, s ijson.Value) bool {
				if err := grammar.PropertyName(name); err != nil {
					c.fail(n.ptr+"/properties/"+escape(name), "properties", "%v", err)
				}
				n.props[name] = c.walk(s, n.ptr+"/properties/"+escape(name), depth+1, false)
				n.order = append(n.order, name)
				return true
			})
		}
	}
	for i, r := range n.required {
		if _, ok := n.props[r]; !ok {
			c.fail(fmt.Sprintf("%s/required/%d", n.ptr, i), "required", "%q is not one of the properties", r)
		}
	}
	if v, ok := o.Get("additionalProperties"); ok {
		if !n.is("object") {
			c.fail(n.ptr+"/additionalProperties", "additionalProperties", "additionalProperties stands only on an object")
		}
		n.additional = c.walk(v, n.ptr+"/additionalProperties", depth+1, true)
	}
	if v, ok := o.Get("items"); ok {
		if !n.is("array") {
			c.fail(n.ptr+"/items", "items", "items stands only on an array")
		}
		n.items = c.walk(v, n.ptr+"/items", depth+1, true)
	}
	if v, ok := o.Get("prefixItems"); ok {
		list, isArray := v.([]ijson.Value)
		if !isArray || len(list) == 0 || !n.is("array") {
			c.fail(n.ptr+"/prefixItems", "prefixItems", "prefixItems is a non-empty array of schemas, on an array")
		}
		for i, s := range list {
			n.prefix = append(n.prefix, c.walk(s, fmt.Sprintf("%s/prefixItems/%d", n.ptr, i), depth+1, false))
		}
	}
	for _, k := range []string{"anyOf", "oneOf"} {
		v, ok := o.Get(k)
		if !ok {
			continue
		}
		list, isArray := v.([]ijson.Value)
		if !isArray || len(list) == 0 {
			c.fail(n.ptr+"/"+k, k, "%s is a non-empty array of schemas", k)
			continue
		}
		var branches []*node
		for i, s := range list {
			branches = append(branches, c.walk(s, fmt.Sprintf("%s/%s/%d", n.ptr, k, i), depth+1, false))
		}
		if k == "anyOf" {
			n.anyOf = branches
		} else {
			n.oneOf = branches
			c.discriminator(n)
		}
	}
	c.properties(n)
}

// discriminator checks oneOf's rule: one property, required in every branch,
// whose const tells the branches apart (§9.2.1).
func (c *compiler) discriminator(n *node) {
	first := n.oneOf[0]
	for _, name := range first.order {
		seen := map[string]bool{}
		ok := true
		for _, b := range n.oneOf {
			p, has := b.props[name]
			s, isString := p.constOf()
			if !has || !isString || !slices.Contains(b.required, name) || seen[s] {
				ok = false
				break
			}
			seen[s] = true
		}
		if ok {
			return
		}
	}
	c.fail(n.ptr+"/oneOf", "oneOf", "every branch of oneOf has the same required property with its own const")
}

func (n *node) constOf() (string, bool) {
	if n == nil || !n.hasConst {
		return "", false
	}
	s, ok := n.constV.(string)
	return s, ok
}

// properties checks what an object's properties say together: keys,
// aliases.
func (c *compiler) properties(n *node) {
	claimed := map[string]string{}
	for _, name := range n.order {
		claimed[name] = name
	}
	for _, name := range n.order {
		p := n.props[name]
		if p.key {
			// An identifier that may be missing (an Isin a holding lacks) is
			// exempt, as §3.1.3's isin is: the key is then the set of its
			// non-null values.
			nullable := p.nullable || p.target != nil && p.target.nullable
			identifier := p.logical != nil && p.logical.def().identifier
			if !identifier && (!slices.Contains(n.required, name) || nullable) {
				c.fail(p.ptr+"/x-atlas-key", "x-atlas-key", "a key property is required and not nullable, unless it is an identifier that may be missing")
			}
		}
		for _, a := range p.aliases {
			if other, taken := claimed[a]; taken {
				c.fail(p.ptr+"/x-atlas-aliases", "x-atlas-aliases", "%q is already %q", a, other)
				continue
			}
			claimed[a] = name
		}
	}
}

func (c *compiler) atlas(n *node, o *ijson.Object) {
	if v, ok := o.Get("x-atlas-logical"); ok {
		s, _ := v.(string)
		l, err := parseLogical(s)
		if err != nil {
			c.fatal(n.ptr+"/x-atlas-logical", "x-atlas-logical", "%v", err)
		} else {
			n.logical = &l
			c.applyLogical(n, o, l)
		}
	}
	if v, ok := o.Get("x-atlas-key"); ok {
		switch {
		case v != true:
			c.fail(n.ptr+"/x-atlas-key", "x-atlas-key", "x-atlas-key is true")
		case c.kind == Request:
			c.fail(n.ptr+"/x-atlas-key", "x-atlas-key", "a key is part of a result row, not a request")
		}
		n.key = true
	}
	if v, ok := o.Get("x-atlas-aliases"); ok {
		list, isArray := v.([]ijson.Value)
		if !isArray || len(list) == 0 {
			c.fail(n.ptr+"/x-atlas-aliases", "x-atlas-aliases", "x-atlas-aliases is a non-empty array of property names")
		}
		for i, e := range list {
			s, _ := e.(string)
			if grammar.PropertyName(s) != nil || slices.Contains(n.aliases, s) {
				c.fail(n.ptr+"/x-atlas-aliases", "x-atlas-aliases", "alias %d is a property name, once", i)
				continue
			}
			n.aliases = append(n.aliases, s)
		}
	}
	if v, ok := o.Get("x-atlas-reference"); ok {
		s, _ := v.(string)
		if _, err := grammar.ParsePinned(s); err != nil {
			c.fail(n.ptr+"/x-atlas-reference", "x-atlas-reference", "x-atlas-reference is a pinned-id")
		}
		n.reference = s
	}
	if v, ok := o.Get("x-atlas-deprecated"); ok {
		c.deprecation(n, v)
	}
	if v, ok := o.Get("x-atlas-unit"); ok {
		s, _ := v.(string)
		u, err := units.Parse(s)
		switch {
		case err != nil:
			c.fail(n.ptr+"/x-atlas-unit", "x-atlas-unit", "%v", err)
		case !n.isNumeric():
			c.fail(n.ptr+"/x-atlas-unit", "x-atlas-unit", "x-atlas-unit stands only on a number or an integer")
		case n.unit != "" && n.unit != u:
			c.fail(n.ptr+"/x-atlas-unit", "x-atlas-unit", "the logical type is in %s, not %s", n.unit, u)
		default:
			n.unit = u
		}
	}
	c.enums(n, o)
}

// applyLogical checks a logical type's base type and applies its
// constraint as if written as its own keywords (§9.2.5 step 2).
func (c *compiler) applyLogical(n *node, o *ijson.Object, l logical) {
	d := l.def()
	if !n.is(d.base) && !(d.base == "number" && n.is("integer")) {
		c.fail(n.ptr+"/x-atlas-logical", "x-atlas-logical", "%s is a %s", l, d.base)
	}
	if u, ok := units.OfLogical(l.name); ok {
		n.unit = u
	}
	if d.pattern != "" {
		n.patterns = append(n.patterns, regexp.MustCompile(mustTranslate(d.pattern)))
	}
	if d.minLength > 0 && n.minLength < d.minLength {
		n.minLength = d.minLength
	}
	if d.format != "" {
		if f, ok := o.Get("format"); ok && f != d.format {
			c.fail(n.ptr+"/format", "format", "%s has format %s", l, d.format)
		}
		if !slices.Contains(n.formats, d.format) {
			n.formats = append(n.formats, d.format)
		}
	}
	if l.name == "Decimal" {
		n.decimal = &l
	}
	if l.name == "Money" {
		c.money(n)
	}
}

func mustTranslate(pattern string) string {
	re, err := compilePattern(pattern)
	if err != nil {
		panic(err)
	}
	return re.String()
}

// money gives a Money schema its two members, amount a Decimal(38,6) and
// currency a Currency, both required. Its own properties may only be
// those two (assumption A-012).
func (c *compiler) money(n *node) {
	for _, name := range n.order {
		if name != "amount" && name != "currency" {
			c.fail(n.ptr+"/properties/"+escape(name), "x-atlas-logical", "Money has only amount and currency")
		}
	}
	if n.props == nil {
		n.props = map[string]*node{}
	}
	amount := newNode(n.ptr + "/x-atlas-logical")
	amount.typed, amount.types = true, []string{"string"}
	amount.formats = []string{"decimal"}
	amount.decimal = &logical{name: "Decimal", precision: 38, scale: 6}
	amount.logical = amount.decimal
	currency := newNode(n.ptr + "/x-atlas-logical")
	currency.typed, currency.types = true, []string{"string"}
	currency.patterns = []*regexp.Regexp{regexp.MustCompile(mustTranslate(logicals["Currency"].pattern))}
	currency.logical = &logical{name: "Currency"}
	for name, implied := range map[string]*node{"amount": amount, "currency": currency} {
		if own, ok := n.props[name]; ok {
			own.mergeImplied(implied)
		} else {
			n.props[name] = implied
			n.order = append(n.order, name)
		}
		if !slices.Contains(n.required, name) {
			n.required = append(n.required, name)
		}
	}
	sort.SliceStable(n.order, func(i, j int) bool { return n.order[i] == "amount" && n.order[j] != "amount" })
}

func (n *node) mergeImplied(implied *node) {
	n.patterns = append(n.patterns, implied.patterns...)
	for _, f := range implied.formats {
		if !slices.Contains(n.formats, f) {
			n.formats = append(n.formats, f)
		}
	}
	if implied.decimal != nil {
		n.decimal = implied.decimal
	}
	if n.logical == nil {
		n.logical = implied.logical
	}
}

// deprecation checks x-atlas-deprecated: null (not deprecated), or all
// four of reason, since, sunset and replaced_by, since a deprecation carries
// a reason, a replacement and a sunset (§3.3.9, Q-PROF-11).
func (c *compiler) deprecation(n *node, v ijson.Value) {
	if v == nil {
		return
	}
	o, ok := v.(*ijson.Object)
	if !ok {
		c.fail(n.ptr+"/x-atlas-deprecated", "x-atlas-deprecated", "x-atlas-deprecated is an object, or null")
		return
	}
	n.deprecated = true
	ptr := n.ptr + "/x-atlas-deprecated"
	o.Each(func(k string, m ijson.Value) bool {
		s, isString := m.(string)
		switch k {
		case "reason":
			if !isString {
				c.fail(ptr, "x-atlas-deprecated", "reason is a string")
			}
		case "since", "sunset":
			if _, err := grammar.ParseDate(s); !isString || err != nil {
				c.fail(ptr, "x-atlas-deprecated", "%s is a date", k)
			}
		case "replaced_by":
			if m != nil && (!isString || grammar.PropertyName(s) != nil) {
				c.fail(ptr, "x-atlas-deprecated", "replaced_by is a property name, or null")
			}
		default:
			c.fail(ptr, "x-atlas-deprecated", "x-atlas-deprecated has reason, since, sunset and replaced_by")
		}
		return true
	})
	for _, k := range []string{"reason", "since", "sunset", "replaced_by"} {
		if _, has := o.Get(k); !has {
			c.fail(ptr, "x-atlas-deprecated", "x-atlas-deprecated has a %s", k)
		}
	}
}

// enums checks x-atlas-enum and the UNKNOWN rule (§9.2.3).
func (c *compiler) enums(n *node, o *ijson.Object) {
	var values []string
	for _, e := range n.enum {
		if s, ok := e.(string); ok {
			values = append(values, s)
		}
	}
	v, has := o.Get("x-atlas-enum")
	if has && (!n.is("string") || n.enum == nil) {
		c.fail(n.ptr+"/x-atlas-enum", "x-atlas-enum", "x-atlas-enum stands only on a string with enum")
		return
	}
	if !n.is("string") || n.enum == nil {
		return
	}
	unknown := slices.Contains(values, "UNKNOWN")
	if c.kind == Request && unknown {
		c.fail(n.ptr+"/enum", "enum", "a request schema does not accept UNKNOWN")
	}
	if c.kind == Open && !unknown {
		c.fail(n.ptr+"/enum", "enum", "a string enum includes UNKNOWN")
	}
	if !has {
		if c.kind == Open {
			c.fail(n.ptr+"/x-atlas-enum", "x-atlas-enum", "a string enum has x-atlas-enum, with UNKNOWN")
		}
		return
	}
	list, ok := v.([]ijson.Value)
	if !ok {
		c.fail(n.ptr+"/x-atlas-enum", "x-atlas-enum", "x-atlas-enum is an array")
		return
	}
	n.enumAliases = map[string]string{}
	for i, e := range list {
		ptr := fmt.Sprintf("%s/x-atlas-enum/%d", n.ptr, i)
		eo, isObject := e.(*ijson.Object)
		if !isObject {
			c.fail(ptr, "x-atlas-enum", "each entry is an object")
			continue
		}
		value, _ := eo.Get("value")
		s, isString := value.(string)
		if !isString || !slices.Contains(values, s) || slices.Contains(n.enumValues, s) {
			c.fail(ptr+"/value", "x-atlas-enum", "each entry's value is one of enum's, once")
		} else {
			n.enumValues = append(n.enumValues, s)
		}
		eo.Each(func(k string, m ijson.Value) bool {
			switch k {
			case "value":
			case "description":
				if _, ok := m.(string); !ok {
					c.fail(ptr+"/description", "x-atlas-enum", "description is a string")
				}
			case "deprecated":
				if _, ok := m.(bool); !ok {
					c.fail(ptr+"/deprecated", "x-atlas-enum", "deprecated is a boolean")
				}
			case "aliases":
				aliases, ok := m.([]ijson.Value)
				if !ok {
					c.fail(ptr+"/aliases", "x-atlas-enum", "aliases is an array of strings")
					break
				}
				for j, a := range aliases {
					as, ok := a.(string)
					_, taken := n.enumAliases[as]
					if !ok || taken || slices.Contains(values, as) {
						c.fail(fmt.Sprintf("%s/aliases/%d", ptr, j), "x-atlas-enum", "an alias is a string that is no value and no other alias")
						continue
					}
					n.enumAliases[as] = s
				}
			default:
				c.fail(ptr+"/"+escape(k), "x-atlas-enum", "an entry has value, description, aliases and deprecated")
			}
			return true
		})
	}
	if len(n.enumValues) != len(values) {
		c.fail(n.ptr+"/x-atlas-enum", "x-atlas-enum", "x-atlas-enum describes every value of enum")
	}
}

// closedness is rule 6 of §9.2.1: a request's objects are closed, every
// other schema's are open. An object without properties may be a map,
// whose additionalProperties is a schema (assumption A-012).
func (c *compiler) closedness(n *node, o *ijson.Object) {
	if !n.is("object") {
		return
	}
	ap, has := o.Get("additionalProperties")
	_, map_ := ap.(*ijson.Object)
	_, hasProps := o.Get("properties")
	if slices.Contains(c.opt.FreeForm, n.ptr) {
		return
	}
	switch {
	case c.kind == Request && ap != false && !(map_ && !hasProps):
		c.fail(n.ptr+"/additionalProperties", "additionalProperties", `every object of a request schema is closed: "additionalProperties": false`)
	case c.kind == Open && has && ap != true && !(map_ && !hasProps):
		c.fail(n.ptr+"/additionalProperties", "additionalProperties", "a schema other than a request's is open: no additionalProperties, or true")
	}
}

// resolveRefs links every $ref to its definition and refuses a definition
// that leads back to itself: atlas/1.0 schemas are not recursive
// (assumption A-012).
func (c *compiler) resolveRefs(root *node) {
	var visit func(n *node, path map[string]bool)
	seen := map[*node]bool{}
	visit = func(n *node, path map[string]bool) {
		if n == nil {
			return
		}
		if n.ref != "" {
			if path[n.ref] {
				c.fail("/$defs/"+escape(n.ref), "$ref", "the definition %s leads back to itself: atlas/1.0 schemas are not recursive (Q-PROF-1)", n.ref)
				return
			}
			n.target = c.defs[n.ref]
			path[n.ref] = true
			visit(n.target, path)
			delete(path, n.ref)
		}
		if seen[n] && n.ref == "" {
			return
		}
		seen[n] = true
		for _, name := range n.order {
			visit(n.props[name], path)
		}
		visit(n.additional, path)
		visit(n.items, path)
		for _, b := range slices.Concat(n.prefix, n.anyOf, n.oneOf) {
			visit(b, path)
		}
	}
	for name, d := range c.defs {
		visit(d, map[string]bool{name: true})
	}
	visit(root, map[string]bool{})
}

// checkDefaults refuses a default that its own schema refuses: resolution
// fills absent members with it (§9.5.2 step 8).
func (c *compiler) checkDefaults(n *node, seen map[*node]bool) {
	if n == nil || seen[n] {
		return
	}
	seen[n] = true
	if n.hasDefault {
		var fs []Failure
		n.validate(n.defaultV, "", &fs)
		if len(fs) > 0 {
			c.fail(n.ptr+"/default", "default", "the default is invalid: %s %s", fs[0].Keyword, fs[0].Message)
		}
	}
	for _, name := range n.order {
		c.checkDefaults(n.props[name], seen)
	}
	c.checkDefaults(n.additional, seen)
	c.checkDefaults(n.items, seen)
	c.checkDefaults(n.target, seen)
	for _, b := range slices.Concat(n.prefix, n.anyOf, n.oneOf) {
		c.checkDefaults(b, seen)
	}
}
