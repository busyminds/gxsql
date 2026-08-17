# Stable IDs and Report Export

## Stable IDs and Kinds

`WithID(id string, exp Expectation) Expectation` decorates an expectation with a
caller-supplied stable ID. It preserves the expectation policy while setting
`Result.ID`.

Blank IDs and duplicate IDs are preflight errors. Without `ContinueOnError`,
validation returns `*PreflightErrors` before SQL starts. With it, the affected
result has `Err` and later expectations still run. IDs are never derived from
`Result.Name`.

`ExpectationKind` is the stable category of a built-in expectation. The `Kind*`
constants cover row-count, structural column, catalog nullability and reported
type, per-row predicate, distinct-count, aggregate, broader metric, temporal,
and reconcile builders; `KindReconcileCountsEqual` (`reconcile_counts_equal`)
marks dual `COUNT(*)` equality from `ReconcileCounts(...).Equal()`. Broader
metric kinds are `KindSumBetween` (`sum_between`), `KindPopulationStdDevBetween`
(`population_stddev_between`), `KindCompletenessRate` (`completeness_rate`),
`KindDuplicateRate` (`duplicate_rate`), `KindValueFrequency`
(`value_frequency`), and `KindDominantShare` (`dominant_share`). Those kinds are
distinct from `KindNotNull` and `KindUnique` / `KindCompositeUnique`.
`KindCustom` marks custom counts from `CustomCount`. Other expectations may
still use `KindCustom` when built-in metadata is unavailable. Use `Kind` and
`ID` for machine joins, not display text.

Structural column results use `KindRequiredColumns` (`required_columns`) or
`KindExactColumns` (`exact_columns`). They export ordered `required_columns` /
`missing_columns` / `unexpected_columns` facts under `gxsql.report.v1`, keep
`RowDenominatorUnavailable`, and never export samples or failed keys. `WithKey`,
sample caps, and `SummaryOnly()` do not change that shape.

Catalog nullability results use `KindColumnNullability` (`column_nullability`)
and export `configured_nullability` / `observed_nullability` plus
`missing_columns` when the named column is absent. Exact reported-type results
use `KindColumnType` (`column_type`) and export `configured_reported_type` /
`observed_reported_type`, also with `missing_columns` on absence. Both keep
`RowDenominatorUnavailable`, never export samples or failed keys, and remain
ineligible for count tolerance. Default export still omits discovery SQL and
bound arguments.

Reconcile-count results use `KindReconcileCountsEqual` and export
`facts.reconcile` with left and right targets, observed counts, relationship
`"equal"`, and optional `left_scope_id` / `secondary_filter_id`. They keep
`RowDenominatorUnavailable`, export `counts.failed` as `0` or `1`, omit
`counts.total` and `counts.failed_percent`, and never export samples or failed
keys. Reference results may export optional `parent_filter_id` under
`facts.reference`. Parent-filter, secondary-filter, and scope predicate text and
arguments stay out of default export.

Custom-count results export `counts.failed` when execution succeeds, including
an explicit zero. `counts.total` and `counts.failed_percent` are omitted because
`RowDenominatorUnavailable`. Custom counts never export samples or failed keys.
`WithKey`, sample caps, and `SummaryOnly()` do not change custom-count export.

Spec 07 metric facts are encoded under `facts.sum`, `facts.population_stddev`,
`facts.completeness`, `facts.duplicate_rate`, `facts.frequency`, and
`facts.dominant_share`. They contain structured counts, normalized floating
observations, configured bounds, and exactness labels. Empty or all-NULL
aggregate observations remain absent; they are never emitted as `NaN`. Frequency
facts include the configured category or `configured_null`; dominant-share facts
include `tie_count` and no selected category value. These table-level metrics
omit row samples and failed keys. Quantile and approximate metric facts are not
part of this export contract.

## Policy Fields

`WithPolicy(exp, Policy)` adds `severity`, optional `description`, and sorted
`tags` to each exported result. The severity values are `error`, `warning`, and
`info`; the zero value is `error`. These fields are exported without opt-in.

