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
// Suite scope and rule eligibility are distinct. [TrustedScope] / [WithScope]
// select the shared population for a validation call. [TrustedEligibility] with
// [When] narrows which rows inside that population are subject to one
// expectation. Eligibility does not rewrite [Report.ScopeID]. When both are
// present, SQL applies suite scope and eligibility as independent conjuncts with
// bindings in suite-scope, eligibility, then expectation order. Ineligible rows
// neither pass nor fail the wrapped rule and do not appear in its samples or
// keys. Eligible-row count is the denominator for percentages and policy
// tolerance. Zero eligible rows pass vacuously: [Result.Total] and
// [Result.FailedCount] are zero, no percentage is fabricated, and
// [Result.Tolerated] stays false.
//
//	shipped := gxsql.When(
//		gxsql.TrustedEligibility("status-shipped", "status = ?", "shipped"),
//		gxsql.Column("shipped_at").NotNull(),
//	)
//	report, err := gxsql.NewSuite(shipped).ValidateTable(ctx, db, gxsql.Table("orders"),
//		gxsql.WithDialect(gxsql.Postgres()),
//		gxsql.WithScope(gxsql.TrustedScope("tenant-acme", "tenant_id = ?", tenantID)),
//	)
//
// [When] wraps exactly one expectation. Nested eligibility fails preflight.
// Supported shapes are ordinary per-row, uniqueness, composite uniqueness, and
// referential-integrity expectations. Table-level, aggregate, distinct-count,
// custom-count, and structural expectations reject eligibility at preflight.
// Eligibility predicates are trusted Go-code input like scope predicates.
// Default errors, display output, and [ExportReport] omit eligibility predicate
// text and bound arguments.
//
// A policy pack is an ordinary Go function that returns a fresh []Expectation.
// Callers concatenate packs and local rules in declaration order and pass the
// flattened list to [NewSuite]. Each pack call must return independent values;
// mutating a returned slice must not affect a later call. Flattened order is
// pack order, then declaration order within each pack, then any caller-appended
// expectations. A composed suite must match the identical flat list written by
// hand, including policy fields and eligibility wrappers. Use [WithID] with
// caller-owned conventions such as reverse-domain or pack-prefix paths
// (for example "acme.orders.id.present"). Blank and duplicate IDs fail
// preflight before SQL; with [ContinueOnError] they occupy declaration-order
// slots. Library [Result.Kind] values are not caller IDs. Completed packs and
// suites may be reused concurrently when configuration is finished and nothing
// mutates during [Suite.ValidateTable].
//
//	func OrderIntegrityPack(prefix string) []gxsql.Expectation {
//		return []gxsql.Expectation{
//			gxsql.WithID(prefix+".id.present", gxsql.String("id").NotEmpty()),
//			gxsql.WithID(prefix+".id.unique", gxsql.Column("id").Unique()),
//			gxsql.WithID(prefix+".shipped_at.present", gxsql.When(
//				gxsql.TrustedEligibility("status-shipped", "status = ?", "shipped"),
//				gxsql.Column("shipped_at").NotNull(),
//			)),
//		}
//	}
//	suite := gxsql.NewSuite(append(
//		OrderIntegrityPack("acme.orders"),
//		gxsql.RowCount().GreaterOrEqual(1),
//	)...)
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
// Policy decoration uses [WithPolicy] around a sealed expectation:
//
//	suite := gxsql.NewSuite(
//		gxsql.WithPolicy(
//			gxsql.String("email").NotEmpty(),
//			gxsql.Policy{
//				Severity:    gxsql.SeverityWarning,
//				Description: "Customer email must be present",
//				Tags:        []string{"customer", "pii"},
//				Tolerance:   gxsql.MaxFailedPercent(0.5),
//			},
//		),
//	)
//
// [MaxFailedPercent] is an inclusive unrounded failed-row percentage in the
// range [0, 100]. It applies to per-row, uniqueness, and referential-integrity
// expectations with [RowDenominatorAvailable]. Empty populations pass without a
// fabricated percentage and are never tolerated. [WithMaxFailedCount] remains
// the inclusive count form and keeps its existing eligible shapes and behavior.
// Tolerance changes only the policy verdict; raw totals, failed counts,
// percentages, samples, keys, and structured facts remain complete under their
// existing caps. A nonzero raw failure within an allowance sets [Result.Tolerated].
//
// [SeverityError] is the zero severity. Warning and info policy failures remain
// in [Report.Results] but do not gate [Report.OK] or [Report.Err]. Other
// severity values are treated as gating failures. Configuration and execution
// errors always gate and are never tolerated. Descriptions are trimmed; blank
// descriptions are omitted. Tags are trimmed, sorted, copied, and rejected when
// blank or duplicated. Metadata never changes [Result.ID], [Result.Kind], or
// gating.
//
// Use [Report.GatingFailures], [Report.PolicyFailures], [Report.Warnings],
// [Report.Infos], [Report.Unexpected], [Report.ToleratedResults], and
// [Report.ExecutionFailures] to query the distinct outcome classes. Default
// [ExportReport] includes policy fields and configured thresholds while still
// omitting samples, keys, SQL, and arguments.
package gxsql
