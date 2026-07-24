package gxsql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sync"
	"testing"
)

const fakeDriverName = "gxsqltest"

func init() {
	sql.Register(fakeDriverName, &fakeDriver{})
}

// fakeDriver is a stdlib-only in-memory SQL driver for gxsql tests.
type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) {
	harnessMu.Lock()
	tables := harnessTables
	harnessMu.Unlock()
	if tables == nil {
		return nil, fmt.Errorf("gxsqltest: no harness data configured")
	}
	cp := make(map[string][]map[string]any, len(tables))
	for k, v := range tables {
		cp[k] = append([]map[string]any(nil), v...)
	}
	return &fakeConn{tables: cp}, nil
}

type fakeConn struct {
	tables map[string][]map[string]any
}

func (c *fakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, driver.ErrSkip
}

func (c *fakeConn) Close() error { return nil }

func (c *fakeConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("gxsqltest: transactions not supported")
}

func (c *fakeConn) QueryContext(_ context.Context, query string, nargs []driver.NamedValue) (driver.Rows, error) {
	args := make([]any, len(nargs))
	for i, nv := range nargs {
		args[i] = nv.Value
	}
	cols, rows, err := executeHarnessQuery(query, args, c.tables)
	if err != nil {
		return nil, err
	}
	return &fakeRows{columns: cols, rows: rows}, nil
}

type fakeRows struct {
	columns []string
	rows    [][]driver.Value
	idx     int
}

func (r *fakeRows) Columns() []string { return r.columns }

func (r *fakeRows) Close() error { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.idx])
	r.idx++
	return nil
}

var (
	harnessMu     sync.Mutex
	harnessTables map[string][]map[string]any
)

func setHarnessData(t *testing.T, tables map[string][]map[string]any) {
	t.Helper()
	harnessMu.Lock()
	harnessTables = tables
	harnessMu.Unlock()
	t.Cleanup(func() {
		harnessMu.Lock()
		harnessTables = nil
		harnessMu.Unlock()
	})
}

func openHarnessDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open(fakeDriverName, "test")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func harnessUsers(rows ...map[string]any) map[string][]map[string]any {
	return map[string][]map[string]any{"users": rows}
}

const errorDriverName = "gxsqlerr"

func init() {
	sql.Register(errorDriverName, &errorDriver{})
}

type errorDriver struct{}

func (errorDriver) Open(string) (driver.Conn, error) { return errorConn{}, nil }

type errorConn struct{}

func (errorConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }

func (errorConn) Close() error { return nil }

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

const scanErrorDriverName = "gxsqlscanerr"

func init() {
	sql.Register(scanErrorDriverName, &scanErrorDriver{})
}

type scanErrorDriver struct{}

func (scanErrorDriver) Open(string) (driver.Conn, error) { return scanErrorConn{}, nil }

type scanErrorConn struct{}

func (scanErrorConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }

func (scanErrorConn) Close() error { return nil }

func (scanErrorConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("gxsqltest: transactions not supported")
}

type scanErrorRows struct{}

func (scanErrorRows) Columns() []string { return []string{"count"} }

func (scanErrorRows) Close() error { return nil }

func (scanErrorRows) Next([]driver.Value) error {
	return fmt.Errorf("sql: Scan error on column index 0")
}

func (scanErrorConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return scanErrorRows{}, nil
}

func openScanErrorDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open(scanErrorDriverName, "test")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

const closeErrorDriverName = "gxsqlcloseerr"

func init() {
	sql.Register(closeErrorDriverName, &closeErrorDriver{})
}

type closeErrorDriver struct{}

func (closeErrorDriver) Open(string) (driver.Conn, error) { return closeErrorConn{}, nil }

type closeErrorConn struct{}

func (closeErrorConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }

func (closeErrorConn) Close() error { return nil }

func (closeErrorConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("gxsqltest: transactions not supported")
}

type closeErrorRows struct{}

func (closeErrorRows) Columns() []string { return []string{"count"} }

func (closeErrorRows) Next(dest []driver.Value) error {
	dest[0] = int64(1)
	return nil
}

func (closeErrorRows) Close() error {
	return fmt.Errorf("gxsqltest: injected close error")
}

func (closeErrorConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return closeErrorRows{}, nil
}

func openCloseErrorDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open(closeErrorDriverName, "test")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestHarnessWhereTopLevelAND(t *testing.T) {
	where := `(tenant_id = $1) AND ("age" IS NULL OR "age" > $2)`
	args := []any{"t1", int64(120)}

	pass := map[string]any{"tenant_id": "t1", "age": int64(200)}
	if !rowMatchesWhere(where, args, pass, "users", nil) {
		t.Fatal("expected in-scope failing row to match scoped failure predicate")
	}

	inScopePass := map[string]any{"tenant_id": "t1", "age": int64(25)}
	if rowMatchesWhere(where, args, inScopePass, "users", nil) {
		t.Fatal("expected in-scope passing row to miss scoped failure predicate")
	}

	outOfScope := map[string]any{"tenant_id": "t2", "age": int64(200)}
	if rowMatchesWhere(where, args, outOfScope, "users", nil) {
		t.Fatal("expected out-of-scope row to miss scoped failure predicate")
	}
}

func TestHarnessWhereEqualityBinding(t *testing.T) {
	row := map[string]any{"tenant_id": "t1", "status": "active"}
	if !rowMatchesWhere(`tenant_id = $1`, []any{"t1"}, row, "users", nil) {
		t.Fatal("expected $n equality match")
	}
	if rowMatchesWhere(`tenant_id = $1`, []any{"t2"}, row, "users", nil) {
		t.Fatal("expected $n equality mismatch")
	}
	if !rowMatchesWhere(`status = ?`, []any{"active"}, row, "users", nil) {
		t.Fatal("expected ? equality match after bindQuestionMarks")
	}
}
