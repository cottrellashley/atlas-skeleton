// Package grammar is §9.1.3–9.1.5 of the Atlas Protocol: the exact grammar of
// every identifier, version, address, fingerprint, protocol version, date,
// time, duration, decimal and large integer that crosses the wire. Every
// grammar is case-sensitive; a value that does not match is refused.
package grammar

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// Limits on identifiers (§9.1.3).
const (
	MaxIDLength      = 128
	MaxAddressLength = 160
)

// Pinned is a capability version: portfolio.holdings@v1.
type Pinned struct {
	ID      string
	Version int
}

func (p Pinned) String() string { return p.ID + "@v" + strconv.Itoa(p.Version) }

// Exact is one exact contract: portfolio.holdings@v1#3f9a1c0b7d2e4a65.
type Exact struct {
	Pinned
	Fingerprint string
}

func (e Exact) String() string { return e.Pinned.String() + "#" + e.Fingerprint }

// Address is what every call resolves to: an id, pinned or bare, and an
// interaction. Version is 0 for a bare id.
type Address struct {
	ID          string
	Version     int
	Interaction string
}

// Pinned reports the address's capability version, when it names one.
func (a Address) Pinned() (Pinned, bool) {
	return Pinned{a.ID, a.Version}, a.Version > 0
}

func (a Address) String() string {
	if a.Version > 0 {
		return Pinned{a.ID, a.Version}.String() + ":" + a.Interaction
	}
	return a.ID + ":" + a.Interaction
}

// Error is a value that does not match its grammar.
type Error struct {
	Rule, Value, Reason string
}

func (e *Error) Error() string { return fmt.Sprintf("%q is not a %s: %s", e.Value, e.Rule, e.Reason) }

func fail(rule, v, reason string) error { return &Error{rule, v, reason} }

// segment = %x61-7A *( %x61-7A / DIGIT / "-" ); never ending with "-" nor containing "--".
func segment(s string) string {
	if s == "" {
		return "an empty segment"
	}
	if s[0] < 'a' || s[0] > 'z' {
		return "a segment starts with a-z"
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return fmt.Sprintf("%q is not allowed in a segment", c)
		}
	}
	if s[len(s)-1] == '-' {
		return "a segment never ends with '-'"
	}
	if strings.Contains(s, "--") {
		return "a segment never contains '--'"
	}
	return ""
}

// CapabilityID checks capability-id = segment 1*( "." segment ), at most 128 characters.
func CapabilityID(s string) error {
	if len(s) > MaxIDLength {
		return fail("capability-id", s, fmt.Sprintf("longer than %d characters", MaxIDLength))
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return fail("capability-id", s, "an id has a domain and at least one name: domain.name")
	}
	for _, p := range parts {
		if why := segment(p); why != "" {
			return fail("capability-id", s, why)
		}
	}
	return nil
}

// Domain is an id's first segment.
func Domain(id string) string {
	d, _, _ := strings.Cut(id, ".")
	return d
}

// Builtin reports whether an id names a built-in capability (domain atlas).
func Builtin(id string) bool { return Domain(id) == "atlas" }

// version = "v" nz-digits.
func version(s string) (int, string) {
	if !strings.HasPrefix(s, "v") {
		return 0, "a version is v followed by digits"
	}
	d := s[1:]
	if d == "" || d[0] < '1' || d[0] > '9' {
		return 0, "a version is a positive integer without a leading zero"
	}
	for i := 0; i < len(d); i++ {
		if d[i] < '0' || d[i] > '9' {
			return 0, "a version is digits only"
		}
	}
	n, err := strconv.Atoi(d)
	if err != nil {
		return 0, "a version is too large"
	}
	return n, ""
}

// ParsePinned parses pinned-id = capability-id "@" version.
func ParsePinned(s string) (Pinned, error) {
	id, v, ok := strings.Cut(s, "@")
	if !ok {
		return Pinned{}, fail("pinned-id", s, "a pinned id is id@vN")
	}
	if err := CapabilityID(id); err != nil {
		return Pinned{}, fail("pinned-id", s, err.(*Error).Reason)
	}
	n, why := version(v)
	if why != "" {
		return Pinned{}, fail("pinned-id", s, why)
	}
	return Pinned{id, n}, nil
}

