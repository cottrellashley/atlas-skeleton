package views

import (
	"fmt"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/arrowschema"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/schema"
)

// read is the read one call makes, and how its answer is made from it.
type read struct {
	req ports.PlanRequest
	// fields are the writer's fields, the properties read from the source,
	// in the profile's order: the answer's own, and the key's and any
	// derived property's inputs it does not ask for.
	fields []string
	// props are the answer's properties, in the profile's order.
	props []string
	// row is the row schema projected to props, and answer the answer's
	// Arrow schema, with the stream's metadata (§9.2.7).
	row    *schema.Schema
	answer *arrow.Schema
}

// read builds the read an invocation makes. fetch and explain build it the
// same way, so explain's plan is fetch's (ATLAS-DATA-004; INTEGRATIONS:
// "explain and execution use the same plan construction").
func (v *view) read(inv *ports.Invocation) (*read, error) {
	rd := &read{props: v.order}
	if inv.Columns != nil && v.model.Search == nil {
		rd.props = slices.DeleteFunc(slices.Clone(v.order), func(p string) bool { return !slices.Contains(inv.Columns, p) })
	}
	need := map[string]bool{}
	for _, p := range rd.props {
		if !v.derived[p] {
			need[p] = true
		}
	}
	for _, p := range v.keys {
		need[p] = true
	}
	if f := v.model.Freshness; f != nil && slices.Contains(rd.props, f.Status) {
		need[f.Latest], need[f.Expected], need[f.Checked] = true, true, true
	}
	for _, p := range v.order {
		if need[p] {
			rd.fields = append(rd.fields, p)
		}
	}
	q := ports.Query{Object: v.model.Object, Tag: v.pinned + ":" + v.model.Interaction}
	seen := map[string]bool{}
	add := func(c ports.Column) {
		if !seen[c.Name] {
			seen[c.Name] = true
			q.Columns = append(q.Columns, c)
		}
	}
	mapping := make([][]string, len(rd.fields))
	for i, p := range rd.fields {
		src := v.model.Columns[p]
		if src.Money != nil {
			// Money's amount is Decimal(38,6) (§9.2.7): the source writes
			// it at that scale.
			add(ports.Column{Name: src.Money.Amount, Type: ports.Decimal, Precision: 38, Scale: 6})
			add(ports.Column{Name: src.Money.Currency, Type: ports.String})
			mapping[i] = []string{src.Money.Amount, src.Money.Currency}
			continue
		}
		c := ports.Column{Name: src.Column, Type: src.Type, Zone: src.Zone}
		if ps, _ := v.row.Property(p); src.Type == ports.Decimal {
			if name, precision, scale, ok := ps.Logical(); ok && name == "Decimal" {
				c.Precision, c.Scale = precision, scale // the contract's scale, rounded by the source (R100)
			}
		}
		add(c)
		mapping[i] = []string{src.Column}
	}
	writer, err := arrowschema.Fields(v.row.Project(rd.fields))
	if err != nil {
		return nil, fmt.Errorf("views: %s: %w", v.pinned, err)
	}
	preds := map[string]ports.Predicate{}
	for _, member := range sortedKeys(v.model.Filters) {
		val, present := inv.Request.Get(member)
		if !present || val == nil {
			continue
		}
		pred, err := predicate(member, v.model.Filters[member], val)
		if err != nil {
			return nil, err
		}
		preds[member] = pred
	}
	var scope []ports.Predicate
	for _, dim := range v.dims {
		allowed, ok := inv.Scope[dim]
		if !ok {
			return nil, internal("the call carries no data scope for %s, which %s is held to", dim, v.pinned)
		}
		if inv.Scope.Open(dim) {
			continue
		}
		// A request that names its own values of the dimension reads only
		// those the scope allows, which the pipeline has already proved it
		// does: the condition is the request's, never the caller's whole
		// scope (PR10).
		if m := v.narrows[dim]; m != "" {
			if pred, named := preds[m]; named {
				pred.Values = within(pred.Values, allowed)
				preds[m] = pred
				continue
			}
		}
		scope = append(scope, ports.Predicate{Column: v.model.Scope[dim], Op: ports.In, Type: ports.String, Values: anys(allowed)})
	}
	for _, member := range sortedKeys(preds) {
		q.Where = append(q.Where, preds[member])
	}
	q.Where = append(q.Where, scope...)
	if l := v.model.Latest; l != nil {
		latest := &ports.Latest{Date: l.Column, Per: l.Per}
		if d, _ := inv.Request.Get(l.Request); d != nil {
			latest.OnOrBefore, _ = d.(string)
		}
		for _, f := range l.Within {
			if pred, ok := preds[f]; ok {
				latest.Within = append(latest.Within, pred)
			}
		}
		latest.Within = append(latest.Within, scope...)
		q.Latest = latest
	}
	q.OrderBy = v.model.Order
	if inv.MaxRows > 0 && v.model.Search == nil {
		q.Limit = inv.MaxRows + 1 // one more, so the pipeline knows whether the cap cut it
	}
	rd.req = ports.PlanRequest{Query: q, Writer: arrow.NewSchema(writer, nil), Columns: mapping, Capability: v.pinned, RequestID: inv.RequestID}
	rd.row = v.row.Project(rd.props)
	fields, err := arrowschema.Fields(rd.row)
	if err != nil {
		return nil, fmt.Errorf("views: %s: %w", v.pinned, err)
	}
	exact := inv.Exact
	if exact == "" {
		exact = v.profile.Exact().String()
	}
	md := arrow.NewMetadata([]string{arrowschema.MetaProfile, arrowschema.MetaProtocol}, []string{exact, protocol.Version})
	rd.answer = arrow.NewSchema(fields, &md)
	return rd, nil
}

