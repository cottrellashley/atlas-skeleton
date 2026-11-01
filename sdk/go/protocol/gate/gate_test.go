package gate

import (
	"strings"
	"testing"

	"atlas.local/sdk/go/protocol/grammar"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/profile"
)

const voc = `"$schema": "https://json-schema.org/draft/2020-12/schema", "$vocabulary": {"https://json-schema.org/draft/2020-12/vocab/core": true, "https://json-schema.org/draft/2020-12/vocab/applicator": true, "https://json-schema.org/draft/2020-12/vocab/validation": true, "https://json-schema.org/draft/2020-12/vocab/meta-data": true, "https://json-schema.org/draft/2020-12/vocab/format-assertion": true, "https://atlas.jhg.com/vocab/2026-10": false}`

// base is portfolio.holdings@v1 as last published: a data capability, agent
// ready.
const base = `{
 "identity": {"id": "portfolio.holdings", "version": 1, "title": "Portfolio holdings", "summary": "Positions in one or more portfolios on a date.", "domain": "portfolio", "owner": "QFI"},
 "properties": {"family": "data", "stability": "official", "lifecycle": "stable", "visibility": "listed",
  "traits": [{"name": "Projectable", "critical": false}, {"name": "AsOf", "critical": false}, {"name": "Handoff", "critical": false}]},
 "interactions": {
  "fetch": {"effect": "read", "result": "table", "delivery": "chunked", "execution": ["gateway", "handoff"], "takes": "request", "returns": "result",
   "deadline": {"default": "PT2M", "max": "PT30M"},
   "bindings": [{"protocol": "https", "server": "gateway", "method": "POST", "address": "/v1/data/portfolio.holdings@v1:fetch", "encodings": ["arrow", "json", "ndjson"], "auth": "bearer"}]}},
 "schemas": {
  "request": {` + voc + `, "type": "object", "additionalProperties": false, "required": ["portfolios"], "properties": {
   "portfolios": {"type": "array", "description": "Portfolio codes.", "minItems": 1, "items": {"type": "string", "x-atlas-logical": "PortfolioCode"}},
   "as_of": {"type": ["string", "null"], "format": "date", "default": null, "description": "The date."},
   "asset_class": {"type": ["string", "null"], "enum": ["credit", "rates", null], "default": null, "description": "Credit or rates."}}},
  "result": {` + voc + `, "type": "array", "items": {"type": "object", "required": ["portfolio", "as_of", "mv", "status"], "properties": {
   "portfolio": {"type": "string", "x-atlas-logical": "PortfolioCode", "x-atlas-key": true, "description": "The portfolio."},
   "as_of": {"type": "string", "format": "date", "x-atlas-key": true, "description": "The date."},
   "mv": {"type": "number", "x-atlas-unit": "fraction", "description": "Weight."},
   "lots": {"type": ["integer", "null"], "x-atlas-unit": "count", "description": "Lots."},
   "status": {"type": "string", "enum": ["open", "sold", "UNKNOWN"], "x-atlas-enum": [{"value": "open"}, {"value": "sold"}, {"value": "UNKNOWN"}], "description": "Status."},
   "legacy": {"type": ["string", "null"], "description": "Old.", "x-atlas-deprecated": {"reason": "Unused.", "since": "2026-01-01", "sunset": "2026-06-01", "replaced_by": null}}}}}},
 "errors": [{"code": "portfolio_not_found", "status": 404, "retryable": false, "description": "No such portfolio.", "next": "Call portfolio.list@v1:fetch."}],
 "meaning": {"grain": "portfolio × security × date", "keys": ["portfolio", "as_of"], "as_of": "the last valuation on or before as_of", "source": "Harbour"},
 "usage": {"when": "You need positions.", "examples": [{"title": "Global credit", "request": {"portfolios": ["GLOBAL-CREDIT"]}}]},
 "evolution": {"lens": [], "changelog": [{"date": "2026-09-01", "change": "First."}]},
 "revision": 1
}`

func load(t *testing.T, doc string) *profile.Profile {
	t.Helper()
	v, err := ijson.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("%v", err)
	}
	p, err := profile.Parse(v.(*ijson.Object))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// next is the base with edits, naming the base as its previous profile and
