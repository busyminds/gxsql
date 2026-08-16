package gxsql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
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
	schemas := harnessSchemas
	columnTypes := harnessColumnTypes
	harnessMu.Unlock()
	if tables == nil && schemas == nil {
		return nil, fmt.Errorf("gxsqltest: no harness data configured")
	}
	cp := make(map[string][]map[string]any, len(tables))
	for k, v := range tables {
		cp[k] = append([]map[string]any(nil), v...)
	}
	sc := make(map[string][]string, len(schemas))
	for k, v := range schemas {
		sc[k] = append([]string(nil), v...)
	}
	ct := make(map[string][]harnessColumnMeta, len(columnTypes))
	for k, v := range columnTypes {
		ct[k] = append([]harnessColumnMeta(nil), v...)
	}
	return &fakeConn{tables: cp, schemas: sc, columnTypes: ct}, nil
}

type fakeConn struct {
	tables      map[string][]map[string]any
	schemas     map[string][]string
	columnTypes map[string][]harnessColumnMeta
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
	cols, rows, table, err := executeHarnessQueryWithTable(query, args, c.tables, c.schemas)
	if err != nil {
		return nil, err
	}
	return &fakeRows{
		columns:     cols,
		rows:        rows,
		columnTypes: resolveHarnessColumnTypes(table, cols, c.columnTypes),
	}, nil
}

// executeHarnessQueryWithTable is a test-only wrapper that preserves
// executeHarnessQuery behavior and also returns the queried table identity
// derived from the same query parsing, not from column names.
func executeHarnessQueryWithTable(query string, args []any, tables map[string][]map[string]any, schemas map[string][]string) ([]string, [][]driver.Value, string, error) {
	cols, rows, err := executeHarnessQuery(query, args, tables, schemas)
	if err != nil {
		return nil, nil, "", err
	}
	return cols, rows, harnessQueriedTable(query, tables, schemas), nil
}

// harnessQueriedTable mirrors executeHarnessQuery's match order and FROM
// capture groups, then resolves via resolveTableName.
func harnessQueriedTable(query string, tables map[string][]map[string]any, schemas map[string][]string) string {
	q := collapseSpaces(query)
	var ref string
	switch {
	case zeroRowStarRe.MatchString(q):
		ref = zeroRowStarRe.FindStringSubmatch(q)[1]
	case strings.Contains(strings.ToUpper(q), "COUNT(CASE WHEN"):
		// executeSharedScalarCountQuery path: table not needed for ColumnTypes.
		return ""
	case countRe.MatchString(q):
		ref = countRe.FindStringSubmatch(q)[1]
	case countDistinctRe.MatchString(q):
		ref = countDistinctRe.FindStringSubmatch(q)[2]
	case aggRe.MatchString(q):
		ref = aggRe.FindStringSubmatch(q)[3]
	case selectRe.MatchString(q):
		ref = selectRe.FindStringSubmatch(q)[2]
	default:
		return ""
	}
	name, err := resolveTableName(ref, tables, schemas)
	if err != nil {
		return ""
	}
	return name
}

type fakeRows struct {
	columns     []string
	rows        [][]driver.Value
	columnTypes []harnessColumnMeta
	idx         int
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

func (r *fakeRows) ColumnTypeDatabaseTypeName(index int) string {
	if index < 0 || index >= len(r.columnTypes) {
		return ""
	}
	return r.columnTypes[index].DatabaseTypeName
}

func (r *fakeRows) ColumnTypeNullable(index int) (nullable, ok bool) {
	if index < 0 || index >= len(r.columnTypes) {
		return false, false
	}
	if r.columnTypes[index].Nullable == nil {
		return false, false
	}
	return *r.columnTypes[index].Nullable, true
}

// harnessColumnMeta models driver-reported ColumnTypes metadata for one column.
type harnessColumnMeta struct {
	Name             string
	DatabaseTypeName string
	Nullable         *bool // nil means unknown (ok=false)
}

var (
	harnessMu          sync.Mutex
	harnessTables      map[string][]map[string]any
	harnessSchemas     map[string][]string
	harnessColumnTypes map[string][]harnessColumnMeta
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

// setHarnessColumns configures ordered physical column names for SELECT *
// discovery probes. Names are returned by Rows.Columns without quote characters.
func setHarnessColumns(t *testing.T, schemas map[string][]string) {
	t.Helper()
	harnessMu.Lock()
	harnessSchemas = schemas
	harnessMu.Unlock()
	t.Cleanup(func() {
		harnessMu.Lock()
		harnessSchemas = nil
		harnessMu.Unlock()
	})
}

// setHarnessColumnTypes configures Rows.ColumnTypes metadata for SELECT *
// discovery probes. When provided, names also populate harnessSchemas so name
// discovery stays consistent. Nullable nil means unknown nullability.
func setHarnessColumnTypes(t *testing.T, meta map[string][]harnessColumnMeta) {
	t.Helper()
	schemas := make(map[string][]string, len(meta))
	for table, cols := range meta {
		names := make([]string, len(cols))
		for i, col := range cols {
			names[i] = col.Name
		}
		schemas[table] = names
	}
	harnessMu.Lock()
	harnessColumnTypes = meta
	harnessSchemas = schemas
	harnessMu.Unlock()
	t.Cleanup(func() {
		harnessMu.Lock()
		harnessColumnTypes = nil
		harnessSchemas = nil
		harnessMu.Unlock()
	})
}

func resolveHarnessColumnTypes(table string, cols []string, byTable map[string][]harnessColumnMeta) []harnessColumnMeta {
	if len(cols) == 0 {
		return nil
	}
	byName := make(map[string]harnessColumnMeta, len(byTable[table]))
	for _, meta := range byTable[table] {
		byName[meta.Name] = meta
	}
	out := make([]harnessColumnMeta, len(cols))
	for i, name := range cols {
		if meta, ok := byName[name]; ok {
			out[i] = meta
			out[i].Name = name
			continue
		}
		out[i] = harnessColumnMeta{Name: name}
	}
	return out
}

func boolPtr(v bool) *bool { return &v }

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
	if !rowMatchesWhere(where, args, pass, "users", nil, nil) {
		t.Fatal("expected in-scope failing row to match scoped failure predicate")
	}

	inScopePass := map[string]any{"tenant_id": "t1", "age": int64(25)}
	if rowMatchesWhere(where, args, inScopePass, "users", nil, nil) {
		t.Fatal("expected in-scope passing row to miss scoped failure predicate")
	}

	outOfScope := map[string]any{"tenant_id": "t2", "age": int64(200)}
	if rowMatchesWhere(where, args, outOfScope, "users", nil, nil) {
		t.Fatal("expected out-of-scope row to miss scoped failure predicate")
	}
}

func TestHarnessWhereEqualityBinding(t *testing.T) {
	row := map[string]any{"tenant_id": "t1", "status": "active"}
	if !rowMatchesWhere(`tenant_id = $1`, []any{"t1"}, row, "users", nil, nil) {
		t.Fatal("expected $n equality match")
	}
	if rowMatchesWhere(`tenant_id = $1`, []any{"t2"}, row, "users", nil, nil) {
		t.Fatal("expected $n equality mismatch")
	}
	if !rowMatchesWhere(`status = ?`, []any{"active"}, row, "users", nil, nil) {
		t.Fatal("expected ? equality match after bindQuestionMarks")
	}
}
