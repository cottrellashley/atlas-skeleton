# Proof 0, hermetic: result

Historical proof run on 2026-10-31 with no outside systems. The first implementation’s verification script passed:

- every Go suite under the race detector
- the 16 Python tests, which passed in five consecutive runs

This is Proof 0 of the [proof plan](../source/atlas-proof-plan.md), done hermetically. The findings describe the first implementation; see [current progress](../HANDOFF-PROGRESS.md) before treating any feature as available. It also goes ahead of parts of Proofs 1 and 3, and of the Python SDK, by putting fakes where the real systems will be.

## What was tried

The gateway, the Go client and the Python face were built against a fake Snowflake (`snowfake`) holding deterministic synthetic Harbour data, and a fake Entra issuer (RS256 keys, real JWT verification).

The fake sits behind the Snowflake adapter's `Warehouse` interface. So the adapter's real code runs: planning, SQL, chunk links and decoding. So do the real client's handoff reads.

The plan's setup used DuckDB as the data source. The fake replaces it, deliberately: the handoff path then runs through the code real Snowflake will use.

Contracts change from the back with `-pin`: the gateway serves an earlier profile, or none, while the clients stay unchanged.

## The plan's pass criteria

| Criterion | Result | Proved by |
|---|---|---|
| A capability added later works with no client change (ATLAS-CORE-009) | Pass, for the Go client and the Python face. The CLI is not built, so ATLAS-CLI-002 is not tested yet. | `TestACapabilityAddedLaterIsCallableWithNoClientChange` (e2e), `test_a_capability_added_later_is_callable_with_no_client_change` |
| Without the grant, `not_granted`. With it, `invalid_request` and no limit reserved (ATLAS-ACCESS-009, -010) | Pass | `TestAuthorisationComesBeforeValidation` |
| Every call, refused or not, leaves one audit record (ATLAS-ACCESS-004) | Pass for the count: one record per call, in memory or a JSONL file. The immutable store and Log Analytics are not exercised. | `TestAuthorisationComesBeforeValidation`, `TestNoTokenIsUnauthenticatedAndAudited` |
| The layer check passes (ATLAS-PACKAGES-001) | Pass. Its self-test shows each rule refusing what it should. | `TestEveryImportOfTheGatewayIsAllowed`, `TestTheCheckRefusesWhatItShould` |

## Also proven, hermetically

| Claim | Spec | Proved by |
|---|---|---|
| Streamed rows equal handed-off rows, and equal the rows read again after links are renewed (Go and Python) | ATLAS-CORE-005, §2.2.1 | `TestStreamedAndHandedOffRowsAreIdentical`, `TestTheClientReadsTheSameRowsStreamedOrHandedOff`, `test_streamed_and_handed_off_tables_are_identical` |
| Links expire, and are refused without their download headers. Renewal re-issues them without running the query again, and only the caller who ran the query may renew. | §2.2.1.3 | `TestExpiredLinksAreRefusedAndRenewed`, `TestExpiredLinksAreRenewedMidRead`, `TestStreamedAndHandedOffRowsAreIdentical` |
| The contract is written into the planned SQL: casts, units, enum conformance, `QUERY_TAG` | §6.4 | `TestPlanWritesTheContractIntoTheStatement` |
| A locked client keeps its columns, dtypes and values across two later revisions, streamed or handed off, in Go and in Python. The revisions rename a column, convert a unit, add an enum value and add a column. | §3 | `TestALockedClientKeepsItsShapeWhileTheContractEvolves`, `test_a_locked_application_keeps_its_dataframe_while_the_contract_evolves` |
| A request written against an old profile still means what it meant | §9.5 | `TestAnOldRequestIsResolvedForward`, the same two tests |
| An enum value newer than the reader's profile arrives as `UNKNOWN` | §3 | `TestOlderContractsSeeUnknownForNewEnumValues`, the Python evolution test |
| Python gets every column with its declared type; no decimal becomes a float | ATLAS-PY-002 | `test_every_column_keeps_the_type_its_profile_declares` |
| The JSON form, which the conformance suite reads, and the Arrow form carry the same values | §9.8.3 | `test_the_json_form_and_the_arrow_form_carry_the_same_values` |
| Ctrl+C ends a call within a second, wherever the call is: before the answer, mid-stream or mid-download. The same engine answers at once afterwards, so its work has stopped. | ATLAS-PY-005 | `test_ctrl_c_ends_a_call_within_a_second_and_the_engine_carries_on` |
| A deadline ends a call with `deadline_exceeded` within a second, on both paths | ATLAS-PY-005 | `test_a_deadline_ends_a_call_with_deadline_exceeded` |
| A forked child gets an engine of its own, and leaves its parent's alone (`os.fork` and a fork `Pool`) | ATLAS-PY-004 | `test_a_forked_child_starts_its_own_engine_and_leaves_its_parents_alone`, `test_each_process_of_a_fork_pool_gets_an_engine_of_its_own` |
| No engine outlives its Python process, even when the process is killed mid-call | ATLAS-PY-007 | `test_no_engine_outlives_the_python_process_that_started_it` |
| An Arrow answer is complete only with its final batch, and the rows that batch counts | §9.6.5 | `TestAnArrowAnswerIsCompleteOnlyWithItsFinalBatch`, `TestStreamedAndHandedOffRowsAreIdentical` |
| Every module is tidy | ATLAS-PACKAGES-004 | `check.sh` |

