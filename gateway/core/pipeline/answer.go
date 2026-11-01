package pipeline

import (
	"fmt"

	"atlas.local/gateway/core/access"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/schema"
)

// checked holds a record answer to its contract and the caller's scope (step
// 8): a record to the interaction's schema; a collection's page to it whole
// when it is the page's schema (catalog_page), else item by item, when it
// names what each item is (access_entry, page.schema.json). What reaches
// the caller is only what the contract declares (R106), and every value it
// scopes is inside the caller's scope (R05).
func (a *attempt) checked(s *schema.Schema, result string, rec *ijson.Object, scope ports.Scope) (*ijson.Object, *protocol.Problem) {
	if s == nil {
		return rec, nil
	}
	if result == "collection" && !s.Valid(rec) {
		items, _ := rec.Get("items")
		list, ok := items.([]ijson.Value)
		next, hasNext := rec.Get("next_cursor")
		if !ok || !hasNext {
			return nil, a.fault("the capability's answer is not a page")
		}
		page := ijson.NewObject()
		kept := make([]ijson.Value, len(list))
		for i, item := range list {
			o, isObject := item.(*ijson.Object)
			if !isObject {
				return nil, a.fault("an item of the capability's page is not a record", "item", i)
			}
			r, prob := a.check(s, o, scope)
			if prob != nil {
				return nil, prob
			}
			kept[i] = r
		}
		page.Set("items", kept)
		page.Set("next_cursor", next)
		if dropped := rec.Len() - page.Len(); dropped > 0 {
			a.log.Warn("the capability answered members its contract does not declare; they were withheld", "members", dropped)
		}
		return page, nil
	}
	return a.check(s, rec, scope)
}

// check holds one record to its schema and the caller's scope.
func (a *attempt) check(s *schema.Schema, rec *ijson.Object, scope ports.Scope) (*ijson.Object, *protocol.Problem) {
	if fs := s.Validate(rec); len(fs) > 0 {
		return nil, a.fault("the capability's answer does not match its contract", "pointer", fs[0].Pointer, "keyword", fs[0].Keyword)
	}
	if kept, dropped := declared(s, rec); dropped {
		a.log.Warn("the capability answered members its contract does not declare; they were withheld")
		rec = kept.(*ijson.Object)
	}
	vals, missing, ok := access.Evidence(s, rec, true)
	if !ok {
		return nil, a.fault("the capability answered a record without the evidence of its scope, and the gateway withheld it", "pointer", missing)
	}
	for _, v := range vals {
		if !scope.Allows(v.Dimension, v.Value) {
			return nil, a.fault("the capability answered data outside the caller's scope, and the gateway withheld it", "dimension", v.Dimension, "pointer", v.Pointer)
		}
	}
	return rec, nil
}

// declared is v with only the members s declares, recursively, and whether
// anything was left out (R106). Result schemas are open (§9.2.1), so a
// valid answer may carry members its contract never named; none of them
// reaches the caller, so a provider cannot pass data past the scope check
// under a name it does not know. An object schema with properties keeps
// those; a map keeps its entries, and an object without properties, or a
// union, is kept as it validated; an array's items are held to its items'
// schema.
func declared(s *schema.Schema, v ijson.Value) (ijson.Value, bool) {
	if s == nil || s.Union() {
		return v, false
	}
	switch x := v.(type) {
	case *ijson.Object:
		if len(s.Properties()) == 0 {
			return v, false
		}
		entries := mapped(s)
		names := x.Names()
		var out *ijson.Object
		for i, name := range names {
			e, _ := x.Get(name)
			ps, known := s.Property(name)
			keep, changed := e, !known && !entries
			if known {
				keep, changed = declared(ps, e)
			}
			if changed && out == nil {
				out = ijson.NewObject()
				for _, before := range names[:i] {
					b, _ := x.Get(before)
					out.Set(before, b)
				}
			}
			if out != nil && (known || entries) {
				out.Set(name, keep)
			}
		}
		if out == nil {
			return v, false
		}
		return out, true
	case []ijson.Value:
		it := s.Items()
		if it == nil {
			return v, false
		}
		var out []ijson.Value
		for i, e := range x {
			keep, changed := declared(it, e)
			if changed && out == nil {
				out = make([]ijson.Value, i, len(x))
				copy(out, x[:i])
			}
			if out != nil {
				out = append(out, keep)
			}
		}
		if out == nil {
			return v, false
		}
		return out, true
	}
	return v, false
}

// mapped reports whether an object schema is a map: its additionalProperties
// a schema, so a member it does not name is one of its entries (A-012).
func mapped(s *schema.Schema) bool {
	src, _ := s.Definition().Source().(*ijson.Object)
	if src == nil {
		return false
	}
	ap, _ := src.Get("additionalProperties")
	_, isSchema := ap.(*ijson.Object)
	return isSchema
}

// accessNext is the next step of a refusal of a capability the caller can
// see: atlas.access, with the capability to ask about (02-6 rule 4, 06-2
// rule 4), after what to do first.
func accessNext(first, pinned, what string) string {
	ask := fmt.Sprintf(`atlas.access@v1:fetch with {"id": %q} %s`, pinned, what)
	if first == "" {
		return "Call " + ask + "."
	}
	return first + "; " + ask + "."
}
