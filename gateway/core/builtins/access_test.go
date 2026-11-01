package builtins

import (
	"strings"
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/documents"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// entryOf is the caller's access entry for one capability, each entry of
// the page valid as an access_entry.
func entryOf(t *testing.T, w *World, who, id string) *ijson.Object {
	t.Helper()
	page := w.Must(Call{Who: who, Address: "atlas.access@v1:fetch", Body: `{"id": "` + id + `"}`})
	if fs := documents.Validate("page", page); len(fs) > 0 {
		t.Fatalf("AP-DOC-001: not a page: %v", fs)
	}
	items, _ := get(page, "items").([]ijson.Value)
	if len(items) != 1 {
		t.Fatalf("%s's access to %s: %s", who, id, str(page))
	}
	if fs := documents.Validate("builtins#access_entry", items[0]); len(fs) > 0 {
		t.Fatalf("AP-DOC-001: not an access_entry: %v", fs)
	}
	return items[0].(*ijson.Object)
}

// ATLAS-DISCOVERY-004, §2.6's status table, R075: each status says what
// §2.6 says it says. Available: how it is held, and the limits with who set
// them. Not granted: who owns it and which role would grant it. Denied:
// that a deny refuses it. Partial: which. Deprecated: the sunset and the
// replacement.
func TestR075TheAccessViewSaysHowToGetIt(t *testing.T) {
	w := NewWorld(t)
	ana := entryOf(t, w, Ana, "fund.positions@v1")
	if str(ana, "status") != "available" || str(ana, "held_by") != "role:fund-analyst" || !strings.Contains(str(ana, "limits"), `"set_by":"everyone"`) {
		t.Errorf("available: %s", str(ana))
	}
	cal := entryOf(t, w, Cal, "fund.positions@v1")
	if str(cal, "status") != "not_granted" || !strings.Contains(str(cal, "next"), "fund-analyst") || !strings.Contains(str(cal, "next"), "Fund Data") {
		t.Errorf("not granted: %s", str(cal))
	}
	if get(cal, "limits") != nil || get(cal, "held_by") != nil {
		t.Errorf("limits and held_by are for what the caller holds: %s", str(cal))
	}
	dan := entryOf(t, w, Dan, "fund.positions@v1")
	if str(dan, "status") != "denied" || str(dan, "reason") != "Dan is barred from positions while on the deal team." {
		t.Errorf("denied: %s", str(dan))
	}
	job := entryOf(t, w, Job, "fund.positions@v1")
	if str(job, "status") != "partial" || !strings.Contains(str(job, "reason"), "explain") || !strings.Contains(str(job, "next"), "fund-analyst") || str(job, "interactions", "fetch", "held_by") != "role:fund-batch" {
		t.Errorf("partial: %s", str(job))
	}
	old := entryOf(t, w, Ana, "fund.old")
	if str(old, "status") != "deprecated" || !strings.Contains(str(old, "reason")+str(old, "next"), "2026-12-31") || !strings.Contains(str(old, "next"), "fund.positions@v1") {
		t.Errorf("deprecated: %s", str(old))
	}
	// ATLAS-DISCOVERY-005: a confidential capability is no one's but its holders'.
	page := w.Must(Call{Who: Ana, Address: "atlas.access@v1:fetch", Body: `{"domain": "fund"}`})
	if strings.Contains(str(page), "fund.secret") {
		t.Errorf("the access view disclosed a confidential capability: %s", str(page))
	}
	if adm := entryOf(t, w, Adm, "fund.secret@v1"); str(adm, "status") != "available" {
		t.Errorf("its holder: %s", str(adm))
	}
}

// familyPolicy grants the fund domain by family alone: Ana's group holds
// the role, and Cal holds nothing.
func familyPolicy() *ports.Policy {
	return &ports.Policy{
		Revision: "pol-family",
		Roles:    map[string][]string{"g-analysts": {"fund-reader"}},
		Grants:   []ports.Grant{{ID: "grant-family", Role: "fund-reader", Capabilities: []string{"fund.*"}}},
	}
}

// ATLAS-DISCOVERY-004, ATLAS-ACCESS-002, 06-3 rule 3, R075: the role the
// access view names is the role the access resolver itself lets use the
// capability. A family grant reaches the reads of a system reached as the
// caller, so not_granted names its role there (§2.6: "which role would
// grant it"); of a system reached as the gateway's own identity it reaches
// nothing, so it names none.
func TestR075AFamilyGrantNamesItsRole(t *testing.T) {
	w := NewWorldIn(t, registry.AsCaller)
	w.Policy = familyPolicy()
	if ana := entryOf(t, w, Ana, "fund.list@v1"); str(ana, "status") != "available" || str(ana, "held_by") != "role:fund-reader" {
		t.Errorf("a family grant's holder: %s", str(ana))
	}
	if cal := entryOf(t, w, Cal, "fund.list@v1"); str(cal, "status") != "not_granted" || !strings.Contains(str(cal, "next"), "the role fund-reader") {
		t.Errorf("not granted names no role a family grant gives: %s", str(cal))
	}
	w = NewWorld(t)
	w.Policy = familyPolicy()
	if ana := entryOf(t, w, Ana, "fund.list@v1"); str(ana, "status") != "not_granted" {
		t.Errorf("a family grant reached a system reached as the gateway: %s", str(ana))
	}
	if cal := entryOf(t, w, Cal, "fund.list@v1"); strings.Contains(str(cal, "next"), "fund-reader") {
		t.Errorf("named a role whose grant cannot reach it: %s", str(cal))
	}
}

// ATLAS-DISCOVERY-004, ATLAS-ACCESS-001, §2.6's status table, R075: a deny
// refuses an interaction whatever else the caller holds, so the access view
// never offers a grant for it. A partial entry says which interaction a
// deny refuses and why, and names a role only for what is merely not
// granted; never a role the caller already holds, nor one a deny refuses
// to its holders.
func TestR075ADenyIsNeverAnsweredWithARole(t *testing.T) {
	deny := func(subject string) ports.Deny {
		return ports.Deny{ID: "deny-explain", Subject: subject, Capabilities: []string{"fund.positions@v1"}, Interactions: []string{"explain"}, Reason: "Explain is closed during the audit."}
	}
	w := NewWorld(t)
	w.Policy.Denies = append(w.Policy.Denies, deny(Ana))
	ana := entryOf(t, w, Ana, "fund.positions@v1")
	if str(ana, "status") != "partial" || str(ana, "interactions", "explain", "status") != "denied" || !strings.Contains(str(ana, "reason"), "Explain is closed during the audit.") {
		t.Errorf("partial, explain denied: %s", str(ana))
	}
	if next := str(ana, "next"); strings.Contains(next, "fund-analyst") || !strings.Contains(next, "deny") || !strings.Contains(next, "Fund Data") {
		t.Errorf("ana was offered a role she holds for what a deny refuses: %s", next)
	}

	// Denied one interaction and not granted the other: the deny, and the
	// role for what is not granted.
	w = NewWorld(t)
	w.Policy.Denies = append(w.Policy.Denies, deny(Cal))
	cal := entryOf(t, w, Cal, "fund.positions@v1")
	if next := str(cal, "next"); str(cal, "status") != "denied" || !strings.Contains(str(cal, "reason"), "Explain is closed") || !strings.Contains(next, "for the role fund-analyst or the role fund-batch, which grants `fetch`.") || !strings.Contains(next, "deny refuses `explain`") {
		t.Errorf("denied explain, fetch not granted: %s", str(cal))
	}

	// A deny of a role refuses its holders: the role grants them nothing.
	w = NewWorld(t)
	w.Policy.Denies = append(w.Policy.Denies, deny("fund-analyst"))
	job := entryOf(t, w, Job, "fund.positions@v1")
	if str(job, "status") != "partial" || str(job, "interactions", "explain", "status") != "not_granted" || strings.Contains(str(job, "next"), "fund-analyst") {
		t.Errorf("named a role whose holders a deny refuses: %s", str(job))
	}
	if cal := entryOf(t, w, Cal, "fund.positions@v1"); str(cal, "status") != "not_granted" || !strings.Contains(str(cal, "next"), "the role fund-analyst") {
		t.Errorf("the role still grants fetch: %s", str(cal))
	}
}
