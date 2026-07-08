# Stable IDs and Report Export

## Stable IDs and kinds

`WithID(id string, exp Expectation) Expectation` decorates an expectation with a
caller-supplied stable ID. It preserves the expectation policy while setting
`Result.ID`.

Blank IDs and duplicate IDs are preflight errors. Without `ContinueOnError`,
validation returns `*PreflightErrors` before SQL starts. With it, the affected
result has `Err` and later expectations still run. IDs are never derived from
`Result.Name`.

`ExpectationKind` is the stable category of a built-in expectation. The `Kind*`
constants cover row-count, per-row predicate, distinct-count, and aggregate
builders; `KindCustom` means built-in metadata is unavailable. Use `Kind` and
`ID` for machine joins, not display text.

## ExportReport

`ExportSchemaVersion` is currently `gxsql.report.v1`.

```go
exported, err := gxsql.ExportReport(report,
    gxsql.IncludeSamples(),
)
```

`ExportReport(report, opts...) (ExportedReport, error)` converts a `Report` to a
versioned, encoding-only JSON DTO. On error it returns no partial DTO.

Defaults protect diagnostics: samples, failed keys, captured SQL, and bound
arguments are omitted. Configured thresholds are exported in
`facts.configured_*`, and default `display_name` redacts bound literals.

| `ExportOption`                 | Effect                                                                                                                      |
| ------------------------------ | --------------------------------------------------------------------------------------------------------------------------- |
| `IncludeSamples()`             | Exports normalized `SampleValues` and cap metadata when failures exist.                                                     |
| `IncludeFailedKeys()`          | Exports normalized `FailedKeys` and cap metadata when failures exist.                                                       |
| `IncludeCapturedDiagnostics()` | Exports redacted, length-capped SQL captured with `CaptureQueryDiagnostics()`.                                              |
| `IncludeCapturedArguments()`   | Exports normalized, count-capped arguments with captured SQL; also requires `CaptureQueryDiagnostics()`.                    |
| `WithQueryRedactor(fn)`        | Applies `fn` after identifier redaction and initial SQL truncation; its output is truncated again. It must return a string. |
| `WithArgsRedactor(fn)`         | Redacts each exported bound argument.                                                                                       |
| `WithSampleRedactor(fn)`       | Redacts each exported sample value.                                                                                         |
| `WithKeyRedactor(fn)`          | Redacts each exported failed key.                                                                                           |

A redactor error or panic fails export closed.

## Exported types

| Type                  | JSON role                                                                              |
| --------------------- | -------------------------------------------------------------------------------------- |
| `ExportedReport`      | Schema version, optional target/scope, and declaration-ordered results.                |
| `ExportedTarget`      | Optional schema and table name.                                                        |
| `ExportedScope`       | Reserved scope field; omitted by the current release.                                  |
| `ExportedResult`      | Identity, verdicts, counts, facts, caps, opted-in diagnostics, and categorized errors. |
| `ExportedCounts`      | Optional total, failed count, and failed percentage.                                   |
| `ExportedFacts`       | Observations and configured thresholds.                                                |
| `ExportedCaps`        | Returned and truncated flags for opted-in samples and keys.                            |
| `ExportedDiagnostics` | Opted-in redacted SQL, optional arguments, and truncation flags.                       |
| `ExportedError`       | Stable error category and export-safe message.                                         |

`PolicyVerdict` is `pass`, `fail`, or `unevaluated`. `unevaluated` is used when
the source `Result` has `Err`. `ExecutionOutcome` distinguishes a successful
execution, policy failure, execution failure, and configuration failure.

## Normalized values

`NormalizedValue` is the JSON-safe representation for returned SQL values. It
has `Kind`, optional `Value`, and optional `Exact`; `Exact` is present only for
lossless encodings. Its kinds are `null`, `bool`, `string`, `json_integer`,
`json_number`, `integer_string`, `decimal_string`, `bytes_base64`,
`time_rfc3339`, `composite`, `non_finite`, and `unsupported`.

Exact integral `float64` values use `json_integer`; non-integral finite values
use `json_number`. Signed zero is `-0.0` with `Exact == false`; non-finite
floats use `non_finite`.

`Redactor` transforms an opted-in value. `ExportOption` configures export.
