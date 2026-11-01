// Package resolve is §9.5: it turns a record written under one profile of a
// capability into the shape of the profile its reader knows, along the
// chain of profiles between them, by their lenses and the reader's schema.
//
// A Plan is prepared once for a writer and a reader, and then resolves any
// number of records; it is safe for concurrent use.
//
// Under the 2026-10-31 design review (R03, ATLAS-CORE §7,
// docs/pass-2/REVIEW.md), the lens set is an experiment. Clients are
// obliged only to aliases, defaults, UNKNOWN and projection, and the gate
// refuses convert_unit, promote and drop within a version. The whole of
// §9.5 stays here, proved by the references' vectors, for the migrations
// that come to need it.
package resolve

import (
	"fmt"
	"slices"
	"strings"

	"atlas.local/sdk/go/protocol/grammar"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/schema"
)

// Error is a record that cannot be resolved: the problem resolution_failed,
// with its target.
type Error struct {
	Target  string // a JSON Pointer into the record, or "" for the chain
	Message string
}

func (e *Error) Error() string {
	if e.Target == "" {
		return "resolution failed: " + e.Message
	}
	return fmt.Sprintf("resolution failed at %s: %s", e.Target, e.Message)
}

func failf(target, format string, args ...any) *Error {
	return &Error{target, fmt.Sprintf(format, args...)}
}

// Profile is one profile of a chain: its exact id, the lens it declares from
// the profile before it, and its schema for the kind of record resolved (a
// table's row schema, a request schema, an event's data schema).
type Profile struct {
	Exact  grammar.Exact
	Lens   Lens
	Schema *schema.Schema
}

// Plan resolves records written under one profile for a reader of another.
type Plan struct {
	identity bool
	steps    []step
	reader   *schema.Schema
	writer   *schema.Schema
}

// step is one lens operation, the direction it runs in, and the schema of
// the profile it turns the record into.
type step struct {
	op      Op
	forward bool
	into    *schema.Schema
}

// Options are what a call adds to a resolution.
type Options struct {
	// Columns are the columns a request named, as the reader names them: a
	// row is matched against the reader's schema projected to them (§9.5.2
	// step 5).
	Columns []string
	// Dynamic is the trait DynamicSchema: no resolution applies.
	Dynamic bool
}

// New prepares the resolution of records written under writer for a reader
// of reader (§9.5.2 steps 1, 2 and 5). chain holds the capability's
// profiles, oldest first, each with its lens from the one before: at least
// those from the older of the two to the newer.
func New(chain []Profile, writer, reader grammar.Exact, opt Options) (*Plan, error) {
	if writer.Fingerprint == reader.Fingerprint && writer.Pinned == reader.Pinned || opt.Dynamic {
		return &Plan{identity: true}, nil
	}
	if writer.Pinned != reader.Pinned {
		return nil, failf("", "%s and %s are not profiles of one version: only the gateway resolves across versions", writer, reader)
	}
	for _, p := range chain {
		if p.Exact.Pinned != reader.Pinned {
			return nil, failf("", "%s is not in %s's chain", p.Exact, reader.Pinned)
		}
	}
	wi := slices.IndexFunc(chain, func(p Profile) bool { return p.Exact == writer })
	ri := slices.IndexFunc(chain, func(p Profile) bool { return p.Exact == reader })
	if wi < 0 || ri < 0 {
		return nil, failf("", "the chain does not hold both %s and %s", writer, reader)
	}
	// Only the profiles between the two take part (§9.5.1).
	var steps []step
	var r, w Profile
	switch {
	case wi < ri: // the writer is older: forward
		r, w = chain[ri], chain[wi]
		for _, p := range chain[wi+1 : ri+1] {
			for _, op := range p.Lens {
				steps = append(steps, step{op, true, p.Schema})
			}
		}
	default: // the writer is newer: backward
		r, w = chain[ri], chain[wi]
		for k := wi; k > ri; k-- {
			lens := chain[k].Lens
			for i := len(lens) - 1; i >= 0; i-- {
				steps = append(steps, step{lens[i], false, chain[k-1].Schema})
			}
		}
	}
	if r.Schema == nil || r.Schema.Type() != "object" {
		return nil, failf("", "the reader's schema for a record is an object schema")
	}
	s := r.Schema
	if opt.Columns != nil {
		for _, c := range opt.Columns {
			if _, ok := s.Property(c); !ok {
				return nil, failf("/"+c, "%q is not a column of %s", c, reader)
			}
		}
		s = s.Project(opt.Columns)
	}
	return &Plan{steps: steps, reader: s, writer: w.Schema}, nil
}

