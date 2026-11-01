"""The face: FIAtlas, and what a call returns (ATLAS-CORE §6).

``call(address, request)`` executes an interaction once. The request record
is the one the CLI and Node send; what comes back is the same logical value,
in Python's types: an exact decimal is a ``decimal.Decimal``, never a float.
A result says whether it is complete, whether the gateway cut it at the
caller's limit, its request id and its usage; its warnings are also emitted
as Python warnings (§9.7.4 step 11).
"""

from __future__ import annotations

import os
import time
import warnings as _warnings
from decimal import Decimal
from typing import Any, Iterator

from ._engine import Engine, engine_path, shared
from .errors import AtlasWarning, TruncatedWarning, problem

#: The variables a connection is made with (§9.7.2), read when it is made.
_ENVIRONMENT = ("ATLAS_URL", "ATLAS_ENV", "ATLAS_HOME", "ATLAS_TOKEN", "ATLAS_CONFORMANCE")


def _decimals(row: dict[str, Any], paths: list[str]) -> dict[str, Any]:
    for path in paths:
        head, _, tail = path.partition(".")
        if head not in row or row[head] is None:
            continue
        if tail:
            inner = row[head]
            if isinstance(inner, dict) and isinstance(inner.get(tail), str):
                inner[tail] = Decimal(inner[tail])
        elif isinstance(row[head], str):
            row[head] = Decimal(row[head])
    return row


def _emit(warnings: list[dict[str, str]]) -> None:
    """Each warning as Python's: DeprecationWarning for a deprecation."""
    for w in warnings:
        kind: type[Warning] = AtlasWarning
        if w.get("code") == "deprecated":
            kind = DeprecationWarning
        elif w.get("code") == "output_limit_exceeded":
            kind = TruncatedWarning
        _warnings.warn(f"{w.get('code')}: {w.get('text')}", kind, stacklevel=3)


class _Answer:
    """What every result says beside its value."""

    def _describe(self, answer: dict[str, Any]) -> None:
        self.profile: str | None = answer.get("profile")
        self.request_id: str | None = answer.get("request_id")
        self.warnings: list[dict[str, str]] = answer.get("warnings") or []
        self.usage: dict[str, Any] | None = answer.get("usage")
        self.truncated: bool = bool(answer.get("truncated"))

    @property
    def complete(self) -> bool:
        """Whether the whole answer arrived: nothing was cut at the caller's
        limit (ATLAS-CORE §10: a truncated result is incomplete)."""
        return not self.truncated


class Result(_Answer):
    """One call's answer: a record, or a table's rows.

    A table iterates its rows, each a dict; ``read_as`` gives it as a list
    of records, a dict of columns, a pyarrow.Table or a pandas.DataFrame.
    """

    def __init__(self, answer: dict[str, Any]):
        self._describe(answer)
        self._decimals: list[str] = answer.get("decimals") or []
        value = answer.get("value")
        if isinstance(value, list):
            self.record = None
            self.rows: list[dict[str, Any]] | None = [_decimals(r, self._decimals) for r in value]
        else:
            self.record = _decimals(value, self._decimals) if isinstance(value, dict) else value
            self.rows = None

    @property
    def table(self) -> bool:
        return self.rows is not None

    def __iter__(self) -> Iterator[dict[str, Any]]:
        return iter(self.rows if self.rows is not None else [self.record])

    def __len__(self) -> int:
        return len(self.rows) if self.rows is not None else 1

    def __getitem__(self, i: int) -> dict[str, Any]:
        if self.rows is None:
            raise TypeError("a record answer has no rows")
        return self.rows[i]

    def read_as(self, kind: Any) -> Any:
        """The rows as ``list`` (records), ``dict`` (columns), a
        ``pyarrow.Table`` or a ``pandas.DataFrame``; decimals stay exact."""
        rows = list(self)
        if kind in (list, "records", "list"):
            return rows
        if kind in (dict, "columns", "dict"):
            names: list[str] = []
            for r in rows:
                names += [k for k in r if k not in names]
            return {n: [r.get(n) for r in rows] for n in names}
        name = getattr(kind, "__name__", kind)
        if name in ("Table", "arrow"):
            import pyarrow as pa

            return pa.Table.from_pylist(rows)
        if name in ("DataFrame", "pandas"):
            import pandas as pd

            return pd.DataFrame(rows)
        raise TypeError(f"read_as takes list, dict, pyarrow.Table or pandas.DataFrame, not {kind!r}")

    def to_pandas(self) -> Any:
        return self.read_as("pandas")

    def __repr__(self) -> str:
        cut = ", cut at the caller's limit" if self.truncated else ""
        if self.rows is not None:
            return f"<Result {self.profile}: {len(self.rows)} rows{cut}>"
        return f"<Result {self.profile}: a record>"


