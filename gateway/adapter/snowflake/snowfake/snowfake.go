// Package snowfake is a fake Snowflake for the hermetic suite. It evaluates
// the adapter's typed plans over deterministic synthetic data, writes results
// in Snowflake's encoding (package snowarrow), keeps them behind expiring
// signed links that need a header, as Snowflake's result storage does, and
// records every query with its tag, for settlement.
//
// It is the skeleton's explicit statement of what it assumes Snowflake does.
// Proof 1 runs the same data-source suite against the real thing.
package snowfake

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/gateway/adapter/snowflake"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/snowarrow"
)

// KeyHeader is the header every chunk download must send, as Snowflake's
// result storage requires its own headers on some clouds.
const KeyHeader = "X-Snowfake-Key"

// Table is a synthetic source table: its columns as stored, and a
// deterministic generator of its rows.
type Table struct {
	Name    string
	Columns []snowarrow.Column
	Rows    int
	Row     func(i int) []any
}

func (t *Table) index(name string) int {
	for i, c := range t.Columns {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// Warehouse is the fake. Use New.
type Warehouse struct {
	// LinkTTL is how long chunk links work.
	LinkTTL time.Duration
	// ChunkRows is the number of rows in each chunk.
	ChunkRows int
	// Now is the warehouse's clock.
	Now func() time.Time
	// Latency is how long each chunk download waits before its first byte,
	// as from storage far away; none by default.
	Latency time.Duration

	mem       memory.Allocator
	secret    []byte
	tables    map[string]*Table
	downloads atomic.Int64

	mu      sync.Mutex
	base    string
	seq     int
	results map[string]*result
	history map[string]snowflake.QueryRecord
}

type result struct {
	sel     snowflake.Select
	table   *Table
	rowType snowarrow.RowType
	rows    []int
	key     string
	created time.Time
}

// New returns a warehouse holding the tables. Serve its Storage and call
// SetStorageURL before handing out links.
func New(mem memory.Allocator, tables ...*Table) *Warehouse {
	w := &Warehouse{
		LinkTTL: 15 * time.Minute, ChunkRows: 10_000, Now: time.Now,
		mem: mem, secret: randomBytes(32), tables: map[string]*Table{},
		results: map[string]*result{}, history: map[string]snowflake.QueryRecord{},
	}
	for _, t := range tables {
		w.tables[t.Name] = t
	}
	return w
}

// SetStorageURL is the base URL the Storage handler is served at.
func (w *Warehouse) SetStorageURL(base string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.base = strings.TrimRight(base, "/")
}

// Downloads counts the chunks served, for tests.
func (w *Warehouse) Downloads() int64 { return w.downloads.Load() }

var _ snowflake.Warehouse = (*Warehouse)(nil)

func (w *Warehouse) Execute(ctx context.Context, st snowflake.Statement) (*snowflake.QueryResult, error) {
	started := w.Now()
	t, ok := w.tables[st.Select.From]
	if !ok {
		return nil, fmt.Errorf("SQL compilation error: object %s does not exist or not authorized", st.Select.From)
	}
	rows, err := w.match(ctx, t, st.Select)
	if err != nil {
		return nil, err
	}
	res := &result{sel: st.Select, table: t, rowType: rowTypeOf(st.Select), rows: rows, key: hex.EncodeToString(randomBytes(16)), created: started}
	first, err := w.chunk(res, 0)
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	w.seq++
	qid := fmt.Sprintf("01b%013d-0000-fake-%04d", started.UnixMilli(), w.seq)
	w.results[qid] = res
	w.history[qid] = snowflake.QueryRecord{
		QueryID: qid, Tag: st.Tag, SQL: st.SQL, Rows: int64(len(rows)),
		Bytes: int64(len(rows)) * rowBytes(res.rowType), Started: started, Ended: w.Now(),
	}
	w.mu.Unlock()
	return w.answer(qid, res, first), nil
}

func (w *Warehouse) Result(_ context.Context, queryID string) (*snowflake.QueryResult, error) {
	w.mu.Lock()
	res, ok := w.results[queryID]
	w.mu.Unlock()
	if !ok || w.Now().Sub(res.created) > 24*time.Hour {
		return nil, fmt.Errorf("the result of query %s is no longer kept", queryID)
	}
	first, err := w.chunk(res, 0)
	if err != nil {
		return nil, err
	}
	return w.answer(queryID, res, first), nil
}

func (w *Warehouse) Estimate(ctx context.Context, st snowflake.Statement) (int64, int64, error) {
	t, ok := w.tables[st.Select.From]
	if !ok {
		return 0, 0, fmt.Errorf("SQL compilation error: object %s does not exist or not authorized", st.Select.From)
	}
	rows, err := w.match(ctx, t, st.Select)
	n := int64(len(rows))
	return n, n * rowBytes(rowTypeOf(st.Select)), err
}

func (w *Warehouse) History(_ context.Context, queryID string) (snowflake.QueryRecord, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rec, ok := w.history[queryID]
	if !ok {
		return rec, fmt.Errorf("no query %s in the history", queryID)
	}
	return rec, nil
}

func (w *Warehouse) answer(qid string, res *result, first []byte) *snowflake.QueryResult {
	expires := w.Now().Add(w.LinkTTL)
	chunks := (len(res.rows) + w.ChunkRows - 1) / w.ChunkRows
	var links []protocol.Chunk
	for n := 1; n < chunks; n++ {
		rows := min(w.ChunkRows, len(res.rows)-n*w.ChunkRows)
		links = append(links, protocol.Chunk{URL: w.link(qid, n, expires), Rows: int64(rows), Bytes: int64(rows) * rowBytes(res.rowType)})
	}
	return &snowflake.QueryResult{
		QueryID: qid, RowType: res.rowType, First: first, Chunks: links,
		Headers: map[string]string{KeyHeader: res.key}, Expires: expires, Rows: int64(len(res.rows)),
	}
}

func (w *Warehouse) link(qid string, n int, expires time.Time) string {
	w.mu.Lock()
	base := w.base
	w.mu.Unlock()
	exp := strconv.FormatInt(expires.Unix(), 10)
	return fmt.Sprintf("%s/results/%s/%d?exp=%s&sig=%s", base, qid, n, exp, w.sign(qid, n, exp))
}

func (w *Warehouse) sign(qid string, n int, exp string) string {
	m := hmac.New(sha256.New, w.secret)
	fmt.Fprintf(m, "%s|%d|%s", qid, n, exp)
	return hex.EncodeToString(m.Sum(nil))
}

// Storage serves chunk links: /results/{query id}/{chunk}?exp=…&sig=….
func (w *Warehouse) Storage() http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, "/results/")
		qid, chunk, ok2 := strings.Cut(rest, "/")
		n, err := strconv.Atoi(chunk)
		exp := r.URL.Query().Get("exp")
		expAt, err2 := strconv.ParseInt(exp, 10, 64)
		if !ok || !ok2 || err != nil || err2 != nil || n < 1 {
			http.Error(rw, "not found", http.StatusNotFound)
			return
		}
		if !hmac.Equal([]byte(r.URL.Query().Get("sig")), []byte(w.sign(qid, n, exp))) || w.Now().Unix() > expAt {
			http.Error(rw, "this link has expired or is not valid", http.StatusForbidden)
			return
		}
		w.mu.Lock()
		res, found := w.results[qid]
		w.mu.Unlock()
		if !found || r.Header.Get(KeyHeader) != res.key {
			http.Error(rw, "the download headers are missing or wrong", http.StatusForbidden)
			return
		}
		if w.Latency > 0 {
			select {
			case <-time.After(w.Latency):
			case <-r.Context().Done():
				return
			}
		}
		b, err := w.chunk(res, n)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}
		w.downloads.Add(1)
		rw.Header().Set("Content-Type", "application/octet-stream")
		rw.Write(b)
	})
}

