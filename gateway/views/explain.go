package views

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol/documents"
	"atlas.local/sdk/go/protocol/ijson"
)

// explain answers what fetch would do with the request, without doing it
// (ATLAS-DATA-004, ATLAS-DATA-007): the read is built as fetch builds it,
// the plan is the one fetch would execute, and the source is asked for
// nothing but its estimate. The checks fetch makes of the values a request
// names (exists, valued) read the source, so explain does not make them.
func (p *Provider) explain(ctx context.Context, v *view, inv *ports.Invocation) (*ports.Answer, error) {
	rd, err := v.read(inv)
	if err != nil {
		return nil, err
	}
	plan, err := p.src.Plan(ctx, rd.req)
	if err != nil {
		return nil, p.failure(v, "planning "+rd.req.Query.Object, err)
	}
	ex, err := p.src.Explain(ctx, plan)
	if err != nil {
		return nil, p.failure(v, "explaining "+rd.req.Query.Object, err)
	}
	if ex == nil || ex.Statement != plan.Statement {
		return nil, internal("the source explained a statement other than the one %s would execute", v.pinned)
	}
	doc := v.explanation(inv, rd, ex)
	if fs := documents.Validate("explain", doc); len(fs) > 0 {
		return nil, internalBecause(fmt.Errorf("%s: %s", fs[0].Pointer, fs[0].Message), "the explanation of %s does not match explain.schema.json", v.pinned)
	}
	return &ports.Answer{Record: doc}, nil
}

// cost is what explain.schema.json's estimate carries (Q-DOC-10).
var cost = regexp.MustCompile(`^[0-9]{1,12}(\.[0-9]{1,3})?$`)

// explanation is the explain answer (explain.schema.json).
func (v *view) explanation(inv *ports.Invocation, rd *read, ex *ports.Explanation) *ijson.Object {
	plan := ijson.NewObject()
	language := ex.Language
	if !slices.Contains([]string{"sql", "http"}, language) {
		language = "UNKNOWN"
	}
	plan.Set("language", language)
	if ex.Dialect != "" {
		plan.Set("dialect", ex.Dialect)
	}
	if ex.Statement != "" {
		plan.Set("statement", ex.Statement)
	}
	if ex.Tag != "" {
		plan.Set("query_tag", ex.Tag)
	}
	estimate := ijson.NewObject()
	count := func(n *int64) ijson.Value {
		if n == nil || *n < 0 {
			return nil
		}
		return ijson.Number(strconv.FormatInt(*n, 10))
	}
	estimate.Set("rows", count(ex.Rows))
	estimate.Set("bytes", count(ex.Bytes))
	estimate.Set("cost", nil)
	if c := ex.Cost; c != nil && c.Unit != "" && cost.MatchString(c.Amount) {
		o := ijson.NewObject()
		o.Set("amount", c.Amount)
		o.Set("unit", c.Unit)
		estimate.Set("cost", o)
	}
	exact := inv.Exact
	if exact == "" {
		exact = v.profile.Exact().String()
	}
	doc := ijson.NewObject()
	doc.Set("address", v.pinned+":"+v.model.Interaction)
	doc.Set("profile", exact)
	doc.Set("plan", plan)
	doc.Set("result_schema", v.resultSchema(rd.props))
	doc.Set("execution", "gateway")
	doc.Set("estimate", estimate)
	if ex.Heuristic {
		w := ijson.NewObject()
		w.Set("code", "estimate_heuristic")
		w.Set("text", "The estimate is a heuristic, not the engine's own measure: the answer may differ from it.")
		doc.Set("warnings", []ijson.Value{w})
	}
	return doc
}

// resultSchema is the result's schema, as the profile writes it, narrowed
// to the properties the request asks for.
func (v *view) resultSchema(props []string) ijson.Value {
	src, ok := v.table.Source().(*ijson.Object)
	if !ok {
		return ijson.NewObject()
	}
	doc := ijson.Clone(src).(*ijson.Object)
	items, _ := doc.Get("items")
	row, ok := items.(*ijson.Object)
	if !ok {
		return doc
	}
	if all, ok := row.Get("properties"); ok {
		if o, ok := all.(*ijson.Object); ok {
			for _, name := range o.Names() {
				if !slices.Contains(props, name) {
					o.Delete(name)
				}
			}
		}
	}
	if req, ok := row.Get("required"); ok {
		if list, ok := req.([]ijson.Value); ok {
			kept := []ijson.Value{}
			for _, r := range list {
				if s, _ := r.(string); slices.Contains(props, s) {
					kept = append(kept, r)
				}
			}
			row.Set("required", kept)
		}
	}
	return doc
}
