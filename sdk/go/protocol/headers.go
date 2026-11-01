package protocol

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/sf"
)

// Headers of §9.6.3, and the HTTP headers the binding uses beside them.
const (
	HeaderAuthorization      = "Authorization"
	HeaderClient             = "Atlas-Client"
	HeaderClientFeatures     = "Atlas-Client-Features"
	HeaderProfileExpect      = "Atlas-Profile-Expect"
	HeaderDeadline           = "Atlas-Deadline"
	HeaderIdempotencyKey     = "Idempotency-Key"
	HeaderIfMatch            = "If-Match"
	HeaderIfNoneMatch        = "If-None-Match"
	HeaderExecution          = "Atlas-Execution"
	HeaderInteractive        = "X-Atlas-Interactive"
	HeaderConfirm            = "Atlas-Confirm"
	HeaderTraceparent        = "Traceparent"
	HeaderTracestate         = "Tracestate"
	HeaderProfile            = "Atlas-Profile"
	HeaderGovernanceRevision = "Atlas-Governance-Revision"
	HeaderLimitRemaining     = "Atlas-Limit-Remaining"
	HeaderErrorCode          = "Atlas-Error-Code"
	HeaderRequestID          = "X-Request-Id"
	HeaderDeprecation        = "Deprecation"
	HeaderSunset             = "Sunset"
	HeaderRetryAfter         = "Retry-After"
	HeaderOperationLocation  = "Operation-Location"
	HeaderCacheControl       = "Cache-Control"
	HeaderETag               = "ETag"
	HeaderWWWAuthenticate    = "WWW-Authenticate"
	HeaderAccept             = "Accept"
	HeaderContentType        = "Content-Type"
)

// NoStore is the Cache-Control of every handoff, connect, ticket and
// transfer answer, and of everything that is not the bootstrap or a profile
// fetched by its exact id (§9.6.7).
const NoStore = "no-store"

// Immutable is the Cache-Control of a profile fetched by its exact id.
const Immutable = "max-age=31536000, immutable"

// Field returns a header's field lines combined into one value, as RFC 9651
// §4.2 parses them, and whether there was any line.
func Field(h http.Header, name string) (string, bool) {
	vs := h.Values(name)
	if len(vs) == 0 {
		return "", false
	}
	return strings.Join(vs, ", "), true
}

// Client is Atlas-Client: who is calling, and the protocol it speaks.
type Client struct {
	Name     string
	Version  string
	Protocol string // a protocol-ver, such as atlas/1.0
}

// Format writes the header's value.
func (c Client) Format() (string, error) {
	if _, _, err := grammar.ParseProtocol(c.Protocol); err != nil {
		return "", err
	}
	return sf.Dictionary{
		{Key: "name", Value: sf.Item{Value: c.Name}},
		{Key: "version", Value: sf.Item{Value: c.Version}},
		{Key: "protocol", Value: sf.Item{Value: c.Protocol}},
	}.Serialize()
}

// ParseClient reads Atlas-Client. Unless name, version and protocol are
// each a String, and protocol a protocol-ver, the header does not parse as
// its type and is treated as absent (Q-HDR-1).
func ParseClient(field string) (Client, error) {
	d, err := sf.ParseDictionary(field)
	if err != nil {
		return Client{}, err
	}
	var c Client
	for _, m := range []struct {
		key string
		to  *string
	}{{"name", &c.Name}, {"version", &c.Version}, {"protocol", &c.Protocol}} {
		s, ok := dictString(d, m.key)
		if !ok {
			return Client{}, fmt.Errorf("%s has no %s String", HeaderClient, m.key)
		}
		*m.to = s
	}
	if _, _, err := grammar.ParseProtocol(c.Protocol); err != nil {
		return Client{}, err
	}
	return c, nil
}

func dictString(d sf.Dictionary, key string) (string, bool) {
	if m, ok := d.Get(key); ok {
		if it, ok := m.(sf.Item); ok {
			if s, ok := it.Value.(string); ok {
				return s, true
			}
		}
	}
	return "", false
}

// Features is Atlas-Client-Features: what a client implements, so the
// gateway serves it the next way down where it lacks something (§9).
type Features struct {
	Levels     []string // core streaming columnar bulk events handoff
	Handoff    []string // chunk formats it decodes, and reach-snowflake
	Lens       []string // the lens operations it applies
	Deliveries []string // single chunked subscription operation transfer
	Encodings  []string // json ndjson arrow csv parquet
	Traits     []string // the trait names it understands, lowercased
}