// chunk encodes chunk n of a result, on demand: a large result never sits in
// memory, only the indices of its rows.
func (w *Warehouse) chunk(res *result, n int) ([]byte, error) {
	lo := min(n*w.ChunkRows, len(res.rows))
	hi := min(lo+w.ChunkRows, len(res.rows))
	rows := make([][]any, 0, hi-lo)
	for _, i := range res.rows[lo:hi] {
		out, err := project(res.table, res.sel, res.table.Row(i))
		if err != nil {
			return nil, err
		}
		rows = append(rows, out)
	}
	rec, err := snowarrow.Encode(w.mem, res.rowType, rows)
	if err != nil {
		return nil, err
	}
	defer rec.Release()
	return snowarrow.WriteChunk(rec)
}

func rowTypeOf(sel snowflake.Select) snowarrow.RowType {
	rt := make(snowarrow.RowType, len(sel.Columns))
	for i, p := range sel.Columns {
		rt[i] = p.Cast
		rt[i].Name = p.Alias
	}
	return rt
}

func rowBytes(rt snowarrow.RowType) int64 {
	var n int64
	for _, c := range rt {
		switch c.Type {
		case snowarrow.Text:
			n += 16
		case snowarrow.Date:
			n += 4
		case snowarrow.Boolean:
			n++
		case snowarrow.TimestampTZ, snowarrow.TimestampLTZ, snowarrow.TimestampNTZ:
			n += 16
		default:
			n += 8
		}
	}
	return n
}

