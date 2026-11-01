package snowfake

import (
	"fmt"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/snowarrow"
)

// Sizes of the synthetic Harbour.
type Size struct {
	Portfolios int
	Holdings   int // securities each portfolio holds on each date
	Indices    int
	Members    int // securities in each index on each date
	Dates      int // business days, ending at End
	End        time.Time
}

// Small is enough for every behavioural test; Large is for memory and
// throughput.
var (
	Small = Size{Portfolios: 4, Holdings: 40, Indices: 2, Members: 60, Dates: 5, End: day(2026, 9, 30)}
	Large = Size{Portfolios: 20, Holdings: 2000, Indices: 2, Members: 3000, Dates: 60, End: day(2026, 9, 30)}
)

// Sectors in the synthetic data. Supranational appears only in data written
// after the sector list grew, so older readers must map it to UNKNOWN.
var Sectors = []string{"Industrials", "Financials", "Utilities", "Energy", "Supranational"}

// Harbour builds deterministic synthetic tables shaped like Harbour's views.
// The same Size always gives the same rows.
func Harbour(s Size) []*Table {
	dates := businessDays(s.End, s.Dates)
	universe := max(4*s.Holdings, s.Members)
	holdings := &Table{
		Name: "HARBOUR.PORTFOLIO_HOLDINGS",
		Columns: []snowarrow.Column{
			{Name: "PORTFOLIO_CODE", Type: snowarrow.Text},
			{Name: "ISIN", Type: snowarrow.Text},
			{Name: "AS_OF_DATE", Type: snowarrow.Date},
			{Name: "MARKET_VALUE", Type: snowarrow.Fixed, Precision: 18, Scale: 2},
			{Name: "WEIGHT", Type: snowarrow.Fixed, Precision: 9, Scale: 6},
			{Name: "OAS_BP", Type: snowarrow.Fixed, Precision: 12, Scale: 4, Nullable: true},
			{Name: "SECTOR", Type: snowarrow.Text},
			{Name: "CURRENCY", Type: snowarrow.Text},
		},
		Rows: s.Portfolios * s.Dates * s.Holdings,
	}
	holdings.Row = func(i int) []any {
		p, d, k := i/(s.Dates*s.Holdings), (i/s.Holdings)%s.Dates, i%s.Holdings
		sec := (p*s.Holdings/2 + k) % universe
		h := mix(uint64(p), uint64(sec), uint64(d))
		var oas any = int64((50+h%850)*10_000 + (h>>20)%10_000)
		if h%97 == 0 {
			oas = nil
		}
		return []any{
			PortfolioCode(p), ISIN(sec), dates[d],
			int64((100_000+h%9_900_000)*100 + (h>>8)%100),
			int64((h >> 12) % 20_000),
			oas,
			sector(sec), []string{"USD", "EUR", "GBP"}[sec%3],
		}
	}
	constituents := &Table{
		Name: "HARBOUR.INDEX_CONSTITUENTS",
		Columns: []snowarrow.Column{
			{Name: "INDEX_CODE", Type: snowarrow.Text},
			{Name: "ISIN", Type: snowarrow.Text},
			{Name: "AS_OF_DATE", Type: snowarrow.Date},
			{Name: "WEIGHT", Type: snowarrow.Fixed, Precision: 9, Scale: 6},
		},
		Rows: s.Indices * s.Dates * s.Members,
	}
	constituents.Row = func(i int) []any {
		x, d, k := i/(s.Dates*s.Members), (i/s.Members)%s.Dates, i%s.Members
		sec := (x*7 + k) % universe
		return []any{IndexCode(x), ISIN(sec), dates[d], int64(mix(uint64(x)+100, uint64(sec), uint64(d)) % 5_000)}
	}
	return []*Table{holdings, constituents}
}

// PortfolioCode is the synthetic code of portfolio p; the first is the
// spec's example fund.
func PortfolioCode(p int) string {
	if names := []string{"147HYC", "200IG", "310EMD", "420MUNI"}; p < len(names) {
		return names[p]
	}
	return fmt.Sprintf("P%03d", p)
}

// IndexCode is the synthetic code of index x.
func IndexCode(x int) string {
	if names := []string{"H0A0", "C0A0"}; x < len(names) {
		return names[x]
	}
	return fmt.Sprintf("IX%02d", x)
}

// ISIN is the synthetic identifier of security k.
func ISIN(k int) string { return fmt.Sprintf("XS%010d", 1_000_000_000+k*7919) }

func sector(sec int) string {
	if sec%8 == 7 {
		return "Supranational"
	}
	return Sectors[sec%4]
}

func businessDays(end time.Time, n int) []time.Time {
	out := make([]time.Time, n)
	d := end
	for i := n - 1; i >= 0; i-- {
		for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			d = d.AddDate(0, 0, -1)
		}
		out[i] = d
		d = d.AddDate(0, 0, -1)
	}
	return out
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// mix is splitmix64 over three inputs: stable, well spread, no state.
func mix(a, b, c uint64) uint64 {
	z := a*0x9E3779B97F4A7C15 ^ b*0xBF58476D1CE4E5B9 ^ c*0x94D049BB133111EB
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}
