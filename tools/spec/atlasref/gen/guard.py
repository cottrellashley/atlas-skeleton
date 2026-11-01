"""references/vectors/guard.json (§6.4.3): statements the read-only guard
accepts or refuses for a registered set of objects. Every expectation is
written by hand; `build` asserts that guard.py (on sqlglot 30.27.0) agrees."""

from .. import guard
from ..jsonio import dumps

PATH = "vectors/guard.json"
REGISTERED = [["HARBOUR", "GOLD"], ["HARBOUR", "SILVER", "PRICES"]]
Y = None  # accepted

# (name, statement, reason or Y for accepted, objects when accepted, note, question)
CASES = [
    ("a select", "SELECT portfolio, weight FROM HARBOUR.GOLD.HOLDINGS", Y, [["HARBOUR", "GOLD", "HOLDINGS"]],
     "an object in a registered schema", None),
    ("a registered object", "SELECT close FROM HARBOUR.SILVER.PRICES", Y, [["HARBOUR", "SILVER", "PRICES"]],
     "registered by itself", None),
    ("unquoted names are upper-cased", "select * from harbour.gold.holdings", Y, [["HARBOUR", "GOLD", "HOLDINGS"]],
     None, None),
    ("a trailing semicolon", "SELECT 1 FROM HARBOUR.GOLD.HOLDINGS;", Y, [["HARBOUR", "GOLD", "HOLDINGS"]], None,
     None),
    ("a join and a subquery", "SELECT h.portfolio, p.close FROM HARBOUR.GOLD.HOLDINGS h JOIN HARBOUR.SILVER.PRICES p "
     "ON p.isin = h.isin WHERE h.weight > (SELECT AVG(weight) FROM HARBOUR.GOLD.HOLDINGS)", Y,
     [["HARBOUR", "GOLD", "HOLDINGS"], ["HARBOUR", "SILVER", "PRICES"]], None, None),
    ("WITH: a CTE name is not a table", "WITH top AS (SELECT * FROM HARBOUR.GOLD.HOLDINGS WHERE weight > 0.01) "
     "SELECT * FROM top", Y, [["HARBOUR", "GOLD", "HOLDINGS"]], None, None),
    ("a CTE that reads another CTE", "WITH a AS (SELECT * FROM HARBOUR.GOLD.HOLDINGS), b AS (SELECT * FROM a) "
     "SELECT * FROM b", Y, [["HARBOUR", "GOLD", "HOLDINGS"]], None, None),
    ("a recursive CTE", "WITH RECURSIVE r AS (SELECT 1 AS n UNION ALL SELECT n + 1 FROM r WHERE n < 3) "
     "SELECT * FROM r", Y, [], None, None),
    ("UNION ALL", "SELECT isin FROM HARBOUR.GOLD.HOLDINGS UNION ALL SELECT isin FROM HARBOUR.SILVER.PRICES", Y,
     [["HARBOUR", "GOLD", "HOLDINGS"], ["HARBOUR", "SILVER", "PRICES"]], None, None),
    ("INTERSECT", "SELECT isin FROM HARBOUR.GOLD.HOLDINGS INTERSECT SELECT isin FROM HARBOUR.SILVER.PRICES", Y,
     [["HARBOUR", "GOLD", "HOLDINGS"], ["HARBOUR", "SILVER", "PRICES"]], None, None),
    ("EXCEPT", "(SELECT isin FROM HARBOUR.GOLD.HOLDINGS) EXCEPT (SELECT isin FROM HARBOUR.SILVER.PRICES)", Y,
     [["HARBOUR", "GOLD", "HOLDINGS"], ["HARBOUR", "SILVER", "PRICES"]], None, None),
    ("no table at all", "SELECT 1", Y, [], None, None),
    ("VALUES", "SELECT * FROM (VALUES (1), (2)) AS v(a)", Y, [], None, None),
    ("time travel reads only", "SELECT * FROM HARBOUR.GOLD.HOLDINGS AT(OFFSET => -60)", Y,
     [["HARBOUR", "GOLD", "HOLDINGS"]], None, None),
    ("a comment hides nothing", "SELECT 1 FROM HARBOUR.GOLD.HOLDINGS -- ; DROP TABLE x", Y,
     [["HARBOUR", "GOLD", "HOLDINGS"]], None, None),
    ("unparsable", "SELEC 1", "parse_error", None, None, None),
    ("empty", "   ", "empty", None, None, None),
    ("two statements", "SELECT 1; SELECT 2", "multiple_statements", None, None, None),
    ("an empty second statement", "SELECT 1;;", "multiple_statements", None, None, "Q-GUARD-2"),
    ("a query then DML", "SELECT 1 FROM HARBOUR.GOLD.HOLDINGS; DELETE FROM HARBOUR.GOLD.HOLDINGS",
     "multiple_statements", None, None, None),
    ("INSERT", "INSERT INTO HARBOUR.GOLD.HOLDINGS VALUES (1)", "dml", None, None, None),
    ("UPDATE", "UPDATE HARBOUR.GOLD.HOLDINGS SET weight = 0", "dml", None, None, None),
    ("DELETE", "DELETE FROM HARBOUR.GOLD.HOLDINGS", "dml", None, None, None),
    ("MERGE", "MERGE INTO HARBOUR.GOLD.HOLDINGS t USING HARBOUR.SILVER.PRICES s ON t.isin = s.isin "
     "WHEN MATCHED THEN DELETE", "dml", None, None, None),
    ("WITH, then INSERT", "WITH x AS (SELECT 1) INSERT INTO HARBOUR.GOLD.HOLDINGS SELECT * FROM x", "dml", None,
     None, None),
    ("CREATE TABLE AS SELECT", "CREATE TABLE HARBOUR.GOLD.COPY AS SELECT * FROM HARBOUR.GOLD.HOLDINGS", "ddl", None,
     None, None),
    ("DROP", "DROP TABLE HARBOUR.GOLD.HOLDINGS", "ddl", None, None, None),
    ("ALTER", "ALTER TABLE HARBOUR.GOLD.HOLDINGS ADD COLUMN x INT", "ddl", None, None, None),
    ("TRUNCATE", "TRUNCATE TABLE HARBOUR.GOLD.HOLDINGS", "ddl", None, None, None),
    ("COPY INTO a stage", "COPY INTO @out FROM HARBOUR.GOLD.HOLDINGS", "copy", None, None, None),
    ("COPY INTO a table", "COPY INTO HARBOUR.GOLD.HOLDINGS FROM @in", "copy", None, None, None),
    ("PUT", "PUT file:///tmp/x.csv @in", "file_transfer", None, None, None),
    ("GET", "GET @out file:///tmp/", "file_transfer", None, None, None),
    ("CALL", "CALL HARBOUR.GOLD.REFRESH()", "call", None, None, None),
    ("SHOW", "SHOW TABLES", "not_a_query", None, None, None),
    ("DESCRIBE", "DESCRIBE TABLE HARBOUR.GOLD.HOLDINGS", "not_a_query", None, None, None),
    ("USE", "USE DATABASE HARBOUR", "not_a_query", None, None, None),
    ("GRANT", "GRANT SELECT ON HARBOUR.GOLD.HOLDINGS TO ROLE r", "not_a_query", None, None, None),
    ("BEGIN", "BEGIN", "not_a_query", None, None, None),
    ("EXPLAIN", "EXPLAIN SELECT 1", "not_a_query", None, "read-only, but not a query", None),
    ("EXECUTE IMMEDIATE", "EXECUTE IMMEDIATE 'DROP TABLE HARBOUR.GOLD.HOLDINGS'", "not_a_query", None, None, None),
    ("SELECT INTO writes", "SELECT * INTO HARBOUR.GOLD.COPY FROM HARBOUR.GOLD.HOLDINGS", "select_into", None, None,
     None),
    ("a stage in FROM", "SELECT $1 FROM @in", "stage", None, None, None),
    ("the user's stage", "SELECT $1 FROM @~/staged/f.csv", "stage", None, None, None),
    ("a table's stage", "SELECT $1 FROM @%holdings", "stage", None, None, None),
    ("a stage in a subquery", "SELECT * FROM HARBOUR.GOLD.HOLDINGS WHERE isin IN (SELECT $1 FROM @in)", "stage",
     None, None, None),
    ("a stage as a function's argument", "SELECT GET_PRESIGNED_URL(@out, 'f.csv')", "stage", None, None, None),
    ("TABLE(RESULT_SCAN(...))", "SELECT * FROM TABLE(RESULT_SCAN(LAST_QUERY_ID()))", "table_function", None, None,
     None),
    ("TABLE(GENERATOR(...))", "SELECT * FROM TABLE(GENERATOR(ROWCOUNT => 10))", "table_function", None, None, None),
    ("a registered schema's table function", "SELECT * FROM HARBOUR.GOLD.HOLDINGS, TABLE(HARBOUR.GOLD.EXPLODE(1))",
     "table_function", None, None, None),
    ("LATERAL FLATTEN", "SELECT f.value FROM HARBOUR.GOLD.HOLDINGS h, LATERAL FLATTEN(input => h.tags) f",
     "table_function", None, "every table function is refused, FLATTEN with them", "Q-GUARD-3"),
    ("SYSTEM$ functions", "SELECT SYSTEM$WHITELIST()", "system_function", None, None, None),
    ("SYSTEM$, in any case", "SELECT system$typeof(1)", "system_function", None, None, None),
    ("SYSTEM$, quoted", 'SELECT "SYSTEM$TYPEOF"(1)', "system_function", None, None, None),
    ("SYSTEM$, qualified", "SELECT HARBOUR.GOLD.SYSTEM$X(1)", "system_function", None, None, None),
    ("IDENTIFIER(...)", "SELECT * FROM IDENTIFIER('HARBOUR.GOLD.HOLDINGS')", "dynamic_identifier", None, None, None),
    ("a schema-qualified name", "SELECT * FROM GOLD.HOLDINGS", "unqualified_object", None,
     "the session's database could be any", "Q-GUARD-1"),
    ("an unqualified name", "SELECT * FROM HOLDINGS", "unqualified_object", None, None, "Q-GUARD-1"),
    ("database..object", "SELECT * FROM HARBOUR..HOLDINGS", "unqualified_object", None, "the default schema",
     "Q-GUARD-1"),
    ("a CTE name out of scope", "SELECT * FROM HARBOUR.GOLD.HOLDINGS h WHERE EXISTS (WITH c AS (SELECT 1 a) "
     "SELECT * FROM c) AND h.isin IN (SELECT isin FROM c)", "unqualified_object", None,
     "c is visible only inside the subquery that defines it", None),
    ("an unregistered schema", "SELECT * FROM HARBOUR.BRONZE.RAW", "unregistered_object", None, None, None),
    ("an unregistered object in a registered database", "SELECT * FROM HARBOUR.SILVER.TRADES", "unregistered_object",
     None, "SILVER is registered object by object", None),
    ("an unregistered object beside registered ones", "SELECT * FROM HARBOUR.GOLD.HOLDINGS h JOIN "
     "SNOWFLAKE.ACCOUNT_USAGE.QUERY_HISTORY q ON true", "unregistered_object", None, None, None),
    ("a quoted name is exact", 'SELECT * FROM "harbour".GOLD.HOLDINGS', "unregistered_object", None,
     "\"harbour\" is not HARBOUR", None),
    ("INFORMATION_SCHEMA", "SELECT * FROM HARBOUR.INFORMATION_SCHEMA.TABLES", "unregistered_object", None, None,
     None),
]


