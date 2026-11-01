"""§9.2.1's rules and §3.1.4a's agent-readiness check."""

import copy

from atlasref import profilecheck, profiles


def test_the_complete_profiles_are_in_the_profile_and_ready_for_agents():
    for family, p in profiles.complete_profiles().items():
        assert profilecheck.lint_profile(p) == [], family
        assert profilecheck.agent_readiness(p) == [], family


def test_a_request_enum_never_has_unknown_and_a_result_enum_always_does():
    req = profiles.root({"type": "object", "additionalProperties": False,
                         "properties": {"s": {"type": "string", "enum": ["a", "UNKNOWN"]}}})
    assert profilecheck.check_schema(req, "request") == [("/properties/s/enum", "enum")]
    res = profiles.root({"type": "object", "properties": {"s": {"type": "string", "enum": ["a"],
                                                                "x-atlas-enum": [{"value": "a"}]}}})
    assert profilecheck.check_schema(res, "other") == [("/properties/s/enum", "enum")]


def test_excluded_keywords_are_refused():
    for kw in ("allOf", "not", "if", "patternProperties", "contains", "unevaluatedProperties", "$dynamicRef"):
        s = profiles.root({"type": "object", kw: {}})
        assert (("/" + kw), kw) in profilecheck.check_schema(s), kw


def test_the_readiness_check_names_what_is_missing():
    p = copy.deepcopy(profiles.holdings())
    p["identity"]["summary"] = "Positions. In portfolios."
    del p["schemas"]["result"]["items"]["properties"]["weight"]["description"]
    del p["errors"][0]["next"]
    assert profilecheck.agent_readiness(p) == [
        ("/errors/0/next", "next"), ("/identity/summary", "summary"),
        ("/schemas/result/items/properties/weight", "description")]
