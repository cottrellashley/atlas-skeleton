"""references/vectors/fingerprints.json (§9.4.4) and references/vectors/profiles/
(the complete profiles, one per family)."""

import copy
import hashlib

import rfc8785

from .. import fingerprint, jcs, profiles
from ..jsonio import dumps

BS = chr(92)


def reorder(v):
    """The same value with every object's members in reverse order (arrays keep
    theirs: their order is meaning)."""
    if isinstance(v, dict):
        return {k: reorder(v[k]) for k in reversed(list(v))}
    if isinstance(v, list):
        return [reorder(x) for x in v]
    return v


def reword(p):
    """The same profile with every member the projection removes changed."""
    v = copy.deepcopy(p)
    v["identity"]["title"] = "Holdings, by portfolio"
    v["identity"]["summary"] = "What each portfolio held on a date."
    v["identity"]["owner"] = "Quant FI"
    v["revision"] = 8
    v["usage"]["when"] = "Positions are what you need."
    v["usage"]["examples"].append({"title": "Another", "interaction": "fetch", "request": {"portfolios": ["EM"]}})
    v["evolution"]["changelog"].append({"date": "2026-10-30", "change": "Reworded."})
    for e in v["errors"]:
        e["description"] += " Check the code."
        e["next"] = "Ask the owner."
    req = v["schemas"]["request"]
    req["title"] = "Request"
    req["description"] = "Reworded."
    req["$comment"] = "Reworded."
    req["properties"]["portfolios"]["examples"] = [["EM-RATES"]]
    req["properties"]["portfolios"]["items"]["description"] = "Reworded."
    req["properties"]["columns"]["$comment"] = "Reworded."
    for e in req["properties"]["asset_class"]["x-atlas-enum"]:
        e["description"] = "Reworded."
    row = v["schemas"]["result"]["items"]
    row["title"] = "A row"
    row["properties"]["title"]["description"] = "Reworded: a property named title is kept, its description is not."
    row["properties"]["duration"]["x-atlas-deprecated"]["reason"] = "Reworded."
    for e in row["properties"]["status"]["x-atlas-enum"]:
        e["description"] = "Reworded."
    return v


def nested_prose(p):
    """Prose at every schema position StripSchema walks (§9.4.1 step 8)."""
    v = copy.deepcopy(p)
    s = v["schemas"]["result"]
    s["$defs"] = {"note": {"type": "string", "description": "A note.", "title": "Note", "examples": ["x"]}}
    s["items"]["properties"]["tags"] = {
        "type": "array", "description": "Tags.", "items": {"type": "string", "description": "A tag.", "title": "Tag"},
        "prefixItems": [{"type": "string", "$comment": "the first tag"}],
    }
    s["items"]["properties"]["extra"] = {
        "type": "object", "description": "A map.",
        "additionalProperties": {"type": "string", "description": "A value.", "examples": ["v"]},
    }
    s["items"]["properties"]["either"] = {
        "description": "One of two.",
        "anyOf": [{"type": "string", "description": "As text."}, {"type": "number", "title": "As a number"}],
    }
    return v


def no_prose(v):
    """The same as nested_prose with its prose removed by hand."""
    v = nested_prose(v)
    s = v["schemas"]["result"]
    s["$defs"] = {"note": {"type": "string"}}
    s["items"]["properties"]["tags"] = {"type": "array", "items": {"type": "string"},
                                       "prefixItems": [{"type": "string"}]}
    s["items"]["properties"]["extra"] = {"type": "object", "additionalProperties": {"type": "string"}}
    s["items"]["properties"]["either"] = {"anyOf": [{"type": "string"}, {"type": "number"}]}
    return v


def case(name, profile, note):
    canonical = fingerprint.canonical(profile)
    assert canonical == rfc8785.dumps(fingerprint.projection(profile)), name
    digest = hashlib.sha256(canonical).hexdigest()
    return {
        "name": name,
        "note": note,
        "profile": profile,
        "expected": {"canonical": canonical.decode("utf-8"), "sha256": digest, "fingerprint": digest[:16]},
    }