def build():
    cases = []
    for name, sql, reason, objects, note, question in CASES:
        ok, got_reason, got_objects = guard.check(sql, REGISTERED)
        want_ok = reason is None
        if ok != want_ok or (not ok and got_reason != reason) or (ok and got_objects != objects):
            raise AssertionError(f"guard.py gives {ok} {got_reason} {got_objects} for {name!r}")
        expected = {"accepted": ok}
        if ok:
            expected["objects"] = got_objects
        else:
            expected["reason"] = got_reason
        case = {"name": name, "statement": sql, "expected": expected}
        if note:
            case["note"] = note
        if question:
            case["question"] = question
        cases.append(case)
    assert len({c["name"] for c in cases}) == len(cases)
    doc = {
        "description": "The read-only guard (§6.4.3): every statement is refused unless it is exactly one read-only "
                       "query (SELECT, UNION, INTERSECT, EXCEPT, WITH) over the registered objects. `registered` "
                       "lists identifiers as Snowflake resolves them (unquoted upper-cased, quoted exact): a "
                       "[database, schema] pair registers every object in the schema, a triple one object. An "
                       "implementation passes a case when it accepts or refuses it as `accepted` says; `reason` "
                       "names the first rule that refuses it, in the order guard.py documents (dml, ddl, copy, "
                       "file_transfer, call, not_a_query, select_into, stage, table_function, system_function, "
                       "dynamic_identifier, unqualified_object, unregistered_object), and `objects` the objects an "
                       "accepted statement reads. Generated with sqlglot 30.27.0's Snowflake parser.",
        "registered": REGISTERED,
        "cases": cases,
    }
    return {PATH: dumps(doc)}
