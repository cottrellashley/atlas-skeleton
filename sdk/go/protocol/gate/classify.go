package gate

import (
	"fmt"
	"slices"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/resolve"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/units"
)

// Classify names every difference between the last published profile of a
// version and a new one, by §3.3.3 to §3.3.7. Prose is not compared: it is
// not part of the contract.
func Classify(old, new *profile.Profile, now time.Time) []Finding {
	c := &classifier{old: old, new: new, now: now}
	c.common()
	c.schemas()
	c.meaning()
	return c.out
}

type classifier struct {
	old, new *profile.Profile
	now      time.Time
	out      []Finding
}

func (c *classifier) add(class Class, ptr, format string, args ...any) {
	c.out = append(c.out, Finding{class, ptr, fmt.Sprintf(format, args...)})
}

// common is §3.3.3: what every family shares.
func (c *classifier) common() {
	o, n := c.old, c.new
	if o.Family != n.Family {
		c.add(Breaking, "/properties/family", "the family changed from %s to %s: declare @v%d", o.Family, n.Family, n.Version+1)
	}
	switch {
	case o.Lifecycle == n.Lifecycle:
	case o.Lifecycle == "preview" && n.Lifecycle == "stable":
	case o.Lifecycle == "stable" && n.Lifecycle == "deprecated":
		c.add(Dangerous, "/properties/lifecycle", "the version is deprecated: every call returns Deprecation and Sunset")
	default:
		c.add(Breaking, "/properties/lifecycle", "the lifecycle moves only preview → stable → deprecated, not %s → %s", o.Lifecycle, n.Lifecycle)
	}
	if o.Visibility != n.Visibility {
		if o.Visibility == "confidential" && n.Visibility == "listed" {
			// safe: more callers may see it
		} else {
			c.add(Dangerous, "/properties/visibility", "visibility changed from %s to %s", o.Visibility, n.Visibility)
		}
	}
	for _, t := range o.Traits {
		i := slices.IndexFunc(n.Traits, func(x profile.Trait) bool { return x.Name == t.Name })
		switch {
		case i < 0 && (t.Name == "Handoff" || t.Name == "Brokerable"):
			// follows its execution: callers fall back to the gateway
		case i < 0:
			c.add(Breaking, "/properties/traits", "the trait %s was removed: declare @v%d", t.Name, n.Version+1)
		case n.Traits[i].Critical && !t.Critical:
			c.add(Dangerous, "/properties/traits", "the trait %s became critical: clients without it are refused the capability", t.Name)
		}
	}
	for _, t := range n.Traits {
		if !o.HasTrait(t.Name) && t.Critical {
			c.add(Dangerous, "/properties/traits", "the critical trait %s was added: clients without it are refused the capability", t.Name)
		}
	}
	for _, name := range o.Order {
		oi, ni := o.Interactions[name], n.Interactions[name]
		ptr := "/interactions/" + name
		if ni == nil {
			c.add(Breaking, ptr, "the interaction %s was removed: declare @v%d", name, n.Version+1)
			continue
		}
		if oi.Effect != ni.Effect {
			c.add(Breaking, ptr+"/effect", "the effect class changed from %s to %s: declare @v%d", oi.Effect, ni.Effect, n.Version+1)
		}
		if oi.Result != ni.Result {
			c.add(Breaking, ptr+"/result", "the result changed from %s to %s: declare @v%d", oi.Result, ni.Result, n.Version+1)
		}
		if oi.Delivery != ni.Delivery {
			c.add(Breaking, ptr+"/delivery", "the delivery changed from %s to %s: declare @v%d", oi.Delivery, ni.Delivery, n.Version+1)
		}
		if ni.Deadline.Max < oi.Deadline.Max {
			c.add(Dangerous, ptr+"/deadline/max", "the deadline maximum was lowered from %v to %v", oi.Deadline.Max, ni.Deadline.Max)
		}
		if ni.Deadline.Default < oi.Deadline.Default {
			c.add(Dangerous, ptr+"/deadline/default", "the default deadline was lowered from %v to %v", oi.Deadline.Default, ni.Deadline.Default)
		}
	}
	for _, e := range n.Errors {
		i := slices.IndexFunc(o.Errors, func(x protocol.Declared) bool { return x.Code == e.Code })
		switch {
		case i < 0:
			c.add(Dangerous, "/errors", "the error %s was added: clients treat an unknown code by its status and retryable", e.Code)
		case o.Errors[i].Status != e.Status || o.Errors[i].Retryable != e.Retryable:
			c.add(Breaking, "/errors", "the error %s changed its status or retryable: declare @v%d", e.Code, n.Version+1)
		}
	}
}

