package conformance

import (
	"context"
	"strings"
	"testing"

	"github.com/busyminds/gxsql"
)

// ReconcileConfig supplies the portable count-reconciliation fixture to
// RunReconcile. Left and Right must each expose tenant_id; Right must also
// expose status. EmptyLeft and EmptyRight are zero-row tables with the same
// shapes, used for both-zero and unequal coverage.
type ReconcileConfig struct {
	DB         gxsql.DB
	Dialect    gxsql.Dialect
	Left       gxsql.TableRef
	Right      gxsql.TableRef
	EmptyLeft  gxsql.TableRef
	EmptyRight gxsql.TableRef
}

// RunReconcile exercises suite-bound COUNT(*) reconciliation against a real
// engine fixture, with and without left suite scope and secondary filters.
func RunReconcile(t *testing.T, cfg ReconcileConfig) {
	t.Helper()
	if cfg.DB == nil {
		t.Fatal("conformance: DB is required")
	}
	if cfg.Dialect == nil {
		t.Fatal("conformance: dialect is required")
	}
	if cfg.Left.Name == "" || cfg.Right.Name == "" {
		t.Fatal("conformance: Left and Right are required")
	}
	if cfg.EmptyLeft.Name == "" || cfg.EmptyRight.Name == "" {
		t.Fatal("conformance: EmptyLeft and EmptyRight are required")
	}

	t.Run("equal counts unscoped", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.ReconcileCounts(cfg.Right).Equal(),
		).ValidateTable(context.Background(), cfg.DB, cfg.Left,
			gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := report.Results[0]
		assertReconcileShape(t, res, cfg.Left, cfg.Right)
		if !res.Success || res.FailedCount != 0 {
			t.Fatalf("equal result = %#v, want FailedCount 0", res)
		}
		rf := res.Facts.Reconcile
		if rf.ObservedLeftCount == nil || *rf.ObservedLeftCount != 3 {
			t.Fatalf("ObservedLeftCount = %#v, want 3", rf.ObservedLeftCount)
		}
		if rf.ObservedRightCount == nil || *rf.ObservedRightCount != 3 {
			t.Fatalf("ObservedRightCount = %#v, want 3", rf.ObservedRightCount)
		}
		if rf.LeftScopeID != "" || rf.SecondaryFilterID != "" {
			t.Fatalf("unscoped facts leaked identities: %#v", rf)
		}
	})

	t.Run("unequal counts unscoped", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.ReconcileCounts(cfg.EmptyRight).Equal(),
		).ValidateTable(context.Background(), cfg.DB, cfg.Left,
			gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := report.Results[0]
		assertReconcileShape(t, res, cfg.Left, cfg.EmptyRight)
		if res.Success || res.FailedCount != 1 {
			t.Fatalf("unequal result = %#v, want FailedCount 1", res)
		}
		rf := res.Facts.Reconcile
		if rf.ObservedLeftCount == nil || *rf.ObservedLeftCount != 3 {
			t.Fatalf("ObservedLeftCount = %#v, want 3", rf.ObservedLeftCount)
		}
		if rf.ObservedRightCount == nil || *rf.ObservedRightCount != 0 {
			t.Fatalf("ObservedRightCount = %#v, want 0", rf.ObservedRightCount)
		}
	})

	t.Run("both sides zero succeed", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.ReconcileCounts(cfg.EmptyRight).Equal(),
		).ValidateTable(context.Background(), cfg.DB, cfg.EmptyLeft,
			gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := report.Results[0]
		assertReconcileShape(t, res, cfg.EmptyLeft, cfg.EmptyRight)
		if !res.Success || res.FailedCount != 0 {
			t.Fatalf("both-zero result = %#v, want success", res)
		}
		rf := res.Facts.Reconcile
		if rf.ObservedLeftCount == nil || *rf.ObservedLeftCount != 0 {
			t.Fatalf("ObservedLeftCount = %#v, want 0", rf.ObservedLeftCount)
		}
		if rf.ObservedRightCount == nil || *rf.ObservedRightCount != 0 {
			t.Fatalf("ObservedRightCount = %#v, want 0", rf.ObservedRightCount)
		}
	})

	t.Run("left-only suite scope does not alter secondary", func(t *testing.T) {
		scope := gxsql.TrustedScope("tenant-t1", "tenant_id = ?", "t1")
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.ReconcileCounts(cfg.Right).Equal(),
		).ValidateTable(context.Background(), db, cfg.Left,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(scope),
			gxsql.CaptureQueryDiagnostics())
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := report.Results[0]
		assertReconcileShape(t, res, cfg.Left, cfg.Right)
		// Left scoped to t1 => 2; secondary unscoped => 3.
		if res.Success || res.FailedCount != 1 {
			t.Fatalf("left-scoped result = %#v, want unequal FailedCount 1", res)
		}
		if report.ScopeID != "tenant-t1" {
			t.Fatalf("ScopeID = %q, want tenant-t1", report.ScopeID)
		}
		rf := res.Facts.Reconcile
		if rf.LeftScopeID != "tenant-t1" {
			t.Fatalf("LeftScopeID = %q, want tenant-t1", rf.LeftScopeID)
		}
		if rf.SecondaryFilterID != "" {
			t.Fatalf("SecondaryFilterID = %q, want empty", rf.SecondaryFilterID)
		}
		if rf.ObservedLeftCount == nil || *rf.ObservedLeftCount != 2 {
			t.Fatalf("ObservedLeftCount = %#v, want 2", rf.ObservedLeftCount)
		}
		if rf.ObservedRightCount == nil || *rf.ObservedRightCount != 3 {
			t.Fatalf("ObservedRightCount = %#v, want 3", rf.ObservedRightCount)
		}
		assertSecondaryOmitsSuiteScope(t, db, cfg)
	})

	t.Run("secondary-only filter narrows right side", func(t *testing.T) {
		scope := gxsql.TrustedScope("tenant-t1", "tenant_id = ?", "t1")
		filter := gxsql.TrustedSecondaryFilter("served-ready", "status = ?", "ready")
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.ReconcileCounts(cfg.Right).WithSecondaryFilter(filter).Equal(),
		).ValidateTable(context.Background(), db, cfg.Left,
			gxsql.WithDialect(cfg.Dialect), gxsql.WithScope(scope),
			gxsql.CaptureQueryDiagnostics())
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := report.Results[0]
		assertReconcileShape(t, res, cfg.Left, cfg.Right)
		// Left scoped t1 => 2; secondary status=ready => 2.
		if !res.Success || res.FailedCount != 0 {
			t.Fatalf("filtered result = %#v, want equal success", res)
		}
		rf := res.Facts.Reconcile
		if rf.LeftScopeID != "tenant-t1" || rf.SecondaryFilterID != "served-ready" {
			t.Fatalf("scope/filter ids = %#v", rf)
		}
		if rf.ObservedLeftCount == nil || *rf.ObservedLeftCount != 2 {
			t.Fatalf("ObservedLeftCount = %#v, want 2", rf.ObservedLeftCount)
		}
		if rf.ObservedRightCount == nil || *rf.ObservedRightCount != 2 {
			t.Fatalf("ObservedRightCount = %#v, want 2", rf.ObservedRightCount)
		}
		assertSecondaryFilterOnlyOnRight(t, db, cfg)
	})
}

