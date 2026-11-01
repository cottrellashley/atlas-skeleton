"""§6.4.3: the read-only guard, as a reference on sqlglot 30.27.0's Snowflake
parser. A statement is accepted only when it is exactly one read-only query
(SELECT, UNION, INTERSECT, EXCEPT, WITH) over the registered objects.

Rules, checked in this order; the first that refuses gives the reason:

1. parse_error: the parser cannot read it.
2. empty: no statement. multiple_statements: more than one (a single trailing
   semicolon is allowed; an empty statement after it is not).
3. The statement's kind: dml (INSERT, UPDATE, DELETE, MERGE), ddl (CREATE,
   ALTER, DROP, TRUNCATE), copy (COPY INTO), file_transfer (PUT, GET), call
   (CALL), not_a_query (everything else: SHOW, DESCRIBE, USE, GRANT, BEGIN,
   EXPLAIN, EXECUTE IMMEDIATE ...); select_into (SELECT ... INTO) writes.
4. stage: a stage reference (@name, @~, @%table), wherever it appears.
5. table_function: TABLE(...), LATERAL FLATTEN and every other table function
   (RESULT_SCAN among them).
6. system_function: a function whose name begins SYSTEM$.
7. dynamic_identifier: IDENTIFIER(...), whose object cannot be checked.
8. unqualified_object: a table that is not a CTE in scope and is not named
   database.schema.object (Q-GUARD-1).
9. unregistered_object: an object neither registered itself nor in a
   registered schema. Identifiers are compared as Snowflake resolves them:
   unquoted ones upper-cased, quoted ones exactly. CTE names are not tables.
"""

import logging

import sqlglot
from sqlglot import exp
from sqlglot.dialects.snowflake import Snowflake

logging.getLogger("sqlglot").setLevel(logging.ERROR)

ORDER = ("parse_error", "empty", "multiple_statements", "dml", "ddl", "copy", "file_transfer", "call",
         "not_a_query", "select_into", "stage", "table_function", "system_function", "dynamic_identifier",
         "unqualified_object", "unregistered_object")

_DML = (exp.Insert, exp.Update, exp.Delete, exp.Merge)
_DDL = (exp.Create, exp.Drop, exp.Alter, exp.TruncateTable)
_QUERY = (exp.Select, exp.Union, exp.Intersect, exp.Except)


def _ident(node):
    """An identifier as Snowflake resolves it: unquoted upper-cased."""
    if node is None:
        return None
    if isinstance(node, exp.Identifier):
        return node.this if node.quoted else node.this.upper()
    if isinstance(node, str):
        return node.upper() if node else None
    return None


def _kind(stmt):
    if isinstance(stmt, _DML):
        return "dml"
    if isinstance(stmt, _DDL):
        return "ddl"
    if isinstance(stmt, exp.Copy):
        return "copy"
    if type(stmt).__name__ in ("Put", "Get"):
        return "file_transfer"
    if isinstance(stmt, exp.Command):
        word = (stmt.this or "").upper()
        if word in ("GET", "PUT"):
            return "file_transfer"
        if word == "CALL":
            return "call"
        return "not_a_query"
    inner = stmt.this if isinstance(stmt, exp.Subquery) else stmt
    if not isinstance(inner, _QUERY):
        return "not_a_query"
    return None


def _ctes_in_scope(node):
    """Names of the CTEs visible at `node`: every WITH on the way up."""
    names = set()
    cur = node
    while cur is not None:
        w = (cur.args.get("with_") or cur.args.get("with")) if hasattr(cur, "args") else None
        if isinstance(w, exp.With):
            for cte in w.expressions:
                names.add(_ident(cte.args["alias"].this))
        cur = cur.parent
    return names


def _stage_tokens(sql):
    try:
        tokens = Snowflake().tokenize(sql)
    except Exception:
        return False
    return any(t.token_type.name == "PARAMETER" and t.text == "@" for t in tokens)


def check(sql, registered):
    """(accepted, reason, objects): `registered` is a list of identifier lists,
    each [database, schema] (every object in the schema) or [database, schema,
    object]; `objects` are the objects read, as [database, schema, object]."""
    try:
        trees = sqlglot.parse(sql, read="snowflake")
    except Exception:
        return False, "parse_error", []
    statements = [t for t in trees if t is not None]
    if not statements:
        return False, "empty", []
    if len(trees) != 1:
        return False, "multiple_statements", []
    stmt = statements[0]
    kind = _kind(stmt)
    if kind:
        return False, kind, []
    found = set()
    for sel in stmt.find_all(exp.Select):
        if sel.args.get("into") is not None:
            found.add("select_into")
    if _stage_tokens(sql):
        found.add("stage")
    objects = []
    for node in stmt.walk():
        if isinstance(node, (exp.TableFromRows, exp.Unnest)) or (
                isinstance(node, exp.Lateral) and isinstance(node.this, exp.Func)):
            found.add("table_function")
        if isinstance(node, exp.Func):
            name = node.name if isinstance(node, exp.Anonymous) else ""
            if isinstance(node, exp.Anonymous) and isinstance(node.this, exp.Identifier):
                name = node.this.this
            if isinstance(name, str) and name.upper().startswith("SYSTEM$"):
                found.add("system_function")
        if isinstance(node, exp.Table):
            this = node.this
            if isinstance(this, exp.Var) and str(this.this).startswith("@"):
                found.add("stage")
                continue
            if isinstance(this, exp.DynamicIdentifier) or type(this).__name__ == "DynamicIdentifier":
                found.add("dynamic_identifier")
                continue
            if not isinstance(this, exp.Identifier):
                found.add("table_function")
                continue
            catalog, db, name = node.args.get("catalog"), node.args.get("db"), _ident(this)
            if catalog is None and db is None and name in _ctes_in_scope(node):
                continue
            c, d = _ident(catalog), _ident(db)
            if not c or not d:
                found.add("unqualified_object")
                continue
            parts = [c, d, name]
            objects.append(parts)
            if not any(r == parts or r == parts[:2] for r in registered):
                found.add("unregistered_object")
    for reason in ORDER:
        if reason in found:
            return False, reason, []
    unique = []
    for o in objects:
        if o not in unique:
            unique.append(o)
    return True, None, unique
