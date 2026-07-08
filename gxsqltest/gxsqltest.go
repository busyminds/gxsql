// Package gxsqltest adapts a gxsql.Suite to Go tests. Check and Require call
// ValidateTable with the given options. Check reports failures via t.Errorf and
// continues; Require stops the test via t.Fatalf. Both call t.Helper().
package gxsqltest

import (
	"context"

	"github.com/busyminds/gxsql"
)

// TestingT is the subset of *testing.T / *testing.B used by this package; both
// satisfy it implicitly, so callers pass their *testing.T unchanged.
type TestingT interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// Check validates a table against the suite. On execution or validation failure
// it calls t.Errorf and the test continues. It returns true when ValidateTable
// succeeds and every expectation passes.
func Check(
	t TestingT,
	ctx context.Context,
	s *gxsql.Suite,
	db gxsql.DB,
	table gxsql.TableRef,
	opts ...gxsql.Option,
) bool {
	t.Helper()
	rep, err := s.ValidateTable(ctx, db, table, opts...)
	if err != nil {
		t.Errorf("gxsql: ValidateTable failed: %v", err)
		return false
	}
	if rep.OK() {
		return true
	}
	t.Errorf("gxsql: data quality failed\n%s", rep)
	return false
}

// Require validates a table against the suite. On execution or validation
// failure it calls t.Fatalf, stopping the test.
func Require(
	t TestingT,
	ctx context.Context,
	s *gxsql.Suite,
	db gxsql.DB,
	table gxsql.TableRef,
	opts ...gxsql.Option,
) {
	t.Helper()
	rep, err := s.ValidateTable(ctx, db, table, opts...)
	if err != nil {
		t.Fatalf("gxsql: ValidateTable failed: %v", err)
	}
	if !rep.OK() {
		t.Fatalf("gxsql: data quality failed\n%s", rep)
	}
}
