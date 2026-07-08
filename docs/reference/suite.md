# Suites, Options, and SQL Integration

## Suite

`Suite` is an ordered set of SQL expectations. Create it with `NewSuite`; its
fields are unexported.

```go
suite := gxsql.NewSuite(
    gxsql.RowCount().GreaterOrEqual(1),
    gxsql.String("email").NotEmpty(),
)
```

| API                                                               | Description                                                           |
| ----------------------------------------------------------------- | --------------------------------------------------------------------- |
| `NewSuite(exps ...Expectation) *Suite`                            | Creates an ordered suite with the default sample and failed-key caps. |
| `(*Suite).WithSampleCap(n int) *Suite`                            | Sets the suite default sample cap; `0` disables sample collection.    |
| `(*Suite).WithFailedKeysCap(n int) *Suite`                        | Sets the suite default failed-key cap; `0` is unlimited.              |
| `(*Suite).ValidateTable(ctx, db, table, opts...) (Report, error)` | Runs every expectation and returns its aggregated report.             |

`ValidateTable` returns `(report, nil)` for failed validation policies. Gate on
`report.OK()` or `report.Err()`. It returns `(Report{}, err)` for run-level,
preflight, or execution errors unless `ContinueOnError()` handles a
per-expectation failure in the report.

## Options

`Option` is an opaque function configuring one validation run. Per-run options
override suite-level caps.

| Option                       | Effect                                                                        |
| ---------------------------- | ----------------------------------------------------------------------------- |
| `WithDialect(d Dialect)`     | Selects the SQL renderer. Defaults to `Postgres()`.                           |
| `WithSampleCap(n int)`       | Overrides the maximum retained sample values; `0` disables sample collection. |
| `WithFailedKeysCap(n int)`   | Overrides the maximum retained failed keys; `0` is unlimited.                 |
| `WithKey(columns ...string)` | Retains supplied row-key columns and disables summary-only mode.              |
| `SummaryOnly()`              | Does not load failed-row identities.                                          |
| `ContinueOnError()`          | Records preflight and execution errors on results and continues.              |
| `CaptureQueryDiagnostics()`  | Records SQL and arguments for optional export only.                           |

When neither `WithKey` nor `SummaryOnly` is supplied, results contain counts and
capped samples but no failed-row identities. Invalid run-level options—such as a
nil dialect, negative caps, or invalid key columns—always prevent evaluation.

## Test helpers

The `github.com/busyminds/gxsql/gxsqltest` package adapts validation to Go
tests. Its `TestingT` interface is the `Helper`, `Errorf`, and `Fatalf` subset
shared by `*testing.T` and `*testing.B`.

| API                                                       | Behavior                                                                                                         |
| --------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `gxsqltest.Check(t, ctx, suite, db, table, opts...) bool` | Calls `t.Errorf` for execution or policy failure and continues. Returns true only when every expectation passes. |
| `gxsqltest.Require(t, ctx, suite, db, table, opts...)`    | Calls `t.Fatalf` for execution or policy failure.                                                                |

Both helpers accept the same options as `ValidateTable`.

## Database and dialects

`DB` is the narrow query interface that `ValidateTable` needs:

```go
type DB interface {
    QueryContext(context.Context, string, ...any) (*sql.Rows, error)
    QueryRowContext(context.Context, string, ...any) *sql.Row
}
```

`*sql.DB` satisfies it. `gxsql` does not open connections or provide drivers.

A `Dialect` supplies identifier quoting, placeholders, and string-length SQL.
The built-ins validate identifiers in `QuoteIdent`.

| Constructor  | Identifier quoting | Placeholders  | String length       |
| ------------ | ------------------ | ------------- | ------------------- |
| `Postgres()` | double quotes      | `$1`, `$2`, … | `CHAR_LENGTH(expr)` |
| `SQLite()`   | double quotes      | `?`           | `LENGTH(expr)`      |
| `DuckDB()`   | double quotes      | `$1`, `$2`, … | `LENGTH(expr)`      |
| `MySQL()`    | backticks          | `?`           | `CHAR_LENGTH(expr)` |

## Table references

`TableRef` holds exported `Schema` and `Name` fields. Construct one with
`Table(name)` for an unqualified table or `SchemaTable(schema, name)` for a
schema-qualified table. Built-in dialects reject empty identifiers and those
outside `^[A-Za-z_][A-Za-z0-9_]*$` when rendering.

## Expectation

`Expectation` appears in public signatures but is sealed. Its unexported
`evaluateSQL` method and unexported option type prevent implementations outside
package `gxsql`. Use the builders in the
[expectations reference](expectations.md).
