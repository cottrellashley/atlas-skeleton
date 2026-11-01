package fakes

import (
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
)

// Table is a warehouse object held in memory: its columns, and its rows,
// each value of its column's type (ports.ColumnType) or nil. A timestamp
// column with a Zone holds wall-clock times in that zone (a time.Time whose
// clock reading is the wall clock, whatever its location), as Snowflake's
// TIMESTAMP_NTZ does; one without holds instants.
type Table struct {
	Name    string
	Columns []ports.Column
	Rows    [][]any
}

// evaluate is a query's rows, each of the query's columns in order.
func evaluate(t *Table, q ports.Query) ([][]any, error) {
	index := map[string]int{}
	for i, c := range t.Columns {
		index[strings.ToUpper(c.Name)] = i
	}
	col := func(name string) (int, ports.ColumnType, error) {
		i, ok := index[strings.ToUpper(name)]
		if !ok {
			return 0, "", fmt.Errorf("fakes: %s has no column %s", t.Name, name)
		}
		return i, t.Columns[i].Type, nil
	}
	match := func(row []any, ps []ports.Predicate) (bool, error) {
		for _, p := range ps {
			i, typ, err := col(p.Column)
			if err != nil {
				return false, err
			}
			v := row[i]
			if typ == ports.Timestamp {
				if v, err = instantOf(v, t.Columns[i], p.Zone); err != nil {
					return false, err
				}
			}
			ok, err := holds(v, typ, p)
			if err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	}
	rows := t.Rows
	if l := q.Latest; l != nil {
		di, _, err := col(l.Date)
		if err != nil {
			return nil, err
		}
		per := make([]int, len(l.Per))
		for k, name := range l.Per {
			if per[k], _, err = col(name); err != nil {
				return nil, err
			}
		}
		key := func(row []any) (string, bool) {
			var b strings.Builder
			for _, i := range per {
				if row[i] == nil {
					return "", false
				}
				fmt.Fprintf(&b, "%v\x00", row[i])
			}
			return b.String(), true
		}
		latest := map[string]string{}
		for _, row := range rows {
			ok, err := match(row, l.Within)
			if err != nil {
				return nil, err
			}
			d, isDate := row[di].(string)
			k, keyed := key(row)
			if !ok || !isDate || !keyed || l.OnOrBefore != "" && d > l.OnOrBefore {
				continue
			}
			if d > latest[k] {
				latest[k] = d
			}
		}
		var kept [][]any
		for _, row := range rows {
			k, keyed := key(row)
			if d, isDate := row[di].(string); keyed && isDate && latest[k] == d {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	var out [][]any
	for _, row := range rows {
		ok, err := match(row, q.Where)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, row)
		}
	}
	order := make([]int, len(q.OrderBy))
	for k, name := range q.OrderBy {
		i, _, err := col(name)
		if err != nil {
			return nil, err
		}
		order[k] = i
	}
	slices.SortStableFunc(out, func(a, b []any) int {
		for _, i := range order {
			if c := compareNullsLast(a[i], b[i], t.Columns[i].Type); c != 0 {
				return c
			}
		}
		return 0
	})
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	picked := make([]int, len(q.Columns))
	for k, c := range q.Columns {
		i, _, err := col(c.Name)
		if err != nil {
			return nil, err
		}
		picked[k] = i
	}
	result := make([][]any, len(out))
	for r, row := range out {
		result[r] = make([]any, len(picked))
		for k, i := range picked {
			v := row[i]
			if t.Columns[i].Type == ports.Timestamp {
				var err error
				if v, err = instantOf(v, t.Columns[i], q.Columns[k].Zone); err != nil {
					return nil, err
				}
			}
			result[r][k] = v
		}
	}
	return result, nil
}

// instantOf is a timestamp cell as an instant in UTC: a wall-clock time
// read in the zone the query declares for it, or an instant as it is. A
// wall-clock time the query declares no zone for is refused, never
// assumed to be UTC, and a zone declared for instants is the deployment's
// mistake (§2.2.1.6, R053).
func instantOf(v any, c ports.Column, zone string) (any, error) {
	if v == nil {
		return nil, nil
	}
	t, ok := v.(time.Time)
	if !ok {
		return nil, fmt.Errorf("fakes: %s holds a %T, not a time", c.Name, v)
	}
	switch {
	case c.Zone == "" && zone == "":
		return t.UTC(), nil
	case c.Zone == "":
		return nil, fmt.Errorf("fakes: %s holds instants: it has no zone to convert from", c.Name)
	case zone == "":
		return nil, fmt.Errorf("fakes: %s holds wall-clock times in no declared zone: declare its zone", c.Name)
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, fmt.Errorf("fakes: %s: %w", c.Name, err)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc).UTC(), nil
}

// holds is one predicate on one value: false for a null, as in SQL.
func holds(v any, typ ports.ColumnType, p ports.Predicate) (bool, error) {
	if v == nil {
		return false, nil
	}
	for _, want := range p.Values {
		if want == nil {
			return false, fmt.Errorf("fakes: a null in a predicate on %s", p.Column)
		}
	}
	switch p.Op {
	case ports.In:
		for _, want := range p.Values {
			if c, err := compare(v, want, typ); err != nil {
				return false, err
			} else if c == 0 {
				return true, nil
			}
		}
		return false, nil
	case ports.Eq, ports.Gte, ports.Lte:
		if len(p.Values) != 1 {
			return false, fmt.Errorf("fakes: %s takes one value", p.Op)
		}
		c, err := compare(v, p.Values[0], typ)
		if err != nil {
			return false, err
		}
		return p.Op == ports.Eq && c == 0 || p.Op == ports.Gte && c >= 0 || p.Op == ports.Lte && c <= 0, nil
	}
	return false, fmt.Errorf("fakes: no comparison %q", p.Op)
}

func compareNullsLast(a, b any, typ ports.ColumnType) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	}
	c, _ := compare(a, b, typ)
	return c
}

