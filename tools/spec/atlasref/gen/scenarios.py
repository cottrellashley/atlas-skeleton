"""references/scenarios/: one file per scenario of §9.8.4's table, and the
fixtures every scenario's gateway serves (scenarios/fixtures/).

The format (gateway script, runner commands, suite commands, expected
results and request assertions) is documented in README.md, "Scenarios".
"""

import base64
import copy
import decimal
import hashlib

from .. import conformance, fingerprint, grammar, problems, schemas
from ..jsonio import dumps
from . import handoff as handoff_gen
from . import resolution as res_gen

DIR = "scenarios/"
GW = "{gateway}"
NOW = lambda s: {"$now_plus_s": s}
NOW_ISO = lambda s: {"$now_plus_iso": s}

SERVED = conformance.served()
CURRENT = conformance.current()
BUILTIN = conformance.builtin_profiles()
MOVED = conformance.echo_moved()
NEWER = conformance.echo_newer()
EXA = {"evolving": [fingerprint.exact_id(p) for p in conformance.evolving()]}
A_, B_, C_ = EXA["evolving"]
ECHO = CURRENT["conformance.echo@v1"]
ROWS = CURRENT["conformance.rows@v1"]
TICK_A = fingerprint.exact_id(conformance.tick("A"))
TICK_B = fingerprint.exact_id(conformance.tick("B"))
HANDOFF = CURRENT["conformance.handoff@v1"]


def builtin_bindings():
    out = {}
    for exact, p in BUILTIN.items():
        cap = p["identity"]["id"]
        out[cap] = {"version": 1, "interactions": {
            name: {"bindings": [dict(i["bindings"][0])]} for name, i in p["interactions"].items()}}
    return out


def default_bootstrap():
    return {
        "protocols": ["atlas/1.0"],
        "minimum_protocol": "atlas/1.0",
        "environment": "conformance",
        "servers": {"gateway": {"url": GW, "description": "The conformance gateway."}},
        "builtins": builtin_bindings(),
        "sign_in": {"authority": GW + "/oauth", "client_id": "atlas-conformance", "scopes": [GW + "/.default"],
                    "token_endpoint": GW + "/oauth/token", "authorization_endpoint": GW + "/oauth/authorize",
                    "device_authorization_endpoint": GW + "/oauth/device"},
        "catalog_etag": '"catalog-1"',
    }


def token_file(access="ct-valid", expires_in=3600, refresh="rt-valid", mode="0600", dir_mode="0700"):
    body = {"access_token": access, "expires_at": NOW(expires_in)}
    if refresh:
        body["refresh_token"] = refresh
    return {"path": "{ATLAS_HOME}/state/token-conformance.json", "mode": mode, "dir_mode": dir_mode, "json": body}


def setup(files=None, env=None, cwd=None):
    s = {"env": {"ATLAS_URL": GW, "ATLAS_HOME": "{ATLAS_HOME}", "ATLAS_CONFORMANCE": "1"}}
    if env:
        s["env"].update(env)
    s["files"] = files if files is not None else [token_file()]
    if cwd:
        s["cwd"] = cwd
    return s


def cmd(i, op, **args):
    return {"id": i, "op": op, "args": args}


def call(i, address, request=None, **options):
    a = {"address": address, "request": request if request is not None else {}}
    if options:
        a["options"] = options
    return {"id": i, "op": "call", "args": a}


def ok(i, value=None, contains=None, warnings=None, usage=None, **extra):
    r = {"id": i, "ok": True}
    if value is not None:
        r["value"] = value
    if contains is not None:
        r["value_contains"] = contains
    if warnings is not None:
        r["warnings"] = warnings
    if usage is not None:
        r["usage"] = usage
    r.update(extra)
    return r


def fail(i, code, target=None, **members):
    p = {"code": code}
    if target is not None:
        p["target"] = target
    p.update(members)
    return {"id": i, "ok": False, "problem": p}


def sel(address=None, nth=None, **kw):
    s = {}
    if address:
        s["address"] = address
    if nth:
        s["nth"] = nth
    s.update(kw)
    return s


def count(select, n):
    return {"assert": "count", "select": select, "equals": n}


def header(select, name, **kw):
    a = {"assert": "header", "select": select, "name": name}
    a.update(kw)
    return a


def step(match, respond, times=1, label=None):
    s = {"match": match, "respond": respond, "times": times}
    if label:
        s["label"] = label
    return s


def problem_respond(code, status=None, headers=None, **members):
    entry = problems.BY_CODE.get(code)
    p = {"code": code, "retryable": members.pop("retryable", entry["retryable"] if entry else False)}
    p.update(members)
    r = {"status": status or (entry["status"] if entry else 400), "problem": p}
    if headers:
        r["headers"] = headers
    return r


def gateway(**kw):
    g = {}
    for k in ("bootstrap", "current", "serve", "tokens", "headers", "storage", "steps", "bus", "state"):
        if k in kw:
            g[k] = kw[k]
    return g


# --- the default rows of conformance.rows@v1 ---------------------------------

def row(i, columns=None):
    us = grammar.parse_date_time_ns("2026-01-01T00:00:00Z") // 1000 + i * 1_000_001
    r = {"i": i, "label": f"row-{i}", "amount": format(decimal.Decimal(i) * decimal.Decimal("1.5"), ".6f"),
         "at": grammar.format_date_time_us(us),
         "day": grammar.format_date(grammar.days_from_civil(2026, 1, 1) + i),
         "share": None if i % 5 == 4 else i / 100}
    if columns:
        r = {k: r[k] for k in columns}
    return r


def rows_(n, columns=None):
    return [row(i, columns) for i in range(n)]


def end_line(rows, truncated=False, problem=None):
    return {"$atlas": "end", "rows": rows, "truncated": truncated, "usage": {"rows": rows}, "problem": problem}


ECHOED = {"text": "hello", "count": 1, "ratio": None, "when": None, "kind": "plain", "tags": []}


# --- the scenarios ----------------------------------------------------------------

