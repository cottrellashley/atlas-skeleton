package porttest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"testing/synctest"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/conc"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/arrowschema"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/documents"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

// ProviderCall is one invocation the provider under test is given, the
// profile it is served under, and what its answer must be.
type ProviderCall struct {
	Name       string
	Profile    *profile.Profile
	Invocation ports.Invocation
	// Rows is how many rows a table's answer holds; negative leaves it
	// unchecked.
	Rows int
	// Problem, when set, is the code the call must fail with.
	Problem string
}

// Provider is the suite of ports.Provider, in process (R20: in-process
// provider conformance first). Every call's answer is held to the port's
// contract and its profile's:
//   - a failure is a problem the profile declares or the protocol
//     registers, never a bare error the caller cannot act on;
//   - an answer holds exactly one of a record, rows or batches, as the
//     interaction's result says;
//   - a table's rows match the result schema, projected to the columns
//     asked for; a batch has the writer schema derived from it (§9.2.7);
//     no two rows share a key; every scoped value is inside the call's
//     scope (R05: the provider narrows its read, and the pipeline checks);
//   - an answer ends with io.EOF and keeps ending; Close is safe to repeat;
//   - Next and Serve honour a context that has ended;
//   - Settle, when set, names a query and is idempotent (R09);
//   - and the provider is safe for concurrent use: every call at once,
//     again and again, gives each its own answer, and leaves no goroutine
//     behind (a synctest bubble ends only when every goroutine in it has).
func Provider(t *testing.T, p ports.Provider, calls ...ProviderCall) {
	t.Helper()
	counts := make([]int, len(calls))
	for i, c := range calls {
		t.Run(c.Name, func(t *testing.T) {
			counts[i] = serveOnce(t, p, c)
		})
	}
	t.Run("a done context ends the call", func(t *testing.T) {
		for _, c := range calls {
			if c.Problem != "" {
				continue
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			inv := c.Invocation
			ans, err := p.Serve(ctx, &inv)
			if err != nil {
				continue
			}
			switch {
			case ans.Rows != nil:
				if row, err := ans.Rows.Next(ctx); err == nil || errors.Is(err, io.EOF) {
					t.Errorf("%s: Next on a done context answered %v, %v", c.Name, row, err)
				}
				_ = ans.Rows.Close()
			case ans.Batches != nil:
				b, err := ans.Batches.Next(ctx)
				if err == nil || errors.Is(err, io.EOF) {
					t.Errorf("%s: Next on a done context answered a batch, %v", c.Name, err)
				}
				if b != nil {
					b.Release()
				}
				_ = ans.Batches.Close()
			}
		}
	})
	t.Run("concurrent calls are each their own", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			g, ctx := conc.WithContext(context.Background())
			g.SetLimit(16)
			for round := range 4 {
				for i, c := range calls {
					g.Go(func() error {
						inv := c.Invocation
						ans, err := p.Serve(ctx, &inv)
						if c.Problem != "" {
							return expectProblem(c, err)
						}
						if err != nil {
							return fmt.Errorf("round %d, %s: %w", round, c.Name, err)
						}
						n, err := drain(ctx, c, ans)
						if err != nil {
							return fmt.Errorf("round %d, %s: %w", round, c.Name, err)
						}
						if n != counts[i] {
							return fmt.Errorf("round %d, %s: %d rows, alone %d", round, c.Name, n, counts[i])
						}
						return nil
					})
				}
			}
			if err := g.Wait(); err != nil {
				t.Fatal(err)
			}
		})
	})
}