`MaxFailedPercent` exports its inclusive configured bound as
`facts.configured_max_failed_percent`. `WithMaxFailedCount` continues to export
`facts.configured_max_failed_count`. `Tolerated` is emitted when any allowance
turns a nonzero raw failure count into a policy pass. Raw
`counts.total`/`failed`/`failed_percent` remain complete and preserve the
measured signal.

`policy_verdict` remains `pass` or `fail` for evaluated results, and
`execution_outcome` remains `ok`, `policy_failure`, `execution_failure`, or
`config_failure`. Warning and info policy failures remain in `results` but do
not gate `Report.OK()` or `Report.Err()`; result errors always gate.

Export remains privacy-safe by default. Samples, failed keys, query diagnostics,
and arguments keep their existing opt-in and redaction rules (`IncludeSamples`,
`IncludeFailedKeys`, `IncludeCapturedDiagnostics`, `IncludeCapturedArguments`,
and redactors). Pattern expectation display names redact configured fragments
and patterns (`has prefix (...)`, `like (...)`, `regex (...)`, and related
forms); bound pattern arguments are not exported unless captured diagnostics are
opted in.

## Scoped Reports and Privacy

`TrustedScope(id, predicate, args...)` creates a scope for trusted Go-code
predicate input. The predicate is not a SQL sandbox: never pass user-authored
predicate text to it. Keep values separate from the predicate with `?`
placeholders; tenant, batch, and half-open time-window values are bound as
arguments:

```go
tenantID := "tenant-acme"
batchID := int64(42)
start := time.Date(2025, time.January, 2, 0, 0, 0, 0, time.UTC)
end := time.Date(2025, time.January, 3, 0, 0, 0, 0, time.UTC)
scope := gxsql.TrustedScope(
    "tenant-acme/batch-42",
    "tenant_id = ? AND batch_id = ? AND event_at >= ? AND event_at < ?",
    tenantID, batchID, start, end,
)
report, err := suite.ValidateTable(ctx, db, gxsql.Table("events"),
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithScope(scope),
)
if err != nil {
    return err
}
exported, err := gxsql.ExportReport(report)
```

`Report.ScopeID` and exported `scope.id` carry only the caller-supplied scope
identity. They do not serialize the scope predicate text or bound arguments.
Parent-filter and secondary-filter identities follow the same rule through
`parent_filter_id`, `secondary_filter_id`, and reconcile `left_scope_id`.
Default `Report.Err()`, `Report.String()`, and `Result.String()` output omit
predicate text and bound arguments, as does default `ExportReport` output.
Ordinary samples and failed keys remain subject to the usual report redaction
guidance. `IncludeCapturedDiagnostics()` or `IncludeCapturedArguments()`
deliberately opts into sensitive SQL diagnostics; use those options only with
appropriate redaction.

`RequiredColumns`, `ExactColumns`, `ColumnNullability`, and `ColumnType`
contracts cannot run under `WithScope`. Pairing any of them with `WithScope`
fails `ValidateTable` preflight before discovery SQL. Run a separate unscoped
structural suite when you need shape checks before scoped content validation.

Production callers should validate with a context deadline and a database role
restricted to read-only validation access. Export itself is encoding-only, but
the scoped validation immediately before it still executes database queries.

## ExportReport

`ExportSchemaVersion` is currently `gxsql.report.v1`.

```go
exported, err := gxsql.ExportReport(report,
    gxsql.WithDataTime(partitionStart),
    gxsql.WithEvaluationTime(time.Now().UTC()),
    gxsql.IncludeSamples(),
)
```

`ExportReport(report, opts...) (ExportedReport, error)` converts a `Report` to a
versioned, encoding-only JSON DTO. On error it returns no partial DTO.

Defaults protect diagnostics: samples, failed keys, captured SQL, bound
arguments, scoped predicate details, and custom-count template SQL are omitted.
Configured thresholds are exported in `facts.configured_*`, and default
`display_name` redacts bound literals. Custom-count errors and default export
omit template text and arguments, including driver-error text that might echo
them. `CaptureQueryDiagnostics()` at validate time plus
`IncludeCapturedDiagnostics()` or `IncludeCapturedArguments()` at export time
are the only paths that may retain rendered custom-count SQL or arguments, and
they remain subject to redactors.

