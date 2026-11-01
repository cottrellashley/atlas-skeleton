package sf

import (
	"encoding/json"
	"testing"

	"atlas.local/sdk/go/internal/refs"
)

// Every field of the vectors parses, or not, as the references' RFC 9651
// reading says (it agrees with http_sf but where RFC 4648 refuses surplus
// base64 padding, Q-SF-1), and each serialises to the same text.
func TestStructuredFieldVectors(t *testing.T) {
	type reading struct {
		OK         bool   `json:"ok"`
		Serialised string `json:"serialised"`
	}
	var v struct {
		Cases []struct {
			Name     string `json:"name"`
			Field    string `json:"field"`
			Expected struct {
				Item       reading `json:"item"`
				List       reading `json:"list"`
				Dictionary reading `json:"dictionary"`
			} `json:"expected"`
			Question string `json:"question"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(refs.Read(t, "vectors/structured-fields.json"), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Cases) < 4000 {
		t.Fatalf("only %d cases", len(v.Cases))
	}
	type field interface{ Serialize() (string, error) }
	failures := 0
	for _, c := range v.Cases {
		for _, r := range []struct {
			kind  string
			want  reading
			parse func(string) (field, error)
		}{
			{"item", c.Expected.Item, func(s string) (field, error) { return ParseItem(s) }},
			{"list", c.Expected.List, func(s string) (field, error) { return ParseList(s) }},
			{"dictionary", c.Expected.Dictionary, func(s string) (field, error) { return ParseDictionary(s) }},
		} {
			got, err := r.parse(c.Field)
			if (err == nil) != r.want.OK {
				failures++
				if failures <= 20 {
					t.Errorf("%s %s %q: parses=%v, the vector says %v (%v)", c.Name, r.kind, c.Field, err == nil, r.want.OK, err)
				}
				continue
			}
			if err != nil {
				continue
			}
			if s, err := got.Serialize(); err != nil || s != r.want.Serialised {
				failures++
				if failures <= 20 {
					t.Errorf("%s %s %q: serialises as %q, the vector says %q (%v)", c.Name, r.kind, c.Field, s, r.want.Serialised, err)
				}
			}
		}
	}
	if failures > 0 {
		t.Fatalf("%d disagreements", failures)
	}
}
