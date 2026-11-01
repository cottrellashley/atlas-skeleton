package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Fingerprint identifies one exact contract: the first 16 lowercase
// hexadecimal characters of the SHA-256 of the profile's projection, the
// profile without its prose (§3.1.4). Two profiles that differ only in prose
// share a fingerprint.
//
// Provisional: the canonical form is sorted-key JSON without HTML escaping,
// not yet RFC 8785, and the projection follows §9.4.1's intent, not its
// exact member list. Only the gateway computes fingerprints in atlas/0.1.
func Fingerprint(p *Profile) (string, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return "", err
	}
	for _, k := range []string{"title", "summary", "owner", "revision"} {
		delete(m, k)
	}
	if ev, ok := m["evolution"].(map[string]any); ok {
		delete(ev, "changelog")
	}
	stripSchemaProse(m["request"])
	if res, ok := m["result"].(map[string]any); ok {
		if cols, ok := res["columns"].([]any); ok {
			for _, c := range cols {
				if cm, ok := c.(map[string]any); ok {
					delete(cm, "description")
				}
			}
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes.TrimRight(buf.Bytes(), "\n"))
	return hex.EncodeToString(sum[:])[:16], nil
}

// ExactOf returns the profile's exact id, id@vN#fingerprint.
func ExactOf(p *Profile) (Exact, error) {
	fp, err := Fingerprint(p)
	if err != nil {
		return Exact{}, err
	}
	return Exact{Pinned: p.Pinned(), Fingerprint: fp}, nil
}

// stripSchemaProse removes descriptions at schema positions only: a property
// that happens to be named description is kept.
func stripSchemaProse(v any) {
	s, ok := v.(map[string]any)
	if !ok {
		return
	}
	delete(s, "description")
	if props, ok := s["properties"].(map[string]any); ok {
		for _, p := range props {
			stripSchemaProse(p)
		}
	}
	stripSchemaProse(s["items"])
}
