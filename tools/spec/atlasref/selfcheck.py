"""What `python -m atlasref check` proves beyond byte-identical regeneration,
re-reading every file from its bytes (README.md, "What check proves")."""

import base64
import decimal
import hashlib
import io
import re

import http_sf
import rfc8785
from jsonschema import Draft202012Validator

from . import (abnf, arrowmap, docvalidate, fingerprint, grammar, guard, headers, jcs, problems, profilecheck,
               resolution, schemas, sfv, snowarrow, validator)
from .jsonio import NotIJSON, loads

SCENARIO_IDS = [
    "CORE-BOOT-001", "CORE-BOOT-002", "CORE-SIGNIN-001", "CORE-SIGNIN-002", "CORE-SIGNIN-003", "CORE-SIGNIN-004",
    "CORE-SIGNIN-005", "CORE-DISC-001", "CORE-DISC-002", "CORE-GUIDE-001", "CORE-EXPLAIN-001", "CORE-CALL-001",
    "CORE-CALL-002", "CORE-CALL-003", "CORE-CALL-004", "CORE-CALL-005", "CORE-PROB-001", "CORE-PROB-002",
    "CORE-RETRY-001", "CORE-DEADLINE-001", "CORE-TOL-001", "CORE-CRIT-001", "CORE-RES-001", "CORE-RES-002",
    "CORE-RES-003", "CORE-LOCK-001", "CORE-DEPR-001", "CORE-OP-001", "CORE-CONFIRM-001", "CORE-CONTENT-001",
    "CORE-CONTENT-002", "CORE-ENV-001", "CORE-UPGRADE-001", "STREAM-001", "STREAM-002", "STREAM-003",
    "STREAM-004", "COL-001", "COL-002", "COL-003", "BULK-001", "BULK-002", "BULK-003", "EVT-001", "EVT-002",
    "EVT-003", "EVT-004", "HAND-001", "HAND-002", "HAND-003",
]
LEVELS = {"CORE": "Core", "STREAM": "Streaming", "COL": "Columnar", "BULK": "Bulk", "EVT": "Events", "HAND": "Handoff"}
RUNNER_OPS = {"connect", "whoami", "catalog", "search", "describe", "guide", "explain", "access", "call", "fetch",
              "operation.get", "operation.wait", "operation.cancel", "subscribe.open", "subscribe.next",
              "subscribe.close", "publish", "export", "upload", "version"}
SUITE_OPS = {"suite.restart", "suite.file", "suite.expect_file"}
ASSERTS = {"count", "header", "body", "same_header", "interval", "order", "deadline", "describe_exact",
           "poll_intervals", "cursor_chain", "body_member_in", "max_body", "bus", "bus_connections",
           "header_features"}


def _binary64(v):
    """I-JSON's numbers are binary64: what the differential validator is given."""
    if isinstance(v, bool) or v is None:
        return v
    if isinstance(v, int):
        return float(v) if abs(v) > 2**53 - 1 else v
    if isinstance(v, list):
        return [_binary64(x) for x in v]
    if isinstance(v, dict):
        return {k: _binary64(x) for k, x in v.items()}
    return v


def run(files):
    out = []
    bad = out.append
    docs = {}
    for path, data in files.items():
        if path.endswith(".json"):
            try:
                docs[path] = loads(data)
            except NotIJSON as e:
                bad(f"{path}: not I-JSON: {e}")
    if out:
        return out
    for path, doc in docs.items():
        if path.startswith("vectors/") and path.endswith(".json") and not path.endswith("/cases.json"):
            if not isinstance(doc.get("description"), str) or not isinstance(doc.get("cases"), list):
                bad(f"{path}: a vector file has a description and cases")
            for c in doc.get("cases", []):
                if "name" not in c or not (("expected" in c) ^ ("problem" in c)):
                    bad(f"{path}: case {c.get('name')!r} needs a name and expected or problem")
    _schemas(docs, bad)
    _grammar(files, docs, bad)
    _fingerprints(docs, bad)
    _profiles(docs, bad)
    _schema_vectors(docs, bad)
    _headers(docs, bad)
    _sf(docs, bad)
    _arrow(docs, bad)
    _resolution(docs, bad)
    _handoff(files, docs, bad)
    _guard(docs, bad)
    _problems(docs, bad)
    _scenarios(docs, bad)
    return out


