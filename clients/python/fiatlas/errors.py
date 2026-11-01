"""What a failed call raises."""

from __future__ import annotations

from typing import Any


class AtlasError(Exception):
    """A problem a call was answered with (§9.3). ``code`` says which."""

    def __init__(self, problem: dict[str, Any]):
        self.problem = problem
        self.code: str = problem.get("code", "internal")
        self.status: int | None = problem.get("status")
        self.retryable: bool = bool(problem.get("retryable"))
        self.detail: str = problem.get("detail") or problem.get("title") or ""
        super().__init__(f"{self.code}: {self.detail}" if self.detail else self.code)


class EngineError(RuntimeError):
    """atlas-engine failed, or could not be started. The next call starts a new one."""
