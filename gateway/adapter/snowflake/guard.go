// Package snowflake is the Snowflake warehouse adapter. It compiles a
// capability's query (ports.Query) to one Snowflake statement, through the
// repository's sqlglot, holds that statement to the read-only guard of
// §6.4.3, and runs it through a database/sql driver, binding every value as
// a parameter. With the duckdb dialect the same statement is transpiled for
// DuckDB, which is how the development gateway stands in for Snowflake
// without Docker.
//
// It keeps INTEGRATIONS' rules for sqlglot: exactly one executable statement;
// a Command or an unknown node refused; an unsupported transformation fails
// and no driver call follows; qualification is never authorisation; input
// size, AST depth and concurrent compilations bounded, because parsing takes
// no cancellation context.
package snowflake

import (
	"fmt"
	"slices"
	"strings"

	sqlglot "atlas.local/sqlglot"
	_ "atlas.local/sqlglot/dialects" // snowflake and duckdb
	"atlas.local/sqlglot/exp"
	"atlas.local/sqlglot/tokens"
)

// The guard's reasons, in the order it checks them: the first that refuses
// a statement is its reason (the references' guard.py).
const (
	ParseError         = "parse_error"
	Empty              = "empty"
	MultipleStatements = "multiple_statements"
	DML                = "dml"
	DDL                = "ddl"
	CopyInto           = "copy"
	FileTransfer       = "file_transfer"
	Call               = "call"
	NotAQuery          = "not_a_query"
	SelectInto         = "select_into"
	Stage              = "stage"
	TableFunction      = "table_function"
	SystemFunction     = "system_function"
	DynamicIdentifier  = "dynamic_identifier"
	UnqualifiedObject  = "unqualified_object"
	UnregisteredObject = "unregistered_object"
)

var reasons = []string{ParseError, Empty, MultipleStatements, DML, DDL, CopyInto, FileTransfer, Call,
	NotAQuery, SelectInto, Stage, TableFunction, SystemFunction, DynamicIdentifier, UnqualifiedObject, UnregisteredObject}

// Verdict is the guard's decision on one statement: accepted with the
// objects it reads, each [database, schema, object] as Snowflake resolves
// them, or refused with the first reason.
type Verdict struct {
	Accepted bool
	Reason   string
	Objects  [][]string
}

// Guard accepts a statement only when it is exactly one read-only query
// (SELECT, UNION, INTERSECT, EXCEPT, WITH) over registered objects (§6.4.3).
// registered holds [database, schema] (every object in the schema) and
// [database, schema, object] entries, as Snowflake resolves them: unquoted
// identifiers upper-cased, quoted ones exactly.
func Guard(sql string, registered [][]string) (v Verdict) {
	defer func() {
		if recover() != nil { // a parser that fails, fails closed
			v = Verdict{Reason: ParseError}
		}
	}()
	trees, err := sqlglot.Parse(sql, "snowflake")
	if err != nil {
		return Verdict{Reason: ParseError}
	}
	var statements []*exp.Expr
	for _, t := range trees {
		if t != nil {
			statements = append(statements, t)
		}
	}
	switch {
	case len(statements) == 0:
		return Verdict{Reason: Empty}
	case len(trees) != 1:
		return Verdict{Reason: MultipleStatements}
	}
	stmt := statements[0]
	if k := kind(stmt); k != "" {
		return Verdict{Reason: k}
	}
	found := map[string]bool{}
	for sel := range stmt.FindAll(exp.Select) {
		if sel.Arg("into") != nil {
			found[SelectInto] = true
		}
	}
	if stageTokens(sql) {
		found[Stage] = true
	}
	var objects [][]string
	for node := range stmt.Walk(true, nil) {
		if node.IsAny(exp.TableFromRows, exp.Unnest) || node.Is(exp.Lateral) && node.This().Is(exp.Func) {
			found[TableFunction] = true
		}
		if node.Is(exp.Anonymous) && systemFunction(node.Name()) {
			found[SystemFunction] = true
		}
		if !node.IsKind(exp.Table) {
			continue
		}
		this := node.This()
		switch {
		case this.IsKind(exp.Var) && strings.HasPrefix(this.ThisStr(), "@"):
			found[Stage] = true
			continue
		case this.IsKind(exp.DynamicIdentifier):
			found[DynamicIdentifier] = true
			continue
		case !this.IsKind(exp.Identifier):
			found[TableFunction] = true
			continue
		}
		catalog, db, name := node.Arg("catalog"), node.Arg("db"), ident(this)
		if empty(catalog) && empty(db) && slices.Contains(ctesInScope(node), name) {
			continue
		}
		c, d := ident(catalog), ident(db)
		if c == "" || d == "" {
			found[UnqualifiedObject] = true
			continue
		}
		parts := []string{c, d, name}
		objects = append(objects, parts)
		if !slices.ContainsFunc(registered, func(r []string) bool { return slices.Equal(r, parts) || slices.Equal(r, parts[:2]) }) {
			found[UnregisteredObject] = true
		}
	}
	for _, r := range reasons {
		if found[r] {
			return Verdict{Reason: r}
		}
	}
	var unique [][]string
	for _, o := range objects {
		if !slices.ContainsFunc(unique, func(u []string) bool { return slices.Equal(u, o) }) {
			unique = append(unique, o)
		}
	}
	return Verdict{Accepted: true, Objects: unique}
}

