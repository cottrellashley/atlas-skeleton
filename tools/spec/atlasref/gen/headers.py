"""references/vectors/headers.json (AP-HTTP-001): every Atlas header of §9.6.3,
as field lines, with its Structured Field parse (RFC 9651, in the httpwg
structured-field-tests form) and what Atlas reads from it, or why it is
treated as absent. Every expectation is written by hand; `build` asserts that
headers.py agrees."""

from .. import headers, sfv
from ..jsonio import dumps

PATH = "vectors/headers.json"
A = "absent"


def h(header, lines, expected, note=None, question=None):
    if isinstance(lines, str):
        lines = [lines]
    return {"header": header, "lines": lines, "want": expected, "note": note, "question": question}


FEATURES_EXAMPLE = ("levels=(core streaming columnar bulk events handoff), handoff=(snowflake-arrow/1 reach-snowflake), "
                    "lens=(rename alias_value convert_unit promote default drop), deliveries=(single chunked "
                    "subscription operation transfer), encodings=(json ndjson arrow csv parquet)")
EMPTY_FEATURES = {"levels": [], "lens": [], "deliveries": [], "encodings": [], "traits": [], "handoff": []}

CASES = [
    # Atlas-Client: Dictionary of name, version, protocol (Strings)
    h("Atlas-Client", 'name="fiatlas", version="2.3.0", protocol="atlas/1.0"',
      {"name": "fiatlas", "version": "2.3.0", "protocol": "atlas/1.0"}, "§9.6.3's example"),
    h("Atlas-Client", 'protocol="atlas/1.0", name="atlas", version="1.4.0"',
      {"name": "atlas", "version": "1.4.0", "protocol": "atlas/1.0"}, "member order is free"),
    h("Atlas-Client", ['name="fiatlas"', 'version="2.3.0", protocol="atlas/1.0"'],
      {"name": "fiatlas", "version": "2.3.0", "protocol": "atlas/1.0"}, "two field lines, joined with \", \""),
    h("Atlas-Client", 'name="fiatlas";build=7, version="2.3.0", protocol="atlas/1.0", os="linux"',
      {"name": "fiatlas", "version": "2.3.0", "protocol": "atlas/1.0"}, "unknown members and parameters are ignored"),
    h("Atlas-Client", 'name=fiatlas, version="2.3.0", protocol="atlas/1.0"', A, "name is a Token, not a String"),
    h("Atlas-Client", 'name="fiatlas", version="2.3.0"', A, "no protocol"),
    h("Atlas-Client", 'name="fiatlas", version="2.3.0", protocol="atlas/1"', A, "protocol is not a protocol-ver",
      question="Q-HDR-1"),
    h("Atlas-Client", 'name="fiatlas", version="2.3.0", protocol="atlas/1.0",', A, "a trailing comma"),
    h("Atlas-Client", 'Name="fiatlas", version="2.3.0", protocol="atlas/1.0"', A, "an upper-case key"),
    h("Atlas-Client", 'name="fiatlas", version="2.3.0", protocol="atlas/1.0", name="other"',
      {"name": "other", "version": "2.3.0", "protocol": "atlas/1.0"}, "a repeated key: the last value wins (RFC 9651)"),
    # Atlas-Client-Features: Dictionary of Inner Lists of Tokens
    h("Atlas-Client-Features", FEATURES_EXAMPLE,
      {"levels": ["core", "streaming", "columnar", "bulk", "events", "handoff"], "lens": [
          "rename", "alias_value", "convert_unit", "promote", "default", "drop"],
       "deliveries": ["single", "chunked", "subscription", "operation", "transfer"],
       "encodings": ["json", "ndjson", "arrow", "csv", "parquet"], "traits": [],
       "handoff": ["snowflake-arrow/1", "reach-snowflake"]}, "§9.6.3's example"),
    h("Atlas-Client-Features", "levels=(core)", dict(EMPTY_FEATURES, levels=["core"]),
      "a member left out is empty"),
    h("Atlas-Client-Features", "levels=(core), traits=(projectable paged)",
      dict(EMPTY_FEATURES, levels=["core"], traits=["projectable", "paged"]), "traits, lowercased"),
    h("Atlas-Client-Features", "levels=(core warp), future=(x y)", dict(EMPTY_FEATURES, levels=["core", "warp"]),
      "an unknown token is kept and ignored when acting; an unknown member is ignored"),
    h("Atlas-Client-Features", "levels=()", dict(EMPTY_FEATURES), "an empty Inner List"),
    h("Atlas-Client-Features", "levels=core", A, "a Token where an Inner List is required"),
    h("Atlas-Client-Features", 'levels=(core "streaming")', A, "a String in the list"),
    h("Atlas-Client-Features", "levels=(core streaming", A, "an unterminated Inner List"),
    h("Atlas-Client-Features", "levels=(Core)", dict(EMPTY_FEATURES, levels=["Core"]),
      "tokens are case-sensitive: Core is not core, so no level is known"),
    # Atlas-Profile-Expect, Atlas-Profile: String, an exact-id
    h("Atlas-Profile-Expect", '"portfolio.holdings@v1#bcaa5bbad97cc612"', "portfolio.holdings@v1#bcaa5bbad97cc612",
      "§9.6.3's example"),
    h("Atlas-Profile-Expect", "portfolio.holdings@v1#bcaa5bbad97cc612", A, "not a String: unquoted, it does not even "
                                                                          "parse as a Token (# and @)"),
    h("Atlas-Profile-Expect", '"portfolio.holdings@v1"', A, "a pinned-id, not an exact-id", question="Q-HDR-1"),
    h("Atlas-Profile-Expect", '"portfolio.holdings@v1#BCAA5BBAD97CC612"', A, "an upper-case fingerprint",
      question="Q-HDR-1"),
    h("Atlas-Profile", '"conformance.echo@v1#0123456789abcdef"', "conformance.echo@v1#0123456789abcdef"),
    h("Atlas-Profile", '"conformance.echo@v1#0123456789abcdef";source=cache', "conformance.echo@v1#0123456789abcdef",
      "a parameter is ignored"),
    # Atlas-Deadline, Deprecation: Date
    h("Atlas-Deadline", "@1793318400", 1793318400, "§9.6.3's example: whole seconds (Q-HDR-2)", question="Q-HDR-2"),
    h("Atlas-Deadline", "1793318400", A, "an Integer, not a Date"),
    h("Atlas-Deadline", "@1793318400.5", A, "a Date has no fraction"),
    h("Atlas-Deadline", "@-1", -1, "a Date before the epoch parses (and has passed)"),
    h("Deprecation", "@1795046400", 1795046400, "RFC 9745: a Date, as §3.3.4's example"),
    h("Deprecation", "Sun, 11 Nov 2026 23:59:59 GMT", A, "RFC 9745's Deprecation is a Date, not an HTTP-date"),
    # Idempotency-Key: String, 1-255 of A-Z a-z 0-9 - _ .
    h("Idempotency-Key", '"8e03978e-40d5-43e8-bc93-6894a57f9324"', "8e03978e-40d5-43e8-bc93-6894a57f9324"),
    h("Idempotency-Key", '"run_2026.10.31"', "run_2026.10.31"),
    h("Idempotency-Key", '"' + "k" * 255 + '"', "k" * 255, "255 characters"),
    h("Idempotency-Key", '"' + "k" * 256 + '"', A, "256 characters"),
    h("Idempotency-Key", '""', A, "empty"),
    h("Idempotency-Key", '"a b"', A, "a space"),
    h("Idempotency-Key", "8e03978e", A, "a Token, not a String"),
    # Atlas-Execution: Token
    h("Atlas-Execution", "handoff", "handoff"),
    h("Atlas-Execution", "gateway", "gateway"),
    h("Atlas-Execution", '"handoff"', A, "a String, not a Token"),
    h("Atlas-Execution", "broker", A, "not gateway, handoff or client: an unknown preference is ignored",
      question="Q-HDR-1"),
    # X-Atlas-Interactive: Boolean
    h("X-Atlas-Interactive", "?1", True),
    h("X-Atlas-Interactive", "?0", False),
    h("X-Atlas-Interactive", "1", A, "an Integer, not a Boolean"),
    h("X-Atlas-Interactive", "?true", A, "not RFC 9651's Boolean"),
    # Atlas-Confirm: Decimal
    h("Atlas-Confirm", "1250.5", "1250.5"),
    h("Atlas-Confirm", "1250.0", "1250.0", "a whole amount is still written as a Decimal"),
    h("Atlas-Confirm", "0.001", "0.001"),
    h("Atlas-Confirm", "999999999999.999", "999999999999.999", "12 integer and 3 fractional digits, the largest"),
    h("Atlas-Confirm", "1250", A, "an Integer, not a Decimal", question="Q-HDR-4"),
    h("Atlas-Confirm", "1.2345", A, "four fractional digits"),
    h("Atlas-Confirm", "1234567890123.1", A, "13 integer digits"),
    h("Atlas-Confirm", '"1250.5"', A, "a String"),
    # Atlas-Governance-Revision: String
    h("Atlas-Governance-Revision", '"g-41"', "g-41"),
    h("Atlas-Governance-Revision", "g-41", A, "a Token, not a String"),
    # Atlas-Limit-Remaining: Dictionary of Integers
    h("Atlas-Limit-Remaining", "rate=58, concurrency=3", {"rate": 58, "concurrency": 3}, "§9.6.3's example"),
    h("Atlas-Limit-Remaining", "rate=58;window=60", {"rate": 58}, "a parameter is ignored"),
    h("Atlas-Limit-Remaining", "rate=58, budget=1.5", A, "a Decimal where an Integer is required"),
    h("Atlas-Limit-Remaining", "rate=(58)", A, "an Inner List"),
    h("Atlas-Limit-Remaining", "rate", A, "a bare key is the Boolean true, not an Integer"),
    # Atlas-Error-Code: Token
    h("Atlas-Error-Code", "limit_exceeded", "limit_exceeded"),
    h("Atlas-Error-Code", "warehouse_suspended", "warehouse_suspended", "a capability's own code"),
    h("Atlas-Error-Code", '"limit_exceeded"', A, "a String, not a Token"),
    h("Atlas-Error-Code", "LimitExceeded", A, "a Token that is not a problem-code", question="Q-HDR-1"),
    # Retry-After: delay-seconds only
    h("Retry-After", "30", 30),
    h("Retry-After", "0", 0),
    h("Retry-After", "Sun, 06 Nov 1994 08:49:37 GMT", A, "§9.6.3: delay-seconds only"),
    h("Retry-After", "1.5", A, "not delay-seconds"),
    h("Retry-After", "-1", A, "not delay-seconds"),
    # Sunset: HTTP-date (RFC 8594)
    h("Sunset", "Sun, 06 Nov 1994 08:49:37 GMT", 784111777, "IMF-fixdate"),
    h("Sunset", "Sat, 01 May 2027 00:00:00 GMT", 1809129600),
    h("Sunset", "Sunday, 06-Nov-94 08:49:37 GMT", 784111777, "the obsolete rfc850 form, which RFC 9110 says a "
                                                             "recipient MUST accept", question="Q-HDR-3"),
    h("Sunset", "Sun Nov  6 08:49:37 1994", 784111777, "the obsolete asctime form"),
    h("Sunset", "Mon, 06 Nov 1994 08:49:37 GMT", A, "the wrong day of the week"),
    h("Sunset", "@1809129600", A, "a Structured Field Date is not an HTTP-date"),
    h("Sunset", "Sun, 06 Nov 1994 08:49:37 UTC", A, "an HTTP-date says GMT"),
    # Authorization: Bearer (RFC 6750)
    h("Authorization", "Bearer eyJhbGciOi.eyJzdWIi.c2lnbg", "eyJhbGciOi.eyJzdWIi.c2lnbg"),
    h("Authorization", "bearer abc", "abc", "the scheme is case-insensitive (RFC 9110)"),
    h("Authorization", "Bearer", A, "no token"),
    h("Authorization", "Basic dXNlcjpwYXNz", A, "not Bearer"),
    h("Authorization", "Bearer a b", A, "a space inside the token"),
    # If-Match: entity-tag
    h("If-Match", '"e8"', ['"e8"']),
    h("If-Match", '"3f9a1c0b7d2e4a65"', ['"3f9a1c0b7d2e4a65"'], "a profile's ETag"),
    h("If-Match", "*", ["*"]),
    h("If-Match", "e8", A, "no quotes"),
    # traceparent: W3C Trace Context
    h("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
      {"version": "00", "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736", "parent_id": "00f067aa0ba902b7",
       "flags": "01"}, "W3C's example"),
    h("traceparent", "00-00000000000000000000000000000000-00f067aa0ba902b7-01", A, "an all-zero trace-id"),
    h("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01", A, "an all-zero parent-id"),
    h("traceparent", "ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", A, "version ff"),
    h("traceparent", "00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01", A, "upper-case hexadecimal"),
    # Operation-Location: an absolute URL
    h("Operation-Location", "https://atlas.example/v1/operations/op-91", "https://atlas.example/v1/operations/op-91"),
    h("Operation-Location", "/v1/operations/op-91", A, "relative"),
    # Cache-Control
    h("Cache-Control", "no-store", ["no-store"], "every handoff, connect, ticket and transfer answer"),
    h("Cache-Control", "max-age=31536000, immutable", ["max-age=31536000", "immutable"],
      "atlas.describe by exact (§9.6.7)"),
    # WWW-Authenticate (§9.6.6)
    h("WWW-Authenticate", 'Bearer realm="atlas"', {"scheme": "Bearer", "params": {"realm": "atlas"}}),
    h("WWW-Authenticate", 'Bearer realm="atlas", error="invalid_token"',
      {"scheme": "Bearer", "params": {"realm": "atlas", "error": "invalid_token"}}, "an expired token"),
    h("WWW-Authenticate", 'Bearer realm="atlas", claims="eyJhY2Nlc3NfdG9rZW4iOnt9fQ"',
      {"scheme": "Bearer", "params": {"realm": "atlas", "claims": "eyJhY2Nlc3NfdG9rZW4iOnt9fQ"}},
      "a conditional-access claims challenge"),
]


