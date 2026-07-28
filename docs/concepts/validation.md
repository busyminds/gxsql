# Validation Behavior

## What gxsql validates

`gxsql` asserts facts about database-table contents through `database/sql`. Each
expectation renders SQL and runs in the database, rather than loading the table
into Go memory. It is useful for deployment gates, ETL checks, and
integration-test databases.

It is not an ORM, migration tool, or schema linter.

## Suites and expectations

A `Suite` is an ordered collection of expectations:

```go
suite := gxsql.NewSuite(
    gxsql.RowCount().GreaterOrEqual(1),
    gxsql.String("email").NotEmpty(),
)
```

Built-in builders create the expectations `gxsql` supports:

| Builder                             | Examples                                                      |
| ----------------------------------- | ------------------------------------------------------------- |
| `RowCount()`                        | `Equal`, `Between`, `GreaterOrEqual`                          |
| `Column(name)`                      | `IsNull`, `NotNull`, `In`, `NotIn`, `Unique`, `DistinctCount` |
| `Int(name)` / `Float(name)`         | range and comparison checks, plus aggregate checks            |
| `String(name)`                      | `Empty`, `NotEmpty`, `LenEqual`, `LenBetween`                 |
| `TrustedCountQuery` + `CustomCount` | Trusted SQL count returning one non-negative failure count    |

Do not implement `Expectation` outside `gxsql`. It is a sealed interface;
construct expectations with these builders. The
[expectations reference](../reference/expectations.md) describes all methods.

Expectations run in declaration order. A completed run contains one `Result` per
expectation in the same order.

## Tables and dialects

Target a table with `Table` or `SchemaTable`:

```go
gxsql.Table("users")
gxsql.SchemaTable("public", "users")
```

Built-in dialects accept identifiers matching `^[A-Za-z_][A-Za-z0-9_]*$` and
quote them before adding them to SQL. Invalid or empty identifiers are
configuration errors.

Select the renderer for the database behind the connection:

| Dialect      | Identifier quoting | Placeholders  | String length       |
| ------------ | ------------------ | ------------- | ------------------- |
| `Postgres()` | `"name"`           | `$1`, `$2`, … | `CHAR_LENGTH(expr)` |
| `SQLite()`   | `"name"`           | `?`           | `LENGTH(expr)`      |
| `DuckDB()`   | `"name"`           | `$1`, `$2`, … | `LENGTH(expr)`      |
| `MySQL()`    | `` `name` ``       | `?`           | `CHAR_LENGTH(expr)` |

`ValidateTable` defaults to `Postgres()` when `WithDialect` is omitted. Pass the
dialect explicitly in application code and tests so the rendered SQL tracks the
selected driver.

`gxsql` neither opens connections nor bundles drivers. Its narrow `DB` interface
is satisfied by `*sql.DB`.

## Validation modes

Call `ValidateTable` to run the suite:

```go
report, err := suite.ValidateTable(ctx, db, gxsql.Table("users"),
    gxsql.WithDialect(gxsql.Postgres()),
)
```

Policy failures are collect-all: a failing expectation does not stop later
expectations, and `ValidateTable` returns `(report, nil)`. Use `report.OK()` or
`report.Err()` to decide whether the data passed.

By default, results retain counts and capped sample values, but no full failed
row identities. Add `WithKey("id")` to retain caller-selected keys, or use
`SummaryOnly()` to state that counts and samples are intended. Per-run options
override suite-level caps.

## Scoped validation

Use `TrustedScope` with `WithScope` when one suite should validate only a
selected population of rows. `TrustedScope` takes a stable caller identity, a
predicate written in trusted Go code, and values for its `?` placeholders:

```go
tenantID := authenticatedTenantID()
scope := gxsql.TrustedScope("tenant-acme", "tenant_id = ?", tenantID)

report, err := suite.ValidateTable(
    ctx,
    db,
    gxsql.Table("orders"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithScope(scope),
)
```

The predicate is trusted Go-code input, not a sandbox for untrusted SQL. Callers
must never pass user-authored predicate text to `TrustedScope`. Choose from
predicates defined by the application, and pass request-derived data only as
separately bound values. `gxsql` binds scope values before the expectation
values and renders the placeholders for the selected dialect; do not interpolate
values into the predicate.

The same pattern handles other bounded populations:

```go
batchID := int64(42)
batchScope := gxsql.TrustedScope("batch-42", "batch_id = ?", batchID)

start := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
end := time.Date(2025, time.January, 2, 0, 0, 0, 0, time.UTC)
windowScope := gxsql.TrustedScope(
    "events-2025-01-01",
    "event_at >= ? AND event_at < ?",
    start,
    end,
)
```

