package gxsql_test

import (
	"context"
	"database/sql"
	"fmt"
	"math"
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

func TestSQLiteReconcileConformance(t *testing.T) {
	db, err := sql.Open("sqlite", "file:gxsql_reconcile?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	setupSQLiteReconcile(t, db)

	conformance.RunReconcile(t, conformance.ReconcileConfig{
		DB:         db,
		Dialect:    gxsql.SQLite(),
		Left:       gxsql.Table("reconcile_left"),
		Right:      gxsql.Table("reconcile_right"),
		EmptyLeft:  gxsql.Table("empty_reconcile_left"),
		EmptyRight: gxsql.Table("empty_reconcile_right"),
	})
}

func TestDuckDBReconcileConformance(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	setupDuckDBReconcile(t, db)

	conformance.RunReconcile(t, conformance.ReconcileConfig{
		DB:         db,
		Dialect:    gxsql.DuckDB(),
		Left:       gxsql.Table("reconcile_left"),
		Right:      gxsql.Table("reconcile_right"),
		EmptyLeft:  gxsql.Table("empty_reconcile_left"),
		EmptyRight: gxsql.Table("empty_reconcile_right"),
	})
}

func TestPostgresReconcileConformance(t *testing.T) {
	dsn := os.Getenv("GXSQL_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("GXSQL_POSTGRES_DSN is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	setupPostgresReconcile(t, db)

	conformance.RunReconcile(t, conformance.ReconcileConfig{
		DB:         db,
		Dialect:    gxsql.Postgres(),
		Left:       gxsql.SchemaTable("public", "reconcile_left"),
		Right:      gxsql.SchemaTable("public", "reconcile_right"),
		EmptyLeft:  gxsql.SchemaTable("public", "empty_reconcile_left"),
		EmptyRight: gxsql.SchemaTable("public", "empty_reconcile_right"),
	})
}

func TestMySQLReconcileConformance(t *testing.T) {
	dsn := os.Getenv("GXSQL_MYSQL_DSN")
	if dsn == "" {
		t.Fatal("GXSQL_MYSQL_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	setupMySQLReconcile(t, db)

	conformance.RunReconcile(t, conformance.ReconcileConfig{
		DB:         db,
		Dialect:    gxsql.MySQL(),
		Left:       gxsql.SchemaTable("gxsql", "reconcile_left"),
		Right:      gxsql.SchemaTable("gxsql", "reconcile_right"),
		EmptyLeft:  gxsql.SchemaTable("gxsql", "empty_reconcile_left"),
		EmptyRight: gxsql.SchemaTable("gxsql", "empty_reconcile_right"),
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
		`CREATE TABLE customers (tenant_id TEXT NOT NULL, id INTEGER NOT NULL, status TEXT NOT NULL, PRIMARY KEY (tenant_id, id))`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER, score REAL, nullable TEXT, payload BLOB, tenant_id TEXT, batch_id INTEGER, event_at TIMESTAMP, order_id TEXT, customer_id INTEGER, paid_cents INTEGER, invoice_cents INTEGER, start_at TIMESTAMP, end_at TIMESTAMP, actual_units INTEGER, planned_units INTEGER)`,
		`CREATE TABLE empty_users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER, score REAL, nullable TEXT, payload BLOB, tenant_id TEXT, batch_id INTEGER, event_at TIMESTAMP, order_id TEXT, customer_id INTEGER, paid_cents INTEGER, invoice_cents INTEGER, start_at TIMESTAMP, end_at TIMESTAMP, actual_units INTEGER, planned_units INTEGER)`,
		`CREATE TABLE cross_column_rows (id INTEGER PRIMARY KEY, paid_cents INTEGER, invoice_cents INTEGER, start_at TIMESTAMP, end_at TIMESTAMP, actual_units INTEGER, planned_units INTEGER, label TEXT, amount INTEGER)`,
		`CREATE TABLE temporal_rows (id INTEGER PRIMARY KEY, tenant_id TEXT, event_at TIMESTAMP, ingested_at TIMESTAMP)`,
		`CREATE TABLE structural_cols (id INTEGER, event_time TIMESTAMP, payload TEXT)`,
		`CREATE TABLE structural_extra (id INTEGER, event_time TIMESTAMP, payload TEXT, note TEXT)`,
		`CREATE TABLE structural_case (id INTEGER, "EventTime" TIMESTAMP, payload TEXT)`,
		`CREATE TABLE schema_contract (id INTEGER NOT NULL, email TEXT, score REAL, payload BLOB)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("SQLite schema: %v", err)
		}
	}
	insertParentFixtures(t, db, "?", "customers")
	insertFixtures(t, db, "?", "users")
	insertCrossColumnFixtures(t, db, "?", "cross_column_rows")
	insertTemporalFixtures(t, db, "?", "temporal_rows")
}

func setupDuckDB(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, query := range []string{
		`DROP TABLE IF EXISTS users`,
		`DROP TABLE IF EXISTS empty_users`,
		`DROP TABLE IF EXISTS customers`,
		`DROP TABLE IF EXISTS cross_column_rows`,
		`DROP TABLE IF EXISTS temporal_rows`,
		`DROP TABLE IF EXISTS structural_cols`,
		`DROP TABLE IF EXISTS structural_extra`,
		`DROP TABLE IF EXISTS structural_case`,
		`DROP TABLE IF EXISTS schema_contract`,
		`SET TimeZone='UTC'`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("DuckDB cleanup: %v", err)
		}
	}
	for _, query := range []string{
		`CREATE TABLE customers (tenant_id VARCHAR NOT NULL, id BIGINT NOT NULL, status VARCHAR NOT NULL, PRIMARY KEY (tenant_id, id))`,
		`CREATE TABLE users (id BIGINT PRIMARY KEY, name VARCHAR, age INTEGER, score DOUBLE, nullable VARCHAR, payload BLOB, tenant_id VARCHAR, batch_id BIGINT, event_at TIMESTAMPTZ, order_id VARCHAR, customer_id BIGINT, paid_cents BIGINT, invoice_cents BIGINT, start_at TIMESTAMP, end_at TIMESTAMP, actual_units BIGINT, planned_units BIGINT)`,
		`CREATE TABLE empty_users (id BIGINT PRIMARY KEY, name VARCHAR, age INTEGER, score DOUBLE, nullable VARCHAR, payload BLOB, tenant_id VARCHAR, batch_id BIGINT, event_at TIMESTAMPTZ, order_id VARCHAR, customer_id BIGINT, paid_cents BIGINT, invoice_cents BIGINT, start_at TIMESTAMP, end_at TIMESTAMP, actual_units BIGINT, planned_units BIGINT)`,
		`CREATE TABLE cross_column_rows (id BIGINT PRIMARY KEY, paid_cents BIGINT, invoice_cents BIGINT, start_at TIMESTAMP, end_at TIMESTAMP, actual_units BIGINT, planned_units BIGINT, label VARCHAR, amount BIGINT)`,
		`CREATE TABLE temporal_rows (id BIGINT PRIMARY KEY, tenant_id VARCHAR, event_at TIMESTAMPTZ, ingested_at TIMESTAMPTZ)`,
		`CREATE TABLE structural_cols (id BIGINT, event_time TIMESTAMP, payload VARCHAR)`,
		`CREATE TABLE structural_extra (id BIGINT, event_time TIMESTAMP, payload VARCHAR, note VARCHAR)`,
		`CREATE TABLE structural_case (id BIGINT, "EventTime" TIMESTAMP, payload VARCHAR)`,
		`CREATE TABLE schema_contract (id BIGINT NOT NULL, email VARCHAR, score DOUBLE, payload BLOB)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("DuckDB schema: %v", err)
		}
	}
	insertParentFixtures(t, db, "$", "customers")
	insertFixtures(t, db, "$", "users")
	insertCrossColumnFixtures(t, db, "$", "cross_column_rows")
	insertTemporalFixtures(t, db, "$", "temporal_rows")
}

func setupPostgres(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DROP TABLE IF EXISTS public.users, public.empty_users, public.customers, public.cross_column_rows, public.temporal_rows, public.structural_cols, public.structural_extra, public.structural_case, public.schema_contract`); err != nil {
		t.Fatalf("PostgreSQL cleanup: %v", err)
	}
	if _, err := db.Exec(`SET TIME ZONE 'UTC'`); err != nil {
		t.Fatalf("PostgreSQL timezone: %v", err)
	}
	for _, query := range []string{
		`CREATE TABLE public.customers (tenant_id TEXT NOT NULL, id BIGINT NOT NULL, status TEXT NOT NULL, PRIMARY KEY (tenant_id, id))`,
		`CREATE TABLE public.users (id BIGINT PRIMARY KEY, name TEXT, age INTEGER, score DOUBLE PRECISION, nullable TEXT, payload BYTEA, tenant_id TEXT, batch_id BIGINT, event_at TIMESTAMP WITH TIME ZONE, order_id TEXT, customer_id BIGINT, paid_cents BIGINT, invoice_cents BIGINT, start_at TIMESTAMP WITH TIME ZONE, end_at TIMESTAMP WITH TIME ZONE, actual_units BIGINT, planned_units BIGINT)`,
		`CREATE TABLE public.empty_users (id BIGINT PRIMARY KEY, name TEXT, age INTEGER, score DOUBLE PRECISION, nullable TEXT, payload BYTEA, tenant_id TEXT, batch_id BIGINT, event_at TIMESTAMP WITH TIME ZONE, order_id TEXT, customer_id BIGINT, paid_cents BIGINT, invoice_cents BIGINT, start_at TIMESTAMP WITH TIME ZONE, end_at TIMESTAMP WITH TIME ZONE, actual_units BIGINT, planned_units BIGINT)`,
		`CREATE TABLE public.cross_column_rows (id BIGINT PRIMARY KEY, paid_cents BIGINT, invoice_cents BIGINT, start_at TIMESTAMP WITH TIME ZONE, end_at TIMESTAMP WITH TIME ZONE, actual_units BIGINT, planned_units BIGINT, label TEXT, amount BIGINT)`,
		`CREATE TABLE public.temporal_rows (id BIGINT PRIMARY KEY, tenant_id TEXT, event_at TIMESTAMP WITH TIME ZONE, ingested_at TIMESTAMP WITH TIME ZONE)`,
		`CREATE TABLE public.structural_cols (id BIGINT, event_time TIMESTAMP WITH TIME ZONE, payload TEXT)`,
		`CREATE TABLE public.structural_extra (id BIGINT, event_time TIMESTAMP WITH TIME ZONE, payload TEXT, note TEXT)`,
		`CREATE TABLE public.structural_case (id BIGINT, "EventTime" TIMESTAMP WITH TIME ZONE, payload TEXT)`,
		`CREATE TABLE public.schema_contract (id BIGINT NOT NULL, email TEXT, score DOUBLE PRECISION, payload BYTEA)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("PostgreSQL schema: %v", err)
		}
	}
	insertParentFixtures(t, db, "$", "public.customers")
	insertFixtures(t, db, "$", "public.users")
	insertCrossColumnFixtures(t, db, "$", "public.cross_column_rows")
	insertTemporalFixtures(t, db, "$", "public.temporal_rows")
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS public.users, public.empty_users, public.customers, public.cross_column_rows, public.temporal_rows, public.structural_cols, public.structural_extra, public.structural_case, public.schema_contract`)
	})
}

