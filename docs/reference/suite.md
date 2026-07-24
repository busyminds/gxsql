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

| Option                                                      | Effect                                                                                                  |
| ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `WithDialect(d Dialect)`                                   | Selects the SQL renderer. Defaults to `Postgres()`.                                                     |
| `WithSampleCap(n int)`                                     | Overrides the maximum retained sample values; `0` disables sample collection.                           |
| `WithFailedKeysCap(n int)`                                 | Overrides the maximum retained failed keys; `0` is unlimited.                                           |
| `WithKey(columns ...string)`                               | Retains supplied row-key columns and disables summary-only mode.                                        |
| `SummaryOnly()`                                            | Does not load failed-row identities.                                                                    |
| `ContinueOnError()`                                        | Records preflight and execution errors on results and continues.                                        |
| `CaptureQueryDiagnostics()`                                | Records SQL and arguments for optional export only.                                                     |
| `WithScope(scope Scope)`                                   | Limits every expectation to rows matching the scope predicate; validates the scope when the run starts. |

When neither `WithKey` nor `SummaryOnly` is supplied, results contain counts and
capped samples but no failed-row identities. Invalid run-level options—such as a
nil dialect, negative caps, invalid key columns, or invalid scopes—always
prevent evaluation.

## Scoped validation

`TrustedScope(id, predicate string, args ...any) Scope` constructs a `Scope`; it
is not an `Option`. Attach the returned scope to a run with `WithScope`.

`TrustedScope` predicates are trusted Go-code input. They are SQL fragments, not
a sandbox for untrusted SQL. Keep the predicate text fixed in application code;
callers must never pass user-authored predicate text. Values bind separately
through `?` placeholders, and the number of placeholders must match the values
passed to `TrustedScope`.

Use a stable caller identity and bind tenant, batch, and time-window values:

```go
tenantID := "tenant-a"
tenantScope := gxsql.TrustedScope("tenant-a", "tenant_id = ?", tenantID)

batchID := int64(42)
batchScope := gxsql.TrustedScope("batch-42", "batch_id = ?", batchID)

start := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
end := start.Add(24 * time.Hour)
windowScope := gxsql.TrustedScope(
    "events-2025-01-01",
    "event_at >= ? AND event_at < ?",
    start, end,
)
```

Attach one scope to a run with `WithScope`; the dialect renders the neutral
placeholders for the selected driver:

```go
report, err := suite.ValidateTable(
    ctx, readOnlyDB, gxsql.Table("events"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithScope(windowScope),
)
```

`Report.ScopeID` and exported `scope.id` carry caller identity only; neither
serializes the scope predicate text or bound arguments. Default validation
errors, display output, and exports omit those scope fields. Ordinary samples
and failed keys remain subject to the usual report redaction guidance. Captured
SQL and arguments require explicit diagnostic capture and export options; treat
them as sensitive.

Production callers should use a database role with read-only permissions and
pass a context with a deadline:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
report, err := suite.ValidateTable(ctx, readOnlyDB, gxsql.Table("events"),
    gxsql.WithScope(tenantScope),
)
```

Check both `err` and `report.Err()` according to the run and policy failure
rules described above.

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
