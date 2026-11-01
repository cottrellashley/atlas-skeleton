"""§9.1.3–§9.1.5, from the text's own examples."""

import pytest

from atlasref import abnf, grammar

G = abnf.parse(grammar.ABNF)

EXAMPLES = [  # every example §9.1.3–§9.1.5 writes beside its rule
    ("capability-id", "portfolio.holdings"), ("capability-id", "atlas.whoami"), ("version", "v1"), ("version", "v12"),
    ("interaction", "fetch"), ("interaction", "get"), ("interaction", "restart"), ("fingerprint", "3f9a1c0b7d2e4a65"),
    ("pinned-id", "portfolio.holdings@v1"), ("exact-id", "portfolio.holdings@v1#3f9a1c0b7d2e4a65"),
    ("address", "portfolio.holdings@v1:fetch"), ("protocol-ver", "atlas/1.0"), ("problem-code", "not_found"),
    ("problem-code", "limit_exceeded"), ("property-name", "market_value"),
    # and from elsewhere in the spec
    ("capability-id", "portfolio.reference-values"), ("address", "astronomer.deployment@v1:restart"),
    ("exact-id", "portfolio.holdings@v1#bcaa5bbad97cc612"), ("duration", "PT2M"), ("duration", "PT30M"),
    ("duration", "PT10M"), ("duration", "PT1H"), ("duration", "P7D"), ("date-time", "2026-10-30T18:00:00Z"),
    ("date", "2026-10-01"), ("dataschema", "urn:atlas:profile:portfolio.holdings@v1:3f9a1c0b7d2e4a65"),
    ("event-type", "com.jhg.fiquant.data.landed.v1"), ("entity-tag", '"e8"'),
]


@pytest.mark.parametrize("rule, text", EXAMPLES)
def test_the_texts_examples(rule, text):
    assert grammar.matches(rule, text)
    assert G.matches(rule, text)


@pytest.mark.parametrize("rule, text, why", [
    ("segment", "a-", "abnf"), ("segment", "a--b", "abnf"), ("version", "V1", "abnf"),
    ("date-time", "2026-10-30T23:59:60Z", "clock: the leap second 60 is not allowed"),
    ("duration", "P", "abnf"), ("duration", "PT", "abnf"),
    ("decimal", "-0", "negative-zero: -0 is not allowed"),
    ("capability-id", "a" * 64 + "." + "b" * 64, "length: a capability id is at most 128 characters"),
])
def test_the_texts_prohibitions(rule, text, why):
    assert grammar.check(rule, text) == why


def test_dates_and_instants():
    assert grammar.parse_date_time_ns("1970-01-01T00:00:01.5+01:00") == (1 - 3600) * 10**9 + 500_000_000
    assert grammar.format_date_time_us(-1) == "1969-12-31T23:59:59.999999Z"
    assert grammar.format_date(grammar.days_from_civil(2000, 2, 29)) == "2000-02-29"
    assert grammar.parse_duration_us("P1DT2H3M4.5S") == ((24 + 2) * 3600 + 3 * 60 + 4) * 10**6 + 500_000
