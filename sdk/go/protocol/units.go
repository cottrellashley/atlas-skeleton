package protocol

import (
	"fmt"
	"math/big"
	"strings"
)

// conversion is exactly one multiplication or one division by an integer
// factor, as §9.5.4 requires: dividing by 10000 is not the same operation as
// multiplying by 0.0001 in binary64.
type conversion struct {
	multiply bool
	factor   int64
}

// Conversions exist only within a dimension, and only these (§9.5.4).
var conversions = map[[2]string]conversion{
	{"fraction", "percent"}: {true, 100},
	{"percent", "fraction"}: {false, 100},
	{"fraction", "bp"}:      {true, 10000},
	{"bp", "fraction"}:      {false, 10000},
	{"percent", "bp"}:       {true, 100},
	{"bp", "percent"}:       {false, 100},
	{"days", "seconds"}:     {true, 86400},
	{"seconds", "days"}:     {false, 86400},
}

func unitConversion(from, to string) (conversion, error) {
	c, ok := conversions[[2]string{strings.ToLower(from), strings.ToLower(to)}]
	if !ok {
		return conversion{}, fmt.Errorf("no conversion from %s to %s", from, to)
	}
	return c, nil
}

func (c conversion) float(v float64) float64 {
	if c.multiply {
		return v * float64(c.factor)
	}
	return v / float64(c.factor)
}

// unscaled applies the factor to a decimal's unscaled value in exact
// arithmetic, rounding a division to the same scale, ties to even.
func (c conversion) unscaled(v *big.Int) *big.Int {
	f := big.NewInt(c.factor)
	if c.multiply {
		return new(big.Int).Mul(v, f)
	}
	q, r := new(big.Int).QuoRem(v, f, new(big.Int))
	twice := new(big.Int).Abs(r)
	twice.Lsh(twice, 1)
	switch cmp := twice.Cmp(f); {
	case cmp > 0, cmp == 0 && q.Bit(0) == 1:
		if v.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return q
}
