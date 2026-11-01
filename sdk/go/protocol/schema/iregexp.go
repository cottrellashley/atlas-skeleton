package schema

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxRepeat is the largest count a quantifier may give: Go's engine refuses
// more, so the gate refuses it for every client (assumption A-013).
const maxRepeat = 1000

// compilePattern checks that a pattern is ^R$ with R an I-Regexp (RFC 9485,
// §9.2.4), and compiles it to match a whole value's code points.
func compilePattern(pattern string) (*regexp.Regexp, error) {
	if !utf8.ValidString(pattern) {
		return nil, fmt.Errorf("a pattern is UTF-8")
	}
	inner, ok := strings.CutPrefix(pattern, "^")
	if ok {
		inner, ok = strings.CutSuffix(inner, "$")
	}
	if !ok {
		return nil, fmt.Errorf("a pattern has the form ^R$")
	}
	re2, err := translate(inner)
	if err != nil {
		return nil, err
	}
	return regexp.Compile(`\A(?:` + re2 + `)\z`)
}

// translate parses an I-Regexp and writes it in Go's syntax, as RFC 9485
// §5.3 describes for engines of its kind: "." becomes [^\n\r], and every
// literal is written as a code point so that nothing in it is special.
func translate(r string) (string, error) {
	p := &irx{s: []rune(r)}
	if err := p.regexp(); err != nil {
		return "", err
	}
	if p.i != len(p.s) {
		return "", p.fail("an unmatched )")
	}
	return p.out.String(), nil
}

type irx struct {
	s   []rune
	i   int
	out strings.Builder
}

func (p *irx) fail(why string) error {
	return fmt.Errorf("not an I-Regexp at character %d: %s", p.i, why)
}

func (p *irx) more() bool { return p.i < len(p.s) }

// i-regexp = branch *( "|" branch )
func (p *irx) regexp() error {
	if err := p.branch(); err != nil {
		return err
	}
	for p.more() && p.s[p.i] == '|' {
		p.i++
		p.out.WriteByte('|')
		if err := p.branch(); err != nil {
			return err
		}
	}
	return nil
}

// branch = *piece
func (p *irx) branch() error {
	for p.more() && p.s[p.i] != '|' && p.s[p.i] != ')' {
		if err := p.piece(); err != nil {
			return err
		}
	}
	return nil
}

// piece = atom [ quantifier ]
func (p *irx) piece() error {
	if err := p.atom(); err != nil {
		return err
	}
	if !p.more() {
		return nil
	}
	switch c := p.s[p.i]; c {
	case '*', '+', '?':
		p.i++
		p.out.WriteRune(c)
	case '{':
		return p.rangeQuantifier()
	}
	return nil
}

// range-quantifier = "{" QuantExact [ "," [ QuantExact ] ] "}"
func (p *irx) rangeQuantifier() error {
	p.i++
	lo, ok, err := p.quantExact()
	if err != nil {
		return err
	}
	if !ok {
		return p.fail("a quantifier needs a count")
	}
	text := strconv.Itoa(lo)
	if p.more() && p.s[p.i] == ',' {
		p.i++
		text += ","
		hi, ok, err := p.quantExact()
		if err != nil {
			return err
		}
		if ok {
			if hi < lo {
				return p.fail("a quantifier's maximum is below its minimum")
			}
			text += strconv.Itoa(hi)
		}
	}
	if !p.more() || p.s[p.i] != '}' {
		return p.fail("an unterminated quantifier")
	}
	p.i++
	p.out.WriteString("{" + text + "}")
	return nil
}

func (p *irx) quantExact() (int, bool, error) {
	start := p.i
	for p.more() && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
		p.i++
	}
	if p.i == start {
		return 0, false, nil
	}
	n, err := strconv.Atoi(string(p.s[start:p.i]))
	if err != nil || n > maxRepeat {
		p.i = start
		return 0, false, p.fail(fmt.Sprintf("a quantifier counts at most %d", maxRepeat))
	}
	return n, true, nil
}

// atom = NormalChar / charClass / ( "(" i-regexp ")" )
func (p *irx) atom() error {
	if !p.more() {
		return p.fail("an atom is missing")
	}
	switch c := p.s[p.i]; {
	case c == '(':
		p.i++
		p.out.WriteString("(?:")
		if err := p.regexp(); err != nil {
			return err
		}
		if !p.more() || p.s[p.i] != ')' {
			return p.fail("an unterminated group")
		}
		p.i++
		p.out.WriteByte(')')
	case c == '.':
		p.i++
		p.out.WriteString(`[^\n\r]`)
	case c == '\\':
		lit, class, err := p.escape()
		if err != nil {
			return err
		}
		if class != "" {
			p.out.WriteString(class)
		} else {
			writeLiteral(&p.out, lit)
		}
	case c == '[':
		return p.charClassExpr()
	case normalChar(c):
		p.i++
		writeLiteral(&p.out, c)
	default:
		return p.fail(fmt.Sprintf("%q must be escaped", c))
	}
	return nil
}

