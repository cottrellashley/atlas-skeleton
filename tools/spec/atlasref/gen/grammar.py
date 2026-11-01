"""references/vectors/grammar.json (AP-CONV-002): strings each grammar of
§9.1.3–§9.1.5 accepts and rejects, with the reason for each rejection.

Every expectation below is written by hand from the text; `build` asserts that
the matchers of grammar.py agree, and `check` that the ABNF of grammar.abnf
does wherever the reason is the ABNF itself.
"""

from .. import grammar
from ..jsonio import dumps

PATH = "vectors/grammar.json"
Y, N = True, False

CAP128 = "a" * 61 + "." + "b" * 66  # 128 characters
CAP129 = "a" * 61 + "." + "b" * 67
ADDR160 = "a" * 61 + "." + "b" * 66 + "@v1:" + "c" * 28  # 160
ADDR161 = ADDR160 + "c"

# (rule, input, accepted, note, question)
CASES = [
    # segment
    ("segment", "a", Y, "one letter", None),
    ("segment", "fetch", Y, None, None),
    ("segment", "reference-values", Y, "a hyphen between letters", None),
    ("segment", "a1-b2", Y, "digits after the first letter", None),
    ("segment", "x9", Y, None, None),
    ("segment", "", N, "empty", None),
    ("segment", "1a", N, "a digit first", None),
    ("segment", "-a", N, "a hyphen first", None),
    ("segment", "a-", N, "ends with - (§9.1.3)", None),
    ("segment", "a--b", N, "contains -- (§9.1.3)", None),
    ("segment", "A", N, "upper case: every grammar is case-sensitive", None),
    ("segment", "aB", N, "upper case", None),
    ("segment", "a_b", N, "_ is not in a segment", None),
    ("segment", "a.b", N, ". separates segments", None),
    ("segment", "caf" + chr(0xE9), N, "non-ASCII letter", None),
    ("segment", "a ", N, "trailing space", None),
    # capability-id
    ("capability-id", "portfolio.holdings", Y, None, None),
    ("capability-id", "atlas.whoami", Y, "a built-in's domain is atlas", None),
    ("capability-id", "atlas.catalog.changed", Y, "three segments", None),
    ("capability-id", "portfolio.reference-values", Y, None, None),
    ("capability-id", "a.b", Y, "the shortest", None),
    ("capability-id", CAP128, Y, "128 characters", None),
    ("capability-id", CAP129, N, "129 characters: at most 128 (§9.1.3)", None),
    ("capability-id", "portfolio", N, "one segment", None),
    ("capability-id", "portfolio.", N, "an empty segment", None),
    ("capability-id", ".holdings", N, "an empty first segment", None),
    ("capability-id", "a..b", N, "an empty segment", None),
    ("capability-id", "portfolio.holdings-", N, "a segment ends with -", None),
    ("capability-id", "portfolio.hold--ings", N, "a segment contains --", None),
    ("capability-id", "Portfolio.holdings", N, "upper case", None),
    ("capability-id", "portfolio.1holdings", N, "a segment starts with a digit", None),
    ("capability-id", "portfolio.holdings@v1", N, "a pinned-id is not a capability-id", None),
    # domain, interaction
    ("domain", "portfolio", Y, None, None),
    ("domain", "model-portfolio", Y, None, None),
    ("domain", "portfolio.holdings", N, "a domain is one segment", None),
    ("interaction", "fetch", Y, None, None),
    ("interaction", "alias-value", Y, None, None),
    ("interaction", "Fetch", N, "upper case", None),
    ("interaction", "alias_value", N, "_ is not in a segment", None),
    # version, nz-digits
    ("version", "v1", Y, None, None),
    ("version", "v12", Y, None, None),
    ("version", "v100", Y, None, None),
    ("version", "v0", N, "zero", None),
    ("version", "v01", N, "a leading zero", None),
    ("version", "V1", N, "upper case V: %s\"v\"", None),
    ("version", "1", N, "no v", None),
    ("version", "v", N, "no digits", None),
    ("version", "v1.0", N, "not semver", None),
    ("version", "v" + chr(0x0661), N, "a non-ASCII digit", None),
    ("nz-digits", "7", Y, None, None),
    ("nz-digits", "10", Y, None, None),
    ("nz-digits", "0", N, "zero", None),
    ("nz-digits", "07", N, "a leading zero", None),
    # fingerprint
    ("fingerprint", "3f9a1c0b7d2e4a65", Y, None, None),
    ("fingerprint", "0000000000000000", Y, None, None),
    ("fingerprint", "3F9A1C0B7D2E4A65", N, "upper-case hexadecimal", None),
    ("fingerprint", "3f9a1c0b7d2e4a6", N, "15 characters", None),
    ("fingerprint", "3f9a1c0b7d2e4a650", N, "17 characters", None),
    ("fingerprint", "3f9a1c0b7d2e4a6g", N, "not hexadecimal", None),
    # pinned-id, exact-id
    ("pinned-id", "portfolio.holdings@v1", Y, None, None),
    ("pinned-id", "atlas.whoami@v1", Y, None, None),
    ("pinned-id", "portfolio.holdings@v0", N, "version zero", None),
    ("pinned-id", "portfolio.holdings", N, "no version", None),
    ("pinned-id", "portfolio.holdings@1", N, "no v", None),
    ("pinned-id", "portfolio.holdings @v1", N, "a space", None),
    ("pinned-id", CAP128 + "@v1", Y, "a 128-character capability id", None),
    ("pinned-id", CAP129 + "@v1", N, "a 129-character capability id", None),
    ("exact-id", "portfolio.holdings@v1#3f9a1c0b7d2e4a65", Y, None, None),
    ("exact-id", "portfolio.holdings#3f9a1c0b7d2e4a65", N, "no version", None),
    ("exact-id", "portfolio.holdings@v1#3F9A1C0B7D2E4A65", N, "upper-case fingerprint", None),
    ("exact-id", "portfolio.holdings@v1#", N, "no fingerprint", None),
    ("exact-id", "portfolio.holdings@v1#A", N, "the spec's own shorthand #A is not an exact-id", None),
    # address
    ("address", "portfolio.holdings@v1:fetch", Y, None, None),
    ("address", "portfolio.holdings:fetch", Y, "a bare id: allowed at a terminal (§9.7.1)", None),
    ("address", "atlas.whoami:fetch", Y, None, None),
    ("address", "astronomer.deployment@v1:restart", Y, "an action's own verb", None),
    ("address", ADDR160, Y, "160 characters", None),
    ("address", ADDR161, N, "161 characters: at most 160 (§9.1.3)", None),
    ("address", "portfolio.holdings@v1#3f9a1c0b7d2e4a65:fetch", N, "an exact-id is not addressed", None),
    ("address", "portfolio.holdings@v1", N, "no interaction", None),
    ("address", "portfolio.holdings@v1:", N, "an empty interaction", None),
    ("address", "portfolio.holdings@v1:Fetch", N, "upper case", None),
    ("address", "portfolio.holdings@v1::fetch", N, "two colons", None),
    ("address", "/v1/data/portfolio.holdings@v1:fetch", N, "a binding's path is not an address", None),
    # protocol-ver
    ("protocol-ver", "atlas/1.0", Y, None, None),
    ("protocol-ver", "atlas/1.12", Y, None, None),
    ("protocol-ver", "atlas/10.0", Y, None, None),
    ("protocol-ver", "atlas/01.00", Y, "1*DIGIT allows leading zeros", "Q-GRAM-2"),
    ("protocol-ver", "Atlas/1.0", N, "upper case: %s\"atlas/\"", None),
    ("protocol-ver", "atlas/1", N, "no minor", None),
    ("protocol-ver", "atlas/1.0.0", N, "three numbers", None),
    ("protocol-ver", "atlas/v1.0", N, "a v", None),
    ("protocol-ver", "atlas 1.0", N, "a space", None),
    # property-name, problem-code
    ("property-name", "market_value", Y, None, None),
    ("property-name", "as_of", Y, None, None),
    ("property-name", "a", Y, None, None),
    ("property-name", "a__b", Y, "the grammar allows __", None),
    ("property-name", "oas_", Y, "the grammar allows a trailing _", None),
    ("property-name", "_a", N, "_ first", None),
    ("property-name", "Market_value", N, "upper case", None),
    ("property-name", "market-value", N, "- is not in a property name", None),
    ("property-name", "1st", N, "a digit first", None),
    ("problem-code", "not_found", Y, None, None),
    ("problem-code", "limit_exceeded", Y, None, None),
    ("problem-code", "warehouse_suspended", Y, "a capability's own code", None),
    ("problem-code", "NotFound", N, "upper case", None),
    ("problem-code", "not-found", N, "- is not in a code", None),
    # date
    ("date", "2026-10-31", Y, None, None),
    ("date", "2024-02-29", Y, "a leap day", None),
    ("date", "2000-02-29", Y, "a leap day in a year divisible by 400", None),
    ("date", "1970-01-01", Y, None, None),
    ("date", "0001-01-01", Y, None, None),
    ("date", "9999-12-31", Y, None, None),
    ("date", "2023-02-29", N, "not a real date: 2023 is not a leap year", None),
    ("date", "1900-02-29", N, "not a real date: 1900 is not a leap year", None),
    ("date", "2026-04-31", N, "not a real date", None),
    ("date", "2026-13-01", N, "month 13", None),
    ("date", "2026-00-10", N, "month 0", None),
    ("date", "2026-10-00", N, "day 0", None),
    ("date", "2026-1-31", N, "a one-digit month", None),
    ("date", "20261031", N, "no hyphens", None),
    ("date", "2026-10-31Z", N, "a date has no zone", None),
    ("date", "+2026-10-31", N, "a sign", None),
    ("date", "2026-10-3" + chr(0x0661), N, "a non-ASCII digit", None),
    # date-time
    ("date-time", "2026-10-30T14:02:11Z", Y, None, None),
    ("date-time", "2026-10-30T14:02:11.5Z", Y, "one fractional digit", None),
    ("date-time", "2026-10-30T14:02:11.123456789Z", Y, "nine fractional digits", None),
    ("date-time", "2026-10-30T14:02:11+01:00", Y, "an offset (the gateway sends Z)", None),
    ("date-time", "2026-10-30T14:02:11-05:30", Y, None, None),
    ("date-time", "2026-10-30T14:02:11-00:00", Y, "RFC 3339's unknown-offset form", None),
    ("date-time", "2026-10-30T23:59:59+23:59", Y, "the largest offset", None),
    ("date-time", "2024-02-29T00:00:00Z", Y, None, None),
    ("date-time", "2026-10-30T14:02:11.1234567891Z", N, "ten fractional digits", None),
    ("date-time", "2026-10-30T14:02:11.Z", N, "a point with no digits", None),
    ("date-time", "2026-10-30T14:02:11", N, "no offset: a date-time MUST carry one", None),
    ("date-time", "2026-10-30t14:02:11Z", N, "lower-case t: %s\"T\"", None),
    ("date-time", "2026-10-30T14:02:11z", N, "lower-case z: %s\"Z\"", None),
    ("date-time", "2026-10-30 14:02:11Z", N, "a space for T", None),
    ("date-time", "2026-10-30T23:59:60Z", N, "the leap second 60 is not allowed (§9.1.4)", None),
    ("date-time", "2026-10-30T24:00:00Z", N, "hour 24", None),
    ("date-time", "2026-10-30T14:60:00Z", N, "minute 60", None),
    ("date-time", "2026-10-30T14:02:11+24:00", N, "offset hour 24", None),
    ("date-time", "2026-10-30T14:02:11+01:60", N, "offset minute 60", None),
    ("date-time", "2026-10-30T14:02:11+0100", N, "an offset without a colon", None),
    ("date-time", "2023-02-29T00:00:00Z", N, "not a real date", None),
    ("date-time", "2026-10-30T14:02Z", N, "no seconds", None),
    # duration
    ("duration", "P1D", Y, None, None),
    ("duration", "PT2M", Y, None, None),
    ("duration", "PT30M", Y, None, None),
    ("duration", "PT1H", Y, None, None),
    ("duration", "PT30S", Y, None, None),
    ("duration", "PT0S", Y, "zero is a component", None),
    ("duration", "PT1.5S", Y, "a fraction of a second", None),
    ("duration", "PT0.000000001S", Y, "nine fractional digits", None),
    ("duration", "P7DT12H30M15.25S", Y, "every component", None),
    ("duration", "P1DT", Y, "the text's grammar allows T with no time components after a day", "Q-GRAM-3"),
    ("duration", "PT36H", Y, "hours beyond a day", None),
    ("duration", "P01D", Y, "1*DIGIT allows a leading zero", None),
    ("duration", "P", N, "P alone is not allowed (§9.1.4)", None),
    ("duration", "PT", N, "no component", None),
    ("duration", "P1W", N, "no weeks", None),
    ("duration", "P1Y", N, "no years", None),
    ("duration", "P1M", N, "no months: M is minutes, after T", None),
    ("duration", "PT1.1234567890S", N, "ten fractional digits", None),
    ("duration", "PT1.S", N, "a point with no digits", None),
    ("duration", "PT.5S", N, "no whole seconds", None),
    ("duration", "PT1.5M", N, "a fraction only on seconds", None),
    ("duration", "PT1S1M", N, "out of order", None),
    ("duration", "pt1h", N, "lower case", None),
    ("duration", "-PT1H", N, "no sign", None),
    ("duration", "P1DT2H", Y, None, None),
    # decimal
    ("decimal", "0", Y, None, None),
    ("decimal", "1", Y, None, None),
    ("decimal", "-1", Y, None, None),
    ("decimal", "1.5", Y, None, None),
    ("decimal", "-12.250", Y, "trailing zeros are digits", None),
    ("decimal", "0.000001", Y, None, None),
    ("decimal", "12345678901234567890123456789012.123456", Y, "38 digits", None),
    ("decimal", "-0", N, "-0 is not allowed (§9.1.5)", None),
    ("decimal", "-0.0", N, "negative zero (§9.1.5)", "Q-DEC-2"),
    ("decimal", "-0.000", N, "negative zero (§9.1.5)", "Q-DEC-2"),
    ("decimal", "-0.5", Y, "negative, not zero", None),
    ("decimal", "01", N, "a leading zero", None),
    ("decimal", "00.5", N, "a leading zero", None),
    ("decimal", "1.", N, "a point with no digits", None),
    ("decimal", ".5", N, "no integer part", None),
    ("decimal", "+1", N, "a plus sign", None),
    ("decimal", "1e5", N, "no exponent", None),
    ("decimal", "1,5", N, "a comma", None),
    ("decimal", " 1", N, "a space", None),
    ("decimal", chr(0xFF11), N, "a full-width digit", None),
    # integer-str
    ("integer-str", "0", Y, None, None),
    ("integer-str", "9007199254740993", Y, "beyond I-JSON's integers: why it is a string", None),
    ("integer-str", "-9223372036854775808", Y, None, None),
    ("integer-str", "-0", Y, "the text forbids -0 only for decimals", "Q-DEC-3"),
    ("integer-str", "007", N, "leading zeros", None),
    ("integer-str", "1.0", N, "a point", None),
    ("integer-str", "", N, "empty", None),
    # strings defined outside §9.1 (grammar.abnf's last section)
    ("idempotency-key", "8e03978e-40d5-43e8-bc93-6894a57f9324", Y, None, None),
    ("idempotency-key", "run_2026.10.31", Y, None, None),
    ("idempotency-key", "k" * 255, Y, "255 characters", None),
    ("idempotency-key", "k" * 256, N, "256 characters", None),
    ("idempotency-key", "", N, "empty", None),
    ("idempotency-key", "a b", N, "a space", None),
    ("idempotency-key", "a/b", N, "/ is not allowed (§9.6.3)", None),
    ("problem-type", "https://atlas.jhg.com/problems/not_found", Y, None, None),
    ("problem-type", "https://atlas.jhg.com/problems/", N, "no code", None),
    ("problem-type", "http://atlas.jhg.com/problems/not_found", N, "http", None),
    ("dataschema", "urn:atlas:profile:portfolio.holdings@v1:3f9a1c0b7d2e4a65", Y, "§9.4.3's example", None),
    ("dataschema", "urn:atlas:profile:portfolio.holdings@v1#3f9a1c0b7d2e4a65", N, "# for :", None),
    ("event-type", "com.jhg.fiquant.data.landed.v1", Y, "§2.5's example", None),
    ("event-type", "com.jhg.fiquant.atlas.catalog.changed.v1", Y, None, None),
    ("event-type", "com.jhg.fiquant.data.landed", N, "no version", None),
    ("event-type", "com.jhg.fiquant.data.landed@v1", N, "@ for .", None),
    ("resource-name", "nautilus/dashboards/d1", Y, "§9.3.4's example", "Q-GRAM-1"),
    ("resource-name", "conformance/things/t-1", Y, None, "Q-GRAM-1"),
    ("resource-name", "kb/bases/b1/documents/7f", Y, "nested collections", "Q-GRAM-1"),
    ("resource-name", "nautilus/dashboards", N, "no id", "Q-GRAM-1"),
    ("resource-name", "nautilus/dashboards/D1", N, "upper case", "Q-GRAM-1"),
    ("resource-name", "/nautilus/dashboards/d1", N, "a leading /", "Q-GRAM-1"),
    ("operation-name", "operations/op-91", Y, "§4.1's example", "Q-GRAM-1"),
    ("operation-name", "operations/", N, "no id", "Q-GRAM-1"),
    ("entity-tag", '"e8"', Y, "§2.3's example", None),
    ("entity-tag", '"3f9a1c0b7d2e4a65"', Y, "a profile's ETag", None),
    ("entity-tag", 'W/"e8"', Y, "a weak ETag", None),
    ("entity-tag", '""', Y, "empty", None),
    ("entity-tag", "e8", N, "no quotes", None),
    ("entity-tag", '"e 8"', N, "a space", None),
    ("entity-tag", '"e8', N, "unterminated", None),
]


