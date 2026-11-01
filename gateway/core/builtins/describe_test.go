package builtins

import (
	"strings"
	"testing"

	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/documents"
	"atlas.local/sdk/go/protocol/ijson"
)

func describeOf(t *testing.T, w *World, c Call) *ijson.Object {
	t.Helper()
	c.Address = "atlas.describe@v1:fetch"
	d := w.Must(c)
	if fs := documents.Validate("describe", d); len(fs) > 0 {
		t.Fatalf("AP-DOC-001: not a describe answer: %v", fs)
	}
	return d
}

// ATLAS-DISCOVERY-003, rules 1, 3 and 4 of §2.6, R021: describe and guide
// are the working view. A capability the caller cannot use is refused by
// name with its status and a next step naming atlas.access; a
// confidential one they do not hold does not exist; a built-in is
// everyone's.
func TestR021DescribeAndGuideServeOnlyWhatTheCallerMayUse(t *testing.T) {
	w := NewWorld(t)
	exact := str(describeOf(t, w, Call{Who: Ana, Body: `{"by": "id", "id": "fund.positions@v1"}`}), "profile", "fingerprint")
	for _, c := range []struct {
		who, body, code string
	}{
		{Dan, `{"by": "id", "id": "fund.positions@v1"}`, protocol.CodeDenied},
		{Dan, `{"by": "id", "id": "fund.positions"}`, protocol.CodeDenied},
		{Dan, `{"by": "exact", "exact": "fund.positions@v1#` + exact + `"}`, protocol.CodeDenied},
		{Cal, `{"by": "id", "id": "fund.positions@v1"}`, protocol.CodeNotGranted},
		{Job, `{"by": "id", "id": "fund.list@v1"}`, protocol.CodeNotGranted},
		{Ana, `{"by": "id", "id": "fund.gone@v1"}`, protocol.CodeRetired},
		{Ana, `{"by": "id", "id": "fund.secret@v1"}`, protocol.CodeNotFound},
		{Cal, `{"by": "id", "id": "fund.secret"}`, protocol.CodeNotFound},
		{Ana, `{"by": "id", "id": "nothing.here@v1"}`, protocol.CodeNotFound},
	} {
		for _, address := range []string{"atlas.describe@v1:fetch", "atlas.guide@v1:fetch"} {
			prob := w.Refused(Call{Who: c.who, Address: address, Body: c.body})
			if prob.Code != c.code {
				t.Errorf("%s %s %s: %s, want %s", c.who, address, c.body, prob.Code, c.code)
			}
			if c.code != protocol.CodeNotFound && !strings.Contains(prob.Next, "atlas.access@v1") {
				t.Errorf("%s %s %s: next %q names no atlas.access", c.who, address, c.body, prob.Next)
			}
		}
	}
	describeOf(t, w, Call{Who: Adm, Body: `{"by": "id", "id": "fund.secret@v1"}`})
	describeOf(t, w, Call{Who: Cal, Body: `{"by": "id", "id": "atlas.guide@v1"}`})
	// Calling by name is explicit: a preview named is described.
	describeOf(t, w, Call{Who: Ana, Body: `{"by": "id", "id": "fund.preview@v1"}`})
}

// R075, governance.schema.json: held_by is how the caller holds an
// interaction (role:<name>, everyone), and each limit says who set it.
func TestR075GovernanceSaysHowTheCallerHoldsIt(t *testing.T) {
	w := NewWorld(t)
	d := describeOf(t, w, Call{Who: Bea, Body: `{"by": "id", "id": "fund.positions@v1"}`})
	if got := str(d, "governance", "for_caller", "interactions", "fetch", "held_by"); got != "role:fund-analyst" {
		t.Errorf("held_by %q", got)
	}
	limits := str(d, "governance", "for_caller", "limits")
	for _, want := range []string{`"kind":"output","max_rows":1000,"set_by":"user:` + Bea + `"`, `"kind":"rate","per_minute":600,"set_by":"everyone"`, `"kind":"concurrency","max":16,"set_by":"everyone"`} {
		if !strings.Contains(limits, want) {
			t.Errorf("limits %s lack %s", limits, want)
		}
	}
	if got := str(describeOf(t, w, Call{Who: Cal, Body: `{"by": "id", "id": "atlas.whoami@v1"}`}), "governance", "for_caller", "interactions", "fetch", "held_by"); got != "everyone" {
		t.Errorf("a built-in's held_by %q", got)
	}
	// Only the interactions the caller holds are theirs.
	j := describeOf(t, w, Call{Who: Job, Body: `{"by": "id", "id": "fund.positions@v1"}`})
	if str(j, "governance", "for_caller", "interactions") != `{"fetch":{"held_by":"role:fund-batch"}}` || strings.Contains(str(j, "eligibility"), "explain") {
		t.Errorf("job holds fetch alone: %s %s", str(j, "governance"), str(j, "eligibility"))
	}
}

