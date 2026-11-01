"""I-JSON (RFC 7493) as §9.1.2 requires it: reading strictly, writing
deterministically.

`loads` refuses what §9.1.2 forbids (a byte-order mark, invalid UTF-8,
duplicate member names, unpaired surrogates, noncharacters, NaN and the
infinities, and numbers that overflow binary64) by raising `NotIJSON`, which a
caller turns into `invalid_document`.

`dumps` is the one way every file under `references/` is written, so a
regeneration is byte-identical: UTF-8, two-space indent, members in the order
the generator built them, a final newline.
"""

import json
import math


class NotIJSON(ValueError):
    """The text is not an I-JSON message (§9.1.2): `invalid_document`."""


def _check_string(s):
    for ch in s:
        cp = ord(ch)
        if 0xD800 <= cp <= 0xDFFF:
            raise NotIJSON(f"unpaired surrogate U+{cp:04X}")
        if 0xFDD0 <= cp <= 0xFDEF or (cp & 0xFFFE) == 0xFFFE:
            raise NotIJSON(f"noncharacter U+{cp:04X}")
    return s


def _pairs(pairs):
    out = {}
    for name, value in pairs:
        _check_string(name)
        if name in out:
            raise NotIJSON(f"duplicate member {name!r}")
        out[name] = value
    return out


def _constant(name):
    raise NotIJSON(f"{name} is not a JSON number")


def _float(text):
    value = float(text)
    if math.isinf(value):
        raise NotIJSON(f"{text} overflows binary64")
    return value


def _walk_strings(value):
    if isinstance(value, str):
        _check_string(value)
    elif isinstance(value, dict):
        for v in value.values():
            _walk_strings(v)
    elif isinstance(value, list):
        for v in value:
            _walk_strings(v)


def loads(data):
    """Parses an I-JSON message from bytes (or str), or raises NotIJSON."""
    if isinstance(data, (bytes, bytearray)):
        if data.startswith(b"\xef\xbb\xbf"):
            raise NotIJSON("byte-order mark")
        try:
            text = bytes(data).decode("utf-8", errors="strict")
        except UnicodeDecodeError as e:
            raise NotIJSON(f"not UTF-8: {e}") from None
    else:
        text = data
    try:
        value = json.loads(text, object_pairs_hook=_pairs, parse_constant=_constant, parse_float=_float)
    except NotIJSON:
        raise
    except ValueError as e:
        raise NotIJSON(f"not JSON: {e}") from None
    _walk_strings(value)
    return value


def load(path):
    with open(path, "rb") as f:
        return loads(f.read())


def dumps(value, indent=2):
    """The deterministic form every reference file is written in."""
    return (json.dumps(value, ensure_ascii=False, indent=indent, allow_nan=False) + "\n").encode("utf-8")


# --- JSON values under I-JSON's number model -------------------------------

MAX_SAFE = 2**53 - 1


def is_number(v):
    return isinstance(v, (int, float)) and not isinstance(v, bool)


def as_binary64(v):
    """A JSON number as the binary64 value I-JSON says it is."""
    return float(v)


def is_integer(v):
    """§9.2.2: a JSON number with no fractional part within the I-JSON range."""
    if not is_number(v):
        return False
    f = as_binary64(v)
    return f.is_integer() and -MAX_SAFE <= f <= MAX_SAFE


def json_type(v):
    """The JSON type of a parsed value; `integer` is a refinement of `number`."""
    if v is None:
        return "null"
    if isinstance(v, bool):
        return "boolean"
    if is_number(v):
        return "number"
    if isinstance(v, str):
        return "string"
    if isinstance(v, list):
        return "array"
    if isinstance(v, dict):
        return "object"
    raise TypeError(type(v))


def equal(a, b):
    """JSON equality: numbers as binary64, objects regardless of member order."""
    ta, tb = json_type(a), json_type(b)
    if ta != tb:
        return False
    if ta == "number":
        return as_binary64(a) == as_binary64(b)
    if ta == "array":
        return len(a) == len(b) and all(equal(x, y) for x, y in zip(a, b))
    if ta == "object":
        return a.keys() == b.keys() and all(equal(a[k], b[k]) for k in a)
    return a == b
