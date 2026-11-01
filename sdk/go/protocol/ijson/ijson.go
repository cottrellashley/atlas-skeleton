// Package ijson is JSON as the Atlas Protocol requires it (§9.1.2, RFC 7493,
// RFC 8259).
//
//   - Parse refuses what I-JSON refuses: duplicate member names at any depth,
//     invalid UTF-8, unpaired surrogates and noncharacters (escaped or not),
//     a byte-order mark, a number beyond binary64, and trailing data.
//   - An Object keeps its members' order. A schema's properties order is the
//     Arrow field order (§9.2.7), so order is never thrown away.
//   - A Number keeps the exact text it was written with.
//   - Canonical writes RFC 8785, the form fingerprints are taken of (§9.4.2).
//
// A Value is one of: nil (null), bool, Number, string, []Value, *Object.
package ijson

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Value is a JSON value: nil, bool, Number, string, []Value or *Object.
type Value = any

// Number is a JSON number, as its exact text.
type Number string

// Float64 is the number as an IEEE 754 binary64 value, rounded to nearest.
func (n Number) Float64() (float64, error) {
	return strconv.ParseFloat(string(n), 64)
}

// Rat is the number's exact value.
func (n Number) Rat() *big.Rat {
	r, ok := new(big.Rat).SetString(string(n))
	if !ok {
		return nil
	}
	return r
}

// IsInteger reports whether the number's binary64 value has no fractional
// part, as JSON Schema's integer means (1.0 is an integer).
func (n Number) IsInteger() bool {
	f, err := n.Float64()
	return err == nil && f == math.Trunc(f) && !math.IsInf(f, 0)
}

// Int64 is the number as an int64, when its binary64 value is an integer
// that fits.
func (n Number) Int64() (int64, bool) {
	f, err := n.Float64()
	if err != nil || f != math.Trunc(f) || f < -(1<<63) || f >= 1<<63 {
		return 0, false
	}
	return int64(f), true
}

// MaxSafeInteger is the largest integer I-JSON carries as a number: 2^53 − 1.
const MaxSafeInteger = 1<<53 - 1

// SafeInteger reports whether the number is an integer within
// [−(2^53)+1, (2^53)−1] (§9.1.2 rule 4).
func (n Number) SafeInteger() bool {
	v, ok := n.Int64()
	return ok && v >= -MaxSafeInteger && v <= MaxSafeInteger
}

// Object is a JSON object whose members keep their order.
type Object struct {
	names  []string
	values map[string]Value
}

// NewObject returns an empty object.
func NewObject() *Object { return &Object{values: map[string]Value{}} }

// Len is the number of members.
func (o *Object) Len() int { return len(o.names) }

// Get returns a member's value.
func (o *Object) Get(name string) (Value, bool) {
	v, ok := o.values[name]
	return v, ok
}

// Set adds a member at the end, or replaces its value in place.
func (o *Object) Set(name string, v Value) {
	if _, ok := o.values[name]; !ok {
		o.names = append(o.names, name)
	}
	o.values[name] = v
}

// Delete removes a member, reporting whether it was there.
func (o *Object) Delete(name string) bool {
	if _, ok := o.values[name]; !ok {
		return false
	}
	delete(o.values, name)
	for i, n := range o.names {
		if n == name {
			o.names = append(o.names[:i], o.names[i+1:]...)
			break
		}
	}
	return true
}

// Names are the members' names, in order. The slice is the caller's.
func (o *Object) Names() []string { return append([]string(nil), o.names...) }

// Each calls fn for every member, in order, until fn returns false.
func (o *Object) Each(fn func(name string, v Value) bool) {
	for _, n := range o.names {
		if !fn(n, o.values[n]) {
			return
		}
	}
}

// Clone copies a value deeply.
func Clone(v Value) Value {
	switch x := v.(type) {
	case []Value:
		out := make([]Value, len(x))
		for i, e := range x {
			out[i] = Clone(e)
		}
		return out
	case *Object:
		out := &Object{names: append([]string(nil), x.names...), values: make(map[string]Value, len(x.values))}
		for k, e := range x.values {
			out.values[k] = Clone(e)
		}
		return out
	default:
		return v
	}
}

// Equal reports whether two values are equal as JSON values: numbers by
// their binary64 value, as I-JSON reads them (1 equals 1.0, and 0.1 equals
// 0.10000000000000000001), objects regardless of member order, arrays
// element by element. It is JSON Schema's equality under I-JSON.
func Equal(a, b Value) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	case Number:
		y, ok := b.(Number)
		if !ok {
			return false
		}
		fx, ex := x.Float64()
		fy, ey := y.Float64()
		return ex == nil && ey == nil && fx == fy
	case []Value:
		y, ok := b.([]Value)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !Equal(x[i], y[i]) {
				return false
			}
		}
		return true
	case *Object:
		y, ok := b.(*Object)
		if !ok || x.Len() != y.Len() {
			return false
		}
		for _, n := range x.names {
			yv, ok := y.values[n]
			if !ok || !Equal(x.values[n], yv) {
				return false
			}
		}
		return true
	}
	return false
}