// CoreFeatures are what a client that declares nothing is served at.
func CoreFeatures() Features {
	return Features{Levels: []string{LevelCore}, Deliveries: []string{DeliverySingle, DeliveryOperation}, Encodings: []string{EncodingJSON}}
}

// Has reports whether the client declares a level. Every client is Core.
func (f Features) Has(level string) bool { return level == LevelCore || contains(f.Levels, level) }

// Decodes reports whether the client decodes a handoff chunk format.
func (f Features) Decodes(format string) bool { return contains(f.Handoff, format) }

// Applies reports whether the client applies a lens operation itself.
func (f Features) Applies(op string) bool { return contains(f.Lens, op) }

// Accepts reports whether the client takes a delivery.
func (f Features) Accepts(delivery string) bool { return contains(f.Deliveries, delivery) }

// Reads reports whether the client reads an encoding.
func (f Features) Reads(encoding string) bool { return contains(f.Encodings, encoding) }

// Understands reports whether the client implements a trait.
func (f Features) Understands(trait string) bool { return contains(f.Traits, trait) }

func (f *Features) members() []struct {
	key  string
	list *[]string
} {
	return []struct {
		key  string
		list *[]string
	}{{"levels", &f.Levels}, {"handoff", &f.Handoff}, {"lens", &f.Lens}, {"deliveries", &f.Deliveries}, {"encodings", &f.Encodings}, {"traits", &f.Traits}}
}

// Format writes the header's value; an empty list is left out.
func (f Features) Format() (string, error) {
	var d sf.Dictionary
	for _, m := range (&f).members() {
		if len(*m.list) == 0 {
			continue
		}
		il := sf.InnerList{}
		for _, t := range *m.list {
			il.Items = append(il.Items, sf.Item{Value: sf.Token(t)})
		}
		d = append(d, sf.DictMember{Key: m.key, Value: il})
	}
	return d.Serialize()
}

// ParseFeatures reads Atlas-Client-Features. Members it does not know are
// ignored; a member it knows that is not an Inner List of Tokens makes the
// header absent (Q-HDR-1).
func ParseFeatures(field string) (Features, error) {
	d, err := sf.ParseDictionary(field)
	if err != nil {
		return Features{}, err
	}
	var f Features
	for _, m := range f.members() {
		v, ok := d.Get(m.key)
		if !ok {
			continue
		}
		il, ok := v.(sf.InnerList)
		if !ok {
			return Features{}, fmt.Errorf("%s: %s is not an Inner List", HeaderClientFeatures, m.key)
		}
		*m.list = []string{}
		for _, it := range il.Items {
			t, ok := it.Value.(sf.Token)
			if !ok {
				return Features{}, fmt.Errorf("%s: %s holds something other than a Token", HeaderClientFeatures, m.key)
			}
			*m.list = append(*m.list, string(t))
		}
	}
	return f, nil
}

// FormatExact writes an exact-id as Atlas-Profile and Atlas-Profile-Expect
// carry it: a Structured Field String.
func FormatExact(e grammar.Exact) (string, error) { return sf.Item{Value: e.String()}.Serialize() }

// ParseExactHeader reads Atlas-Profile or Atlas-Profile-Expect.
func ParseExactHeader(field string) (grammar.Exact, error) {
	s, err := itemString(field)
	if err != nil {
		return grammar.Exact{}, err
	}
	return grammar.ParseExact(s)
}

// FormatDeadline writes Atlas-Deadline, an absolute time in whole seconds.
// It rounds down, so a deadline is never sent later than it is.
func FormatDeadline(t time.Time) string {
	s, _ := sf.Item{Value: sf.Date(t.Unix())}.Serialize()
	return s
}

// ParseDeadline reads Atlas-Deadline, a Structured Field Date.
func ParseDeadline(field string) (time.Time, error) {
	it, err := sf.ParseItem(field)
	if err != nil {
		return time.Time{}, err
	}
	d, ok := it.Value.(sf.Date)
	if !ok {
		return time.Time{}, fmt.Errorf("%s %q is not a date", HeaderDeadline, field)
	}
	return time.Unix(int64(d), 0).UTC(), nil
}

