# Expectation Builders

Builders return sealed `Expectation` values for `NewSuite`. Column and table
identifiers are validated by the selected dialect during preflight.

## Row count

`RowCount() RowCountBuilder` starts a table-level row-count expectation.

| Method                      | Policy                             |
| --------------------------- | ---------------------------------- |
| `Equal(want int)`           | Row count equals `want`.           |
| `Between(lo, hi int)`       | `lo <= row count <= hi`.           |
| `GreaterThan(bound int)`    | Row count is greater than `bound`. |
| `GreaterOrEqual(bound int)` | Row count is at least `bound`.     |
| `LessThan(bound int)`       | Row count is less than `bound`.    |
| `LessOrEqual(bound int)`    | Row count is at most `bound`.      |

Row-count results have `RowDenominatorUnavailable`; per-row fields stay at their
zero values. Observed counts and configured thresholds are available in
`Result.Facts`.

## Generic columns

`Column(name string) ColumnBuilder` starts generic column checks.

| Method               | Policy                                                                       |
| -------------------- | ---------------------------------------------------------------------------- |
| `IsNull()`           | Every value is SQL `NULL`.                                                   |
| `NotNull()`          | Every value is not SQL `NULL`.                                               |
| `In(vals ...any)`    | Every value is a member of `vals`; column `NULL` fails.                      |
| `NotIn(vals ...any)` | Every value is outside `vals`; column `NULL` fails.                          |
| `Unique()`           | No non-null value appears more than once; all rows in duplicate groups fail. |
| `DistinctCount()`    | Starts a table-level count of distinct non-null values.                      |

`In` and `NotIn` require at least one non-nil value. Empty lists and nil entries
are configuration errors. Each value becomes a bound placeholder; see
[operational limits](../concepts/operations.md) before using a large list.

`DistinctCount()` returns `DistinctCountBuilder`, whose `Equal`, `Between`,
`GreaterThan`, `GreaterOrEqual`, `LessThan`, and `LessOrEqual` methods apply the
corresponding integer comparison to the number of distinct non-null values. Like
row count, this is a table-level result.

Single-column `Unique()` ignores SQL `NULL` values: they do not participate in
duplicate detection. `FailedCount` counts every row in each duplicate group, not
the number of groups. Empty tables and empty scoped populations pass vacuously.
Results use `KindUnique` and set `Result.Column` to the checked column.

## Composite columns

`Columns(names ...string) ColumnsBuilder` starts multi-column checks. Supply two
or more separately validated identifiers. Empty names, invalid identifiers,
duplicates within one tuple, and fewer than two columns for composite uniqueness
fail `ValidateTable` preflight before SQL.

| Method                                                 | Policy                                                                                                       |
| ------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ |
| `Unique()`                                             | Each complete non-`NULL` tuple appears at most once in the scoped local population; every duplicate row fails. |
| `References(parent TableRef, parentColumns ...string)` | Every complete non-`NULL` local tuple resolves to at least one unscoped parent row; orphans fail locally.      |

`Column(name).References(parent, parentColumn)` covers the single-column form
with the same semantics and `KindReference`.

### Composite uniqueness

`Columns("tenant_id", "order_id").Unique()` extends the single-column NULL
policy to tuples: a row participates only when **every** component is non-`NULL`.
A tuple with any `NULL` component is ignored. `FailedCount` is duplicate
**rows**, never duplicate groups. Suite `WithScope` limits the evaluated
population before duplicate detection. Empty scoped populations pass vacuously.

Results use `KindCompositeUnique`, leave `Result.Column` blank, and populate
`Result.Facts.KeyColumns` in declaration order. Samples (capped) and failed keys
(`WithKey`) come only from failing local rows. Composite uniqueness is eligible
for `WithMaxFailedCount`.

```go
suite := gxsql.NewSuite(
    gxsql.Columns("tenant_id", "order_id").Unique(),
)
report, err := suite.ValidateTable(ctx, db, gxsql.Table("orders"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithScope(gxsql.TrustedScope("tenant-acme", "tenant_id = ?", tenantID)),
    gxsql.WithKey("tenant_id", "order_id"),
)
```

### Referential integrity

`Columns("tenant_id", "customer_id").References(gxsql.SchemaTable("public", "customers"), "tenant_id", "id")`
maps local columns to equal-arity parent columns. Parent targets use existing
`Table` / `SchemaTable` construction so schema-qualified parents stay structured
and are rendered through the active dialect.

A local row is evaluated only when every local key component is non-`NULL`. A
row with any `NULL` local component passes (nullable foreign-key policy). A
complete local tuple fails when no parent row matches every mapped component.
Multiple matching parent rows still pass; the check proves existence, not parent
uniqueness. Orphans count as failing **local** rows. Empty local scopes pass.

Partial mappings against a composite parent key are allowed by the API, but
are only correct when the mapped parent columns are unique at that arity;
otherwise unrelated parent rows can match.

`WithScope` applies only to the local validated table. Parent lookup is
intentionally unscoped: local scope is never reused on the parent side. There is
no parent-scope API in this release.

