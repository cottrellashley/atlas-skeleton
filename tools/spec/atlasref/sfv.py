"""RFC 9651, Structured Field Values for HTTP: the parsing and serialisation
algorithms of its section 4, transcribed. Every Atlas header is one (§9.6.3).

Values: an Item is (bare, params); an Inner List is (InnerList([items]),
params); params and dictionaries are ordered dicts. Bare items are int,
decimal.Decimal, str, Token, bytes, bool, Date and DisplayString.
"""

import base64
import decimal


class SFError(ValueError):
    pass


class Token(str):
    pass


class DisplayString(str):
    pass


class Date(int):
    pass


class InnerList(list):
    pass


_LCALPHA = set("abcdefghijklmnopqrstuvwxyz")
_ALPHA = _LCALPHA | set("ABCDEFGHIJKLMNOPQRSTUVWXYZ")
_DIGIT = set("0123456789")
_TCHAR = _ALPHA | _DIGIT | set("!#$%&'*+-.^_`|~")
_KEY_REST = _LCALPHA | _DIGIT | set("_-.*")
_B64 = _ALPHA | _DIGIT | set("+/=")


class _Input:
    def __init__(self, s):
        self.s, self.i = s, 0

    def peek(self):
        return self.s[self.i] if self.i < len(self.s) else ""

    def take(self):
        ch = self.peek()
        self.i += 1
        return ch

    def empty(self):
        return self.i >= len(self.s)

    def skip_sp(self):
        while self.peek() == " ":
            self.i += 1

    def skip_ows(self):
        while self.peek() in (" ", "\t") and self.peek() != "":
            self.i += 1


def parse(text, kind):
    """§4.2: parses a field value (the joined lines) as "item", "list" or
    "dictionary", or raises SFError."""
    if isinstance(text, bytes):
        try:
            text = text.decode("ascii")
        except UnicodeDecodeError:
            raise SFError("not ASCII") from None
    if not text.isascii():
        raise SFError("not ASCII")
    inp = _Input(text)
    inp.skip_sp()
    if kind == "list":
        out = _list(inp)
    elif kind == "dictionary":
        out = _dictionary(inp)
    elif kind == "item":
        out = _item(inp)
    else:
        raise ValueError(kind)
    inp.skip_sp()
    if not inp.empty():
        raise SFError("trailing characters")
    return out


def join_lines(lines):
    """RFC 9110 section 5.3: lines of one field, combined with ", "."""
    return ", ".join(lines)


def _list(inp):
    members = []
    while not inp.empty():
        members.append(_item_or_inner(inp))
        inp.skip_ows()
        if inp.empty():
            return members
        if inp.take() != ",":
            raise SFError("expected , in list")
        inp.skip_ows()
        if inp.empty():
            raise SFError("trailing , in list")
    return members


def _dictionary(inp):
    out = {}
    while not inp.empty():
        key = _key(inp)
        if inp.peek() == "=":
            inp.take()
            member = _item_or_inner(inp)
        else:
            member = (True, _params(inp))
        out[key] = member
        inp.skip_ows()
        if inp.empty():
            return out
        if inp.take() != ",":
            raise SFError("expected , in dictionary")
        inp.skip_ows()
        if inp.empty():
            raise SFError("trailing , in dictionary")
    return out


def _item_or_inner(inp):
    if inp.peek() == "(":
        return _inner_list(inp)
    return _item(inp)


def _inner_list(inp):
    inp.take()
    items = InnerList()
    while not inp.empty():
        inp.skip_sp()
        if inp.peek() == ")":
            inp.take()
            return items, _params(inp)
        items.append(_item(inp))
        if inp.peek() not in (" ", ")"):
            raise SFError("expected SP or ) in inner list")
    raise SFError("unterminated inner list")


def _item(inp):
    bare = _bare(inp)
    return bare, _params(inp)


def _params(inp):
    params = {}
    while not inp.empty():
        if inp.peek() != ";":
            break
        inp.take()
        inp.skip_sp()
        key = _key(inp)
        value = True
        if inp.peek() == "=":
            inp.take()
            value = _bare(inp)
        params[key] = value
    return params


