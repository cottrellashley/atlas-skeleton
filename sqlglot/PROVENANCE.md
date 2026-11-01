# Provenance

This module is sqlglot-go: Ashley Cottrell's line-by-line Go port of [SQLGlot](https://github.com/tobymao/sqlglot) v30.27.0. It is ported into this repository as a module of its own rather than taken as a dependency, so Atlas's SQL foundation lives, builds and is tested in the repository.

| | |
|---|---|
| Upstream | https://github.com/cottrellashley/sqlglot-go |
| Commit | `3d81e5393850e249136284859f6203a34db29f16` (2026-10-30), the commit the 2026-10-31 design review inspected (INTEGRATIONS) |
| Ported | 2026-10-31 |
| Module here | `github.com/jhgonline/fi-app-atlas/sqlglot` (was `github.com/cottrellashley/sqlglot-go`) |
| Licence | MIT, [LICENSE](LICENSE): SQLGlot's authors and the Go port |

## What came across

Everything at that commit except its CI workflow (`.github/`), which `make check` replaces:
- every Go package;
- the 74 test files, including the 53 tests converted from SQLGlot's Python suite, `tests/core` and `tests/dialects`;
- the fixtures in `testdata/`;
- the code generators (`internal/cmd`);
- the port's own tools (`tools/port`, which generated the `*_gen.go` files and the converted tests);
- `README.md` and `PORTING.md`.

## What changed in the port

| Change | Why |
|---|---|
| The module path, in all 178 files that name it, the generators included | It is a module of this repository |
| Twelve unused declarations removed: `isSeqRestricted`, `fnArgs`, `stagedFileSingleTokens`, `pyRStrip`, `strArgs`, `isComplement`, `pf`, `tokenErr`, and the fields `buildOpts.into`, `buildOpts.prefix`, `Plan.dag` and `item.isNone` | staticcheck U1000 |
| `ParseOneOpts` and lineage's subquery step take the first element directly, not through a loop that always returns on its first pass. The behaviour is unchanged | staticcheck SA4004 |
| `strings.EqualFold` in the Snowflake generator's UNNEST check | staticcheck SA6005 |
| One `append(args, buildOptions(a)...)` | staticcheck S1011 |
| `unnest_subqueries` no longer reassigns a result it never reads. `replaceCondition` still rewrites the tree in place | staticcheck SA4006 |
| `tools/port/convert_tests.py` writes a `//lint:file-ignore SA4006` line with its reason, and the 53 converted tests carry it. The tests keep Python's order of assignment, so some argument defaults and loop targets are never read | staticcheck SA4006 in generated code, fixed where it is generated |
| `staticcheck.conf` turns off ST1005 for this module | The error strings are SQLGlot's own, kept word for word |

The upstream suite passed before the port: `go vet` and `go test -race` on every package, 26 s. Here it passes again, with gofmt, go vet, staticcheck and `go mod tidy`, as part of `make check`.

## Updating

To take a newer upstream commit:
1. Copy it over this directory, keeping this file.
2. Rewrite the module path again.
3. Re-apply the changes above, or drop those upstream has made.
4. Run `make check`.
5. Record the new commit here.
