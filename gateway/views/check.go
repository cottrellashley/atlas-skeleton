package views

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/arrowschema"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

// view is a view model checked against its profile, with what a call needs
// of both worked out once.
type view struct {
	model        Model
	profile      *profile.Profile
	pinned       string   // portfolio.holdings@v1
	interactions []string // fetch, and explain where the profile offers it
	request      *schema.Schema
	table        *schema.Schema // what the interaction returns: an array of rows
	row          *schema.Schema
	order        []string // the row's properties, in the profile's order
	keys         []string // the row's key properties, in that order
	derived      map[string]bool
	// dims are the data dimensions the view is held to, sorted: every one
	// a property of its result or a member of its request is typed with.
	dims []string
	// narrows names, for a dimension, the request member whose filter reads
	// the scope's own column: the request's values, held to the scope, are
	// then the condition, rather than the whole scope (PR10).
	narrows map[string]string
	within  time.Duration // Freshness.Within
}

// check holds a view model to its profile, so that a model that cannot
// answer its contract stops the gateway at start (PLUGGING-IN: "never on a
// call"; R074, R099).
func check(b Binding) (*view, error) {
	m, p := b.Model, b.Profile
	if p == nil {
		return nil, errors.New("views: a binding has a profile")
	}
	name := fmt.Sprintf("views: %s@v%d:%s", p.ID, p.Version, m.Interaction)
	in := p.Interactions[m.Interaction]
	if in == nil || m.Interaction == Explain {
		return nil, fmt.Errorf("%s: the profile has no such interaction to serve", name)
	}
	v := &view{model: m, profile: p, pinned: p.ID + "@v" + strconv.Itoa(p.Version), interactions: []string{m.Interaction}, derived: map[string]bool{}, narrows: map[string]string{}}
	if in.Takes != "" {
		v.request = p.Schemas[in.Takes]
	}
	v.table = p.Schemas[in.Returns]
	if v.table == nil || v.table.Items() == nil || len(v.table.Items().Properties()) == 0 {
		return nil, fmt.Errorf("%s: a view answers a table of rows with properties", name)
	}
	v.row = v.table.Items()
	v.order = v.row.Properties()
	var problems []string
	bad := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	if ex := p.Interactions[Explain]; ex != nil {
		switch {
		case ex.Effect != "read" || ex.Result != "record":
			bad("explain is a read that answers a record")
		case ex.Takes != in.Takes:
			bad("explain takes %q, not what %s takes (%q)", ex.Takes, m.Interaction, in.Takes)
		default:
			v.interactions = append(v.interactions, Explain)
		}
	}
	if s := m.Search; s != nil {
		v.derived[s.Score], v.derived[s.Why] = true, true
	}
	if f := m.Freshness; f != nil {
		v.derived[f.Status] = true
	}
	if _, err := arrowschema.Fields(v.row); err != nil {
		bad("the result has no Arrow form: %v", err)
	}
	types := map[string]ports.ColumnType{}
	uses := func(prop, column string, t ports.ColumnType) {
		if was, ok := types[column]; ok && was != t {
			bad("%s reads %s as %s, but another property reads it as %s", prop, column, t, was)
		}
		types[column] = t
	}
	for _, prop := range v.order {
		ps, _ := v.row.Property(prop)
		if ps.Key() {
			v.keys = append(v.keys, prop)
		}
		src, ok := m.Columns[prop]
		logical, _, _, _ := ps.Logical()
		switch {
		case v.derived[prop]:
			if ok {
				bad("%s is computed, so it has no column", prop)
			}
			continue
		case !ok:
			bad("the result's %s has no column", prop)
			continue
		case logical == "Money":
			if src.Money == nil || src.Money.Amount == "" || src.Money.Currency == "" || src.Column != "" || src.Values != nil {
				bad("%s is Money: give its amount and currency columns, and nothing else", prop)
				continue
			}
			uses(prop, src.Money.Amount, ports.Decimal)
			uses(prop, src.Money.Currency, ports.String)
			continue
		case src.Money != nil || src.Column == "":
			bad("%s has one column", prop)
			continue
		}
		want, known := columnType(ps)
		switch {
		case !known:
			bad("%s has no column type", prop)
		case src.Type != want:
			bad("%s is read from a %s column, not %q", prop, want, src.Type)
		}
		uses(prop, src.Column, src.Type)
		if src.Zone != "" && src.Type != ports.Timestamp {
			bad("%s has a zone, which only a timestamp column has", prop)
		}
		if src.Values != nil {
			if ps.Enum() == nil || len(src.Values) == 0 {
				bad("%s maps its column's values, so it is an enum and maps at least one", prop)
			}
			for _, column := range sortedKeys(src.Values) {
				if to := src.Values[column]; !ps.EnumHas(to) {
					bad("%s maps %q to %q, which its enum lacks", prop, column, to)
				}
			}
		}
	}
	for _, prop := range sortedKeys(m.Columns) {
		if _, ok := v.row.Property(prop); !ok {
			bad("the column of %s maps no property of the result", prop)
		}
	}
	for _, member := range sortedKeys(m.Filters) {
		f := m.Filters[member]
		ms := memberSchema(v.request, member)
		if ms == nil {
			bad("the filter %s names no member of the request", member)
			continue
		}
		switch f.Op {
		case ports.In, ports.Eq, ports.Gte, ports.Lte:
		default:
			bad("the filter %s compares by %q", member, f.Op)
		}
		if f.Column == "" || !validType(f.Type) {
			bad("the filter %s names its column and its type", member)
		}
		if f.Zone != "" && f.Type != ports.Timestamp {
			bad("the filter %s has a zone, which only a timestamp column has", member)
		}
		values := ms
		if ms.Type() == "array" {
			if f.Op != ports.In {
				bad("the filter %s takes an array, so it compares by in", member)
			}
			values = ms.Items()
		}
		if f.Values != nil && len(f.Values) == 0 {
			bad("the filter %s maps no value", member)
		}
		if f.Values != nil && values != nil {
			for _, e := range values.Enum() {
				if s, ok := e.(string); ok {
					if _, mapped := f.Values[s]; !mapped {
						bad("the filter %s maps no value %q", member, s)
					}
				}
			}
		}
	}
	if l := m.Latest; l != nil {
		if memberSchema(v.request, l.Request) == nil {
			bad("latest's request names %s, which is no member of the request", l.Request)
		}
		if l.Column == "" || len(l.Per) == 0 {
			bad("latest names its date column and the columns of its groups")
		}
		for _, f := range l.Within {
			if _, ok := m.Filters[f]; !ok {
				bad("latest's within names %s, which is no filter", f)
			}
		}
	}
	v.scope(bad)
	if len(v.keys) > 0 {
		var keyColumns []string
		for _, k := range v.keys {
			if src := m.Columns[k]; src.Column != "" {
				keyColumns = append(keyColumns, src.Column)
			} else {
				bad("the key %s has no column of its own", k)
			}
		}
		if len(m.Order) < len(keyColumns) || !sameSet(m.Order[:len(keyColumns)], keyColumns) {
			bad("the order begins with the key's columns (%s), so that rows sharing a key are next to each other", strings.Join(keyColumns, ", "))
		}
	}
	declared := func(code string) bool {
		return slices.ContainsFunc(p.Errors, func(d protocol.Declared) bool { return d.Code == code })
	}
	if e := m.Exists; e != nil {
		if !stringArray(memberSchema(v.request, e.Request)) || e.Object == "" || e.Column == "" {
			bad("exists names an array of strings in the request, and the object and column to find them in")
		}
		if !declared(e.Error) {
			bad("exists answers %s, which the profile does not declare", e.Error)
		}
	}
	if vd := m.Valued; vd != nil {
		if !stringArray(memberSchema(v.request, vd.Request)) || memberSchema(v.request, vd.AsOf) == nil || vd.Object == "" || vd.Column == "" || vd.Date == "" {
			bad("valued names an array of strings and a date in the request, and the object, column and date column to find them in")
		}
		if !declared(vd.Error) {
			bad("valued answers %s, which the profile does not declare", vd.Error)
		}
	}
	if f := m.Freshness; f != nil {
		v.freshness(bad)
	}
	if s := m.Search; s != nil {
		if memberSchema(v.request, s.Words) == nil || memberSchema(v.request, s.Limit) == nil {
			bad("search names the request's words and limit members")
		}
		for _, prop := range []string{s.Code, s.Name, s.Score, s.Why} {
			if _, ok := v.row.Property(prop); !ok {
				bad("search names %q, which is no property of the result", prop)
			}
		}
		if v.derived[s.Code] || v.derived[s.Name] {
			bad("search matches words in its code and name, which are read, not computed")
		}
	}
	if m.Unavailable != "" && !declared(m.Unavailable) {
		bad("unavailable is %s, which the profile does not declare", m.Unavailable)
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s: %s", name, strings.Join(problems, "; "))
	}
	return v, nil
}