Results use `KindReference`, leave `Result.Column` blank, and populate
`Result.Facts.Reference` with `LocalColumns`, structured `Parent` (`TableRef`),
and `ParentColumns`. Samples and failed keys are local-only under existing caps;
parent values and parent keys are never emitted. Preflight rejects empty
mappings, unequal arity, invalid or duplicate identifiers, and unsupported
dialect capability before SQL.

```go
suite := gxsql.NewSuite(
    gxsql.Columns("tenant_id", "customer_id").References(
        gxsql.SchemaTable("public", "customers"), "tenant_id", "id",
    ),
)
report, err := suite.ValidateTable(ctx, db, gxsql.Table("orders"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithScope(gxsql.TrustedScope("tenant-acme", "tenant_id = ?", tenantID)),
    gxsql.WithKey("id"),
)
```

## Numeric columns

`Int(name string)` and `Float(name string)` both return `NumberColumn` for
ordered numeric checks. Per-row comparisons treat SQL `NULL` as failing.

| Method                             | Policy                                        |
| ---------------------------------- | --------------------------------------------- |
| `Between(lo, hi any)`              | `lo <= value <= hi`.                          |
| `GreaterThan(bound any)`           | `value > bound`.                              |
| `GreaterOrEqual(bound any)`        | `value >= bound`.                             |
| `LessThan(bound any)`              | `value < bound`.                              |
| `LessOrEqual(bound any)`           | `value <= bound`.                             |
| `AverageBetween(lo, hi float64)`   | The column average is in the inclusive range. |
| `MinGreaterOrEqual(bound float64)` | The column minimum is at least the bound.     |
| `MaxLessOrEqual(bound float64)`    | The column maximum is at most the bound.      |

The aggregate methods are table-level checks. They pass vacuously when the
column has no non-null numeric value.

## String columns

`String(name string) StringColumn` starts string-specific checks. SQL `NULL`
fails each string policy.

| Method                   | Policy                                                  |
| ------------------------ | ------------------------------------------------------- |
| `NotEmpty()`             | Every string is non-empty.                              |
| `Empty()`                | Every string is empty.                                  |
| `LenEqual(n int)`        | Every database string length equals `n`.                |
| `LenBetween(lo, hi int)` | Every database string length is in the inclusive range. |

Length uses the dialect's SQL length expression—`CHAR_LENGTH` or `LENGTH`—not Go
rune counting.

## Custom counts

`TrustedCountQuery(template string, args ...any) CountQuery` creates an
immutable trusted SQL template. Use `CustomCount(name string, query CountQuery)`
to add it to a suite. Template text is application-owned Go code, not a sandbox
for user-authored SQL.

The template must contain exactly one `{{target}}` and one `{{scope}}` marker,
both outside SQL strings and comments. `{{target}}` is replaced with the
validated table reference; `{{scope}}` is replaced with `TRUE` when unscoped or
the parenthesized `WithScope` predicate. Custom `?` placeholders must follow
`{{scope}}`; scope arguments bind first, followed by custom arguments. Marker,
placeholder, and arity errors are rejected during preflight.

The query must return exactly one row and one column containing a non-negative
signed integer representable as Go `int`; textual numerics are not coerced.
Successful results use `KindCustom`, leave `Column` blank, set
`RowDenominatorUnavailable`, and expose the complete `FailedCount` (including
zero). `Total`, `FailedPercent`, `SampleValues`, and `FailedKeys` are
unavailable. `WithKey`, sample caps, and `SummaryOnly()` do not add diagnostics
to custom-count results. See [suite and SQL integration](suite.md) for join and
aggregate examples.

## Bounded failure tolerance

`WithMaxFailedCount(max int, exp Expectation) Expectation` wraps one eligible
builder result with an inclusive non-negative maximum failed-row count. Equality
passes. There is no percentage, pass-rate, `Mostly`, rounding, or compound
policy.

Eligible shapes are per-row and uniqueness expectations: `Column` null and
membership checks, single-column `Unique()`, composite `Columns(...).Unique()`,
`Column`/`Columns` `References()`, numeric per-row comparisons (`Between`,
`GreaterThan`, `GreaterOrEqual`, `LessThan`, `LessOrEqual`), and string checks
(`NotEmpty`, `Empty`, `LenEqual`, `LenBetween`). Wrapping a table-level,
aggregate, distinct-count, row-count, or custom-count declaration—or a negative
bound, nil inner expectation, or a second nested tolerance—fails `ValidateTable`
preflight before SQL.

Tolerance changes only the policy verdict after the inner expectation evaluates
once. Raw `Total`, `FailedCount`, `FailedPercent`, samples, and failed keys
remain under existing cap and key options. Empty evaluated populations pass and
are not tolerated. Scope remains the evaluated population for all raw counts.
Execution and configuration errors are never tolerated.

Gate with `report.OK()` or `report.Err()`. Tolerated results count as successful
and are omitted from `report.Failures()`; inspect `Report.Results` and
`Result.Tolerated` for remediation. The wrapper is immutable and may nest with
`WithID` in either order. See [suite and SQL integration](suite.md) and
[reports, errors, rendering, and limits](results.md) for display text, facts,
and nesting details.

## Machine identity

Use `WithID(id, expectation)` to decorate any builder result with a stable
result identity. Read [stable IDs and export](export.md) for preflight rules and
export behavior.
