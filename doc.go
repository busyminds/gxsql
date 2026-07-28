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
//	)
//
//	report, err := suite.ValidateTable(ctx, db, gxsql.Table("users"),
//		gxsql.WithDialect(gxsql.Postgres()),
//	)
//	if err != nil {
//		// Configuration or execution error; no complete report is available.
//	}
//	if err := report.Err(); err != nil {
//		// Validation completed, but one or more policies failed.
//	}
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
// per-row or uniqueness expectation. max is an inclusive non-negative
// failed-row bound; equality passes. Tolerance changes only the policy
// verdict—raw Total, FailedCount, FailedPercent, samples, and failed keys
// stay under existing caps. Gate with [Report.Err]; tolerated results count
// as successful and are omitted from [Report.Failures], so inspect
// [Report.Results] and [Result.Tolerated] for remediation. Table-level,
// aggregate, distinct-count, row-count, and custom-count wrappers fail
// preflight. Execution and configuration errors are never tolerated:
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
