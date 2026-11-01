// Package engine is the runner protocol (§9.8.3) over the Go client: one
// JSON command a line in, one JSON answer a line out. It is the
// conformance suite's runner for the Go client, and the engine under the
// Python SDK, which adds two things for its face (§5.1): a table can come
// back as an Arrow IPC stream on a second pipe, and cancel stops a
// command's work. The engine adds no logic: it translates commands into
// calls on the client and results into lines.
//
// It is built to outlive nothing and to wedge on nothing. Commands run at
// most MaxRunning at once and are held at most MaxPending, while the input
// is always read, so a cancel line is acted on at once (ATLAS-PY-005). A
// command that panics is answered internal and harms no other. When its
// input ends, every command still running is cancelled (ATLAS-PY-007). A
// table is streamed as its rows arrive, the answer that names a stream
// first and its batches after (§5.1), so a result of any size flows.
package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	atlas "atlas.local/sdk/go"
	"atlas.local/sdk/go/kit/conc"
	"atlas.local/sdk/go/kit/problem"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/arrowschema"
	"atlas.local/sdk/go/protocol/frame"
	"atlas.local/sdk/go/protocol/ijson"
)

// Name is the engine's client name in Atlas-Client.
const Name = "atlas-engine"

// The engine's bounds.
const (
	// MaxRunning is how many commands run at once.
	MaxRunning = 8
	// MaxPending is how many commands it holds, running or waiting; one
	// more is answered unavailable at once.
	MaxPending = 64
	// MaxConnections is how many connections it keeps.
	MaxConnections = 64
	// MaxLine is the longest command line it reads.
	MaxLine = 64 << 20
	// batchRows is the rows of one Arrow batch.
	batchRows = 4096
	// writeGrace is how long a cancelled stream may take to write its
	// final batch to a face that is not reading, before the write is cut.
	writeGrace = time.Second
)

// Command is one line in.
type Command struct {
	ID   int64           `json:"id"`
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args"`
	// Results "arrow" asks for a table as an Arrow IPC stream on the
	// results pipe (§5.1); args.options.arrow is the same ask.
	Results string `json:"results"`
}

