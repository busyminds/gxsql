package conformance

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/busyminds/gxsql"
)

func structuralColsTable(cfg Config) gxsql.TableRef {
	return gxsql.TableRef{Schema: cfg.Table.Schema, Name: "structural_cols"}
}

func structuralExtraTable(cfg Config) gxsql.TableRef {
	return gxsql.TableRef{Schema: cfg.Table.Schema, Name: "structural_extra"}
}

func structuralCaseTable(cfg Config) gxsql.TableRef {
	return gxsql.TableRef{Schema: cfg.Table.Schema, Name: "structural_case"}
}

func runStructuralColumnContracts(t *testing.T, cfg Config) {
	t.Helper()
	cols := structuralColsTable(cfg)
	extra := structuralExtraTable(cfg)
	required := []string{"id", "event_time", "payload"}

	t.Run("structural/required and exact pass", func(t *testing.T) {
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.RequiredColumns(required...),
			gxsql.ExactColumns(required...),
		).ValidateTable(context.Background(), db, cols, gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertStructuralPass(t, report.Results[0], gxsql.KindRequiredColumns, required)
		assertStructuralPass(t, report.Results[1], gxsql.KindExactColumns, required)
		assertStructuralDiscoverySQL(t, db, cfg.Dialect, cols)
	})

	t.Run("structural/missing unexpected and both", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.RequiredColumns("id", "event_time", "payload", "missing_col"),
			gxsql.ExactColumns(required...),
			gxsql.ExactColumns("id", "event_time", "ghost"),
		).ValidateTable(context.Background(), cfg.DB, extra, gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertStructuralFail(t, report.Results[0], gxsql.KindRequiredColumns,
			[]string{"id", "event_time", "payload", "missing_col"},
			[]string{"missing_col"}, nil)
		assertStructuralFail(t, report.Results[1], gxsql.KindExactColumns,
			required, nil, []string{"note"})
		assertStructuralFail(t, report.Results[2], gxsql.KindExactColumns,
			[]string{"id", "event_time", "ghost"},
			[]string{"ghost"}, []string{"payload", "note"})
	})

	t.Run("structural/reorder independence", func(t *testing.T) {
		// Exact set against structural_cols; declaration order differs from CREATE order.
		reordered := []string{"payload", "id", "event_time"}
		report, err := gxsql.NewSuite(
			gxsql.RequiredColumns(reordered...),
			gxsql.ExactColumns(reordered...),
		).ValidateTable(context.Background(), cfg.DB, cols, gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertStructuralPass(t, report.Results[0], gxsql.KindRequiredColumns, reordered)
		assertStructuralPass(t, report.Results[1], gxsql.KindExactColumns, reordered)
	})

	t.Run("structural/physical case", func(t *testing.T) {
		caseTable := structuralCaseTable(cfg)
		wrong := []string{"id", "eventtime", "payload"}
		right := []string{"id", "EventTime", "payload"}

		miss, err := gxsql.NewSuite(gxsql.RequiredColumns(wrong...)).ValidateTable(
			context.Background(), cfg.DB, caseTable, gxsql.WithDialect(cfg.Dialect),
		)
		if err != nil {
			t.Fatalf("wrong-case ValidateTable: %v", err)
		}
		if miss.Results[0].Success {
			t.Fatalf("engine folded case; wrong-case required should fail: %#v", miss.Results[0])
		}
		if !reflect.DeepEqual(miss.Results[0].Facts.MissingColumns, []string{"eventtime"}) {
			t.Fatalf("MissingColumns = %#v, want [eventtime]; discovered spelling may differ",
				miss.Results[0].Facts.MissingColumns)
		}

		pass, err := gxsql.NewSuite(gxsql.ExactColumns(right...)).ValidateTable(
			context.Background(), cfg.DB, caseTable, gxsql.WithDialect(cfg.Dialect),
		)
		if err != nil {
			t.Fatalf("mixed-case ValidateTable: %v", err)
		}
		if !pass.Results[0].Success {
			t.Fatalf("physical EventTime exact set failed: %#v (engine may not preserve quoted case)",
				pass.Results[0])
		}
		assertStructuralPass(t, pass.Results[0], gxsql.KindExactColumns, right)
	})
}

