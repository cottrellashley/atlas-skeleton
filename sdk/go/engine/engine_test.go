package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"

	atlas "github.com/jhgonline/fi-app-atlas/sdk/go"
	"github.com/jhgonline/fi-app-atlas/sdk/go/engine/fakegw"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/conc"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/frame"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// Every test here runs in a testing/synctest bubble, which fails when a
// goroutine is left blocked at its end (nothing leaks) and when every
// goroutine blocks (a deadlock is a failure, never a hang). The pipes are
// io.Pipe, with no buffer at all: an engine that writes a stream before
// the face reads it blocks at its first byte.

type line struct {
	ID        int64             `json:"id"`
	OK        bool              `json:"ok"`
	Value     json.RawMessage   `json:"value"`
	Problem   *protocol.Problem `json:"problem"`
	Warnings  []atlas.Warning   `json:"warnings"`
	Calls     int               `json:"calls"`
	Usage     json.RawMessage   `json:"usage"`
	Cancelled bool              `json:"cancelled"`
	Truncated bool              `json:"truncated"`
	raw       string
}

type face struct {
	t       *testing.T
	in      *io.PipeWriter
	results *io.PipeReader
	answers chan line
	run     func() error
	closed  bool
}

func isolate(t *testing.T) {
	for _, k := range []string{"ATLAS_URL", "ATLAS_ENV", "ATLAS_TOKEN", "ATLAS_HOME"} {
		t.Setenv(k, "")
	}
}

func home(t *testing.T) string {
	h := t.TempDir()
	if err := atlas.SignIn(h, "conformance", "ct-valid", "rt-valid", time.Hour); err != nil {
		t.Fatal(err)
	}
	return h
}

// start runs an engine against gw, as a face would: its commands on a
// pipe, its answers read as they come, and its results pipe when arrow.
func start(t *testing.T, gw *fakegw.Gateway, arrow bool, edit ...func(*atlas.Options)) *face {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	f := &face{t: t, in: inW, answers: make(chan line, MaxPending*2)}
	var results io.Writer
	if arrow {
		var w *io.PipeWriter
		f.results, w = io.Pipe()
		results = w
		t.Cleanup(func() { w.Close(); f.results.Close() })
	}
	opt := atlas.Options{URL: gw.URL, Home: home(t), HTTP: gw.Client()}
	for _, ed := range edit {
		ed(&opt)
	}
	e := New(opt, outW, results)
	run := conc.Go(func() error {
		defer outW.Close()
		return e.Run(context.Background(), inR)
	})
	read := conc.Go(func() error {
		defer close(f.answers)
		sc := bufio.NewScanner(outR)
		sc.Buffer(nil, MaxLine)
		for sc.Scan() {
			var l line
			if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
				return fmt.Errorf("the engine wrote a line that is not JSON: %q", sc.Text())
			}
			l.raw = sc.Text()
			f.answers <- l
		}
		return sc.Err()
	})
	f.run = func() error {
		if !f.closed {
			f.closed = true
			inW.Close()
		}
		err := run()
		if rerr := read(); rerr != nil && err == nil {
			err = rerr
		}
		return err
	}
	t.Cleanup(func() {
		if err := f.run(); err != nil {
			t.Errorf("Run: %v", err)
		}
	})
	return f
}

func (f *face) send(cmd string) {
	f.t.Helper()
	if _, err := io.WriteString(f.in, cmd+"\n"); err != nil {
		f.t.Fatal(err)
	}
}

func (f *face) next() line {
	f.t.Helper()
	l, ok := <-f.answers
	if !ok {
		f.t.Fatal("the engine wrote no more answers")
	}
	return l
}

