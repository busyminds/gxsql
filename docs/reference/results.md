# Reports, Errors, Rendering, and Limits

## Report and Result

`Report` aggregates a validation run. It exposes `Results []Result` in suite
order, `Target *TableRef`, and `ScopeID`, which `ValidateTable` sets when a
scoped validation run is used.

| API                                | Description                                                                                   |
| ---------------------------------- | --------------------------------------------------------------------------------------------- |
| `Report.OK() bool`                 | True when no non-advisory policy failure or result error exists.                              |
| `Report.Failures() []Result`       | Every result with `Success == false`, including advisory failures and errors.                 |
| `Report.GatingFailures() []Result` | Non-advisory policy failures and all result errors.                                           |
| `Report.PolicyFailures() []Result` | Evaluated data-quality failures, excluding result errors.                                     |
| `Report.Warnings()`, `Infos()`     | All results with the matching severity, including passing results.                            |
| `Report.Unexpected()`              | Evaluated results with raw failures, including tolerated outcomes.                            |
| `Report.ToleratedResults()`        | Results whose nonzero raw failures passed an allowance.                                       |
| `Report.ExecutionFailures()`       | Result slots with configuration or execution errors.                                          |
| `Report.Err() error`               | Nil when no hard-gating result exists; otherwise `*ValidationError` with the complete report. |
| `Report.String() string`           | Human-readable report summary and result lines; advisory and tolerated results stay visible.  |
| `Result.String() string`           | Human-readable line prefixed by a pass or failure marker; says `tolerated` when applicable.   |

`Result` is the outcome of one expectation. Its exported fields are:

| Field                                                     | Meaning                                                                                                                             |
| --------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| `ID`, `Kind`                                              | Machine-facing identity; policy metadata never changes either field.                                                                |
| `Name`, `Column`                                          | Human-facing check description and affected column. Do not parse `Name`. Blank `Column` for composite unique and reference results. |
| `Severity`                                                | `SeverityError`, `SeverityWarning`, or `SeverityInfo`; zero value is error.                                                         |
| `Description`, `Tags`                                     | Optional normalized policy metadata. Descriptions are trimmed; tags are sorted and copied.                                          |
| `Success`, `Err`                                          | Policy outcome and a per-expectation failure recorded by `ContinueOnError`.                                                         |
| `Tolerated`                                               | True when a nonzero raw failure count passed within any configured allowance.                                                       |
| `RowDenominator`, `Total`, `FailedCount`, `FailedPercent` | Population metrics; `FailedCount` uses the expectation-specific unit.                                                               |
| `Facts`                                                   | Structured observed values and configured thresholds.                                                                               |
| `SampleValues`, `FailedKeys`                              | Capped diagnostic data.                                                                                                             |

`RowDenominatorAvailable` means `Total` and `FailedPercent` describe a per-row
population. `RowDenominatorUnavailable` marks a table-level check; `Total == 0`
does not mean the table was empty. Custom-count results are one exception to the
usual denominator interpretation: they use `KindCustom` and
`RowDenominatorUnavailable`, but `FailedCount` is a complete non-negative count
even though `Total` and `FailedPercent` are unavailable. Reconcile-count results
use `KindReconcileCountsEqual` and `RowDenominatorUnavailable`; `FailedCount` is
`0` when the dual `COUNT(*)` values are equal and `1` when they differ. `Column`
is blank. Broader metric results (`KindSumBetween`,
`KindPopulationStdDevBetween`, `KindCompletenessRate`, `KindDuplicateRate`,
`KindValueFrequency`, `KindDominantShare`) are also table-level: they keep
`FailedPercent` at its zero value and publish observations only under nested
`Result.Facts` fields. Those rate facts are distinct from `NotNull` / `Unique` /
`Columns(...).Unique()` `FailedPercent`. `WithMaxFailedCount` and
`MaxFailedPercent` apply to denominator-available per-row, uniqueness, and
referential-integrity shapes, including composite uniqueness and references with
or without parent filters. They do not apply to custom-count, reconcile-count,
or broader metric results. `MaxFailedPercent(p)` uses the inclusive unrounded
comparison `FailedCount / Total * 100 <= p` for `p` in `[0, 100]`. Both forms
change `Success` only and preserve raw observations. Empty evaluated populations
pass without division by zero or `NaN` and are not tolerated. Scope remains the
evaluated population for all raw counts. Table-level, aggregate, distinct-count,
row-count, custom-count, reconcile-count, broader-metric, and structural column
wrappers (including catalog nullability and reported-type contracts) fail
preflight. Execution and configuration errors keep `Success: false` and
`Tolerated: false`; non-advisory policy failures gate, while warning/info
failures remain queryable. Catalog schema contracts also use
`RowDenominatorUnavailable`, emit no samples or failed keys, and remain
count-tolerance-ineligible.

