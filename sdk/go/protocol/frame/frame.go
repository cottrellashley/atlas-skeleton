// Package frame is the framing of a chunked answer (§9.6.5): NDJSON lines
// ended by an end line, and an Arrow IPC stream ended by a batch of zero
// rows that carries the end line. A reader never treats an answer without
// its end as complete: it is upstream_failed (AP-HTTP-002).
package frame

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/arrowschema"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// End is the end line (chunk-end.schema.json): the rows sent, whether they
// were cut at the caller's cap, the usage, and a failure after the first
// row. Every member is always written, and a reader requires every one:
// usage is {} when nothing was measured, and problem is null on success.
type End struct {
	Rows      int64
	Truncated bool
	Usage     json.RawMessage // a JSON object; nil is written as {}
	Problem   *protocol.Problem
}

// The end line's members, which chunk-end.schema.json requires.
var endMembers = []string{"$atlas", "rows", "truncated", "usage", "problem"}

// The members every problem has (problem.schema.json).
var problemMembers = []string{"type", "status", "code", "retryable", "next", "request_id"}

// MaxLine is the longest NDJSON line a reader takes.
const MaxLine = 16 << 20

func incomplete(why string) error {
	return protocol.NewProblem(protocol.CodeUpstreamFailed, "the answer is incomplete: "+why)
}

func invalid(why string) error {
	return protocol.NewProblem(protocol.CodeInvalidDocument, "the answer is not framed as §9.6.5 says: "+why)
}

func marshalEnd(e End, rows int64) ([]byte, error) {
	usage := e.Usage
	if len(usage) == 0 {
		usage = json.RawMessage("{}")
	}
	if u, err := ijson.Parse(usage); err != nil {
		return nil, fmt.Errorf("frame: the end line's usage: %w", err)
	} else if _, ok := u.(*ijson.Object); !ok {
		return nil, errors.New("frame: the end line's usage is an object")
	}
	problem := json.RawMessage("null")
	if e.Problem != nil {
		b, err := json.Marshal(e.Problem)
		if err != nil {
			return nil, err
		}
		v, err := ijson.Parse(b)
		if err != nil {
			return nil, err
		}
		if _, err := parseProblem(v.(*ijson.Object)); err != nil {
			return nil, fmt.Errorf("frame: the end line's problem is not a whole problem (its next and request_id are filled before the end): %w", err)
		}
		problem = b
	}
	return json.Marshal(struct {
		Atlas     string          `json:"$atlas"`
		Rows      int64           `json:"rows"`
		Truncated bool            `json:"truncated"`
		Usage     json.RawMessage `json:"usage"`
		Problem   json.RawMessage `json:"problem"`
	}{"end", rows, e.Truncated, usage, problem})
}

// parseEnd reads an end line as chunk-end.schema.json defines it: every
// member present and of its type, so that a line that only says
// {"$atlas":"end"} is never taken for a complete answer. A member it does
// not know is ignored, as in every document the gateway sends.
func parseEnd(o *ijson.Object) (*End, error) {
	for _, name := range endMembers {
		if _, ok := o.Get(name); !ok {
			return nil, invalid("the end line has no " + name)
		}
	}
	if m, _ := o.Get("$atlas"); m != "end" {
		return nil, invalid(fmt.Sprintf("the end line has $atlas %v", m))
	}
	var e End
	rows, _ := o.Get("rows")
	n, isNumber := rows.(ijson.Number)
	count, isInteger := n.Int64()
	if !isNumber || !isInteger || count < 0 {
		return nil, invalid("the end line's rows is not a count")
	}
	e.Rows = count
	truncated, _ := o.Get("truncated")
	t, isBool := truncated.(bool)
	if !isBool {
		return nil, invalid("the end line's truncated is not a boolean")
	}
	e.Truncated = t
	usage, _ := o.Get("usage")
	if _, isObject := usage.(*ijson.Object); !isObject {
		return nil, invalid("the end line's usage is not an object")
	}
	b, err := ijson.Marshal(usage)
	if err != nil {
		return nil, invalid("the end line's usage: " + err.Error())
	}
	e.Usage = b
	switch p := mustGet(o, "problem").(type) {
	case nil:
	case *ijson.Object:
		problem, err := parseProblem(p)
		if err != nil {
			return nil, err
		}
		e.Problem = problem
	default:
		return nil, invalid("the end line's problem is neither null nor a problem")
	}
	return &e, nil
}

func mustGet(o *ijson.Object, name string) ijson.Value {
	v, _ := o.Get(name)
	return v
}

