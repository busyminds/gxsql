# Validate a Table

This guide validates an existing table with `gxsql` and PostgreSQL. The same
flow works with SQLite, DuckDB, or MySQL after selecting the matching dialect.

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

    _ "github.com/jackc/pgx/v5/stdlib"
)

ctx := context.Background()

db, err := sql.Open("pgx", "postgres://localhost/mydb?sslmode=disable")
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

For a local run, SQLite with `modernc.org/sqlite` is an option:

```go
import _ "modernc.org/sqlite"

db, err := sql.Open("sqlite", "file:example.db")
```

`gxsql` only needs `QueryContext` and `QueryRowContext`, so `*sql.DB` satisfies
its `DB` interface directly.

## Build a suite

A suite is an ordered collection of expectations. Each expectation becomes SQL
that checks table contents in the database:

```go
suite := gxsql.NewSuite(
    gxsql.RowCount().GreaterOrEqual(1),
    gxsql.Int("age").Between(0, 120),
    gxsql.String("email").NotEmpty(),
    gxsql.Column("email").Unique(),
)
```

Use the builders for the data type and assertion you need. The
[expectations reference](../reference/expectations.md) lists every builder.

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
`ValidateTable` defaults to PostgreSQL when no dialect is supplied, but passing
it explicitly keeps rendered SQL coupled to the selected driver.

`WithKey("id")` retains the identities of failing rows, up to the failed-key
cap. Omit it when counts and sample values are enough. See
[results and remediation](../concepts/results.md) for the retention controls.

## Understand the two outcomes

A completed validation has two independent outcomes:

| Signal                | Meaning                                                               |
| --------------------- | --------------------------------------------------------------------- |
| `err != nil`          | A configuration or SQL execution failure prevented a complete report. |
| `report.Err() != nil` | Validation completed, but at least one expectation failed.            |

`ValidateTable` collects all policy failures in declaration order. It stops on
configuration and execution failures by default. `ContinueOnError()` instead
records those failures in the affected `Result` and evaluates later
expectations. See
[validation behavior](../concepts/validation.md#error-handling) for the complete
error model.

## Next

- [Use gxsql in Go tests](testing.md)
- [Learn validation behavior and dialects](../concepts/validation.md)
- [Inspect reports and remediate failures](../concepts/results.md)
- [Browse the API reference](../reference/)
