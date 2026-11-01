package gosf

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	sf "github.com/snowflakedb/gosnowflake"

	"github.com/jhgonline/fi-app-atlas/gateway/adapter/snowflake"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
)

// snowflakeServer speaks just enough of Snowflake's REST protocol for the
// real driver to log in, run a query and answer it in Arrow, report a
// query's status, abort a query and log out. It records what it was sent.
type snowflakeServer struct {
	t    *testing.T
	mu   sync.Mutex
	srv  *httptest.Server
	sent []sent
	// session are the session parameters the driver logged in with.
	session map[string]any
	aborts  int
	// held, when set, holds each query until the client goes away,
	// calling held once it does.
	held func()
}

// sent is one query request: its text, its per-statement parameters and
// its bindings, as the driver sent them.
type sent struct {
	SQL        string                    `json:"sqlText"`
	Parameters map[string]any            `json:"parameters"`
	Bindings   map[string]map[string]any `json:"bindings"`
	Async      bool                      `json:"asyncExec"`
}

func newServer(t *testing.T) *snowflakeServer {
	s := &snowflakeServer{t: t}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *snowflakeServer) reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.t.Error(err)
	}
}

func (s *snowflakeServer) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	switch {
	case r.URL.Path == "/session/v1/login-request":
		var login struct {
			Data struct {
				Session map[string]any `json:"SESSION_PARAMETERS"`
			} `json:"data"`
		}
		_ = json.Unmarshal(body, &login)
		s.mu.Lock()
		s.session = login.Data.Session
		s.mu.Unlock()
		s.reply(w, map[string]any{"success": true, "data": map[string]any{"token": "session-token", "masterToken": "master-token",
			"validityInSeconds": 3600, "masterValidityInSeconds": 14400, "sessionId": 1, "parameters": []any{},
			"sessionInfo": map[string]any{"databaseName": "HARBOUR", "schemaName": "HOLDINGS", "warehouseName": "WH", "roleName": "READER"}}})
	case r.URL.Path == "/queries/v1/query-request":
		var q sent
		if err := json.Unmarshal(body, &q); err != nil {
			s.t.Error(err)
		}
		s.mu.Lock()
		s.sent = append(s.sent, q)
		held := s.held
		s.mu.Unlock()
		if held != nil {
			held()
			<-r.Context().Done() // until the driver gives up on it
			return
		}
		s.reply(w, map[string]any{"success": true, "data": answer(s.t)})
	case strings.HasPrefix(r.URL.Path, "/monitoring/queries/"):
		s.reply(w, map[string]any{"success": true, "data": map[string]any{"queries": []any{map[string]any{"status": "SUCCESS",
			"startTime": 1761890400000, "endTime": 1761890401000, "stats": map[string]any{"scanBytes": 2048, "producedRows": 2}}}}})
	case r.URL.Path == "/queries/v1/abort-request":
		s.mu.Lock()
		s.aborts++
		s.mu.Unlock()
		s.reply(w, map[string]any{"success": true})
	case r.URL.Path == "/session":
		s.reply(w, map[string]any{"success": true})
	default:
		http.NotFound(w, r)
	}
}

// answer is a query's answer as Snowflake writes it: the row type, and the
// rows as an Arrow IPC stream in Snowflake's encodings, a NUMBER(38,6) as
// its unscaled Int64 and a NUMBER(9,2) as the Int16 its values fit.
func answer(t *testing.T) map[string]any {
	schema := arrow.NewSchema([]arrow.Field{{Name: "portfolio", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "market_value.amount", Type: arrow.PrimitiveTypes.Int64, Nullable: true}, {Name: "market_value.currency", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "price", Type: arrow.PrimitiveTypes.Int16, Nullable: true}, {Name: "as_of", Type: arrow.FixedWidthTypes.Date32, Nullable: true}}, nil)
	b := array.NewRecordBuilder(allocOf, schema)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).AppendValues([]string{"147HYC", "147HYC"}, nil)
	b.Field(1).(*array.Int64Builder).AppendValues([]int64{1014852172500, -2024690000000}, nil)
	b.Field(2).(*array.StringBuilder).AppendValues([]string{"USD", "EUR"}, nil)
	b.Field(3).(*array.Int16Builder).AppendValues([]int16{10125, 0}, []bool{true, false})
	b.Field(4).(*array.Date32Builder).AppendValues([]arrow.Date32{20756, 20756}, nil)
	rec := b.NewRecordBatch()
	defer rec.Release()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema))
	if err := w.Write(rec); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return map[string]any{"queryId": "01bf-0000-1234-0001", "queryResultFormat": "arrow", "total": 2, "returned": 2,
		"rowsetBase64": base64.StdEncoding.EncodeToString(buf.Bytes()),
		"rowtype": []any{
			map[string]any{"name": "portfolio", "type": "text", "nullable": true, "length": 16777216},
			map[string]any{"name": "market_value.amount", "type": "fixed", "precision": 38, "scale": 6, "nullable": true},
			map[string]any{"name": "market_value.currency", "type": "text", "nullable": true, "length": 3},
			map[string]any{"name": "price", "type": "fixed", "precision": 9, "scale": 2, "nullable": true},
			map[string]any{"name": "as_of", "type": "date", "nullable": true},
		}}
}