def build():
    cases = []
    for c in CASES:
        status, value = headers.parse(c["header"], c["lines"])
        if c["want"] == A:
            if status != "absent":
                raise AssertionError(f"headers.py reads {c['header']} {c['lines']} as {value!r}")
            expected = {"absent": True, "why": value}
        else:
            if (status, value) != ("value", c["want"]):
                raise AssertionError(f"headers.py reads {c['header']} {c['lines']} as {status} {value!r}")
            expected = {"value": value}
        if c["header"] in headers.SF_TYPE:
            kind = headers.SF_TYPE[c["header"]]
            try:
                expected["sf"] = {"type": kind, "parsed": sfv.to_json(sfv.parse(sfv.join_lines(c["lines"]), kind), kind)}
            except sfv.SFError:
                expected["sf"] = {"type": kind, "parsed": None}
        label = c["lines"][0] if len(c["lines"]) == 1 else " / ".join(c["lines"])
        case = {"name": f"{c['header']}: {label if len(label) <= 60 else label[:57] + '...'}",
                "header": c["header"], "lines": c["lines"], "expected": expected}
        if c["note"]:
            case["note"] = c["note"]
        if c["question"]:
            case["question"] = c["question"]
        cases.append(case)
    names = [c["name"] for c in cases]
    dupes = {n for n in names if names.count(n) > 1}
    for c in cases:
        if c["name"] in dupes:
            c["name"] += f" ({len(c['lines'][0])} characters)"
    assert len({c["name"] for c in cases}) == len(cases)
    doc = {
        "description": "Every Atlas header of §9.6.3 (AP-HTTP-001): its field lines (several lines are joined with "
                       "\", \", RFC 9110 section 5.3); for a Structured Field, `sf` is its RFC 9651 parse in the "
                       "form of github.com/httpwg/structured-field-tests (null when it does not parse as its type); "
                       "`value` is what Atlas reads from it; `absent` means a receiver treats the header as absent "
                       "(§9.6.3: one that does not parse as its type), with `why`. A value that parses as its "
                       "Structured Field type but breaks the grammar §9.6.3 gives it is absent too (Q-HDR-1). "
                       "Atlas-Client-Features lists every member, empty when the header leaves it out.",
        "headers": sorted({c["header"] for c in cases}),
        "cases": cases,
    }
    return {PATH: dumps(doc)}
