"""§9.6.3's example headers, and RFC 9651's own examples, read as written."""

import decimal

from atlasref import headers, sfv


def test_the_example_of_9_6_3():
    assert headers.parse("Atlas-Client", ['name="fiatlas", version="2.3.0", protocol="atlas/1.0"']) == (
        "value", {"name": "fiatlas", "version": "2.3.0", "protocol": "atlas/1.0"})
    status, f = headers.parse("Atlas-Client-Features", [
        "levels=(core streaming columnar bulk events handoff), handoff=(snowflake-arrow/1 reach-snowflake), "
        "lens=(rename alias_value convert_unit promote default drop), deliveries=(single chunked subscription "
        "operation transfer), encodings=(json ndjson arrow csv parquet)"])
    assert status == "value" and f["handoff"] == ["snowflake-arrow/1", "reach-snowflake"] and f["traits"] == []
    assert headers.parse("Atlas-Profile-Expect", ['"portfolio.holdings@v1#bcaa5bbad97cc612"']) == (
        "value", "portfolio.holdings@v1#bcaa5bbad97cc612")
    assert headers.parse("Atlas-Deadline", ["@1793318400"]) == ("value", 1793318400)
    assert headers.parse("Atlas-Limit-Remaining", ["rate=58, concurrency=3"]) == ("value", {"rate": 58, "concurrency": 3})


def test_a_header_that_does_not_parse_as_its_type_is_absent():
    assert headers.parse("Atlas-Deadline", ["1793318400"])[0] == "absent"
    assert headers.parse("Atlas-Confirm", ["12"])[0] == "absent"
    assert headers.parse("Retry-After", ["Wed, 21 Oct 2015 07:28:00 GMT"])[0] == "absent"


def test_rfc_9651_examples():
    assert sfv.serialize(sfv.parse("sugar, tea, rum", "list"), "list") == "sugar, tea, rum"
    d = sfv.parse('en="Applepie", da=:w4ZibGV0w6ZydGU=:', "dictionary")
    assert d["da"][0] == (chr(0xC6) + "blet" + chr(0xE6) + "rte").encode("utf-8")  # "aebletaerte", apple pie
    assert sfv.parse("rating=1.5, feelings=(joy sadness)", "dictionary")["rating"][0] == decimal.Decimal("1.5")
    assert sfv.parse('%"This is intended for display to %c3%bcsers."', "item")[0] == \
        "This is intended for display to " + chr(0xFC) + "sers."
    assert sfv.serialize(sfv.parse("a=?1, b, c; foo=bar", "dictionary"), "dictionary") == "a, b, c;foo=bar"