func setupMySQL(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, query := range []string{
		`DROP TABLE IF EXISTS users`,
		`DROP TABLE IF EXISTS empty_users`,
		`DROP TABLE IF EXISTS customers`,
		`DROP TABLE IF EXISTS cross_column_rows`,
		`DROP TABLE IF EXISTS temporal_rows`,
		`DROP TABLE IF EXISTS structural_cols`,
		`DROP TABLE IF EXISTS structural_extra`,
		`DROP TABLE IF EXISTS structural_case`,
		`DROP TABLE IF EXISTS schema_contract`,
		`DROP TABLE IF EXISTS utf8_char_length`,
		`SET time_zone = '+00:00'`,
		`CREATE TABLE customers (tenant_id VARCHAR(255) NOT NULL, id BIGINT NOT NULL, status VARCHAR(32) NOT NULL, PRIMARY KEY (tenant_id, id)) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE users (id BIGINT PRIMARY KEY, name VARCHAR(255), age INTEGER, score DOUBLE, nullable TEXT, payload BLOB, tenant_id VARCHAR(255), batch_id BIGINT, event_at DATETIME(6), order_id VARCHAR(255), customer_id BIGINT, paid_cents BIGINT, invoice_cents BIGINT, start_at DATETIME(6), end_at DATETIME(6), actual_units BIGINT, planned_units BIGINT) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE empty_users (id BIGINT PRIMARY KEY, name VARCHAR(255), age INTEGER, score DOUBLE, nullable TEXT, payload BLOB, tenant_id VARCHAR(255), batch_id BIGINT, event_at DATETIME(6), order_id VARCHAR(255), customer_id BIGINT, paid_cents BIGINT, invoice_cents BIGINT, start_at DATETIME(6), end_at DATETIME(6), actual_units BIGINT, planned_units BIGINT) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE cross_column_rows (id BIGINT PRIMARY KEY, paid_cents BIGINT, invoice_cents BIGINT, start_at DATETIME(6), end_at DATETIME(6), actual_units BIGINT, planned_units BIGINT, label VARCHAR(255), amount BIGINT) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE temporal_rows (id BIGINT PRIMARY KEY, tenant_id VARCHAR(255), event_at DATETIME(6), ingested_at DATETIME(6)) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE structural_cols (id BIGINT, event_time DATETIME(6), payload TEXT) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE structural_extra (id BIGINT, event_time DATETIME(6), payload TEXT, note TEXT) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		"CREATE TABLE structural_case (id BIGINT, `EventTime` DATETIME(6), payload TEXT) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin",
		`CREATE TABLE schema_contract (id BIGINT NOT NULL, email TEXT, score DOUBLE, payload BLOB) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE utf8_char_length (id BIGINT PRIMARY KEY, name VARCHAR(255)) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("MySQL schema: %v", err)
		}
	}
	insertParentFixtures(t, db, "?", "customers")
	insertFixtures(t, db, "?", "users")
	insertCrossColumnFixtures(t, db, "?", "cross_column_rows")
	insertTemporalFixtures(t, db, "?", "temporal_rows")
	if _, err := db.Exec(`INSERT INTO utf8_char_length (id, name) VALUES (?, ?)`, 1, "é"); err != nil {
		t.Fatalf("MySQL utf8_char_length fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS users`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS empty_users`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS customers`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS cross_column_rows`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS temporal_rows`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS structural_cols`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS structural_extra`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS structural_case`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS schema_contract`)
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
	argsPlaceholders := make([]string, 3)
	for i := range argsPlaceholders {
		if placeholder == "?" {
			argsPlaceholders[i] = placeholder
			continue
		}
		argsPlaceholders[i] = fmt.Sprintf("%s%d", placeholder, i+1)
	}
	query := fmt.Sprintf("INSERT INTO %s (tenant_id, id, status) VALUES (%s, %s, %s)",
		table, argsPlaceholders[0], argsPlaceholders[1], argsPlaceholders[2])
	parents := []struct {
		tenantID string
		id       int64
		status   string
	}{
		{"tenant-a", 10, "active"},
		{"tenant-b", 30, "inactive"},
		{"tenant-c", 1, "active"},
	}
	for _, parent := range parents {
		if _, err := db.Exec(query, parent.tenantID, parent.id, parent.status); err != nil {
			t.Fatalf("insert parent %s/%d: %v", parent.tenantID, parent.id, err)
		}
	}
}

func insertFixtures(t *testing.T, db *sql.DB, placeholder, table string) {
	t.Helper()
	argsPlaceholders := make([]string, 17)
	for i := range argsPlaceholders {
		if placeholder == "?" {
			argsPlaceholders[i] = placeholder
			continue
		}
		argsPlaceholders[i] = fmt.Sprintf("%s%d", placeholder, i+1)
	}
	query := fmt.Sprintf("INSERT INTO %s (id, name, age, score, nullable, payload, tenant_id, batch_id, event_at, order_id, customer_id, paid_cents, invoice_cents, start_at, end_at, actual_units, planned_units) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)",
		table, argsPlaceholders[0], argsPlaceholders[1], argsPlaceholders[2], argsPlaceholders[3],
		argsPlaceholders[4], argsPlaceholders[5], argsPlaceholders[6], argsPlaceholders[7],
		argsPlaceholders[8], argsPlaceholders[9], argsPlaceholders[10], argsPlaceholders[11],
		argsPlaceholders[12], argsPlaceholders[13], argsPlaceholders[14], argsPlaceholders[15],
		argsPlaceholders[16])
	fixtures := []struct {
		id           int64
		name         string
		age          any
		score        any
		nullable     any
		payload      []byte
		tenantID     string
		batchID      int64
		eventAt      time.Time
		orderID      any
		customerID   any
		paidCents    any
		invoiceCents any
		startAt      any
		endAt        any
		actualUnits  any
		plannedUnits any
	}{
		{1, "alice", 20, 1.5, "present", []byte{1, 2}, "tenant-a", 1, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), "dup", int64(10), int64(10), int64(10), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), int64(20), int64(10)},
		{2, "", nil, 2.5, nil, []byte{3}, "tenant-a", 2, time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), "dup", nil, int64(20), nil, time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), int64(20), int64(10)},
		{3, "alice", 200, nil, "present", []byte{4}, "tenant-b", 1, time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC), "x", int64(30), int64(30), int64(20), time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC), nil, int64(21), int64(10)},
		{4, "zed", 10, 3.5, "present", []byte{5}, "tenant-b", 2, time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC), nil, int64(99), nil, int64(40), nil, time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC), int64(10), int64(0)},
	}
	for _, fixture := range fixtures {
		if _, err := db.Exec(query, fixture.id, fixture.name, fixture.age, fixture.score,
			fixture.nullable, fixture.payload, fixture.tenantID, fixture.batchID, fixture.eventAt,
			fixture.orderID, fixture.customerID, fixture.paidCents, fixture.invoiceCents,
			fixture.startAt, fixture.endAt, fixture.actualUnits, fixture.plannedUnits); err != nil {
			t.Fatalf("insert fixture %d: %v", fixture.id, err)
		}
	}
}

