package grammar

import (
	"math/big"
	"strings"
	"testing"
	"time"
)

// AP-CONV-002: every implementation accepts exactly the strings the grammars
// of §9.1.3 to §9.1.5 accept.
func TestIdentifiers(t *testing.T) {
	for _, c := range []struct {
		s  string
		ok bool
	}{
		{"portfolio.holdings", true},
		{"portfolio.reference-values", true},
		{"signal.fvcm.curves", true},
		{"atlas.whoami", true},
		{"a1.b2", true},
		{"portfolio", false},           // a domain alone
		{"Portfolio.holdings", false},  // case-sensitive
		{"portfolio.holdings-", false}, // ends with -
		{"portfolio.hold--ings", false},
		{"portfolio..holdings", false},
		{"1portfolio.holdings", false},
		{"portfolio.holdings_x", false},
		{"portfolio." + strings.Repeat("a", 118), true},
		{"portfolio." + strings.Repeat("a", 119), false}, // 129 characters
	} {
		if err := CapabilityID(c.s); (err == nil) != c.ok {
			t.Errorf("CapabilityID(%q) = %v, want ok=%v", c.s, err, c.ok)
		}
	}
}

func TestPinnedExactAndAddresses(t *testing.T) {
	if p, err := ParsePinned("portfolio.holdings@v12"); err != nil || p.Version != 12 || p.String() != "portfolio.holdings@v12" {
		t.Fatalf("pinned: %+v %v", p, err)
	}
	for _, bad := range []string{"portfolio.holdings@v0", "portfolio.holdings@v01", "portfolio.holdings@1", "portfolio.holdings@V1", "portfolio.holdings"} {
		if _, err := ParsePinned(bad); err == nil {
			t.Errorf("%q accepted as a pinned id", bad)
		}
	}
	e, err := ParseExact("portfolio.holdings@v1#3f9a1c0b7d2e4a65")
	if err != nil || e.Fingerprint != "3f9a1c0b7d2e4a65" || e.String() != "portfolio.holdings@v1#3f9a1c0b7d2e4a65" {
		t.Fatalf("exact: %+v %v", e, err)
	}
	for _, bad := range []string{"portfolio.holdings@v1#3F9A1C0B7D2E4A65", "portfolio.holdings@v1#3f9a1c0b7d2e4a6", "portfolio.holdings#3f9a1c0b7d2e4a65"} {
		if _, err := ParseExact(bad); err == nil {
			t.Errorf("%q accepted as an exact id", bad)
		}
	}
	for _, c := range []struct {
		s    string
		want Address
		ok   bool
	}{
		{"portfolio.holdings@v1:fetch", Address{"portfolio.holdings", 1, "fetch"}, true},
		{"portfolio.holdings:fetch", Address{ID: "portfolio.holdings", Interaction: "fetch"}, true}, // a bare id
		{"atlas.operation@v1:cancel", Address{"atlas.operation", 1, "cancel"}, true},
		{"portfolio.holdings@v1", Address{}, false},
		{"portfolio.holdings@v1:Fetch", Address{}, false},
		{"portfolio.holdings@v1:", Address{}, false},
	} {
		got, err := ParseAddress(c.s)
		if (err == nil) != c.ok || c.ok && got != c.want {
			t.Errorf("ParseAddress(%q) = %+v, %v", c.s, got, err)
		}
		if c.ok && got.String() != c.s {
			t.Errorf("%q round-trips as %q", c.s, got.String())
		}
	}
	if _, err := ParseAddress("portfolio." + strings.Repeat("a", 118) + "@v1:" + strings.Repeat("f", 30)); err == nil {
		t.Error("an address over 160 characters is refused")
	}
}