def _key(inp):
    ch = inp.peek()
    if ch not in _LCALPHA and ch != "*":
        raise SFError("bad key")
    start = inp.i
    while not inp.empty() and inp.peek() in _KEY_REST:
        inp.take()
    return inp.s[start : inp.i]


def _bare(inp):
    ch = inp.peek()
    if ch == "-" or ch in _DIGIT:
        return _number(inp)
    if ch == '"':
        return _string(inp)
    if ch == "*" or ch in _ALPHA:
        return _token(inp)
    if ch == ":":
        return _binary(inp)
    if ch == "?":
        return _boolean(inp)
    if ch == "@":
        return _date(inp)
    if ch == "%":
        return _display(inp)
    raise SFError("bad item")


def _number(inp):
    kind, sign, num = "integer", 1, ""
    if inp.peek() == "-":
        inp.take()
        sign = -1
    if inp.empty():
        raise SFError("empty number")
    if inp.peek() not in _DIGIT:
        raise SFError("number without a digit")
    while not inp.empty():
        ch = inp.peek()
        if ch in _DIGIT:
            num += inp.take()
        elif kind == "integer" and ch == ".":
            if len(num) > 12:
                raise SFError("decimal with too many integer digits")
            num += inp.take()
            kind = "decimal"
        else:
            break
        if kind == "integer" and len(num) > 15:
            raise SFError("integer too long")
        if kind == "decimal" and len(num) > 16:
            raise SFError("decimal too long")
    if kind == "integer":
        return sign * int(num)
    if num.endswith("."):
        raise SFError("decimal ends with .")
    if len(num.split(".")[1]) > 3:
        raise SFError("decimal with too many fractional digits")
    return decimal.Decimal(num) * sign


def _string(inp):
    inp.take()
    out = []
    while not inp.empty():
        ch = inp.take()
        if ch == "\\":
            if inp.empty():
                raise SFError("unterminated escape")
            nxt = inp.take()
            if nxt not in ('"', "\\"):
                raise SFError("bad escape")
            out.append(nxt)
        elif ch == '"':
            return "".join(out)
        elif ord(ch) < 0x20 or ord(ch) > 0x7E:
            raise SFError("bad character in string")
        else:
            out.append(ch)
    raise SFError("unterminated string")


def _token(inp):
    start = inp.i
    inp.take()
    while not inp.empty() and (inp.peek() in _TCHAR or inp.peek() in ":/"):
        inp.take()
    return Token(inp.s[start : inp.i])


def _binary(inp):
    inp.take()
    start = inp.i
    while not inp.empty() and inp.peek() != ":":
        if inp.peek() not in _B64:
            raise SFError("bad base64")
        inp.take()
    if inp.empty():
        raise SFError("unterminated byte sequence")
    content = inp.s[start : inp.i]
    inp.take()
    try:
        padded = content + "=" * (-len(content) % 4)
        return base64.b64decode(padded, validate=True)
    except Exception:
        raise SFError("bad base64") from None


def _boolean(inp):
    inp.take()
    ch = inp.take()
    if ch == "1":
        return True
    if ch == "0":
        return False
    raise SFError("bad boolean")


def _date(inp):
    inp.take()
    value = _number(inp)
    if not isinstance(value, int):
        raise SFError("a date is an integer")
    return Date(value)


def _display(inp):
    inp.take()
    if inp.take() != '"':
        raise SFError("bad display string")
    out = bytearray()
    while not inp.empty():
        ch = inp.take()
        if ord(ch) < 0x20 or ord(ch) > 0x7E:
            raise SFError("bad character in display string")
        if ch == "%":
            hexd = inp.s[inp.i : inp.i + 2]
            if len(hexd) != 2 or any(c not in "0123456789abcdef" for c in hexd):
                raise SFError("bad percent-encoding")
            inp.i += 2
            out.append(int(hexd, 16))
        elif ch == '"':
            try:
                return DisplayString(out.decode("utf-8"))
            except UnicodeDecodeError:
                raise SFError("display string not UTF-8") from None
        else:
            out.append(ord(ch))
    raise SFError("unterminated display string")