Use a half-open time window (`>= start` and `< end`) to make adjacent windows
unambiguous. Attach a scope with `gxsql.WithScope` in the `ValidateTable`
options. Scope configuration, including placeholder arity, is validated before
SQL runs.

`Report.ScopeID` contains only the normalized caller identity and is empty for
an unscoped run. It does not serialize the scope predicate text or bound
arguments. `Report.Err()` and its default `ValidationError.Error()` text
likewise omit those scope fields. `Report.String()` may still include ordinary
result samples or failed keys, so redact those as appropriate; it does not
serialize the scope predicate or its arguments.

`ExportReport` emits the caller identity as `scope.id` only: default exports
omit scope predicate text and bound arguments, along with captured SQL and
arguments. Enable diagnostic export only deliberately and apply redaction when
those values may be sensitive.

In production, pass a context with a deadline to every `ValidateTable` call and
use a read-only database role, preferably one restricted to the validation
tables or views.

## Custom count checks

`TrustedCountQuery` and `CustomCount` add one constrained custom shape: a
trusted SQL template that returns a single non-negative failure count. Template
text is trusted Go-code input, not a sandbox for untrusted SQL. Callers must
never pass user-authored SQL in templates or interpolate identifiers or values
into template text. The library inserts `{{target}}` from the validated
`TableRef` and `{{scope}}` from `WithScope` (or `TRUE` when unscoped). The
template author places both markers in valid SQL and qualifies scope references
when aliases require it; `gxsql` does not parse, relocate, or rewrite arbitrary
SQL.

A template must contain exactly one `{{target}}` and one `{{scope}}`, both
outside SQL strings and comments. Custom `?` placeholders must come after
`{{scope}}` so bound arguments stay in scope-first, custom-second order across
dialects. Preflight rejects missing, duplicate, quoted, or commented markers,
malformed placeholder text, custom-placeholder arity mismatch, blank display
names, and invalid scope composition before any custom-count SQL runs.

The query must return exactly one row and one column. Signed integer driver
values (`int`, `int8`, `int16`, `int32`, or `int64`) are accepted; textual
numerics are not coerced. The count must be non-negative and representable as Go
`int`. On success the result uses `KindCustom`, blank `Column`,
`RowDenominatorUnavailable`, and a complete `FailedCount` (including zero).
`Total`, `FailedPercent`, `SampleValues`, and `FailedKeys` are unavailable.
`WithKey`, sample caps, and `SummaryOnly()` add no diagnostics for custom
counts—appropriate for gates, not row-level remediation.

Portable join count (scoped; qualify `{{scope}}` for the table alias):

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

Portable `GROUP BY` / `HAVING` count (unscoped; `{{scope}}` renders `TRUE`):

```go
groupCount := gxsql.TrustedCountQuery(`SELECT COUNT(*)
FROM (
  SELECT o.account_id
  FROM {{target}} AS o
  WHERE {{scope}}
  GROUP BY o.account_id
  HAVING COUNT(*) > ?
) AS violating_groups`, int64(1))

suite := gxsql.NewSuite(
    gxsql.CustomCount("accounts with multiple order lines", groupCount),
)
report, err := suite.ValidateTable(ctx, db, gxsql.Table("order_lines"),
    gxsql.WithDialect(gxsql.Postgres()),
)
```

Default reports, errors, and `ExportReport` omit template SQL and bound
arguments, including driver-error text. `CaptureQueryDiagnostics()` is an opt-in
export-only path subject to existing redactors.

## Error handling

| Situation                                | `ValidateTable`                              | Result data                                      |
| ---------------------------------------- | -------------------------------------------- | ------------------------------------------------ |
| An expectation policy fails              | `(report, nil)`                              | `Success == false` on the failed result          |
| Run-level option is invalid              | `(Report{}, err)` before SQL                 | No report                                        |
| Expectation preflight or execution fails | `(Report{}, err)` by default                 | No report                                        |
| `ContinueOnError()` is set               | `(report, nil)` for per-expectation failures | Affected result has `Success == false` and `Err` |

Run-level errors include a nil dialect, negative caps, and invalid `WithKey`
columns. Preflight errors include invalid identifiers, empty or nil-valued
`In`/`NotIn` lists, duplicate or blank `WithID` values, blank custom-count
display names, invalid or duplicated `{{target}}`/`{{scope}}` markers, custom
placeholders that appear before `{{scope}}`, and custom-placeholder arity
mismatch. Invalid custom-count declarations never execute SQL.

`ContinueOnError()` does not make a nil top-level error mean success. Inspect
`report.OK()`, `report.Err()`, and each `Result.Err` when it is enabled.

## Next

- [Inspect results and failed-row data](results.md)
- [Plan query cost and retention](operations.md)
- [Suite and SQL integration reference](../reference/suite.md)
