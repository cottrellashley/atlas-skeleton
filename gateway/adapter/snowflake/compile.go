package snowflake

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol/grammar"
	sqlglot "atlas.local/sqlglot"
	"atlas.local/sqlglot/errs"
	"atlas.local/sqlglot/exp"
)

// The bounds on what is compiled (INTEGRATIONS: parsing takes no
// cancellation context, so its input is bounded instead).
const (
	MaxStatement = 256 << 10 // bytes of SQL
	MaxDepth     = 64        // levels of the statement's syntax tree
	MaxValues    = 1000      // values in one IN group; a longer IN is groups joined by OR
	MaxBinds     = 16384     // values bound in one statement
)

// Statement is a compiled query: its SQL in the target dialect, the values
// bound to its parameters in order, and the objects it reads, as the guard
// read the statement that runs. A statement a Source hands its Engine also
// carries the tag the engine records, out of band, and the columns of its
// answer.
type Statement struct {
	SQL     string
	Args    []any
	Objects [][]string
	// Tag is what the engine records with the query (Snowflake's
	// QUERY_TAG). It is never part of SQL.
	Tag string
	// Columns are the answer's columns, in the order SQL selects them.
	Columns []Output
	// Plan is the plan the statement was made from, kept with it as pass
	// 1's statement kept its Select: a real engine runs SQL, and a fake may
	// evaluate the plan's request instead.
	Plan *ports.Plan
}

// identifier is an unquoted Snowflake identifier: the only names a query
// may hold, so that no name becomes SQL either. Every identifier is written
// upper-cased and quoted, as Snowflake resolves an unquoted one, so a word
// Snowflake reserves is still a name (R114).
var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]{0,254}$`)

// zoneName is an IANA time zone name, the only text a statement holds as a
// literal (CONVERT_TIMEZONE's source zone): letters, digits, '_', '+' and
// '-', in at most four parts.
var zoneName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]{0,63}(/[A-Za-z0-9_+-]{1,63}){0,3}$`)

// The aliases of the statement's own relations.
const (
	rows   = "atlas_t"
	scan   = "atlas_l"
	latest = "atlas_latest"
)

type builder struct {
	sql  strings.Builder
	args []any
}

func (b *builder) add(format string, a ...any) { fmt.Fprintf(&b.sql, format, a...) }

func column(name string) (string, error) {
	if !identifier.MatchString(name) {
		return "", fmt.Errorf("snowflake: %q is not an identifier", name)
	}
	return strings.ToUpper(name), nil
}

func object(name string) ([]string, error) {
	parts := strings.Split(name, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("snowflake: %q is not database.schema.object", name)
	}
	for i, p := range parts {
		c, err := column(p)
		if err != nil {
			return nil, err
		}
		parts[i] = c
	}
	return parts, nil
}

// zone checks a timestamp column's zone, which a statement writes as a
// literal.
func zone(z string) error {
	if z != "" && !zoneName.MatchString(z) {
		return fmt.Errorf("snowflake: %q is not a time zone name", z)
	}
	return nil
}

// ref is a column as a statement reads it: a timestamp held as wall-clock
// time in a declared zone is converted to UTC wall-clock time, so every
// timestamp a statement reads or compares is in UTC (§2.2.1.5, R053).
func ref(alias, name string, typ ports.ColumnType, z string) (string, error) {
	c, err := column(name)
	if err != nil {
		return "", err
	}
	if err := zone(z); err != nil {
		return "", err
	}
	if typ == ports.Timestamp && z != "" {
		return fmt.Sprintf("CONVERT_TIMEZONE('%s', 'UTC', %s.%s)", z, alias, c), nil
	}
	return alias + "." + c, nil
}

// bind adds one value to the statement's parameters.
func (b *builder) bind(v any) error {
	if len(b.args) >= MaxBinds {
		return fmt.Errorf("%w: more than %d values", ports.ErrQueryTooLarge, MaxBinds)
	}
	b.args = append(b.args, v)
	return nil
}