// schemas compares every schema the two profiles share, each by its role:
// what an interaction takes is held BACKWARD (the gateway reads every
// earlier request), everything else FULL (§3.3.2a).
func (c *classifier) schemas() {
	for name, os := range c.old.Schemas {
		ns, ok := c.new.Schemas[name]
		if !ok {
			continue // its interaction's removal is classified above
		}
		part := c.part(name)
		c.record(os, ns, "/schemas/"+name, part, true)
	}
}

// part is a schema's role, for the rules that differ by it.
type part struct {
	name    string // request, result, resource, plan, receipt, payload
	request bool
	table   bool
}

func (c *classifier) part(name string) part {
	p := part{name: name}
	for _, in := range c.new.Interactions {
		if in.Takes == name {
			p.request = true
			p.name = "request"
		}
		if in.Returns == name && in.Result == "table" {
			p.table = true
		}
	}
	if !p.request {
		switch c.new.Family {
		case "resource":
			p.name = "resource"
		case "event":
			p.name = "payload"
		}
	}
	return p
}

// record compares two record schemas, or anything nested in one.
func (c *classifier) record(o, n *schema.Schema, ptr string, p part, top bool) {
	if items, nitems := o.Items(), n.Items(); items != nil && nitems != nil {
		c.record(items, nitems, ptr+"/items", p, top)
		return
	}
	if len(o.Properties()) == 0 || len(n.Properties()) == 0 {
		c.value(o, n, ptr, p)
		return
	}
	renamed := map[string]string{} // old name → new name
	for _, name := range n.Properties() {
		np, _ := n.Property(name)
		if _, existed := o.Property(name); existed {
			continue
		}
		for _, a := range np.Aliases() {
			if _, was := o.Property(a); was {
				if _, still := n.Property(a); !still {
					renamed[a] = name
				}
			}
		}
	}
	for _, name := range n.Properties() {
		np, _ := n.Property(name)
		at := ptr + "/properties/" + name
		op, existed := o.Property(name)
		if !existed {
			if from := keyOf(renamed, name); from != "" {
				c.renamed(from, name, at, p, top)
				op, _ = o.Property(from)
				c.value(op, np, at, p)
				continue
			}
			_, hasDefault := np.Default()
			mustHave := n.Required(name) && !np.Nullable() && !hasDefault
			switch {
			case p.request && mustHave:
				c.add(Breaking, at, "%s property '%s' became required: declare @v%d", p.name, name, c.new.Version+1)
			case !p.request && !p.table && mustHave:
				c.add(Breaking, at, "%s field '%s' is required, and earlier %ss lack it: give it a default, or declare @v%d", p.name, name, p.name, c.new.Version+1)
			}
			continue
		}
		c.value(op, np, at, p)
		if !o.Required(name) && n.Required(name) && p.request {
			if _, hasDefault := np.Default(); !hasDefault && !np.Nullable() {
				c.add(Breaking, at, "%s property '%s' became required: declare @v%d", p.name, name, c.new.Version+1)
			}
		}
	}
	for _, name := range o.Properties() {
		if _, still := n.Property(name); still || renamed[name] != "" {
			continue
		}
		op, _ := o.Property(name)
		at := ptr + "/properties/" + name
		if sunsetPassed(op, c.now) {
			if top && !c.lensHas("drop", name) {
				c.add(Dangerous, at, "'%s' was dropped after its sunset: the lens drops it", name)
			}
			continue
		}
		c.add(Breaking, at, "%s '%s' was dropped before its sunset: deprecate it first, or declare @v%d", p.name, name, c.new.Version+1)
	}
}

func keyOf(m map[string]string, v string) string {
	for k, x := range m {
		if x == v {
			return k
		}
	}
	return ""
}

