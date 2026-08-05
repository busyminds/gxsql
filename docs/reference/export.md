[export.md#FA7C] 1:# Stable IDs and Report Export 2: 3:## Stable IDs and kinds
4: 5:`WithID(id string, exp Expectation) Expectation` decorates an expectation
with a 6:caller-supplied stable ID. It preserves the expectation policy while
setting 7:`Result.ID`. 8: 9:Blank IDs and duplicate IDs are preflight errors.
Without `ContinueOnError`, 10:validation returns `*PreflightErrors` before SQL
starts. With it, the affected 11:result has `Err` and later expectations still
run. IDs are never derived from 12:`Result.Name`. 13: 14:`ExpectationKind` is
the stable category of a built-in expectation. The `Kind*` 15:constants cover
row-count, per-row predicate, distinct-count, and aggregate 16:builders;
`KindCustom` marks custom counts from `CustomCount`. Other 17:expectations may
still use `KindCustom` when built-in metadata is unavailable. 18:Use `Kind` and
`ID` for machine joins, not display text. 19: 20:Custom-count results export
`counts.failed` when execution succeeds, including 21:an explicit zero.
`counts.total` and `counts.failed_percent` are omitted because
22:`RowDenominatorUnavailable`. Custom counts never export samples or failed
keys. 23:`WithKey`, sample caps, and `SummaryOnly()` do not change custom-count
export. 24: 25:## Bounded failure tolerance 26: 27:`WithMaxFailedCount` results
remain discoverable in JSON without changing 28:`ExportSchemaVersion`. When
`Result.Tolerated` is true, `ExportedResult` emits 29:`tolerated: true`; the
field is omitted otherwise. `ExportedFacts` includes
30:`configured_max_failed_count` whenever that decorator was applied. Existing
31:`counts.total`, `counts.failed`, and `counts.failed_percent` carry the raw
32:observations. Exported `policy_verdict` stays `pass` and `execution_outcome`
33:stays `ok` for a tolerated policy pass; the tolerance flag, configured bound,
34:and raw counts distinguish it from a clean pass. 35: 36:Export remains
privacy-safe by default. The tolerance flag, bound, and raw 37:counts are
exported; samples, failed keys, query diagnostics, and arguments keep 38:their
existing opt-in and redaction rules (`IncludeSamples`, 39:`IncludeFailedKeys`,
`IncludeCapturedDiagnostics`, `IncludeCapturedArguments`, 40:and redactors). 41:
42:## Scoped reports and privacy 43: 44:`TrustedScope(id, predicate, args...)`
creates a scope for trusted Go-code 45:predicate input. The predicate is not a
SQL sandbox: never pass user-authored 46:predicate text to it. Keep values
separate from the predicate with `?` 47:placeholders; tenant, batch, and
half-open time-window values are bound as 48:arguments: 49:
50:`go 51:tenantID := "tenant-acme" 52:batchID := int64(42) 53:start := time.Date(2025, time.January, 2, 0, 0, 0, 0, time.UTC) 54:end := time.Date(2025, time.January, 3, 0, 0, 0, 0, time.UTC) 55:scope := gxsql.TrustedScope( 56:    "tenant-acme/batch-42", 57:    "tenant_id = ? AND batch_id = ? AND event_at >= ? AND event_at < ?", 58:    tenantID, batchID, start, end, 59:) 60:report, err := suite.ValidateTable(ctx, db, gxsql.Table("events"), 61:    gxsql.WithDialect(gxsql.Postgres()), 62:    gxsql.WithScope(scope), 63:) 64:if err != nil { 65:    return err 66:} 67:exported, err := gxsql.ExportReport(report) 68:`
69: 70:`Report.ScopeID` and exported `scope.id` carry only the caller-supplied
scope 71:identity. They do not serialize the scope predicate text or bound
arguments. 72:Default `Report.Err()`, `Report.String()`, and `Result.String()`
output omit 73:those scope fields, as does default `ExportReport` output.
Ordinary samples and 74:failed keys remain subject to the usual report redaction
guidance. 75:`IncludeCapturedDiagnostics()` or `IncludeCapturedArguments()`
deliberately opts 76:into sensitive SQL diagnostics; use those options only with
appropriate 77:redaction. 78: 79:Production callers should validate with a
context deadline and a database role 80:restricted to read-only validation
access. Export itself is encoding-only, but 81:the scoped validation immediately
before it still executes database queries. 82: 83:## ExportReport 84:
85:`ExportSchemaVersion` is currently `gxsql.report.v1`. 86:
87:`go 88:exported, err := gxsql.ExportReport(report, 89:    gxsql.IncludeSamples(), 90:) 91:`
92: 93:`ExportReport(report, opts...) (ExportedReport, error)` converts a
`Report` to a 94:versioned, encoding-only JSON DTO. On error it returns no
partial DTO. 95: 96:Defaults protect diagnostics: samples, failed keys, captured
SQL, bound 97:arguments, scoped predicate details, and custom-count template SQL
are omitted. 98:Configured thresholds are exported in `facts.configured_*`, and
default 99:`display_name` redacts bound literals. Custom-count errors and
default export 100:omit template text and arguments, including driver-error text
that might echo 101:them. `CaptureQueryDiagnostics()` at validate time plus
102:`IncludeCapturedDiagnostics()` or `IncludeCapturedArguments()` at export
time 103:are the only paths that may retain rendered custom-count SQL or
arguments, and 104:they remain subject to redactors. 105: 106:| `ExportOption` |
Effect | 107:| ------------------------------ |
---------------------------------------------------------------------------------------------------------------------------
| 108:| `IncludeSamples()` | Exports normalized `SampleValues` and cap metadata
when failures exist. | 109:| `IncludeFailedKeys()` | Exports normalized
`FailedKeys` and cap metadata when failures exist. | 110:|
`IncludeCapturedDiagnostics()` | Exports redacted, length-capped SQL captured
with `CaptureQueryDiagnostics()`. | 111:| `IncludeCapturedArguments()` | Exports
normalized, count-capped arguments with captured SQL; also requires
`CaptureQueryDiagnostics()`. | 112:| `WithQueryRedactor(fn)` | Applies `fn`
after identifier redaction and initial SQL truncation; its output is truncated
again. It must return a string. | 113:| `WithArgsRedactor(fn)` | Redacts each
exported bound argument. | 114:| `WithSampleRedactor(fn)` | Redacts each
exported sample value. | 115:| `WithKeyRedactor(fn)` | Redacts each exported
failed key. | 116: 117:A redactor error or panic fails export closed. 118:
119:## Exported types 120: 121:| Type | JSON role | 122:| ---------------------
|
------------------------------------------------------------------------------------------------------------
| 123:| `ExportedReport` | Schema version, optional target/scope, and
declaration-ordered results. | 124:| `ExportedTarget` | Optional schema and
table name. | 125:| `ExportedScope` | Optional stable caller scope identity as
`scope.id`; predicate and bound values are not included. | 126:|
`ExportedResult` | Identity, verdicts, optional `tolerated`, counts, facts,
caps, opted-in diagnostics, and categorized errors. | 127:| `ExportedCounts` |
Optional total, failed count, and failed percentage. | 128:| `ExportedFacts` |
Observations and configured thresholds, including optional
`configured_max_failed_count`, `key_columns`, and `reference`. | |
`ExportedReferenceFacts` | Structured local-to-parent mapping (`local_columns`,
parent target, `parent_columns`) for reference results. | 129:| `ExportedCaps` |
Returned and truncated flags for opted-in samples and keys. | 130:|
`ExportedDiagnostics` | Opted-in redacted SQL, optional arguments, and
truncation flags. | 131:| `ExportedError` | Stable error category and
export-safe message. | 132: 133:`PolicyVerdict` is `pass`, `fail`, or
`unevaluated`. `unevaluated` is used when 134:the source `Result` has `Err`.
`ExecutionOutcome` distinguishes a successful 135:execution, policy failure,
execution failure, and configuration failure. 136: 137:## Normalized values 138:
139:`NormalizedValue` is the JSON-safe representation for returned SQL values.
It 140:has `Kind`, optional `Value`, and optional `Exact`; `Exact` is present
only for 141:lossless encodings. Its kinds are `null`, `bool`, `string`,
`json_integer`, 142:`json_number`, `integer_string`, `decimal_string`,
`bytes_base64`, 143:`time_rfc3339`, `composite`, `non_finite`, and
`unsupported`. 144: 145:Exact integral `float64` values use `json_integer`;
non-integral finite values 146:use `json_number`. Signed zero is `-0.0` with
`Exact == false`; non-finite 147:floats use `non_finite`. 148: 149:`Redactor`
transforms an opted-in value. `ExportOption` configures export. 150:
