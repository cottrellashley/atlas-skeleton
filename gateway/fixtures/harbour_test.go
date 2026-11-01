package fixtures

import (
	"math"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
)

// tableOf is one of the fixture's tables, as rows of named values.
func tableOf(t *testing.T, name string) []map[string]any {
	t.Helper()
	tables, err := Harbour()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(tables, func(x fakes.Table) bool { return x.Name == name })
	if i < 0 {
		t.Fatalf("the fixture has no %s", name)
	}
	var out []map[string]any
	for _, row := range tables[i].Rows {
		m := map[string]any{}
		for k, c := range tables[i].Columns {
			m[c.Name] = row[k]
		}
		out = append(out, m)
	}
	return out
}

func rat(t *testing.T, s any) *big.Rat {
	t.Helper()
	r, ok := new(big.Rat).SetString(s.(string))
	if !ok {
		t.Fatalf("%v is not a decimal", s)
	}
	return r
}

// toBase is FX_RATES_V's rate from a currency to a portfolio's base
// currency on a date: one for the base currency itself.
func toBase(t *testing.T, fx []map[string]any, date, from, to string) *big.Rat {
	t.Helper()
	if from == to {
		return big.NewRat(1, 1)
	}
	for _, r := range fx {
		if r["AS_OF_DATE"] == date && r["FROM_CCY"] == from && r["TO_CCY"] == to {
			return rat(t, r["RATE"])
		}
	}
	t.Fatalf("no rate from %s to %s on %s", from, to, date)
	return nil
}

