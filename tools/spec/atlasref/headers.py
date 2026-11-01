"""§9.6.3: every Atlas header, parsed as its Structured Field type (RFC 9651)
and read for what Atlas means by it.

`parse(header, lines)` returns ("value", form) or ("absent", why): a receiver
ignores an Atlas header it does not know, and treats one that does not parse
as its type as absent. A value that parses as its SF type but breaks the
grammar the table gives it (an Atlas-Profile-Expect that is not an exact-id)
is treated as not parsing as its type (Q-HDR-1).
"""

import re

from . import formats, grammar, sfv

LEVELS = ("core", "streaming", "columnar", "bulk", "events", "handoff", "broker")
FEATURE_KEYS = ("levels", "lens", "deliveries", "encodings", "traits", "handoff")
EXECUTIONS = ("gateway", "handoff", "client")

_B64TOKEN = re.compile(r"[A-Za-z0-9\-._~+/]+=*")
_HTTP_DATE_IMF = re.compile(
    r"(Mon|Tue|Wed|Thu|Fri|Sat|Sun), ([0-9]{2}) (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) ([0-9]{4}) "
    r"([0-9]{2}):([0-9]{2}):([0-9]{2}) GMT")
_HTTP_DATE_850 = re.compile(
    r"(Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday), ([0-9]{2})-"
    r"(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)-([0-9]{2}) ([0-9]{2}):([0-9]{2}):([0-9]{2}) GMT")
_HTTP_DATE_ASC = re.compile(
    r"(Mon|Tue|Wed|Thu|Fri|Sat|Sun) (Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) ([ 0-9][0-9]) "
    r"([0-9]{2}):([0-9]{2}):([0-9]{2}) ([0-9]{4})")
_MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]
_DAYS_SHORT = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]
_TRACEPARENT = re.compile(r"([0-9a-f]{2})-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})(-.*)?")
_ETAG = re.compile(r'(W/)?"[\x21\x23-\x7e\x80-\xff]*"')

# header → its Structured Field type, as §9.6.3's table gives it
SF_TYPE = {
    "Atlas-Client": "dictionary",
    "Atlas-Client-Features": "dictionary",
    "Atlas-Profile-Expect": "item",
    "Atlas-Deadline": "item",
    "Idempotency-Key": "item",
    "Atlas-Execution": "item",
    "X-Atlas-Interactive": "item",
    "Atlas-Confirm": "item",
    "Atlas-Profile": "item",
    "Atlas-Governance-Revision": "item",
    "Atlas-Limit-Remaining": "dictionary",
    "Atlas-Error-Code": "item",
    "Deprecation": "item",
}


class Absent(Exception):
    pass


def _bare(item, cls, what):
    bare, _params = item
    if isinstance(bare, sfv.InnerList) or not _is(bare, cls):
        raise Absent(f"not {what}")
    return bare


def _is(bare, cls):
    if cls is bool:
        return isinstance(bare, bool)
    if cls is int:
        return isinstance(bare, int) and not isinstance(bare, (bool, sfv.Date))
    if cls is str:
        return isinstance(bare, str) and not isinstance(bare, (sfv.Token, sfv.DisplayString))
    return isinstance(bare, cls)


def _client(d):
    out = {}
    for key, what in (("name", "a String"), ("version", "a String"), ("protocol", "a String")):
        if key not in d:
            raise Absent(f"no {key}")
        out[key] = str(_bare(d[key], str, f"{key}: {what}"))
    if not grammar.matches("protocol-ver", out["protocol"]):
        raise Absent("protocol is not a protocol-ver")
    return out


def _features(d):
    out = {k: [] for k in FEATURE_KEYS}
    for key, (value, _params) in d.items():
        if not isinstance(value, sfv.InnerList):
            raise Absent(f"{key} is not an Inner List")
        tokens = []
        for bare, _p in value:
            if not isinstance(bare, sfv.Token):
                raise Absent(f"{key} holds something other than a Token")
            tokens.append(str(bare))
        if key in out:
            out[key] = tokens
    return out


def _limits(d):
    out = {}
    for key, item in d.items():
        out[key] = int(_bare(item, int, f"{key}: an Integer"))
    return out


def _decimal_text(d):
    s = format(d, "f")
    return s


def parse_sf_header(name, value):
    kind = SF_TYPE[name]
    if kind == "dictionary":
        if name == "Atlas-Client":
            return _client(value)
        if name == "Atlas-Client-Features":
            return _features(value)
        return _limits(value)
    if name in ("Atlas-Profile-Expect", "Atlas-Profile"):
        s = str(_bare(value, str, "a String"))
        if not grammar.matches("exact-id", s):
            raise Absent("not an exact-id")
        return s
    if name == "Atlas-Governance-Revision":
        return str(_bare(value, str, "a String"))
    if name == "Idempotency-Key":
        s = str(_bare(value, str, "a String"))
        if not grammar.matches("idempotency-key", s):
            raise Absent("not 1-255 characters of A-Z a-z 0-9 - _ .")
        return s
    if name in ("Atlas-Deadline", "Deprecation"):
        return int(_bare(value, sfv.Date, "a Date"))
    if name == "Atlas-Execution":
        t = str(_bare(value, sfv.Token, "a Token"))
        if t not in EXECUTIONS:
            raise Absent("not gateway, handoff or client")
        return t
    if name == "X-Atlas-Interactive":
        return bool(_bare(value, bool, "a Boolean"))
    if name == "Atlas-Confirm":
        import decimal

        return _decimal_text(_bare(value, decimal.Decimal, "a Decimal"))
    if name == "Atlas-Error-Code":
        t = str(_bare(value, sfv.Token, "a Token"))
        if not grammar.matches("problem-code", t):
            raise Absent("not a problem-code")
        return t
    raise KeyError(name)