Caller-owned `WithDataTime` and `WithEvaluationTime` set optional top-level
`ExportedReport.DataTime` / `EvaluationTime` (`*time.Time`; JSON `data_time` /
`evaluation_time`). Non-zero values are normalized to UTC copies and encode as
RFC3339Nano via `encoding/json`; zero values omit the JSON field.
`ExportReport(report)` with no time options remains byte-compatible with
existing goldens. gxsql does not infer either clock during `ValidateTable`.
Evaluation-time is supplied only by `WithEvaluationTime`; Spec 03 observer
duration is not evaluation-time and is not a history clock.

| `ExportOption`                 | Effect                                                                                                                      |
| ------------------------------ | --------------------------------------------------------------------------------------------------------------------------- |
| `WithDataTime(t)`              | Sets caller-owned business/as-of time (`data_time`). Zero omits the field.                                                  |
| `WithEvaluationTime(t)`        | Sets caller-owned validation/export time (`evaluation_time`). Zero omits the field.                                         |
| `IncludeSamples()`             | Exports normalized `SampleValues` and cap metadata when failures exist.                                                     |
| `IncludeFailedKeys()`          | Exports normalized report-retained `FailedKeys` and cap metadata when failures exist. Not a complete retrieval path.        |
| `IncludeCapturedDiagnostics()` | Exports redacted, length-capped SQL captured with `CaptureQueryDiagnostics()`.                                              |
| `IncludeCapturedArguments()`   | Exports normalized, count-capped arguments with captured SQL; also requires `CaptureQueryDiagnostics()`.                    |
| `WithQueryRedactor(fn)`        | Applies `fn` after identifier redaction and initial SQL truncation; its output is truncated again. It must return a string. |
| `WithArgsRedactor(fn)`         | Redacts each exported bound argument.                                                                                       |
| `WithSampleRedactor(fn)`       | Redacts each exported sample value.                                                                                         |
| `WithKeyRedactor(fn)`          | Redacts each exported failed key.                                                                                           |

A redactor error or panic fails export closed.

## Exported Types

