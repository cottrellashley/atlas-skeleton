package pipeline

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/arrowschema"
)

// batchStream is a BatchStream over batches made from rows, each handed out
// once, for its reader to release.
type batchStream struct {
	schema  *arrow.Schema
	batches []arrow.RecordBatch
	closed  bool
}

func (s *batchStream) Schema() *arrow.Schema { return s.schema }

func (s *batchStream) Next(ctx context.Context) (arrow.RecordBatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(s.batches) == 0 {
		return nil, io.EOF
	}
	b := s.batches[0]
	s.batches = s.batches[1:]
	return b, nil
}

func (s *batchStream) Close() error {
	for _, b := range s.batches {
		b.Release()
	}
	s.batches, s.closed = nil, true
	return nil
}

// listBatches answers portfolio.list as Arrow batches of these codes, one
// batch a group, in the writer schema the contract derives (§9.2.7), with
// extra columns added when asked.
func listBatches(t *testing.T, mem memory.Allocator, extra bool, groups ...[]string) func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
	return func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		p := loadProfile(t, "svc/portfolio/profiles/portfolio.list@v1.json")
		row := p.Schemas["result"].Items()
		fields, err := arrowschema.Fields(row)
		if err != nil {
			return nil, err
		}
		sch := arrow.NewSchema(fields, nil)
		s := &batchStream{schema: sch}
		for _, codes := range groups {
			b, err := arrowschema.ToBatch(mem, sch, row, listRows(codes...))
			if err != nil {
				return nil, err
			}
			if extra {
				leak := array.NewStringBuilder(mem)
				for range codes {
					leak.Append("GLOBAL-CREDIT")
				}
				col := leak.NewArray()
				leak.Release()
				wide := arrow.NewSchema(append(append([]arrow.Field{}, fields...), arrow.Field{Name: "sister_portfolio", Type: arrow.BinaryTypes.String}), nil)
				cols := append(append([]arrow.Array{}, b.Columns()...), col)
				w := array.NewRecordBatch(wide, cols, b.NumRows())
				b.Release()
				col.Release()
				b = w
				s.schema = wide
			}
			s.batches = append(s.batches, b)
		}
		return &ports.Answer{Batches: s}, nil
	}
}

// R05, R11 (Arrow is an optimisation of the same meaning): an answer of
// Arrow batches is held to the caller's scope exactly as rows are, read a
// row at a time or a batch at a time: a portfolio outside it ends the
// answer, and nothing of its batch is sent.
func TestR05BatchesAreHeldToTheScope(t *testing.T) {
	for _, byBatch := range []bool{false, true} {
		mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
		r := newRig(t, serve(listBatches(t, mem, false, []string{"147HYC"}, []string{"147HYC", "GLOBAL-CREDIT"})), Options{})
		c := r.call("t-bob", listAt, `{}`)
		res, err := r.run(c)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Rows.Batched() {
			t.Fatal("an answer of batches says so")
		}
		sent, err := read(res, byBatch)
		if code(err) != protocol.CodeInternal || sent != 1 {
			t.Errorf("by batch %v: %d rows sent, %v", byBatch, sent, err)
		}
		if out := r.facts(c.RequestID, ports.Outcome); len(out) != 1 || out[0].Code != protocol.CodeInternal || out[0].Step != ports.StepAnswer {
			t.Errorf("by batch %v: %+v", byBatch, out)
		}
		mem.AssertSize(t, 0)
	}
}

// read reads an answer to its end, by rows or by batches, releasing every
// batch: how many rows it was sent, and how it ended.
func read(res *Result, byBatch bool) (int64, error) {
	defer res.Close()
	var n int64
	for {
		if !byBatch {
			_, err := res.Rows.Next()
			if err != nil {
				return n, ends(err)
			}
			n++
			continue
		}
		b, err := res.Rows.NextBatch()
		if err != nil {
			return n, ends(err)
		}
		n += b.NumRows()
		b.Release()
	}
}

func ends(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// R105, R126, R106 over batches: the cap cuts a batch at the exact row, an
// answer of exactly the cap is whole, and a column the contract does not
// declare never leaves (R106).
func TestR05TheCapAndTheContractHoldOverBatches(t *testing.T) {
	for _, c := range []struct {
		name      string
		groups    [][]string
		cap       int
		extra     bool
		sent      int64
		truncated bool
	}{
		{name: "exactly the cap", groups: [][]string{{"147HYC", "GLOBAL-CREDIT"}, {"EM-RATES"}}, cap: 3, sent: 3},
		{name: "one past the cap", groups: [][]string{{"147HYC", "GLOBAL-CREDIT"}, {"EM-RATES", "EURO-IG"}}, cap: 3, sent: 3, truncated: true},
		{name: "the cap at a batch's end", groups: [][]string{{"147HYC", "GLOBAL-CREDIT"}, {}, {"EM-RATES"}}, cap: 2, sent: 2, truncated: true},
		{name: "an undeclared column", groups: [][]string{{"147HYC", "EURO-IG"}}, cap: 10, extra: true, sent: 2},
	} {
		for _, byBatch := range []bool{false, true} {
			mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
			r := newRig(t, serve(listBatches(t, mem, c.extra, c.groups...)), Options{})
			withLimit(r, ports.Limit{Subject: "alice", Capability: "portfolio.list@v1", MaxRows: c.cap})
			res, err := r.run(r.call("t-alice", listAt, `{}`))
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if byBatch {
				if want, _ := arrowschema.Fields(res.Writer.Schemas["result"].Items()); len(res.Rows.Schema().Fields()) != len(want) {
					t.Errorf("%s: the batches' schema is the contract's: %v", c.name, res.Rows.Schema())
				}
			}
			names, err := first(res, byBatch)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			for _, n := range names {
				if n == "sister_portfolio" {
					t.Errorf("%s, by batch %v: an undeclared column left", c.name, byBatch)
				}
			}
			if _, err := read(res, byBatch); err != nil {
				t.Errorf("%s: %v", c.name, err)
			}
			if res.Rows.Count() != c.sent || res.Rows.Truncated() != c.truncated {
				t.Errorf("%s, by batch %v: %d rows, truncated %v", c.name, byBatch, res.Rows.Count(), res.Rows.Truncated())
			}
			mem.AssertSize(t, 0)
		}
	}
}

// first reads an answer's first row, or batch: the names of its members,
// or of its columns.
func first(res *Result, byBatch bool) ([]string, error) {
	if !byBatch {
		row, err := res.Rows.Next()
		if err != nil {
			return nil, err
		}
		return row.Names(), nil
	}
	b, err := res.Rows.NextBatch()
	if err != nil {
		return nil, err
	}
	defer b.Release()
	var names []string
	for _, f := range b.Schema().Fields() {
		names = append(names, f.Name)
	}
	return names, nil
}

// R05: a batch whose declared column is of another type than the contract
// derives is the provider's fault, refused before anything is sent.
func TestR05ABatchOfAnotherShapeIsRefused(t *testing.T) {
	r := newRig(t, serve(func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		sch := arrow.NewSchema([]arrow.Field{{Name: "code", Type: arrow.PrimitiveTypes.Int64}}, nil)
		return &ports.Answer{Batches: &batchStream{schema: sch}}, nil
	}), Options{})
	if _, err := r.run(r.call("t-alice", listAt, `{}`)); code(err) != protocol.CodeInternal {
		t.Fatalf("%v", err)
	}
}
