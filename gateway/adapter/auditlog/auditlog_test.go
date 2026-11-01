package auditlog

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"atlas.local/gateway/ports"
	"atlas.local/gateway/porttest"
	"atlas.local/sdk/go/kit/conc"
	"atlas.local/sdk/go/protocol"
)

// ATLAS-MECH-014: a fact holds no token, key, body or query string, and its
// line is one JSON object.
func TestRecord(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf)
	f := ports.Fact{Kind: ports.Admission, At: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), RequestID: "r1", Attempt: 1, Principal: "p1", Address: "portfolio.list@v1:fetch", RequestDigest: "abc", Environment: "dv"}
	if err := s.Record(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil || !strings.HasSuffix(buf.String(), "\n") || strings.Count(buf.String(), "\n") != 1 {
		t.Fatalf("%q %v", buf.String(), err)
	}
	if got["kind"] != "admission" || got["request_digest"] != "abc" || got["at"] != "2026-10-31T12:00:00Z" {
		t.Fatalf("%v", got)
	}
}

// R056, R073: the JSON-lines sink passes the AuditSink suite over each
// writer it keeps facts on: a file it appends to and syncs, and a pipe a
// collector reads (a container's standard output).
func TestTheSinkPassesTheAuditSuite(t *testing.T) {
	t.Run("a file", func(t *testing.T) {
		porttest.AuditSink(t, func(t *testing.T) porttest.Audit {
			path := filepath.Join(t.TempDir(), "audit.jsonl")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close() })
			return porttest.Audit{
				Sink: s,
				Read: func(t *testing.T, _ int) []ports.Fact { return readFile(t, path) },
				// A file that can no longer be written: as a failed disk, every
				// write after this one fails.
				Break: func(t *testing.T) { _ = s.file.Close() },
			}
		})
	})
	// R012: a pipe cannot be synced; its reader keeps the facts.
	t.Run("a pipe", func(t *testing.T) {
		porttest.AuditSink(t, func(t *testing.T) porttest.Audit {
			c := newCollector(t)
			s, err := NewFile(c.w)
			if err != nil {
				t.Fatal(err)
			}
			return porttest.Audit{Sink: s, Read: c.read, Break: func(t *testing.T) { c.stop() }}
		})
	})
}

// R012: a sink over a pipe records, where it used to answer every call
// "sync |1: bad file descriptor", and so refused every call's admission.
func TestR012ASinkOverAPipeRecords(t *testing.T) {
	c := newCollector(t)
	s := New(c.w) // as cmd/gateway built it, over os.Stdout
	f := porttest.People[0]
	fact := ports.Fact{Kind: ports.Admission, At: time.Now().UTC(), RequestID: "r1", Attempt: 1, Principal: f.Principal, Environment: "dv"}
	if err := s.Record(t.Context(), fact); err != nil {
		t.Fatalf("a sink over a pipe: %v", err)
	}
	if got := c.read(t, 1); len(got) != 1 || got[0].RequestID != "r1" {
		t.Fatalf("the collector read %+v", got)
	}
}

// R012: a regular file is synced before Record returns, so an acknowledged
// fact survives the machine; a pipe or socket is not, because fsync fails
// on it, and its reader keeps the fact (ASSUMPTIONS.md A-101).
func TestR012OnlyARegularFileIsSynced(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if s := New(f); s.sync == nil {
		t.Fatal("a regular file is not synced")
	}
	c := newCollector(t)
	if s := New(c.w); s.sync != nil {
		t.Fatal("a pipe is synced, which fails")
	}
	w := &countingWriter{}
	if s := New(w); s.sync == nil {
		t.Fatal("a writer with Sync is not synced")
	}
}

// R047, R012: a sink that keeps nothing is refused when it is made, never on
// every call: /dev/null (and a terminal) is a device, which keeps no record.
func TestR047ASinkThatKeepsNothingIsRefused(t *testing.T) {
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if _, err := NewFile(null); err == nil || !strings.Contains(err.Error(), "keeps no record") {
		t.Fatalf("/dev/null: %v", err)
	}
	if _, err := Open(os.DevNull); err == nil {
		t.Fatal("Open accepted /dev/null")
	}
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	if _, err := Open(fifo); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("a FIFO named as the audit file: %v", err)
	}
}

