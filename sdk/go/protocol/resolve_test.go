package protocol

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// holdings builds three profiles of one capability version, as §3.3's worked
// examples evolve it: B renames mv to market_value (dangerous: an alias and a
// rename lens); C adds a nullable currency (safe), moves oas from basis
// points to a fraction (dangerous: a unit lens) and adds an enum value
// (dangerous: older readers map it to UNKNOWN).
func holdings(t testing.TB) (a, b, c *Profile) {
	t.Helper()
	sectors := []string{"Industrials", "Financials", Unknown}
	base := []Column{
		{Name: "portfolio", Type: TypeString, Key: true},
		{Name: "security", Type: TypeString, Key: true},
		{Name: "as_of", Type: TypeDate, Key: true},
		{Name: "mv", Type: TypeDecimal, Precision: 18, Scale: 2, Unit: "currency_unit"},
		{Name: "oas", Type: TypeNumber, Nullable: true, Logical: "Bp"},
		{Name: "sector", Type: TypeString, Enum: sectors},
	}
	mk := func(cols []Column, ev *Evolution) *Profile {
		return &Profile{
			ID: "portfolio.holdings", Version: 1, Title: "Portfolio holdings", Domain: "portfolio",
			Family: FamilyData, Stability: "official", Lifecycle: "stable", Visibility: "listed",
			Traits:       []string{TraitProjectable},
			Interactions: map[string]Interaction{"fetch": {Effect: "read", Result: "table", Delivery: "chunked"}},
			Result:       &Table{Columns: cols},
			Evolution:    ev,
		}
	}
	a = mk(base, nil)
	colsB := append([]Column(nil), base...)
	colsB[3] = Column{Name: "market_value", Type: TypeDecimal, Precision: 18, Scale: 2, Unit: "currency_unit", Aliases: []string{"mv"}}
	b = mk(colsB, &Evolution{Previous: exact(t, a), Lens: []LensOp{{Op: LensRename, From: "mv", To: "market_value"}}})
	colsC := append([]Column(nil), colsB...)
	colsC[4] = Column{Name: "oas", Type: TypeNumber, Nullable: true, Logical: "Fraction"}
	colsC[5] = Column{Name: "sector", Type: TypeString, Enum: []string{"Industrials", "Financials", "Supranational", Unknown}}
	colsC = append(colsC, Column{Name: "currency", Type: TypeString, Nullable: true})
	c = mk(colsC, &Evolution{Previous: exact(t, b), Lens: []LensOp{{Op: LensConvertUnit, Field: "oas", From: "Bp", To: "Fraction"}}})
	return a, b, c
}

func exact(t testing.TB, p *Profile) string {
	t.Helper()
	e, err := ExactOf(p)
	if err != nil {
		t.Fatal(err)
	}
	return e.String()
}

func source(t testing.TB, ps ...*Profile) ProfileSource {
	m := map[string]*Profile{}
	for _, p := range ps {
		m[exact(t, p)] = p
	}
	return func(e string) (*Profile, error) {
		if p, ok := m[e]; ok {
			return p, nil
		}
		return nil, fmt.Errorf("no profile %s", e)
	}
}

