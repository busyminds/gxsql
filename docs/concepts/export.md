# Stable Identity and Export

Use stable IDs and the versioned export data transfer object when you need to
join, store, or consume validation results outside the Go process.

## Give Expectations Stable IDs

Wrap an expectation with `WithID`:

```go
suite := gxsql.NewSuite(
    gxsql.WithID("users.email.not-empty", gxsql.String("email").NotEmpty()),
)
```

IDs are optional for ad-hoc validation. When present, `Result.ID` supplies a
machine-joinable identity. IDs are never derived from `Result.Name`. Built-in
`Result.Kind` values provide a stable category for each expectation.

Blank and duplicate IDs are preflight errors. By default they stop validation
before SQL and are collected in `*PreflightErrors`. With `ContinueOnError()`,
the affected result contains `Err` and later expectations still run.

## Export a Report

`ExportReport` converts a `Report` to a versioned JSON data transfer object:

```go
exported, err := gxsql.ExportReport(report)
if err != nil {
    return err
}
data, err := json.Marshal(exported)
```

The schema version is `gxsql.report.v1`. Version 1 preserves result declaration
order and exports IDs, kinds, display names, verdicts, counts, facts, and
categorized errors. Per-result machine identity starts with `id`, optional
`segment_id`, and `kind`. `segment_id` is present only for segmented runs and is
omitted when empty. Run-level joins also use `scope.id`, optional `target`, and
caller-owned `data_time` / `evaluation_time` when present. Display names are not
join keys. The format does not promise a public decoder.

Custom-count results export only `counts.failed` when evaluation succeeds.
`counts.total` and `counts.failed_percent` are omitted because no row
denominator exists. Custom-count results never export samples or failed keys.
Their template SQL and bound arguments remain omitted unless query diagnostics
are captured and explicitly requested at export time.

Structural column results likewise omit counts total/percent, samples, and
failed keys. They export schema-name facts as `required_columns`,
`missing_columns`, and `unexpected_columns` under `gxsql.report.v1`. Those names
are metadata, not row values.

## Attach Caller-Owned Run Times

Pass explicit times at export when you need historical joins. gxsql does not
infer watermarks or schedule runs:

```go
exported, err := gxsql.ExportReport(report,
    gxsql.WithDataTime(partitionStart),
    gxsql.WithEvaluationTime(time.Now().UTC()),
)
```

- `data_time` is the business/as-of time of the validated population.
- `evaluation_time` is when validation or export ran.
- Non-zero values encode as UTC RFC3339Nano. Zero values omit the JSON field.
- Observer timing is not evaluation-time and is not a history clock.

## Join History Outside gxsql

Use stable identity plus the privacy-safe mapper, then store and look up records
in caller-owned code. Core ships no baseline store, scheduler, or drift
enforcer.

```go
exported, err := gxsql.ExportReport(report,
    gxsql.WithDataTime(partitionStart),
    gxsql.WithEvaluationTime(time.Now().UTC()),
)
if err != nil {
    return err
}
records, err := gxsql.MeasurementRecordsFromExport(exported)
if err != nil {
    return err
}
if err := history.Append(ctx, records); err != nil { // caller-owned
    return err
}

key := gxsql.MeasurementKey{
    ResultID:  "users.email.not-empty",
    SegmentID: "eu", // empty for unsegmented reports
}
if exported.Scope != nil {
    key.ScopeID = exported.Scope.ID
}
if exported.Target != nil {
    key.TargetSchema = exported.Target.Schema
    key.TargetTable = exported.Target.Table
}
prior, err := history.Get(ctx, key)
```

Join vocabulary:

| Field                            | Role                                            |
| -------------------------------- | ----------------------------------------------- |
| result `id` / `ResultID`         | Expectation identity from `WithID`              |
| `segment_id` / `SegmentID`       | Segment identity; empty for unsegmented reports |
| `kind`                           | Optional series/conflict check                  |
| `scope.id`                       | Optional partition/scope check                  |
| `target.schema` / `target.table` | Contextual series/conflict identity             |
| `data_time` / `evaluation_time`  | Caller-owned timeline fields                    |

`WithID` stays optional for ordinary validation. A measurement series is
identified by `(ResultID, SegmentID)`. History mapping requires non-blank result
IDs and unique `(ResultID, SegmentID)` pairs; the same result ID may appear once
per segment. Kind, scope, and target checks on `MeasurementKey` are optional
when left empty, but do not join renamed targets silently—a changed target is a
different series unless the caller remaps identity explicitly outside gxsql.

`MeasurementRecordsFromExport` copies structured counts, facts, verdicts, tags,
and categorized errors after re-sanitizing messages to `gxsql: <category>`. It
always omits samples, failed keys, caps, and diagnostics—even when those were
opted into the export. ContinueOnError slots keep `policy_verdict=unevaluated`
with a distinct `execution_outcome`; do not treat them as policy failures.
Broader metric comparisons use existing structured facts such as
`facts.frequency`; no extra series labels are required.

`BaselineStore` is only the lookup shape (`Get`); callers implement append,
windowing, comparison, and any enforcement. Encode JSON with
`json.Marshal(exported)` when you need an artifact; there is no public decoder.

## Understand Verdicts

Each exported result separates policy and execution status:

- `policy_verdict` is `pass` or `fail` only when `Result.Err` is nil. A result
  with `Err` is `unevaluated`.
- `execution_outcome` distinguishes an evaluated success, a policy failure, an
  execution failure, and a configuration failure.

Configured thresholds appear in `facts.configured_*`. Default `display_name`
redacts bound literals. Consumers must use structured facts rather than parse
display text.

`WithPolicy` exports `severity` as `error`, `warning`, or `info`, plus
normalized `description` and sorted `tags` when configured.
`facts.configured_max_failed_percent` carries the inclusive `MaxFailedPercent`
threshold. These fields are present without diagnostic opt-in; raw counts remain
complete and `tolerated` marks nonzero raw failures that passed an allowance.

## Opt In to Diagnostics Deliberately

By default, export excludes sample values, failed keys, SQL text, and bound
arguments. Opt in to each class separately:

| Option                         | Requires                                      | Effect                                           |
| ------------------------------ | --------------------------------------------- | ------------------------------------------------ |
| `IncludeSamples()`             | Nothing else                                  | Exports normalized samples and cap metadata.     |
| `IncludeFailedKeys()`          | Nothing else                                  | Exports normalized row keys and cap metadata.    |
| `IncludeCapturedDiagnostics()` | `CaptureQueryDiagnostics()` during validation | Exports redacted, length-capped SQL.             |
| `IncludeCapturedArguments()`   | `CaptureQueryDiagnostics()` during validation | Also exports normalized, count-capped arguments. |

`IncludeFailedKeys()` exports only keys retained on the report (still subject to
`WithFailedKeysCap`). It is not complete failure retrieval. For the full failing
identity set, call `FailingKeys` at runtime; see
[Results and Remediation](results.md).

Use `WithQueryRedactor`, `WithArgsRedactor`, `WithSampleRedactor`, and
`WithKeyRedactor` for custom redaction. Any redactor error or panic fails export
without returning a partial data transfer object.

## Next

- [Plan privacy and operational limits](operations.md)
- [Read the export API reference](../reference/export.md)
