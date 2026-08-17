# Results and Remediation

A completed `ValidateTable` call returns a `Report` with one `Result` per
expectation. Results preserve suite declaration order.

## Read a Report

| Member                          | Use                                                                                  |
| ------------------------------- | ------------------------------------------------------------------------------------ |
| `report.OK()`                   | True when no non-advisory policy failure or result error exists.                     |
| `report.Failures()`             | Return all results whose `Success` is false, including advisory failures and errors. |
| `report.GatingFailures()`       | Return non-advisory policy failures and all result errors.                           |
| `report.PolicyFailures()`       | Return evaluated data-quality failures, excluding result errors.                     |
| `report.Warnings()` / `Infos()` | Return all results with the matching severity.                                       |
| `report.Unexpected()`           | Return evaluated results with raw failures, including tolerated outcomes.            |
| `report.ToleratedResults()`     | Return results whose nonzero raw failures passed an allowance.                       |
| `report.ExecutionFailures()`    | Return result slots with configuration or execution errors.                          |
| `report.Err()`                  | Return `nil` when no hard-gating result exists, or `*ValidationError` otherwise.     |
| `report.String()`               | Produce a human-readable summary and per-result lines.                               |

A failed policy does not make `ValidateTable` return a non-nil error. Use
`report.Err()` when validation must gate an application action:

```go
if err := report.Err(); err != nil {
    var validationErr *gxsql.ValidationError
    if errors.As(err, &validationErr) {
        for _, result := range validationErr.Report.Failures() {
            fmt.Println(result.String())
        }
    }
}
```

## Raw Observations Versus Policy Verdict

`Success` is the policy verdict. Raw observations stay intact under their normal
cap settings even when an allowance produces a policy pass. Those observations
include `Total`, `FailedCount`, `FailedPercent`, samples, and failed keys.

`WithPolicy(exp, Policy)` adds `SeverityError`, `SeverityWarning`, or
`SeverityInfo`, optional description/tags, and at most one tolerance. The zero
severity is error. Warning and info policy failures remain in `Report.Results`
but do not gate `Report.OK()` or `Report.Err()`. Other severity values are
treated as gating failures. Configuration and execution errors always gate.

`MaxFailedPercent(p)` uses the inclusive unrounded comparison
`FailedCount / Total * 100 <= p` for `p` in `[0, 100]`. It applies to
denominator-available per-row, uniqueness, and referential-integrity
expectations. Empty evaluated populations pass and are not tolerated.
`WithMaxFailedCount(max, exp)` remains the inclusive count form with the same
raw-observation rules.

Descriptions are trimmed and blank values are omitted. Tags are trimmed, sorted,
copied, and rejected when blank or duplicated. Metadata never changes `ID`,
`Kind`, or gating. Read configured allowances from
`Result.Facts.ConfiguredMaxFailedCount` and
`Result.Facts.ConfiguredMaxFailedPercent`.

Use focused report filters to inspect advisory outcomes, raw unexpected rows,
tolerated results, policy failures, and execution failures. Do not rely on
`Failures()` to find tolerated results because tolerated results have
`Success: true`.

For composite uniqueness and referential integrity, remediate from local
`FailedCount`, capped `SampleValues`, and opted-in `FailedKeys`. Read component
names from `Facts.KeyColumns` or the parent mapping from `Facts.Reference`. Do
not parse `Name`. Do not expect parent values in diagnostics.

```go
if err := report.Err(); err != nil {
    return err // above-bound or other policy failures
}
for _, result := range report.Results {
    if result.Tolerated {
        // raw FailedCount/samples/keys remain for remediation
    }
}
```

Per-row checks set `RowDenominator` to `RowDenominatorAvailable` and populate:

- `Total`: table row count evaluated by the check.
- `FailedCount` and `FailedPercent`: the complete number and proportion of
  failures.
- `SampleValues`: capped examples of offending values.
- `FailedKeys`: optional caller-selected row identities.
- `Tolerated`: true when a nonzero raw failure count passed within any
  configured allowance; false for clean passes, above-bound failures, empty
  populations, and errors.
- `Severity`, `Description`, and `Tags`: policy classification and metadata.

Table-level checks—row count, distinct count, numeric aggregates (including
`SumBetween` / `StdDevBetween`), freshness, structural column contracts,
catalog schema contracts, reconcile counts, broader rate/frequency metrics, and
custom counts—use `RowDenominatorUnavailable`. Their `Total` remains zero
because no per-row population is reported. Custom counts instead expose their
complete `FailedCount`, including zero, and do not retain `FailedPercent`,
samples, or failed keys. Reconcile counts expose `FailedCount` as `0` or `1`.
Structural `RequiredColumns` / `ExactColumns` and catalog
`ColumnNullability` / `ColumnType` results also omit samples and failed keys;
remediate from `Facts.RequiredColumns`, `Facts.MissingColumns`,
`Facts.UnexpectedColumns`, and the nullability/type fact fields. Broader
metrics publish nested facts such as `Facts.Completeness`, `Facts.DuplicateRate`,
`Facts.Frequency`, `Facts.DominantShare`, `Facts.Sum`, and
`Facts.PopulationStdDev` instead of overloading `FailedPercent`. Read the
observed value and configured threshold from `Facts` for built-in table-level
checks. `Name` is only human-facing display text.

