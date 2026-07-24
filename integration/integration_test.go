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
		Transaction: transactionFactory(db),
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
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER, score REAL, nullable TEXT, payload BLOB, tenant_id TEXT, batch_id INTEGER, event_at TIMESTAMP)`,
		`CREATE TABLE empty_users (id INTEGER PRIMARY KEY, name TEXT, age INTEGER, score REAL, nullable TEXT, payload BLOB, tenant_id TEXT, batch_id INTEGER, event_at TIMESTAMP)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("SQLite schema: %v", err)
		}
	}
	insertFixtures(t, db, "?", "users")
}

func setupDuckDB(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DROP TABLE IF EXISTS users`); err != nil {
		t.Fatalf("DuckDB cleanup users: %v", err)
	}
	if _, err := db.Exec(`DROP TABLE IF EXISTS empty_users`); err != nil {
		t.Fatalf("DuckDB cleanup empty_users: %v", err)
	}
	for _, query := range []string{
		`CREATE TABLE users (id BIGINT PRIMARY KEY, name VARCHAR, age INTEGER, score DOUBLE, nullable VARCHAR, payload BLOB, tenant_id VARCHAR, batch_id BIGINT, event_at TIMESTAMP)`,
		`CREATE TABLE empty_users (id BIGINT PRIMARY KEY, name VARCHAR, age INTEGER, score DOUBLE, nullable VARCHAR, payload BLOB, tenant_id VARCHAR, batch_id BIGINT, event_at TIMESTAMP)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("DuckDB schema: %v", err)
		}
	}
	insertFixtures(t, db, "$", "users")
}

func setupPostgres(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DROP TABLE IF EXISTS public.users, public.empty_users`); err != nil {
		t.Fatalf("PostgreSQL cleanup: %v", err)
	}
	for _, query := range []string{
		`CREATE TABLE public.users (id BIGINT PRIMARY KEY, name TEXT, age INTEGER, score DOUBLE PRECISION, nullable TEXT, payload BYTEA, tenant_id TEXT, batch_id BIGINT, event_at TIMESTAMP WITH TIME ZONE)`,
		`CREATE TABLE public.empty_users (id BIGINT PRIMARY KEY, name TEXT, age INTEGER, score DOUBLE PRECISION, nullable TEXT, payload BYTEA, tenant_id TEXT, batch_id BIGINT, event_at TIMESTAMP WITH TIME ZONE)`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("PostgreSQL schema: %v", err)
		}
	}
	insertFixtures(t, db, "$", "public.users")
	t.Cleanup(func() { _, _ = db.Exec(`DROP TABLE IF EXISTS public.users, public.empty_users`) })
}
func setupMySQL(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, query := range []string{
		`DROP TABLE IF EXISTS users`,
		`DROP TABLE IF EXISTS empty_users`,
		`DROP TABLE IF EXISTS utf8_char_length`,
		`CREATE TABLE users (id BIGINT PRIMARY KEY, name VARCHAR(255), age INTEGER, score DOUBLE, nullable TEXT, payload BLOB, tenant_id VARCHAR(255), batch_id BIGINT, event_at DATETIME(6)) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE empty_users (id BIGINT PRIMARY KEY, name VARCHAR(255), age INTEGER, score DOUBLE, nullable TEXT, payload BLOB, tenant_id VARCHAR(255), batch_id BIGINT, event_at DATETIME(6)) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
		`CREATE TABLE utf8_char_length (id BIGINT PRIMARY KEY, name VARCHAR(255)) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatalf("MySQL schema: %v", err)
		}
	}
	insertFixtures(t, db, "?", "users")
	if _, err := db.Exec(`INSERT INTO utf8_char_length (id, name) VALUES (?, ?)`, 1, "é"); err != nil {
		t.Fatalf("MySQL utf8_char_length fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP TABLE IF EXISTS users`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS empty_users`)
		_, _ = db.Exec(`DROP TABLE IF EXISTS utf8_char_length`)
	})
}

func insertFixtures(t *testing.T, db *sql.DB, placeholder, table string) {
	t.Helper()
	argsPlaceholders := make([]string, 9)
	for i := range argsPlaceholders {
		if placeholder == "?" {
			argsPlaceholders[i] = placeholder
			continue
		}
		argsPlaceholders[i] = fmt.Sprintf("%s%d", placeholder, i+1)
	}
	query := fmt.Sprintf("INSERT INTO %s (id, name, age, score, nullable, payload, tenant_id, batch_id, event_at) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)",
		table, argsPlaceholders[0], argsPlaceholders[1], argsPlaceholders[2], argsPlaceholders[3], argsPlaceholders[4], argsPlaceholders[5], argsPlaceholders[6], argsPlaceholders[7], argsPlaceholders[8])
	fixtures := []struct {
		id       int64
		name     string
		age      any
		score    any
		nullable any
		payload  []byte
		tenantID string
		batchID  int64
		eventAt  time.Time
	}{
		{1, "alice", 20, 1.5, "present", []byte{1, 2}, "tenant-a", 1, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
		{2, "", nil, 2.5, nil, []byte{3}, "tenant-a", 2, time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)},
		{3, "alice", 200, nil, "present", []byte{4}, "tenant-b", 1, time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC)},
		{4, "zed", 10, 3.5, "present", []byte{5}, "tenant-b", 2, time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC)},
	}
	for _, fixture := range fixtures {
		if _, err := db.Exec(query, fixture.id, fixture.name, fixture.age, fixture.score,
			fixture.nullable, fixture.payload, fixture.tenantID, fixture.batchID, fixture.eventAt); err != nil {
			t.Fatalf("insert fixture %d: %v", fixture.id, err)
		}
	}
}
