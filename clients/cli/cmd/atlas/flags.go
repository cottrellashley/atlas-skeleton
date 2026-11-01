package main

import (
	"errors"
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

// parse parses flags wherever they stand among args, and returns the
// positional arguments (all of them after "--"). want is how many there
// must be: n exactly, -1 at least one, -2 at most one.
func parse(fs *flag.FlagSet, args []string, want int) ([]string, error) {
	var pos []string
	flags := args
	for i, a := range args {
		if a == "--" {
			flags, pos = args[:i], append(pos, args[i+1:]...)
			break
		}
	}
	var before []string
	for len(flags) > 0 {
		if err := fs.Parse(flags); err != nil {
			return nil, flagProblem(err)
		}
		flags = fs.Args()
		if len(flags) > 0 {
			before = append(before, flags[0])
			flags = flags[1:]
		}
	}
	pos = append(before, pos...)
	switch {
	case want >= 0 && len(pos) != want, want == -1 && len(pos) == 0, want == -2 && len(pos) > 1:
		what := map[int]string{0: "no arguments", 1: "one argument", -1: "at least one argument", -2: "at most one argument"}[want]
		target := fs.Name()
		if len(pos) > 0 {
			target = pos[len(pos)-1]
		}
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("atlas %s takes %s, not %d", fs.Name(), what, len(pos))).WithTarget(target).WithNext("atlas --help shows each command's arguments.")
	}
	return pos, nil
}

// flagProblem is a flag's error as invalid_request, its target the flag
// (§9.7.6: exit 2).
func flagProblem(err error) error {
	var p *protocol.Problem
	if errors.As(err, &p) || errors.Is(err, flag.ErrHelp) {
		return err
	}
	target := ""
	for _, word := range strings.Fields(err.Error()) {
		if strings.HasPrefix(word, "-") {
			target = "--" + strings.TrimRight(strings.TrimLeft(word, "-"), ":")
		}
	}
	return protocol.NewProblem(protocol.CodeInvalidRequest, err.Error()).WithTarget(target).WithNext("atlas --help shows the flags.")
}

// members are a request schema's top-level members as flags: --as_of, and
// --as-of too.
type members struct {
	s      *schema.Schema
	order  []string
	values map[string][]string
}

type memberValue struct {
	m    *members
	name string
	bool bool
}

func (v *memberValue) String() string { return "" }

func (v *memberValue) Set(s string) error {
	v.m.values[v.name] = append(v.m.values[v.name], s)
	return nil
}

func (v *memberValue) IsBoolFlag() bool { return v.bool }

// requestFlags adds a flag for each member of a request schema (§5.3 rule
// 1: never a flag written for one capability).
func requestFlags(fs *flag.FlagSet, s *schema.Schema) *members {
	m := &members{s: s, values: map[string][]string{}}
	if s == nil {
		return m
	}
	for _, name := range s.Properties() {
		p, _ := s.Property(name)
		if fs.Lookup(name) != nil {
			continue // a member named as a call option keeps the option's flag
		}
		v := &memberValue{m: m, name: name, bool: p.Type() == "boolean"}
		fs.Var(v, name, describe(p))
		if kebab := strings.ReplaceAll(name, "_", "-"); kebab != name && fs.Lookup(kebab) == nil {
			fs.Var(v, kebab, describe(p))
		}
		m.order = append(m.order, name)
	}
	return m
}

func describe(p *schema.Schema) string {
	src, _ := p.Source().(*ijson.Object)
	if src != nil {
		if d, ok := src.Get("description"); ok {
			if s, ok := d.(string); ok {
				return s
			}
		}
	}
	return p.Type()
}

// record is the request: the --request record, with each member flag's
// value set over it, typed by its schema. An array takes comma-separated
// values or a repeated flag; a value that is not a string by its schema is
// JSON (a number, true, null, an object).
func (m *members) record(base string) (*ijson.Object, error) {
	v, err := ijson.Parse([]byte(base))
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, "the request is not I-JSON: "+err.Error()).WithTarget("--request")
	}
	o, ok := v.(*ijson.Object)
	if !ok {
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, "a request record is a JSON object").WithTarget("--request")
	}
	for _, name := range m.order {
		raw, set := m.values[name]
		if !set {
			continue
		}
		p, _ := m.s.Property(name)
		value, err := typed(p, raw)
		if err != nil {
			return nil, protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("--%s: %v", name, err)).WithTarget("/" + name)
		}
		o.Set(name, value)
	}
	return o, nil
}

func typed(p *schema.Schema, raw []string) (ijson.Value, error) {
	if p.Type() == "array" {
		var out []ijson.Value
		for _, r := range raw {
			for _, item := range strings.Split(r, ",") {
				v, err := scalar(p.Items(), strings.TrimSpace(item))
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			}
		}
		if out == nil {
			out = []ijson.Value{}
		}
		return out, nil
	}
	return scalar(p, raw[len(raw)-1])
}

func scalar(p *schema.Schema, s string) (ijson.Value, error) {
	if p != nil && p.Type() == "string" {
		return s, nil
	}
	if p != nil && p.Type() == "boolean" && s == "" {
		return true, nil
	}
	v, err := ijson.Parse([]byte(s))
	if err != nil {
		if p == nil || p.Type() == "" {
			return s, nil // a union: a value that is not JSON is its text
		}
		return nil, fmt.Errorf("%q is not a JSON %s", s, p.Type())
	}
	return v, nil
}

// help lists the flags a capability's request gives, from its profile.
func (m *members) help(exact string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "atlas fetch %s: its flags, from its request schema\n\n", exact)
	names := append([]string(nil), m.order...)
	sort.Strings(names)
	for _, name := range names {
		p, _ := m.s.Property(name)
		required := ""
		if m.s.Required(name) {
			required = ", required"
		}
		fmt.Fprintf(&b, "  --%s (%s%s)\n      %s\n", name, p.Type(), required, describe(p))
	}
	b.WriteString("\n  --request JSON|@FILE|-, --accept M, --deadline D, --idempotency-key K\n")
	return b.String()
}