`ID` and `Kind` are stable machine-facing identity fields. Use `WithID` to
supply an ID and `Result.Kind` to classify a built-in expectation. See
[Stable Identity and Export](export.md).

## Control Retained Failure Data

The defaults are `DefaultSampleCap` (20) sample values and
`DefaultFailedKeysCap` (100) row keys per result. Counts and percentages remain
complete when samples or keys are capped.

```go
suite.WithSampleCap(5).WithFailedKeysCap(50)

report, err := suite.ValidateTable(ctx, db, table,
    gxsql.WithKey("id"),
    gxsql.WithSampleCap(5),
    gxsql.WithFailedKeysCap(50),
)
```

Suite methods set defaults for future runs. Options override them for one run.

| Option                 | Effect                                                       |
| ---------------------- | ------------------------------------------------------------ |
| `WithKey(columns...)`  | Requests failed-row identities for the supplied key columns. |
| `WithFailedKeysCap(n)` | Caps identities on `Result`; `0` retains all keys in memory. |
| `WithSampleCap(n)`     | Caps sample values; `0` disables sample collection.          |
| `SummaryOnly()`        | Does not load failed-row keys onto `Result`.                 |

Use `WithFailedKeysCap(0)` only when every failed identity must sit on the
report and unbounded retention is acceptable. Prefer `FailingKeys` when the
complete set must leave the process without growing `Report` memory.

## Stream Complete Failure Identity

`FailedKeys` is a bounded diagnostic slice. Use `FailingKeys` when an adapter
must process every failing identity without retaining the complete set in the
report:

```go
report, err := suite.ValidateTable(ctx, db, table,
    gxsql.WithDialect(gxsql.Postgres()),
    gxsql.WithKey("id"),
    gxsql.SummaryOnly(),
)
if err != nil {
    return err
}
iter, err := gxsql.FailingKeys(ctx, db, table, report,
    gxsql.ForResultID("orders.email.not-null"),
    gxsql.WithDialect(gxsql.Postgres()),
)
if err != nil {
    return err
}
defer iter.Close()
for iter.Next() {
    key := iter.Key()
    // Write or join key in caller-owned code.
    _ = key
}
if err := iter.Err(); err != nil {
    return err
}
```

`FailingKeys` re-runs the selected read-only failure predicate from the plan
attached during `ValidateTable` and orders rows by the selected key columns.
SQL `NULL` key components appear as `nil` in `RowKey`. It supports ordinary
per-row, unique, composite-unique, and local referential-orphan checks;
table-level shapes return `CategoryUnsupported`. Select exactly one result with
`ForResultID`, `ForResultIndex`, or an unambiguous `ForKind`.

Table and scope are bound at validation. The call-site table must match the
original validated `TableRef` stored on that plan; mutating `Report.Target`
cannot redirect retrieval. Suite scope and eligibility stay in the plan.
Optional `WithScope` on `FailingKeys` is compatibility-only: omit it to reuse
the bound plan; mismatched identity or extra `WithScope` on an unscoped report
is rejected and never retargets the population.

`Key` returns a copy. Callers must `Close` the iterator (idempotent). When
`Next` returns false, inspect `Err`. Exhaustion, cancellation, scan, and close
paths release underlying rows; context, scan, database, and observer failures
surface through `Err` / `Close`. An attempted retrieval emits exactly one
privacy-safe `QueryCategoryFailingKeys` observer event (no SQL/args). Use the
same caller-owned transaction for validate and retrieve when snapshot
consistency matters.

`SummaryOnly` with `WithKey` suppresses report retention but preserves the
key-column selection for retrieval; you may also pass `WithKey` on
`FailingKeys`. Complete identity stays outside durable `Report` / `Result`.
Default export and `String()` paths never dump the stream; `IncludeFailedKeys`
exports only report-retained keys. Persistence and repair stay in application
code. `WithFailedKeysCap(0)` remains unlimited in-memory retention, not
streaming.

## Vacuous Passes

Some expectations pass because no applicable values exist:

| Situation                                             | Behavior                                                         |
| ----------------------------------------------------- | ---------------------------------------------------------------- |
| Numeric aggregate on an all-`NULL` column             | Passes; no observed-value suffix is appended to `Result.Name`.   |
| Distinct count on an empty table or all-`NULL` column | Evaluates to `0`; the expectation runs normally.                 |
| Per-row check on an empty table                       | Passes when its failure predicate matches no rows; `Total == 0`. |
| Empty `In` or `NotIn` list                            | Configuration error before SQL.                                  |

`Timestamp(...).FreshSince(cutoff)` is not vacuous. It requires an observed
maximum non-`NULL` value in the scoped population and `observed >= cutoff`. An
empty scope fails. A non-empty all-`NULL` scope also fails because no accepted
watermark exists. Use `NotNull` when completeness is required.

If an empty table or all-null column must fail for other checks, add an explicit
row-count or non-null expectation.

## Next

- [Understand validation and error behavior](validation.md)
- [Control cost and sensitive diagnostic data](operations.md)
- [Reports and errors reference](../reference/results.md)
