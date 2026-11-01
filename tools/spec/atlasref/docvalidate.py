"""Validating protocol documents against references/schemas with jsonschema,
format assertion on (the formats of §9.2.2), patterns as I-Regexps, and the
documents resolving each other by $id."""

import functools

from jsonschema import Draft202012Validator, FormatChecker
from jsonschema import validators as jsv
from referencing import Registry, Resource

from . import formats, iregexp, schemas


def _format_checker():
    fc = FormatChecker(formats=())
    for f in formats.FORMATS:
        fc.checks(f)(lambda value, f=f: not isinstance(value, str) or formats.check(f, value))
    return fc


def _pattern(validator, pattern, instance, schema):
    if isinstance(instance, str):
        r = pattern[1:-1] if pattern.startswith("^") and pattern.endswith("$") else None
        try:
            ok = iregexp.compile_pattern(pattern).fullmatch(instance) is not None if r is not None else None
        except iregexp.InvalidIRegexp:
            ok = None
        if ok is None:
            import re

            ok = re.search(pattern, instance) is not None
        if not ok:
            from jsonschema.exceptions import ValidationError

            yield ValidationError(f"{instance!r} does not match {pattern!r}")


def _logical(validator, name, instance, schema):
    """§9.2.5 step 2: a logical type's constraint, as if written as keywords."""
    from .validator import logical_constraint

    implied = logical_constraint(name) if isinstance(name, str) else {}
    if implied:
        if "format" in implied and isinstance(instance, str) and implied["format"] == "decimal":
            if not formats.check("decimal", instance, name):
                from jsonschema.exceptions import ValidationError

                yield ValidationError(f"{instance!r} is not a {name}")
            implied = {k: v for k, v in implied.items() if k != "format"}
        yield from validator.descend(instance, implied)


def _is_integer(checker, instance):
    from .jsonio import is_integer

    return is_integer(instance)


_TYPES = Draft202012Validator.TYPE_CHECKER.redefine("integer", _is_integer)
AtlasDraft = jsv.extend(Draft202012Validator, {"pattern": _pattern}, type_checker=_TYPES)
ProfileDraft = jsv.extend(Draft202012Validator, {"pattern": _pattern, "x-atlas-logical": _logical},
                          type_checker=_TYPES)


def profile_valid(schema, instance):
    """jsonschema's verdict on an instance of a schema in the Atlas Schema
    profile: the differential check of validator.py."""
    v = ProfileDraft(schema, format_checker=_FC)
    return not list(v.iter_errors(instance))


_FC = _format_checker()


@functools.lru_cache(maxsize=None)
def registry():
    docs = schemas.all_documents()
    return Registry().with_resources([(d["$id"], Resource.from_contents(d)) for d in docs.values()])


@functools.lru_cache(maxsize=None)
def validator_for(name, ref=None):
    """A validator for the document `name` (or one of its $defs)."""
    docs = schemas.all_documents()
    schema = docs[name] if ref is None else {"$ref": schemas.BASE + name + "#/$defs/" + ref}
    return AtlasDraft(schema, registry=registry(), format_checker=_FC)


def errors(name, instance, ref=None):
    return [f"{'/'.join(map(str, e.absolute_path))}: {e.message}" for e in validator_for(name, ref).iter_errors(instance)]
