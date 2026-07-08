# Reports, Errors, Rendering, and Limits

## Report and result

`Report` aggregates a validation run. It exposes `Results []Result` in suite
order and `Target *TableRef`, which `ValidateTable` sets.

| API                          | Description                                                                      |
| ---------------------------- | -------------------------------------------------------------------------------- |
| `Report.OK() bool`           | True when every result passed.                                                   |
| `Report.Failures() []Result` | Results with `Success == false`, including errors recorded by `ContinueOnError`. |
| `Report.Err() error`         | Nil for a passing report; otherwise `*ValidationError` with the complete report. |
| `Report.String() string`     | Human-readable report summary and result lines.                                  |
| `Result.String() string`     | Human-readable line prefixed by a pass or failure marker.                        |

`Result` is the outcome of one expectation. Its exported fields are:

| Field                                                     | Meaning                                                                     |
| --------------------------------------------------------- | --------------------------------------------------------------------------- |
| `ID`, `Kind`                                              | Machine-facing identity.                                                    |
| `Name`, `Column`                                          | Human-facing check description and affected column. Do not parse `Name`.    |
| `Success`, `Err`                                          | Policy outcome and a per-expectation failure recorded by `ContinueOnError`. |
| `RowDenominator`, `Total`, `FailedCount`, `FailedPercent` | Population metrics.                                                         |
| `Facts`                                                   | Structured observed values and configured thresholds.                       |
| `SampleValues`, `FailedKeys`                              | Capped diagnostic data.                                                     |

`RowDenominatorAvailable` means `Total` and `FailedPercent` describe a per-row
population. `RowDenominatorUnavailable` marks a table-level check; `Total == 0`
does not mean the table was empty.

`RowKey` is `[]any` containing caller-supplied `WithKey` values in the same
column order.

## Structured facts

`ResultFacts` separates machine-readable values from display text:

- `ObservedCount` and `ObservedFloat` hold evaluated table-level values.
- `ConfiguredCount`, `ConfiguredCountLower`, and `ConfiguredCountUpper` hold
  integer thresholds.
- `ConfiguredFloatLower`, `ConfiguredFloatUpper`, and `ConfiguredFloatBound`
  hold aggregate thresholds.
- `ConfiguredBound`, `ConfiguredBoundLower`, and `ConfiguredBoundUpper` retain
  driver-bound per-row comparison values.

Built-in expectations populate threshold fields at construction time.

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
display output, even if more were retained. `Report.String()` renders
`gxsql report: X/Y expectations passed` followed by one result line per
expectation. Treat this output as operator-facing text; prefer `Facts`, IDs, and
exported DTOs for machines.

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