// adding a changelog entry.
func next(t *testing.T, edits ...string) *profile.Profile {
	t.Helper()
	old := load(t, base)
	doc := base
	for i := 0; i+1 < len(edits); i += 2 {
		if !strings.Contains(doc, edits[i]) {
			t.Fatalf("the base has no %q", edits[i])
		}
		doc = strings.Replace(doc, edits[i], edits[i+1], 1)
	}
	doc = strings.Replace(doc, `"evolution": {"lens": [`, `"evolution": {"previous_profile": "`+old.Exact().String()+`", "lens": [`, 1)
	doc = strings.Replace(doc, `{"date": "2026-09-01", "change": "First."}]`, `{"date": "2026-09-01", "change": "First."}, {"date": "2026-10-30", "change": "Next."}]`, 1)
	return load(t, doc)
}

func classes(fs []Finding) string {
	var parts []string
	for _, f := range fs {
		parts = append(parts, f.Class.String()+" "+f.Pointer)
	}
	return strings.Join(parts, "; ")
}

func expectCheck(t *testing.T, p *profile.Profile, want ...string) Report {
	t.Helper()
	r := Check(p, load(t, base), nil)
	got := classes(r.Findings)
	if got != strings.Join(want, "; ") {
		t.Errorf("findings\n got: %s\nwant: %s\n%v", got, strings.Join(want, "; "), r.Findings)
	}
	return r
}

func TestTheBaseIsClean(t *testing.T) {
	p := load(t, base)
	if fs := Lint(p, nil); len(fs) > 0 {
		t.Fatalf("lint: %v", fs)
	}
	if rs := Ready(p); len(rs) > 0 {
		t.Fatalf("ready: %v", rs)
	}
	if r := Check(p, p, nil); len(r.Findings) > 0 {
		t.Fatalf("a profile against itself: %v", r.Findings)
	}
}

func TestProseIsNotAChange(t *testing.T) {
	p := load(t, strings.Replace(strings.Replace(base, `"Weight."`, `"The weight in the portfolio."`, 1), `"revision": 1`, `"revision": 2`, 1))
	if r := Check(p, load(t, base), nil); len(r.Findings) > 0 {
		t.Fatalf("prose: %v", r.Findings)
	}
}

// §3.3.4's worked example: a column renamed within @v1.
func TestRenameWithAliasAndLens(t *testing.T) {
	p := next(t,
		`"mv": {"type": "number", "x-atlas-unit": "fraction", "description": "Weight."}`, `"market_value": {"type": "number", "x-atlas-unit": "fraction", "description": "Weight.", "x-atlas-aliases": ["mv"]}`,
		`"required": ["portfolio", "as_of", "mv", "status"]`, `"required": ["portfolio", "as_of", "market_value", "status"]`,
		`"lens": []`, `"lens": [{"op": "rename", "from": "mv", "to": "market_value"}]`)
	r := expectCheck(t, p, "dangerous /schemas/result/items/properties/market_value")
	if r.Refused() {
		t.Fatal("a rename with aliases and a lens is served")
	}
	noLens := next(t,
		`"mv": {"type": "number", "x-atlas-unit": "fraction", "description": "Weight."}`, `"market_value": {"type": "number", "x-atlas-unit": "fraction", "description": "Weight.", "x-atlas-aliases": ["mv"]}`,
		`"required": ["portfolio", "as_of", "mv", "status"]`, `"required": ["portfolio", "as_of", "market_value", "status"]`)
	if r := Check(noLens, load(t, base), nil); !r.Refused() {
		t.Fatalf("a rename without a lens leaves earlier readers unable to read: %v", r.Findings)
	}
}

// ATLAS-VERSION-007: the field and the rule are named.
func TestRequiredParameterAdded(t *testing.T) {
	p := next(t, `"required": ["portfolios"], "properties": {`, `"required": ["portfolios", "pool"], "properties": {"pool": {"type": "string", "description": "The pool."},`)
	r := expectCheck(t, p, "breaking /schemas/request/properties/pool")
	if !r.Refused() || !strings.Contains(r.Reason(), "request property 'pool' became required: declare @v2") {
		t.Fatalf("reason: %q", r.Reason())
	}
	safe := next(t, `"required": ["portfolios"], "properties": {`, `"required": ["portfolios"], "properties": {"pool": {"type": ["string", "null"], "default": null, "description": "The pool."},`)
	expectCheck(t, safe)
}