def _schemas(docs, bad):
    names = set(schemas.DOCUMENTS)
    found = {p[len("schemas/"):] for p in docs if p.startswith("schemas/")}
    if found != names or len(names) != 21:
        bad(f"schemas/: expected the 21 documents of §9.3.1, found {sorted(found)}")
    for name in names:
        doc = docs.get("schemas/" + name)
        if doc is None:
            continue
        try:
            Draft202012Validator.check_schema(doc)
        except Exception as e:
            bad(f"schemas/{name}: not a valid JSON Schema 2020-12: {e}")
        if doc.get("$id") != schemas.BASE + name or doc.get("$schema") != profilecheck.DRAFT:
            bad(f"schemas/{name}: $id or $schema")
    reg = docs.get("schemas/problem.schema.json", {}).get("$defs", {}).get("registry_code", {}).get("enum")
    if reg != [c["code"] for c in problems.REGISTRY]:
        bad("problem.schema.json: the registry enum is not §9.3.3's")
    table = problems.read_spec_table()
    if table is not None and table != [(c["code"], c["status"], c["retryable"]) for c in problems.REGISTRY]:
        bad("problems.py: the registry differs from 09-3-documents.md's table")
    env = docs.get("schemas/resource.schema.json", {}).get("$defs", {})
    wrapped = profiles_root({"type": "object", "properties": env, "required": ["name", "metadata"]})
    for p, rule in profilecheck.check_schema(wrapped, "other"):
        bad(f"resource.schema.json: the envelope leaves the Atlas Schema profile at {p} ({rule})")


def profiles_root(body):
    s = {"$schema": profilecheck.DRAFT, "$vocabulary": dict(profilecheck.VOCABULARY)}
    s.update(body)
    return s


def _grammar(files, docs, bad):
    text = files["grammar.abnf"].decode("utf-8")
    try:
        g = abnf.parse(text)
    except abnf.ABNFError as e:
        bad(f"grammar.abnf does not parse: {e}")
        return
    for rule in grammar.SPEC_RULES + grammar.EXTRA_RULES:
        if rule not in g.rules:
            bad(f"grammar.abnf has no rule {rule}")
    for c in docs["vectors/grammar.json"]["cases"]:
        want, why = c["expected"]["valid"], c["expected"].get("why")
        got = grammar.check(c["rule"], c["input"])
        if (got is None) != want or (not want and got != why):
            bad(f"grammar.json {c['name']}: grammar.py says {got}")
        a = g.matches(c["rule"], c["input"])
        if (want or why == "abnf") and a != want:
            bad(f"grammar.json {c['name']}: grammar.abnf says {a}")
        if not want and why != "abnf" and not a:
            bad(f"grammar.json {c['name']}: refused for prose ({why}), yet grammar.abnf refuses it too")


def _fingerprints(docs, bad):
    for c in docs["vectors/fingerprints.json"]["cases"]:
        p = c["profile"]
        mine = fingerprint.canonical(p)
        theirs = rfc8785.dumps(fingerprint.projection(p))
        e = c["expected"]
        if mine != theirs:
            bad(f"fingerprints.json {c['name']}: tools/spec's RFC 8785 differs from the rfc8785 package")
        if mine.decode("utf-8") != e["canonical"] or hashlib.sha256(mine).hexdigest() != e["sha256"] or \
                e["sha256"][:16] != e["fingerprint"]:
            bad(f"fingerprints.json {c['name']}: canonical form, sha256 or fingerprint")


def _profiles(docs, bad):
    env = schemas.envelope_defs()
    envelope = {k: env[k] for k in ("name", "metadata", "status")}
    families = set()
    for path in sorted(p for p in docs if p.startswith("vectors/profiles/")):
        for c in docs[path]["cases"]:
            p = c["profile"]
            families.add(p["properties"]["family"])
            for e in docvalidate.errors("profile.schema.json", p):
                bad(f"{path}: not valid against profile.schema.json: {e}")
            roles = profilecheck.schema_roles(p)
            for name, s in p["schemas"].items():
                for e in docvalidate.validator_for("atlas-schema.schema.json").iter_errors(s):
                    bad(f"{path}: schema {name} not valid against atlas-schema.schema.json: {e.message}")
                for ptr, rule in profilecheck.check_schema(s, roles[name]):
                    bad(f"{path}: schema {name} leaves the Atlas Schema profile at {ptr} ({rule})")
            for ptr, rule in profilecheck.lint_profile(p, envelope=envelope):
                bad(f"{path}: lint {ptr} ({rule})")
            for ptr, rule in profilecheck.agent_readiness(p):
                bad(f"{path}: not ready for agents at {ptr} ({rule})")
            if fingerprint.fingerprint(p) != p.get("fingerprint") or c["expected"]["exact"] != fingerprint.exact_id(p):
                bad(f"{path}: fingerprint")
            for ex in (p.get("usage") or {}).get("examples", []):
                takes = p["interactions"][ex["interaction"]].get("takes")
                if validator.validate(p["schemas"][takes], ex["request"]):
                    bad(f"{path}: example {ex['title']!r} is not valid against {takes}")
    if families != {"data", "resource", "action", "event"}:
        bad(f"vectors/profiles/: one profile per family, found {sorted(families)}")


