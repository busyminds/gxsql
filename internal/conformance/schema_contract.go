package conformance

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/busyminds/gxsql"
)

func schemaContractTable(cfg Config) gxsql.TableRef {
	return gxsql.TableRef{Schema: cfg.Table.Schema, Name: "schema_contract"}
}

// schemaContractReportedTypes probes the fixture so expectations use the
// driver-reported DatabaseTypeName spelling rather than DDL spelling.
func schemaContractReportedTypes(t *testing.T, cfg Config) map[string]string {
	t.Helper()
	table := schemaContractTable(cfg)
	name, err := cfg.Dialect.QuoteIdent(table.Name)
	if err != nil {
		t.Fatalf("QuoteIdent table: %v", err)
	}
	qualified := name
	if table.Schema != "" {
		schema, err := cfg.Dialect.QuoteIdent(table.Schema)
		if err != nil {
			t.Fatalf("QuoteIdent schema: %v", err)
		}
		qualified = schema + "." + name
	}
	rows, err := cfg.DB.QueryContext(context.Background(), "SELECT * FROM "+qualified+" WHERE 1 = 0")
	if err != nil {
		t.Fatalf("schema type probe: %v", err)
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.ColumnTypes()
	if err != nil {
		t.Fatalf("schema type metadata: %v", err)
	}
	out := make(map[string]string, len(columns))
	for _, column := range columns {
		out[column.Name()] = column.DatabaseTypeName()
	}
	return out
}

func schemaMetadataCapability(d gxsql.Dialect) (gxsql.SchemaMetadataCapability, bool) {
	sd, ok := d.(gxsql.SchemaMetadataDialect)
	if !ok {
		return gxsql.SchemaMetadataCapability{}, false
	}
	return sd.SchemaMetadataCapability(), true
}

// runSchemaContracts exercises catalog nullability and exact reported-type
// contracts against the schema_contract fixture. Discovery must stay on the
// read-only zero-row ColumnTypes path with no row-value sampling or writes.
func runSchemaContracts(t *testing.T, cfg Config) {
	t.Helper()
	table := schemaContractTable(cfg)
	reportedTypes := schemaContractReportedTypes(t, cfg)
	idType := reportedTypes["id"]
	emailType := reportedTypes["email"]
	scoreType := reportedTypes["score"]
	payloadType := reportedTypes["payload"]
	if idType == "" || emailType == "" || scoreType == "" || payloadType == "" {
		t.Fatalf("schema probe did not report fixture types: %#v", reportedTypes)
	}
	cap, ok := schemaMetadataCapability(cfg.Dialect)
	if !ok {
		t.Fatal("dialect must advertise SchemaMetadataDialect")
	}
	if !cap.ExactReportedType {
		t.Fatal("built-in dialects must advertise exact reported-type support")
	}

	t.Run("schema/exact type pass and mismatch", func(t *testing.T) {
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.ColumnType("id").ReportedAs(idType),
			gxsql.ColumnType("email").ReportedAs(emailType),
			gxsql.ColumnType("score").ReportedAs(scoreType),
			gxsql.ColumnType("payload").ReportedAs(payloadType),
			gxsql.ColumnType("id").ReportedAs("NOSUCHTYPE"),
		).ValidateTable(context.Background(), db, table, gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertSchemaTypePass(t, report.Results[0], "id", idType)
		assertSchemaTypePass(t, report.Results[1], "email", emailType)
		assertSchemaTypePass(t, report.Results[2], "score", scoreType)
		assertSchemaTypePass(t, report.Results[3], "payload", payloadType)
		assertSchemaTypeFail(t, report.Results[4], "id", "NOSUCHTYPE", idType)
		assertSchemaDiscoverySQL(t, db, cfg.Dialect, table, 5)
	})

	t.Run("schema/missing column", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.ColumnType("missing_col").ReportedAs(idType),
			gxsql.ColumnNullability("missing_col").NotNullable(),
		).ValidateTable(
			context.Background(), cfg.DB, table,
			gxsql.WithDialect(cfg.Dialect), gxsql.ContinueOnError(),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertSchemaMissing(t, report.Results[0], gxsql.KindColumnType, "missing_col")
		if cap.Nullability {
			assertSchemaMissing(t, report.Results[1], gxsql.KindColumnNullability, "missing_col")
		} else {
			assertUnsupportedNullability(t, report.Results[1].Err, cfg.Dialect)
			if len(report.Results[1].Facts.MissingColumns) != 0 {
				t.Fatalf("unsupported nullability unexpectedly published MissingColumns: %#v", report.Results[1].Facts)
			}
		}
	})

	t.Run("schema/nullability supported pass and mismatch", func(t *testing.T) {
		if !cap.Nullability {
			db := &recordingDB{DB: cfg.DB}
			_, err := gxsql.NewSuite(
				gxsql.ColumnNullability("id").NotNullable(),
			).ValidateTable(context.Background(), db, table, gxsql.WithDialect(cfg.Dialect))
			assertUnsupportedNullability(t, err, cfg.Dialect)
			assertNoSQL(t, db)

			rep, err := gxsql.NewSuite(
				gxsql.ColumnNullability("id").NotNullable(),
				gxsql.ColumnType("id").ReportedAs(idType),
			).ValidateTable(
				context.Background(), cfg.DB, table,
				gxsql.WithDialect(cfg.Dialect), gxsql.ContinueOnError(),
			)
			if err != nil {
				t.Fatalf("ContinueOnError ValidateTable: %v", err)
			}
			assertUnsupportedNullability(t, rep.Results[0].Err, cfg.Dialect)
			assertSchemaTypePass(t, rep.Results[1], "id", idType)
			return
		}

		probe, err := gxsql.NewSuite(
			gxsql.ColumnNullability("id").NotNullable(),
		).ValidateTable(
			context.Background(), cfg.DB, table,
			gxsql.WithDialect(cfg.Dialect), gxsql.ContinueOnError(),
		)
		if err != nil {
			t.Fatalf("nullability probe ValidateTable: %v", err)
		}
		res := probe.Results[0]
		if res.Err != nil {
			var unknown *gxsql.UnknownMetadataError
			if errors.As(res.Err, &unknown) {
				t.Fatalf("advertised nullability became unknown: %#v", unknown)
			}
			t.Fatalf("nullability probe Err = %v", res.Err)
		}

		report, err := gxsql.NewSuite(
			gxsql.ColumnNullability("id").NotNullable(),
			gxsql.ColumnNullability("email").Nullable(),
			gxsql.ColumnNullability("id").Nullable(),
			gxsql.ColumnNullability("email").NotNullable(),
		).ValidateTable(context.Background(), cfg.DB, table, gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		assertSchemaNullabilityPass(t, report.Results[0], "id", gxsql.CatalogNullabilityNotNullable)
		assertSchemaNullabilityPass(t, report.Results[1], "email", gxsql.CatalogNullabilityNullable)
		assertSchemaNullabilityFail(t, report.Results[2], "id",
			gxsql.CatalogNullabilityNullable, gxsql.CatalogNullabilityNotNullable)
		assertSchemaNullabilityFail(t, report.Results[3], "email",
			gxsql.CatalogNullabilityNotNullable, gxsql.CatalogNullabilityNullable)
	})

	t.Run("schema/deterministic facts and no samples", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.ColumnType("email").ReportedAs("WRONG"),
			gxsql.ColumnType("id").ReportedAs(idType),
		).ValidateTable(context.Background(), cfg.DB, table, gxsql.WithDialect(cfg.Dialect))
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		if report.Results[0].Success {
			t.Fatalf("first result should be type mismatch: %#v", report.Results[0])
		}
		if !report.Results[1].Success {
			t.Fatalf("second result should be type pass: %#v", report.Results[1])
		}
		assertSchemaTypeFail(t, report.Results[0], "email", "WRONG", emailType)
		assertSchemaTypePass(t, report.Results[1], "id", idType)
		if report.Results[0].Name == report.Results[1].Name {
			t.Fatalf("result names collided: %q", report.Results[0].Name)
		}
		for i, res := range report.Results {
			if len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
				t.Fatalf("result %d retained samples/keys: %#v", i, res)
			}
			if res.RowDenominator != gxsql.RowDenominatorUnavailable {
				t.Fatalf("result %d RowDenominator = %v", i, res.RowDenominator)
			}
		}
	})
}

