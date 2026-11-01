"""§9.2.1–§9.2.4: whether a schema stays inside the Atlas Schema profile; the
profile lint of §3.3.12 step 1a; and §3.1.4a's agent-readiness check.

Each check returns violations `(pointer, rule)`: the JSON Pointer of the
offending member in the schema (or profile) and the keyword or rule it breaks.
Where the text leaves the rule open, the reading taken is recorded in
QUESTIONS.md and named in a comment (Q-PROF-n).
"""

from . import formats, grammar, iregexp, jcs
from .jsonio import is_integer, is_number, json_type
from .validator import LOGICAL_BASE, Validator, escape, sort_failures

DRAFT = "https://json-schema.org/draft/2020-12/schema"
VOCABULARY = {
    "https://json-schema.org/draft/2020-12/vocab/core": True,
    "https://json-schema.org/draft/2020-12/vocab/applicator": True,
    "https://json-schema.org/draft/2020-12/vocab/validation": True,
    "https://json-schema.org/draft/2020-12/vocab/meta-data": True,
    "https://json-schema.org/draft/2020-12/vocab/format-assertion": True,
    "https://atlas.jhg.com/vocab/2026-10": False,
}
ROOT_ONLY = ("$schema", "$vocabulary", "$id", "$defs")  # $defs root only: Q-PROF-1
CORE = ("$ref", "$comment")
APPLICATOR = ("properties", "additionalProperties", "items", "prefixItems", "anyOf", "oneOf")
VALIDATION = (
    "type", "enum", "const", "required", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum",
    "multipleOf", "minLength", "maxLength", "pattern", "minItems", "maxItems", "uniqueItems",
    "minProperties", "maxProperties",
)
META = ("title", "description", "default", "deprecated", "examples", "readOnly", "writeOnly")
ATLAS = (
    "x-atlas-logical", "x-atlas-key", "x-atlas-aliases", "x-atlas-reference", "x-atlas-deprecated",
    "x-atlas-unit", "x-atlas-enum",
)
ALLOWED = set(CORE + APPLICATOR + VALIDATION + META + ("format",) + ATLAS)
TYPES = ("null", "boolean", "object", "array", "number", "integer", "string")
UNITS = ("bp", "percent", "fraction", "years", "days", "seconds", "count", "currency_unit")
# §9.2.3: key identifiers "that may be missing" (Q-PROF-6).
MISSABLE_KEYS = ("Isin", "Cusip", "Sedol", "Figi", "Lei")
# Q-PROF-2: what may stand beside $ref.
REF_SIBLINGS = set(META) | {"$comment", "x-atlas-key", "x-atlas-aliases", "x-atlas-reference", "x-atlas-deprecated"}
MAX_DEPTH = 32
MAX_BYTES = 1024 * 1024


def _types(s):
    t = s.get("type")
    if isinstance(t, str):
        return [t]
    if isinstance(t, list):
        return [x for x in t if isinstance(x, str)]
    return []


def _base_type(s):
    ts = [t for t in _types(s) if t != "null"]
    return ts[0] if ts else None


def _nullable(s):
    return "null" in _types(s)