// renamed classifies a renamed property: dangerous, with aliases and, where
// readers on both sides must follow it, a rename lens.
func (c *classifier) renamed(from, to, ptr string, p part, top bool) {
	if top && !p.request && !c.lensHas("rename", from, to) {
		c.add(Breaking, ptr, "'%s' was renamed '%s' without a rename lens: earlier readers could not read it", from, to)
		return
	}
	c.add(Dangerous, ptr, "'%s' was renamed '%s': its aliases and lens keep earlier readers and writers working", from, to)
}

func (c *classifier) lensHas(op string, fields ...string) bool {
	return slices.ContainsFunc(c.new.Evolution.Lens, func(x resolve.Op) bool {
		if x.Op != op {
			return false
		}
		switch op {
		case "rename":
			return x.From == fields[0] && x.To == fields[1]
		default:
			return x.Field == fields[0]
		}
	})
}

// value compares one property's schema: its type, nullability, unit, enum
// and constraints.
func (c *classifier) value(o, n *schema.Schema, ptr string, p part) {
	v := c.new.Version + 1
	switch {
	case o.Nullable() && !n.Nullable():
		if p.request {
			c.add(Breaking, ptr, "%s property no longer accepts null: declare @v%d", p.name, v)
		}
	case !o.Nullable() && n.Nullable():
		if !p.request {
			c.add(Breaking, ptr, "%s field became nullable: declare @v%d", p.name, v)
		}
	}
	ot, nt := typeName(o), typeName(n)
	switch {
	case ot == nt:
	case promoted(ot, nt):
		if p.request {
			// a widening: every earlier request is still valid
		} else {
			c.add(Dangerous, ptr, "the type was promoted from %s to %s: resolution promotes it", ot, nt)
		}
	default:
		c.add(Breaking, ptr, "the type changed from %s to %s, which is no promotion: declare @v%d", ot, nt, v)
		return
	}
	ou, _ := o.Unit()
	nu, _ := n.Unit()
	if ou != nu {
		if ou != "" && nu != "" && units.Convertible(ou, nu) {
			c.add(Dangerous, ptr, "the unit changed from %s to %s: a convert_unit lens converts it", ou, nu)
		} else {
			c.add(Breaking, ptr, "the unit changed from %q to %q, across dimensions: declare @v%d", ou, nu, v)
		}
	}
	c.enum(o, n, ptr, p)
	c.decimal(o, n, ptr, p)
	if !p.request && o.Key() != n.Key() {
		c.add(Breaking, ptr, "the row's key changed: declare @v%d", v)
	}
	if !o.Deprecated() && n.Deprecated() {
		c.add(Dangerous, ptr, "deprecated: every call that uses it returns Deprecation and Sunset")
	}
	od, oHas := o.Default()
	nd, nHas := n.Default()
	if p.request && oHas && (!nHas || !ijson.Equal(od, nd)) { // a default changed or removed
		c.add(Breaking, ptr, "%s property's default changed: declare @v%d", p.name, v)
	}
	c.constraints(o, n, ptr, p)
	if len(o.Properties()) > 0 && len(n.Properties()) > 0 || o.Items() != nil && n.Items() != nil {
		c.record(o, n, ptr, p, false)
	}
}

func typeName(s *schema.Schema) string {
	if name, _, _, ok := s.Logical(); ok && (name == "Decimal" || name == "Money") {
		return name
	}
	if f := s.Format(); f != "" {
		return s.Type() + "/" + f
	}
	return s.Type()
}

func promoted(from, to string) bool {
	return from == "integer" && to == "number" || from == "string/date" && to == "string/date-time"
}

