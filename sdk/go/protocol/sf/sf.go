// Package sf is Structured Field Values for HTTP (RFC 9651), the grammar of
// every Atlas header (§9.6.3, D23). It parses and serialises Lists,
// Dictionaries and Items exactly as RFC 9651 §4 specifies, including Dates
// and Display Strings, so two languages never read one header differently.
//
// A bare item is one of: int64 (Integer), Decimal, string (String), Token,
// []byte (Byte Sequence), bool (Boolean), Date, DisplayString.
package sf

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Token is an sf-token, a bare identifier such as core or snowflake-arrow/1.
type Token string

// Date is an sf-date: seconds since the Unix epoch, written @1793318400.
type Date int64

// Decimal is an sf-decimal, held exactly in thousandths: 1.5 is 1500.
type Decimal int64

// DisplayString is an sf-displaystring: Unicode, written %"..." with
// percent-encoded UTF-8.
type DisplayString string

// Param is one parameter of an item or inner list.
type Param struct {
	Key   string
	Value any
}

// Params are an item's or inner list's parameters, in order.
type Params []Param

// Get returns a parameter's value.
func (ps Params) Get(key string) (any, bool) {
	for _, p := range ps {
		if p.Key == key {
			return p.Value, true
		}
	}
	return nil, false
}

// Item is a bare item with parameters.
type Item struct {
	Value  any
	Params Params
}

// InnerList is a parenthesised list of items with parameters.
type InnerList struct {
	Items  []Item
	Params Params
}

// Member is an Item or an InnerList: what a List or Dictionary holds.
type Member any

// List is an sf-list.
type List []Member

// DictMember is one member of a Dictionary.
type DictMember struct {
	Key   string
	Value Member
}

// Dictionary is an sf-dictionary, in order.
type Dictionary []DictMember

// Get returns a member's value.
func (d Dictionary) Get(key string) (Member, bool) {
	for _, m := range d {
		if m.Key == key {
			return m.Value, true
		}
	}
	return nil, false
}

// Error is a field value that does not parse.
type Error struct {
	Field  string
	Offset int
	Reason string
}

func (e *Error) Error() string {
	return fmt.Sprintf("structured field %q at %d: %s", e.Field, e.Offset, e.Reason)
}

type parser struct {
	s string
	i int
}

func (p *parser) fail(reason string) error { return &Error{p.s, p.i, reason} }

func newParser(field string) (*parser, error) {
	for i := 0; i < len(field); i++ {
		if field[i] > 0x7E || field[i] < 0x20 && field[i] != '\t' {
			return nil, &Error{field, i, "not visible ASCII"}
		}
	}
	p := &parser{s: field}
	p.sp()
	return p, nil
}

func (p *parser) done() error {
	p.sp()
	if p.i != len(p.s) {
		return p.fail("unexpected characters after the value")
	}
	return nil
}

// ParseItem parses an Item field (RFC 9651 §4.2.3).
func ParseItem(field string) (Item, error) {
	p, err := newParser(field)
	if err != nil {
		return Item{}, err
	}
	it, err := p.item()
	if err != nil {
		return Item{}, err
	}
	return it, p.done()
}

// ParseList parses a List field (RFC 9651 §4.2.1).
func ParseList(field string) (List, error) {
	p, err := newParser(field)
	if err != nil {
		return nil, err
	}
	var out List
	for p.i < len(p.s) {
		m, err := p.member()
		if err != nil {
			return nil, err
		}
		out = append(out, m)
		if end, err := p.next(); err != nil || end {
			return out, err
		}
	}
	return out, p.done()
}

// ParseDictionary parses a Dictionary field (RFC 9651 §4.2.2). A key seen
// twice keeps its first place and its last value.
func ParseDictionary(field string) (Dictionary, error) {
	p, err := newParser(field)
	if err != nil {
		return nil, err
	}
	var out Dictionary
	for p.i < len(p.s) {
		key, err := p.key()
		if err != nil {
			return nil, err
		}
		var m Member
		if p.i < len(p.s) && p.s[p.i] == '=' {
			p.i++
			if m, err = p.member(); err != nil {
				return nil, err
			}
		} else {
			ps, err := p.params()
			if err != nil {
				return nil, err
			}
			m = Item{Value: true, Params: ps}
		}
		replaced := false
		for i := range out {
			if out[i].Key == key {
				out[i].Value, replaced = m, true
			}
		}
		if !replaced {
			out = append(out, DictMember{key, m})
		}
		if end, err := p.next(); err != nil || end {
			return out, err
		}
	}
	return out, p.done()
}