// Identity reports whether the plan returns every record unchanged.
func (p *Plan) Identity() bool { return p.identity }

// Reader is the (projected) schema records are matched against, or nil for
// an identity plan.
func (p *Plan) Reader() *schema.Schema { return p.reader }

// Record resolves one record (§9.5.2 steps 3 to 15). The record is not
// changed; the result shares no member with it that resolution changed.
func (p *Plan) Record(x *ijson.Object) (*ijson.Object, error) {
	if p.identity {
		return x, nil
	}
	y := shallowCopy(x)
	for _, s := range p.steps {
		if err := s.op.apply(y, s.forward, s.into); err != nil {
			return nil, err
		}
	}
	z, err := match(y, p.reader, "")
	if err != nil {
		return nil, err
	}
	return z, nil
}

func shallowCopy(x *ijson.Object) *ijson.Object {
	y := ijson.NewObject()
	x.Each(func(n string, v ijson.Value) bool {
		y.Set(n, v)
		return true
	})
	return y
}

// match is steps 5 to 15: the record in the reader's shape. A failure's
// target is the top-level member it is in, even when it is deeper (Q-RES-1):
// target is that member's pointer when matching inside it, else "".
func match(y *ijson.Object, s *schema.Schema, target string) (*ijson.Object, error) {
	z := ijson.NewObject()
	for _, name := range s.Properties() {
		ps, _ := s.Property(name)
		at := target
		if at == "" {
			at = "/" + escape(name)
		}
		v, ok := y.Get(name) // step 6
		if !ok {
			aliases := ps.Aliases()
			for i := len(aliases) - 1; i >= 0 && !ok; i-- { // step 7: the last present alias
				v, ok = y.Get(aliases[i])
			}
		}
		if !ok {
			v, ok = ps.Default() // step 8
		}
		if !ok && ps.Nullable() && s.Required(name) {
			v, ok = nil, true // step 9
		}
		if !ok {
			if !s.Required(name) {
				continue // step 10
			}
			return nil, failf(at, "%q is required, and the record has no value for it", name) // step 11
		}
		cv, err := convert(v, ps, at) // step 12
		if err != nil {
			return nil, err
		}
		z.Set(name, cv) // step 13
	}
	return z, nil // steps 14 and 15: what no property took is dropped
}

// convert is §9.5.5: a value in the reader's type.
func convert(v ijson.Value, s *schema.Schema, ptr string) (ijson.Value, error) {
	if s.Valid(v) {
		return v, nil
	}
	switch x := v.(type) {
	case nil:
		if s.Nullable() {
			return nil, nil
		}
	case string:
		if s.Type() == "string" && s.Format() == "date-time" {
			if _, err := grammar.ParseDate(x); err == nil {
				return checked(x+"T00:00:00Z", s, ptr)
			}
		}
		if s.Enum() != nil && !s.EnumHas(x) {
			if value, ok := s.EnumAlias(x); ok {
				return value, nil
			}
			if s.EnumHas("UNKNOWN") {
				return "UNKNOWN", nil
			}
			return nil, failf(ptr, "%q is not one of the reader's values, and the reader has no UNKNOWN", x)
		}
	case *ijson.Object:
		if s.Type() == "object" && len(s.Properties()) > 0 {
			return match(x, s, ptr)
		}
	case []ijson.Value:
		if items := s.Items(); s.Type() == "array" && items != nil {
			out := make([]ijson.Value, len(x))
			for i, e := range x {
				c, err := convert(e, items, ptr)
				if err != nil {
					return nil, err
				}
				out[i] = c
			}
			return out, nil
		}
	}
	return nil, failf(ptr, "the value cannot be read as the reader's %s", describe(s))
}

func checked(v ijson.Value, s *schema.Schema, ptr string) (ijson.Value, error) {
	if !s.Valid(v) {
		return nil, failf(ptr, "the value cannot be read as the reader's %s", describe(s))
	}
	return v, nil
}

func describe(s *schema.Schema) string {
	if name, _, _, ok := s.Logical(); ok {
		return name
	}
	if t := s.Type(); t != "" {
		return t
	}
	return "schema"
}

func escape(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(name, "~", "~0"), "/", "~1")
}

// Fields reports the top-level fields a lens names, for the gate's checks.
func (l Lens) Fields() []string {
	var out []string
	for _, op := range l {
		for _, f := range []string{op.Field, op.from(), op.to()} {
			if f != "" && !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
	}
	return out
}
