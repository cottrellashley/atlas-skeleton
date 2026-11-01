"""FIAtlas: Atlas's calls from Python."""

from __future__ import annotations

import os
from typing import Any

from ._engine import Engine, engine_path


class Table:
    """A table answer, resolved to this application's reader profile.

    ``via`` is how the rows travelled: ``gateway`` (streamed) or ``handoff``
    (downloaded straight from the source's result storage). ``writer`` is
    the profile the gateway wrote under; ``reader`` the one the rows are
    resolved to (the locked one, when the application has a lock).
    """

    def __init__(self, arrow: Any, *, writer: str, reader: str, via: str):
        self.arrow = arrow
        self.writer = writer
        self.reader = reader
        self.via = via

    @property
    def columns(self) -> list[str]:
        return list(self.arrow.column_names)

    def __len__(self) -> int:
        return self.arrow.num_rows

    def read_as(self, kind: Any = "arrow") -> Any:
        """The table as pyarrow (``pa.Table`` or ``"arrow"``), pandas with
        Arrow-backed dtypes so every column keeps its declared type
        (``pd.DataFrame`` or ``"pandas"``), or row dicts (``list``)."""
        name = kind if isinstance(kind, str) else f"{kind.__module__}.{kind.__qualname__}"
        if name in ("arrow", "pyarrow.lib.Table"):
            return self.arrow
        if name in ("pandas", "pandas.DataFrame", "pandas.core.frame.DataFrame"):
            import pandas as pd

            return self.arrow.to_pandas(types_mapper=pd.ArrowDtype)
        if kind is list or name == "records":
            return self.arrow.to_pylist()
        raise ValueError(f"read_as does not know {kind!r}")

    def __repr__(self) -> str:
        return f"<Table {len(self)} rows via {self.via}, reader {self.reader}>"


class FIAtlas:
    """Atlas from Python. Every call goes through atlas-engine, which starts
    on first use, one per process.

    ``url`` and ``token`` default to ATLAS_URL and ATLAS_TOKEN. ``lock`` is
    the application's atlas.lock; by default the engine looks for one in the
    working directory and its parents.
    """

    def __init__(self, url: str | None = None, token: str | None = None, *, lock: str | os.PathLike[str] | None = None, engine: str | None = None):
        self._connect = {"url": url or "", "token": token or "", "lock": os.fspath(lock) if lock else ""}
        self._path = engine
        self._engine: Engine | None = None
        self.connected: dict[str, Any] | None = None

    def _run(self, op: str, args: dict[str, Any] | None = None, **options: Any) -> dict[str, Any]:
        e = self._engine
        if e is None or not e.usable():
            if e is not None:
                e.stop()  # this process's broken engine ends; a parent's is left alone
            e = Engine(engine_path(self._path))
            try:
                self.connected = e.run("connect", self._connect)["result"]
            except BaseException:
                e.stop()
                raise
            self._engine = e
        return e.run(op, args, **options)

    def fetch(self, address: str, request: dict[str, Any] | None = None, *, execution: str | None = None, timeout: float | None = None) -> Table:
        """Call a table interaction, such as ``portfolio.holdings@v1:fetch``.

        ``execution`` is a preference (``gateway`` or ``handoff``); the
        gateway decides. ``timeout`` is the call's deadline in seconds.
        """
        ans = self._run("fetch", {"address": address, "request": request or {}}, results="arrow", execution=execution, timeout=timeout)
        r = ans["result"]
        return Table(ans["table"], writer=r["writer"], reader=r["reader"], via=r["via"])

    def call(self, address: str, request: dict[str, Any] | None = None, *, timeout: float | None = None) -> Any:
        """Call an interaction whose answer is one record, such as explain."""
        return self._run("call", {"address": address, "request": request or {}}, timeout=timeout).get("result")

    def describe(self, id: str) -> dict[str, Any]:
        return self._run("describe", {"id": id})["result"]

    def search(self, words: str) -> dict[str, Any]:
        return self._run("search", {"words": words})["result"]

    def catalog(self) -> dict[str, Any]:
        return self._run("catalog")["result"]

    def whoami(self) -> dict[str, Any]:
        return self._run("whoami")["result"]

    def lock(self, ids: list[str], path: str | os.PathLike[str] = "atlas.lock") -> dict[str, str]:
        """Pin each capability version to its current profile in the lock at
        path, as ``atlas lock`` does. A client that connects with the lock
        reads under those profiles from then on."""
        return self._run("lock", {"ids": ids, "path": os.fspath(path)})["result"]

    def close(self) -> None:
        if self._engine is not None:
            self._engine.stop()
            self._engine = None

    def __enter__(self) -> FIAtlas:
        return self

    def __exit__(self, *exc: object) -> None:
        self.close()