class _Checker:
    def __init__(self, root, role, free_form):
        self.root, self.role, self.free_form = root, role, set(free_form)
        self.out = []

    def bad(self, ptr, rule):
        self.out.append((ptr, rule))

    def run(self):
        root = self.root
        if not isinstance(root, dict):
            self.bad("", "type")
            return
        if root.get("$schema") != DRAFT:
            self.bad("/$schema", "$schema")
        if root.get("$vocabulary") != VOCABULARY or not all(
            isinstance(root.get("$vocabulary", {}).get(k), bool) for k in VOCABULARY
        ):
            self.bad("/$vocabulary", "$vocabulary")
        if "$id" in root and not (isinstance(root["$id"], str) and formats.check("uri", root["$id"])):
            self.bad("/$id", "$id")
        defs = root.get("$defs", {})
        if not isinstance(defs, dict):
            self.bad("/$defs", "$defs")
            defs = {}
        for name, sub in defs.items():
            self.schema(sub, "/$defs/" + escape(name), 2, "$defs", None)
        self.cycles(defs)
        if len(jcs.dumps(root)) > MAX_BYTES:
            self.bad("", "size")
        self.schema(root, "", 1, "root", None)

    def cycles(self, defs):
        graph = {name: set() for name in defs}

        def refs(node, acc):
            if isinstance(node, dict):
                r = node.get("$ref")
                if isinstance(r, str) and r.startswith("#/$defs/"):
                    acc.add(r[len("#/$defs/") :].replace("~1", "/").replace("~0", "~"))
                for k, v in node.items():
                    if k not in ("enum", "const", "default", "examples"):
                        refs(v, acc)
            elif isinstance(node, list):
                for v in node:
                    refs(v, acc)

        for name, sub in defs.items():
            refs(sub, graph[name])
        state = {}

        def visit(n):
            state[n] = 1
            for m in graph.get(n, ()):
                if state.get(m) == 1 or (state.get(m) is None and m in graph and visit(m)):
                    return True
            state[n] = 2
            return False

        for name in graph:
            state.clear()
            if visit(name):
                self.bad("/$defs/" + escape(name), "$ref")  # Q-PROF-1: no recursion

    def schema(self, s, ptr, depth, position, parent):
        if isinstance(s, bool):
            if position not in ("items", "additionalProperties"):
                self.bad(ptr, "boolean-schema")  # Q-PROF-3
            return
        if not isinstance(s, dict):
            self.bad(ptr, "type")
            return
        if depth > MAX_DEPTH:
            self.bad(ptr, "depth")
            return
        for k in s:
            if k in ROOT_ONLY:
                if position != "root":
                    self.bad(ptr + "/" + escape(k), k)
            elif k not in ALLOWED:
                self.bad(ptr + "/" + escape(k), k)
        if "$ref" in s:
            self.ref(s, ptr)
        if "type" in s:
            t = s["type"]
            ok = (isinstance(t, str) and t in TYPES) or (
                isinstance(t, list) and len(t) == 2 and t[1] == "null" and t[0] in TYPES and t[0] != "null"
            )
            if not ok:
                self.bad(ptr + "/type", "type")
        elif not any(k in s for k in ("$ref", "anyOf", "oneOf")):
            self.bad(ptr + "/type", "type")  # Q-PROF-4: every schema has a type
        base = _base_type(s)
        logical = s.get("x-atlas-logical")
        self.objects(s, ptr, depth, base, logical)
        self.arrays(s, ptr, depth)
        self.combinators(s, ptr, depth)
        self.values(s, ptr, base)
        self.atlas(s, ptr, base, logical, position, parent)

    def ref(self, s, ptr):
        r = s["$ref"]
        defs = self.root.get("$defs", {}) if isinstance(self.root.get("$defs"), dict) else {}
        name = r[len("#/$defs/") :].replace("~1", "/").replace("~0", "~") if isinstance(r, str) and r.startswith("#/$defs/") else None
        if name is None or "/" in r[len("#/$defs/") :] or name not in defs:
            self.bad(ptr + "/$ref", "$ref")
        for k in s:
            if k not in REF_SIBLINGS and k != "$ref":
                self.bad(ptr + "/" + escape(k), "$ref")

    def objects(self, s, ptr, depth, base, logical):
        props = s.get("properties")
        if props is not None:
            if not isinstance(props, dict):
                self.bad(ptr + "/properties", "properties")
                props = {}
            for name, sub in props.items():
                if not grammar.matches("property-name", name):
                    self.bad(ptr + "/properties/" + escape(name), "properties")
                self.schema(sub, ptr + "/properties/" + escape(name), depth + 1, "property", s)
        if "required" in s:
            req = s["required"]
            if not (isinstance(req, list) and all(isinstance(n, str) for n in req) and len(set(req)) == len(req)):
                self.bad(ptr + "/required", "required")
            else:
                implied = ("amount", "currency") if logical == "Money" else ()
                for i, n in enumerate(req):
                    if n not in (props or {}) and n not in implied:
                        self.bad(ptr + "/required/" + str(i), "required")  # Q-PROF-5
        if "additionalProperties" in s:
            ap = s["additionalProperties"]
            if isinstance(ap, dict):
                self.schema(ap, ptr + "/additionalProperties", depth + 1, "additionalProperties", s)
            elif not isinstance(ap, bool):
                self.bad(ptr + "/additionalProperties", "additionalProperties")
        if base == "object":
            ap = s.get("additionalProperties")
            is_map = props is None and logical != "Money" and isinstance(ap, dict)  # Q-PROF-7: maps
            if props is None and ap is None and ("oneOf" in s or "anyOf" in s):
                return  # the branches are the objects, each checked itself
            if self.role == "request" and not is_map and ap is not False and ptr not in self.free_form:
                self.bad(ptr + "/additionalProperties", "additionalProperties")
            if self.role != "request" and not is_map and ap not in (None, True):
                self.bad(ptr + "/additionalProperties", "additionalProperties")
        for k in ("minProperties", "maxProperties"):
            if k in s and not (is_integer(s[k]) and s[k] >= 0):
                self.bad(ptr + "/" + k, k)

    def arrays(self, s, ptr, depth):
        if "items" in s:
            it = s["items"]
            if isinstance(it, (dict, bool)):
                self.schema(it, ptr + "/items", depth + 1, "items", s)
            else:
                self.bad(ptr + "/items", "items")
        if "prefixItems" in s:
            pi = s["prefixItems"]
            if not (isinstance(pi, list) and pi):
                self.bad(ptr + "/prefixItems", "prefixItems")
            else:
                for i, sub in enumerate(pi):
                    self.schema(sub, f"{ptr}/prefixItems/{i}", depth + 1, "prefixItems", s)
        for k in ("minItems", "maxItems"):
            if k in s and not (is_integer(s[k]) and s[k] >= 0):
                self.bad(ptr + "/" + k, k)
        if "uniqueItems" in s and not isinstance(s["uniqueItems"], bool):
            self.bad(ptr + "/uniqueItems", "uniqueItems")

    def combinators(self, s, ptr, depth):
        for k in ("anyOf", "oneOf"):
            if k not in s:
                continue
            branches = s[k]
            if not (isinstance(branches, list) and branches):
                self.bad(ptr + "/" + k, k)
                continue
            for i, sub in enumerate(branches):
                self.schema(sub, f"{ptr}/{k}/{i}", depth + 1, k, s)
            if k == "oneOf" and not self.discriminated(branches):
                self.bad(ptr + "/oneOf", "oneOf")

    @staticmethod
    def discriminated(branches):
        """§9.2.1: oneOf only with a const discriminator in every branch: one
        property, required in every branch, with a distinct const in each."""
        if not all(isinstance(b, dict) and isinstance(b.get("properties"), dict) for b in branches):
            return False
        names = set.intersection(*(set(b["properties"]) for b in branches))
        for n in sorted(names):
            consts = []
            for b in branches:
                p = b["properties"][n]
                if not (isinstance(p, dict) and "const" in p and n in b.get("required", [])):
                    break
                consts.append(jcs.canonical(p["const"]))
            else:
                if len(set(consts)) == len(consts):
                    return True
        return False

    def values(self, s, ptr, base):
        if "enum" in s:
            e = s["enum"]
            if not (isinstance(e, list) and e) or len({jcs.canonical(x) for x in e}) != len(e):
                self.bad(ptr + "/enum", "enum")
            elif base == "string":
                if self.role == "request" and "UNKNOWN" in e:
                    self.bad(ptr + "/enum", "enum")
                if self.role != "request" and "UNKNOWN" not in e:
                    self.bad(ptr + "/enum", "enum")
                if self.role != "request" and "x-atlas-enum" not in s:
                    self.bad(ptr + "/x-atlas-enum", "x-atlas-enum")
        for k in ("minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum"):
            if k in s and not is_number(s[k]):
                self.bad(ptr + "/" + k, k)
        if "multipleOf" in s:  # Q-PROF-8: integers only, on integers
            m = s["multipleOf"]
            if not (is_integer(m) and m > 0 and base == "integer"):
                self.bad(ptr + "/multipleOf", "multipleOf")
        for k in ("minLength", "maxLength"):
            if k in s and not (is_integer(s[k]) and s[k] >= 0):
                self.bad(ptr + "/" + k, k)
        if "pattern" in s:
            try:
                iregexp.compile_pattern(s["pattern"])
            except (iregexp.InvalidIRegexp, TypeError):
                self.bad(ptr + "/pattern", "pattern")
        if "format" in s and (s["format"] not in formats.FORMATS or base not in ("string", None)):
            self.bad(ptr + "/format", "format")
        for k in ("title", "description", "$comment"):
            if k in s and not isinstance(s[k], str):
                self.bad(ptr + "/" + k, k)
        for k in ("deprecated", "readOnly", "writeOnly"):
            if k in s and not isinstance(s[k], bool):
                self.bad(ptr + "/" + k, k)
        if "examples" in s and not isinstance(s["examples"], list):
            self.bad(ptr + "/examples", "examples")
        if "default" in s and not Validator(self.root).valid(s, s["default"]):
            self.bad(ptr + "/default", "default")  # Q-PROF-5

    def atlas(self, s, ptr, base, logical, position, parent):
        if "x-atlas-logical" in s:
            lb = LOGICAL_BASE.get(logical) if isinstance(logical, str) else None
            if isinstance(logical, str) and formats.parse_decimal_logical(logical):
                lb = "string"
                if "format" in s and s["format"] != "decimal":
                    self.bad(ptr + "/format", "format")
            if lb is None or lb != base:
                self.bad(ptr + "/x-atlas-logical", "x-atlas-logical")
            if logical == "Money" and isinstance(s.get("properties"), dict):
                for name in s["properties"]:
                    if name not in ("amount", "currency"):
                        self.bad(ptr + "/properties/" + escape(name), "x-atlas-logical")  # Q-PROF-13
        if "x-atlas-key" in s:
            if s["x-atlas-key"] is not True or self.role == "request" or position != "property":
                self.bad(ptr + "/x-atlas-key", "x-atlas-key")
            elif logical not in MISSABLE_KEYS:
                name = ptr.rsplit("/", 1)[-1].replace("~1", "/").replace("~0", "~")
                if _nullable(s) or name not in (parent or {}).get("required", []):
                    self.bad(ptr + "/x-atlas-key", "x-atlas-key")
        if "x-atlas-aliases" in s:
            a = s["x-atlas-aliases"]
            if not (isinstance(a, list) and a and len(set(map(str, a))) == len(a)
                    and all(isinstance(x, str) and grammar.matches("property-name", x) for x in a)):
                self.bad(ptr + "/x-atlas-aliases", "x-atlas-aliases")
        if "x-atlas-reference" in s and not (
            isinstance(s["x-atlas-reference"], str) and grammar.matches("pinned-id", s["x-atlas-reference"])
        ):
            self.bad(ptr + "/x-atlas-reference", "x-atlas-reference")
        if "x-atlas-deprecated" in s and not deprecation_ok(s["x-atlas-deprecated"]):
            self.bad(ptr + "/x-atlas-deprecated", "x-atlas-deprecated")
        if "x-atlas-unit" in s and (s["x-atlas-unit"] not in UNITS or base not in ("number", "integer")):
            self.bad(ptr + "/x-atlas-unit", "x-atlas-unit")
        if "x-atlas-enum" in s:
            xe = s["x-atlas-enum"]
            values = [x for x in s.get("enum", []) if isinstance(x, str)] if isinstance(s.get("enum"), list) else None
            ok = base == "string" and values is not None and isinstance(xe, list)
            if ok:
                seen = []
                for entry in xe:
                    if not (isinstance(entry, dict) and isinstance(entry.get("value"), str)):
                        ok = False
                        break
                    for k in entry:
                        if k not in ("value", "description", "aliases", "deprecated"):
                            ok = False
                    if "description" in entry and not isinstance(entry["description"], str):
                        ok = False
                    if "aliases" in entry and not (
                        isinstance(entry["aliases"], list) and all(isinstance(x, str) for x in entry["aliases"])
                    ):
                        ok = False
                    if "deprecated" in entry and not deprecation_ok(entry["deprecated"], value_level=True):
                        ok = False
                    seen.append(entry["value"])
                ok = ok and len(seen) == len(set(seen)) and set(seen) == set(values)  # Q-PROF-10
            if not ok:
                self.bad(ptr + "/x-atlas-enum", "x-atlas-enum")


