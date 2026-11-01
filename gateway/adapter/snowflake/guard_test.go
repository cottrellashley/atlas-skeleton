package snowflake

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/internal/refs"
	sqlglot "github.com/jhgonline/fi-app-atlas/sqlglot"
	"github.com/jhgonline/fi-app-atlas/sqlglot/exp"
)

// §6.4.3, the read-only guard: every case of the references' guard vectors,
// on the repository's sqlglot, gives the reference guard's verdict. The
// guard has no requirement id of its own; ATLAS-DATA-006 is the export
// links' (R119).
func TestGuardVectors(t *testing.T) {
	var v struct {
		Registered [][]string `json:"registered"`
		Cases      []struct {
			Name      string `json:"name"`
			Statement string `json:"statement"`
			Expected  struct {
				Accepted bool       `json:"accepted"`
				Objects  [][]string `json:"objects"`
				Reason   string     `json:"reason"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(refs.Read(t, "vectors/guard.json"), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range v.Cases {
		got := Guard(c.Statement, v.Registered)
		want := c.Expected
		if got.Accepted != want.Accepted || got.Reason != want.Reason || !slices.EqualFunc(got.Objects, want.Objects, slices.Equal) {
			t.Errorf("%s: %q\n got %+v\nwant %+v", c.Name, c.Statement, got, want)
		}
	}
}

// The guard fails closed: nothing it cannot read is accepted. Every table
// of an accepted statement is a CTE visible where it is read, worked out
// here apart from the guard's own walk, or one of the objects the verdict
// lists (R115).
func FuzzGuard(f *testing.F) {
	for _, s := range []string{"SELECT a FROM HARBOUR.GOLD.HOLDINGS", "SELECT 1; DROP TABLE x", "WITH a AS (SELECT 1) SELECT * FROM a", "SELECT * FROM @stage", "CALL p()",
		"WITH a AS (SELECT * FROM b), b AS (SELECT 1 AS x) SELECT * FROM a", "WITH r AS (SELECT 1 AS n UNION ALL SELECT n + 1 FROM r) SELECT * FROM r",
		"WITH a AS (SELECT * FROM HARBOUR.GOLD.H), b AS (SELECT * FROM a) SELECT * FROM b JOIN (WITH c AS (SELECT 1) SELECT * FROM c) d ON true"} {
		f.Add(s)
	}
	registered := [][]string{{"HARBOUR", "GOLD"}}
	f.Fuzz(func(t *testing.T, sql string) {
		v := Guard(sql, registered)
		if !v.Accepted {
			return
		}
		for _, o := range v.Objects {
			if o[0] != "HARBOUR" || o[1] != "GOLD" {
				t.Fatalf("%q reads %v", sql, o)
			}
		}
		trees, err := sqlglot.Parse(sql, "snowflake")
		if err != nil || len(trees) != 1 || trees[0] == nil {
			t.Fatalf("%q was accepted but does not parse to one statement: %v", sql, err)
		}
		for node := range trees[0].Walk(true, nil) {
			if !node.IsKind(exp.Table) || !node.This().IsKind(exp.Identifier) {
				continue
			}
			catalog, db, name := ident(node.Arg("catalog")), ident(node.Arg("db")), ident(node.This())
			if catalog == "" && db == "" && slices.Contains(visibleCTEs(trees[0], node), name) {
				continue
			}
			if !slices.ContainsFunc(v.Objects, func(o []string) bool { return slices.Equal(o, []string{catalog, db, name}) }) {
				t.Fatalf("%q was accepted, but it reads %s.%s.%s, which its verdict %v does not list", sql, catalog, db, name, v.Objects)
			}
		}
	})
}

// visibleCTEs are the CTEs a table may name, found by searching each WITH's
// CTEs for the table, not by walking up from it: the CTEs written up to the
// one that holds it, or every CTE when it is in the WITH's own query.
func visibleCTEs(root, table *exp.Expr) []string {
	holds := func(e *exp.Expr) bool {
		for n := range e.Walk(true, nil) {
			if n == table {
				return true
			}
		}
		return false
	}
	var names []string
	for w := range root.Walk(true, nil) {
		if !w.IsKind(exp.With) || w.Parent() == nil || !holds(w.Parent()) {
			continue
		}
		ctes := w.Expressions()
		upto := len(ctes) // in the WITH's own query: every CTE
		for j, cte := range ctes {
			if holds(cte) {
				upto = j + 1
				break
			}
		}
		for _, cte := range ctes[:upto] {
			if alias := cte.Get("alias"); alias != nil {
				names = append(names, ident(alias.This()))
			}
		}
	}
	return names
}

// R115 (INTEGRATIONS: "the guard must inspect CTEs"): a CTE sees only the
// CTEs written before it, and itself (Snowflake's WITH: "the second CTE can
// refer to the first CTE, but not vice versa"). A later one's name inside an
// earlier CTE is a table Snowflake resolves in the session's schema, so it
// is an unqualified read the guard refuses.
func TestR115CTEVisibilityIsPositional(t *testing.T) {
	registered := [][]string{{"HARBOUR", "GOLD"}}
	for _, c := range []struct {
		sql     string
		accept  bool
		reason  string
		objects [][]string
	}{
		{"WITH a AS (SELECT * FROM b), b AS (SELECT 1 AS x) SELECT * FROM a", false, UnqualifiedObject, nil},
		{"WITH a AS (SELECT * FROM HARBOUR.GOLD.HOLDINGS), b AS (SELECT * FROM a) SELECT * FROM b", true, "", [][]string{{"HARBOUR", "GOLD", "HOLDINGS"}}},
		{"WITH RECURSIVE r AS (SELECT 1 AS n UNION ALL SELECT n + 1 FROM r WHERE n < 3) SELECT * FROM r", true, "", nil},
		{"WITH r AS (SELECT 1 AS n UNION ALL SELECT n + 1 FROM r WHERE n < 3) SELECT * FROM r", true, "", nil},
		{"WITH a AS (SELECT 1 AS x), b AS (SELECT * FROM a) SELECT * FROM a JOIN b ON true", true, "", nil},
		{"WITH a AS (WITH b AS (SELECT 1 AS x) SELECT * FROM c), c AS (SELECT 2 AS y) SELECT * FROM a", false, UnqualifiedObject, nil},
	} {
		v := Guard(c.sql, registered)
		if v.Accepted != c.accept || v.Reason != c.reason || !slices.EqualFunc(v.Objects, c.objects, slices.Equal) {
			t.Errorf("%q\n got %+v\nwant accepted=%v reason=%q objects=%v", c.sql, v, c.accept, c.reason, c.objects)
		}
	}
}

// R115: a function that reads an object named in a string, such as
// GET_DDL('TABLE', 'HARBOUR.SECRET.SALARIES'), reads past the registered
// objects without naming a table, so it is refused with the system
// functions.
func TestR115ObjectNamedInAString(t *testing.T) {
	registered := [][]string{{"HARBOUR", "GOLD"}}
	for _, sql := range []string{
		"SELECT GET_DDL('TABLE', 'HARBOUR.SECRET.SALARIES')",
		"SELECT get_ddl('table', 'harbour.secret.salaries') FROM HARBOUR.GOLD.HOLDINGS",
		"SELECT x FROM HARBOUR.GOLD.HOLDINGS WHERE y = GET_DDL('TABLE', 'HARBOUR.SECRET.SALARIES')",
	} {
		if v := Guard(sql, registered); v.Accepted || v.Reason != SystemFunction {
			t.Errorf("%q: %+v, want refused as %s", sql, v, SystemFunction)
		}
	}
}