def _schema_vectors(docs, bad):
    for c in docs["vectors/schemas.json"]["cases"]:
        if c["kind"] == "validate":
            inst = loads(c["instance_text"]) if "instance_text" in c else c["instance"]
            got = [{"pointer": p, "keyword": k} for p, k in validator.validate(c["schema"], inst)]
            if got != c["expected"]["errors"] or c["expected"]["valid"] != (not got):
                bad(f"schemas.json {c['name']}: validator.py gives {got}")
            if docvalidate.profile_valid(c["schema"], _binary64(inst)) != c["expected"]["valid"]:
                bad(f"schemas.json {c['name']}: jsonschema's verdict differs")
        else:
            got = profilecheck.check_schema(c["schema"], c["role"], free_form=c.get("free_form", ()))
            want = [] if "expected" in c else [(c["problem"]["target"], c["rule"])]
            if got != want:
                bad(f"schemas.json {c['name']}: profilecheck.py gives {got}")
            meta = not list(docvalidate.validator_for("atlas-schema.schema.json").iter_errors(c["schema"]))
            if c["meta_schema"] != ("accepts" if meta else "refuses"):
                bad(f"schemas.json {c['name']}: the meta-schema's verdict")
            if "expected" in c and not meta:
                bad(f"schemas.json {c['name']}: accepted by the profile, refused by the meta-schema")


def _headers(docs, bad):
    for c in docs["vectors/headers.json"]["cases"]:
        status, value = headers.parse(c["header"], c["lines"])
        e = c["expected"]
        if e.get("absent") and status != "absent" or not e.get("absent") and (status, value) != ("value", e["value"]):
            bad(f"headers.json {c['name']}: headers.py gives {status} {value!r}")


def _sf(docs, bad):
    doc = docs["vectors/structured-fields.json"]
    for c in doc["cases"]:
        for kind in ("item", "list", "dictionary"):
            try:
                v = sfv.parse(c["field"], kind)
                got = {"ok": True, "serialised": sfv.serialize(v, kind)}
            except sfv.SFError:
                got = {"ok": False}
            if got != c["expected"][kind]:
                bad(f"structured-fields.json {c['name']} {kind}: sfv.py gives {got}")
            try:
                hv = http_sf.parse(c["field"].encode("utf-8"), tltype=kind)
                h = {"ok": True, "serialised": "" if kind != "item" and len(hv) == 0 else http_sf.ser(hv)}
            except Exception:
                h = {"ok": False}
            if (h != c["expected"][kind]) != (c.get("question") == "Q-SF-1") and h != c["expected"][kind]:
                bad(f"structured-fields.json {c['name']} {kind}: http_sf differs and the case is not marked")


def _arrow(docs, bad):
    for c in docs["vectors/arrow.json"]["cases"]:
        try:
            d = arrowmap.derive(c["schema"], c["exact"])
        except arrowmap.NotDerivable as e:
            if c.get("problem", {}).get("target") != e.pointer:
                bad(f"arrow.json {c['name']}: refused at {e.pointer}")
            continue
        if "expected" not in c or d != c["expected"]["arrow"]:
            bad(f"arrow.json {c['name']}: arrowmap.py derives otherwise")
            continue
        pas = arrowmap.to_pa_schema(d)
        if [arrowmap.from_pa_field(pas.field(i)) for i in range(len(pas))] != d["fields"]:
            bad(f"arrow.json {c['name']}: the pyarrow round trip")


def _resolution(docs, bad):
    for c in docs["vectors/resolution.json"]["cases"]:
        try:
            got, problem = resolution.resolve(c["record"], c["writer"], c["reader"], c["chain"], c.get("columns"),
                                              c.get("dynamic_schema", False)), None
        except resolution.ResolutionFailed as e:
            got, problem = None, e.problem()
        if ("problem" in c and problem != c["problem"]) or ("expected" in c and (problem or got != c["expected"])):
            bad(f"resolution.json {c['name']}: resolution.py gives {problem or got}")
        if "expected" in c and c["record_kind"] == "row" and not c.get("dynamic_schema"):
            table = resolution.rows_to_columns([c["record"]] * 3)
            cols = resolution.resolve_columns(table, c["writer"], c["reader"], c["chain"], c.get("columns"))
            if resolution.columns_to_rows(cols) != [c["expected"]] * 3:
                bad(f"resolution.json {c['name']}: column-wise resolution differs from row-wise")


