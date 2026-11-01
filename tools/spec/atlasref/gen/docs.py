"""references/schemas/*.schema.json (§9.3.1) and references/grammar.abnf (§9.1)."""

from .. import grammar, schemas
from ..jsonio import dumps


def build():
    out = {"schemas/" + name: dumps(doc) for name, doc in schemas.all_documents().items()}
    out["grammar.abnf"] = grammar.ABNF.encode("utf-8")
    return out
