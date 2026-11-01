package gosf

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"atlas.local/gateway/adapter/snowflake"
)

// holdingsRecord is one record of answer's rows in Snowflake's encodings:
// the portfolio, and an amount in millionths.
func holdingsRecord(portfolio string, amount int64) arrow.RecordBatch {
	b := array.NewRecordBuilder(allocOf, answerSchema)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).Append(portfolio)
	b.Field(1).(*array.Int64Builder).Append(amount)
	b.Field(2).(*array.StringBuilder).Append("USD")
	b.Field(3).(*array.Int16Builder).AppendNull()
	b.Field(4).(*array.Date32Builder).Append(20756)
	return b.NewRecordBatch()
}

// chunkOf is a remote chunk of records, one per amount.
func chunkOf(t *testing.T, amounts ...int64) []byte {
	var recs []arrow.RecordBatch
	for _, a := range amounts {
		rec := holdingsRecord("147HYC", a)
		defer rec.Release()
		recs = append(recs, rec)
	}
	return ipcOf(t, answerSchema, recs...)
}

// R3-02: the engine owns its list of batches. gosnowflake hands out its own
// slice when an answer has no inline first batch, and its downloader writes
// each fetched chunk through that slice, so an engine that cleared its
// entries in the driver's slice dereferenced nil on the first remote chunk.
// Through the real driver: remote chunks only, inline rows and remote
// chunks, a chunk of several records, and a download cancelled midway.
func TestR3_02RemoteBatchesAreTheEnginesOwn(t *testing.T) {
	inline := "147HYC 1014852.172500 USD 101.25 2026-10-30; 147HYC -2024690.000000 EUR null 2026-10-30"
	cases := []struct {
		name   string
		inline bool
		chunks [][]byte
		want   string
	}{
		{"remote only", false, [][]byte{chunkOf(t, 1_000000), chunkOf(t, 2_000000)},
			"147HYC 1.000000 USD null 2026-10-30; 147HYC 2.000000 USD null 2026-10-30"},
		{"inline and remote", true, [][]byte{chunkOf(t, 3_000000)},
			inline + "; 147HYC 3.000000 USD null 2026-10-30"},
		{"several records in a chunk", false, [][]byte{chunkOf(t, 4_000000, 5_000000, 6_000000)},
			"147HYC 4.000000 USD null 2026-10-30; 147HYC 5.000000 USD null 2026-10-30; 147HYC 6.000000 USD null 2026-10-30"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newServer(t)
			s.chunks, s.inline = tc.chunks, tc.inline
			source, err := snowflake.NewSource(engineFor(t, s), snowflake.Options{Registered: [][]string{{"HARBOUR", "HOLDINGS"}}})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			p, err := source.Plan(ctx, holdingsRequest())
			if err != nil {
				t.Fatal(err)
			}
			res, err := source.Execute(ctx, p)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(readRows(t, ctx, res.Batches, p.Writer), "; "); got != tc.want {
				t.Fatalf("the answer:\n got %s\nwant %s", got, tc.want)
			}
		})
	}
	t.Run("a download cancelled midway", func(t *testing.T) {
		s := newServer(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s.chunks, s.stall = [][]byte{chunkOf(t, 1)}, cancel
		source, err := snowflake.NewSource(engineFor(t, s), snowflake.Options{Registered: [][]string{{"HARBOUR", "HOLDINGS"}}})
		if err != nil {
			t.Fatal(err)
		}
		p, err := source.Plan(ctx, holdingsRequest())
		if err != nil {
			t.Fatal(err)
		}
		res, err := source.Execute(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		_, err = res.Batches.Next(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("a cancelled download: %v", err)
		}
		if err := res.Batches.Close(); err != nil {
			t.Fatal(err)
		}
	})
}