`RowKey` is `[]any` containing caller-supplied `WithKey` values in the same
column order. SQL `NULL` components appear as `nil`. Capped `FailedKeys` remain
report diagnostics; complete streaming identity uses `FailingKeys` and never
requires storing the full set on `Report` / `Result`.

Composite uniqueness and referential integrity both report complete local
`FailedCount` values under `RowDenominatorAvailable`: duplicate participating
rows for `KindCompositeUnique`, orphaned local rows for `KindReference`. Samples
and failed keys remain local, capped, and subject to existing privacy controls.
Parent values never appear in diagnostics. Optional parent-filter identity
appears only as `Facts.Reference.ParentFilterID`.

## Structured Facts

`ResultFacts` separates machine-readable values from display text:

- `ObservedCount` and `ObservedFloat` hold evaluated table-level values.
- `ConfiguredCount`, `ConfiguredCountLower`, and `ConfiguredCountUpper` hold
  integer thresholds.
- `ConfiguredFloatLower`, `ConfiguredFloatUpper`, and `ConfiguredFloatBound`
  hold aggregate thresholds.
- `ConfiguredBound`, `ConfiguredBoundLower`, and `ConfiguredBoundUpper` retain
  driver-bound per-row comparison values.
- `ConfiguredMaxFailedCount` holds the inclusive `WithMaxFailedCount` bound when
  that decorator was applied, including raw-zero, above-bound, and
  `ContinueOnError` execution-error outcomes.
- `ConfiguredMaxFailedPercent` holds the inclusive `MaxFailedPercent` bound,
  including raw-zero, above-bound, and `ContinueOnError` execution-error
  outcomes.
- `KeyColumns` names local composite-unique components in declaration order when
  set. Prefer this over parsing `Name`; `Result.Column` stays blank for those
  results.
- `Reference` holds local-to-parent mapping facts for referential checks:
  `LocalColumns`, structured `Parent` (`TableRef`), `ParentColumns`, and
  optional `ParentFilterID`. Nil when the result is not a reference check.
  Predicate text and parent-filter arguments are never published here.
- `Reconcile` holds dual-side count reconciliation facts: structured `Left` and
  `Right` targets, `ObservedLeftCount`, `ObservedRightCount`, fixed
  `Relationship` (`"equal"`), optional `LeftScopeID`, and optional
  `SecondaryFilterID`. Nil when the result is not a reconcile check. Predicate
  text and filter arguments are never published here.
- `Comparison` holds same-row operand and relationship facts: `LeftColumn`,
  `RightColumn`, and `Relationship`. Nil when the result is not a same-row
  column comparison.
- `Ratio` holds same-row integer ratio facts: `LeftColumn`, `RightColumn`, and
  `Bound`. Nil when the result is not a ratio-equality check.
- `ConfiguredTimeStart` and `ConfiguredTimeEnd` hold caller-supplied half-open
  window bounds for `Timestamp(...).InWindow`.
- `ConfiguredTimeCutoff`, `ObservedTime`, and `ObservedTimePresent` hold
  freshness configuration and observation for `Timestamp(...).FreshSince`.
  `ObservedTimePresent` is a `*bool`: nil when freshness does not apply, pointer
  false for explicit absence, and pointer true when `ObservedTime` is set.
- `RequiredColumns` lists expected structural column names in declaration order.
- `MissingColumns` lists absent expected names in declaration order for name
  contracts and for catalog nullability/type contracts when the checked column
  is absent.
- `UnexpectedColumns` lists unexpected discovered names in discovery order for
  `ExactColumns` failures.
- `ConfiguredNullability` and `ObservedNullability` hold catalog nullability
  claims and observations for `KindColumnNullability` as `CatalogNullability`
  values `nullable`, `not_nullable`, or `unknown`. Observed unknown never yields
  a passing policy result.
- `ConfiguredReportedType` and `ObservedReportedType` hold the caller-configured
  and driver-reported type spellings for `KindColumnType`. Comparison is
  byte-for-byte; missing columns omit observed type rather than inventing one.
- `Sum` holds nested `SumFacts` for `KindSumBetween`: integer `Observed` with
  `Exactness` `exact_integer`, or `ObservedFloat` with `Exactness` `float64`,
  plus configured lower/upper bounds. Empty or all-`NULL` input leaves the
  observation pointer nil (absence, not zero or `NaN`).
- `PopulationStdDev` holds nested `PopulationStdDevFacts` for
  `KindPopulationStdDevBetween`: `Observed`, configured bounds, `Algorithm`
  `STDDEV_POP`, and `Exactness` `exact_population`. Empty or all-`NULL` input
  leaves `Observed` nil. Quantile facts are not published.
- `Completeness` holds nested `CompletenessFacts` for `KindCompletenessRate`:
  `NonNullCount`, scoped `TotalCount` (SQL `NULL` rows included), `Rate`, and
  either `ConfiguredBound` for single-sided checks or `ConfiguredLower` /
  `ConfiguredUpper` for `Between`. Distinct from `NotNull` `FailedPercent`.