def deprecation_ok(d, value_level=False):
    """§9.2.3: { reason, since, sunset, replaced_by }: strings; since and sunset
    dates; replaced_by a property name (an enum value, on a value) or null. All
    four are required (Q-PROF-11); null means not deprecated."""
    if d is None:
        return True
    if not isinstance(d, dict) or set(d) != {"reason", "since", "sunset", "replaced_by"}:
        return False
    if not isinstance(d["reason"], str):
        return False
    if not (isinstance(d["since"], str) and grammar.matches("date", d["since"])):
        return False
    if not (isinstance(d["sunset"], str) and grammar.matches("date", d["sunset"])):
        return False
    rb = d["replaced_by"]
    if rb is None:
        return True
    if not isinstance(rb, str):
        return False
    return True if value_level else grammar.matches("property-name", rb)


def check_schema(schema, role="other", free_form=()):
    """Violations of the Atlas Schema profile; `role` is "request" for a schema
    an interaction takes, else "other". `free_form` names the pointers of the
    objects a request may leave open: only the built-ins' two free-form
    members (atlas.compare's `request`, atlas.settings' `settings`, Q-PROF-9)."""
    c = _Checker(schema, role, free_form)
    c.run()
    return sort_failures(c.out)


# --- §3.1.4a, the agent-readiness check -------------------------------------