// rows reads one Arrow stream from the results pipe: its rows and its end.
func (f *face) rows() ([]*ijson.Object, *frame.End, []string, error) {
	rd, err := frame.NewArrowReader(f.results, memory.NewGoAllocator())
	if err != nil {
		return nil, nil, nil, err
	}
	defer rd.Release()
	var names []string
	for _, fl := range rd.Schema().Fields() {
		names = append(names, fl.Name)
	}
	var out []*ijson.Object
	for {
		b, err := rd.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			}
			return out, rd.End(), names, err
		}
		for i := 0; i < int(b.NumRows()); i++ {
			o := ijson.NewObject()
			for j, fl := range b.Schema().Fields() {
				o.Set(fl.Name, b.Column(j).ValueStr(i))
			}
			out = append(out, o)
		}
		b.Release()
	}
}

// PR03, R003, R005, §5.1: the answer that names a stream comes first and
// the batches follow, so a table far past any pipe's capacity streams to a
// face that reads its answer before its results.
func TestPR03AnArrowTableOfAnySizeStreams(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		f := start(t, gw, true)
		f.send(`{"id": 1, "op": "call", "results": "arrow", "args": {"address": "conformance.rows@v1:fetch", "request": {"n": 20000}}}`)
		a := f.next()
		if !a.OK || !strings.Contains(string(a.Value), `"stream":"arrow"`) {
			t.Fatalf("the first answer does not name the stream: %s", a.raw)
		}
		rows, end, _, err := f.rows()
		if err != nil || len(rows) != 20000 || end == nil || end.Rows != 20000 || end.Problem != nil {
			t.Fatalf("%d rows, end %+v, %v", len(rows), end, err)
		}
		if got, _ := rows[19999].Get("label"); got != "row-19999" {
			t.Fatalf("the last row: %v", rows[19999])
		}
	})
}

// R005, §9.7.13: batches leave as rows arrive, a batch at a time, never
// after the whole table is held: the face has its first batch while the
// gateway is still sending.
func TestR005BatchesLeaveAsRowsArrive(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		more := make(chan struct{})
		gw.Handle("conformance.rows@v1:fetch", func(c *fakegw.Call) {
			c.W.Header().Set("Content-Type", protocol.MediaNDJSON)
			c.W.WriteHeader(http.StatusOK)
			nw := frame.NewNDJSONWriter(c.W)
			for i := int64(0); i < 2*batchRows; i++ {
				if i == batchRows+10 {
					_ = nw.Flush()
					c.W.(http.Flusher).Flush()
					<-more // the rest only once the face has the first batch
				}
				_ = nw.Row(fakegw.Row(i))
			}
			_ = nw.End(frame.End{})
		})
		f := start(t, gw, true)
		f.send(`{"id": 1, "op": "call", "results": "arrow", "args": {"address": "conformance.rows@v1:fetch", "request": {"n": 8192}}}`)
		if a := f.next(); !a.OK {
			t.Fatalf("%s", a.raw)
		}
		rd, err := frame.NewArrowReader(f.results, memory.NewGoAllocator())
		if err != nil {
			t.Fatal(err)
		}
		defer rd.Release()
		b, err := rd.Next()
		if err != nil || b.NumRows() != batchRows {
			t.Fatalf("the first batch: %v rows, %v", b, err)
		}
		b.Release()
		close(more)
		total := int64(batchRows)
		for {
			b, err := rd.Next()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					t.Fatal(err)
				}
				break
			}
			total += b.NumRows()
			b.Release()
		}
		if total != 2*batchRows {
			t.Fatalf("%d rows", total)
		}
	})
}