// parseProblem reads the end line's problem, which has every member
// problem.schema.json requires.
func parseProblem(o *ijson.Object) (*protocol.Problem, error) {
	for _, name := range problemMembers {
		if _, ok := o.Get(name); !ok {
			return nil, invalid("the end line's problem has no " + name)
		}
	}
	for name, want := range map[string]string{"type": "string", "code": "string", "next": "string", "request_id": "string", "retryable": "boolean", "status": "integer"} {
		v := mustGet(o, name)
		var ok bool
		switch want {
		case "string":
			_, ok = v.(string)
		case "boolean":
			_, ok = v.(bool)
		case "integer":
			n, isNumber := v.(ijson.Number)
			_, isInteger := n.Int64()
			ok = isNumber && isInteger
		}
		if !ok {
			return nil, invalid(fmt.Sprintf("the end line's problem has a %s that is not a %s", name, want))
		}
	}
	b, err := ijson.Marshal(o)
	if err != nil {
		return nil, invalid("the end line's problem: " + err.Error())
	}
	var p protocol.Problem
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, invalid("the end line's problem: " + err.Error())
	}
	return &p, nil
}

// NDJSONWriter writes rows, one per line, then the end line.
type NDJSONWriter struct {
	w     *bufio.Writer
	rows  int64
	ended bool
}

// NewNDJSONWriter writes an NDJSON answer to w.
func NewNDJSONWriter(w io.Writer) *NDJSONWriter { return &NDJSONWriter{w: bufio.NewWriter(w)} }

// Row writes one record.
func (n *NDJSONWriter) Row(v ijson.Value) error {
	if n.ended {
		return errors.New("frame: a row after the end line")
	}
	b, err := ijson.Marshal(v)
	if err != nil {
		return err
	}
	n.rows++
	if _, err := n.w.Write(b); err != nil {
		return err
	}
	return n.w.WriteByte('\n')
}

// Flush sends what is buffered, so a slow stream reaches the client as it
// is produced.
func (n *NDJSONWriter) Flush() error { return n.w.Flush() }

// End writes the end line, counting the rows written, and flushes.
func (n *NDJSONWriter) End(e End) error {
	if n.ended {
		return errors.New("frame: a second end line")
	}
	n.ended = true
	b, err := marshalEnd(e, n.rows)
	if err != nil {
		return err
	}
	if _, err := n.w.Write(append(b, '\n')); err != nil {
		return err
	}
	return n.w.Flush()
}

// NDJSONReader reads an NDJSON answer: its rows, then its end.
type NDJSONReader struct {
	r    *bufio.Reader
	rows int64
	end  *End
	err  error
}

// NewNDJSONReader reads an NDJSON answer from r.
func NewNDJSONReader(r io.Reader) *NDJSONReader {
	return &NDJSONReader{r: bufio.NewReaderSize(r, 64<<10)}
}

// Next returns the next row. After the last row it returns io.EOF, or the
// end line's problem when the answer failed after its first row. An answer
// that stops without an end line is upstream_failed.
func (n *NDJSONReader) Next() (*ijson.Object, error) {
	if n.err != nil {
		return nil, n.err
	}
	row, err := n.next()
	if err != nil {
		n.err = err
	}
	return row, err
}

func (n *NDJSONReader) next() (*ijson.Object, error) {
	if n.end != nil {
		if _, err := n.r.ReadByte(); err != io.EOF {
			return nil, invalid("data after the end line")
		}
		if n.end.Problem != nil {
			return nil, n.end.Problem
		}
		return nil, io.EOF
	}
	line, err := n.line()
	if err != nil {
		return nil, err
	}
	v, err := ijson.ParseLimits(line, ijson.Limits{MaxBytes: MaxLine})
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidDocument, fmt.Sprintf("line %d: %v", n.rows+1, err))
	}
	o, ok := v.(*ijson.Object)
	if !ok {
		return nil, invalid(fmt.Sprintf("line %d is not a record", n.rows+1))
	}
	if _, isEnd := o.Get("$atlas"); isEnd {
		e, err := parseEnd(o)
		if err != nil {
			return nil, err
		}
		if e.Rows != n.rows {
			return nil, invalid(fmt.Sprintf("the end line counts %d rows, and %d arrived", e.Rows, n.rows))
		}
		n.end = e
		return n.next()
	}
	n.rows++
	return o, nil
}

// line reads one line ended by a single LF.
func (n *NDJSONReader) line() ([]byte, error) {
	var buf []byte
	for {
		chunk, err := n.r.ReadSlice('\n')
		buf = append(buf, chunk...)
		if len(buf) > MaxLine {
			return nil, invalid(fmt.Sprintf("a line longer than %d bytes", MaxLine))
		}
		switch {
		case err == nil:
			line := buf[:len(buf)-1]
			if len(bytes.TrimSpace(line)) == 0 {
				return nil, invalid("a blank line")
			}
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case err == io.EOF:
			if len(buf) == 0 {
				return nil, incomplete("it ends without an end line")
			}
			return nil, incomplete("its last line is cut short")
		default:
			return nil, incomplete(err.Error())
		}
	}
}

// End returns the end line, once Next has returned io.EOF or its problem.
func (n *NDJSONReader) End() *End { return n.end }

