package ijson

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Marshal writes a value compactly, members in their order, numbers as
// their text. It is the wire form; Canonical is the form for hashing.
func Marshal(v Value) ([]byte, error) {
	var sb strings.Builder
	if err := write(&sb, v, false); err != nil {
		return nil, err
	}
	return []byte(sb.String()), nil
}

// Canonical writes a value in the JSON Canonicalization Scheme (RFC 8785):
// members sorted by their names' UTF-16 code units, no whitespace, strings
// with only the escapes RFC 8785 requires, numbers in ECMAScript's shortest
// form (§9.4.2).
func Canonical(v Value) ([]byte, error) {
	var sb strings.Builder
	if err := write(&sb, v, true); err != nil {
		return nil, err
	}
	return []byte(sb.String()), nil
}

func write(sb *strings.Builder, v Value, canonical bool) error {
	switch x := v.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		sb.WriteString(strconv.FormatBool(x))
	case Number:
		if !canonical {
			sb.WriteString(string(x))
			return nil
		}
		f, err := x.Float64()
		if err != nil {
			return fmt.Errorf("ijson: %q is not a binary64 number: %w", x, err)
		}
		s, err := ECMAScriptNumber(f)
		if err != nil {
			return err
		}
		sb.WriteString(s)
	case string:
		writeString(sb, x)
	case []Value:
		sb.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				sb.WriteByte(',')
			}
			if err := write(sb, e, canonical); err != nil {
				return err
			}
		}
		sb.WriteByte(']')
	case *Object:
		names := x.names
		if canonical {
			names = slices.Clone(names)
			slices.SortFunc(names, compareUTF16)
		}
		sb.WriteByte('{')
		for i, n := range names {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeString(sb, n)
			sb.WriteByte(':')
			if err := write(sb, x.values[n], canonical); err != nil {
				return err
			}
		}
		sb.WriteByte('}')
	default:
		return fmt.Errorf("ijson: %T is not a JSON value", v)
	}
	return nil
}

// writeString writes a string with exactly the escapes RFC 8785 §3.2.2.2
// requires: the quotation mark, the reverse solidus, and the controls below
// U+0020 (with their short forms where JSON has one).
func writeString(sb *strings.Builder, s string) {
	const hex = "0123456789abcdef"
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			if r < 0x20 {
				sb.WriteString(`\u00`)
				sb.WriteByte(hex[r>>4])
				sb.WriteByte(hex[r&0xF])
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
}

// compareUTF16 orders strings by their UTF-16 code units, as RFC 8785
// §3.2.3 sorts member names.
func compareUTF16(a, b string) int {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return int(ua[i]) - int(ub[i])
		}
	}
	return len(ua) - len(ub)
}

// ECMAScriptNumber writes a binary64 value as ECMAScript's Number.prototype
// .toString does (ECMA-262 Number::toString, RFC 8785 §3.2.2.3): the shortest
// digits that round-trip, in decimal notation for exponents from −6 to 20 and
// exponent notation otherwise; −0 is "0". NaN and infinities are refused.
func ECMAScriptNumber(f float64) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("ijson: %v has no JSON form", f)
	}
	if f == 0 {
		return "0", nil
	}
	sign := ""
	if f < 0 {
		sign, f = "-", -f
	}
	// The shortest round-tripping digits d.ddd and exponent e: f = d.ddd × 10^e.
	e := strconv.FormatFloat(f, 'e', -1, 64)
	mant, exp, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	x, _ := strconv.Atoi(exp)
	k := len(digits)
	n := x + 1 // ECMAScript's n: the decimal point sits after n digits
	var s string
	switch {
	case k <= n && n <= 21:
		s = digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		s = digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		s = "0." + strings.Repeat("0", -n) + digits
	default:
		s = digits[:1]
		if k > 1 {
			s += "." + digits[1:]
		}
		if n-1 >= 0 {
			s += "e+" + strconv.Itoa(n-1)
		} else {
			s += "e-" + strconv.Itoa(-(n - 1))
		}
	}
	return sign + s, nil
}

// FromGo turns a Go value encoding/json can marshal into a Value, keeping
// struct fields in their declared order.
func FromGo(x any) (Value, error) {
	b, err := json.Marshal(x)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// MarshalJSON writes the object with its members in their order, so an
// *Object inside any Go value encoding/json marshals keeps its order.
func (o *Object) MarshalJSON() ([]byte, error) { return Marshal(o) }

// MarshalJSON writes the number as its own text, exactly.
func (n Number) MarshalJSON() ([]byte, error) {
	if _, err := Parse([]byte(n)); err != nil || !isNumber(string(n)) {
		return nil, fmt.Errorf("ijson: %q is not a JSON number", string(n))
	}
	return []byte(n), nil
}

func isNumber(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool { return !strings.ContainsRune("0123456789+-.eE", r) }) < 0
}
