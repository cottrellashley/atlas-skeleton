// Package e2e runs the gateway binary as a process and drives it with the Go
// client over real sockets: the client side of the pattern, end to end.
package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/sdk/go/client"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

var gatewayBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "atlas-e2e")
	if err != nil {
		panic(err)
	}
	gatewayBin = filepath.Join(dir, "gateway")
	build := exec.Command("go", "build", "-o", gatewayBin, "../cmd/gateway")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type running struct {
	URL string
}

// start runs the gateway on a free port and waits for its ready line.
func start(t *testing.T, args ...string) running {
	t.Helper()
	cmd := exec.Command(gatewayBin, append([]string{"-addr", "127.0.0.1:0"}, args...)...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatalf("the gateway did not start: %v", err)
	}
	var ready map[string]string
	if err := json.Unmarshal([]byte(line), &ready); err != nil {
		t.Fatalf("ready line %q: %v", line, err)
	}
	return running{URL: ready["gateway"]}
}

func token(t *testing.T, g running, persona string) string {
	t.Helper()
	resp, err := http.Post(g.URL+"/.dev/token", "application/json", strings.NewReader(`{"persona":"`+persona+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&tok)
	return tok.AccessToken
}

func connect(t *testing.T, g running, persona string, lock *client.Lock, mem memory.Allocator, parallel int) *client.Client {
	t.Helper()
	c, err := client.Connect(context.Background(), client.Options{URL: g.URL, Token: token(t, g, persona), Lock: lock, Mem: mem, Parallel: parallel})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// rows reads a table into JSON records, the forms a caller would see.
func rows(t *testing.T, tbl *client.Table) []map[string]any {
	t.Helper()
	defer tbl.Release()
	var out []map[string]any
	for tbl.Next() {
		rec := tbl.RecordBatch()
		for i := range int(rec.NumRows()) {
			r, err := protocol.RowRecord(rec, i)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
	}
	if err := tbl.Err(); err != nil {
		t.Fatalf("reading the table: %v", err)
	}
	return out
}

func columns(tbl *client.Table) []string {
	var names []string
	for _, f := range tbl.Schema().Fields() {
		names = append(names, f.Name)
	}
	return names
}

var holdings = map[string]any{"portfolios": []string{"147HYC", "200IG"}}

func TestTheClientReadsTheSameRowsStreamedOrHandedOff(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)
	g := start(t, "-chunk-rows", "13")
	c := connect(t, g, "pm", nil, mem, 0)
	ctx := context.Background()
	streamed, err := c.Fetch(ctx, "portfolio.holdings@v1:fetch", holdings, client.CallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if streamed.Execution != protocol.ExecutionGateway || streamed.Writer != streamed.Reader {
		t.Fatalf("an unlocked client reads under the current profile: %+v", streamed)
	}
	a := rows(t, streamed)
	handed, err := c.Fetch(ctx, "portfolio.holdings@v1:fetch", holdings, client.CallOptions{Execution: protocol.ExecutionHandoff})
	if err != nil {
		t.Fatal(err)
	}
	if handed.Execution != protocol.ExecutionHandoff {
		t.Fatalf("the client declared handoff and asked for it: %s", handed.Execution)
	}
	b := rows(t, handed)
	if len(a) == 0 || fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatalf("streamed and handed-off rows differ: %d vs %d", len(a), len(b))
	}
	if v, ok := a[0]["market_value"].(string); !ok || !strings.Contains(v, ".") {
		t.Fatalf("an exact decimal must arrive exact: %#v", a[0]["market_value"])
	}
}

// The heart of it: the contract changes behind a locked client, and the
// client's view does not.
func TestALockedClientKeepsItsShapeWhileTheContractEvolves(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), client.LockFile)

	// 1. Built against the first profile: mv, oas in basis points.
	before := start(t, "-pin", "portfolio.holdings@v1=1")
	c1 := connect(t, before, "pm", nil, mem, 0)
	lock, err := c1.LockCurrent(ctx, []string{"portfolio.holdings@v1"}, path)
	if err != nil {
		t.Fatal(err)
	}
	t1, err := c1.Fetch(ctx, "portfolio.holdings@v1:fetch", holdings, client.CallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cols1 := columns(t1)
	rows1 := rows(t, t1)

	// 2. The gateway is redeployed two profiles on; nothing about the client changes.
	after := start(t)
	locked, err := client.LoadLock(path)
	if err != nil || locked.Capabilities["portfolio.holdings@v1"] != lock.Capabilities["portfolio.holdings@v1"] {
		t.Fatalf("lock: %v", err)
	}
	for _, how := range []string{protocol.ExecutionGateway, protocol.ExecutionHandoff} {
		c2 := connect(t, after, "pm", locked, mem, 0)
		t2, err := c2.Fetch(ctx, "portfolio.holdings@v1:fetch", holdings, client.CallOptions{Execution: how})
		if err != nil {
			t.Fatal(err)
		}
		if t2.Writer == t2.Reader {
			t.Fatal("the gateway should now write under a newer profile than the lock's")
		}
		if cols := columns(t2); !slices.Equal(cols, cols1) {
			t.Fatalf("%s: the locked client's columns changed: %v, was %v", how, cols, cols1)
		}
		rows2 := rows(t, t2)
		if len(rows2) != len(rows1) {
			t.Fatalf("%s: %d rows, was %d", how, len(rows2), len(rows1))
		}
		for i := range rows1 {
			if !sameRow(rows1[i], rows2[i]) {
				t.Fatalf("%s: row %d changed:\n was %v\n now %v", how, i, rows1[i], rows2[i])
			}
		}
	}

	// 3. A request written against the old profile still means what it meant.
	c3 := connect(t, after, "pm", locked, mem, 0)
	t3, err := c3.Fetch(ctx, "portfolio.holdings@v1:fetch", map[string]any{"portfolios": []string{"147HYC"}, "columns": []string{"mv", "oas"}}, client.CallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cols := columns(t3); !slices.Equal(cols, []string{"mv", "oas"}) {
		t.Fatalf("projected columns under the old names: %v", cols)
	}
	rows(t, t3)
}

// sameRow compares two rows exactly, except oas: a number that crossed a unit
// lens twice in binary64 may differ in its last bits (§9.5.4).
func sameRow(a, b map[string]any) bool {
	for k, va := range a {
		vb := b[k]
		if k == "oas" && va != nil && vb != nil {
			fa, _ := va.(json.Number).Float64()
			fb, _ := vb.(json.Number).Float64()
			if math.Abs(fa-fb) > 1e-9*math.Max(1, math.Abs(fa)) {
				return false
			}
			continue
		}
		if fmt.Sprint(va) != fmt.Sprint(vb) {
			return false
		}
	}
	return len(a) == len(b)
}

// ATLAS-CORE-009: a capability deployed after the client was built is found
// and called, with no change to the client.
func TestACapabilityAddedLaterIsCallableWithNoClientChange(t *testing.T) {
	mem := memory.NewGoAllocator()
	ctx := context.Background()
	old := start(t, "-pin", "index.constituents@v1=0")
	found, err := connect(t, old, "pm", nil, mem, 0).Search(ctx, "index constituents")
	if err != nil || slices.ContainsFunc(found.Items, func(e protocol.CatalogEntry) bool { return e.ID == "index.constituents" }) {
		t.Fatalf("before it is deployed, search must not find it: %v %+v", err, found.Items)
	}
	now := start(t)
	c := connect(t, now, "pm", nil, mem, 0)
	found, err = c.Search(ctx, "index constituents")
	if err != nil || len(found.Items) == 0 || found.Items[0].ID != "index.constituents" {
		t.Fatalf("after: %v %+v", err, found.Items)
	}
	tbl, err := c.Fetch(ctx, found.Items[0].ID+"@v1:fetch", map[string]any{"indices": []string{"H0A0"}}, client.CallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := rows(t, tbl); len(got) == 0 || got[0]["index"] != "H0A0" {
		t.Fatalf("index constituents: %v", got)
	}
}

func TestExpiredLinksAreRenewedMidRead(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)
	g := start(t, "-chunk-rows", "5", "-link-ttl", "1s")
	c := connect(t, g, "pm", nil, mem, 1) // one chunk ahead, so later links expire unread
	ctx := context.Background()
	tbl, err := c.Fetch(ctx, "portfolio.holdings@v1:fetch", holdings, client.CallOptions{Execution: protocol.ExecutionHandoff})
	if err != nil {
		t.Fatal(err)
	}
	defer tbl.Release()
	n := 0
	for tbl.Next() {
		if n == 0 {
			time.Sleep(2500 * time.Millisecond)
		}
		n += int(tbl.RecordBatch().NumRows())
	}
	if err := tbl.Err(); err != nil {
		t.Fatalf("the client should have renewed the links: %v", err)
	}
	full, err := c.Fetch(ctx, "portfolio.holdings@v1:fetch", holdings, client.CallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := len(rows(t, full)); n != want {
		t.Fatalf("after renewing, %d rows; the result has %d", n, want)
	}
}

func TestProblemsReachTheCallerWithTheirCodes(t *testing.T) {
	g := start(t)
	ctx := context.Background()
	c := connect(t, g, "outsider", nil, memory.NewGoAllocator(), 0)
	if _, err := c.Fetch(ctx, "portfolio.holdings@v1:fetch", holdings, client.CallOptions{}); code(err) != protocol.CodeNotGranted {
		t.Fatalf("outsider: %v", err)
	}
	pm := connect(t, g, "pm", nil, memory.NewGoAllocator(), 0)
	if _, err := pm.Fetch(ctx, "portfolio.holdings@v1:fetch", map[string]any{"portfolios": "147HYC"}, client.CallOptions{}); code(err) != protocol.CodeInvalidRequest {
		t.Fatalf("an invalid request must be refused before it is sent: %v", err)
	}
	if _, err := pm.Fetch(ctx, "portfolio.missing@v1:fetch", nil, client.CallOptions{}); code(err) != protocol.CodeNotFound {
		t.Fatalf("missing: %v", err)
	}
}

func code(err error) string {
	if p, ok := err.(*protocol.Problem); ok {
		return p.Code
	}
	return fmt.Sprint(err)
}