func assertReconcileShape(t *testing.T, res gxsql.Result, left, right gxsql.TableRef) {
	t.Helper()
	if res.Kind != gxsql.KindReconcileCountsEqual {
		t.Fatalf("Kind = %q, want %q", res.Kind, gxsql.KindReconcileCountsEqual)
	}
	if res.RowDenominator != gxsql.RowDenominatorUnavailable {
		t.Fatalf("RowDenominator = %q, want unavailable", res.RowDenominator)
	}
	if len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
		t.Fatalf("unexpected samples/keys: %#v %#v", res.SampleValues, res.FailedKeys)
	}
	rf := res.Facts.Reconcile
	if rf == nil {
		t.Fatal("Facts.Reconcile is nil")
	}
	if rf.Left != left || rf.Right != right {
		t.Fatalf("tables = left %#v right %#v, want %#v / %#v", rf.Left, rf.Right, left, right)
	}
	if rf.Relationship != "equal" {
		t.Fatalf("Relationship = %q, want equal", rf.Relationship)
	}
}

func assertSecondaryOmitsSuiteScope(t *testing.T, db *recordingDB, cfg ReconcileConfig) {
	t.Helper()
	rightName := strings.ToLower(cfg.Right.Name)
	leftName := strings.ToLower(cfg.Left.Name)
	var sawLeftScope, sawRightUnscoped bool
	for _, q := range db.queries {
		lower := strings.ToLower(q.text)
		mentionsRight := strings.Contains(lower, rightName)
		mentionsLeft := strings.Contains(lower, leftName)
		if mentionsRight && !mentionsLeft {
			sawRightUnscoped = true
			if strings.Contains(lower, "tenant_id") {
				t.Fatalf("secondary count leaked suite scope predicate: %s", q.text)
			}
			for _, arg := range q.args {
				if arg == "t1" {
					t.Fatalf("secondary count leaked suite scope args: %#v", q)
				}
			}
		}
		if mentionsLeft && !mentionsRight && strings.Contains(lower, "tenant_id") {
			sawLeftScope = true
		}
	}
	if !sawLeftScope || !sawRightUnscoped {
		t.Fatalf("missing left-scoped/right-unscoped queries: left=%v right=%v queries=%#v",
			sawLeftScope, sawRightUnscoped, db.queries)
	}
}

func assertSecondaryFilterOnlyOnRight(t *testing.T, db *recordingDB, cfg ReconcileConfig) {
	t.Helper()
	rightName := strings.ToLower(cfg.Right.Name)
	leftName := strings.ToLower(cfg.Left.Name)
	var sawSecondaryFilter, sawLeftScope bool
	for _, q := range db.queries {
		lower := strings.ToLower(q.text)
		mentionsRight := strings.Contains(lower, rightName)
		mentionsLeft := strings.Contains(lower, leftName)
		if mentionsRight && !mentionsLeft {
			if !strings.Contains(lower, "status") {
				t.Fatalf("secondary missing filter predicate: %s", q.text)
			}
			sawSecondaryFilter = true
			if strings.Contains(lower, "tenant_id") {
				t.Fatalf("secondary reused left scope: %s", q.text)
			}
		}
		if mentionsLeft && !mentionsRight {
			if strings.Contains(lower, "tenant_id") {
				sawLeftScope = true
			}
			if strings.Contains(lower, "status") {
				t.Fatalf("left count reused secondary filter: %s", q.text)
			}
		}
	}
	if !sawSecondaryFilter || !sawLeftScope {
		t.Fatalf("missing secondary-filter/left-scope queries: secondary=%v left=%v queries=%#v",
			sawSecondaryFilter, sawLeftScope, db.queries)
	}
}