func assertSchemaTypePass(t *testing.T, res gxsql.Result, column, reported string) {
	t.Helper()
	if res.Err != nil || !res.Success || res.Kind != gxsql.KindColumnType || res.Column != column {
		t.Fatalf("result = %#v, want type pass column=%q reported=%q", res, column, reported)
	}
	if res.Facts.ConfiguredReportedType != reported || res.Facts.ObservedReportedType != reported {
		t.Fatalf("facts = %#v, want reported %q", res.Facts, reported)
	}
	if res.RowDenominator != gxsql.RowDenominatorUnavailable || len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
		t.Fatalf("shape = %#v", res)
	}
	if len(res.Facts.MissingColumns) != 0 {
		t.Fatalf("MissingColumns = %#v", res.Facts.MissingColumns)
	}
}

func assertSchemaTypeFail(t *testing.T, res gxsql.Result, column, expected, observed string) {
	t.Helper()
	if res.Err != nil || res.Success || res.Kind != gxsql.KindColumnType || res.Column != column {
		t.Fatalf("result = %#v, want type mismatch column=%q", res, column)
	}
	if res.Facts.ConfiguredReportedType != expected || res.Facts.ObservedReportedType != observed {
		t.Fatalf("facts = %#v, want configured=%q observed=%q", res.Facts, expected, observed)
	}
	if len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
		t.Fatalf("diagnostics = samples %#v keys %#v", res.SampleValues, res.FailedKeys)
	}
}

