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
position. Use the schema contracts below for catalog nullability and exact
reported-type claims. `WithScope` is incompatible: pairing either expectation
with `WithScope` fails `ValidateTable` preflight rather than ignoring scope. Run
a separate structural suite before content validation when shape fail-fast
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

## Schema Contracts

Catalog nullability and exact reported-type contracts extend the structural
family beyond name presence. They gate on driver-reported metadata from a
read-only zero-row probe plus `database/sql.Rows.ColumnTypes`, not on sampled
row values. gxsql does not invent a universal SQL type system or silently map
type names across engines.

### Catalog Nullability

`ColumnNullability(name string) ColumnNullabilityBuilder` starts a catalog
nullability contract for one validated identifier.

| Method          | Policy                                                                |
| --------------- | --------------------------------------------------------------------- |
| `NotNullable()` | The column is advertised `NOT NULL` by `Rows.ColumnTypes` metadata.   |
| `Nullable()`    | The column is advertised NULL-capable by `Rows.ColumnTypes` metadata. |

These assert **catalog** nullability. They do not replace content
`Column(...).NotNull()` / `IsNull()`, which still measure current-row NULL
rates. A catalog pass does not satisfy a content null check, and a content pass
does not prove catalog nullability.

Results use `KindColumnNullability` (`column_nullability`), set `Result.Column`
to the checked name, and use `RowDenominatorUnavailable`. Structured facts
publish `ConfiguredNullability` and, when the column is present,
`ObservedNullability` as `CatalogNullability` values `nullable`, `not_nullable`,
or `unknown`.

### Exact Reported Type

`ColumnType(name string) ColumnTypeBuilder` starts an exact reported-type
contract. `ReportedAs(typeName string)` requires the driver-reported type name
to equal `typeName` **byte-for-byte**. gxsql does not lowercase, uppercase, trim
modifiers, or equate dialect synonyms (`INTEGER` vs `INT` vs `INT4`). Callers
must supply the exact spelling their selected dialect and driver report through
`ColumnType.DatabaseTypeName()`.

Results use `KindColumnType` (`column_type`), set `Result.Column` to the checked
name, and use `RowDenominatorUnavailable`. Facts publish
`ConfiguredReportedType` and, when the column is present,
`ObservedReportedType`. Cross-engine type equality is not promised.

```go
structure := gxsql.NewSuite(
    gxsql.RequiredColumns("id", "email"),
    gxsql.ColumnNullability("email").NotNullable(), // MySQL-advertised nullability
    gxsql.ColumnType("id").ReportedAs("BIGINT"),   // dialect-exact spelling
)
report, err := structure.ValidateTable(ctx, db, gxsql.Table("users"),
    gxsql.WithDialect(gxsql.MySQL()),
)
```

### Table-Level Semantics

Both builders are table-level structural contracts:

- Discovery uses `SELECT * FROM <quoted target> WHERE 1 = 0` followed by
  `Rows.ColumnTypes()`. The probe never scans row values and never writes
  schema.
- Column names still compare byte-for-byte against driver-reported spellings.
- Results never retain samples or failed keys. `WithKey`, sample caps, and
  `SummaryOnly()` add nothing.
- `WithMaxFailedCount` and `MaxFailedPercent` are ineligible; wrapping either
  builder fails preflight.
- `WithScope` is incompatible and fails `ValidateTable` preflight.
- `When(...)` eligibility is rejected for structural expectations at preflight.

### Missing Columns and Fail-Closed Behavior

When the named column is absent after successful discovery, the result is an
ordinary table-level **policy** failure: `Success == false`,
`Facts.MissingColumns` lists that column, and observed nullability/type fields
are not invented. Missing-column misses are not treated as type or nullability
matches.

Unsupported claims fail closed before discovery SQL:

- Dialects that omit `SchemaMetadataDialect`, or that advertise
  `Nullability: false` / `ExactReportedType: false` for the requested claim,
  fail suite preflight with `CategoryUnsupported` and
  `UnsupportedCapabilityError` naming kind, dialect, and missing capability
  (`nullability` or `exact_reported_type`). No discovery SQL runs.
  `ColumnNullability` therefore refuses at preflight on `Postgres`, `DuckDB`,
  and `SQLite`.
- When a dialect advertises nullability support (`MySQL`) but
  `ColumnType.Nullable()` returns unknown (`ok == false`), evaluation fails
  closed with `CategoryUnsupported` and `UnknownMetadataError` naming kind,
  dialect, column, and capability `nullability`. That outcome never becomes a
  passing policy result. Under `ContinueOnError`, the slot keeps
  `ObservedNullability: unknown` with `Result.Err` set.