// R030: the Arrow table is in the reader's schema, the rows resolved into
// it, under a gateway that writes a newer profile.
func TestR030ArrowFollowsTheReader(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		gw.Current("conformance.evolving@v1#bc7fde7c0bac077d")
		gw.WriteUnder("conformance.evolving@v1:fetch", "conformance.evolving@v1#5f4ce7e7661ebd4c")
		gw.Handle("conformance.evolving@v1:fetch", func(c *fakegw.Call) {
			c.Rows(1, func(int64) *ijson.Object {
				v, _ := ijson.Parse([]byte(`{"id": "p0", "market_value": 100.5, "weight": 0.25, "currency": "USD", "status": "open", "count": 3}`))
				return v.(*ijson.Object)
			}, frame.End{})
		})
		f := start(t, gw, true)
		f.send(`{"id": 1, "op": "call", "results": "arrow", "args": {"address": "conformance.evolving@v1:fetch", "request": {"ids": ["p0"]}}}`)
		if a := f.next(); !a.OK {
			t.Fatalf("%s", a.raw)
		}
		rows, end, names, err := f.rows()
		if err != nil || end == nil || len(rows) != 1 {
			t.Fatalf("%v %v %v", rows, end, err)
		}
		if strings.Join(names, ",") != "id,mv,weight,status,count,legacy" {
			t.Fatalf("the Arrow table's columns are %v, not the reader's", names)
		}
		if mv, _ := rows[0].Get("mv"); mv != "100.5" {
			t.Fatalf("mv %v", mv)
		}
	})
}

// R032, R063: a catalogue answer without items is that command's
// invalid_document; the next command succeeds, and Run ends cleanly.
func TestR032ABadAnswerFailsOneCommand(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		gw.Handle("atlas.catalog:fetch", func(c *fakegw.Call) {
			v, _ := ijson.Parse([]byte(`{"next_cursor": null}`))
			c.JSON(v)
		})
		f := start(t, gw, false)
		f.send(`{"id": 1, "op": "catalog", "args": {}}`)
		if a := f.next(); a.ID != 1 || a.OK || a.Problem == nil || a.Problem.Code != "invalid_document" {
			t.Fatalf("catalog: %s", a.raw)
		}
		f.send(`{"id": 2, "op": "whoami", "args": {}}`)
		if a := f.next(); a.ID != 2 || !a.OK {
			t.Fatalf("whoami after it: %s", a.raw)
		}
		if err := f.run(); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}

// R032, R063: a command that panics (here the caller's warning hook) is
// answered internal, and no other command is touched.
func TestR063APanicIsOneCommandsInternal(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		gw.Handle("conformance.echo@v1:fetch", func(c *fakegw.Call) {
			c.W.Header().Set("Deprecation", protocol.FormatDeprecation(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
			c.JSON(fakegw.Echo(c.Body))
		})
		f := start(t, gw, false, func(o *atlas.Options) { o.Warn = func(atlas.Warning) { panic("a hook that fails") } })
		f.send(`{"id": 1, "op": "call", "args": {"address": "conformance.echo@v1:fetch", "request": {"text": "x"}}}`)
		if a := f.next(); a.ID != 1 || a.OK || a.Problem.Code != "internal" || !strings.Contains(a.Problem.Detail, "a hook that fails") {
			t.Fatalf("the panicking command: %s", a.raw)
		}
		f.send(`{"id": 2, "op": "whoami", "args": {}}`)
		if a := f.next(); a.ID != 2 || !a.OK {
			t.Fatalf("whoami after it: %s", a.raw)
		}
	})
}

// R063, ATLAS-PY-007: when the engine's input ends, every command still
// running is cancelled and answered, the gateway's request ends, and Run
// returns.
func TestR063TheEndOfInputCancelsEverything(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		ended := make(chan struct{})
		gw.Handle("atlas.whoami:fetch", func(c *fakegw.Call) {
			<-c.R.Context().Done()
			close(ended)
		})
		f := start(t, gw, false)
		f.send(`{"id": 1, "op": "whoami", "args": {}}`)
		synctest.Wait()
		if err := f.run(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		a := f.next()
		if a.ID != 1 || !a.Cancelled || a.Problem.Code != "deadline_exceeded" {
			t.Fatalf("the running command: %s", a.raw)
		}
		<-ended
	})
}