class Table(_Answer):
    """A table read as Arrow from the engine's results pipe."""

    def __init__(self, arrow: Any, answer: dict[str, Any]):
        self._describe(answer)
        self.arrow = arrow

    @property
    def columns(self) -> list[str]:
        return list(self.arrow.column_names)

    def __len__(self) -> int:
        return self.arrow.num_rows

    def read_as(self, kind: Any) -> Any:
        """The table as a ``pyarrow.Table``, a ``pandas.DataFrame`` with
        Arrow-backed dtypes (decimals stay exact), ``list`` of records or
        ``dict`` of columns."""
        name = getattr(kind, "__name__", kind)
        if name in ("Table", "arrow"):
            return self.arrow
        if name in ("DataFrame", "pandas"):
            import pandas as pd

            return self.arrow.to_pandas(types_mapper=pd.ArrowDtype)
        if kind in (list, "records", "list"):
            return self.arrow.to_pylist()
        if kind in (dict, "columns", "dict"):
            return self.arrow.to_pydict()
        raise TypeError(f"read_as takes list, dict, pyarrow.Table or pandas.DataFrame, not {kind!r}")

    def to_pandas(self) -> Any:
        return self.read_as("pandas")


class _Domain:
    """``atlas.<domain>``: each name under it is a fetch of that id (§9.7.1)."""

    def __init__(self, atlas: FIAtlas, domain: str):
        self._atlas, self._domain = atlas, domain

    def __getattr__(self, name: str) -> Any:
        if name.startswith("_"):
            raise AttributeError(name)
        capability = f"{self._domain}.{name.replace('_', '-')}"

        def fetch(*, deadline: float | None = None, **request: Any) -> Result:
            return self._atlas.call(f"{capability}:fetch", request, deadline=deadline)

        fetch.__name__ = name
        fetch.__doc__ = f"{capability}:fetch, at its current version; the keywords are its request."
        return fetch


