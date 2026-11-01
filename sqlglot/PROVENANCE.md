# SQL library: implementation and maintenance

This is the local Go implementation of SQLGlot v30.27.0, maintained as the
`atlas.local/sqlglot` module. Its runtime does not need Python or another source
checkout. SQLGlot and the Go implementation are covered by the MIT notice in
[LICENSE](LICENSE).

## Included implementation

The module contains its Go packages, native and converted tests, fixtures in
`testdata/`, code generators in `internal/cmd`, maintenance scripts in `tools/port`,
and the generated sources those scripts produced. The repository's `make check`
validates the module alongside the gateway and SDK.

The base, Snowflake and DuckDB dialects are implemented. Converted tests for other
dialects remain available as extension material; skipped cases do not mean those
dialects are supported. API usage and coverage are described in [README.md](README.md),
and language-mapping details are in [PORTING.md](PORTING.md).

## Local integration decisions

| Decision | Reason to preserve it |
|---|---|
| Use `atlas.local/sqlglot` imports, including in generators | Generated output must continue to build against this repository's module. |
| Remove unused private declarations and fields | Keep staticcheck U1000 clean without changing public behavior. |
| `ParseOneOpts` and lineage's subquery step select the first expression directly | Avoid a loop that always returns on its first pass (SA4004). This API behavior does not replace the gateway's exactly-one-statement check. |
| Use `strings.EqualFold` in the Snowflake generator's `UNNEST` check | Avoid repeated string conversion (SA6005). |
| Use `append(args, buildOptions(a)...)` | Preserve the simpler equivalent append operation (S1011). |
| Do not reassign the unused result in `unnest_subqueries` | `replaceCondition` already mutates the tree in place (SA4006). |
| Generate the explained `//lint:file-ignore SA4006` header in converted tests | The tests preserve Python assignment order; some defaults and loop targets are intentionally unread. Fix generated-code conventions in `tools/port/convert_tests.py`. |
| Disable ST1005 in this module's `staticcheck.conf` | Preserve SQLGlot's established error text. |

Warehouse authentication, authorization, cancellation, effect tracking and driver
behavior belong to the [gateway adapter](../gateway/adapter/snowflake). Keep this
library focused on SQL syntax and transformations; qualification alone does not
authorize a query.

## Extending and maintaining the module

1. Add a regression case for the behavior being changed, using the existing native
   tests, converted suites or fixtures.
2. Make the narrow change in the owning package. Preserve expression-tree and
   dialect behavior unless the capability requires an explicit change.
3. Update the generator when changing generated output so regeneration preserves
   the fix.
4. Run `make check` from the repository root. During iteration, run
   `go test -race ./...` from this directory.
5. Update the relevant usage or maintenance note when behavior or supported
   dialects change.

Changing the SQLGlot compatibility baseline is separate, deliberate maintenance
work. The [optional maintenance tools](PORTING.md#optional-maintenance-tools) can
compare against a public Python package and regenerate converted tests when the
matching test corpus is available. Ordinary builds and tests use the sources
already included here.