// MaxIdempotencyKey is the longest Idempotency-Key (§9.6.3).
const MaxIdempotencyKey = grammar.MaxIdempotencyKey

// CheckIdempotencyKey reports whether k is 1 to 255 characters of
// A-Z a-z 0-9 - _ and ".".
func CheckIdempotencyKey(k string) error { return grammar.IdempotencyKey(k) }

// FormatIdempotencyKey writes Idempotency-Key, a Structured Field String.
func FormatIdempotencyKey(k string) (string, error) {
	if err := CheckIdempotencyKey(k); err != nil {
		return "", err
	}
	return sf.Item{Value: k}.Serialize()
}

// ParseIdempotencyKey reads Idempotency-Key.
func ParseIdempotencyKey(field string) (string, error) {
	k, err := itemString(field)
	if err != nil {
		return "", err
	}
	return k, CheckIdempotencyKey(k)
}

// ParseExecution reads Atlas-Execution: gateway, handoff or client.
func ParseExecution(field string) (string, error) {
	t, err := itemToken(field)
	if err != nil {
		return "", err
	}
	switch t {
	case ExecutionGateway, ExecutionHandoff, ExecutionClient:
		return t, nil
	}
	return "", fmt.Errorf("%s %q is not gateway, handoff or client", HeaderExecution, t)
}

// FormatBoolean writes a Structured Field Boolean, as X-Atlas-Interactive.
func FormatBoolean(b bool) string {
	s, _ := sf.Item{Value: b}.Serialize()
	return s
}

// ParseBoolean reads a Structured Field Boolean.
func ParseBoolean(field string) (bool, error) {
	it, err := sf.ParseItem(field)
	if err != nil {
		return false, err
	}
	b, ok := it.Value.(bool)
	if !ok {
		return false, fmt.Errorf("%q is not a boolean", field)
	}
	return b, nil
}

// FormatConfirm writes Atlas-Confirm from an estimate's amount, a decimal
// string with at most three fraction digits and twelve integer digits.
func FormatConfirm(amount string) (string, error) {
	d, err := ParseAmount(amount)
	if err != nil {
		return "", err
	}
	return sf.Item{Value: d}.Serialize()
}

// ParseConfirm reads Atlas-Confirm, a Structured Field Decimal, in
// thousandths.
func ParseConfirm(field string) (sf.Decimal, error) {
	it, err := sf.ParseItem(field)
	if err != nil {
		return 0, err
	}
	d, ok := it.Value.(sf.Decimal)
	if !ok {
		return 0, fmt.Errorf("%s %q is not a decimal", HeaderConfirm, field)
	}
	return d, nil
}

// ParseAmount reads an estimate's amount into thousandths, so it compares
// exactly with Atlas-Confirm.
func ParseAmount(amount string) (sf.Decimal, error) {
	d, err := grammar.ParseDecimal(amount)
	if err != nil {
		return 0, err
	}
	if !d.Fits(15, 3) {
		return 0, fmt.Errorf("an amount %q has at most 12 integer and 3 fraction digits", amount)
	}
	u, _ := d.Unscaled(3)
	return sf.Decimal(u.Int64()), nil
}

// Limit is one member of Atlas-Limit-Remaining.
type Limit struct {
	Name      string
	Remaining int64
}

// FormatLimits writes Atlas-Limit-Remaining, a Dictionary of Integers.
func FormatLimits(ls []Limit) (string, error) {
	d := make(sf.Dictionary, 0, len(ls))
	for _, l := range ls {
		d = append(d, sf.DictMember{Key: l.Name, Value: sf.Item{Value: l.Remaining}})
	}
	return d.Serialize()
}

// ParseLimits reads Atlas-Limit-Remaining, a Dictionary of Integers: a
// member that is not one makes the header absent (Q-HDR-1). Parameters are
// ignored.
func ParseLimits(field string) ([]Limit, error) {
	d, err := sf.ParseDictionary(field)
	if err != nil {
		return nil, err
	}
	var out []Limit
	for _, m := range d {
		it, ok := m.Value.(sf.Item)
		n, isInt := it.Value.(int64)
		if !ok || !isInt {
			return nil, fmt.Errorf("%s: %s is not an Integer", HeaderLimitRemaining, m.Key)
		}
		out = append(out, Limit{m.Key, n})
	}
	return out, nil
}

