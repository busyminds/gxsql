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

## Observe Query Cost Safely

Attach `WithObserver(gxsql.ObserverFunc(...))` when a caller needs statement
counts and timings:

```go
observer := gxsql.ObserverFunc(func(event gxsql.QueryEvent) {
	log.Printf("id=%s kind=%s category=%s duration=%s status=%s",
		event.ID, event.Kind, event.Category, event.Duration, event.Status)
})

report, err := suite.ValidateTable(ctx, db, table,
	gxsql.WithDialect(gxsql.Postgres()),
	gxsql.WithObserver(observer),
)
```

The callback runs synchronously once for each attempted statement. Events
contain check identity when supplied with `WithID`, expectation kind, typed
query category, monotonic elapsed duration, and typed status. They do not
contain SQL text, bound arguments, scope predicates, samples, or failed keys.
The observer does not run extra queries to populate event fields.

If the observer panics, `ValidateTable` returns a typed observer error and no
partial report. Keep the callback short and do not make validation decisions
from duration values without recording engine and fixture metadata.

## Use Shared Scalar Evaluation

`WithSharedScalarEvaluation()` is an opt-in run option. It is off by default.
Use it when a suite has two or more adjacent built-in per-row checks and you
need lower query cost for failure counts.

It combines contiguous compatible per-row failure counts into
conditional-aggregate SQL statement(s). Compatible slots are built-in per-row
checks that admit a failure predicate, including nullability, membership, string
and numeric predicates, cross-column checks, and `Timestamp(...).InWindow(...)`.
`WithID`, `WithPolicy`, and `WithMaxFailedCount` wrappers do not block
combination.

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

## Transaction and Snapshot Ownership

`gxsql` accepts `*sql.Tx` because it satisfies the narrow `DB` interface. The
caller begins, configures, commits, rolls back, and closes the transaction:

```go
tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
if err != nil {
	return err
}
defer tx.Rollback()

report, err := suite.ValidateTable(ctx, tx, table,
	gxsql.WithDialect(gxsql.Postgres()),
)
```

Use an isolation level supported by the target engine when a consistent
multi-statement view is required. A pooled `*sql.DB` without a caller-owned
transaction may use different connections between statements. Do not describe
that mode as snapshot-consistent.

## Limit Retained Data

| Control                | Default                    | Effect                                                     |
| ---------------------- | -------------------------- | ---------------------------------------------------------- |
| `WithSampleCap(n)`     | 20                         | Caps `SampleValues`.                                       |
| `WithFailedKeysCap(n)` | 100                        | Caps `FailedKeys` when `WithKey` is set; `0` is unlimited. |
| `WithKey(...)`         | Off                        | Loads caller-selected failing row identities.              |
| `SummaryOnly()`        | Implicit without `WithKey` | Does not load failed-row keys.                             |

Use `WithKey` when failure rates are low or an operator needs a bounded sample
of identities on the report. Prefer `SummaryOnly()` for widespread failures on
large tables, then call `FailingKeys` for complete streaming remediation.
`WithFailedKeysCap(0)` retains every key in report memory; it is not a
streaming contract.

For large failure sets, keep the report bounded with `SummaryOnly()` and
`WithKey(...)`, then call `FailingKeys` with one result selector
(`ForResultID`, `ForResultIndex`, or an unambiguous `ForKind`) plus
`WithDialect`. Pass the same validated `TableRef`; retrieval binds to that
plan-stored target, so mutating `Report.Target` cannot redirect it. Scope is
already bound from `ValidateTable`—omit `WithScope` on retrieval, or pass a
matching one only as a compatibility check; mismatched or extra `WithScope` is
rejected. Always `Close` the iterator and inspect `Err` after `Next` returns
false; terminal paths close rows and can surface observer errors. The iterator
streams complete identities in key-column order (`NULL` → `nil`) and does not
add them to the report. Core issues only read SQL; caller-owned adapters own
any sink writes. See [Results and Remediation](results.md).

## Record a Baseline

The deterministic fixture definitions in `testdata/bench/` describe the
PostgreSQL and SQLite schema, eight-row distribution, indexes, seed procedure,
and required runtime metadata. Do not commit generated database files. Record
engine version, Go version, operating system, architecture, and CPU with each
comparison.

Run the standard benchmarks from the module root:

```bash
go test -run '^$' -bench . -benchmem -count 5 ./...
```

Benchmark output is comparison evidence, not a performance guarantee. Query
category counts and observer timings are the baseline; engine cost estimates are
outside this contract.

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
