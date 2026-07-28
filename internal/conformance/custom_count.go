package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/busyminds/gxsql"
)

const (
	joinCountTemplate = `SELECT COUNT(*)
FROM {{target}} AS o
JOIN accounts AS a ON a.id = o.account_id
WHERE {{scope}} AND a.status = ?`

	groupByCountTemplate = `SELECT COUNT(*)
FROM (
  SELECT o.account_id
  FROM {{target}} AS o
  WHERE {{scope}}
  GROUP BY o.account_id
  HAVING COUNT(*) > ?
) AS violating_groups`
)

// CustomCountConfig supplies the portable custom-count fixture to RunCustomCount.
// Table must expose id, account_id, batch_id, and tenant_id. The accounts table
// is created and populated by each integration setup helper.
type CustomCountConfig struct {
	DB      gxsql.DB
	Dialect gxsql.Dialect
	Table   gxsql.TableRef
}

// RunCustomCount exercises the published join and GROUP BY/HAVING custom-count
// workflows against a real engine fixture.
func RunCustomCount(t *testing.T, cfg CustomCountConfig) {
	t.Helper()
	if cfg.DB == nil {
		t.Fatal("conformance: DB is required")
	}
	if cfg.Dialect == nil {
		t.Fatal("conformance: dialect is required")
	}

	t.Run("join count unscoped", func(t *testing.T) {
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.CustomCount(
				"inactive account orders",
				gxsql.TrustedCountQuery(joinCountTemplate, "inactive"),
			),
		).ValidateTable(context.Background(), db, cfg.Table, gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		if len(db.queries) != 1 {
			t.Fatalf("queries = %d, want 1", len(db.queries))
		}
		assertCustomCountArgs(t, db.queries[0], nil, []any{"inactive"})
		assertUnscopedCustomCountSQL(t, db.queries[0].text)
		if len(report.Results) != 1 {
			t.Fatalf("results len = %d, want 1", len(report.Results))
		}
		res := report.Results[0]
		if res.FailedCount != 1 || res.Success {
			t.Fatalf("result = %#v, want failed=1 and Success=false", res)
		}
		if res.RowDenominator != gxsql.RowDenominatorUnavailable || res.Total != 0 || res.FailedPercent != 0 {
			t.Fatalf("denominator semantics = %#v, want unavailable counts only", res)
		}
	})

	t.Run("join count scoped", func(t *testing.T) {
		scope := gxsql.TrustedScope("tenant-a", "o.tenant_id = ?", "tenant-a")
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.CustomCount(
				"inactive account orders in tenant",
				gxsql.TrustedCountQuery(joinCountTemplate, "inactive"),
			),
		).ValidateTable(context.Background(), db, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(scope))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		if len(db.queries) != 1 {
			t.Fatalf("queries = %d, want 1", len(db.queries))
		}
		assertCustomCountArgs(t, db.queries[0], []any{"tenant-a"}, []any{"inactive"})
		assertScopedCustomCountSQL(t, db.queries[0].text, "o.tenant_id")
		if report.ScopeID != "tenant-a" {
			t.Fatalf("scope id = %q, want tenant-a", report.ScopeID)
		}
		res := report.Results[0]
		if res.FailedCount != 1 || res.Success {
			t.Fatalf("result = %#v, want failed=1 and Success=false", res)
		}
	})

	t.Run("group by having unscoped", func(t *testing.T) {
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.CustomCount(
				"accounts with multiple order lines",
				gxsql.TrustedCountQuery(groupByCountTemplate, int64(1)),
			),
		).ValidateTable(context.Background(), db, cfg.Table, gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		if len(db.queries) != 1 {
			t.Fatalf("queries = %d, want 1", len(db.queries))
		}
		assertCustomCountArgs(t, db.queries[0], nil, []any{int64(1)})
		assertUnscopedCustomCountSQL(t, db.queries[0].text)
		res := report.Results[0]
		if res.FailedCount != 2 || res.Success {
			t.Fatalf("result = %#v, want failed=2 and Success=false", res)
		}
	})

	t.Run("group by having scoped", func(t *testing.T) {
		scope := gxsql.TrustedScope("batch-1", "o.batch_id = ?", int64(1))
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.CustomCount(
				"accounts with multiple order lines in batch",
				gxsql.TrustedCountQuery(groupByCountTemplate, int64(1)),
			),
		).ValidateTable(context.Background(), db, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(scope))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		if len(db.queries) != 1 {
			t.Fatalf("queries = %d, want 1", len(db.queries))
		}
		assertCustomCountArgs(t, db.queries[0], []any{int64(1)}, []any{int64(1)})
		assertScopedCustomCountSQL(t, db.queries[0].text, "o.batch_id")
		if report.ScopeID != "batch-1" {
			t.Fatalf("scope id = %q, want batch-1", report.ScopeID)
		}
		res := report.Results[0]
		if res.FailedCount != 1 || res.Success {
			t.Fatalf("result = %#v, want failed=1 and Success=false", res)
		}
	})

	t.Run("invalid template preflight before SQL", func(t *testing.T) {
		db := &recordingDB{DB: cfg.DB}
		_, err := gxsql.NewSuite(
			gxsql.CustomCount(
				"invalid template",
				gxsql.TrustedCountQuery("SELECT COUNT(*) FROM {{target}} WHERE {{target}} AND {{scope}}", nil),
			),
		).ValidateTable(context.Background(), db, cfg.Table, gxsql.WithDialect(cfg.Dialect))
		if err == nil || !errors.Is(err, gxsql.ErrCategoryInvalidConfig) {
			t.Fatalf("error = %v, want invalid_config preflight", err)
		}
		if len(db.queries) != 0 {
			t.Fatalf("queries = %d, want 0 before preflight failure", len(db.queries))
		}
	})

	t.Run("export privacy", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.CustomCount(
				"inactive account orders",
				gxsql.TrustedCountQuery(joinCountTemplate, "inactive"),
			),
		).ValidateTable(context.Background(), cfg.DB, cfg.Table, gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		dto, err := gxsql.ExportReport(report)
		if err != nil {
			t.Fatalf("ExportReport: %v", err)
		}
		raw, err := json.Marshal(dto)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		payload := strings.ToLower(string(raw))
		for _, forbidden := range []string{"select ", "join ", "{{target}}", "{{scope}}"} {
			if strings.Contains(payload, forbidden) {
				t.Fatalf("export leaked %q: %s", forbidden, string(raw))
			}
		}
	})
}