// ParseGovernanceRevision reads Atlas-Governance-Revision, a String.
func ParseGovernanceRevision(field string) (string, error) { return itemString(field) }

// FormatGovernanceRevision writes Atlas-Governance-Revision.
func FormatGovernanceRevision(rev string) (string, error) { return sf.Item{Value: rev}.Serialize() }

// FormatErrorCode writes Atlas-Error-Code, a Token.
func FormatErrorCode(code string) (string, error) {
	if err := grammar.ProblemCode(code); err != nil {
		return "", err
	}
	return code, nil
}

// ParseErrorCode reads Atlas-Error-Code.
func ParseErrorCode(field string) (string, error) {
	t, err := itemToken(field)
	if err != nil {
		return "", err
	}
	return t, grammar.ProblemCode(t)
}

// FormatDeprecation writes Deprecation, a Structured Field Date (RFC 9745).
func FormatDeprecation(t time.Time) string { return FormatDeadline(t) }

// ParseDeprecation reads Deprecation.
func ParseDeprecation(field string) (time.Time, error) { return ParseDeadline(field) }

// FormatSunset writes Sunset, an HTTP-date (RFC 8594) in its preferred form.
func FormatSunset(t time.Time) string { return t.UTC().Format(http.TimeFormat) }

// ParseSunset reads Sunset, an HTTP-date. now places rfc850's two-digit
// year (RFC 9110 §5.6.7).
func ParseSunset(field string, now time.Time) (time.Time, error) {
	return ParseHTTPDate(field, now)
}