// value is a parameter for a value compared with a column of typ: cast
// where the parameter's type would otherwise be the driver's guess.
func (b *builder) value(v any, typ ports.ColumnType, z string) error {
	switch typ {
	case ports.Date:
		s, ok := v.(string)
		if _, err := grammar.ParseDate(s); !ok || err != nil {
			return fmt.Errorf("snowflake: %v is not a date", v)
		}
		b.add("CAST(? AS DATE)")
	case ports.Timestamp:
		// An instant, with its offset (§9.1), compared as an instant
		// (R111): a TIMESTAMP_TZ in UTC, or for a column held as wall-clock
		// time, the UTC wall-clock time its reference is converted to.
		t, err := instant(v)
		if err != nil {
			return err
		}
		if z != "" {
			v = t.Format("2006-01-02 15:04:05.000000000")
			b.add("CAST(? AS TIMESTAMP_NTZ)")
		} else {
			v = formatInstant(t)
			b.add("CAST(? AS TIMESTAMP_TZ)")
		}
	case ports.Decimal:
		s, ok := v.(string)
		d, err := grammar.ParseDecimal(s)
		if !ok || err != nil {
			return fmt.Errorf("snowflake: %v is not a decimal", v)
		}
		p := max(len(strings.TrimLeft(d.Integer, "0"))+len(d.Fraction), 1)
		if p > 38 {
			return fmt.Errorf("snowflake: %v has more than 38 digits", v)
		}
		b.add("CAST(? AS NUMBER(%d, %d))", p, len(d.Fraction))
	default:
		switch v.(type) {
		case string, int64, float64, bool:
		default:
			return fmt.Errorf("snowflake: a %T is not a value a statement binds", v)
		}
		b.add("?")
	}
	return b.bind(v)
}