// enum compares the values a property takes (§3.3.4).
func (c *classifier) enum(o, n *schema.Schema, ptr string, p part) {
	if o.Enum() == nil && n.Enum() == nil {
		return
	}
	if o.Enum() != nil && n.Enum() == nil {
		if !p.request {
			c.add(Breaking, ptr, "the enum was removed: earlier readers would refuse new values")
		}
		return
	}
	if o.Enum() == nil {
		if p.request {
			c.add(Breaking, ptr, "an enum now narrows what the request accepts: declare @v%d", c.new.Version+1)
		}
		return
	}
	for _, e := range n.Enum() {
		if !slices.ContainsFunc(o.Enum(), func(x ijson.Value) bool { return ijson.Equal(x, e) }) && !p.request {
			c.add(Dangerous, ptr, "the value %s was added: earlier readers read it as UNKNOWN", describe(e))
		}
	}
	for _, e := range o.Enum() {
		if slices.ContainsFunc(n.Enum(), func(x ijson.Value) bool { return ijson.Equal(x, e) }) {
			continue
		}
		s, _ := e.(string)
		if _, aliased := n.EnumAlias(s); aliased {
			c.add(Dangerous, ptr, "the value %s is now an alias: an alias_value lens and x-atlas-enum's aliases carry it", describe(e))
			continue
		}
		if p.request {
			c.add(Breaking, ptr, "the request no longer accepts %s: make it an alias of a value it keeps, or declare @v%d", describe(e), c.new.Version+1)
		} else {
			c.add(Dangerous, ptr, "the value %s was removed: readers of earlier data read it as UNKNOWN", describe(e))
		}
	}
}

func describe(v ijson.Value) string {
	b, _ := ijson.Marshal(v)
	return string(b)
}

// decimal compares a Decimal(p,s)'s precision and scale: a request may
// widen, a result may narrow.
func (c *classifier) decimal(o, n *schema.Schema, ptr string, p part) {
	on, op, os, ok1 := o.Logical()
	nn, np, ns, ok2 := n.Logical()
	if !ok1 || !ok2 || on != "Decimal" || nn != "Decimal" || op == np && os == ns {
		return
	}
	wider := np-ns >= op-os && ns >= os
	narrower := np-ns <= op-os && ns <= os
	switch {
	case p.request && !wider:
		c.add(Breaking, ptr, "the request's Decimal(%d,%d) narrowed to Decimal(%d,%d): declare @v%d", op, os, np, ns, c.new.Version+1)
	case !p.request && !narrower:
		c.add(Breaking, ptr, "Decimal(%d,%d) widened to Decimal(%d,%d): earlier readers would refuse the new values", op, os, np, ns)
	}
}

// constraints compares the bounds on a value: a request may only relax
// them, and every other schema may only tighten them, because a reader
// validates what it receives (§9.3 rule 1).
func (c *classifier) constraints(o, n *schema.Schema, ptr string, p part) {
	os, _ := o.Source().(*ijson.Object)
	ns, _ := n.Source().(*ijson.Object)
	if os == nil || ns == nil {
		return
	}
	for _, k := range []string{"minimum", "exclusiveMinimum", "minLength", "minItems", "minProperties"} {
		c.bound(os, ns, k, ptr, p, true)
	}
	for _, k := range []string{"maximum", "exclusiveMaximum", "maxLength", "maxItems", "maxProperties"} {
		c.bound(os, ns, k, ptr, p, false)
	}
	for _, k := range []string{"pattern", "multipleOf", "const"} {
		ov, oHas := os.Get(k)
		nv, nHas := ns.Get(k)
		if oHas == nHas && (!oHas || ijson.Equal(ov, nv)) {
			continue
		}
		relaxed := oHas && !nHas
		c.bounded(k, relaxed, ptr, p)
	}
}

func (c *classifier) bound(os, ns *ijson.Object, k, ptr string, p part, lower bool) {
	ov, oHas := os.Get(k)
	nv, nHas := ns.Get(k)
	if !oHas && !nHas {
		return
	}
	if oHas && nHas {
		of, _ := ov.(ijson.Number).Float64()
		nf, _ := nv.(ijson.Number).Float64()
		if of == nf {
			return
		}
		c.bounded(k, lower == (nf < of), ptr, p)
		return
	}
	c.bounded(k, oHas && !nHas, ptr, p)
}

func (c *classifier) bounded(k string, relaxed bool, ptr string, p part) {
	switch {
	case p.request && !relaxed:
		c.add(Breaking, ptr, "the request's %s now refuses values it accepted: declare @v%d", k, c.new.Version+1)
	case !p.request && relaxed:
		c.add(Breaking, ptr, "%s relaxed: earlier readers would refuse the new values", k)
	}
}

