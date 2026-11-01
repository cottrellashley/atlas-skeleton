package sqlglot_test

import (
	"testing"

	sqlglot "atlas.local/sqlglot"
	"atlas.local/sqlglot/exp"
)

func simplifyFixture(e *exp.Expr, o fixtureOpts) (*exp.Expr, error) {
	var d any
	if o.Dialect != "" {
		d = o.Dialect
	}
	e, err := sqlglot.AnnotateTypesOpts(e, sqlglot.AnnotateOptions{Schema: o.Schema, Dialect: d})
	if err != nil {
		return nil, err
	}
	return sqlglot.SimplifyOpts(e, true, true, d), nil
}

func TestSimplify(t *testing.T) {
	checkFile(t, "simplify", false, optimizerSchema(), simplifyFixture)
}
