package gate

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/profile"
	"atlas.local/sdk/go/protocol/schema"
)

// Ready is the agent-readiness check (§3.1.4a, ATLAS-VISION-002): every
// reason the profile is not ready for agents. A profile with any is served
// only as preview.
func Ready(p *profile.Profile) []string {
	var out []string
	miss := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	id, _ := p.Doc.Get("identity")
	summary, _ := id.(*ijson.Object).Get("summary")
	s, _ := summary.(string)
	if s == "" || utf8.RuneCountInString(s) > 200 || sentences(s) != 1 {
		miss("identity.summary is one sentence of at most 200 characters")
	}
	for name, sc := range p.Schemas {
		walkProperties(sc, "/schemas/"+name, func(ptr, prop string, ps *schema.Schema) {
			src, _ := ps.Source().(*ijson.Object)
			if src == nil {
				return // a member a logical type implies
			}
			if d, ok := src.Get("description"); prop != "" && (!ok || d == "") {
				miss("%s has a description", ptr)
			}
			if t := ps.Type(); t == "number" || t == "integer" {
				_, hasLogical := src.Get("x-atlas-logical")
				_, hasUnit := src.Get("x-atlas-unit")
				if !hasLogical && !hasUnit {
					miss("%s, a number, has x-atlas-logical or x-atlas-unit (count where it is a plain count)", ptr)
				}
			}
		})
	}
	if p.Usage == nil {
		miss("usage.when is set, and usage.examples has an example")
	} else {
		if w, _ := p.Usage.Get("when"); w == nil || w == "" {
			miss("usage.when is set")
		}
		ex, _ := p.Usage.Get("examples")
		list, _ := ex.([]ijson.Value)
		valid := false
		for _, e := range list {
			eo, _ := e.(*ijson.Object)
			if eo == nil {
				continue
			}
			req, _ := eo.Get("request")
			if s := exampleSchema(p, eo); s != nil && req != nil && s.Valid(req) {
				valid = true
			}
		}
		if !valid {
			miss("usage.examples has an example whose request is valid against its interaction's request schema")
		}
	}
	for i, e := range p.Errors {
		if e.Description == "" || e.Next == "" {
			miss("errors[%d] (%s) has a description and a next", i, e.Code)
		}
	}
	var need []string
	switch p.Family {
	case "data":
		need = []string{"grain", "source"}
		for _, in := range p.Interactions {
			if in.Result == "table" {
				need = append(need, "keys")
				break
			}
		}
		if p.HasTrait("AsOf") {
			need = append(need, "as_of")
		}
	case "event":
		need = []string{"emitted", "subject", "ordering", "retention", "history"}
	case "action":
		need = []string{"effects", "policy"}
	}
	for _, k := range need {
		if p.Meaning == nil {
			miss("meaning.%s is set", k)
			continue
		}
		if v, ok := p.Meaning.Get(k); !ok || v == "" {
			miss("meaning.%s is set", k)
		}
	}
	return out
}

// exampleSchema is the request schema of the interaction an example names,
// or of fetch, or of the first interaction that takes one.
func exampleSchema(p *profile.Profile, example *ijson.Object) *schema.Schema {
	name := "fetch"
	if v, ok := example.Get("interaction"); ok {
		name, _ = v.(string)
	}
	if in, ok := p.Interactions[name]; ok && in.Takes != "" {
		return p.Schemas[in.Takes]
	}
	if _, named := example.Get("interaction"); named {
		return nil
	}
	for _, n := range p.Order {
		if t := p.Interactions[n].Takes; t != "" {
			return p.Schemas[t]
		}
	}
	return nil
}

// sentences counts the sentences in s: the number of ".", "!" and "?" that
// end a word, and one more for text after the last.
func sentences(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n := 0
	for i, r := range s {
		if (r == '.' || r == '!' || r == '?') && (i+1 == len(s) || s[i+1] == ' ') {
			n++
		}
	}
	if last := s[len(s)-1]; last != '.' && last != '!' && last != '?' {
		n++
	}
	return n
}