// next consumes the separator after a list or dictionary member. It reports
// end when the input is exhausted.
func (p *parser) next() (end bool, err error) {
	p.ows()
	if p.i == len(p.s) {
		return true, nil
	}
	if p.s[p.i] != ',' {
		return false, p.fail("members are separated by ','")
	}
	p.i++
	p.ows()
	if p.i == len(p.s) {
		return false, p.fail("a trailing comma")
	}
	return false, nil
}

func (p *parser) sp() {
	for p.i < len(p.s) && p.s[p.i] == ' ' {
		p.i++
	}
}

func (p *parser) ows() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t') {
		p.i++
	}
}

func (p *parser) member() (Member, error) {
	if p.i < len(p.s) && p.s[p.i] == '(' {
		return p.innerList()
	}
	return p.item()
}

func (p *parser) innerList() (InnerList, error) {
	p.i++ // (
	var il InnerList
	for p.i < len(p.s) {
		p.sp()
		if p.i < len(p.s) && p.s[p.i] == ')' {
			p.i++
			ps, err := p.params()
			il.Params = ps
			return il, err
		}
		it, err := p.item()
		if err != nil {
			return il, err
		}
		il.Items = append(il.Items, it)
		if p.i >= len(p.s) || p.s[p.i] != ' ' && p.s[p.i] != ')' {
			return il, p.fail("inner-list items are separated by a space")
		}
	}
	return il, p.fail("an unterminated inner list")
}

func (p *parser) item() (Item, error) {
	v, err := p.bare()
	if err != nil {
		return Item{}, err
	}
	ps, err := p.params()
	return Item{Value: v, Params: ps}, err
}

func (p *parser) params() (Params, error) {
	var ps Params
	for p.i < len(p.s) && p.s[p.i] == ';' {
		p.i++
		p.sp()
		key, err := p.key()
		if err != nil {
			return nil, err
		}
		var v any = true
		if p.i < len(p.s) && p.s[p.i] == '=' {
			p.i++
			if v, err = p.bare(); err != nil {
				return nil, err
			}
		}
		replaced := false
		for i := range ps {
			if ps[i].Key == key {
				ps[i].Value, replaced = v, true
			}
		}
		if !replaced {
			ps = append(ps, Param{key, v})
		}
	}
	return ps, nil
}

// key = ( lcalpha / "*" ) *( lcalpha / DIGIT / "_" / "-" / "." / "*" )
func (p *parser) key() (string, error) {
	if p.i >= len(p.s) || !(lcalpha(p.s[p.i]) || p.s[p.i] == '*') {
		return "", p.fail("a key starts with a-z or *")
	}
	start := p.i
	for p.i < len(p.s) && keyChar(p.s[p.i]) {
		p.i++
	}
	return p.s[start:p.i], nil
}

