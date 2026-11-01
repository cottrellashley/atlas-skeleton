package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// ProfileSource returns a profile by its exact id: from a cache, or by asking
// the gateway (atlas.describe with exact).
type ProfileSource func(exact string) (*Profile, error)

// Chain is the profiles of one capability version between a writer and a
// reader, oldest first (§9.5.1).
type Chain struct {
	Profiles    []*Profile
	WriterNewer bool
}

// Identity reports whether the writer and the reader are one profile, so
// that resolution changes nothing.
func (c Chain) Identity() bool { return len(c.Profiles) <= 1 }

const maxChain = 64

// BuildChain finds the profiles between writer and reader by following
// evolution.previous from the newer back to the older. Profiles of different
// versions never resolve: across a version boundary only the gateway answers,
// by the new version's own plan (§9.5.2, step 2).
func BuildChain(src ProfileSource, writer, reader string) (Chain, error) {
	w, err := ParseExact(writer)
	if err != nil {
		return Chain{}, resolutionFailed("writer profile: %v", err)
	}
	r, err := ParseExact(reader)
	if err != nil {
		return Chain{}, resolutionFailed("reader profile: %v", err)
	}
	if w.Pinned != r.Pinned {
		return Chain{}, resolutionFailed("the writer %s and the reader %s are different capability versions", writer, reader)
	}
	if writer == reader {
		p, err := src(writer)
		if err != nil {
			return Chain{}, err
		}
		return Chain{Profiles: []*Profile{p}}, nil
	}
	path, err := walk(src, writer, reader)
	if err != nil {
		return Chain{}, err
	}
	if path != nil {
		return Chain{Profiles: reversed(path), WriterNewer: true}, nil
	}
	if path, err = walk(src, reader, writer); err != nil {
		return Chain{}, err
	}
	if path != nil {
		return Chain{Profiles: reversed(path), WriterNewer: false}, nil
	}
	return Chain{}, resolutionFailed("no chain of profiles joins %s and %s", writer, reader)
}

// walk follows evolution.previous from one exact id back towards another and
// returns [from … to], or nil when the trail ends first.
func walk(src ProfileSource, from, to string) ([]*Profile, error) {
	cur := from
	var path []*Profile
	for range maxChain {
		p, err := src(cur)
		if err != nil {
			return nil, err
		}
		path = append(path, p)
		if cur == to {
			return path, nil
		}
		if cur = p.Previous(); cur == "" {
			return nil, nil
		}
	}
	return nil, resolutionFailed("the chain from %s is longer than %d profiles", from, maxChain)
}

func reversed(ps []*Profile) []*Profile {
	out := make([]*Profile, len(ps))
	for i, p := range ps {
		out[len(ps)-1-i] = p
	}
	return out
}

// step is one lens operation, applied forward or backward, with the profile
// that declares it.
type step struct {
	op      LensOp
	forward bool
	at      *Profile
}

// steps lists the lens operations to apply, in order (§9.5.2, step 4): when
// the writer is newer, each profile's lens backward from the newest, in
// reverse; when it is older, each forward from the oldest, in order.
func (c Chain) steps(schema string) []step {
	var out []step
	n := len(c.Profiles) - 1
	if c.WriterNewer {
		for k := n; k >= 1; k-- {
			ops := lensOf(c.Profiles[k])
			for i := len(ops) - 1; i >= 0; i-- {
				if ops[i].AppliesTo(schema) {
					out = append(out, step{ops[i], false, c.Profiles[k]})
				}
			}
		}
		return out
	}
	for k := 1; k <= n; k++ {
		for _, op := range lensOf(c.Profiles[k]) {
			if op.AppliesTo(schema) {
				out = append(out, step{op, true, c.Profiles[k]})
			}
		}
	}
	return out
}

func lensOf(p *Profile) []LensOp {
	if p.Evolution == nil {
		return nil
	}
	return p.Evolution.Lens
}

// ResolveRow resolves one result record written under the chain's writer to
// the reader's table, already projected to the columns the request named
// (§9.5.2). Numbers are json.Number; decimals are strings.
func ResolveRow(c Chain, rec map[string]any, reader *Table) (map[string]any, error) {
	if c.Identity() {
		return rec, nil
	}
	y := make(map[string]any, len(rec))
	for k, v := range rec {
		y[k] = v
	}
	for _, s := range c.steps(SchemaResult) {
		if err := applyToRecord(s, y); err != nil {
			return nil, err
		}
	}
	z := make(map[string]any, len(reader.Columns))
	for _, col := range reader.Columns {
		v, ok := take(y, col.Name, col.Aliases)
		if !ok {
			if col.Nullable {
				z[col.Name] = nil
				continue
			}
			return nil, resolutionFailed("the writer's record has no value for %s", col.Name).WithTarget("/" + col.Name)
		}
		cv, err := readerValue(v, col)
		if err != nil {
			return nil, err
		}
		z[col.Name] = cv
	}
	return z, nil
}

// ResolveRequest resolves a request written under an earlier profile forward
// to the current request schema, where the gateway is the reader (§9.5.6).
// Members no current parameter takes are dropped; defaults and validation
// come after.
func ResolveRequest(c Chain, req map[string]any, current *Schema) (map[string]any, error) {
	if c.Identity() || current == nil {
		return req, nil
	}
	y := make(map[string]any, len(req))
	for k, v := range req {
		y[k] = v
	}
	for _, s := range c.steps(SchemaRequest) {
		if err := applyToRecord(s, y); err != nil {
			return nil, err
		}
	}
	z := make(map[string]any, len(y))
	for name, prop := range current.Properties {
		if v, ok := take(y, name, prop.Aliases); ok {
			z[name] = v
		}
	}
	return z, nil
}