// lookup is a read of one column of an object, for the values a request
// names: what exists and valued check.
type lookup struct {
	req    ports.PlanRequest
	member string
	asked  []string // the member's values, in the request's order
}

// lookupOf is the lookup of a request member's values in object.column,
// narrowed to the caller's scope where the member names a scoped dimension.
func (v *view) lookupOf(inv *ports.Invocation, member, object, column string, latest *ports.Latest) lookup {
	val, _ := inv.Request.Get(member)
	arr, _ := val.([]ijson.Value)
	l := lookup{member: member}
	values := make([]any, 0, len(arr))
	for _, x := range arr {
		s, _ := x.(string)
		l.asked = append(l.asked, s)
		values = append(values, s)
	}
	for _, dim := range v.dims {
		if v.narrows[dim] == member && !inv.Scope.Open(dim) {
			values = within(values, inv.Scope[dim])
		}
	}
	l.req = v.lookup(inv, object, column, values, latest)
	return l
}

// lookup is the plan request reading column of object where it is one of
// values, its latest date per value when latest is set.
func (v *view) lookup(inv *ports.Invocation, object, column string, values []any, latest *ports.Latest) ports.PlanRequest {
	in := ports.Predicate{Column: column, Op: ports.In, Type: ports.String, Values: values}
	q := ports.Query{
		Object:  object,
		Columns: []ports.Column{{Name: column, Type: ports.String}},
		Where:   []ports.Predicate{in},
		OrderBy: []string{column},
		Tag:     v.pinned + ":" + v.model.Interaction,
	}
	if latest != nil {
		l := *latest
		l.Within = []ports.Predicate{in}
		q.Latest = &l
	}
	writer := arrow.NewSchema([]arrow.Field{{Name: "value", Type: arrow.BinaryTypes.String, Nullable: true}}, nil)
	return ports.PlanRequest{Query: q, Writer: writer, Columns: [][]string{{column}}, Capability: v.pinned, RequestID: inv.RequestID}
}

// probe is an invocation with no request values and an open scope: what
// Build plans once at start.
func (v *view) probe() *ports.Invocation {
	scope := ports.Scope{}
	for _, dim := range v.dims {
		scope[dim] = []string{ports.AllValues}
	}
	return &ports.Invocation{Capability: v.pinned, Interaction: v.model.Interaction, Exact: v.profile.Exact().String(), Request: ijson.NewObject(), Scope: scope}
}

// within keeps the values the scope allows, in their order.
func within(values []any, allowed []string) []any {
	out := make([]any, 0, len(values))
	for _, x := range values {
		if s, ok := x.(string); ok && slices.ContainsFunc(allowed, func(a string) bool { return a == s || a == ports.AllValues }) {
			out = append(out, x)
		}
	}
	return out
}

func anys(values []string) []any {
	out := make([]any, len(values))
	for i, s := range values {
		out[i] = s
	}
	return out
}

// predicate is the condition a request member's value makes.
func predicate(member string, f Filter, val ijson.Value) (ports.Predicate, error) {
	mapValue := func(x ijson.Value) (any, error) {
		s, isString := x.(string)
		if isString && f.Values != nil {
			mapped, ok := f.Values[s]
			if !ok {
				return nil, fmt.Errorf("views: the filter on %s maps no value %q", f.Column, s)
			}
			return mapped, nil
		}
		switch y := x.(type) {
		case string, bool:
			return y, nil
		case ijson.Number:
			switch f.Type {
			case ports.Integer:
				n, ok := y.Int64()
				if !ok {
					return nil, fmt.Errorf("views: %s is not an integer", y)
				}
				return n, nil
			case ports.Decimal:
				return string(y), nil
			}
			return y.Float64()
		}
		return nil, fmt.Errorf("views: a %T cannot be compared with %s", x, f.Column)
	}
	pred := ports.Predicate{Column: f.Column, Op: f.Op, Type: f.Type, Zone: f.Zone}
	if arr, ok := val.([]ijson.Value); ok {
		if f.Op != ports.In {
			return pred, fmt.Errorf("views: an array is compared only by in, not %s", f.Op)
		}
		if len(arr) > MaxValues {
			return pred, protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("%s holds %d values, more than the %d one call may name", member, len(arr), MaxValues)).
				WithTarget("/" + member).WithNext(fmt.Sprintf("Name at most %d, in more calls if you need them all.", MaxValues))
		}
		pred.Values = make([]any, 0, len(arr))
		for _, x := range arr {
			m, err := mapValue(x)
			if err != nil {
				return pred, err
			}
			pred.Values = append(pred.Values, m)
		}
		return pred, nil
	}
	m, err := mapValue(val)
	if err != nil {
		return pred, err
	}
	pred.Values = []any{m}
	return pred, nil
}
