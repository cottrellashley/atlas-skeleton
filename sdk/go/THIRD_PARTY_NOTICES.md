# Third-party notices

The shared gateway utilities below include MIT-licensed code. Their copyright and
permission notice is reproduced unchanged here. All listed files are maintained
inside this repository; their design and integration guidance is in
[kit/PROVENANCE.md](kit/PROVENANCE.md).

## Shared gateway utilities

| Package | Files covered by the notice |
|---|---|
| `kit/health` | `health.go`, `health_test.go` |
| `kit/hot` | `config.go`, `config_test.go` |
| `kit/httpserver` | `httpserver.go`, `httpserver_test.go` |
| `kit/jwt` | `jwks.go`, `jws.go`, `verifier.go`, `jwt_test.go` |
| `kit/lifecycle` | `lifecycle.go`, `lifecycle_test.go` |
| `kit/middleware` | `middleware.go`, `middleware_test.go` |
| `kit/telemetry` | `accesslog.go`, `logging.go`, `route.go`, `traceparent.go`, `writer.go`, `telemetry_test.go` |

Keep this notice with redistributed copies of these files. Public module
dependencies keep their own licenses. The local SQL library's notices are in
[sqlglot/LICENSE](../../sqlglot/LICENSE), with an additional fixture notice in
[sqlglot/testdata/fixtures/jsonpath/LICENSE](../../sqlglot/testdata/fixtures/jsonpath/LICENSE).

### Licence

```text
MIT License

Copyright (c) 2026 Ashley Cottrell

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
