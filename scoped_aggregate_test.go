package gxsql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func TestAggregatesScopeObservedValuesAndVerdicts(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "amount": float64(10)},
		map[string]any{"id": int64(2), "amount": float64(20)},
		map[string]any{"id": int64(3), "tenant_id": "t2", "amount": float64(100)},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")

	tests := []struct {
		name string
		exp  Expectation
		want float64
	}{
		{name: "average", exp: Float("amount").AverageBetween(14, 16), want: 15},
		{name: "minimum", exp: Float("amount").MinGreaterOrEqual(10), want: 10},
		{name: "maximum", exp: Float("amount").MaxLessOrEqual(20), want: 20},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openRecordingHarnessDB(t)
			res := evalTableLevelWithScope(t, db, tc.exp, scope)
			if !res.Success {
				t.Fatalf("expected scoped aggregate pass, got %#v", res)
			}
			if res.Facts.ObservedFloat == nil || *res.Facts.ObservedFloat != tc.want {
				t.Fatalf("ObservedFloat = %v, want %g", res.Facts.ObservedFloat, tc.want)
			}
			if !strings.Contains(res.Name, fmt.Sprintf("got %g", tc.want)) {
				t.Fatalf("Name = %q, want observed value", res.Name)
			}
			assertTableLevelDenominatorUnavailable(t, res)
			if len(db.queries) != 1 {
				t.Fatalf("queries = %d, want one aggregate query", len(db.queries))
			}
			assertScopeQuery(t, db.queries[0], scope, false)
		})
	}
}

func TestAggregatesScopeExecutesOnBuiltInDialects(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "amount": float64(10)},
		map[string]any{"id": int64(2), "amount": float64(20)},
		map[string]any{"id": int64(3), "tenant_id": "t2", "amount": float64(100)},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")

	tests := []struct {
		name    string
		dialect Dialect
	}{
		{name: "postgres", dialect: Postgres()},
		{name: "sqlite", dialect: SQLite()},
		{name: "duckdb", dialect: DuckDB()},
		{name: "mysql", dialect: MySQL()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openRecordingHarnessDB(t)
			res := evalTableLevelWithScope(t, db, Float("amount").AverageBetween(14, 16), scope, func(opts *evalOptions) {
				opts.dialect = tc.dialect
			})
			if !res.Success || res.Facts.ObservedFloat == nil || *res.Facts.ObservedFloat != 15 {
				t.Fatalf("result = %#v, want scoped average 15", res)
			}
			if len(db.queries) != 1 {
				t.Fatalf("queries = %d, want one aggregate query", len(db.queries))
			}
			if !strings.Contains(db.queries[0].text, " WHERE (tenant_id = ") {
				t.Fatalf("query = %q, want scoped WHERE", db.queries[0].text)
			}
			if len(db.queries[0].args) != 1 || db.queries[0].args[0] != "t1" {
				t.Fatalf("args = %#v, want [t1]", db.queries[0].args)
			}
		})
	}
}

func TestAggregatesScopeAllNullAndEmptyAreVacuous(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "amount": nil},
		map[string]any{"id": int64(2), "tenant_id": "t2", "amount": float64(100)},
	))

	tests := []struct {
		name  string
		scope trustedScope
	}{
		{name: "all null", scope: mustTestScope(t, "tenant_id = ?", "t1")},
		{name: "empty scope", scope: mustTestScope(t, "tenant_id = ?", "nobody")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, exp := range []Expectation{
				Float("amount").AverageBetween(1, 100),
				Float("amount").MinGreaterOrEqual(0),
				Float("amount").MaxLessOrEqual(100),
			} {
				db := openRecordingHarnessDB(t)
				res := evalTableLevelWithScope(t, db, exp, tc.scope)
				if !res.Success {
					t.Fatalf("expected vacuous scoped pass, got %#v", res)
				}
				if res.Facts.ObservedFloat != nil {
					t.Fatalf("ObservedFloat = %v, want nil", res.Facts.ObservedFloat)
				}
				if strings.Contains(res.Name, "got ") {
					t.Fatalf("Name = %q, want no observed suffix", res.Name)
				}
				assertTableLevelDenominatorUnavailable(t, res)
				if len(db.queries) != 1 {
					t.Fatalf("queries = %d, want one aggregate query", len(db.queries))
				}
				assertScopeQuery(t, db.queries[0], tc.scope, false)
			}
		})
	}
}

type aggregateQueryCapture struct {
	DB
	query string
	args  []any
}

func (c *aggregateQueryCapture) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	c.query = query
	c.args = append([]any(nil), args...)
	return c.DB.QueryContext(ctx, query, args...)
}

func (c *aggregateQueryCapture) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.DB.QueryRowContext(ctx, query, args...)
}

func TestAggregateScopePlaceholderRendering(t *testing.T) {
	tests := []struct {
		name        string
		dialect     Dialect
		placeholder string
	}{
		{name: "postgres", dialect: Postgres(), placeholder: "$1"},
		{name: "duckdb", dialect: DuckDB(), placeholder: "$1"},
		{name: "mysql", dialect: MySQL(), placeholder: "?"},
		{name: "sqlite", dialect: SQLite(), placeholder: "?"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scope := mustTestScope(t, "tenant_id = ? OR region = ?", "t1", "west")
			base := openErrorDB(t)
			capture := &aggregateQueryCapture{DB: base}
			_, _, _, args, err := queryAggregateFloatWithArgs(
				context.Background(), capture, Table("users"), evalOptions{dialect: tc.dialect, scope: &scope}, "amount", "AVG",
			)
			if err == nil {
				t.Fatal("expected database error from error DB")
			}
			if len(args) != 2 || args[0] != "t1" || args[1] != "west" {
				t.Fatalf("returned args = %#v, want scope args first", args)
			}
			if len(capture.args) != 2 || capture.args[0] != "t1" || capture.args[1] != "west" {
				t.Fatalf("query args = %#v, want scope args first", capture.args)
			}
			if !strings.Contains(capture.query, " WHERE (") {
				t.Fatalf("query = %q, want independently parenthesized scope WHERE", capture.query)
			}
			if !strings.Contains(capture.query, tc.placeholder) {
				t.Fatalf("query = %q, want placeholder %q", capture.query, tc.placeholder)
			}
			if tc.placeholder == "?" && strings.Count(capture.query, "?") != 2 {
				t.Fatalf("query = %q, want two neutral placeholders", capture.query)
			}
		})
	}
}
