"""§9.3.3, the problem registry, and §9.7.6's CLI exit codes.

The table is transcribed here; `check` also reads 09-3-documents.md from the
repository's docs/source/atlas-spec directory and fails if the transcription
differs. ATLAS_SPEC can override that directory.
"""

import os
import pathlib
import re

from . import REPO

TYPE_BASE = "https://atlas.jhg.com/problems/"

REGISTRY = [
    {"code": "invalid_document", "status": 400, "retryable": False,
     "when": "A body is not I-JSON, or has duplicate members"},
    {"code": "invalid_request", "status": 400, "retryable": False,
     "when": "A request fails its schema; target is the first failing pointer"},
    {"code": "unauthenticated", "status": 401, "retryable": False,
     "when": "No valid token; WWW-Authenticate says how to sign in"},
    {"code": "not_granted", "status": 403, "retryable": False,
     "when": "A listed capability or interaction the caller holds no grant for"},
    {"code": "denied", "status": 403, "retryable": False, "when": "A deny refuses it"},
    {"code": "budget_exceeded", "status": 403, "retryable": False, "when": "Reserving the call would pass a budget"},
    {"code": "approval_required", "status": 403, "retryable": False,
     "when": "An action awaits another principal's approval"},
    {"code": "confirmation_required", "status": 428, "retryable": False,
     "when": "The call's estimated cost passes the caller's confirmation threshold; estimate holds it (amount, "
             "unit), and the call is repeated with Atlas-Confirm"},
    {"code": "not_found", "status": 404, "retryable": False,
     "when": "No such capability, version or resource, or a confidential one the caller does not hold"},
    {"code": "not_in_environment", "status": 404, "retryable": False,
     "when": "It exists, but not in this environment"},
    {"code": "idempotency_conflict", "status": 409, "retryable": False,
     "when": "The key was used for a different request"},
    {"code": "in_progress", "status": 409, "retryable": True,
     "when": "A request with this Idempotency-Key is still running; repeat it after retry_after to get its answer"},
    {"code": "resume_gap", "status": 409, "retryable": False,
     "when": "A stream or subscription can no longer resume from the position given; the client reports the gap "
             "and starts again"},
    {"code": "retired", "status": 410, "retryable": False, "when": "The version was retired at its sunset"},
    {"code": "precondition_failed", "status": 412, "retryable": False, "when": "The ETag no longer matches"},
    {"code": "critical_trait_unsupported", "status": 422, "retryable": False,
     "when": "The client lacks a trait the capability marks critical"},
    {"code": "resolution_failed", "status": 422, "retryable": False,
     "when": "A payload could not be resolved (§9.5)"},
    {"code": "upgrade_required", "status": 426, "retryable": False,
     "when": "The client's protocol is below the gateway's minimum"},
    {"code": "limit_exceeded", "status": 429, "retryable": True,
     "when": "A rate or concurrency limit; retry_after"},
    {"code": "output_limit_exceeded", "status": 200, "retryable": False,
     "when": "The answer was truncated at the caller's cap (status 200, in the end line or final frame)"},
    {"code": "internal", "status": 500, "retryable": False, "when": "A fault in the gateway"},
    {"code": "upstream_failed", "status": 502, "retryable": True,
     "when": "The service's upstream failed in a way that may pass"},
    {"code": "unavailable", "status": 503, "retryable": True,
     "when": "The service is unhealthy (its circuit is open); retry_after"},
    {"code": "deadline_exceeded", "status": 504, "retryable": False,
     "when": "The call's deadline passed; its work was stopped"},
]
BY_CODE = {c["code"]: c for c in REGISTRY}

# §9.7.6: the CLI's exit code for each problem; 1 for anything else.
CLI_EXIT = {
    "invalid_request": 2,
    "unauthenticated": 3,
    "not_granted": 4, "denied": 4, "budget_exceeded": 4, "approval_required": 4, "confirmation_required": 4,
    "not_found": 5, "not_in_environment": 5, "retired": 5,
    "limit_exceeded": 6, "unavailable": 6, "in_progress": 6,
    "deadline_exceeded": 7,
    "resolution_failed": 8, "critical_trait_unsupported": 8, "upgrade_required": 8,
}


def cli_exit(code):
    return CLI_EXIT.get(code, 1)


def problem(code, target=None, **members):
    """A problem document for `code` (tests and scenarios)."""
    entry = BY_CODE[code]
    doc = {"type": TYPE_BASE + code, "status": entry["status"], "code": code, "retryable": entry["retryable"]}
    if target is not None:
        doc["target"] = target
    doc.update(members)
    return doc


def spec_dir():
    return pathlib.Path(os.environ.get("ATLAS_SPEC", REPO / "docs/source/atlas-spec"))


def read_spec_table(directory=None):
    """The registry as 09-3-documents.md writes it: [(code, status, retryable)],
    or None when the spec is not on this machine."""
    path = pathlib.Path(directory or spec_dir()) / "09-3-documents.md"
    if not path.exists():
        return None
    text = path.read_bytes().decode("utf-8")
    section = text.split("## 9.3.3 The problem registry", 1)[1].split("\n## ", 1)[0]
    rows = []
    for line in section.splitlines():
        m = re.match(r"^\| `([a-z][a-z0-9_]*)` \| (\d{3})[^|]*\| (yes|no) \|", line)
        if m:
            rows.append((m.group(1), int(m.group(2)), m.group(3) == "yes"))
    return rows