// take finds a value by name, else by the last of its aliases present.
func take(rec map[string]any, name string, aliases []string) (any, bool) {
	if v, ok := rec[name]; ok {
		return v, true
	}
	for i := len(aliases) - 1; i >= 0; i-- {
		if v, ok := rec[aliases[i]]; ok {
			return v, true
		}
	}
	return nil, false
}

func applyToRecord(s step, rec map[string]any) error {
	op := s.op
	from, to := op.From, op.To
	if !s.forward {
		from, to = to, from
	}
	switch op.Op {
	case LensRename:
		if v, ok := rec[from]; ok {
			delete(rec, from)
			rec[to] = v
		}
	case LensAliasValue:
		if v, ok := rec[op.Field]; ok {
			rec[op.Field] = eachValue(v, func(x any) (any, error) {
				if str, ok := x.(string); ok && str == from {
					return to, nil
				}
				return x, nil
			})
		}
	case LensConvertUnit:
		v, ok := rec[op.Field]
		if !ok || v == nil {
			return nil
		}
		conv, err := unitConversion(from, to)
		if err != nil {
			return resolutionFailed("%v", err).WithTarget("/" + op.Field)
		}
		out, err := eachValueErr(v, func(x any) (any, error) { return convertJSON(x, conv) })
		if err != nil {
			return resolutionFailed("%s: %v", op.Field, err).WithTarget("/" + op.Field)
		}
		rec[op.Field] = out
	case LensPromote:
		// A JSON number reads as integer or number alike; only a backward
		// promotion can fail, when the value is not exact.
		if v, ok := rec[op.Field]; ok && !s.forward {
			if n, ok := v.(json.Number); ok {
				if _, err := strconv.ParseInt(n.String(), 10, 64); err != nil {
					return resolutionFailed("%s: %s is not an exact %s", op.Field, n, op.From).WithTarget("/" + op.Field)
				}
			}
		}
	case LensDefault:
		if !s.forward {
			delete(rec, op.Field)
			return nil
		}
		if _, ok := rec[op.Field]; !ok {
			v, err := decodeValue(op.Value)
			if err != nil {
				return resolutionFailed("default for %s: %v", op.Field, err)
			}
			rec[op.Field] = v
		}
	case LensDrop:
		if s.forward {
			delete(rec, op.Field)
		}
	default:
		return resolutionFailed("lens operation %q is not one this client implements", op.Op)
	}
	return nil
}

// readerValue converts one value to the reader's column (§9.5.5): an enum
// value the reader does not know becomes UNKNOWN.
func readerValue(v any, col Column) (any, error) {
	if v == nil || len(col.Enum) == 0 {
		return v, nil
	}
	str, ok := v.(string)
	if !ok || contains(col.Enum, str) {
		return v, nil
	}
	if contains(col.Enum, Unknown) {
		return Unknown, nil
	}
	return nil, resolutionFailed("%s: %q is not a value the reader knows", col.Name, str).WithTarget("/" + col.Name)
}

func eachValue(v any, f func(any) (any, error)) any {
	out, _ := eachValueErr(v, f)
	return out
}

func eachValueErr(v any, f func(any) (any, error)) (any, error) {
	arr, ok := v.([]any)
	if !ok {
		return f(v)
	}
	out := make([]any, len(arr))
	for i, x := range arr {
		y, err := f(x)
		if err != nil {
			return nil, err
		}
		out[i] = y
	}
	return out, nil
}

// convertJSON applies a unit conversion to a JSON number (binary64, round to
// nearest) or an exact decimal string (exact arithmetic, same scale).
func convertJSON(v any, conv conversion) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return nil, err
		}
		return json.Number(strconv.FormatFloat(conv.float(f), 'g', -1, 64)), nil
	case float64:
		return conv.float(x), nil
	case string:
		unscaled, scale, err := parseDecimal(x)
		if err != nil {
			return nil, err
		}
		return formatDecimal(conv.unscaled(unscaled), scale), nil
	}
	return nil, fmt.Errorf("%v is not a number", v)
}

func decodeValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	err := dec.Decode(&v)
	return v, err
}

// parseDecimal reads an exact decimal string into its unscaled value and scale.
func parseDecimal(s string) (*big.Int, int, error) {
	neg := strings.HasPrefix(s, "-")
	digits := strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(digits, ".")
	if whole == "" || strings.Trim(whole+frac, "0123456789") != "" {
		return nil, 0, fmt.Errorf("%q is not a decimal", s)
	}
	n, ok := new(big.Int).SetString(whole+frac, 10)
	if !ok {
		return nil, 0, fmt.Errorf("%q is not a decimal", s)
	}
	if neg {
		n.Neg(n)
	}
	return n, len(frac), nil
}

// formatDecimal writes an unscaled value with exactly scale digits after the
// point (§9.1.5).
func formatDecimal(n *big.Int, scale int) string {
	s := new(big.Int).Abs(n).String()
	if scale > 0 {
		for len(s) <= scale {
			s = "0" + s
		}
		s = s[:len(s)-scale] + "." + s[len(s)-scale:]
	}
	if n.Sign() < 0 {
		s = "-" + s
	}
	return s
}

// FormatDecimal writes an unscaled value at its scale, as exact decimals
// cross the wire in JSON.
func FormatDecimal(n *big.Int, scale int) string { return formatDecimal(n, scale) }

func resolutionFailed(format string, args ...any) *Problem {
	return NewProblem(CodeResolutionFailed, fmt.Sprintf(format, args...))
}
