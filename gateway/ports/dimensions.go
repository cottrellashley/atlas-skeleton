package ports

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"atlas.local/sdk/go/protocol/grammar"
	"atlas.local/sdk/go/protocol/schema"
)

// A Dimension is a kind of data a policy's scope grants values of, and the
// gateway enforces: a portfolio, an index, a company's workspace or desk
// (the access-dimension amendment, ASSUMPTIONS.md A-200). Registering one
// grants nobody anything: scope comes only from policy, and a dimension a
// caller has no scope in is none of it.
type Dimension struct {
	// Name is how profiles (x-atlas-scope) and policies (scopes) name it.
	Name string
	// Description says what its values are, for discovery.
	Description string
}

// ProtocolDimensions are atlas/1.0's own: one for each of its code types
// (§9.2.3), whose values are codes of them without an annotation.
var ProtocolDimensions = []Dimension{
	{Name: "index", Description: "An index, by its code (IndexCode)."},
	{Name: "portfolio", Description: "A portfolio, by its code (PortfolioCode)."},
}

// logicalDimensions are the protocol's code types, by the dimension each
// names: a typed rule, not a policy language (R05, R071).
var logicalDimensions = map[string]string{
	"PortfolioCode": "portfolio",
	"IndexCode":     "index",
}

// DimensionOf is the dimension a schema's value is a code of, and whether
// scope is enforced on it: the one its x-atlas-scope names, or the one its
// protocol code type names. It is read from the schema alone, so request
// checks, providers, result checks, discovery and whoami all read it the
// same way, and the registry (Dimensions.Check) refuses at start a schema
// whose dimension the deployment does not define.
func DimensionOf(s *schema.Schema) (string, bool) {
	if s == nil {
		return "", false
	}
	if d, ok := s.Scope(); ok {
		return d, true
	}
	if name, _, _, ok := s.Logical(); ok {
		d, scoped := logicalDimensions[name]
		return d, scoped
	}
	return "", false
}

// MaxDimensions is the most dimensions one deployment defines.
const MaxDimensions = 64

// Dimensions is a deployment's registry of dimensions: the protocol's, and
// those its company defines. It is immutable once made, and one registry
// serves the whole gateway.
type Dimensions struct {
	by    map[string]Dimension
	names []string
}

// NewDimensions is the registry of the protocol's dimensions and defined.
// A name that is not a property name, one defined twice, or one that is
// the protocol's, is refused.
func NewDimensions(defined ...Dimension) (*Dimensions, error) {
	d := &Dimensions{by: map[string]Dimension{}}
	var errs []error
	for _, x := range slices.Concat(ProtocolDimensions, defined) {
		switch _, seen := d.by[x.Name]; {
		case grammar.PropertyName(x.Name) != nil || len(x.Name) > schema.MaxDimension:
			errs = append(errs, fmt.Errorf("the dimension %q is not a name: a-z first, then a-z, 0-9 and _, at most %d bytes", x.Name, schema.MaxDimension))
		case seen:
			errs = append(errs, fmt.Errorf("the dimension %q is defined twice, or is the protocol's own", x.Name))
		case strings.TrimSpace(x.Description) == "":
			errs = append(errs, fmt.Errorf("the dimension %q says what its values are", x.Name))
		default:
			d.by[x.Name] = x
			d.names = append(d.names, x.Name)
		}
	}
	if len(d.names) > MaxDimensions {
		errs = append(errs, fmt.Errorf("%d dimensions are defined, more than %d", len(d.names), MaxDimensions))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("ports: the dimensions: %w", err)
	}
	slices.Sort(d.names)
	return d, nil
}

// DefaultDimensions are the protocol's dimensions alone.
func DefaultDimensions() *Dimensions {
	d, err := NewDimensions()
	if err != nil {
		panic(err) // the protocol's own are valid; a test holds them
	}
	return d
}

// Has reports whether the registry defines a dimension.
func (d *Dimensions) Has(name string) bool {
	_, ok := d.by[name]
	return ok
}

// Names are every dimension, sorted.
func (d *Dimensions) Names() []string { return slices.Clone(d.names) }

// Get is a dimension by name.
func (d *Dimensions) Get(name string) (Dimension, bool) {
	x, ok := d.by[name]
	return x, ok
}

// Check is every reason a schema's scope does not hold under the registry,
// each with its pointer:
//   - an x-atlas-scope naming a dimension it does not define;
//   - one beside a protocol code type of another dimension;
//   - one whose schema, its $ref resolved, is not a string;
//   - a scoped value under anyOf, oneOf, prefixItems, additionalProperties,
//     allOf or if/then/else, which the gateway reads its scope through
//     properties and items only, so would serve unenforced.
func (d *Dimensions) Check(s *schema.Schema) []string {
	var out []string
	var visit func(*schema.Schema, string, int)
	visit = func(s *schema.Schema, ptr string, depth int) {
		if s == nil || depth > schema.MaxDepth {
			return
		}
		if name, ok := s.Scope(); ok {
			if !d.Has(name) {
				out = append(out, fmt.Sprintf("%s: x-atlas-scope names %q, which the deployment does not define (it defines %s)", ptr, name, strings.Join(d.names, ", ")))
			}
			if logical, _, _, ok := s.Logical(); ok {
				if other, coded := logicalDimensions[logical]; coded && other != name {
					out = append(out, fmt.Sprintf("%s: x-atlas-scope names %q, but a %s is a code of %q", ptr, name, logical, other))
				}
			}
			if s.Definition().Type() != "string" {
				out = append(out, fmt.Sprintf("%s: x-atlas-scope stands only on a string, a code of its dimension", ptr))
			}
		}
		for _, a := range s.Opaque() {
			if holdsScope(a.Schema, 0) {
				out = append(out, fmt.Sprintf("%s%s: a scoped value under %s, which the gateway does not read scope through: scope a property or an array's items", ptr, a.Pointer, a.Keyword))
			}
		}
		visit(s.Items(), ptr+"/items", depth+1)
		for _, n := range s.Properties() {
			ps, _ := s.Property(n)
			visit(ps, ptr+"/properties/"+n, depth+1)
		}
	}
	visit(s, "", 0)
	return out
}

// holdsScope reports whether a schema holds a scoped value anywhere, through
// any applicator.
func holdsScope(s *schema.Schema, depth int) bool {
	if s == nil || depth > schema.MaxDepth {
		return false
	}
	if _, ok := DimensionOf(s); ok {
		return true
	}
	if holdsScope(s.Items(), depth+1) {
		return true
	}
	for _, n := range s.Properties() {
		if ps, _ := s.Property(n); holdsScope(ps, depth+1) {
			return true
		}
	}
	for _, a := range s.Opaque() {
		if holdsScope(a.Schema, depth+1) {
			return true
		}
	}
	return false
}