// ATLAS-VERSION-001: every change is classified by §3.3.3 to §3.3.7.
func TestResultColumns(t *testing.T) {
	expectCheck(t, next(t, `"lots": {"type": ["integer", "null"]`, `"spread": {"type": "number", "x-atlas-logical": "Bp", "description": "Spread."}, "lots": {"type": ["integer", "null"]`))
	expectCheck(t, next(t, `"mv": {"type": "number",`, `"mv": {"type": ["number", "null"],`), "breaking /schemas/result/items/properties/mv")
	expectCheck(t, next(t, `"lots": {"type": ["integer", "null"]`, `"lots": {"type": "integer"`), "breaking /schemas/result/items/properties/lots")
	expectCheck(t, next(t, `"lots": {"type": ["integer", "null"], "x-atlas-unit": "count"`, `"lots": {"type": ["number", "null"], "x-atlas-unit": "count"`), "breaking /schemas/result/items/properties/lots")
	expectCheck(t, next(t, `"lots": {"type": ["integer", "null"], "x-atlas-unit": "count"`, `"lots": {"type": ["string", "null"]`), "breaking /schemas/result/items/properties/lots")
	expectCheck(t, next(t, `"mv": {"type": "number", "x-atlas-unit": "fraction"`, `"mv": {"type": "number", "x-atlas-unit": "bp"`, `"lens": []`, `"lens": [{"op": "convert_unit", "field": "mv", "from": "fraction", "to": "bp"}]`), "breaking /schemas/result/items/properties/mv; breaking /evolution/lens/0")
	expectCheck(t, next(t, `"mv": {"type": "number", "x-atlas-unit": "fraction"`, `"mv": {"type": "number", "x-atlas-unit": "days"`), "breaking /schemas/result/items/properties/mv")
	expectCheck(t, next(t, `"enum": ["open", "sold", "UNKNOWN"], "x-atlas-enum": [{"value": "open"}, {"value": "sold"}, {"value": "UNKNOWN"}]`, `"enum": ["open", "sold", "held", "UNKNOWN"], "x-atlas-enum": [{"value": "open"}, {"value": "sold"}, {"value": "held"}, {"value": "UNKNOWN"}]`),
		"dangerous /schemas/result/items/properties/status")
	// A column removed, before its sunset or after it: a new major version.
	expectCheck(t, next(t, `"lots": {"type": ["integer", "null"], "x-atlas-unit": "count", "description": "Lots."},`, ``), "breaking /schemas/result/items/properties/lots")
	expectCheck(t, next(t, `,
   "legacy": {"type": ["string", "null"], "description": "Old.", "x-atlas-deprecated": {"reason": "Unused.", "since": "2026-01-01", "sunset": "2026-06-01", "replaced_by": null}}`, ``, `"lens": []`, `"lens": [{"op": "drop", "field": "legacy"}]`), "breaking /schemas/result/items/properties/legacy; breaking /evolution/lens/0")
	// A result field added with no missing value for what was written before.
	expectCheck(t, next(t, `"required": ["portfolio", "as_of", "mv", "status"]`, `"required": ["portfolio", "as_of", "mv", "status", "spread"]`, `"lots": {"type": ["integer", "null"]`, `"spread": {"type": "number", "x-atlas-logical": "Bp", "description": "Spread."}, "lots": {"type": ["integer", "null"]`), "breaking /schemas/result/items/properties/spread")
}

func TestRequestValues(t *testing.T) {
	expectCheck(t, next(t, `"enum": ["credit", "rates", null]`, `"enum": ["credit", "rates", "equity", null]`))
	expectCheck(t, next(t, `"enum": ["credit", "rates", null]`, `"enum": ["credit", null]`), "breaking /schemas/request/properties/asset_class")
	expectCheck(t, next(t, `"minItems": 1,`, `"minItems": 2,`), "breaking /schemas/request/properties/portfolios")
	expectCheck(t, next(t, `"minItems": 1,`, ``))
	expectCheck(t, next(t, `"format": "date", "default": null, "description": "The date."`, `"format": "date", "default": null, "description": "The date.", "maxLength": 10`), "breaking /schemas/request/properties/as_of")
	expectCheck(t, next(t, `"as_of": {"type": ["string", "null"], "format": "date", "default": null`, `"as_of": {"type": "string", "format": "date", "default": "2026-01-01"`), "breaking /schemas/request/properties/as_of; breaking /schemas/request/properties/as_of")
}