// R094, ATLAS-PY-005: with every worker busy, the engine still reads its
// input, so a cancel ends its command at once; past MaxPending, a command
// is answered unavailable at once.
func TestR094ACancelIsReadWhileEveryWorkerIsBusy(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		gw.Handle("conformance.echo@v1:fetch", func(c *fakegw.Call) { c.Hold(release) })
		f := start(t, gw, false)
		f.send(`{"id": 100, "op": "whoami", "args": {}}`) // connect first, so every call below is in flight
		if a := f.next(); !a.OK {
			t.Fatal(a.raw)
		}
		for id := 1; id <= MaxPending; id++ {
			f.send(fmt.Sprintf(`{"id": %d, "op": "call", "args": {"address": "conformance.echo@v1:fetch", "request": {"text": "x"}}}`, id))
		}
		f.send(fmt.Sprintf(`{"id": %d, "op": "whoami", "args": {}}`, MaxPending+1))
		if a := f.next(); a.ID != MaxPending+1 || a.Problem == nil || a.Problem.Code != "unavailable" {
			t.Fatalf("past MaxPending: %s", a.raw)
		}
		synctest.Wait()
		start := time.Now()
		f.send(`{"op": "cancel", "id": 1}`)
		a := f.next()
		if a.ID != 1 || !a.Cancelled || time.Since(start) != 0 {
			t.Fatalf("the cancelled command: %s, after %v", a.raw, time.Since(start))
		}
		f.send(fmt.Sprintf(`{"op": "cancel", "id": %d}`, MaxPending)) // one still waiting for a worker
		if a := f.next(); a.ID != MaxPending || !a.Cancelled {
			t.Fatalf("the waiting command: %s", a.raw)
		}
		if n := len(gw.To("conformance.echo@v1:fetch")); n != MaxRunning {
			t.Fatalf("%d calls ran at once, want %d", n, MaxRunning)
		}
	})
}

// R026: a line that is not JSON (a NaN in its request) is answered under
// its own id, so the face that sent it is not left waiting.
func TestR026ALineThatIsNotJSONIsAnsweredByItsID(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		f := start(t, fakegw.New(t, fakegw.Options{}), false)
		f.send(`{"id": 5, "op": "call", "args": {"address": "conformance.echo@v1:fetch", "request": {"ratio": NaN}}}`)
		if a := f.next(); a.ID != 5 || a.OK || a.Problem.Code != "invalid_request" {
			t.Fatalf("%s", a.raw)
		}
		f.send(`{"id": 6, "op": "whoami", "args": {}}`)
		if a := f.next(); a.ID != 6 || !a.OK {
			t.Fatalf("%s", a.raw)
		}
	})
}

// R061, R026: a call is bounded by its deadline_ms in the engine: a held
// call answers deadline_exceeded at its deadline, not as a cancellation.
func TestR061TheDeadlineEndsAHeldCall(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		gw.Handle("conformance.echo@v1:fetch", func(c *fakegw.Call) { c.Hold(release) })
		f := start(t, gw, false)
		start := time.Now()
		f.send(`{"id": 1, "op": "call", "args": {"address": "conformance.echo@v1:fetch", "request": {"text": "x"}, "options": {"deadline_ms": 1000}}}`)
		a := f.next()
		if a.OK || a.Problem.Code != "deadline_exceeded" || a.Cancelled || time.Since(start) != time.Second {
			t.Fatalf("%s after %v", a.raw, time.Since(start))
		}
	})
}

