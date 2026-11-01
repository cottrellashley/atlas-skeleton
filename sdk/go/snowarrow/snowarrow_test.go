package snowarrow

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// target is a writer schema as protocol.ArrowSchema derives it.
var target = arrow.NewSchema([]arrow.Field{
	{Name: "security", Type: arrow.BinaryTypes.String},
	{Name: "quantity", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	{Name: "market_value", Type: &arrow.Decimal128Type{Precision: 18, Scale: 2}, Nullable: true},
	{Name: "notional", Type: &arrow.Decimal128Type{Precision: 38, Scale: 6}, Nullable: true},
	{Name: "oas", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	{Name: "spread", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	{Name: "as_of", Type: arrow.FixedWidthTypes.Date32},
	{Name: "priced_at", Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, Nullable: true},
	{Name: "loaded_at", Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, Nullable: true},
	{Name: "active", Type: arrow.FixedWidthTypes.Boolean},
}, nil)

var rowType = RowType{
	{Name: "security", Type: Text},
	{Name: "quantity", Type: Fixed, Precision: 18, Scale: 0, Nullable: true},
	{Name: "market_value", Type: Fixed, Precision: 18, Scale: 2, Nullable: true},
	{Name: "notional", Type: Fixed, Precision: 38, Scale: 6, Nullable: true},
	{Name: "oas", Type: Real, Nullable: true},
	{Name: "spread", Type: Fixed, Precision: 12, Scale: 4, Nullable: true}, // a decimal the profile reads as a number
	{Name: "as_of", Type: Date},
	{Name: "priced_at", Type: TimestampTZ, Scale: 9, Nullable: true},
	{Name: "loaded_at", Type: TimestampLTZ, Scale: 3, Nullable: true},
	{Name: "active", Type: Boolean},
}

func TestDecodeEveryTypeThroughAChunk(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)
	ny := time.FixedZone("NY", -4*3600)
	priced := time.Date(2026, 9, 30, 16, 30, 0, 123456789, ny)
	loaded := time.Date(1969, 12, 31, 23, 59, 59, 999_000_000, time.UTC) // before the epoch
	huge, _ := new(big.Int).SetString("12345678901234567890123456", 10)
	rows := [][]any{
		{"XS0000000001", int64(7), int64(123450), huge, 125.5, int64(12345), time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), priced, loaded, true},
		{"XS0000000002", nil, int64(-5), nil, nil, nil, time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), nil, nil, false},
	}
	enc, err := Encode(mem, rowType, rows)
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := WriteChunk(enc)
	enc.Release()
	if err != nil {
		t.Fatal(err)
	}
	batches, err := ReadChunk(mem, bytes.NewReader(chunk))
	if err != nil || len(batches) != 1 {
		t.Fatalf("read chunk: %v (%d batches)", err, len(batches))
	}
	defer batches[0].Release()
	if got := batches[0].Column(1).DataType(); got.ID() != arrow.INT8 {
		t.Fatalf("quantity 7 should travel as the narrowest integer, got %s", got)
	}
	out, err := Decode(mem, rowType, batches[0], target)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Release()
	q := out.Column(1).(*array.Int64)
	mv := out.Column(2).(*array.Decimal128)
	notional := out.Column(3).(*array.Decimal128)
	spread := out.Column(5).(*array.Float64)
	priced0 := out.Column(7).(*array.Timestamp)
	loaded0 := out.Column(8).(*array.Timestamp)
	switch {
	case q.Value(0) != 7 || !q.IsNull(1):
		t.Fatalf("quantity: %v", q)
	case mv.Value(0).ToString(2) != "1234.50" || mv.Value(1).ToString(2) != "-0.05":
		t.Fatalf("market_value: %s %s", mv.Value(0).ToString(2), mv.Value(1).ToString(2))
	case notional.Value(0).BigInt().Cmp(huge) != 0:
		t.Fatalf("notional lost precision: %s", notional.Value(0).BigInt())
	case spread.Value(0) != 1.2345:
		t.Fatalf("spread: %v", spread.Value(0))
	case priced0.Value(0) != arrow.Timestamp(priced.UTC().UnixMicro()):
		t.Fatalf("priced_at: %v want %v", priced0.Value(0).ToTime(arrow.Microsecond), priced.UTC())
	case loaded0.Value(0) != arrow.Timestamp(loaded.UnixMicro()):
		t.Fatalf("loaded_at before the epoch: %d want %d", loaded0.Value(0), loaded.UnixMicro())
	case !out.Column(7).IsNull(1):
		t.Fatal("a null timestamp must stay null")
	}
}

func TestDecodeRefusesAColumnThatBreaksTheContract(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)
	rt := RowType{{Name: "quantity", Type: Fixed, Precision: 18, Scale: 2}}
	enc, _ := Encode(mem, rt, [][]any{{int64(100)}})
	defer enc.Release()
	want := arrow.NewSchema([]arrow.Field{{Name: "quantity", Type: arrow.PrimitiveTypes.Int64}}, nil)
	if _, err := Decode(mem, rt, enc, want); err == nil {
		t.Fatal("NUMBER(18,2) must not be read silently as an integer")
	}
	missing := arrow.NewSchema([]arrow.Field{{Name: "price", Type: arrow.PrimitiveTypes.Float64}}, nil)
	if _, err := Decode(mem, rt, enc, missing); err == nil {
		t.Fatal("a writer column the result lacks must be an error")
	}
}

func TestIntegerWidthFollowsTheChunk(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)
	rt := RowType{{Name: "n", Type: Fixed, Precision: 18}}
	for _, tc := range []struct {
		v    int64
		want arrow.Type
	}{{100, arrow.INT8}, {-30000, arrow.INT16}, {2_000_000_000, arrow.INT32}, {9_000_000_000, arrow.INT64}} {
		enc, err := Encode(mem, rt, [][]any{{tc.v}})
		if err != nil {
			t.Fatal(err)
		}
		if enc.Column(0).DataType().ID() != tc.want {
			t.Errorf("%d travelled as %s", tc.v, enc.Column(0).DataType())
		}
		out, err := Decode(mem, rt, enc, arrow.NewSchema([]arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, nil))
		if err != nil || out.Column(0).(*array.Int64).Value(0) != tc.v {
			t.Errorf("%d decoded wrong: %v", tc.v, err)
		}
		enc.Release()
		if out != nil {
			out.Release()
		}
	}
}
