package builtins

import (
	"fmt"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"

	"atlas.local/sdk/go/protocol/documents"
	"atlas.local/sdk/go/protocol/ijson"
)

func guideOf(t *testing.T, w *World, who, body string) (*ijson.Object, string) {
	t.Helper()
	g := w.Must(Call{Who: who, Address: "atlas.guide@v1:fetch", Body: body})
	if fs := documents.Validate("builtins#guide_result", g); len(fs) > 0 {
		t.Fatalf("AP-DOC-001: the guide is not a guide_result: %v", fs)
	}
	return g, str(g, "markdown")
}

// R000, ATLAS-DISCOVERY-009: atlas.guide answers for every capability the
// caller may use, a built-in or one whose owner wrote no usage, and never
// panics.
func TestR000TheGuideAnswersEveryCapability(t *testing.T) {
	w := NewWorld(t)
	ps, err := Profiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		for _, client := range []string{"cli", "python", "node", "go", "mcp"} {
			_, md := guideOf(t, w, Ana, fmt.Sprintf(`{"by": "id", "id": %q, "client": %q}`, p.ID+"@v1", client))
			if !strings.Contains(md, "`"+p.ID+"@v1`") {
				t.Errorf("%s's guide does not name it:\n%s", p.ID, md)
			}
		}
	}
	if _, md := guideOf(t, w, Ana, `{"by": "id", "id": "fund.bare"}`); !strings.Contains(md, "## Parameters") {
		t.Fatalf("a profile without usage:\n%s", md)
	}
	// A request that is one of several shapes lists every shape's members.
	for _, id := range []string{"atlas.guide@v1", "atlas.describe@v1"} {
		_, md := guideOf(t, w, Cal, `{"by": "id", "id": "`+id+`"}`)
		for _, want := range []string{"Shape 2", "| `by` | `\"exact\"` | yes |", "| `exact` | exact-id | yes |", "| `id` | capability-id or pinned-id | yes |"} {
			if !strings.Contains(md, want) {
				t.Errorf("%s's guide lacks %s:\n%s", id, want, md)
			}
		}
	}
}

// R01 (REVIEW.md: "a guide's identity includes the prose revision"), D7:
// the guide's revisions are the contract, the served document, the
// governance and the context. A prose-only change moves the document
// revision and the guide with it, never the contract.
func TestR000TheGuidesRevisionsFollowTheDocument(t *testing.T) {
	w := NewWorld(t)
	before, md := guideOf(t, w, Ana, `{"by": "id", "id": "fund.positions@v1"}`)
	revs := get(before, "revisions")
	for _, member := range []string{"contract", "document", "governance", "context"} {
		if s, _ := get(revs, member).(string); s == "" {
			t.Fatalf("revisions has no %s: %s", member, str(revs))
		}
	}
	if str(revs, "governance") != "pol-1" || str(revs, "context") != "house-1" {
		t.Fatalf("revisions: %s", str(revs))
	}
	reworded := Positions(func(s string) string {
		s = strings.Replace(s, "Its share of the fund; a fund's weights sum to one.", "Its share of the fund's value, cash included.", 1)
		s = strings.Replace(s, "Wait retry_after seconds, then repeat the call.", "Wait retry_after seconds; the warehouse resumes by itself.", 1)
		return strings.Replace(s, `"change": "First published."`, `"change": "Reworded."`, 1)
	})
	if err := w.Registry.Register(w.Entries(reworded)...); err != nil {
		t.Fatal(err)
	}
	after, md2 := guideOf(t, w, Ana, `{"by": "id", "id": "fund.positions@v1"}`)
	if str(after, "exact") != str(before, "exact") || str(after, "revisions", "contract") != str(revs, "contract") {
		t.Fatalf("a prose change moved the contract: %s then %s", str(before, "exact"), str(after, "exact"))
	}
	if str(after, "revisions", "document") == str(revs, "document") || md2 == md || !strings.Contains(md2, "cash included") {
		t.Fatalf("the guide did not follow its document: %s then %s", str(revs), str(after, "revisions"))
	}
	// The same document, governance, context and client give the same guide.
	if _, again := guideOf(t, w, Ana, `{"by": "id", "id": "fund.positions@v1"}`); again != md2 {
		t.Fatal("the same revisions gave another guide")
	}
}

