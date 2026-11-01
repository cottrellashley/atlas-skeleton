"""A stand-in gateway for the face's tests: the references' fixture
(spec/protocol/references/scenarios/fixtures/gateway.json, written from the
spec alone) served over real HTTP on loopback, so the real engine binary is
driven end to end. It answers conformance.echo and conformance.rows by their
defaults (the references' README), and a test sets ``mode`` to make it hold
a call, truncate a table or deprecate a capability. It records every path
it is asked for.
"""

from __future__ import annotations

import datetime as dt
import hashlib
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

FIXTURE = Path(__file__).resolve().parents[3] / "spec" / "protocol" / "references" / "scenarios" / "fixtures" / "gateway.json"


def _substitute(v: Any, url: str) -> Any:
    if isinstance(v, str):
        return v.replace("{gateway}", url)
    if isinstance(v, list):
        return [_substitute(x, url) for x in v]
    if isinstance(v, dict):
        return {k: _substitute(x, url) for k, x in v.items()}
    return v


def row(i: int) -> dict[str, Any]:
    at = dt.datetime(2026, 1, 1, tzinfo=dt.timezone.utc) + dt.timedelta(seconds=i, microseconds=i)
    return {
        "i": i,
        "label": f"row-{i}",
        "amount": f"{i * 3 // 2}.{i * 3 % 2 * 500000:06d}",
        "at": at.strftime("%Y-%m-%dT%H:%M:%S.%fZ"),
        "day": (dt.date(2026, 1, 1) + dt.timedelta(days=i)).isoformat(),
        "share": None if i % 5 == 4 else i / 100,
    }


