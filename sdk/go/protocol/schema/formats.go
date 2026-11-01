package schema

import (
	"fmt"
	"math/big"
	"net/netip"
	"strconv"
	"strings"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
)

// formats are the formats of §9.2.2. Each is an assertion.
var formats = map[string]bool{
	"date": true, "date-time": true, "duration": true, "decimal": true, "int64": true,
	"uri": true, "uuid": true,
	"capability-id": true, "pinned-id": true, "exact-id": true, "address": true,
}

var (
	minInt64 = big.NewInt(-1 << 63)
	maxInt64 = big.NewInt(1<<63 - 1)
)

// The longest int64 and the most significant digits of any number a
// duration within the grammar's range holds (R049). The grammar reads
// both with big.Int, whose time grows with the square of a number's
// digits, so a longer one is refused before it is read: validation stays
// linear in a string a request may make a megabyte long.
const (
	maxInt64Length   = len("-9223372036854775808")
	maxDurationDigit = 19 // 10^19 nanoseconds is past time.Duration
)

// checkFormat reports why s does not match a format, or "". A decimal is
// held to the precision and scale of the property's Decimal(p,s), if any.
func checkFormat(format, s string, decimal *logical) string {
	var err error
	switch format {
	case "date":
		_, err = grammar.ParseDate(s)
	case "date-time":
		_, err = grammar.ParseDateTime(s)
	case "duration":
		if significantDigits(s) > maxDurationDigit {
			return fmt.Sprintf("%s holds a number of more than %d significant digits, past the longest duration", describe(s), maxDurationDigit)
		}
		_, err = grammar.ParseDuration(s)
	case "decimal":
		var d grammar.Decimal
		if d, err = grammar.ParseDecimal(s); err == nil && decimal != nil && !d.Fits(decimal.precision, decimal.scale) {
			return fmt.Sprintf("%s does not fit Decimal(%d,%d)", describe(s), decimal.precision, decimal.scale)
		}
	case "int64":
		if len(s) > maxInt64Length {
			return fmt.Sprintf("%s has more than %d characters, which no signed 64-bit integer has", describe(s), maxInt64Length)
		}
		var n *big.Int
		if n, err = grammar.ParseIntegerString(s); err == nil && (n.Cmp(minInt64) < 0 || n.Cmp(maxInt64) > 0) {
			return fmt.Sprintf("%q is outside a signed 64-bit integer", s)
		}
	case "uri":
		err = checkURI(s)
	case "uuid":
		err = checkUUID(s)
	case "capability-id":
		err = grammar.CapabilityID(s)
	case "pinned-id":
		_, err = grammar.ParsePinned(s)
	case "exact-id":
		_, err = grammar.ParseExact(s)
	case "address":
		_, err = grammar.ParseAddress(s)
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// significantDigits is the most digits, leading zeros aside, of any run
// of digits in s.
func significantDigits(s string) int {
	most, run := 0, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '0' && run == 0: // a leading zero
		case digit(c):
			run++
			most = max(most, run)
		default:
			run = 0
		}
	}
	return most
}

// checkUUID accepts a lowercase RFC 9562 UUID: 8-4-4-4-12 lowercase hex.
func checkUUID(s string) error {
	if len(s) != 36 {
		return fmt.Errorf("%s is not a lowercase UUID", describe(s))
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return fmt.Errorf("%s is not a lowercase UUID", describe(s))
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return fmt.Errorf("%s is not a lowercase UUID", describe(s))
		}
	}
	return nil
}

// checkURI accepts a URI by RFC 3986 §3: a scheme, then a hierarchical
// part, an optional query and an optional fragment (assumption A-010).
func checkURI(s string) error {
	bad := func(why string) error { return fmt.Errorf("%s is not an absolute URI: %s", describe(s), why) }
	scheme, rest, ok := strings.Cut(s, ":")
	if !ok || scheme == "" || !alpha(scheme[0]) {
		return bad("it has no scheme")
	}
	for i := 1; i < len(scheme); i++ {
		if c := scheme[i]; !(alpha(c) || digit(c) || c == '+' || c == '-' || c == '.') {
			return bad("its scheme holds " + strconv.QuoteRune(rune(c)))
		}
	}
	rest, fragment, hasFragment := strings.Cut(rest, "#")
	if hasFragment && !uriChars(fragment, "/?") {
		return bad("its fragment")
	}
	rest, query, hasQuery := strings.Cut(rest, "?")
	if hasQuery && !uriChars(query, "/?") {
		return bad("its query")
	}
	if after, ok := strings.CutPrefix(rest, "//"); ok {
		authority, path, _ := strings.Cut(after, "/")
		if err := checkAuthority(authority); err != nil {
			return bad(err.Error())
		}
		rest = path
	}
	if !uriChars(rest, "/") {
		return bad("its path")
	}
	return nil
}

func checkAuthority(a string) error {
	if userinfo, host, ok := strings.Cut(a, "@"); ok {
		if !uriCharsExcept(userinfo, ":", "@") {
			return fmt.Errorf("its userinfo")
		}
		a = host
	}
	host, port := a, ""
	if strings.HasPrefix(a, "[") {
		end := strings.IndexByte(a, ']')
		if end < 0 {
			return fmt.Errorf("an unterminated IP literal")
		}
		lit := a[1:end]
		if strings.HasPrefix(lit, "v") || strings.HasPrefix(lit, "V") {
			ver, rest, ok := strings.Cut(lit[1:], ".")
			if !ok || ver == "" || !hex(ver) || rest == "" || !uriCharsExcept(rest, ":", "@/?%") {
				return fmt.Errorf("its IPvFuture literal")
			}
		} else if ip, err := netip.ParseAddr(lit); err != nil || !ip.Is6() || ip.Zone() != "" {
			return fmt.Errorf("its IPv6 literal")
		}
		host, port = "", a[end+1:]
		if port != "" {
			if port[0] != ':' {
				return fmt.Errorf("data after its IP literal")
			}
			port = port[1:]
		}
	} else if h, p, ok := strings.Cut(a, ":"); ok {
		host, port = h, p
	}
	if !uriCharsExcept(host, "", ":@") {
		return fmt.Errorf("its host")
	}
	for i := 0; i < len(port); i++ {
		if !digit(port[i]) {
			return fmt.Errorf("its port")
		}
	}
	return nil
}

// uriChars reports whether s is pchar and the extra characters, with valid
// percent-encodings.
func uriChars(s, extra string) bool { return uriCharsExcept(s, extra+":@", "") }

// uriCharsExcept reports whether s holds only unreserved, sub-delims,
// percent-encodings and the extra characters, and none of except.
func uriCharsExcept(s, extra, except string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case strings.IndexByte(except, c) >= 0:
			return false
		case c == '%':
			if i+2 >= len(s) || !hex(s[i+1:i+3]) {
				return false
			}
			i += 2
		case alpha(c) || digit(c) || strings.IndexByte("-._~!$&'()*+,;=", c) >= 0 || strings.IndexByte(extra, c) >= 0:
		default:
			return false
		}
	}
	return true
}

func alpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func digit(c byte) bool { return c >= '0' && c <= '9' }

func hex(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(digit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return s != ""
}