func TestProtocolAndNames(t *testing.T) {
	if ma, mi, err := ParseProtocol("atlas/1.0"); err != nil || ma != 1 || mi != 0 {
		t.Fatalf("atlas/1.0: %d %d %v", ma, mi, err)
	}
	for _, bad := range []string{"atlas/1", "Atlas/1.0", "atlas/1.x", "atlas/.1"} {
		if _, _, err := ParseProtocol(bad); err == nil {
			t.Errorf("%q accepted as a protocol version", bad)
		}
	}
	for _, s := range []string{"oas", "market_value", "a1_b"} {
		if PropertyName(s) != nil || ProblemCode(s) != nil {
			t.Errorf("%q is a property name and a problem code", s)
		}
	}
	for _, s := range []string{"OAS", "market-value", "_x", "1a", ""} {
		if PropertyName(s) == nil {
			t.Errorf("%q accepted as a property name", s)
		}
	}
}

func TestDatesAndTimes(t *testing.T) {
	if d, err := ParseDate("2024-02-29"); err != nil || d.String() != "2024-02-29" {
		t.Fatalf("a leap day: %v %v", d, err)
	}
	for _, bad := range []string{"2023-02-29", "2026-13-01", "2026-00-10", "2026-1-01", "20260101", "2026-01-32"} {
		if _, err := ParseDate(bad); err == nil {
			t.Errorf("%q accepted as a date", bad)
		}
	}
	for _, c := range []struct {
		s    string
		want time.Time
	}{
		{"2026-10-30T14:02:11Z", time.Date(2026, 10, 30, 14, 2, 11, 0, time.UTC)},
		{"2026-10-30T14:02:11.5Z", time.Date(2026, 10, 30, 14, 2, 11, 500000000, time.UTC)},
		{"2026-10-30T14:02:11.123456789+01:00", time.Date(2026, 10, 30, 13, 2, 11, 123456789, time.UTC)},
		{"2026-10-30T00:00:00-05:30", time.Date(2026, 10, 30, 5, 30, 0, 0, time.UTC)},
	} {
		got, err := ParseDateTime(c.s)
		if err != nil || !got.Equal(c.want) {
			t.Errorf("ParseDateTime(%q) = %v, %v", c.s, got, err)
		}
	}
	for _, bad := range []string{
		"2026-10-30T14:02:11",             // no offset
		"2026-10-30t14:02:11Z",            // lowercase t
		"2026-10-30T14:02:11z",            // lowercase z
		"2026-10-30T14:02:60Z",            // leap second
		"2026-10-30T24:00:00Z",            // hour 24
		"2026-10-30T14:02:11.Z",           // empty fraction
		"2026-10-30T14:02:11.1234567890Z", // ten digits
		"2026-10-30 14:02:11Z",
	} {
		if _, err := ParseDateTime(bad); err == nil {
			t.Errorf("%q accepted as a date-time", bad)
		}
	}
	if got := FormatDateTime(time.Date(2026, 10, 30, 14, 2, 11, 123456789, time.FixedZone("", 3600))); got != "2026-10-30T13:02:11.123456Z" {
		t.Errorf("FormatDateTime truncates to microseconds in UTC: %s", got)
	}
}

func TestDurations(t *testing.T) {
	for _, c := range []struct {
		s    string
		want time.Duration
	}{
		{"PT2M", 2 * time.Minute},
		{"PT30M", 30 * time.Minute},
		{"PT1H", time.Hour},
		{"P7D", 7 * 24 * time.Hour},
		{"P1DT2H3M4.5S", 26*time.Hour + 3*time.Minute + 4500*time.Millisecond},
		{"PT0.000000001S", time.Nanosecond},
		{"P1DT", 24 * time.Hour}, // as the ABNF writes it (Q-GRAM-3)
	} {
		got, err := ParseDuration(c.s)
		if err != nil || got != c.want {
			t.Errorf("ParseDuration(%q) = %v, %v", c.s, got, err)
		}
	}
	for _, bad := range []string{"P", "PT", "PT1.S", "P1Y", "P1M", "P1W", "PT1S2M", "PT1.5M", "pt1h", "PT1H2H", "PT1.1234567890S", "1H"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("%q accepted as a duration", bad)
		}
	}
}

