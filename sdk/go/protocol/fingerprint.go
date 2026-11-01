package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"atlas.local/sdk/go/protocol/grammar"
	"atlas.local/sdk/go/protocol/ijson"
)

// FP is a profile's fingerprint and what it was computed from (§9.4.2).
type FP struct {
	Value     string // the fingerprint: the first 16 characters of SHA256
	SHA256    string // the digest, 64 lowercase hexadecimal characters
	Canonical []byte // the projection, serialised by RFC 8785
}

// Fingerprint computes a profile's fingerprint (§9.4): its projection,
// serialised by RFC 8785, hashed by SHA-256, the first 16 hex characters.
func Fingerprint(profile *ijson.Object) (FP, error) {
	c, err := ijson.Canonical(Project(profile))
	if err != nil {
		return FP{}, err
	}
	sum := sha256.Sum256(c)
	digest := hex.EncodeToString(sum[:])
	return FP{Value: digest[:16], SHA256: digest, Canonical: c}, nil
}

// CheckFingerprint recomputes a received profile's fingerprint and compares
// it with the one it carries, as a client does with every profile (§9.4.2).
// A mismatch is invalid_document.
func CheckFingerprint(profile *ijson.Object) (FP, error) {
	fp, err := Fingerprint(profile)
	if err != nil {
		return FP{}, NewProblem(CodeInvalidDocument, err.Error())
	}
	carried, _ := profile.Get("fingerprint")
	s, _ := carried.(string)
	if grammar.Fingerprint(s) != nil || s != fp.Value {
		return fp, NewProblem(CodeInvalidDocument, fmt.Sprintf("the profile carries fingerprint %q, but its projection's is %s", s, fp.Value)).WithTarget("/fingerprint")
	}
	return fp, nil
}

// Project returns a profile's projection (§9.4.1): a copy without the
// members that do not change how a client calls, resolves or reads it. The
// profile itself is not changed.
func Project(profile *ijson.Object) *ijson.Object {
	p := ijson.Clone(profile).(*ijson.Object)
	for _, name := range []string{"fingerprint", "revision", "usage"} {
		p.Delete(name)
	}
	if identity, ok := object(p, "identity"); ok {
		for _, name := range []string{"title", "summary", "owner"} {
			identity.Delete(name)
		}
	}
	if errs, ok := array(p, "errors"); ok {
		for _, e := range errs {
			if o, ok := e.(*ijson.Object); ok {
				o.Delete("description")
				o.Delete("next")
			}
		}
	}
	if evolution, ok := object(p, "evolution"); ok {
		evolution.Delete("changelog")
	}
	if schemas, ok := object(p, "schemas"); ok {
		schemas.Each(func(_ string, s ijson.Value) bool {
			StripSchema(s)
			return true
		})
	}
	return p
}

// StripSchema removes, in place, the members of a schema that are prose
// (§9.4.1 steps 7 to 10). It removes them only at schema positions, so a
// property named title under properties is kept.
func StripSchema(v ijson.Value) {
	s, ok := v.(*ijson.Object)
	if !ok {
		return // a boolean schema has nothing to strip
	}
	for _, name := range []string{"title", "description", "$comment", "examples"} {
		s.Delete(name)
	}
	for _, name := range []string{"properties", "$defs"} {
		if m, ok := object(s, name); ok {
			m.Each(func(_ string, sub ijson.Value) bool {
				StripSchema(sub)
				return true
			})
		}
	}
	if items, ok := s.Get("items"); ok {
		StripSchema(items)
	}
	if ap, ok := s.Get("additionalProperties"); ok {
		StripSchema(ap)
	}
	for _, name := range []string{"prefixItems", "anyOf", "oneOf"} {
		if list, ok := array(s, name); ok {
			for _, sub := range list {
				StripSchema(sub)
			}
		}
	}
	if list, ok := array(s, "x-atlas-enum"); ok {
		for _, e := range list {
			if o, ok := e.(*ijson.Object); ok {
				o.Delete("description")
			}
		}
	}
	if d, ok := object(s, "x-atlas-deprecated"); ok {
		d.Delete("reason")
	}
}

func object(o *ijson.Object, name string) (*ijson.Object, bool) {
	v, _ := o.Get(name)
	m, ok := v.(*ijson.Object)
	return m, ok
}

func array(o *ijson.Object, name string) ([]ijson.Value, bool) {
	v, _ := o.Get(name)
	a, ok := v.([]ijson.Value)
	return a, ok
}
