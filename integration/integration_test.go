package gxsql_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	_ "github.com/go-sql-driver/mysql"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/busyminds/gxsql"
	"github.com/busyminds/gxsql/internal/conformance"
)

func TestSQLiteConformance(t *testing.T) {
	db, err := sql.Open("sqlite", "file:gxsql_conformance?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	setupSQLite(t, db)

	conformance.Run(t, conformance.Config{
		DB:          db,
		Dialect:     gxsql.SQLite(),
		Table:       gxsql.Table("users"),
		EmptyTable:  gxsql.Table("empty_users"),
		ParentTable: gxsql.SchemaTable("main", "customers"),
		Transaction: transactionFactory(db),
	})
}

func TestDuckDBConformance(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	setupDuckDB(t, db)

	conformance.Run(t, conformance.Config{
		DB:          db,
		Dialect:     gxsql.DuckDB(),
		Table:       gxsql.Table("users"),
		EmptyTable:  gxsql.Table("empty_users"),
		ParentTable: gxsql.SchemaTable("main", "customers"),
		Transaction: transactionFactory(db),
	})
}

func TestPostgresConformance(t *testing.T) {
	dsn := os.Getenv("GXSQL_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("GXSQL_POSTGRES_DSN is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	setupPostgres(t, db)

	conformance.Run(t, conformance.Config{
		DB:          db,
		Dialect:     gxsql.Postgres(),
		Table:       gxsql.SchemaTable("public", "users"),
		EmptyTable:  gxsql.SchemaTable("public", "empty_users"),
		ParentTable: gxsql.SchemaTable("public", "customers"),
		Transaction: transactionFactory(db),
	})
}

func TestSQLiteCustomCountConformance(t *testing.T) {
	db, err := sql.Open("sqlite", "file:gxsql_custom_count?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	setupSQLiteCustomCount(t, db)

	conformance.RunCustomCount(t, conformance.CustomCountConfig{
		DB:      db,
		Dialect: gxsql.SQLite(),
		Table:   gxsql.Table("order_lines"),
	})
}

func TestDuckDBCustomCountConformance(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	setupDuckDBCustomCount(t, db)

	conformance.RunCustomCount(t, conformance.CustomCountConfig{
		DB:      db,
		Dialect: gxsql.DuckDB(),
		Table:   gxsql.Table("order_lines"),
	})
}

func TestPostgresCustomCountConformance(t *testing.T) {
	dsn := os.Getenv("GXSQL_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("GXSQL_POSTGRES_DSN is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	setupPostgresCustomCount(t, db)

	conformance.RunCustomCount(t, conformance.CustomCountConfig{
		DB:      db,
		Dialect: gxsql.Postgres(),
		Table:   gxsql.SchemaTable("public", "order_lines"),
	})
}

func TestMySQLCustomCountConformance(t *testing.T) {
	dsn := os.Getenv("GXSQL_MYSQL_DSN")
	if dsn == "" {
		t.Fatal("GXSQL_MYSQL_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	setupMySQLCustomCount(t, db)

	conformance.RunCustomCount(t, conformance.CustomCountConfig{
		DB:      db,
		Dialect: gxsql.MySQL(),
		Table:   gxsql.SchemaTable("gxsql", "order_lines"),
	})
}

func TestMySQLConformance(t *testing.T) {
	dsn := os.Getenv("GXSQL_MYSQL_DSN")
	if dsn == "" {
		t.Fatal("GXSQL_MYSQL_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	setupMySQL(t, db)

	report, err := gxsql.NewSuite(gxsql.String("name").LenEqual(1)).ValidateTable(
		context.Background(), db, gxsql.SchemaTable("gxsql", "utf8_char_length"), gxsql.WithDialect(gxsql.MySQL()),
	)
	if err != nil {
		t.Fatalf("MySQL character length ValidateTable: %v", err)
	}
	if len(report.Results) != 1 {
		t.Fatalf("MySQL character length results: got %d, want 1", len(report.Results))
	}
	if !report.Results[0].Success {
		t.Fatalf("MySQL character length LenEqual(1) on é: failed %d of %d rows",
			report.Results[0].FailedCount, report.Results[0].Total)
	}

	conformance.Run(t, conformance.Config{
		DB:          db,
		Dialect:     gxsql.MySQL(),
		Table:       gxsql.SchemaTable("gxsql", "users"),
		EmptyTable:  gxsql.SchemaTable("gxsql", "empty_users"),
		ParentTable: gxsql.SchemaTable("gxsql", "customers"),
		Transaction: transactionFactory(db),
	})
}

func transactionFactory(db *sql.DB) func(context.Context) (gxsql.DB, func() error, error) {
	return func(ctx context.Context) (gxsql.DB, func() error, error) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return nil, nil, err
		}
		return tx, tx.Rollback, nil
	}
}

func setupSQLite(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, query := range []string{
		`CREATE TABLE customers (tenant_id TEXT NOT NULL, id INTEGER NOT NULL, PRIMARY KEY (tenant_id, id))`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER, score REAL, nullable TEXT, payload BLOB, tenant_id TEXT, batch_id INTEGER, event_at TIMESTAMP, order_id TEXT, customer_id INTEGER)`,
		`CREATE TABLE empty_users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER, score REAL, nullable TEXT, payload BLOB, tenant_id TEXT, batch_id INTEGER, event_at TIMESTAMP, order_id TEXT, customer_id INTEGER)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("SQLite schema: %v", err)
		}
	}
	insertParentFixtures(t, db, "?", "customers")
	insertFixtures(t, db, "?", "users")
}

func setupDuckDB(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, query := range []string{
		`DROP TABLE IF EXISTS users`,
		`DROP TABLE IF EXISTS empty_users`,
		`DROP TABLE IF EXISTS customers`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("DuckDB cleanup: %v", err)
		}
	}
	for _, query := range []string{
		`CREATE TABLE customers (tenant_id VARCHAR NOT NULL, id BIGINT NOT NULL, PRIMARY KEY (tenant_id, id))`,
		`CREATE TABLE users (id BIGINT PRIMARY KEY, name VARCHAR, age INTEGER, score DOUBLE, nullable VARCHAR, payload BLOB, tenant_id VARCHAR, batch_id BIGINT, event_at TIMESTAMP, order_id VARCHAR, customer_id BIGINT)`,
		`CREATE TABLE empty_users (id BIGINT PRIMARY KEY, name VARCHAR, age INTEGER, score DOUBLE, nullable VARCHAR, payload BLOB, tenant_id VARCHAR, batch_id BIGINT, event_at TIMESTAMP, order_id VARCHAR, customer_id BIGINT)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("DuckDB schema: %v", err)
		}
	}
	insertParentFixtures(t, db, "$", "customers")
	insertFixtures(t, db, "$", "users")
}

func setupPostgres(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DROP TABLE IF EXISTS public.users, public.empty_users, public.customers`); err != nil {
		t.Fatalf("PostgreSQL cleanup: %v", err)
	}
	for _, query := range []string{
		`CREATE TABLE public.customers (tenant_id TEXT NOT NULL, id BIGINT NOT NULL, PRIMARY KEY (tenant_id, id))`,
		`CREATE TABLE public.users (id BIGINT PRIMARY KEY, name TEXT, age INTEGER, score DOUBLE PRECISION, nullable TEXT, payload BYTEA, tenant_id TEXT, batch_id BIGINT, event_at TIMESTAMP WITH TIME ZONE, order_id TEXT, customer_id BIGINT)`,
		`CREATE TABLE public.empty_users (id BIGINT PRIMARY KEY, name TEXT, age INTEGER, score DOUBLE PRECISION, nullable TEXT, payload BYTEA, tenant_id TEXT, batch_id BIGINT, event_at TIMESTAMP WITH TIME ZONE, order_id TEXT, customer_id BIGINT)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("PostgreSQL schema: %v", err)
		}
	}
	insertParentFixtures(t, db, "$", "public.customers")
	insertFixtures(t, db, "$", "public.users")
	t.Cleanup(func() { _, _ = db.Exec(`DROP TABLE IF EXISTS public.users, public.empty_users, public.customers`) })
}

func setupMySQL(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, query := range []string{
		`DROP TABLE IF EXISTS users`,
		`DROP TABLE IF EXISTS empty_users`,
		`DROP TABLE IF EXISTS customers`,
		`DROP TABLE IF EXISTS utf8_char_length`,
		`CREATE TABLE customers (tenant_id VARCHAR(255) NOT NULL, id BIGINT NOT NULL, PRIMARY KEY (tenant_id, id)) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE users (id BIGINT PRIMARY KEY, name VARCHAR(255), age INTEGER, score DOUBLE, nullable TEXT, payload BLOB, tenant_id VARCHAR(255), batch_id BIGINT, event_at DATETIME(6), order_id VARCHAR(255), customer_id BIGINT) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE empty_users (id BIGINT PRIMARY KEY, name VARCHAR(255), age INTEGER, score DOUBLE, nullable TEXT, payload BLOB, tenant_id VARCHAR(255), batch_id BIGINT, event_at DATETIME(6), order_id VARCHAR(255), customer_id BIGINT) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE utf8_char_length (id BIGINT PRIMARY KEY, name VARCHAR(255)) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("MySQL schema: %v", err)
		}
	}
	insertParentFixtures(t, db, "?", "customers")
	insertFixtures(t, db, "?", "users")
	if _, err := db.Exec(`INSERT INTO utf8_char_length (id, name) VALUES (?, ?)`, 1, "é"); err != nil {
		t.Fatalf("MySQL utf8_char_length fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS users`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS empty_users`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS customers`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS utf8_char_length`)
	})
}

func setupSQLiteCustomCount(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, query := range []string{
		`CREATE TABLE accounts (id INTEGER PRIMARY KEY, status TEXT NOT NULL)`,
		`CREATE TABLE order_lines (id INTEGER PRIMARY KEY, account_id INTEGER NOT NULL, batch_id INTEGER NOT NULL, tenant_id TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("SQLite custom-count schema: %v", err)
		}
	}
	insertCustomCountFixtures(t, db, "?", "accounts", "order_lines")
}

func setupDuckDBCustomCount(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, query := range []string{
		`DROP TABLE IF EXISTS order_lines`,
		`DROP TABLE IF EXISTS accounts`,
		`CREATE TABLE accounts (id BIGINT PRIMARY KEY, status VARCHAR NOT NULL)`,
		`CREATE TABLE order_lines (id BIGINT PRIMARY KEY, account_id BIGINT NOT NULL, batch_id BIGINT NOT NULL, tenant_id VARCHAR NOT NULL)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("DuckDB custom-count schema: %v", err)
		}
	}
	insertCustomCountFixtures(t, db, "$", "accounts", "order_lines")
}

