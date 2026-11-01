package builtins

import (
	"fmt"
	"strings"
)

const vocabulary = `"$schema": "https://json-schema.org/draft/2020-12/schema",
    "$vocabulary": {
     "https://json-schema.org/draft/2020-12/vocab/core": true,
     "https://json-schema.org/draft/2020-12/vocab/applicator": true,
     "https://json-schema.org/draft/2020-12/vocab/validation": true,
     "https://json-schema.org/draft/2020-12/vocab/meta-data": true,
     "https://json-schema.org/draft/2020-12/vocab/format-assertion": true,
     "https://atlas.jhg.com/vocab/2026-10": false
    }`

// Positions is fund.positions@v1, written as portfolio.holdings@v1 is: a
// chunked table, an explain, a reference to the list of valid codes,
// errors whose next steps name other capabilities, and owner prose that
// names capabilities, one of them (fund.trades) served nowhere.
func Positions(edit func(string) string) string {
	doc := `{
 "identity": {"id": "fund.positions", "version": 1, "title": "Fund positions", "summary": "Positions in one or more funds on a valuation date.", "domain": "fund", "owner": "Fund Data"},
 "properties": {"family": "data", "stability": "official", "lifecycle": "stable", "visibility": "listed", "traits": [{"name": "Projectable", "critical": false}]},
 "interactions": {
  "fetch": {"effect": "read", "result": "table", "delivery": "chunked", "execution": ["gateway"], "takes": "request", "returns": "result",
   "deadline": {"default": "PT30S", "max": "PT2M"},
   "bindings": [{"protocol": "https", "server": "gateway", "method": "POST", "address": "/v1/data/fund.positions@v1:fetch", "encodings": ["ndjson", "json", "arrow"], "auth": "bearer"}]},
  "explain": {"effect": "read", "result": "record", "delivery": "single", "execution": ["gateway"], "takes": "request", "returns": "plan",
   "deadline": {"default": "PT30S", "max": "PT2M"},
   "bindings": [{"protocol": "https", "server": "gateway", "method": "POST", "address": "/v1/data/fund.positions@v1:explain", "encodings": ["json"], "auth": "bearer"}]}
 },
 "schemas": {
  "request": {` + vocabulary + `,
   "type": "object", "description": "Which funds, on which date.", "additionalProperties": false, "required": ["funds"],
   "properties": {
    "funds": {"type": "array", "description": "Fund codes, as fund.list names them: one to ten.", "minItems": 1, "maxItems": 10, "uniqueItems": true,
     "items": {"type": "string", "description": "A fund code.", "x-atlas-logical": "PortfolioCode", "x-atlas-reference": "fund.list@v1"}},
    "as_of": {"type": ["string", "null"], "format": "date", "default": null, "description": "The valuation date; null is each fund's latest."},
    "columns": {"type": ["array", "null"], "description": "The columns to return; every column when null.", "uniqueItems": true, "default": null,
     "items": {"type": "string", "description": "A column name.", "minLength": 1}}
   }},
  "result": {` + vocabulary + `,
   "type": "array", "description": "One row per security a fund holds on its valuation date.",
   "items": {"type": "object", "description": "One position.", "required": ["fund", "security", "weight"],
    "properties": {
     "fund": {"type": "string", "description": "The fund's code.", "x-atlas-logical": "PortfolioCode", "x-atlas-key": true},
     "security": {"type": "string", "description": "The security held.", "minLength": 1, "x-atlas-key": true},
     "weight": {"type": "number", "x-atlas-logical": "Fraction", "description": "Its share of the fund; a fund's weights sum to one."}
    }}},
  "plan": {` + vocabulary + `,
   "type": "object", "description": "What fetch would do.",
   "properties": {"statement": {"type": "string", "description": "The statement fetch would run."}}}
 },
 "errors": [
  {"code": "fund_not_found", "status": 404, "retryable": false, "description": "No fund has one of the codes asked for.", "next": "Call fund.search@v1:fetch to find the code, or fund.list@v1:fetch for the codes you may use."},
  {"code": "warehouse_suspended", "status": 503, "retryable": true, "description": "The warehouse is suspended or unreachable.", "next": "Wait retry_after seconds, then repeat the call."}
 ],
 "meaning": {
  "grain": "fund × security",
  "keys": ["fund", "security"],
  "null_as_of": "each fund's latest valuation; data.freshness@v1 says how fresh it is",
  "scope": "a fund outside the caller's data scope is refused, never silently left out",
  "source": "the test warehouse"
 },
 "usage": {
  "when": "You need what named funds hold on a date: securities, quantities, market values, weights or spreads.",
  "when_not": "You need a fund's valuation dates: use fund.positions-dates@v1. You need a fund's trades: use fund.trades@v1.",
  "examples": [
   {"title": "Latest positions of one fund", "interaction": "fetch", "request": {"funds": ["ALPHA"]}},
   {"title": "Weights on a date", "interaction": "fetch", "request": {"funds": ["ALPHA"], "as_of": "2026-10-28", "columns": ["fund", "weight"]}},
   {"title": "Not a valid request", "interaction": "fetch", "request": {"funds": []}}
  ],
  "related": ["fund.search@v1", "fund.list@v1", "fund.positions-dates@v1", "data.freshness@v1"],
  "cost": "One warehouse query per call, of the funds asked for."
 },
 "evolution": {"lens": [], "changelog": [{"date": "2026-10-31", "change": "First published."}]}
}`
	if edit != nil {
		doc = edit(doc)
	}
	return doc
}

// simple is a one-interaction table capability: what most of the world's
// capabilities are.
type simple struct {
	id, title, summary, when, whenNot string
	properties                        string   // members added to properties
	related                           []string // usage.related
	noUsage                           bool
	bindings                          string // the fetch bindings, when not the default
}

func (s simple) json() string {
	domain, _, _ := strings.Cut(s.id, ".")
	props := `"family": "data", "stability": "official", "lifecycle": "stable", "visibility": "listed", "traits": []`
	if s.properties != "" {
		props = s.properties
	}
	bindings := fmt.Sprintf(`[{"protocol": "https", "server": "gateway", "method": "POST", "address": "/v1/data/%s@v1:fetch", "encodings": ["json"], "auth": "bearer"}]`, s.id)
	if s.bindings != "" {
		bindings = s.bindings
	}
	usage := ""
	if !s.noUsage {
		related := "[]"
		if len(s.related) > 0 {
			related = `["` + strings.Join(s.related, `", "`) + `"]`
		}
		whenNot := ""
		if s.whenNot != "" {
			whenNot = fmt.Sprintf(`"when_not": %q, `, s.whenNot)
		}
		usage = fmt.Sprintf(`,
 "usage": {"when": %q, %s"examples": [{"title": "All of them", "interaction": "fetch", "request": {}}], "related": %s}`, s.when, whenNot, related)
	}
	return fmt.Sprintf(`{
 "identity": {"id": %q, "version": 1, "title": %q, "summary": %q, "domain": %q, "owner": "Fund Data"},
 "properties": {%s},
 "interactions": {"fetch": {"effect": "read", "result": "table", "delivery": "single", "execution": ["gateway"], "takes": "request", "returns": "result",
  "deadline": {"default": "PT30S", "max": "PT2M"}, "bindings": %s}},
 "schemas": {
  "request": {%s, "type": "object", "description": "What to look for.", "additionalProperties": false,
   "properties": {"words": {"type": "string", "description": "Words to look for.", "minLength": 1}}},
  "result": {%s, "type": "array", "description": "One row per fund.",
   "items": {"type": "object", "description": "A fund.", "required": ["code"],
    "properties": {"code": {"type": "string", "description": "The fund's code.", "x-atlas-logical": "PortfolioCode", "x-atlas-key": true}}}}
 },
 "errors": [],
 "meaning": {"grain": "one row per fund", "keys": ["code"], "source": "the test warehouse"}%s
}`, s.id, s.title, s.summary, domain, props, bindings, vocabulary, vocabulary, usage)
}

// DomainProfiles are the world's capabilities beside the built-ins.
func DomainProfiles() []string {
	return []string{
		Positions(nil),
		simple{id: "fund.list", title: "Funds", summary: "The funds you may read: code, name and base currency.",
			when: "You need the codes of the funds you may read.", whenNot: "You have a fund's name but not its code: use fund.search@v1.",
			related: []string{"fund.search@v1", "fund.positions@v1"}}.json(),
		simple{id: "fund.search", title: "Find a fund", summary: "Fund codes for loose words, ranked, with the reason each matched.",
			when: "You have a fund's name, or part of it, and need the code other capabilities take.", whenNot: "You want every fund you may read: use fund.list@v1.",
			related: []string{"fund.list@v1", "fund.positions@v1"}}.json(),
		simple{id: "fund.positions-dates", title: "Valuation dates", summary: "The dates a fund was valued: its positions and total market value on each.",
			when:    "You need the dates a fund has positions for, before asking fund.positions@v1 for one of them, or a fund's total value over time.",
			whenNot: "You need the positions themselves: use fund.positions@v1.", related: []string{"fund.positions@v1", "fund.list@v1", "fund.secret@v1"},
			bindings: `[{"protocol": "https", "server": "gateway", "method": "POST", "address": "/v1/data/fund.positions-dates@v1:fetch", "encodings": ["json"], "auth": "bearer"},
			 {"protocol": "https", "server": "gateway", "method": "POST", "address": "/v1/data/fund.positions-dates@v1:export", "encodings": ["json"], "auth": "bearer", "delivery": "transfer"}]`}.json(),
		simple{id: "data.freshness", title: "Data freshness", summary: "Each source's latest date, the date expected, and whether it is current.",
			when: "You need to know how fresh a source is before trusting its latest answer.", related: []string{"fund.positions@v1"}}.json(),
		simple{id: "fund.preview", title: "Fund liquidity", summary: "A preview of fund liquidity and its freshness.", when: "You are trying the liquidity preview.",
			properties: `"family": "data", "stability": "official", "lifecycle": "preview", "visibility": "listed", "traits": []`}.json(),
		simple{id: "exotic.curve", title: "Exotic curves", summary: "Curves whose freshness only an exotic client can read.", when: "You read exotic curves.",
			properties: `"family": "data", "stability": "official", "lifecycle": "stable", "visibility": "listed", "traits": [{"name": "Exotic", "critical": true}]`}.json(),
		simple{id: "fund.secret", title: "Secret ledger", summary: "The ledger only administrators may know of.", when: "You administer the ledger.",
			properties: `"family": "data", "stability": "official", "lifecycle": "stable", "visibility": "confidential", "traits": []`}.json(),
		simple{id: "fund.old", title: "Old positions", summary: "Positions in the old layout.", when: "You still read the old layout.",
			properties: `"family": "data", "stability": "official", "lifecycle": "deprecated", "visibility": "listed", "traits": [], "deprecated": "2026-09-01", "sunset": "2026-12-31", "replaced_by": "fund.positions@v1"`}.json(),
		simple{id: "fund.gone", title: "Gone", summary: "A retired capability.", when: "Never now.",
			properties: `"family": "data", "stability": "official", "lifecycle": "retired", "visibility": "listed", "traits": []`}.json(),
		simple{id: "fund.bare", title: "Bare", summary: "A capability whose owner wrote no usage.", noUsage: true}.json(),
	}
}
