# Results and remediation

A completed `ValidateTable` call returns a `Report` with one `Result` per
expectation. Results preserve suite declaration order.

## Read a report

| Member              | Use                                                                            |
| ------------------- | ------------------------------------------------------------------------------ |
| `report.OK()`       | Test whether every result passed.                                              |
| `report.Failures()` | Return only results whose `Success` is false.                                  |
| `report.Err()`      | Return `nil` for a passing report, or `*ValidationError` with the full report. |
| `report.String()`   | Produce a human-readable summary and per-result lines.                         |

A failed policy does not make `ValidateTable` return a non-nil error. Use
`report.Err()` when validation should gate an application action:

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

## Raw observations versus policy verdict

`Success` is the policy verdict. Raw observations stay intact under their normal
cap settings even when a policy pass is tolerated. Those observations include
`Total`, `FailedCount`, `FailedPercent`, samples, and failed keys.

`WithMaxFailedCount(max, exp)` sets an inclusive non-negative maximum failed-row
count. Equality passes. There is no percentage, pass-rate, `Mostly`, rounding,
or compound policy. Tolerance changes only the verdict. A nonzero raw failure
count at or below `max` yields `Success: true` and `Tolerated: true`. Above the
bound, `Success` is false and `Tolerated` is false. A raw-zero or empty
evaluated population passes and is not tolerated. Empty populations do not
divide by zero or produce `NaN`. Scope remains the evaluated population for all
raw counts.

Tolerated results count as successful for `report.OK()` and `report.Err()`.
`report.Failures()` omits them. They remain visible in `Result.Tolerated`,
`Report.String()`, and exported JSON. For remediation, walk `Report.Results` and
inspect `Result.Tolerated`. Do not rely on `Failures()` alone. Read the
configured bound from `Result.Facts.ConfiguredMaxFailedCount`.

Only per-row and uniqueness expectations qualify, including composite
`Columns(...).Unique()` and `References()`. Wrapping a table-level, aggregate,
distinct-count, row-count, or custom-count declaration fails preflight.
Execution and configuration errors are never tolerated.

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

## Read a result

Per-row checks set `RowDenominator` to `RowDenominatorAvailable` and populate:

- `Total`: table row count evaluated by the check.
- `FailedCount` and `FailedPercent`: the complete number and proportion of
  failures.
- `SampleValues`: capped examples of offending values.
- `FailedKeys`: optional caller-selected row identities.
- `Tolerated`: true when a nonzero raw failure count passed within
  `WithMaxFailedCount`; false for clean passes, above-bound failures, empty
  populations, and errors.

Table-level checks—row count, distinct count, numeric aggregates, and custom
counts—use `RowDenominatorUnavailable`. Their `Total` remains zero because no
per-row population is reported. Custom counts instead expose their complete
`FailedCount`, including zero, and do not retain `FailedPercent`, samples, or
failed keys. Read the observed value and configured threshold from `Facts` for
built-in table-level checks. `Name` is only human-facing display text.

`ID` and `Kind` are stable machine-facing identity fields. Use `WithID` to
supply an ID and `Result.Kind` to classify a built-in expectation. See
[stable identity and export](export.md).

## Control retained failure data

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
| `WithFailedKeysCap(n)` | Caps identities; `0` retains all keys.                       |
| `WithSampleCap(n)`     | Caps sample values; `0` disables sample collection.          |
| `SummaryOnly()`        | Does not load failed-row keys.                               |

Use `WithFailedKeysCap(0)` only when every failed identity is required and
unbounded retention is acceptable.

## Vacuous passes

Some expectations pass because no applicable values exist:

| Situation                                             | Behavior                                                         |
| ----------------------------------------------------- | ---------------------------------------------------------------- |
| Numeric aggregate on an all-`NULL` column             | Passes; no observed-value suffix is appended to `Result.Name`.   |
| Distinct count on an empty table or all-`NULL` column | Evaluates to `0`; the expectation runs normally.                 |
| Per-row check on an empty table                       | Passes when its failure predicate matches no rows; `Total == 0`. |
| Empty `In` or `NotIn` list                            | Configuration error before SQL.                                  |

If an empty table or all-null column must fail, add an explicit row-count or
non-null expectation.

## Next

- [Understand validation and error behavior](validation.md)
- [Control cost and sensitive diagnostic data](operations.md)
- [Reports and errors reference](../reference/results.md)