// roundHalfEven is r at six decimal places, ties to even, as Money writes.
func roundHalfEven(r *big.Rat) string {
	scaled := new(big.Rat).Mul(r, big.NewRat(1_000_000, 1))
	q, m := new(big.Int).QuoRem(scaled.Num(), scaled.Denom(), new(big.Int))
	twice := new(big.Int).Mul(new(big.Int).Abs(m), big.NewInt(2))
	if c := twice.Cmp(scaled.Denom()); c > 0 || c == 0 && q.Bit(0) == 1 {
		if scaled.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return grammar.FormatDecimal(q, 6)
}

type valuation struct {
	portfolio, date string
	rows            []map[string]any
	total           *big.Rat
}

// valuations are POSITIONS_V's rows by portfolio and date, with their total
// in the portfolio's base currency.
func valuations(t *testing.T) (map[[2]string]*valuation, map[string]string) {
	t.Helper()
	base := map[string]string{}
	for _, p := range tableOf(t, "HARBOUR.REF.PORTFOLIOS_V") {
		base[p["PORTFOLIO_CODE"].(string)] = p["BASE_CCY"].(string)
	}
	fx := tableOf(t, "HARBOUR.REF.FX_RATES_V")
	out := map[[2]string]*valuation{}
	for _, r := range tableOf(t, "HARBOUR.HOLDINGS.POSITIONS_V") {
		k := [2]string{r["PORTFOLIO_CODE"].(string), r["AS_OF_DATE"].(string)}
		if out[k] == nil {
			out[k] = &valuation{portfolio: k[0], date: k[1], total: new(big.Rat)}
		}
		v := out[k]
		v.rows = append(v.rows, r)
		in := new(big.Rat).Mul(rat(t, r["MARKET_VALUE"]), toBase(t, fx, k[1], r["CURRENCY"].(string), base[k[0]]))
		r["_base"] = in
		v.total.Add(v.total, in)
	}
	return out, base
}

// R033, R083: every weight is its position's market value in the
// portfolio's base currency over the portfolio's total, so weights include
// cash, sum to one, and a short's is negative (the holdings profile's
// weight and meaning.weights).
func TestR033WeightsAreBaseCurrencyShares(t *testing.T) {
	vs, _ := valuations(t)
	for _, v := range vs {
		sum := 0.0
		for _, r := range v.rows {
			w := r["WEIGHT"].(float64)
			want, _ := new(big.Rat).Quo(r["_base"].(*big.Rat), v.total).Float64()
			if math.Abs(w-want) > 1e-9 {
				t.Errorf("%s %s %s: weight %v, its base-currency share %v", v.portfolio, v.date, r["SECURITY_ID"], w, want)
			}
			if mv := rat(t, r["MARKET_VALUE"]); mv.Sign() < 0 && w >= 0 {
				t.Errorf("%s %s %s: a negative market value has weight %v", v.portfolio, v.date, r["SECURITY_ID"], w)
			}
			sum += w
		}
		if math.Abs(sum-1) > 1e-9 {
			t.Errorf("%s %s: the weights sum to %v", v.portfolio, v.date, sum)
		}
	}
}

// R083: VALUATIONS_V holds each portfolio's valuations exactly: its total
// in its base currency (rounded half-even to Money's scale), its position
// count, its base currency, and the dates its positions hold, no more.
func TestR083TotalsAreBaseCurrencySums(t *testing.T) {
	vs, base := valuations(t)
	seen := map[[2]string]bool{}
	for _, r := range tableOf(t, "HARBOUR.HOLDINGS.VALUATIONS_V") {
		k := [2]string{r["PORTFOLIO_CODE"].(string), r["AS_OF_DATE"].(string)}
		v := vs[k]
		if v == nil || seen[k] {
			t.Fatalf("%v: a valuation with no positions, or twice", k)
		}
		seen[k] = true
		if got, want := r["TOTAL_MV"].(string), roundHalfEven(v.total); got != want {
			t.Errorf("%v: TOTAL_MV %s, the positions' base-currency sum %s", k, got, want)
		}
		if r["POSITION_COUNT"].(int64) != int64(len(v.rows)) || r["BASE_CCY"] != base[k[0]] {
			t.Errorf("%v: %v positions in %v; it holds %d in %s", k, r["POSITION_COUNT"], r["BASE_CCY"], len(v.rows), base[k[0]])
		}
		latest := time.Time{}
		for _, p := range v.rows {
			if at := p["LOADED_AT"].(time.Time); at.After(latest) {
				latest = at
			}
		}
		if !r["LOADED_AT"].(time.Time).Equal(latest) {
			t.Errorf("%v: loaded at %v, its rows last at %v", k, r["LOADED_AT"], latest)
		}
	}
	if len(seen) != len(vs) {
		t.Fatalf("%d valuations for %d valued dates", len(seen), len(vs))
	}
}

// R033, R038: one row per key (the holdings profile's grain and keys: a
// security's lots summed), and the keys the profile states: cash
// CASH-<currency>, a derivative its contract's id, a loan its facility's,
// none of them with an ISIN; every ISIN with a valid ISO 6166 check digit;
// a position closed that day at zero.
func TestR033R038KeysAndIdentifiers(t *testing.T) {
	seen := map[string]bool{}
	kinds := map[string]bool{}
	for _, r := range tableOf(t, "HARBOUR.HOLDINGS.POSITIONS_V") {
		id, _ := r["SECURITY_ID"].(string)
		key := r["PORTFOLIO_CODE"].(string) + "|" + id + "|" + r["AS_OF_DATE"].(string)
		if seen[key] {
			t.Errorf("%s: two rows with one key", key)
		}
		seen[key] = true
		isin, hasISIN := r["ISIN"].(string)
		prefix, _, _ := strings.Cut(id, "-")
		switch prefix {
		case "CASH", "FXF", "CDS", "LN":
			kinds[prefix] = true
			if hasISIN {
				t.Errorf("%s: a %s position has no ISIN", key, prefix)
			}
			if prefix == "CASH" && id != "CASH-"+r["CURRENCY"].(string) {
				t.Errorf("%s: cash is CASH-<currency>", key)
			}
		default:
			if !hasISIN || !validISIN(isin) {
				t.Errorf("%s: the ISIN %q has no valid check digit", key, isin)
			}
		}
		if r["POSITION_STATUS"] == "CLOSED" && (rat(t, r["QUANTITY"]).Sign() != 0 || rat(t, r["MARKET_VALUE"]).Sign() != 0 || r["WEIGHT"].(float64) != 0) {
			t.Errorf("%s: a position closed that day is zero", key)
		}
	}
	for _, k := range []string{"CASH", "FXF", "CDS", "LN"} {
		if !kinds[k] {
			t.Errorf("the fixture holds no %s position to fix its key", k)
		}
	}
}

// validISIN is ISO 6166's check: letters as numbers (A is 10), then Luhn.
func validISIN(s string) bool {
	if len(s) != 12 {
		return false
	}
	var digits strings.Builder
	for _, c := range s[:11] {
		switch {
		case '0' <= c && c <= '9':
			digits.WriteRune(c)
		case 'A' <= c && c <= 'Z':
			digits.WriteString(big.NewInt(int64(c - 'A' + 10)).String())
		default:
			return false
		}
	}
	sum := 0
	d := digits.String()
	for i := len(d) - 1; i >= 0; i-- {
		n := int(d[i] - '0')
		if (len(d)-1-i)%2 == 0 {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
	}
	return int(s[11]-'0') == (10-sum%10)%10
}

// R035: the load register agrees with the data: each source's latest date
// is the latest its object holds, the date it expects is the business day
// that should have landed by its check (Monday to Friday, by 06:00 UTC the
// next day), and its status is what those dates say.
func TestR035TheLoadRegisterAgreesWithTheData(t *testing.T) {
	objects := map[string]string{
		"harbour.holdings":   "HARBOUR.HOLDINGS.POSITIONS_V",
		"harbour.valuations": "HARBOUR.HOLDINGS.VALUATIONS_V",
		"harbour.fx":         "HARBOUR.REF.FX_RATES_V",
	}
	for _, r := range tableOf(t, "HARBOUR.OPS.LOADS_V") {
		source := r["SOURCE"].(string)
		latest, _ := r["LATEST_DATE"].(string)
		expected := r["EXPECTED_DATE"].(string)
		checked := r["CHECKED_AT"].(time.Time)
		if obj, ok := objects[source]; ok {
			most := ""
			for _, row := range tableOf(t, obj) {
				most = max(most, row["AS_OF_DATE"].(string))
			}
			if latest != most {
				t.Errorf("%s: latest %s, but %s holds %s", source, latest, obj, most)
			}
		}
		if want := due(checked); expected != want {
			t.Errorf("%s: checked at %v, it expects %s, not %s", source, checked, want, expected)
		}
		if status := map[bool]string{true: "CURRENT", false: "LATE"}[latest != "" && latest >= expected]; r["STATUS"] != status {
			t.Errorf("%s: %v, but its dates say %s", source, r["STATUS"], status)
		}
	}
}

// due is the latest business day whose data should have landed by at: a
// weekday whose next day's 06:00 UTC has passed.
func due(at time.Time) string {
	d := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	for {
		weekday := d.Weekday() != time.Saturday && d.Weekday() != time.Sunday
		if weekday && !d.AddDate(0, 0, 1).Add(6*time.Hour).After(at) {
			return d.Format(time.DateOnly)
		}
		d = d.AddDate(0, 0, -1)
	}
}

// R033, R083; AGENTS.md (fuzz every parser): the fixture's reader refuses
// what is not a fixture, and reads what it accepts without panicking.
func FuzzParseHarbour(f *testing.F) {
	f.Add(harbour)
	for _, s := range []string{
		`{"tables": []}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X", "date"]], "rows": [["2026-02-30"]]}]}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X", "decimal"]], "rows": [["1e3"]]}]}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X", "integer"]], "rows": [[9007199254740993]]}]}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X", "timestamp"]], "rows": [["2026-10-31T06:00:00"]]}]}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X", "string"], ["x", "string"]], "rows": []}]}`,
		`{"tables": [{"name": "A.B.C", "columns": [], "rows": []}, {"name": "a.b.c", "columns": [], "rows": []}]}`,
		`{"tables": [], "tables": []}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X", "money"]], "rows": []}]}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X", "timestamp", "Europe/London"]], "rows": [["2026-10-31T06:00:00Z"]]}]}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		tables, err := parseHarbour(b)
		if err != nil {
			return
		}
		for _, tb := range tables {
			for _, row := range tb.Rows {
				if len(row) != len(tb.Columns) {
					t.Fatalf("%s: a row of %d values for %d columns", tb.Name, len(row), len(tb.Columns))
				}
			}
		}
	})
}

// The dev warehouse holds the fixture's times as UTC wall-clock times
// (DuckDB's TIMESTAMP), so the fixture says so, and a read declares the
// zone it converts them from (§2.2.1.6, R053).
func TestTheFixturesTimesAreUTCWallClockTimes(t *testing.T) {
	tables, err := Harbour()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, tb := range tables {
		for _, c := range tb.Columns {
			if c.Type == "timestamp" {
				n++
				if c.Zone != "UTC" {
					t.Errorf("%s.%s: zone %q", tb.Name, c.Name, c.Zone)
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("no timestamp column")
	}
}

// R033, R083 (INTEGRATIONS: the fixtures agree with the intended results):
// the fixture's reader holds every value to its column's type.
func TestTheFixtureReaderRefusesWhatIsNotAFixture(t *testing.T) {
	for _, s := range []string{
		`{"tables": [{"name": "A.B.C", "columns": [["X", "date"]], "rows": [["2026-02-30"]]}]}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X", "decimal"]], "rows": [["1e3"]]}]}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X", "integer"]], "rows": [[9007199254740993]]}]}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X", "timestamp"]], "rows": [["2026-10-31T06:00:00"]]}]}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X", "string"], ["x", "string"]], "rows": []}]}`,
		`{"tables": [{"name": "A.B.C", "columns": [], "rows": []}, {"name": "a.b.c", "columns": [], "rows": []}]}`,
		`{"tables": [], "tables": []}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X", "money"]], "rows": []}]}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X", "string"]], "rows": [["a", "b"]]}]}`,
		`{"tables": [], "nope": 1}`,
		`{"Tables": []}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X", "string", "UTC"]], "rows": []}]}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X", "timestamp", "Mars/Olympus"]], "rows": []}]}`,
		`{"tables": [{"name": "A.B.C", "columns": [["X"]], "rows": []}]}`,
		`{"tables": [{"Name": "A.B.C", "columns": [], "rows": []}]}`,
	} {
		if _, err := parseHarbour([]byte(s)); err == nil {
			t.Errorf("%s was read", s)
		}
	}
}