def _one_sentence(text):
    if not isinstance(text, str) or not text or len(text) > 200 or text[-1] not in ".!?":
        return False
    body = text[:-1]
    return not any(body.find(p) >= 0 for p in (". ", "! ", "? "))


def _describe_walk(s, ptr, out):
    """Step 2: every property at every depth has a description; every number
    or integer property has x-atlas-logical or x-atlas-unit."""
    if not isinstance(s, dict):
        return
    for name, sub in (s.get("properties") or {}).items():
        p = ptr + "/properties/" + escape(name)
        if isinstance(sub, dict):
            if not isinstance(sub.get("description"), str) or not sub["description"]:
                out.append((p, "description"))
            if _base_type(sub) in ("number", "integer") and "x-atlas-logical" not in sub and "x-atlas-unit" not in sub:
                out.append((p, "x-atlas-unit"))
        _describe_walk(sub, p, out)
    for k in ("items", "additionalProperties"):
        if isinstance(s.get(k), dict):
            _describe_walk(s[k], ptr + "/" + k, out)
    for k in ("prefixItems", "anyOf", "oneOf"):
        for i, sub in enumerate(s.get(k) or []):
            _describe_walk(sub, f"{ptr}/{k}/{i}", out)
    for name, sub in (s.get("$defs") or {}).items():
        _describe_walk(sub, ptr + "/$defs/" + escape(name), out)