// compare orders two values of a column's type; a predicate's value may be
// written in any Go type that holds it.
func compare(a, b any, typ ports.ColumnType) (int, error) {
	switch typ {
	case ports.String, ports.Date:
		x, ok1 := a.(string)
		y, ok2 := b.(string)
		if !ok1 || !ok2 {
			return 0, fmt.Errorf("fakes: %T and %T are not both strings", a, b)
		}
		return strings.Compare(x, y), nil
	case ports.Boolean:
		x, ok1 := a.(bool)
		y, ok2 := b.(bool)
		if !ok1 || !ok2 {
			return 0, fmt.Errorf("fakes: %T and %T are not both booleans", a, b)
		}
		switch {
		case x == y:
			return 0, nil
		case !x:
			return -1, nil
		}
		return 1, nil
	case ports.Timestamp:
		x, ok1 := timeOf(a)
		y, ok2 := timeOf(b)
		if !ok1 || !ok2 {
			return 0, fmt.Errorf("fakes: %v and %v are not both instants", a, b)
		}
		return x.Compare(y), nil
	}
	x, ok1 := number(a)
	y, ok2 := number(b)
	if !ok1 || !ok2 {
		return 0, fmt.Errorf("fakes: %T and %T are not both numbers", a, b)
	}
	return x.Cmp(y), nil
}

// timeOf is a timestamp value as an instant: a time.Time, or an RFC 3339
// date-time with its offset, as a request writes one (R111).
func timeOf(v any) (time.Time, bool) {
	switch x := v.(type) {
	case time.Time:
		return x, true
	case string:
		t, err := grammar.ParseDateTime(x)
		return t, err == nil
	}
	return time.Time{}, false
}

func number(v any) (*big.Rat, bool) {
	switch x := v.(type) {
	case int64:
		return new(big.Rat).SetInt64(x), true
	case int:
		return new(big.Rat).SetInt64(int64(x)), true
	case float64:
		r := new(big.Rat)
		if r.SetFloat64(x) == nil {
			return nil, false
		}
		return r, true
	case string:
		return new(big.Rat).SetString(x)
	}
	return nil, false
}

// memRows are a query's rows. The query's context bounds them, as it
// bounds a driver's: once it ends, the rows end with its error, never with
// io.EOF.
