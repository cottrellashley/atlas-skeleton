// Package engine is atlas-engine's core: the runner protocol of §9.8.3 over
// the Go client, with the two additions the Python face needs (§5.1): Arrow
// results on a second pipe, and cancel.
//
// Commands arrive one JSON line at a time and run one at a time, in order;
// a cancel acts as it arrives. Every command gets one JSON answer line. A
// fetch that asks for "results": "arrow" is answered with a line naming its
// stream, and its batches follow on the results pipe as an Arrow IPC stream
// ended by the final zero-row batch (§9.6.5). A command cancelled before it
// answers is answered {"id": n, "ok": false, "cancelled": true}; one
// cancelled while its stream is written ends the stream without the final
// batch, which the face that cancelled it expects.
package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/sdk/go/client"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/problem"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// Command is one line on standard input. For cancel, ID names the command
// to cancel.
type Command struct {
	ID      int             `json:"id"`
	Op      string          `json:"op"`
	Args    json.RawMessage `json:"args,omitempty"`
	Options Options         `json:"options"`
}

// Options are how a command runs, never part of its request.
type Options struct {
	// Results is "arrow" for a table on the results pipe; otherwise a table
	// comes back as JSON row records, the form the conformance suite reads.
	Results string `json:"results,omitempty"`
	// Execution is a preference: gateway or handoff.
	Execution string `json:"execution,omitempty"`
	// Timeout is the command's deadline in seconds; the call carries it to
	// the gateway.
	Timeout float64 `json:"timeout,omitempty"`
}

// Answer is one line on standard output.
type Answer struct {
	ID        int               `json:"id"`
	OK        bool              `json:"ok"`
	Result    any               `json:"result,omitempty"`
	Problem   *protocol.Problem `json:"problem,omitempty"`
	Cancelled bool              `json:"cancelled,omitempty"`
}

// args holds every operation's arguments; each reads the members it needs.
type args struct {
	URL     string         `json:"url"`
	Token   string         `json:"token"`
	Lock    string         `json:"lock"`
	Words   string         `json:"words"`
	ID      string         `json:"id"`
	IDs     []string       `json:"ids"`
	Path    string         `json:"path"`
	Address string         `json:"address"`
	Request map[string]any `json:"request"`
}

// errCancelled is the cause of a command the face cancelled.
var errCancelled = errors.New("cancelled by the face")

// Engine runs commands against one client.
type Engine struct {
	results io.Writer
	mem     memory.Allocator
	c       *client.Client // only the worker touches it

	mu  sync.Mutex // guards out
	out *json.Encoder

	cmu     sync.Mutex
	cancels map[int]context.CancelCauseFunc
}

// New returns an engine answering on out, with table streams on results,
// which is nil when the engine has no results pipe.
func New(out, results io.Writer) *Engine {
	return &Engine{
		out: json.NewEncoder(out), results: results, mem: memory.NewGoAllocator(),
		cancels: map[int]context.CancelCauseFunc{},
	}
}

type job struct {
	cmd Command
	ctx context.Context
}

// Run reads commands until in closes, then finishes those it has.
func (e *Engine) Run(ctx context.Context, in io.Reader) error {
	queue := make(chan job, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for j := range queue {
			e.run(j)
		}
	}()
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	for sc.Scan() {
		var cmd Command
		if err := json.Unmarshal(sc.Bytes(), &cmd); err != nil {
			e.answer(Answer{Problem: protocol.NewProblem(protocol.CodeInvalidDocument, "a command is not JSON: "+err.Error())})
			continue
		}
		if cmd.Op == "cancel" {
			e.cmu.Lock()
			if cancel, ok := e.cancels[cmd.ID]; ok {
				cancel(errCancelled)
			}
			e.cmu.Unlock()
			continue
		}
		cctx, cancel := context.WithCancelCause(ctx)
		e.cmu.Lock()
		e.cancels[cmd.ID] = cancel
		e.cmu.Unlock()
		queue <- job{cmd, cctx}
	}
	close(queue)
	<-done
	return sc.Err()
}

func (e *Engine) run(j job) {
	defer func() {
		e.cmu.Lock()
		if cancel, ok := e.cancels[j.cmd.ID]; ok {
			cancel(nil)
			delete(e.cancels, j.cmd.ID)
		}
		e.cmu.Unlock()
	}()
	ctx := j.ctx
	if t := j.cmd.Options.Timeout; t > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(t*float64(time.Second)))
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		e.fail(ctx, j.cmd.ID, err) // cancelled while it waited
		return
	}
	e.handle(ctx, j.cmd)
}

func (e *Engine) answer(a Answer) {
	e.mu.Lock()
	defer e.mu.Unlock()
	_ = e.out.Encode(a)
}

// fail answers a command that did not succeed: cancelled, past its deadline,
// or with the problem the client gave.
func (e *Engine) fail(ctx context.Context, id int, err error) {
	if errors.Is(context.Cause(ctx), errCancelled) {
		e.answer(Answer{ID: id, Cancelled: true})
		return
	}
	e.answer(Answer{ID: id, Problem: problemOf(ctx, err)})
}

func problemOf(ctx context.Context, err error) *protocol.Problem {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return protocol.NewProblem(protocol.CodeDeadlineExceeded, "the call's deadline passed")
	}
	return problem.From(err)
}

func (e *Engine) reply(ctx context.Context, id int, v any, err error) {
	if err != nil {
		e.fail(ctx, id, err)
		return
	}
	e.answer(Answer{ID: id, OK: true, Result: v})
}

