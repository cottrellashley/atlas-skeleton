package schema

import (
	"fmt"
	"strconv"
	"strings"
)

// logical is an x-atlas-logical type (§9.2.3).
type logical struct {
	name             string // Bp, Decimal, Money, Isin, …
	precision, scale int    // Decimal only
}

func (l logical) String() string {
	if l.name == "Decimal" {
		return fmt.Sprintf("Decimal(%d,%d)", l.precision, l.scale)
	}
	return l.name
}

type logicalDef struct {
	base      string // the JSON type it is
	pattern   string // as if written as pattern
	minLength int    // as if written as minLength
	format    string // as if written as format
	// identifier: a key that may be missing, so a key property of this type
	// may be null (assumption A-011).
	identifier bool
}

// logicals are the complete list of atlas/1.0 (§9.2.3).
var logicals = map[string]logicalDef{
	"Bp": {base: "number"}, "Percent": {base: "number"}, "Fraction": {base: "number"}, "Years": {base: "number"},
	"Decimal":       {base: "string", format: "decimal"},
	"Money":         {base: "object"},
	"Currency":      {base: "string", pattern: "^[A-Z]{3}$"},
	"Country":       {base: "string", pattern: "^[A-Z]{2}$"},
	"Isin":          {base: "string", pattern: "^[A-Z]{2}[A-Z0-9]{9}[0-9]$", identifier: true},
	"Cusip":         {base: "string", pattern: "^[A-Z0-9]{8}[0-9]$", identifier: true},
	"Sedol":         {base: "string", pattern: "^[B-DF-HJ-NP-TV-Z0-9]{6}[0-9]$", identifier: true},
	"Figi":          {base: "string", pattern: "^BBG[B-DF-HJ-NP-TV-Z0-9]{8}[0-9]$", identifier: true},
	"Lei":           {base: "string", pattern: "^[A-Z0-9]{18}[0-9]{2}$", identifier: true},
	"Tenor":         {base: "string", pattern: "^[1-9][0-9]*[DWMY]$"},
	"PortfolioCode": {base: "string", minLength: 1},
	"IndexCode":     {base: "string", minLength: 1},
	"Path":          {base: "string", minLength: 1},
	"Principal":     {base: "string", format: "uuid"},
	"Uri":           {base: "string", format: "uri"},
}

// MaxDecimalPrecision is the largest p of Decimal(p,s).
const MaxDecimalPrecision = 38

// parseLogical reads an x-atlas-logical value: a name of the list, or
// Decimal(p,s) with p 1 to 38 and s 0 to p, written without spaces.
func parseLogical(s string) (logical, error) {
	if inner, ok := strings.CutPrefix(s, "Decimal("); ok {
		inner, ok = strings.CutSuffix(inner, ")")
		ps, ss, comma := strings.Cut(inner, ",")
		if !ok || !comma {
			return logical{}, fmt.Errorf("%q is not Decimal(p,s)", s)
		}
		p, perr := smallInt(ps)
		sc, serr := smallInt(ss)
		if perr != nil || serr != nil || p < 1 || p > MaxDecimalPrecision || sc > p {
			return logical{}, fmt.Errorf("%q: Decimal(p,s) has p from 1 to %d and s from 0 to p", s, MaxDecimalPrecision)
		}
		return logical{name: "Decimal", precision: p, scale: sc}, nil
	}
	if _, ok := logicals[s]; !ok || s == "Decimal" {
		return logical{}, fmt.Errorf("%q is not a logical type of atlas/1.0", s)
	}
	return logical{name: s}, nil
}

// smallInt reads digits without a leading zero.
func smallInt(s string) (int, error) {
	if s == "" || len(s) > 2 || len(s) > 1 && s[0] == '0' {
		return 0, fmt.Errorf("%q", s)
	}
	for i := 0; i < len(s); i++ {
		if !digit(s[i]) {
			return 0, fmt.Errorf("%q", s)
		}
	}
	return strconv.Atoi(s)
}

func (l logical) def() logicalDef { return logicals[l.name] }
