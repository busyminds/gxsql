# Expectation Builders

Builders return sealed `Expectation` values for `NewSuite`. The selected dialect
validates column and table identifiers during preflight.

## Row Count

`RowCount() RowCountBuilder` starts a table-level row-count expectation.

| Method                      | Policy                             |
| --------------------------- | ---------------------------------- |
| `Equal(want int)`           | Row count equals `want`.           |
| `Between(lo, hi int)`       | `lo <= row count <= hi`.           |
| `GreaterThan(bound int)`    | Row count is greater than `bound`. |
| `GreaterOrEqual(bound int)` | Row count is at least `bound`.     |
| `LessThan(bound int)`       | Row count is less than `bound`.    |
| `LessOrEqual(bound int)`    | Row count is at most `bound`.      |

Row-count results have `RowDenominatorUnavailable`. Per-row fields stay at their
zero values. Observed counts and configured thresholds are available in
`Result.Facts`.

## Structural Columns

`RequiredColumns(names ...string)` and `ExactColumns(names ...string)` are
table-level column-set contracts. Supply one or more separately validated
identifiers. An empty list, duplicate names, or invalid identifiers fails
`ValidateTable` preflight before SQL.

| Builder                            | Policy                                                                                       |
| ---------------------------------- | -------------------------------------------------------------------------------------------- |
| `RequiredColumns(names ...string)` | Every expected name exists on the target; additional discovered names are allowed.           |
| `ExactColumns(names ...string)`    | The discovered column set matches `names` exactly: no missing names and no unexpected names. |

Both builders compare unordered sets. Column order never changes the verdict.
Names compare byte-for-byte against dialect/driver-reported
`database/sql.Rows.Columns()` spellings. gxsql does not lowercase, uppercase, or
otherwise normalize expected names. Callers must supply the physical reported
spelling.

Discovery uses a read-only zero-row probe:

```sql
SELECT * FROM <quoted target> WHERE 1 = 0
```

followed by `Rows.Columns()`. The probe never scans row values and never writes
schema. Missing and unexpected columns on a successfully discovered target are
ordinary table-level policy results. A missing target, inaccessible target,
permission denial, query or render failure, or metadata capability failure is a
typed execution or preflight error (for example `CategoryDatabase`), not a
failed structural result.

Results use `KindRequiredColumns` (`required_columns`) or `KindExactColumns`
(`exact_columns`), leave `Result.Column` blank, and set
`RowDenominatorUnavailable`. They never retain samples or failed keys.
`WithKey`, sample caps, and `SummaryOnly()` do not add diagnostics.
`WithMaxFailedCount` is not eligible; wrapping either builder fails preflight.

Structured facts publish:

- `Result.Facts.RequiredColumns`: expected names in caller declaration order
- `Result.Facts.MissingColumns`: absent expected names in declaration order
- `Result.Facts.UnexpectedColumns`: for `ExactColumns` only, discovered names
  absent from the expected set, in discovery order

These checks do not validate column types, nullability, defaults, or ordinal
position. `WithScope` is incompatible: pairing either expectation with
`WithScope` fails `ValidateTable` preflight rather than ignoring scope. Run a
separate structural suite before content validation when shape fail-fast
matters:

```go
structure := gxsql.NewSuite(
    gxsql.RequiredColumns("id", "event_time", "payload"),
    gxsql.ExactColumns("id", "event_time", "payload"),
)
report, err := structure.ValidateTable(ctx, db, gxsql.Table("ingest_events"),
    gxsql.WithDialect(gxsql.Postgres()),
)
```

## Generic Columns

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

## Composite Columns

`Columns(names ...string) ColumnsBuilder` starts multi-column checks. Supply two
or more separately validated identifiers. Empty names, invalid identifiers,
duplicates within one tuple, and fewer than two columns for composite uniqueness
fail `ValidateTable` preflight before SQL.

| Method                                                 | Policy                                                                                                         |
| ------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------- |
| `Unique()`                                             | Each complete non-`NULL` tuple appears at most once in the scoped local population; every duplicate row fails. |
| `References(parent TableRef, parentColumns ...string)` | Every complete non-`NULL` local tuple resolves to at least one unscoped parent row; orphans fail locally.      |

`Column(name).References(parent, parentColumn)` covers the single-column form
with the same semantics and `KindReference`.

### Composite Uniqueness

`Columns("tenant_id", "order_id").Unique()` extends the single-column NULL
policy to tuples: a row participates only when **every** component is
non-`NULL`. A tuple with any `NULL` component is ignored. `FailedCount` is
duplicate **rows**, never duplicate groups. Suite `WithScope` limits the
evaluated population before duplicate detection. Empty scoped populations pass
vacuously.

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

### Referential Integrity

`Columns("tenant_id", "customer_id").References(gxsql.SchemaTable("public", "customers"), "tenant_id", "id")`
maps local columns to equal-arity parent columns. Parent targets use existing
`Table` / `SchemaTable` construction so schema-qualified parents stay structured
and are rendered through the active dialect.

