# Suites, Options, and SQL Integration

## Suite

`Suite` is an ordered set of SQL expectations. Create it with `NewSuite`. Its
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
preflight, or execution errors unless `ContinueOnError()` records a
per-expectation failure in the report.

## Options

`Option` is an opaque function that configures one validation run. Per-run
options override suite-level caps.

| Option                         | Effect                                                                                                                                                            |
| ------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `WithDialect(d Dialect)`       | Selects the SQL renderer. Defaults to `Postgres()`.                                                                                                               |
| `WithSampleCap(n int)`         | Overrides the maximum retained sample values; `0` disables sample collection.                                                                                     |
| `WithFailedKeysCap(n int)`     | Overrides the maximum retained failed keys; `0` is unlimited.                                                                                                     |
| `WithKey(columns ...string)`   | Retains supplied row-key columns and disables summary-only mode.                                                                                                  |
| `SummaryOnly()`                | Does not load failed-row identities.                                                                                                                              |
| `ContinueOnError()`            | Records preflight and execution errors on results and continues.                                                                                                  |
| `CaptureQueryDiagnostics()`    | Records SQL and arguments for optional export only.                                                                                                               |
| `WithSharedScalarEvaluation()` | Combines contiguous compatible built-in per-row failure counts into conditional-aggregate statement(s). Disabled by default.                                      |
| `WithScope(scope Scope)`       | Limits every expectation to rows that match the scope predicate; validates the scope when the run starts. Incompatible with `RequiredColumns` and `ExactColumns`. |

When the run supplies neither `WithKey` nor `SummaryOnly`, results contain
counts and capped samples but no failed-row identities. Invalid run-level
options—such as a nil dialect, negative caps, invalid key columns, or invalid
scopes—always prevent evaluation.

### WithSharedScalarEvaluation

`WithSharedScalarEvaluation()` is an opt-in performance option. Pass it on a
`ValidateTable` call when measurement shows that repeated per-row failure-count
queries dominate the suite:

```go
report, err := suite.ValidateTable(ctx, db, gxsql.Table("orders"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithSharedScalarEvaluation(),
)
```

The option combines only contiguous runs of compatible built-in per-row scalar
checks into conditional-aggregate statement(s). It does not change published
semantic report fields for those checks: counts, verdicts, tolerance, samples,
failed keys, declaration order, and scope behavior stay aligned with sequential
evaluation. Captured diagnostics, when enabled, record the actual combined
statement rather than fabricated per-check SQL.

Rules and limits:

- Disabled by default; omit the option to keep sequential evaluation.
- Only contiguous compatible slots combine. Intervening incompatible
  expectations stay sequential and keep declaration order.
- Large contiguous runs split across multiple statements when needed to stay
  within engine SELECT target limits.
- Uniqueness, table-level, aggregate, distinct-count, custom-count, structural,
  and relation checks never combine.
- Shared statement errors attribute to every slot in that combined statement.
  Per-expectation diagnostic failures attribute only to the affected result.

## Scoped Validation

`TrustedScope(id, predicate string, args ...any) Scope` constructs a `Scope`. It
is not an `Option`. Attach the returned scope to a run with `WithScope`.

`TrustedScope` predicates are trusted Go-code input. They are SQL fragments, not
a sandbox for untrusted SQL. Keep the predicate text fixed in application code.
Callers must never pass user-authored predicate text. Values bind separately
through `?` placeholders. The number of placeholders must match the values
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

Attach one scope to a run with `WithScope`. The dialect renders the neutral
placeholders for the selected driver:

```go
report, err := suite.ValidateTable(
    ctx, readOnlyDB, gxsql.Table("events"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithScope(windowScope),
)
```

`Report.ScopeID` and exported `scope.id` carry caller identity only. Neither
serializes the scope predicate text or bound arguments. Default validation
errors, display output, and exports omit those scope fields. Ordinary samples
and failed keys remain subject to the usual report redaction guidance. Captured
SQL and arguments require explicit diagnostic capture and export options; treat
them as sensitive.

`RequiredColumns` and `ExactColumns` have no row population. Pairing either
expectation with `WithScope` fails `ValidateTable` preflight with an
`invalid_config` error rather than ignoring scope. Run a separate unscoped
structural suite when shape checks must gate content validation.

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

## Custom Count Checks

| API                                                          | Description                                                                                      |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| `TrustedCountQuery(template string, args ...any) CountQuery` | Builds an immutable trusted SQL count template with bound custom arguments.                      |
| `CustomCount(name string, query CountQuery) Expectation`     | Executes the template and treats the scalar result as a failure count. `name` must be non-blank. |
| `CountQuery`                                                 | Immutable carrier for template and arguments; construct only with `TrustedCountQuery`.           |

Template SQL is trusted Go-code input, not a sandbox for untrusted text. Callers
must never insert user-authored SQL into templates. A template contains exactly
one `{{target}}` and one `{{scope}}`, both outside SQL strings and comments. The
library renders `{{target}}` only from the validated `TableRef`. `{{scope}}`
renders `TRUE` for an unscoped run or the parenthesized scope predicate from
`WithScope`. Place both markers in syntactically valid SQL and qualify scope
column references when the query uses table aliases. `gxsql` does not parse or
rewrite joins, `GROUP BY`, `HAVING`, or aliases.