class StandIn:
    """The gateway; ``url`` is where it listens, ``mode`` what it does."""

    def __init__(self) -> None:
        doc = json.loads(FIXTURE.read_text())
        self.profiles: dict[str, dict] = doc["profiles"]
        self.current: dict[str, str] = doc["current"]
        self.mode = ""
        self.seen: list[str] = []
        self.release = threading.Event()
        self.holding = threading.Event()  # set once a call is being held
        self._bootstrap = doc["bootstrap"]
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), self._handler())
        self.server.daemon_threads = True
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"
        self.bootstrap = _substitute(self._bootstrap, self.url)
        self._thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self._thread.start()

    def close(self) -> None:
        self.release.set()
        self.server.shutdown()
        self.server.server_close()

    def _handler(self) -> type[BaseHTTPRequestHandler]:
        gw = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, *args: Any) -> None:
                pass

            def _send(self, status: int, body: bytes, media: str, headers: dict[str, str] | None = None) -> None:
                self.send_response(status)
                self.send_header("Content-Type", media)
                self.send_header("Content-Length", str(len(body)))
                self.send_header("X-Request-Id", "standin-1")
                for k, v in (headers or {}).items():
                    self.send_header(k, v)
                self.end_headers()
                self.wfile.write(body)

            def _json(self, v: Any, profile: str | None = None, headers: dict[str, str] | None = None) -> None:
                h = dict(headers or {})
                if profile:
                    h["Atlas-Profile"] = f'"{profile}"'
                self._send(200, json.dumps(v).encode(), "application/json", h)

            def _problem(self, status: int, code: str, detail: str, **extra: Any) -> None:
                p = {"type": f"https://atlas.jhg.com/problems/{code}", "title": code, "status": status, "code": code, "retryable": False,
                     "detail": detail, "next": "See the detail.", "request_id": "standin-1", **extra}
                self._send(status, json.dumps(p).encode(), "application/problem+json", {"Atlas-Error-Code": code})

            def do_GET(self) -> None:
                gw.seen.append(self.path)
                if self.path == "/.well-known/atlas":
                    body = json.dumps(gw.bootstrap).encode()
                    self._send(200, body, "application/json", {"ETag": '"' + hashlib.sha256(body).hexdigest()[:16] + '"'})
                    return
                self._problem(404, "not_found", self.path)

            def do_POST(self) -> None:
                gw.seen.append(self.path)
                body = json.loads(self.rfile.read(int(self.headers.get("Content-Length") or 0)) or b"{}")
                if self.headers.get("Authorization") != "Bearer ct-valid":
                    self._problem(401, "unauthenticated", "the token is not valid")
                    return
                if self.path == "/v1/atlas/atlas.whoami:fetch":
                    self._json({"principal": "00000000-0000-4000-8000-000000000001", "name": "Stand-in", "kind": "person", "roles": [],
                                "environment": "conformance", "available_domains": ["conformance"], "house_rules": {"revision": "h1", "rules": []}})
                elif self.path == "/v1/atlas/atlas.describe:fetch":
                    self._describe(body)
                elif self.path == "/v1/data/conformance.echo@v1:fetch":
                    if gw.mode == "hold":
                        gw.holding.set()
                        gw.release.wait()
                        return
                    out = {"text": body.get("text", ""), "count": body.get("count", 1), "ratio": body.get("ratio"), "when": body.get("when"),
                           "kind": body.get("kind", "plain"), "tags": body.get("tags", [])}
                    headers = {"Deprecation": "@1790812800", "Sunset": "Sun, 31 Jan 2027 00:00:00 GMT"} if gw.mode == "deprecated" else None
                    self._json(out, gw.current["conformance.echo@v1"], headers)
                elif self.path == "/v1/data/conformance.rows@v1:fetch":
                    self._rows(body)
                elif self.path == "/v1/data/conformance.echo@v1:refuse":
                    self._problem(403, "not_granted", "no", target="/text")
                else:
                    self._problem(404, "not_found", self.path)

            def _describe(self, body: dict) -> None:
                exact = body.get("exact") if body.get("by") == "exact" else gw.current.get(body.get("id", ""))
                if exact is None and "@" not in body.get("id", "@"):
                    exact = gw.current.get(body["id"] + "@v1")
                doc = gw.profiles.get(exact or "")
                if doc is None:
                    self._problem(404, "not_found", str(exact))
                    return
                interactions = {name: {"held_by": "everyone"} for name in doc["interactions"]}
                eligibility = [{"interaction": name, "eligible": [{"binding": b["address"]} for b in i["bindings"]], "ineligible": []} for name, i in doc["interactions"].items()]
                pinned = f"{doc['identity']['id']}@v{doc['identity']['version']}"
                self._json({"profile": doc, "governance": {"capability": pinned, "governance_revision": "g1", "for_caller": {"interactions": interactions, "limits": [], "approval": None}},
                            "availability": "available", "eligibility": eligibility}, "atlas.describe@v1#ff036976dde88885")

            def _rows(self, body: dict) -> None:
                n = int(body.get("n", 0))
                columns = body.get("columns")
                self.send_response(200)
                self.send_header("Content-Type", "application/x-ndjson")
                self.send_header("Atlas-Profile", '"' + gw.current["conformance.rows@v1"] + '"')
                self.send_header("Transfer-Encoding", "chunked")
                self.send_header("X-Request-Id", "standin-1")
                self.end_headers()
                sent = n if gw.mode != "truncate" else min(n, 2)

                def chunk(b: bytes) -> None:
                    self.wfile.write(f"{len(b):x}\r\n".encode() + b + b"\r\n")

                lines = []
                for i in range(sent):
                    r = row(i)
                    if columns:
                        r = {c: r[c] for c in columns if c in r}
                    lines.append(json.dumps(r, separators=(",", ":")))
                    if len(lines) == 1000:
                        chunk(("\n".join(lines) + "\n").encode())
                        lines = []
                if lines:
                    chunk(("\n".join(lines) + "\n").encode())
                problem = None
                if gw.mode == "truncate":
                    problem = {"type": "https://atlas.jhg.com/problems/output_limit_exceeded", "title": "Output limit exceeded", "status": 200,
                               "code": "output_limit_exceeded", "retryable": False, "detail": "max_rows is 2", "next": "Ask for fewer rows.", "request_id": "standin-1"}
                end = {"$atlas": "end", "rows": sent, "truncated": gw.mode == "truncate", "usage": {"rows": sent}, "problem": problem}
                chunk((json.dumps(end, separators=(",", ":")) + "\n").encode())
                self.wfile.write(b"0\r\n\r\n")

        return Handler
