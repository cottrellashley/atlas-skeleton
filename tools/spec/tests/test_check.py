"""The one command: every file regenerates byte-identically and every check passes."""

from atlasref import __main__ as cli


def test_check_passes():
    assert cli.check() == 0