func assertCustomCountArgs(t *testing.T, record recordedQuery, scopeArgs, customArgs []any) {
	t.Helper()
	wantLen := len(scopeArgs) + len(customArgs)
	if len(record.args) != wantLen {
		t.Fatalf("args = %#v, want scope %#v then custom %#v", record.args, scopeArgs, customArgs)
	}
	for i, want := range scopeArgs {
		if !equalScopeValue(record.args[i], want) {
			t.Fatalf("scope arg[%d] = %#v, want %#v", i, record.args[i], want)
		}
	}
	offset := len(scopeArgs)
	for i, want := range customArgs {
		if !equalScopeValue(record.args[offset+i], want) {
			t.Fatalf("custom arg[%d] = %#v, want %#v", i, record.args[offset+i], want)
		}
	}
}

func assertUnscopedCustomCountSQL(t *testing.T, query string) {
	t.Helper()
	upper := strings.ToUpper(query)
	if !strings.Contains(upper, " TRUE") && !strings.Contains(upper, "(TRUE)") {
		t.Fatalf("unscoped query = %q, want rendered TRUE scope", query)
	}
}

func assertScopedCustomCountSQL(t *testing.T, query, scopeColumn string) {
	t.Helper()
	lower := strings.ToLower(query)
	if !strings.Contains(lower, "where") || !strings.Contains(lower, strings.ToLower(scopeColumn)) {
		t.Fatalf("scoped query = %q, want WHERE with %s", query, scopeColumn)
	}
}