- Metadata lookup, render, and inaccessible-target failures remain typed
  execution or preflight errors, not policy mismatches.

### Per-Dialect `Rows.ColumnTypes` Capability Matrix

Built-in dialects advertise schema-metadata claims through
`SchemaMetadataDialect`. Discovery reads `Rows.ColumnTypes()` after the zero-row
probe. Advertised support means gxsql trusts affirmative metadata from the
conformance-supported driver; `ok == false` fails closed. Drivers outside that
support matrix are not covered by the built-in capability claim.

| Dialect    | Catalog nullability | Exact reported type | Metadata source for advertised claims                                                                                 |
| ---------- | ------------------- | ------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `MySQL`    | supported           | supported           | `Rows.ColumnTypes` (`Nullable`, `DatabaseTypeName`)                                                                   |
| `Postgres` | unsupported         | supported           | Type via `Rows.ColumnTypes` (`DatabaseTypeName`); nullability refused at preflight (`pgx` omits `ColumnTypeNullable`) |
| `DuckDB`   | unsupported         | supported           | Type via `Rows.ColumnTypes` (`DatabaseTypeName`); nullability refused at preflight (`Nullable` returns `ok == false`) |
| `SQLite`   | unsupported         | supported           | Type via `Rows.ColumnTypes` (`DatabaseTypeName`); nullability refused at preflight (untruthful `Nullable`)            |

Only `MySQL` advertises catalog nullability through `Rows.ColumnTypes`. Exact
reported-type claims remain available on every built-in dialect; supply the
driver-reported spelling for that engine. Precision/scale identity, ordinal
position, and cross-engine type families are out of scope for these builders.

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
| `CompletenessRate()` | Starts a completeness-rate builder over scoped rows.                         |
| `DuplicateRate()`    | Starts a duplicate-rate builder over scoped rows.                            |
| `Frequency(value)`   | Starts a category-share builder for one value (nil means SQL `NULL`).        |
| `DominantShare()`    | Starts a maximum category-share builder with deterministic tie facts.        |

`IsNull` / `NotNull` are content checks over row values. For catalog nullability
of a named column, use `ColumnNullability(...).Nullable()` / `NotNullable()`
instead.

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

### Completeness and Duplicate Rates

`CompletenessRate()` and `DuplicateRate()` return builders whose
`GreaterOrEqual`, `LessOrEqual`, and `Between` methods compare a derived rate in
`[0, 1]` against inclusive bounds. These are dedicated metric APIs: they do not
rename or wrap `NotNull` / `Unique`, and they do not overload
`Result.FailedPercent`.

| Builder              | Numerator                                            | Denominator                                 | Kind                   |
| -------------------- | ---------------------------------------------------- | ------------------------------------------- | ---------------------- |
| `CompletenessRate()` | Non-`NULL` row count                                 | Scoped row count (SQL `NULL` rows included) | `KindCompletenessRate` |
| `DuplicateRate()`    | Rows that participate in non-`NULL` duplicate groups | Scoped row count (SQL `NULL` rows included) | `KindDuplicateRate`    |

`DuplicateRate` ignores SQL `NULL` when forming duplicate groups, matching the
single-column uniqueness null policy, then divides by the full scoped row count.
`NotNull` `FailedPercent` remains the share of rows that are SQL `NULL`.
`Unique` / `Columns(...).Unique()` `FailedPercent` remains the share of scoped
rows in duplicate groups under those builders' own denominators. Do not treat
`1 - FailedPercent/100` as a completeness contract.

Results are table-level: `RowDenominatorUnavailable`, `FailedPercent` stays at
its zero value, and observations live in `Result.Facts.Completeness` or
`Result.Facts.DuplicateRate` (`NonNullCount` or `DuplicateCount`, `TotalCount`,
`Rate`, and either `ConfiguredBound` for single-sided checks or
`ConfiguredLower` / `ConfiguredUpper` for `Between`). Empty scopes pass without
divide-by-zero or `NaN`; count facts may be present while `Rate` stays absent.
Samples and failed keys are not retained. `WithMaxFailedCount` and
`MaxFailedPercent` are ineligible.

```go
gxsql.Column("email").CompletenessRate().GreaterOrEqual(0.99)
gxsql.Column("email").DuplicateRate().LessOrEqual(0.05)
```

### Value Frequency and Dominant Share