// ATLAS-DISCOVERY-010, R022: no guide names a capability the caller cannot
// use: not in related, not in a reference's hint, not in the owner's
// prose or an error's next, and never a confidential one.
func TestR022TheGuideNamesOnlyWhatTheCallerCanUse(t *testing.T) {
	w := NewWorld(t)
	_, job := guideOf(t, w, Job, `{"by": "id", "id": "fund.positions@v1"}`)
	for _, hidden := range []string{"fund.search", "fund.list", "fund.positions-dates", "fund.trades", "explain"} {
		if strings.Contains(job, hidden) {
			t.Errorf("job's guide names %s, which job cannot use:\n%s", hidden, job)
		}
	}
	if !strings.Contains(job, "data.freshness@v1") || !strings.Contains(job, "| `funds` | array of PortfolioCode | yes | — | One to ten. |") || !strings.Contains(job, "When not to: You need a fund's valuation dates.") {
		t.Errorf("job's guide lost what job can use:\n%s", job)
	}
	_, ana := guideOf(t, w, Ana, `{"by": "id", "id": "fund.positions@v1"}`)
	for _, shown := range []string{"`fund.search@v1`", "`fund.list@v1`", "`fund.positions-dates@v1`", "Valid values come from `fund.list@v1`", "fund.search@v1:fetch", "`explain`"} {
		if !strings.Contains(ana, shown) {
			t.Errorf("ana's guide lacks %s:\n%s", shown, ana)
		}
	}
	if strings.Contains(ana, "fund.trades") {
		t.Errorf("a guide names a capability nobody serves:\n%s", ana)
	}
	// ATLAS-DISCOVERY-005: a confidential capability is named only to its holders.
	if _, md := guideOf(t, w, Ana, `{"by": "id", "id": "fund.positions-dates@v1"}`); strings.Contains(md, "fund.secret") {
		t.Errorf("a guide disclosed a confidential capability:\n%s", md)
	}
	if _, md := guideOf(t, w, Adm, `{"by": "id", "id": "fund.positions-dates@v1"}`); !strings.Contains(md, "fund.secret@v1") {
		t.Errorf("its holder's guide does not name it:\n%s", md)
	}
	// Section 9: what refers to it, as well as what it refers to.
	if _, md := guideOf(t, w, Ana, `{"by": "id", "id": "fund.list@v1"}`); !strings.Contains(md[max(0, strings.Index(md, "## Related")):], "fund.positions-dates@v1") {
		t.Errorf("the guide does not list what refers to it:\n%s", md)
	}
}

// The nine sections of §2.6, in their order.
var sections = []string{"# ", "## When to use it", "## What you may do", "## Parameters", "## What comes back", "## Examples", "## Problems", "## Your limits and its cost", "## Related"}

