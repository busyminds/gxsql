package conformance

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/busyminds/gxsql"
)

func runRelationalKeyIntegrity(t *testing.T, cfg Config) {
	t.Helper()
	if cfg.ParentTable.Name == "" {
		t.Fatal("conformance: ParentTable is required for relational key integrity")
	}

	t.Run("composite unique counts every duplicate row", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.Columns("tenant_id", "order_id").Unique(),
		).ValidateTable(context.Background(), cfg.DB, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithKey("id"), gxsql.WithFailedKeysCap(0))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := report.Results[0]
		if res.Kind != gxsql.KindCompositeUnique {
			t.Fatalf("Kind = %q, want %q", res.Kind, gxsql.KindCompositeUnique)
		}
		if res.Column != "" {
			t.Fatalf("Column = %q, want blank", res.Column)
		}
		if !reflect.DeepEqual(res.Facts.KeyColumns, []string{"tenant_id", "order_id"}) {
			t.Fatalf("KeyColumns = %#v", res.Facts.KeyColumns)
		}
		if res.Success || res.Total != 4 || res.FailedCount != 2 {
			t.Fatalf("result = %#v, want total=4 failed=2 (duplicate rows, not groups)", res)
		}
		if len(res.FailedKeys) != 2 {
			t.Fatalf("FailedKeys = %#v, want both duplicate row ids", res.FailedKeys)
		}
	})

	t.Run("composite unique ignores NULL-containing tuples", func(t *testing.T) {
		// Fixture row 4 has order_id NULL; it must not participate. The only
		// participating duplicate group is (tenant-a, dup) on rows 1 and 2.
		report, err := gxsql.NewSuite(
			gxsql.Columns("tenant_id", "order_id").Unique(),
		).ValidateTable(context.Background(), cfg.DB, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithSampleCap(10))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := report.Results[0]
		if res.FailedCount != 2 {
			t.Fatalf("FailedCount = %d, want 2; NULL order_id rows must be ignored", res.FailedCount)
		}
		for _, sample := range res.SampleValues {
			if sample == nil {
				t.Fatalf("SampleValues = %#v, want only non-NULL participating duplicates", res.SampleValues)
			}
		}
	})

	t.Run("composite unique respects local scope", func(t *testing.T) {
		scope := gxsql.TrustedScope("tenant-a", "tenant_id = ?", "tenant-a")
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.Columns("tenant_id", "order_id").Unique(),
		).ValidateTable(context.Background(), db, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(scope), gxsql.WithKey("id"))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertScopedQueries(t, db, "tenant_id", false, "tenant-a")
		res := report.Results[0]
		if res.Total != 2 || res.FailedCount != 2 || res.Success {
			t.Fatalf("scoped composite unique = %#v, want total=2 failed=2", res)
		}
	})

	t.Run("reference orphans and nullable local policy", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.Columns("tenant_id", "customer_id").References(cfg.ParentTable, "tenant_id", "id"),
		).ValidateTable(context.Background(), cfg.DB, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithKey("id"), gxsql.WithSampleCap(10),
			gxsql.WithFailedKeysCap(0))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := report.Results[0]
		if res.Kind != gxsql.KindReference {
			t.Fatalf("Kind = %q, want %q", res.Kind, gxsql.KindReference)
		}
		if res.Column != "" {
			t.Fatalf("Column = %q, want blank", res.Column)
		}
		if res.Facts.Reference == nil {
			t.Fatal("Facts.Reference is nil")
		}
		ref := res.Facts.Reference
		if !reflect.DeepEqual(ref.LocalColumns, []string{"tenant_id", "customer_id"}) ||
			!reflect.DeepEqual(ref.ParentColumns, []string{"tenant_id", "id"}) ||
			ref.Parent != cfg.ParentTable {
			t.Fatalf("Reference facts = %#v, want parent %#v", ref, cfg.ParentTable)
		}
		// Row 2 has NULL customer_id (passes). Row 4 is the only orphan.
		if res.Success || res.Total != 4 || res.FailedCount != 1 {
			t.Fatalf("reference result = %#v, want total=4 failed=1 orphan", res)
		}
		if len(res.FailedKeys) != 1 || len(res.FailedKeys[0]) != 1 || res.FailedKeys[0][0] != int64(4) {
			t.Fatalf("FailedKeys = %#v, want local orphan id 4", res.FailedKeys)
		}
	})

	t.Run("reference local scope with unscoped parent lookup", func(t *testing.T) {
		scope := gxsql.TrustedScope("tenant-a", "tenant_id = ?", "tenant-a")
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.Columns("tenant_id", "customer_id").References(cfg.ParentTable, "tenant_id", "id"),
		).ValidateTable(context.Background(), db, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(scope), gxsql.WithKey("id"),
			gxsql.CaptureQueryDiagnostics())
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertScopedQueries(t, db, "tenant_id", false, "tenant-a")
		res := report.Results[0]
		// Scoped population is rows 1-2: matching parent and NULL local key.
		if !res.Success || res.Total != 2 || res.FailedCount != 0 {
			t.Fatalf("scoped reference = %#v, want clean pass over local rows only", res)
		}
		// Parent row for tenant-b must remain visible to unscoped lookup; prove
		// out-of-scope local orphans are excluded by checking unscoped still fails.
		unscoped, err := gxsql.NewSuite(
			gxsql.Columns("tenant_id", "customer_id").References(cfg.ParentTable, "tenant_id", "id"),
		).ValidateTable(context.Background(), cfg.DB, cfg.Table,
			gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("unscoped ValidateTable: %v", err)
		}
		if unscoped.Results[0].FailedCount != 1 {
			t.Fatalf("unscoped FailedCount = %d, want 1", unscoped.Results[0].FailedCount)
		}
	})

	t.Run("schema-qualified parent rendering and local-only diagnostics", func(t *testing.T) {
		if cfg.ParentTable.Schema == "" {
			t.Fatal("ParentTable.Schema is required to exercise schema-qualified rendering")
		}
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.Columns("tenant_id", "customer_id").References(cfg.ParentTable, "tenant_id", "id"),
		).ValidateTable(context.Background(), db, cfg.Table,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithKey("id"), gxsql.WithSampleCap(10),
			gxsql.CaptureQueryDiagnostics())
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := report.Results[0]
		if res.FailedCount != 1 {
			t.Fatalf("FailedCount = %d, want 1", res.FailedCount)
		}
		exported, err := gxsql.ExportReport(report, gxsql.IncludeCapturedDiagnostics(), gxsql.IncludeSamples(), gxsql.IncludeFailedKeys())
		if err != nil {
			t.Fatalf("ExportReport: %v", err)
		}
		diag := exported.Results[0].Diagnostics
		if diag == nil || diag.Query == "" {
			t.Fatalf("diagnostics = %#v, want captured SQL", diag)
		}
		query := strings.ToLower(diag.Query)
		schema := strings.ToLower(cfg.ParentTable.Schema)
		parent := strings.ToLower(cfg.ParentTable.Name)
		if !strings.Contains(query, schema) || !strings.Contains(query, parent) {
			t.Fatalf("diagnostic SQL %q, want schema-qualified parent %s.%s", diag.Query, cfg.ParentTable.Schema, cfg.ParentTable.Name)
		}
		// Parent-only identity (tenant-c) must never appear in local diagnostics.
		for _, sample := range res.SampleValues {
			if sample == "tenant-c" {
				t.Fatalf("SampleValues = %#v includes parent-only value", res.SampleValues)
			}
		}
		if len(res.FailedKeys) != 1 || len(res.FailedKeys[0]) != 1 || res.FailedKeys[0][0] != int64(4) {
			t.Fatalf("FailedKeys = %#v, want only local orphan id 4", res.FailedKeys)
		}
		for _, key := range res.FailedKeys {
			for _, part := range key {
				if part == "tenant-c" {
					t.Fatalf("FailedKeys = %#v includes parent-only value", res.FailedKeys)
				}
			}
		}
	})

	t.Run("single-column reference still uses structured facts", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.Column("customer_id").References(cfg.ParentTable, "id"),
		).ValidateTable(context.Background(), cfg.DB, cfg.Table,
			gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := report.Results[0]
		if res.Kind != gxsql.KindReference || res.Column != "" || res.Facts.Reference == nil {
			t.Fatalf("single-column reference = %#v", res)
		}
		if !reflect.DeepEqual(res.Facts.Reference.LocalColumns, []string{"customer_id"}) ||
			!reflect.DeepEqual(res.Facts.Reference.ParentColumns, []string{"id"}) {
			t.Fatalf("Reference facts = %#v", res.Facts.Reference)
		}
		if res.Success || res.Total != 4 || res.FailedCount != 1 {
			t.Fatalf("single-column reference = %#v, want total=4 failed=1", res)
		}
	})
}
