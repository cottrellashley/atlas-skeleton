// Package profile reads a capability's profile (§3.1): its identity,
// properties, interactions and bindings, its schemas compiled as the Atlas
// Schema profile holds them, its errors, meaning, compatibility, usage and
// evolution. It is the view the gate, the gateway's registry and the
// clients share.
package profile

import (
	"fmt"
	"slices"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/documents"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/resolve"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

// Profile is one profile, as served.
type Profile struct {
	Doc     *ijson.Object
	ID      string
	Version int
	FP      protocol.FP

	Family, Stability, Lifecycle, Visibility string
	Traits                                   []Trait

	Interactions map[string]*Interaction
	Order        []string // interaction names, as the profile lists them

	Schemas map[string]*schema.Schema
	Kinds   map[string]schema.Kind

	Errors        []protocol.Declared
	Meaning       *ijson.Object
	Compatibility map[string]string
	Usage         *ijson.Object
	Evolution     Evolution
}

// Trait is one trait: its name, and whether a client without it must not
// call the capability (§9.3.2).
type Trait struct {
	Name     string
	Critical bool
}

// Interaction is one verb of the capability.
type Interaction struct {
	Name      string
	Effect    string // read, write, …
	Result    string // table, record, collection, content, …
	Delivery  string
	Execution []string
	Takes     string // the schema it takes, by name
	Returns   string // the schema it returns, by name
	Deadline  Deadline
	Bindings  []Binding
	Encodings []string
}

// Deadline is an interaction's default and maximum deadline.
type Deadline struct{ Default, Max time.Duration }

// Binding is where and how an interaction is served.
type Binding struct {
	Protocol, Server, Method, Address, Auth string
	Encodings                               []string
}

// Evolution is what a profile says of the one before it.
type Evolution struct {
	PreviousProfile *grammar.Exact // within the version
	PreviousVersion string         // a pinned-id, for a version after @v1
	Lens            resolve.Lens
	Changelog       []ijson.Value
}

// Exact is the profile's exact-id.
func (p *Profile) Exact() grammar.Exact {
	return grammar.Exact{Pinned: grammar.Pinned{ID: p.ID, Version: p.Version}, Fingerprint: p.FP.Value}
}

// HasTrait reports whether the profile has a trait.
func (p *Profile) HasTrait(name string) bool {
	return slices.ContainsFunc(p.Traits, func(t Trait) bool { return t.Name == name })
}

// Error is a profile that cannot be read: where, and why.
type Error struct {
	Pointer string
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("profile %s: %s", e.Pointer, e.Message) }

type reader struct {
	errs []error
}

func (r *reader) fail(ptr, format string, args ...any) {
	r.errs = append(r.errs, &Error{ptr, fmt.Sprintf(format, args...)})
}

func (r *reader) object(o *ijson.Object, name, ptr string, required bool) *ijson.Object {
	v, ok := o.Get(name)
	if !ok {
		if required {
			r.fail(ptr+"/"+name, "is required")
		}
		return nil
	}
	m, isObject := v.(*ijson.Object)
	if !isObject {
		r.fail(ptr+"/"+name, "is an object")
	}
	return m
}

func (r *reader) str(o *ijson.Object, name, ptr string, required bool) string {
	if o == nil {
		return ""
	}
	v, ok := o.Get(name)
	if !ok {
		if required {
			r.fail(ptr+"/"+name, "is required")
		}
		return ""
	}
	s, isString := v.(string)
	if !isString {
		r.fail(ptr+"/"+name, "is a string")
	}
	return s
}

func (r *reader) strs(o *ijson.Object, name, ptr string) []string {
	if o == nil {
		return nil
	}
	v, ok := o.Get(name)
	if !ok {
		return nil
	}
	list, isArray := v.([]ijson.Value)
	if !isArray {
		r.fail(ptr+"/"+name, "is an array of strings")
		return nil
	}
	var out []string
	for i, e := range list {
		s, ok := e.(string)
		if !ok {
			r.fail(fmt.Sprintf("%s/%s/%d", ptr, name, i), "is a string")
			continue
		}
		out = append(out, s)
	}
	return out
}

// Parse reads a profile. Its schemas are compiled as requests where an
// interaction takes them and as open schemas otherwise, and its fingerprint
// is computed from it (the one it carries is checked by
// protocol.CheckFingerprint, not here).
func Parse(doc *ijson.Object) (*Profile, error) {
	r := &reader{}
	p := &Profile{Doc: doc, Interactions: map[string]*Interaction{}, Schemas: map[string]*schema.Schema{}, Kinds: map[string]schema.Kind{}, Compatibility: map[string]string{}}
	id := r.object(doc, "identity", "", true)
	p.ID = r.str(id, "id", "/identity", true)
	if p.ID != "" {
		if err := grammar.CapabilityID(p.ID); err != nil {
			r.fail("/identity/id", "%v", err)
		}
	}
	if id != nil {
		v, _ := id.Get("version")
		n, ok := v.(ijson.Number)
		ver, isInt := n.Int64()
		if !ok || !isInt || ver < 1 {
			r.fail("/identity/version", "is a positive integer")
		}
		p.Version = int(ver)
	}
	props := r.object(doc, "properties", "", true)
	p.Family = r.str(props, "family", "/properties", true)
	p.Stability = r.str(props, "stability", "/properties", false)
	p.Lifecycle = r.str(props, "lifecycle", "/properties", false)
	p.Visibility = r.str(props, "visibility", "/properties", false)
	if props != nil {
		if v, ok := props.Get("traits"); ok {
			list, _ := v.([]ijson.Value)
			for i, e := range list {
				t, ok := e.(*ijson.Object)
				if !ok {
					r.fail(fmt.Sprintf("/properties/traits/%d", i), `is {"name", "critical"}`)
					continue
				}
				critical, _ := t.Get("critical")
				p.Traits = append(p.Traits, Trait{Name: r.str(t, "name", fmt.Sprintf("/properties/traits/%d", i), true), Critical: critical == true})
			}
		}
	}
	interactions := r.object(doc, "interactions", "", true)
	if interactions != nil {
		interactions.Each(func(name string, v ijson.Value) bool {
			ptr := "/interactions/" + name
			if err := grammar.Interaction(name); err != nil {
				r.fail(ptr, "%v", err)
			}
			o, ok := v.(*ijson.Object)
			if !ok {
				r.fail(ptr, "is an object")
				return true
			}
			p.Interactions[name] = r.interaction(name, o, ptr)
			p.Order = append(p.Order, name)
			return true
		})
	}
	takes := map[string]bool{}
	for _, in := range p.Interactions {
		if in.Takes != "" {
			takes[in.Takes] = true
		}
	}
	if schemas := r.object(doc, "schemas", "", true); schemas != nil {
		schemas.Each(func(name string, v ijson.Value) bool {
			kind := schema.Open
			if takes[name] {
				kind = schema.Request
			}
			// A built-in's schemas are the protocol's own documents, by
			// reference (§9.3.2): 2020-12 schemas, outside the profile's
			// rules (assumption A-016).
			var opt schema.Options
			if grammar.Builtin(p.ID) {
				opt = schema.Options{Unchecked: true, Documents: documents.Set()}
			}
			s, err := schema.CompileWith(v, kind, opt)
			if err != nil {
				r.fail("/schemas/"+name, "%v", err)
				return true
			}
			p.Schemas[name], p.Kinds[name] = s, kind
			return true
		})
	}
	for _, name := range p.Order {
		in := p.Interactions[name]
		for _, s := range []string{in.Takes, in.Returns} {
			if _, ok := p.Schemas[s]; s != "" && !ok && len(r.errs) == 0 {
				r.fail("/interactions/"+name, "names the schema %q, which the profile does not have", s)
			}
		}
	}
	if v, ok := doc.Get("errors"); ok {
		list, _ := v.([]ijson.Value)
		for i, e := range list {
			ptr := fmt.Sprintf("/errors/%d", i)
			o, ok := e.(*ijson.Object)
			if !ok {
				r.fail(ptr, "is an object")
				continue
			}
			d := protocol.Declared{Code: r.str(o, "code", ptr, true), Description: r.str(o, "description", ptr, false), Next: r.str(o, "next", ptr, false)}
			if err := grammar.ProblemCode(d.Code); err != nil {
				r.fail(ptr+"/code", "%v", err)
			}
			if protocol.Known(d.Code) {
				r.fail(ptr+"/code", "%q is the protocol's own: a capability declares only its own codes", d.Code)
			}
			if st, ok := o.Get("status"); ok {
				n, _ := st.(ijson.Number)
				s, isInt := n.Int64()
				if !isInt || s < 400 || s > 599 {
					r.fail(ptr+"/status", "is an HTTP status from 400 to 599")
				}
				d.Status = int(s)
			}
			retryable, _ := o.Get("retryable")
			d.Retryable = retryable == true
			p.Errors = append(p.Errors, d)
		}
	}
	p.Meaning = r.object(doc, "meaning", "", false)
	if c := r.object(doc, "compatibility", "", false); c != nil {
		c.Each(func(name string, v ijson.Value) bool {
			s, _ := v.(string)
			p.Compatibility[name] = s
			return true
		})
	}
	p.Usage = r.object(doc, "usage", "", false)
	if ev := r.object(doc, "evolution", "", false); ev != nil {
		if s := r.str(ev, "previous_profile", "/evolution", false); s != "" {
			e, err := grammar.ParseExact(s)
			if err != nil {
				r.fail("/evolution/previous_profile", "%v", err)
			} else {
				p.Evolution.PreviousProfile = &e
			}
		}
		p.Evolution.PreviousVersion = r.str(ev, "previous_version", "/evolution", false)
		if l, ok := ev.Get("lens"); ok {
			lens, err := resolve.ParseLens(l)
			if err != nil {
				r.fail("/evolution/lens", "%v", err)
			}
			p.Evolution.Lens = lens
		}
		if cl, ok := ev.Get("changelog"); ok {
			p.Evolution.Changelog, _ = cl.([]ijson.Value)
		}
	}
	if len(r.errs) > 0 {
		return nil, r.errs[0]
	}
	fp, err := protocol.Fingerprint(doc)
	if err != nil {
		return nil, err
	}
	p.FP = fp
	return p, nil
}

func (r *reader) interaction(name string, o *ijson.Object, ptr string) *Interaction {
	in := &Interaction{
		Name: name, Effect: r.str(o, "effect", ptr, true), Result: r.str(o, "result", ptr, false),
		Delivery: r.str(o, "delivery", ptr, true), Execution: r.strs(o, "execution", ptr),
		Takes: r.str(o, "takes", ptr, false), Returns: r.str(o, "returns", ptr, false),
		Encodings: r.strs(o, "encodings", ptr),
	}
	if d := r.object(o, "deadline", ptr, false); d != nil {
		for _, k := range []string{"default", "max"} {
			s := r.str(d, k, ptr+"/deadline", true)
			v, err := grammar.ParseDuration(s)
			if s != "" && err != nil {
				r.fail(ptr+"/deadline/"+k, "%v", err)
			}
			if k == "default" {
				in.Deadline.Default = v
			} else {
				in.Deadline.Max = v
			}
		}
		if in.Deadline.Default > in.Deadline.Max {
			r.fail(ptr+"/deadline", "the default is above the maximum")
		}
	}
	if v, ok := o.Get("bindings"); ok {
		list, _ := v.([]ijson.Value)
		for i, e := range list {
			bp := fmt.Sprintf("%s/bindings/%d", ptr, i)
			b, ok := e.(*ijson.Object)
			if !ok {
				r.fail(bp, "is an object")
				continue
			}
			in.Bindings = append(in.Bindings, Binding{
				Protocol: r.str(b, "protocol", bp, true), Server: r.str(b, "server", bp, true), Method: r.str(b, "method", bp, false),
				Address: r.str(b, "address", bp, true), Auth: r.str(b, "auth", bp, false), Encodings: r.strs(b, "encodings", bp),
			})
		}
	}
	return in
}