// R012, §6.2.8 group commit: facts recorded together share one write and
// one sync, so a call waits only for that write, and no fact waits for
// another's fsync under one global lock.
func TestR012FactsRecordedTogetherShareOneWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hold := make(chan struct{})
		w := &countingWriter{hold: hold}
		s := New(w)
		first := conc.Go(func() error { return s.Record(context.Background(), fact("first")) })
		synctest.Wait() // the first fact's write is under way, and held
		var g conc.Group
		for i := range 9 {
			g.Go(func() error { return s.Record(context.Background(), fact("later-"+string(rune('a'+i)))) })
		}
		synctest.Wait() // the nine wait for the write in progress
		close(hold)
		if err := first(); err != nil {
			t.Fatal(err)
		}
		if err := g.Wait(); err != nil {
			t.Fatal(err)
		}
		if w.writes != 2 || w.syncs != 2 {
			t.Fatalf("%d writes and %d syncs for ten facts, want 2 and 2", w.writes, w.syncs)
		}
		got, err := Read(bytes.NewReader(w.buf.Bytes()))
		if err != nil || len(got) != 10 {
			t.Fatalf("%d facts, %v", len(got), err)
		}
	})
}

// R012, R056: a store that fails is reported to every fact of the write
// that failed, and to later ones while it keeps failing; never swallowed.
func TestR012AFailedWriteIsReported(t *testing.T) {
	w := &countingWriter{fail: errors.New("no space left on device")}
	s := New(w)
	err := s.Record(t.Context(), fact("r1"))
	if err == nil || !strings.Contains(err.Error(), "no space left on device") {
		t.Fatalf("got %v", err)
	}
	if err := s.Check(t.Context()); err == nil {
		t.Fatal("a sink whose last write failed reports itself healthy")
	}
	w.mu.Lock()
	w.fail = nil
	w.mu.Unlock()
	if err := s.Record(t.Context(), fact("r2")); err != nil {
		t.Fatal(err)
	}
	if err := s.Check(t.Context()); err != nil {
		t.Fatalf("a sink whose last write succeeded: %v", err)
	}
}

// R012, §6.7.5's breaker: a replica whose audit write failed is unready, so
// the load balancer sends its calls elsewhere; 15 s later it is ready again,
// so that a call probes the store, rather than staying out of rotation for
// want of a write to heal it.
func TestR012AFailedSinkIsUnreadyThenProbed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := &countingWriter{fail: errors.New("input/output error")}
		s := New(w)
		if err := s.Record(context.Background(), fact("r1")); err == nil {
			t.Fatal("a failed write was accepted")
		}
		time.Sleep(14 * time.Second)
		if err := s.Check(context.Background()); err == nil {
			t.Fatal("ready 14 s after a failed write")
		}
		time.Sleep(time.Second)
		if err := s.Check(context.Background()); err != nil {
			t.Fatalf("not ready to probe 15 s after a failed write: %v", err)
		}
		if err := s.Record(context.Background(), fact("r2")); err == nil {
			t.Fatal("the probe was accepted by a store still failing")
		}
		if err := s.Check(context.Background()); err == nil {
			t.Fatal("ready after the probe failed")
		}
	})
}

// R012: a write that stops short leaves a torn line; the next write starts
// a line of its own, so one failure never corrupts the facts after it.
func TestR012ATornLineNeverJoinsTheNext(t *testing.T) {
	w := &countingWriter{short: true}
	s := New(w)
	if err := s.Record(t.Context(), fact("torn")); err == nil {
		t.Fatal("a short write was accepted")
	}
	w.mu.Lock()
	w.short = false
	w.mu.Unlock()
	if err := s.Record(t.Context(), fact("whole")); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(w.buf.String(), "\n"), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], `"request_id":"whole"`) || !json.Valid([]byte(lines[1])) {
		t.Fatalf("%q", w.buf.String())
	}
}