func TestDecimals(t *testing.T) {
	for _, c := range []struct {
		s          string
		p, sc      int
		ok, fits   bool
		unscaledAt string
	}{
		{"1234.56", 18, 2, true, true, "123456"},
		{"0.0012", 5, 4, true, true, "12"},
		{"-12.5", 18, 2, true, true, "-1250"},
		{"1234.5", 5, 2, true, false, ""}, // 4 integer digits > p−s = 3
		{"0.123", 5, 2, true, false, ""},  // 3 fraction digits > s
		{"0", 1, 0, true, true, "0"},
		{"-0", 1, 0, false, false, ""},
		{"-0.00", 3, 2, false, false, ""},
		{"01.5", 3, 1, false, false, ""},
		{"1.", 3, 1, false, false, ""},
		{".5", 3, 1, false, false, ""},
		{"1e3", 5, 0, false, false, ""},
	} {
		d, err := ParseDecimal(c.s)
		if (err == nil) != c.ok {
			t.Errorf("ParseDecimal(%q) = %v", c.s, err)
			continue
		}
		if !c.ok {
			continue
		}
		if d.Fits(c.p, c.sc) != c.fits {
			t.Errorf("%q Fits(%d,%d) = %v", c.s, c.p, c.sc, !c.fits)
		}
		if c.fits {
			u, ok := d.Unscaled(c.sc)
			if !ok || u.String() != c.unscaledAt {
				t.Errorf("%q unscaled at %d = %v", c.s, c.sc, u)
			}
		}
	}
	for _, c := range []struct {
		unscaled int64
		scale    int
		want     string
	}{{123456, 2, "1234.56"}, {5, 3, "0.005"}, {-1250, 2, "-12.50"}, {7, 0, "7"}, {0, 2, "0.00"}} {
		if got := FormatDecimal(big.NewInt(c.unscaled), c.scale); got != c.want {
			t.Errorf("FormatDecimal(%d, %d) = %q, want %q", c.unscaled, c.scale, got, c.want)
		}
	}
	if v, err := ParseIntegerString("-90071992547409930"); err != nil || v.String() != "-90071992547409930" {
		t.Errorf("an integer string: %v %v", v, err)
	}
	if v, err := ParseIntegerString("-0"); err != nil || v.Sign() != 0 {
		t.Errorf("-0 is an integer string (Q-DEC-3): %v %v", v, err)
	}
	for _, bad := range []string{"-00", "007", "1.0", "+1", ""} {
		if _, err := ParseIntegerString(bad); err == nil {
			t.Errorf("%q accepted as an integer string", bad)
		}
	}
}

func FuzzAddress(f *testing.F) {
	for _, s := range []string{"portfolio.holdings@v1:fetch", "a.b:c", "x.y@v99:z-w"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		a, err := ParseAddress(s)
		if err != nil {
			return
		}
		if a.String() != s {
			t.Fatalf("an accepted address round-trips: %q → %q", s, a.String())
		}
	})
}

func FuzzDuration(f *testing.F) {
	for _, s := range []string{"PT2M", "P1DT2H3M4.5S", "P7D"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { _, _ = ParseDuration(s) })
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                       "PT0S",
		90 * time.Minute:        "PT1H30M",
		1500 * time.Millisecond: "PT1.5S",
		26*time.Hour + 3*time.Minute + 4500*time.Millisecond: "P1DT2H3M4.5S",
		7 * 24 * time.Hour: "P7D",
		time.Microsecond:   "PT0.000001S",
	} {
		got, err := FormatDuration(d)
		if err != nil || got != want {
			t.Errorf("FormatDuration(%v) = %q %v, want %q", d, got, err, want)
		}
		if back, err := ParseDuration(got); err != nil || back != d {
			t.Errorf("%q reads back as %v %v", got, back, err)
		}
	}
	if _, err := FormatDuration(-time.Second); err == nil {
		t.Error("a negative duration has no form")
	}
}