// ParseExact parses exact-id = pinned-id "#" fingerprint.
func ParseExact(s string) (Exact, error) {
	p, fp, ok := strings.Cut(s, "#")
	if !ok {
		return Exact{}, fail("exact-id", s, "an exact id is id@vN#fingerprint")
	}
	pinned, err := ParsePinned(p)
	if err != nil {
		return Exact{}, fail("exact-id", s, err.(*Error).Reason)
	}
	if err := Fingerprint(fp); err != nil {
		return Exact{}, fail("exact-id", s, err.(*Error).Reason)
	}
	return Exact{pinned, fp}, nil
}

// Fingerprint checks fingerprint = 16( DIGIT / %x61-66 ).
func Fingerprint(s string) error {
	if len(s) != 16 {
		return fail("fingerprint", s, "a fingerprint is 16 hexadecimal characters")
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return fail("fingerprint", s, "a fingerprint is lowercase hexadecimal")
		}
	}
	return nil
}

// ParseAddress parses address = ( capability-id / pinned-id ) ":" interaction,
// at most 160 characters.
func ParseAddress(s string) (Address, error) {
	if len(s) > MaxAddressLength {
		return Address{}, fail("address", s, fmt.Sprintf("longer than %d characters", MaxAddressLength))
	}
	head, inter, ok := strings.Cut(s, ":")
	if !ok {
		return Address{}, fail("address", s, "an address is id@vN:interaction or id:interaction")
	}
	if why := segment(inter); why != "" {
		return Address{}, fail("address", s, "the interaction: "+why)
	}
	if strings.Contains(head, "@") {
		p, err := ParsePinned(head)
		if err != nil {
			return Address{}, fail("address", s, err.(*Error).Reason)
		}
		return Address{p.ID, p.Version, inter}, nil
	}
	if err := CapabilityID(head); err != nil {
		return Address{}, fail("address", s, err.(*Error).Reason)
	}
	return Address{ID: head, Interaction: inter}, nil
}

// Interaction checks interaction = segment.
func Interaction(s string) error {
	if why := segment(s); why != "" {
		return fail("interaction", s, why)
	}
	return nil
}

// ParseProtocol parses protocol-ver = "atlas/" 1*DIGIT "." 1*DIGIT.
func ParseProtocol(s string) (major, minor int, err error) {
	rest, ok := strings.CutPrefix(s, "atlas/")
	ma, mi, dot := strings.Cut(rest, ".")
	if !ok || !dot || !digits(ma) || !digits(mi) {
		return 0, 0, fail("protocol-ver", s, "a protocol version is atlas/MAJOR.MINOR")
	}
	major, err1 := strconv.Atoi(ma)
	minor, err2 := strconv.Atoi(mi)
	if err1 != nil || err2 != nil {
		return 0, 0, fail("protocol-ver", s, "too large")
	}
	return major, minor, nil
}

// PropertyName checks property-name = %x61-7A *( %x61-7A / DIGIT / "_" ).
func PropertyName(s string) error { return snake("property-name", s) }

// ProblemCode checks problem-code = %x61-7A *( %x61-7A / DIGIT / "_" ).
func ProblemCode(s string) error { return snake("problem-code", s) }

func snake(rule, s string) error {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return fail(rule, s, "starts with a-z")
	}
	for i := 1; i < len(s); i++ {
		if c := s[i]; !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return fail(rule, s, fmt.Sprintf("%q is not a-z, 0-9 or _", c))
		}
	}
	return nil
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Date is a calendar date without a time zone.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

func (d Date) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day) }

