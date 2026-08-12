# Validation Behavior

## What gxsql Validates

`gxsql` asserts facts about database table contents through `database/sql`. Each
expectation renders SQL and runs in the database. It does not load the table
into Go memory. Use it for deployment gates, ETL checks, and integration-test
databases.

It is not an ORM, a migration tool, or a schema linter.

## Suites and Expectations

A `Suite` is an ordered collection of expectations:

```go
suite := gxsql.NewSuite(
    gxsql.RowCount().GreaterOrEqual(1),
    gxsql.String("email").NotEmpty(),
)
```

Built-in builders create the expectations that `gxsql` supports:

| Builder                             | Examples                                                                  |
| ----------------------------------- | ------------------------------------------------------------------------- |
| `RowCount()`                        | `Equal`, `Between`, `GreaterOrEqual`                                      |
| `RequiredColumns` / `ExactColumns`  | unordered column-set presence or exact-set contracts                      |
| `Column(name)`                      | `IsNull`, `NotNull`, `In`, `NotIn`, `Unique`, `DistinctCount`             |
| `Column(left)` same-row comparisons | `EqualColumn`, `NotEqualColumn`, `LessThanColumn`, `GreaterOrEqualColumn` |
| `Columns(names...)`                 | composite `Unique`, `References`                                          |
| `Int(name)` / `Float(name)`         | range and comparison checks, plus aggregate checks                        |
| `Int(name).RatioEqual`              | integer algebraic `value == right * bound` (not SQL `/`)                  |
| `String(name)`                      | `Empty`, `NotEmpty`, `LenEqual`, `LenBetween`                             |
| `Timestamp(name)`                   | `InWindow`, `FreshSince`                                                  |
| `TrustedCountQuery` + `CustomCount` | Trusted SQL count returning one non-negative failure count                |

Do not implement `Expectation` outside `gxsql`. It is a sealed interface.
Construct expectations with these builders. The
[expectations reference](../reference/expectations.md) describes all methods.

Expectations run in declaration order. A completed run contains one `Result` per
expectation in the same order.

## Tables and Dialects

Target a table with `Table` or `SchemaTable`:

```go
gxsql.Table("users")
gxsql.SchemaTable("public", "users")
```

Built-in dialects accept identifiers that match `^[A-Za-z_][A-Za-z0-9_]*$`. They
quote those identifiers before they add them to SQL. Invalid or empty
identifiers are configuration errors.

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

`gxsql` does not open connections and does not bundle drivers. Its narrow `DB`
interface is satisfied by `*sql.DB`.

## Validation Modes

Call `ValidateTable` to run the suite:

```go
report, err := suite.ValidateTable(ctx, db, gxsql.Table("users"),
    gxsql.WithDialect(gxsql.Postgres()),
)
```

Policy failures are collect-all. A failing expectation does not stop later
expectations. `ValidateTable` returns `(report, nil)` for completed validation.
Use `report.OK()` or `report.Err()` for hard gating; warning and info policy
failures remain queryable without gating.

By default, results retain counts and capped sample values, but not full failed
row identities. Add `WithKey("id")` to retain caller-selected keys. Use
`SummaryOnly()` to state that counts and samples are intended. Per-run options
override suite-level caps.

## Structural Column Contracts

Use `RequiredColumns` or `ExactColumns` to gate table shape before content
checks. Both compare unordered column-name sets against the physical spellings
that `Rows.Columns()` reports, byte-for-byte, with no case folding. Column order
does not affect the verdict. Discovery is a read-only zero-row
`SELECT * ... WHERE 1 = 0` probe.

`RequiredColumns` allows extra discovered columns. `ExactColumns` rejects both
missing and unexpected names. Missing and unexpected differences are ordinary
table-level results with structured facts. A missing target or a permission
denial is a typed execution error, not a content-policy failure. These builders
do not validate types, nullability, or ordinal position.

`WithScope` is incompatible and fails preflight. Prefer a separate structural
suite when fail-fast shape gating matters:

```go
structure := gxsql.NewSuite(
    gxsql.RequiredColumns("id", "event_time", "payload"),
    gxsql.ExactColumns("id", "event_time", "payload"),
)
report, err := structure.ValidateTable(ctx, db, gxsql.Table("ingest_events"),
    gxsql.WithDialect(gxsql.Postgres()),
)
```