A local row is evaluated only when every local key component is non-`NULL`. A
row with any `NULL` local component passes (nullable foreign-key policy). A
complete local tuple fails when no parent row matches every mapped component.
Multiple matching parent rows still pass; the check proves existence, not parent
uniqueness. Orphans count as failing **local** rows. Empty local scopes pass.

Partial mappings against a composite parent key are allowed by the API, but are
only correct when the mapped parent columns are unique at that arity; otherwise
unrelated parent rows can match.

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

## Numeric Columns

`Int(name string)` and `Float(name string)` both return `NumberColumn` for
ordered numeric checks. Per-row comparisons treat SQL `NULL` as failing.

| Method                                  | Policy                                                         |
| --------------------------------------- | -------------------------------------------------------------- |
| `Between(lo, hi any)`                   | `lo <= value <= hi`.                                           |
| `GreaterThan(bound any)`                | `value > bound`.                                               |
| `GreaterOrEqual(bound any)`             | `value >= bound`.                                              |
| `LessThan(bound any)`                   | `value < bound`.                                               |
| `LessOrEqual(bound any)`                | `value <= bound`.                                              |
| `AverageBetween(lo, hi float64)`        | The column average is in the inclusive range.                  |
| `MinGreaterOrEqual(bound float64)`      | The column minimum is at least the bound.                      |
| `MaxLessOrEqual(bound float64)`         | The column maximum is at most the bound.                       |
| `RatioEqual(right string, bound int64)` | Integer-only algebraic `value == right * bound` (not SQL `/`). |

The aggregate methods are table-level checks. They pass vacuously when the
column has no non-null numeric value.

`RatioEqual` is available only from `Int(...)`. `Float(...).RatioEqual` fails
preflight. A row passes only when both operands are non-`NULL`, the right-hand
column (denominator) is nonzero, and the algebraic equality holds. A `NULL`
operand or a zero denominator fails the row. The bound is an `int64`
placeholder; the implementation multiplies rather than dividing so fractional
ratios are not truncated. Arithmetic overflow or an unsupported numeric storage
form reported by the database is a `CategoryDatabase` execution error, never a
silently rounded result. Decimal ratios, floating-point ratios, raw operator
strings, and general SQL or arithmetic expressions are not supported—use
`CustomCount` for those forms.

Results use `KindRatioEqual` and populate `Result.Facts.Ratio` with
`LeftColumn`, `RightColumn`, and `Bound`. Ratio equality is an ordinary per-row
expectation: complete counts, sample and failed-key caps, `WithScope`,
`WithMaxFailedCount`, declaration order, `ContinueOnError`, and default export
privacy all retain their existing behavior.

## Same-Row Column Comparisons

`Column(left)` compares two separately validated columns from the same target.
Operators are selected by named methods; there is no public operator-string,
expression, or raw-SQL form.

| Method                               | Relationship |
| ------------------------------------ | ------------ |
| `EqualColumn(right string)`          | `=`          |
| `NotEqualColumn(right string)`       | `<>`         |
| `LessThanColumn(right string)`       | `<`          |
| `LessOrEqualColumn(right string)`    | `<=`         |
| `GreaterThanColumn(right string)`    | `>`          |
| `GreaterOrEqualColumn(right string)` | `>=`         |

```go
gxsql.Column("end_date").GreaterOrEqualColumn("start_date")
gxsql.Column("paid_cents").LessOrEqualColumn("invoice_cents")
```

A row fails when either operand is SQL `NULL` or when the named relationship is
false. An empty table or empty scoped population passes vacuously. Preflight
rejects invalid identifiers and identical left/right operands before SQL.

The first portable fixture families are like-for-like integer/numeric columns
and like-for-like temporal columns. Operands are compared natively: gxsql does
not coerce types, cast to text, or substitute a different predicate. When the
engine rejects an incompatible pair, the failure is a typed execution error
(`CategoryDatabase`), not a rewritten comparison.

Results use distinct kinds (`KindEqualColumn`, `KindNotEqualColumn`,
`KindLessThanColumn`, `KindLessOrEqualColumn`, `KindGreaterThanColumn`,
`KindGreaterOrEqualColumn`) and populate `Result.Facts.Comparison` with
`LeftColumn`, `RightColumn`, and `Relationship` (the fixed operator token).
These shapes reuse ordinary per-row reporting: complete failed counts, capped
samples and failed keys, `WithScope`, `WithMaxFailedCount`, summary mode,
declaration order, `ContinueOnError`, and privacy-safe default export.

## Timestamp Columns

`Timestamp(name string) TimestampColumn` starts temporal window and freshness
checks on one timestamp/datetime column. Callers supply every bound and cutoff
as Go `time.Time`. Bounds are bound SQL parameters; gxsql never interpolates
timestamps as text and never calls database current-time functions such as
`NOW()` or `CURRENT_TIMESTAMP`.

