// Package snowarrow reads and writes Snowflake's Arrow result chunks, the
// handoff format snowflake-arrow/1. Its decoder turns a chunk into a writer
// profile's Arrow schema; it is written once, in Go, for the gateway's
// streamed path and for every client's handoff path (§2.2.1.3).
//
// The encoding is what this skeleton assumes Snowflake sends. Every line
// marked ASSUMPTION is checked against real result chunks in Proof 1. The
// fake Snowflake writes exactly these assumptions through Encode, so a
// difference found later shows up as a failing decoder test, not as wrong
// data reaching a caller.
//
//   - Each chunk is a complete Arrow IPC stream with its own schema.
//     ASSUMPTION.
//   - fixed (NUMBER) with precision up to 18: the narrowest of Int8, Int16,
//     Int32 and Int64 that holds every value in the chunk, holding the value
//     times 10^scale. ASSUMPTION.
//   - fixed with precision above 18: Decimal128(precision, scale). ASSUMPTION.
//   - real: Float64. text: Utf8. boolean: Boolean. date: Date32. ASSUMPTION.
//   - timestamp_ntz and timestamp_ltz with scale up to 7: Int64 holding
//     seconds since the epoch times 10^scale; above 7, a struct of epoch
//     (Int64 seconds) and fraction (Int32 nanoseconds). ASSUMPTION.
//   - timestamp_tz with scale up to 3: a struct of epoch (Int64, scaled) and
//     timezone (Int32, offset minutes plus 1440); above 3, a struct of epoch
//     (Int64 seconds), fraction (Int32 nanoseconds) and timezone. ASSUMPTION.
package snowarrow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Format names this encoding in a handoff document.
const Format = "snowflake-arrow/1"

// Snowflake's logical types, as its row type names them.
const (
	Fixed        = "fixed"
	Real         = "real"
	Text         = "text"
	Boolean      = "boolean"
	Date         = "date"
	TimestampNTZ = "timestamp_ntz"
	TimestampLTZ = "timestamp_ltz"
	TimestampTZ  = "timestamp_tz"
)

// Column is one column of Snowflake's row type, as the query response
// describes it.
type Column struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Precision int    `json:"precision,omitempty"`
	Scale     int    `json:"scale"`
	Nullable  bool   `json:"nullable"`
}

// RowType is a result's columns, in order.
type RowType []Column

// MarshalSource writes the row type as a handoff document's source member.
func (rt RowType) MarshalSource() json.RawMessage {
	b, _ := json.Marshal(struct {
		RowType RowType `json:"row_type"`
	}{rt})
	return b
}

// ParseSource reads a handoff document's source member.
func ParseSource(raw json.RawMessage) (RowType, error) {
	var s struct {
		RowType RowType `json:"row_type"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("handoff source: %w", err)
	}
	if len(s.RowType) == 0 {
		return nil, fmt.Errorf("handoff source has no row type")
	}
	return s.RowType, nil
}

// WriteChunk serialises a batch as one chunk: an Arrow IPC stream.
func WriteChunk(rec arrow.RecordBatch) ([]byte, error) {
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(rec.Schema()))
	if err := w.Write(rec); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ReadChunk reads every batch of one chunk. The caller releases them.
func ReadChunk(mem memory.Allocator, r io.Reader) ([]arrow.RecordBatch, error) {
	rd, err := ipc.NewReader(r, ipc.WithAllocator(mem))
	if err != nil {
		return nil, fmt.Errorf("chunk is not an Arrow IPC stream: %w", err)
	}
	defer rd.Release()
	var out []arrow.RecordBatch
	for rd.Next() {
		rec := rd.RecordBatch()
		rec.Retain()
		out = append(out, rec)
	}
	if err := rd.Err(); err != nil {
		for _, r := range out {
			r.Release()
		}
		return nil, err
	}
	return out, nil
}