def build():
    cases = []
    for rule, text, accepted, note, question in CASES:
        why = grammar.check(rule, text)
        if (why is None) != accepted:
            raise AssertionError(f"grammar.py disagrees with the hand-written case {rule} {text!r}: {why}")
        label = repr(text) if len(text) <= 60 else f"{len(text)} characters"
        case = {"name": f"{rule}: {label}", "rule": rule, "input": text,
                "expected": {"valid": accepted}}
        if not accepted:
            case["expected"]["why"] = why
        if note:
            case["note"] = note
        if question:
            case["question"] = question
        cases.append(case)
    names = [c["name"] for c in cases]
    assert len(names) == len(set(names)), "case names are unique"
    doc = {
        "description": "§9.1.3–§9.1.5 (AP-CONV-002): strings each grammar accepts and rejects. `valid` is the "
                       "grammar with the sentences its section adds (a segment never ends with - or holds --, the "
                       "length limits, P alone, -0, real dates and times); `why` names what rejects a string: "
                       "\"abnf\" (grammar.abnf itself), or the sentence of the text. Cases marked with a question "
                       "follow the reading recorded in QUESTIONS.md. The last rules are the strings §9 defines "
                       "outside §9.1 (grammar.abnf's last section).",
        "rules": grammar.RULES,
        "cases": cases,
    }
    return {PATH: dumps(doc)}
