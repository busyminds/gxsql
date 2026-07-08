# Stable Identity and Export

Use stable IDs and the versioned export DTO when validation results must be
joined, stored, or consumed outside the Go process.

## Give expectations stable IDs

Wrap an expectation with `WithID`:

```go
suite := gxsql.NewSuite(
    gxsql.WithID("users.email.not-empty", gxsql.String("email").NotEmpty()),
)
```

IDs are optional for ad-hoc validation. When present, `Result.ID` supplies a
machine-joinable identity; IDs are never derived from `Result.Name`. Built-in
`Result.Kind` values provide a stable category for each expectation.

Blank and duplicate IDs are preflight errors. By default they stop validation
before SQL and are collected in `*PreflightErrors`. With `ContinueOnError()`,
the affected result contains `Err` and later expectations still run.

## Export a report

`ExportReport` converts a `Report` to a versioned JSON DTO:

```go
exported, err := gxsql.ExportReport(report)
if err != nil {
    return err
}
data, err := json.Marshal(exported)
```

The schema version is `gxsql.report.v1`. Version 1 guarantees declaration-order
results; stable `id`, `kind`, `display_name`, verdicts, counts, facts, and
categorized errors. It does not promise a public decoder.

## Understand verdicts

Each exported result separates policy and execution status:

- `policy_verdict` is `pass` or `fail` only when `Result.Err` is nil. A result
  with `Err` is `unevaluated`.
- `execution_outcome` distinguishes an evaluated success, policy failure,
  execution failure, and configuration failure.

Configured thresholds appear in `facts.configured_*`. Default `display_name`
redacts bound literals, so consumers should use structured facts rather than
parse display text.

## Opt in to diagnostics deliberately

By default, export excludes sample values, failed keys, SQL text, and bound
arguments. Opt in to each class separately:

| Option                         | Requires                                      | Effect                                           |
| ------------------------------ | --------------------------------------------- | ------------------------------------------------ |
| `IncludeSamples()`             | Nothing else                                  | Exports normalized samples and cap metadata.     |
| `IncludeFailedKeys()`          | Nothing else                                  | Exports normalized row keys and cap metadata.    |
| `IncludeCapturedDiagnostics()` | `CaptureQueryDiagnostics()` during validation | Exports redacted, length-capped SQL.             |
| `IncludeCapturedArguments()`   | `CaptureQueryDiagnostics()` during validation | Also exports normalized, count-capped arguments. |

Use `WithQueryRedactor`, `WithArgsRedactor`, `WithSampleRedactor`, and
`WithKeyRedactor` for custom redaction. Any redactor error or panic fails export
without returning a partial DTO.

## Next

- [Plan privacy and operational limits](operations.md)
- [Read the export API reference](../reference/export.md)
