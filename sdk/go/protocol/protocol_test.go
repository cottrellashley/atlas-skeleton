package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseAddressAndIDs(t *testing.T) {
	a, err := ParseAddress("portfolio.holdings@v1:fetch")
	if err != nil || a.ID != "portfolio.holdings" || a.Version != 1 || a.Interaction != "fetch" {
		t.Fatalf("parse: %+v %v", a, err)
	}
	if a.String() != "portfolio.holdings@v1:fetch" {
		t.Fatalf("round trip: %s", a)
	}
	for _, bad := range []string{
		"portfolio@v1:fetch",           // one segment
		"Portfolio.holdings@v1:fetch",  // upper case
		"portfolio.holdings@v0:fetch",  // version must be positive
		"portfolio.holdings@v01:fetch", // no leading zero
		"portfolio.holdings-@v1:fetch", // segment ends in '-'
		"portfolio.hold--ings@v1:fetch",
		"portfolio.holdings@v1",
		"portfolio.holdings:fetch",
	} {
		if _, err := ParseAddress(bad); err == nil {
			t.Errorf("%q should not parse", bad)
		}
	}
	e, err := ParseExact("portfolio.holdings@v1#bcaa5bbad97cc612")
	if err != nil || e.Fingerprint != "bcaa5bbad97cc612" || e.Pinned.String() != "portfolio.holdings@v1" {
		t.Fatalf("exact: %+v %v", e, err)
	}
	if _, err := ParseExact("portfolio.holdings@v1#BCAA5BBAD97CC612"); err == nil {
		t.Fatal("an upper-case fingerprint should not parse")
	}
	if !IsBuiltin("atlas.describe") || IsBuiltin("portfolio.holdings") {
		t.Fatal("only the atlas domain is built in")
	}
}

func TestValidateRequest(t *testing.T) {
	no := false
	one := 1
	s := &Schema{
		Type: Type{Name: "object"}, AdditionalProperties: &no,
		Required: []string{"portfolios"},
		Properties: map[string]*Schema{
			"portfolios": {Type: Type{Name: "array"}, MinItems: &one, Items: &Schema{Type: Type{Name: "string"}}},
			"as_of":      {Type: Type{Name: "string"}, Format: "date"},
			"columns":    {Type: Type{Name: "array"}, Items: &Schema{Type: Type{Name: "string"}}},
			"limit":      {Type: Type{Name: "integer"}, Default: json.RawMessage(`1000`)},
			"side":       {Type: Type{Name: "string"}, Enum: []string{"buy", "sell"}},
		},
	}
	req, _ := DecodeRecord([]byte(`{"portfolios":["147HYC"],"as_of":"2026-09-30"}`))
	if err := ApplyDefaults(s, req); err != nil {
		t.Fatal(err)
	}
	if f := Validate(s, req); len(f) != 0 {
		t.Fatalf("valid request failed: %+v", f)
	}
	if req["limit"] != json.Number("1000") {
		t.Fatalf("default not applied: %v", req["limit"])
	}
	bad, _ := DecodeRecord([]byte(`{"portfolios":[],"as_of":"2026-02-30","side":"hold","extra":1,"limit":1.5}`))
	got := map[string]string{}
	for _, f := range Validate(s, bad) {
		got[f.Pointer] = f.Keyword
	}
	want := map[string]string{"/portfolios": "minItems", "/as_of": "format", "/side": "enum", "/extra": "additionalProperties", "/limit": "type"}
	for ptr, kw := range want {
		if got[ptr] != kw {
			t.Errorf("%s: want %s, got %q (all: %v)", ptr, kw, got[ptr], got)
		}
	}
	missing, _ := DecodeRecord([]byte(`{}`))
	if f := Validate(s, missing); len(f) != 1 || f[0].Pointer != "/portfolios" || f[0].Keyword != "required" {
		t.Fatalf("missing required: %+v", f)
	}
}

func TestFingerprintIgnoresProseOnly(t *testing.T) {
	a, _, _ := holdings(t)
	fp1, err := Fingerprint(a)
	if err != nil {
		t.Fatal(err)
	}
	prose := *a
	prose.Title, prose.Summary, prose.Revision = "Holdings, renamed", "Different words.", 9
	cols := append([]Column(nil), a.Result.Columns...)
	cols[0].Description = "Reworded."
	prose.Result = &Table{Columns: cols}
	if fp2, _ := Fingerprint(&prose); fp2 != fp1 {
		t.Fatalf("prose changed the fingerprint: %s vs %s", fp1, fp2)
	}
	more := *a
	more.Result = &Table{Columns: append(append([]Column(nil), a.Result.Columns...), Column{Name: "currency", Type: TypeString, Nullable: true})}
	if fp3, _ := Fingerprint(&more); fp3 == fp1 {
		t.Fatal("a new column must change the fingerprint")
	}
}

func TestDeadlineAndFeatures(t *testing.T) {
	at := time.Unix(1793318400, 0)
	got, err := ParseDeadline(FormatDeadline(at))
	if err != nil || !got.Equal(at) || FormatDeadline(at) != "@1793318400" {
		t.Fatalf("deadline: %v %v", got, err)
	}
	f := ParseFeatures("levels=(core columnar handoff), handoff=(snowflake-arrow/1), lens=(rename)")
	if !f.Has(LevelColumnar) || !f.Decodes("snowflake-arrow/1") || f.Has(LevelStreaming) {
		t.Fatalf("features: %+v", f)
	}
	if !ParseFeatures("").Has(LevelCore) {
		t.Fatal("a client that declares nothing is served at Core")
	}
}