// ATLAS-DISCOVERY-009, R080: every section in order, within max_tokens,
// shortened from the last and saying so, never losing sections 1 to 4 or
// a parameter's name.
func TestR080TheGuideKeepsItsSectionsWithinItsBudget(t *testing.T) {
	w := NewWorld(t)
	for _, budget := range []int{200, 300, 600, 4000} {
		g, md := guideOf(t, w, Bea, fmt.Sprintf(`{"by": "id", "id": "fund.positions@v1", "max_tokens": %d}`, budget))
		n, _ := get(g, "tokens").(ijson.Number).Int64()
		if int(n) > budget || int(n) != (len(md)+3)/4 {
			t.Errorf("max_tokens %d: tokens %d for %d bytes", budget, n, len(md))
		}
		at := -1
		for i, h := range sections {
			j := strings.Index(md, h)
			if i < 4 && j < 0 {
				t.Errorf("max_tokens %d: section %d (%q) is gone:\n%s", budget, i+1, h, md)
			}
			if j >= 0 {
				if j < at {
					t.Errorf("max_tokens %d: section %d is out of order:\n%s", budget, i+1, md)
				}
				at = j
			}
		}
		for _, param := range []string{"`funds`", "`as_of`", "`columns`"} {
			if !strings.Contains(md, param) {
				t.Errorf("max_tokens %d: the parameter %s is gone:\n%s", budget, param, md)
			}
		}
		if truncated := get(g, "truncated") == true; truncated != strings.Contains(md, "Shortened to fit max_tokens") || budget == 4000 && truncated {
			t.Errorf("max_tokens %d: truncated %v, and the guide says %v", budget, truncated, strings.Contains(md, "Shortened"))
		}
	}
	_, md := guideOf(t, w, Bea, `{"by": "id", "id": "fund.positions@v1"}`)
	for _, want := range []string{"Positions are confidential", "1000 rows", "600 calls a minute", "One warehouse query per call", "Run it"} {
		if !strings.Contains(md, want) {
			t.Errorf("bea's guide lacks %q:\n%s", want, md)
		}
	}
	// Section 3 lists only what the caller holds, and examples are only the
	// valid ones of interactions they hold.
	_, job := guideOf(t, w, Job, `{"by": "id", "id": "fund.positions@v1"}`)
	if strings.Contains(job, "Not a valid request") || strings.Contains(md, "Not a valid request") {
		t.Errorf("a guide shows an example its request schema refuses")
	}
	if i := strings.Index(md, "## What you may do"); i < 0 || !strings.Contains(md[i:], "`explain`") {
		t.Errorf("bea holds explain:\n%s", md)
	}
	if order := headings(md); !slices.Equal(order, sections) {
		t.Errorf("the sections: %q", order)
	}
	// Every capability, for every caller who may read its guide, at every
	// budget: within it, sections 1 to 4 kept, in order.
	for _, c := range w.Registry.Snapshot().Capabilities() {
		for _, who := range []string{Ana, Job, Adm} {
			for _, budget := range []int{200, 230, 350, 500, 1000, 20000} {
				g, prob := w.Serve(Call{Who: who, Address: "atlas.guide@v1:fetch", Body: fmt.Sprintf(`{"by": "id", "id": %q, "max_tokens": %d}`, c.Pinned.String(), budget)})
				if prob != nil {
					continue
				}
				md := str(g, "markdown")
				if n, _ := get(g, "tokens").(ijson.Number).Int64(); int(n) > budget {
					t.Errorf("%s for %s at %d: %d tokens", c.Pinned, who, budget, n)
				}
				if order := headings(md); len(order) < 4 || !slices.Equal(order[:4], sections[:4]) {
					t.Errorf("%s for %s at %d: sections %q", c.Pinned, who, budget, order)
				}
			}
		}
	}
}

// ATLAS-DISCOVERY-010, AGENTS.md (fuzz every parser): whatever an owner
// writes, the filter a guide's prose passes through leaves no mention of a
// capability the reader may not be told of, and keeps every clause that
// names none.
func FuzzGuideProse(f *testing.F) {
	for _, seed := range []string{
		"You need a fund's valuation dates: use fund.secret@v1. You need trades: use fund.list@v1.",
		"Call fund.secret@v1:fetch; then fund.list.", "e.g. a.b and fund.secret", "", "...;;::", "fund.secret@v1",
		"Fund codes, as fund.secret names them: one to ten.", "\xff\xfe fund.secret@v9:x:", "A: b; c. D! e?",
	} {
		f.Add(seed)
	}
	hidden := func(word string) bool { return !strings.Contains(word, "secret") }
	f.Fuzz(func(t *testing.T, s string) {
		g := &guide{r: guideReader{MayName: hidden}}
		out := g.prose(s)
		for _, m := range mention.FindAllString(out, -1) {
			if !hidden(m) {
				t.Fatalf("%q kept %s: %q", s, m, out)
			}
		}
		if g.mayNameAll(s) && out != s {
			t.Fatalf("prose naming nothing hidden was changed: %q to %q", s, out)
		}
	})
}

func headings(md string) []string {
	var out []string
	for _, line := range strings.Split(md, "\n") {
		for _, h := range sections {
			if strings.HasPrefix(line, h) && (h != "# " || len(out) == 0) && !slices.Contains(out, h) {
				out = append(out, h)
			}
		}
	}
	return out
}

// ATLAS-DISCOVERY-010, R022: a guide written for no reader in particular
// fails closed. It names no capability but the built-ins and its own, and
// holds none of its interactions, so code that writes a guide cannot leak
// a capability by leaving its reader unset.
func TestR022AGuideForNoReaderNamesNothing(t *testing.T) {
	w := NewWorld(t)
	p := w.Registry.Snapshot().Lookup("fund.positions@v1").Current
	md, _ := fitGuide(sectionsOf(p, "cli", guideReader{}), 0)
	for _, other := range []string{"fund.list", "fund.search", "fund.positions-dates", "data.freshness", "fund.trades"} {
		if strings.Contains(md, other) {
			t.Errorf("a guide for no reader names %s:\n%s", other, md)
		}
	}
	if !strings.Contains(md, "You hold none of its interactions.") || strings.Contains(md, "atlas call") {
		t.Errorf("a guide for no reader holds what it has not been told of:\n%s", md)
	}
}

// delimiters are a Markdown table row's cell delimiters: its bars, but
// those escaped (GFM: "include a pipe in a cell's content by escaping it").
func delimiters(row string) int {
	n := 0
	for i := range row {
		if row[i] == '|' && (i == 0 || row[i-1] != '\\') {
			n++
		}
	}
	return n
}

// R080, R000, ATLAS-DISCOVERY-009: what a guide quotes from its profile
// keeps the guide whole. A default with a bar and a backtick stays in its
// cell, as code; and an example whose request has a backtick is still Go
// that parses, so the example is one an agent can run.
func TestR080TheGuideQuotesWhatItShows(t *testing.T) {
	w := NewWorld(t)
	tick := "`"
	quirks := Positions(func(s string) string {
		s = strings.ReplaceAll(s, "fund.positions", "fund.quirks")
		s = strings.Replace(s, `"columns": {`, `"sep": {"type": "string", "default": "a|b`+tick+`c", "description": "The separator."}, "columns": {`, 1)
		return strings.Replace(s, `"request": {"funds": ["ALPHA"]}}`, `"request": {"funds": ["ALPHA"], "sep": "x`+tick+`y"}}`, 1)
	})
	w.Policy.Grants[0].Capabilities = append(w.Policy.Grants[0].Capabilities, "fund.quirks@v1")
	if err := w.Registry.Register(w.Entries(quirks)...); err != nil {
		t.Fatal(err)
	}
	_, md := guideOf(t, w, Ana, `{"by": "id", "id": "fund.quirks@v1", "client": "go"}`)
	var row string
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "| `sep` |") {
			row = line
		}
	}
	if delimiters(row) != 6 || !strings.Contains(row, "| ``\"a\\|b`c\"`` |") {
		t.Errorf("the default broke its row: %q", row)
	}
	var examples int
	for _, block := range strings.Split(md, "```go\n")[1:] {
		code, _, _ := strings.Cut(block, "```")
		if _, err := parser.ParseFile(token.NewFileSet(), "example.go", "package p\n\nfunc f() {\n"+code+"}\n", 0); err != nil {
			t.Errorf("an example is not Go: %v\n%s", err, code)
		}
		examples++
	}
	if examples == 0 || !strings.Contains(md, "x`y") {
		t.Errorf("no example of the request:\n%s", md)
	}
}