See the
[expectations reference](../reference/expectations.md#structural-columns) for
fact ordering and error details.

## Scoped Validation

Use `TrustedScope` with `WithScope` when one suite must validate only a selected
population of rows. `TrustedScope` takes:

- a stable caller identity
- a predicate written in trusted Go code
- values for its `?` placeholders

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

The predicate is trusted Go-code input, not a sandbox for untrusted SQL. Never
pass user-authored predicate text to `TrustedScope`. Choose from predicates that
the application defines. Pass request-derived data only as separately bound
values. `gxsql` binds scope values before the expectation values and renders the
placeholders for the selected dialect. Do not interpolate values into the
predicate.

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
options. The library validates scope configuration, including placeholder arity,
before SQL runs.

`Report.ScopeID` contains only the normalized caller identity. It is empty for
an unscoped run. It does not serialize the scope predicate text or bound
arguments. `Report.Err()` and its default `ValidationError.Error()` text also
omit those scope fields. `Report.String()` may still include ordinary result
samples or failed keys, so redact those as appropriate. It does not serialize
the scope predicate or its arguments.

`ExportReport` emits the caller identity as `scope.id` only. Default exports
omit scope predicate text and bound arguments, along with captured SQL and
arguments. Enable diagnostic export only deliberately. Apply redaction when
those values may be sensitive.

In production, pass a context with a deadline to every `ValidateTable` call. Use
a read-only database role. Prefer a role that is restricted to the validation
tables or views.

## Custom Count Checks

`TrustedCountQuery` and `CustomCount` add one constrained custom shape: a
trusted SQL template that returns a single non-negative failure count. Template
text is trusted Go-code input, not a sandbox for untrusted SQL. Never pass
user-authored SQL in templates. Never interpolate identifiers or values into
template text. The library inserts `{{target}}` from the validated `TableRef`
and `{{scope}}` from `WithScope` (or `TRUE` when unscoped). Place both markers
in valid SQL. Qualify scope references when aliases require it. `gxsql` does not
parse, relocate, or rewrite arbitrary SQL.

A template must contain exactly one `{{target}}` and one `{{scope}}`. Keep both
markers outside SQL strings and comments. Place custom `?` placeholders after
`{{scope}}` so bound arguments stay in scope-first, custom-second order across
dialects. Preflight rejects these problems before any custom-count SQL runs:

- missing, duplicate, quoted, or commented markers
- malformed placeholder text
- custom-placeholder arity mismatch
- blank display names
- invalid scope composition

The query must return exactly one row and one column. Signed integer driver
values (`int`, `int8`, `int16`, `int32`, or `int64`) are accepted. Textual
numerics are not coerced. The count must be non-negative and representable as Go
`int`. On success the result uses `KindCustom`, blank `Column`,
`RowDenominatorUnavailable`, and a complete `FailedCount` (including zero).
`Total`, `FailedPercent`, `SampleValues`, and `FailedKeys` are unavailable.
`WithKey`, sample caps, and `SummaryOnly()` add no diagnostics for custom
counts. That shape fits gates, not row-level remediation.

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

## Error Handling

| Situation                                | `ValidateTable`                              | Result data                                      |
| ---------------------------------------- | -------------------------------------------- | ------------------------------------------------ |
| An expectation policy fails              | `(report, nil)`                              | `Success == false` on the failed result          |
| Run-level option is invalid              | `(Report{}, err)` before SQL                 | No report                                        |
| Expectation preflight or execution fails | `(Report{}, err)` by default                 | No report                                        |
| `ContinueOnError()` is set               | `(report, nil)` for per-expectation failures | Affected result has `Success == false` and `Err` |

Run-level errors include a nil dialect, negative caps, and invalid `WithKey`
columns. Preflight errors include:

- invalid identifiers
- empty or nil-valued `In`/`NotIn` lists
- duplicate or blank `WithID` values
- blank custom-count display names
- invalid or duplicated `{{target}}`/`{{scope}}` markers
- custom placeholders that appear before `{{scope}}`
- custom-placeholder arity mismatch
- empty, duplicate, or invalid `RequiredColumns` / `ExactColumns` names
- `WithScope` combined with `RequiredColumns` or `ExactColumns`

Invalid custom-count declarations never execute SQL. Structural column
expectations never ignore an attached scope.

`ContinueOnError()` does not make a nil top-level error mean success. Inspect
`report.OK()`, `report.Err()`, and each `Result.Err` when it is enabled.

## Next

- [Inspect results and failed-row data](results.md)
- [Plan query cost and retention](operations.md)
- [Suite and SQL integration reference](../reference/suite.md)
