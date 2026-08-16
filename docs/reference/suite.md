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

| API                                                               | Description                                                                                                     |
| ----------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `NewSuite(exps ...Expectation) *Suite`                            | Creates an ordered suite with the default sample and failed-key caps. Accepts a flattened pack-plus-local list. |
| `(*Suite).WithSampleCap(n int) *Suite`                            | Sets the suite default sample cap; `0` disables sample collection.                                              |
| `(*Suite).WithFailedKeysCap(n int) *Suite`                        | Sets the suite default failed-key cap; `0` is unlimited.                                                        |
| `(*Suite).ValidateTable(ctx, db, table, opts...) (Report, error)` | Runs every expectation and returns its aggregated report.                                                       |

`ValidateTable` returns `(report, nil)` for failed validation policies. Gate on
`report.OK()` or `report.Err()`. It returns `(Report{}, err)` for run-level,
preflight, or execution errors unless `ContinueOnError()` records a
per-expectation failure in the report.

## Options

`Option` is an opaque function that configures one validation run. Per-run
options override suite-level caps.

| API                            | Effect                                                                                                                                                            |
| ------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `WithDialect(d Dialect)`       | Selects the SQL renderer. Defaults to `Postgres()`.                                                                                                               |
| `WithSampleCap(n int)`         | Overrides the maximum retained sample values; `0` disables sample collection.                                                                                     |
| `WithFailedKeysCap(n int)`     | Overrides the maximum retained failed keys; `0` is unlimited.                                                                                                     |
| `WithKey(columns ...string)`   | Retains supplied row-key columns and disables summary-only mode.                                                                                                  |
| `SummaryOnly()`                | Does not load failed-row identities.                                                                                                                              |
| `ContinueOnError()`            | Records preflight and execution errors on results and continues.                                                                                                  |
| `CaptureQueryDiagnostics()`    | Records SQL and arguments for optional export only.                                                                                                               |
| `WithObserver(observer)`       | Emits synchronous privacy-safe `QueryEvent` values; observer panics abort with a typed observer error.                                                            |
| `WithSharedScalarEvaluation()` | Combines contiguous compatible built-in per-row failure counts into conditional-aggregate statement(s). Disabled by default.                                      |
| `WithScope(scope Scope)`       | Limits every expectation to rows that match the scope predicate; validates the scope when the run starts. Incompatible with `RequiredColumns` and `ExactColumns`. |

When the run supplies neither `WithKey` nor `SummaryOnly`, results contain
counts and capped samples but no failed-row identities. Invalid run-level
options—such as a nil dialect, negative caps, invalid key columns, or invalid
scopes—always prevent evaluation.

### WithObserver

`WithObserver(gxsql.ObserverFunc(...))` emits one synchronous `QueryEvent` for
each attempted statement. Events expose `ID`, `Kind`, `Category`, `Duration`,
and `Status`. They omit SQL text, bound arguments, scope predicates, samples,
and failed keys. The duration uses a monotonic clock. Row counts are not a
stable event field in this release, and observation never runs an extra query.

Observer panics are recovered and returned as a typed observer error. No partial
report is returned. Keep observer callbacks side-effect-light and avoid using
them to alter validation policy.

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

Suite scope never becomes a parent or secondary filter. Use distinct trusted
constructors when a cross-table check needs a non-local predicate:

| Mechanism                                          | Applies to                                | Does not apply to                   | Published identity                        |
| -------------------------------------------------- | ----------------------------------------- | ----------------------------------- | ----------------------------------------- |
| `WithScope(TrustedScope(...))`                     | Local / left population for the run       | Parent lookup; secondary `COUNT(*)` | `Report.ScopeID`; reconcile `LeftScopeID` |
| `WithParentFilter(TrustedParentFilter(...))`       | Parent side of a referential `NOT EXISTS` | Local rows; suite scope reuse       | `Reference.ParentFilterID`                |
| `WithSecondaryFilter(TrustedSecondaryFilter(...))` | Secondary `COUNT(*)` in `ReconcileCounts` | Left `COUNT(*)`; suite scope reuse  | `Reconcile.SecondaryFilterID`             |

Parent and secondary filters keep the same trusted-input rules as
`TrustedScope`: fixed Go-code predicate text, `?` placeholders, matching bound
values, and no user-authored SQL. Default errors, display output, and exports
publish identity only—never predicate text or arguments.

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

## Rule Eligibility

| API                                                                 | Description                                                                                         |
| ------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| `TrustedEligibility(id, predicate string, args ...any) Eligibility` | Builds an immutable trusted eligibility predicate with bound values.                                |
| `When(eligibility Eligibility, exp Expectation) Expectation`        | Wraps one expectation so only eligible rows inside the suite scope are evaluated.                   |
| `Eligibility`                                                       | Immutable carrier for identity, predicate, and arguments; construct only with `TrustedEligibility`. |

`TrustedEligibility` predicates are trusted Go-code input, not a sandbox for
untrusted SQL. Keep the predicate text fixed in application code. Pass dynamic
values through `?` placeholders. Never pass user-authored predicate text.
Placeholder arity must match the supplied values.