// engineFor is an Engine over gosnowflake, reaching server, with a
// transport of its own whose idle connections the test closes.
func engineFor(t *testing.T, s *snowflakeServer) *Engine {
	u, err := url.Parse(s.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	e, err := New(Config{Driver: sf.Config{Account: "harbour", User: "atlas_reader", Password: "not-a-secret", Host: u.Hostname(), Port: port, Protocol: "http",
		Database: "HARBOUR", Schema: "HOLDINGS", Warehouse: "WH", Role: "READER", Transporter: tr, DisableTelemetry: true, DisableQueryContextCache: true,
		ValidateDefaultParameters: sf.ConfigBoolFalse}, MaxOpen: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

// R051, ATLAS-DATA-004, ATLAS-SNOWFLAKE-004, R09: through the Snowflake
// Source and the real driver, the statement Snowflake receives is the
// plan's, byte for byte, with every value a binding and the tag sent as the
// statement's QUERY_TAG, never in its text; it runs synchronously; its
// answer, in Snowflake's own encodings, comes back in the writer schema
// with exact decimals; and Settle reads Snowflake's record of it.
func TestThroughTheDriver(t *testing.T) {
	s := newServer(t)
	e := engineFor(t, s)
	source, err := snowflake.NewSource(e, snowflake.Options{Registered: [][]string{{"HARBOUR", "HOLDINGS"}}})
	if err != nil {
		t.Fatal(err)
	}
	money := arrow.StructOf(arrow.Field{Name: "amount", Type: money6}, arrow.Field{Name: "currency", Type: arrow.BinaryTypes.String})
	price := &arrow.Decimal128Type{Precision: 9, Scale: 2}
	r := ports.PlanRequest{
		Query: ports.Query{Object: "HARBOUR.HOLDINGS.POSITIONS_V", Columns: []ports.Column{{Name: "PORTFOLIO_CODE", Type: ports.String},
			{Name: "MARKET_VALUE", Type: ports.Decimal}, {Name: "CURRENCY", Type: ports.String}, {Name: "PRICE", Type: ports.Decimal}, {Name: "AS_OF_DATE", Type: ports.Date}},
			Where: []ports.Predicate{{Column: "PORTFOLIO_CODE", Op: ports.In, Type: ports.String, Values: []any{"147HYC"}}, {Column: "AS_OF_DATE", Op: ports.Lte, Type: ports.Date, Values: []any{"2026-10-30"}}}},
		Writer: arrow.NewSchema([]arrow.Field{{Name: "portfolio", Type: arrow.BinaryTypes.String}, {Name: "market_value", Type: money, Nullable: true},
			{Name: "price", Type: price, Nullable: true}, {Name: "as_of", Type: arrow.FixedWidthTypes.Date32}}, nil),
		Columns:    [][]string{{"PORTFOLIO_CODE"}, {"MARKET_VALUE", "CURRENCY"}, {"PRICE"}, {"AS_OF_DATE"}},
		Capability: "portfolio.holdings@v1", RequestID: "req-42",
	}
	ctx := context.Background()
	p, err := source.Plan(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	res, err := source.Execute(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	var rows []string
	for {
		b, err := res.Batches.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !b.Schema().Equal(p.Writer) {
			t.Fatalf("a batch of %s", b.Schema())
		}
		for i := range int(b.NumRows()) {
			m := b.Column(1).(*array.Struct)
			px := b.Column(2).(*array.Decimal128)
			pxs := "null"
			if px.IsValid(i) {
				pxs = px.Value(i).ToString(2)
			}
			rows = append(rows, strings.Join([]string{b.Column(0).ValueStr(i), m.Field(0).(*array.Decimal128).Value(i).ToString(6), m.Field(1).ValueStr(i), pxs, b.Column(3).ValueStr(i)}, " "))
		}
		b.Release()
	}
	if err := res.Batches.Close(); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(rows, "; "), "147HYC 1014852.172500 USD 101.25 2026-10-30; 147HYC -2024690.000000 EUR null 2026-10-30"; got != want {
		t.Fatalf("the answer:\n got %s\nwant %s", got, want)
	}
	s.mu.Lock()
	q, session := s.sent[0], s.session
	s.mu.Unlock()
	if q.SQL != p.Statement || q.Async {
		t.Fatalf("Snowflake was sent %q (async %v); the plan is %q", q.SQL, q.Async, p.Statement)
	}
	if q.Parameters["QUERY_TAG"] != p.Tag || p.Tag != "atlas:portfolio.holdings@v1:req-42" || strings.Contains(q.SQL, "req-42") {
		t.Fatalf("the tag: %v, the plan's %q", q.Parameters, p.Tag)
	}
	if len(q.Bindings) != 2 || q.Bindings["1"]["value"] != "147HYC" || q.Bindings["2"]["value"] != "2026-10-30" {
		t.Fatalf("the bindings %v", q.Bindings)
	}
	if session["TIMEZONE"] != "UTC" {
		t.Fatalf("the session's parameters %v", session)
	}
	u, err := source.Settle(ctx, res.QueryID)
	if err != nil || u.QueryID != "01bf-0000-1234-0001" || u.Rows != 2 || u.Bytes != 2048 {
		t.Fatalf("settled %+v %v", u, err)
	}
}

// R17, gaps/6 §3.12: the statement runs synchronously, so when its context
// ends while Snowflake is still running it, the driver aborts it in
// Snowflake, rather than leaving it running as async mode would.
func TestACancelledStatementIsAbortedInSnowflake(t *testing.T) {
	s := newServer(t)
	e := engineFor(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.mu.Lock()
	s.held = cancel
	s.mu.Unlock()
	_, err := e.Execute(ctx, &snowflake.Statement{SQL: `SELECT 1 AS "n"`, Tag: "atlas:test@v1:req-1", Columns: []snowflake.Output{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled statement: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aborts != 1 {
		t.Fatalf("%d abort requests", s.aborts)
	}
}