func (e *Engine) handle(ctx context.Context, cmd Command) {
	var a args
	if len(cmd.Args) > 0 {
		if err := json.Unmarshal(cmd.Args, &a); err != nil {
			e.fail(ctx, cmd.ID, protocol.NewProblem(protocol.CodeInvalidRequest, "the arguments of "+cmd.Op+": "+err.Error()))
			return
		}
	}
	switch cmd.Op {
	case "version":
		e.answer(Answer{ID: cmd.ID, OK: true, Result: map[string]any{
			"name": client.Name, "version": client.Version, "protocol": protocol.Version, "levels": client.Features.Levels,
		}})
		return
	case "connect":
		e.connect(ctx, cmd.ID, a)
		return
	}
	if e.c == nil {
		e.fail(ctx, cmd.ID, protocol.NewProblem(protocol.CodeInvalidRequest, "connect first"))
		return
	}
	switch cmd.Op {
	case "whoami":
		v, err := e.c.WhoAmI(ctx)
		e.reply(ctx, cmd.ID, v, err)
	case "catalog":
		v, err := e.c.Catalog(ctx)
		e.reply(ctx, cmd.ID, v, err)
	case "search":
		v, err := e.c.Search(ctx, a.Words)
		e.reply(ctx, cmd.ID, v, err)
	case "describe":
		v, err := e.c.Describe(ctx, a.ID)
		e.reply(ctx, cmd.ID, v, err)
	case "lock":
		l, err := e.c.LockCurrent(ctx, a.IDs, a.Path)
		if err != nil {
			e.fail(ctx, cmd.ID, err)
			return
		}
		e.answer(Answer{ID: cmd.ID, OK: true, Result: l.Capabilities})
	case "call":
		v, err := e.c.Call(ctx, a.Address, a.Request)
		e.reply(ctx, cmd.ID, v, err)
	case "fetch":
		e.fetch(ctx, cmd, a)
	default:
		e.fail(ctx, cmd.ID, protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("the engine has no operation %q", cmd.Op)))
	}
}

// connect signs in with the token given or ATLAS_TOKEN, and reads the lock:
// the one named, or atlas.lock in the working directory or a parent.
func (e *Engine) connect(ctx context.Context, id int, a args) {
	var lock *client.Lock
	var err error
	if a.Lock != "" {
		if lock, err = client.LoadLock(a.Lock); errors.Is(err, fs.ErrNotExist) {
			lock, err = nil, nil
		}
	} else if wd, werr := os.Getwd(); werr == nil {
		lock, err = client.FindLock(wd)
	}
	if err != nil {
		e.fail(ctx, id, err)
		return
	}
	c, err := client.Connect(ctx, client.Options{URL: a.URL, Token: a.Token, Lock: lock, Mem: e.mem})
	if err != nil {
		e.fail(ctx, id, err)
		return
	}
	e.c = c
	b := c.Bootstrap()
	result := map[string]any{"protocol": b.Protocol, "environment": b.Environment}
	if lock != nil {
		result["lock"] = lock.Path
	}
	e.answer(Answer{ID: id, OK: true, Result: result})
}

func (e *Engine) fetch(ctx context.Context, cmd Command, a args) {
	if cmd.Options.Results == "arrow" && e.results == nil {
		e.fail(ctx, cmd.ID, protocol.NewProblem(protocol.CodeInvalidRequest, "the engine was started without a results pipe"))
		return
	}
	tbl, err := e.c.Fetch(ctx, a.Address, a.Request, client.CallOptions{Execution: cmd.Options.Execution})
	if err != nil {
		e.fail(ctx, cmd.ID, err)
		return
	}
	defer tbl.Release()
	result := map[string]any{"writer": tbl.Writer, "reader": tbl.Reader, "via": tbl.Execution}
	if cmd.Options.Results != "arrow" {
		var rows []map[string]any
		for tbl.Next() {
			rec := tbl.RecordBatch()
			for i := range int(rec.NumRows()) {
				r, err := protocol.RowRecord(rec, i)
				if err != nil {
					e.fail(ctx, cmd.ID, err)
					return
				}
				rows = append(rows, r)
			}
		}
		if err := tbl.Err(); err != nil {
			e.fail(ctx, cmd.ID, err)
			return
		}
		result["rows"] = rows
		e.answer(Answer{ID: cmd.ID, OK: true, Result: result})
		return
	}
	result["stream"] = cmd.ID
	e.answer(Answer{ID: cmd.ID, OK: true, Result: result})
	e.stream(ctx, tbl)
}

// stream writes the table on the results pipe: its batches, then the final
// batch carrying the end line, as the gateway writes an Arrow answer.
func (e *Engine) stream(ctx context.Context, tbl *client.Table) {
	w := ipc.NewWriter(e.results, ipc.WithSchema(tbl.Schema()), ipc.WithAllocator(e.mem))
	defer w.Close() // the end-of-stream marker, always: the face never waits on a stream
	var rows int64
	var err error
	for err == nil && tbl.Next() {
		rec := tbl.RecordBatch()
		if err = w.Write(rec); err == nil {
			rows += rec.NumRows()
		}
	}
	if err == nil {
		err = tbl.Err()
	}
	if err != nil && errors.Is(context.Cause(ctx), errCancelled) {
		return
	}
	end := protocol.EndLine{Rows: rows}
	if err != nil {
		end.Problem = problemOf(ctx, err)
	}
	final, ferr := protocol.EndBatch(e.mem, tbl.Schema(), end)
	if ferr != nil {
		return
	}
	defer final.Release()
	_ = w.Write(final)
}