def http_date(text):
    """RFC 9110 section 5.6.7: IMF-fixdate, or the obsolete rfc850 and asctime
    forms a recipient MUST accept; seconds since the epoch."""
    m = _HTTP_DATE_IMF.fullmatch(text)
    if m:
        day, mon, year = int(m.group(2)), _MONTHS.index(m.group(3)) + 1, int(m.group(4))
        hh, mi, ss, weekday = int(m.group(5)), int(m.group(6)), int(m.group(7)), m.group(1)
    else:
        m = _HTTP_DATE_850.fullmatch(text)
        if m:
            yy = int(m.group(4))
            year = 2000 + yy if 2000 + yy <= 2026 + 50 else 1900 + yy  # RFC 9110: within 50 years (Q-HDR-3)
            day, mon = int(m.group(2)), _MONTHS.index(m.group(3)) + 1
            hh, mi, ss, weekday = int(m.group(5)), int(m.group(6)), int(m.group(7)), m.group(1)[:3]
        else:
            m = _HTTP_DATE_ASC.fullmatch(text)
            if not m:
                raise Absent("not an HTTP-date")
            mon, day, year = _MONTHS.index(m.group(2)) + 1, int(m.group(3).strip()), int(m.group(7))
            hh, mi, ss, weekday = int(m.group(4)), int(m.group(5)), int(m.group(6)), m.group(1)
    if not grammar.real_date(year, mon, day) or hh > 23 or mi > 59 or ss > 60:
        raise Absent("not a real date and time")
    days = grammar.days_from_civil(year, mon, day)
    if _DAYS_SHORT[(days + 3) % 7] != weekday:
        raise Absent("the day of the week is wrong")
    return days * 86400 + hh * 3600 + mi * 60 + min(ss, 59)


def parse(name, lines):
    """("value", form) or ("absent", why) for the field lines of `name`."""
    joined = sfv.join_lines(lines) if name in SF_TYPE else ", ".join(lines)
    try:
        if name in SF_TYPE:
            try:
                value = sfv.parse(joined, SF_TYPE[name])
            except sfv.SFError as e:
                raise Absent(f"not a Structured Field {SF_TYPE[name]}: {e}") from None
            return "value", parse_sf_header(name, value)
        if name == "Retry-After":
            if not re.fullmatch(r"[0-9]+", joined.strip()):
                raise Absent("not delay-seconds (an HTTP-date is not allowed)")
            return "value", int(joined.strip())
        if name == "Sunset":
            return "value", http_date(joined.strip())
        if name == "Authorization":
            scheme, sep, rest = joined.strip().partition(" ")
            if not sep or scheme.lower() != "bearer":
                raise Absent("not a Bearer credential")
            token = rest.lstrip(" ")
            if not _B64TOKEN.fullmatch(token):
                raise Absent("not a b64token (RFC 6750)")
            return "value", token
        if name == "If-Match":
            v = joined.strip()
            if v == "*":
                return "value", ["*"]
            tags = [t.strip() for t in v.split(",")]
            if not all(_ETAG.fullmatch(t) for t in tags):
                raise Absent("not a list of entity-tags")
            return "value", tags
        if name == "traceparent":
            v = joined.strip()
            m = _TRACEPARENT.fullmatch(v)
            if not m or m.group(1) == "ff" or set(m.group(2)) == {"0"} or set(m.group(3)) == {"0"}:
                raise Absent("not a W3C traceparent")
            if m.group(1) == "00" and m.group(5) is not None:
                raise Absent("version 00 has exactly four fields")
            return "value", {"version": m.group(1), "trace_id": m.group(2), "parent_id": m.group(3),
                             "flags": m.group(4)}
        if name == "Operation-Location":
            v = joined.strip()
            if not formats.check("uri", v):
                raise Absent("not an absolute URI")
            return "value", v
        if name == "Cache-Control":
            directives = [d.strip().lower() for d in joined.split(",") if d.strip()]
            return "value", directives
        if name == "WWW-Authenticate":
            return "value", _challenge(joined.strip())
    except Absent as e:
        return "absent", str(e)
    raise KeyError(name)


_PARAM = re.compile(r'\s*([A-Za-z0-9!#$%&\'*+\-.^_`|~]+)\s*=\s*("(?:[^"\\]|\\.)*"|[A-Za-z0-9!#$%&\'*+\-.^_`|~]+)\s*')


def _challenge(text):
    """One RFC 9110 challenge: the scheme and its auth-params (§9.6.6)."""
    scheme, _, rest = text.partition(" ")
    if not scheme or scheme.lower() != "bearer":
        raise Absent("not a Bearer challenge")
    params, pos = {}, 0
    rest = rest.strip()
    while pos < len(rest):
        m = _PARAM.match(rest, pos)
        if not m:
            raise Absent("bad auth-param")
        key, value = m.group(1).lower(), m.group(2)
        if value.startswith('"'):
            value = re.sub(r"\\(.)", r"\1", value[1:-1])
        if key in params:
            raise Absent("a repeated auth-param")
        params[key] = value
        pos = m.end()
        if pos < len(rest):
            if rest[pos] != ",":
                raise Absent("expected , between auth-params")
            pos += 1
    return {"scheme": "Bearer", "params": params}
