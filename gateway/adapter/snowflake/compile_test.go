package snowflake

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"atlas.local/gateway/ports"
)

var registered = [][]string{{"HARBOUR", "HOLDINGS"}}

func holdings() ports.Query {
	return ports.Query{
		Object:  "harbour.holdings.positions_v",
		Columns: []ports.Column{{Name: "PORTFOLIO_CODE", Type: ports.String}, {Name: "MARKET_VALUE", Type: ports.Decimal}, {Name: "AS_OF_DATE", Type: ports.Date}},
		Latest: &ports.Latest{Date: "AS_OF_DATE", Per: []string{"PORTFOLIO_CODE"}, OnOrBefore: "2026-10-30",
			Within: []ports.Predicate{{Column: "PORTFOLIO_CODE", Op: ports.In, Type: ports.String, Values: []any{"147HYC", "GLOBAL-CREDIT"}}}},
		Where:   []ports.Predicate{{Column: "PORTFOLIO_CODE", Op: ports.In, Type: ports.String, Values: []any{"147HYC", "GLOBAL-CREDIT"}}, {Column: "MARKET_VALUE", Op: ports.Gte, Type: ports.Decimal, Values: []any{"-1.5"}}},
		OrderBy: []string{"PORTFOLIO_CODE"},
		Limit:   10,
	}
}

// The compiled statement: every name an identifier the query declared,
// upper-cased and quoted, every value a parameter, in the order of the
// statement's text.
func TestCompile(t *testing.T) {
	s, err := Compile(holdings(), registered, "snowflake")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.SQL, `CAST("atlas_t"."PORTFOLIO_CODE" AS VARCHAR)`) || !strings.Contains(s.SQL, `CAST("atlas_t"."MARKET_VALUE" AS VARCHAR) AS "MARKET_VALUE"`) {
		t.Fatalf("decimals are read as their exact text: %s", s.SQL)
	}
	for _, part := range []string{`FROM "HARBOUR"."HOLDINGS"."POSITIONS_V" AS "atlas_t" JOIN (SELECT "atlas_l"."PORTFOLIO_CODE", MAX("atlas_l"."AS_OF_DATE") AS "AS_OF_DATE" FROM "HARBOUR"."HOLDINGS"."POSITIONS_V" AS "atlas_l" WHERE "atlas_l"."PORTFOLIO_CODE" IN (?, ?) AND "atlas_l"."AS_OF_DATE" <= CAST(? AS DATE) GROUP BY "atlas_l"."PORTFOLIO_CODE") AS "atlas_latest"`,
		`WHERE "atlas_t"."PORTFOLIO_CODE" IN (?, ?) AND "atlas_t"."MARKET_VALUE" >= CAST(? AS DECIMAL(2, 1))`, `ORDER BY "atlas_t"."PORTFOLIO_CODE" IS NULL, "atlas_t"."PORTFOLIO_CODE" ASC LIMIT 10`} {
		if !strings.Contains(s.SQL, part) {
			t.Errorf("no %q in\n%s", part, s.SQL)
		}
	}
	if got := len(s.Args); got != 6 || s.Args[2] != "2026-10-30" || s.Args[5] != "-1.5" {
		t.Fatalf("args %v", s.Args)
	}
	duck, err := Compile(holdings(), registered, "duckdb")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(duck.SQL, `CAST("atlas_t"."MARKET_VALUE" AS TEXT)`) || !strings.Contains(duck.SQL, `ORDER BY "atlas_t"."PORTFOLIO_CODE" IS NULL, "atlas_t"."PORTFOLIO_CODE" ASC LIMIT 10`) {
		t.Fatalf("duckdb: %s", duck.SQL)
	}
	if _, err := Compile(holdings(), registered, "Snowflake"); err == nil {
		t.Fatal("a dialect sqlglot does not know compiled") // R136: New lower-cases the setting
	}
}

// What a query may not do never reaches a driver.
func TestCompileRefuses(t *testing.T) {
	for name, edit := range map[string]func(*ports.Query){
		"a name that is SQL":       func(q *ports.Query) { q.Columns[0].Name = "x; DROP TABLE y" },
		"an object not registered": func(q *ports.Query) { q.Object = "HARBOUR.SECRET.SALARIES" },
		"a two-part object":        func(q *ports.Query) { q.Object = "HOLDINGS.POSITIONS_V" },
		"no columns":               func(q *ports.Query) { q.Columns = nil },
		"a null value":             func(q *ports.Query) { q.Where[0].Values = []any{nil} },
		"a date that is not":       func(q *ports.Query) { q.Latest.OnOrBefore = "2026-13-01" },
		"an unknown comparison":    func(q *ports.Query) { q.Where[0].Op = "like" },
		"too many values":          func(q *ports.Query) { q.Where[0].Values = make([]any, MaxValues+1) },
	} {
		q := holdings()
		edit(&q)
		if _, err := Compile(q, registered, "snowflake"); err == nil {
			t.Errorf("%s: compiled", name)
		}
	}
	q := holdings()
	q.Object = "HARBOUR.SECRET.SALARIES"
	if _, err := Compile(q, registered, "snowflake"); !errors.Is(err, ports.ErrUnknownObject) {
		t.Errorf("an object not registered is ErrUnknownObject: %v", err)
	}
	q = holdings()
	q.Where[0].Values = nil
	if s, err := Compile(q, registered, "snowflake"); err != nil || !strings.Contains(s.SQL, "1 = 0") {
		t.Errorf("IN nothing is false: %v %v", s, err)
	}
}

