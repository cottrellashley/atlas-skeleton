"""references/vectors/problems.json: the registry of §9.3.3 and §9.7.6's CLI
exit codes, one case per code, in the registry's order."""

from .. import problems
from ..jsonio import dumps

PATH = "vectors/problems.json"


def build():
    cases = []
    for c in problems.REGISTRY:
        cases.append({
            "name": c["code"],
            "code": c["code"],
            "expected": {
                "status": c["status"],
                "retryable": c["retryable"],
                "type": problems.TYPE_BASE + c["code"],
                "cli_exit": problems.cli_exit(c["code"]),
            },
        })
    cases.append({
        "name": "unknown code",
        "code": "warehouse_suspended",
        "note": "A code outside the registry is handled by the problem's own status and retryable (§9.3.3); the CLI "
                "exits 1 (§9.7.6).",
        "expected": {"cli_exit": 1},
    })
    doc = {
        "description": "The problem registry (§9.3.3): each code's HTTP status, whether it is retryable, its type URI, "
                       "and the CLI's exit code for it (§9.7.6). output_limit_exceeded travels with status 200, in an "
                       "end line or final frame.",
        "type_base": problems.TYPE_BASE,
        "cases": cases,
    }
    return {PATH: dumps(doc)}
