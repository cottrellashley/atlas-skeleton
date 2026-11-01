package builtins

import (
	"encoding/json"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/jhgonline/fi-app-atlas/gateway/core/access"
	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/documents"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

func pageOf(t *testing.T, w *World, c Call) *ijson.Object {
	t.Helper()
	p := w.Must(c)
	if fs := documents.Validate("catalog-page", p); len(fs) > 0 {
		t.Fatalf("AP-DOC-001: not a catalogue page: %v", fs)
	}
	return p
}

// ATLAS-CORE §2 (Orient: "authorized scope"), R020: whoami gives the
// caller's own data scope, tenant and application, so an agent knows what
// it may ask for before it asks.
func TestR020WhoamiGivesTheCallersScope(t *testing.T) {
	w := NewWorld(t)
	// Every dimension scope is kept in is named, so one the caller has
	// nothing in reads as none, never as unrestricted (ScopeFor: "a
	// dimension they have no scope in as none").
	for who, scope := range map[string]string{
		Ana: `{"index":[],"portfolio":["ALPHA","BETA"]}`,
		Bea: `{"index":[],"portfolio":["ALPHA"]}`,
		Job: `{"index":[],"portfolio":["*"]}`,
		Cal: `{"index":[],"portfolio":[]}`,
	} {
		me := w.Must(Call{Who: who, Address: "atlas.whoami@v1:fetch"})
		if fs := documents.Validate("builtins#whoami_result", me); len(fs) > 0 {
			t.Fatalf("AP-DOC-001: %v", fs)
		}
		if got := str(me, "scope"); got != scope {
			t.Errorf("%s's scope %s, want %s", who, got, scope)
		}
		if str(me, "tenant") != "tenant-1" || str(me, "application") == "" {
			t.Errorf("%s's context: %s", who, str(me))
		}
	}
}

// ATLAS-DISCOVERY-002, §2.6 rule 2, R077: one availability rule in every
// built-in. A capability with a critical trait the client lacks is in no
// working view; a preview only in a catalogue that asked for previews,
// never in search; and whoami's domains are the domains available.
func TestR077OneAvailabilityRule(t *testing.T) {
	w := NewWorld(t)
	all := func(c Call, members map[string]any) []string {
		var out []string
		req := map[string]any{"page_size": 3}
		for k, v := range members {
			req[k] = v
		}
		for {
			b, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			c.Body = string(b)
			page := pageOf(t, w, c)
			out = append(out, pageIDs(page)...)
			next, _ := get(page, "next_cursor").(string)
			if next == "" {
				return out
			}
			req["cursor"] = next
		}
	}
	core := all(Call{Who: Ana, Address: "atlas.catalog@v1:fetch"}, nil)
	want := []string{"data.freshness", "fund.bare", "fund.list", "fund.old", "fund.positions", "fund.positions-dates", "fund.search"}
	if !slices.Equal(core, want) {
		t.Errorf("a Core client's catalogue %v, want %v", core, want)
	}
	exotic := clientFeatures(t, "traits=(exotic)")
	if got := all(Call{Who: Ana, Address: "atlas.catalog@v1:fetch", Features: exotic}, nil); !slices.Contains(got, "exotic.curve") {
		t.Errorf("a client with the trait: %v", got)
	}
	if got := all(Call{Who: Ana, Address: "atlas.catalog@v1:fetch"}, map[string]any{"previews": true}); !slices.Contains(got, "fund.preview") || slices.Contains(got, "exotic.curve") {
		t.Errorf("previews asked for: %v", got)
	}
	found := pageIDs(pageOf(t, w, Call{Who: Ana, Address: "atlas.search@v1:fetch", Body: `{"words": "freshness"}`}))
	if !slices.Equal(found, []string{"data.freshness"}) {
		t.Errorf("search lists only the working view: %v", found)
	}
	if got := pageIDs(pageOf(t, w, Call{Who: Ana, Address: "atlas.search@v1:fetch", Body: `{"words": "freshness"}`, Features: exotic})); !slices.Contains(got, "exotic.curve") || slices.Contains(got, "fund.preview") {
		t.Errorf("search for a client with the trait: %v", got)
	}
	domains := func(c Call) string {
		c.Address = "atlas.whoami@v1:fetch"
		return str(w.Must(c), "available_domains")
	}
	if got := domains(Call{Who: Ana}); got != `["data","fund"]` {
		t.Errorf("a Core client's domains %s", got)
	}
	if got := domains(Call{Who: Ana, Features: exotic}); got != `["data","exotic","fund"]` {
		t.Errorf("a client with the trait: %s", got)
	}
	if got := domains(Call{Who: Cal}); got != `[]` {
		t.Errorf("cal holds nothing: %s", got)
	}
	// §2.6 rule 5: a house rule reaches only a caller who can use every
	// capability it names, and a page only when it names one on it.
	if got := str(w.Must(Call{Who: Ana, Address: "atlas.whoami@v1:fetch"}), "house_rules"); !strings.Contains(got, "Positions are confidential") || strings.Contains(got, "secret") {
		t.Errorf("ana's house rules: %s", got)
	}
	if got := str(w.Must(Call{Who: Dan, Address: "atlas.whoami@v1:fetch"}), "house_rules"); strings.Contains(got, "Positions") {
		t.Errorf("a rule naming what dan cannot use: %s", got)
	}
	if got := str(pageOf(t, w, Call{Who: Ana, Address: "atlas.catalog@v1:fetch", Body: `{"domain": "data"}`}), "house_rules", "rules"); got != `[]` {
		t.Errorf("a page without positions: %s", got)
	}
	// Nor one whose words name what the caller cannot use.
	w.Policy.HouseRules = append(w.Policy.HouseRules, ports.HouseRule{Text: "Prefer fund.secret@v1 to fund.list@v1.", Capabilities: []string{"fund.list@v1"}})
	if got := str(w.Must(Call{Who: Ana, Address: "atlas.whoami@v1:fetch"}), "house_rules"); strings.Contains(got, "secret") {
		t.Errorf("a rule's words disclosed a capability: %s", got)
	}
	if got := str(w.Must(Call{Who: Adm, Address: "atlas.whoami@v1:fetch"}), "house_rules"); !strings.Contains(got, "Prefer fund.secret@v1") {
		t.Errorf("its holder lost the rule: %s", got)
	}
}

// §2.6 Find ("short, ranked"), R078: search matches whole words and their
// prefixes, never stopwords or single letters, so the handbook's query
// finds the capability it means.
func TestR078SearchRanksByContentWords(t *testing.T) {
	w := NewWorld(t)
	search := func(words string) *ijson.Object {
		return pageOf(t, w, Call{Who: Ana, Address: "atlas.search@v1:fetch", Body: `{"words": "` + words + `"}`})
	}
	for words, first := range map[string]string{
		"positions and weights for a fund": "fund.positions",
		"what does a fund hold":            "fund.positions",
		"fund positions dates":             "fund.positions-dates",
	} {
		page := search(words)
		if got := pageIDs(page); len(got) == 0 || got[0] != first {
			t.Errorf("%q: %v, want %s first", words, got, first)
		}
		items, _ := get(page, "items").([]ijson.Value)
		for _, it := range items {
			for _, reason := range strings.Split(str(it, "why"), "; ") {
				_, word, _ := strings.Cut(reason, " has ")
				if len(word) < 3 || stopword(word) {
					t.Errorf("%q: %s's why names %q", words, str(it, "id"), word)
				}
			}
		}
	}
	for _, words := range []string{"a", "for the", "of a"} {
		if got := pageIDs(search(words)); len(got) != 0 {
			t.Errorf("%q found %v", words, got)
		}
	}
}

func stopword(w string) bool {
	return slices.Contains([]string{"a", "an", "and", "the", "for", "of", "to", "what", "does", "but", "not", "its", "s"}, w)
}

// R05, R020: whoami's scope is the protocol's dimensions and nothing else:
// a dimension a policy names that the gateway does not keep scope in is
// enforced nowhere, so it is never shown as if it were.
func TestR020WhoamiNamesOnlyTheProtocolsDimensions(t *testing.T) {
	w := NewWorld(t)
	w.Policy.Scopes["g-analysts"] = ports.Scope{"portfolio": {"BETA", "ALPHA", "BETA"}, "desk": {"RATES"}}
	if got := str(w.Must(Call{Who: Ana, Address: "atlas.whoami@v1:fetch"}), "scope"); got != `{"index":[],"portfolio":["ALPHA","BETA"]}` {
		t.Errorf("ana's scope %s", got)
	}
}

// R05: the dimensions whoami names are every dimension the ports keep
// scope in, one for each of atlas/1.0's code types that names one
// (§9.2.3), so a dimension the protocol adds is never left out.
func TestR020TheScopesDimensionsAreTheProtocols(t *testing.T) {
	b, err := fs.ReadFile(documents.Files(), "atlas-schema.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	var logicals []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if l, ok := x["x-atlas-logical"].(map[string]any); ok {
				alts, _ := l["anyOf"].([]any)
				for _, alt := range append(alts, l) {
					a, _ := alt.(map[string]any)
					names, _ := a["enum"].([]any)
					for _, name := range names {
						if n, isString := name.(string); isString && !slices.Contains(logicals, n) {
							logicals = append(logicals, n)
						}
					}
				}
			}
			for _, e := range x {
				walk(e)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(doc)
	var want []string
	for _, l := range logicals {
		doc, err := ijson.Parse([]byte(`{"$schema": "https://json-schema.org/draft/2020-12/schema", "$vocabulary": {"https://json-schema.org/draft/2020-12/vocab/core": true, "https://json-schema.org/draft/2020-12/vocab/applicator": true, "https://json-schema.org/draft/2020-12/vocab/validation": true, "https://json-schema.org/draft/2020-12/vocab/meta-data": true, "https://json-schema.org/draft/2020-12/vocab/format-assertion": true, "https://atlas.jhg.com/vocab/2026-10": false}, "type": "string", "x-atlas-logical": "` + l + `"}`))
		if err != nil {
			t.Fatal(err)
		}
		s, err := schema.Compile(doc, schema.Open)
		if err != nil {
			continue // a logical type that is no string is no code
		}
		if d, ok := ports.DimensionOf(s); ok && !slices.Contains(want, d) {
			want = append(want, d)
		}
	}
	slices.Sort(want)
	if got := ports.DefaultDimensions().Names(); len(logicals) == 0 || !slices.Equal(got, want) {
		t.Fatalf("the protocol's dimensions %v; its code types name %v (of %d logical types)", got, want, len(logicals))
	}
}

// ATLAS-CORE §3 ("what discovery shows and what a call is allowed never
// disagree"), §2.6 rule 2, R077, R075: a built-in answers from one
// catalogue. A capability registered after the pipeline resolved the
// caller, and reached by a family grant, is listed and available to them,
// as a call of it is decided.
func TestR077OneCallReadsOneCatalogue(t *testing.T) {
	w := NewWorldIn(t, registry.AsCaller)
	w.Policy = familyPolicy()
	before := access.ResolveIn(w.Policy, Identities[Ana], w.Registry.Snapshot())
	if err := w.Registry.Register(w.Entries(simple{id: "fund.extra", title: "Extra", summary: "One more fund listing.", when: "A test."}.json())...); err != nil {
		t.Fatal(err)
	}
	if got := pageIDs(pageOf(t, w, Call{Who: Ana, Address: "atlas.catalog@v1:fetch", Subject: before})); !slices.Contains(got, "fund.extra") {
		t.Errorf("the catalogue left out what ana can call: %v", got)
	}
	page := w.Must(Call{Who: Ana, Address: "atlas.access@v1:fetch", Body: `{"id": "fund.extra@v1"}`, Subject: before})
	if got := str(page, "items"); !strings.Contains(got, `"status":"available"`) || !strings.Contains(got, `"held_by":"role:fund-reader"`) {
		t.Errorf("the access view disagrees with the call: %s", got)
	}
}

// R078, AGENTS.md (fuzz every parser): whatever words a caller sends,
// search reads at most maxQueryWords content words, each a run of letters
// and digits more than one letter long, none a stopword and none twice. It
// never panics, lists only the caller's working view, and every reason it
// gives names one of those words.
func FuzzSearchWords(f *testing.F) {
	for _, seed := range []string{
		"positions and weights for a fund", "what does a fund hold", "fund positions dates", "a", "for the", "",
		"fund\xe2\x80\x99s positions", "fund\xc2\xa0positions", "FUND\xe2\x80\x94POSITIONS", "\xff\xfe fund", "fund.positions@v1:fetch",
		strings.Repeat("ab ", 150), "Fund Funds funds FUNDS", "d\xc3\xa9j\xc3\xa0 vu", "e\xcc\x81t\xc3\xa9",
	} {
		f.Add(seed)
	}
	base := NewWorld(f)
	working := map[string]bool{}
	for _, id := range pageIDs(base.Must(Call{Who: Ana, Address: "atlas.catalog@v1:fetch", Body: `{"page_size": 500}`})) {
		working[id] = true
	}
	f.Fuzz(func(t *testing.T, s string) {
		s = strings.ToValidUTF8(s, string(utf8.RuneError)) // as JSON carries it
		ws := contentWords(s)
		if len(ws) > maxQueryWords {
			t.Fatalf("%q: %d words", s, len(ws))
		}
		for i, word := range ws {
			if utf8.RuneCountInString(word) < 2 || isStopword(word) || slices.Contains(ws[:i], word) {
				t.Fatalf("%q: the content word %q", s, word)
			}
			if strings.IndexFunc(word, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) }) >= 0 {
				t.Fatalf("%q: %q is not a word of letters and digits", s, word)
			}
		}
		body, err := json.Marshal(map[string]any{"words": s, "page_size": 500})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ijson.Parse(body); err != nil {
			return // not I-JSON (a noncharacter, say): the edge refuses it before any built-in reads it
		}
		w := *base
		w.T = t
		page := pageOf(t, &w, Call{Who: Ana, Address: "atlas.search@v1:fetch", Body: string(body)})
		items, _ := get(page, "items").([]ijson.Value)
		for _, it := range items {
			if !working[str(it, "id")] {
				t.Fatalf("%q found %s, outside ana's working view", s, str(it, "id"))
			}
			for _, reason := range strings.Split(str(it, "why"), "; ") {
				field, word, _ := strings.Cut(reason, " has ")
				if !slices.Contains([]string{"id", "title", "summary", "when"}, field) || !slices.Contains(ws, word) {
					t.Fatalf("%q: %s's reason %q names no content word of %q", s, str(it, "id"), reason, ws)
				}
			}
		}
	})
}
