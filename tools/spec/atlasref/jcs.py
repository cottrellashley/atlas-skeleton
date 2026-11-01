"""RFC 8785, the JSON Canonicalization Scheme (§9.4.2 step 1), written from
the RFC so it can be checked against the `rfc8785` package and both against
the hand-worked cases in the tests.
"""

import decimal
import math

from .jsonio import is_number

BS = chr(92)
_SHORT = {0x08: "b", 0x09: "t", 0x0A: "n", 0x0C: "f", 0x0D: "r"}


def string(s):
    out = ['"']
    for ch in s:
        cp = ord(ch)
        if ch == '"':
            out.append(BS + '"')
        elif ch == BS:
            out.append(BS + BS)
        elif cp < 0x20:
            out.append(BS + _SHORT[cp] if cp in _SHORT else BS + "u" + format(cp, "04x"))
        else:
            out.append(ch)
    out.append('"')
    return "".join(out)


def number(v):
    """ECMAScript's Number.prototype.toString of the binary64 value."""
    x = float(v)
    if math.isnan(x) or math.isinf(x):
        raise ValueError("NaN and the infinities have no JSON form")
    if x == 0:
        return "0"
    sign = "-" if x < 0 else ""
    d = decimal.Decimal(repr(abs(x)))  # repr is the shortest round-trip form
    _, digit_tuple, exp = d.as_tuple()
    digits = "".join(str(t) for t in digit_tuple)
    stripped = digits.rstrip("0")
    exp += len(digits) - len(stripped)
    digits = stripped
    k = len(digits)
    n = k + exp  # the value is 0.digits × 10^n
    if k <= n <= 21:
        body = digits + "0" * (n - k)
    elif 0 < n <= 21:
        body = digits[:n] + "." + digits[n:]
    elif -6 < n <= 0:
        body = "0." + "0" * (-n) + digits
    else:
        e = n - 1
        body = digits[0] + ("." + digits[1:] if k > 1 else "") + "e" + ("+" if e >= 0 else "-") + str(abs(e))
    return sign + body


def _utf16_key(name):
    return name.encode("utf-16-be")


def canonical(value):
    """The canonical form as text."""
    if value is None:
        return "null"
    if value is True:
        return "true"
    if value is False:
        return "false"
    if is_number(value):
        return number(value)
    if isinstance(value, str):
        return string(value)
    if isinstance(value, list):
        return "[" + ",".join(canonical(v) for v in value) + "]"
    if isinstance(value, dict):
        names = sorted(value, key=_utf16_key)
        return "{" + ",".join(string(n) + ":" + canonical(value[n]) for n in names) + "}"
    raise TypeError(type(value))


def dumps(value):
    """The canonical form as UTF-8 bytes (§9.4.2 step 2)."""
    return canonical(value).encode("utf-8")