// §9.C, ATLAS-DISCOVERY-008, R079: eligibility is for the calling client,
// served the next way down: a chunked binding with JSON is a Core client's,
// as a single answer. A binding's own delivery counts, and a critical
// trait the client lacks makes every binding ineligible.
func TestR079EligibilityIsTheNextWayDown(t *testing.T) {
	w := NewWorld(t)
	core := describeOf(t, w, Call{Who: Ana, Body: `{"by": "id", "id": "fund.positions@v1"}`})
	if got := str(core, "eligibility"); !strings.Contains(got, `{"binding":"/v1/data/fund.positions@v1:fetch","encoding":"json"`) || strings.Contains(got, `"ineligible":[{`) {
		t.Errorf("a Core client: %s", got)
	}
	streaming := describeOf(t, w, Call{Who: Ana, Body: `{"by": "id", "id": "fund.positions@v1"}`, Features: clientFeatures(t, "levels=(core streaming), deliveries=(single chunked operation), encodings=(json ndjson)")})
	if got := str(streaming, "eligibility"); !strings.Contains(got, `"binding":"/v1/data/fund.positions@v1:fetch","encoding":"ndjson"`) {
		t.Errorf("a streaming client: %s", got)
	}
	if got := str(core, "eligibility"); !strings.Contains(got, `"max_rows":100000`) {
		t.Errorf("an eligible binding carries the caller's output limit: %s", got)
	}
	dates := str(describeOf(t, w, Call{Who: Ana, Body: `{"by": "id", "id": "fund.positions-dates@v1"}`}), "eligibility")
	if !strings.Contains(dates, `"eligible":[{"binding":"/v1/data/fund.positions-dates@v1:fetch","encoding":"json","limits":{"max_rows":100000}}]`) || !strings.Contains(dates, `"binding":"/v1/data/fund.positions-dates@v1:export","hint"`) || !strings.Contains(dates, `"delivery:transfer"`) {
		t.Errorf("a binding's own delivery: %s", dates)
	}
	exotic := str(describeOf(t, w, Call{Who: Ana, Body: `{"by": "id", "id": "exotic.curve@v1"}`}), "eligibility")
	if !strings.Contains(exotic, `"eligible":[]`) || !strings.Contains(exotic, `"trait:exotic"`) {
		t.Errorf("a critical trait the client lacks: %s", exotic)
	}
	if got := str(describeOf(t, w, Call{Who: Ana, Body: `{"by": "id", "id": "exotic.curve@v1"}`, Features: clientFeatures(t, "traits=(exotic)")}), "eligibility"); strings.Contains(got, "trait:") {
		t.Errorf("a client with the trait: %s", got)
	}
}

// §2.6 "Can it be served now?", R079: availability is the provider's
// report, now: unavailable while its source is down, available again after.
func TestR079AvailabilityIsTheProvidersNow(t *testing.T) {
	w := NewWorld(t)
	body := `{"by": "id", "id": "fund.positions@v1"}`
	if got := str(describeOf(t, w, Call{Who: Ana, Body: body}), "availability"); got != "available" {
		t.Fatal(got)
	}
	w.Stub.Down(true)
	if got := str(describeOf(t, w, Call{Who: Ana, Body: body}), "availability"); got != "unavailable" {
		t.Fatalf("while its source is down: %s", got)
	}
	w.Stub.Down(false)
	if got := str(describeOf(t, w, Call{Who: Ana, Body: body}), "availability"); got != "available" {
		t.Fatalf("after: %s", got)
	}
}

// D8 (gaps/2.md), R01: describe by exact answers the newest registered
// document for that exact id, with the current governance.
func TestD8DescribeByExactIsTheNewestDocument(t *testing.T) {
	w := NewWorld(t)
	first := describeOf(t, w, Call{Who: Ana, Body: `{"by": "id", "id": "fund.positions@v1"}`})
	exact := "fund.positions@v1#" + str(first, "profile", "fingerprint")
	if err := w.Registry.Register(w.Entries(Positions(func(s string) string {
		return strings.Replace(s, "Positions in one or more funds on a valuation date.", "What one or more funds hold on a valuation date.", 1)
	}))...); err != nil {
		t.Fatal(err)
	}
	d := describeOf(t, w, Call{Who: Ana, Body: `{"by": "exact", "exact": "` + exact + `"}`})
	if str(d, "profile", "identity", "summary") != "What one or more funds hold on a valuation date." || str(d, "governance", "governance_revision") != "pol-1" {
		t.Fatalf("by exact: %s %s", str(d, "profile", "identity"), str(d, "governance"))
	}
}