func lcalpha(c byte) bool { return c >= 'a' && c <= 'z' }
func digit(c byte) bool   { return c >= '0' && c <= '9' }
func alpha(c byte) bool   { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func keyChar(c byte) bool {
	return lcalpha(c) || digit(c) || c == '_' || c == '-' || c == '.' || c == '*'
}

// tchar of RFC 9110, which an sf-token extends with ":" and "/".
func tchar(c byte) bool {
	return alpha(c) || digit(c) || strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
}

func (p *parser) bare() (any, error) {
	if p.i >= len(p.s) {
		return nil, p.fail("a value is missing")
	}
	switch c := p.s[p.i]; {
	case c == '-' || digit(c):
		return p.number()
	case c == '"':
		return p.str()
	case c == '*' || alpha(c):
		start := p.i
		p.i++
		for p.i < len(p.s) && (tchar(p.s[p.i]) || p.s[p.i] == ':' || p.s[p.i] == '/') {
			p.i++
		}
		return Token(p.s[start:p.i]), nil
	case c == ':':
		return p.bytes()
	case c == '?':
		if p.i+1 < len(p.s) && (p.s[p.i+1] == '0' || p.s[p.i+1] == '1') {
			p.i += 2
			return p.s[p.i-1] == '1', nil
		}
		return nil, p.fail("a boolean is ?0 or ?1")
	case c == '@':
		p.i++
		n, err := p.number()
		if err != nil {
			return nil, err
		}
		v, ok := n.(int64)
		if !ok {
			return nil, p.fail("a date is an integer")
		}
		return Date(v), nil
	case c == '%':
		return p.display()
	default:
		return nil, p.fail(fmt.Sprintf("%q starts no value", c))
	}
}

// number parses sf-integer or sf-decimal (RFC 9651 §4.2.4).
func (p *parser) number() (any, error) {
	neg := false
	if p.i < len(p.s) && p.s[p.i] == '-' {
		neg = true
		p.i++
	}
	if p.i >= len(p.s) || !digit(p.s[p.i]) {
		return nil, p.fail("a number needs a digit")
	}
	start := p.i
	point := -1
	for p.i < len(p.s) {
		c := p.s[p.i]
		if digit(c) {
			p.i++
		} else if c == '.' && point < 0 {
			if p.i-start > 12 {
				return nil, p.fail("a decimal has at most 12 integer digits")
			}
			point = p.i
			p.i++
		} else {
			break
		}
		if point < 0 && p.i-start > 15 {
			return nil, p.fail("an integer has at most 15 digits")
		}
		if point >= 0 && p.i-start > 16 {
			return nil, p.fail("a decimal has at most 16 characters")
		}
	}
	text := p.s[start:p.i]
	if point < 0 {
		v, _ := strconv.ParseInt(text, 10, 64)
		if neg {
			v = -v
		}
		return v, nil
	}
	intPart, frac := text[:point-start], text[point-start+1:]
	if frac == "" {
		return nil, p.fail("a decimal ends with a digit")
	}
	if len(frac) > 3 {
		return nil, p.fail("a decimal has at most 3 fraction digits")
	}
	v, _ := strconv.ParseInt(intPart+frac+strings.Repeat("0", 3-len(frac)), 10, 64)
	if neg {
		v = -v
	}
	return Decimal(v), nil
}

func (p *parser) str() (string, error) {
	p.i++ // "
	var sb strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		p.i++
		switch {
		case c == '\\':
			if p.i >= len(p.s) || p.s[p.i] != '"' && p.s[p.i] != '\\' {
				return "", p.fail(`a string escapes only " and \`)
			}
			sb.WriteByte(p.s[p.i])
			p.i++
		case c == '"':
			return sb.String(), nil
		case c < 0x20 || c > 0x7E:
			return "", p.fail("a string is visible ASCII")
		default:
			sb.WriteByte(c)
		}
	}
	return "", p.fail("an unterminated string")
}

func (p *parser) bytes() ([]byte, error) {
	p.i++ // :
	start := p.i
	for p.i < len(p.s) && p.s[p.i] != ':' {
		c := p.s[p.i]
		if !(alpha(c) || digit(c) || c == '+' || c == '/' || c == '=') {
			return nil, p.fail("a byte sequence is base64")
		}
		p.i++
	}
	if p.i >= len(p.s) {
		return nil, p.fail("an unterminated byte sequence")
	}
	b64 := p.s[start:p.i]
	p.i++
	// Padding may be missing, in part or in whole, and is synthesised (RFC
	// 9651 §4.2.7); an "=" beyond what base64 needs fails the decoding, as
	// RFC 4648 has it, and so the parse (Q-SF-1, assumption A-018).
	data := strings.TrimRight(b64, "=")
	need := (4 - len(data)%4) % 4
	if pad := len(b64) - len(data); pad > need {
		return nil, p.fail("a byte sequence has more \"=\" padding than base64 needs")
	}
	b, err := base64.StdEncoding.DecodeString(data + strings.Repeat("=", need))
	if err != nil {
		return nil, p.fail("a byte sequence is base64")
	}
	return b, nil
}

// display parses sf-displaystring (RFC 9651 §4.2.10).
func (p *parser) display() (DisplayString, error) {
	if p.i+1 >= len(p.s) || p.s[p.i+1] != '"' {
		return "", p.fail(`a display string starts %"`)
	}
	p.i += 2
	var b []byte
	for p.i < len(p.s) {
		c := p.s[p.i]
		p.i++
		switch {
		case c == '%':
			if p.i+2 > len(p.s) || !lowerHex(p.s[p.i]) || !lowerHex(p.s[p.i+1]) {
				return "", p.fail("a display string escape is % and two lowercase hex digits")
			}
			v, _ := strconv.ParseUint(p.s[p.i:p.i+2], 16, 8)
			b = append(b, byte(v))
			p.i += 2
		case c == '"':
			if !utf8.Valid(b) {
				return "", p.fail("a display string is UTF-8")
			}
			return DisplayString(b), nil
		case c < 0x20 || c > 0x7E:
			return "", p.fail("a display string is visible ASCII")
		default:
			b = append(b, c)
		}
	}
	return "", p.fail("an unterminated display string")
}

func lowerHex(c byte) bool { return digit(c) || c >= 'a' && c <= 'f' }

// Serialize writes an Item (RFC 9651 §4.1.3).
func (it Item) Serialize() (string, error) {
	var sb strings.Builder
	if err := it.write(&sb); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// Serialize writes a List (RFC 9651 §4.1.1).
func (l List) Serialize() (string, error) {
	var sb strings.Builder
	for i, m := range l {
		if i > 0 {
			sb.WriteString(", ")
		}
		if err := writeMember(&sb, m); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}

// Serialize writes a Dictionary (RFC 9651 §4.1.2).
func (d Dictionary) Serialize() (string, error) {
	var sb strings.Builder
	for i, m := range d {
		if i > 0 {
			sb.WriteString(", ")
		}
		if err := writeKey(&sb, m.Key); err != nil {
			return "", err
		}
		if it, ok := m.Value.(Item); ok && it.Value == true {
			if err := writeParams(&sb, it.Params); err != nil {
				return "", err
			}
			continue
		}
		sb.WriteByte('=')
		if err := writeMember(&sb, m.Value); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}

func writeMember(sb *strings.Builder, m Member) error {
	switch x := m.(type) {
	case Item:
		return x.write(sb)
	case InnerList:
		sb.WriteByte('(')
		for i, it := range x.Items {
			if i > 0 {
				sb.WriteByte(' ')
			}
			if err := it.write(sb); err != nil {
				return err
			}
		}
		sb.WriteByte(')')
		return writeParams(sb, x.Params)
	}
	return fmt.Errorf("sf: %T is not an item or an inner list", m)
}

func (it Item) write(sb *strings.Builder) error {
	if err := writeBare(sb, it.Value); err != nil {
		return err
	}
	return writeParams(sb, it.Params)
}

func writeParams(sb *strings.Builder, ps Params) error {
	for _, p := range ps {
		sb.WriteByte(';')
		if err := writeKey(sb, p.Key); err != nil {
			return err
		}
		if p.Value == true {
			continue
		}
		sb.WriteByte('=')
		if err := writeBare(sb, p.Value); err != nil {
			return err
		}
	}
	return nil
}

func writeKey(sb *strings.Builder, k string) error {
	if k == "" || !(lcalpha(k[0]) || k[0] == '*') {
		return fmt.Errorf("sf: %q is not a key", k)
	}
	for i := 1; i < len(k); i++ {
		if !keyChar(k[i]) {
			return fmt.Errorf("sf: %q is not a key", k)
		}
	}
	sb.WriteString(k)
	return nil
}

func writeBare(sb *strings.Builder, v any) error {
	switch x := v.(type) {
	case int64:
		return writeInt(sb, x)
	case int:
		return writeInt(sb, int64(x))
	case Decimal:
		if x <= -1e15 || x >= 1e15 {
			return errors.New("sf: a decimal has at most 12 integer digits")
		}
		neg := x < 0
		if neg {
			x = -x
			sb.WriteByte('-')
		}
		sb.WriteString(strconv.FormatInt(int64(x)/1000, 10))
		frac := strings.TrimRight(fmt.Sprintf("%03d", int64(x)%1000), "0")
		if frac == "" {
			frac = "0"
		}
		sb.WriteString("." + frac)
	case string:
		sb.WriteByte('"')
		for i := 0; i < len(x); i++ {
			c := x[i]
			if c < 0x20 || c > 0x7E {
				return fmt.Errorf("sf: %q is not visible ASCII", x)
			}
			if c == '"' || c == '\\' {
				sb.WriteByte('\\')
			}
			sb.WriteByte(c)
		}
		sb.WriteByte('"')
	case Token:
		if x == "" || !(alpha(x[0]) || x[0] == '*') {
			return fmt.Errorf("sf: %q is not a token", x)
		}
		for i := 1; i < len(x); i++ {
			if !(tchar(x[i]) || x[i] == ':' || x[i] == '/') {
				return fmt.Errorf("sf: %q is not a token", x)
			}
		}
		sb.WriteString(string(x))
	case []byte:
		sb.WriteString(":" + base64.StdEncoding.EncodeToString(x) + ":")
	case bool:
		if x {
			sb.WriteString("?1")
		} else {
			sb.WriteString("?0")
		}
	case Date:
		sb.WriteByte('@')
		return writeInt(sb, int64(x))
	case DisplayString:
		if !utf8.ValidString(string(x)) {
			return errors.New("sf: a display string is UTF-8")
		}
		const hex = "0123456789abcdef"
		sb.WriteString(`%"`)
		for i := 0; i < len(x); i++ {
			c := x[i]
			if c == '%' || c == '"' || c < 0x20 || c > 0x7E {
				sb.WriteByte('%')
				sb.WriteByte(hex[c>>4])
				sb.WriteByte(hex[c&0xF])
			} else {
				sb.WriteByte(c)
			}
		}
		sb.WriteByte('"')
	default:
		return fmt.Errorf("sf: %T is not a bare item", v)
	}
	return nil
}

func writeInt(sb *strings.Builder, v int64) error {
	if v < -999_999_999_999_999 || v > 999_999_999_999_999 {
		return errors.New("sf: an integer has at most 15 digits")
	}
	sb.WriteString(strconv.FormatInt(v, 10))
	return nil
}