// SyntaxError is why a document is not I-JSON, and where.
type SyntaxError struct {
	Offset int
	Reason string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("not I-JSON at byte %d: %s", e.Offset, e.Reason)
}

// Limits bound a document, so a hostile one cannot exhaust a stack or memory.
type Limits struct {
	MaxDepth int // nesting of arrays and objects; 0 means 128
	MaxBytes int // 0 means no limit beyond the caller's
}

// Parse reads one whole I-JSON document.
func Parse(b []byte) (Value, error) { return ParseLimits(b, Limits{}) }

// ParseLimits reads one whole I-JSON document within limits.
func ParseLimits(b []byte, l Limits) (Value, error) {
	if l.MaxDepth == 0 {
		l.MaxDepth = 128
	}
	if l.MaxBytes > 0 && len(b) > l.MaxBytes {
		return nil, &SyntaxError{0, fmt.Sprintf("larger than %d bytes", l.MaxBytes)}
	}
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return nil, &SyntaxError{0, "a byte-order mark"}
	}
	p := &parser{b: b, max: l.MaxDepth}
	p.space()
	v, err := p.value(0)
	if err != nil {
		return nil, err
	}
	p.space()
	if p.i != len(p.b) {
		return nil, p.fail("data after the document")
	}
	return v, nil
}

type parser struct {
	b   []byte
	i   int
	max int
}

func (p *parser) fail(reason string) error { return &SyntaxError{p.i, reason} }