// match evaluates the WHERE clause, then ORDER BY and LIMIT, and returns the
// indices of the rows that qualify.
func (w *Warehouse) match(ctx context.Context, t *Table, sel snowflake.Select) ([]int, error) {
	latest := map[string]any{}
	for _, f := range sel.Where {
		if f.Op != ports.OpLatest {
			continue
		}
		col := t.index(f.Source)
		if col < 0 {
			return nil, fmt.Errorf("SQL compilation error: invalid identifier %s", f.Source)
		}
		var best any
		for i := range t.Rows {
			if v := t.Row(i)[col]; best == nil || compare(v, best) > 0 {
				best = v
			}
		}
		latest[f.Source] = best
	}
	cols := make([]int, len(sel.Where))
	for i, f := range sel.Where {
		if cols[i] = t.index(f.Source); cols[i] < 0 {
			return nil, fmt.Errorf("SQL compilation error: invalid identifier %s", f.Source)
		}
	}
	var rows []int
	for i := range t.Rows {
		if i%65536 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		row := t.Row(i)
		keep := true
		for j, f := range sel.Where {
			if !holds(f, row[cols[j]], latest[f.Source]) {
				keep = false
				break
			}
		}
		if keep {
			rows = append(rows, i)
		}
	}
	if len(sel.OrderBy) > 0 {
		var keys []int
		for _, alias := range sel.OrderBy {
			for _, p := range sel.Columns {
				if p.Alias == alias {
					keys = append(keys, t.index(p.Source))
				}
			}
		}
		sort.SliceStable(rows, func(a, b int) bool {
			ra, rb := t.Row(rows[a]), t.Row(rows[b])
			for _, k := range keys {
				if c := compare(ra[k], rb[k]); c != 0 {
					return c < 0
				}
			}
			return false
		})
	}
	if sel.Limit > 0 && len(rows) > sel.Limit {
		rows = rows[:sel.Limit]
	}
	return rows, nil
}

func holds(f ports.Filter, v, latest any) bool {
	switch f.Op {
	case ports.OpEq:
		return compare(v, f.Values[0]) == 0
	case ports.OpIn:
		for _, x := range f.Values {
			if compare(v, x) == 0 {
				return true
			}
		}
		return false
	case ports.OpBetween:
		return compare(v, f.Values[0]) >= 0 && compare(v, f.Values[1]) <= 0
	case ports.OpLatest:
		return compare(v, latest) == 0
	}
	return false
}

func compare(a, b any) int {
	switch x := a.(type) {
	case string:
		y, _ := b.(string)
		return strings.Compare(x, y)
	case int64:
		y, _ := b.(int64)
		return cmpOrdered(x, y)
	case float64:
		y, _ := b.(float64)
		return cmpOrdered(x, y)
	case time.Time:
		y, _ := b.(time.Time)
		return x.Compare(y)
	case bool:
		y, _ := b.(bool)
		switch {
		case x == y:
			return 0
		case !x:
			return -1
		}
		return 1
	}
	return 0
}

func cmpOrdered[T int64 | float64](a, b T) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// project evaluates each projection of one row: the source value, divided if
// the plan says so, made to conform to the contract's enum, and cast.
func project(t *Table, sel snowflake.Select, row []any) ([]any, error) {
	out := make([]any, len(sel.Columns))
	for i, p := range sel.Columns {
		j := t.index(p.Source)
		if j < 0 {
			return nil, fmt.Errorf("SQL compilation error: invalid identifier %s", p.Source)
		}
		v := row[j]
		if s, ok := v.(string); ok && len(p.Enum) > 0 && !contains(p.Enum, s) {
			v = protocol.Unknown
		}
		cv, err := cast(v, t.Columns[j], p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.Alias, err)
		}
		out[i] = cv
	}
	return out, nil
}

func cast(v any, from snowarrow.Column, p snowflake.Projection) (any, error) {
	if v == nil {
		return nil, nil
	}
	to := p.Cast
	switch to.Type {
	case snowarrow.Real:
		var r *big.Rat
		switch x := v.(type) {
		case int64: // a stored NUMBER(p,s): exact until the one conversion to binary64
			r = new(big.Rat).SetFrac(big.NewInt(x), pow10(from.Scale))
		case float64:
			r = new(big.Rat).SetFloat64(x)
		default:
			return nil, fmt.Errorf("cannot cast %T to FLOAT", v)
		}
		if p.Divide != 0 {
			r.Quo(r, new(big.Rat).SetInt64(p.Divide))
		}
		f, _ := r.Float64()
		return f, nil
	case snowarrow.Fixed:
		x, ok := v.(int64)
		if !ok || from.Type != snowarrow.Fixed {
			return nil, fmt.Errorf("cannot cast %T to NUMBER", v)
		}
		if to.Scale < from.Scale {
			return nil, fmt.Errorf("NUMBER(%d,%d) cannot be cast to the smaller scale %d without rounding", from.Precision, from.Scale, to.Scale)
		}
		return new(big.Int).Mul(big.NewInt(x), pow10(to.Scale-from.Scale)), nil
	}
	return v, nil
}

func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