## Numbers

These are the best of five measurements from that local proof run, with every portfolio on the latest date. They are not current performance guarantees.

| Data | Gateway alone (NDJSON over HTTP) | Python face → engine → gateway, to a `pa.Table` |
|---|---|---|
| Small: 160 rows | 1.3 ms | 1.2 ms handed off, 1.6 ms streamed |
| Large: 40,000 rows (3.6 MB of Arrow) | 1,064 ms | 1,033 ms handed off, 1,045 ms streamed |

The hop from the face to the engine is lost in the noise. The second at Large is the fake warehouse scanning 2.4 million synthetic rows per query.

A real throughput figure needs a cheaper fake or Proof 1's real Snowflake. The spec's 30-million-row example is untested.

## Assumed, to check against the real systems

- **Snowflake's chunk encoding** (`sdk/go/snowarrow`, each line marked `ASSUMPTION`):
  - each chunk is a whole IPC stream
  - `NUMBER` up to precision 18 is a scaled integer of the narrowest width, and above 18 a `Decimal128`
  - timestamps are scaled integers, or epoch/fraction/timezone structs, depending on their scale

  The fake writes exactly these, so a difference found in Proof 1 shows up as a failing decoder test, not as wrong data.
- **Chunk links.** The fake issues HMAC-signed, expiring links that need one header. Real links are presigned storage URLs with their own headers; Proof 1, step 1 records which.
- **Re-issuing links from a persisted result** costs no warehouse time: the fake keeps results for 24 hours. Proof 1, step 4.
- **Settlement from query history by `QUERY_TAG`.**
- **Tokens.** The fake issuer stands in for Entra. On-Behalf-Of is not exercised (Proof 3).

## Findings for the spec

1. **The final batch works across Go and Python.** arrow-go 18.8 writes and reads per-batch message metadata, and pyarrow reads it (`read_next_batch_with_custom_metadata`). §9.6.5's final zero-row batch carrying `atlas:end` is implementable as written.

   The provisional `Atlas-End` HTTP trailer was removed. Proxies drop trailers, so it was the riskier choice.

   The engine's pipe to the face ends its streams the same way (§5.1).
2. **A cancelled command has no problem code.** §5.1 says a cancelled command is answered with "the problem `deadline_exceeded` or the cancellation", but §9.3's registry has no cancellation code. The engine provisionally does two things:
   - before the stream begins, it answers `{"ok": false, "cancelled": true}`
   - once the stream has begun, it ends the stream without the final batch

   The face expects either, since it asked for the cancel. This needs a decision.
3. **ATLAS-PY-007 can't rest on standard input closing.** A conformance runner closes its input and then waits for its answers, so the engine finishes the commands it already has. So the engine also ends itself when its parent changes, which is how it sees the Python process die.

   This works on Linux and macOS. Windows needs a job object (not done).
4. **Ctrl+C reaches the engine too**, since it shares the terminal's process group. The engine ignores SIGINT, and the face turns Ctrl+C into `cancel`. Without that, Ctrl+C would kill the engine, not just cancel the call.
5. **A unit lens is not bit-exact when applied twice to a binary64 column** (bp → fraction at the source, back to bp for the lock). The tests compare `oas` within 1e-9 relative. §9.5 should say whether number columns that cross a lens are compared exactly.
6. **Imports within a layer.** §6.5.2 says "a package may depend only on packages in lower layers". But its own table lets `porttest` and `fakes` depend on `ports`, in the same layer, and `core` packages depend on each other.

   The check allows imports within a layer, except between members of an independent group. The spec's wording should say "its own layer or lower".
7. **A lock of exact ids does not meet ATLAS-VISION-004 by itself.** That requirement says a locked artefact makes no discovery call when it runs. But the lock holds only exact ids, so a locked client still asks `atlas.describe` for each profile, once per process.

   To meet it, the lock must carry the profiles themselves, or the types generated from them (`atlas lock gen`, §5.4).
8. **`sdk/go/go.mod` was not tidy.** Arrow and TOML were marked indirect, and their own requirements were missing. Fixed; `check.sh` now catches it.

## Not done

- the CLI
- `async` and asyncio cancellation
- polars and DuckDB for `read_as`
- the attribute form, `atlas.portfolio.holdings(…)`
- Windows engine lifetime
- the exact `atlas/1.0` documents: RFC 8785 fingerprints, §9.4's projection, vectors, the conformance gateway
- On-Behalf-Of, Service Bus, Blob, Cosmos DB, the MCP edge
- cancelling the warehouse query when a streaming client goes away (Proof 3, step 4)
