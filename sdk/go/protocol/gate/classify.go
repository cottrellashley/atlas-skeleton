package gate

import (
	"fmt"
	"slices"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/resolve"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

// Classify names every difference between the last published profile of a
// version and a new one, by §3.3.3 to §3.3.7 as the design review narrows
// them (R03, docs/pass-2/REVIEW.md). Within a version a request may only
// widen, a result may only add a field with a defined missing value, and
// a rename keeps its old name as an alias. A removal, a unit change, a type
// change, a result field made non-null and a changed default are a new
// major version: an old reader could not hold what the new writer sends,
// or a new reader what was written before. Prose is not compared: it is not
// part of the contract.
func Classify(old, new *profile.Profile) []Finding {
	c := &classifier{old: old, new: new}
	c.common()
	c.schemas()
	c.meaning()
	return c.out
}

type classifier struct {
	old, new *profile.Profile
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

// schemas compares the schemas each interaction uses, interaction by
// interaction and whatever they are named, so pointing an interaction at
// another schema cannot escape the comparison. What an interaction takes
// is held BACKWARD: the gateway reads every earlier request, resolved
// forward. What it returns is held FULL: an earlier reader reads what the
// new writer sends, and a new reader reads what was written before
// (§3.3.2a, R03).
func (c *classifier) schemas() {
	seen := map[string]bool{}
	for _, name := range c.old.Order {
		oi, ni := c.old.Interactions[name], c.new.Interactions[name]
		if ni == nil {
			continue // its removal is classified in common
		}
		c.role(seen, name, "takes", oi.Takes, ni.Takes, part{name: "request", request: true})
		p := part{name: "result", table: ni.Result == "table"}
		switch c.new.Family {
		case "resource":
			p.name = "resource"
		case "event":
			p.name = "payload"
		}
		c.role(seen, name, "returns", oi.Returns, ni.Returns, p)
	}
}

// role compares what one interaction takes, or returns, before and after.
func (c *classifier) role(seen map[string]bool, interaction, member, oldName, newName string, p part) {
	os, oHas := c.old.Schemas[oldName]
	ns, nHas := c.new.Schemas[newName]
	ptr := "/interactions/" + interaction + "/" + member
	switch {
	case !oHas && !nHas:
		return
	case !oHas && p.request && ns.Valid(ijson.NewObject()):
		return // an earlier caller sent {}, which the new request still accepts
	case !oHas:
		c.add(Breaking, ptr, "the interaction now %s a schema it did not: declare @v%d", member, c.new.Version+1)
		return
	case !nHas:
		c.add(Breaking, ptr, "the interaction no longer %s the schema it did: declare @v%d", member, c.new.Version+1)
		return
	}
	key := oldName + "\x00" + newName + "\x00" + p.name
	if seen[key] {
		return
	}
	seen[key] = true
	c.compare(os, ns, "/schemas/"+newName, p, true)
}

// part is a schema's role, for the rules that differ by it.
type part struct {
	name    string // request, result, resource, payload
	request bool
	table   bool
}

// compare compares two schemas in a role. Every level is compared, the
// root included: first the schema's own keywords (its type, nullability,
// unit, enum, bounds and every other assertion), then what it holds, an
// array's items and an object's properties. A constraint on an enclosing
// schema, such as maxProperties on a request, is classified like any other.
func (c *classifier) compare(o, n *schema.Schema, ptr string, p part, top bool) {
	if !c.value(o, n, ptr, p) {
		return // a different type: nothing within it compares
	}
	oi, ni := o.Items(), n.Items()
	if oi != nil && ni != nil {
		c.compare(oi, ni, ptr+"/items", p, top)
	}
	if len(o.Properties()) > 0 || len(n.Properties()) > 0 {
		c.properties(o, n, ptr, p, top)
	}
}

// missingValue reports whether a reader holding s has a value for a
// property that a record lacks: none is needed where it is optional, and
// otherwise it is its default, or null where it allows null (§9.5.2 steps
// 8 to 10).
func missingValue(s *schema.Schema, name string, ps *schema.Schema) bool {
	if !s.Required(name) {
		return true
	}
	_, hasDefault := ps.Default()
	return hasDefault || ps.Nullable()
}

// properties compares an object's properties: the ones kept, renamed,
// added and removed, and what each side requires of the other's records.
func (c *classifier) properties(o, n *schema.Schema, ptr string, p part, top bool) {
	v := c.new.Version + 1
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
		from, op := name, (*schema.Schema)(nil)
		if x, existed := o.Property(name); existed {
			op = x
		} else if from = keyOf(renamed, name); from != "" {
			c.renamed(from, name, at, p, top)
			op, _ = o.Property(from)
		}
		if op == nil {
			// A property neither side had: what was written before lacks it.
			if !missingValue(n, name, np) {
				if p.request {
					c.add(Breaking, at, "%s property '%s' became required: declare @v%d", p.name, name, v)
				} else {
					c.add(Breaking, at, "%s field '%s' is required and has no missing value for what was written before it: make it optional, nullable or defaulted, or declare @v%d (R03)", p.name, name, v)
				}
			}
			continue
		}
		c.compare(op, np, at, p, false)
		// What one side wrote without the property, the other side reads.
		if !o.Required(from) && !missingValue(n, name, np) {
			if p.request {
				c.add(Breaking, at, "%s property '%s' became required: declare @v%d", p.name, name, v)
			} else {
				c.add(Breaking, at, "%s field '%s' became required with no default and no null, and what was written before may lack it: declare @v%d (R03)", p.name, name, v)
			}
		}
		if !p.request && !n.Required(name) && !missingValue(o, from, op) {
			c.add(Breaking, at, "%s field '%s' is no longer required, and earlier readers have no default or null for a record that lacks it: declare @v%d (R03)", p.name, name, v)
		}
		for _, a := range op.Aliases() {
			if !slices.Contains(np.Aliases(), a) {
				c.add(Breaking, at, "the alias '%s' was removed, so what was written under that name no longer reads: declare @v%d", a, v)
			}
		}
	}
	for _, name := range o.Properties() {
		if _, still := n.Property(name); still || renamed[name] != "" {
			continue
		}
		at := ptr + "/properties/" + name
		if p.request {
			c.add(Breaking, at, "the request no longer accepts '%s', which earlier requests send: declare @v%d", name, v)
			continue
		}
		c.add(Breaking, at, "%s field '%s' was removed: no default can give an earlier reader its value, so a removal is a new major version, @v%d (R03)", p.name, name, v)
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

// value compares one schema's own keywords: its type, nullability, unit,
// enum and constraints. It reports false when the types differ, so that
// nothing within them is compared.
func (c *classifier) value(o, n *schema.Schema, ptr string, p part) bool {
	v := c.new.Version + 1
	switch {
	case o.Nullable() && !n.Nullable():
		if p.request {
			c.add(Breaking, ptr, "%s property no longer accepts null: declare @v%d", p.name, v)
		} else {
			c.add(Breaking, ptr, "%s field became non-null, and what was written before holds nulls: declare @v%d (R03)", p.name, v)
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
		if !p.request { // a request's promotion widens it: every earlier request is still valid
			c.add(Breaking, ptr, "the type changed from %s to %s: an earlier reader cannot hold every new value (1.5 in an integer), so declare @v%d (R03)", ot, nt, v)
		}
	default:
		c.add(Breaking, ptr, "the type changed from %s to %s, which is no promotion: declare @v%d", ot, nt, v)
		return false
	}
	ou, _ := o.Unit()
	nu, _ := n.Unit()
	if ou != nu {
		c.add(Breaking, ptr, "the unit changed from %q to %q: a version keeps its units, so declare @v%d (R03)", ou, nu, v)
	}
	c.enum(o, n, ptr, p)
	c.decimal(o, n, ptr, p)
	if !p.request && o.Key() != n.Key() {
		c.add(Breaking, ptr, "the row's key changed: declare @v%d", v)
	}
	if !o.Deprecated() && n.Deprecated() {
		c.add(Dangerous, ptr, "deprecated: every call that uses it returns Deprecation and Sunset")
	}
	if o.Reference() != n.Reference() {
		c.add(Dangerous, ptr, "the capability that lists its values changed from %q to %q", o.Reference(), n.Reference())
	}
	od, oHas := o.Default()
	nd, nHas := n.Default()
	if oHas && (!nHas || !ijson.Equal(od, nd)) { // a default changed or removed
		c.add(Breaking, ptr, "the %s's default changed: a version keeps its defaults, so declare @v%d", p.name, v)
	}
	c.constraints(o, n, ptr, p)
	return true
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
		c.add(Breaking, ptr, "an enum now narrows what the %s holds: declare @v%d", p.name, c.new.Version+1)
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

// decimal compares a Decimal(p,s)'s precision and scale. A request may
// widen them. A result keeps them: narrowing refuses values written
// before, and widening sends values an earlier reader refuses.
func (c *classifier) decimal(o, n *schema.Schema, ptr string, p part) {
	on, op, os, ok1 := o.Logical()
	nn, np, ns, ok2 := n.Logical()
	if !ok1 || !ok2 || on != "Decimal" || nn != "Decimal" || op == np && os == ns {
		return
	}
	wider := np-ns >= op-os && ns >= os
	switch {
	case p.request && !wider:
		c.add(Breaking, ptr, "the request's Decimal(%d,%d) narrowed to Decimal(%d,%d): declare @v%d", op, os, np, ns, c.new.Version+1)
	case !p.request:
		c.add(Breaking, ptr, "Decimal(%d,%d) changed to Decimal(%d,%d): an earlier reader checks what the new writer sends, and a new reader what was written before, so a %s keeps its precision and scale: declare @v%d (R03)", op, os, np, ns, p.name, c.new.Version+1)
	}
}

// own are the keywords compare and value classify by their own rules, and
// prose, which is not part of the contract. Every other keyword is
// compared by constraints.
var own = map[string]bool{
	"$schema": true, "$vocabulary": true, "$id": true, "$defs": true, "$ref": true, "$comment": true,
	"title": true, "description": true, "examples": true, "deprecated": true, "readOnly": true, "writeOnly": true,
	"type": true, "format": true, "enum": true, "default": true, "properties": true, "required": true, "items": true,
	"x-atlas-logical": true, "x-atlas-unit": true, "x-atlas-key": true, "x-atlas-aliases": true,
	"x-atlas-reference": true, "x-atlas-deprecated": true, "x-atlas-enum": true,
	"minimum": true, "exclusiveMinimum": true, "minLength": true, "minItems": true, "minProperties": true,
	"maximum": true, "exclusiveMaximum": true, "maxLength": true, "maxItems": true, "maxProperties": true,
}

// constraints compares every other assertion on a value. A request may
// only relax its bounds. Every other schema keeps them, because a reader
// validates what it receives (§9.3 rule 1) and a new reader reads what was
// written before. A keyword whose change the gate cannot show to be safe
// (prefixItems, anyOf, oneOf) is breaking.
func (c *classifier) constraints(o, n *schema.Schema, ptr string, p part) {
	os, _ := o.Definition().Source().(*ijson.Object)
	ns, _ := n.Definition().Source().(*ijson.Object)
	if os == nil || ns == nil {
		return
	}
	for _, k := range []string{"minimum", "exclusiveMinimum", "minLength", "minItems", "minProperties"} {
		c.bound(os, ns, k, ptr, p, true)
	}
	for _, k := range []string{"maximum", "exclusiveMaximum", "maxLength", "maxItems", "maxProperties"} {
		c.bound(os, ns, k, ptr, p, false)
	}
	names := slices.Concat(os.Names(), ns.Names())
	slices.Sort(names)
	for _, k := range slices.Compact(names) {
		if own[k] {
			continue
		}
		ov, oHas := os.Get(k)
		nv, nHas := ns.Get(k)
		if oHas == nHas && (!oHas || sameSchema(ov, nv)) {
			continue
		}
		_, oSchema := ov.(*ijson.Object)
		_, nSchema := nv.(*ijson.Object)
		switch {
		case k == "pattern" || k == "multipleOf" || k == "const":
			c.bounded(k, oHas && !nHas, ptr, p)
		case k == "uniqueItems":
			c.bounded(k, ov == true && nv != true, ptr, p)
		case k == "additionalProperties" && !oSchema && !nSchema:
			c.bounded(k, ov == false && (!nHas || nv == true), ptr, p)
		default:
			c.add(Breaking, ptr, "%s changed, and the gate cannot show that every value it held before still holds: declare @v%d", k, c.new.Version+1)
		}
	}
}

// sameSchema reports whether two keyword values say the same thing,
// ignoring prose.
func sameSchema(a, b ijson.Value) bool {
	a, b = ijson.Clone(a), ijson.Clone(b)
	protocol.StripSchema(a)
	protocol.StripSchema(b)
	return ijson.Equal(a, b)
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
		c.add(Breaking, ptr, "%s relaxed: earlier readers would refuse the new values: declare @v%d (R03)", k, c.new.Version+1)
	case !p.request:
		c.add(Breaking, ptr, "%s tightened: what was written before may not hold, and a new reader reads it: declare @v%d (R03)", k, c.new.Version+1)
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
			bad("drops '%s': a removal is a new major version, @v%d (R03)", op.Field, new.Version+1)
		case "default":
			if _, ok := has(old, op.Field); ok {
				bad("defaults '%s', which the previous profile already has", op.Field)
			}
			if s, ok := has(new, op.Field); !ok || !s.Valid(op.Value) {
				bad("defaults '%s' to a value this profile does not accept", op.Field)
			}
		case "convert_unit", "promote":
			bad("%s on '%s' changes a unit or a type, which a version keeps: declare @v%d. The resolver's %s is an experiment, not a client obligation (R03)", op.Op, op.Field, new.Version+1, op.Op)
		case "alias_value":
			_, ok1 := has(old, op.Field)
			_, ok2 := has(new, op.Field)
			if !ok1 || !ok2 {
				bad("%s on '%s', which both profiles have", op.Op, op.Field)
			}
		}
	}
	return out
}