func row(t testing.TB, s string) map[string]any {
	t.Helper()
	m, err := DecodeRecord([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestResolveWriterNewerToLockedReader(t *testing.T) {
	a, b, c := holdings(t)
	ch, err := BuildChain(source(t, a, b, c), exact(t, c), exact(t, a))
	if err != nil {
		t.Fatal(err)
	}
	if !ch.WriterNewer || len(ch.Profiles) != 3 {
		t.Fatalf("chain: newer=%v len=%d", ch.WriterNewer, len(ch.Profiles))
	}
	got, err := ResolveRow(ch, row(t, `{"portfolio":"147HYC","security":"XS0000000001","as_of":"2026-09-30",
		"market_value":"1234.50","oas":0.0125,"sector":"Supranational","currency":"USD"}`), a.Result)
	if err != nil {
		t.Fatal(err)
	}
	want := row(t, `{"portfolio":"147HYC","security":"XS0000000001","as_of":"2026-09-30",
		"mv":"1234.50","oas":125,"sector":"UNKNOWN"}`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved:\n got %v\nwant %v", got, want)
	}
}

func TestResolveWriterOlderToNewerReader(t *testing.T) {
	a, b, c := holdings(t)
	ch, err := BuildChain(source(t, a, b, c), exact(t, a), exact(t, c))
	if err != nil {
		t.Fatal(err)
	}
	if ch.WriterNewer {
		t.Fatal("writer A is older than reader C")
	}
	got, err := ResolveRow(ch, row(t, `{"portfolio":"P","security":"S","as_of":"2026-09-30","mv":"10.00","oas":125,"sector":"Industrials"}`), c.Result)
	if err != nil {
		t.Fatal(err)
	}
	if got["market_value"] != "10.00" || got["oas"] != json.Number("0.0125") || got["currency"] != nil || got["sector"] != "Industrials" {
		t.Fatalf("resolved: %v", got)
	}
	if _, ok := got["mv"]; ok {
		t.Fatal("the old name must not survive")
	}
}

func TestResolveProjectedToRequestedColumns(t *testing.T) {
	a, b, c := holdings(t)
	ch, _ := BuildChain(source(t, a, b, c), exact(t, c), exact(t, a))
	got, err := ResolveRow(ch, row(t, `{"market_value":"5.00","oas":0.0001}`), a.Result.Project([]string{"mv", "oas"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["mv"] != "5.00" || got["oas"] != json.Number("1") {
		t.Fatalf("projected: %v", got)
	}
}

func TestDifferentVersionsNeverResolve(t *testing.T) {
	a, _, _ := holdings(t)
	v2 := *a
	v2.Version = 2
	_, err := BuildChain(source(t, a, &v2), exact(t, &v2), exact(t, a))
	var p *Problem
	if !asProblem(err, &p) || p.Code != CodeResolutionFailed {
		t.Fatalf("want resolution_failed, got %v", err)
	}
}

func TestResolveRequestForward(t *testing.T) {
	no := false
	reqA := &Schema{Type: Type{Name: "object"}, AdditionalProperties: &no, Properties: map[string]*Schema{"date": {Type: Type{Name: "string"}}}}
	reqB := &Schema{Type: Type{Name: "object"}, AdditionalProperties: &no, Properties: map[string]*Schema{"as_of": {Type: Type{Name: "string"}, Aliases: []string{"date"}}}}
	pa := &Profile{ID: "x.y", Version: 1, Request: reqA}
	pb := &Profile{ID: "x.y", Version: 1, Request: reqB, Evolution: &Evolution{Previous: exact(t, pa), Lens: []LensOp{{Op: LensRename, Schema: SchemaRequest, From: "date", To: "as_of"}}}}
	ch, err := BuildChain(source(t, pa, pb), exact(t, pa), exact(t, pb))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveRequest(ch, row(t, `{"date":"2026-09-30","stray":1}`), reqB)
	if err != nil || !reflect.DeepEqual(got, map[string]any{"as_of": "2026-09-30"}) {
		t.Fatalf("request: %v %v", got, err)
	}
}

// Column-wise resolution must give exactly the row-wise answer (AP-RESOLVE-002).
func TestColumnWiseEqualsRowWise(t *testing.T) {
	a, b, c := holdings(t)
	src := source(t, a, b, c)
	rows := []string{
		`{"portfolio":"147HYC","security":"S1","as_of":"2026-09-30","market_value":"1234.50","oas":0.0125,"sector":"Supranational","currency":"USD"}`,
		`{"portfolio":"147HYC","security":"S2","as_of":"2026-09-30","market_value":"-0.05","oas":null,"sector":"Financials","currency":null}`,
		`{"portfolio":"200IG","security":"S3","as_of":"2026-09-30","market_value":"99999999.99","oas":0.00015,"sector":"Industrials","currency":"EUR"}`,
	}
	for _, dir := range []struct {
		name           string
		writer, reader *Profile
		recs           []string
	}{
		{"writer newer", c, a, rows},
		{"writer older", a, c, []string{
			`{"portfolio":"P","security":"S1","as_of":"2026-09-30","mv":"10.00","oas":125,"sector":"Industrials"}`,
			`{"portfolio":"P","security":"S2","as_of":"2026-09-30","mv":"0.01","oas":null,"sector":"Financials"}`,
		}},
	} {
		t.Run(dir.name, func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
			defer mem.AssertSize(t, 0)
			ch, err := BuildChain(src, exact(t, dir.writer), exact(t, dir.reader))
			if err != nil {
				t.Fatal(err)
			}
			wSchema, _ := ArrowSchema(dir.writer.Result, exact(t, dir.writer))
			rSchema, _ := ArrowSchema(dir.reader.Result, exact(t, dir.reader))
			batch := buildBatch(t, mem, dir.writer.Result, wSchema, dir.recs)
			defer batch.Release()
			resolved, err := ResolveBatch(mem, ch, batch, dir.reader.Result, rSchema)
			if err != nil {
				t.Fatal(err)
			}
			defer resolved.Release()
			for i, r := range dir.recs {
				want, err := ResolveRow(ch, row(t, r), dir.reader.Result)
				if err != nil {
					t.Fatal(err)
				}
				got := rowOf(t, resolved, dir.reader.Result, i)
				if !reflect.DeepEqual(normal(want), normal(got)) {
					t.Fatalf("row %d:\n columns %v\n    rows %v", i, got, want)
				}
			}
		})
	}
}

func buildBatch(t *testing.T, mem memory.Allocator, tbl *Table, schema *arrow.Schema, recs []string) arrow.RecordBatch {
	t.Helper()
	b := array.NewRecordBuilder(mem, schema)
	defer b.Release()
	for _, r := range recs {
		m := row(t, r)
		for i, col := range tbl.Columns {
			if err := appendJSON(b.Field(i), col, m[col.Name]); err != nil {
				t.Fatalf("%s: %v", col.Name, err)
			}
		}
	}
	return b.NewRecordBatch()
}

// rowOf reads row i of a batch back into the JSON forms ResolveRow uses.
func rowOf(t *testing.T, rec arrow.RecordBatch, tbl *Table, i int) map[string]any {
	t.Helper()
	out := map[string]any{}
	for c, col := range tbl.Columns {
		a := rec.Column(c)
		if a.IsNull(i) {
			out[col.Name] = nil
			continue
		}
		switch x := a.(type) {
		case *array.String:
			out[col.Name] = x.Value(i)
		case *array.Float64:
			out[col.Name] = x.Value(i)
		case *array.Date32:
			out[col.Name] = x.Value(i).ToTime().Format("2006-01-02")
		case *array.Decimal128:
			out[col.Name] = FormatDecimal(x.Value(i).BigInt(), int(x.DataType().(*arrow.Decimal128Type).Scale))
		default:
			t.Fatalf("unexpected column type %s", a.DataType())
		}
	}
	return out
}

// normal turns json.Number into float64 so both forms compare.
func normal(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if n, ok := v.(json.Number); ok {
			f, _ := n.Float64()
			v = f
		}
		out[k] = v
	}
	return out
}

func asProblem(err error, p **Problem) bool {
	q, ok := err.(*Problem)
	*p = q
	return ok
}