// serveOnce is one call, every check made; it answers how many rows came.
func serveOnce(t *testing.T, p ports.Provider, c ProviderCall) int {
	t.Helper()
	ctx := context.Background()
	inv := c.Invocation
	ans, err := p.Serve(ctx, &inv)
	if c.Problem != "" {
		if err := expectProblem(c, err); err != nil {
			t.Fatal(err)
		}
		return 0
	}
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	n, err := drain(ctx, c, ans)
	if err != nil {
		t.Fatal(err)
	}
	if c.Rows >= 0 && n != c.Rows && interactionOf(c).Result == "table" {
		t.Fatalf("%d rows, want %d", n, c.Rows)
	}
	if ans.Settle != nil {
		u1, err1 := ans.Settle(ctx)
		u2, err2 := ans.Settle(ctx)
		if err1 != nil || err2 != nil || u1.QueryID == "" || !reflect.DeepEqual(u1, u2) {
			t.Fatalf("Settle is idempotent and names its query: %+v %v, then %+v %v", u1, err1, u2, err2)
		}
	}
	return n
}

func interactionOf(c ProviderCall) *profile.Interaction {
	if in := c.Profile.Interactions[c.Invocation.Interaction]; in != nil {
		return in
	}
	return &profile.Interaction{}
}

// expectProblem checks a call failed with the problem it should.
func expectProblem(c ProviderCall, err error) error {
	var prob *protocol.Problem
	if !errors.As(err, &prob) {
		return fmt.Errorf("%s: want the problem %s, got %v", c.Name, c.Problem, err)
	}
	if prob.Code != c.Problem {
		return fmt.Errorf("%s: want the problem %s, got %s (%s)", c.Name, c.Problem, prob.Code, prob.Detail)
	}
	if err := prob.Check(c.Profile.Errors...); err != nil {
		return fmt.Errorf("%s: a problem the profile does not declare: %w", c.Name, err)
	}
	return nil
}

// drain reads an answer to its end, holding it to its contract.
func drain(ctx context.Context, c ProviderCall, ans *ports.Answer) (int, error) {
	if ans == nil {
		return 0, errors.New("the provider answered nothing")
	}
	set := 0
	for _, x := range []bool{ans.Record != nil, ans.Rows != nil, ans.Batches != nil} {
		if x {
			set++
		}
	}
	if set != 1 {
		return 0, fmt.Errorf("an answer holds exactly one of a record, rows or batches, not %d", set)
	}
	in := interactionOf(c)
	if ans.Record != nil {
		if in.Result == "table" {
			return 0, errors.New("a table's interaction answered a record")
		}
		if s := c.Profile.Schemas[in.Returns]; s != nil {
			if fs := s.Validate(ans.Record); len(fs) > 0 {
				return 0, fmt.Errorf("the record does not match its schema: %v", fs[0])
			}
		}
		if c.Invocation.Interaction == "explain" {
			if fs := documents.Validate("explain", ans.Record); len(fs) > 0 {
				return 0, fmt.Errorf("the explain answer does not match explain.schema.json: %v", fs[0])
			}
		}
		return 0, nil
	}
	if in.Result != "table" {
		return 0, fmt.Errorf("a %s interaction answered a table", in.Result)
	}
	table := c.Profile.Schemas[in.Returns]
	if table == nil || table.Items() == nil {
		return 0, errors.New("the interaction returns no table schema")
	}
	row := table.Items()
	if c.Invocation.Columns != nil {
		row = row.Project(c.Invocation.Columns)
	}
	var rows []*ijson.Object
	if ans.Rows != nil {
		defer ans.Rows.Close()
		for {
			r, err := ans.Rows.Next(ctx)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return 0, err
			}
			rows = append(rows, r)
		}
		if _, err := ans.Rows.Next(ctx); !errors.Is(err, io.EOF) {
			return 0, fmt.Errorf("after the last row: %v", err)
		}
		if err := ans.Rows.Close(); err != nil {
			return 0, err
		}
		if err := ans.Rows.Close(); err != nil {
			return 0, fmt.Errorf("a second Close: %w", err)
		}
	} else {
		defer ans.Batches.Close()
		want, err := arrowschema.Fields(row)
		if err != nil {
			return 0, err
		}
		if err := sameFields(ans.Batches.Schema().Fields(), want); err != nil {
			return 0, fmt.Errorf("the answer's schema: %w", err)
		}
		for {
			b, err := ans.Batches.Next(ctx)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return 0, err
			}
			if err := sameFields(b.Schema().Fields(), want); err != nil {
				b.Release()
				return 0, fmt.Errorf("a batch's schema: %w", err)
			}
			got, err := arrowschema.ToRows(b, row)
			b.Release()
			if err != nil {
				return 0, err
			}
			rows = append(rows, got...)
		}
		if b, err := ans.Batches.Next(ctx); !errors.Is(err, io.EOF) {
			if b != nil {
				b.Release()
			}
			return 0, fmt.Errorf("after the last batch: %v", err)
		}
		if err := ans.Batches.Close(); err != nil {
			return 0, err
		}
		if err := ans.Batches.Close(); err != nil {
			return 0, fmt.Errorf("a second Close: %w", err)
		}
	}
	// The key is checked where the answer holds all of it: a projection that
	// leaves part out may repeat what remains.
	var keys []string
	whole := table.Items()
	for _, name := range whole.Properties() {
		if ps, _ := whole.Property(name); ps.Key() {
			keys = append(keys, name)
		}
	}
	if slices.ContainsFunc(keys, func(k string) bool { _, ok := row.Property(k); return !ok }) {
		keys = nil
	}
	seen := map[string]bool{}
	for i, r := range rows {
		if fs := row.Validate(r); len(fs) > 0 {
			return 0, fmt.Errorf("row %d does not match the result schema: %v", i, fs[0])
		}
		for _, v := range scoped(row, r, "") {
			if !c.Invocation.Scope.Allows(v[0], v[2]) {
				return 0, fmt.Errorf("row %d holds %s %q at %s, outside the call's scope", i, v[0], v[2], v[1])
			}
		}
		if len(keys) > 0 {
			k, _ := ijson.Canonical(project(r, keys))
			if seen[string(k)] {
				return 0, fmt.Errorf("row %d repeats the key %s", i, k)
			}
			seen[string(k)] = true
		}
	}
	return len(rows), nil
}

