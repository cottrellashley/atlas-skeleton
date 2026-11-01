package grammar

import (
	"encoding/json"
	"testing"

	"github.com/jhgonline/fi-app-atlas/sdk/go/internal/refs"
)

// AP-CONV-002: every rule accepts exactly the strings grammar.json says it
// accepts.
func TestGrammarVectors(t *testing.T) {
	var v struct {
		Rules []string `json:"rules"`
		Cases []struct {
			Name     string `json:"name"`
			Rule     string `json:"rule"`
			Input    string `json:"input"`
			Expected struct {
				Valid bool   `json:"valid"`
				Why   string `json:"why"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(refs.Read(t, "vectors/grammar.json"), &v); err != nil {
		t.Fatal(err)
	}
	check := func(f func(string) error) func(string) error { return f }
	rules := map[string]func(string) error{
		"segment":         check(Segment),
		"capability-id":   check(CapabilityID),
		"domain":          check(Segment),
		"version":         check(Version),
		"nz-digits":       check(NzDigits),
		"interaction":     check(Interaction),
		"fingerprint":     check(Fingerprint),
		"pinned-id":       func(s string) error { _, err := ParsePinned(s); return err },
		"exact-id":        func(s string) error { _, err := ParseExact(s); return err },
		"address":         func(s string) error { _, err := ParseAddress(s); return err },
		"protocol-ver":    func(s string) error { _, _, err := ParseProtocol(s); return err },
		"property-name":   check(PropertyName),
		"problem-code":    check(ProblemCode),
		"date":            func(s string) error { _, err := ParseDate(s); return err },
		"date-time":       func(s string) error { _, err := ParseDateTime(s); return err },
		"duration":        func(s string) error { _, err := ParseDuration(s); return err },
		"decimal":         func(s string) error { _, err := ParseDecimal(s); return err },
		"integer-str":     func(s string) error { _, err := ParseIntegerString(s); return err },
		"idempotency-key": check(IdempotencyKey),
		"problem-type":    check(ProblemType),
		"dataschema":      check(DataSchema),
		"event-type":      check(EventType),
		"resource-name":   check(ResourceName),
		"resource-id":     check(ResourceID),
		"operation-name":  check(OperationName),
		"entity-tag":      check(EntityTag),
	}
	for _, r := range v.Rules {
		if rules[r] == nil {
			t.Errorf("no implementation of the rule %s", r)
		}
	}
	if len(v.Cases) < 200 {
		t.Fatalf("only %d cases", len(v.Cases))
	}
	for _, c := range v.Cases {
		f := rules[c.Rule]
		if f == nil {
			continue
		}
		if err := f(c.Input); (err == nil) != c.Expected.Valid {
			t.Errorf("%s: valid=%v, the vector says %v (%s): %v", c.Name, err == nil, c.Expected.Valid, c.Expected.Why, err)
		}
	}
}