# --- §4.1, serialising -------------------------------------------------------


def serialize(value, kind):
    if kind == "list":
        return ", ".join(_ser_member(m) for m in value)
    if kind == "dictionary":
        parts = []
        for k, (v, params) in value.items():
            if v is True and not isinstance(v, InnerList):
                parts.append(_ser_key(k) + _ser_params(params))
            else:
                parts.append(_ser_key(k) + "=" + _ser_member((v, params)))
        return ", ".join(parts)
    return _ser_member(value)


def _ser_member(m):
    v, params = m
    if isinstance(v, InnerList):
        return "(" + " ".join(_ser_member(i) for i in v) + ")" + _ser_params(params)
    return _ser_bare(v) + _ser_params(params)


def _ser_params(params):
    out = []
    for k, v in params.items():
        out.append(";" + _ser_key(k) + ("" if v is True else "=" + _ser_bare(v)))
    return "".join(out)


def _ser_key(k):
    if not k or (k[0] not in _LCALPHA and k[0] != "*") or any(c not in _KEY_REST for c in k):
        raise SFError("bad key")
    return k


def _ser_bare(v):
    if isinstance(v, bool):
        return "?1" if v else "?0"
    if isinstance(v, Date):
        return "@" + str(int(v))
    if isinstance(v, int):
        if abs(v) > 999_999_999_999_999:
            raise SFError("integer out of range")
        return str(v)
    if isinstance(v, decimal.Decimal):
        q = v.quantize(decimal.Decimal("0.001"), rounding=decimal.ROUND_HALF_EVEN)
        if abs(q) >= 10**12:
            raise SFError("decimal out of range")
        sign = "-" if q < 0 else ""
        whole, _, frac = format(abs(q), "f").partition(".")
        frac = frac.rstrip("0") or "0"
        return sign + whole + "." + frac
    if isinstance(v, DisplayString):
        out = ['%"']
        for b in v.encode("utf-8"):
            ch = chr(b)
            if ch in '%"' or b < 0x20 or b > 0x7E:
                out.append("%" + format(b, "02x"))
            else:
                out.append(ch)
        return "".join(out) + '"'
    if isinstance(v, Token):
        if not v or (v[0] not in _ALPHA and v[0] != "*") or any(c not in _TCHAR and c not in ":/" for c in v):
            raise SFError("bad token")
        return str(v)
    if isinstance(v, str):
        if any(ord(c) < 0x20 or ord(c) > 0x7E for c in v):
            raise SFError("bad string")
        return '"' + v.replace("\\", "\\\\").replace('"', '\\"') + '"'
    if isinstance(v, (bytes, bytearray)):
        return ":" + base64.b64encode(bytes(v)).decode("ascii") + ":"
    raise SFError(f"cannot serialise {type(v)}")


# --- the httpwg structured-field-tests JSON form ---------------------------


def to_json(value, kind):
    """The form github.com/httpwg/structured-field-tests uses for `expected`."""
    if kind == "list":
        return [_member_json(m) for m in value]
    if kind == "dictionary":
        return [[k, _member_json(m)] for k, m in value.items()]
    return _member_json(value)


def _member_json(m):
    v, params = m
    p = [[k, _bare_json(x)] for k, x in params.items()]
    if isinstance(v, InnerList):
        return [[_member_json(i) for i in v], p]
    return [_bare_json(v), p]


def _bare_json(v):
    if isinstance(v, bool):
        return v
    if isinstance(v, Date):
        return {"__type": "date", "value": int(v)}
    if isinstance(v, int):
        return v
    if isinstance(v, decimal.Decimal):
        return float(v)
    if isinstance(v, DisplayString):
        return {"__type": "displaystring", "value": str(v)}
    if isinstance(v, Token):
        return {"__type": "token", "value": str(v)}
    if isinstance(v, str):
        return v
    if isinstance(v, (bytes, bytearray)):
        return {"__type": "binary", "value": base64.b32encode(bytes(v)).decode("ascii")}
    raise TypeError(type(v))
