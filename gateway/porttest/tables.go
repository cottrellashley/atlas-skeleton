package porttest

import (
	"fmt"
	"math/big"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
)

// Holdings is the object WarehouseTables holds.
const Holdings = "TESTDB.PUBLIC.HOLDINGS"

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

// big is 2^53 + 1: an integer a float64 cannot hold.
const bigInt = int64(1<<53 + 1)

// WarehouseTables is the data the Warehouse suite queries. An adapter's
// test loads it into its engine: every column type, nulls in each, an
// integer past 2^53 and a decimal past a float's precision.
func WarehouseTables() []fakes.Table {
	return []fakes.Table{{
		Name: Holdings,
		Columns: []ports.Column{
			{Name: "PORTFOLIO", Type: ports.String}, {Name: "SECURITY", Type: ports.String},
			{Name: "ISIN", Type: ports.String}, {Name: "AS_OF", Type: ports.Date},
			{Name: "MV", Type: ports.Decimal}, {Name: "CCY", Type: ports.String},
			{Name: "WEIGHT", Type: ports.Float}, {Name: "LOTS", Type: ports.Integer},
			{Name: "OPEN", Type: ports.Boolean}, {Name: "LOADED_AT", Type: ports.Timestamp},
		},
		Rows: [][]any{
			{"P1", "S1", "US0000000001", "2026-10-29", "1000.123456", "USD", 0.5, int64(3), true, ts("2026-10-29T18:00:00.123456Z")},
			{"P1", "S2", nil, "2026-10-29", "-200.000001", "EUR", 0.5, int64(1), false, ts("2026-10-29T18:00:00Z")},
			{"P1", "S1", "US0000000001", "2026-10-30", "1010", "USD", 1.0, bigInt, true, ts("2026-10-30T18:00:00Z")},
			{"P2", "S3", "GB0000000003", "2026-10-28", "12345678901234567890.123456", "GBP", 1.0, nil, true, ts("2026-10-28T18:00:00.000001Z")},
			{"P2", "S3", "GB0000000003", "2026-10-31", "2.5", "GBP", 1.0, int64(2), nil, nil},
			{nil, "S9", nil, "2026-10-30", "1", "USD", nil, int64(1), true, ts("2026-10-30T18:00:00Z")},
		},
	}}
}

func columns(names ...string) []ports.Column {
	types := map[string]ports.ColumnType{}
	for _, c := range WarehouseTables()[0].Columns {
		types[c.Name] = c.Type
	}
	out := make([]ports.Column, len(names))
	for i, n := range names {
		out[i] = ports.Column{Name: n, Type: types[n]}
	}
	return out
}

// same compares a value an adapter returned with the one expected, by the
// column's type: a decimal by value, at whatever scale the engine writes.
func same(got, want any, typ ports.ColumnType) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	switch typ {
	case ports.Decimal:
		g, ok1 := got.(string)
		x, ok2 := new(big.Rat).SetString(g)
		y, _ := new(big.Rat).SetString(want.(string))
		return ok1 && ok2 && x.Cmp(y) == 0
	case ports.Timestamp:
		g, ok := got.(time.Time)
		return ok && g.Location() == time.UTC && g.Equal(want.(time.Time))
	}
	return fmt.Sprintf("%T %v", got, got) == fmt.Sprintf("%T %v", want, want)
}
