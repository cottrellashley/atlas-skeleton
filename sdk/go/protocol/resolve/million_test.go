package resolve

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"atlas.local/sdk/go/protocol/arrowschema"
	"atlas.local/sdk/go/protocol/schema"
)

const holdingsRow = `"portfolio": {"type": "string", "x-atlas-logical": "PortfolioCode", "x-atlas-key": true},
	"isin": {"type": ["string", "null"], "x-atlas-logical": "Isin", "x-atlas-key": true},
	"as_of": {"type": "string", "format": "date", "x-atlas-key": true},
	"market_value": {"type": "object", "x-atlas-logical": "Money"},
	%s,
	"status": {"type": "string", "enum": ["open", "sold", "UNKNOWN"], "x-atlas-enum": [{"value": "open"}, {"value": "sold"}, {"value": "UNKNOWN"}]},
	"lots": {"type": ["integer", "null"], "x-atlas-unit": "count"}`

// holdingsChain is portfolio.holdings@v1 before and after mv was renamed
// weight and converted from a fraction to basis points.
func holdingsChain(t testing.TB) []Profile {
	older := row(t.(*testing.T), schema.Open, `"portfolio", "as_of", "market_value", "mv", "status"`, fmt.Sprintf(holdingsRow, `"mv": {"type": "number", "x-atlas-unit": "fraction"}`))
	newer := row(t.(*testing.T), schema.Open, `"portfolio", "as_of", "market_value", "weight", "status"`, fmt.Sprintf(holdingsRow, `"weight": {"type": "number", "x-atlas-unit": "bp", "x-atlas-aliases": ["mv"]}`))
	return []Profile{
		{id(t.(*testing.T), "portfolio.holdings@v1#1111111111111111"), nil, older},
		{id(t.(*testing.T), "portfolio.holdings@v1#2222222222222222"), lens(t.(*testing.T), `[{"op": "rename", "from": "mv", "to": "weight"}, {"op": "convert_unit", "field": "weight", "from": "fraction", "to": "bp"}]`), newer},
	}
}

// holdingsBatch builds n rows of the older profile directly in Arrow.
func holdingsBatch(mem memory.Allocator, ws *schema.Schema, n int) arrow.RecordBatch {
	fields, err := arrowschema.Fields(ws)
	if err != nil {
		panic(err)
	}
	b := array.NewRecordBuilder(mem, arrow.NewSchema(fields, nil))
	defer b.Release()
	isins := []string{"US0378331005", "US5949181045", "GB0002634946", "DE0007164600"}
	day := arrow.Date32FromTime(time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC))
	for i := range n {
		b.Field(0).(*array.StringBuilder).Append("GLOBAL-CREDIT")
		if i%10 == 0 {
			b.Field(1).AppendNull()
		} else {
			b.Field(1).(*array.StringBuilder).Append(isins[i%len(isins)])
		}
		b.Field(2).(*array.Date32Builder).Append(day)
		mv := b.Field(3).(*array.StructBuilder)
		mv.Append(true)
		mv.FieldBuilder(0).(*array.Decimal128Builder).Append(decimal128.FromBigInt(big.NewInt(int64(i) * 1_000_000)))
		mv.FieldBuilder(1).(*array.StringBuilder).Append("USD")
		b.Field(4).(*array.Float64Builder).Append(float64(i%1000) / 1000)
		b.Field(5).(*array.StringBuilder).Append([]string{"open", "sold"}[i%2])
		if i%3 == 0 {
			b.Field(6).AppendNull()
		} else {
			b.Field(6).(*array.Int64Builder).Append(int64(i))
		}
	}
	return b.NewRecordBatch()
}

// ATLAS-RESOLVE-004: a million rows resolve column by column, in time that
// follows the columns' size: the rename and the unchanged columns are the
// writer's own arrays, the unit conversion is one pass, and no cell is
// resolved on its own.
func TestAMillionRowsColumnByColumn(t *testing.T) {
	const n = 1_000_000
	ps := holdingsChain(t)
	plan, err := New(ps, ps[0].Exact, ps[1].Exact, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)
	in := holdingsBatch(mem, ps[0].Schema, n)
	defer in.Release()
	before := valueByValue.Load()
	start := time.Now()
	out, err := plan.Batch(mem, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Release()
	took := time.Since(start)
	if cells := valueByValue.Load() - before; cells != 0 {
		t.Fatalf("%d cells were resolved one by one", cells)
	}
	if out.NumRows() != n {
		t.Fatalf("%d rows", out.NumRows())
	}
	names := []string{}
	for _, f := range out.Schema().Fields() {
		names = append(names, f.Name)
	}
	if fmt.Sprint(names) != "[portfolio isin as_of market_value weight status lots]" {
		t.Fatalf("columns %v", names)
	}
	w := out.Column(4).(*array.Float64)
	if w.Value(123) != 1230 || w.Value(999) != 9990 {
		t.Fatalf("weight in bp: %v %v", w.Value(123), w.Value(999))
	}
	for i := range out.NumCols() {
		if i != 4 && out.Column(int(i)).Data() != in.Column(int(i)).Data() {
			t.Errorf("column %d was copied", i)
		}
	}
	t.Logf("a million rows resolved in %v", took)
}

func BenchmarkResolveColumns(b *testing.B) {
	t := &testing.T{}
	ps := holdingsChain(t)
	plan, _ := New(ps, ps[0].Exact, ps[1].Exact, Options{})
	mem := memory.NewGoAllocator()
	in := holdingsBatch(mem, ps[0].Schema, 1_000_000)
	defer in.Release()
	b.ResetTimer()
	for range b.N {
		out, err := plan.Batch(mem, in, nil)
		if err != nil {
			b.Fatal(err)
		}
		out.Release()
	}
}

func BenchmarkResolveRows(b *testing.B) {
	t := &testing.T{}
	ps := holdingsChain(t)
	plan, _ := New(ps, ps[0].Exact, ps[1].Exact, Options{})
	mem := memory.NewGoAllocator()
	in := holdingsBatch(mem, ps[0].Schema, 10_000)
	defer in.Release()
	rows, err := arrowschema.ToRows(in, ps[0].Schema)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		for _, r := range rows {
			if _, err := plan.Record(r); err != nil {
				b.Fatal(err)
			}
		}
	}
}
