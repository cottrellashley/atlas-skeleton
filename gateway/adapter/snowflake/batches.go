package snowflake

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/bitutil"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
)

// contractName is a writer field's name, which a statement writes as a
// quoted alias: a property name of a profile (§9.2).
var contractName = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,127}$`)

// outputs are a plan's answer columns, and the select list that makes
// each: a field of the writer schema is one column; a struct field, such as
// Money, is one column per member, aliased field.member. Each is the query
// column it is made from, cast to its contract type.
func outputs(r ports.PlanRequest) ([]Output, []string, error) {
	if r.Writer == nil || r.Writer.NumFields() == 0 {
		return nil, nil, fmt.Errorf("%w: a plan needs its writer schema", ports.ErrPlanRefused)
	}
	if len(r.Columns) != r.Writer.NumFields() {
		return nil, nil, fmt.Errorf("%w: %d fields made from %d sets of columns", ports.ErrPlanRefused, r.Writer.NumFields(), len(r.Columns))
	}
	byName := map[string]ports.Column{}
	for _, c := range r.Query.Columns {
		byName[strings.ToUpper(c.Name)] = c
	}
	var outs []Output
	var items []string
	add := func(name string, t arrow.DataType, from string) error {
		c, ok := byName[strings.ToUpper(from)]
		if !ok {
			return fmt.Errorf("%w: %s is made from %s, which the query does not read", ports.ErrPlanRefused, name, from)
		}
		item, wall, err := cast(c, t)
		if err != nil {
			return err
		}
		outs = append(outs, Output{Name: name, Type: t, WallUTC: wall})
		items = append(items, item)
		return nil
	}
	for i, f := range r.Writer.Fields() {
		if !contractName.MatchString(f.Name) {
			return nil, nil, fmt.Errorf("%w: %q is not a contract name", ports.ErrPlanRefused, f.Name)
		}
		if st, ok := f.Type.(*arrow.StructType); ok {
			if len(r.Columns[i]) != st.NumFields() {
				return nil, nil, fmt.Errorf("%w: %s has %d members, made from %d columns", ports.ErrPlanRefused, f.Name, st.NumFields(), len(r.Columns[i]))
			}
			for j, m := range st.Fields() {
				if !contractName.MatchString(m.Name) {
					return nil, nil, fmt.Errorf("%w: %q is not a contract name", ports.ErrPlanRefused, m.Name)
				}
				if err := add(f.Name+"."+m.Name, m.Type, r.Columns[i][j]); err != nil {
					return nil, nil, err
				}
			}
			continue
		}
		if len(r.Columns[i]) != 1 {
			return nil, nil, fmt.Errorf("%w: %s is made from %d columns, not one", ports.ErrPlanRefused, f.Name, len(r.Columns[i]))
		}
		if err := add(f.Name, f.Type, r.Columns[i][0]); err != nil {
			return nil, nil, err
		}
	}
	return outs, items, nil
}

// cast is a query column read as its contract type: the select item, and
// whether it is a timestamp the statement converts to UTC wall-clock time.
// Only a cast that keeps the value's meaning is written: a decimal is never
// read through a float, unless its contract declares a number (ATLAS-
// SNOWFLAKE-004).
func cast(c ports.Column, t arrow.DataType) (string, bool, error) {
	r, err := ref(rows, c.Name, c.Type, c.Zone)
	if err != nil {
		return "", false, fmt.Errorf("%w: %w", ports.ErrPlanRefused, err)
	}
	switch t := t.(type) {
	case *arrow.StringType:
		if c.Type == ports.String {
			return "CAST(" + r + " AS VARCHAR)", false, nil
		}
	case *arrow.Int64Type:
		if c.Type == ports.Integer { // Q-SNOW-4: an integer is a NUMBER(18,0)
			return "CAST(" + r + " AS NUMBER(18, 0))", false, nil
		}
	case *arrow.Float64Type:
		if c.Type == ports.Float || c.Type == ports.Decimal || c.Type == ports.Integer {
			return "CAST(" + r + " AS DOUBLE)", false, nil
		}
	case *arrow.Decimal128Type:
		if (c.Type == ports.Decimal || c.Type == ports.Integer) && t.Precision >= 1 && t.Precision <= 38 && t.Scale >= 0 && t.Scale <= t.Precision {
			return fmt.Sprintf("CAST(%s AS NUMBER(%d, %d))", r, t.Precision, t.Scale), false, nil
		}
	case *arrow.BooleanType:
		if c.Type == ports.Boolean {
			return "CAST(" + r + " AS BOOLEAN)", false, nil
		}
	case *arrow.Date32Type:
		if c.Type == ports.Date {
			return "CAST(" + r + " AS DATE)", false, nil
		}
	case *arrow.TimestampType:
		// An instant is read as it is, so the engine can see a wall-clock
		// time that declared no zone and refuse it; a declared zone is
		// converted in the statement (§2.2.1.6).
		if c.Type == ports.Timestamp && t.Unit == arrow.Microsecond && t.TimeZone == "UTC" {
			return r, c.Zone != "", nil
		}
	}
	return "", false, fmt.Errorf("%w: the %s column %s cannot be read as %s", ports.ErrPlanRefused, c.Type, c.Name, t)
}

// stream is an engine's answer as the writer schema's batches: each engine
// batch checked against the statement's columns, its struct fields
// assembled from their members' columns without copying them, and checked
// against the writer schema (06-4 rule 2: a batch that is not is the
// adapter's fault).
type stream struct {
	source   *Source
	writer   *arrow.Schema
	outputs  []Output
	engine   ports.BatchStream
	released bool
	failed   error
}

func (s *stream) Schema() *arrow.Schema { return s.writer }

func (s *stream) Next(ctx context.Context) (arrow.RecordBatch, error) {
	if s.failed != nil {
		return nil, s.failed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	flat, err := s.engine.Next(ctx)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		s.failed = s.source.classify(ctx, err)
		return nil, s.failed
	}
	defer flat.Release()
	b, err := assemble(s.source.mem, s.writer, s.outputs, flat)
	if err != nil {
		s.failed = err
		return nil, err
	}
	return b, nil
}

// Close releases the engine's answer, which stops its query if it is still
// running, and the bulkhead's place.
func (s *stream) Close() error {
	err := s.engine.Close()
	if !s.released {
		s.released = true
		s.source.inflight.release()
	}
	return err
}

// assemble makes a writer batch from an engine batch of the statement's
// columns.
func assemble(mem memory.Allocator, writer *arrow.Schema, outs []Output, flat arrow.RecordBatch) (arrow.RecordBatch, error) {
	if int(flat.NumCols()) != len(outs) {
		return nil, fmt.Errorf("snowflake: the engine answered %d columns for %d", flat.NumCols(), len(outs))
	}
	for k, o := range outs {
		if !arrow.TypeEqual(flat.Column(k).DataType(), o.Type) {
			return nil, fmt.Errorf("snowflake: the engine answered %s as %s, not %s", o.Name, flat.Column(k).DataType(), o.Type)
		}
	}
	n := int(flat.NumRows())
	cols := make([]arrow.Array, 0, writer.NumFields())
	defer func() {
		for _, c := range cols {
			c.Release()
		}
	}()
	k := 0
	for _, f := range writer.Fields() {
		st, isStruct := f.Type.(*arrow.StructType)
		if !isStruct {
			col := flat.Column(k)
			k++
			if !f.Nullable && col.NullN() > 0 {
				return nil, fmt.Errorf("snowflake: %s is null in %d rows, and its contract does not allow null", f.Name, col.NullN())
			}
			col.Retain()
			cols = append(cols, col)
			continue
		}
		members := make([]arrow.Array, st.NumFields())
		for j := range members {
			members[j] = flat.Column(k + j)
		}
		k += len(members)
		s, err := structOf(mem, f, st, members, n)
		if err != nil {
			return nil, err
		}
		cols = append(cols, s)
	}
	return array.NewRecordBatch(writer, cols, int64(n)), nil
}

// structOf is a struct field made from its members' columns: null where
// every member is null, and refused where only some are (a Money has both
// its amount and its currency, or neither).
func structOf(mem memory.Allocator, f arrow.Field, st *arrow.StructType, members []arrow.Array, n int) (arrow.Array, error) {
	var bitmap *memory.Buffer
	nulls := 0
	for i := range n {
		some, all := false, true
		for _, m := range members {
			if m.IsNull(i) {
				some = true
			} else {
				all = false
			}
		}
		switch {
		case all:
			if bitmap == nil {
				bitmap = memory.NewResizableBuffer(mem)
				bitmap.Resize(int(bitutil.BytesForBits(int64(n))))
				bits := bitmap.Bytes()
				for b := range bits {
					bits[b] = 0xff
				}
			}
			bitutil.ClearBit(bitmap.Bytes(), i)
			nulls++
		case some:
			if bitmap != nil {
				bitmap.Release()
			}
			return nil, fmt.Errorf("snowflake: %s has only some of its members in row %d", f.Name, i)
		}
	}
	if bitmap != nil {
		defer bitmap.Release()
	}
	if nulls > 0 && !f.Nullable {
		return nil, fmt.Errorf("snowflake: %s is null in %d rows, and its contract does not allow null", f.Name, nulls)
	}
	s, err := array.NewStructArrayWithFieldsAndNulls(members, st.Fields(), bitmap, nulls, 0)
	if err != nil {
		return nil, fmt.Errorf("snowflake: assembling %s: %w", f.Name, err)
	}
	return s, nil
}
