"""§9.4: a profile's projection, its canonical form and its fingerprint.

The canonical form is this package's own RFC 8785 (`jcs`); `check` compares it
with the `rfc8785` package on every vector.
"""

import copy
import hashlib

from . import jcs


def strip_schema(s):
    """§9.4.1 steps 7 to 10, at schema positions only: a property named
    `title` under `properties` is kept."""
    if not isinstance(s, dict):
        return
    for name in ("title", "description", "$comment", "examples"):  # 7
        s.pop(name, None)
    for name in ("properties", "$defs"):  # 8
        if isinstance(s.get(name), dict):
            for sub in s[name].values():
                strip_schema(sub)
    strip_schema(s.get("items"))
    if isinstance(s.get("additionalProperties"), dict):
        strip_schema(s["additionalProperties"])
    for name in ("prefixItems", "anyOf", "oneOf"):
        if isinstance(s.get(name), list):
            for sub in s[name]:
                strip_schema(sub)
    if isinstance(s.get("x-atlas-enum"), list):  # 9
        for entry in s["x-atlas-enum"]:
            if isinstance(entry, dict):
                entry.pop("description", None)
    if isinstance(s.get("x-atlas-deprecated"), dict):  # 10
        s["x-atlas-deprecated"].pop("reason", None)


def projection(profile):
    """§9.4.1: the profile without what does not change how a client calls,
    resolves or reads it."""
    p = copy.deepcopy(profile)  # 1
    for name in ("fingerprint", "revision", "usage"):  # 2
        p.pop(name, None)
    if isinstance(p.get("identity"), dict):  # 3
        for name in ("title", "summary", "owner"):
            p["identity"].pop(name, None)
    if isinstance(p.get("errors"), list):  # 4
        for e in p["errors"]:
            if isinstance(e, dict):
                e.pop("description", None)
                e.pop("next", None)
    if isinstance(p.get("evolution"), dict):  # 5
        p["evolution"].pop("changelog", None)
    if isinstance(p.get("schemas"), dict):  # 6
        for s in p["schemas"].values():
            strip_schema(s)
    return p


def canonical(profile):
    """§9.4.2 steps 1 and 2: the projection's canonical form, as UTF-8."""
    return jcs.dumps(projection(profile))


def sha256(profile):
    return hashlib.sha256(canonical(profile)).hexdigest()


def fingerprint(profile):
    """§9.4.2 steps 3 and 4: the first 16 of the digest's 64 hex digits."""
    return sha256(profile)[:16]


def exact_id(profile):
    ident = profile["identity"]
    return f"{ident['id']}@v{ident['version']}#{fingerprint(profile)}"


def with_fingerprint(profile):
    """The profile as the gateway serves it: its `fingerprint` set."""
    p = copy.deepcopy(profile)
    p["fingerprint"] = fingerprint(p)
    return p
