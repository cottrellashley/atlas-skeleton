"""§9.2.4: a `pattern` is `^ R $`, where R is an I-Regexp (RFC 9485), matched
against the whole value's code points.

The parser follows RFC 9485's ABNF; the translation to Python's `re` follows
its section 5 (`.` is `[^\\n\\r]`; `\\p{..}` categories are expanded here from
`unicodedata`, so Python's engine never interprets them). Outside the matched
`^` and `$`, `^` and `$` are ordinary characters in I-Regexp.

Two restrictions beyond RFC 9485 are applied, recorded in QUESTIONS.md
(Q-RE-1): `\\p{C}`/`\\p{Cn}` (and their complements) are refused, since their
meaning moves with every Unicode version, and a quantifier's counts are at
most 1000, the largest every common engine accepts.
"""

import functools
import re
import sys
import unicodedata

BS = chr(92)
MAX_COUNT = 1000
CATEGORIES = {
    "L", "Ll", "Lm", "Lo", "Lt", "Lu", "M", "Mc", "Me", "Mn", "N", "Nd", "Nl", "No",
    "P", "Pc", "Pd", "Pe", "Pf", "Pi", "Po", "Ps", "Z", "Zl", "Zp", "Zs",
    "S", "Sc", "Sk", "Sm", "So", "C", "Cc", "Cf", "Cn", "Co",
}
REFUSED_CATEGORIES = {"C", "Cn"}
_NORMAL_EXCLUDED = set("()*+.?[" + BS + "]{|}")
_CC_EXCLUDED = set("[" + BS + "]-")
_SINGLE_ESC = set("()*+-.?[" + BS + "]^nrt{|}")


class InvalidIRegexp(ValueError):
    pass


@functools.lru_cache(maxsize=None)
def category_ranges(cat):
    """The code points of a general category (or a one-letter group), as
    sorted (lo, hi) ranges, surrogates excluded."""
    ranges, start, prev = [], None, None
    for cp in range(sys.maxunicode + 1):
        if 0xD800 <= cp <= 0xDFFF:
            member = False
        else:
            c = unicodedata.category(chr(cp))
            member = c == cat if len(cat) == 2 else c[0] == cat
        if member:
            if start is None:
                start = cp
            prev = cp
        elif start is not None:
            ranges.append((start, prev))
            start = None
    if start is not None:
        ranges.append((start, prev))
    return tuple(ranges)


def _complement(ranges):
    out, nxt = [], 0
    for lo, hi in ranges:
        if lo > nxt:
            out.append((nxt, lo - 1))
        nxt = hi + 1
    if nxt <= sys.maxunicode:
        out.append((nxt, sys.maxunicode))
    return out


def _normalise(ranges):
    out = []
    for lo, hi in sorted(ranges):
        if out and lo <= out[-1][1] + 1:
            out[-1] = (out[-1][0], max(out[-1][1], hi))
        else:
            out.append((lo, hi))
    return out


def _cp_escape(cp):
    return BS + "U" + format(cp, "08x")


def _class(ranges, negate=False):
    body = "".join(_cp_escape(lo) if lo == hi else _cp_escape(lo) + "-" + _cp_escape(hi) for lo, hi in ranges)
    if not body:
        return "(?!)" if not negate else "[" + _cp_escape(0) + "-" + _cp_escape(sys.maxunicode) + "]"
    return "[" + ("^" if negate else "") + body + "]"