// NormalChar is any code point but ( ) * + . ? [ \ ] { | } and the
// surrogates.
func normalChar(c rune) bool {
	return !strings.ContainsRune(`()*+.?[\]{|}`, c) && !(c >= 0xD800 && c <= 0xDFFF)
}

// escape reads SingleCharEsc, catEsc or complEsc after a backslash. It
// returns a literal, or a class in Go's syntax.
func (p *irx) escape() (lit rune, class string, err error) {
	p.i++ // \
	if !p.more() {
		return 0, "", p.fail("a trailing backslash")
	}
	c := p.s[p.i]
	p.i++
	switch {
	case c == 'n':
		return '\n', "", nil
	case c == 'r':
		return '\r', "", nil
	case c == 't':
		return '\t', "", nil
	case strings.ContainsRune(`()*+-.?[\]^{|}`, c):
		return c, "", nil
	case c == 'p' || c == 'P':
		if !p.more() || p.s[p.i] != '{' {
			return 0, "", p.fail(`\p and \P take {category}`)
		}
		end := p.i + 1
		for end < len(p.s) && p.s[end] != '}' {
			end++
		}
		if end >= len(p.s) {
			return 0, "", p.fail("an unterminated category")
		}
		name := string(p.s[p.i+1 : end])
		p.i = end + 1
		if !category(name) {
			return 0, "", p.fail(fmt.Sprintf("%q is not a general category of RFC 9485", name))
		}
		return 0, `\` + string(c) + "{" + name + "}", nil
	}
	return 0, "", p.fail(fmt.Sprintf(`\%c is not an I-Regexp escape`, c))
}

// category reports whether name is an IsCategory of RFC 9485. C and Cn are
// refused: their code points are the unassigned ones, which change with
// every Unicode version, so no two engines agree on them (assumption A-013).
func category(name string) bool {
	switch name {
	case "L", "Ll", "Lm", "Lo", "Lt", "Lu",
		"M", "Mc", "Me", "Mn",
		"N", "Nd", "Nl", "No",
		"P", "Pc", "Pd", "Pe", "Pf", "Pi", "Po", "Ps",
		"Z", "Zl", "Zp", "Zs",
		"S", "Sc", "Sk", "Sm", "So",
		"Cc", "Cf", "Co":
		return true
	}
	return false
}

// charClassExpr = "[" [ "^" ] ( "-" / CCE1 ) *CCE1 [ "-" ] "]"
func (p *irx) charClassExpr() error {
	p.i++ // [
	p.out.WriteByte('[')
	if p.more() && p.s[p.i] == '^' {
		p.i++
		p.out.WriteByte('^')
	}
	first := true
	for {
		if !p.more() {
			return p.fail("an unterminated class")
		}
		c := p.s[p.i]
		if c == ']' && !first {
			p.i++
			p.out.WriteByte(']')
			return nil
		}
		if c == '-' {
			// A literal "-" only first, or last before "]".
			if first || p.i+1 < len(p.s) && p.s[p.i+1] == ']' {
				p.i++
				writeLiteral(&p.out, '-')
				first = false
				continue
			}
			return p.fail(`"-" in a class is first, last or in a range`)
		}
		if err := p.cce1(); err != nil {
			return err
		}
		first = false
	}
}

// CCE1 = ( CCchar [ "-" CCchar ] ) / charClassEsc
func (p *irx) cce1() error {
	lo, class, err := p.ccChar()
	if err != nil {
		return err
	}
	if class != "" {
		p.out.WriteString(class)
		return nil
	}
	if p.i+1 < len(p.s) && p.s[p.i] == '-' && p.s[p.i+1] != ']' {
		p.i++
		hi, class, err := p.ccChar()
		if err != nil {
			return err
		}
		if class != "" {
			return p.fail("a range ends in a character, not a category")
		}
		if hi < lo {
			return p.fail("a range's end is below its start")
		}
		writeLiteral(&p.out, lo)
		p.out.WriteByte('-')
		writeLiteral(&p.out, hi)
		return nil
	}
	writeLiteral(&p.out, lo)
	return nil
}

// CCchar = any code point but - [ \ ] and the surrogates, or SingleCharEsc.
func (p *irx) ccChar() (rune, string, error) {
	c := p.s[p.i]
	switch {
	case c == '\\':
		return p.escape()
	case c == '-' || c == '[' || c == ']' || c >= 0xD800 && c <= 0xDFFF:
		return 0, "", p.fail(fmt.Sprintf("%q in a class must be escaped", c))
	}
	p.i++
	return c, "", nil
}

func writeLiteral(b *strings.Builder, c rune) {
	fmt.Fprintf(b, `\x{%X}`, c)
}
