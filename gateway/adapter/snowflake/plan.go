package snowflake

import (
	"fmt"
	"strings"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/snowarrow"
)

// Select is the plan as one typed tree (§6.4.3). The real driver runs its
// rendered SQL; the fake Snowflake evaluates the tree itself.
type Select struct {
	From    string
	Columns []Projection
	Where   []ports.Filter
	OrderBy []string
	Limit   int
}

// Projection is one output column: a source expression cast to the
// contract's Snowflake type and aliased to its contract name. Enum lists the
// values the contract allows; anything else is written as UNKNOWN.
type Projection struct {
	Source string
	Divide int64
	Alias  string
	Cast   snowarrow.Column
	Enum   []string
}

// castFor is the Snowflake type each contract type is cast to: the
// contract written into the planned SQL, so that Snowflake itself produces
// the writer schema (§2.2.1.5, §2.2.1.6).
func castFor(c protocol.Column) (snowarrow.Column, error) {
	sc := snowarrow.Column{Name: c.Name, Nullable: c.Nullable}
	switch c.Type {
	case protocol.TypeString:
		sc.Type = snowarrow.Text
	case protocol.TypeInteger:
		sc.Type, sc.Precision = snowarrow.Fixed, 18
	case protocol.TypeNumber:
		sc.Type = snowarrow.Real
	case protocol.TypeDecimal:
		sc.Type, sc.Precision, sc.Scale = snowarrow.Fixed, c.Precision, c.Scale
	case protocol.TypeBoolean:
		sc.Type = snowarrow.Boolean
	case protocol.TypeDate:
		sc.Type = snowarrow.Date
	case protocol.TypeDateTime:
		sc.Type, sc.Scale = snowarrow.TimestampTZ, 6
	default:
		return sc, fmt.Errorf("column %s: no Snowflake cast for %s", c.Name, c.Type)
	}
	return sc, nil
}

func buildSelect(q ports.Query, writer *protocol.Table) (Select, error) {
	if len(q.Columns) != len(writer.Columns) {
		return Select{}, fmt.Errorf("the query maps %d columns, the writer has %d", len(q.Columns), len(writer.Columns))
	}
	sel := Select{From: q.Source, Where: q.Filters, OrderBy: q.OrderBy, Limit: q.Limit}
	for i, wc := range writer.Columns {
		src := q.Columns[i]
		if src.Writer != wc.Name {
			return Select{}, fmt.Errorf("query column %d maps %s, the writer's is %s", i, src.Writer, wc.Name)
		}
		cast, err := castFor(wc)
		if err != nil {
			return Select{}, err
		}
		sel.Columns = append(sel.Columns, Projection{Source: src.Source, Divide: src.Divide, Alias: wc.Name, Cast: cast, Enum: wc.Enum})
	}
	return sel, nil
}

// Render writes the statement in Snowflake's dialect, with values inlined so
// that explain shows exactly what runs (ATLAS-DATA-004).
func (s Select) Render(tag string) string {
	var b strings.Builder
	b.WriteString("SELECT ")
	for i, p := range s.Columns {
		if i > 0 {
			b.WriteString(", ")
		}
		expr := ident(p.Source)
		if p.Divide != 0 {
			expr = fmt.Sprintf("%s / %d", expr, p.Divide)
		}
		if len(p.Enum) > 0 {
			var allowed []string
			for _, v := range p.Enum {
				if v != protocol.Unknown {
					allowed = append(allowed, literal(v))
				}
			}
			expr = fmt.Sprintf("CASE WHEN %s IN (%s) THEN %s ELSE 'UNKNOWN' END", expr, strings.Join(allowed, ", "), expr)
		}
		fmt.Fprintf(&b, "CAST(%s AS %s) AS %q", expr, sqlType(p.Cast), p.Alias)
	}
	fmt.Fprintf(&b, " FROM %s", s.From)
	for i, f := range s.Where {
		if i == 0 {
			b.WriteString(" WHERE ")
		} else {
			b.WriteString(" AND ")
		}
		col := ident(f.Source)
		switch f.Op {
		case ports.OpEq:
			fmt.Fprintf(&b, "%s = %s", col, literal(f.Values[0]))
		case ports.OpIn:
			vals := make([]string, len(f.Values))
			for j, v := range f.Values {
				vals[j] = literal(v)
			}
			fmt.Fprintf(&b, "%s IN (%s)", col, strings.Join(vals, ", "))
		case ports.OpBetween:
			fmt.Fprintf(&b, "%s BETWEEN %s AND %s", col, literal(f.Values[0]), literal(f.Values[1]))
		case ports.OpLatest:
			fmt.Fprintf(&b, "%s = (SELECT MAX(%s) FROM %s)", col, col, s.From)
		}
	}
	if len(s.OrderBy) > 0 {
		quoted := make([]string, len(s.OrderBy))
		for i, o := range s.OrderBy {
			quoted[i] = fmt.Sprintf("%q", o)
		}
		fmt.Fprintf(&b, " ORDER BY %s", strings.Join(quoted, ", "))
	}
	if s.Limit > 0 {
		fmt.Fprintf(&b, " LIMIT %d", s.Limit)
	}
	if tag != "" {
		fmt.Fprintf(&b, " /* QUERY_TAG=%s */", tag)
	}
	return b.String()
}

func sqlType(c snowarrow.Column) string {
	switch c.Type {
	case snowarrow.Fixed:
		return fmt.Sprintf("NUMBER(%d,%d)", c.Precision, c.Scale)
	case snowarrow.Real:
		return "FLOAT"
	case snowarrow.Text:
		return "VARCHAR"
	case snowarrow.Boolean:
		return "BOOLEAN"
	case snowarrow.Date:
		return "DATE"
	case snowarrow.TimestampTZ:
		return fmt.Sprintf("TIMESTAMP_TZ(%d)", c.Scale)
	}
	return strings.ToUpper(c.Type)
}

// ident accepts only plain upper-case identifiers, so that a source name can
// never carry SQL.
func ident(s string) string {
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.') {
			panic(fmt.Sprintf("snowflake: %q is not a plain identifier", s))
		}
	}
	return s
}

func literal(v any) string {
	switch x := v.(type) {
	case string:
		return "'" + strings.ReplaceAll(x, "'", "''") + "'"
	case time.Time:
		return "'" + x.Format(time.DateOnly) + "'::DATE"
	case int, int64, float64:
		return fmt.Sprint(x)
	}
	return "'" + strings.ReplaceAll(fmt.Sprint(v), "'", "''") + "'"
}