func (p *parser) space() {
	for p.i < len(p.b) {
		switch p.b[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) value(depth int) (Value, error) {
	if p.i >= len(p.b) {
		return nil, p.fail("unexpected end")
	}
	switch c := p.b[p.i]; {
	case c == '{':
		return p.object(depth + 1)
	case c == '[':
		return p.array(depth + 1)
	case c == '"':
		return p.string()
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	case p.literal("true"):
		return true, nil
	case p.literal("false"):
		return false, nil
	case p.literal("null"):
		return nil, nil
	default:
		return nil, p.fail(fmt.Sprintf("unexpected %q", c))
	}
}

func (p *parser) literal(word string) bool {
	if strings.HasPrefix(string(p.b[p.i:min(len(p.b), p.i+len(word))]), word) {
		p.i += len(word)
		return true
	}
	return false
}

func (p *parser) object(depth int) (Value, error) {
	if depth > p.max {
		return nil, p.fail(fmt.Sprintf("nested deeper than %d", p.max))
	}
	p.i++ // {
	o := NewObject()
	p.space()
	if p.i < len(p.b) && p.b[p.i] == '}' {
		p.i++
		return o, nil
	}
	for {
		p.space()
		if p.i >= len(p.b) || p.b[p.i] != '"' {
			return nil, p.fail("a member name must be a string")
		}
		at := p.i
		name, err := p.string()
		if err != nil {
			return nil, err
		}
		if _, dup := o.values[name]; dup {
			return nil, &SyntaxError{at, fmt.Sprintf("duplicate member %q", name)}
		}
		p.space()
		if p.i >= len(p.b) || p.b[p.i] != ':' {
			return nil, p.fail("a member name is followed by ':'")
		}
		p.i++
		p.space()
		v, err := p.value(depth)
		if err != nil {
			return nil, err
		}
		o.Set(name, v)
		p.space()
		if p.i >= len(p.b) {
			return nil, p.fail("unexpected end in an object")
		}
		switch p.b[p.i] {
		case ',':
			p.i++
		case '}':
			p.i++
			return o, nil
		default:
			return nil, p.fail("members are separated by ','")
		}
	}
}

func (p *parser) array(depth int) (Value, error) {
	if depth > p.max {
		return nil, p.fail(fmt.Sprintf("nested deeper than %d", p.max))
	}
	p.i++ // [
	out := []Value{}
	p.space()
	if p.i < len(p.b) && p.b[p.i] == ']' {
		p.i++
		return out, nil
	}
	for {
		p.space()
		v, err := p.value(depth)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.space()
		if p.i >= len(p.b) {
			return nil, p.fail("unexpected end in an array")
		}
		switch p.b[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			return out, nil
		default:
			return nil, p.fail("elements are separated by ','")
		}
	}
}

func (p *parser) number() (Value, error) {
	start := p.i
	if p.b[p.i] == '-' {
		p.i++
	}
	switch {
	case p.i < len(p.b) && p.b[p.i] == '0':
		p.i++
	case p.i < len(p.b) && p.b[p.i] >= '1' && p.b[p.i] <= '9':
		for p.i < len(p.b) && isDigit(p.b[p.i]) {
			p.i++
		}
	default:
		return nil, p.fail("a number needs digits")
	}
	if p.i < len(p.b) && p.b[p.i] == '.' {
		p.i++
		if p.i >= len(p.b) || !isDigit(p.b[p.i]) {
			return nil, p.fail("a fraction needs digits")
		}
		for p.i < len(p.b) && isDigit(p.b[p.i]) {
			p.i++
		}
	}
	if p.i < len(p.b) && (p.b[p.i] == 'e' || p.b[p.i] == 'E') {
		p.i++
		if p.i < len(p.b) && (p.b[p.i] == '+' || p.b[p.i] == '-') {
			p.i++
		}
		if p.i >= len(p.b) || !isDigit(p.b[p.i]) {
			return nil, p.fail("an exponent needs digits")
		}
		digits := p.i
		for p.i < len(p.b) && isDigit(p.b[p.i]) {
			p.i++
		}
		// binary64 spans about 10^±308: a longer exponent can only mean zero or
		// infinity, and exact arithmetic on it would cost without bound.
		if p.i-digits > 4 {
			return nil, &SyntaxError{start, "an exponent beyond binary64"}
		}
	}
	n := Number(p.b[start:p.i])
	f, err := n.Float64()
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return nil, &SyntaxError{start, "not a number"}
	}
	if math.IsInf(f, 0) {
		return nil, &SyntaxError{start, "a number beyond binary64"}
	}
	return n, nil
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func (p *parser) string() (string, error) {
	p.i++ // "
	var sb strings.Builder
	for {
		if p.i >= len(p.b) {
			return "", p.fail("unterminated string")
		}
		c := p.b[p.i]
		switch {
		case c == '"':
			p.i++
			return sb.String(), nil
		case c == '\\':
			r, err := p.escape()
			if err != nil {
				return "", err
			}
			sb.WriteRune(r)
		case c < 0x20:
			return "", p.fail("an unescaped control character in a string")
		case c < utf8.RuneSelf:
			sb.WriteByte(c)
			p.i++
		default:
			r, size := utf8.DecodeRune(p.b[p.i:])
			if r == utf8.RuneError && size <= 1 {
				return "", p.fail("invalid UTF-8")
			}
			if noncharacter(r) {
				return "", p.fail(fmt.Sprintf("the noncharacter U+%04X", r))
			}
			sb.WriteRune(r)
			p.i += size
		}
	}
}

func (p *parser) escape() (rune, error) {
	if p.i+1 >= len(p.b) {
		return 0, p.fail("unterminated escape")
	}
	c := p.b[p.i+1]
	p.i += 2
	switch c {
	case '"':
		return '"', nil
	case '\\':
		return '\\', nil
	case '/':
		return '/', nil
	case 'b':
		return '\b', nil
	case 'f':
		return '\f', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 't':
		return '\t', nil
	case 'u':
		hi, err := p.hex4()
		if err != nil {
			return 0, err
		}
		r := rune(hi)
		switch {
		case utf16.IsSurrogate(r) && r < 0xDC00: // a high surrogate: a low one must follow
			if p.i+1 >= len(p.b) || p.b[p.i] != '\\' || p.b[p.i+1] != 'u' {
				return 0, p.fail("an unpaired surrogate")
			}
			p.i += 2
			lo, err := p.hex4()
			if err != nil {
				return 0, err
			}
			r = utf16.DecodeRune(r, rune(lo))
			if r == utf8.RuneError {
				return 0, p.fail("an unpaired surrogate")
			}
		case utf16.IsSurrogate(r):
			return 0, p.fail("an unpaired surrogate")
		}
		if noncharacter(r) {
			return 0, p.fail(fmt.Sprintf("the noncharacter U+%04X", r))
		}
		return r, nil
	}
	return 0, p.fail(fmt.Sprintf("the unknown escape \\%c", c))
}

func (p *parser) hex4() (uint16, error) {
	if p.i+4 > len(p.b) {
		return 0, p.fail("a \\u escape needs four hexadecimal digits")
	}
	v, err := strconv.ParseUint(string(p.b[p.i:p.i+4]), 16, 16)
	if err != nil {
		return 0, p.fail("a \\u escape needs four hexadecimal digits")
	}
	p.i += 4
	return uint16(v), nil
}

// noncharacter reports Unicode's noncharacters: U+FDD0–U+FDEF and the last two
// code points of every plane (RFC 7493 §2.1).
func noncharacter(r rune) bool {
	return r >= 0xFDD0 && r <= 0xFDEF || r&0xFFFE == 0xFFFE
}