// ParseHTTPDate reads an HTTP-date in any of RFC 9110's three forms, in
// GMT, its day of the week the right one. An rfc850 date's two-digit year
// that would be more than 50 years after now is the century before.
func ParseHTTPDate(s string, now time.Time) (time.Time, error) {
	for _, form := range []struct {
		layout string
		sep    string // what ends the day's name
		long   bool   // the day's name in full (rfc850)
	}{
		{"Mon, 02 Jan 2006 15:04:05 GMT", ",", false},
		{"Monday, 02-Jan-06 15:04:05 GMT", ",", true},
		{"Mon Jan _2 15:04:05 2006", " ", false},
	} {
		t, err := time.Parse(form.layout, s)
		if err != nil {
			continue
		}
		if form.long {
			century := now.Year() / 100 * 100
			t = t.AddDate(century+t.Year()%100-t.Year(), 0, 0)
			if t.Year() > now.Year()+50 {
				t = t.AddDate(-100, 0, 0)
			}
		}
		day, _, _ := strings.Cut(s, form.sep)
		want := t.Weekday().String()
		if !form.long {
			want = want[:3]
		}
		if day != want {
			return time.Time{}, fmt.Errorf("%q: the day of the week is wrong", s)
		}
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("%q is not an HTTP-date", s)
}

// ParseBearer reads an Authorization field's Bearer token (RFC 6750): the
// scheme in any case, a space, a b64token.
func ParseBearer(field string) (string, error) {
	scheme, token, ok := strings.Cut(field, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", fmt.Errorf("not a Bearer credential")
	}
	body := strings.TrimRight(token, "=")
	if body == "" {
		return "", fmt.Errorf("not a b64token (RFC 6750)")
	}
	for i := 0; i < len(body); i++ {
		c := body[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("-._~+/", c) >= 0) {
			return "", fmt.Errorf("not a b64token (RFC 6750)")
		}
	}
	return token, nil
}

// ParseCacheControl reads Cache-Control's directives, in order.
func ParseCacheControl(field string) []string {
	var out []string
	for _, d := range strings.Split(field, ",") {
		if d = strings.TrimSpace(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// ParseIfMatch reads If-Match: "*", or a list of entity-tags.
func ParseIfMatch(field string) ([]string, error) {
	field = strings.TrimSpace(field)
	if field == "*" {
		return []string{"*"}, nil
	}
	var out []string
	for _, tag := range strings.Split(field, ",") {
		tag = strings.TrimSpace(tag)
		if err := grammar.EntityTag(tag); err != nil {
			return nil, fmt.Errorf("not a list of entity-tags")
		}
		out = append(out, tag)
	}
	return out, nil
}

// MaxRetryAfter bounds a Retry-After a client honours: a day.
const MaxRetryAfter = 86400

// FormatRetryAfter writes Retry-After in delay-seconds, the only form
// atlas/1.0 sends.
func FormatRetryAfter(seconds int) string { return strconv.Itoa(max(seconds, 0)) }

// ParseRetryAfter reads Retry-After as delay-seconds. An HTTP-date is not
// atlas/1.0 and does not parse.
func ParseRetryAfter(field string) (time.Duration, error) {
	field = strings.TrimSpace(field)
	if field == "" {
		return 0, fmt.Errorf("%s is empty", HeaderRetryAfter)
	}
	for i := 0; i < len(field); i++ {
		if field[i] < '0' || field[i] > '9' {
			return 0, fmt.Errorf("%s %q is not delay-seconds", HeaderRetryAfter, field)
		}
	}
	n := MaxRetryAfter
	if len(field) <= 9 {
		n, _ = strconv.Atoi(field)
	}
	return time.Duration(min(n, MaxRetryAfter)) * time.Second, nil
}

// ParseOperationLocation reads Operation-Location: an absolute URL.
func ParseOperationLocation(field string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(field))
	if err != nil {
		return nil, err
	}
	if !u.IsAbs() || u.Host == "" || u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("%s %q is not an absolute URL", HeaderOperationLocation, field)
	}
	return u, nil
}

// StrongETag writes a strong entity tag: "3f9a1c0b7d2e4a65".
func StrongETag(opaque string) string { return `"` + opaque + `"` }

// IfMatch reports whether an If-Match field matches the current strong
// entity tag, by RFC 9110 §13.1.1's strong comparison: "*", or a list
// holding the tag; a weak tag never matches.
func IfMatch(field, current string) bool {
	field = strings.TrimSpace(field)
	if field == "*" {
		return current != ""
	}
	for _, tag := range strings.Split(field, ",") {
		tag = strings.TrimSpace(tag)
		if len(tag) >= 2 && tag[0] == '"' && tag[len(tag)-1] == '"' && tag == current {
			return true
		}
	}
	return false
}

// IfNoneMatch reports whether an If-None-Match field names the current
// entity tag, by weak comparison, so a 304 can be answered.
func IfNoneMatch(field, current string) bool {
	field = strings.TrimSpace(field)
	if field == "*" {
		return current != ""
	}
	for _, tag := range strings.Split(field, ",") {
		if strings.TrimPrefix(strings.TrimSpace(tag), "W/") == strings.TrimPrefix(current, "W/") {
			return true
		}
	}
	return false
}

// Traceparent is a W3C Trace Context traceparent.
type Traceparent struct {
	Version  string // 2 lowercase hex; this package writes 00
	TraceID  string // 32 lowercase hex, not all zero
	ParentID string // 16 lowercase hex, not all zero
	Flags    byte
}

// ParseTraceparent reads a traceparent. An unknown version is read as 00
// requires, its trailing fields ignored, as Trace Context §4.3 says.
func ParseTraceparent(field string) (Traceparent, error) {
	bad := errors.New("not a traceparent")
	field = strings.TrimSpace(field)
	if len(field) < 55 || field[2] != '-' || field[35] != '-' || field[52] != '-' {
		return Traceparent{}, bad
	}
	version := field[:2]
	if !lowerHex(version) || version == "ff" || version == "00" && len(field) != 55 || version != "00" && len(field) > 55 && field[55] != '-' {
		return Traceparent{}, bad
	}
	t := Traceparent{Version: version, TraceID: field[3:35], ParentID: field[36:52]}
	flags := field[53:55]
	if !lowerHex(t.TraceID) || !lowerHex(t.ParentID) || !lowerHex(flags) || allZero(t.TraceID) || allZero(t.ParentID) {
		return Traceparent{}, bad
	}
	f, _ := strconv.ParseUint(flags, 16, 8)
	t.Flags = byte(f)
	return t, nil
}

// String writes the traceparent.
func (t Traceparent) String() string {
	return fmt.Sprintf("00-%s-%s-%02x", t.TraceID, t.ParentID, t.Flags)
}

func lowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return s != ""
}

func allZero(s string) bool { return strings.Trim(s, "0") == "" }

// Challenge is the Bearer challenge of a 401 (§9.6.6, RFC 6750): realm
// "atlas", with error "invalid_token" when the token has expired and claims
// when Conditional Access asks for more.
type Challenge struct {
	Realm  string
	Error  string
	Claims string // base64url
}

// Format writes the WWW-Authenticate value.
func (c Challenge) Format() string {
	var b strings.Builder
	b.WriteString(`Bearer realm=` + quote(c.Realm))
	if c.Error != "" {
		b.WriteString(`, error=` + quote(c.Error))
	}
	if c.Claims != "" {
		b.WriteString(`, claims=` + quote(c.Claims))
	}
	return b.String()
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// ParseChallenge reads the Bearer challenge from a WWW-Authenticate field
// (RFC 9110 §11.6.1), skipping any other scheme's.
func ParseChallenge(field string) (Challenge, error) {
	p := authParser{s: field}
	for !p.end() {
		scheme := p.token()
		if scheme == "" {
			p.i++ // a token68 or a stray character: not ours
			continue
		}
		params, err := p.params()
		if err != nil {
			return Challenge{}, err
		}
		if strings.EqualFold(scheme, "Bearer") {
			return Challenge{Realm: params["realm"], Error: params["error"], Claims: params["claims"]}, nil
		}
	}
	return Challenge{}, errors.New("no Bearer challenge")
}

type authParser struct {
	s string
	i int
}

func (p *authParser) end() bool { p.ws(); return p.i >= len(p.s) }

func (p *authParser) ws() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == ',') {
		p.i++
	}
}