func sameFields(got, want []arrow.Field) error {
	if len(got) != len(want) {
		return fmt.Errorf("%d fields, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name || !arrow.TypeEqual(got[i].Type, want[i].Type) || got[i].Nullable != want[i].Nullable {
			return fmt.Errorf("field %d is %s %s (nullable %v), want %s %s (nullable %v)", i, got[i].Name, got[i].Type, got[i].Nullable, want[i].Name, want[i].Type, want[i].Nullable)
		}
	}
	return nil
}

func project(r *ijson.Object, names []string) *ijson.Object {
	o := ijson.NewObject()
	for _, n := range names {
		if v, ok := r.Get(n); ok {
			o.Set(n, v)
		}
	}
	return o
}

// scoped are a value's scoped values, read through its schema: each the
// dimension, the pointer and the value (ports.DimensionOf).
func scoped(s *schema.Schema, v ijson.Value, ptr string) [][3]string {
	if s == nil || v == nil {
		return nil
	}
	if dim, isScoped := ports.DimensionOf(s); isScoped {
		if str, isString := v.(string); isString {
			return [][3]string{{dim, ptr, str}}
		}
		return nil
	}
	var out [][3]string
	switch x := v.(type) {
	case []ijson.Value:
		if it := s.Items(); it != nil {
			for i, e := range x {
				out = append(out, scoped(it, e, ptr+"/"+strconv.Itoa(i))...)
			}
		}
	case *ijson.Object:
		for _, name := range s.Properties() {
			if e, ok := x.Get(name); ok {
				ps, _ := s.Property(name)
				out = append(out, scoped(ps, e, ptr+"/"+name)...)
			}
		}
	}
	return slices.Clip(out)
}
