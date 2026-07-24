package gxsql

import (
	"context"
	"errors"
	"testing"
)

func TestValidateTableEmptyPublicScope(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "tenant-a",
		map[string]any{
			"id":   int64(1),
			"name": "Alice",
			"age":  int64(25),
		},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(
		Int("age").Between(0, 120),
		RowCount().Equal(0),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant", "tenant_id = ?", "tenant-missing")),
	)
	if err != nil {
		t.Fatalf("ValidateTable error = %v", err)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(rep.Results))
	}

	perRow := rep.Results[0]
	if !perRow.Success {
		t.Fatalf("empty scoped per-row expectation failed: %#v", perRow)
	}
	if perRow.Total != 0 {
		t.Fatalf("per-row total = %d, want 0", perRow.Total)
	}
	if perRow.FailedCount != 0 {
		t.Fatalf("per-row failed count = %d, want 0", perRow.FailedCount)
	}
	if perRow.FailedPercent != 0 {
		t.Fatalf("per-row failed percent = %f, want 0", perRow.FailedPercent)
	}
	if perRow.RowDenominator != RowDenominatorAvailable {
		t.Fatalf("row denominator = %q, want %q", perRow.RowDenominator, RowDenominatorAvailable)
	}

	if !rep.Results[1].Success {
		t.Fatalf("empty scoped row-count expectation failed: %#v", rep.Results[1])
	}
	if !rep.OK() {
		t.Fatalf("empty scoped report = %#v, want OK", rep)
	}
}

func TestValidateTableInvalidPublicScopePreflightNoExecution(t *testing.T) {
	t.Run("placeholder arity mismatch", func(t *testing.T) {
		setHarnessData(t, scopedHarnessUsers("tenant_id", "tenant-a",
			map[string]any{
				"id":   int64(1),
				"name": "Alice",
				"age":  int64(25),
			},
		))
		db := openRecordingHarnessDB(t)

		rep, err := NewSuite(
			Int("age").Between(0, 120),
			String("name").NotEmpty(),
			RowCount().Equal(1),
		).ValidateTable(
			context.Background(), db, Table("users"),
			WithDialect(Postgres()),
			WithScope(TrustedScope("tenant", "tenant_id = ?", "tenant-a", "unexpected")),
		)
		if err == nil {
			t.Fatal("expected invalid scope configuration error")
		}
		if !errors.Is(err, ErrCategoryInvalidConfig) {
			t.Fatalf("error category = %v, want invalid_config", err)
		}
		if len(rep.Results) != 0 || rep.Target != nil || rep.ScopeID != "" {
			t.Fatalf("report = %#v, want zero report", rep)
		}
		if len(db.queries) != 0 {
			t.Fatalf("queries = %d, want 0 for invalid scope", len(db.queries))
		}
	})
}
