# gxsql

> **Note:** The features and direction of `gxsql` are defined and guided by me,
> while almost all of the code has been generated with AI assistance. It works
> for me and is used in my own production projects. Contributions are very
> welcome — please feel free to open a pull request.

`gxsql` is a SQL-native data quality assertion framework for Go. It validates
database tables through `database/sql`, renders each expectation as SQL, and
runs those checks in the database instead of loading whole tables into
application memory. Validation is collect-all: every expectation runs in
declaration order, and one report captures all passes and policy failures.
Configuration and execution errors are separate. By default they stop evaluation
and return an error. Use `gxsql.ContinueOnError()` when later expectations must
still run after per-expectation database errors.

## Install

```bash
go get github.com/busyminds/gxsql
```

`gxsql` requires Go 1.24 or newer.

The core package is driver-neutral and has no runtime dependencies outside the
Go standard library. You own the driver: open a `*sql.DB` with your
`database/sql` driver, then pass `gxsql.WithDialect(...)` explicitly so the
rendered SQL matches that engine. The examples below use
`github.com/jackc/pgx/v5/stdlib`, `modernc.org/sqlite`,
`github.com/duckdb/duckdb-go/v2`, and `github.com/go-sql-driver/mysql` only as
conformance and integration drivers.

## Support Matrix

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

| Area         | Level     | Floor / active coverage                                            | Notes                                                                         |
| ------------ | --------- | ------------------------------------------------------------------ | ----------------------------------------------------------------------------- |
| Go toolchain | supported | minimum Go 1.24; actively tested on Go 1.24.x and 1.26.x           | required to build and run the module                                          |
| Ubuntu       | supported | `ubuntu-24.04` in CI                                               | first-class CI target                                                         |
| PostgreSQL   | supported | PostgreSQL 16 in CI via `github.com/jackc/pgx/v5/stdlib`           | built-in `gxsql.Postgres()`; the driver is a conformance-only test dependency |
| SQLite       | supported | SQLite 3.50.4 in CI via `modernc.org/sqlite` v1.39.1               | built-in `gxsql.SQLite()`; the driver is a conformance-only test dependency   |
| DuckDB       | supported | DuckDB 1.5.4 in CI via `github.com/duckdb/duckdb-go/v2` v2.10504.0 | built-in `gxsql.DuckDB()`; the driver is a conformance-only test dependency   |
| MySQL        | supported | MySQL 8.4 in CI via `github.com/go-sql-driver/mysql` v1.10.0       | built-in `gxsql.MySQL()`; the driver is a conformance-only test dependency    |

`gxsql` is intentionally driver-neutral: the core package validates against a
caller-selected `database/sql` driver, while PostgreSQL, SQLite, DuckDB, and
MySQL appear in the matrix as CI conformance paths rather than bundled runtime
dependencies.

## Example Entry Points

The most common entry points are below and are expanded in the rest of this
README:

1. `ValidateTable` quick start with explicit dialect selection.
2. Report gating with `report.Err()` / `report.Failures()` after a completed
   run.
3. `gxsqltest.Check` and `gxsqltest.Require` for `testing.T`.
4. `ExportReport` for machine-readable JSON export.
5. `TrustedCountQuery` / `CustomCount` for portable join and aggregate counts.
6. `WithPolicy` and `WithMaxFailedCount` for severity, metadata, and failed-row
   allowances.
7. `Timestamp(...).InWindow(...)` and `Timestamp(...).FreshSince(...)` for
   caller-supplied temporal windows and freshness cutoffs.
8. `RequiredColumns(...)` and `ExactColumns(...)` for portable structural
   column-set gates before content validation.
9. `When` / `TrustedEligibility` for rule-level eligibility distinct from suite
   scope, plus ordinary Go policy-pack composition with `WithID`.

## Quick Start