def fingerprint_cases():
    data = fingerprint.with_fingerprint(profiles.holdings())
    added = copy.deepcopy(data)
    added["schemas"]["result"]["items"]["properties"]["spread"] = {
        "type": ["number", "null"], "description": "Spread over the swap curve.", "x-atlas-logical": "Bp"}
    quoted = copy.deepcopy(data)
    quoted["meaning"]["source"] = 'Harbour\'s "core" view ' + chr(0x2014) + "\tZ" + chr(0xFC) + "rich"
    controls = copy.deepcopy(data)
    controls["meaning"]["note"] = "".join(chr(c) for c in (0x00, 0x08, 0x0C, 0x0A, 0x0D, 0x1F, 0x7F, 0x2028)) + BS + "/"
    utf16 = copy.deepcopy(data)
    utf16["meaning"]["by"] = {chr(0x1F600): 1, chr(0xFB01): 2, "z": 3, chr(0xE9): 4, "Z": 5, "": 6}
    numbers = copy.deepcopy(data)
    numbers["schemas"]["request"]["properties"]["tolerance"] = {
        "type": "number", "description": "A tolerance.", "x-atlas-logical": "Fraction",
        "minimum": 1e-7, "maximum": 1e21, "default": 0.000001,
        "enum": [0, 1, 1.5, 100, 1e20, 1e21, 1e-6, 1e-7, 123456789.125, 9007199254740991, -0.0, 5e-324],
    }
    cases = [
        case("complete", data, "A complete data profile, as served (with its fingerprint member)."),
        case("prose-changed-and-reordered", reorder(reword(data)),
             "The complete profile with every removed member changed and every object's members reordered: the "
             "same fingerprint."),
        case("column-added", added, "A result column added: a new fingerprint."),
        case("non-ascii-quote-tab", quoted,
             "Non-ASCII text, a quote and a tab in a kept value, escaped only as RFC 8785 requires."),
        case("control-characters", controls,
             "Every control character class in a kept value: the short escapes, \\u00xx for the rest, and DEL, "
             "U+2028 and / written as themselves."),
        case("utf16-member-order", utf16,
             "Member names sorted by their UTF-16 code units: U+1F600 (a surrogate pair, D83D) sorts before U+FB01, "
             "which code-point order would reverse."),
        case("numbers", numbers, "Numbers in ECMAScript's shortest form: 1e+21, 1e-7, 0.000001, 100, -0 as 0, "
                                 "5e-324."),
        case("prose-at-every-schema-position", nested_prose(data),
             "Prose at every position StripSchema walks ($defs, items, prefixItems, additionalProperties, anyOf): "
             "removed, so the fingerprint equals the next case's."),
        case("the-same-without-that-prose", no_prose(data), "The previous case with that prose removed by hand."),
    ]
    assert cases[0]["expected"]["fingerprint"] == cases[1]["expected"]["fingerprint"]
    assert cases[0]["expected"]["fingerprint"] != cases[2]["expected"]["fingerprint"]
    assert cases[7]["expected"]["fingerprint"] == cases[8]["expected"]["fingerprint"]
    assert '"title":{"type":["string","null"]}' in cases[0]["expected"]["canonical"], "a property named title is kept"
    return cases


def build():
    out = {}
    doc = {
        "description": "§9.4.4: profiles with the canonical form of their projection (RFC 8785), its SHA-256 "
                       "(lowercase hexadecimal) and the fingerprint (its first 16 characters). Written by tools/spec "
                       "with its own RFC 8785 and checked against the rfc8785 package; never from an "
                       "implementation's output.",
        "cases": fingerprint_cases(),
    }
    out["vectors/fingerprints.json"] = dumps(doc)
    for family, p in profiles.complete_profiles().items():
        served = fingerprint.with_fingerprint(p)
        doc = {
            "description": f"A complete, agent-ready {family} profile (§3.1.3, §3.1.4a), as the gateway serves it: "
                           "valid against profile.schema.json, each of its schemas valid against "
                           "atlas-schema.schema.json in its role (a request schema closed, every other open), with "
                           "the fingerprint of §9.4.",
            "cases": [{
                "name": fingerprint.exact_id(served),
                "profile": served,
                "expected": {"fingerprint": served["fingerprint"], "exact": fingerprint.exact_id(served),
                             "valid": True, "agent_ready": True},
            }],
        }
        out[f"vectors/profiles/{family}.json"] = dumps(doc)
    return out
