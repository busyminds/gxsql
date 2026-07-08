# gxsql Agent Guide

## Module

`github.com/busyminds/gxsql` — SQL-native table validation through
`database/sql`. Expectations render to SQL and run in the database; rows are not
loaded into Go memory first.

- **Go:** `go 1.24.0` minimum; CI tests Go 1.24.x and 1.26.x.
- **Core package:** uses `database/sql` and does not import a concrete driver.
  PostgreSQL, SQLite, DuckDB, and MySQL drivers live only in `integration/`.

## Start here

Read in this order before changing behavior:

1. `doc.go` — public contract and export privacy defaults
2. `README.md` and `docs/` — user-facing semantics and examples
3. `Makefile` — supported local/CI commands and pinned tool versions
4. `gxsqltest/` — `Check` / `Require` adapters for `*testing.T`

## Layout

| Path                    | Role                                                                        |
| ----------------------- | --------------------------------------------------------------------------- |
| `*.go` (module root)    | Public API and implementation                                               |
| `gxsqltest/`            | Test helpers (`Check`, `Require`)                                           |
| `integration/`          | Separate module for real-engine conformance tests and drivers               |
| `docs/`                 | Tutorial, concepts, API reference                                           |
| `internal/conformance/` | Shared real-engine contract runner (used by `integration/`; not public API) |

## Driver and dialect boundaries

**Caller owns the driver.** Open `*sql.DB` with any `database/sql` driver, then
pass it as `gxsql.DB` (narrow interface: `QueryContext`, `QueryRowContext`
only).

**Caller selects the dialect.** Pass `gxsql.WithDialect(...)` explicitly in
production code and tests. When omitted, `ValidateTable` defaults to
`gxsql.Postgres()`.

Built-in dialects: `gxsql.Postgres()`, `gxsql.SQLite()`, `gxsql.DuckDB()`, and
`gxsql.MySQL()`. They implement `Dialect` (`QuoteIdent`, `Placeholder`,
`StringLength`). `gxsql.DuckDB()` uses double-quoted identifiers, `$n`
placeholders, and `LENGTH(expr)`; callers must supply a compatible
`database/sql` DuckDB driver. `gxsql.MySQL()` uses backtick-quoted identifiers,
`?` placeholders, and `CHAR_LENGTH(expr)`; callers must supply a compatible
`database/sql` MySQL driver. CI covers MySQL 8.4; MariaDB is not in the
supported matrix. CI runs the shared conformance kit against DuckDB 1.5.4 via
`github.com/duckdb/duckdb-go/v2` v2.10504.0. Identifiers must match
`^[A-Za-z_][A-Za-z0-9_]*$`. Custom `Dialect` implementations outside the
built-in set are not part of the published CI matrix; do not document or
implement them unless explicitly in scope.

`Expectation` is **sealed** — build expectations only via `RowCount`, `Column`,
`Int`, `Float`, and `String` builders. Do not implement `Expectation` outside
package `gxsql`.

## Validation model

### Collect-all, declaration order

`NewSuite(exps...)` preserves declaration order. `ValidateTable` runs every
expectation in that order. **Assertion failures never stop later expectations.**
`Report.Results[i]` always corresponds to `expectations[i]`.

### Policy failures vs returned errors

These are separate channels:

| Situation                                                                                                     | `ValidateTable` error | `Report` / gating                                   |
| ------------------------------------------------------------------------------------------------------------- | --------------------- | --------------------------------------------------- |
| Expectation assertion failed (data quality)                                                                   | `nil`                 | `Result.Success == false`; gate with `report.Err()` |
| Invalid config (bad identifier, nil dialect, negative caps, blank/duplicate `WithID`, empty/nil `In`/`NotIn`) | non-nil (default)     | zero report (default)                               |
| Database, scan, rendering, or context error (default)                                                         | non-nil               | not returned (zero report)                          |
| Execution error with `ContinueOnError()`                                                                      | `nil`                 | `Result.Err` set on affected slots                  |

- **`report.Err()`** — returns `nil` when every expectation passed; otherwise
  `*ValidationError` wrapping the full report. Use this for data-quality gating.
- **`ValidateTable`'s returned `error`** — configuration or execution failure,
  not a data-quality verdict.

Preflight collects all expectation configuration issues before SQL. Without
`ContinueOnError`, any preflight failure returns `*PreflightErrors` and runs no
SQL. With `ContinueOnError`, expectation preflight issues become config-failure
results in declaration order and valid expectations still execute.

## ContinueOnError

Default: first database/scan/rendering/context error aborts `ValidateTable` and
returns a zero `Report`.

With `gxsql.ContinueOnError()`:

- Expectation preflight and execution errors attach to individual `Result.Err`
  values; `ValidateTable` returns `(report, nil)`.
- Later expectations still run.
- Run-level option errors (nil dialect, negative caps, invalid key columns)
  still return a top-level error.
- **A nil top-level error is not success** — inspect `report.Err()` and
  per-result `Result.Err` / `Success`.
- Optional `CaptureQueryDiagnostics()` still captures SQL on execution errors;
  captured content is never exposed through default serialization or export.

## SummaryOnly, WithKey, and caps