`When` narrows one expectation. It does not replace `WithScope` and does not
change `Report.ScopeID`. When a run also supplies `WithScope`, SQL applies suite
scope and eligibility as independent conjuncts. Bound values follow suite-scope,
eligibility, then expectation order.

```go
shippedAtPresent := gxsql.When(
    gxsql.TrustedEligibility("status-shipped", "status = ?", "shipped"),
    gxsql.Column("shipped_at").NotNull(),
)

report, err := gxsql.NewSuite(shippedAtPresent).ValidateTable(
    ctx, readOnlyDB, gxsql.Table("orders"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithScope(gxsql.TrustedScope("tenant-acme", "tenant_id = ?", tenantID)),
)
```

Eligible results use the eligible-row count as `Total` and as the denominator
for percentages and policy tolerance. Samples and failed keys come only from
eligible failing rows under existing caps. Ineligible rows neither pass nor fail
the wrapped rule. Zero eligible rows pass vacuously with `Total == 0`,
`FailedCount == 0`, no fabricated percentage, and `Tolerated == false`.

Supported shapes: ordinary per-row, uniqueness, composite uniqueness, and
referential integrity (including parent-filtered references). Table-level,
aggregate, distinct-count, custom-count, reconcile-count, and structural
expectations reject eligibility at preflight. Nested `When` wrappers are
configuration errors. Nil or invalid eligibility configuration fails preflight
before SQL. Without `ContinueOnError()`, those failures return
`(Report{}, *PreflightErrors)`. With it, the affected declaration-order slot
records `Err` and later expectations still run.

Default validation errors, display output, and `ExportReport` omit eligibility
predicate text and bound arguments. Captured SQL and arguments still require
explicit diagnostic capture and export options.

## Policy Pack Composition

A policy pack is an ordinary Go function that returns a fresh `[]Expectation`.
Callers concatenate packs and local rules, then pass the flattened list to
`NewSuite`. There is no pack registry or ID-prefix helper in this release.

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

Rules:

1. Each pack call returns independent values. Mutating a returned slice must not
   affect a later call’s return value.
2. Pack functions take only explicit parameters. Do not share mutable package-
   level expectation values that validation can observe.
3. Flattened declaration order is pack order, then order within each pack, then
   caller-appended expectations. `Report.Results[i]` matches that flattened
   list.
4. A composed suite must match the identical hand-flattened list
   field-for-field, including policy fields and eligibility wrappers.

Use `WithID` with caller-owned conventions such as reverse-domain or pack-prefix
paths (`acme.orders.id.present`). Collision detection compares the final
caller-visible stable ID string. Blank and duplicate IDs fail preflight before
SQL under default validation. With `ContinueOnError()`, duplicate-ID failures
occupy declaration-order slots and must not produce ambiguous exported
identities for executed siblings. Missing IDs remain allowed. Library `Kind`
values are not caller IDs.

Reuse completed packs and suites concurrently only when configuration is
finished and nothing mutates during `ValidateTable`.

## Custom Count Checks

| API                                                          | Description                                                                                      |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| `TrustedCountQuery(template string, args ...any) CountQuery` | Builds an immutable trusted SQL count template with bound custom arguments.                      |
| `CustomCount(name string, query CountQuery) Expectation`     | Executes the template and treats the scalar result as a failure count. `name` must be non-blank. |
| `CountQuery`                                                 | Immutable carrier for template and arguments; construct only with `TrustedCountQuery`.           |

Prefer `ReconcileCounts(secondary).Equal()` for suite-bound dual `COUNT(*)`
equality with left-only suite scope and an optional secondary filter. Remain on
`CustomCount` for joins, `GROUP BY` / `HAVING`, non-`COUNT(*)` aggregates,
non-equality relationships, and other exotic cross-table recipes. See
[expectation builders](expectations.md) for the built-in reconcile contract.

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
tolerance form, including composite `Columns(...).Unique()` and `References()`
with or without `WithParentFilter`. Wrapping a table-level, aggregate,
distinct-count, row-count, custom-count, reconcile-count, or structural column
declaration with `MaxFailedPercent`—or combining count and percent
tolerance—fails preflight before SQL. Without `ContinueOnError()`, invalid
policy returns the zero report and `*PreflightErrors`. With it, the matching
declaration-order slot records the configuration error and later expectations
still run.

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

Pass a caller-owned `*sql.Tx` when several statements must use one transaction
and isolation level. `gxsql` does not begin, commit, roll back, or close the
transaction:

```go
tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
if err != nil {
	return err
}
defer tx.Rollback()

report, err := suite.ValidateTable(ctx, tx, gxsql.Table("orders"),
	gxsql.WithDialect(gxsql.Postgres()),
)
```

Without a caller-owned transaction, a pooled `*sql.DB` may use different
connections between statements. Avoid snapshot-consistency claims in that mode.

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
[expectations reference](expectations.md). Decorate builders with `WithID`,
`WithPolicy`, `WithMaxFailedCount`, and `When` as needed; do not implement
`Expectation` outside the package.