// ParseDate parses date = 4DIGIT "-" 2DIGIT "-" 2DIGIT, a real calendar date.
func ParseDate(s string) (Date, error) {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' || !digits(s[:4]) || !digits(s[5:7]) || !digits(s[8:]) {
		return Date{}, fail("date", s, "a date is YYYY-MM-DD")
	}
	y, _ := strconv.Atoi(s[:4])
	m, _ := strconv.Atoi(s[5:7])
	d, _ := strconv.Atoi(s[8:])
	if m < 1 || m > 12 || d < 1 || d > daysIn(y, time.Month(m)) {
		return Date{}, fail("date", s, "not a real calendar date")
	}
	return Date{y, time.Month(m), d}, nil
}

func daysIn(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// ParseDateTime parses date-time: a date, "T", hh:mm:ss, an optional fraction
// of 1 to 9 digits, and an offset ("Z" or ±hh:mm), which is required. The
// leap second 60 is refused. The result keeps the offset; fractions beyond
// nanoseconds cannot occur.
func ParseDateTime(s string) (time.Time, error) {
	if len(s) < 20 || s[10] != 'T' {
		return time.Time{}, fail("date-time", s, "a date-time is YYYY-MM-DDThh:mm:ss[.f]Z or with an offset")
	}
	d, err := ParseDate(s[:10])
	if err != nil {
		return time.Time{}, fail("date-time", s, err.(*Error).Reason)
	}
	t := s[11:]
	if len(t) < 9 || t[2] != ':' || t[5] != ':' || !digits(t[:2]) || !digits(t[3:5]) || !digits(t[6:8]) {
		return time.Time{}, fail("date-time", s, "the time is hh:mm:ss")
	}
	hh, _ := strconv.Atoi(t[:2])
	mm, _ := strconv.Atoi(t[3:5])
	ss, _ := strconv.Atoi(t[6:8])
	if hh > 23 || mm > 59 || ss > 59 {
		return time.Time{}, fail("date-time", s, "not a real time (no leap second)")
	}
	rest := t[8:]
	nanos := 0
	if strings.HasPrefix(rest, ".") {
		i := 1
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		frac := rest[1:i]
		if len(frac) < 1 || len(frac) > 9 {
			return time.Time{}, fail("date-time", s, "a fraction has 1 to 9 digits")
		}
		nanos, _ = strconv.Atoi(frac + strings.Repeat("0", 9-len(frac)))
		rest = rest[i:]
	}
	var loc *time.Location
	switch {
	case rest == "Z":
		loc = time.UTC
	case len(rest) == 6 && (rest[0] == '+' || rest[0] == '-') && rest[3] == ':' && digits(rest[1:3]) && digits(rest[4:]):
		oh, _ := strconv.Atoi(rest[1:3])
		om, _ := strconv.Atoi(rest[4:])
		if oh > 23 || om > 59 {
			return time.Time{}, fail("date-time", s, "not a real offset")
		}
		off := oh*3600 + om*60
		if rest[0] == '-' {
			off = -off
		}
		loc = time.FixedZone("", off)
	default:
		return time.Time{}, fail("date-time", s, "a date-time ends with Z or an offset ±hh:mm")
	}
	return time.Date(d.Year, d.Month, d.Day, hh, mm, ss, nanos, loc), nil
}

// FormatDateTime writes an instant as the gateway sends one: in UTC, with
// "Z", microseconds when it has a fraction (§9.1.4, §9.2.6).
func FormatDateTime(t time.Time) string {
	// Always six fractional digits (Q-TIME-1), cut, never rounded (§9.1.4).
	return t.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z")
}

// ParseDuration parses duration = "P" [ n "D" ] [ "T" [ n "H" ] [ n "M" ] [ n [ "." 1*9DIGIT ] "S" ] ]:
// days, hours, minutes and seconds only, at least one of them. A "T" with
// nothing after it is a duration after days (P1DT), as the ABNF has it, and
// not alone (PT).
func ParseDuration(s string) (time.Duration, error) {
	rest, ok := strings.CutPrefix(s, "P")
	if !ok || rest == "" {
		return 0, fail("duration", s, "a duration is P[nD][T[nH][nM][n[.f]S]] with at least one part")
	}
	var total big.Int
	add := func(n string, unit time.Duration) {
		v, _ := new(big.Int).SetString(n, 10)
		total.Add(&total, v.Mul(v, big.NewInt(int64(unit))))
	}
	take := func(designators string) (num, frac string, d byte) {
		i := 0
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		j := i
		if i < len(rest) && rest[i] == '.' && strings.Contains(designators, "S") {
			j++
			for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
				j++
			}
			frac = rest[i+1 : j]
			if frac == "" {
				return "", "", 0 // a point needs a digit after it
			}
		}
		if i == 0 || j >= len(rest) || !strings.ContainsRune(designators, rune(rest[j])) || j > i && rest[j] != 'S' {
			return "", "", 0 // a fraction belongs to seconds only
		}
		num, d = rest[:i], rest[j]
		rest = rest[j+1:]
		return num, frac, d
	}
	parts := 0
	if n, _, d := peekDays(rest); d == 'D' {
		rest = rest[len(n)+1:]
		add(n, 24*time.Hour)
		parts++
	}
	if after, ok := strings.CutPrefix(rest, "T"); ok {
		rest = after
		if rest == "" && parts == 0 {
			// P1DT is a duration, as the ABNF writes it; PT is not (Q-GRAM-3)
			return 0, fail("duration", s, "a T needs at least one time part")
		}
		order := "HMS"
		for rest != "" {
			num, frac, d := take(order)
			if d == 0 {
				return 0, fail("duration", s, "time parts are nH, nM and n[.f]S, in that order")
			}
			order = order[strings.IndexByte(order, d)+1:]
			switch d {
			case 'H':
				add(num, time.Hour)
			case 'M':
				add(num, time.Minute)
			case 'S':
				if frac != "" && (len(frac) > 9) {
					return 0, fail("duration", s, "a fraction of a second has 1 to 9 digits")
				}
				add(num, time.Second)
				if frac != "" {
					add(frac+strings.Repeat("0", 9-len(frac)), time.Nanosecond)
				}
			}
			parts++
		}
	}
	if rest != "" || parts == 0 {
		return 0, fail("duration", s, "a duration is P[nD][T[nH][nM][n[.f]S]] with at least one part")
	}
	if !total.IsInt64() {
		return 0, fail("duration", s, "too long")
	}
	return time.Duration(total.Int64()), nil
}