func insertCrossColumnFixtures(t *testing.T, db *sql.DB, placeholder, table string) {
	t.Helper()
	argsPlaceholders := make([]string, 9)
	for i := range argsPlaceholders {
		if placeholder == "?" {
			argsPlaceholders[i] = placeholder
			continue
		}
		argsPlaceholders[i] = fmt.Sprintf("%s%d", placeholder, i+1)
	}
	query := fmt.Sprintf(
		"INSERT INTO %s (id, paid_cents, invoice_cents, start_at, end_at, actual_units, planned_units, label, amount) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)",
		table,
		argsPlaceholders[0], argsPlaceholders[1], argsPlaceholders[2], argsPlaceholders[3],
		argsPlaceholders[4], argsPlaceholders[5], argsPlaceholders[6], argsPlaceholders[7],
		argsPlaceholders[8],
	)
	overflowPlanned := int64(math.MaxInt64)/2 + 1
	fixtures := []struct {
		id           int64
		paidCents    any
		invoiceCents any
		startAt      any
		endAt        any
		actualUnits  any
		plannedUnits any
		label        any
		amount       any
	}{
		// 1: numeric equal, start < end, ratio*2 pass
		{1, int64(10), int64(10), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), int64(20), int64(10), nil, nil},
		// 2: numeric greater, temporal equal, ratio*-2 pass
		{2, int64(20), int64(10), time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), int64(-20), int64(10), nil, nil},
		// 3: numeric less, start > end, ratio*0 pass
		{3, int64(5), int64(10), time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), int64(0), int64(5), nil, nil},
		// 4: both-NULL numeric and temporal, non-matching ratio
		{4, nil, nil, nil, nil, int64(21), int64(10), nil, nil},
		// 5: right/end NULL and zero denominator
		{5, int64(10), nil, time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC), nil, int64(10), int64(0), nil, nil},
		// 6: left/start NULL, ratio*2 pass
		{6, nil, int64(40), nil, time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC), int64(40), int64(20), nil, nil},
		// 7: overflow planned units and incompatible mixed-type pair (scoped in tests)
		{7, int64(0), int64(0), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), int64(0), overflowPlanned, "abc", int64(1)},
	}
	for _, fixture := range fixtures {
		if _, err := db.Exec(query, fixture.id, fixture.paidCents, fixture.invoiceCents,
			fixture.startAt, fixture.endAt, fixture.actualUnits, fixture.plannedUnits,
			fixture.label, fixture.amount); err != nil {
			t.Fatalf("insert cross_column fixture %d: %v", fixture.id, err)
		}
	}
}

