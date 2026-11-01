// Package auditlog is an AuditSink that writes each fact as one JSON line,
// for a collector (Azure Monitor's agent, Fluent Bit) to ship to the audit
// store. Record returns once its fact is kept, so an admission is durable
// before its call is dispatched (ATLAS-CORE §8 step 7):
//
//   - on a regular file, the line is written and the file synced: the fact
//     survives the machine;
//   - on a pipe or a socket (a container's standard output, read by its
//     collector), the line is handed whole to the reader, whose durability
//     it then is: such a writer cannot be synced (ASSUMPTIONS.md A-101).
//
// Anything else (a terminal, /dev/null) keeps no record and is refused when
// the sink is made, never on each call. Facts recorded together share one
// write and one sync (§6.2.8's group commit), so a call waits only for that
// write, never for each other fact's (A-102).
package auditlog

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/conc"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// WriteTimeout bounds one write to a pipe or a socket: a collector that
// stops reading fails the write, and the facts in it, instead of holding the
// sink (and every call waiting on it, and Close) for as long as it stalls.
const WriteTimeout = 5 * time.Second

// ErrClosed is a fact recorded after Close.
var ErrClosed = errors.New("auditlog: the sink is closed")

// Sink writes facts to a writer. It is safe for concurrent use.
type Sink struct {
	w    io.Writer
	sync func() error // nil when the writer is not synced
	file *os.File     // the file Open opened, or the descriptor NewFile duplicated: Close closes it
	// deadline, when set, bounds the next write (a pipe's or socket's) to
	// timeout, WriteTimeout unless SetWriteTimeout says otherwise.
	deadline func(time.Time) error
	timeout  time.Duration

	clock  clock.Clock
	mu     sync.Mutex
	open   *batch    // the facts waiting for the next write; nil when none
	last   error     // the last write's error: nil once a write succeeds
	lastAt time.Time // when it failed
	torn   bool      // the last write stopped inside a line
	closed bool

	turn chan struct{} // capacity 1: held by whoever writes a batch
}

// batch is the facts that share one write.
type batch struct {
	lines [][]byte      // each fact's line; nil where one was withdrawn
	done  chan struct{} // closed once the batch is written, or failed
	err   error         // set before done is closed
}

// New is a Sink over w. A regular file, or any writer with a Sync method,
// is synced after each write; a pipe, a socket or a device is not. New
// refuses nothing: NewFile and Open check that a file can keep a fact.
func New(w io.Writer) *Sink {
	s := &Sink{w: w, clock: clock.System, turn: make(chan struct{}, 1)}
	switch f := w.(type) {
	case *os.File:
		if st, err := f.Stat(); err != nil || st.Mode().IsRegular() {
			s.sync = f.Sync // a file it cannot stat is synced, and its failure is reported
		}
	case interface{ Sync() error }:
		s.sync = f.Sync
	}
	return s
}

// NewFile is a Sink over f: a regular file, which is synced, or a pipe or
// socket, whose reader keeps the facts. Anything else keeps no record and
// is refused. Close does not close f.
func NewFile(f *os.File) (*Sink, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("auditlog: %s: %w", f.Name(), err)
	}
	m := st.Mode()
	if !m.IsRegular() && m&(os.ModeNamedPipe|os.ModeSocket) == 0 {
		return nil, fmt.Errorf("auditlog: %s is a %s, which keeps no record: give a file, or a pipe or socket a collector reads", f.Name(), describe(m))
	}
	if m.IsRegular() {
		return New(f), nil
	}
	// A pipe or socket is written through a duplicate of its descriptor in
	// non-blocking mode, which the runtime's poller drives, so each write
	// can carry a deadline (WriteTimeout). Where that is not possible, the
	// writes are unbounded, as they were.
	if p, err := pollable(f); err == nil {
		s := New(p)
		s.file, s.deadline = p, p.SetWriteDeadline
		return s, nil
	}
	return New(f), nil
}

