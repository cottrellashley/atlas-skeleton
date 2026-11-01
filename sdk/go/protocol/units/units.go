// Package units is §9.5.4: the units a number may carry, their dimensions,
// and the only conversions between them, in binary64 and in exact decimal
// arithmetic.
package units

import (
	"fmt"
	"math/big"

	"atlas.local/sdk/go/protocol/grammar"
)

// Unit is an x-atlas-unit.
type Unit string

// The units of atlas/1.0.
const (
	Bp           Unit = "bp"
	Percent      Unit = "percent"
	Fraction     Unit = "fraction"
	Years        Unit = "years"
	Days         Unit = "days"
	Seconds      Unit = "seconds"
	Count        Unit = "count"
	CurrencyUnit Unit = "currency_unit"
)

// Dimension is what a unit measures; conversions exist only within one.
type Dimension string

// The dimensions of atlas/1.0.
const (
	Ratio Dimension = "ratio"
	Time  Dimension = "time"
	None  Dimension = "none"
	Money Dimension = "money"
)

var dimensions = map[Unit]Dimension{
	Bp: Ratio, Percent: Ratio, Fraction: Ratio,
	Years: Time, Days: Time, Seconds: Time,
	Count: None, CurrencyUnit: Money,
}

// All are the units, in the order of §9.5.4's table.
var All = []Unit{Bp, Percent, Fraction, Years, Days, Seconds, Count, CurrencyUnit}

// Parse reads an x-atlas-unit.
func Parse(s string) (Unit, error) {
	if _, ok := dimensions[Unit(s)]; !ok {
		return "", fmt.Errorf("%q is not a unit of atlas/1.0", s)
	}
	return Unit(s), nil
}

// DimensionOf returns a unit's dimension.
func DimensionOf(u Unit) Dimension { return dimensions[u] }

// OfLogical returns the unit a logical type implies: Bp, Percent, Fraction
// and Years imply theirs; no other logical type implies one.
func OfLogical(logical string) (Unit, bool) {
	switch logical {
	case "Bp":
		return Bp, true
	case "Percent":
		return Percent, true
	case "Fraction":
		return Fraction, true
	case "Years":
		return Years, true
	}
	return "", false
}

// conversion is one multiplication or one division by an exact integer.
type conversion struct {
	multiply bool
	factor   int64
}

var conversions = map[[2]Unit]conversion{
	{Fraction, Percent}: {true, 100},
	{Percent, Fraction}: {false, 100},
	{Fraction, Bp}:      {true, 10000},
	{Bp, Fraction}:      {false, 10000},
	{Percent, Bp}:       {true, 100},
	{Bp, Percent}:       {false, 100},
	{Days, Seconds}:     {true, 86400},
	{Seconds, Days}:     {false, 86400},
}

// Convertible reports whether a value in from converts to to: the same
// unit, or one of the eight conversions of §9.5.4.
func Convertible(from, to Unit) bool {
	_, ok := conversions[[2]Unit{from, to}]
	return ok || from == to
}

// Factor is a conversion as an exact fraction: a value in from, times
// num/den, is the value in to.
func Factor(from, to Unit) (num, den int64, ok bool) {
	if from == to {
		return 1, 1, true
	}
	c, ok := conversions[[2]Unit{from, to}]
	if !ok {
		return 0, 0, false
	}
	if c.multiply {
		return c.factor, 1, true
	}
	return 1, c.factor, true
}

// Float converts a binary64 value by exactly one multiplication or one
// division, rounded to nearest, ties to even.
func Float(v float64, from, to Unit) (float64, error) {
	if from == to {
		return v, nil
	}
	c, ok := conversions[[2]Unit{from, to}]
	if !ok {
		return 0, fmt.Errorf("no conversion from %s to %s", from, to)
	}
	if c.multiply {
		return float64(v * float64(c.factor)), nil // the conversion rounds alone, never fused
	}
	return float64(v / float64(c.factor)), nil
}

// Decimal converts a decimal string by the same factor in exact
// arithmetic, rounded to scale, ties to even (§9.5.4).
func Decimal(v string, from, to Unit, scale int) (string, error) {
	if _, err := grammar.ParseDecimal(v); err != nil {
		return "", err
	}
	r, _ := new(big.Rat).SetString(v) // a decimal by §9.1.5 is always a rational
	num, den, ok := Factor(from, to)
	if !ok {
		return "", fmt.Errorf("no conversion from %s to %s", from, to)
	}
	r.Mul(r, big.NewRat(num, den))
	return grammar.FormatDecimal(RoundHalfEven(r, scale), scale), nil
}

// RoundHalfEven returns r at scale as an unscaled integer: r × 10^scale,
// rounded to nearest, ties to even.
func RoundHalfEven(r *big.Rat, scale int) *big.Int {
	scaled := new(big.Rat).Mul(r, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)))
	q, m := new(big.Int).QuoRem(scaled.Num(), scaled.Denom(), new(big.Int))
	// q is truncated toward zero; m carries the sign of the numerator.
	twice := new(big.Int).Abs(m)
	twice.Lsh(twice, 1)
	switch twice.Cmp(scaled.Denom()) {
	case 1:
		roundAway(q, m)
	case 0:
		if q.Bit(0) == 1 {
			roundAway(q, m)
		}
	}
	return q
}

func roundAway(q, m *big.Int) {
	if m.Sign() < 0 {
		q.Sub(q, big.NewInt(1))
	} else {
		q.Add(q, big.NewInt(1))
	}
}
