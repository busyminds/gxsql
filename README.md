# gxsql

> **Note:** The features and direction of `gxsql` are defined and guided by me,
> while almost all of the code has been generated with AI assistance. It works
> for me and is used in my own production projects. Contributions are very
> welcome — please feel free to open a pull request.

`gxsql` is a SQL-native data quality assertion framework for Go. It validates
database tables through `database/sql`, renders each expectation as SQL, and
evaluates checks in the database instead of loading whole tables into
application memory. Validation is collect-all: every expectation runs in
declaration order and one report captures all passes and failures. Execution
errors are different — by default they stop evaluation and return an error; use
`gxsql.ContinueOnError()` when later expectations should still run after
per-expectation database errors.

## Install

```bash
go get github.com/busyminds/gxsql
```

`gxsql` requires Go 1.24 or newer.

The core package is driver-neutral and has no runtime dependencies outside the
Go standard library. `gxsql` does not choose a SQL driver for you: open a
`*sql.DB` with your own `database/sql` driver, then select the dialect
explicitly when validating. The examples below use
`github.com/jackc/pgx/v5/stdlib`, `modernc.org/sqlite`,
`github.com/duckdb/duckdb-go/v2`, and `github.com/go-sql-driver/mysql` only as
conformance/integration drivers.

## Support matrix

Support levels used in this module:

- **supported** — covered by the current release docs and CI matrix.
- **built-in** — ships a first-party `Dialect` renderer in package `gxsql`. Does
  not imply a CI conformance job or bundled driver.
- **experimental** — not used for first-release claims; may change without
  notice.
- **community-maintained** — works through a caller-selected `database/sql`
  driver, but the engine/driver stack is owned outside `gxsql`.
- **expected-to-work** — outside the published matrix, but should work if it
  satisfies the `database/sql` and dialect contracts.

Built-in dialect renderers are `gxsql.Postgres()`, `gxsql.SQLite()`,
`gxsql.DuckDB()`, and `gxsql.MySQL()`. The matrix below separates dialect/API
support from engines exercised in CI conformance jobs.

| Area               | Level                | Floor / active coverage                                            | Notes                                                                                      |
| ------------------ | -------------------- | ------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| Go toolchain       | supported            | minimum Go 1.24; actively tested on Go 1.24.x and 1.26.x           | required to build and run the module                                                       |
| Ubuntu             | supported            | `ubuntu-24.04` in CI                                               | first-class CI target                                                                      |
| PostgreSQL         | supported            | PostgreSQL 16 in CI via `github.com/jackc/pgx/v5/stdlib`           | built-in `gxsql.Postgres()`; the driver is a conformance-only test dependency              |
| SQLite             | supported            | SQLite 3.50.4 in CI via `modernc.org/sqlite` v1.39.1               | built-in `gxsql.SQLite()`; the driver is a conformance-only test dependency                |
| DuckDB             | supported            | DuckDB 1.5.4 in CI via `github.com/duckdb/duckdb-go/v2` v2.10504.0 | built-in `gxsql.DuckDB()`; the driver is a conformance-only test dependency                |
| MySQL              | supported            | MySQL 8.4 in CI via `github.com/go-sql-driver/mysql` v1.10.0       | built-in `gxsql.MySQL()`; the driver is a conformance-only test dependency                 |

`gxsql` is intentionally driver-neutral: the core package validates against a
caller-selected `database/sql` driver, while PostgreSQL, SQLite, DuckDB, and
MySQL appear in the matrix as CI conformance paths rather than bundled runtime
dependencies.

## Example entry points

The four most common entry points are below and are expanded in the rest of this
README:

1. `ValidateTable` quick start.
2. Report gating with `report.Err()` / `report.Failures()`.
3. `gxsqltest.Check` and `gxsqltest.Require` for `testing.T`.
4. `ExportReport` for machine-readable JSON export.

## Quick start

```go
package main

import (
    "context"
    "database/sql"
    "log"
    "time"

    _ "github.com/jackc/pgx/v5/stdlib" // or your database/sql driver
    "github.com/busyminds/gxsql"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    db, err := sql.Open("pgx", "postgres://localhost/mydb?sslmode=disable")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    suite := gxsql.NewSuite(
        gxsql.RowCount().GreaterOrEqual(1),
        gxsql.Int("age").Between(0, 120),
        gxsql.String("email").NotEmpty(),
        gxsql.Column("id").Unique(),
    )

    report, err := suite.ValidateTable(ctx, db, gxsql.Table("users"),
        gxsql.WithDialect(gxsql.Postgres()),
        gxsql.WithKey("id"),
    )
    if err != nil {
        log.Fatalf("gxsql execution error: %v", err)
    }
    if err := report.Err(); err != nil {
        log.Fatalf("data quality check failed: %v", err)
    }
}
```

