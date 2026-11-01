package harbour

import (
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// The profiles are declared beside the code that fulfils them (§3.1.5). In
// atlas/0.1 they are Go values; profile files come with §9.3's document.

var (
	no  = false
	one = 1
)

func requestFor(listName, reference string) *protocol.Schema {
	return &protocol.Schema{
		Type: protocol.Type{Name: "object"}, AdditionalProperties: &no,
		Required: []string{listName},
		Properties: map[string]*protocol.Schema{
			listName: {
				Type: protocol.Type{Name: "array"}, MinItems: &one, Reference: reference,
				Description: "Which to read, by code.",
				Items:       &protocol.Schema{Type: protocol.Type{Name: "string"}},
			},
			"as_of": {
				Type: protocol.Type{Name: "string"}, Format: "date",
				Description: "The date to read; the latest date with data when omitted.",
			},
			"columns": {
				Type:        protocol.Type{Name: "array"},
				Description: "The columns to return; every column when omitted.",
				Items:       &protocol.Schema{Type: protocol.Type{Name: "string"}},
			},
		},
	}
}

func dataProfile(id, title, summary string, request *protocol.Schema, cols []protocol.Column, meaning map[string]string, ev *protocol.Evolution) *protocol.Profile {
	pinned := id + "@v1"
	binding := func(interaction string) []protocol.Binding {
		return []protocol.Binding{{Protocol: "https", Server: "gateway", Method: "POST", Address: "/v1/data/" + pinned + ":" + interaction,
			Encodings: []string{protocol.MediaArrowStream, protocol.MediaNDJSON}}}
	}
	return &protocol.Profile{
		ID: id, Version: 1, Title: title, Summary: summary, Domain: protocol.Domain(id), Owner: "QFI",
		Family: protocol.FamilyData, Stability: "official", Lifecycle: "stable", Visibility: "listed",
		Traits: []string{protocol.TraitProjectable, protocol.TraitFilterable, protocol.TraitAsOf, protocol.TraitKeyed, protocol.TraitHandoff},
		Interactions: map[string]protocol.Interaction{
			"fetch": {Effect: "read", Result: "table", Delivery: "chunked",
				Executions: []string{protocol.ExecutionGateway, protocol.ExecutionHandoff}, Bindings: binding("fetch")},
			"explain": {Effect: "read", Result: "record", Delivery: "single",
				Executions: []string{protocol.ExecutionGateway}, Bindings: binding("explain")},
		},
		Request: request, Result: &protocol.Table{Columns: cols}, Meaning: meaning, Evolution: ev,
	}
}

var sectorsV1 = []string{"Industrials", "Financials", "Utilities", "Energy", protocol.Unknown}

// holdings declares portfolio.holdings@v1 as it evolved, within its version:
//  1. the first profile;
//  2. mv renamed market_value: dangerous, so an alias and a rename lens;
//  3. oas moved from basis points to a fraction (a unit lens), a sector added
//     (older readers map it to UNKNOWN), a nullable currency added (safe).
func holdings() declaration {
	meaning := map[string]string{
		"grain":  "portfolio × security × date",
		"as_of":  "the portfolio's end-of-day holdings on as_of; the latest date with data when omitted",
		"source": "Harbour's portfolio holdings view",
	}
	key := func(name, desc string, t string) protocol.Column {
		return protocol.Column{Name: name, Type: t, Key: true, Description: desc}
	}
	base := []protocol.Column{
		key("portfolio", "The portfolio's code.", protocol.TypeString),
		key("security", "The security's ISIN.", protocol.TypeString),
		key("as_of", "The holdings date.", protocol.TypeDate),
		{Name: "mv", Type: protocol.TypeDecimal, Precision: 18, Scale: 2, Unit: "currency_unit", Description: "Market value in the portfolio's base currency."},
		{Name: "weight", Type: protocol.TypeDecimal, Precision: 9, Scale: 6, Logical: "Fraction", Description: "The position's share of the portfolio's market value."},
		{Name: "oas", Type: protocol.TypeNumber, Nullable: true, Logical: "Bp", Description: "Option-adjusted spread over the government curve."},
		{Name: "sector", Type: protocol.TypeString, Enum: sectorsV1, Description: "The issuer's sector."},
	}
	sources1 := map[string]ports.SourceColumn{
		"portfolio": {Writer: "portfolio", Source: "PORTFOLIO_CODE"}, "security": {Writer: "security", Source: "ISIN"},
		"as_of": {Writer: "as_of", Source: "AS_OF_DATE"}, "mv": {Writer: "mv", Source: "MARKET_VALUE"},
		"weight": {Writer: "weight", Source: "WEIGHT"}, "oas": {Writer: "oas", Source: "OAS_BP"},
		"sector": {Writer: "sector", Source: "SECTOR"},
	}
	r1 := dataProfile("portfolio.holdings", "Portfolio holdings", "Positions in one or more portfolios on a date.",
		requestFor("portfolios", "portfolio.list@v1"), base, meaning, nil)

	cols2 := append([]protocol.Column(nil), base...)
	cols2[3] = protocol.Column{Name: "market_value", Type: protocol.TypeDecimal, Precision: 18, Scale: 2, Unit: "currency_unit",
		Aliases: []string{"mv"}, Description: "Market value in the portfolio's base currency."}
	sources2 := copyMap(sources1)
	delete(sources2, "mv")
	sources2["market_value"] = ports.SourceColumn{Writer: "market_value", Source: "MARKET_VALUE"}
	r2 := dataProfile("portfolio.holdings", "Portfolio holdings", "Positions in one or more portfolios on a date.",
		requestFor("portfolios", "portfolio.list@v1"), cols2, meaning,
		&protocol.Evolution{Lens: []protocol.LensOp{{Op: protocol.LensRename, From: "mv", To: "market_value"}}, Changelog: "mv is now market_value."})

	cols3 := append([]protocol.Column(nil), cols2...)
	cols3[5] = protocol.Column{Name: "oas", Type: protocol.TypeNumber, Nullable: true, Logical: "Fraction", Description: "Option-adjusted spread over the government curve."}
	cols3[6] = protocol.Column{Name: "sector", Type: protocol.TypeString, Enum: []string{"Industrials", "Financials", "Utilities", "Energy", "Supranational", protocol.Unknown}, Description: "The issuer's sector."}
	cols3 = append(cols3, protocol.Column{Name: "currency", Type: protocol.TypeString, Nullable: true, Logical: "Currency", Description: "The security's currency."})
	sources3 := copyMap(sources2)
	sources3["oas"] = ports.SourceColumn{Writer: "oas", Source: "OAS_BP", Divide: 10_000}
	sources3["currency"] = ports.SourceColumn{Writer: "currency", Source: "CURRENCY"}
	r3 := dataProfile("portfolio.holdings", "Portfolio holdings", "Positions in one or more portfolios on a date.",
		requestFor("portfolios", "portfolio.list@v1"), cols3, meaning,
		&protocol.Evolution{Lens: []protocol.LensOp{{Op: protocol.LensConvertUnit, Field: "oas", From: "Bp", To: "Fraction"}},
			Changelog: "oas is a fraction; Supranational is a sector; currency is new."})

	return declaration{
		view: "HARBOUR.PORTFOLIO_HOLDINGS",
		filters: func(req ports.Request) ([]ports.Filter, error) {
			on, err := asOf(req, "AS_OF_DATE")
			return []ports.Filter{{Source: "PORTFOLIO_CODE", Op: ports.OpIn, Values: values(req["portfolios"])}, on}, err
		},
		revisions: []revision{{r1, sources1}, {r2, sources2}, {r3, sources3}},
	}
}

// constituents declares index.constituents@v1.
func constituents() declaration {
	cols := []protocol.Column{
		{Name: "index", Type: protocol.TypeString, Key: true, Description: "The index's code."},
		{Name: "security", Type: protocol.TypeString, Key: true, Description: "The security's ISIN."},
		{Name: "as_of", Type: protocol.TypeDate, Key: true, Description: "The constituents date."},
		{Name: "weight", Type: protocol.TypeDecimal, Precision: 9, Scale: 6, Logical: "Fraction", Description: "The constituent's weight in the index."},
	}
	p := dataProfile("index.constituents", "Index constituents", "Constituents of indices on a date, with their weights.",
		requestFor("indices", "index.list@v1"), cols,
		map[string]string{"grain": "index × security × date", "source": "Harbour's index constituents view"}, nil)
	return declaration{
		view: "HARBOUR.INDEX_CONSTITUENTS",
		filters: func(req ports.Request) ([]ports.Filter, error) {
			on, err := asOf(req, "AS_OF_DATE")
			return []ports.Filter{{Source: "INDEX_CODE", Op: ports.OpIn, Values: values(req["indices"])}, on}, err
		},
		revisions: []revision{{p, map[string]ports.SourceColumn{
			"index": {Writer: "index", Source: "INDEX_CODE"}, "security": {Writer: "security", Source: "ISIN"},
			"as_of": {Writer: "as_of", Source: "AS_OF_DATE"}, "weight": {Writer: "weight", Source: "WEIGHT"},
		}}},
	}
}

func copyMap(m map[string]ports.SourceColumn) map[string]ports.SourceColumn {
	out := make(map[string]ports.SourceColumn, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