func insertTemporalFixtures(t *testing.T, db *sql.DB, placeholder, table string) {
	t.Helper()
	argsPlaceholders := make([]string, 4)
	for i := range argsPlaceholders {
		if placeholder == "?" {
			argsPlaceholders[i] = placeholder
			continue
		}
		argsPlaceholders[i] = fmt.Sprintf("%s%d", placeholder, i+1)
	}
	query := fmt.Sprintf(
		"INSERT INTO %s (id, tenant_id, event_at, ingested_at) VALUES (%s, %s, %s, %s)",
		table,
		argsPlaceholders[0], argsPlaceholders[1], argsPlaceholders[2], argsPlaceholders[3],
	)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	cutoff := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	fractional := start.Add(123456 * time.Microsecond)
	// Use microsecond boundaries, the precision supported by MySQL DATETIME(6).
	fixtures := []struct {
		id         int64
		tenantID   string
		eventAt    any
		ingestedAt any
	}{
		{1, "tenant-a", start, cutoff},
		{2, "tenant-a", start.Add(12 * time.Hour), end},
		{3, "tenant-a", end, cutoff.Add(-time.Hour)},
		{4, "tenant-a", start.Add(-time.Microsecond), cutoff},
		{5, "tenant-a", end.Add(time.Microsecond), cutoff},
		{6, "tenant-a", nil, nil},
		{7, "tenant-b", start.Add(time.Hour), cutoff},
		{8, "tenant-b", end, cutoff},
		{9, "tenant-c", fractional, cutoff},
	}
	for _, fixture := range fixtures {
		if _, err := db.Exec(query, fixture.id, fixture.tenantID, fixture.eventAt, fixture.ingestedAt); err != nil {
			t.Fatalf("insert temporal fixture %d: %v", fixture.id, err)
		}
	}
}