func TestCommonChanges(t *testing.T) {
	expectCheck(t, next(t, `{"name": "AsOf", "critical": false}, `, ``), "breaking /properties/traits")
	expectCheck(t, next(t, `, {"name": "Handoff", "critical": false}`, ``))
	expectCheck(t, next(t, `{"name": "AsOf", "critical": false}`, `{"name": "AsOf", "critical": false}, {"name": "Licensed", "critical": true}`), "dangerous /properties/traits")
	expectCheck(t, next(t, `"deadline": {"default": "PT2M", "max": "PT30M"}`, `"deadline": {"default": "PT2M", "max": "PT10M"}`), "dangerous /interactions/fetch/deadline/max")
	expectCheck(t, next(t, `"effect": "read"`, `"effect": "write"`), "breaking /interactions/fetch/effect")
	expectCheck(t, next(t, `"address": "/v1/data/portfolio.holdings@v1:fetch"`, `"address": "/v2/data/portfolio.holdings@v1:fetch"`))
	expectCheck(t, next(t, `"errors": [`, `"errors": [{"code": "warehouse_suspended", "status": 503, "retryable": true, "description": "Suspended.", "next": "Wait."}, `), "dangerous /errors")
	expectCheck(t, next(t, `"lifecycle": "stable"`, `"lifecycle": "preview"`), "breaking /properties/lifecycle")
	expectCheck(t, next(t, `"visibility": "listed"`, `"visibility": "confidential"`), "dangerous /properties/visibility")
}

// §3.3.4: a change of grain is a new meaning.
func TestMeaning(t *testing.T) {
	expectCheck(t, next(t, `"grain": "portfolio × security × date"`, `"grain": "portfolio × security × timestamp"`), "breaking /meaning/grain")
}

func TestAnnouncementsAndLinks(t *testing.T) {
	old := load(t, base)
	doc := strings.Replace(base, `"enum": ["open", "sold", "UNKNOWN"], "x-atlas-enum": [{"value": "open"}, {"value": "sold"}, {"value": "UNKNOWN"}]`, `"enum": ["open", "sold", "held", "UNKNOWN"], "x-atlas-enum": [{"value": "open"}, {"value": "sold"}, {"value": "held"}, {"value": "UNKNOWN"}]`, 1)
	r := Check(load(t, doc), old, nil)
	got := classes(r.Findings)
	if !strings.Contains(got, "breaking /evolution/previous_profile") || !strings.Contains(got, "breaking /evolution/changelog") {
		t.Fatalf("a dangerous change names its previous profile and has a changelog entry: %s", got)
	}
	bad := next(t, `"lens": []`, `"lens": [{"op": "rename", "from": "nope", "to": "mv"}]`)
	if r := Check(bad, old, nil); !strings.Contains(classes(r.Findings), "breaking /evolution/lens/0") {
		t.Fatalf("a lens maps the previous profile onto this one: %v", r.Findings)
	}
}

type catalogue map[string]bool

func (c catalogue) Has(p grammar.Pinned) bool { return c[p.String()] }

// ATLAS-PROFILE-005, ATLAS-CLIENTS-006: the lint of §3.3.12 step 1a.
func TestLint(t *testing.T) {
	for name, doc := range map[string]string{
		"no errors member":    strings.Replace(base, `"errors": [{"code": "portfolio_not_found", "status": 404, "retryable": false, "description": "No such portfolio.", "next": "Call portfolio.list@v1:fetch."}],`, "", 1),
		"no deadline maximum": strings.Replace(base, `"deadline": {"default": "PT2M", "max": "PT30M"},`, "", 1),
		"no HTTPS binding":    strings.Replace(base, `"protocol": "https"`, `"protocol": "amqp"`, 1),
		"a GET binding":       strings.Replace(base, `"method": "POST"`, `"method": "GET"`, 1),
		"a path on the machine": strings.Replace(base, `"properties": {
   "portfolios"`, `"properties": {"output_path": {"type": ["string", "null"], "default": null, "description": "Where."},
   "portfolios"`, 1),
		"a looser mode":         strings.Replace(base, `"usage": {`, `"compatibility": {"result": "BACKWARD"}, "usage": {`, 1),
		"the wrong domain":      strings.Replace(base, `"domain": "portfolio"`, `"domain": "index"`, 1),
		"an unknown reference":  strings.Replace(base, `"x-atlas-logical": "PortfolioCode"}}`, `"x-atlas-logical": "PortfolioCode", "x-atlas-reference": "portfolio.list@v1"}}`, 1),
		"a sunset too soon":     strings.Replace(base, `"sunset": "2026-06-01"`, `"sunset": "2026-02-01"`, 1),
		"deprecated, no sunset": strings.Replace(base, `"lifecycle": "stable"`, `"lifecycle": "deprecated"`, 1),
	} {
		p := load(t, doc)
		if fs := Lint(p, catalogue{}); len(fs) == 0 {
			t.Errorf("%s: passed the lint", name)
		}
	}
	ok := strings.Replace(base, `"x-atlas-logical": "PortfolioCode"}}`, `"x-atlas-logical": "PortfolioCode", "x-atlas-reference": "portfolio.list@v1"}}`, 1)
	if fs := Lint(load(t, ok), catalogue{"portfolio.list@v1": true}); len(fs) > 0 {
		t.Fatalf("a reference the catalogue has: %v", fs)
	}
}

