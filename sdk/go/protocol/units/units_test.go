package units

import (
	"math"
	"testing"
)

// §9.5.4: exactly one multiplication or one division in binary64.
func TestFloat(t *testing.T) {
	for _, c := range []struct {
		v        float64
		from, to Unit
		want     float64
	}{
		{0.0125, Fraction, Bp, 125},
		{125, Bp, Fraction, 125.0 / 10000},
		{0.5, Fraction, Percent, 50},
		{12.5, Percent, Bp, 1250},
		{1250, Bp, Percent, 12.5},
		{2, Days, Seconds, 172800},
		{43200, Seconds, Days, 0.5},
		{3, Years, Years, 3},
	} {
		got, err := Float(c.v, c.from, c.to)
		if err != nil || got != c.want {
			t.Errorf("%v %s→%s = %v %v, want %v", c.v, c.from, c.to, got, err, c.want)
		}
	}
	// Dividing by 10000 is not multiplying by 0.0001.
	v := 3.0
	if div, _ := Float(v, Bp, Fraction); div != v/10000 || div == v*0.0001 {
		t.Fatalf("bp→fraction divides: %v (×0.0001 gives %v)", div, v*0.0001)
	}
	for _, c := range [][2]Unit{{Years, Days}, {Count, Bp}, {CurrencyUnit, Count}, {Bp, Days}} {
		if Convertible(c[0], c[1]) {
			t.Errorf("%s converts to %s", c[0], c[1])
		}
		if _, err := Float(1, c[0], c[1]); err == nil {
			t.Errorf("%s→%s converted", c[0], c[1])
		}
	}
	if f, _ := Float(math.Inf(1), Bp, Fraction); !math.IsInf(f, 1) {
		t.Fatal("infinity divides to infinity (the caller refuses non-I-JSON numbers)")
	}
}

// On a Decimal(p,s) string the factor is applied exactly and the result is
// rounded to the reader's scale, ties to even.
func TestDecimal(t *testing.T) {
	for _, c := range []struct {
		v        string
		from, to Unit
		scale    int
		want     string
	}{
		{"0.0125", Fraction, Bp, 2, "125.00"},
		{"125", Bp, Fraction, 4, "0.0125"},
		{"0.00125", Fraction, Percent, 3, "0.125"},
		{"0.000125", Fraction, Percent, 3, "0.012"}, // 0.0125 at scale 3: a tie, to even
		{"0.000135", Fraction, Percent, 3, "0.014"}, // 0.0135: a tie, to even
		{"-0.000125", Fraction, Percent, 3, "-0.012"},
		{"-0.000135", Fraction, Percent, 3, "-0.014"},
		{"1", Seconds, Days, 6, "0.000012"}, // 1/86400 = 0.0000115740…
		{"-0.004", Bp, Bp, 2, "0.00"},
		{"12.345", Percent, Percent, 2, "12.34"},
		{"12.355", Percent, Percent, 2, "12.36"},
	} {
		got, err := Decimal(c.v, c.from, c.to, c.scale)
		if err != nil || got != c.want {
			t.Errorf("%s %s→%s at %d = %q %v, want %q", c.v, c.from, c.to, c.scale, got, err, c.want)
		}
	}
	if _, err := Decimal("1e3", Bp, Fraction, 2); err == nil {
		t.Fatal("not a decimal string")
	}
}

func TestTables(t *testing.T) {
	for _, u := range All {
		if _, err := Parse(string(u)); err != nil {
			t.Error(err)
		}
	}
	if _, err := Parse("bps"); err == nil {
		t.Fatal("bps is not a unit")
	}
	if u, ok := OfLogical("Bp"); !ok || u != Bp {
		t.Fatal("Bp is in bp")
	}
	if _, ok := OfLogical("Money"); ok {
		t.Fatal("Money implies no unit")
	}
}
