// Package gxsql validates database table contents through database/sql without
// loading whole tables into application memory.
//
// Build a suite from expectations, then validate a table with the dialect that
// matches the database connection:
//
//	suite := gxsql.NewSuite(
//		gxsql.RowCount().GreaterOrEqual(1),
//		gxsql.Int("age").Between(0, 120),
//		gxsql.String("email").NotEmpty(),
//		gxsql.Columns("tenant_id", "order_id").Unique(),
//		gxsql.Columns("tenant_id", "customer_id").References(
//			gxsql.SchemaTable("public", "customers"), "tenant_id", "id",
//		),
//		gxsql.Column("end_date").GreaterOrEqualColumn("start_date"),
//		gxsql.Column("paid_cents").LessOrEqualColumn("invoice_cents"),
//		gxsql.Int("actual_units").RatioEqual("planned_units", 2),
//		gxsql.Timestamp("event_time").InWindow(windowStart, windowEnd),
//		gxsql.Timestamp("ingested_at").FreshSince(cutoff),
//	)
//
//	report, err := suite.ValidateTable(ctx, db, gxsql.Table("orders"),
//		gxsql.WithDialect(gxsql.Postgres()),
//		gxsql.WithScope(gxsql.TrustedScope("tenant-acme", "tenant_id = ?", tenantID)),
//		gxsql.WithKey("id"),
//	)
//	if err != nil {
//		// Configuration or execution error; no complete report is available.
//	}
//	if err := report.Err(); err != nil {
//		// Validation completed, but one or more policies failed.
//	}
//
// Composite uniqueness ([Columns].Unique) ignores tuples with any NULL
// component and counts every duplicate participating row. Referential checks
// ([Columns].References / [Column].References) pass any-NULL local tuples,
// count orphaned complete local rows, apply [WithScope] only to the local
// table, and leave parent lookup unscoped. Results leave [Result.Column]
// blank and publish [ResultFacts.KeyColumns] or [ResultFacts.Reference].
// Same-row relationships use fixed [ColumnBuilder] methods
// ([ColumnBuilder.EqualColumn], [ColumnBuilder.NotEqualColumn],
// [ColumnBuilder.LessThanColumn], [ColumnBuilder.LessOrEqualColumn],
// [ColumnBuilder.GreaterThanColumn], [ColumnBuilder.GreaterOrEqualColumn]) and
// integer [NumberColumn.RatioEqual]. Direct comparisons require non-NULL
// operands from the same validated target, fail either-NULL rows, pass empty
// scoped populations vacuously, and are proven for like-for-like
// integer/numeric and temporal fixture families without coercion or text casts.
// Results publish [ResultFacts.Comparison] or [ResultFacts.Ratio] under distinct
// kinds such as [KindEqualColumn] and [KindRatioEqual]. Ratio equality uses the
// algebraic form left == right * bound (not SQL division), fails a zero
// denominator, and is available only from [Int]; [Float] RatioEqual, decimal
// ratios, floating ratios, raw operators, and general expressions are not
// supported. Database-reported arithmetic overflow or unsupported numeric
// storage is a [CategoryDatabase] execution error. These shapes reuse existing
// caps, [WithScope], [WithMaxFailedCount], declaration order, and privacy-safe
// [ExportReport] defaults.
//
// Temporal checks use caller-supplied [time.Time] values only. Construct them
// with [Timestamp]. [TimestampColumn.InWindow] is a half-open per-row window
// (start <= value < end): SQL NULL fails, an empty scoped population passes
// vacuously, and zero or inverted bounds fail preflight. [TimestampColumn.FreshSince]
// is a table-level freshness check on MAX(column): empty and all-NULL scopes
// fail with explicit observation absence, a maximum at or after the cutoff
// passes (including future-valued maxima), and the library never embeds
// NOW()/CURRENT_TIMESTAMP. Window results publish [ResultFacts.ConfiguredTimeStart]
// and [ResultFacts.ConfiguredTimeEnd] under [KindTimestampInWindow]. Freshness
// results publish [ResultFacts.ConfiguredTimeCutoff], [ResultFacts.ObservedTime],
// and [ResultFacts.ObservedTimePresent] under [KindTimestampFreshSince]. Exported JSON
// normalizes those times as time_rfc3339 UTC RFC3339Nano values
// (configured_time_start/end/cutoff, observed_time, observed_time_present).
//
//	windowStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
//	windowEnd := windowStart.Add(24 * time.Hour)
//	cutoff := windowEnd.Add(-30 * time.Minute)
//	suite := gxsql.NewSuite(
//		gxsql.Timestamp("event_time").InWindow(windowStart, windowEnd),
//		gxsql.Timestamp("ingested_at").FreshSince(cutoff),
//	)
//
// Structural column contracts use [RequiredColumns] and [ExactColumns]. They
// compare unordered column-name sets against dialect/driver-reported
// [database/sql.Rows.Columns] spellings byte-for-byte (no case folding).
// [RequiredColumns] passes when every expected name is present and allows
// additional discovered names. [ExactColumns] passes only when the discovered
// set matches exactly. Discovery is a read-only zero-row probe
// (`SELECT * FROM <quoted target> WHERE 1 = 0`) that never scans row values or
// writes schema. Missing and unexpected names are ordinary table-level results
// under [KindRequiredColumns] / [KindExactColumns] with
// [RowDenominatorUnavailable]: no samples, failed keys, or row denominator.
// Facts publish [ResultFacts.RequiredColumns] plus ordered
// [ResultFacts.MissingColumns] (declaration order) and, for exact checks,
// [ResultFacts.UnexpectedColumns] (discovery order). A missing target,
// permission denial, render failure, or metadata capability failure is a typed
// execution or preflight error (for example [CategoryDatabase]), not a failed
// structural result. Empty expected lists and duplicate or invalid identifiers
// fail preflight. [WithScope] is incompatible and is rejected at ValidateTable
// preflight rather than ignored. These checks do not validate column types,
// nullability, or ordinal position. Prefer a separate structural suite before
// content validation when shape fail-fast matters:
//
//	structure := gxsql.NewSuite(
//		gxsql.RequiredColumns("id", "event_time", "payload"),
//		gxsql.ExactColumns("id", "event_time", "payload"),
//	)
//	report, err := structure.ValidateTable(ctx, db, gxsql.Table("ingest_events"),
//		gxsql.WithDialect(gxsql.Postgres()),
//	)
//
// For scoped validation, use [TrustedScope] with [WithScope]. The scope
// predicate limits every expectation to matching rows and uses dialect-neutral
// ? placeholders; each value is bound separately through the arguments:
//
//	tenantID := "tenant-acme"
//	scope := gxsql.TrustedScope("tenant-acme", "tenant_id = ?", tenantID)
//	report, err := suite.ValidateTable(ctx, db, gxsql.Table("users"),
//		gxsql.WithDialect(gxsql.Postgres()),
//		gxsql.WithScope(scope),
//	)
//
// Scope predicates are trusted Go-code input, not a sandbox for untrusted SQL.
// Callers must not pass user-authored predicate text. [Report.ScopeID] and the
// exported scope.id carry caller identity only; default errors and display
// output, and [ExportReport], do not serialize the scope predicate text or
// bound arguments. Other report samples remain subject to their usual
// redaction requirements. In production, use a read-only database role and a
// context deadline for each validation.
//
// Custom count checks use [TrustedCountQuery] and [CustomCount]. The SQL
// template is trusted Go-code input reviewed by the application, not a
// sandbox for untrusted text; callers must never insert user-authored SQL into
// templates. A template contains exactly one {{target}} and one {{scope}},
// both outside SQL strings and comments. The library renders {{target}} from
// the validated [TableRef] and {{scope}} from [WithScope] (or TRUE when
// unscoped). Custom ? placeholders must follow {{scope}}; bound arguments are
// scope values first, then custom values. The query returns one row and one
// column. Signed integer driver values (int through int64) are accepted;
// textual numerics are not coerced. The count must be non-negative and uses
// [RowDenominatorUnavailable]: no total, percentage, samples,
// or failed keys. Default reports, errors, and [ExportReport] omit template SQL
// and arguments, including driver-error text. [CaptureQueryDiagnostics] is an
// opt-in export-only path subject to existing redactors.
//
// Bounded failure tolerance uses [WithMaxFailedCount] around an eligible
// per-row or uniqueness expectation, including composite uniqueness and
// referential integrity. max is
// an inclusive non-negative failed-row bound; equality passes. Tolerance
// changes only the policy verdict—raw Total, FailedCount, FailedPercent,
// samples, and failed keys stay under existing caps. Gate with [Report.Err];
// tolerated results count as successful and are omitted from
// [Report.Failures], so inspect [Report.Results] and [Result.Tolerated] for
// remediation. Table-level, aggregate, distinct-count, row-count,
// custom-count, and structural column wrappers fail preflight. Execution and configuration errors
// are never tolerated:
//
//	suite := gxsql.NewSuite(
//		gxsql.WithMaxFailedCount(2, gxsql.String("email").NotEmpty()),
//	)
//	report, err := suite.ValidateTable(ctx, db, gxsql.Table("users"),
//		gxsql.WithDialect(gxsql.Postgres()),
//	)
//	if err != nil {
//		// Configuration or execution error; no complete report is available.
//	}
//	if err := report.Err(); err != nil {
//		// Above-bound or other policy failures.
//	}
//
// Failed policies are collected in declaration order. Use [WithKey] to retain
// failed-row identities, [WithID] to give expectations stable machine identity,
// and [ExportReport] for the versioned JSON DTO. For Go tests, use the
// gxsqltest subpackage's Check or Require helper.
package gxsql
