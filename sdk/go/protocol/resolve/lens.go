package resolve

import (
	"fmt"
	"math/big"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/units"
)

// Lens is a profile's evolution.lens: the operations that take a record of
// the profile before it to one of it, in order (§9.5.3).
type Lens []Op

// Op is one lens operation.
type Op struct {
	Op    string      // rename, alias_value, convert_unit, promote, default, drop
	Field string      // every operation but rename
	From  ijson.Value // rename: a property name; alias_value: a value; convert_unit: a unit; promote: a type
	To    ijson.Value
	Value ijson.Value // default
}

func (op Op) from() string {
	if op.Op == "rename" {
		s, _ := op.From.(string)
		return s
	}
	return ""
}

func (op Op) to() string {
	if op.Op == "rename" {
		s, _ := op.To.(string)
		return s
	}
	return ""
}

// promotions are the type changes §9.5.5 defines both ways.
var promotions = map[[2]string]bool{{"integer", "number"}: true, {"date", "date-time"}: true}

// ParseLens reads an evolution.lens.
func ParseLens(v ijson.Value) (Lens, error) {
	list, ok := v.([]ijson.Value)
	if !ok {
		return nil, fmt.Errorf("a lens is an array of operations")
	}
	out := make(Lens, 0, len(list))
	for i, e := range list {
		o, ok := e.(*ijson.Object)
		if !ok {
			return nil, fmt.Errorf("lens operation %d is an object", i)
		}
		op, err := parseOp(o)
		if err != nil {
			return nil, fmt.Errorf("lens operation %d: %w", i, err)
		}
		out = append(out, op)
	}
	return out, nil
}

func parseOp(o *ijson.Object) (Op, error) {
	get := func(k string) (ijson.Value, bool) { return o.Get(k) }
	name := func(k string) (string, error) {
		v, _ := get(k)
		s, _ := v.(string)
		if err := grammar.PropertyName(s); err != nil {
			return "", fmt.Errorf("%s: %w", k, err)
		}
		return s, nil
	}
	members := func(allowed ...string) error {
		var err error
		o.Each(func(k string, _ ijson.Value) bool {
			ok := k == "op"
			for _, a := range allowed {
				ok = ok || k == a
			}
			if !ok {
				err = fmt.Errorf("%q is not a member of this operation", k)
			}
			return err == nil
		})
		return err
	}
	kind, _ := get("op")
	var op Op
	op.Op, _ = kind.(string)
	var err error
	switch op.Op {
	case "rename":
		if err = members("from", "to"); err != nil {
			return op, err
		}
		var from, to string
		if from, err = name("from"); err != nil {
			return op, err
		}
		if to, err = name("to"); err != nil {
			return op, err
		}
		op.From, op.To = from, to
	case "alias_value", "convert_unit", "promote":
		if err = members("field", "from", "to"); err != nil {
			return op, err
		}
		if op.Field, err = name("field"); err != nil {
			return op, err
		}
		from, hasFrom := get("from")
		to, hasTo := get("to")
		if !hasFrom || !hasTo {
			return op, fmt.Errorf("%s has from and to", op.Op)
		}
		op.From, op.To = from, to
		switch op.Op {
		case "convert_unit":
			f, _ := from.(string)
			t, _ := to.(string)
			fu, err1 := unitOf(f)
			tu, err2 := unitOf(t)
			if err1 != nil || err2 != nil || !units.Convertible(fu, tu) {
				return op, fmt.Errorf("convert_unit from %v to %v is not a conversion of §9.5.4", from, to)
			}
			op.From, op.To = string(fu), string(tu)
		case "promote":
			f, _ := from.(string)
			t, _ := to.(string)
			if !promotions[[2]string{f, t}] {
				return op, fmt.Errorf("promote from %v to %v is not a promotion of §9.5.5", from, to)
			}
		}
	case "default":
		if err = members("field", "value"); err != nil {
			return op, err
		}
		if op.Field, err = name("field"); err != nil {
			return op, err
		}
		v, ok := get("value")
		if !ok {
			return op, fmt.Errorf("default has a value")
		}
		op.Value = v
	case "drop":
		if err = members("field"); err != nil {
			return op, err
		}
		if op.Field, err = name("field"); err != nil {
			return op, err
		}
	default:
		return op, fmt.Errorf("%q is not a lens operation of atlas/1.0", op.Op)
	}
	return op, nil
}

// unitOf reads a unit as a lens names it: an x-atlas-unit (bp), or the
// logical type that implies it (Bp), which mean the same (Q-RES-7).
func unitOf(name string) (units.Unit, error) {
	if u, ok := units.OfLogical(name); ok {
		return u, nil
	}
	return units.Parse(name)
}

