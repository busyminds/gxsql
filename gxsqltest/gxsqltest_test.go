package gxsqltest_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"testing"

	"github.com/busyminds/gxsql"
	"github.com/busyminds/gxsql/gxsqltest"
)

const errorDriverName = "gxsqltesterr"

func init() {
	sql.Register(errorDriverName, &errorDriver{})
}

type errorDriver struct{}

func (errorDriver) Open(string) (driver.Conn, error) { return errorConn{}, nil }

type errorConn struct{}

func (errorConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (errorConn) Close() error                        { return nil }
func (errorConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("gxsqltest: transactions not supported")
}
func (errorConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, fmt.Errorf("gxsqltest: injected database error")
}

func openErrorDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open(errorDriverName, "test")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type fakeT struct {
	helpers int
	errorfs int
	fatalfs int
	last    string
}

func (f *fakeT) Helper() { f.helpers++ }
func (f *fakeT) Errorf(format string, args ...any) {
	f.errorfs++
	f.last = fmt.Sprintf(format, args...)
}
func (f *fakeT) Fatalf(format string, args ...any) {
	f.fatalfs++
	f.last = fmt.Sprintf(format, args...)
}

func TestCheckReturnsFalseOnValidateTableError(t *testing.T) {
	f := &fakeT{}
	suite := gxsql.NewSuite(gxsql.Column("status").In())
	if gxsqltest.Check(f, context.Background(), suite, nil, gxsql.Table("users"), gxsql.WithDialect(gxsql.Postgres())) {
		t.Fatal("Check should return false on configuration error")
	}
	if f.errorfs != 1 {
		t.Fatalf("Errorf called %d times, want 1", f.errorfs)
	}
	if f.helpers == 0 {
		t.Fatal("Check should call t.Helper()")
	}
}

func TestRequireFatalsOnValidateTableError(t *testing.T) {
	f := &fakeT{}
	suite := gxsql.NewSuite(gxsql.Column("status").In())
	gxsqltest.Require(f, context.Background(), suite, nil, gxsql.Table("users"), gxsql.WithDialect(gxsql.Postgres()))
	if f.fatalfs != 1 {
		t.Fatalf("Fatalf called %d times, want 1", f.fatalfs)
	}
	if f.helpers == 0 {
		t.Fatal("Require should call t.Helper()")
	}
}

func TestCheckReturnsFalseOnDatabaseError(t *testing.T) {
	f := &fakeT{}
	db := openErrorDB(t)
	suite := gxsql.NewSuite(gxsql.Int("age").Between(0, 120))
	if gxsqltest.Check(f, context.Background(), suite, db, gxsql.Table("users"), gxsql.WithDialect(gxsql.Postgres())) {
		t.Fatal("Check should return false on database error")
	}
	if f.errorfs != 1 {
		t.Fatalf("Errorf called %d times, want 1", f.errorfs)
	}
}