// §9.8.3, Q-RUN-5, Q-RUN-6, STREAM-003, STREAM-004: the runner protocol's
// lines: version; a record with warnings [] and calls; a table's rows in
// value with its usage; a truncated table's warning; a table failing after
// its first rows answers the rows read with the problem.
func TestTheRunnerProtocol(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		gw.Handle("conformance.rows@v1:fetch", func(c *fakegw.Call) {
			n, _ := c.Body.Get("n")
			switch n {
			case ijson.Number("2"):
				p := protocol.NewProblem(protocol.CodeOutputLimitExceeded, "max_rows is 2")
				p.Next, p.RequestID = "Ask for fewer.", "r1"
				c.Rows(2, fakegw.Row, frame.End{Truncated: true, Usage: []byte(`{"rows":2}`), Problem: p})
			case ijson.Number("3"):
				p := protocol.NewProblem(protocol.CodeUpstreamFailed, "the warehouse went away")
				p.Next, p.RequestID = "Try again.", "r2"
				c.Rows(3, fakegw.Row, frame.End{Usage: []byte(`{}`), Problem: p})
			default:
				c.Rows(1, fakegw.Row, frame.End{Usage: []byte(`{"bytes":10}`)})
			}
		})
		f := start(t, gw, false)
		f.send(`{"id": 1, "op": "version", "args": {}}`)
		if a := f.next(); !a.OK || !strings.Contains(string(a.Value), `"protocol":"atlas/1.0"`) {
			t.Fatalf("version: %s", a.raw)
		}
		f.send(`{"id": 2, "op": "call", "args": {"address": "conformance.echo@v1:fetch", "request": {"text": "x"}}}`)
		if a := f.next(); !a.OK || a.Calls != 1 || a.Warnings == nil || !strings.Contains(a.raw, `"warnings":[]`) || string(a.Value) != `{"text":"x","count":1,"ratio":null,"when":null,"kind":"plain","tags":[]}` {
			t.Fatalf("call: %s", a.raw)
		}
		f.send(`{"id": 3, "op": "call", "args": {"address": "conformance.rows@v1:fetch", "request": {"n": 1}}}`)
		if a := f.next(); !a.OK || string(a.Usage) != `{"bytes":10}` || !strings.HasPrefix(string(a.Value), `[{"i":0,`) {
			t.Fatalf("table: %s", a.raw)
		}
		f.send(`{"id": 4, "op": "call", "args": {"address": "conformance.rows@v1:fetch", "request": {"n": 2}}}`)
		if a := f.next(); !a.OK || !a.Truncated || len(a.Warnings) != 1 || a.Warnings[0].Code != "output_limit_exceeded" {
			t.Fatalf("truncated: %s", a.raw)
		}
		f.send(`{"id": 5, "op": "call", "args": {"address": "conformance.rows@v1:fetch", "request": {"n": 3}}}`)
		a := f.next()
		var rows []json.RawMessage
		_ = json.Unmarshal(a.Value, &rows)
		if a.OK || a.Problem.Code != "upstream_failed" || len(rows) != 3 {
			t.Fatalf("failed after its rows: %s", a.raw)
		}
	})
}

// §9.8.3, Q-RUN-1, §9.7.1: fetch and explain are call(id:fetch) and
// call(id:explain), by a bare id or a pinned one.
func TestFetchAndExplainAreCalls(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		gw.Handle("conformance.rows@v1:explain", func(c *fakegw.Call) {
			v, _ := ijson.Parse([]byte(`{"sql": "select 1"}`))
			c.JSON(v)
		})
		f := start(t, gw, false)
		f.send(`{"id": 1, "op": "fetch", "args": {"id": "conformance.rows", "request": {"n": 2}}}`)
		if a := f.next(); !a.OK || !strings.HasPrefix(string(a.Value), `[{"i":0,`) {
			t.Fatalf("fetch: %s", a.raw)
		}
		f.send(`{"id": 2, "op": "explain", "args": {"id": "conformance.rows@v1", "request": {"n": 2}}}`)
		if a := f.next(); !a.OK || string(a.Value) != `{"sql":"select 1"}` {
			t.Fatalf("explain: %s", a.raw)
		}
	})
}

// AGENTS.md: the line reader is fuzzed: lineID never panics, and finds a
// leading id in a line that is not JSON.
func FuzzLineID(f *testing.F) {
	for _, seed := range []string{`{"id": 5, "x": NaN}`, `{"op": "x", "id": 7}`, `[1]`, `{"id": "5"}`, `{"id": 1e99}`, ``} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		id := lineID(b)
		var c Command
		if json.Unmarshal(b, &c) == nil && id != c.ID {
			t.Fatalf("lineID %d, the line's id %d", id, c.ID)
		}
	})
}
