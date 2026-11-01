# Porting notes

The local `atlas.local/sqlglot` module follows SQLGlot v30.27.0. The code follows
the Python module structure closely so deliberate compatibility changes can be
mapped to the matching Go code. Most Go files name that mapping in a `// Port of ...`
comment. Generated sources, converted tests and fixtures are included in this
repository; these source mappings are maintenance context, not build dependencies.

## Naming conventions

| Python | Go |
|---|---|
| expression class `exp.Foo` | kind constant `exp.Foo`; private bases `_Foo` are `exp.FooBase` |
| `exp.Foo(this=x, expression=y)` | `exp.New(exp.Foo, "this", x, "expression", y)` |
| `self.expression(exp.Foo(...))` in the parser | `p.Expr(exp.Foo, ...)` |
| `isinstance(e, exp.Foo)` / `type(e) is exp.Foo` | `e.Is(exp.Foo)` / `e.IsKind(exp.Foo)` |
| `TokenType.L_PAREN` | `tokens.LParen` (CamelCase of the member name) |
| `exp.DType.BIGINT` | `exp.DTypeBigint` |
| class attribute `FOO_BAR` | field `FooBar` of `DialectClass`, `ParserConfig`, `GeneratorConfig` or `tokens.Config` |
| dialect-specific class attributes | `ParserConfig.Extra` / `GeneratorConfig.Extra` |
| `self._parse_foo(...)` | `p.ParseFoo(...)`: virtual through `ParserConfig.Methods`, default implementation `parseFoo` |
| `self.foo_sql(e)` | `g.FooSQL(e)`; per-kind generation functions live in `GeneratorConfig.SQLMethods`, `TRANSFORMS` in `GeneratorConfig.Transforms` (which take precedence, like `_build_dispatch`) |
| `super().foo_sql(e)` | capture the previous `SQLMethods` / `Transforms` entry before replacing it |
| `self._curr`, `_prev`, `_next`, `_index` | `p.Curr`, `p.Prev`, `p.Next`, `p.Index` (token truthiness is `tok.Ok()`) |
| `self._match(T)` / `self._match(T, advance=False)` | `p.Match(T)` / `p.Check(T)` |
| `raise OptimizeError(...)` in optimizer rules | `panic(&errs.OptimizeError{...})`, recovered into a returned error by the exported entry points |

Things to watch for:

- Python dicts keep insertion order; use `OrderedMap` where the order is observable.
- Dicts keyed by expressions compare structurally; use `exp.ExprKey` (`e.MapKey()`) plus
  `Equal`, or a list with `Equal` lookups.
- Go evaluates map lookups and function calls in an unspecified order within one expression;
  look a table entry up before calling a parsing method that advances the tokens.
- Python's truthiness differs per type: `e.Bool(key)` mirrors `bool(args.get(key))`.
- A Python `None` argument is an absent (nil) argument in Go. Code that sets an empty list where
  Python leaves the argument unset changes `simplify.gen` and serde output.

## Status

Ported: tokenizer, parser, generator, expressions and builders, base dialect, Snowflake,
DuckDB, dialect helpers, transforms, JSONPath, time formats, schema, the whole optimizer
(`optimize` and every rule), lineage, diff, anonymize, serde, planner, executor and the CLI.

Not ported: the other 32 dialects. Their converted test suites are already in
`tests/dialects` and run as soon as a dialect is registered.

## Optional maintenance tools

Ordinary Go builds and `make check` use the generated sources and tests already in
this repository. Run the tools below only for a deliberate compatibility update,
using a scratch environment separate from the normal build.

The Python helpers accept a directory containing the public `sqlglot` package,
such as an isolated installation of version 30.27.0. Test conversion additionally
requires the matching public Python test corpus; a package installation may omit
those tests. Run Python helpers with `python3 -I` and consult each script's usage
for its inputs.

- `convert_tests.py` and `convert_all.sh` regenerate `tests/dialects` and `tests/core` from the
  Python test modules. The `PYTHON_ONLY` table of `convert_tests.py` lists the test methods that
  become documented skips.
- `gen_tables.py` generates the token types, data types, expression kinds and method
  resolution tables (`tokens/*_gen.go`, `exp/*_gen.go`). `gen_modules.py` generates the
  module of each expression class, for messages. `gen_config.py`, `gen_time.py`,
  `gen_typing.py` and `emit_typing.py` generated the base dialect tables.
- `pysql.py` and `pyexec.py` are the Python halves of differential tests against
  `internal/cmd/gosql` and `internal/cmd/goexec`. Their first argument is the
  isolated directory containing the reference Python package. Feed both
  implementations the same queries and compare their output.

## Test runtime

`internal/pyrt` runs the converted tests. It implements the subset of Python the tests use:
values (lists, tuples, dicts, sets, None), operators with Python semantics, f-strings,
`unittest` assertions and context managers, and bindings from the Python API (`sqlglot`,
`sqlglot.exp`, `sqlglot.optimizer`, `sqlglot.executor`, ...) to the Go API. A statement that
needs an unported dialect evaluates to a skip marker that propagates through the values that
depend on it, so only that statement is skipped. Constructs the converter can't translate would
fail their statement visibly; there are none in the current suites.