// meaning is the contract's words: for data the grain, keys, as-of rule and
// source; for an event when it is emitted, its subject, its ordering and
// retention; for an action its effects and policy. Any change to them is a
// new meaning, except an event's retention and an action's policy.
func (c *classifier) meaning() {
	om, nm := c.old.Meaning, c.new.Meaning
	if om == nil && nm == nil {
		return
	}
	if om == nil || nm == nil {
		c.add(Breaking, "/meaning", "the meaning changed: declare @v%d", c.new.Version+1)
		return
	}
	names := map[string]bool{}
	for _, k := range append(om.Names(), nm.Names()...) {
		names[k] = true
	}
	for k := range names {
		ov, _ := om.Get(k)
		nv, _ := nm.Get(k)
		if ijson.Equal(ov, nv) {
			continue
		}
		switch {
		case c.new.Family == "event" && k == "retention":
			if longer(ov, nv) {
				continue
			}
			c.add(Dangerous, "/meaning/retention", "the retention was shortened")
		case c.new.Family == "action" && k == "policy":
			c.add(Dangerous, "/meaning/policy", "the approval policy changed; fewer approvals need the domain owner's approval")
		default:
			c.add(Breaking, "/meaning/"+k, "the meaning's %s changed: declare @v%d", k, c.new.Version+1)
		}
	}
}

func longer(a, b ijson.Value) bool {
	as, _ := a.(string)
	bs, _ := b.(string)
	da, e1 := grammar.ParseDuration(as)
	db, e2 := grammar.ParseDuration(bs)
	return e1 == nil && e2 == nil && db > da
}

func sunsetPassed(s *schema.Schema, now time.Time) bool {
	src, _ := s.Source().(*ijson.Object)
	if src == nil {
		return false
	}
	d, ok := src.Get("x-atlas-deprecated")
	if !ok {
		return false
	}
	sunset, _ := d.(*ijson.Object).Get("sunset")
	str, _ := sunset.(string)
	date, err := grammar.ParseDate(str)
	return err == nil && !time.Date(date.Year, date.Month, date.Day, 0, 0, 0, 0, time.UTC).After(now)
}

// lensConsistent is §3.3.12 step 6: every lens operation maps the previous
// profile onto this one, and every alias names a field that existed.
func lensConsistent(old, new *profile.Profile) []Finding {
	var out []Finding
	has := func(p *profile.Profile, field string) (*schema.Schema, bool) {
		for _, s := range p.Schemas {
			row := s
			if it := s.Items(); it != nil {
				row = it
			}
			if f, ok := row.Property(field); ok {
				return f, true
			}
		}
		return nil, false
	}
	for i, op := range new.Evolution.Lens {
		ptr := fmt.Sprintf("/evolution/lens/%d", i)
		bad := func(format string, args ...any) {
			out = append(out, Finding{Breaking, ptr, fmt.Sprintf(format, args...)})
		}
		switch op.Op {
		case "rename":
			from, to := op.From.(string), op.To.(string)
			if _, ok := has(old, from); !ok {
				bad("renames '%s', which the previous profile does not have", from)
			}
			if _, ok := has(new, to); !ok {
				bad("renames to '%s', which this profile does not have", to)
			}
		case "drop":
			if _, ok := has(old, op.Field); !ok {
				bad("drops '%s', which the previous profile does not have", op.Field)
			}
			if _, ok := has(new, op.Field); ok {
				bad("drops '%s', which this profile still has", op.Field)
			}
		case "default":
			if _, ok := has(old, op.Field); ok {
				bad("defaults '%s', which the previous profile already has", op.Field)
			}
			if s, ok := has(new, op.Field); !ok || !s.Valid(op.Value) {
				bad("defaults '%s' to a value this profile does not accept", op.Field)
			}
		case "convert_unit":
			os, ok1 := has(old, op.Field)
			ns, ok2 := has(new, op.Field)
			ou, _ := os.Unit()
			nu, _ := ns.Unit()
			if !ok1 || !ok2 || ou != "" && string(ou) != op.From || nu != "" && string(nu) != op.To {
				bad("converts '%s' from %v to %v, which the two profiles' units do not say", op.Field, op.From, op.To)
			}
		case "promote", "alias_value":
			_, ok1 := has(old, op.Field)
			_, ok2 := has(new, op.Field)
			if !ok1 || !ok2 {
				bad("%s on '%s', which both profiles have", op.Op, op.Field)
			}
		}
	}
	return out
}