class FIAtlas:
    """A connection to the gateway: ``url``, else ``env``'s, else the
    environment's (§9.7.2), signed in with ``token`` (a workload's) or from
    the shared token store under ATLAS_HOME that ``atlas login`` writes.
    Every FIAtlas of a process shares its one engine (§5.1)."""

    def __init__(self, url: str | None = None, env: str | None = None, token: str | None = None, *, engine: str | None = None):
        self._path = engine_path(engine)
        self._connect = {k: v for k, v in {"url": url, "env": env, "token": token}.items() if v}
        self._connect["environment"] = {k: os.environ[k] for k in _ENVIRONMENT if k in os.environ}
        self._engine: Engine | None = None
        self._connection: int | None = None

    def __getattr__(self, name: str) -> Any:
        if name.startswith("_"):
            raise AttributeError(name)
        return _Domain(self, name)

    def _run(self, op: str, args: dict[str, Any] | None = None, *, until: float | None = None, **kw: Any) -> dict[str, Any]:
        """Run op on the engine, connecting first when needed. until is the
        public call's one deadline (R3-06, on time.monotonic()'s clock): the
        connection, the wait for the engine and the call all come out of it,
        and the deadline the call sends is what is left of it."""
        engine = shared(self._path)
        if engine is not self._engine:
            answer = engine.run("connect", self._connect, until=until)
            self._engine, self._connection = engine, answer["value"]["connection"]
        args = {**(args or {}), "connection": self._connection}
        if until is not None and isinstance(args.get("options"), dict):
            left = until - time.monotonic()
            if left <= 0:
                raise problem({"code": "deadline_exceeded", "detail": "the call reached its deadline before its request was sent"})
            args["options"] = {**args["options"], "deadline_ms": max(1, int(left * 1000))}
        return engine.run(op, args, until=until, **kw)

    @staticmethod
    def _until(deadline: float | None) -> float | None:
        return None if deadline is None else time.monotonic() + deadline

    def call(self, address: str, request: dict[str, Any] | None = None, *, deadline: float | None = None,
             accept: list[str] | None = None, idempotency_key: str | None = None) -> Result:
        """Execute one interaction: the request is checked against its contract
        before anything is sent, and the answer as it is read. deadline, in
        seconds, bounds the whole call."""
        until = self._until(deadline)
        opts: dict[str, Any] = {}
        if deadline is not None:
            opts["deadline_ms"] = int(deadline * 1000)
        if accept is not None:
            opts["accept"] = accept
        if idempotency_key is not None:
            opts["idempotency_key"] = idempotency_key
        answer = self._run("call", {"address": address, "request": request or {}, "options": opts}, until=until)
        result = Result(answer)
        _emit(result.warnings)
        return result

    def fetch(self, address: str, request: dict[str, Any] | None = None, *, deadline: float | None = None) -> Table:
        """A data fetch as an Arrow table, streamed from the engine's results
        pipe. It needs pyarrow (``pip install fiatlas[arrow]``); without it
        nothing is asked of the engine."""
        try:
            import pyarrow  # noqa: F401
        except ImportError as e:
            raise ImportError("fetch() reads Arrow and needs pyarrow: pip install fiatlas[arrow], or use call()") from e
        until = self._until(deadline)
        if ":" not in address:
            address += ":fetch"
        opts: dict[str, Any] = {}
        if deadline is not None:
            opts["deadline_ms"] = int(deadline * 1000)
        answer = self._run("call", {"address": address, "request": request or {}, "options": opts}, until=until, results="arrow")
        if "table" not in answer:
            raise problem({"code": "invalid_document", "detail": f"{address} is not a table"})
        table = Table(answer["table"], answer)
        _emit(table.warnings)
        return table

    def whoami(self) -> dict[str, Any]:
        return self._run("whoami")["value"]

    def catalog(self, domain: str | None = None, family: str | None = None, effect: str | None = None) -> list[dict[str, Any]]:
        args = {k: v for k, v in {"domain": domain, "family": family, "effect": effect}.items() if v}
        return self._run("catalog", args)["value"]

    def search(self, words: str) -> list[dict[str, Any]]:
        return self._run("search", {"words": words})["value"]

    def describe(self, id: str) -> dict[str, Any]:
        return self._run("describe", {"id": id})["value"]

    def guide(self, id: str, client: str = "python", max_tokens: int | None = None) -> str:
        args: dict[str, Any] = {"id": id, "client": client}
        if max_tokens:
            args["max_tokens"] = max_tokens
        return self._run("guide", args)["value"]

    def access(self, id: str | None = None, domain: str | None = None) -> list[dict[str, Any]]:
        return self._run("access", {k: v for k, v in {"id": id, "domain": domain}.items() if v})["value"]

    def close(self) -> None:
        """Let go of this connection. The process's engine stays for the
        others, and ends with the process."""
        engine, self._engine = self._engine, None
        if engine is not None and engine.usable() and self._connection is not None:
            try:
                engine.run("disconnect", {"connection": self._connection})
            except Exception:  # noqa: BLE001 - closing never raises
                pass
        self._connection = None

    def __enter__(self) -> FIAtlas:
        return self

    def __exit__(self, *exc: object) -> None:
        self.close()


__all__ = ["FIAtlas", "Result", "Table"]