func empty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case *exp.Expr:
		return x == nil
	case string:
		return x == ""
	}
	return false
}

// ident is an identifier as Snowflake resolves it: unquoted upper-cased.
func ident(v any) string {
	switch x := v.(type) {
	case *exp.Expr:
		if x == nil || !x.IsKind(exp.Identifier) {
			return ""
		}
		if q, _ := x.Arg("quoted").(bool); q {
			return x.ThisStr()
		}
		return strings.ToUpper(x.ThisStr())
	case string:
		return strings.ToUpper(x)
	}
	return ""
}

// kind refuses a statement that is not a read-only query, by what it is.
func kind(stmt *exp.Expr) string {
	switch {
	case stmt.IsAny(exp.Insert, exp.Update, exp.Delete, exp.Merge):
		return DML
	case stmt.IsAny(exp.Create, exp.Drop, exp.Alter, exp.TruncateTable):
		return DDL
	case stmt.Is(exp.Copy):
		return CopyInto
	case stmt.ClassName() == "Put" || stmt.ClassName() == "Get":
		return FileTransfer
	case stmt.Is(exp.Command):
		word, _ := stmt.Arg("this").(string)
		switch strings.ToUpper(word) {
		case "GET", "PUT":
			return FileTransfer
		case "CALL":
			return Call
		}
		return NotAQuery
	}
	inner := stmt
	if stmt.Is(exp.Subquery) {
		inner = stmt.This()
	}
	if !inner.IsAny(exp.Select, exp.Union, exp.Intersect, exp.Except) {
		return NotAQuery
	}
	return ""
}

// objectFunctions read an object named in a string argument rather than as
// a table (GET_DDL('TABLE', 'HARBOUR.SECRET.SALARIES')), so the objects they
// read are never seen as tables (R115). They are refused with the system
// functions, upper-cased as Snowflake resolves their names.
var objectFunctions = []string{"GET_DDL"}

// systemFunction reports a function the guard refuses by name: a SYSTEM$
// function, or one that reads an object named in a string.
func systemFunction(name string) bool {
	name = strings.ToUpper(name)
	return strings.HasPrefix(name, "SYSTEM$") || slices.Contains(objectFunctions, name)
}

// ctesInScope are the names of the CTEs visible at node, from every WITH on
// the way up. Visibility is positional, as Snowflake resolves it (R115): a
// table inside a WITH's i-th CTE sees that WITH's CTEs 0 to i (i itself, so
// a CTE may recurse), and the WITH's own query sees all of them. A later
// CTE's name inside an earlier one is therefore a table of the session's
// schema, which the guard refuses as unqualified.
func ctesInScope(node *exp.Expr) []string {
	var names []string
	var entered []*exp.Expr // the WITHs reached from inside one of their CTEs
	var prev *exp.Expr
	for cur := node; cur != nil; prev, cur = cur, cur.Parent() {
		if cur.IsKind(exp.With) {
			ctes := cur.Expressions()
			// prev is the CTE the walk came up through. A WITH reached
			// any other way shows nothing: the guard fails closed.
			i := slices.Index(ctes, prev)
			names = append(names, cteNames(ctes[:i+1])...)
			entered = append(entered, cur)
			continue
		}
		w := cur.Get("with_")
		if w == nil {
			w = cur.Get("with")
		}
		if !w.IsKind(exp.With) || slices.Contains(entered, w) {
			continue
		}
		names = append(names, cteNames(w.Expressions())...)
	}
	return names
}

func cteNames(ctes []*exp.Expr) []string {
	var names []string
	for _, cte := range ctes {
		if alias := cte.Get("alias"); alias != nil {
			names = append(names, ident(alias.This()))
		}
	}
	return names
}

// stageTokens reports a stage reference the parser may not show as a
// table: an @ token anywhere in the statement.
func stageTokens(sql string) bool {
	ts, err := sqlglot.Tokenize(sql, "snowflake")
	if err != nil {
		return false
	}
	return slices.ContainsFunc(ts, func(t *tokens.Token) bool { return t.Type == tokens.Parameter && t.Text == "@" })
}

// refuse is the error of a statement the guard refused.
func refuse(v Verdict) error {
	return fmt.Errorf("snowflake: the read-only guard refused the statement: %s", v.Reason)
}
