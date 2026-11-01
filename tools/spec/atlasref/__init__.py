"""Atlas Protocol references, generated from the text of §9 with independent
reference implementations: tools/spec writes spec/protocol/references/."""

import pathlib

REPO = pathlib.Path(__file__).resolve().parents[3]


def references_root():
    return REPO / "spec" / "protocol" / "references"