func setupSQLiteReconcile(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, query := range []string{
		`CREATE TABLE reconcile_left (id INTEGER PRIMARY KEY, tenant_id TEXT NOT NULL)`,
		`CREATE TABLE reconcile_right (id INTEGER PRIMARY KEY, tenant_id TEXT NOT NULL, status TEXT NOT NULL)`,
		`CREATE TABLE empty_reconcile_left (id INTEGER PRIMARY KEY, tenant_id TEXT NOT NULL)`,
		`CREATE TABLE empty_reconcile_right (id INTEGER PRIMARY KEY, tenant_id TEXT NOT NULL, status TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("SQLite reconcile schema: %v", err)
		}
	}
	insertReconcileFixtures(t, db, "?", "reconcile_left", "reconcile_right")
}

func setupDuckDBReconcile(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, query := range []string{
		`DROP TABLE IF EXISTS reconcile_left`,
		`DROP TABLE IF EXISTS reconcile_right`,
		`DROP TABLE IF EXISTS empty_reconcile_left`,
		`DROP TABLE IF EXISTS empty_reconcile_right`,
		`CREATE TABLE reconcile_left (id BIGINT PRIMARY KEY, tenant_id VARCHAR NOT NULL)`,
		`CREATE TABLE reconcile_right (id BIGINT PRIMARY KEY, tenant_id VARCHAR NOT NULL, status VARCHAR NOT NULL)`,
		`CREATE TABLE empty_reconcile_left (id BIGINT PRIMARY KEY, tenant_id VARCHAR NOT NULL)`,
		`CREATE TABLE empty_reconcile_right (id BIGINT PRIMARY KEY, tenant_id VARCHAR NOT NULL, status VARCHAR NOT NULL)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("DuckDB reconcile schema: %v", err)
		}
	}
	insertReconcileFixtures(t, db, "$", "reconcile_left", "reconcile_right")
}