// scope checks that every dimension the result or the request holds is
// held to the scope by the column that holds it (R074, R099): a model with
// no scope, or a scope on another column, would either refuse every scoped
// caller or answer them nothing.
func (v *view) scope(bad func(string, ...any)) {
	m := v.model
	dims := dimensions(v.row)
	for _, d := range dimensions(v.request) {
		if !slices.Contains(dims, d) {
			dims = append(dims, d)
		}
	}
	slices.Sort(dims)
	v.dims = dims
	for _, dim := range sortedKeys(m.Scope) {
		if !slices.Contains(dims, dim) {
			bad("the scope's %s is no dimension of the result or the request", dim)
		}
	}
	for _, dim := range dims {
		col, ok := m.Scope[dim]
		if !ok {
			bad("the result or the request holds %s values, so the scope names the column that holds %s", dim, dim)
			continue
		}
		for _, member := range sortedKeys(m.Filters) {
			f := m.Filters[member]
			if f.Column == col && f.Op == ports.In && f.Values == nil && slices.Contains(dimensions(memberSchema(v.request, member)), dim) {
				v.narrows[dim] = member
				break
			}
		}
		var holds []string
		for _, prop := range v.order {
			ps, _ := v.row.Property(prop)
			if slices.Contains(dimensions(ps), dim) && m.Columns[prop].Column != "" {
				holds = append(holds, m.Columns[prop].Column)
			}
		}
		switch {
		case len(holds) > 0 && !slices.Contains(holds, col):
			bad("the scope reads %s from %s, but the result's %s values are in %s", dim, col, dim, strings.Join(holds, ", "))
		case len(holds) == 0 && v.narrows[dim] == "":
			bad("no column the result reads, and no filter of the request, holds %s, so its rows cannot be held to the scope", dim)
		}
	}
}

