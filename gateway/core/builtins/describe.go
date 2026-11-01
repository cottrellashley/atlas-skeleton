package builtins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"atlas.local/gateway/core/access"
	"atlas.local/gateway/core/registry"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/grammar"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/profile"
)

func errNoCapability() *protocol.Problem {
	return protocol.NewProblem(protocol.CodeNotFound, "no such capability, version or profile").WithNext("Call atlas.search@v1:fetch to find one.")
}

// find is the profile a describe or guide request names, and its
// capability: by exact id, the newest document registered for it (D8); by
// pinned id, its current profile; by capability id, the current profile of
// the highest version the caller can use.
//
// Describe and guide are the working view (§2.6): they serve a capability
// only to a caller who may use it, and refuse one they may not by name, as
// a call of it is refused (rule 4): denied or not_granted, then retired,
// each with a next step naming atlas.access. A confidential capability the
// caller does not hold is not_found, as one that does not exist (§9.3.3).
// The built-ins are everyone's. Calling by name is explicit, so a preview
// named is served, and a capability whose critical trait the client lacks
// is described with every binding ineligible.
func (v *view) find(req *ijson.Object) (*profile.Profile, *registry.Capability, error) {
	var prof *profile.Profile
	switch strMember(req, "by") {
	case "exact":
		prof = v.snap.Profile(strMember(req, "exact"))
	case "id":
		if c := v.capability(strMember(req, "id")); c != nil {
			prof = c.Current
		}
	}
	if prof == nil {
		return nil, nil, errNoCapability()
	}
	c := v.snap.Lookup(grammar.Pinned{ID: prof.ID, Version: prof.Version}.String())
	if c == nil {
		return nil, nil, errNoCapability()
	}
	if c.Builtin() {
		return prof, c, nil
	}
	cur := c.Current
	next := "Call atlas.access@v1:fetch with {\"id\": \"" + c.Pinned.String() + "\"} to see why, and who to ask."
	var allowed bool
	var deny *access.Decision
	for _, name := range cur.Order {
		d := v.s.Decide(cur, name)
		switch {
		case d.Allowed:
			allowed = true
		case d.Code == protocol.CodeNotFound:
			return nil, nil, errNoCapability()
		case d.Code == protocol.CodeDenied && deny == nil:
			deny = &d
		}
	}
	switch {
	case !allowed && deny != nil:
		reason := deny.Reason
		if reason == "" {
			reason = "a deny refuses " + c.Pinned.String()
		}
		return nil, nil, protocol.NewProblem(protocol.CodeDenied, reason).WithNext(next)
	case !allowed:
		return nil, nil, protocol.NewProblem(protocol.CodeNotGranted, "you hold no grant for "+c.Pinned.String()).WithNext(next)
	case cur.Lifecycle == "retired":
		return nil, nil, protocol.NewProblem(protocol.CodeRetired, c.Pinned.String()+" was retired at its sunset").WithNext(next)
	}
	return prof, c, nil
}

// describe is the machine contract and the caller's governance of it, its
// availability now and the calling client's eligibility (describe.schema
// .json). The governance, availability and eligibility are the caller's
// own and current, whichever document is described (R01, D8), and never
// cached: the edge answers every built-in no-store.
func (p *Provider) describe(ctx context.Context, v *view, req *ijson.Object) (*ports.Answer, error) {
	prof, c, err := v.find(req)
	if err != nil {
		return nil, err
	}
	held := map[string]any{}
	for _, name := range prof.Order {
		if d := v.s.Decide(c.Current, name); d.Allowed {
			held[name] = map[string]any{"held_by": v.heldBy(prof, d)}
		}
	}
	return record(map[string]any{
		"profile": prof.Doc,
		"governance": map[string]any{
			"capability":          c.Pinned.String(),
			"governance_revision": v.s.Policy.Revision,
			"for_caller":          map[string]any{"interactions": held, "limits": v.limits(c.Current), "approval": nil},
		},
		"availability": availability(ctx, c),
		"eligibility":  v.eligibility(prof, c),
	})
}

// health is what a provider may report of whether it can serve a
// capability now (§2.6, "Can it be served now?"): from what it already
// knows, such as a breaker or a switch, never by calling its source while
// a describe waits. A provider that does not report it is available.
type health interface {
	Available(ctx context.Context, capability string) bool
}

func availability(ctx context.Context, c *registry.Capability) string {
	if h, ok := c.Provider.(health); ok && !h.Available(ctx, c.Pinned.String()) {
		return "unavailable"
	}
	return "available"
}