Open the database connection yourself and select the dialect that matches that
driver. A returned `error` from `ValidateTable` is a configuration or execution
failure. Use `report.Err()` to gate on data-quality (policy) failures after a
completed run:

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
        gxsql.Columns("tenant_id", "order_id").Unique(),
        gxsql.Columns("tenant_id", "customer_id").References(
            gxsql.SchemaTable("public", "customers"), "tenant_id", "id",
        ),
    )

    report, err := suite.ValidateTable(ctx, db, gxsql.Table("orders"),
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

## Scoped Validation

Use `TrustedScope` with `WithScope` to limit every expectation to rows that
match a caller-defined predicate. The predicate is trusted Go-code input, not a
sandbox for untrusted SQL. Do not pass user-authored predicate text. Keep the
predicate text fixed in Go, use `?` placeholders, and pass each dynamic value as
a separate argument. The dialect renderer and `database/sql` bind those values;
do not interpolate them into the predicate. The examples below assume `ctx`,
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

`Report.ScopeID` and the exported JSON `scope.id` carry caller identity only.
They do not serialize the scope predicate text or bound arguments. Default
errors, `Report.String()` display output, and default `ExportReport` output omit
those scope fields. Ordinary samples and failed keys still need the usual report
redaction. For production validation, use a read-only database role that is
restricted to validation tables or views, and set a context deadline on every
`ValidateTable` call.

## Rule Eligibility Versus Suite Scope

Suite `TrustedScope` / `WithScope` selects the shared population for a run.
`TrustedEligibility` with `When` narrows which rows inside that population are
subject to one expectation. Keep the concepts separate: eligibility does not
rewrite `Report.ScopeID`, and it is not a second suite scope.

```go
shippedAtPresent := gxsql.When(
    gxsql.TrustedEligibility("status-shipped", "status = ?", "shipped"),
    gxsql.Column("shipped_at").NotNull(),
)

eligReport, err := gxsql.NewSuite(shippedAtPresent).ValidateTable(ctx, db, gxsql.Table("orders"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithScope(gxsql.TrustedScope("tenant-acme", "tenant_id = ?", tenantID)),
)
if err != nil {
    log.Fatal(err)
}
if err := eligReport.Err(); err != nil {
    log.Fatal(err)
}
```

When both are present, SQL applies suite scope and eligibility as independent
conjuncts with bindings in suite-scope, eligibility, then expectation order.
Eligible-row count is the denominator for percentages and policy tolerance.
Ineligible rows neither pass nor fail the wrapped rule. Zero eligible rows pass
vacuously with no fabricated percentage and `Tolerated == false`.

`When` wraps exactly one expectation. Nested eligibility fails preflight.
Supported shapes are ordinary per-row, uniqueness, composite uniqueness, and
referential integrity. Table-level, aggregate, distinct-count, custom-count, and
structural expectations reject eligibility at preflight. Default errors, display
output, and `ExportReport` omit eligibility predicate text and bound arguments.

## Policy Packs

A policy pack is an ordinary Go function that returns a fresh `[]Expectation`.
Concatenate packs and local rules in declaration order, then pass the flattened
list to `NewSuite`. There is no pack registry.

```go
func OrderIntegrityPack(prefix string) []gxsql.Expectation {
    return []gxsql.Expectation{
        gxsql.WithID(prefix+".id.present", gxsql.String("id").NotEmpty()),
        gxsql.WithID(prefix+".id.unique", gxsql.Column("id").Unique()),
        gxsql.WithID(prefix+".shipped_at.present", gxsql.When(
            gxsql.TrustedEligibility("status-shipped", "status = ?", "shipped"),
            gxsql.Column("shipped_at").NotNull(),
        )),
    }
}

suite := gxsql.NewSuite(append(
    OrderIntegrityPack("acme.orders"),
    gxsql.RowCount().GreaterOrEqual(1),
)...)
```

Each pack call must return independent values; mutating a returned slice must
not affect a later call. Flattened order is pack order, then declaration order
within each pack, then caller-appended expectations. A composed suite must match
the identical flat list written by hand. Prefer reverse-domain or pack-prefix
stable IDs such as `acme.orders.id.present`. Blank and duplicate IDs fail
preflight before SQL; with `ContinueOnError()` they occupy declaration-order
slots. Library `Kind` values are not caller IDs. Reuse completed packs and
suites concurrently only after configuration is finished and nothing mutates
during `ValidateTable`.

## Structural Column Contracts

Use `RequiredColumns` and `ExactColumns` to catch migration or producer drift
before content checks hide the cause. Both compare unordered column-name sets
against dialect- and driver-reported `Rows.Columns()` spellings byte-for-byte,
with no case folding. Column order never changes the verdict. Discovery is a
read-only zero-row probe and never scans row values. A missing target or
permission denial is an execution error. Missing or unexpected names are policy
failures on the completed report.

```go
structure := gxsql.NewSuite(
    gxsql.RequiredColumns("id", "event_time", "payload"),
    gxsql.ExactColumns("id", "event_time", "payload"),
)
structureReport, err := structure.ValidateTable(ctx, db, gxsql.Table("ingest_events"),
    gxsql.WithDialect(gxsql.Postgres()),
)
if err != nil {
    log.Fatal(err) // missing target, permission denial, or other typed error
}
if err := structureReport.Err(); err != nil {
    log.Fatal(err) // missing or unexpected column names
}
```

`RequiredColumns` allows additional discovered names. `ExactColumns` requires an
exact set match. Missing names are ordered by caller declaration; unexpected
names are ordered by discovery. Results use `RowDenominatorUnavailable` and
never retain samples or failed keys. These builders do not validate types,
nullability, or ordinal position. `WithScope` is incompatible and fails
preflight; run structural checks in a separate unscoped suite.

For catalog contracts, use `ColumnNullability` and `ColumnType`:

```go
schema := gxsql.NewSuite(
    gxsql.ColumnNullability("email").Nullable(),
    gxsql.ColumnType("id").ReportedAs("BIGINT"),
)
```

These contracts use `Rows.ColumnTypes()` after the same zero-row probe. Type
names compare byte-for-byte with driver-reported spellings; gxsql does not
equate type names across dialects. Nullability is advertised only by dialects
with a supported metadata path. Unsupported or unknown metadata fails closed.
Catalog contracts are table-level and do not replace content `NotNull` or
`IsNull` checks.

## Custom Count Checks

Use `TrustedCountQuery` and `CustomCount` when a built-in expectation cannot
express the rule but a trusted SQL count can. Template SQL is Go-code input that
your team reviews; it is not a sandbox for untrusted SQL. Never insert
user-authored SQL into templates or interpolate identifiers or values into
template text.

The library renders `{{target}}` from the validated `TableRef` and `{{scope}}`
from `WithScope` (or `TRUE` when unscoped). A template must contain exactly one
`{{target}}` and one `{{scope}}`, both outside SQL strings and comments. Place
both markers in syntactically valid SQL and qualify scope column references when
the query uses table aliases. Custom `?` placeholders must come after
`{{scope}}`; bound argument order is scope arguments first, then custom
arguments. Preflight rejects invalid markers, placeholder placement, and arity
mismatches before any custom-count SQL runs.

The query must return one row and one non-negative signed-integer count; textual
numerics are not coerced. Results use `KindCustom` with a complete `FailedCount`
and `RowDenominatorUnavailable`; samples and failed keys are unavailable.

```go
joinCount := gxsql.TrustedCountQuery(`SELECT COUNT(*)
FROM {{target}} AS o
JOIN accounts AS a ON a.id = o.account_id
WHERE {{scope}} AND a.status = ?`, "inactive")

suite := gxsql.NewSuite(gxsql.CustomCount("inactive account orders", joinCount))
report, err := suite.ValidateTable(ctx, db, gxsql.Table("order_lines"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithScope(gxsql.TrustedScope("tenant-a", "o.tenant_id = ?", tenantID)),
)
```

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

## When to Use gxsql

Use `gxsql` when you need to:

- Gate deployments or ETL jobs on database table quality.
- Audit production tables without pulling all rows into application memory.
- Run CI checks against integration-test databases.
- Collect every data-quality failure in one report instead of failing on the
  first check.

## When Not to Use gxsql

- **In-memory Go data** — `gxsql` validates database tables only; load rows into
  Go and validate in memory with a different approach.
- **Non-SQL stores** — `gxsql` validates tables through `database/sql` only.
- **Custom expectation types** — built-in expectations are constructed via the
  provided builders; `Expectation` is sealed and not an extension point.

## Dialect Notes

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

## Real-Engine Conformance

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
CGO_ENABLED=1 go test -race -run '^TestDuckDBConformance$' ./...
```

Run MySQL conformance by supplying an isolated database:

```bash
cd integration
GXSQL_MYSQL_DSN='user:password@tcp(localhost:3306)/gxsql?parseTime=true' \
  go test -race -run '^TestMySQLConformance$' ./...
```

## Core Concepts

| Concept             | Description                                                                                                                                                                                                                                                            |
| ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Suite**           | An ordered set of expectations from `gxsql.NewSuite(...)`. Results appear in the same declaration order.                                                                                                                                                               |
| **Expectation**     | One data-quality assertion over a table, built with `RowCount`, `RequiredColumns`, `ExactColumns`, `Column`, `Int`, `Float`, `String`, `Timestamp`, or `CustomCount`.                                                                                                  |
| **TableRef**        | Names the table under test: `gxsql.Table("users")` or `gxsql.SchemaTable("public", "users")`. Identifiers must match `^[A-Za-z_][A-Za-z0-9_]*$`.                                                                                                                       |
| **Dialect**         | Renders identifiers, placeholders, and string-length expressions. Built-in: `gxsql.Postgres()`, `gxsql.SQLite()`, `gxsql.DuckDB()`, and `gxsql.MySQL()`. `ValidateTable` defaults to PostgreSQL when no dialect is supplied; pass `gxsql.WithDialect(...)` explicitly. |
| **Report / Result** | A `Report` holds one `Result` per expectation. Use `report.OK()`, `report.Failures()`, `report.Err()`, and `report.String()` to gate and inspect outcomes.                                                                                                             |
| **Eligibility**     | `TrustedEligibility` + `When` narrow which scoped rows one expectation evaluates. Distinct from suite `WithScope`; does not rewrite `Report.ScopeID`.                                                                                                                   |
| **Policy pack**     | An ordinary Go function returning a fresh `[]Expectation`. Concatenate packs with local rules; declaration order and `WithID` conventions stay caller-owned.                                                                                                           |

Policy failures do **not** make `ValidateTable` return an error. The returned
`error` means configuration or SQL execution failed (invalid identifiers,
database errors, or context cancellation). Use `report.Err()` to gate on data
quality.

## Expectation Examples

```go
windowStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
windowEnd := windowStart.Add(24 * time.Hour)
cutoff := windowEnd.Add(-30 * time.Minute)

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

    // Timestamp window and freshness (caller-supplied time.Time only)
    gxsql.Timestamp("event_time").InWindow(windowStart, windowEnd),
    gxsql.Timestamp("ingested_at").FreshSince(cutoff),
)

