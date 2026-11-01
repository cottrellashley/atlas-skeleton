package builtins

import (
	"strings"
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// firstPage is Ana's catalogue, two to a page, and the cursor of page two.
func firstPage(t *testing.T, w *World) (string, []string) {
	t.Helper()
	page := pageOf(t, w, Call{Who: Ana, Address: "atlas.catalog@v1:fetch", Body: `{"page_size": 2}`})
	next, _ := get(page, "next_cursor").(string)
	if next == "" {
		t.Fatalf("one page: %s", str(page))
	}
	return next, pageIDs(page)
}

func refusedAtCursor(t *testing.T, w *World, c Call, why string) {
	t.Helper()
	prob := w.Refused(c)
	if prob.Code != protocol.CodeInvalidRequest || prob.Target != "/cursor" || prob.Next == "" {
		t.Errorf("%s: %s %q", why, prob.Code, prob.Target)
	}
}

// ATLAS-MECH-009, §6.7.9, R076: a cursor is its caller's, for its request,
// for an hour, and for the listing it was cut from: after a policy or
// catalogue change it is refused, never resumed at the wrong place.
func TestR076ACursorIsBoundToItsListing(t *testing.T) {
	w := NewWorld(t)
	next, first := firstPage(t, w)
	if strings.Join(first, " ") != "data.freshness fund.bare" {
		t.Fatalf("page one: %v", first)
	}
	page2 := Call{Who: Ana, Address: "atlas.catalog@v1:fetch", Body: `{"page_size": 2, "cursor": "` + next + `"}`}
	if got := pageIDs(pageOf(t, w, page2)); strings.Join(got, " ") != "fund.list fund.old" {
		t.Fatalf("page two: %v", got)
	}
	refusedAtCursor(t, w, Call{Who: Bea, Address: page2.Address, Body: page2.Body}, "another principal")
	refusedAtCursor(t, w, Call{Who: Ana, Address: page2.Address, Body: `{"page_size": 3, "cursor": "` + next + `"}`}, "another request")
	refusedAtCursor(t, w, Call{Who: Ana, Address: "atlas.search@v1:fetch", Body: `{"words": "fund", "page_size": 2, "cursor": "` + next + `"}`}, "another listing")
	refusedAtCursor(t, w, Call{Who: Ana, Address: page2.Address, Body: `{"page_size": 2, "cursor": "` + next[:len(next)-2] + `AA"}`}, "a tampered cursor")
	for _, junk := range []string{"x", ".", "..", "e30.", strings.Repeat("A", 4096), next + "." + next} {
		refusedAtCursor(t, w, Call{Who: Ana, Address: page2.Address, Body: `{"page_size": 2, "cursor": "` + junk + `"}`}, "junk "+junk[:min(len(junk), 8)])
	}

	// A policy change that hides a capability already listed would skip
	// one still to come; the cursor is refused instead.
	w.Policy = WorldPolicy()
	w.Policy.Revision = "pol-2"
	w.Policy.Denies = append(w.Policy.Denies, ports.Deny{ID: "deny-ana-freshness", Subject: Ana, Capabilities: []string{"data.freshness@v1"}, Reason: "A test."})
	refusedAtCursor(t, w, page2, "after a policy change")
	w.Policy = WorldPolicy()
	if got := pageIDs(pageOf(t, w, page2)); strings.Join(got, " ") != "fund.list fund.old" {
		t.Fatalf("the same policy again: %v", got)
	}

	// So is one cut from a catalogue that has since changed.
	more := simple{id: "fund.extra", title: "Extra", summary: "One more.", when: "A test."}.json()
	w.Policy.Grants[0].Capabilities = append(w.Policy.Grants[0].Capabilities, "fund.extra@v1")
	if err := w.Registry.Register(w.Entries(more)...); err != nil {
		t.Fatal(err)
	}
	refusedAtCursor(t, w, page2, "after a catalogue change")

	// And one older than its hour (§6.7.9: cursors live one hour).
	next, _ = firstPage(t, w)
	page2.Body = `{"page_size": 2, "cursor": "` + next + `"}`
	w.Clock.Advance(time.Hour - time.Second)
	pageOf(t, w, page2)
	w.Clock.Advance(time.Second)
	refusedAtCursor(t, w, page2, "after its hour")
}

// ATLAS-MECH-009, R076: a listing that never issues a cursor accepts none.
func TestR076ListingsWithoutCursorsRefuseOne(t *testing.T) {
	w := NewWorld(t)
	for _, address := range []string{"atlas.clients@v1:fetch", "atlas.usage@v1:fetch", "atlas.operation@v1:list"} {
		refusedAtCursor(t, w, Call{Who: Ana, Address: address, Body: `{"cursor": "x"}`}, address)
		w.Must(Call{Who: Ana, Address: address})
	}
}