| Type                            | JSON Role                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| ------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ExportedReport`                | Schema version, optional caller-owned `data_time` / `evaluation_time`, optional target/scope, and declaration-ordered results.                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `ExportedTarget`                | Optional schema and table name.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `ExportedScope`                 | Optional stable caller scope identity as `scope.id`; predicate and bound values are not included.                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `ExportedResult`                | Identity, verdicts, optional `tolerated`, counts, facts, caps, opted-in diagnostics, and categorized errors.                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `ExportedCounts`                | Optional total, failed count, and failed percentage.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `ExportedFacts`                 | Observations and configured thresholds, including optional `configured_max_failed_count`, `configured_max_failed_percent`, temporal `configured_time_*` / `observed_time` fields, `key_columns`, `comparison`, `ratio`, `reference`, `reconcile`, broader-metric `sum` / `population_stddev` / `completeness` / `duplicate_rate` / `frequency` / `dominant_share`, structural `required_columns` / `missing_columns` / `unexpected_columns`, and schema-contract `configured_nullability` / `observed_nullability` / `configured_reported_type` / `observed_reported_type`. |
| `ExportedSumFacts`              | Nested SUM observation and bounds (`observed` / `observed_float`, configured bounds, `exactness`).                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `ExportedPopulationStdDevFacts` | Nested population stddev observation (`observed`, configured bounds, `algorithm`, `exactness`).                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `ExportedRateFacts`             | Nested completeness or duplicate-rate counts (`non_null_count` or `duplicate_count`, `total_count`, `rate`, and `configured_bound` or `configured_lower` / `configured_upper`).                                                                                                                                                                                                                                                                                                                                                                                             |
| `ExportedFrequencyFacts`        | Nested category-share facts (`configured_value` / `configured_null`, counts, `share`, and `configured_bound` or `configured_lower` / `configured_upper`).                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `ExportedDominantShareFacts`    | Nested dominant-share facts (`dominant_count`, `total_count`, `share`, `tie_count`, and `configured_bound` or `configured_lower` / `configured_upper`).                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `ExportedComparisonFacts`       | Same-row comparison operands and relationship.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `ExportedRatioFacts`            | Same-row ratio operands and integral bound.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `ExportedReferenceFacts`        | Structured local-to-parent mapping (`local_columns`, parent target, `parent_columns`, optional `parent_filter_id`) for reference results.                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `ExportedReconcileFacts`        | Dual-side reconcile mapping (`left`, `right`, observed counts, `relationship`, optional `left_scope_id` / `secondary_filter_id`).                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `ExportedCaps`                  | Returned and truncated flags for opted-in samples and keys.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `ExportedDiagnostics`           | Opted-in redacted SQL, optional arguments, and truncation flags.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `ExportedError`                 | Stable error category and export-safe message (`gxsql: <category>`; raw error text is not exported).                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |

`PolicyVerdict` is `pass`, `fail`, or `unevaluated`. `unevaluated` is used when
the source `Result` has `Err`. `ExecutionOutcome` distinguishes a successful
execution, policy failure, execution failure, and configuration failure.

## History Helpers

`MeasurementRecordsFromExport(report ExportedReport) ([]MeasurementRecord, error)`
maps an encode-only export into privacy-safe history snapshots for caller-owned
storage. Every result must carry a non-blank unique `id`; blank and duplicate
IDs fail with `CategoryInvalidConfig`.

| Type / API                     | Role                                                                                          |
| ------------------------------ | --------------------------------------------------------------------------------------------- |
| `MeasurementKey`               | Lookup key: `ResultID` plus optional `Kind`, `ScopeID`, `TargetSchema`, `TargetTable`.        |
| `MeasurementRecord`            | Privacy-safe snapshot: identity, times, verdicts, tags, counts, facts, and errors.            |
| `BaselineStore`                | Caller-implemented `Get(ctx, key) ([]MeasurementRecord, error)` lookup shape only.            |
| `MeasurementRecordsFromExport` | Maps report-level target/scope/times onto each record; never copies samples/keys/diagnostics. |

`ResultID` is the primary join field. Kind, scope id, and target schema/table
are included on `MeasurementKey` as optional series/conflict checks when set;
empty optional fields do not constrain a caller-owned store. Do not join renamed
targets silently. Report-level `DataTime` / `EvaluationTime` are copied onto
each `MeasurementRecord` when present; evaluation-time comes only from
caller-owned `WithEvaluationTime` (Spec 03 observer duration is not
evaluation-time). ContinueOnError slots keep `PolicyVerdictUnevaluated` with a
distinct `ExecutionOutcome` rather than a policy failure. History mapping
retains categorized `errors[]` while applying export-safe messages
(`gxsql: <category>`); it does not pass through arbitrary `Message` text from a
hand-built DTO. Existing structured `Facts` are the metric comparison shape;
category and share stay inside nested facts such as `facts.frequency`.

`WithID` remains the existing optional API. The mapper rejects blank and
duplicate export IDs. gxsql does not persist, window, compare, or enforce
baselines, and ships no storage implementation, scheduler, or anomaly helper.
Callers own append and lookup. Encode JSON with `json.Marshal`; there is no
public decoder.

## Broader Metric Facts

Sum, population standard-deviation, completeness, duplicate-rate, frequency, and
dominant-share results export nested facts under `gxsql.report.v1`. They keep
`RowDenominatorUnavailable`, omit metric populations from `counts.total` /
`counts.failed_percent`, and never export samples or failed keys by default.
These facts are not aliases of `NotNull` / `Unique` `failed_percent`.

| Kind / JSON `kind`          | Facts object              | Notable fields                                                                                |
| --------------------------- | ------------------------- | --------------------------------------------------------------------------------------------- |
| `sum_between`               | `facts.sum`               | `observed` or `observed_float`, configured bounds, `exactness` (`exact_integer` or `float64`) |
| `population_stddev_between` | `facts.population_stddev` | `observed`, configured bounds, `algorithm` (`STDDEV_POP`), `exactness` (`exact_population`)   |
| `completeness_rate`         | `facts.completeness`      | `non_null_count`, `total_count`, `rate`, and `configured_bound` or configured lower/upper     |
| `duplicate_rate`            | `facts.duplicate_rate`    | `duplicate_count`, `total_count`, `rate`, and `configured_bound` or configured lower/upper    |
| `value_frequency`           | `facts.frequency`         | `configured_value` / `configured_null`, counts, `share`, and configured bound or lower/upper  |
| `dominant_share`            | `facts.dominant_share`    | `dominant_count`, `total_count`, `share`, `tie_count`, and configured bound or lower/upper    |

Empty or all-`NULL` aggregate observations omit the observed field rather than
encoding zero or `NaN`. Empty rate scopes may publish zero counts while omitting
`rate` / `share`. Dominant ties export `tie_count` without selecting a category
value. Quantile facts and shared multi-aggregate profile payloads are not part
of this export surface.

## Temporal Facts

Window results export `configured_time_start` and `configured_time_end` as
`time_rfc3339` normalized UTC RFC3339Nano values. Freshness results export
`configured_time_cutoff`, and when a maximum exists also `observed_time` with
`observed_time_present: true`. Explicit absence (empty or all-NULL scope) keeps
`configured_time_cutoff` and emits `observed_time_present: false` while omitting
`observed_time`. Non-freshness results omit the presence marker. Default export
still omits samples, failed keys, and query diagnostics.

## Structural Column Facts

`RequiredColumns` and `ExactColumns` export schema-name lists only. They never
export row values. Successful discovery publishes `required_columns` in caller
declaration order. Missing expected names appear as `missing_columns` in that
same declaration order. For `ExactColumns` only, unexpected discovered names
appear as `unexpected_columns` in driver discovery order. Empty difference lists
are omitted. Default export still omits samples, failed keys, and query
diagnostics; column-name facts are not row diagnostics and follow the ordinary
facts path under `gxsql.report.v1`.

## Schema Contract Facts

Catalog nullability and exact reported-type results export configured and
observed schema facts only. They never export row values. Nullability results
emit `configured_nullability` and, when discovery found the column,
`observed_nullability` (`nullable`, `not_nullable`, or `unknown`). Type results
emit `configured_reported_type` and, when found, `observed_reported_type` using
the exact driver-reported `DatabaseTypeName` spelling. Absent columns publish
`missing_columns` with the checked name and omit invented observations.

Default `ExportReport` remains encode-only and privacy-safe: samples, failed
keys, discovery SQL, and bound arguments stay omitted unless callers opt into
captured diagnostics with the existing redaction path. Identifier-bearing query
text, when opted in, still follows current redactors and length caps.
`WithScope` remains incompatible with these contracts, so scoped predicate text
is never part of their evaluation or export surface.

## Reference and Reconcile Facts

Reference results export structured `facts.reference` with `local_columns`, the
parent target, and `parent_columns`. When `WithParentFilter` was attached,
`parent_filter_id` carries only the caller identity from `TrustedParentFilter`.
Parent values never appear in samples or failed keys.

Reconcile results export structured `facts.reconcile` with `left`, `right`,
`observed_left_count`, `observed_right_count`, and `relationship: "equal"`. When
the run used `WithScope`, `left_scope_id` carries the suite scope identity. When
`WithSecondaryFilter` was attached, `secondary_filter_id` carries only the
caller identity from `TrustedSecondaryFilter`. Default export omits predicate
text, bound arguments, samples, failed keys, and query diagnostics for both
shapes.

## Normalized Values

`NormalizedValue` is the JSON-safe representation for returned SQL values. It
has `Kind`, optional `Value`, and optional `Exact`; `Exact` is present only for
lossless encodings. Its kinds are `null`, `bool`, `string`, `json_integer`,
`json_number`, `integer_string`, `decimal_string`, `bytes_base64`,
`time_rfc3339`, `composite`, `non_finite`, and `unsupported`.

Exact integral `float64` values use `json_integer`; non-integral finite values
use `json_number`. Signed zero is `-0.0` with `Exact == false`; non-finite
floats use `non_finite`.

`Redactor` transforms an opted-in value. `ExportOption` configures export.