func (p *authParser) token() string {
	p.ws()
	start := p.i
	for p.i < len(p.s) && tchar(p.s[p.i]) {
		p.i++
	}
	return p.s[start:p.i]
}

// params reads auth-params up to the next challenge: name=value pairs, the
// value a token or a quoted-string.
func (p *authParser) params() (map[string]string, error) {
	out := map[string]string{}
	for {
		save := p.i
		name := p.token()
		for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t') {
			p.i++
		}
		if name == "" || p.i >= len(p.s) || p.s[p.i] != '=' {
			p.i = save // the next challenge's scheme, or the end
			return out, nil
		}
		p.i++
		for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t') {
			p.i++
		}
		var value string
		if p.i < len(p.s) && p.s[p.i] == '"' {
			var b strings.Builder
			p.i++
			for {
				if p.i >= len(p.s) {
					return nil, errors.New("an unterminated quoted-string")
				}
				c := p.s[p.i]
				p.i++
				if c == '"' {
					break
				}
				if c == '\\' && p.i < len(p.s) {
					c = p.s[p.i]
					p.i++
				}
				b.WriteByte(c)
			}
			value = b.String()
		} else {
			start := p.i
			for p.i < len(p.s) && tchar(p.s[p.i]) {
				p.i++
			}
			value = p.s[start:p.i]
		}
		out[strings.ToLower(name)] = value
	}
}

func tchar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
}

// ChooseMedia picks the answer's media type from Accept (§9.6.4): the first
// type the client lists that the binding serves. No Accept, or */*, takes
// the binding's first. q values are ignored. ok is false for a 406.
func ChooseMedia(accept string, servable []string) (media string, ok bool) {
	if len(servable) == 0 {
		return "", false
	}
	if strings.TrimSpace(accept) == "" {
		return servable[0], true
	}
	for _, part := range strings.Split(accept, ",") {
		mt, _, _ := strings.Cut(part, ";")
		mt = strings.ToLower(strings.TrimSpace(mt))
		if mt == "*/*" {
			return servable[0], true
		}
		for _, s := range servable {
			if mt == s {
				return s, true
			}
		}
	}
	return "", false
}

func itemString(field string) (string, error) {
	it, err := sf.ParseItem(field)
	if err != nil {
		return "", err
	}
	s, ok := it.Value.(string)
	if !ok {
		return "", fmt.Errorf("%q is not a string", field)
	}
	return s, nil
}

func itemToken(field string) (string, error) {
	it, err := sf.ParseItem(field)
	if err != nil {
		return "", err
	}
	t, ok := it.Value.(sf.Token)
	if !ok {
		return "", fmt.Errorf("%q is not a token", field)
	}
	return string(t), nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// FormatConfirmDecimal writes a parsed Atlas-Confirm back as its decimal
// text.
func FormatConfirmDecimal(d sf.Decimal) (string, error) { return sf.Item{Value: d}.Serialize() }