def agent_readiness(profile):
    """§3.1.4a; an empty list means the profile is ready for agents."""
    out = []
    ident = profile.get("identity", {})
    if not _one_sentence(ident.get("summary")):
        out.append(("/identity/summary", "summary"))
    for name, s in (profile.get("schemas") or {}).items():
        _describe_walk(s, "/schemas/" + escape(name), out)
    usage = profile.get("usage") or {}
    if not isinstance(usage.get("when"), str) or not usage["when"]:
        out.append(("/usage/when", "when"))
    examples = usage.get("examples") or []
    good = False
    for i, ex in enumerate(examples):
        inter = (profile.get("interactions") or {}).get(ex.get("interaction"), {})
        takes = inter.get("takes")
        schema = (profile.get("schemas") or {}).get(takes)
        if schema is not None and isinstance(ex.get("request"), dict) and not Validator(schema).errors(ex["request"]):
            good = True
        else:
            out.append((f"/usage/examples/{i}", "examples"))
    if not good:
        out.append(("/usage/examples", "examples"))
    for i, e in enumerate(profile.get("errors") or []):
        for k in ("description", "next"):
            if not isinstance(e.get(k), str) or not e[k]:
                out.append((f"/errors/{i}/{k}", k))
    props = profile.get("properties", {})
    family = props.get("family")
    meaning = profile.get("meaning") or {}
    traits = {t.get("name") for t in props.get("traits", []) if isinstance(t, dict)}
    need = []
    if family == "data":
        need = ["grain", "source"]
        if any(i.get("result") == "table" for i in (profile.get("interactions") or {}).values()):
            need.append("keys")
        if "AsOf" in traits:
            need.append("as_of")
    elif family == "event":
        need = ["emitted", "subject", "ordering", "retention", "history"]
    elif family == "action":
        need = ["effects", "policy"]
    for k in need:
        if k not in meaning:
            out.append(("/meaning/" + k, k))
    return sort_failures(out)