// ATLAS-VISION-002: the agent-readiness check of §3.1.4a.
func TestReady(t *testing.T) {
	for name, doc := range map[string]string{
		"a long summary":           strings.Replace(base, `"Positions in one or more portfolios on a date."`, `"`+strings.Repeat("word ", 50)+`."`, 1),
		"two sentences":            strings.Replace(base, `"Positions in one or more portfolios on a date."`, `"Positions. On a date."`, 1),
		"a property without words": strings.Replace(base, `"description": "Weight."`, `"title": "Weight"`, 1),
		"a number without a unit":  strings.Replace(base, `"x-atlas-unit": "count", `, ``, 1),
		"an invalid example":       strings.Replace(base, `"request": {"portfolios": ["GLOBAL-CREDIT"]}`, `"request": {"portfolios": []}`, 1),
		"no usage.when":            strings.Replace(base, `"when": "You need positions.", `, ``, 1),
		"an error without next":    strings.Replace(base, `, "next": "Call portfolio.list@v1:fetch."`, ``, 1),
		"meaning without keys":     strings.Replace(base, `"keys": ["portfolio", "as_of"], `, ``, 1),
		"AsOf without as_of":       strings.Replace(base, `"as_of": "the last valuation on or before as_of", `, ``, 1),
	} {
		if rs := Ready(load(t, doc)); len(rs) == 0 {
			t.Errorf("%s: ready", name)
		}
	}
}

// R03's counterexamples (DESIGN-REVIEW): the gate never classifies an old
// value it cannot represent as safe.
func TestTheReviewsCounterexamples(t *testing.T) {
	t.Run("a required field disappears; no default recovers its old value", func(t *testing.T) {
		r := expectCheck(t, next(t, `"mv": {"type": "number", "x-atlas-unit": "fraction", "description": "Weight."},`, ``, `"required": ["portfolio", "as_of", "mv", "status"]`, `"required": ["portfolio", "as_of", "status"]`),
			"breaking /schemas/result/items/properties/mv")
		if !r.Refused() {
			t.Fatal("a removal is refused within a version")
		}
	})
	t.Run("an integer becomes a number, and the writer sends 1.5", func(t *testing.T) {
		expectCheck(t, next(t, `"lots": {"type": ["integer", "null"], "x-atlas-unit": "count"`, `"lots": {"type": ["number", "null"], "x-atlas-unit": "count"`, `"lens": []`, `"lens": [{"op": "promote", "field": "lots", "from": "integer", "to": "number"}]`),
			"breaking /schemas/result/items/properties/lots; breaking /evolution/lens/0")
	})
	t.Run("a newer non-null reader meets historical nulls", func(t *testing.T) {
		expectCheck(t, next(t, `"lots": {"type": ["integer", "null"]`, `"lots": {"type": "integer"`), "breaking /schemas/result/items/properties/lots")
	})
	t.Run("a rename keeps its old name, and stays within the version", func(t *testing.T) {
		r := expectCheck(t, next(t,
			`"mv": {"type": "number", "x-atlas-unit": "fraction", "description": "Weight."}`, `"market_value": {"type": "number", "x-atlas-unit": "fraction", "description": "Weight.", "x-atlas-aliases": ["mv"]}`,
			`"required": ["portfolio", "as_of", "mv", "status"]`, `"required": ["portfolio", "as_of", "market_value", "status"]`,
			`"lens": []`, `"lens": [{"op": "rename", "from": "mv", "to": "market_value"}]`), "dangerous /schemas/result/items/properties/market_value")
		if r.Refused() {
			t.Fatal("an exact rename with its alias is served")
		}
	})
}