// R047: Close writes what was accepted, closes the file Open opened, and
// refuses facts after it.
func TestCloseFlushesAndRefusesLaterFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Record(t.Context(), fact("r1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(t.Context(), fact("r2")); !errors.Is(err, ErrClosed) {
		t.Fatalf("a fact after Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("a second Close: %v", err)
	}
	if got := readFile(t, path); len(got) != 1 {
		t.Fatalf("%d facts", len(got))
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the audit file's mode: %v %v", st.Mode(), err)
	}
}

// Read refuses what is not a line of facts, naming the line.
func TestReadRefusesWhatIsNotAFact(t *testing.T) {
	for name, text := range map[string]string{
		"not JSON":           "{\"kind\":\"admission\"\n",
		"no kind":            `{"at":"2026-10-31T12:00:00Z","request_id":"r","attempt":1,"environment":"dv"}` + "\n",
		"a bad time":         `{"kind":"admission","at":"yesterday","request_id":"r","attempt":1,"environment":"dv"}` + "\n",
		"an array":           "[1]\n",
		"a torn last line":   `{"kind":"admission","at":"2026-10-31T12:00:00Z","request_id":"r","attempt":1,"environment":"dv"}`,
		"a line beyond 1MiB": `{"kind":"` + strings.Repeat("a", 1<<20) + `"}` + "\n",
	} {
		if _, err := Read(strings.NewReader(text)); err == nil || !strings.Contains(err.Error(), "line 1") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Read is a parser: it never panics, and a fact it reads is written back as
// the same line (AGENTS.md: fuzz every parser).
func FuzzRead(f *testing.F) {
	var buf bytes.Buffer
	s := New(&buf)
	for _, fc := range []ports.Fact{fact("r1"), {Kind: ports.Outcome, At: time.Unix(0, 1).UTC(), RequestID: "r2", Attempt: 3, Usage: map[string]string{"rows": "1"}, Duration: time.Second}} {
		if err := s.Record(context.Background(), fc); err != nil {
			f.Fatal(err)
		}
	}
	f.Add(buf.Bytes())
	f.Add([]byte("{}\n"))
	f.Add([]byte("\n\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		facts, err := Read(bytes.NewReader(b))
		if err != nil {
			return
		}
		var again bytes.Buffer
		s := New(&again)
		for _, fc := range facts {
			if err := s.Record(context.Background(), fc); err != nil {
				t.Fatal(err)
			}
		}
		back, err := Read(bytes.NewReader(again.Bytes()))
		if err != nil || len(back) != len(facts) {
			t.Fatalf("%d facts read back as %d: %v", len(facts), len(back), err)
		}
		for i := range facts {
			a, _ := encode(facts[i])
			b, _ := encode(back[i])
			if !bytes.Equal(a, b) {
				t.Fatalf("%s became %s", a, b)
			}
		}
	})
}

func fact(id string) ports.Fact {
	return ports.Fact{Kind: ports.Admission, At: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), RequestID: id, Attempt: 1, Environment: "dv", Address: "portfolio.list@v1:fetch"}
}

func readFile(t *testing.T, path string) []ports.Fact {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := Read(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

// collector is the reader of a pipe, as a collector reads a container's
// standard output: it reads every line as it arrives.
type collector struct {
	w       *os.File
	r       *os.File
	mu      sync.Mutex
	lines   []string
	changed chan struct{}
	done    func() error
	once    sync.Once
}

func newCollector(t *testing.T) *collector {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	c := &collector{r: r, w: w, changed: make(chan struct{})}
	c.done = conc.Go(func() error {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64<<10), 2<<20)
		for sc.Scan() {
			c.mu.Lock()
			c.lines = append(c.lines, sc.Text())
			close(c.changed)
			c.changed = make(chan struct{})
			c.mu.Unlock()
		}
		return nil
	})
	t.Cleanup(func() { c.w.Close(); c.stop() })
	return c
}

// read waits for n lines and reads them as facts.
func (c *collector) read(t *testing.T, n int) []ports.Fact {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	for {
		c.mu.Lock()
		lines, changed := c.lines, c.changed
		c.mu.Unlock()
		if len(lines) >= n {
			facts, err := Read(strings.NewReader(strings.Join(lines, "\n") + "\n"))
			if err != nil {
				t.Fatal(err)
			}
			return facts
		}
		select {
		case <-changed:
		case <-ctx.Done():
			t.Fatalf("the collector read %d lines, waiting for %d", len(lines), n)
		}
	}
}

// stop is the collector going away: the pipe has no reader, so a write to
// it fails.
func (c *collector) stop() {
	c.once.Do(func() {
		c.r.Close()
		_ = c.done()
	})
}

// countingWriter counts writes and syncs; it can hold a write until
// released, fail, or write short.
type countingWriter struct {
	mu            sync.Mutex
	buf           bytes.Buffer
	writes, syncs int
	hold          chan struct{}
	fail          error
	short         bool
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	hold := w.hold
	w.hold = nil
	w.mu.Unlock()
	if hold != nil {
		<-hold
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes++
	if w.fail != nil {
		return 0, w.fail
	}
	if w.short {
		w.buf.Write(p[:len(p)/2])
		return len(p) / 2, nil
	}
	return w.buf.Write(p)
}

func (w *countingWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.syncs++
	return nil
}

// R012, R073 (the review's regression): a collector that stops reading
// never holds a call past its own deadline, and the sink still closes. A
// pipe's writes carry a deadline, and the write happens beside the call.
func TestR012AStalledCollectorBoundsTheCallAndTheClose(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	s, err := NewFile(w)
	if err != nil {
		t.Fatal(err)
	}
	s.SetWriteTimeout(300 * time.Millisecond)
	// Fill the pipe, as a collector that stopped reading leaves it.
	chunk := make([]byte, 64<<10)
	_ = w.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
	for {
		if _, err := w.Write(chunk); err != nil {
			break
		}
	}
	_ = w.SetWriteDeadline(time.Time{})
	fact := ports.Fact{Kind: ports.Admission, At: time.Now().UTC(), RequestID: "r1", Attempt: 1, Principal: "p1", Environment: "dv"}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := s.Record(ctx, fact); err == nil {
		t.Fatal("a fact written to a full pipe is reported kept")
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the call waited %v, past its deadline of 100ms", took)
	}
	start = time.Now()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("Close waited %v on a stalled collector", took)
	}
	// The write that was cut is reported: the next call learns the sink failed.
	if err := s.Record(context.Background(), fact); !errors.Is(err, ErrClosed) {
		t.Fatalf("after Close: %v", err)
	}
}

// R3-04: the line a fact is persisted as holds every member of the fact,
// and the audit's own reader gives the same fact back. The stored bytes
// are asserted, not only what a reader decodes.
func TestR3_04EveryFieldOfAFactIsPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f := ports.Fact{
		Kind: ports.Settlement, At: time.Date(2026, 10, 31, 12, 0, 1, 500000000, time.UTC), RequestID: "r-1", Attempt: 2, Operation: "operations/7",
		Principal: "alice", CallerKind: "person", Application: "atlas-cli", Claimed: "bob", Client: protocol.Client{Name: "atlas-cli", Version: "0.2.0", Protocol: "atlas/1.0"},
		Environment: "dv", PolicyRevision: "p-7", Address: "portfolio.holdings@v1:fetch", Exact: "portfolio.holdings@v1#aaaa", Expected: "portfolio.holdings@v1#bbbb",
		RequestDigest: "9f2b", Step: ports.StepSettle, Code: "upstream_failed", Rows: 3, Truncated: true, Usage: map[string]string{"credits": "0.002"},
		Source: &ports.Usage{QueryID: "01bf", Rows: 3, Bytes: 2048, Cost: map[string]string{"credits": "0.002"}}, Duration: 1500 * time.Millisecond,
	}
	if err := s.Record(t.Context(), f); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kind":"settlement","at":"2026-10-31T12:00:01.5Z","request_id":"r-1","attempt":2,"operation":"operations/7","principal":"alice","caller_kind":"person","application":"atlas-cli","claimed":"bob","client":{"name":"atlas-cli","version":"0.2.0","protocol":"atlas/1.0"},"environment":"dv","policy_revision":"p-7","address":"portfolio.holdings@v1:fetch","exact":"portfolio.holdings@v1#aaaa","expected":"portfolio.holdings@v1#bbbb","request_digest":"9f2b","step":"settle","code":"upstream_failed","rows":3,"truncated":true,"usage":{"credits":"0.002"},"source":{"query_id":"01bf","rows":3,"bytes":2048,"cost":{"credits":"0.002"}},"duration_ms":1500}` + "\n"
	if string(b) != want {
		t.Fatalf("the stored line:\n got %s\nwant %s", b, want)
	}
	got := readFile(t, path)
	if len(got) != 1 || !reflect.DeepEqual(got[0], f) {
		t.Fatalf("read back %+v, want %+v", got, f)
	}
}