// eligibility is, for each interaction the caller holds, the bindings the
// calling client can use and those it cannot, with what is missing
// (ATLAS-DISCOVERY-008). A client that lacks a delivery is served the next
// way down, never refused (§9.C): a chunked binding that writes JSON is a
// Core client's, as one answer. A binding's own delivery is its own
// (binding.schema.json), and a critical trait the client lacks makes every
// binding ineligible (§9.3.2).
func (v *view) eligibility(prof *profile.Profile, c *registry.Capability) []any {
	out := []any{}
	traits := v.unsupported(c.Current)
	maxRows := v.s.Limit(c.Current).MaxRows
	for _, name := range prof.Order {
		if !v.s.Decide(c.Current, name).Allowed {
			continue
		}
		in := prof.Interactions[name]
		eligible, ineligible := []any{}, []any{}
		for j, b := range in.Bindings {
			delivery := bindingDelivery(prof, name, j, in.Delivery)
			missing := []string{}
			for _, t := range traits {
				missing = append(missing, "trait:"+t)
			}
			if b.Protocol != "https" {
				missing = append(missing, "protocol:"+b.Protocol)
			}
			encoding, ok := v.encoding(delivery, b.Encodings)
			if !ok {
				if !v.accepts(delivery) {
					missing = append(missing, "delivery:"+delivery)
				} else {
					missing = append(missing, "encoding:"+strings.Join(b.Encodings, "|"))
				}
			}
			if len(missing) > 0 {
				ineligible = append(ineligible, map[string]any{"binding": b.Address, "missing": missing, "hint": "a client that declares them in Atlas-Client-Features can use this binding"})
				continue
			}
			e := map[string]any{"binding": b.Address, "encoding": encoding}
			if in.Result == "table" && maxRows > 0 {
				e["limits"] = map[string]any{"max_rows": maxRows}
			}
			eligible = append(eligible, e)
		}
		out = append(out, map[string]any{"interaction": name, "eligible": eligible, "ineligible": ineligible})
	}
	return out
}

// accepts reports whether the client takes a delivery. Every client is
// Core, and Core takes single and operation answers (§9.C).
func (v *view) accepts(delivery string) bool {
	return delivery == protocol.DeliverySingle || delivery == protocol.DeliveryOperation || v.features.Accepts(delivery)
}

// encoding is the first of a binding's encodings the client reads for a
// delivery: as itself when the client takes it, else the next way down,
// a chunked answer as one JSON answer (§9.C, §9.6.5).
func (v *view) encoding(delivery string, encodings []string) (string, bool) {
	reads := func(e string) bool { return e == protocol.EncodingJSON || v.features.Reads(e) }
	switch {
	case v.accepts(delivery):
		for _, e := range encodings {
			if reads(e) {
				return e, true
			}
		}
	case delivery == protocol.DeliveryChunked && slices.Contains(encodings, protocol.EncodingJSON):
		return protocol.EncodingJSON, true
	}
	return "", false
}

// bindingDelivery is the delivery a binding answers with: its own, where
// it declares one, else its interaction's (binding.schema.json delivery).
func bindingDelivery(prof *profile.Profile, interaction string, i int, def string) string {
	in, _ := prof.Doc.Get("interactions")
	o, _ := in.(*ijson.Object)
	if o == nil {
		return def
	}
	iv, _ := o.Get(interaction)
	io, _ := iv.(*ijson.Object)
	if io == nil {
		return def
	}
	bv, _ := io.Get("bindings")
	list, _ := bv.([]ijson.Value)
	if i >= len(list) {
		return def
	}
	b, _ := list[i].(*ijson.Object)
	if b == nil {
		return def
	}
	if d, ok := b.Get("delivery"); ok {
		if s, isString := d.(string); isString && s != "" {
			return s
		}
	}
	return def
}

// guide is the capability's guide for the caller and their client (§2.6),
// fitted to max_tokens. Its revisions are everything it was written from
// (R01, D7): the contract, the served document, the governance, the
// managed context and the catalogue whose capabilities it may name. The
// same revisions, caller and client give the same guide.
func (p *Provider) guide(v *view, req *ijson.Object) (*ports.Answer, error) {
	prof, c, err := v.find(req)
	if err != nil {
		return nil, err
	}
	client := strMember(req, "client")
	if client == "" {
		client = "cli"
	}
	document, err := documentRevision(prof)
	if err != nil {
		return nil, err
	}
	md, truncated := fitGuide(sectionsOf(prof, client, v.reader(prof, c)), intMember(req, "max_tokens", 4000))
	revisions := map[string]any{
		"contract": prof.FP.Value, "document": document, "governance": v.s.Policy.Revision,
		"catalog": strings.Trim(v.snap.ETag, `"`),
	}
	if r := v.s.Policy.HouseRulesRevision; r != "" {
		revisions["context"] = r
	}
	return record(map[string]any{
		"exact": prof.Exact().String(), "client": client, "markdown": md, "tokens": tokenCount(md), "truncated": truncated,
		"revisions": revisions,
	})
}

// documentRevision is the served document's revision (D2 of gaps/2.md):
// the first 16 hexadecimal characters of the SHA-256 of its RFC 8785 form,
// prose and all. It moves with every change a reader could see, where the
// fingerprint moves only with the contract (§9.4).
func documentRevision(p *profile.Profile) (string, error) {
	b, err := ijson.Canonical(p.Doc)
	if err != nil {
		return "", fmt.Errorf("builtins: %s: its document: %w", p.Exact(), err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16], nil
}

// reader is the caller as a guide is written for them.
func (v *view) reader(prof *profile.Profile, c *registry.Capability) guideReader {
	var referredBy []string
	for _, other := range v.listed() {
		if other != c && refersTo(other.Current, c.Pinned) {
			referredBy = append(referredBy, other.Pinned.String())
		}
	}
	return guideReader{
		Holds:      func(interaction string) bool { return v.s.Decide(c.Current, interaction).Allowed },
		MayName:    v.mayName,
		HouseRules: v.rulesNaming(c.Pinned.ID),
		Limits:     v.limitSentences(c.Current),
		ReferredBy: referredBy,
	}
}

// refersTo reports whether a profile refers to a capability: in its
// related capabilities or a reference of its schemas.
func refersTo(p *profile.Profile, to grammar.Pinned) bool {
	for _, r := range references(p) {
		if r == to.String() || r == to.ID {
			return true
		}
	}
	return false
}