// apply runs the operation on a record, forward or backward (§9.5.3). into
// is the schema of the profile the operation turns the record into: a
// decimal is rounded to its scale there (Q-RES-2).
func (op Op) apply(y *ijson.Object, forward bool, into *schema.Schema) error {
	switch op.Op {
	case "rename":
		from, to := op.from(), op.to()
		if !forward {
			from, to = to, from
		}
		if v, ok := y.Get(from); ok {
			y.Delete(from)
			y.Set(to, v)
		}
		return nil
	case "default":
		if forward {
			if _, ok := y.Get(op.Field); !ok {
				y.Set(op.Field, op.Value)
			}
		} else {
			y.Delete(op.Field)
		}
		return nil
	case "drop":
		if forward {
			y.Delete(op.Field)
		}
		return nil
	}
	v, ok := y.Get(op.Field)
	if !ok {
		return nil // an operation on an absent field does nothing
	}
	out, err := op.value(v, forward, "/"+escape(op.Field), into)
	if err != nil {
		return err
	}
	y.Set(op.Field, out)
	return nil
}

// value runs a value operation on one value, or on each element of an
// array. null passes through. A failure's target is the field (Q-RES-1).
func (op Op) value(v ijson.Value, forward bool, ptr string, into *schema.Schema) (ijson.Value, error) {
	if v == nil {
		return nil, nil
	}
	if list, ok := v.([]ijson.Value); ok {
		out := make([]ijson.Value, len(list))
		for i, e := range list {
			c, err := op.value(e, forward, ptr, into)
			if err != nil {
				return nil, err
			}
			out[i] = c
		}
		return out, nil
	}
	from, to := op.From, op.To
	if !forward {
		from, to = to, from
	}
	switch op.Op {
	case "alias_value":
		if ijson.Equal(v, from) {
			return to, nil
		}
		return v, nil
	case "convert_unit":
		return convertUnit(v, units.Unit(from.(string)), units.Unit(to.(string)), ptr, decimalScale(into, op.Field))
	case "promote":
		return promote(v, from.(string), to.(string), ptr)
	}
	return v, nil
}

// decimalScale is the scale s of the Decimal(p,s) a profile's property (or
// its items) holds, or -1.
func decimalScale(into *schema.Schema, field string) int {
	if into == nil {
		return -1
	}
	p, ok := into.Property(field)
	if !ok {
		return -1
	}
	if items := p.Items(); items != nil {
		p = items
	}
	if name, _, scale, ok := p.Logical(); ok && name == "Decimal" {
		return scale
	}
	return -1
}

// convertUnit converts a number by one multiplication or division, and a
// decimal string exactly, rounded to scale, ties to even. A decimal with no
// scale to round to cannot be converted (Q-RES-2).
func convertUnit(v ijson.Value, from, to units.Unit, ptr string, scale int) (ijson.Value, error) {
	switch x := v.(type) {
	case ijson.Number:
		f, err := x.Float64()
		if err != nil {
			return nil, failf(ptr, "%s is not a binary64 number", x)
		}
		out, err := units.Float(f, from, to)
		if err != nil {
			return nil, failf(ptr, "%v", err)
		}
		s, err := ijson.ECMAScriptNumber(out)
		if err != nil {
			return nil, failf(ptr, "%v", err)
		}
		return ijson.Number(s), nil
	case string:
		r, err := rat(x)
		if err != nil {
			return nil, failf(ptr, "%q is neither a number nor a decimal", x)
		}
		if scale < 0 {
			return nil, failf(ptr, "a decimal converted into a profile with no Decimal(p,s) to round to")
		}
		return grammar.FormatDecimal(units.RoundHalfEven(factor(r, from, to), scale), scale), nil
	}
	return nil, failf(ptr, "a unit converts only a number or a decimal")
}

func rat(s string) (*big.Rat, error) {
	if _, err := grammar.ParseDecimal(s); err != nil {
		return nil, err
	}
	r, _ := new(big.Rat).SetString(s)
	return r, nil
}

// factor applies a conversion's factor in exact arithmetic. ParseLens has
// already refused a pair that does not convert.
func factor(r *big.Rat, from, to units.Unit) *big.Rat {
	num, den, _ := units.Factor(from, to)
	return new(big.Rat).Mul(r, big.NewRat(num, den))
}

// promote converts a value from type from to type to. Backward, from and to
// are the declared ones swapped, and the conversion is only where it is
// exact (§9.5.3, §9.5.5).
func promote(v ijson.Value, from, to string, ptr string) (ijson.Value, error) {
	switch {
	case from == "integer" && to == "number":
		if _, ok := v.(ijson.Number); ok {
			return v, nil
		}
	case from == "number" && to == "integer":
		n, ok := v.(ijson.Number)
		if ok && n.SafeInteger() {
			f, _ := n.Float64()
			s, _ := ijson.ECMAScriptNumber(f)
			return ijson.Number(s), nil
		}
		if ok {
			return nil, failf(ptr, "%s is not exactly an integer", n)
		}
	case from == "date" && to == "date-time":
		if s, ok := v.(string); ok {
			if _, err := grammar.ParseDate(s); err == nil {
				return s + "T00:00:00Z", nil
			}
		}
	case from == "date-time" && to == "date":
		if s, ok := v.(string); ok {
			if t, err := grammar.ParseDateTime(s); err == nil {
				t = t.UTC()
				if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
					return t.Format("2006-01-02"), nil
				}
				return nil, failf(ptr, "%s is not exactly a date", s)
			}
		}
	}
	return nil, failf(ptr, "the value is not a %s", from)
}
