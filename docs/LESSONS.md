# Lessons learnt, and rules to keep

Written at the end of the first handoff session (2026-11-01: C0 to C2, and C3 in part). Read it with [AGENTS.md](../AGENTS.md), [HANDOFF.md](HANDOFF.md) and [HANDOFF-PROGRESS.md](HANDOFF-PROGRESS.md). AGENTS.md binds; this records what the work taught, and what to watch.

## 1. Is it sound? The verdict

The shape is right for the vision: one pipeline, ports, modules, and contract-driven clients. The C1 exit test proved it. A separate company module added a ticket capability and its own `workspace` dimension with no change to Atlas. Scope, audit, deadlines, discovery and the three clients came with it.

The risk is **incompleteness, not tangle**. Until C4 and C5 land, a company's easiest path still has gaps it might patch around, and patching around them is how spaghetti starts.

## 2. Rules to keep

### The boundary
1. **A company adds; it never edits.** A new integration is a `ports.Module`, a profile, and, where needed, a `ports.Dimension`, in the company's own package or module. If an integration needs an Atlas file changed, that is a framework gap. Fix it in Atlas, with a test and a ledger entry. Never patch it inside the company module.
2. **Never copy assembly internals.** If a company has to copy Atlas code to plug in, the framework is unfinished. The one known case is `gateway/cmd/gateway` (see §4).
3. **No writing, metered or long-running integration before C5.** Without the shared mechanisms for idempotency, approvals, budgets and durable operations, each connector would hand-roll them. That is the fastest route to spaghetti.

### One pipeline, one place
4. **A cross-cutting concern is a pipeline step, at its written position.** Each finding here was fixed once, in the core, because nothing repeats a step. Keep it so: a provider never re-checks access, scope or limits.
5. **The outcome is what the caller received.** A success is recorded only once the answer has been delivered; a failure is recorded at once. A caller who went away, or a panic mid-write, is never a success (R3-07, D-9).
6. **One absolute deadline per public call, set at its entry.** Discovery, waiting on locks or on the engine, refresh, the request and consuming its answer all come out of it. Shared work is waited on in a way the waiter can abandon (R3-06).

### Scope and access
7. **Registering a dimension grants nothing.** Grants come only from policy. An undefined dimension, in a profile or in a policy, stops the start.
8. **Enforce scope only where it is read.** Scope is read through properties and items, so a scoped value anywhere else (`anyOf`, `oneOf`, `prefixItems`, `additionalProperties`, `allOf`, `if`/`then`/`else`) is refused at registration. Never let a schema carry scope that the checks do not walk.
9. **A result's required scope values are its evidence.** If they are missing, null or mistyped, the answer is withheld. The core checks results even when the provider has already filtered its source.

### Contracts
10. **Conservative evolution.** When the gate cannot prove a change safe in both directions, it is breaking: boolean items, nested renames, lenses over fields several schemas share. Refusal is acceptable; silent misreading is not.
11. **The protocol references are not ours to edit.** A change the work needs goes in ASSUMPTIONS.md as a *proposed amendment*, implemented in code, with its tests admitting exactly that difference (as `TestTheBuiltinsAreTheProtocols` admits A-200 and nothing more).

### Working method
12. **Test first, and see it fail for the stated reason.** Several "obvious" fixes were wrong until a test said so. The first R3-01 fix refused every valid projected Arrow answer; only `make e2e` caught it.
13. **Test the happy path too, not only the refusal.** The R3-01 regression above lived in the gap between "it refuses bad answers" and "it still sends good ones".
14. **One fresh adversarial reviewer per package, before the commit.** Every review found real holes the author had missed: scope under applicators, `$ref` siblings, a panic mid-write, a budget restarted after discovery.
15. **Keep `handoff` green; commit at every green point.** Run `make check` and `make e2e` before each commit and push often, so a session that ends suddenly loses nothing.
16. **A test that hangs is not a test.** Under `synctest`, a goroutine blocked on a `sync.Mutex` stops fake time. Bound every wait, so that without the fix the test fails rather than hangs.
17. **Read the dependency's source before fixing around it.** R3-02 was confirmed in gosnowflake's own code before it was fixed, then reproduced through the real driver.
18. **Be honest about evidence levels.** "Hermetic" means fakes, DuckDB and fixtures. Snowflake, Entra and Azure have never been reached. Never mark something proven on the strength of a fake.

## 3. Lessons from the defects

| What happened | Lesson |
|---|---|
| A provider that left out the scoped column passed the guard (R3-01) | A check that finds nothing to check must refuse, not pass |
| The fix then refused valid projected Arrow answers | Re-checking scope after a projection had dropped the evidence; check where the evidence still is |
| gosnowflake's own batch slice was mutated (R3-02) | Never mutate a slice a dependency handed you; own a copy |
| Refresh omitted `client_id` (R3-03) | Fakes must be as strict as the real system; the fake token endpoints now refuse what Entra refuses |
| The audit file silently dropped five fields (R3-04) | Port suites must compare *every* member; the suite now refuses to run unless its sample facts set them all |
| Records whose caller went away were audited as success (R3-07) | Records and tables must share one outcome path |
| The built-in whoami never sent `scope` | The pipeline withholds members a contract does not declare (R106); unit tests that bypass the pipeline cannot see this |
| An existing test asserted the defect (R3-12, `fund.bare`) | When a fix breaks an old test, ask whether the test encoded the bug |
| `modcheck` refused any module outside jhgonline | A guard written for one case can block a legitimate one; generalise it so it gets stricter, not looser |
| `TestAuditFacts` raced the asynchronous settlement | Wait on a signal (`fakes.Audit.Await`), never on time |

## 4. Be careful: the known risks

1. **The production root is not importable.** `gateway/cmd/gateway` is a `main` package, so a company cannot add modules to it without editing or copying it. Extract it into a package under `gateway/cmd/` (C4) before the first real integration.
2. **Changing work has no shared mechanisms yet** (C5). See rule 3.
3. **Profiles are heavy to write.** That is deliberate, because agents need the detail, but company engineers will feel it. Invest in `atlas-new` and the generated guides (C6), or engineers will copy and paste profiles and drift.
4. **Evolution is narrow by design.** Top-level renames only, and no ambiguous lenses. Some contract changes will need a new major version. Make that cheap, rather than widening the gate unsafely.
5. **The protocol amendments need an owner.** A-200 (company dimensions) awaits the references owner and Ashley. If amendments sit unanswered, code and references drift apart.
6. **The gates are not run.** Gate A (C4) and Gate B (C5) are what prove the handoff. Until then, "it plugs in" is proven for a read-only HTTP capability on fakes, and nothing more.
7. **Open C2 gaps.**
   - With no explicit deadline, Python's engine-lock wait and its connect are unbounded.
   - A Go refresh runs on its holder's context, so cancelling the holder aborts the shared refresh.
   - The C2 exit matrix lacks the stalled-upstream, expired-queue and serialisation-failure cases, and the NDJSON and Arrow forms.

## 5. Before the first real company integration

- Finish C3 (R3-13, R3-14), then C4: the importable production root, the conformance runners, the RS256 issuer, and the Gate A exercise with its boundary hash check.
- For any changing or job capability, finish C5 first.
- Ask the company for its first integrations early; they choose the reference extensions.