## Scoped validation

Use `TrustedScope` with `WithScope` to limit every expectation to rows matching
a caller-defined predicate. Predicate text is trusted Go-code input, not an
untrusted SQL sandbox: callers must not pass user-authored predicate text.
Keep predicate text fixed in Go, use `?` placeholders, and pass each dynamic
value as a separate argument so the dialect renderer and `database/sql` bind
the values without string interpolation. The following examples assume `ctx`,
`db`, and `suite` from the quick start:

```go
tenantID := "tenant-acme"
tenantScope := gxsql.TrustedScope("tenant-acme", "tenant_id = ?", tenantID)

tenantReport, err := suite.ValidateTable(ctx, db, gxsql.Table("users"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithScope(tenantScope),
)
if err != nil {
    log.Fatal(err)
}
if err := tenantReport.Err(); err != nil {
    log.Fatal(err)
}
```

```go
batchID := int64(42)
batchScope := gxsql.TrustedScope("batch-42", "batch_id = ?", batchID)

batchReport, err := suite.ValidateTable(ctx, db, gxsql.Table("events"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithScope(batchScope),
)
if err != nil {
    log.Fatal(err)
}
if err := batchReport.Err(); err != nil {
    log.Fatal(err)
}
```

Use a half-open time window (`>= start` and `< end`) with both bounds supplied
as separate values:

```go
start := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
end := time.Date(2025, time.January, 2, 0, 0, 0, 0, time.UTC)
windowScope := gxsql.TrustedScope(
    "events-2025-01-01",
    "event_at >= ? AND event_at < ?",
    start,
    end,
)

windowReport, err := suite.ValidateTable(ctx, db, gxsql.Table("events"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithScope(windowScope),
)
if err != nil {
    log.Fatal(err)
}
if err := windowReport.Err(); err != nil {
    log.Fatal(err)
}
```

`Report.ScopeID` and the exported JSON `scope.id` carry caller identity only;
they do not serialize the scope predicate text or bound arguments. Default
errors, `Report.String()` display output, and default `ExportReport` output omit
those scope fields. Ordinary samples and failed keys remain subject to the
usual report redaction guidance. For production validation, use a read-only
database role (ideally limited to validation views) and set a context deadline
on every `ValidateTable` call.

## Why gxsql

- **SQL-native validation** — expectations render to SQL and run in the database
  instead of loading whole tables into Go memory.
- **Collect-all results for data failures** — assertion failures do not stop
  later checks; one report holds every pass and failure. Execution errors stop
  evaluation unless `ContinueOnError()` is supplied.
- **Actionable failure reporting** — per-row checks include failed counts,
  capped sample values, and optional failed-row keys capped by default.
- **`database/sql` compatible** — works with any driver that satisfies the
  narrow `gxsql.DB` interface.
- **Standard-library-only core** — no third-party dependencies in the public
  API.
- **Test integration** — the `gxsqltest` subpackage provides `Check` and
  `Require` adapters for `*testing.T`.

## When to use gxsql

Use `gxsql` when you need to:

- Gate deployments or ETL jobs on database table quality.
- Audit production tables without pulling all rows into application memory.
- Run CI checks against integration-test databases.
- Collect every data-quality failure in one report instead of failing on the
  first check.

## When not to use gxsql

- **In-memory Go data** — `gxsql` validates database tables only; load rows into
  Go and validate in memory with a different approach.
- **Non-SQL stores** — `gxsql` validates tables through `database/sql` only.
- **Custom expectation types** — built-in expectations are constructed via the
  provided builders; `Expectation` is sealed and not an extension point.

## Dialect notes

- Built-in dialects are `gxsql.Postgres()`, `gxsql.SQLite()`, `gxsql.DuckDB()`,
  and `gxsql.MySQL()`.
- Pass `gxsql.WithDialect(...)` explicitly in production code, tests, and
  examples; `ValidateTable` defaults to PostgreSQL when no dialect is supplied.
- `gxsql.DuckDB()` renders double-quoted identifiers, `$1`, `$2`, …
  placeholders, and `LENGTH(expr)` for string-length checks. Import and open a
  compatible `database/sql` DuckDB driver yourself — `gxsql` does not bundle
  one.
- `gxsql.MySQL()` renders backtick-quoted identifiers, `?` placeholders, and
  `CHAR_LENGTH(expr)` for string-length checks. Import and open
  `github.com/go-sql-driver/mysql` or another compatible `database/sql` MySQL
  driver yourself — `gxsql` does not bundle one. The supported CI baseline is
  MySQL 8.4; MariaDB is not part of the supported matrix.
- String-length expectations use the dialect’s SQL length function, not Go rune
  counting.
- Other engines are possible only through a correct `Dialect` implementation;
  they are not part of the built-in dialect set.

