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
// For scoped validation, use [TrustedScope] with [WithScope]. A scoped report
// exposes only the caller-supplied stable identity through [Report.ScopeID].
//
// Failed policies are collected in declaration order. Use [WithKey] to retain
// failed-row identities, [WithID] to give expectations stable machine identity,
// and [ExportReport] for the versioned JSON DTO. For Go tests, use the
// gxsqltest subpackage's Check or Require helper.
package gxsql