func setupPostgresReconcile(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DROP TABLE IF EXISTS public.reconcile_left, public.reconcile_right, public.empty_reconcile_left, public.empty_reconcile_right`); err != nil {
		t.Fatalf("PostgreSQL reconcile cleanup: %v", err)
	}
	for _, query := range []string{
		`CREATE TABLE public.reconcile_left (id BIGINT PRIMARY KEY, tenant_id TEXT NOT NULL)`,
		`CREATE TABLE public.reconcile_right (id BIGINT PRIMARY KEY, tenant_id TEXT NOT NULL, status TEXT NOT NULL)`,
		`CREATE TABLE public.empty_reconcile_left (id BIGINT PRIMARY KEY, tenant_id TEXT NOT NULL)`,
		`CREATE TABLE public.empty_reconcile_right (id BIGINT PRIMARY KEY, tenant_id TEXT NOT NULL, status TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("PostgreSQL reconcile schema: %v", err)
		}
	}
	insertReconcileFixtures(t, db, "$", "public.reconcile_left", "public.reconcile_right")
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS public.reconcile_left, public.reconcile_right, public.empty_reconcile_left, public.empty_reconcile_right`)
	})
}

func setupMySQLReconcile(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, query := range []string{
		`DROP TABLE IF EXISTS reconcile_left`,
		`DROP TABLE IF EXISTS reconcile_right`,
		`DROP TABLE IF EXISTS empty_reconcile_left`,
		`DROP TABLE IF EXISTS empty_reconcile_right`,
		`CREATE TABLE reconcile_left (id BIGINT PRIMARY KEY, tenant_id VARCHAR(255) NOT NULL) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE reconcile_right (id BIGINT PRIMARY KEY, tenant_id VARCHAR(255) NOT NULL, status VARCHAR(32) NOT NULL) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE empty_reconcile_left (id BIGINT PRIMARY KEY, tenant_id VARCHAR(255) NOT NULL) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE empty_reconcile_right (id BIGINT PRIMARY KEY, tenant_id VARCHAR(255) NOT NULL, status VARCHAR(32) NOT NULL) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("MySQL reconcile schema: %v", err)
		}
	}
	insertReconcileFixtures(t, db, "?", "reconcile_left", "reconcile_right")
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS reconcile_left`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS reconcile_right`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS empty_reconcile_left`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS empty_reconcile_right`)
	})
}

