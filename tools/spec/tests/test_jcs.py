"""RFC 8785 (§9.4.2 step 1), hand-worked."""

import hashlib

import pytest

from atlasref import jcs

BS = chr(92)


@pytest.mark.parametrize("value, text", [
    (0, "0"), (-0.0, "0"), (1, "1"), (-1, "-1"), (1.5, "1.5"), (100.0, "100"), (1e21, "1e+21"), (1e20, "100000000000000000000"),
    (1e-6, "0.000001"), (1e-7, "1e-7"), (123.456, "123.456"), (5e-324, "5e-324"), (9007199254740991, "9007199254740991"),
    (0.1 + 0.2, "0.30000000000000004"), (1.7976931348623157e308, "1.7976931348623157e+308"),
])
def test_numbers_in_ecmascript_form(value, text):
    assert jcs.number(value) == text


def test_members_sorted_by_utf16_code_units():
    # U+1F600 is the surrogate pair D83D DE00, which sorts before U+FB01 in UTF-16
    # though it is the larger code point.
    doc = {chr(0xFB01): 1, chr(0x1F600): 2, "a": 3, "B": 4}
    assert jcs.canonical(doc) == '{"B":4,"a":3,"' + chr(0x1F600) + '":2,"' + chr(0xFB01) + '":1}'


def test_only_the_escapes_rfc8785_requires():
    s = '"' + BS + chr(8) + chr(9) + chr(10) + chr(12) + chr(13) + chr(0x1F) + chr(0x7F) + "/" + chr(0xE9) + chr(0x2028)
    want = '"' + BS + '"' + BS + BS + BS + "b" + BS + "t" + BS + "n" + BS + "f" + BS + "r" + BS + "u001f" + chr(0x7F) + "/" + chr(0xE9) + chr(0x2028) + '"'
    assert jcs.string(s) == want


def test_no_whitespace_and_literals():
    assert jcs.canonical({"b": [True, False, None], "a": {}}) == '{"a":{},"b":[true,false,null]}'


def test_fingerprint_is_the_first_16_of_the_sha256():
    canonical = jcs.dumps({"identity": {"id": "a.b", "version": 1}})
    assert canonical == b'{"identity":{"id":"a.b","version":1}}'
    digest = hashlib.sha256(canonical).hexdigest()
    assert len(digest) == 64 and digest[:16] == digest[:16].lower()
