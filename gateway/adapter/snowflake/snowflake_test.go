package snowflake_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/gateway/adapter/snowflake"
	"github.com/jhgonline/fi-app-atlas/gateway/adapter/snowflake/snowfake"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/gateway/porttest"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/snowarrow"
)

const exact = "portfolio.holdings@v1#0123456789abcdef"

var writer = &protocol.Table{Columns: []protocol.Column{
	{Name: "portfolio", Type: protocol.TypeString, Key: true},
	{Name: "security", Type: protocol.TypeString, Key: true},
	{Name: "as_of", Type: protocol.TypeDate, Key: true},
	{Name: "market_value", Type: protocol.TypeDecimal, Precision: 18, Scale: 2},
	{Name: "weight", Type: protocol.TypeDecimal, Precision: 9, Scale: 6},
	{Name: "oas", Type: protocol.TypeNumber, Nullable: true, Logical: "Fraction"},
	{Name: "sector", Type: protocol.TypeString, Enum: []string{"Industrials", "Financials", "Utilities", "Energy", "Supranational", protocol.Unknown}},
	{Name: "currency", Type: protocol.TypeString, Nullable: true},
}}

var query = ports.Query{
	Source: "HARBOUR.PORTFOLIO_HOLDINGS",
	Columns: []ports.SourceColumn{
		{Writer: "portfolio", Source: "PORTFOLIO_CODE"}, {Writer: "security", Source: "ISIN"},
		{Writer: "as_of", Source: "AS_OF_DATE"}, {Writer: "market_value", Source: "MARKET_VALUE"},
		{Writer: "weight", Source: "WEIGHT"}, {Writer: "oas", Source: "OAS_BP", Divide: 10_000},
		{Writer: "sector", Source: "SECTOR"}, {Writer: "currency", Source: "CURRENCY"},
	},
	Filters: []ports.Filter{{Source: "PORTFOLIO_CODE", Op: ports.OpIn, Values: []any{"147HYC", "200IG"}}, {Source: "AS_OF_DATE", Op: ports.OpLatest}},
	OrderBy: []string{"portfolio", "security"},
	Tag:     "req-1",
}

type fixture struct {
	w      *snowfake.Warehouse
	src    *snowflake.Source
	client *http.Client
	mem    *memory.CheckedAllocator
	clock  *clock
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }

func setup(t *testing.T) fixture {
	t.Helper()
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	t.Cleanup(func() { mem.AssertSize(t, 0) })
	clk := &clock{now: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}
	w := snowfake.New(mem, snowfake.Harbour(snowfake.Small)...)
	w.ChunkRows, w.Now = 7, clk.Now // many small chunks
	srv := httptest.NewServer(w.Storage())
	t.Cleanup(srv.Close)
	w.SetStorageURL(srv.URL)
	return fixture{w: w, src: snowflake.New(w, srv.Client(), mem), client: srv.Client(), mem: mem, clock: clk}
}

func TestSnowflakeSourcePassesTheDataSourceSuite(t *testing.T) {
	f := setup(t)
	porttest.DataSource(t, f.src, query, writer, exact, f.client, f.mem)
	if f.w.Downloads() == 0 {
		t.Fatal("the handoff should have downloaded chunks from result storage")
	}
}

func TestPlanWritesTheContractIntoTheStatement(t *testing.T) {
	f := setup(t)
	plan, err := f.src.Plan(context.Background(), query, writer)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`CAST(MARKET_VALUE AS NUMBER(18,2)) AS "market_value"`,
		`CAST(OAS_BP / 10000 AS FLOAT) AS "oas"`,
		`CASE WHEN SECTOR IN ('Industrials', 'Financials', 'Utilities', 'Energy', 'Supranational') THEN SECTOR ELSE 'UNKNOWN' END`,
		`PORTFOLIO_CODE IN ('147HYC', '200IG')`,
		`AS_OF_DATE = (SELECT MAX(AS_OF_DATE) FROM HARBOUR.PORTFOLIO_HOLDINGS)`,
		`/* QUERY_TAG=req-1 */`,
	} {
		if !strings.Contains(plan.Statement, want) {
			t.Errorf("statement lacks %s:\n%s", want, plan.Statement)
		}
	}
}

func TestExpiredLinksAreRefusedAndRenewed(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	plan, _ := f.src.Plan(ctx, query, writer)
	h, err := f.src.Handoff(ctx, plan, exact)
	if err != nil || len(h.Chunks) == 0 {
		t.Fatalf("handoff: %v, %d links", err, len(h.Chunks))
	}
	if _, err := snowarrow.FetchChunk(ctx, f.client, h.Chunks[0], nil); !errors.Is(err, snowarrow.ErrLinkExpired) {
		t.Fatalf("a download without the result's headers must be refused, got %v", err)
	}
	f.clock.Add(20 * time.Minute) // past the 15-minute links
	if _, err := snowarrow.FetchChunk(ctx, f.client, h.Chunks[0], h.Headers); !errors.Is(err, snowarrow.ErrLinkExpired) {
		t.Fatalf("an expired link must be refused, got %v", err)
	}
	before := f.w.Downloads()
	renewed, err := f.src.Renew(ctx, h.QueryID, exact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snowarrow.FetchChunk(ctx, f.client, renewed.Chunks[0], renewed.Headers); err != nil {
		t.Fatalf("a renewed link should work: %v", err)
	}
	if f.w.Downloads() != before+1 {
		t.Fatal("renewing must not run the query again")
	}
}

func TestOlderContractsSeeUnknownForNewEnumValues(t *testing.T) {
	f := setup(t)
	old := &protocol.Table{Columns: append([]protocol.Column(nil), writer.Columns...)}
	old.Columns[6] = protocol.Column{Name: "sector", Type: protocol.TypeString, Enum: []string{"Industrials", "Financials", "Utilities", "Energy", protocol.Unknown}}
	plan, _ := f.src.Plan(context.Background(), query, old)
	res, err := f.src.Execute(context.Background(), plan, exact)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Reader.Release()
	schema, _ := protocol.ArrowSchema(old, exact)
	unknown := 0
	for _, row := range porttest.ReadRows(t, res.Reader, schema) {
		if strings.Contains(row, "Supranational") {
			t.Fatalf("a value outside the writer's enum reached the answer: %s", row)
		}
		if strings.Contains(row, "|UNKNOWN|") {
			unknown++
		}
	}
	if unknown == 0 {
		t.Fatal("the synthetic data has Supranational rows; they should arrive as UNKNOWN")
	}
}