// Answer is one line out. Warnings are always written, [] when there are
// none (§9.8.3).
type Answer struct {
	ID       int64             `json:"id"`
	OK       bool              `json:"ok"`
	Value    any               `json:"value,omitempty"`
	Problem  *protocol.Problem `json:"problem,omitempty"`
	Warnings []atlas.Warning   `json:"warnings"`
	Calls    int               `json:"calls,omitempty"`
	// Decimals are the members of each row (or of the record) that hold
	// exact decimals, as paths: quantity, market_value.amount. A face
	// turns them into its exact type, Python's Decimal (ATLAS-CORE §6).
	Decimals []string `json:"decimals,omitempty"`
	// Profile is the exact id the answer was written under.
	Profile string `json:"profile,omitempty"`
	// Usage is a table's end-line usage (Q-RUN-5).
	Usage json.RawMessage `json:"usage,omitempty"`
	// Truncated says the gateway cut the table at the caller's limit.
	Truncated bool   `json:"truncated,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

// Engine runs commands.
type Engine struct {
	opt     atlas.Options
	out     io.Writer
	outMu   sync.Mutex
	results io.Writer // the Arrow pipe, or nil

	resultsMu sync.Mutex // one stream at a time
	broken    bool       // a stream was cut mid-message: the pipe is out of step

	mu          sync.Mutex
	clients     map[int64]*atlas.Client
	order       []int64 // the connections, oldest first
	defaultConn *atlas.Client
	running     map[int64]context.CancelFunc
}

// New is an engine writing answers to out and Arrow tables to results.
func New(opt atlas.Options, out, results io.Writer) *Engine {
	if opt.Name == "" {
		opt.Name = Name
	}
	return &Engine{opt: opt, out: out, results: results, clients: map[int64]*atlas.Client{}, running: map[int64]context.CancelFunc{}}
}

// Run reads commands from in until it ends, answering each. Commands run
// while the next line is read, so cancel reaches them. When in ends, every
// command still running is cancelled and answered, and Run returns.
func (e *Engine) Run(ctx context.Context, in io.Reader) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), MaxLine)
	var g conc.Group // never one that cancels the rest: a command's failure is its own
	g.SetLimit(MaxPending)
	slots := make(chan struct{}, MaxRunning)
	for sc.Scan() {
		line := sc.Bytes()
		var c Command
		if err := json.Unmarshal(line, &c); err != nil {
			e.answer(failure(lineID(line), protocol.NewProblem(protocol.CodeInvalidRequest, "the command is not JSON: "+err.Error())))
			continue
		}
		if c.Op == "cancel" {
			e.cancel(c)
			continue
		}
		work, cancel := context.WithCancel(ctx)
		e.mu.Lock()
		_, taken := e.running[c.ID]
		if !taken {
			e.running[c.ID] = cancel
		}
		e.mu.Unlock()
		if taken {
			cancel()
			e.answer(failure(c.ID, protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("command %d is still running: an id names one command", c.ID)).WithTarget("id")))
			continue
		}
		if !g.TryGo(func() error { e.command(ctx, work, cancel, slots, c); return nil }) {
			e.forget(c.ID)
			cancel()
			e.answer(failure(c.ID, protocol.NewProblem(protocol.CodeUnavailable, fmt.Sprintf("the engine holds %d commands already", MaxPending)).WithRetryAfter(1)))
		}
	}
	readErr := sc.Err()
	stop() // the face is gone, or its input broke: nothing it asked for goes on (ATLAS-PY-007)
	if err := g.Wait(); err != nil {
		return fmt.Errorf("atlas-engine: %w", err)
	}
	if readErr != nil {
		return fmt.Errorf("atlas-engine: reading commands: %w", readErr)
	}
	return nil
}

// command runs one command in a slot, and answers it unless it answered
// itself (a table, as it streamed). A command the face cancelled, or whose
// engine's input ended, is answered as cancelled (§5.1).
func (e *Engine) command(run, ctx context.Context, cancel context.CancelFunc, slots chan struct{}, c Command) {
	defer cancel()
	defer e.forget(c.ID)
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		e.answer(cancelled(run, c.ID))
		return
	}
	a := e.safely(ctx, c)
	if a == nil {
		return
	}
	if !a.OK && ctx.Err() != nil && a.Problem != nil && a.Problem.Code == protocol.CodeDeadlineExceeded {
		*a = cancelled(run, c.ID)
	}
	e.answer(*a)
}

func cancelled(run context.Context, id int64) Answer {
	why := "the command was cancelled"
	if run.Err() != nil {
		why = "the engine's input ended, so the command was cancelled"
	}
	return Answer{ID: id, OK: false, Cancelled: true, Problem: protocol.NewProblem(protocol.CodeDeadlineExceeded, why)}
}

// safely runs a command, turning a panic into its internal problem: one
// command's failure never reaches another (R032, R063).
func (e *Engine) safely(ctx context.Context, c Command) (a *Answer) {
	defer func() {
		if v := recover(); v != nil {
			a = fail(c.ID, protocol.NewProblem(protocol.CodeInternal, fmt.Sprintf("the engine failed on this command: %v", v)))
		}
	}()
	return e.do(ctx, c)
}

func (e *Engine) forget(id int64) {
	e.mu.Lock()
	delete(e.running, id)
	e.mu.Unlock()
}

func (e *Engine) cancel(c Command) {
	var target struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(c.Args, &target) // pass 1 named the target in args; §5.1 in the line's own id
	if target.ID == 0 {
		target.ID = c.ID
	}
	e.mu.Lock()
	cancel := e.running[target.ID]
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (e *Engine) answer(a Answer) {
	if a.Warnings == nil {
		a.Warnings = []atlas.Warning{}
	}
	e.outMu.Lock()
	defer e.outMu.Unlock()
	_ = json.NewEncoder(e.out).Encode(a) // a face that is gone reads nothing
}

// lineID is a line's id when the line is not JSON as a whole (a NaN in its
// args): read up to its id member, so the face can match the answer.
func lineID(b []byte) int64 {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return 0
	}
	for d.More() {
		k, err := d.Token()
		if err != nil {
			return 0
		}
		if k == "id" {
			v, err := d.Token()
			n, ok := v.(json.Number)
			if err != nil || !ok {
				return 0
			}
			id, err := n.Int64()
			if err != nil {
				return 0
			}
			return id
		}
		var skip json.RawMessage
		if err := d.Decode(&skip); err != nil {
			return 0
		}
	}
	return 0
}

type callArgs struct {
	Address string          `json:"address"`
	Request json.RawMessage `json:"request"`
	Options struct {
		DeadlineMS int64    `json:"deadline_ms"`
		Accept     []string `json:"accept"`
		Arrow      bool     `json:"arrow"`
		Results    string   `json:"results"`
		Key        string   `json:"idempotency_key"`
	} `json:"options"`
	ID         string `json:"id"`
	Words      string `json:"words"`
	Client     string `json:"client"`
	MaxTokens  int    `json:"max_tokens"`
	Domain     string `json:"domain"`
	Family     string `json:"family"`
	Effect     string `json:"effect"`
	URL        string `json:"url"`
	Env        string `json:"env"`
	Token      string `json:"token"`
	Connection int64  `json:"connection"`
	// Environment is the face's ATLAS_* variables, read in place of the
	// engine's own (one engine serves every connection of its process).
	Environment map[string]string `json:"environment"`
}

// failure answers a command with a problem: every failure is one (kit/problem).
func failure(id int64, err error) Answer {
	p := problem.From(err)
	var known *protocol.Problem
	if !errors.As(err, &known) {
		p = protocol.NewProblem(protocol.CodeInternal, err.Error()) // the engine's own failure: its words are the caller's to read
	}
	return Answer{ID: id, OK: false, Problem: p}
}

func ok(id int64, v any) *Answer { return &Answer{ID: id, OK: true, Value: v, Calls: 1} }

func fail(id int64, err error) *Answer { a := failure(id, err); return &a }

// connect makes a connection, which becomes the one commands without
// args.connection use.
func (e *Engine) connect(ctx context.Context, id int64, a callArgs) (*atlas.Client, error) {
	opt := e.opt
	if a.URL != "" {
		opt.URL = a.URL
	}
	if a.Env != "" {
		opt.Env = a.Env
	}
	if a.Token != "" {
		opt.Token = a.Token
	}
	if a.Environment != nil {
		opt.Environment = a.Environment
	}
	c, err := atlas.Connect(ctx, opt)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.order) >= MaxConnections {
		delete(e.clients, e.order[0])
		e.order = e.order[1:]
	}
	e.clients[id] = c
	e.order = append(e.order, id)
	e.defaultConn = c
	return c, nil
}

// client is the connection a command names, or the default one, made on
// first use from the engine's options.
func (e *Engine) client(ctx context.Context, id int64, a callArgs) (*atlas.Client, error) {
	e.mu.Lock()
	switch {
	case a.Connection != 0:
		c := e.clients[a.Connection]
		e.mu.Unlock()
		if c == nil {
			return nil, protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("no connection %d: connect first", a.Connection)).WithTarget("connection")
		}
		return c, nil
	case e.defaultConn != nil && a.URL == "":
		c := e.defaultConn
		e.mu.Unlock()
		return c, nil
	}
	e.mu.Unlock()
	return e.connect(ctx, id, a)
}

// do runs one command. It returns nil when the command answered itself.
func (e *Engine) do(ctx context.Context, c Command) *Answer {
	var a callArgs
	if len(c.Args) > 0 && string(c.Args) != "null" {
		if err := json.Unmarshal(c.Args, &a); err != nil {
			return fail(c.ID, protocol.NewProblem(protocol.CodeInvalidRequest, "the command's args: "+err.Error()).WithTarget("args"))
		}
	}
	switch c.Op {
	case "version":
		return ok(c.ID, map[string]any{"name": e.opt.Name, "version": atlas.Version, "protocol": protocol.Version, "levels": []string{"core", "streaming", "columnar"}})
	case "connect":
		client, err := e.connect(ctx, c.ID, a)
		if err != nil {
			return fail(c.ID, err)
		}
		return ok(c.ID, map[string]any{"environment": client.Environment(), "gateway": client.Gateway(), "connection": c.ID})
	case "disconnect":
		e.mu.Lock()
		if cl := e.clients[a.Connection]; cl != nil {
			delete(e.clients, a.Connection)
			e.order = slices.DeleteFunc(e.order, func(id int64) bool { return id == a.Connection })
			if e.defaultConn == cl {
				e.defaultConn = nil
			}
		}
		e.mu.Unlock()
		return ok(c.ID, map[string]any{})
	}
	client, err := e.client(ctx, c.ID, a)
	if err != nil {
		return fail(c.ID, err)
	}
	var value any
	switch c.Op {
	case "whoami":
		value, err = client.Whoami(ctx)
	case "catalog":
		filter := map[string]any{}
		for k, v := range map[string]string{"domain": a.Domain, "family": a.Family, "effect": a.Effect} {
			if v != "" {
				filter[k] = v
			}
		}
		value, err = client.Catalog(ctx, filter)
	case "search":
		value, err = client.Search(ctx, a.Words)
	case "describe":
		value, err = client.Describe(ctx, a.ID)
	case "guide":
		value, err = client.GuideWithin(ctx, a.ID, a.Client, a.MaxTokens)
	case "access":
		value, err = client.AccessIn(ctx, a.ID, a.Domain)
	case "call", "fetch", "explain":
		if c.Op != "call" && a.Address == "" && a.ID != "" {
			a.Address = a.ID + ":" + c.Op // fetch and explain are call(id:fetch) and call(id:explain) (§9.7.1, Q-RUN-1)
		}
		return e.call(ctx, c, client, a)
	default:
		return fail(c.ID, protocol.NewProblem(protocol.CodeInvalidRequest, "the engine runs no op "+c.Op).WithTarget("op"))
	}
	if err != nil {
		return fail(c.ID, err)
	}
	return ok(c.ID, value)
}

func (e *Engine) call(ctx context.Context, c Command, client *atlas.Client, a callArgs) *Answer {
	var opts []atlas.CallOption
	if a.Options.DeadlineMS > 0 {
		opts = append(opts, atlas.WithDeadline(time.Duration(a.Options.DeadlineMS)*time.Millisecond))
	}
	if a.Options.Accept != nil {
		opts = append(opts, atlas.WithAccept(a.Options.Accept...))
	}
	if a.Options.Key != "" {
		opts = append(opts, atlas.WithIdempotencyKey(a.Options.Key))
	}
	req := atlas.Request(a.Request)
	if len(a.Request) == 0 || string(a.Request) == "null" {
		req = "{}"
	}
	r, err := client.Call(ctx, a.Address, req, opts...)
	if err != nil {
		return fail(c.ID, err)
	}
	defer r.Close()
	decimals := decimalPaths(r)
	if !r.Table() {
		return &Answer{ID: c.ID, OK: true, Value: r.Record, Warnings: r.Warnings, Calls: 1, Decimals: decimals, Profile: r.Exact, RequestID: r.RequestID}
	}
	arrow := c.Results == "arrow" || a.Options.Results == "arrow" || a.Options.Arrow
	if arrow && e.results != nil && r.Profile != nil {
		return e.stream(ctx, c.ID, r, decimals)
	}
	e.table(ctx, c.ID, r, decimals)
	return nil
}

// table answers a table as the runner protocol writes it, one line whose
// value holds every row (§9.8.3), written as the rows arrive so the
// engine holds one row at a time; ok and the problem come after the rows,
// so a table that fails midway answers the rows read (Q-RUN-6).
func (e *Engine) table(ctx context.Context, id int64, r *atlas.Result, decimals []string) {
	e.outMu.Lock()
	defer e.outMu.Unlock()
	w := bufio.NewWriterSize(e.out, 64<<10)
	fmt.Fprintf(w, `{"id":%d,"value":[`, id)
	var failed error
	for n := 0; ; n++ {
		row, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			failed = err
			break
		}
		b, err := ijson.Marshal(row)
		if err != nil {
			failed = protocol.NewProblem(protocol.CodeInvalidDocument, "a row is not I-JSON: "+err.Error())
			break
		}
		if n > 0 {
			_ = w.WriteByte(',')
		}
		_, _ = w.Write(b)
	}
	tail := Answer{ID: id, OK: failed == nil, Warnings: r.Warnings, Calls: 1, Decimals: decimals, Profile: r.Exact, Usage: r.Usage(), Truncated: r.Truncated(), RequestID: r.RequestID}
	if failed != nil {
		tail.Problem = failure(id, failed).Problem
		tail.Cancelled = ctx.Err() != nil && tail.Problem.Code == protocol.CodeDeadlineExceeded
	}
	if tail.Warnings == nil {
		tail.Warnings = []atlas.Warning{}
	}
	b, _ := json.Marshal(tail)
	_, _ = w.WriteString("],")
	_, _ = w.Write(b[bytes.IndexByte(b, ',')+1:]) // the members after id
	_ = w.WriteByte('\n')
	_ = w.Flush() // a face that is gone reads nothing
}

// decimalPaths are the members of a row or record that are exact
// decimals, in the profile the rows are delivered in: the reader's (R030).
func decimalPaths(r *atlas.Result) []string {
	if r.Profile == nil || r.Interaction == nil {
		return nil
	}
	s := r.Profile.Schemas[r.Interaction.Returns]
	if s == nil {
		return nil
	}
	if s.Items() != nil {
		s = s.Items()
	}
	var out []string
	for _, name := range s.Properties() {
		if r.Columns != nil && !slices.Contains(r.Columns, name) {
			continue
		}
		ps, _ := s.Property(name)
		switch logical, _, _, _ := ps.Logical(); {
		case logical == "Money":
			out = append(out, name+".amount")
		case logical == "Decimal" || ps.Format() == "decimal":
			out = append(out, name)
		}
	}
	return out
}

// stream answers a table as Arrow IPC on the results pipe (§5.1): first
// the answer that names the stream, then the batches as the rows arrive,
// in the reader's schema (R030), ended by the final batch carrying the end
// line, a problem after the rows included. Once it has named the stream it
// answers nothing more, and returns nil.
func (e *Engine) stream(ctx context.Context, id int64, r *atlas.Result, decimals []string) *Answer {
	row := r.Profile.Schemas[r.Interaction.Returns].Items()
	if r.Columns != nil {
		row = row.Project(r.Columns)
	}
	fields, err := arrowschema.Fields(row)
	if err != nil {
		return fail(id, protocol.NewProblem(protocol.CodeInvalidDocument, "the table has no Arrow form: "+err.Error()))
	}
	md := arrow.NewMetadata([]string{arrowschema.MetaProfile, arrowschema.MetaProtocol}, []string{r.Profile.Exact().String(), protocol.Version})
	sch := arrow.NewSchema(fields, &md)
	e.resultsMu.Lock()
	defer e.resultsMu.Unlock()
	if e.broken {
		return fail(id, protocol.NewProblem(protocol.CodeUnavailable, "the results pipe was cut mid-stream: start a new engine").WithRetryAfter(1))
	}
	// §5.1: the answer names the stream; the batches follow it.
	e.answer(Answer{ID: id, OK: true, Value: map[string]any{"stream": "arrow", "profile": r.Exact}, Warnings: r.Warnings, Calls: 1, Decimals: decimals, Profile: r.Exact, RequestID: r.RequestID})
	if d, ok := e.results.(interface{ SetWriteDeadline(time.Time) error }); ok {
		// A cancelled stream may write its final batch; a face that does
		// not read it within writeGrace no longer holds the engine.
		stop := context.AfterFunc(ctx, func() { _ = d.SetWriteDeadline(time.Now().Add(writeGrace)) })
		defer func() {
			stop()
			_ = d.SetWriteDeadline(time.Time{})
		}()
	}
	mem := memory.NewGoAllocator()
	aw, err := frame.NewArrowWriter(e.results, sch, mem)
	if err != nil {
		e.broken = true
		return nil
	}
	batch := make([]*ijson.Object, 0, batchRows)
	write := func() error {
		if len(batch) == 0 {
			return nil
		}
		b, err := arrowschema.ToBatch(mem, sch, row, batch)
		if err != nil {
			return protocol.NewProblem(protocol.CodeInvalidDocument, "a row has no Arrow form: "+err.Error())
		}
		defer b.Release()
		batch = batch[:0]
		if err := aw.Write(b); err != nil {
			e.broken = true
			return err
		}
		return nil
	}
	var failed error
	for failed == nil {
		next, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			failed = err
			break
		}
		if batch = append(batch, next); len(batch) == batchRows {
			failed = write()
		}
	}
	if failed == nil {
		failed = write()
	}
	if e.broken {
		return nil // the face stopped reading: nothing more can reach it
	}
	end := frame.End{Truncated: r.Truncated(), Usage: r.Usage()}
	switch {
	case failed != nil:
		end.Problem = whole(failure(id, failed).Problem, r.RequestID)
	case r.End() != nil && r.End().Problem != nil:
		end.Problem = whole(r.End().Problem, r.RequestID) // the truncation, as the gateway wrote it (Q-DOC-15)
	}
	if err := aw.End(end); err != nil {
		e.broken = true
	}
	return nil
}

// whole is a problem with every member an end line requires.
func whole(p *protocol.Problem, requestID string) *protocol.Problem {
	q := *p
	if q.Next == "" {
		q.Next = "See the problem's detail."
	}
	if q.RequestID == "" {
		q.RequestID = requestID
	}
	if q.RequestID == "" {
		q.RequestID = "atlas-engine"
	}
	if q.Type == "" {
		q.Type = protocol.ProblemTypeBase + q.Code
	}
	return &q
}

// ResultsPipe is the Arrow pipe the face passed (ATLAS_ENGINE_RESULTS_FD),
// or nil. Where it can be, it is made pollable, so that a write a face
// stops reading can be cut by its deadline.
func ResultsPipe(fd int) io.Writer {
	if fd <= 2 {
		return nil
	}
	pollable(fd)
	return os.NewFile(uintptr(fd), "results")
}
