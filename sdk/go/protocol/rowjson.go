package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// AppendRowJSON writes row i of a batch as one JSON object, in the batch's
// column order: exact decimals as strings (§9.1.5), dates as YYYY-MM-DD,
// date-times as UTC RFC 3339 with microseconds, and a number that is not
// finite as null. NDJSON answers are made of these lines.
func AppendRowJSON(buf *bytes.Buffer, rec arrow.RecordBatch, i int) error {
	buf.WriteByte('{')
	for c := range int(rec.NumCols()) {
		if c > 0 {
			buf.WriteByte(',')
		}
		name, _ := json.Marshal(rec.ColumnName(c))
		buf.Write(name)
		buf.WriteByte(':')
		if err := appendValue(buf, rec.Column(c), i); err != nil {
			return fmt.Errorf("%s: %w", rec.ColumnName(c), err)
		}
	}
	buf.WriteByte('}')
	return nil
}

// RowRecord returns row i of a batch as a record in the same forms, for
// callers that want values rather than text.
func RowRecord(rec arrow.RecordBatch, i int) (map[string]any, error) {
	var buf bytes.Buffer
	if err := AppendRowJSON(&buf, rec, i); err != nil {
		return nil, err
	}
	return DecodeRecord(buf.Bytes())
}

func appendValue(buf *bytes.Buffer, a arrow.Array, i int) error {
	if a.IsNull(i) {
		buf.WriteString("null")
		return nil
	}
	switch x := a.(type) {
	case *array.String:
		b, _ := json.Marshal(x.Value(i))
		buf.Write(b)
	case *array.Int64:
		buf.WriteString(strconv.FormatInt(x.Value(i), 10))
	case *array.Float64:
		v := x.Value(i)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			buf.WriteString("null")
		} else {
			buf.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
		}
	case *array.Boolean:
		buf.WriteString(strconv.FormatBool(x.Value(i)))
	case *array.Date32:
		buf.WriteString(`"` + x.Value(i).ToTime().Format(time.DateOnly) + `"`)
	case *array.Timestamp:
		unit := x.DataType().(*arrow.TimestampType).Unit
		buf.WriteString(`"` + x.Value(i).ToTime(unit).UTC().Format("2006-01-02T15:04:05.000000Z") + `"`)
	case *array.Decimal128:
		scale := x.DataType().(*arrow.Decimal128Type).Scale
		buf.WriteString(`"` + FormatDecimal(x.Value(i).BigInt(), int(scale)) + `"`)
	default:
		return fmt.Errorf("no JSON form for %s", a.DataType())
	}
	return nil
}