`Frequency(value any)` and `DominantShare()` return builders with the same
`GreaterOrEqual` / `LessOrEqual` / `Between` rate comparisons in `[0, 1]`.

`Frequency(value)` measures one category's share of scoped rows. Pass a concrete
value for equality matching, or `nil` to select the SQL `NULL` category. `NULL`
participates in the scoped denominator like any other category. Results use
`KindValueFrequency` and publish `Result.Facts.Frequency` with `ConfiguredValue`
/ `ConfiguredNull`, `ValueCount`, `TotalCount`, `Share`, and either
`ConfiguredBound` for single-sided checks or `ConfiguredLower` /
`ConfiguredUpper` for `Between`.

`DominantShare()` measures the maximum category share. When several categories
tie at that maximum, facts publish `TieCount` and do not select a representative
value. Results use `KindDominantShare` and publish `Result.Facts.DominantShare`
with `DominantCount`, `TotalCount`, `Share`, `TieCount`, and either
`ConfiguredBound` for single-sided checks or `ConfiguredLower` /
`ConfiguredUpper` for `Between`.

Both shapes are table-level (`RowDenominatorUnavailable`). Empty scopes pass
without `NaN`; frequency may publish zero counts with an absent `Share`, while
dominant share may leave count/share facts absent when no category exists.
Samples and failed keys are not retained. Count tolerance wrappers are
ineligible. These builders are not membership checks: use `In` / `NotIn` when
every row must belong to or avoid a value set.

```go
gxsql.Column("status").Frequency("ready").GreaterOrEqual(0.4)
gxsql.Column("status").Frequency(nil).LessOrEqual(0.1)
gxsql.Column("status").DominantShare().LessOrEqual(0.8)
```

## Composite Columns

`Columns(names ...string) ColumnsBuilder` starts multi-column checks. Supply two
or more separately validated identifiers. Empty names, invalid identifiers,
duplicates within one tuple, and fewer than two columns for composite uniqueness
fail `ValidateTable` preflight before SQL.

| Method                                                 | Policy                                                                                                                                        |
| ------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `Unique()`                                             | Each complete non-`NULL` tuple appears at most once in the scoped local population; every duplicate row fails.                                |
| `References(parent TableRef, parentColumns ...string)` | Every complete non-`NULL` local tuple resolves to at least one parent row; optional `WithParentFilter` narrows parents; orphans fail locally. |

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

`WithScope` applies only to the local validated table. Parent lookup never
reuses suite scope. Narrow the parent population with an explicit parent filter
instead:

| API                                                                   | Role                                                                 |
| --------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `TrustedParentFilter(id, predicate string, args ...any) ParentFilter` | Builds an immutable trusted parent-side predicate with bound values. |
| `(reference).WithParentFilter(filter ParentFilter)`                   | Applies that predicate only inside the parent `NOT EXISTS` lookup.   |

`ParentFilter` is a distinct type from `Scope` and from reconciliation
`SecondaryFilter`. Suite scope cannot be passed as a parent filter.
`TrustedParentFilter` predicates are trusted Go-code input, not a sandbox for
untrusted SQL. Keep the predicate text fixed in application code. Bind values
through `?` placeholders; placeholder arity must match the supplied values.
Never pass user-authored predicate text.

When both `WithScope` and `WithParentFilter` are set, they remain independent:
suite scope limits local rows; the parent filter limits which parent rows can
satisfy the reference. Filter identity is published as
`Result.Facts.Reference.ParentFilterID`. Predicate text and bound arguments are
never published in facts, default display output, or default export.

Results use `KindReference`, leave `Result.Column` blank, and populate
`Result.Facts.Reference` with `LocalColumns`, structured `Parent` (`TableRef`),
`ParentColumns`, and optional `ParentFilterID`. Samples and failed keys are
local-only under existing caps; parent values and parent keys are never emitted.
Preflight rejects empty mappings, unequal arity, invalid or duplicate
identifiers, invalid parent filters, and unsupported dialect capability before
SQL. Parent-filtered references remain eligible for `WithMaxFailedCount` and
`MaxFailedPercent`.

