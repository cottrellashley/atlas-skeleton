"""§9.2.5 and the Atlas vocabulary of §9.2.3, from the spec's examples."""

from atlasref import iregexp, profiles, validator

REQUEST = profiles.holdings()["schemas"]["request"]


def test_the_data_examples_request_is_valid():
    for ex in profiles.holdings()["usage"]["examples"]:
        assert validator.validate(REQUEST, ex["request"]) == []


def test_an_invalid_request_lists_every_failure_and_the_first_is_the_target():
    errors = validator.validate(REQUEST, {"portfolios": [], "zzz": 1, "as_of": "2026-02-30"})
    assert errors == [("/as_of", "format"), ("/portfolios", "minItems"), ("/zzz", "additionalProperties")]
    assert errors[0][0] == "/as_of"  # invalid_request's target


def test_logical_type_patterns_accept_real_identifiers():
    for logical, value in [("Isin", "US0378331005"), ("Cusip", "037833100"), ("Sedol", "2046251"),
                           ("Figi", "BBG000BLNNH6"), ("Lei", "HWUPKR0MPOU8FGXBT394"), ("Currency", "USD"),
                           ("Country", "GB"), ("Tenor", "10Y")]:
        assert validator.validate({"type": "string", "x-atlas-logical": logical}, value) == [], logical


def test_money_is_an_amount_and_a_currency():
    money = {"type": "object", "x-atlas-logical": "Money"}
    assert validator.validate(money, {"amount": "1250.500000", "currency": "USD"}) == []
    assert validator.validate(money, {"amount": "1250.5"}) == [("/currency", "required")]


def test_i_regexp_dot_and_anchors():
    assert iregexp.matches("^a.c$", "abc") and not iregexp.matches("^a.c$", "a\nc")
    assert iregexp.matches("^$5$", "$5")  # $ inside R is a character
