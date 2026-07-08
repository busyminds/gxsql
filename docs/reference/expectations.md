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

## Machine identity

Use `WithID(id, expectation)` to decorate any builder result with a stable
result identity. Read [stable IDs and export](export.md) for preflight rules and
export behavior.