func peekDays(s string) (string, string, byte) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i < len(s) && s[i] == 'D' {
		return s[:i], "", 'D'
	}
	return "", "", 0
}

// Decimal is an exact decimal: its sign, its integer digits (no leading
// zeros, "0" for zero) and its fraction digits as written.
type Decimal struct {
	Negative bool
	Integer  string
	Fraction string
}

// ParseDecimal parses decimal = [ "-" ] ( "0" / nz-digits ) [ "." 1*DIGIT ];
// "-0" (any negative zero) is refused.
func ParseDecimal(s string) (Decimal, error) {
	rest := s
	neg := false
	if strings.HasPrefix(rest, "-") {
		neg, rest = true, rest[1:]
	}
	intPart, frac, hasPoint := strings.Cut(rest, ".")
	if !digits(intPart) || len(intPart) > 1 && intPart[0] == '0' || hasPoint && !digits(frac) {
		return Decimal{}, fail("decimal", s, "a decimal is [-](0|[1-9][0-9]*)[.digits]")
	}
	if neg && strings.Trim(intPart+frac, "0") == "" {
		return Decimal{}, fail("decimal", s, "-0 is not allowed")
	}
	return Decimal{neg, intPart, frac}, nil
}

// Fits reports whether the decimal fits Decimal(p,s): at most s digits after
// the point and at most p−s before it, so that it is exact in Decimal128(p,s)
// (§9.1.5, §9.2.7).
func (d Decimal) Fits(precision, scale int) bool {
	intDigits := len(strings.TrimLeft(d.Integer, "0"))
	return len(d.Fraction) <= scale && intDigits <= precision-scale
}