// variant is doc with edits, each an exact text and its replacement.
func variant(t *testing.T, doc string, edits ...string) string {
	t.Helper()
	for i := 0; i+1 < len(edits); i += 2 {
		if !strings.Contains(doc, edits[i]) {
			t.Fatalf("the profile has no %q", edits[i])
		}
		doc = strings.Replace(doc, edits[i], edits[i+1], 1)
	}
	return doc
}

func expectClassify(t *testing.T, oldDoc, newDoc string, want ...string) {
	t.Helper()
	if got := classes(Classify(load(t, oldDoc), load(t, newDoc))); got != strings.Join(want, "; ") {
		t.Errorf("findings\n got: %s\nwant: %s", got, strings.Join(want, "; "))
	}
}

// An interaction pointed at another schema is compared with the one it
// used, whatever the two are named.
func TestClassifyFollowsTheInteractionsSchemas(t *testing.T) {
	renamed := variant(t, base, `"returns": "result"`, `"returns": "rows"`, `"result": {"$schema"`, `"rows": {"$schema"`)
	expectClassify(t, base, renamed)
	incompatible := variant(t, renamed, `"mv": {"type": "number", "x-atlas-unit": "fraction"`, `"mv": {"type": "number", "x-atlas-unit": "days"`)
	expectClassify(t, base, incompatible, "breaking /schemas/rows/items/properties/mv")
	// The old schema still there, unused, does not hide the new one.
	both := variant(t, base, `"returns": "result"`, `"returns": "result2"`, `"result": {"$schema"`, `"result2": {`+voc+`, "type": "array", "items": {"type": "object", "properties": {"mv": {"type": "string", "description": "Weight."}}}}, "result": {"$schema"`)
	expectClassify(t, base, both, "breaking /schemas/result2/items/properties/mv", "breaking /schemas/result2/items/properties/mv", "breaking /schemas/result2/items/properties/portfolio", "breaking /schemas/result2/items/properties/as_of", "breaking /schemas/result2/items/properties/lots", "breaking /schemas/result2/items/properties/status", "breaking /schemas/result2/items/properties/legacy")
	// A request schema where there was none is safe only if {} still passes.
	start, end := strings.Index(base, `"request": {`+voc), strings.Index(base, `"result": {`+voc)
	none := variant(t, base[:start]+base[end:], `"takes": "request", `, ``)
	expectClassify(t, none, base, "breaking /interactions/fetch/takes")
	expectClassify(t, base, none, "breaking /interactions/fetch/takes")
}

// Every level is compared, the enclosing schema included.
func TestClassifyComparesEnclosingSchemas(t *testing.T) {
	req := `"type": "object", "additionalProperties": false, "required": ["portfolios"]`
	expectClassify(t, base, variant(t, base, req, `"type": "object", "additionalProperties": false, "maxProperties": 1, "required": ["portfolios"]`), "breaking /schemas/request")
	expectClassify(t, variant(t, base, req, `"type": "object", "additionalProperties": false, "maxProperties": 1, "required": ["portfolios"]`), base)
	expectClassify(t, base, variant(t, base, `"type": "array", "items": {"type": "object", "required"`, `"type": "array", "maxItems": 100, "items": {"type": "object", "required"`), "breaking /schemas/result")
	expectClassify(t, base, variant(t, base, `"items": {"type": "object", "required"`, `"items": {"type": "object", "minProperties": 5, "required"`), "breaking /schemas/result/items")
	expectClassify(t, base, variant(t, base, `"minItems": 1, "items"`, `"minItems": 1, "uniqueItems": true, "items"`), "breaking /schemas/request/properties/portfolios")
}