```go
suite := gxsql.NewSuite(
    gxsql.Columns("tenant_id", "customer_id").References(
        gxsql.SchemaTable("public", "customers"), "tenant_id", "id",
    ).WithParentFilter(
        gxsql.TrustedParentFilter("customers-active", "status = ?", "active"),
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
| `SumBetween(lo, hi any)`                | The column `SUM` is in the inclusive range.                    |
| `StdDevBetween(lo, hi float64)`         | Population `STDDEV_POP` is in the inclusive range.             |
| `RatioEqual(right string, bound int64)` | Integer-only algebraic `value == right * bound` (not SQL `/`). |

The aggregate methods are table-level checks. They pass vacuously when the
column has no non-null numeric value. Quantile builders are not provided. Shared
multi-aggregate execution is not provided: each aggregate expectation evaluates
independently in declaration order.

### Sum Bounds

`SumBetween(lo, hi)` requires `SUM(column)` to lie in the inclusive range. SQL
`NULL` values are excluded from the sum. Empty or all-`NULL` input passes with
an **absent** observed sum fact (pointer nil)—absence is not encoded as zero and
never as `NaN`.

| Builder path            | Observation path           | `Exactness` label | Facts                                                           |
| ----------------------- | -------------------------- | ----------------- | --------------------------------------------------------------- |
| `Int(...).SumBetween`   | Exact integer `SUM`        | `exact_integer`   | `Observed`, `ConfiguredLower`, `ConfiguredUpper`                |
| `Float(...).SumBetween` | Documented `float64` `SUM` | `float64`         | `ObservedFloat`, `ConfiguredFloatLower`, `ConfiguredFloatUpper` |

Results use `KindSumBetween` (`sum_between`), set `Result.Column`, use
`RowDenominatorUnavailable`, and publish nested `Result.Facts.Sum`. Integer
bounds must be integers; float bounds must be finite; `lo > hi` fails preflight.
Engine overflow on the integer path, or a non-finite floating sum, is a
`CategoryDatabase` execution error—never a silent wrapped pass. gxsql does not
claim cross-engine float bit-identity, and default builders are exact (no
approximate sum mode).

```go
gxsql.Int("amount").SumBetween(0, 1_000_000)
gxsql.Float("amount").SumBetween(0.0, 1_000_000.0)
```

### Population Standard Deviation

`StdDevBetween(lo, hi float64)` requires the **population** standard deviation
of non-`NULL` values to lie in the inclusive range. The algorithm label is
`STDDEV_POP` with exactness `exact_population`. Results use
`KindPopulationStdDevBetween` (`population_stddev_between`), set
`RowDenominatorUnavailable`, and publish `Result.Facts.PopulationStdDev`
(`Observed`, `ConfiguredLower`, `ConfiguredUpper`, `Algorithm`, `Exactness`).
Empty or all-`NULL` input passes with an absent observation (never `NaN`).

Dialects advertise support through `AggregateMetricsDialect`. Built-in support:

| Dialect    | Population `STDDEV_POP` |
| ---------- | ----------------------- |
| `Postgres` | supported               |
| `DuckDB`   | supported               |
| `MySQL`    | supported               |
| `SQLite`   | unsupported             |

Unsupported dialects fail closed at suite preflight with `CategoryUnsupported`
and `UnsupportedCapabilityError` naming kind `population_stddev_between`, the
dialect, and capability `aggregate.population_stddev`. No SQL runs. There is no
sample-standard-deviation builder and no quantile builder on this path.

```go
gxsql.Float("amount").StdDevBetween(0.0, 25.0)
```

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
fails each string policy. Empty tables and empty scoped populations pass
vacuously.

| Method                   | Policy                                                     |
| ------------------------ | ---------------------------------------------------------- |
| `NotEmpty()`             | Every string is non-empty.                                 |
| `Empty()`                | Every string is empty.                                     |
| `LenEqual(n int)`        | Every database string length equals `n`.                   |
| `LenBetween(lo, hi int)` | Every database string length is in the inclusive range.    |
| `HasPrefix(prefix)`      | Value starts with the literal fragment `prefix`.           |
| `HasSuffix(suffix)`      | Value ends with the literal fragment `suffix`.             |
| `Contains(substr)`       | Value contains the literal fragment `substr`.              |
| `Like(pattern)`          | Value matches the caller-owned SQL `LIKE` pattern.         |
| `NotLike(pattern)`       | Value does not match the caller-owned SQL `LIKE` pattern.  |
| `Regex(pattern)`         | Value matches under a dialect-advertised regex capability. |

Length uses the dialect's SQL length expression—`CHAR_LENGTH` or `LENGTH`—not Go
rune counting.

### Portable LIKE-Family Patterns

`HasPrefix`, `HasSuffix`, and `Contains` treat the argument as a **literal
fragment**. Before rendering SQL `LIKE`, gxsql escapes backslash, `%`, and `_`
and adds a dialect-safe `ESCAPE` clause so caller data never becomes wildcards
by default. The fragment is then wrapped with `%` as needed (`prefix%`,
`%suffix`, or `%substr%`) and bound as a placeholder.

`Like` and `NotLike` treat the argument as a **raw SQL LIKE pattern**. Callers
own wildcards; gxsql binds the pattern without automatic escaping and without an
`ESCAPE` clause. Use these when you intentionally need `%` / `_` metacharacters.

All five builders validate identifiers, bind values as placeholders, honor
`WithScope`, publish distinct kinds (`has_prefix`, `has_suffix`, `contains`,
`like`, `not_like`), set `Result.Column`, and remain eligible for
`WithMaxFailedCount`. Case folding follows the engine and collation; gxsql does
not claim portable case-insensitive matching.

```go
gxsql.String("code").HasPrefix("ACME-")
gxsql.String("path").Contains("/inbox/")
gxsql.String("email").Like("%@example.com")
gxsql.String("sku").NotLike("%-TMP")
```

gxsql does not ship email, URL, phone, or other format-catalog builders. Express
formats with `Like`, capability-gated `Regex`, or `CustomCount` recipes.

### Capability-Gated Regex

`Regex(pattern)` runs only when the selected dialect implements `RegexDialect`
and advertises a complete `RegexCapability` (name, operator or function, flags,
match mode, null behavior, and Unicode limits). Built-in support:

| Dialect    | Advertised? | Operator | Match mode |
| ---------- | ----------- | -------- | ---------- |
| `Postgres` | yes         | `~`      | substring  |
| `DuckDB`   | yes         | `~`      | substring  |
| `MySQL`    | yes         | `REGEXP` | substring  |
| `SQLite`   | no          | —        | —          |

Unsupported dialects fail closed at suite preflight with `CategoryUnsupported`
and an `UnsupportedCapabilityError` naming kind `regex`, the dialect label, and
capability `regex`. No SQL runs, and gxsql never rewrites regex to `LIKE`. Under
`ContinueOnError`, the same static capability failure occupies the
declaration-order slot as `Result.Err` before later rules execute.

Advertised engines still differ in flags, Unicode, and regex dialect. gxsql
documents per-dialect metadata and does not claim cross-engine regex parity.
Anchor patterns yourself when you need a whole-string match under substring
operators.

```go
gxsql.String("ref").Regex(`^[A-Z]{3}-[0-9]+$`)
```

### Pattern Export Privacy

Pattern literals appear in in-memory `Result.Name` for local debugging. Default
`ExportReport` display names redact them to forms such as
`code has prefix (...)`, `path contains (...)`, `email like (...)`, and
`ref regex (...)`. Bound pattern arguments stay out of default export; capture
them only with `CaptureQueryDiagnostics` plus opt-in diagnostic export. Samples
and failed keys keep their existing opt-in rules.

## Reconcile Counts

`ReconcileCounts(secondary TableRef) ReconcileCountsBuilder` starts a
suite-bound `COUNT(*)` reconciliation. The table passed to `ValidateTable` is
always the left side; `secondary` is the explicit right side.

| Method                                        | Policy                                             |
| --------------------------------------------- | -------------------------------------------------- |
| `WithSecondaryFilter(filter SecondaryFilter)` | Applies `filter` only to the secondary `COUNT(*)`. |
| `Equal()`                                     | Left and right `COUNT(*)` values must be equal.    |

`TrustedSecondaryFilter(id, predicate string, args ...any) SecondaryFilter`
builds the optional secondary predicate. `SecondaryFilter` is a distinct type
from `Scope` and `ParentFilter`. Suite scope cannot be reused as a secondary
filter. Secondary-filter predicates are trusted Go-code input with `?`
placeholders and separately bound values—not a sandbox for user-authored SQL.

Suite `WithScope` applies only to the left `COUNT(*)`. It never narrows the
secondary table. Use `WithSecondaryFilter` when the right side needs its own
population. Filter identity is published as
`Result.Facts.Reconcile.SecondaryFilterID`. Left suite-scope identity is
published as `Result.Facts.Reconcile.LeftScopeID` when `WithScope` is set.
Predicate text and bound arguments are never published in facts, default display
output, or default export.

Equality yields `FailedCount` 0; inequality yields `FailedCount` 1. Results use
`KindReconcileCountsEqual` (`reconcile_counts_equal`), leave `Column` blank, set
`RowDenominatorUnavailable`, and populate `Result.Facts.Reconcile` with left and
right targets, observed counts, fixed `Relationship` `"equal"`, and optional
scope or secondary-filter identities. Samples and failed keys are never
retained. `WithKey`, sample caps, and `SummaryOnly()` do not add diagnostics.
`WithMaxFailedCount` and `MaxFailedPercent` are not eligible; wrapping a
reconcile expectation fails preflight.

```go
suite := gxsql.NewSuite(
    gxsql.ReconcileCounts(gxsql.Table("orders_served")).
        WithSecondaryFilter(
            gxsql.TrustedSecondaryFilter("served-ready", "status = ?", "ready"),
        ).
        Equal(),
)
report, err := suite.ValidateTable(ctx, db, gxsql.Table("orders"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithScope(gxsql.TrustedScope("tenant-acme", "tenant_id = ?", tenantID)),
)
```

Built-in reconciliation covers only dual `COUNT(*)` equality. Keep using
`CustomCount` for joins, `GROUP BY` / `HAVING`, `SUM` / `AVG` or other
aggregates across sides, non-equality relationships, and other exotic
cross-table recipes. See [suite and SQL integration](suite.md) for those
patterns.

## Custom Counts

`TrustedCountQuery(template string, args ...any) CountQuery` creates an
immutable trusted SQL template. Use `CustomCount(name string, query CountQuery)`
to add it to a suite. Template text is application-owned Go code, not a sandbox
for user-authored SQL.

Prefer `ReconcileCounts(...).Equal()` for suite-bound dual `COUNT(*)` equality.
Remain on `CustomCount` when the check needs joins, grouped aggregates,
non-equality relationships, or other SQL shapes outside that built-in contract.

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
referential-integrity expectations qualify, including parent-filtered
references. Table-level, aggregate, distinct-count, row-count, custom-count,
reconcile-count, and structural column declarations (including catalog
nullability and reported-type contracts) fail preflight. A second tolerance form
in one decorated expectation also fails preflight.

`WithMaxFailedCount(max int, exp Expectation)` remains the inclusive
non-negative maximum failed-row count form. It applies to the same eligible
per-row, uniqueness, and referential-integrity shapes, including composite
uniqueness, references with or without `WithParentFilter`, same-row comparisons,
ratios, numeric bounds, string and pattern checks, and timestamp windows.
Existing count-tolerance behavior is unchanged. `ReconcileCounts` and
`CustomCount` remain ineligible.

Tolerance changes only the policy verdict after the inner expectation evaluates
once. Raw `Total`, `FailedCount`, `FailedPercent`, samples, and failed keys
remain under existing caps and key options. Suite scope remains the outer
population; when rule eligibility is applied, only eligible rows inside that
scope form the evaluated population for raw counts, percentages, and tolerance.

Descriptions are trimmed; blank descriptions are omitted. Tags are trimmed,
sorted lexicographically, and copied immutably. Blank or duplicate tags fail
preflight. Metadata never changes `WithID`, `Kind`, or gating. The decorator may
nest with `WithID` and the count wrapper in either order when only one tolerance
form is present.

Use `Report.Results` and the focused report filters to inspect warning, info,
raw unexpected, tolerated, policy-failure, and execution-failure outcomes.

## Rule Eligibility

`TrustedEligibility(id, predicate string, args ...any) Eligibility` builds an
immutable trusted eligibility predicate. Predicate text is trusted Go-code SQL
with `?` placeholders and separately bound values—not a sandbox for
user-authored SQL.

`When(eligibility Eligibility, exp Expectation) Expectation` applies that
predicate to one expectation. Eligibility narrows which rows inside the suite
scope are subject to the rule; it does not replace `WithScope`. See
[suite and SQL integration](suite.md) for scope composition and pack examples.

Supported shapes: ordinary per-row (row-denominator), uniqueness, composite
uniqueness, and referential integrity (including parent-filtered references).
Table-level, aggregate, distinct-count, custom-count, reconcile-count, and
structural expectations (including catalog nullability and reported-type
contracts) reject eligibility at `ValidateTable` preflight. Nested `When`
wrappers are configuration errors.

Eligible rows define `Total` and the denominator for percentages and tolerance.
Ineligible rows neither pass nor fail. Zero eligible rows use the existing
vacuous-pass behavior. `Report.ScopeID` remains the suite scope identity.
Default validation errors, display output, and exports omit eligibility
predicate text and bound arguments.

## Machine Identity

Use `WithID(id, expectation)` to decorate any builder result with a stable
result identity. Read [stable IDs and export](export.md) for preflight rules and
export behavior.