// ArrowWriter writes an Arrow IPC stream: the schema, record batches, then
// the final batch of zero rows with the end line, then end-of-stream.
type ArrowWriter struct {
	w      *ipc.Writer
	mem    memory.Allocator
	schema *arrow.Schema
	rows   int64
	ended  bool
}

// NewArrowWriter writes an Arrow answer to w. The schema carries
// atlas:profile and atlas:protocol (§9.2.7).
func NewArrowWriter(w io.Writer, schema *arrow.Schema, mem memory.Allocator) (*ArrowWriter, error) {
	for _, k := range []string{arrowschema.MetaProfile, arrowschema.MetaProtocol} {
		if _, ok := schema.Metadata().GetValue(k); !ok {
			return nil, fmt.Errorf("frame: the stream's schema has no %s", k)
		}
	}
	return &ArrowWriter{w: ipc.NewWriter(w, ipc.WithSchema(schema), ipc.WithAllocator(mem)), mem: mem, schema: schema}, nil
}

// Write writes one batch of rows.
func (a *ArrowWriter) Write(b arrow.RecordBatch) error {
	if a.ended {
		return errors.New("frame: a batch after the final batch")
	}
	a.rows += b.NumRows()
	return a.w.Write(b)
}

// End writes the final batch, counting the rows written, and closes the
// stream with its end-of-stream marker.
func (a *ArrowWriter) End(e End) error {
	if a.ended {
		return errors.New("frame: a second final batch")
	}
	a.ended = true
	b, err := marshalEnd(e, a.rows)
	if err != nil {
		return err
	}
	cols := make([]arrow.Array, a.schema.NumFields())
	for i, f := range a.schema.Fields() {
		cols[i] = array.MakeArrayOfNull(a.mem, f.Type, 0)
	}
	final := array.NewRecordBatchWithMetadata(a.schema, cols, 0, arrow.NewMetadata([]string{arrowschema.MetaEnd}, []string{string(b)}))
	for _, c := range cols {
		c.Release()
	}
	defer final.Release()
	if err := a.w.Write(final); err != nil {
		return err
	}
	return a.w.Close()
}

// ArrowReader reads an Arrow answer: its batches, then its end.
type ArrowReader struct {
	r    *ipc.Reader
	rows int64
	end  *End
	err  error
}

// NewArrowReader reads an Arrow answer's schema from r.
func NewArrowReader(r io.Reader, mem memory.Allocator) (*ArrowReader, error) {
	rd, err := ipc.NewReader(r, ipc.WithAllocator(mem))
	if err != nil {
		return nil, incomplete("its schema: " + err.Error())
	}
	return &ArrowReader{r: rd}, nil
}

// Schema is the stream's schema, with its atlas:profile.
func (a *ArrowReader) Schema() *arrow.Schema { return a.r.Schema() }

// Next returns the next batch of rows, which the caller releases. After the
// last batch it returns io.EOF, or the end line's problem. A stream that
// stops without the final batch is upstream_failed.
func (a *ArrowReader) Next() (arrow.RecordBatch, error) {
	if a.err != nil {
		return nil, a.err
	}
	b, err := a.next()
	if err != nil {
		a.err = err
	}
	return b, err
}

func (a *ArrowReader) next() (arrow.RecordBatch, error) {
	if !a.r.Next() {
		if err := a.r.Err(); err != nil && a.end == nil {
			return nil, incomplete(err.Error())
		}
		if a.end == nil {
			return nil, incomplete("it ends without its final batch")
		}
		if a.end.Problem != nil {
			return nil, a.end.Problem
		}
		return nil, io.EOF
	}
	b := a.r.RecordBatch()
	if a.end != nil {
		return nil, invalid("a batch after the final batch")
	}
	if m, ok := b.(arrow.RecordBatchWithMetadata); ok {
		if text, isEnd := m.Metadata().GetValue(arrowschema.MetaEnd); isEnd {
			if b.NumRows() != 0 {
				return nil, invalid(fmt.Sprintf("the final batch has %d rows", b.NumRows()))
			}
			v, err := ijson.ParseLimits([]byte(text), ijson.Limits{MaxBytes: MaxLine})
			if err != nil {
				return nil, invalid("the end line: " + err.Error())
			}
			o, ok := v.(*ijson.Object)
			if !ok {
				return nil, invalid("the final batch's atlas:end is not an end line")
			}
			e, err := parseEnd(o)
			if err != nil {
				return nil, err
			}
			if e.Rows != a.rows {
				return nil, invalid(fmt.Sprintf("the end line counts %d rows, and %d arrived", e.Rows, a.rows))
			}
			a.end = e
			return a.next()
		}
	}
	a.rows += b.NumRows()
	b.Retain()
	return b, nil
}

// End returns the end line, once Next has returned io.EOF or its problem.
func (a *ArrowReader) End() *End { return a.end }

// Release frees the reader.
func (a *ArrowReader) Release() { a.r.Release() }
