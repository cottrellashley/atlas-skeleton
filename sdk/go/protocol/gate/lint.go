package gate

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"atlas.local/sdk/go/protocol/grammar"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/profile"
	"atlas.local/sdk/go/protocol/schema"
)

// modes orders the compatibility modes from loosest to strictest (§3.3.2a).
var modes = map[string]int{"BACKWARD": 1, "FORWARD": 1, "BACKWARD_TRANSITIVE": 2, "FORWARD_TRANSITIVE": 2, "FULL": 3, "FULL_TRANSITIVE": 4}

// pathLike names a place on the caller's machine (ATLAS-CLIENTS-006).
var pathLike = regexp.MustCompile(`^(path|file|filename|dir|directory|folder)$|_(path|file|filename|dir|directory|folder)$`)

// Lint holds a profile to the rules it keeps on its own (§3.3.12 step 1a).
// Schemas left the Atlas Schema profile already fail profile.Parse; Lint
// checks what spans the profile. Every finding is Breaking: a profile that
// fails is not served. The atlas.* built-ins are fixed by the protocol and
// not linted (assumption A-016).
func Lint(p *profile.Profile, cat Catalogue) []Finding {
	if grammar.Builtin(p.ID) {
		return nil
	}
	var out []Finding
	refuse := func(ptr, format string, args ...any) {
		out = append(out, Finding{Breaking, ptr, fmt.Sprintf(format, args...)})
	}
	if d, _ := p.Doc.Get("identity"); d != nil {
		if dom, _ := d.(*ijson.Object).Get("domain"); dom != grammar.Domain(p.ID) {
			refuse("/identity/domain", "is %q, the first segment of %s", grammar.Domain(p.ID), p.ID)
		}
	}
	if _, ok := p.Doc.Get("errors"); !ok {
		refuse("/errors", "every capability declares its errors, an empty list if it has none of its own (ATLAS-PROFILE-005)")
	}
	if len(p.Order) == 0 {
		refuse("/interactions", "a capability has at least one interaction")
	}
	for _, name := range p.Order {
		in := p.Interactions[name]
		ptr := "/interactions/" + name
		if in.Deadline.Max == 0 {
			refuse(ptr+"/deadline", "every interaction declares a deadline maximum (ATLAS-PROFILE-005)")
		}
		https := false
		for _, b := range in.Bindings {
			https = https || b.Protocol == "https"
			if !strings.HasPrefix(b.Address, "/") {
				refuse(ptr+"/bindings", "an address begins with /: %q", b.Address)
			}
			if b.Protocol == "https" && b.Method != "POST" {
				refuse(ptr+"/bindings", "atlas/1.0 binds every interaction to POST, not %q", b.Method)
			}
		}
		if !https && in.Delivery != "transfer" {
			refuse(ptr+"/bindings", "every interaction has a binding over HTTPS, its fallback")
		}
	}
	for name, s := range p.Schemas {
		kind := p.Kinds[name]
		def := modes["FULL_TRANSITIVE"]
		if kind == schema.Request {
			def = modes["BACKWARD_TRANSITIVE"]
		}
		if m, ok := p.Compatibility[name]; ok {
			if modes[m] == 0 {
				refuse("/compatibility/"+name, "%q is not a compatibility mode", m)
			} else if modes[m] < def {
				refuse("/compatibility/"+name, "%s is looser than the default for this schema; a capability may only be stricter", m)
			}
		}
		walkProperties(s, "/schemas/"+name, func(ptr, prop string, ps *schema.Schema) {
			if prop != "" && pathLike.MatchString(prop) {
				if logical, _, _, _ := ps.Logical(); logical != "Path" && logical != "Uri" {
					refuse(ptr, "%q names a place on the caller's machine; what happens to a result there is the client's (ATLAS-CLIENTS-006). A place on the server is x-atlas-logical Path", prop)
				}
			}
			if ref := ps.Reference(); ref != "" && cat != nil {
				if pin, err := grammar.ParsePinned(ref); err == nil && !cat.Has(pin) {
					refuse(ptr+"/x-atlas-reference", "%s is not a capability the gateway serves", ref)
				}
			}
			if src, ok := ps.Source().(*ijson.Object); ok {
				if d, ok := src.Get("x-atlas-deprecated"); ok {
					checkSunset(d, ptr+"/x-atlas-deprecated", refuse)
				}
			}
		})
	}
	if p.Family == "resource" {
		for _, name := range p.Order {
			in := p.Interactions[name]
			if s, ok := p.Schemas[in.Returns]; ok && (name == "get" || name == "create" || name == "update") {
				for _, member := range []string{"name", "metadata"} {
					if _, ok := s.Property(member); !ok {
						refuse("/schemas/"+in.Returns, "a resource's record declares the envelope's %s, as resource.schema.json does (§9.3.4)", member)
					}
				}
			}
		}
	}
	if p.Lifecycle == "deprecated" {
		// The notice is measured from the deprecation the profile records,
		// never from the day it is checked, so a profile admitted once is
		// admitted unchanged until its sunset.
		props, _ := p.Doc.Get("properties")
		o, _ := props.(*ijson.Object)
		since, hasSince := o.Get("deprecated")
		sunset, hasSunset := o.Get("sunset")
		switch {
		case !hasSince:
			refuse("/properties/deprecated", "a deprecated version records the date it was deprecated, which Deprecation reports (§3.3.9)")
		case !hasSunset:
			refuse("/properties/sunset", "a deprecated version has a sunset (§3.3.9)")
		default:
			if notice, ok := noticeOf(since, sunset); !ok || notice < MinimumSunset {
				refuse("/properties/sunset", "a sunset is at least 90 days after the deprecation (§3.3.9)")
			}
		}
	}
	return out
}

// walkProperties visits every property of a schema, at every depth, and
// every array's items, which it names "".
func walkProperties(s *schema.Schema, ptr string, visit func(ptr, name string, p *schema.Schema)) {
	if s == nil {
		return
	}
	if items := s.Items(); items != nil {
		if ptr != "" && len(items.Properties()) == 0 {
			visit(ptr+"/items", "", items)
		}
		walkProperties(items, ptr+"/items", visit)
	}
	for _, name := range s.Properties() {
		p, _ := s.Property(name)
		at := ptr + "/properties/" + name
		visit(at, name, p)
		walkProperties(p, at, visit)
	}
}

func checkSunset(v ijson.Value, ptr string, refuse func(string, string, ...any)) {
	o, ok := v.(*ijson.Object)
	if !ok {
		return
	}
	since, _ := o.Get("since")
	sunset, has := o.Get("sunset")
	if !has {
		return
	}
	if notice, ok := noticeOf(since, sunset); ok && notice < MinimumSunset {
		refuse(ptr+"/sunset", "a sunset is at least 90 days after the deprecation (§3.3.9)")
	}
}

// noticeOf is the time from a deprecation's date to its sunset's, both
// dates; ok is false when either is not a date.
func noticeOf(since, sunset ijson.Value) (time.Duration, bool) {
	s1, _ := since.(string)
	s2, _ := sunset.(string)
	d1, err1 := grammar.ParseDate(s1)
	d2, err2 := grammar.ParseDate(s2)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	t1 := time.Date(d1.Year, d1.Month, d1.Day, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(d2.Year, d2.Month, d2.Day, 0, 0, 0, 0, time.UTC)
	return t2.Sub(t1), true
}