// Structural shape gates belong in a separate unscoped suite:
structure := gxsql.NewSuite(
    gxsql.RequiredColumns("id", "event_time", "payload"),
    gxsql.ExactColumns("id", "event_time", "payload"),
)
```

`InWindow` is half-open (`start <= value < end`): NULL fails and an empty scope
passes vacuously. `FreshSince` requires `MAX(column) >= cutoff`; empty and
all-NULL scopes fail, while a future-valued maximum still passes against that
explicit cutoff. gxsql never embeds database current-time SQL.

Per-row checks set `Total` to the table row count and populate `FailedCount`,
`FailedPercent`, `SampleValues`, and optionally `FailedKeys` on failure.
Table-level checks (row count, distinct count, aggregates, freshness) append
observed values to `Result.Name` (for example `row count >= 1: got 42`).
Structural column results publish ordered missing and unexpected names in
`Result.Facts` instead of samples or failed keys.

## Failed Rows and Reports

By default, per-row failures include failed counts and capped sample values
(`DefaultSampleCap` is 20). When neither `WithKey` nor `SummaryOnly()` is
supplied, `ValidateTable` uses summary-only mode internally and does not load
complete failed-row keys.

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

## Policy Decoration and Tolerance

Use `WithPolicy(exp, Policy)` to add severity, metadata, and an optional
`MaxFailedPercent` allowance:

```go
suite := gxsql.NewSuite(
    gxsql.WithPolicy(
        gxsql.String("email").NotEmpty(),
        gxsql.Policy{
            Severity:    gxsql.SeverityWarning,
            Description: "Customer email must be present",
            Tags:        []string{"customer", "pii"},
            Tolerance:   gxsql.MaxFailedPercent(0.5),
        },
    ),
)
```

`MaxFailedPercent(p)` is inclusive, compares the unrounded
`FailedCount / Total * 100` ratio, and accepts `p` in `[0, 100]`. It applies to
denominator-available per-row, uniqueness, and referential-integrity
expectations. `WithMaxFailedCount` remains the inclusive count form with its
existing eligibility and behavior. A policy accepts at most one tolerance form.

Tolerance changes only the policy verdict. Raw `Total`, `FailedCount`,
`FailedPercent`, samples, and failed keys remain complete under normal caps.
Empty evaluated populations pass without division by zero or `NaN` and are not
tolerated. Configuration and execution errors always gate and are never
tolerated.

`SeverityError` is the zero severity. Warning and info policy failures remain
queryable in `Report.Results` but do not make `report.OK()` false or
`report.Err()` non-nil. Other severity values are treated as gating failures.
Use `GatingFailures`, `PolicyFailures`, `Warnings`, `Infos`, `Unexpected`,
`ToleratedResults`, and `ExecutionFailures` to select outcomes. `Failures()`
continues to return every non-success result.

Descriptions are trimmed and blank values are omitted. Tags are trimmed, sorted,
copied, and rejected when blank or duplicated. Metadata never changes
`Result.ID` or `Result.Kind`. Export includes policy fields and configured
thresholds without changing privacy defaults: samples, failed keys, and captured
arguments are omitted by default; when callers opt them in, they contain
normalized values unless the corresponding sample, key, or args redactor is
supplied. Captured SQL diagnostics redact the validated target identifier by
default; use the query redactor when broader SQL redaction is required.

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

- `Check` reports an execution/configuration error or a hard-gating policy
  failure with `t.Errorf`, continues the test, and returns `true` when no
  hard-gating failure exists.
- `Require` calls `t.Fatalf` on an execution/configuration error or a
  hard-gating policy failure and stops the test.

## Operational Notes

`gxsql` executes SQL against the database. Per-row checks share one population
`COUNT(*)` during a validation run. Without `WithSharedScalarEvaluation()`, each
check then runs one failure-count query. Failures can add sample and failed-key
queries. Plan query cost on large tables. Set a deadline on every
`ValidateTable` context.

To combine contiguous compatible per-row failure counts into fewer statements,
pass `WithSharedScalarEvaluation()`. The option is off by default and does not
change published semantic report fields. See
[operational limits](docs/concepts/operations.md#use-shared-scalar-evaluation)
for cost planning, compatibility limits, and diagnostics attribution.

| Control                | Default                    | Effect                                                        |
| ---------------------- | -------------------------- | ------------------------------------------------------------- |
| `WithSampleCap(n)`     | 20                         | Caps `SampleValues`                                           |
| `WithFailedKeysCap(n)` | 100                        | Caps `FailedKeys` when `WithKey` is set; zero means unlimited |
| `SummaryOnly()`        | implicit without `WithKey` | No failed-row keys loaded                                     |
| `WithKey(...)`         | off                        | Loads failing row keys (capped by default)                    |

**Result retention:** Use `WithKey` when failure rates are low or when you need
row identities for remediation. Prefer `SummaryOnly()` for widespread failures
on large tables. Pass `WithFailedKeysCap(0)` only when you accept unbounded key
retention.

**`In` / `NotIn` lists:** Each value becomes a bound placeholder. Lists in the
low thousands are generally practical. For larger domains, validate through a
lookup-table join outside `gxsql`. Do not split a `NotIn` domain across multiple
expectations; each expectation would exclude only its own values and would
change the policy.

**Database privileges:** `ValidateTable` inherits the connection's permissions.
Use a read-only role restricted to validation tables or views in production.

**Report output:** `Report.String()` and `gxsqltest.Check` / `Require` may embed
sample values in logs. Redact before you send that output to observability
systems when columns may hold PII or secrets.

## Machine Identity and Export

Attach stable result IDs with `gxsql.WithID(id, expectation)` for CI/ETL joins.
IDs are optional for ad-hoc runs: when omitted, `Result.ID` stays empty and
export JSON omits the `id` field. Blank or duplicate IDs fail preflight before
SQL, including duplicates that appear only after packs are concatenated. With
`ContinueOnError()`, duplicate-ID failures occupy declaration-order slots
without ambiguous exported identities for executed siblings. Prefer
reverse-domain or pack-prefix paths such as `acme.orders.id.present`. Never
derive machine IDs from descriptions or tags. Every built-in expectation exposes
a library-defined `Kind` on `Result`; `Kind` values are not caller IDs. Display
`Name` text may change with observed values; `ID` and `Kind` stay stable across
equivalent runs. Default `ExportReport` continues to omit samples, keys, SQL,
arguments, and eligibility or scope predicate text.

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

Opt in with `IncludeSamples`, `IncludeFailedKeys`, `IncludeCapturedDiagnostics`,
and `IncludeCapturedArguments` (requires `CaptureQueryDiagnostics()` at validate
time). Opted-in samples, failed keys, and arguments contain normalized values
unless `WithSampleRedactor`, `WithKeyRedactor`, or `WithArgsRedactor` is
supplied. Captured SQL redacts the validated target identifier by default; use
`WithQueryRedactor` when broader SQL redaction is required. Redactor failures
fail closed with no partial JSON. v1 is **encode-only** — no public decoder is
promised.

See [stable IDs and report export](docs/reference/export.md#exportreport) for
export field policy, value encodings, and privacy defaults.

## Migration Notes (Pre-v1)

| Change                                      | Action                                                                          |
| ------------------------------------------- | ------------------------------------------------------------------------------- |
| Machine consumption used `Result.Name` only | Wrap expectations with `WithID` and gate on `Kind`                              |
| Logs included sample values by default      | Use `ExportReport` privacy defaults; opt in to sensitive fields                 |
| Empty `In()` / `NotIn()`                    | Still configuration errors before SQL; use `ContinueOnError()` to collect slots |
| Exact-zero failure gating only              | Wrap eligible per-row or uniqueness expectations with `WithMaxFailedCount`      |
| Warning / info severity                     | Not implemented                                                                 |

## Documentation

- [Documentation index](docs/README.md)
- [Getting started tutorial](docs/tutorial/README.md)
- [Core concepts](docs/concepts/README.md)
- [API reference](docs/reference/README.md)