func setupPostgresCustomCount(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DROP TABLE IF EXISTS public.order_lines, public.accounts`); err != nil {
		t.Fatalf("PostgreSQL custom-count cleanup: %v", err)
	}
	for _, query := range []string{
		`CREATE TABLE public.accounts (id BIGINT PRIMARY KEY, status TEXT NOT NULL)`,
		`CREATE TABLE public.order_lines (id BIGINT PRIMARY KEY, account_id BIGINT NOT NULL, batch_id BIGINT NOT NULL, tenant_id TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("PostgreSQL custom-count schema: %v", err)
		}
	}
	insertCustomCountFixtures(t, db, "$", "public.accounts", "public.order_lines")
	t.Cleanup(func() { _, _ = db.Exec(`DROP TABLE IF EXISTS public.order_lines, public.accounts`) })
}

func setupMySQLCustomCount(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, query := range []string{
		`DROP TABLE IF EXISTS order_lines`,
		`DROP TABLE IF EXISTS accounts`,
		`CREATE TABLE accounts (id BIGINT PRIMARY KEY, status VARCHAR(32) NOT NULL) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE order_lines (id BIGINT PRIMARY KEY, account_id BIGINT NOT NULL, batch_id BIGINT NOT NULL, tenant_id VARCHAR(255) NOT NULL) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("MySQL custom-count schema: %v", err)
		}
	}
	insertCustomCountFixtures(t, db, "?", "accounts", "order_lines")
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS order_lines`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS accounts`)
	})
}

func insertCustomCountFixtures(t *testing.T, db *sql.DB, placeholder, accountsTable, orderLinesTable string) {
	t.Helper()
	placeholderAt := func(start, count int) []string {
		out := make([]string, count)
		for i := range out {
			if placeholder == "?" {
				out[i] = "?"
				continue
			}
			out[i] = fmt.Sprintf("%s%d", placeholder, start+i)
		}
		return out
	}

	p1 := placeholderAt(1, 2)
	p2 := placeholderAt(3, 2)
	p3 := placeholderAt(5, 2)
	accountQuery := fmt.Sprintf(
		"INSERT INTO %s (id, status) VALUES (%s, %s), (%s, %s), (%s, %s)",
		accountsTable,
		p1[0], p1[1], p2[0], p2[1], p3[0], p3[1],
	)
	if _, err := db.Exec(accountQuery, int64(1), "active", int64(2), "inactive", int64(3), "active"); err != nil {
		t.Fatalf("insert accounts: %v", err)
	}

	linePlaceholders := make([]string, 4)
	for i := range linePlaceholders {
		if placeholder == "?" {
			linePlaceholders[i] = placeholder
			continue
		}
		linePlaceholders[i] = fmt.Sprintf("%s%d", placeholder, i+1)
	}
	lineQuery := fmt.Sprintf(
		"INSERT INTO %s (id, account_id, batch_id, tenant_id) VALUES (%s, %s, %s, %s)",
		orderLinesTable,
		linePlaceholders[0], linePlaceholders[1], linePlaceholders[2], linePlaceholders[3],
	)
	lines := []struct {
		id        int64
		accountID int64
		batchID   int64
		tenantID  string
	}{
		{1, 1, 1, "tenant-a"},
		{2, 1, 1, "tenant-a"},
		{3, 2, 2, "tenant-a"},
		{4, 3, 2, "tenant-b"},
		{5, 3, 1, "tenant-b"},
	}
	for _, line := range lines {
		if _, err := db.Exec(lineQuery, line.id, line.accountID, line.batchID, line.tenantID); err != nil {
			t.Fatalf("insert order line %d: %v", line.id, err)
		}
	}
}

func insertParentFixtures(t *testing.T, db *sql.DB, placeholder, table string) {
	t.Helper()
	argsPlaceholders := make([]string, 2)
	for i := range argsPlaceholders {
		if placeholder == "?" {
			argsPlaceholders[i] = placeholder
			continue
		}
		argsPlaceholders[i] = fmt.Sprintf("%s%d", placeholder, i+1)
	}
	query := fmt.Sprintf("INSERT INTO %s (tenant_id, id) VALUES (%s, %s)",
		table, argsPlaceholders[0], argsPlaceholders[1])
	parents := []struct {
		tenantID string
		id       int64
	}{
		{"tenant-a", 10},
		{"tenant-b", 30},
		{"tenant-c", 1},
	}
	for _, parent := range parents {
		if _, err := db.Exec(query, parent.tenantID, parent.id); err != nil {
			t.Fatalf("insert parent %s/%d: %v", parent.tenantID, parent.id, err)
		}
	}
}

func insertFixtures(t *testing.T, db *sql.DB, placeholder, table string) {
	t.Helper()
	argsPlaceholders := make([]string, 11)
	for i := range argsPlaceholders {
		if placeholder == "?" {
			argsPlaceholders[i] = placeholder
			continue
		}
		argsPlaceholders[i] = fmt.Sprintf("%s%d", placeholder, i+1)
	}
	query := fmt.Sprintf("INSERT INTO %s (id, name, age, score, nullable, payload, tenant_id, batch_id, event_at, order_id, customer_id) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)",
		table, argsPlaceholders[0], argsPlaceholders[1], argsPlaceholders[2], argsPlaceholders[3], argsPlaceholders[4], argsPlaceholders[5], argsPlaceholders[6], argsPlaceholders[7], argsPlaceholders[8], argsPlaceholders[9], argsPlaceholders[10])
	fixtures := []struct {
		id         int64
		name       string
		age        any
		score      any
		nullable   any
		payload    []byte
		tenantID   string
		batchID    int64
		eventAt    time.Time
		orderID    any
		customerID any
	}{
		{1, "alice", 20, 1.5, "present", []byte{1, 2}, "tenant-a", 1, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), "dup", int64(10)},
		{2, "", nil, 2.5, nil, []byte{3}, "tenant-a", 2, time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), "dup", nil},
		{3, "alice", 200, nil, "present", []byte{4}, "tenant-b", 1, time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC), "x", int64(30)},
		{4, "zed", 10, 3.5, "present", []byte{5}, "tenant-b", 2, time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC), nil, int64(99)},
	}
	for _, fixture := range fixtures {
		if _, err := db.Exec(query, fixture.id, fixture.name, fixture.age, fixture.score,
			fixture.nullable, fixture.payload, fixture.tenantID, fixture.batchID, fixture.eventAt,
			fixture.orderID, fixture.customerID); err != nil {
			t.Fatalf("insert fixture %d: %v", fixture.id, err)
		}
	}
}