- `DuplicateRate` holds nested `DuplicateRateFacts` for `KindDuplicateRate`:
  `DuplicateCount`, scoped `TotalCount`, `Rate`, and either `ConfiguredBound`
  for single-sided checks or `ConfiguredLower` / `ConfiguredUpper` for
  `Between`. Distinct from `Unique` / composite-unique `FailedPercent`.
- `Frequency` holds nested `FrequencyFacts` for `KindValueFrequency`:
  `ConfiguredValue` / `ConfiguredNull`, `ValueCount`, `TotalCount`, `Share`, and
  either `ConfiguredBound` for single-sided checks or `ConfiguredLower` /
  `ConfiguredUpper` for `Between`. SQL `NULL` is one category.
- `DominantShare` holds nested `DominantShareFacts` for `KindDominantShare`:
  `DominantCount`, `TotalCount`, `Share`, `TieCount`, and either
  `ConfiguredBound` for single-sided checks or `ConfiguredLower` /
  `ConfiguredUpper` for `Between`. Ties publish the maximum share and tie count
  without selecting a value.

Built-in expectations populate threshold and mapping fields at construction
time. Do not encode composite tuples as comma-separated `Column` text. Broader
metric rates and aggregates live only in these nested facts; do not derive them
from `FailedPercent`.

## Validation Errors

`ValidationError` wraps a failed `Report`; use `errors.As` to recover its
`Report` field.

`PreflightErrors` collects invalid expectation configuration before SQL starts.
Each `PreflightIssue` has the suite `Index`, optional `ID`, and underlying
`Err`. Without `ContinueOnError`, it is returned as the top-level error. It
unwraps its issue errors.

`CategorizedError` wraps an underlying error with a closed `ErrorCategory`:
`invalid_config`, `unsupported`, `rendering`, `database`, `scan`, `context`, or
`observer`. Test a category with `errors.Is` and the matching marker:
`ErrCategoryInvalidConfig`, `ErrCategoryUnsupported`, `ErrCategoryRendering`,
`ErrCategoryDatabase`, `ErrCategoryScan`, `ErrCategoryContext`, or
`ErrCategoryObserver`.

Schema-contract capability refusals use `UnsupportedCapabilityError` under
`CategoryUnsupported` and name the expectation kind, dialect, and missing
capability (`nullability` or `exact_reported_type`). `ColumnNullability` on
`Postgres`, `DuckDB`, and `SQLite` fails that way at preflight. When a dialect
advertises nullability (`MySQL`) but `Rows.ColumnTypes` cannot resolve it for a
column, `UnknownMetadataError` is returned under the same category and never
becomes a passing policy result. Population standard-deviation claims that the
dialect does not advertise fail the same way with capability
`aggregate.population_stddev` and kind `population_stddev_between` (`SQLite` in
the built-in matrix). Use `errors.As` to inspect those typed errors.

## Display Output

`Result.String()` includes at most ten sample values and ten failed keys in
display output, even if more were retained. A tolerated result explicitly says
`tolerated` and still shows raw failed count, total, failed percentage, samples,
and failed keys under those display caps. `Report.String()` renders
`gxsql report: X/Y expectations passed` followed by one result line per
expectation in declaration order; a tolerated policy pass counts as passed and
keeps its tolerance marker. Treat this output as operator-facing text; prefer
`Facts`, IDs, `Tolerated`, and exported DTOs for machines.

For remediation, walk `Report.Results` and read `Result.Tolerated`. Do not rely
on `Failures()` alone: tolerated results have `Success: true` and are omitted
from that slice while remaining in `Results`, display text, and JSON. For the
complete failing identity set, call `FailingKeys` with `ForResultID`,
`ForResultIndex`, or `ForKind`, the original validated `TableRef`, and
`Close`/`Err` handling; do not scrape `String()` output, mutate `Report.Target`
to retarget retrieval, or treat `IncludeFailedKeys` export as a complete
retrieval path.

## Limits

| Constant                       | Value | Scope                                                   |
| ------------------------------ | ----- | ------------------------------------------------------- |
| `DefaultSampleCap`             | 20    | Retained offending samples per result.                  |
| `DefaultFailedKeysCap`         | 100   | Retained failed keys per result when `WithKey` is used. |
| `MaxExportedQueryTextRunes`    | 4096  | Exported SQL text.                                      |
| `MaxExportedArgumentCount`     | 256   | Exported bound arguments.                               |
| `MaxExportedErrorMessageRunes` | 512   | Export-safe error messages.                             |

`WithSampleCap`, `WithFailedKeysCap`, and the suite methods override the
retention defaults. See [operational limits](../concepts/operations.md) for cost
and privacy guidance, and [Results and Remediation](../concepts/results.md) for
`FailingKeys` streaming retrieval.