// SetWriteTimeout bounds each write to a pipe or socket to d (WriteTimeout
// by default). It is set before the sink is used.
func (s *Sink) SetWriteTimeout(d time.Duration) { s.timeout = d }

func (s *Sink) writeTimeout() time.Duration {
	if s.timeout > 0 {
		return s.timeout
	}
	return WriteTimeout
}

// Open is a Sink appending to the regular file at path, created with mode
// 0600 if it does not exist. It proves at once that the file can be
// written and synced, so a gateway whose audit cannot be kept does not
// start. Close closes the file.
func Open(path string) (*Sink, error) {
	if st, err := os.Stat(path); err == nil && !st.Mode().IsRegular() {
		return nil, fmt.Errorf("auditlog: %s is not a regular file but a %s: name a file, or use standard output for a collector", path, describe(st.Mode()))
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("auditlog: %w", err)
	}
	s, err := NewFile(f)
	if err == nil && s.sync != nil {
		if err = s.sync(); err != nil {
			err = fmt.Errorf("auditlog: %s cannot be synced: %w", path, err)
		}
	}
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	s.file = f
	return s, nil
}

func describe(m os.FileMode) string {
	switch {
	case m.IsDir():
		return "directory"
	case m&os.ModeCharDevice != 0:
		return "character device (a terminal, or /dev/null)"
	case m&os.ModeDevice != 0:
		return "device"
	case m&os.ModeNamedPipe != 0:
		return "named pipe"
	case m&os.ModeSocket != 0:
		return "socket"
	}
	return m.Type().String()
}

// line is a fact as it is written: snake_case, times in UTC.
type line struct {
	Kind           string            `json:"kind"`
	At             string            `json:"at"`
	RequestID      string            `json:"request_id"`
	Attempt        int               `json:"attempt"`
	Operation      string            `json:"operation,omitempty"`
	Principal      string            `json:"principal,omitempty"`
	CallerKind     string            `json:"caller_kind,omitempty"`
	Application    string            `json:"application,omitempty"`
	Claimed        string            `json:"claimed,omitempty"`
	Client         *client           `json:"client,omitempty"`
	Environment    string            `json:"environment"`
	PolicyRevision string            `json:"policy_revision,omitempty"`
	Address        string            `json:"address,omitempty"`
	Exact          string            `json:"exact,omitempty"`
	Expected       string            `json:"expected,omitempty"`
	RequestDigest  string            `json:"request_digest,omitempty"`
	Step           string            `json:"step,omitempty"`
	Code           string            `json:"code,omitempty"`
	Rows           int64             `json:"rows,omitempty"`
	Truncated      bool              `json:"truncated,omitempty"`
	Usage          map[string]string `json:"usage,omitempty"`
	Source         *source           `json:"source,omitempty"`
	DurationMS     int64             `json:"duration_ms,omitempty"`
}

// client is the calling client, as its Atlas-Client header named it.
type client struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Protocol string `json:"protocol"`
}

// source is the source's own record of the work, as settlement copied it.
type source struct {
	QueryID string            `json:"query_id,omitempty"`
	Rows    int64             `json:"rows,omitempty"`
	Bytes   int64             `json:"bytes,omitempty"`
	Cost    map[string]string `json:"cost,omitempty"`
}

// encode is a fact's line, its newline included: every member of the
// fact (R3-04).
func encode(f ports.Fact) ([]byte, error) {
	l := line{
		Kind: string(f.Kind), At: f.At.UTC().Format(time.RFC3339Nano), RequestID: f.RequestID, Attempt: f.Attempt, Operation: f.Operation,
		Principal: f.Principal, CallerKind: f.CallerKind, Application: f.Application, Claimed: f.Claimed, Environment: f.Environment, PolicyRevision: f.PolicyRevision,
		Address: f.Address, Exact: f.Exact, Expected: f.Expected, RequestDigest: f.RequestDigest, Step: string(f.Step), Code: f.Code, Rows: f.Rows, Truncated: f.Truncated, Usage: f.Usage,
		DurationMS: f.Duration.Milliseconds(),
	}
	if f.Client != (protocol.Client{}) {
		l.Client = &client{Name: f.Client.Name, Version: f.Client.Version, Protocol: f.Client.Protocol}
	}
	if f.Source != nil {
		l.Source = &source{QueryID: f.Source.QueryID, Rows: f.Source.Rows, Bytes: f.Source.Bytes, Cost: f.Source.Cost}
	}
	b, err := json.Marshal(l)
	if err != nil {
		return nil, fmt.Errorf("auditlog: %w", err)
	}
	return append(b, '\n'), nil
}

