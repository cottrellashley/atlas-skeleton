package edge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"atlas.local/gateway/core/pipeline"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/middleware"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/arrowschema"
	"atlas.local/sdk/go/protocol/frame"
	"atlas.local/sdk/go/protocol/ijson"
)

// memBatches is a batch table in memory: a provider's ports.Answer.Batches
// as the pipeline hands them on.
type memBatches struct {
	schema    *arrow.Schema
	batches   []arrow.RecordBatch
	i         int
	n         int64
	truncated bool
	fail      error // after the last batch
}

func (m *memBatches) Schema() *arrow.Schema { return m.schema }

func (m *memBatches) Next() (arrow.RecordBatch, error) {
	if m.i >= len(m.batches) {
		if m.fail != nil {
			return nil, m.fail
		}
		return nil, io.EOF
	}
	b := m.batches[m.i]
	m.i++
	m.n += b.NumRows()
	b.Retain() // the receiver releases what it is given
	return b, nil
}

func (m *memBatches) Count() int64    { return m.n }
func (m *memBatches) Truncated() bool { return m.truncated }

// batchCase is a holdings result and its rows as Arrow batches of the
// sizes given, in the writer schema §9.2.7 derives.
func batchCase(t *testing.T, r *rig, sizes ...int) (*pipeline.Result, []*ijson.Object, func() *memBatches) {
	t.Helper()
	c := r.reg.Snapshot().Lookup("portfolio.holdings@v1")
	res := &pipeline.Result{Capability: c, Interaction: c.Current.Interactions["fetch"], Writer: c.Current, Deadline: r.clock.Now().Add(time.Minute)}
	table := c.Current.Schemas[res.Interaction.Returns]
	sch, err := arrowschema.Derive(table, c.Current.Exact().String(), protocol.Version)
	if err != nil {
		t.Fatal(err)
	}
	var rows []*ijson.Object
	var batches []arrow.RecordBatch
	mem := memory.NewGoAllocator()
	for i, n := range sizes {
		part := holdingRows([]string{"147HYC"}, n)
		for j, row := range part {
			row.Set("security_id", fmt.Sprintf("B%d-%06d", i, j))
		}
		b, err := arrowschema.ToBatch(mem, sch, table.Items(), part)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(b.Release)
		batches = append(batches, b)
		// The rows as the batch holds them: what every encoding must say.
		meant, err := arrowschema.ToRows(b, table.Items())
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, meant...)
	}
	return res, rows, func() *memBatches { return &memBatches{schema: sch, batches: batches} }
}

// write runs one of the edge's writers on a recorder, as ServeHTTP would.
func write(r *rig, f func(w http.ResponseWriter, req *http.Request)) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, holdingsAt, nil)
	x := &exchange{id: "req-1", start: r.clock.Now()}
	x.w = &writer{ResponseWriter: middleware.Wrap(rec), x: x, r: req}
	x.w.Header().Set(protocol.HeaderRequestID, x.id)
	req = req.WithContext(context.WithValue(req.Context(), exchangeKey{}, x))
	f(x.w, req)
	return rec
}