| Method                           | Policy                                                          |
| -------------------------------- | --------------------------------------------------------------- |
| `InWindow(start, end time.Time)` | Half-open per-row window `start <= value < end`.                |
| `FreshSince(cutoff time.Time)`   | Table-level `MAX(column) >= cutoff` over the scoped population. |

Temporal checks use the database and driver timestamp behavior for the selected
dialect. Use a combination that accepts bound Go `time.Time` values and
configure its connection or session timezone consistently with the stored
instants. Comparisons operate on timestamp values, not formatted strings.
Fractional-second precision follows the database and driver; test exact boundary
behavior at the precision your schema preserves.

The built-in conformance matrix covers PostgreSQL, SQLite, DuckDB, and MySQL.
Date-only values, time-only values, implicit timezone conversion, and other
temporal types are not separate gxsql rule inputs.

### Half-Open Window

`Timestamp("event_time").InWindow(start, end)` fails SQL `NULL` values. An empty
table or empty scoped population passes vacuously. Preflight rejects a zero
bound and `end <= start` before SQL. Results use `KindTimestampInWindow`
(`timestamp_in_window`) and publish `Result.Facts.ConfiguredTimeStart` /
`ConfiguredTimeEnd`. Ordinary per-row reporting applies: complete failed counts,
capped samples and failed keys, `WithScope`, `WithMaxFailedCount`, summary mode,
declaration order, `ContinueOnError`, and privacy-safe default export.

```go
windowStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
windowEnd := windowStart.Add(24 * time.Hour)
suite := gxsql.NewSuite(
    gxsql.Timestamp("event_time").InWindow(windowStart, windowEnd),
)
```

### Freshness Cutoff

`Timestamp("ingested_at").FreshSince(cutoff)` reads the maximum non-NULL column
value in scope. It passes when an observed maximum exists and
`observed >= cutoff`. Empty scopes and non-empty all-NULL scopes fail because no
accepted watermark exists. NULL rows do not themselves fail this aggregate when
another non-NULL value supplies the maximum; use `NotNull` when completeness is
required. A maximum later than the cutoff passes: gxsql has no independent idea
of “future” beyond the caller-supplied cutoff. Preflight rejects a zero cutoff.

Results use `KindTimestampFreshSince` (`timestamp_fresh_since`), set
`RowDenominatorUnavailable` (no row total, percentage, samples, or failed keys),
and publish `ConfiguredTimeCutoff` plus `ObservedTime` / `ObservedTimePresent`.
Present is a `*bool`: nil when freshness does not apply, pointer false for
explicit absence, and pointer true when `ObservedTime` is set.

```go
cutoff := time.Date(2026, 7, 1, 23, 30, 0, 0, time.UTC)
suite := gxsql.NewSuite(
    gxsql.Timestamp("ingested_at").FreshSince(cutoff),
)
```

## String Columns

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

## Custom Counts

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

## Bounded Failure Tolerance

`WithPolicy(exp, Policy)` decorates an expectation with severity, optional
description and tags, and at most one rate tolerance. `SeverityError` is the
zero value. `SeverityWarning` and `SeverityInfo` keep policy failures queryable
without gating a completed report. Configuration and execution errors always
gate and are never tolerated.

`MaxFailedPercent(p)` is the canonical rate tolerance. `p` is an inclusive
percentage in `[0, 100]`. A denominator-available result passes when
`FailedCount / Total * 100 <= p`, before display rounding. Raw-zero and empty
evaluated populations pass and are not tolerated. Per-row, uniqueness, and
referential-integrity expectations qualify. Table-level, aggregate,
distinct-count, row-count, custom-count, and structural column declarations fail
preflight. A second tolerance form in one decorated expectation also fails
preflight.

`WithMaxFailedCount(max int, exp Expectation)` remains the inclusive
non-negative maximum failed-row count form. It applies to the same eligible
per-row, uniqueness, and referential-integrity shapes, including composite
uniqueness, same-row comparisons, ratios, numeric bounds, string checks, and
timestamp windows. Existing count-tolerance behavior is unchanged.

Tolerance changes only the policy verdict after the inner expectation evaluates
once. Raw `Total`, `FailedCount`, `FailedPercent`, samples, and failed keys
remain under existing caps and key options. Scope remains the evaluated
population for all raw counts.

Descriptions are trimmed; blank descriptions are omitted. Tags are trimmed,
sorted lexicographically, and copied immutably. Blank or duplicate tags fail
preflight. Metadata never changes `WithID`, `Kind`, or gating. The decorator may
nest with `WithID` and the count wrapper in either order when only one tolerance
form is present.

Use `Report.Results` and the focused report filters to inspect warning, info,
raw unexpected, tolerated, policy-failure, and execution-failure outcomes.

## Machine Identity

Use `WithID(id, expectation)` to decorate any builder result with a stable
result identity. Read [stable IDs and export](export.md) for preflight rules and
export behavior.