## Real-engine conformance

CI runs one shared conformance kit against PostgreSQL 16, SQLite 3.50.4, DuckDB
1.5.4, and MySQL 8.4 using the integration-only drivers
`github.com/duckdb/duckdb-go/v2` (v2.10504.0) and
`github.com/go-sql-driver/mysql` (v1.10.0). The kit exercises identifier
qualification, bound placeholders, null and text/byte scans, single and
composite keys, ordering and diagnostic caps, empty targets, cancellation,
database/scan errors, `ContinueOnError`, and transaction-compatible `gxsql.DB`
handles. The fake driver remains for exact query-shape and deterministic
failure-path tests.

Run the SQLite fixture locally from the integration module:

```bash
cd integration
go test -race -run '^TestSQLiteConformance$' ./...
```

Run PostgreSQL conformance by supplying an isolated database:

```bash
cd integration
GXSQL_POSTGRES_DSN='postgres://user:password@localhost:5432/gxsql?sslmode=disable' \
  go test -race -run '^TestPostgresConformance$' ./...
```

Run DuckDB conformance locally from the integration module:

```bash
cd integration
go test -race -run '^TestDuckDBConformance$' ./...
```

## Core concepts

| Concept             | Description                                                                                                                                                                                                                                                            |
| ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Suite**           | An ordered set of expectations from `gxsql.NewSuite(...)`. Results appear in the same declaration order.                                                                                                                                                               |
| **Expectation**     | One data-quality assertion over a table, built with `RowCount`, `Column`, `Int`, `Float`, or `String`.                                                                                                                                                                 |
| **TableRef**        | Names the table under test: `gxsql.Table("users")` or `gxsql.SchemaTable("public", "users")`. Identifiers must match `^[A-Za-z_][A-Za-z0-9_]*$`.                                                                                                                       |
| **Dialect**         | Renders identifiers, placeholders, and string-length expressions. Built-in: `gxsql.Postgres()`, `gxsql.SQLite()`, `gxsql.DuckDB()`, and `gxsql.MySQL()`. `ValidateTable` defaults to PostgreSQL when no dialect is supplied; pass `gxsql.WithDialect(...)` explicitly. |
| **Report / Result** | A `Report` holds one `Result` per expectation. Use `report.OK()`, `report.Failures()`, `report.Err()`, and `report.String()` to gate and inspect outcomes.                                                                                                             |

Validation failures do **not** make `ValidateTable` return an error. The
returned `error` means SQL execution or configuration failed (invalid
identifiers, database errors, context cancellation, and similar). Use
`report.Err()` to gate on data quality.

## Expectation examples

```go
suite := gxsql.NewSuite(
    // Table-level row count
    gxsql.RowCount().Between(100, 10_000),

    // Per-row numeric checks
    gxsql.Int("age").Between(0, 120),
    gxsql.Float("score").GreaterOrEqual(0),

    // String checks
    gxsql.String("email").NotEmpty(),
    gxsql.String("code").LenBetween(3, 10),

    // Generic column checks
    gxsql.Column("status").NotNull(),
    gxsql.Column("status").In("active", "pending", "closed"),
    gxsql.Column("email").Unique(),
    gxsql.Column("country").DistinctCount().GreaterOrEqual(1),

    // Numeric aggregates (vacuous pass when the column is all NULL)
    gxsql.Int("amount").AverageBetween(0, 1_000),
    gxsql.Int("amount").MinGreaterOrEqual(0),
    gxsql.Int("amount").MaxLessOrEqual(1_000_000),
)
```

Per-row checks set `Total` to the table row count and populate `FailedCount`,
`FailedPercent`, `SampleValues`, and optionally `FailedKeys` on failure.
Table-level checks (row count, distinct count, aggregates) append observed
values to `Result.Name` (for example `row count >= 1: got 42`).

## Failed rows and reports

By default, per-row failures include failed counts and capped sample values
(`DefaultSampleCap` is 20). When neither `WithKey` nor `SummaryOnly()` is
supplied, `ValidateTable` uses summary-only mode internally (no complete
failed-row keys).

```go
// Counts plus capped samples only
report, err := suite.ValidateTable(ctx, db, gxsql.Table("users"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.SummaryOnly(),
)

// Record failing row identities by key columns (capped by DefaultFailedKeysCap = 100)
report, err := suite.ValidateTable(ctx, db, gxsql.Table("users"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithKey("id"),
    gxsql.WithSampleCap(5),
    gxsql.WithFailedKeysCap(0), // unlimited keys when every failing row is needed
)
```

Inspect failures after gating:

```go
if err := report.Err(); err != nil {
    var ve *gxsql.ValidationError
    if errors.As(err, &ve) {
        for _, res := range ve.Report.Failures() {
            fmt.Println(res.String())
        }
    }
}
```

