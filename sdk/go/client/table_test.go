package client

import (
	"bytes"
	"io"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

func TestAnArrowAnswerIsCompleteOnlyWithItsFinalBatch(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)
	schema := arrow.NewSchema([]arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, nil)
	for _, c := range []struct {
		name string
		end  *protocol.EndLine
		code string // "" when the answer is complete
	}{
		{"complete", &protocol.EndLine{Rows: 3}, ""},
		{"no final batch", nil, protocol.CodeUpstreamFailed},
		{"rows lost on the way", &protocol.EndLine{Rows: 4}, protocol.CodeUpstreamFailed},
		{"a problem after the rows", &protocol.EndLine{Rows: 3, Problem: protocol.NewProblem(protocol.CodeDeadlineExceeded, "")}, protocol.CodeDeadlineExceeded},
	} {
		t.Run(c.name, func(t *testing.T) {
			ipcr, err := ipc.NewReader(bytes.NewReader(answer(t, mem, schema, c.end)), ipc.WithAllocator(mem))
			if err != nil {
				t.Fatal(err)
			}
			a := &arrowAnswer{ipcr: ipcr, body: io.NopCloser(nil)}
			defer a.Release()
			var rows int64
			for a.Next() {
				rows += a.RecordBatch().NumRows()
			}
			got := ""
			if p, ok := a.Err().(*protocol.Problem); ok {
				got = p.Code
			} else if a.Err() != nil {
				t.Fatalf("not a problem: %v", a.Err())
			}
			if got != c.code || rows != 3 {
				t.Fatalf("got %q after %d rows, want %q", got, rows, c.code)
			}
		})
	}
}

// answer writes three rows, then the final batch carrying end unless it is nil.
func answer(t *testing.T, mem memory.Allocator, schema *arrow.Schema, end *protocol.EndLine) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema), ipc.WithAllocator(mem))
	b := array.NewInt64Builder(mem)
	defer b.Release()
	b.AppendValues([]int64{1, 2, 3}, nil)
	col := b.NewArray()
	defer col.Release()
	rec := array.NewRecordBatch(schema, []arrow.Array{col}, 3)
	defer rec.Release()
	if err := w.Write(rec); err != nil {
		t.Fatal(err)
	}
	if end != nil {
		final, err := protocol.EndBatch(mem, schema, *end)
		if err != nil {
			t.Fatal(err)
		}
		defer final.Release()
		if err := w.Write(final); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