// instant is a timestamp value: a time.Time, or an RFC 3339 date-time that
// carries its offset (§9.1.4), in UTC.
func instant(v any) (time.Time, error) {
	switch x := v.(type) {
	case time.Time:
		return x.UTC(), nil
	case string:
		t, err := grammar.ParseDateTime(x)
		if err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("snowflake: %v is not a date-time with its offset", v)
}

// formatInstant writes an instant in UTC, to the microsecond, or to the
// nanosecond when it has one.
func formatInstant(t time.Time) string {
	if t.Nanosecond()%1000 != 0 {
		return t.UTC().Format("2006-01-02T15:04:05.000000000Z")
	}
	return t.UTC().Format("2006-01-02T15:04:05.000000Z")
}

func (b *builder) predicates(alias string, ps []ports.Predicate) error {
	for i, p := range ps {
		c, err := ref(alias, p.Column, p.Type, p.Zone)
		if err != nil {
			return err
		}
		for _, v := range p.Values {
			if v == nil {
				return errors.New("snowflake: a null in a predicate: compare with a value")
			}
		}
		if i > 0 {
			b.add(" AND ")
		}
		switch p.Op {
		case ports.In:
			if len(p.Values) == 0 {
				b.add("1 = 0") // in nothing
				continue
			}
			if len(b.args)+len(p.Values) > MaxBinds {
				return fmt.Errorf("%w: %d values for %s", ports.ErrQueryTooLarge, len(p.Values), p.Column)
			}
			// An IN of more than MaxValues is groups of at most
			// MaxValues, joined by OR: the same condition, since no value
			// is null (R112).
			groups := (len(p.Values) + MaxValues - 1) / MaxValues
			if groups > 1 {
				b.add("(")
			}
			for g := range groups {
				if g > 0 {
					b.add(" OR ")
				}
				b.add("%s IN (", c)
				for i, v := range p.Values[g*MaxValues : min((g+1)*MaxValues, len(p.Values))] {
					if i > 0 {
						b.add(", ")
					}
					if err := b.value(v, p.Type, p.Zone); err != nil {
						return err
					}
				}
				b.add(")")
			}
			if groups > 1 {
				b.add(")")
			}
		case ports.Eq, ports.Gte, ports.Lte:
			if len(p.Values) != 1 {
				return fmt.Errorf("snowflake: %s takes one value", p.Op)
			}
			b.add("%s %s ", c, map[ports.Op]string{ports.Eq: "=", ports.Gte: ">=", ports.Lte: "<="}[p.Op])
			if err := b.value(p.Values[0], p.Type, p.Zone); err != nil {
				return err
			}
		default:
			return fmt.Errorf("snowflake: no comparison %q", p.Op)
		}
	}
	return nil
}

// from writes everything after the select list: the object, the latest of
// each group, the conditions, the order and the limit.
func (b *builder) from(q ports.Query, obj []string) error {
	from := strings.Join(obj, ".")
	b.add(" FROM %s AS %s", from, rows)
	if l := q.Latest; l != nil {
		date, err := column(l.Date)
		if err != nil {
			return err
		}
		if len(l.Per) == 0 {
			return errors.New("snowflake: the latest of what: Per names no column")
		}
		per := make([]string, len(l.Per))
		for i, p := range l.Per {
			if per[i], err = column(p); err != nil {
				return err
			}
		}
		b.add(" JOIN (SELECT ")
		for _, p := range per {
			b.add("%s.%s, ", scan, p)
		}
		b.add("MAX(%s.%s) AS %s FROM %s AS %s", scan, date, date, from, scan)
		within := l.Within
		if l.OnOrBefore != "" {
			within = append(within[:len(within):len(within)], ports.Predicate{Column: l.Date, Op: ports.Lte, Type: ports.Date, Values: []any{l.OnOrBefore}})
		}
		if len(within) > 0 {
			b.add(" WHERE ")
			if err := b.predicates(scan, within); err != nil {
				return err
			}
		}
		b.add(" GROUP BY %s.%s", scan, strings.Join(per, ", "+scan+"."))
		b.add(") AS %s ON ", latest)
		for _, p := range per {
			b.add("%s.%s = %s.%s AND ", rows, p, latest, p)
		}
		b.add("%s.%s = %s.%s", rows, date, latest, date)
	}
	if len(q.Where) > 0 {
		b.add(" WHERE ")
		if err := b.predicates(rows, q.Where); err != nil {
			return err
		}
	}
	if len(q.OrderBy) > 0 {
		b.add(" ORDER BY ")
		for i, o := range q.OrderBy {
			c, err := column(o)
			if err != nil {
				return err
			}
			if i > 0 {
				b.add(", ")
			}
			// Nulls sort last because the statement says so: a null key
			// first, false before true (R113). NULLS LAST alone is both
			// dialects' default, which sqlglot drops, leaving the order to
			// a session's DEFAULT_NULL_ORDERING.
			b.add("%s.%s IS NULL, %s.%s ASC", rows, c, rows, c)
		}
	}
	if q.Limit > 0 {
		b.add(" LIMIT %s", strconv.Itoa(q.Limit))
	}
	return nil
}

// Compile turns q into one SELECT, every column as the object holds it,
// for the deprecated Warehouse. Every name in it is an identifier the query
// declares, every value a parameter; sqlglot parses what was built and
// generates it in Snowflake's dialect, the guard checks the generated
// statement against registered, and with the duckdb dialect that statement
// is transpiled, so the statement that runs is one the guard saw.
func Compile(q ports.Query, registered [][]string, dialect string) (*Statement, error) {
	if dialect != "snowflake" && dialect != "duckdb" {
		return nil, fmt.Errorf("snowflake: no dialect %q: snowflake, or duckdb for the development warehouse", dialect)
	}
	obj, err := object(q.Object)
	if err != nil {
		return nil, err
	}
	if len(q.Columns) == 0 {
		return nil, errors.New("snowflake: a query returns at least one column")
	}
	var b builder
	b.add("SELECT ")
	for i, c := range q.Columns {
		name, err := column(c.Name)
		if err != nil {
			return nil, err
		}
		r, err := ref(rows, c.Name, c.Type, c.Zone)
		if err != nil {
			return nil, err
		}
		if i > 0 {
			b.add(", ")
		}
		switch {
		case c.Type == ports.Decimal && c.Precision > 0: // the contract's scale, rounded by the source (R100), as its exact text
			if c.Precision > 38 || c.Scale < 0 || c.Scale > c.Precision {
				return nil, fmt.Errorf("snowflake: %s is no Decimal(%d,%d)", c.Name, c.Precision, c.Scale)
			}
			b.add("CAST(CAST(%s AS NUMBER(%d, %d)) AS VARCHAR) AS %s", r, c.Precision, c.Scale, name)
		case c.Type == ports.Decimal: // exact, whatever the driver does with NUMBER
			b.add("CAST(%s AS VARCHAR) AS %s", r, name)
		default:
			b.add("%s AS %s", r, name)
		}
	}
	if err := b.from(q, obj); err != nil {
		return nil, err
	}
	out, objects, err := finish(b.sql.String(), registered, obj, dialect)
	if err != nil {
		return nil, err
	}
	return &Statement{SQL: out[len(out)-1], Args: b.args, Objects: objects}, nil
}

// finish parses the built text, generates it in Snowflake's dialect with
// every identifier quoted, and guards the generated statement: the one
// that runs (R113). It must read exactly the query's object, registered.
// It returns the Snowflake statement, then its transpilation to each
// further dialect.
func finish(text string, registered [][]string, obj []string, dialects ...string) ([]string, [][]string, error) {
	if len(text) > MaxStatement {
		return nil, nil, fmt.Errorf("%w: the statement is %d bytes, more than %d", ports.ErrQueryTooLarge, len(text), MaxStatement)
	}
	out, err := generate(text, dialects...)
	if err != nil {
		return nil, nil, err
	}
	v := Guard(out[0], registered)
	if !v.Accepted {
		if v.Reason == UnregisteredObject {
			return nil, nil, fmt.Errorf("%w: %s", ports.ErrUnknownObject, strings.Join(obj, "."))
		}
		return nil, nil, refuse(v)
	}
	if len(v.Objects) != 1 || !slices.Equal(v.Objects[0], obj) {
		return nil, nil, fmt.Errorf("%w: the statement reads %v, not %s alone", ports.ErrPlanRefused, v.Objects, strings.Join(obj, "."))
	}
	return out, v.Objects, nil
}

// generate writes the statement in Snowflake's dialect, then in each
// further dialect, every identifier quoted. A construct a target cannot
// express fails here, before any driver call.
func generate(text string, dialects ...string) (out []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("snowflake: generating %v: %v", dialects, r)
		}
	}()
	trees, err := sqlglot.Parse(text, "snowflake")
	if err != nil || len(trees) != 1 || trees[0] == nil {
		return nil, fmt.Errorf("snowflake: the compiled statement does not parse: %v", err)
	}
	if d := depth(trees[0]); d > MaxDepth {
		return nil, fmt.Errorf("%w: the statement nests %d levels, more than %d", ports.ErrQueryTooLarge, d, MaxDepth)
	}
	for _, d := range append([]string{"snowflake"}, dialects...) {
		if d == "snowflake" && len(out) > 0 {
			out = append(out, out[0])
			continue
		}
		s, err := trees[0].Generate(d, exp.UnsupportedLevel(errs.Raise), exp.Identify(true))
		if err != nil {
			return nil, fmt.Errorf("snowflake: generating %s: %w", d, err)
		}
		out = append(out, s)
	}
	return out, nil
}

// Transpile writes a Snowflake statement in another dialect, every
// identifier quoted: how the development warehouse runs a Source's
// statements on DuckDB (PLAN.md P2-6).
func Transpile(statement, dialect string) (string, error) {
	if len(statement) > MaxStatement {
		return "", fmt.Errorf("%w: the statement is %d bytes, more than %d", ports.ErrQueryTooLarge, len(statement), MaxStatement)
	}
	out, err := generate(statement, dialect)
	if err != nil {
		return "", err
	}
	return out[1], nil
}

func depth(e *exp.Expr) int {
	most := 0
	for node := range e.Walk(false, nil) {
		d := 0
		for p := node; p != nil; p = p.Parent() {
			d++
		}
		most = max(most, d)
	}
	return most
}
