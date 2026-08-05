# Reports, Errors, Rendering, and Limits

## Report and result

`Report` aggregates a validation run. It exposes `Results []Result` in suite
order and `Target *TableRef`, which `ValidateTable` sets.

| API                          | Description                                                                                                  |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `Report.OK() bool`           | True when every result passed, including tolerated policy passes.                                            |
| `Report.Failures() []Result` | Results with `Success == false`, including errors recorded by `ContinueOnError`. Omits tolerated results.    |
| `Report.Err() error`         | Nil for a passing report (tolerated passes included); otherwise `*ValidationError` with the complete report. |
| `Report.String() string`     | Human-readable report summary and result lines; tolerated passes stay visible.                               |
| `Result.String() string`     | Human-readable line prefixed by a pass or failure marker; says `tolerated` when applicable.                  |

`Result` is the outcome of one expectation. Its exported fields are:

| Field                                                     | Meaning                                                                                                                             |
| --------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| `ID`, `Kind`                                              | Machine-facing identity.                                                                                                            |
| `Name`, `Column`                                          | Human-facing check description and affected column. Do not parse `Name`. Blank `Column` for composite unique and reference results. |
| `Success`, `Err`                                          | Policy outcome and a per-expectation failure recorded by `ContinueOnError`.                                                         |
| `Tolerated`                                               | True when a nonzero raw failure count passed within `WithMaxFailedCount`.                                                           |
| `RowDenominator`, `Total`, `FailedCount`, `FailedPercent` | Population metrics; `FailedCount` uses the expectation-specific unit.                                                               |
| `Facts`                                                   | Structured observed values and configured thresholds.                                                                               |
| `SampleValues`, `FailedKeys`                              | Capped diagnostic data.                                                                                                             |

`RowDenominatorAvailable` means `Total` and `FailedPercent` describe a per-row
population. `RowDenominatorUnavailable` marks a table-level check; `Total == 0`
does not mean the table was empty. Custom-count results are the exception to the
usual denominator interpretation: they use `KindCustom` and
`RowDenominatorUnavailable`, but `FailedCount` is a complete non-negative count
even though `Total` and `FailedPercent` are unavailable. `Column` is blank and
custom counts never retain samples or failed keys; `WithKey`, sample caps, and
`SummaryOnly()` do not change this shape.

`WithMaxFailedCount` applies only to per-row and uniqueness shapes
(`RowDenominatorAvailable`), including composite uniqueness and referential
integrity. It changes `Success` only. Raw observations stay complete under
existing caps. Empty evaluated populations pass without division by zero or
`NaN` and are not tolerated. Scope remains the evaluated population for all raw
counts. Table-level, aggregate, distinct-count, row-count, and custom-count
wrappers fail preflight. Execution and configuration errors keep
`Success: false` and `Tolerated: false`.

`RowKey` is `[]any` containing caller-supplied `WithKey` values in the same
column order.

Composite uniqueness and referential integrity both report complete local
`FailedCount` values under `RowDenominatorAvailable`: duplicate participating
rows for `KindCompositeUnique`, orphaned local rows for `KindReference`. Samples
and failed keys remain local, capped, and subject to existing privacy controls.
Parent values never appear in diagnostics.

## Structured facts

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
- `KeyColumns` names local composite-unique components in declaration order when
  set. Prefer this over parsing `Name`; `Result.Column` stays blank for those
  results.
- `Reference` holds local-to-parent mapping facts for referential checks:
  `LocalColumns`, structured `Parent` (`TableRef`), and `ParentColumns`. Nil
  when the result is not a reference check.

Built-in expectations populate threshold and mapping fields at construction
time. Do not encode composite tuples as comma-separated `Column` text.

## Validation errors

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

## Display output

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
from that slice while remaining in `Results`, display text, and JSON.

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
and privacy guidance.