def _handoff(files, docs, bad):
    import pyarrow.ipc as ipc

    base = "vectors/handoff/snowflake-arrow-1/synthetic/"
    doc = docs[base + "cases.json"]
    for c in doc["cases"]:
        for e in docvalidate.errors("handoff.schema.json", c["handoff"]):
            bad(f"handoff {c['name']}: the handoff document: {e}")
        chunks = []
        for ch in c["chunks"]:
            data = files.get(base + ch["file"])
            if data is None or hashlib.sha256(data).hexdigest() != ch["sha256"] or len(data) != ch["bytes"]:
                bad(f"handoff {c['name']}: chunk {ch['file']}")
                continue
            chunks.append(data)
        if base64.b64decode(c["handoff"]["first_chunk"]["data"]) != chunks[0]:
            bad(f"handoff {c['name']}: first_chunk is not chunk 0")
        if arrowmap.derive(c["result_schema"], c["writer"]) != c["arrow_schema"]:
            bad(f"handoff {c['name']}: the derived Arrow schema")
        try:
            batches = [snowarrow.decode_chunk(b, c["arrow_schema"], c["source_schema"]) for b in chunks]
        except snowarrow.Mismatch:
            if "problem" not in c:
                bad(f"handoff {c['name']}: does not decode")
            continue
        if "problem" in c:
            bad(f"handoff {c['name']}: decodes, but expects invalid_document")
            continue
        if [{"rows": snowarrow.rows_json(b)} for b in batches] != c["expected"]["batches"]:
            bad(f"handoff {c['name']}: the decoded rows")
        expected = ipc.open_stream(io.BytesIO(files[base + c["expected"]["file"]])).read_all()
        if not expected.equals(__import__("pyarrow").Table.from_batches(batches)) or \
                not expected.schema.equals(batches[0].schema, check_metadata=True):
            bad(f"handoff {c['name']}: expected.arrows")


def _guard(docs, bad):
    doc = docs["vectors/guard.json"]
    for c in doc["cases"]:
        ok, reason, objects = guard.check(c["statement"], doc["registered"])
        e = c["expected"]
        if ok != e["accepted"] or (ok and objects != e["objects"]) or (not ok and reason != e["reason"]):
            bad(f"guard.json {c['name']}: guard.py gives {ok} {reason}")


def _problems(docs, bad):
    cases = docs["vectors/problems.json"]["cases"]
    for c in cases:
        if c["code"] in problems.BY_CODE:
            r = problems.BY_CODE[c["code"]]
            if (c["expected"]["status"], c["expected"]["retryable"]) != (r["status"], r["retryable"]):
                bad(f"problems.json {c['name']}")


def _scenarios(docs, bad):
    found = sorted(p[len("scenarios/"):-len(".json")] for p in docs if p.startswith("scenarios/") and
                   "/fixtures/" not in p)
    if found != sorted(SCENARIO_IDS):
        bad(f"scenarios/: expected §9.8.4's 50 ids, found {len(found)}: "
            f"{sorted(set(SCENARIO_IDS) ^ set(found))}")
    fixtures = docs.get("scenarios/fixtures/gateway.json", {})
    served = fixtures.get("profiles", {})
    for exact, p in served.items():
        if fingerprint.exact_id(p) != exact:
            bad(f"fixtures: {exact} is not that profile's exact-id")
        for e in docvalidate.errors("profile.schema.json", p):
            bad(f"fixtures: {exact}: {e}")
    sample = _substitute(fixtures.get("bootstrap", {}))
    for e in docvalidate.errors("bootstrap.schema.json", sample):
        bad(f"fixtures: the bootstrap: {e}")
    text_ids = re.compile(r"[a-z][a-z0-9.-]*@v[1-9][0-9]*#[0-9a-f]{16}")
    for sid in SCENARIO_IDS:
        s = docs.get(f"scenarios/{sid}.json")
        if s is None:
            continue
        for k in ("id", "level", "description", "gateway", "commands", "expected", "expected_requests"):
            if k not in s:
                bad(f"{sid}: no {k}")
        if s.get("id") != sid or s.get("level") != LEVELS[sid.split("-")[0]]:
            bad(f"{sid}: id or level")
        ids = [c["id"] for c in s["commands"]]
        if ids != [e["id"] for e in s["expected"]] or len(set(ids)) != len(ids):
            bad(f"{sid}: every command has one expected result, in order")
        for c in s["commands"]:
            if c["op"] not in RUNNER_OPS | SUITE_OPS:
                bad(f"{sid}: unknown op {c['op']}")
        for a in s["expected_requests"]:
            if a.get("assert") not in ASSERTS:
                bad(f"{sid}: unknown assertion {a.get('assert')}")
        for exact in text_ids.findall(str(s)):
            if exact not in served and not exact.startswith("vectors."):
                bad(f"{sid}: names {exact}, which the fixtures do not serve")


def _substitute(v):
    if isinstance(v, str):
        return v.replace("{gateway}", "https://conformance.test")
    if isinstance(v, list):
        return [_substitute(x) for x in v]
    if isinstance(v, dict):
        return {k: _substitute(x) for k, x in v.items()}
    return v


__all__ = ["run", "SCENARIO_IDS", "decimal", "jcs"]