// Unscaled is the decimal times 10^scale, exactly; ok is false when it has
// more fraction digits than scale.
func (d Decimal) Unscaled(scale int) (*big.Int, bool) {
	if len(d.Fraction) > scale {
		return nil, false
	}
	v, _ := new(big.Int).SetString(d.Integer+d.Fraction+strings.Repeat("0", scale-len(d.Fraction)), 10)
	if d.Negative {
		v.Neg(v)
	}
	return v, true
}

// FormatDecimal writes an unscaled value at a scale as a sender writes a
// Decimal(p,s): exactly s digits after the point when s > 0 (§9.1.5).
func FormatDecimal(unscaled *big.Int, scale int) string {
	neg := unscaled.Sign() < 0
	digitsStr := new(big.Int).Abs(unscaled).String()
	if scale > 0 {
		if len(digitsStr) <= scale {
			digitsStr = strings.Repeat("0", scale-len(digitsStr)+1) + digitsStr
		}
		digitsStr = digitsStr[:len(digitsStr)-scale] + "." + digitsStr[len(digitsStr)-scale:]
	}
	if neg {
		return "-" + digitsStr
	}
	return digitsStr
}

// ParseIntegerString parses integer-str = [ "-" ] ( "0" / nz-digits ): an
// integer sent as a string because it lies outside the I-JSON range.
func ParseIntegerString(s string) (*big.Int, error) {
	rest := strings.TrimPrefix(s, "-")
	// -0 is an integer string: only a decimal refuses it (Q-DEC-3).
	if !digits(rest) || len(rest) > 1 && rest[0] == '0' {
		return nil, fail("integer-str", s, "an integer string is [-](0|[1-9][0-9]*)")
	}
	v, _ := new(big.Int).SetString(s, 10)
	return v, nil
}

// FormatDuration writes a non-negative duration as a duration (§9.1.4), in
// days of 24 hours, hours, minutes and seconds, leaving out the parts that
// are zero: PT0S for nothing, P1DT2H, PT1.5S.
func FormatDuration(d time.Duration) (string, error) {
	if d < 0 {
		return "", fail("duration", d.String(), "a duration is not negative")
	}
	if d == 0 {
		return "PT0S", nil
	}
	const day = 24 * time.Hour
	days, d := d/day, d%day
	h, d := d/time.Hour, d%time.Hour
	m, d := d/time.Minute, d%time.Minute
	var b strings.Builder
	b.WriteByte('P')
	if days > 0 {
		fmt.Fprintf(&b, "%dD", days)
	}
	if h > 0 || m > 0 || d > 0 {
		b.WriteByte('T')
	}
	if h > 0 {
		fmt.Fprintf(&b, "%dH", h)
	}
	if m > 0 {
		fmt.Fprintf(&b, "%dM", m)
	}
	if d > 0 {
		secs, frac := d/time.Second, d%time.Second
		if frac == 0 {
			fmt.Fprintf(&b, "%dS", secs)
		} else {
			fmt.Fprintf(&b, "%d.%sS", secs, strings.TrimRight(fmt.Sprintf("%09d", frac), "0"))
		}
	}
	return b.String(), nil
}

// Segment checks segment = a-z *( a-z / DIGIT / "-" ), never ending with
// "-" nor holding "--". A domain is a segment.
func Segment(s string) error {
	if why := segment(s); why != "" {
		return fail("segment", s, why)
	}
	return nil
}

// Version checks version = "v" nz-digits.
func Version(s string) error {
	if _, why := version(s); why != "" {
		return fail("version", s, why)
	}
	return nil
}

// NzDigits checks nz-digits = %x31-39 *DIGIT.
func NzDigits(s string) error {
	if s == "" || s[0] == '0' || !digits(s) {
		return fail("nz-digits", s, "digits without a leading zero")
	}
	return nil
}

// MaxIdempotencyKey is the longest idempotency key.
const MaxIdempotencyKey = 255

// IdempotencyKey checks idempotency-key = 1*255( ALPHA / DIGIT / "-" / "_" /
// "." ) (§9.6.3).
func IdempotencyKey(s string) error {
	if s == "" || len(s) > MaxIdempotencyKey {
		return fail("idempotency-key", s, fmt.Sprintf("1 to %d characters", MaxIdempotencyKey))
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return fail("idempotency-key", s, "only A-Z a-z 0-9 - _ and .")
		}
	}
	return nil
}

// ProblemTypeBase prefixes every problem's type (§9.3.3).
const ProblemTypeBase = "https://atlas.jhg.com/problems/"

// ProblemType checks problem-type = "https://atlas.jhg.com/problems/"
// problem-code.
func ProblemType(s string) error {
	code, ok := strings.CutPrefix(s, ProblemTypeBase)
	if !ok || ProblemCode(code) != nil {
		return fail("problem-type", s, ProblemTypeBase+"<problem-code>")
	}
	return nil
}

// DataSchema checks dataschema = "urn:atlas:profile:" pinned-id ":"
// fingerprint, an event's writer profile (§9.4.3).
func DataSchema(s string) error {
	rest, ok := strings.CutPrefix(s, "urn:atlas:profile:")
	i := strings.LastIndexByte(rest, ':')
	if !ok || i < 0 {
		return fail("dataschema", s, "urn:atlas:profile:<pinned-id>:<fingerprint>")
	}
	if _, err := ParsePinned(rest[:i]); err != nil {
		return err
	}
	return Fingerprint(rest[i+1:])
}

// EventType checks event-type = "com.jhg.fiquant." capability-id ".v"
// nz-digits (§2.5).
func EventType(s string) error {
	rest, ok := strings.CutPrefix(s, "com.jhg.fiquant.")
	i := strings.LastIndex(rest, ".v")
	if !ok || i < 0 {
		return fail("event-type", s, "com.jhg.fiquant.<capability-id>.v<N>")
	}
	if err := CapabilityID(rest[:i]); err != nil {
		return err
	}
	return NzDigits(rest[i+2:])
}

// ResourceID checks resource-id = ( a-z / DIGIT ) *( a-z / DIGIT / "-" ),
// a provisional rule (Q-GRAM-1).
func ResourceID(s string) error {
	if s == "" {
		return fail("resource-id", s, "empty")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' && i > 0) {
			return fail("resource-id", s, "a-z, digits and -, not first")
		}
	}
	return nil
}

// ResourceName checks resource-name = domain 1*( "/" segment "/"
// resource-id ): nautilus/dashboards/d1 (§9.3.4, provisional).
func ResourceName(s string) error {
	parts := strings.Split(s, "/")
	if len(parts) < 3 || len(parts)%2 == 0 {
		return fail("resource-name", s, "a domain, then collection and id pairs")
	}
	if err := Segment(parts[0]); err != nil {
		return err
	}
	for i := 1; i < len(parts); i += 2 {
		if err := Segment(parts[i]); err != nil {
			return err
		}
		if err := ResourceID(parts[i+1]); err != nil {
			return err
		}
	}
	return nil
}

// OperationName checks operation-name = "operations/" resource-id.
func OperationName(s string) error {
	id, ok := strings.CutPrefix(s, "operations/")
	if !ok {
		return fail("operation-name", s, "operations/<id>")
	}
	return ResourceID(id)
}

// EntityTag checks entity-tag = [ "W/" ] DQUOTE *etagc DQUOTE, etagc = %x21 /
// %x23-7E (RFC 9110 §8.8.3, without obs-text).
func EntityTag(s string) error {
	body := strings.TrimPrefix(s, "W/")
	if len(body) < 2 || body[0] != '"' || body[len(body)-1] != '"' {
		return fail("entity-tag", s, "a quoted tag, optionally W/")
	}
	for i := 1; i < len(body)-1; i++ {
		if c := body[i]; c != 0x21 && (c < 0x23 || c > 0x7E) {
			return fail("entity-tag", s, "visible ASCII but \"")
		}
	}
	return nil
}