# --- §3.3.12 step 1a, the lint ----------------------------------------------


def schema_roles(profile):
    """Each schema's role: "request" for one an interaction takes."""
    takes = {i.get("takes") for i in (profile.get("interactions") or {}).values() if isinstance(i, dict)}
    return {name: ("request" if name in takes else "other") for name in (profile.get("schemas") or {})}


def lint_profile(profile, free_form=None, envelope=None):
    """The lint's checks that a document schema cannot make: each schema in
    the Atlas Schema profile by its role; every takes/returns names a schema;
    every interaction has a deadline maximum and an https binding; a resource
    profile's record schemas declare the envelope exactly as
    resource.schema.json (`envelope`: its name, metadata, status schemas)."""
    out = []
    schemas = profile.get("schemas") or {}
    roles = schema_roles(profile)
    for name, s in schemas.items():
        for p, rule in check_schema(s, roles[name], free_form=(free_form or {}).get(name, ())):
            out.append(("/schemas/" + escape(name) + p, rule))
    for iname, inter in (profile.get("interactions") or {}).items():
        base = "/interactions/" + escape(iname)
        for k in ("takes", "returns"):
            if k in inter and inter[k] not in schemas:
                out.append((base + "/" + k, k))
        if not isinstance(inter.get("deadline"), dict) or "max" not in inter["deadline"]:
            out.append((base + "/deadline", "deadline"))
        if not any(isinstance(b, dict) and b.get("protocol") == "https" for b in inter.get("bindings", [])):
            out.append((base + "/bindings", "bindings"))
    if profile.get("properties", {}).get("family") == "resource" and envelope is not None:
        for iname, inter in (profile.get("interactions") or {}).items():
            r = inter.get("returns")
            if r in schemas and isinstance(schemas[r], dict) and "metadata" in (schemas[r].get("properties") or {}):
                props = schemas[r]["properties"]
                for member in ("name", "metadata", "status"):
                    if member == "status" and member not in props:
                        continue
                    if member not in props or _strip_prose(props[member]) != _strip_prose(envelope[member]):
                        out.append(("/schemas/" + escape(r) + "/properties/" + member, "envelope"))
    return sort_failures(out)


def _strip_prose(s):
    if isinstance(s, dict):
        return {k: _strip_prose(v) for k, v in s.items() if k not in ("title", "description", "$comment", "examples")}
    if isinstance(s, list):
        return [_strip_prose(v) for v in s]
    return s


__all__ = ["check_schema", "agent_readiness", "lint_profile", "deprecation_ok", "VOCABULARY", "DRAFT", "json_type"]