// Record writes the fact, and returns once it is kept: written whole, and
// synced where the writer can be. A fact whose context ends before its
// write begins is withdrawn and never written; one whose context ends while
// its write is under way may still be kept, and Record says so.
func (s *Sink) Record(ctx context.Context, f ports.Fact) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	text, err := encode(f)
	if err != nil {
		return err
	}
	b, i, err := s.enqueue(text)
	if err != nil {
		return err
	}
	for {
		select {
		case <-b.done:
			return b.err
		case s.turn <- struct{}{}:
			// The write happens beside the call, not on it: a call waits for
			// its fact no longer than its own deadline, and the write itself
			// is bounded (WriteTimeout on a pipe or socket).
			conc.Go(func() error {
				defer func() { <-s.turn }()
				s.commit()
				return nil
			})
		case <-ctx.Done():
			if s.withdraw(b, i) {
				return ctx.Err()
			}
			select {
			case <-b.done:
				return b.err
			default:
				return fmt.Errorf("auditlog: %w while the fact was being written: it may still be kept", ctx.Err())
			}
		}
	}
}

// enqueue adds a line to the open batch.
func (s *Sink) enqueue(text []byte) (*batch, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, ErrClosed
	}
	if s.open == nil {
		s.open = &batch{done: make(chan struct{})}
	}
	s.open.lines = append(s.open.lines, text)
	return s.open, len(s.open.lines) - 1, nil
}

// withdraw takes a line out of its batch if the batch is not yet being
// written, and reports whether it did.
func (s *Sink) withdraw(b *batch, i int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open != b {
		return false
	}
	b.lines[i] = nil
	return true
}

// commit writes the open batch. Its caller holds the turn.
func (s *Sink) commit() {
	s.mu.Lock()
	b, torn := s.open, s.torn
	s.open = nil
	s.mu.Unlock()
	if b == nil {
		return
	}
	var buf []byte
	if torn {
		buf = append(buf, '\n') // end the torn line, so it never joins the next
	}
	for _, l := range b.lines {
		buf = append(buf, l...)
	}
	var err error
	if len(buf) > 0 {
		if s.deadline != nil {
			_ = s.deadline(time.Now().Add(s.writeTimeout()))
		}
		n, werr := s.w.Write(buf)
		if s.deadline != nil {
			_ = s.deadline(time.Time{})
		}
		switch {
		case werr != nil:
			err = fmt.Errorf("auditlog: write: %w", werr)
		case n < len(buf):
			err = fmt.Errorf("auditlog: write: %w", io.ErrShortWrite)
		case s.sync != nil:
			if serr := s.sync(); serr != nil {
				err = fmt.Errorf("auditlog: sync: %w", serr)
			}
		}
		s.mu.Lock()
		s.last, s.lastAt = err, s.clock.Now()
		// A write that stopped inside a line leaves it torn; one that wrote
		// nothing leaves the earlier tear as it was.
		s.torn = n > 0 && n < len(buf) || torn && n == 0
		s.mu.Unlock()
	}
	b.err = err
	close(b.done)
}

// probeAfter is how long a sink whose write failed reports itself unready
// before it lets a call probe the store again: §6.7.5's breaker, which
// probes after 15 s.
const probeAfter = 15 * time.Second

