"""What a failed call raises (§9.7.6), and the warnings a call emits.

A problem is an ``AtlasProblem``, of the subclass its code names where the
registry has it (``NotGranted`` for ``not_granted``), with every member of
the problem document as an attribute and the document itself, unknown
members included, as ``problem``. A code the face does not know is a bare
``AtlasProblem``, handled by its ``status`` and ``retryable`` (CORE-PROB-002).
"""

from __future__ import annotations

from typing import Any

#: The problem registry (§9.3.3): each code's status and retryable.
REGISTRY: dict[str, tuple[int, bool]] = {
    "invalid_document": (400, False),
    "invalid_request": (400, False),
    "unauthenticated": (401, False),
    "not_granted": (403, False),
    "denied": (403, False),
    "budget_exceeded": (403, False),
    "approval_required": (403, False),
    "confirmation_required": (428, False),
    "not_found": (404, False),
    "not_in_environment": (404, False),
    "idempotency_conflict": (409, False),
    "in_progress": (409, True),
    "resume_gap": (409, False),
    "retired": (410, False),
    "precondition_failed": (412, False),
    "critical_trait_unsupported": (422, False),
    "resolution_failed": (422, False),
    "upgrade_required": (426, False),
    "limit_exceeded": (429, True),
    "output_limit_exceeded": (200, False),
    "internal": (500, False),
    "upstream_failed": (502, True),
    "unavailable": (503, True),
    "deadline_exceeded": (504, False),
}

_MEMBERS = ("type", "title", "status", "detail", "instance", "code", "retryable", "retry_after", "target", "next", "details", "request_id", "estimate")


class AtlasProblem(Exception):
    """A problem a call was answered with (§9.3, §9.7.6)."""

    code: str = "internal"

    def __init__(self, problem: dict[str, Any] | None = None, **members: Any):
        problem = dict(problem or {}, **members)
        self.problem: dict[str, Any] = problem
        self.code = problem.get("code") or type(self).code
        status, retryable = REGISTRY.get(self.code, (None, False))
        self.type: str | None = problem.get("type")
        self.title: str | None = problem.get("title")
        self.status: int | None = problem.get("status", status)
        self.detail: str = problem.get("detail") or problem.get("title") or ""
        self.instance: str | None = problem.get("instance")
        self.retryable: bool = bool(problem.get("retryable", retryable))
        self.retry_after: int | None = problem.get("retry_after")
        self.target: str | None = problem.get("target")
        self.next: str | None = problem.get("next")
        self.details: list[dict[str, Any]] = problem.get("details") or []
        self.request_id: str | None = problem.get("request_id")
        self.estimate: dict[str, Any] | None = problem.get("estimate")
        super().__init__(f"{self.code}: {self.detail}" if self.detail else self.code)

    def __getattr__(self, name: str) -> Any:
        # A member a newer gateway added is an attribute too.
        problem = self.__dict__.get("problem") or {}
        if name in problem:
            return problem[name]
        raise AttributeError(name)


def _class_name(code: str) -> str:
    return "".join(part.capitalize() for part in code.split("_"))


#: One subclass per registry code, by code: NotGranted for not_granted.
CLASSES: dict[str, type[AtlasProblem]] = {code: type(_class_name(code), (AtlasProblem,), {"code": code, "__doc__": f"The problem {code} (§9.3.3)."}) for code in REGISTRY}
globals().update({cls.__name__: cls for cls in CLASSES.values()})


def problem(document: dict[str, Any] | None) -> AtlasProblem:
    """The exception for a problem document: its code's class, or AtlasProblem."""
    document = document or {}
    return CLASSES.get(document.get("code", ""), AtlasProblem)(document)


#: The name pass 1 used; it is AtlasProblem.
AtlasError = AtlasProblem


class EngineError(RuntimeError):
    """atlas-engine failed, or could not be started. The next call starts a new one."""


class AtlasWarning(UserWarning):
    """A warning a call's answer carried (§9.7.4 step 11)."""


class TruncatedWarning(AtlasWarning):
    """The gateway cut a table at the caller's limit: the rows are not all of it (STREAM-004)."""


__all__ = ["AtlasProblem", "AtlasError", "EngineError", "AtlasWarning", "TruncatedWarning", "REGISTRY", "CLASSES", "problem", *(cls.__name__ for cls in CLASSES.values())]