// freshness checks a Freshness against the row it writes into.
func (v *view) freshness(bad func(string, ...any)) {
	f := v.model.Freshness
	status, ok := v.row.Property(f.Status)
	if !ok {
		bad("freshness writes %s, which is no property of the result", f.Status)
	} else {
		for _, s := range []string{f.Values.Current, f.Values.Late, f.Values.Unchecked} {
			if s == "" || !status.EnumHas(s) {
				bad("freshness writes %q into %s, which its enum lacks", s, f.Status)
			}
		}
	}
	for prop, want := range map[string]string{f.Latest: "date", f.Expected: "date", f.Checked: "date-time"} {
		ps, ok := v.row.Property(prop)
		switch {
		case !ok || v.derived[prop]:
			bad("freshness reads %q, which is no property the result reads", prop)
		case ps.Type() != "string" || ps.Format() != want:
			bad("freshness reads %s as a %s", prop, want)
		}
	}
	d, err := grammar.ParseDuration(f.Within)
	if err != nil || d <= 0 {
		bad("freshness's within is a positive ISO 8601 duration, not %q", f.Within)
	}
	v.within = d
}

// columnType is the type of the column a property is read from.
func columnType(ps *schema.Schema) (ports.ColumnType, bool) {
	if name, _, _, ok := ps.Logical(); ok && name == "Decimal" {
		return ports.Decimal, true
	}
	switch ps.Type() {
	case "boolean":
		return ports.Boolean, true
	case "integer":
		return ports.Integer, true
	case "number":
		return ports.Float, true
	case "string":
		switch ps.Format() {
		case "date":
			return ports.Date, true
		case "date-time":
			return ports.Timestamp, true
		case "int64":
			return ports.Integer, true
		}
		return ports.String, true
	}
	return "", false
}

func validType(t ports.ColumnType) bool {
	switch t {
	case ports.String, ports.Integer, ports.Float, ports.Decimal, ports.Boolean, ports.Date, ports.Timestamp:
		return true
	}
	return false
}

// dimensions are the data dimensions a schema's values are typed with, at
// any depth (ports.DimensionOf).
func dimensions(s *schema.Schema) []string {
	var out []string
	var visit func(*schema.Schema, int)
	visit = func(s *schema.Schema, depth int) {
		if s == nil || depth > 32 {
			return
		}
		if dim, scoped := ports.DimensionOf(s); scoped && !slices.Contains(out, dim) {
			out = append(out, dim)
		}
		visit(s.Items(), depth+1)
		for _, n := range s.Properties() {
			ps, _ := s.Property(n)
			visit(ps, depth+1)
		}
	}
	visit(s, 0)
	return out
}

func memberSchema(request *schema.Schema, member string) *schema.Schema {
	if request == nil {
		return nil
	}
	ps, _ := request.Property(member)
	return ps
}

// stringArray reports whether a member is an array of strings.
func stringArray(s *schema.Schema) bool {
	return s != nil && s.Type() == "array" && s.Items() != nil && s.Items().Type() == "string"
}

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}
