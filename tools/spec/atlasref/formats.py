"""§9.2.2: the formats of the Atlas Schema profile, each an assertion."""

import re

from . import grammar

FORMATS = (
    "date", "date-time", "duration", "decimal", "int64", "uri", "uuid",
    "capability-id", "pinned-id", "exact-id", "address",
)

# RFC 3986 section 3, as one expression: URI = scheme ":" hier-part [ "?" query ] [ "#" fragment ].
_UNRESERVED = r"A-Za-z0-9\-._~"
_SUB_DELIMS = r"!$&'()*+,;="
_PCT = r"%[0-9A-Fa-f]{2}"
_PCHAR = rf"(?:[{_UNRESERVED}{_SUB_DELIMS}:@]|{_PCT})"
_SEGMENT = rf"{_PCHAR}*"
_SEGMENT_NZ = rf"{_PCHAR}+"
_DEC_OCTET = r"(?:25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])"
_IPV4 = rf"{_DEC_OCTET}\.{_DEC_OCTET}\.{_DEC_OCTET}\.{_DEC_OCTET}"
_H16 = r"[0-9A-Fa-f]{1,4}"
_LS32 = rf"(?:{_H16}:{_H16}|{_IPV4})"
_IPV6 = (
    rf"(?:(?:{_H16}:){{6}}{_LS32}"
    rf"|::(?:{_H16}:){{5}}{_LS32}"
    rf"|(?:{_H16})?::(?:{_H16}:){{4}}{_LS32}"
    rf"|(?:(?:{_H16}:){{0,1}}{_H16})?::(?:{_H16}:){{3}}{_LS32}"
    rf"|(?:(?:{_H16}:){{0,2}}{_H16})?::(?:{_H16}:){{2}}{_LS32}"
    rf"|(?:(?:{_H16}:){{0,3}}{_H16})?::{_H16}:{_LS32}"
    rf"|(?:(?:{_H16}:){{0,4}}{_H16})?::{_LS32}"
    rf"|(?:(?:{_H16}:){{0,5}}{_H16})?::{_H16}"
    rf"|(?:(?:{_H16}:){{0,6}}{_H16})?::)"
)
_IPVFUTURE = rf"v[0-9A-Fa-f]+\.[{_UNRESERVED}{_SUB_DELIMS}:]+"
_IP_LITERAL = rf"\[(?:{_IPV6}|{_IPVFUTURE})\]"
_REG_NAME = rf"(?:[{_UNRESERVED}{_SUB_DELIMS}]|{_PCT})*"
_HOST = rf"(?:{_IP_LITERAL}|{_IPV4}|{_REG_NAME})"
_USERINFO = rf"(?:[{_UNRESERVED}{_SUB_DELIMS}:]|{_PCT})*"
_AUTHORITY = rf"(?:{_USERINFO}@)?{_HOST}(?::[0-9]*)?"
_PATH_ABEMPTY = rf"(?:/{_SEGMENT})*"
_PATH_ABSOLUTE = rf"/(?:{_SEGMENT_NZ}(?:/{_SEGMENT})*)?"
_PATH_ROOTLESS = rf"{_SEGMENT_NZ}(?:/{_SEGMENT})*"
_HIER = rf"(?://{_AUTHORITY}{_PATH_ABEMPTY}|{_PATH_ABSOLUTE}|{_PATH_ROOTLESS}|)"
_QUERY = rf"(?:{_PCHAR}|[/?])*"
URI = re.compile(rf"[A-Za-z][A-Za-z0-9+\-.]*:{_HIER}(?:\?{_QUERY})?(?:#{_QUERY})?")
UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
DECIMAL_LOGICAL = re.compile(r"Decimal\(([1-9][0-9]?),([0-9][0-9]?)\)")


def parse_decimal_logical(name):
    """(p, s) of `Decimal(p,s)` with p in 1..38 and s in 0..p, else None."""
    m = DECIMAL_LOGICAL.fullmatch(name) if isinstance(name, str) else None
    if not m:
        return None
    p, s = int(m.group(1)), int(m.group(2))
    return (p, s) if 1 <= p <= 38 and 0 <= s <= p else None


def decimal_fits(value, p, s):
    """§9.1.5: at most s digits after the point; at most p digits in all, the
    integer part without leading zeros and the fraction padded to s (Q-DEC-1)."""
    body = value[1:] if value.startswith("-") else value
    whole, _, frac = body.partition(".")
    if len(frac) > s:
        return False
    whole_digits = 0 if whole == "0" else len(whole)
    return whole_digits + s <= p


def check(fmt, value, logical=None):
    """True when the string `value` is valid for `fmt` (§9.2.2)."""
    if fmt in ("capability-id", "pinned-id", "exact-id", "address", "date", "date-time", "duration"):
        return grammar.matches(fmt, value)
    if fmt == "decimal":
        if not grammar.matches("decimal", value):
            return False
        ps = parse_decimal_logical(logical) if logical else None
        return decimal_fits(value, *ps) if ps else True
    if fmt == "int64":
        return grammar.matches("integer-str", value) and -(2**63) <= int(value) <= 2**63 - 1
    if fmt == "uri":
        return value.isascii() and URI.fullmatch(value) is not None
    if fmt == "uuid":
        return UUID.fullmatch(value) is not None
    raise ValueError(f"format {fmt!r} is not in the profile")
