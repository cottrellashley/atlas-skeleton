package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// MetaEnd is the message metadata key of an Arrow answer's final batch.
const MetaEnd = "atlas:end"

// EndBatch returns the final batch of an Arrow answer (§9.6.5): no rows, in
// the answer's schema, with the end line as its message metadata. The
// end-of-stream marker follows it. The caller releases it.
func EndBatch(mem memory.Allocator, schema *arrow.Schema, end EndLine) (arrow.RecordBatch, error) {
	end.Atlas = "end"
	b, err := json.Marshal(end)
	if err != nil {
		return nil, err
	}
	cols := make([]arrow.Array, schema.NumFields())
	for i, f := range schema.Fields() {
		cols[i] = array.MakeArrayOfNull(mem, f.Type, 0)
		defer cols[i].Release()
	}
	return array.NewRecordBatchWithMetadata(schema, cols, 0, arrow.NewMetadata([]string{MetaEnd}, []string{string(b)})), nil
}

// EndOf reports whether rec is an Arrow answer's final batch and, when it
// is, returns its end line. A final batch with rows, or whose metadata is not
// an end line, is an error.
func EndOf(rec arrow.RecordBatch) (*EndLine, bool, error) {
	m, ok := rec.(arrow.RecordBatchWithMetadata)
	if !ok {
		return nil, false, nil
	}
	md := m.Metadata()
	i := md.FindKey(MetaEnd)
	if i < 0 {
		return nil, false, nil
	}
	var end EndLine
	if err := json.Unmarshal([]byte(md.Values()[i]), &end); err != nil || end.Atlas != "end" {
		return nil, true, fmt.Errorf("the final batch's %s is not an end line", MetaEnd)
	}
	if rec.NumRows() != 0 {
		return nil, true, fmt.Errorf("the final batch has %d rows", rec.NumRows())
	}
	return &end, true, nil
}
