package schema

import (
	"bytes"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// annotations are the keywords that describe a value without deciding
// whether it is valid: prose, defaults, and the Atlas keywords that
// annotate.
var annotations = map[string]bool{
	"$schema": true, "$vocabulary": true, "$id": true, "$comment": true,
	"title": true, "description": true, "examples": true, "default": true, "deprecated": true, "readOnly": true, "writeOnly": true,
	"x-atlas-aliases": true, "x-atlas-key": true, "x-atlas-reference": true, "x-atlas-deprecated": true, "x-atlas-unit": true, "x-atlas-enum": true,
}

// SameValues reports whether two schemas admit exactly the same values: it
// compares what validates and leaves out what only describes. A schema
// with a $ref is never reported the same, because its meaning is in its
// root's definitions.
func SameValues(a, b *Schema) bool {
	ca, okA := validating(a.n.source)
	cb, okB := validating(b.n.source)
	return okA && okB && bytes.Equal(ca, cb)
}

func validating(v ijson.Value) ([]byte, bool) {
	c, ok := strip(v)
	if !ok {
		return nil, false
	}
	b, err := ijson.Canonical(c)
	return b, err == nil
}

// strip copies a schema without its annotations, at schema positions only.
func strip(v ijson.Value) (ijson.Value, bool) {
	o, isObject := v.(*ijson.Object)
	if !isObject {
		return v, true // a boolean schema
	}
	if _, ok := o.Get("$ref"); ok {
		return nil, false
	}
	out := ijson.NewObject()
	ok := true
	o.Each(func(k string, m ijson.Value) bool {
		switch {
		case annotations[k]:
		case k == "properties" || k == "$defs":
			sub := ijson.NewObject()
			m.(*ijson.Object).Each(func(name string, s ijson.Value) bool {
				var c ijson.Value
				c, ok = strip(s)
				sub.Set(name, c)
				return ok
			})
			out.Set(k, sub)
		case k == "items" || k == "additionalProperties":
			var c ijson.Value
			c, ok = strip(m)
			out.Set(k, c)
		case k == "prefixItems" || k == "anyOf" || k == "oneOf":
			list := m.([]ijson.Value)
			cs := make([]ijson.Value, len(list))
			for i, s := range list {
				if cs[i], ok = strip(s); !ok {
					break
				}
			}
			out.Set(k, cs)
		default:
			out.Set(k, m)
		}
		return ok
	})
	return out, ok
}
