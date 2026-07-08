package gxsql_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sync"

	"github.com/busyminds/gxsql"
	"github.com/busyminds/gxsql/gxsqltest"
)

const exampleDriverName = "gxsql_example_test"

var registerExampleDriverOnce sync.Once

func openExampleDB(scenario string) (*sql.DB, error) {
	registerExampleDriverOnce.Do(func() {
		sql.Register(exampleDriverName, exampleDriver{})
	})
	return sql.Open(exampleDriverName, scenario)
}

type exampleDriver struct{}

func (exampleDriver) Open(name string) (driver.Conn, error) {
	return exampleConn{scenario: name}, nil
}

type exampleConn struct {
	scenario string
}

func (c exampleConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c exampleConn) Close() error                        { return nil }
func (c exampleConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }

func (c exampleConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	switch c.scenario {
	case "pass":
		return c.passQuery(query, args)
	case "keys":
		return c.keysQuery(query, args)
	case "error":
		return nil, fmt.Errorf("example driver: query failed")
	default:
		return nil, fmt.Errorf("example driver: unknown scenario %q", c.scenario)
	}
}

func (exampleConn) passQuery(query string, args []driver.NamedValue) (driver.Rows, error) {
	switch query {
	case `SELECT COUNT(*) FROM "users"`:
		return newExampleRows([]string{"count"}, []driver.Value{int64(2)}), nil
	case `SELECT COUNT(*) FROM "users" WHERE "age" IS NULL OR "age" < ? OR "age" > ?`:
		if err := expectArgs(args, int64(0), int64(120)); err != nil {
			return nil, err
		}
		return newExampleRows([]string{"count"}, []driver.Value{int64(0)}), nil
	default:
		return nil, fmt.Errorf("example driver: unexpected query %q", query)
	}
}

func (exampleConn) keysQuery(query string, args []driver.NamedValue) (driver.Rows, error) {
	switch query {
	case `SELECT COUNT(*) FROM "users"`:
		return newExampleRows([]string{"count"}, []driver.Value{int64(3)}), nil
	case `SELECT COUNT(*) FROM "users" WHERE "age" IS NULL OR "age" < ? OR "age" > ?`:
		if err := expectArgs(args, int64(0), int64(120)); err != nil {
			return nil, err
		}
		return newExampleRows([]string{"count"}, []driver.Value{int64(2)}), nil
	case `SELECT "id" FROM "users" WHERE "age" IS NULL OR "age" < ? OR "age" > ? ORDER BY "id"`:
		if err := expectArgs(args, int64(0), int64(120)); err != nil {
			return nil, err
		}
		return newExampleRows([]string{"id"}, []driver.Value{int64(2)}, []driver.Value{int64(3)}), nil
	default:
		return nil, fmt.Errorf("example driver: unexpected query %q", query)
	}
}

func expectArgs(args []driver.NamedValue, want ...any) error {
	if len(args) != len(want) {
		return fmt.Errorf("example driver: arg count = %d, want %d", len(args), len(want))
	}
	for i := range want {
		if args[i].Value != want[i] {
			return fmt.Errorf("example driver: arg %d = %v, want %v", i, args[i].Value, want[i])
		}
	}
	return nil
}

type exampleRows struct {
	cols []string
	rows [][]driver.Value
	idx  int
}

func newExampleRows(cols []string, rows ...[]driver.Value) driver.Rows {
	return &exampleRows{cols: cols, rows: rows}
}

func (r *exampleRows) Columns() []string { return r.cols }
func (r *exampleRows) Close() error      { return nil }

func (r *exampleRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.idx])
	r.idx++
	return nil
}

type recorderT struct {
	errors int
	fatals int
}

func (r *recorderT) Helper() {}
func (r *recorderT) Errorf(string, ...any) {
	r.errors++
}
func (r *recorderT) Fatalf(string, ...any) {
	r.fatals++
}

func ExampleSuite_basic() {
	db, err := openExampleDB("pass")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	suite := gxsql.NewSuite(
		gxsql.RowCount().Equal(2),
		gxsql.Int("age").Between(0, 120),
	)
	report, err := suite.ValidateTable(
		context.Background(), db, gxsql.Table("users"),
		gxsql.WithDialect(gxsql.SQLite()),
	)
	if err != nil {
		panic(err)
	}

	fmt.Println(report.OK())
	fmt.Println(report.String())

	// Output:
	// true
	// gxsql report: 2/2 expectations passed
	//   ✓ row count == 2: got 2
	//   ✓ age between [0,120] (2 rows)
}

func ExampleSuite_failedRowKeysAndSummary() {
	db, err := openExampleDB("keys")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	report, err := gxsql.NewSuite(gxsql.Int("age").Between(0, 120)).WithSampleCap(0).ValidateTable(
		context.Background(), db, gxsql.Table("users"),
		gxsql.WithDialect(gxsql.SQLite()),
		gxsql.WithKey("id"),
		gxsql.WithFailedKeysCap(0),
	)
	if err != nil {
		panic(err)
	}

	fmt.Println(report.OK())
	fmt.Println(report.String())
	fmt.Println(report.Results[0].FailedKeys)

	// Output:
	// false
	// gxsql report: 0/1 expectations passed
	//   ✗ age between [0,120]  2/3 failed (66.7%)  e.g. [] @ [[2] [3]]
	// [[2] [3]]
}

func ExampleSuite_continueOnError() {
	db, err := openExampleDB("error")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	report, err := gxsql.NewSuite(
		gxsql.Int("age").Between(0, 120),
		gxsql.String("email").NotEmpty(),
	).ValidateTable(
		context.Background(), db, gxsql.Table("users"),
		gxsql.WithDialect(gxsql.SQLite()),
		gxsql.ContinueOnError(),
	)

	fmt.Println(err == nil)
	fmt.Println(report.Results[0].Err != nil && report.Results[1].Err != nil)
	fmt.Println(report.String())

	// Output:
	// true
	// true
	// gxsql report: 0/2 expectations passed
	//   ✗ age between [0,120]
	//   ✗ email not empty
}

func Example() {
	checkT := &recorderT{}
	fmt.Println(gxsqltest.Check(
		checkT,
		context.Background(),
		gxsql.NewSuite(gxsql.Column("status").In()),
		nil,
		gxsql.Table("users"),
		gxsql.WithDialect(gxsql.Postgres()),
	))
	fmt.Println(checkT.errors, checkT.fatals)

	requireT := &recorderT{}
	gxsqltest.Require(
		requireT,
		context.Background(),
		gxsql.NewSuite(gxsql.Column("status").In()),
		nil,
		gxsql.Table("users"),
		gxsql.WithDialect(gxsql.Postgres()),
	)
	fmt.Println(requireT.errors, requireT.fatals)

	// Output:
	// false
	// 1 0
	// 0 1
}
