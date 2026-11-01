package frame

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/arrowschema"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

func code(err error) string {
	var p *protocol.Problem
	if errors.As(err, &p) {
		return p.Code
	}
	if err == io.EOF {
		return "EOF"
	}
	return "not a problem: " + err.Error()
}

func ndjson(t *testing.T, rows []string, end End) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := NewNDJSONWriter(&buf)
	for _, r := range rows {
		v, err := ijson.Parse([]byte(r))
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Row(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.End(end); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// readAll reads an answer to its end, returning the rows and how it ended.
func readAll(b []byte) (int, error) {
	r := NewNDJSONReader(bytes.NewReader(b))
	n := 0
	for {
		_, err := r.Next()
		if err != nil {
			return n, err
		}
		n++
	}
}

func TestNDJSON(t *testing.T) {
	b := ndjson(t, []string{`{"a":1}`, `{"a":2,"b":"x"}`}, End{Truncated: true, Usage: json.RawMessage(`{"bytes":2048}`)})
	want := "{\"a\":1}\n{\"a\":2,\"b\":\"x\"}\n{\"$atlas\":\"end\",\"rows\":2,\"truncated\":true,\"usage\":{\"bytes\":2048}}\n"
	if string(b) != want {
		t.Fatalf("framing\n got %q\nwant %q", b, want)
	}
	r := NewNDJSONReader(bytes.NewReader(b))
	for i := 0; i < 2; i++ {
		if _, err := r.Next(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("after the rows: %v", err)
	}
	if e := r.End(); e == nil || e.Rows != 2 || !e.Truncated || string(e.Usage) != `{"bytes":2048}` {
		t.Fatalf("end: %+v", r.End())
	}
	failed := ndjson(t, []string{`{"a":1}`}, End{Problem: protocol.NewProblem(protocol.CodeUpstreamFailed, "the warehouse went away")})
	if n, err := readAll(failed); n != 1 || code(err) != protocol.CodeUpstreamFailed {
		t.Fatalf("a failure after the first row is the end line's problem: %d %v", n, err)
	}
}

// AP-HTTP-002: every truncation of an NDJSON answer is upstream_failed.
func TestNDJSONTruncated(t *testing.T) {
	b := ndjson(t, []string{`{"a":1}`, `{"a":"two"}`, `{"a":[3]}`}, End{})
	for cut := 0; cut < len(b); cut++ {
		if _, err := readAll(b[:cut]); code(err) != protocol.CodeUpstreamFailed {
			t.Fatalf("cut at %d of %d: %v", cut, len(b), err)
		}
	}
	if n, err := readAll(b); n != 3 || err != io.EOF {
		t.Fatalf("the whole answer: %d %v", n, err)
	}
}

func TestNDJSONRefuses(t *testing.T) {
	for name, body := range map[string]string{
		"a blank line":                  "{\"a\":1}\n\n{\"$atlas\":\"end\",\"rows\":1,\"truncated\":false}\n",
		"a row that is not a record":    "[1]\n{\"$atlas\":\"end\",\"rows\":1,\"truncated\":false}\n",
		"duplicate members":             "{\"a\":1,\"a\":2}\n{\"$atlas\":\"end\",\"rows\":1,\"truncated\":false}\n",
		"the wrong count":               "{\"a\":1}\n{\"$atlas\":\"end\",\"rows\":2,\"truncated\":false}\n",
		"data after the end line":       "{\"$atlas\":\"end\",\"rows\":0,\"truncated\":false}\n{\"a\":1}\n",
		"a marker that is not the end":  "{\"$atlas\":\"start\"}\n",
		"two values on a line":          "{\"a\":1} {\"a\":2}\n{\"$atlas\":\"end\",\"rows\":1,\"truncated\":false}\n",
		"an end line that is not valid": "{\"$atlas\":\"end\",\"rows\":\"none\"}\n",
	} {
		if _, err := readAll([]byte(body)); code(err) != protocol.CodeInvalidDocument {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func arrowSchema() *arrow.Schema {
	md := arrow.NewMetadata([]string{arrowschema.MetaProfile, arrowschema.MetaProtocol}, []string{"a.b@v1#3f9a1c0b7d2e4a65", protocol.Version})
	return arrow.NewSchema([]arrow.Field{{Name: "a", Type: arrow.PrimitiveTypes.Int64}, {Name: "b", Type: arrow.BinaryTypes.String, Nullable: true}}, &md)
}

func batch(mem memory.Allocator, from, n int) arrow.RecordBatch {
	b := array.NewRecordBuilder(mem, arrowSchema())
	defer b.Release()
	for i := from; i < from+n; i++ {
		b.Field(0).(*array.Int64Builder).Append(int64(i))
		b.Field(1).(*array.StringBuilder).Append(strings.Repeat("x", i%5))
	}
	return b.NewRecordBatch()
}

func arrowAnswer(t *testing.T, mem memory.Allocator, end End) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := NewArrowWriter(&buf, arrowSchema(), mem)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		b := batch(mem, i*10, 10)
		if err := w.Write(b); err != nil {
			t.Fatal(err)
		}
		b.Release()
	}
	if err := w.End(end); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func readArrow(mem memory.Allocator, b []byte) (int64, *End, error) {
	r, err := NewArrowReader(bytes.NewReader(b), mem)
	if err != nil {
		return 0, nil, err
	}
	defer r.Release()
	var rows int64
	for {
		rec, err := r.Next()
		if err != nil {
			return rows, r.End(), err
		}
		rows += rec.NumRows()
		rec.Release()
	}
}

func TestArrow(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)
	b := arrowAnswer(t, mem, End{Usage: json.RawMessage(`{"bytes":1}`)})
	rows, end, err := readArrow(mem, b)
	if err != io.EOF || rows != 30 || end == nil || end.Rows != 30 {
		t.Fatalf("rows %d, end %+v, %v", rows, end, err)
	}
	failed := arrowAnswer(t, mem, End{Problem: protocol.NewProblem(protocol.CodeDeadlineExceeded, "stopped")})
	if rows, _, err := readArrow(mem, failed); rows != 30 || code(err) != protocol.CodeDeadlineExceeded {
		t.Fatalf("the final batch's problem: %d %v", rows, err)
	}
	if _, err := NewArrowWriter(io.Discard, arrow.NewSchema(arrowSchema().Fields(), nil), mem); err == nil {
		t.Fatal("a stream's schema carries atlas:profile and atlas:protocol")
	}
}

// AP-HTTP-002: an Arrow answer without its final batch is upstream_failed.
// A cut after the final batch loses only the end-of-stream marker, which
// readers do not need.
func TestArrowTruncated(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)
	b := arrowAnswer(t, mem, End{})
	const eos = 8
	for cut := 0; cut < len(b)-eos; cut++ {
		if _, _, err := readArrow(mem, b[:cut]); code(err) != protocol.CodeUpstreamFailed {
			t.Fatalf("cut at %d of %d: %v", cut, len(b), err)
		}
	}
	if rows, _, err := readArrow(mem, b[:len(b)-eos]); rows != 30 || err != io.EOF {
		t.Fatalf("without the end-of-stream marker: %d %v", rows, err)
	}
}

func TestArrowRefuses(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)
	write := func(batches ...arrow.RecordBatch) []byte {
		var buf bytes.Buffer
		w := ipc.NewWriter(&buf, ipc.WithSchema(arrowSchema()), ipc.WithAllocator(mem))
		for _, b := range batches {
			if err := w.Write(b); err != nil {
				t.Fatal(err)
			}
			b.Release()
		}
		w.Close()
		return buf.Bytes()
	}
	end := func(rows int64, text string) arrow.RecordBatch {
		b := batch(mem, 0, int(rows))
		defer b.Release()
		cols := []arrow.Array{b.Column(0), b.Column(1)}
		return array.NewRecordBatchWithMetadata(arrowSchema(), cols, rows, arrow.NewMetadata([]string{arrowschema.MetaEnd}, []string{text}))
	}
	for name, body := range map[string][]byte{
		"a batch after the final batch": write(end(0, `{"$atlas":"end","rows":0,"truncated":false}`), batch(mem, 0, 1)),
		"a final batch with rows":       write(end(2, `{"$atlas":"end","rows":2,"truncated":false}`)),
		"the wrong count":               write(batch(mem, 0, 3), end(0, `{"$atlas":"end","rows":4,"truncated":false}`)),
		"an end line that is not JSON":  write(end(0, `{"$atlas":"end",`)),
	} {
		if _, _, err := readArrow(mem, body); code(err) != protocol.CodeInvalidDocument {
			t.Errorf("%s: %v", name, err)
		}
	}
}
