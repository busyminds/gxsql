package gxsql

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

// These benchmarks measure scoped-total query behavior using the in-memory
// fake SQL harness in harness_test.go. Results reflect harness execution only
// and must not be interpreted as universal production speedups on real
// PostgreSQL or SQLite deployments.

const scopedTotalExpectationCount = 6

func benchmarkScopedTotalHarnessData() map[string][]map[string]any {
	const rowCount = 64
	rows := make([]map[string]any, 0, rowCount)
	for i := range rowCount {
		row := map[string]any{
			"id":           int64(i + 1),
			"age":          int64(20 + (i % 40)),
			"score":        int64(50 + (i % 30)),
			"name":         fmt.Sprintf("user-%d", i),
			"email":        fmt.Sprintf("user%d@example.com", i),
			"years_active": int64(i % 10),
			"status":       "active",
		}
		if i%8 == 7 {
			row["tenant_id"] = "t2"
		}
		rows = append(rows, row)
	}
	return scopedHarnessUsers("tenant_id", "t1", rows...)
}

func benchmarkScopedTotalExpectations() []Expectation {
	return []Expectation{
		Int("age").Between(0, 120),
		Int("score").Between(0, 100),
		Column("name").NotNull(),
		Column("email").NotNull(),
		Int("years_active").Between(0, 50),
		Column("status").NotNull(),
	}
}

func setupBenchmarkHarness(b *testing.B, tables map[string][]map[string]any) *sql.DB {
	b.Helper()
	harnessMu.Lock()
	harnessTables = tables
	harnessMu.Unlock()
	b.Cleanup(func() {
		harnessMu.Lock()
		harnessTables = nil
		harnessMu.Unlock()
	})

	db, err := sql.Open(fakeDriverName, "benchmark")
	if err != nil {
		b.Fatalf("sql.Open: %v", err)
	}
	b.Cleanup(func() { _ = db.Close() })
	return db
}

func mustBenchmarkScope(b *testing.B) Scope {
	b.Helper()
	scope, err := newTrustedScope("benchmark-scope", "tenant_id = ?", []any{"t1"})
	if err != nil {
		b.Fatalf("newTrustedScope: %v", err)
	}
	return scope
}

func benchmarkScopedTotalDialects(b *testing.B, fn func(b *testing.B, dialect Dialect, db DB, scope Scope)) {
	b.Helper()
	for _, tc := range []struct {
		name    string
		dialect Dialect
	}{
		{"Postgres", Postgres()},
		{"SQLite", SQLite()},
	} {
		b.Run(tc.name, func(b *testing.B) {
			db := setupBenchmarkHarness(b, benchmarkScopedTotalHarnessData())
			scope := mustBenchmarkScope(b)
			fn(b, tc.dialect, db, scope)
		})
	}
}

// BenchmarkScopedTotalNoReuse measures N separate ValidateTable calls, each with
// one denominator-using expectation. Every call issues its own scoped total
// query on the fake harness.
func BenchmarkScopedTotalNoReuse(b *testing.B) {
	benchmarkScopedTotalDialects(b, func(b *testing.B, dialect Dialect, db DB, scope Scope) {
		exps := benchmarkScopedTotalExpectations()
		if len(exps) != scopedTotalExpectationCount {
			b.Fatalf("expectations = %d, want %d", len(exps), scopedTotalExpectationCount)
		}
		ctx := context.Background()
		table := Table("users")
		opts := []Option{
			WithDialect(dialect),
			WithScope(scope),
			SummaryOnly(),
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, exp := range exps {
				suite := NewSuite(exp)
				if _, err := suite.ValidateTable(ctx, db, table, opts...); err != nil {
					b.Fatalf("ValidateTable: %v", err)
				}
			}
		}
	})
}

// BenchmarkScopedTotalReuse measures one ValidateTable call with N
// denominator-using expectations that share a single scoped total query on the
// fake harness.
func BenchmarkScopedTotalReuse(b *testing.B) {
	benchmarkScopedTotalDialects(b, func(b *testing.B, dialect Dialect, db DB, scope Scope) {
		exps := benchmarkScopedTotalExpectations()
		if len(exps) != scopedTotalExpectationCount {
			b.Fatalf("expectations = %d, want %d", len(exps), scopedTotalExpectationCount)
		}
		suite := NewSuite(exps...)
		ctx := context.Background()
		table := Table("users")
		opts := []Option{
			WithDialect(dialect),
			WithScope(scope),
			SummaryOnly(),
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := suite.ValidateTable(ctx, db, table, opts...); err != nil {
				b.Fatalf("ValidateTable: %v", err)
			}
		}
	})
}