// A result is read in both directions: an earlier reader reads what the new
// writer sends, and a new reader reads what was written before.
func TestClassifyHistoricalResults(t *testing.T) {
	spread := `"lots": {"type": ["integer", "null"]`
	withSpread := variant(t, base, spread, `"spread": {"type": "number", "x-atlas-logical": "Bp", "description": "Spread."}, `+spread)
	required := `"required": ["portfolio", "as_of", "mv", "status"]`
	expectClassify(t, withSpread, variant(t, withSpread, required, `"required": ["portfolio", "as_of", "mv", "status", "spread"]`), "breaking /schemas/result/items/properties/spread")
	defaulted := variant(t, withSpread, `"spread": {"type": "number",`, `"spread": {"type": "number", "default": 0,`)
	expectClassify(t, defaulted, variant(t, defaulted, required, `"required": ["portfolio", "as_of", "mv", "status", "spread"]`))
	nullable := variant(t, withSpread, `"spread": {"type": "number",`, `"spread": {"type": ["number", "null"],`)
	expectClassify(t, nullable, variant(t, nullable, required, `"required": ["portfolio", "as_of", "mv", "status", "spread"]`))
	expectClassify(t, base, variant(t, base, required, `"required": ["portfolio", "as_of", "status"]`), "breaking /schemas/result/items/properties/mv")

	portfolio := `"portfolio": {"type": "string", "x-atlas-logical": "PortfolioCode",`
	bounded := variant(t, base, portfolio, portfolio+` "maxLength": 12,`)
	expectClassify(t, base, bounded, "breaking /schemas/result/items/properties/portfolio")
	expectClassify(t, bounded, base, "breaking /schemas/result/items/properties/portfolio")
	expectClassify(t, bounded, variant(t, bounded, `"maxLength": 12`, `"maxLength": 20`), "breaking /schemas/result/items/properties/portfolio")

	price := `"price": {"type": ["string", "null"], "format": "decimal", "x-atlas-logical": "Decimal(18,6)", "description": "Price."}, `
	priced := variant(t, base, spread, price+spread)
	expectClassify(t, priced, variant(t, priced, "Decimal(18,6)", "Decimal(12,2)"), "breaking /schemas/result/items/properties/price")
	expectClassify(t, priced, variant(t, priced, "Decimal(18,6)", "Decimal(20,8)"), "breaking /schemas/result/items/properties/price")

	// A request may still widen.
	limit := `"portfolios": {"type": "array", "description": "Portfolio codes.", "minItems": 1, "items": {"type": "string", "x-atlas-logical": "PortfolioCode"}}`
	limited := variant(t, base, limit, `"portfolios": {"type": "array", "description": "Portfolio codes.", "minItems": 1, "items": {"type": "string", "x-atlas-logical": "PortfolioCode", "maxLength": 12}}`)
	expectClassify(t, limited, variant(t, limited, `"maxLength": 12`, `"maxLength": 20`))
	expectClassify(t, limited, variant(t, limited, `"maxLength": 12`, `"maxLength": 8`), "breaking /schemas/request/properties/portfolios/items")
}

// The notice of a deprecated version is measured from the deprecation it
// records, never from the day the gate runs: the gate takes no clock, so a
// profile admitted once is admitted unchanged on every later day.
func TestDeprecationNoticeIsFromItsDate(t *testing.T) {
	deprecated := func(dates string) *profile.Profile {
		return load(t, variant(t, base, `"lifecycle": "stable"`, `"lifecycle": "deprecated"`+dates))
	}
	if fs := Lint(deprecated(`, "deprecated": "2026-10-01", "sunset": "2027-01-10"`), nil); len(fs) > 0 {
		t.Fatalf("a sunset 101 days after its deprecation: %v", fs)
	}
	for name, dates := range map[string]string{
		"a sunset 61 days on": `, "deprecated": "2026-10-01", "sunset": "2026-12-01"`,
		"no deprecation date": `, "sunset": "2027-06-01"`,
		"no sunset":           `, "deprecated": "2026-10-01"`,
	} {
		if fs := Lint(deprecated(dates), nil); len(fs) != 1 || !strings.HasPrefix(fs[0].Pointer, "/properties/") {
			t.Errorf("%s: %v", name, fs)
		}
	}
}