// Check is the sink's health, for readiness: unready for probeAfter after a
// write fails, so the load balancer sends calls elsewhere, then ready again
// so that one call probes the store, rather than out of rotation for want
// of a write to heal it; ErrClosed once it is closed.
func (s *Sink) Check(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return ErrClosed
	case s.last != nil && s.clock.Now().Sub(s.lastAt) < probeAfter:
		return s.last
	}
	return nil
}

// Close writes the facts already accepted, refuses any after them, and
// closes the file Open opened. A second Close does nothing.
func (s *Sink) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	s.turn <- struct{}{}
	s.commit()
	<-s.turn
	if s.file != nil {
		return s.file.Close()
	}
	return nil
}

// maxLine bounds one line of the audit, so a reader never buffers without
// limit.
const maxLine = 1 << 20

// Read reads the facts a Sink wrote, in order: the audit's own read path,
// for tests and for tools that check or replay an audit file. Every line
// must be one whole fact; the first that is not is an error naming it.
func Read(r io.Reader) ([]ports.Fact, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	var facts []ports.Fact
	for n := 1; ; n++ {
		text, err := readLine(br)
		if errors.Is(err, io.EOF) && len(text) == 0 {
			return facts, nil
		}
		if err != nil {
			return nil, fmt.Errorf("auditlog: line %d: %w", n, err)
		}
		if len(bytes.TrimSpace(text)) == 0 {
			continue
		}
		f, err := decode(text)
		if err != nil {
			return nil, fmt.Errorf("auditlog: line %d: %w", n, err)
		}
		facts = append(facts, f)
	}
}

// readLine is one line without its newline: io.EOF with nothing read at the
// end, and an error for a last line with no newline, which is torn.
func readLine(br *bufio.Reader) ([]byte, error) {
	var text []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if len(text)+len(chunk) > maxLine+1 {
			return nil, fmt.Errorf("longer than %d bytes", maxLine)
		}
		text = append(text, chunk...)
		switch {
		case err == nil:
			return text[:len(text)-1], nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(text) > 0:
			return nil, errors.New("torn: it has no newline")
		default:
			return text, err
		}
	}
}

// decode is one line's fact.
func decode(text []byte) (ports.Fact, error) {
	var l line
	if err := json.Unmarshal(text, &l); err != nil {
		return ports.Fact{}, err
	}
	switch {
	case l.Kind == "":
		return ports.Fact{}, errors.New("a fact has a kind")
	case l.RequestID == "":
		return ports.Fact{}, errors.New("a fact has a request_id")
	case !strings.HasSuffix(l.At, "Z"):
		return ports.Fact{}, fmt.Errorf("at %q is not a time in UTC", l.At)
	case l.DurationMS > math.MaxInt64/int64(time.Millisecond) || l.DurationMS < math.MinInt64/int64(time.Millisecond):
		return ports.Fact{}, fmt.Errorf("duration_ms %d is out of range", l.DurationMS)
	}
	at, err := time.Parse(time.RFC3339Nano, l.At)
	if err != nil {
		return ports.Fact{}, err
	}
	f := ports.Fact{
		Kind: ports.FactKind(l.Kind), At: at, RequestID: l.RequestID, Attempt: l.Attempt, Operation: l.Operation,
		Principal: l.Principal, CallerKind: l.CallerKind, Application: l.Application, Claimed: l.Claimed, Environment: l.Environment, PolicyRevision: l.PolicyRevision,
		Address: l.Address, Exact: l.Exact, Expected: l.Expected, RequestDigest: l.RequestDigest, Step: ports.Step(l.Step), Code: l.Code, Rows: l.Rows, Truncated: l.Truncated, Usage: l.Usage,
		Duration: time.Duration(l.DurationMS) * time.Millisecond,
	}
	if l.Client != nil {
		f.Client = protocol.Client{Name: l.Client.Name, Version: l.Client.Version, Protocol: l.Client.Protocol}
	}
	if l.Source != nil {
		f.Source = &ports.Usage{QueryID: l.Source.QueryID, Rows: l.Source.Rows, Bytes: l.Source.Bytes, Cost: l.Source.Cost}
	}
	return f, nil
}