func assertStructuralPass(t *testing.T, res gxsql.Result, kind gxsql.ExpectationKind, required []string) {
	t.Helper()
	if res.Err != nil || !res.Success || res.Kind != kind {
		t.Fatalf("result = %#v, want success kind=%q", res, kind)
	}
	if res.Column != "" || res.RowDenominator != gxsql.RowDenominatorUnavailable {
		t.Fatalf("shape = %#v", res)
	}
	if res.Total != 0 || res.FailedCount != 0 || res.FailedPercent != 0 {
		t.Fatalf("counts = %#v", res)
	}
	if len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
		t.Fatalf("diagnostics = samples %#v keys %#v", res.SampleValues, res.FailedKeys)
	}
	if !reflect.DeepEqual(res.Facts.RequiredColumns, required) {
		t.Fatalf("RequiredColumns = %#v, want %#v", res.Facts.RequiredColumns, required)
	}
	if len(res.Facts.MissingColumns) != 0 || len(res.Facts.UnexpectedColumns) != 0 {
		t.Fatalf("differences = missing %#v unexpected %#v", res.Facts.MissingColumns, res.Facts.UnexpectedColumns)
	}
}

func assertStructuralFail(
	t *testing.T,
	res gxsql.Result,
	kind gxsql.ExpectationKind,
	required, missing, unexpected []string,
) {
	t.Helper()
	if res.Err != nil || res.Success || res.Kind != kind {
		t.Fatalf("result = %#v, want failure kind=%q", res, kind)
	}
	if res.Column != "" || res.RowDenominator != gxsql.RowDenominatorUnavailable {
		t.Fatalf("shape = %#v", res)
	}
	if len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
		t.Fatalf("diagnostics = samples %#v keys %#v", res.SampleValues, res.FailedKeys)
	}
	if !reflect.DeepEqual(res.Facts.RequiredColumns, required) {
		t.Fatalf("RequiredColumns = %#v, want %#v", res.Facts.RequiredColumns, required)
	}
	if !reflect.DeepEqual(res.Facts.MissingColumns, missing) {
		t.Fatalf("MissingColumns = %#v, want %#v", res.Facts.MissingColumns, missing)
	}
	if !reflect.DeepEqual(res.Facts.UnexpectedColumns, unexpected) {
		t.Fatalf("UnexpectedColumns = %#v, want %#v", res.Facts.UnexpectedColumns, unexpected)
	}
}

func assertStructuralDiscoverySQL(t *testing.T, db *recordingDB, dialect gxsql.Dialect, table gxsql.TableRef) {
	t.Helper()
	if len(db.queries) != 2 {
		t.Fatalf("queries = %d, want 2 discovery probes", len(db.queries))
	}
	quotedName, err := dialect.QuoteIdent(table.Name)
	if err != nil {
		t.Fatalf("QuoteIdent: %v", err)
	}
	wantSuffix := "SELECT * FROM "
	if table.Schema != "" {
		quotedSchema, err := dialect.QuoteIdent(table.Schema)
		if err != nil {
			t.Fatalf("QuoteIdent schema: %v", err)
		}
		wantSuffix += quotedSchema + "." + quotedName
	} else {
		wantSuffix += quotedName
	}
	wantSuffix += " WHERE 1 = 0"
	for i, q := range db.queries {
		if q.text != wantSuffix {
			t.Fatalf("query %d = %q, want %q", i, q.text, wantSuffix)
		}
		if len(q.args) != 0 {
			t.Fatalf("query %d args = %#v", i, q.args)
		}
		lower := strings.ToLower(q.text)
		if strings.Contains(lower, "information_schema") || strings.Contains(lower, "insert ") {
			t.Fatalf("query %d not read-only zero-row probe: %q", i, q.text)
		}
	}
}