// R3-09: an array's items are compared whatever form they take: a schema,
// true (or absent: any item), or false (none). A request may only accept
// more: narrowing its items to false, or from any item to a schema, is
// breaking, and widening them is not. A result's items keep their form,
// since an earlier reader checks what a new writer sends and a new reader
// what was written before.
func TestR3_09BooleanItemsAreCompared(t *testing.T) {
	items := `"items": {"type": "string", "x-atlas-logical": "PortfolioCode"}`
	tags := func(items string) string {
		return `"tags": {"type": ["array", "null"], ` + items + `, "description": "Tags."}, "status": {`
	}
	for _, c := range []struct {
		name     string
		from, to [2]string
		want     string
	}{
		{"a request's items narrowed to none", [2]string{items, items}, [2]string{items, `"items": false`}, "breaking /schemas/request/properties/portfolios/items"},
		{"a request's items widened to any", [2]string{items, items}, [2]string{items, `"items": true`}, ""},
		{"a request's items left out: any", [2]string{items, items}, [2]string{", " + items, ""}, ""},
		{"a request's items narrowed from any to a schema", [2]string{items, `"items": true`}, [2]string{items, items}, "breaking /schemas/request/properties/portfolios/items"},
		{"a request's items widened from none", [2]string{items, `"items": false`}, [2]string{items, items}, ""},
		{"a result's items from a schema to any", [2]string{`"status": {`, tags(`"items": {"type": "string"}`)}, [2]string{`"status": {`, tags(`"items": true`)}, "breaking /schemas/result/items/properties/tags/items"},
		{"a result's items from any to a schema", [2]string{`"status": {`, tags(`"items": true`)}, [2]string{`"status": {`, tags(`"items": {"type": "string"}`)}, "breaking /schemas/result/items/properties/tags/items"},
	} {
		got := classes(Check(edited(t, c.to, edited(t, c.from, nil)), edited(t, c.from, nil), nil).Findings)
		if got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// edited is the base with one replacement; with previous, the profile after
// it, naming it and adding a changelog entry.
func edited(t *testing.T, edit [2]string, previous *profile.Profile) *profile.Profile {
	t.Helper()
	if !strings.Contains(base, edit[0]) {
		t.Fatalf("the base has no %q", edit[0])
	}
	doc := strings.Replace(base, edit[0], edit[1], 1)
	if previous != nil {
		doc = strings.Replace(doc, `"evolution": {"lens": [`, `"evolution": {"previous_profile": "`+previous.Exact().String()+`", "lens": [`, 1)
		doc = strings.Replace(doc, `{"date": "2026-09-01", "change": "First."}]`, `{"date": "2026-09-01", "change": "First."}, {"date": "2026-10-30", "change": "Next."}]`, 1)
	}
	return load(t, doc)
}

// R3-11: a nested result field renamed, even with its alias, is breaking.
// Resolution renames top-level fields only, so an earlier reader cannot
// read details.code from a writer that sends details.id. A top-level
// rename with its lens stays dangerous.
func TestR3_11ANestedResultRenameIsBreaking(t *testing.T) {
	details := func(field string) string {
		return `"details": {"type": "object", "required": ["` + field + `"], "properties": {"` + field + `": {"type": "string", "description": "The code."` +
			map[bool]string{true: `, "x-atlas-aliases": ["code"]`, false: ``}[field == "id"] + `}}, "description": "Details."}, "status": {`
	}
	from := [2]string{`"status": {`, details("code")}
	to := [2]string{`"status": {`, details("id")}
	got := classes(Check(edited(t, to, edited(t, from, nil)), edited(t, from, nil), nil).Findings)
	if !strings.Contains(got, "breaking /schemas/result/items/properties/details/properties/id") {
		t.Errorf("a nested result rename: %q", got)
	}
}

// R3-10: a lens acts on every schema of its profile that holds its field,
// so a lens naming a field that more than one schema holds would change an
// unrelated one too. Such a profile is refused until lenses carry their
// schema (A-202). Here the result's as_of is renamed while the request's
// as_of, unrelated, stays.
func TestR3_10ALensOverASharedFieldIsRefused(t *testing.T) {
	p := next(t,
		`"as_of": {"type": "string", "format": "date", "x-atlas-key": true, "description": "The date."}`,
		`"valued_on": {"type": "string", "format": "date", "x-atlas-key": true, "x-atlas-aliases": ["as_of"], "description": "The date."}`,
		`"required": ["portfolio", "as_of", "mv", "status"]`, `"required": ["portfolio", "valued_on", "mv", "status"]`,
		`"evolution": {"lens": [`, `"evolution": {"lens": [{"op": "rename", "from": "as_of", "to": "valued_on"}`)
	r := Check(p, load(t, base), nil)
	if !r.Refused() || !strings.Contains(r.Reason(), "more than one schema") {
		t.Fatalf("a lens over a field two schemas hold: %v", r.Findings)
	}
}
