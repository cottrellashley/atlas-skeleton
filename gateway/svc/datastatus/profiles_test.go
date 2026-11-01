package datastatus_test

import (
	"strings"
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/svc/datastatus"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

func served(t *testing.T) *profile.Profile {
	t.Helper()
	fs, err := datastatus.Capabilities()
	if err != nil || len(fs) != 1 {
		t.Fatalf("%d capabilities, %v", len(fs), err)
	}
	v, err := ijson.Parse(fs[0].Profile)
	if err != nil {
		t.Fatal(err)
	}
	p, err := profile.Parse(v.(*ijson.Object))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func describe(p *profile.Profile, path ...string) string {
	var v ijson.Value = p.Doc
	for _, name := range path {
		o, _ := v.(*ijson.Object)
		if o == nil {
			return ""
		}
		v, _ = o.Get(name)
	}
	o, _ := v.(*ijson.Object)
	if o == nil {
		return ""
	}
	d, _ := o.Get("description")
	s, _ := d.(string)
	return s
}

// R035: data.status states the calendar its expected dates follow, how
// often the register checks, and that a stale check leaves the status
// unchecked, a value of its enum.
func TestR035TheCalendarAndTheChecksAreStated(t *testing.T) {
	p := served(t)
	row := []string{"schemas", "result", "items", "properties"}
	if !strings.Contains(describe(p, append(row, "expected_date")...), "Monday to Friday") ||
		!strings.Contains(describe(p, append(row, "checked_at")...), "26 hours") ||
		!strings.Contains(describe(p, append(row, "status")...), "as this call finds it") {
		t.Fatal("the calendar, the register's checks and when the status is judged")
	}
	status, _ := p.Schemas["result"].Items().Property("status")
	for _, v := range []string{"current", "late", "unchecked", "UNKNOWN"} {
		if !status.EnumHas(v) {
			t.Errorf("the status enum has no %s", v)
		}
	}
}

// R110: the most sources one call may name is stated where a caller reads
// the request.
func TestR110TheBoundOnSourcesIsStated(t *testing.T) {
	if d := describe(served(t), "schemas", "request", "properties", "sources"); !strings.Contains(d, "At most 1000") {
		t.Fatalf("sources: %q", d)
	}
}