// R114: every identifier is quoted in the statement that runs, so a column
// named by a word Snowflake reserves (START, ORDER) is still a column. The
// names are upper-cased first, so quoting changes nothing in how Snowflake
// resolves them, and the guard reads the quoted statement as the same
// objects.
func TestR114ReservedWordsAreQuoted(t *testing.T) {
	q := ports.Query{Object: "HARBOUR.HOLDINGS.T", Columns: []ports.Column{{Name: "START", Type: ports.Date}, {Name: "ORDER", Type: ports.String}},
		Where: []ports.Predicate{{Column: "ROWS", Op: ports.Eq, Type: ports.String, Values: []any{"x"}}}, OrderBy: []string{"START"}}
	for _, dialect := range []string{"snowflake", "duckdb"} {
		s, err := Compile(q, registered, dialect)
		if err != nil {
			t.Fatalf("%s: %v", dialect, err)
		}
		for _, part := range []string{`"atlas_t"."START"`, `"atlas_t"."ORDER"`, `"atlas_t"."ROWS" = ?`, `FROM "HARBOUR"."HOLDINGS"."T" AS "atlas_t"`} {
			if !strings.Contains(s.SQL, part) {
				t.Errorf("%s: no %s in\n%s", dialect, part, s.SQL)
			}
		}
		if dialect == "snowflake" {
			if v := Guard(s.SQL, registered); !v.Accepted || !slices.EqualFunc(v.Objects, [][]string{{"HARBOUR", "HOLDINGS", "T"}}, slices.Equal) {
				t.Errorf("the statement that runs is guarded as %+v", v)
			}
		}
	}
}

// R113: nulls sort last because the statement says so, with a null key
// before each ordering column, not because of a session's
// DEFAULT_NULL_ORDERING. sqlglot drops NULLS LAST as both dialects' default;
// it keeps the key.
func TestR113NullsLastIsInTheStatement(t *testing.T) {
	for _, dialect := range []string{"snowflake", "duckdb"} {
		s, err := Compile(holdings(), registered, dialect)
		if err != nil {
			t.Fatal(err)
		}
		if want := `ORDER BY "atlas_t"."PORTFOLIO_CODE" IS NULL, "atlas_t"."PORTFOLIO_CODE"`; !strings.Contains(s.SQL, want) {
			t.Errorf("%s: no %s in\n%s", dialect, want, s.SQL)
		}
	}
	// The objects a statement reports are the guard's reading of the text
	// that runs, which is the text generated, not the text built.
	s, err := Compile(holdings(), registered, "snowflake")
	if err != nil {
		t.Fatal(err)
	}
	if v := Guard(s.SQL, registered); !v.Accepted || !slices.EqualFunc(v.Objects, s.Objects, slices.Equal) {
		t.Fatalf("Compile reports %v; the guard reads %+v", s.Objects, v)
	}
}

// R112, PR10: an IN of any size the bounds allow compiles, as IN groups of
// at most MaxValues each, joined by OR: a caller granted 2,500 portfolios
// still reads one of them. Past MaxBinds values in one statement, the
// query is too large, a typed refusal (ports.ErrQueryTooLarge), never an
// internal error.
func TestR112ManyValuesInOneIn(t *testing.T) {
	values := make([]any, 2500)
	for i := range values {
		values[i] = fmt.Sprintf("P%04d", i)
	}
	q := holdings()
	q.Where[0].Values = values
	s, err := Compile(q, registered, "snowflake")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(s.SQL, `"atlas_t"."PORTFOLIO_CODE" IN (`); n != 3 {
		t.Errorf("%d IN groups for 2,500 values, want 3", n)
	}
	if !strings.Contains(s.SQL, `("atlas_t"."PORTFOLIO_CODE" IN (`) || !strings.Contains(s.SQL, `) OR "atlas_t"."PORTFOLIO_CODE" IN (`) {
		t.Errorf("the groups are not one condition: %.300s", s.SQL)
	}
	if len(s.Args) != 2500+2+1+1 {
		t.Errorf("%d binds", len(s.Args))
	}
	q.Where[0].Values = make([]any, MaxBinds+1)
	for i := range q.Where[0].Values {
		q.Where[0].Values[i] = "P"
	}
	if _, err := Compile(q, registered, "snowflake"); !errors.Is(err, ports.ErrQueryTooLarge) {
		t.Fatalf("past MaxBinds: %v", err)
	}
}