def scenarios():
    S = []

    def scenario(sid, level, description, gw, commands, expected, expected_requests, setup_=None, note=None,
                 questions=None):
        s = {"id": sid, "level": level, "description": description, "setup": setup_ or setup(), "gateway": gw,
             "commands": commands, "expected": expected, "expected_requests": expected_requests}
        if note:
            s["note"] = note
        if questions:
            s["questions"] = questions
        S.append(s)

    W = "atlas.whoami:fetch"
    boot = sel(path="/.well-known/atlas", method="GET")

    # CORE-BOOT-001
    bs = default_bootstrap()
    bs["builtins"]["atlas.whoami"]["interactions"]["fetch"]["bindings"][0]["address"] = "/bootstrapped/who-am-i"
    scenario("CORE-BOOT-001", "Core", "The bootstrap is fetched without a token and revalidated with If-None-Match; "
             "every URI is built from it (here atlas.whoami at an address the bootstrap moved).",
             gateway(bootstrap=bs, headers={"/.well-known/atlas": {"ETag": '"boot-1"'}}),
             [cmd(1, "connect"), cmd(2, "whoami"), cmd(3, "connect"), cmd(4, "whoami")],
             [ok(1), ok(2, contains={"environment": "conformance"}), ok(3), ok(4)],
             [header(sel(path="/.well-known/atlas", nth=1), "Authorization", absent=True),
              header(sel(path="/.well-known/atlas", nth=2), "If-None-Match", equals='"boot-1"'),
              count(sel(path="/bootstrapped/who-am-i", method="POST"), 2),
              count(sel(path="/v1/atlas/atlas.whoami:fetch"), 0)],
             note="The gateway answers the second bootstrap 304 Not Modified to its If-None-Match.")

    # CORE-BOOT-002
    files = [token_file(), {"path": "{ATLAS_HOME}/config.toml", "mode": "0600",
                            "text": 'env = "cfg"\n\n[environments.cfg]\nurl = "{gateway4}"\n\n'
                                    '[environments.envc]\nurl = "{gateway3}"\n'}]
    scenario("CORE-BOOT-002", "Core", "The gateway is chosen in 9.7.2's order: the connect argument, ATLAS_URL, "
             "ATLAS_ENV, config.toml. Four listeners of one conformance gateway stand for four gateways.",
             gateway(),
             [cmd(1, "connect", url="{gateway}"), cmd(2, "whoami"),
              cmd(3, "suite.restart", env={"ATLAS_URL": "{gateway2}", "ATLAS_ENV": "envc"}),
              cmd(4, "connect"), cmd(5, "whoami"),
              cmd(6, "suite.restart", env={"ATLAS_URL": None, "ATLAS_ENV": "envc"}),
              cmd(7, "connect"), cmd(8, "whoami"),
              cmd(9, "suite.restart", env={"ATLAS_URL": None, "ATLAS_ENV": None}),
              cmd(10, "connect"), cmd(11, "whoami")],
             [ok(1), ok(2), ok(3), ok(4), ok(5), ok(6), ok(7), ok(8), ok(9), ok(10), ok(11)],
             [count(sel(W, listener="gateway"), 1), count(sel(W, listener="gateway2"), 1),
              count(sel(W, listener="gateway3"), 1), count(sel(W, listener="gateway4"), 1)],
             setup_=setup(files=files, env={"ATLAS_URL": "{gateway2}"}),
             note="The connect argument wins over ATLAS_URL; with ATLAS_URL unset, ATLAS_ENV (envc, named in "
                  "config.toml) wins over config.toml's env (cfg). Each listener serves its own bootstrap, whose "
                  "environment is conformance, so one token file serves all four.", questions=["Q-CLI-1"])

    # CORE-SIGNIN-001..005
    scenario("CORE-SIGNIN-001", "Core", "The token comes from <ATLAS_HOME>/state/token-<env>.json, <env> being the "
             "bootstrap's environment.",
             gateway(tokens={"access": ["ct-from-file"], "refresh": {}}),
             [cmd(1, "connect"), cmd(2, "whoami")], [ok(1), ok(2)],
             [header(boot, "Authorization", absent=True),
              header(sel(W), "Authorization", equals="Bearer ct-from-file")],
             setup_=setup(files=[token_file(access="ct-from-file")]), questions=["Q-CLI-1"])
    scenario("CORE-SIGNIN-002", "Core", "A token within 60 seconds of expiry is refreshed first, at the bootstrap's "
             "token endpoint; the file is rewritten atomically, mode 0600 in a 0700 directory.",
             gateway(tokens={"access": ["ct-new"], "refresh": {"rt-1": {"access_token": "ct-new",
                                                                         "refresh_token": "rt-2",
                                                                         "expires_in": 3600}}}),
             [cmd(1, "connect"), cmd(2, "whoami"),
              cmd(3, "suite.expect_file", path="{ATLAS_HOME}/state/token-conformance.json", mode="0600",
                  dir_mode="0700", json_contains={"access_token": "ct-new", "refresh_token": "rt-2"})],
             [ok(1), ok(2), ok(3)],
             [{"assert": "order", "selects": [sel(path="/oauth/token", method="POST"), sel(W)]},
              {"assert": "body", "select": sel(path="/oauth/token"), "form_contains": {
                  "grant_type": "refresh_token", "refresh_token": "rt-1"}},
              header(sel(W), "Authorization", equals="Bearer ct-new"),
              count(sel(header_equals={"Authorization": "Bearer ct-old"}), 0)],
             setup_=setup(files=[token_file(access="ct-old", expires_in=30, refresh="rt-1")]))
    scenario("CORE-SIGNIN-003", "Core", "A 401 with error=\"invalid_token\" makes the client refresh and repeat "
             "once; a second 401 is unauthenticated.",
             gateway(tokens={"access": ["ct-valid", "ct-refreshed"], "refresh": {
                 "rt-valid": {"access_token": "ct-refreshed", "refresh_token": "rt-2", "expires_in": 3600},
                 "rt-2": {"access_token": "ct-refreshed", "refresh_token": "rt-3", "expires_in": 3600}}},
                 steps=[step(sel(W, nth=1), problem_respond("unauthenticated", headers={
                     "WWW-Authenticate": 'Bearer realm="atlas", error="invalid_token"'})),
                        step(sel(W, nth=3), problem_respond("unauthenticated", headers={
                            "WWW-Authenticate": 'Bearer realm="atlas", error="invalid_token"'}), times=2)]),
             [cmd(1, "connect"), cmd(2, "whoami"), cmd(3, "whoami")],
             [ok(1), ok(2), fail(3, "unauthenticated")],
             [count(sel(W), 4), count(sel(path="/oauth/token"), 2),
              header(sel(W, nth=2), "Authorization", equals="Bearer ct-refreshed")],
             note="whoami 1 is refused, the token refreshed, whoami 2 succeeds; whoami 3 and its one repeat (4) "
                  "are both refused: unauthenticated, after exactly one repeat.")
    challenge = 'Bearer realm="atlas", claims="eyJhY2Nlc3NfdG9rZW4iOnsiYWNycyI6eyJ2YWx1ZXMiOlsiYzEiXX19fQ"'
    scenario("CORE-SIGNIN-004", "Core", "A conditional-access claims challenge, with no person at the client: "
             "unauthenticated, with the challenge in details.",
             gateway(steps=[step(sel(W), problem_respond("unauthenticated", headers={"WWW-Authenticate": challenge}),
                                 times=5)]),
             [cmd(1, "connect"), cmd(2, "whoami")],
             [ok(1), fail(2, "unauthenticated", details=[{"challenge": challenge}])],
             [count(sel(W), 1), count(sel(path="/oauth/token"), 0)],
             questions=["Q-DOC-9"],
             note="A refreshed token cannot satisfy a claims challenge; only a person signing in again can.")
    scenario("CORE-SIGNIN-005", "Core", "A token file readable by others (mode 0644) is refused, and with no person "
             "the call is unauthenticated; the token is never sent.",
             gateway(), [cmd(1, "connect"), cmd(2, "whoami")], [ok(1), fail(2, "unauthenticated")],
             [count(sel(header_equals={"Authorization": "Bearer ct-valid"}), 0)],
             setup_=setup(files=[token_file(mode="0644")]))

    # CORE-DISC-001, 002
    rows_wrong = copy.deepcopy(SERVED[ROWS])
    rows_wrong["fingerprint"] = "0000000000000000"
    scenario("CORE-DISC-001", "Core", "Catalogue pages are followed by cursor; every profile's fingerprint is "
             "recomputed, and a wrong one is invalid_document.",
             gateway(state={"catalog_page_size": 4},
                     steps=[step(sel("atlas.describe:fetch", body_contains={"id": "conformance.rows@v1"}),
                                 {"status": 200, "describe_profile": rows_wrong})]),
             [cmd(1, "connect"), cmd(2, "catalog", domain="conformance"),
              cmd(3, "describe", id="conformance.echo@v1"), cmd(4, "describe", id="conformance.rows@v1")],
             [ok(1), ok(2, contains={"$count": len(CURRENT) - 2}), ok(3, contains={"profile": {"fingerprint":
                                                                                               ECHO.split("#")[1]}}),
              fail(4, "invalid_document")],
             [count(sel("atlas.catalog:fetch"), 4),
              {"assert": "cursor_chain", "select": sel("atlas.catalog:fetch")}],
             note="13 of the 15 conformance capabilities are in the working view (retired@v1 is retired; "
                  "critical@v1 has a critical trait no client supports, §2.6 rule 2), four to a page: four pages, "
                  "each request carrying the previous page's next_cursor. `$count` in value_contains is the number "
                  "of items the iterator yields.")
    scenario("CORE-DISC-002", "Core", "A profile is fetched once per exact-id, then served from the cache, in "
             "memory and, after a restart, from <ATLAS_HOME>/cache/profiles/.",
             gateway(),
             [cmd(1, "connect"), call(2, "conformance.echo@v1:fetch", {"text": "hello"}),
              call(3, "conformance.echo@v1:fetch", {"text": "hello"}), cmd(4, "suite.restart"), cmd(5, "connect"),
              call(6, "conformance.echo@v1:fetch", {"text": "hello"})],
             [ok(1), ok(2, ECHOED), ok(3, ECHOED), ok(4), ok(5), ok(6, ECHOED)],
             [count(sel("atlas.describe:fetch"), 1), count(sel("conformance.echo@v1:fetch"), 3)])

    # CORE-GUIDE-001
    md = "# conformance.echo@v1\n\n  Echo *anything* back.\t\n\n```python\natlas.call(\"conformance.echo@v1:fetch\", {\"text\": \"hi\"})\n```\n"
    scenario("CORE-GUIDE-001", "Core", "The client asks atlas.guide for its own syntax and returns the Markdown "
             "unchanged.",
             gateway(steps=[step(sel("atlas.guide:fetch"), {"status": 200, "body": {
                 "exact": ECHO, "client": "python", "markdown": md, "tokens": 37, "truncated": False,
                 "revisions": {"governance": "g-1", "context": "c-1"}}})]),
             [cmd(1, "connect"), cmd(2, "guide", id="conformance.echo@v1")],
             [ok(1), ok(2, contains={"markdown": md})],
             [{"assert": "body", "select": sel("atlas.guide:fetch"), "contains": {"by": "id",
                                                                                 "id": "conformance.echo@v1"}},
              {"assert": "body_member_in", "select": sel("atlas.guide:fetch"), "member": "/client",
               "values": ["cli", "python", "node", "go", "mcp"]},
              header(sel("atlas.guide:fetch"), "Atlas-Profile-Expect", absent=True)],
             questions=["Q-RUN-1"])

    # CORE-EXPLAIN-001
    statement = ('SELECT\n  CAST("I" AS NUMBER(18,0)) AS "i"  -- every column aliased and cast\nFROM\tTABLE(GENERATOR('
                 'ROWCOUNT => 3))\nWHERE 1 = 1 /* naïve */')
    explain = {"address": "conformance.rows@v1:fetch", "profile": ROWS,
               "plan": {"language": "sql", "dialect": "snowflake", "statement": statement, "query_tag": "atlas:x"},
               "result_schema": {"type": "array", "items": {"type": "object", "properties": {"i": {"type": "integer"}}}},
               "execution": "gateway", "estimate": {"rows": 3, "bytes": 96, "cost": None}}
    scenario("CORE-EXPLAIN-001", "Core", "explain is validated as fetch is (an invalid request fails before "
             "anything is sent), and its answer is returned whole, the statement byte for byte.",
             gateway(steps=[step(sel("conformance.rows@v1:explain"), {"status": 200, "body": explain})]),
             [cmd(1, "connect"), cmd(2, "explain", id="conformance.rows@v1", request={"n": -1}),
              cmd(3, "explain", id="conformance.rows@v1", request={"n": 3, "columns": ["i"]})],
             [ok(1), fail(2, "invalid_request", target="/n"), ok(3, explain)],
             [count(sel("conformance.rows@v1:explain"), 1)], questions=["Q-RUN-1"])

    # CORE-CALL-001..005
    thing_create = {"name": "conformance/things/t1", "colour": "red", "size": 3}
    every = [header(sel(W), "Atlas-Client", parses=True), header(sel(W), "Atlas-Client-Features", parses=True),
             header(sel(W), "Atlas-Deadline", parses=True), header(sel(W), "traceparent", parses=True),
             header(sel(W), "Atlas-Profile-Expect", absent=True),
             header(sel(W), "Authorization", equals="Bearer ct-valid")]
    for a in ("conformance.echo@v1:fetch", "conformance.thing@v1:create"):
        every += [header(sel(a), h, parses=True) for h in ("Atlas-Client", "Atlas-Client-Features",
                                                            "Atlas-Profile-Expect", "Atlas-Deadline", "traceparent")]
    every += [header(sel("conformance.echo@v1:fetch"), "Atlas-Profile-Expect", equals=ECHO),
              header(sel("conformance.echo@v1:fetch"), "Idempotency-Key", absent=True),
              header(sel("conformance.thing@v1:create"), "Idempotency-Key", parses=True),
              header(sel("conformance.echo@v1:fetch"), "Accept", contains="application/json")]
    scenario("CORE-CALL-001", "Core", "Every request header of §9.6.3 is present and parses as its type; a "
             "built-in carries no Atlas-Profile-Expect; only a call whose effect is not read carries an "
             "Idempotency-Key.",
             gateway(), [cmd(1, "connect"), cmd(2, "whoami"), call(3, "conformance.echo@v1:fetch", {"text": "hello"}),
                         call(4, "conformance.thing@v1:create", thing_create)],
             [ok(1), ok(2), ok(3, ECHOED), ok(4, contains={"name": "conformance/things/t1", "colour": "red"})],
             every)
    scenario("CORE-CALL-002", "Core", "An invalid request fails invalid_request with its first pointer as target "
             "and every failure in details, and nothing is sent.",
             gateway(),
             [cmd(1, "connect"), call(2, "conformance.echo@v1:fetch", {"text": "", "count": 101, "extra": 1})],
             [ok(1), fail(2, "invalid_request", target="/count", details=[
                 {"target": "/count", "keyword": "maximum"}, {"target": "/extra", "keyword": "additionalProperties"},
                 {"target": "/text", "keyword": "minLength"}])],
             [count(sel("conformance.echo@v1:fetch"), 0)], questions=["Q-VAL-1", "Q-DOC-9"])
    scenario("CORE-CALL-003", "Core", "The client follows the binding the profile serves, here at an unusual "
             "address; it never builds a path.",
             gateway(current={"conformance.echo@v1": fingerprint.exact_id(MOVED)}),
             [cmd(1, "connect"), call(2, "conformance.echo@v1:fetch", {"text": "hello"})], [ok(1), ok(2, ECHOED)],
             [count(sel(path="/elsewhere/7/echo;v=1", method="POST"), 1),
              count(sel(path="/v1/data/conformance.echo@v1:fetch"), 0)])
    scenario("CORE-CALL-004", "Core", "A write refused once with 503 (retryable) is retried with the same "
             "Idempotency-Key.",
             gateway(steps=[step(sel("conformance.thing@v1:create", nth=1), problem_respond(
                 "unavailable", headers={"Retry-After": "1"}, retry_after=1))]),
             [cmd(1, "connect"), call(2, "conformance.thing@v1:create", thing_create)],
             [ok(1), ok(2, contains={"name": "conformance/things/t1"})],
             [count(sel("conformance.thing@v1:create"), 2),
              {"assert": "same_header", "name": "Idempotency-Key",
               "selects": [sel("conformance.thing@v1:create", nth=1), sel("conformance.thing@v1:create", nth=2)]},
              {"assert": "interval", "from": sel("conformance.thing@v1:create", nth=1),
               "to": sel("conformance.thing@v1:create", nth=2), "min_ms": 1000}])
    req5 = {"text": "na" + chr(0xEF) + "ve é", "count": 2, "ratio": 0.5, "when": "2026-10-31", "kind": "loud",
            "tags": ["b", "a"]}
    from .. import jcs
    scenario("CORE-CALL-005", "Core", "The same inputs give the same request record in every client, byte for "
             "byte once canonicalised by RFC 8785 (AP-CLIENT-003).",
             gateway(), [cmd(1, "connect"), call(2, "conformance.echo@v1:fetch", req5)],
             [ok(1), ok(2, contains={"text": req5["text"], "kind": "loud"})],
             [{"assert": "body", "select": sel("conformance.echo@v1:fetch"), "jcs": jcs.canonical(req5)}])

    # CORE-PROB-001, 002
    codes = [c for c in problems.REGISTRY if c["code"] != "output_limit_exceeded"]
    steps_, cmds, exps = [], [cmd(1, "connect")], [ok(1)]
    for k, c in enumerate(codes, start=2):
        repeats = 4 if c["retryable"] else (2 if c["code"] == "unauthenticated" else 1)
        members = {"target": "/text", "detail": f"conformance: {c['code']}"}
        hdrs = {}
        if c["retryable"]:
            members["retry_after"] = 0
            hdrs["Retry-After"] = "0"
        if c["code"] == "confirmation_required":
            members["estimate"] = {"amount": "1.000", "unit": "credits"}
        if c["code"] == "unauthenticated":
            hdrs["WWW-Authenticate"] = 'Bearer realm="atlas", error="invalid_token"'
        steps_.append(step(sel("conformance.echo@v1:fetch", body_contains={"text": c["code"]}),
                           problem_respond(c["code"], headers=hdrs or None, **members), times=repeats))
        cmds.append(call(k, "conformance.echo@v1:fetch", {"text": c["code"]}))
        exps.append(fail(k, c["code"], target="/text", status=c["status"], retryable=c["retryable"],
                         detail=f"conformance: {c['code']}", type=problems.TYPE_BASE + c["code"],
                         cli_exit=problems.cli_exit(c["code"])))
    scenario("CORE-PROB-001", "Core", "Each code of the registry in turn becomes the language's error with every "
             "member of the problem; a CLI runner reports §9.7.6's exit code as cli_exit.",
             gateway(steps=steps_), cmds, exps,
             [count(sel("conformance.echo@v1:fetch", body_contains={"text": c["code"]}),
                    4 if c["retryable"] else (2 if c["code"] == "unauthenticated" else 1)) for c in codes],
             note="Retryable codes are answered four times (the call and its three retries, with Retry-After 0); "
                  "unauthenticated twice (the call and its one repeat after a refresh). output_limit_exceeded only "
                  "travels in an end line (STREAM-004).", questions=["Q-RUN-2"])
    scenario("CORE-PROB-002", "Core", "A code no client knows is handled by its status and retryable.",
             gateway(steps=[
                 step(sel("conformance.echo@v1:fetch", body_contains={"text": "a"}), problem_respond(
                     "conformance_strange", status=503, headers={"Retry-After": "0"}, retryable=True, retry_after=0),
                      times=4),
                 step(sel("conformance.echo@v1:fetch", body_contains={"text": "b"}), problem_respond(
                     "conformance_odd", status=422, retryable=False))]),
             [cmd(1, "connect"), call(2, "conformance.echo@v1:fetch", {"text": "a"}),
              call(3, "conformance.echo@v1:fetch", {"text": "b"})],
             [ok(1), fail(2, "conformance_strange", status=503, retryable=True, cli_exit=1),
              fail(3, "conformance_odd", status=422, retryable=False, cli_exit=1)],
             [count(sel("conformance.echo@v1:fetch", body_contains={"text": "a"}), 4),
              count(sel("conformance.echo@v1:fetch", body_contains={"text": "b"}), 1)])

    # CORE-RETRY-001
    E = "conformance.echo@v1:fetch"
    scenario("CORE-RETRY-001", "Core", "429 and 503 with Retry-After: at most three retries, each no sooner than "
             "Retry-After, never past the deadline; a problem that is not retryable is not retried.",
             gateway(steps=[
                 step(sel(E, body_contains={"text": "x"}, nth=1), problem_respond("limit_exceeded", headers={
                     "Retry-After": "1"}, retry_after=1)),
                 step(sel(E, body_contains={"text": "x"}, nth=2), problem_respond("unavailable", headers={
                     "Retry-After": "2"}, retry_after=2)),
                 step(sel(E, body_contains={"text": "y"}), problem_respond("unavailable", headers={
                     "Retry-After": "0"}, retry_after=0), times=10),
                 step(sel(E, body_contains={"text": "z"}), problem_respond("unavailable", headers={
                     "Retry-After": "2"}, retry_after=2), times=10),
                 step(sel(E, body_contains={"text": "w"}), problem_respond("denied"), times=10)]),
             [cmd(1, "connect"), call(2, E, {"text": "x"}), call(3, E, {"text": "y"}),
              call(4, E, {"text": "z"}, deadline_ms=3000), call(5, E, {"text": "w"})],
             [ok(1), ok(2, contains={"text": "x"}), fail(3, "unavailable"), fail(4, "unavailable"),
              fail(5, "denied")],
             [count(sel(E, body_contains={"text": "x"}), 3),
              {"assert": "interval", "from": sel(E, body_contains={"text": "x"}, nth=1),
               "to": sel(E, body_contains={"text": "x"}, nth=2), "min_ms": 1000},
              {"assert": "interval", "from": sel(E, body_contains={"text": "x"}, nth=2),
               "to": sel(E, body_contains={"text": "x"}, nth=3), "min_ms": 2000},
              count(sel(E, body_contains={"text": "y"}), 4),
              count(sel(E, body_contains={"text": "z"}), 2),
              count(sel(E, body_contains={"text": "w"}), 1),
              {"assert": "same_header", "name": "Atlas-Deadline", "selects": [
                  sel(E, body_contains={"text": "z"}, nth=1), sel(E, body_contains={"text": "z"}, nth=2)]}],
             note="z: the deadline is 3 s away; the first retry is at 2 s, a second would be at 4 s, past it, so "
                  "the client raises the problem it holds. A retry keeps the call's deadline.")

    # CORE-DEADLINE-001
    scenario("CORE-DEADLINE-001", "Core", "Atlas-Deadline is the interaction's default when the call names none, "
             "capped at its maximum; a call still running at its deadline ends deadline_exceeded.",
             gateway(steps=[step(sel(E, body_contains={"text": "slow"}), {"delay_ms": 5000, "serve": True})]),
             [cmd(1, "connect"), call(2, E, {"text": "a"}), call(3, E, {"text": "b"}, deadline_ms=600000),
              call(4, E, {"text": "slow"}, deadline_ms=1000)],
             [ok(1), ok(2), ok(3), fail(4, "deadline_exceeded")],
             [{"assert": "deadline", "select": sel(E, body_contains={"text": "a"}), "min_s": 28, "max_s": 31},
              {"assert": "deadline", "select": sel(E, body_contains={"text": "b"}), "min_s": 118, "max_s": 121},
              {"assert": "deadline", "select": sel(E, body_contains={"text": "slow"}), "min_s": 0, "max_s": 2}],
             note="conformance.echo@v1:fetch has deadline default PT30S and max PT2M; Atlas-Deadline is in whole "
                  "seconds, rounded down (Q-HDR-2).", questions=["Q-HDR-2"])

    # CORE-TOL-001
    newer = fingerprint.exact_id(NEWER)
    scenario("CORE-TOL-001", "Core", "Unknown members, headers, enum values and traits are ignored; an unknown "
             "enum value reads UNKNOWN.",
             gateway(serve=[newer], steps=[step(sel(E), {"status": 200, "profile": newer, "headers": {
                 "Atlas-Future": "?1", "X-Something-New": "1"}, "body": {
                 "text": "hello", "count": 1, "ratio": None, "when": None, "kind": "whisper", "tags": [],
                 "echoed_at": "2026-10-31T00:00:00Z", "zzz": {"nested": True}}})]),
             [cmd(1, "connect"), call(2, E, {"text": "hello"})],
             [ok(1), ok(2, dict(ECHOED, kind="UNKNOWN"))],
             [{"assert": "describe_exact", "exact": newer, "count": 1}],
             note="The answer is written under a newer profile of the version (its kind 'whisper', its member "
                  "echoed_at, its trait FutureTrait); the client fetches it by exact and resolves it to the "
                  "profile it called with: whisper reads UNKNOWN, echoed_at and zzz are dropped.")

    # CORE-CRIT-001
    scenario("CORE-CRIT-001", "Core", "A trait marked critical that the client does not implement: "
             "critical_trait_unsupported, and no call is made.",
             gateway(), [cmd(1, "connect"), call(2, "conformance.critical@v1:fetch", {"text": "x"})],
             [ok(1), fail(2, "critical_trait_unsupported")],
             [count(sel("conformance.critical@v1:fetch"), 0)])

    # CORE-RES-001..003
    lock_a = {"path": "{WORKDIR}/atlas.lock", "mode": "0644", "text": "[capabilities]\n\"conformance.evolving\" = "
              "{ version = 1, fingerprint = \"" + A_.split("#")[1] + "\" }\n"}
    EV = "conformance.evolving@v1:fetch"
    scenario("CORE-RES-001", "Core", "An answer under a newer fingerprint: the client fetches the writer's profile "
             "(and the chain's) by exact, applies each lens backward, and returns its own shape.",
             gateway(steps=[step(sel(EV), {"status": 200, "profile": C_, "body": [res_gen.ROW_C]})]),
             [cmd(1, "connect"), call(2, EV, {"ids": ["p1"]}, accept=["application/json"])],
             [ok(1), ok(2, [{"id": "p1", "mv": 1250.5, "weight": 1.5, "status": "sold", "count": 3}])],
             [header(sel(EV), "Atlas-Profile-Expect", equals=A_),
              {"assert": "describe_exact", "exact": C_, "count": 1},
              {"assert": "describe_exact", "exact": B_, "count": 1}],
             setup_=setup(files=[token_file(), lock_a], cwd="{WORKDIR}"),
             note="The same record as resolution.json's case 'through two lenses backward'.")
    lock_c = dict(lock_a, text=lock_a["text"].replace(A_.split("#")[1], C_.split("#")[1]))
    scenario("CORE-RES-002", "Core", "Rows answering a request that named columns are valid against the result "
             "schema projected to those columns (§9.5.2 step 5).",
             gateway(steps=[step(sel(EV), {"status": 200, "profile": C_, "body": [
                 {"id": "p1", "weight": 0.015}, {"id": "p2", "weight": 0.25}]})]),
             [cmd(1, "connect"), call(2, EV, {"ids": ["p1", "p2"], "columns": ["id", "weight"]},
                                      accept=["application/json"])],
             [ok(1), ok(2, [{"id": "p1", "weight": 0.015}, {"id": "p2", "weight": 0.25}])], [],
             setup_=setup(files=[token_file(), lock_c], cwd="{WORKDIR}"))
    scenario("CORE-RES-003", "Core", "A request written under an older, locked profile: Atlas-Profile-Expect names "
             "it, the body is written under it, and the gateway resolves it forward.",
             gateway(), [cmd(1, "connect"), call(2, EV, {"ids": ["p1"], "region": "EU"},
                                                 accept=["application/json"])],
             [ok(1), ok(2)],
             [header(sel(EV), "Atlas-Profile-Expect", equals=A_),
              {"assert": "body", "select": sel(EV), "equals": {"ids": ["p1"], "region": "EU"}}],
             setup_=setup(files=[token_file(), lock_a], cwd="{WORKDIR}"),
             note="The conformance gateway resolves the request #A → #C (region becomes area) before validating it "
                  "against #C, and answers under #C; the client resolves the rows back to #A.")

    # CORE-LOCK-001
    lock_far = {"path": "{WORKDIR}/app/atlas.lock", "mode": "0644", "text": lock_a["text"] +
                "\"conformance.echo\" = { version = 1, fingerprint = \"" + ECHO.split("#")[1] + "\" }\n"}
    scenario("CORE-LOCK-001", "Core", "atlas.lock is found in the nearest ancestor of the working directory; its "
             "entries are the reader profiles, fetched by exact, with no discovery call for them.",
             gateway(), [cmd(1, "connect"), call(2, "conformance.echo@v1:fetch", {"text": "hello"}),
                         call(3, EV, {"ids": ["p1"]}, accept=["application/json"])],
             [ok(1), ok(2, ECHOED), ok(3)],
             [count(sel("atlas.catalog:fetch"), 0), count(sel("atlas.search:fetch"), 0),
              count(sel("atlas.describe:fetch", body_contains={"by": "id"}), 0),
              header(sel(EV), "Atlas-Profile-Expect", equals=A_),
              header(sel("conformance.echo@v1:fetch"), "Atlas-Profile-Expect", equals=ECHO)],
             setup_=setup(files=[token_file(), lock_far, {"path": "{WORKDIR}/app/src/deep/.keep", "text": ""}],
                          cwd="{WORKDIR}/app/src/deep"))

    # CORE-DEPR-001
    scenario("CORE-DEPR-001", "Core", "Deprecation and Sunset on an answer: a warning naming what is deprecated, "
             "its sunset and its replacement.",
             gateway(headers={"conformance.evolving@v1:fetch": {"Deprecation": "@1790812800",
                                                                "Sunset": "Sun, 31 Jan 2027 00:00:00 GMT"}}),
             [cmd(1, "connect"), call(2, EV, {"ids": ["p1"]}, accept=["application/json"])],
             [ok(1), ok(2, warnings=[{"code": "deprecated", "text_contains": [
                 "conformance.evolving@v1", "2027-01-31", "conformance.evolving@v2"]}])], [],
             questions=["Q-RUN-3"],
             note="The sunset and the replacement are those of conformance.evolving@v1's profile (lifecycle "
                  "deprecated, sunset 2027-01-31, replaced_by conformance.evolving@v2).")

    # CORE-OP-001
    S_ = "conformance.slow@v1:fetch"
    scenario("CORE-OP-001", "Core", "202 and an operation: polls no sooner than Retry-After, then the default "
             "doubling from 1 s to at most 30 s; reads result, or result_location when it is larger than 1 MiB; "
             "cancel reaches cancelled.",
             gateway(),
             [cmd(1, "connect"), call(2, S_, {"polls": 2}), cmd(3, "operation.wait", name="{operation:2}",
                                                                timeout_ms=30000),
              call(4, S_, {"polls": 0, "result_bytes": 1100000}),
              cmd(5, "operation.wait", name="{operation:4}", timeout_ms=30000),
              call(6, S_, {"polls": 20}), cmd(7, "operation.cancel", name="{operation:6}"),
              cmd(8, "operation.get", name="{operation:6}")],
             [ok(1), ok(2, contains={"address": S_, "state": {"$in": ["pending", "running"]}}),
              ok(3, {"done": True, "padding": "x" * 16}), ok(4), ok(5, contains={"done": True, "$length": {
                  "/padding": 1100000}}), ok(6), ok(7), ok(8, contains={"state": "cancelled"})],
             [{"assert": "poll_intervals", "operation": "{operation:2}", "min_ms": [1000, 1000, 2000]},
              count(sel(path_prefix="/v1/operations/", path_suffix="/result", method="GET"), 1),
              header(sel(path_prefix="/v1/operations/", path_suffix="/result"), "Authorization",
                     equals="Bearer ct-valid"),
              count(sel(path_suffix=":cancel", method="POST"), 1)],
             note="`call` on an operation delivery returns the operation (its document); operation.wait polls it "
                  "to its end and returns its result. The gateway answers 202 with Retry-After: 1 and each poll "
                  "before the last without one, so the client waits 1, 1, 2 s. {operation:N} is the name of the "
                  "operation command N returned.", questions=["Q-RUN-5"])

    # CORE-CONFIRM-001
    CO = "conformance.costly@v1:fetch"
    scenario("CORE-CONFIRM-001", "Core", "confirmation_required with an estimate: without a person, the problem; a "
             "person who declines, the problem; a person who accepts, one repeat with Atlas-Confirm.",
             gateway(),
             [cmd(1, "connect"), call(2, CO, {"text": "a"}), call(3, CO, {"text": "b"}, person=True, confirm="decline"),
              call(4, CO, {"text": "c"}, person=True, confirm="accept")],
             [ok(1), fail(2, "confirmation_required", estimate={"amount": "12.500", "unit": "credits"}),
              fail(3, "confirmation_required"), ok(4, {"ok": True})],
             [count(sel(CO, body_contains={"text": "a"}), 1), count(sel(CO, body_contains={"text": "b"}), 1),
              count(sel(CO, body_contains={"text": "c"}), 2),
              header(sel(CO, body_contains={"text": "c"}, nth=2), "Atlas-Confirm", equals="12.5"),
              header(sel(CO, body_contains={"text": "c"}, nth=1), "Atlas-Confirm", absent=True)],
             note="conformance.costly@v1 answers confirmation_required (estimate 12.500 credits) unless Atlas-Confirm "
                  "is at least 12.5; it is a read, so no Idempotency-Key is sent (§9.6.3).", questions=["Q-DOC-10"])

    # CORE-CONTENT-001, 002
    small = b"hello, atlas\n"
    small_meta = {"name": "small.txt", "media_type": "text/plain", "bytes": len(small),
                  "sha256": hashlib.sha256(small).hexdigest()}
    FG = "conformance.file@v1:get"
    scenario("CORE-CONTENT-001", "Core", "Small content: with no path, the bytes in memory with their metadata and "
             "nothing on disk; with a path, the file there, written atomically, beside atlas-manifest.json.",
             gateway(state={"files": {"small.txt": {"base64": base64.b64encode(small).decode(), "media_type":
                                                    "text/plain"}}}),
             [cmd(1, "connect"), call(2, FG, {"name": "small.txt"}),
              cmd(3, "suite.expect_file", path="{ATLAS_HOME}/downloads", absent=True),
              call(4, FG, {"name": "small.txt"}, path="{WORKDIR}/out/"),
              cmd(5, "suite.expect_file", path="{WORKDIR}/out/small.txt", base64=base64.b64encode(small).decode()),
              cmd(6, "suite.expect_file", path="{WORKDIR}/out/atlas-manifest.json", json_contains={"files": [
                  {"name": "small.txt", "media_type": "text/plain", "bytes": len(small), "sha256": small_meta["sha256"],
                   "address": FG, "metadata": small_meta}]})],
             [ok(1), ok(2, {"content": {"base64": base64.b64encode(small).decode(), "metadata": small_meta}}), ok(3),
              ok(4, contains={"manifest": {"files": [{"name": "small.txt", "path": "{WORKDIR}/out/small.txt"}]}}),
              ok(5), ok(6)],
             [], setup_=setup(cwd="{WORKDIR}"), questions=["Q-DOC-14", "Q-RUN-4"])
    big = bytes(range(256)) * 16
    big_meta = {"name": "big.bin", "media_type": "application/octet-stream", "bytes": len(big),
                "sha256": hashlib.sha256(big).hexdigest()}
    scenario("CORE-CONTENT-002", "Core", "Content above max_memory lands at <ATLAS_HOME>/downloads/<date>/<request "
             "id>/<name> with its manifest; a wrong sha256 leaves no file and is invalid_document.",
             gateway(state={"files": {"big.bin": {"base64": base64.b64encode(big).decode(),
                                                  "media_type": "application/octet-stream"},
                                      "bad.bin": {"base64": base64.b64encode(big).decode(),
                                                  "media_type": "application/octet-stream", "sha256": "0" * 64}}},
                     steps=[step(sel(FG, body_contains={"name": "big.bin"}), {"serve": True, "headers": {
                         "X-Request-Id": "req-content-2"}})]),
             [cmd(1, "connect"), call(2, FG, {"name": "big.bin"}, max_memory=1024),
              cmd(3, "suite.expect_file", path="{ATLAS_HOME}/downloads/{today}/req-content-2/big.bin",
                  base64=base64.b64encode(big).decode()),
              cmd(4, "suite.expect_file", path="{ATLAS_HOME}/downloads/{today}/req-content-2/atlas-manifest.json",
                  json_contains={"files": [{"name": "big.bin", "request_id": "req-content-2"}]}),
              call(5, FG, {"name": "bad.bin"}, max_memory=1024),
              cmd(6, "suite.expect_file", path="{ATLAS_HOME}/downloads/{today}/*/bad.bin", absent=True)],
             [ok(1), ok(2, contains={"manifest": {"files": [{"name": "big.bin", "bytes": len(big)}]}}), ok(3), ok(4),
              fail(5, "invalid_document"), ok(6)],
             [], note="{today} is the UTC date the content landed; the request id is the X-Request-Id of the call's "
                      "first answer.", questions=["Q-RUN-4"])

    # CORE-ENV-001
    TH = "conformance.thing@v1:"
    thing = {"name": "conformance/things/t1", "colour": "red", "size": 3}
    scenario("CORE-ENV-001", "Core", "The resource envelope: an unknown condition kept; status changing without "
             "the ETag; an update built from the record sends its own fields and labels only, with If-Match from "
             "metadata.etag, and succeeds after the status change; a deleting record reads back.",
             gateway(state={"things": [{"name": "conformance/things/t1", "colour": "red", "size": 3,
                                        "labels": {"desk": "credit"}, "etag": '"e1"', "generation": 1,
                                        "conditions": [{"type": "ready", "status": "true", "reason": "converged"},
                                                       {"type": "quantum", "status": "unknown",
                                                        "reason": "entangled"}]}]},
                     steps=[step(sel(TH + "update"), {"serve": True, "before": {"set_status": {
                         "name": "conformance/things/t1", "observed_generation": 1, "conditions": [
                             {"type": "ready", "status": "false", "reason": "reconverging"}]}}}),
                            step(sel(TH + "delete"), {"serve": True, "deleting": True})]),
             [cmd(1, "connect"), call(2, TH + "get", {"name": thing["name"]}),
              call(3, TH + "update", "{value:2}", fields=["colour"], set={"colour": "blue"}),
              call(4, TH + "delete", {"name": thing["name"]}, if_match="{value:3/metadata/etag}"),
              call(5, TH + "get", {"name": thing["name"]})],
             [ok(1), ok(2, contains={"metadata": {"etag": '"e1"', "generation": 1, "labels": {"desk": "credit"}},
                                     "status": {"conditions": [{"type": "ready"}, {"type": "quantum",
                                                                                    "reason": "entangled"}]}}),
              ok(3, contains={"colour": "blue", "metadata": {"generation": 2}}),
              ok(4, contains={"metadata": {"deleting": {"$present": True}}}),
              ok(5, contains={"name": thing["name"], "metadata": {"deleting": {"$present": True}}})],
             [header(sel(TH + "update"), "If-Match", equals='"e1"'),
              {"assert": "body", "select": sel(TH + "update"), "equals": {
                  "name": "conformance/things/t1", "fields": ["colour"], "colour": "blue", "size": 3,
                  "metadata": {"labels": {"desk": "credit"}}}},
              header(sel(TH + "delete"), "If-Match", parses=True)],
             note="Command 3 passes the record command 2 returned ({value:2}) as the request, with options fields "
                  "and set: the client builds the update from it, dropping status and every metadata member but "
                  "labels, and sends If-Match from metadata.etag. The gateway's controller changed status (not the "
                  "ETag) before the update arrived, so it succeeds.", questions=["Q-RUN-4"])

    # CORE-UPGRADE-001
    scenario("CORE-UPGRADE-001", "Core", "A client below the gateway's minimum protocol: 426, upgrade_required.",
             gateway(bootstrap={"protocols": ["atlas/1.0", "atlas/2.0"], "minimum_protocol": "atlas/2.0"},
                     steps=[step(sel(W), problem_respond("upgrade_required"), times=5)]),
             [cmd(1, "connect"), cmd(2, "whoami")], [ok(1), fail(2, "upgrade_required")],
             [count(sel(W), 1)],
             note="Whether connect itself refuses a bootstrap whose minimum is above the client's protocol is not "
                  "said; the suite accepts either connect failing upgrade_required, or whoami doing so.")

    # STREAM-001..004
    R = "conformance.rows@v1:fetch"
    nd = ["application/x-ndjson"]
    scenario("STREAM-001", "Streaming", "NDJSON rows, then the end line: every row, then the end line's usage.",
             gateway(), [cmd(1, "connect"), call(2, R, {"n": 7}, accept=nd)],
             [ok(1), ok(2, rows_(7), usage={"rows": 7})],
             [header(sel(R), "Accept", contains="application/x-ndjson")], questions=["Q-RUN-5"])
    scenario("STREAM-002", "Streaming", "A stream that ends without its end line: upstream_failed.",
             gateway(steps=[step(sel(R), {"status": 200, "stream": {"encoding": "ndjson", "rows": rows_(3),
                                                                     "end": None}})]),
             [cmd(1, "connect"), call(2, R, {"n": 3}, accept=nd)], [ok(1), fail(2, "upstream_failed")], [])
    scenario("STREAM-003", "Streaming", "A problem in the end line after rows is raised after the rows.",
             gateway(steps=[step(sel(R), {"status": 200, "stream": {"encoding": "ndjson", "rows": rows_(2),
                                                                     "end": end_line(2, problem={
                                                                         "code": "internal", "retryable": False})}})]),
             [cmd(1, "connect"), call(2, R, {"n": 5}, accept=nd)],
             [ok(1), {"id": 2, "ok": False, "problem": {"code": "internal"}, "value": rows_(2)}], [],
             questions=["Q-RUN-6"])
    scenario("STREAM-004", "Streaming", "An answer truncated at the output limit: the rows sent, and a warning "
             "output_limit_exceeded.",
             gateway(steps=[step(sel(R), {"status": 200, "stream": {"encoding": "ndjson", "rows": rows_(4),
                                                                     "end": end_line(4, truncated=True, problem={
                                                                         "code": "output_limit_exceeded",
                                                                         "status": 200, "retryable": False})}})]),
             [cmd(1, "connect"), call(2, R, {"n": 100}, accept=nd)],
             [ok(1), ok(2, rows_(4), warnings=[{"code": "output_limit_exceeded"}])], [], questions=["Q-DOC-15"])

    # COL-001..003
    ar = ["application/vnd.apache.arrow.stream"]
    scenario("COL-001", "Columnar", "Arrow with its metadata and the zero-row final batch gives the same records "
             "as NDJSON: decimals exact, timestamps UTC microseconds.",
             gateway(), [cmd(1, "connect"), call(2, R, {"n": 6}, accept=ar), call(3, R, {"n": 6}, accept=nd)],
             [ok(1), ok(2, rows_(6)), ok(3, rows_(6))],
             [header(sel(R, nth=1), "Accept", contains="application/vnd.apache.arrow.stream")],
             questions=["Q-TIME-1"])
    scenario("COL-002", "Columnar", "An Arrow stream without its zero-row final batch: upstream_failed.",
             gateway(steps=[step(sel(R), {"status": 200, "stream": {"encoding": "arrow", "rows": rows_(3),
                                                                     "end": None}})]),
             [cmd(1, "connect"), call(2, R, {"n": 3}, accept=ar)], [ok(1), fail(2, "upstream_failed")], [])
    scenario("COL-003", "Columnar", "An Arrow answer under a newer fingerprint: column-wise resolution equals "
             "row-wise.",
             gateway(steps=[step(sel(EV), {"status": 200, "profile": C_, "stream": {
                 "encoding": "arrow", "rows": [res_gen.ROW_C, dict(res_gen.ROW_C, id="p2", status="open")],
                 "end": "default"}})]),
             [cmd(1, "connect"), call(2, EV, {"ids": ["p1", "p2"]}, accept=ar)],
             [ok(1), ok(2, [{"id": "p1", "mv": 1250.5, "weight": 1.5, "status": "sold", "count": 3},
                            {"id": "p2", "mv": 1250.5, "weight": 1.5, "status": "open", "count": 3}])],
             [], setup_=setup(files=[token_file(), lock_a], cwd="{WORKDIR}"))

    # BULK-001..003
    X = "conformance.rows@v1:export"
    scenario("BULK-001", "Bulk", "An export to parts, one failing once: every part fetched and checked by its "
             "sha256, the failed one again.",
             gateway(state={"export_parts": 3},
                     steps=[step(sel(storage_part=2, method="GET", nth=1), {"status": 500, "body": None})]),
             [cmd(1, "connect"), cmd(2, "export", id="conformance.rows@v1", request={"n": 100}, format="parquet",
                                     path="{WORKDIR}/export/")],
             [ok(1), ok(2, contains={"manifest": {"files": [{"media_type": "application/vnd.apache.parquet"}]}})],
             [count(sel(storage_part=1, method="GET"), 1), count(sel(storage_part=2, method="GET"), 2),
              count(sel(storage_part=3, method="GET"), 1)], setup_=setup(cwd="{WORKDIR}"))
    blob = 20 * 1024 * 1024
    scenario("BULK-002", "Bulk", "An upload in parts of at most 8 MiB, then one commit with the same "
             "Idempotency-Key as the call.",
             gateway(),
             [cmd(1, "connect"),
              cmd(2, "suite.file", path="{WORKDIR}/bundle.bin", size=blob, pattern="0123456789abcdef"),
              cmd(3, "upload", address="conformance.file@v1:publish", request={
                  "name": "bundle.bin", "media_type": "application/octet-stream", "bytes": blob,
                  "sha256": "{sha256:{WORKDIR}/bundle.bin}"}, source="{WORKDIR}/bundle.bin")],
             [ok(1), ok(2), ok(3, contains={"name": "bundle.bin", "bytes": blob})],
             [{"assert": "max_body", "select": sel(storage_upload=True, method="PUT"), "bytes": 8388608},
              count(sel(storage_upload=True, method="PUT"), 3),
              count(sel(path_suffix=":commit", method="POST"), 1),
              {"assert": "same_header", "name": "Idempotency-Key", "selects": [
                  sel("conformance.file@v1:publish"), sel(path_suffix=":commit")]}],
             note="20 MiB in parts of 8, 8 and 4 MiB, as the transfer document lists them.", questions=["Q-RUN-4"])
    scenario("BULK-003", "Bulk", "A link that has expired: the client asks again for fresh links, and finishes.",
             gateway(state={"export_parts": 2},
                     steps=[step(sel(storage_part=1, method="GET", nth=1), {"status": 403, "body": None,
                                                                            "expire_links": True})]),
             [cmd(1, "connect"), cmd(2, "export", id="conformance.rows@v1", request={"n": 10}, format="parquet",
                                     path="{WORKDIR}/export/")],
             [ok(1), ok(2)],
             [count(sel("atlas.operation:get"), 1), count(sel(storage_part=1, method="GET"), 2)],
             setup_=setup(cwd="{WORKDIR}"))

    # EVT-001..004
    T = "conformance.tick@v1"
    ev = lambda i, profile, data, eid=None: {"id": eid or f"tick-{i}", "source": "conformance",
                                             "type": "com.jhg.fiquant.conformance.tick.v1", "time": NOW_ISO(0),
                                             "dataschema": "urn:atlas:profile:" + profile.replace("#", ":"),
                                             "datacontenttype": "application/json", "specversion": "1.0",
                                             "subject": "ticker", "data": data}
    scenario("EVT-001", "Events", "subscribe answers an emulator connect handover; the bus sends events, one "
             "twice and one under an older profile: each is yielded once, resolved.",
             gateway(bus=[{"after": "connected", "send": [ev(1, TICK_B, {"seq": 1, "n": 10}),
                                                          ev(2, TICK_B, {"seq": 2, "n": 20}),
                                                          ev(2, TICK_B, {"seq": 2, "n": 20}),
                                                          ev(3, TICK_A, {"seq": 3, "count": 30})]}]),
             [cmd(1, "connect"), cmd(2, "subscribe.open", id=T), cmd(3, "subscribe.next", subscription=2),
              cmd(4, "subscribe.next", subscription=2), cmd(5, "subscribe.next", subscription=2),
              cmd(6, "subscribe.next", subscription=2, timeout_ms=1500), cmd(7, "subscribe.close", subscription=2)],
             [ok(1), ok(2), ok(3, contains={"data": {"seq": 1, "n": 10}}), ok(4, contains={"data": {"seq": 2, "n": 20}}),
              ok(5, contains={"data": {"seq": 3, "n": 30}}), ok(6, {"none": True}), ok(7)],
             [count(sel(T + ":subscribe"), 1)],
             note="The client resolves tick-3 from #A (count) to #B (n) by #B's lens; tick-2's second delivery is "
                  "dropped by id. subscribe.next with a timeout answers {\"none\": true} when nothing arrives.",
             questions=["Q-RUN-7"])
    scenario("EVT-002", "Events", "publish validates the payload first, then sends a CloudEvent with 9.7.8's "
             "attributes over the handover.",
             gateway(), [cmd(1, "connect"), cmd(2, "publish", id=T, payload={"seq": 1, "n": "ten"}),
                         cmd(3, "publish", id=T, payload={"seq": 1, "n": 10})],
             [ok(1), fail(2, "invalid_request", target="/n"), ok(3, contains={"id": {"$uuid": True}})],
             [count(sel(T + ":publish"), 1),
              {"assert": "bus", "count": 1, "contains": {"type": "com.jhg.fiquant.conformance.tick.v1",
                                                         "dataschema": "urn:atlas:profile:" + TICK_B.replace("#", ":"),
                                                         "specversion": "1.0", "data": {"seq": 1, "n": 10}},
               "uuid": "/id", "recent": "/time", "equals_client_name": "/source"}],
             questions=["Q-EVT-1"])
    scenario("EVT-003", "Events", "An emulator connect handover from a gateway whose environment is dv: refused "
             "(AP-EVENTS-002).",
             gateway(bootstrap={"environment": "dv"}),
             [cmd(1, "connect"), cmd(2, "subscribe.open", id=T)],
             [ok(1), fail(2, "invalid_document")], [{"assert": "bus_connections", "equals": 0}],
             setup_=setup(files=[dict(token_file(), path="{ATLAS_HOME}/state/token-dv.json")]),
             note="With environment dv the token file is token-dv.json (Q-CLI-1).")
    scenario("EVT-004", "Events", "A grant revoked while subscribed: Service Bus refuses the connection, the "
             "gateway answers not_granted, and the iterator ends with it.",
             gateway(bus=[{"after": "connected", "send": [ev(1, TICK_B, {"seq": 1, "n": 1})]},
                          {"after": "delivered:tick-1", "revoke": True}],
                     steps=[step(sel(T + ":subscribe", nth=2), problem_respond("not_granted"), times=5)]),
             [cmd(1, "connect"), cmd(2, "subscribe.open", id=T), cmd(3, "subscribe.next", subscription=2),
              cmd(4, "subscribe.next", subscription=2, timeout_ms=10000)],
             [ok(1), ok(2), ok(3, contains={"data": {"seq": 1}}), fail(4, "not_granted")],
             [count(sel(T + ":subscribe"), 2)])

    # HAND-001..003
    H = "conformance.handoff@v1:fetch"
    every = handoff_gen.EVERY_ROWS
    feats = {"Atlas-Client-Features": {"levels_contains": ["handoff"], "handoff_contains": ["snowflake-arrow/1"]}}
    expected_every = "{vectors:handoff/snowflake-arrow-1/synthetic/cases.json#every-type}"
    scenario("HAND-001", "Handoff", "A handoff document: the chunks are fetched in parallel with their headers, "
             "decoded, checked, and give the same rows as the gateway's own execution.",
             gateway(state={"handoff_case": "every-type"}),
             [cmd(1, "connect"), call(2, H, {}, execution="handoff", accept=ar),
              call(3, H, {}, execution="gateway", accept=ar)],
             [ok(1), ok(2, expected_every), ok(3, expected_every)],
             [header(sel(H, nth=1), "Atlas-Execution", equals="handoff"),
              {"assert": "header", "select": sel(storage_chunk=True), "name": "x-ms-encryption-key-sha256",
               "present": True}, {"assert": "header_features", "select": sel(H, nth=1), **feats}],
             note="The value is the every-type case's expected rows, all batches concatenated.")
    scenario("HAND-002", "Handoff", "Links that expire mid-download: the handoff's operation is asked again for "
             "fresh links.",
             gateway(state={"handoff_case": "widths-change"},
                     steps=[step(sel(storage_chunk=1, method="GET", nth=1), {"status": 403, "body": None,
                                                                             "expire_links": True})]),
             [cmd(1, "connect"), call(2, H, {}, execution="handoff", accept=ar)],
             [ok(1), ok(2, "{vectors:handoff/snowflake-arrow-1/synthetic/cases.json#widths-change}")],
             [count(sel("atlas.operation:get"), 1)])
    scenario("HAND-003", "Handoff", "A chunk whose schema differs from the writer profile's: invalid_document.",
             gateway(state={"handoff_case": "mismatch-second-chunk"}),
             [cmd(1, "connect"), call(2, H, {}, execution="handoff", accept=ar)],
             [ok(1), fail(2, "invalid_document")], [])
    return S


def fixtures():
    profiles = {}
    for exact, p in list(SERVED.items()) + list(BUILTIN.items()) + [
            (fingerprint.exact_id(MOVED), MOVED), (fingerprint.exact_id(NEWER), NEWER)]:
        profiles[exact] = p
    return {
        "description": "What every scenario's conformance gateway serves unless its script says otherwise: every "
                       "profile by exact-id (the conformance capabilities, the variants scenarios name, and the "
                       "atlas.* built-ins), which profile is current for each pinned id, and the bootstrap, with "
                       "{gateway} standing for the gateway's own URL.",
        "current": CURRENT,
        "bootstrap": default_bootstrap(),
        "tokens": {"access": ["ct-valid", "ct-refreshed"],
                   "refresh": {"rt-valid": {"access_token": "ct-refreshed", "refresh_token": "rt-next",
                                            "expires_in": 3600}}},
        "profiles": profiles,
    }


def build():
    out = {}
    all_ = scenarios()
    for s in all_:
        out[DIR + s["id"] + ".json"] = dumps(s)
    out[DIR + "fixtures/gateway.json"] = dumps(fixtures())
    return out
