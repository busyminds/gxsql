// Package gxsql validates database table contents through database/sql.
//
// Build a suite from expectations and validate a table with the dialect that
// matches the database connection:
//
//	suite := gxsql.NewSuite(
//		gxsql.RowCount().GreaterOrEqual(1),
//		gxsql.Int("age").Between(0, 120),
//		gxsql.String("email").NotEmpty(),
//		gxsql.Column("id").Unique(),
//		gxsql.Timestamp("created_at").FreshSince(cutoff),
//	)
//
//	report, err := suite.ValidateTable(ctx, db, gxsql.Table("users"),
//		gxsql.WithDialect(gxsql.Postgres()),
//		gxsql.WithKey("id"),
//	)
//	if err != nil {
//		// Configuration or execution failed. No complete report is available.
//	}
//	if err := report.Err(); err != nil {
//		// Validation completed, but one or more policies failed.
//	}
//
// ValidateTable evaluates expectations in declaration order when unsegmented.
// WithSegments evaluates scope-compatible expectations in segment-major order.
// A policy failure is part of the returned Report, not the returned error.
// Configuration and execution failures return an error unless
// [ContinueOnError] is selected.
// The default dialect is [Postgres]; pass [WithDialect] explicitly when the
// database uses another engine.
//
// The package provides expectations for:
//
//   - row counts, column values, nullability, membership, uniqueness, and
//     referential integrity;
//   - numeric comparisons, aggregates, rates, frequencies, and timestamps;
//   - structural column sets and driver-reported column metadata; and
//   - trusted custom counts and count reconciliation.
//
// Per-row expectations report a row denominator and failed-row metrics.
// Table-level expectations report their own structured facts instead. Empty
// populations pass when the expectation is per-row or when its documented
// metric is vacuous. NULL handling and empty-population behavior vary by
// expectation; see the expectation reference for details.
//
// [WithScope] limits the shared population with a trusted, dialect-neutral
// predicate. [When] applies an additional trusted predicate to one eligible
// expectation. Do not pass user-authored SQL as scope or eligibility input.
// [WithPolicy] adds severity and metadata. [WithMaxFailedCount] and
// [MaxFailedPercent] add failure tolerance only to eligible row-based
// expectations.
//
// Reports preserve their evaluation order: expectation declaration order when
// unsegmented, and segment-major order with [WithSegments]. Use [Report.OK],
// [Report.Err], and the report filter methods to inspect outcomes. Use [WithID]
// for stable machine identity; [Result.Name] is display text and is not an
// identifier.
//
// [ExportReport] creates a versioned, privacy-safe data transfer object.
// Samples, failed keys, SQL text, and bound arguments are omitted by default.
// [MeasurementRecordsFromExport] maps an export to caller-owned history
// storage. gxsql does not persist history or enforce drift policies.
//
// [WithObserver] receives synchronous, privacy-safe events for attempted SQL
// statements. Events include query category, expectation kind, duration, and
// status, but never include SQL text, arguments, predicates, samples, or keys.
//
// Custom SQL input from [TrustedCountQuery], [TrustedParentFilter], and
// [TrustedSecondaryFilter] must come from trusted application code. These
// helpers do not make untrusted SQL safe.
//
// See the
// [tutorial](https://github.com/busyminds/gxsql/tree/main/docs/tutorial),
// [concepts](https://github.com/busyminds/gxsql/tree/main/docs/concepts), and
// [reference](https://github.com/busyminds/gxsql/tree/main/docs/reference)
// documentation for installation, expectation semantics, reports, export,
// compatibility, and SQL integration details.
package gxsql
