package resolve

import (
	"errors"
	"testing"

	"github.com/jhgonline/fi-app-atlas/sdk/go/internal/refs"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

// AP-RESOLVE-001: every case of resolution.json gives exactly its record,
// or resolution_failed at its target. AP-RESOLVE-002: every row case gives
// the same column by column, as a table of one row and of many.
func TestResolutionVectors(t *testing.T) {
	doc, err := ijson.Parse(refs.Read(t, "vectors/resolution.json"))
	if err != nil {
		t.Fatal(err)
	}
	headed, _ := ijson.Parse([]byte("{" + head + "}"))
	vocabulary, _ := headed.(*ijson.Object).Get("$vocabulary")
	str := func(o *ijson.Object, k string) string { v, _ := o.Get(k); s, _ := v.(string); return s }
	cases, _ := doc.(*ijson.Object).Get("cases")
	if len(cases.([]ijson.Value)) < 40 {
		t.Fatal("the resolution vectors are missing")
	}
	columnar := 0
	for _, cv := range cases.([]ijson.Value) {
		c := cv.(*ijson.Object)
		name := str(c, "name")
		kind := schema.Open
		if str(c, "record_kind") == "request" {
			kind = schema.Request
		}
		var chain []Profile
		ok := true
		steps, _ := c.Get("chain")
		for i, sv := range steps.([]ijson.Value) {
			step := sv.(*ijson.Object)
			exact, err := grammar.ParseExact(str(step, "profile"))
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			sch, _ := step.Get("schema")
			full := ijson.Clone(sch).(*ijson.Object) // a profile's record schema, with its root's head
			full.Set("$schema", schema.Dialect)
			full.Set("$vocabulary", vocabulary)
			// Some cases use a schema the profile itself refuses (an enum
			// without UNKNOWN) to test what resolution does with it.
			compiled, err := schema.CompileWith(full, kind, schema.Options{Unchecked: true})
			if err != nil {
				t.Errorf("%s: step %d's schema: %v", name, i, err)
				ok = false
				break
			}
			lv, _ := step.Get("lens")
			lens, err := ParseLens(lv)
			if err != nil {
				t.Errorf("%s: step %d's lens: %v", name, i, err)
				ok = false
				break
			}
			chain = append(chain, Profile{exact, lens, compiled})
		}
		if !ok {
			continue
		}
		writer, _ := grammar.ParseExact(str(c, "writer"))
		reader, _ := grammar.ParseExact(str(c, "reader"))
		var opt Options
		if cols, has := c.Get("columns"); has && cols != nil {
			for _, col := range cols.([]ijson.Value) {
				opt.Columns = append(opt.Columns, col.(string))
			}
		}
		if dyn, _ := c.Get("dynamic_schema"); dyn == true {
			opt.Dynamic = true
		}
		record, _ := c.Get("record")
		expected, wantRecord := c.Get("expected")
		problem, wantProblem := c.Get("problem")

		plan, err := New(chain, writer, reader, opt)
		var got *ijson.Object
		if err == nil {
			got, err = plan.Record(record.(*ijson.Object))
		}
		switch {
		case wantRecord && err != nil:
			t.Errorf("%s: %v; the vector expects %s", name, err, marshal(expected))
		case wantRecord && !ijson.Equal(got, expected):
			t.Errorf("%s:\n got %s\nwant %s", name, marshal(got), marshal(expected))
		case wantProblem:
			var e *Error
			target, hasTarget := problem.(*ijson.Object).Get("target")
			if !errors.As(err, &e) {
				t.Errorf("%s: resolved to %s; the vector expects resolution_failed", name, marshal(got))
			} else if hasTarget && e.Target != target {
				t.Errorf("%s: failed at %q; the vector at %v", name, e.Target, target)
			}
		}
		if str(c, "record_kind") == "row" && !opt.Dynamic && plan != nil {
			columnar++
			parity(t, chain, writer, reader, opt, marshal(record))
		}
	}
	if columnar == 0 {
		t.Fatal("no row case was checked column by column")
	}
}

func marshal(v ijson.Value) string {
	b, _ := ijson.Marshal(v)
	return string(b)
}