func canonical(t testing.TB, v ijson.Value) string {
	t.Helper()
	b, err := ijson.Canonical(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// memStream is a provider's answer as Arrow record batches
// (ports.BatchStream), in the writer schema §9.2.7 derives.
type memStream struct {
	schema  *arrow.Schema
	batches []arrow.RecordBatch
	i       int
}

func (m *memStream) Schema() *arrow.Schema { return m.schema }

func (m *memStream) Next(ctx context.Context) (arrow.RecordBatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.i >= len(m.batches) {
		return nil, io.EOF
	}
	b := m.batches[m.i]
	m.i++
	b.Retain()
	return b, nil
}

func (m *memStream) Close() error { return nil }

// The structural step of lane E, R11 ("Arrow is an optimisation of the same
// meaning"), §9.6.5, §9.2.7: a provider that answers Arrow record batches
// (ports.Answer.Batches) is streamed to an Arrow client as it is, batch
// for batch, ended by the final batch of zero rows whose atlas:end holds
// the end line; NDJSON and JSON read the same answer as rows, to the same
// records. Cut at the caller's cap, every encoding says so, or refuses.
func TestR11BatchesStreamAsTheyAre(t *testing.T) {
	r := newRig(t, rigOptions{limits: []ports.Limit{{Subject: "bob", Capability: "portfolio.holdings@v1", MaxRows: 7}}})
	_, rows, source := batchCase(t, r, 6, 4)
	r.provide(func(context.Context, *ports.Invocation) (*ports.Answer, error) {
		m := source()
		return &ports.Answer{Batches: &memStream{schema: m.schema, batches: m.batches}}, nil
	})
	body := `{"portfolios": ["147HYC"]}`

	a := r.call("t-alice", holdingsAt, body, "Accept", protocol.MediaArrowStream)
	if a.status != 200 || a.header.Get("Content-Type") != protocol.MediaArrowStream {
		t.Fatalf("%d %s", a.status, a.body)
	}
	rd, err := frame.NewArrowReader(bytes.NewReader(a.body), memory.NewGoAllocator())
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Release()
	if p, _ := rd.Schema().Metadata().GetValue(arrowschema.MetaProfile); p != r.reg.Snapshot().Lookup("portfolio.holdings@v1").Current.Exact().String() {
		t.Fatalf("the stream's schema: %v", rd.Schema().Metadata())
	}
	var sizes []int64
	var got []*ijson.Object
	row := r.reg.Snapshot().Lookup("portfolio.holdings@v1").Current.Schemas["result"].Items()
	for {
		b, err := rd.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, b.NumRows())
		part, err := arrowschema.ToRows(b, row)
		b.Release()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, part...)
	}
	if end := rd.End(); end == nil || end.Rows != 10 || end.Truncated || end.Problem != nil || len(sizes) != 2 || sizes[0] != 6 || sizes[1] != 4 {
		t.Fatalf("batches %v, end %+v", sizes, rd.End())
	}
	if canonical(t, toValues(got)) != canonical(t, toValues(rows)) {
		t.Fatal("the Arrow rows differ from the rows the batches were made of")
	}

	nd, end, err := ndjsonRows(t, r.call("t-alice", holdingsAt, body, "Accept", protocol.MediaNDJSON))
	if err != io.EOF || end.Rows != 10 || canonical(t, toValues(nd)) != canonical(t, toValues(rows)) {
		t.Fatalf("NDJSON from batches: %d rows, %+v, %v", len(nd), end, err)
	}
	j := r.call("t-alice", holdingsAt, body, "Accept", protocol.MediaJSON)
	v, err := ijson.Parse(j.body)
	if err != nil || j.header.Get("Content-Type") != protocol.MediaJSON || canonical(t, v) != canonical(t, toValues(rows)) {
		t.Fatalf("JSON from batches: %d %s", j.status, j.body)
	}

	// Bob is capped at 7 rows: every encoding says the answer was cut.
	n, end, err := arrowAnswer(t, r.call("t-bob", holdingsAt, body, "Accept", protocol.MediaArrowStream))
	if n != 7 || !end.Truncated || codeOf(err) != protocol.CodeOutputLimitExceeded {
		t.Fatalf("cut Arrow: %d, %+v, %v", n, end, err)
	}
	nd, end, err = ndjsonRows(t, r.call("t-bob", holdingsAt, body, "Accept", protocol.MediaNDJSON))
	if len(nd) != 7 || !end.Truncated || codeOf(err) != protocol.CodeOutputLimitExceeded {
		t.Fatalf("cut NDJSON: %d, %+v, %v", len(nd), end, err)
	}
	if p := r.call("t-bob", holdingsAt, body, "Accept", protocol.MediaJSON).problem(t); p.Code != protocol.CodeInvalidRequest || p.Details[0].Code != protocol.CodeOutputLimitExceeded {
		t.Fatalf("cut JSON: %+v", p)
	}
}

// R11 with PR04 and STREAM-003, at the edge's own seam: batches cut at the
// caller's cap end with truncated and output_limit_exceeded, and a failure
// after the first batch is the final batch's problem.
func TestR11BatchesCarryTheirEnd(t *testing.T) {
	r := newRig(t, rigOptions{noServer: true})
	res, _, source := batchCase(t, r, 6, 4)

	cutAt := source()
	cutAt.truncated = true
	rec := write(r, func(w http.ResponseWriter, req *http.Request) { r.edge.arrowBatches(w, req, res, cutAt) })
	n, end, err := arrowAnswer(t, answer{status: rec.Code, header: rec.Header(), body: rec.Body.Bytes()})
	if n != 10 || !end.Truncated || codeOf(err) != protocol.CodeOutputLimitExceeded {
		t.Fatalf("a cut Arrow answer: %d, %+v, %v", n, end, err)
	}
	failing := source()
	failing.fail = protocol.NewProblem(protocol.CodeUpstreamFailed, "the warehouse went away")
	rec = write(r, func(w http.ResponseWriter, req *http.Request) { r.edge.arrowBatches(w, req, res, failing) })
	n, end, err = arrowAnswer(t, answer{status: rec.Code, header: rec.Header(), body: rec.Body.Bytes()})
	if n != 10 || codeOf(err) != protocol.CodeUpstreamFailed || end.Problem.RequestID != "req-1" {
		t.Fatalf("a failed Arrow answer: %d, %+v, %v", n, end, err)
	}
}

func toValues(rows []*ijson.Object) []ijson.Value {
	out := make([]ijson.Value, len(rows))
	for i, r := range rows {
		out[i] = r
	}
	return out
}
