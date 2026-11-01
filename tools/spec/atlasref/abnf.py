"""A small ABNF engine (RFC 5234 with RFC 7405's %s and %i), so that
`references/grammar.abnf` itself can be checked against the grammar vectors.

It parses the rules into a tree and matches a string by computing every
position a rule can end at (no backtracking blow-up for these grammars).
Prose values (<...>) and incremental alternatives (=/) are not needed by the
Atlas grammar and are refused.
"""

import re

CORE = """
ALPHA  = %x41-5A / %x61-7A
DIGIT  = %x30-39
HEXDIG = DIGIT / "A" / "B" / "C" / "D" / "E" / "F"
DQUOTE = %x22
SP     = %x20
HTAB   = %x09
WSP    = SP / HTAB
CR     = %x0D
LF     = %x0A
CRLF   = CR LF
VCHAR  = %x21-7E
"""


class ABNFError(ValueError):
    pass


def _strip_comment(line):
    out, quoted = [], False
    for ch in line:
        if ch == '"':
            quoted = not quoted
        if ch == ";" and not quoted:
            break
        out.append(ch)
    return "".join(out)


def _rules_text(text):
    """Yields (name, definition) with continuation lines joined."""
    current = None
    for raw in text.splitlines():
        line = _strip_comment(raw).rstrip()
        if not line.strip():
            continue
        if line[0] in " \t":
            if current is None:
                raise ABNFError(f"continuation with no rule: {raw!r}")
            current[1].append(line.strip())
            continue
        if current:
            yield current[0], " ".join(current[1])
        name, sep, rest = line.partition("=")
        if not sep or rest.startswith("/"):
            raise ABNFError(f"not a rule: {raw!r}")
        current = (name.strip().lower(), [rest.strip()])
    if current:
        yield current[0], " ".join(current[1])


_TOKEN = re.compile(
    r"\s*(?:"
    r"(?P<repeat>\d*\*\d*|\d+)(?=[\w(\[\"%])"
    r"|(?P<name>[A-Za-z][A-Za-z0-9-]*)"
    r'|(?P<str>%[si]?"[^"]*"|"[^"]*")'
    r"|(?P<num>%[xdb][0-9A-Fa-f]+(?:(?:\.[0-9A-Fa-f]+)+|-[0-9A-Fa-f]+)?)"
    r"|(?P<punct>[/()\[\]])"
    r")"
)


def _tokens(text):
    pos, out = 0, []
    while pos < len(text):
        if text[pos:].strip() == "":
            break
        m = _TOKEN.match(text, pos)
        if not m or m.end() == pos:
            raise ABNFError(f"cannot read {text[pos:]!r}")
        kind = m.lastgroup
        out.append((kind, m.group(kind)))
        pos = m.end()
    return out


class _Parser:
    def __init__(self, tokens):
        self.t, self.i = tokens, 0

    def peek(self):
        return self.t[self.i] if self.i < len(self.t) else (None, None)

    def take(self):
        tok = self.peek()
        self.i += 1
        return tok

    def alternation(self):
        alts = [self.concatenation()]
        while self.peek() == ("punct", "/"):
            self.take()
            alts.append(self.concatenation())
        return ("alt", alts) if len(alts) > 1 else alts[0]

    def concatenation(self):
        items = []
        while True:
            kind, value = self.peek()
            if kind is None or (kind == "punct" and value in "/)]"):
                break
            items.append(self.repetition())
        if not items:
            raise ABNFError("empty concatenation")
        return ("cat", items) if len(items) > 1 else items[0]

    def repetition(self):
        kind, value = self.peek()
        lo, hi = 1, 1
        if kind == "repeat":
            self.take()
            if "*" in value:
                a, b = value.split("*")
                lo, hi = int(a or 0), int(b) if b else None
            else:
                lo = hi = int(value)
        element = self.element()
        return element if (lo, hi) == (1, 1) else ("rep", lo, hi, element)

    def element(self):
        kind, value = self.take()
        if kind == "name":
            return ("ref", value.lower())
        if kind == "str":
            if value.startswith("%s"):
                return ("lit", value[3:-1], True)
            if value.startswith("%i"):
                return ("lit", value[3:-1], False)
            return ("lit", value[1:-1], False)
        if kind == "num":
            base = {"x": 16, "d": 10, "b": 2}[value[1]]
            body = value[2:]
            if "-" in body:
                a, b = body.split("-")
                return ("range", int(a, base), int(b, base))
            return ("lit", "".join(chr(int(p, base)) for p in body.split(".")), True)
        if (kind, value) == ("punct", "("):
            inner = self.alternation()
            if self.take() != ("punct", ")"):
                raise ABNFError("unclosed (")
            return inner
        if (kind, value) == ("punct", "["):
            inner = self.alternation()
            if self.take() != ("punct", "]"):
                raise ABNFError("unclosed [")
            return ("rep", 0, 1, inner)
        raise ABNFError(f"unexpected {value!r}")


def parse(text, with_core=True):
    rules = {}
    for source in ([CORE] if with_core else []) + [text]:
        for name, definition in _rules_text(source):
            if name in rules and source is text:
                raise ABNFError(f"rule {name} defined twice")
            p = _Parser(_tokens(definition))
            tree = p.alternation()
            if p.i != len(p.t):
                raise ABNFError(f"trailing tokens in {name}")
            rules[name] = tree
    for name, tree in rules.items():
        _check_refs(tree, rules, name)
    return Grammar(rules)


def _check_refs(tree, rules, name):
    kind = tree[0]
    if kind == "ref" and tree[1] not in rules:
        raise ABNFError(f"{name} refers to undefined rule {tree[1]}")
    for sub in tree[1] if kind in ("alt", "cat") else ([tree[3]] if kind == "rep" else []):
        _check_refs(sub, rules, name)


class Grammar:
    def __init__(self, rules):
        self.rules = rules

    def matches(self, rule, s):
        memo = {}
        return len(s) in self._ends(("ref", rule.lower()), s, 0, memo)

    def _ends(self, node, s, i, memo):
        key = (id(node), i)
        if key in memo:
            return memo[key]
        memo[key] = frozenset()  # no left recursion in these grammars
        kind = node[0]
        if kind == "ref":
            out = self._ends(self.rules[node[1]], s, i, memo)
        elif kind == "lit":
            text, sensitive = node[1], node[2]
            chunk = s[i : i + len(text)]
            ok = chunk == text if sensitive else chunk.lower() == text.lower()
            out = frozenset([i + len(text)]) if ok and len(chunk) == len(text) else frozenset()
        elif kind == "range":
            out = frozenset([i + 1]) if i < len(s) and node[1] <= ord(s[i]) <= node[2] else frozenset()
        elif kind == "cat":
            positions = {i}
            for sub in node[1]:
                positions = set().union(*(self._ends(sub, s, p, memo) for p in positions)) if positions else set()
            out = frozenset(positions)
        elif kind == "alt":
            out = frozenset().union(*(self._ends(sub, s, i, memo) for sub in node[1]))
        elif kind == "rep":
            lo, hi, sub = node[1], node[2], node[3]
            result = {i} if lo == 0 else set()
            frontier, count = {i}, 0
            limit = hi if hi is not None else len(s) + lo + 1
            while frontier and count < limit:
                count += 1
                frontier = set().union(*(self._ends(sub, s, p, memo) for p in frontier))
                if count >= lo:
                    result |= frontier
            out = frozenset(result)
        else:
            raise ABNFError(kind)
        memo[key] = out
        return out