| Control                | Default            | Effect                                                                                                      |
| ---------------------- | ------------------ | ----------------------------------------------------------------------------------------------------------- |
| `DefaultSampleCap`     | 20                 | Max `SampleValues` per failing result                                                                       |
| `DefaultFailedKeysCap` | 100                | Max `FailedKeys` when key mode is on; `0` = unlimited                                                       |
| Key mode               | off (summary-only) | When neither `WithKey` nor explicit `SummaryOnly()` is set, `ValidateTable` enables summary-only internally |
| `WithKey(cols...)`     | —                  | Loads failing row keys; disables summary-only                                                               |
| `SummaryOnly()`        | —                  | Counts + capped samples only; no failed-row keys                                                            |

Suite-level `Suite.WithSampleCap` / `WithFailedKeysCap` set defaults;
`ValidateTable` options override for one run. When keys are capped,
`FailedCount` and `FailedPercent` remain complete.

**Resource note:** each per-row expectation issues at least two full-table
`COUNT(*)` queries (total + failing). Prefer `SummaryOnly()` on large tables
with widespread failures; use `WithFailedKeysCap(0)` only when unbounded key
retention is acceptable. **Operational safeguards:**

- Set a deadline on every `ValidateTable` context. Production validation
  connections should use a read-only role scoped to validation views.
- Each `In` / `NotIn` value adds a bound placeholder; keep lists in the low
  thousands or use a lookup-table join instead.

## Diagnostics and export privacy

- `CaptureQueryDiagnostics()` records SQL and bound args on `Result` for
  optional export only — never in default `Result` string output or default
  `ExportReport`.
- `ExportReport` schema: `gxsql.report.v1` (`ExportSchemaVersion`).
  **Encode-only; no public decoder is promised.**
- **Default export omits** samples, failed keys, query text, and bound
  arguments.
- Opt in: `IncludeSamples`, `IncludeFailedKeys`, `IncludeCapturedDiagnostics`
  (table identifiers redacted to `<table>`), `IncludeCapturedArguments`
  (requires validate-time `CaptureQueryDiagnostics`).
- `policy_verdict` is `pass`/`fail` only when `Result.Err == nil`; any `Err`
  yields `unevaluated`. Configured thresholds export in `facts.configured_*`;
  default `display_name` redacts bound literals. Redactor failure fails export
  closed with no partial JSON.
- `Report.String()` and `gxsqltest.Check` / `Require` may embed sample values or
  failed keys in logs; redact before shipping to observability backends when
  columns may contain PII or secrets.

Attach stable machine identity with `WithID(id, exp)`; built-in expectations set
library-defined `Kind` on each `Result`.

## Test conventions

Run from the module root. For behavior changes, follow the test-first loop:
write a focused test that fails for the intended reason, implement the minimum
fix, then confirm green.

**Unit tests (`package gxsql`):** use the in-memory fake driver in
`harness_test.go` — `setHarnessData`, `openHarnessDB`, `harnessUsers`. Pass
`WithDialect(Postgres())` unless exercising another dialect's behavior. Do not
re-implement harness query logic in individual tests.

**Integration tests (`integration/integration_test.go`, `package gxsql_test`):**

```bash
cd integration
go test -race -run '^TestSQLiteConformance$' ./...
GXSQL_POSTGRES_DSN='postgres://…' go test -race -run '^TestPostgresConformance$' ./...
GXSQL_MYSQL_DSN='user:password@tcp(localhost:3306)/gxsql?parseTime=true' \
  go test -race -run '^TestMySQLConformance$' ./...
go test -race -run '^TestDuckDBConformance$' ./...
```

All four delegate to `internal/conformance.Run`.

**Consumer tests:** use `gxsqltest.Check` (continues on failure, returns `bool`)
or `gxsqltest.Require` (`t.Fatalf` on execution or validation failure).

**Export golden fixtures:** `go test -update ./...` in packages with golden
export tests (see `export_test.go`).

Preserve report shape, error categories, and wording unless a behavioral change
is intentional and covered by tests. Update `doc.go` and relevant `docs/` when
exported behavior changes.

## Commands

All targets run from the module root:

| Target                  | Command                                                                                                                |
| ----------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `make check`            | CI gate: `fmt-check`, `test`, `integration-test`, `lint`, `audit`                                                      |
| `make all`              | `check` + `build`                                                                                                      |
| `make build`            | `go build ./...`                                                                                                       |
| `make test`             | `go test -race ./...`                                                                                                  |
| `make integration-test` | `cd integration && go test -race ./...`                                                                                |
| `make fix`              | format, fix, and tidy both Go modules                                                                                  |
| `make fmt-check`        | fail if `gofmt -l .` is non-empty                                                                                      |
| `make lint`             | `go vet ./...`; `go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 ./...`                                               |
| `make audit`            | `go run github.com/securego/gosec/v2/cmd/gosec@v2.27.1 ./...`; `go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...` |
| `make cover`            | race-enabled coverage → `coverage.html`                                                                                |
| `make clean`            | remove `coverage.out`, `coverage.html`                                                                                 |
| `make help`             | list targets                                                                                                           |

CI's `analysis` job runs `make lint` and `make audit` on Go 1.26.x. The `fmt`,
`test`, `postgres-integration`, `mysql-integration`, `sqlite-integration`, and
`duckdb-integration` jobs mirror the other `make check` steps (Go 1.24.x and
1.26.x for test and engine conformance).

Before yielding, run the narrowest check that proves the change — usually
`make test` or a focused `go test -race -run '^TestName$' ./...`.
