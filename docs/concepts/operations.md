# Operational Limits and Privacy

`gxsql` executes SQL against the selected database. Plan validation work as you
would any production query workload.

## Set deadlines and choose checks deliberately

Pass a deadline-bearing `context.Context` to every `ValidateTable` call. Query
cost depends on the engine, table size, indexes, and statistics.

Per-row expectations run at least two `COUNT(*)` queries: one for the evaluated
population and one for rows matching the failure predicate. Failures can add
queries for samples and, with `WithKey`, failed-row identities.

Table-level expectations—row count, distinct count, and aggregates—typically run
one query each.

## Limit retained data

| Control                | Default                    | Effect                                                     |
| ---------------------- | -------------------------- | ---------------------------------------------------------- |
| `WithSampleCap(n)`     | 20                         | Caps `SampleValues`.                                       |
| `WithFailedKeysCap(n)` | 100                        | Caps `FailedKeys` when `WithKey` is set; `0` is unlimited. |
| `WithKey(...)`         | Off                        | Loads caller-selected failing row identities.              |
| `SummaryOnly()`        | Implicit without `WithKey` | Does not load failed-row keys.                             |

Use `WithKey` when failure rates are low or an operator needs specific rows for
remediation. Prefer summary-only results for widespread failures on large
tables. Unbounded failed-key retention can consume unbounded process memory.

## Avoid oversized membership lists

Each `In` or `NotIn` value becomes a bound placeholder. Lists in the low
thousands are generally practical; for larger domains, validate a lookup-table
join outside `gxsql`.

Do not divide a `NotIn` domain across multiple expectations: each expectation
would independently exclude only its own values, changing the policy.

## Protect data and database access

`ValidateTable` uses the permissions of its database connection. In production,
use a read-only role restricted to the validation tables or views.

`Report.String()` and the `gxsqltest` helpers can include sampled values in
output. Redact or avoid sending such output to observability systems when a
column may hold PII or secrets.

`ExportReport` is deliberately conservative: samples, failed keys, captured SQL,
and bound arguments are omitted unless explicitly enabled. When exporting those
fields, use the redactor options as needed. See
[stable identity and export](export.md).

## Next

- [Inspect result retention controls](results.md)
- [Export a machine-readable report](export.md)
- [Limits and rendering reference](../reference/results.md)