// R111: a timestamp value is an instant written with its offset (§9.1:
// a date-time MUST carry one). It is normalised to UTC and compared as a
// TIMESTAMP_TZ, so a column is compared by instant whatever the session's
// TIMESTAMP_TYPE_MAPPING. Anything else is refused before any driver call.
func TestR111TimestampValuesAreInstants(t *testing.T) {
	q := ports.Query{Object: "HARBOUR.HOLDINGS.LOADS", Columns: []ports.Column{{Name: "SOURCE", Type: ports.String}},
		Where: []ports.Predicate{{Column: "LOADED_AT", Op: ports.Lte, Type: ports.Timestamp, Values: []any{"2026-10-29T23:00:00+05:00"}}}}
	s, err := Compile(q, registered, "snowflake")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.SQL, `"atlas_t"."LOADED_AT" <= CAST(? AS TIMESTAMPTZ)`) || len(s.Args) != 1 || s.Args[0] != "2026-10-29T18:00:00.000000Z" {
		t.Fatalf("%s %v", s.SQL, s.Args)
	}
	q.Where[0].Values = []any{time.Date(2026, 10, 29, 23, 0, 0, 0, time.FixedZone("", 5*3600))}
	if s, err := Compile(q, registered, "snowflake"); err != nil || s.Args[0] != "2026-10-29T18:00:00.000000Z" {
		t.Fatalf("a time.Time: %v %v", s, err)
	}
	for _, bad := range []any{"yesterday", "2026-10-29T18:00:00", "2026-10-29", int64(1)} {
		q.Where[0].Values = []any{bad}
		if _, err := Compile(q, registered, "snowflake"); err == nil {
			t.Errorf("%v (%T) compiled as a timestamp", bad, bad)
		}
	}
}

// The compiler fails closed: whatever names and values a query holds, it
// refuses it, or writes one statement the guard accepts, reading the
// query's object alone, with one parameter for each value it binds and no
// value written into its text. The planner, which writes contract names as
// aliases, holds to the same.
func FuzzCompile(f *testing.F) {
	f.Add("PORTFOLIO_CODE", "147HYC", "2026-10-30", "-1.5", "2026-10-29T23:00:00+05:00", "Europe/London", "portfolio")
	f.Add("START", "x'); DROP TABLE y; --", "2026-13-01", "1e5", "yesterday", "UTC'), 'x", "Portfolio")
	f.Fuzz(func(t *testing.T, name, s, date, dec, ts, zone, field string) {
		q := ports.Query{Object: "HARBOUR.HOLDINGS.POSITIONS_V",
			Columns: []ports.Column{{Name: name, Type: ports.String}, {Name: "T", Type: ports.Timestamp, Zone: zone}},
			Latest:  &ports.Latest{Date: "AS_OF_DATE", Per: []string{name}, OnOrBefore: date},
			Where: []ports.Predicate{{Column: name, Op: ports.In, Type: ports.String, Values: []any{s, s + "x"}},
				{Column: "MV", Op: ports.Gte, Type: ports.Decimal, Values: []any{dec}}, {Column: "T", Op: ports.Lte, Type: ports.Timestamp, Zone: zone, Values: []any{ts}}},
			OrderBy: []string{name}, Limit: 10}
		check := func(sql string, args int) {
			v := Guard(sql, registered)
			if !v.Accepted || len(v.Objects) != 1 || !slices.Equal(v.Objects[0], []string{"HARBOUR", "HOLDINGS", "POSITIONS_V"}) {
				t.Fatalf("%q is guarded as %+v", sql, v)
			}
			if n := strings.Count(sql, "?"); n != args {
				t.Fatalf("%d parameters for %d values in %q", n, args, sql)
			}
		}
		if st, err := Compile(q, registered, "snowflake"); err == nil {
			check(st.SQL, len(st.Args))
		}
		s2, err := NewSource(engineFunc(nil), Options{Registered: registered})
		if err != nil {
			t.Fatal(err)
		}
		r := ports.PlanRequest{Query: q, Writer: arrow.NewSchema([]arrow.Field{{Name: field, Type: arrow.BinaryTypes.String, Nullable: true},
			{Name: "t", Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, Nullable: true}}, nil), Columns: [][]string{{name}, {"T"}}}
		if p, err := s2.Plan(context.Background(), r); err == nil {
			check(p.Statement, len(p.Binds))
		}
	})
}