Use `gxsql.ContinueOnError()` when you want database errors recorded on
individual `Result.Err` values while later expectations still run. Inspect
`report.Err()` and per-result errors — a nil top-level error is not success in
that mode.

## Testing with gxsqltest

The `gxsqltest` package adapts suite validation to Go's `testing` package:

```go
import (
    "context"
    "testing"

    "github.com/busyminds/gxsql"
    "github.com/busyminds/gxsql/gxsqltest"
)

func TestUsers(t *testing.T) {
    ctx := context.Background()
    // db and suite from setup...

    gxsqltest.Require(t, ctx, suite, db, gxsql.Table("users"),
        gxsql.WithDialect(gxsql.SQLite()),
    )
}
```

- `Check` reports failures with `t.Errorf`, continues the test, and returns
  `true` when every expectation passes.
- `Require` calls `t.Fatalf` on execution or validation failure and stops the
  test.

## Operational notes

`gxsql` executes SQL against the database. Each per-row expectation issues at
least two full-table `COUNT(*)` queries (total rows plus failing rows). Plan
query cost on large tables and set `context` deadlines on every `ValidateTable`
call.

| Control                | Default                    | Effect                                                        |
| ---------------------- | -------------------------- | ------------------------------------------------------------- |
| `WithSampleCap(n)`     | 20                         | Caps `SampleValues`                                           |
| `WithFailedKeysCap(n)` | 100                        | Caps `FailedKeys` when `WithKey` is set; zero means unlimited |
| `SummaryOnly()`        | implicit without `WithKey` | No failed-row keys loaded                                     |
| `WithKey(...)`         | off                        | Loads failing row keys (capped by default)                    |

**Key mode guidance:** `WithKey` suits low failure rates or when you need row
identities for remediation. On very large tables with widespread failures,
prefer `SummaryOnly()` or pass `WithFailedKeysCap(0)` only when you accept
unbounded key retention.

**`In` / `NotIn` list size:** each value becomes a bound placeholder. Lists in
the low thousands are usually fine; beyond that, split into multiple
expectations or use a lookup table join outside `gxsql`.

**Database privileges:** `ValidateTable` inherits the connection's permissions.
Use a read-only role scoped to validation views in production.

**Report output:** `Report.String()` and `gxsqltest.Check`/`Require` may embed
sample values in logs. Redact before shipping to observability backends when
columns may contain PII or secrets.

## Machine identity and export

Attach stable result IDs with `gxsql.WithID(id, expectation)` for CI/ETL joins.
IDs are optional for ad-hoc runs: when omitted, `Result.ID` stays empty and
export JSON omits the `id` field. Blank or duplicate IDs fail preflight before
SQL. Every built-in expectation exposes a library-defined `Kind` on `Result`.
Display `Name` text may change with observed values; `ID` and `Kind` stay stable
across equivalent runs.

Export encoding-only JSON for CI and audits:

```go
dto, err := gxsql.ExportReport(report,
    gxsql.IncludeSamples(),
    gxsql.IncludeFailedKeys(),
)
// dto.SchemaVersion == gxsql.ExportSchemaVersion ("gxsql.report.v1")
```

By default, `ExportReport` omits samples, failed keys, query text, and bound
arguments. `policy_verdict` is `pass` or `fail` only when `Result.Err == nil`;
any `Err` yields `unevaluated` (execution/config failure, not a data-quality
verdict). `execution_outcome` is unchanged. Configured thresholds export in
`facts.configured_*` keys; default `display_name` redacts bound literals.

Opt in with `IncludeSamples`, `IncludeFailedKeys`, `IncludeCapturedDiagnostics`
(redacted table-free SQL), and `IncludeCapturedArguments` (also enables
normalized capped args; requires `CaptureQueryDiagnostics()` at validate time).
Redactor failures fail closed with no partial JSON. v1 is **encode-only** — no
public decoder is promised.

See [API reference](docs/reference/README.md#exportreport) for export field
policy, value encodings, and privacy defaults.

## Migration notes (pre-v1)

| Change                                      | Action                                                                          |
| ------------------------------------------- | ------------------------------------------------------------------------------- |
| Machine consumption used `Result.Name` only | Wrap expectations with `WithID` and gate on `Kind`                              |
| Logs included sample values by default      | Use `ExportReport` privacy defaults; opt in to sensitive fields                 |
| Empty `In()` / `NotIn()`                    | Still configuration errors before SQL; use `ContinueOnError()` to collect slots |
| Tolerance / severity                        | Not yet implemented — deferred to Spec 07                                       |

## Documentation

- [Documentation index](docs/README.md)
- [Getting started tutorial](docs/tutorial/README.md)
- [Core concepts](docs/concepts/README.md)
- [API reference](docs/reference/README.md)
