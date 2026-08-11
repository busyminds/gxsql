# Operational Limits and Privacy

`gxsql` executes SQL against the selected database. Plan validation work as you
would plan any production query workload.

## Set Deadlines and Choose Checks Deliberately

Pass a deadline-bearing `context.Context` to every `ValidateTable` call. Query
cost depends on the engine, table size, indexes, and statistics.

Per-row checks share one population `COUNT(*)` during a validation run. Without
`WithSharedScalarEvaluation()`, each check then runs one failure-count query.
Failures can add sample and failed-key queries.

Table-level expectations—row count, distinct count, and aggregates—typically run
one query each. Structural column contracts run one read-only zero-row discovery
query (`SELECT * FROM <quoted target> WHERE 1 = 0`) and read `Rows.Columns()`;
they do not scan row values or write schema.

## Use Shared Scalar Evaluation

`WithSharedScalarEvaluation()` is an opt-in run option. It is off by default.
Use it when a suite has two or more adjacent built-in per-row checks and you
need lower query cost for failure counts.

It combines contiguous compatible per-row failure counts into
conditional-aggregate SQL statement(s). Compatible slots are built-in per-row
checks that admit a failure predicate, including nullability, membership, string
and numeric predicates, cross-column checks, and `Timestamp(...).InWindow(...)`.
`WithID` and `WithMaxFailedCount` wrappers do not block combination.

Exact compatibility boundary:

- Only contiguous declaration-order runs of length two or more combine.
- A compatible expectation between incompatible neighbors stays sequential.
- Uniqueness, table-level, aggregate, distinct-count, custom-count, structural,
  and relation checks never combine.
- Large contiguous runs split across multiple statements when needed to stay
  within engine SELECT target limits.

Preserved behavior: counts, verdicts, tolerance, samples, failed keys,
declaration order, and scope stay aligned with sequential evaluation. Published
semantic report fields do not change for compatible checks.

Diagnostics: when `CaptureQueryDiagnostics()` is enabled, captured SQL records
the actual combined statement. The library does not invent per-check SQL for
those slots.

See the [suite reference](../reference/suite.md) for the option catalog and SQL
integration details.

## Limit Retained Data

| Control                | Default                    | Effect                                                     |
| ---------------------- | -------------------------- | ---------------------------------------------------------- |
| `WithSampleCap(n)`     | 20                         | Caps `SampleValues`.                                       |
| `WithFailedKeysCap(n)` | 100                        | Caps `FailedKeys` when `WithKey` is set; `0` is unlimited. |
| `WithKey(...)`         | Off                        | Loads caller-selected failing row identities.              |
| `SummaryOnly()`        | Implicit without `WithKey` | Does not load failed-row keys.                             |

Use `WithKey` when failure rates are low or an operator needs specific rows for
remediation. Prefer summary-only results for widespread failures on large
tables. Unbounded failed-key retention can consume unbounded process memory.

## Avoid Oversized Membership Lists

Each `In` or `NotIn` value becomes a bound placeholder. Lists in the low
thousands are generally practical. For larger domains, validate a lookup-table
join outside `gxsql`.

Do not divide a `NotIn` domain across multiple expectations. Each expectation
would independently exclude only its own values and would change the policy.

## Protect Data and Database Access

`ValidateTable` uses the permissions of its database connection. In production,
use a read-only role that is restricted to the validation tables or views.

`Report.String()` and the `gxsqltest` helpers can include sampled values in
output. Redact or avoid sending such output to observability systems when a
column may hold PII or secrets.

`ExportReport` is deliberately conservative. Samples, failed keys, captured SQL,
and bound arguments are omitted unless you enable them explicitly. When you
export those fields, use the redactor options as needed. See
[Stable Identity and Export](export.md).

## Next

- [Inspect result retention controls](results.md)
- [Export a machine-readable report](export.md)
- [Limits and rendering reference](../reference/results.md)