Custom `?` placeholders must appear after `{{scope}}`. Bound arguments are scope
values first, then custom template values. Preflight rejects marker,
placeholder, and arity errors before SQL. Invalid custom declarations never
execute a statement. Without `ContinueOnError()`, preflight or execution failure
returns `(Report{}, err)`. With it, the affected result records `Err` in its
declaration slot and later expectations still run. Malformed count results
(wrong row/column shape, non-integer, negative, or overflow) are scan-category
errors.

On success, results use `KindCustom`, blank `Column`,
`RowDenominatorUnavailable`, and a complete `FailedCount`. `WithKey`, sample
caps, and `SummaryOnly()` add no diagnostics. Default errors and display output
omit template SQL and arguments; use `CaptureQueryDiagnostics()` only when
opting into export diagnostics.

Join count example:

```go
query := gxsql.TrustedCountQuery(`SELECT COUNT(*)
FROM {{target}} AS o
JOIN accounts AS a ON a.id = o.account_id
WHERE {{scope}} AND a.status = ?`, "inactive")
exp := gxsql.CustomCount("inactive account orders", query)
```

`GROUP BY` / `HAVING` example:

```go
query := gxsql.TrustedCountQuery(`SELECT COUNT(*)
FROM (
  SELECT o.account_id
  FROM {{target}} AS o
  WHERE {{scope}}
  GROUP BY o.account_id
  HAVING COUNT(*) > ?
) AS violating_groups`, int64(1))
exp := gxsql.CustomCount("accounts with multiple order lines", query)
```

## Policy Decoration and Tolerance

`WithPolicy(exp, Policy)` adds severity, optional description/tags, and at most
one tolerance. `SeverityError` is the zero value; warning and info failures
remain queryable without gating a completed report. Configuration and execution
errors always gate.

| API                                                        | Description                                                                                                |
| ---------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `WithPolicy(exp, Policy) Expectation`                      | Decorates an expectation with severity, metadata, and an optional tolerance. Immutable after construction. |
| `MaxFailedPercent(p) Tolerance`                            | Inclusive unrounded failed-row percentage in `[0, 100]`; requires a row denominator.                       |
| `WithMaxFailedCount(max int, exp Expectation) Expectation` | Existing inclusive maximum failed-row allowance; behavior and eligible shapes remain unchanged.            |
| `Report.GatingFailures() []Result`                         | Non-advisory policy failures and all result errors.                                                        |
| `Report.PolicyFailures() []Result`                         | Evaluated data-quality failures, excluding result errors.                                                  |

Per-row, uniqueness, and referential-integrity expectations qualify for either
tolerance form, including composite `Columns(...).Unique()` and `References()`.
Wrapping a table-level, aggregate, distinct-count, row-count, custom-count, or
structural column declaration with `MaxFailedPercent`—or combining count and
percent tolerance—fails preflight before SQL. Without `ContinueOnError()`,
invalid policy returns the zero report and `*PreflightErrors`. With it, the
matching declaration-order slot records the configuration error and later
expectations still run.

`MaxFailedPercent(p)` compares `FailedCount / Total * 100 <= p` before display
rounding. Both tolerance forms preserve raw `Total`, `FailedCount`,
`FailedPercent`, samples, keys, and configured facts. Empty evaluated
populations pass and are not tolerated.

Descriptions are trimmed; blank descriptions are omitted. Tags are trimmed,
sorted, copied, and rejected when blank or duplicated. Metadata never changes
`WithID` or `Kind`. Use `Report.Results` and the focused report filters for
advisory, raw unexpected, tolerated, policy-failure, and execution-failure
outcomes.

## Test Helpers

The `github.com/busyminds/gxsql/gxsqltest` package adapts validation to Go
tests. Its `TestingT` interface is the `Helper`, `Errorf`, and `Fatalf` subset
shared by `*testing.T` and `*testing.B`.

| API                                                       | Behavior                                                                                                                     |
| --------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `gxsqltest.Check(t, ctx, suite, db, table, opts...) bool` | Calls `t.Errorf` for execution or hard-gating policy failure and continues. Returns true when no hard-gating failure exists. |
| `gxsqltest.Require(t, ctx, suite, db, table, opts...)`    | Calls `t.Fatalf` for execution or hard-gating policy failure.                                                                |

Both helpers accept the same options as `ValidateTable`.

## Database and Dialects

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

| Constructor  | Identifier Quoting | Placeholders  | String Length       |
| ------------ | ------------------ | ------------- | ------------------- |
| `Postgres()` | double quotes      | `$1`, `$2`, … | `CHAR_LENGTH(expr)` |
| `SQLite()`   | double quotes      | `?`           | `LENGTH(expr)`      |
| `DuckDB()`   | double quotes      | `$1`, `$2`, … | `LENGTH(expr)`      |
| `MySQL()`    | backticks          | `?`           | `CHAR_LENGTH(expr)` |

## Table References

`TableRef` holds exported `Schema` and `Name` fields. Construct one with
`Table(name)` for an unqualified table or `SchemaTable(schema, name)` for a
schema-qualified table. Built-in dialects reject empty identifiers and those
outside `^[A-Za-z_][A-Za-z0-9_]*$` when rendering.

## Expectation

`Expectation` appears in public signatures but is sealed. Its unexported
`evaluateSQL` method and unexported option type prevent implementations outside
package `gxsql`. Use the builders in the
[expectations reference](expectations.md).
