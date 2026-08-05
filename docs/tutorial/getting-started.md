# Validate a Table

Use this guide to validate an existing table with `gxsql` and PostgreSQL. The
same flow works with SQLite, DuckDB, or MySQL after you select the matching
dialect.

## Install

```bash
go get github.com/busyminds/gxsql
```

`gxsql` requires Go 1.24 or later. It does not bundle a database driver.

## Open a database

Open `*sql.DB` with the driver and connection settings your application already
uses. For PostgreSQL with `pgx`:

```go
import (
    "context"
    "database/sql"
    "log"
    "time"

    _ "github.com/jackc/pgx/v5/stdlib"
)

ctx := context.Background()

db, err := sql.Open("pgx", "postgres://localhost/mydb?sslmode=disable")
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

For a local run, open SQLite with `modernc.org/sqlite`:

```go
import _ "modernc.org/sqlite"

db, err := sql.Open("sqlite", "file:example.db")
```

`gxsql` needs only `QueryContext` and `QueryRowContext`. `*sql.DB` satisfies the
`DB` interface directly.

## Build a suite

A suite is an ordered collection of expectations. Each expectation becomes SQL
that checks table contents in the database:

```go
suite := gxsql.NewSuite(
    gxsql.RowCount().GreaterOrEqual(1),
    gxsql.Int("age").Between(0, 120),
    gxsql.String("email").NotEmpty(),
    gxsql.Column("email").Unique(),
    gxsql.Columns("tenant_id", "order_id").Unique(),
    gxsql.Columns("tenant_id", "customer_id").References(
        gxsql.SchemaTable("public", "customers"), "tenant_id", "id",
    ),
)
```

Composite uniqueness ignores tuples with any SQL `NULL` component. It counts
every duplicate participating **row**. Referential checks pass rows with any
`NULL` local key component. Those checks count orphaned complete local tuples.
Referential checks look up parents without applying local `WithScope`. Samples
and failed keys stay local under existing caps.

For ordering and simple reconciliation, use the fixed same-row builders instead
of raw operators or expressions:

```go
suite := gxsql.NewSuite(
    gxsql.Column("end_date").GreaterOrEqualColumn("start_date"),
    gxsql.Column("paid_cents").LessOrEqualColumn("invoice_cents"),
    gxsql.Int("actual_units").RatioEqual("planned_units", 2),
)
```

Comparisons require both operands to be non-`NULL`. An empty scoped population
passes vacuously. Direct comparisons cover like-for-like integer, numeric, or
temporal columns without coercion. `RatioEqual` checks
`actual_units == planned_units * 2` algebraically, not through SQL division. It
fails a zero denominator and supports integers only. Decimal ratios, floating
ratios, and arbitrary expressions are unsupported.


Timestamp window and freshness checks take caller-supplied `time.Time` values.
The window is half-open (`start <= value < end`); NULL fails and an empty scope
passes vacuously. Freshness requires `MAX(column) >= cutoff`; empty and
all-NULL scopes fail, while a future-valued maximum still passes against that
explicit cutoff:

```go
windowStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
windowEnd := windowStart.Add(24 * time.Hour)
cutoff := windowEnd.Add(-30 * time.Minute)

suite := gxsql.NewSuite(
    gxsql.Timestamp("event_time").InWindow(windowStart, windowEnd),
    gxsql.Timestamp("ingested_at").FreshSince(cutoff),
)
```

Use the builders for the data type and assertion you need. The
[expectations reference](../reference/expectations.md) lists every builder.

### Gate table shape first

Before content checks, confirm the target still exposes the expected columns.
`RequiredColumns` and `ExactColumns` compare unordered physical column-name
sets byte-for-byte against `Rows.Columns()`. They do not validate types,
nullability, or column order. Run them in a separate unscoped suite; `WithScope`
is rejected at preflight:

```go
structure := gxsql.NewSuite(
    gxsql.RequiredColumns("id", "event_time", "payload"),
    gxsql.ExactColumns("id", "event_time", "payload"),
)
structureReport, err := structure.ValidateTable(ctx, db, gxsql.Table("ingest_events"),
    gxsql.WithDialect(gxsql.Postgres()),
)
if err != nil {
    // Missing target, permission denial, or other typed execution/preflight error.
    log.Fatalf("structural discovery error: %v", err)
}
if err := structureReport.Err(); err != nil {
    // Missing or unexpected column names; inspect Result.Facts.
    log.Fatalf("structural check failed: %v", err)
}
```

Discovery is a read-only zero-row probe. Missing and unexpected names appear as
ordinary table-level results with structured facts; they never include samples
or failed keys.

## Run validation

Pass a table reference and the dialect matching the database behind `db`:

```go
report, err := suite.ValidateTable(ctx, db, gxsql.Table("users"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithKey("id"),
)
if err != nil {
    // Configuration or execution error; no complete report is available.
    log.Fatalf("gxsql execution error: %v", err)
}
if err := report.Err(); err != nil {
    // Expectations ran, but one or more data-quality policies failed.
    log.Fatalf("data quality check failed: %v", err)
}
```

Use `gxsql.SQLite()`, `gxsql.DuckDB()`, or `gxsql.MySQL()` for those engines.
`ValidateTable` defaults to PostgreSQL when no dialect is supplied. Pass the
dialect explicitly so rendered SQL stays coupled to the selected driver.

`WithKey("id")` retains the identities of failing rows, up to the failed-key
cap. Omit it when counts and sample values are enough. See
[results and remediation](../concepts/results.md) for the retention controls.

## Understand the two outcomes

A completed validation has two independent outcomes:

| Signal                | Meaning                                                              |
| --------------------- | -------------------------------------------------------------------- |
| `err != nil`          | A configuration or SQL execution failure prevented a complete report |
| `report.Err() != nil` | Validation completed, but at least one expectation failed            |

`ValidateTable` collects all policy failures in declaration order. It stops on
configuration and execution failures by default. `ContinueOnError()` records
those failures in the affected `Result` and evaluates later expectations. See
[validation behavior](../concepts/validation.md#error-handling) for the complete
error model.

## Next

- [Use gxsql in Go tests](testing.md)
- [Learn validation behavior and dialects](../concepts/validation.md)
- [Inspect reports and remediate failures](../concepts/results.md)
- [Browse the API reference](../reference/)
