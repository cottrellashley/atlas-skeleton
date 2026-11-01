package schema

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"atlas.local/sdk/go/internal/refs"
	"atlas.local/sdk/go/protocol/ijson"
)

// AP-SCHEMA-001: every validator gives the vectors' result for every case,
// and refuses exactly the schemas the Atlas Schema profile refuses, at the
// member that breaks the rule.
func TestSchemaVectors(t *testing.T) {
	doc, err := ijson.Parse(refs.Read(t, "vectors/schemas.json"))
	if err != nil {
		t.Fatal(err)
	}
	cases, _ := doc.(*ijson.Object).Get("cases")
	if len(cases.([]ijson.Value)) < 150 {
		t.Fatal("the schema vectors are missing")
	}
	str := func(o *ijson.Object, k string) string { v, _ := o.Get(k); s, _ := v.(string); return s }
	for _, cv := range cases.([]ijson.Value) {
		c := cv.(*ijson.Object)
		name := str(c, "name")
		sch, _ := c.Get("schema")
		switch str(c, "kind") {
		case "validate":
			// The validation vectors test 2020-12's semantics, some on schemas
			// the profile itself would refuse.
			s, err := CompileWith(sch, Open, Options{Unchecked: true})
			if err != nil {
				t.Errorf("%s: the schema does not compile: %v", name, err)
				continue
			}
			instance, ok := c.Get("instance")
			if text := str(c, "instance_text"); !ok {
				if instance, err = ijson.Parse([]byte(text)); err != nil {
					t.Errorf("%s: %v", name, err)
					continue
				}
			}
			e, _ := c.Get("expected")
			b, _ := ijson.Marshal(e)
			var want struct {
				Valid  bool `json:"valid"`
				Errors []struct {
					Pointer string `json:"pointer"`
					Keyword string `json:"keyword"`
				} `json:"errors"`
			}
			_ = json.Unmarshal(b, &want)
			var got, wanted []string
			fs := s.Validate(instance)
			for _, f := range fs {
				got = append(got, f.Pointer+" "+f.Keyword)
			}
			for _, w := range want.Errors {
				wanted = append(wanted, w.Pointer+" "+w.Keyword)
			}
			if (len(fs) == 0) != want.Valid || !slices.Equal(got, wanted) {
				t.Errorf("%s\n got: %s\nwant: %s", name, strings.Join(got, "; "), strings.Join(wanted, "; "))
			}
		case "profile":
			kind := Open
			if str(c, "role") == "request" {
				kind = Request
			}
			var opt Options
			if ff, ok := c.Get("free_form"); ok {
				for _, p := range ff.([]ijson.Value) {
					opt.FreeForm = append(opt.FreeForm, p.(string))
				}
			}
			_, err := CompileWith(sch, kind, opt)
			problem, refused := c.Get("problem")
			if !refused {
				if err != nil {
					t.Errorf("%s: refused: %v", name, err)
				}
				continue
			}
			target := str(problem.(*ijson.Object), "target")
			rule := str(c, "rule")
			var e *Error
			if !errors.As(err, &e) {
				t.Errorf("%s: accepted; the vectors refuse it at %s (%s)", name, target, rule)
				continue
			}
			if v := e.Violations[0]; v.Pointer != target || v.Rule != rule {
				t.Errorf("%s: refused at %q (%s), the vectors at %q (%s): %v", name, v.Pointer, v.Rule, target, rule, e)
			}
		}
	}
}