func assertSchemaNullabilityPass(t *testing.T, res gxsql.Result, column string, expected gxsql.CatalogNullability) {
	t.Helper()
	if res.Err != nil || !res.Success || res.Kind != gxsql.KindColumnNullability || res.Column != column {
		t.Fatalf("result = %#v, want nullability pass column=%q", res, column)
	}
	if res.Facts.ConfiguredNullability != expected || res.Facts.ObservedNullability != expected {
		t.Fatalf("facts = %#v, want %q", res.Facts, expected)
	}
	if res.RowDenominator != gxsql.RowDenominatorUnavailable || len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
		t.Fatalf("shape = %#v", res)
	}
}

func assertSchemaNullabilityFail(t *testing.T, res gxsql.Result, column string, expected, observed gxsql.CatalogNullability) {
	t.Helper()
	if res.Err != nil || res.Success || res.Kind != gxsql.KindColumnNullability || res.Column != column {
		t.Fatalf("result = %#v, want nullability mismatch column=%q", res, column)
	}
	if res.Facts.ConfiguredNullability != expected || res.Facts.ObservedNullability != observed {
		t.Fatalf("facts = %#v, want configured=%q observed=%q", res.Facts, expected, observed)
	}
	if len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
		t.Fatalf("diagnostics = samples %#v keys %#v", res.SampleValues, res.FailedKeys)
	}
}

func assertSchemaMissing(t *testing.T, res gxsql.Result, kind gxsql.ExpectationKind, column string) {
	t.Helper()
	if res.Err != nil || res.Success || res.Kind != kind || res.Column != column {
		t.Fatalf("result = %#v, want missing %s kind=%q", res, column, kind)
	}
	if !reflect.DeepEqual(res.Facts.MissingColumns, []string{column}) {
		t.Fatalf("MissingColumns = %#v, want [%s]", res.Facts.MissingColumns, column)
	}
	if !strings.Contains(res.Name, "missing "+column) {
		t.Fatalf("Name = %q, want missing %s", res.Name, column)
	}
	if len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
		t.Fatalf("diagnostics = samples %#v keys %#v", res.SampleValues, res.FailedKeys)
	}
}

func assertUnsupportedNullability(t *testing.T, err error, dialect gxsql.Dialect) {
	t.Helper()
	if err == nil {
		t.Fatal("expected unsupported nullability error")
	}
	if !errors.Is(err, gxsql.ErrCategoryUnsupported) {
		t.Fatalf("category = %v, want unsupported", err)
	}
	var unsupported *gxsql.UnsupportedCapabilityError
	if !errors.As(err, &unsupported) {
		var preflight *gxsql.PreflightErrors
		if errors.As(err, &preflight) && len(preflight.Issues) > 0 {
			if !errors.As(preflight.Issues[0].Err, &unsupported) {
				t.Fatalf("issue = %#v, want UnsupportedCapabilityError", preflight.Issues[0])
			}
		} else {
			t.Fatalf("err = %T (%v), want UnsupportedCapabilityError", err, err)
		}
	}
	if unsupported.Kind != gxsql.KindColumnNullability || unsupported.Capability != "nullability" {
		t.Fatalf("unsupported = %#v", unsupported)
	}
	if unsupported.Dialect == "" {
		t.Fatal("unsupported capability omitted dialect")
	}
	cap, ok := schemaMetadataCapability(dialect)
	if ok && cap.Nullability {
		t.Fatalf("dialect unexpectedly advertises nullability while refusing: %#v", unsupported)
	}
}

func assertSchemaDiscoverySQL(t *testing.T, db *recordingDB, dialect gxsql.Dialect, table gxsql.TableRef, wantQueries int) {
	t.Helper()
	if len(db.queries) != wantQueries {
		t.Fatalf("queries = %d, want %d discovery probes", len(db.queries), wantQueries)
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
		if strings.Contains(lower, "information_schema") ||
			strings.Contains(lower, "insert ") ||
			strings.Contains(lower, "update ") ||
			strings.Contains(lower, "delete ") {
			t.Fatalf("query %d not read-only zero-row probe: %q", i, q.text)
		}
	}
}
