"""references/vectors/structured-fields.json: a differential corpus for
RFC 9651. Every field value is read as an Item, a List and a Dictionary by
http_sf (an independent implementation) and by this package's own parser
(sfv.py); `check` fails if the two disagree. The corpus (the hand-written
fields, then mutations from a fixed seed) is the one the parent session first
generated, so its inputs are unchanged.
"""

import random

import http_sf

from .. import sfv
from ..jsonio import dumps

PATH = "vectors/structured-fields.json"

HANDWRITTEN = [
    # The headers of §9.6.3, as its example writes them.
    'name="fiatlas", version="2.3.0", protocol="atlas/1.0"',
    "levels=(core streaming columnar bulk events handoff), handoff=(snowflake-arrow/1 reach-snowflake), "
    "lens=(rename alias_value convert_unit promote default drop), deliveries=(single chunked subscription operation transfer), "
    "encodings=(json ndjson arrow csv parquet)",
    '"portfolio.holdings@v1#bcaa5bbad97cc612"',
    "@1793318400",
    '"8e03978e-40d5-43e8-bc93-6894a57f9324"',
    "gateway", "handoff", "client",
    "?1", "?0",
    "1250.5", "1250", "0.001", "-12.250",
    "rate=58, concurrency=3",
    "limit_exceeded",
    # RFC 9651's own examples.
    "sugar, tea, rum",
    '("foo" "bar"), ("baz"), ("bat" "one"), ()',
    '("foo"; a=1;b=2);lvl=5, ("bar" "baz");lvl=1',
    'en="Applepie", da=:w4ZibGV0w6ZydGU=:',
    "a=?0, b, c; foo=bar",
    "rating=1.5, feelings=(joy sadness)",
    '%"This is intended for display to %c3%bcsers."',
    "abc;a=1;b=2; cde_456",
    # Edges of every rule.
    "", " ", "  1  ", "\t1", "1\t", "a,\tb", "a ,b", "a, b,", ",a",
    "999999999999999", "-999999999999999", "1000000000000000", "-0", "00",
    "123456789012.123", "1234567890123.1", "1.2345", "1.", ".5", "-", "--1",
    '"a\\"b"', '"a\\\\b"', '"a\\b"', '"unterminated', '"tab\there"',
    "*token", "a:b/c", "1abc", "Token", "t*k~n",
    ":aGk=:", ":aGk:", ":aGk==:", ":a*b:", ":aGk=", "::",
    "?2", "?", "?01",
    "@0", "@-1", "@1.5", "@", "@1793318400;tz=utc",
    '%"%c3%bc"', '%"%C3%BC"', '%"%ff"', '%"%c"', '%"plain"', '%"unterminated', '%plain',
    "a=1, a=2", "a;x=1;x=2", "A=1", "a*b-c.d_e=1", "*a=1", "1a=1",
    "(a b", "(a\"b\")", "(a  b)", "( a b )", "(a)(b)", "((a))",
    "a=(1 2);x, b=?1;y=%\"z\"",
]

ALPHABET = list(' \t,;=()"\\:?@%*-._/ab1Z09\x00\x7fé')


def mutate(rng, s):
    s = list(s)
    for _ in range(rng.randint(1, 3)):
        op = rng.random()
        i = rng.randint(0, len(s))
        if op < 0.4 or not s:
            s.insert(i, rng.choice(ALPHABET))
        elif op < 0.7:
            del s[min(i, len(s) - 1)]
        else:
            s[min(i, len(s) - 1)] = rng.choice(ALPHABET)
    return "".join(s)


def read_http_sf(text, kind):
    try:
        value = http_sf.parse(text.encode("utf-8"), tltype=kind)
    except Exception:
        return {"ok": False}
    if kind != "item" and len(value) == 0:
        return {"ok": True, "serialised": ""}  # empty: the field is not sent (RFC 9651 section 4.1)
    return {"ok": True, "serialised": http_sf.ser(value)}


def read_own(text, kind):
    try:
        value = sfv.parse(text, kind)
    except sfv.SFError:
        return {"ok": False}
    return {"ok": True, "serialised": sfv.serialize(value, kind)}


def inputs():
    rng = random.Random(9651)
    fields = list(dict.fromkeys(HANDWRITTEN))
    seen = set(fields)
    seeds = [s for s in HANDWRITTEN if s.strip()]
    while len(fields) < len(HANDWRITTEN) + 4000:
        s = mutate(rng, rng.choice(seeds))
        if s not in seen:
            seen.add(s)
            fields.append(s)
    return fields


def build():
    """Expected values are this package's (RFC 9651 as written); a case where
    http_sf reads otherwise is marked with the question that settles it."""
    cases = []
    for n, text in enumerate(inputs(), 1):
        expected = {kind: read_own(text, kind) for kind in ("item", "list", "dictionary")}
        case = {"name": f"sf-{n:04d}", "field": text, "expected": expected}
        if any(read_http_sf(text, kind) != expected[kind] for kind in expected):
            case["question"] = "Q-SF-1"
        cases.append(case)
    doc = {
        "description": "RFC 9651 (every Atlas header is a Structured Field, §9.6.3): field values, each read as an "
                       "Item, a List and a Dictionary, with whether it parses and, when it does, its serialisation "
                       "(the empty string for an empty List or Dictionary: the field is not sent). Read by tools/spec's "
                       "own parser and checked case by case against http_sf " + http_sf.__version__ + "; where "
                       "they differ (base64 padding, Q-SF-1) the case follows RFC 4648 and is marked. An "
                       "implementation passes when the same values parse and each serialises to the same text.",
        "cases": cases,
    }
    return {PATH: dumps(doc, indent=1)}


def disagreements():
    """Cases where this package's parser and http_sf differ."""
    out = []
    for text in inputs():
        for kind in ("item", "list", "dictionary"):
            a, b = read_http_sf(text, kind), read_own(text, kind)
            if a != b:
                out.append((text, kind, a, b))
    return out