class _Parser:
    def __init__(self, src):
        self.s, self.i = src, 0

    def peek(self):
        return self.s[self.i] if self.i < len(self.s) else None

    def take(self):
        ch = self.peek()
        if ch is None:
            raise InvalidIRegexp("unexpected end")
        self.i += 1
        return ch

    def regexp(self):
        branches = [self.branch()]
        while self.peek() == "|":
            self.take()
            branches.append(self.branch())
        return "|".join(branches)

    def branch(self):
        out = []
        while self.peek() is not None and self.peek() not in "|)":
            out.append(self.piece())
        return "".join(out)

    def piece(self):
        atom = self.atom()
        q = self.quantifier()
        return "(?:" + atom + ")" + q if q else atom

    def quantifier(self):
        ch = self.peek()
        if ch in ("*", "+", "?"):
            self.take()
            return ch
        if ch == "{":
            self.take()
            lo = self.digits()
            hi = lo
            if self.peek() == ",":
                self.take()
                hi = self.digits() if self.peek() != "}" else None
            if self.take() != "}":
                raise InvalidIRegexp("bad range quantifier")
            if lo > MAX_COUNT or (hi is not None and hi > MAX_COUNT):
                raise InvalidIRegexp("quantifier count above 1000 (Q-RE-1)")
            if hi is not None and hi < lo:
                raise InvalidIRegexp("quantifier {n,m} with m < n")
            return "{" + str(lo) + ("" if hi == lo else "," + ("" if hi is None else str(hi))) + "}"
        return ""

    def digits(self):
        start = self.i
        while self.peek() is not None and self.peek().isascii() and self.peek().isdigit():
            self.i += 1
        if self.i == start:
            raise InvalidIRegexp("quantifier needs a count")
        return int(self.s[start : self.i])

    def atom(self):
        ch = self.peek()
        if ch == "(":
            self.take()
            inner = self.regexp()
            if self.take() != ")":
                raise InvalidIRegexp("unclosed group")
            return "(?:" + inner + ")"
        if ch == ".":
            self.take()
            return "[^" + BS + "n" + BS + "r]"
        if ch == "[":
            return self.class_expr()
        if ch == BS:
            kind, value = self.escape()
            if kind == "char":
                return re.escape(value)
            return _class(value)
        if ch in _NORMAL_EXCLUDED:
            raise InvalidIRegexp(f"{ch!r} must be escaped")
        if 0xD800 <= ord(ch) <= 0xDFFF:
            raise InvalidIRegexp("surrogate")
        self.take()
        return re.escape(ch)

    def escape(self):
        """Returns ("char", c) for a SingleCharEsc, ("set", ranges) for \\p/\\P."""
        self.take()
        ch = self.take()
        if ch in ("p", "P"):
            if self.take() != "{":
                raise InvalidIRegexp("\\p needs {")
            start = self.i
            while self.peek() not in (None, "}"):
                self.i += 1
            name = self.s[start : self.i]
            if self.take() != "}":
                raise InvalidIRegexp("unclosed \\p{")
            if name not in CATEGORIES:
                raise InvalidIRegexp(f"unknown category {name!r}")
            if name in REFUSED_CATEGORIES:
                raise InvalidIRegexp(f"\\p{{{name}}} depends on the Unicode version (Q-RE-1)")
            ranges = list(category_ranges(name))
            return "set", ranges if ch == "p" else _complement(ranges)
        if ch not in _SINGLE_ESC:
            raise InvalidIRegexp(f"\\{ch} is not an I-Regexp escape")
        return "char", {"n": "\n", "r": "\r", "t": "\t"}.get(ch, ch)

    def cc_char(self):
        ch = self.peek()
        if ch == BS:
            kind, value = self.escape()
            return kind, value
        if ch is None or ch in _CC_EXCLUDED:
            raise InvalidIRegexp(f"{ch!r} in a class")
        self.take()
        return "char", ch

    def class_expr(self):
        self.take()  # [
        negate = False
        if self.peek() == "^":
            self.take()
            negate = True
        ranges = []
        first = True
        while True:
            ch = self.peek()
            if ch is None:
                raise InvalidIRegexp("unclosed class")
            if ch == "]" and not first:
                self.take()
                break
            if ch == "-" and (first or self.s[self.i + 1 : self.i + 2] == "]"):
                self.take()
                ranges.append((ord("-"), ord("-")))
                first = False
                continue
            kind, value = self.cc_char()
            if kind == "set":
                ranges.extend(value)
            elif self.peek() == "-" and self.s[self.i + 1 : self.i + 2] != "]":
                self.take()
                kind2, value2 = self.cc_char()
                if kind2 != "char" or ord(value2) < ord(value):
                    raise InvalidIRegexp("bad range in class")
                ranges.append((ord(value), ord(value2)))
            else:
                ranges.append((ord(value), ord(value)))
            first = False
        return _class(_normalise(ranges), negate)


@functools.lru_cache(maxsize=4096)
def translate(r):
    """An I-Regexp R as an equivalent Python pattern, or InvalidIRegexp."""
    p = _Parser(r)
    out = p.regexp()
    if p.i != len(r):
        raise InvalidIRegexp(f"unexpected {r[p.i]!r}")
    return out


def split_pattern(pattern):
    """§9.2.4: a pattern is ^ R $; returns R, or raises InvalidIRegexp."""
    if not (isinstance(pattern, str) and len(pattern) >= 2 and pattern[0] == "^" and pattern[-1] == "$"):
        raise InvalidIRegexp("a pattern is ^ R $")
    return pattern[1:-1]


@functools.lru_cache(maxsize=4096)
def compile_pattern(pattern):
    return re.compile(translate(split_pattern(pattern)))


def matches(pattern, value):
    return compile_pattern(pattern).fullmatch(value) is not None
