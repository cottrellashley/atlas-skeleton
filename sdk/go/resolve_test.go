package atlas

import (
	"context"
	"testing"

	"github.com/jhgonline/fi-app-atlas/sdk/go/engine/fakegw"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/frame"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

const (
	evolvingA = "conformance.evolving@v1#bc7fde7c0bac077d" // mv, weight a Percent
	evolvingB = "conformance.evolving@v1#5f4ce7e7661ebd4c" // mv renamed market_value; weight a Fraction; currency
)

// writerB is a position as #B writes it, projected to columns given in #B's
// names.
func writerB(i int64, columns []string) *ijson.Object {
	o := ijson.NewObject()
	o.Set("id", "p"+string(rune('0'+i)))
	o.Set("market_value", ijson.Number("100.5"))
	o.Set("weight", ijson.Number("0.25"))
	o.Set("currency", "USD")
	o.Set("status", "open")
	o.Set("count", ijson.Number("3"))
	if columns == nil {
		return o
	}
	p := ijson.NewObject()
	for _, c := range columns {
		v, _ := o.Get(c)
		p.Set(c, v)
	}
	return p
}

// evolving is a gateway whose current profile for the client is #A while it
// writes #B, as a gateway mid-rollout does (R03: an exact rename).
func evolving(t *testing.T, columns []string) *fakegw.Gateway {
	gw := fakegw.New(t, fakegw.Options{})
	gw.Current(evolvingA)
	gw.WriteUnder("conformance.evolving@v1:fetch", evolvingB)
	gw.Handle("conformance.evolving@v1:fetch", func(c *fakegw.Call) {
		c.Rows(2, func(i int64) *ijson.Object { return writerB(i, columns) }, frame.End{})
	})
	return gw
}

// R030, §9.7.4 step 10: rows written under a newer profile are delivered
// resolved to the reader's, and the result says so: its Profile is the
// reader's, whose schema the rows are in, and Exact the writer's.
func TestR030RowsAreDeliveredInTheReadersProfile(t *testing.T) {
	isolate(t)
	for _, accept := range []string{"application/x-ndjson", "application/vnd.apache.arrow.stream", "application/json"} {
		gw := evolving(t, nil)
		c := connect(t, gw, signedIn(t)) // each from #A: an answer under #B moves the pointer (D4.3)
		r, err := c.Call(context.Background(), "conformance.evolving@v1:fetch", map[string]any{"ids": []string{"p0", "p1"}}, WithAccept(accept))
		if err != nil {
			t.Fatal(err)
		}
		rows, err := r.All()
		if err != nil {
			t.Fatalf("%s: %v", accept, err)
		}
		if r.Profile.Exact().String() != evolvingA || r.Exact != evolvingB || r.Writer.Exact().String() != evolvingB {
			t.Fatalf("%s: Profile %s, Exact %s", accept, r.Profile.Exact(), r.Exact)
		}
		if len(rows) != 2 {
			t.Fatalf("%s: %d rows", accept, len(rows))
		}
		mv, ok := rows[0].Get("mv")
		_, renamed := rows[0].Get("market_value")
		if !ok || renamed || mv.(ijson.Number) != "100.5" {
			t.Fatalf("%s: the row is not in the reader's shape: %v", accept, rows[0])
		}
	}
}

// R031, §9.5.2 step 5, CORE-RES-002: a projected call under a newer writer
// resolves against the reader's schema projected to the columns asked for:
// a required column left out is not missing.
func TestR031ProjectedRowsResolveUnderANewerWriter(t *testing.T) {
	isolate(t)
	gw := evolving(t, []string{"id", "market_value"})
	c := connect(t, gw, signedIn(t))
	r, err := c.Call(context.Background(), "conformance.evolving@v1:fetch", map[string]any{"ids": []string{"p0"}, "columns": []string{"id", "mv"}}, WithAccept("application/x-ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := r.All()
	if err != nil {
		t.Fatalf("the projected rows did not resolve: %v", err)
	}
	if len(rows) != 2 || rows[0].Len() != 2 {
		t.Fatalf("rows: %v", rows)
	}
	if mv, _ := rows[1].Get("mv"); mv != ijson.Number("100.5") {
		t.Fatalf("row: %v", rows[1])
	}
}