func insertReconcileFixtures(t *testing.T, db *sql.DB, placeholder, leftTable, rightTable string) {
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

	leftRows := []struct {
		id       int64
		tenantID string
	}{
		{1, "t1"},
		{2, "t1"},
		{3, "t2"},
	}
	for _, row := range leftRows {
		ph := placeholderAt(1, 2)
		query := fmt.Sprintf("INSERT INTO %s (id, tenant_id) VALUES (%s, %s)", leftTable, ph[0], ph[1])
		if _, err := db.Exec(query, row.id, row.tenantID); err != nil {
			t.Fatalf("insert reconcile_left %d: %v", row.id, err)
		}
	}

	rightRows := []struct {
		id       int64
		tenantID string
		status   string
	}{
		{10, "t1", "ready"},
		{11, "t1", "ready"},
		{12, "t2", "held"},
	}
	for _, row := range rightRows {
		ph := placeholderAt(1, 3)
		query := fmt.Sprintf(
			"INSERT INTO %s (id, tenant_id, status) VALUES (%s, %s, %s)",
			rightTable, ph[0], ph[1], ph[2],
		)
		if _, err := db.Exec(query, row.id, row.tenantID, row.status); err != nil {
			t.Fatalf("insert reconcile_right %d: %v", row.id, err)
		}
	}
}
