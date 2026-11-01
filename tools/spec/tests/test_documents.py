"""The spec's own example documents, valid against the schemas of §9.3.1."""

import tomllib

from atlasref import docvalidate, fingerprint, profiles

OWNER = "3f2b0c1d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"  # §2.3 abbreviates it "3f2b…"


def test_section_2_5_connect_handover():
    doc = {
        "kind": "connect", "protocol": "amqp", "namespace": "dv-eus2-fiqdataevents-sbus.servicebus.windows.net",
        "entity": "data-landed/subscriptions/ana-7f2",
        "auth": {"kind": "entra-scope", "scope": "https://servicebus.azure.net/.default"},
        "rights": ["listen"], "profile": "data.landed@v1#51c7e08b2d6f9a43", "format": "cloudevents+json",
        "settle": "peek-lock", "durable": True, "expires": "2026-10-30T18:00:00Z",
    }
    assert docvalidate.errors("connect.schema.json", doc) == []
    emulator = dict(doc, auth={"kind": "emulator", "connection_string": "Endpoint=sb://localhost;UseDevelopmentEmulator=true"})
    assert docvalidate.errors("connect.schema.json", emulator) == []
    assert docvalidate.errors("connect.schema.json", dict(doc, auth={"kind": "emulator"}))


def test_section_2_3_resource_envelope():
    record = {
        "name": "nautilus/dashboards/d1",
        "metadata": {"uid": "6f1c0e9a2b7d4c3e8f5a1b2c3d4e5f60", "etag": '"e8"', "generation": 8,
                     "created": "2026-10-01T09:00:00Z", "updated": "2026-10-30T14:02:11Z", "owner": OWNER,
                     "labels": {"desk": "credit"}, "deleting": None},
        "title": "Credit risk", "visibility": "public",
        "status": {"observed_generation": 8, "conditions": [
            {"type": "published", "status": "true", "reason": "copied", "message": "", "observed_generation": 8,
             "last_transition": "2026-10-30T14:02:15Z"},
            {"type": "ready", "status": "true", "reason": "converged", "message": "", "observed_generation": 8,
             "last_transition": "2026-10-30T14:02:15Z"}]},
    }
    assert docvalidate.errors("resource.schema.json", record) == []
    bad = dict(record, metadata=dict(record["metadata"], etag="e8"))
    assert docvalidate.errors("resource.schema.json", bad)


def test_section_3_1_3_governance():
    doc = {"capability": "portfolio.holdings@v1", "governance_revision": "g-41", "for_caller": {
        "interactions": {"fetch": {"held_by": "role:fi-analysts"}, "export": {"held_by": "role:fi-analysts"}},
        "limits": [{"interaction": "fetch", "kind": "rate", "per_minute": 60, "remaining": 58,
                    "set_by": "role:fi-analysts"},
                   {"interaction": "fetch", "kind": "concurrency", "max": 4},
                   {"interaction": "fetch", "kind": "output", "max_rows": 1000000, "max_bytes": 1073741824}],
        "approval": None}}
    assert docvalidate.errors("governance.schema.json", doc) == []


def test_section_2_6_describe_with_eligibility():
    p = fingerprint.with_fingerprint(profiles.holdings())
    doc = {"profile": p, "governance": {"capability": "portfolio.holdings@v1", "governance_revision": "g-41",
                                        "for_caller": {"interactions": {}, "limits": [], "approval": None}},
           "availability": "available",
           "eligibility": [{"interaction": "fetch",
                            "eligible": [{"binding": "https", "encoding": "json", "limits": {"max_rows": 100000}}],
                            "ineligible": [{"binding": "flight", "missing": ["transport:arrow-flight"],
                                            "hint": "Use atlas.python, or export with --format parquet"}]}]}
    assert docvalidate.errors("describe.schema.json", doc) == []


def test_section_3_3_10_lock():
    text = """
[capabilities]
"portfolio.holdings" = { version = 1, fingerprint = "3f9a1c0b7d2e4a65" }
"curve.government-zero" = { version = 1, fingerprint = "0e02d4c1aa7b3f10" }
"data.landed" = { version = 1, fingerprint = "51c7ee8b2d6f9a43" }
"""
    assert docvalidate.errors("lock.schema.json", tomllib.loads(text)) == []


def test_section_4_1_problem_and_9_6_5_end_line():
    problem = {"type": "https://atlas.jhg.com/problems/limit_exceeded", "title": "Too many calls", "status": 429,
               "code": "limit_exceeded", "retryable": True, "retry_after": 30, "next": "Wait 30 seconds.",
               "request_id": "req-1"}
    assert docvalidate.errors("problem.schema.json", problem) == []
    assert docvalidate.errors("problem.schema.json", {k: v for k, v in problem.items() if k != "retry_after"})
    end = {"$atlas": "end", "rows": 3, "truncated": False, "usage": {"rows": 3}, "problem": None}
    assert docvalidate.errors("chunk-end.schema.json", end) == []


def test_an_operation_and_its_states():
    op = {"name": "operations/op-91", "address": "yieldbook.scenarios@v1:fetch", "state": "running",
          "created": "2026-10-31T09:00:00Z", "updated": "2026-10-31T09:00:02Z"}
    assert docvalidate.errors("operation.schema.json", op) == []
    assert docvalidate.errors("operation.schema.json", dict(op, state="succeeded"))  # no result
    assert docvalidate.errors("operation.schema.json", dict(op, state="succeeded", result={"x": 1},
                                                            profile="yieldbook.scenarios@v1#0123456789abcdef")) == []
