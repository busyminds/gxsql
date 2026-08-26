package gxsql

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func setSchemaUsers(t *testing.T, cols []harnessColumnMeta) {
	t.Helper()
	setHarnessColumnTypes(t, map[string][]harnessColumnMeta{"users": append([]harnessColumnMeta(nil), cols...)})
	setHarnessData(t, harnessUsers())
}

func TestSchemaMetadataCapabilityMatrix(t *testing.T) {
	cases := []struct {
		name              string
		dialect           Dialect
		nullability       bool
		exactReportedType bool
	}{
		{"postgres", Postgres(), false, true},
		{"mysql", MySQL(), true, true},
		{"duckdb", DuckDB(), false, true},
		{"sqlite", SQLite(), false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cap, ok := schemaMetadataCapabilityFor(tc.dialect)
			if !ok {
				t.Fatal("expected SchemaMetadataDialect")
			}
			if cap.Name != schemaMetadataCapabilityName {
				t.Fatalf("Name = %q", cap.Name)
			}
			if cap.Nullability != tc.nullability || cap.ExactReportedType != tc.exactReportedType {
				t.Fatalf("capability = %#v", cap)
			}
			if dialectLabel(tc.dialect) != tc.name {
				t.Fatalf("dialectLabel = %q", dialectLabel(tc.dialect))
			}
		})
	}
}

func TestColumnNullabilityPassAndMismatch(t *testing.T) {
	setSchemaUsers(t, []harnessColumnMeta{
		{Name: "id", DatabaseTypeName: "integer", Nullable: boolPtr(false)},
		{Name: "email", DatabaseTypeName: "text", Nullable: boolPtr(true)},
	})
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(
		ColumnNullability("id").NotNullable(),
		ColumnNullability("email").Nullable(),
		ColumnNullability("email").NotNullable(),
	).ValidateTable(context.Background(), db, Table("users"), WithDialect(MySQL()))
	if err != nil {
		t.Fatal(err)
	}
	assertSchemaNullabilityPass(t, rep.Results[0], "id", CatalogNullabilityNotNullable)
	assertSchemaNullabilityPass(t, rep.Results[1], "email", CatalogNullabilityNullable)
	assertSchemaNullabilityFail(t, rep.Results[2], "email", CatalogNullabilityNotNullable, CatalogNullabilityNullable)
	if len(db.queries) != 3 {
		t.Fatalf("queries = %#v, want 3 discovery probes", db.queries)
	}
	for _, q := range db.queries {
		if q.text != "SELECT * FROM `users` WHERE 1 = 0" || len(q.args) != 0 {
			t.Fatalf("query = %#v", q)
		}
	}
}

func TestColumnTypePassAndMismatch(t *testing.T) {
	setSchemaUsers(t, []harnessColumnMeta{
		{Name: "id", DatabaseTypeName: "integer", Nullable: boolPtr(false)},
		{Name: "email", DatabaseTypeName: "text", Nullable: boolPtr(true)},
	})
	db := openHarnessDB(t)

	rep, err := NewSuite(
		ColumnType("id").ReportedAs("integer"),
		ColumnType("id").ReportedAs("BIGINT"),
	).ValidateTable(context.Background(), db, Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatal(err)
	}
	assertSchemaTypePass(t, rep.Results[0], "id", "integer")
	assertSchemaTypeFail(t, rep.Results[1], "id", "BIGINT", "integer")
}

// TestSchemaContractMetadataIsTableScoped proves ColumnTypes metadata is resolved
// for the queried table only. Two tables share column "id" with conflicting
// type/nullability; a global name→meta flatten cannot satisfy both contracts.
func TestSchemaContractMetadataIsTableScoped(t *testing.T) {
	setHarnessColumnTypes(t, map[string][]harnessColumnMeta{
		"users": {
			{Name: "id", DatabaseTypeName: "integer", Nullable: boolPtr(false)},
		},
		"orders": {
			{Name: "id", DatabaseTypeName: "text", Nullable: boolPtr(true)},
		},
	})
	db := openHarnessDB(t)

	usersRep, err := NewSuite(
		ColumnType("id").ReportedAs("integer"),
		ColumnNullability("id").NotNullable(),
	).ValidateTable(context.Background(), db, Table("users"), WithDialect(MySQL()))
	if err != nil {
		t.Fatal(err)
	}
	assertSchemaTypePass(t, usersRep.Results[0], "id", "integer")
	assertSchemaNullabilityPass(t, usersRep.Results[1], "id", CatalogNullabilityNotNullable)

	ordersRep, err := NewSuite(
		ColumnType("id").ReportedAs("text"),
		ColumnNullability("id").Nullable(),
	).ValidateTable(context.Background(), db, Table("orders"), WithDialect(MySQL()))
	if err != nil {
		t.Fatal(err)
	}
	assertSchemaTypePass(t, ordersRep.Results[0], "id", "text")
	assertSchemaNullabilityPass(t, ordersRep.Results[1], "id", CatalogNullabilityNullable)
}

func TestColumnTypeUnknownFailsClosed(t *testing.T) {
	setSchemaUsers(t, []harnessColumnMeta{
		{Name: "id", DatabaseTypeName: "", Nullable: boolPtr(false)},
	})
	db := openCountingHarnessDB(t)

	_, err := NewSuite(ColumnType("id").ReportedAs("integer")).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected unknown reported type error")
	}
	var unknown *UnknownMetadataError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %T (%v), want UnknownMetadataError", err, err)
	}
	if unknown.Kind != KindColumnType || unknown.Column != "id" ||
		unknown.Capability != exactReportedTypeCapabilityName {
		t.Fatalf("unknown = %#v", unknown)
	}
}

func TestColumnTypeReportedAsWhitespaceRejectedBeforeSQL(t *testing.T) {
	setSchemaUsers(t, []harnessColumnMeta{
		{Name: "id", DatabaseTypeName: "integer", Nullable: boolPtr(false)},
	})
	counter := openCountingHarnessDB(t)

	_, err := NewSuite(ColumnType("id").ReportedAs(" integer ")).ValidateTable(
		context.Background(), counter, Table("users"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected whitespace-padded type name configuration error")
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0 before SQL on config failure", counter.queries)
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("category = %v", err)
	}
}

func TestSchemaContractMissingColumn(t *testing.T) {
	setSchemaUsers(t, []harnessColumnMeta{
		{Name: "id", DatabaseTypeName: "integer", Nullable: boolPtr(false)},
	})
	db := openHarnessDB(t)

	rep, err := NewSuite(
		ColumnNullability("email").NotNullable(),
		ColumnType("email").ReportedAs("text"),
	).ValidateTable(context.Background(), db, Table("users"), WithDialect(MySQL()))
	if err != nil {
		t.Fatal(err)
	}
	for i, res := range rep.Results {
		if res.Err != nil || res.Success {
			t.Fatalf("result %d = %#v", i, res)
		}
		if !reflect.DeepEqual(res.Facts.MissingColumns, []string{"email"}) {
			t.Fatalf("result %d MissingColumns = %#v", i, res.Facts.MissingColumns)
		}
		if !strings.Contains(res.Name, "missing email") {
			t.Fatalf("result %d Name = %q", i, res.Name)
		}
	}
}

func TestColumnNullabilityUnknownFailsClosed(t *testing.T) {
	setSchemaUsers(t, []harnessColumnMeta{
		{Name: "email", DatabaseTypeName: "text", Nullable: nil},
	})
	db := openCountingHarnessDB(t)

	_, err := NewSuite(ColumnNullability("email").NotNullable()).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(MySQL()),
	)
	if err == nil {
		t.Fatal("expected unknown nullability error")
	}
	if !errors.Is(err, ErrCategoryUnsupported) {
		t.Fatalf("category = %v", err)
	}
	var unknown *UnknownMetadataError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %T (%v), want UnknownMetadataError", err, err)
	}
	if unknown.Kind != KindColumnNullability || unknown.Column != "email" || unknown.Capability != nullabilityCapabilityName {
		t.Fatalf("unknown = %#v", unknown)
	}

	rep, err := NewSuite(ColumnNullability("email").NotNullable()).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(MySQL()), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success || res.Err == nil || !errors.Is(res.Err, ErrCategoryUnsupported) {
		t.Fatalf("continue result = %#v", res)
	}
	if res.Facts.ConfiguredNullability != CatalogNullabilityNotNullable || res.Facts.ObservedNullability != CatalogNullabilityUnknown {
		t.Fatalf("facts = %#v", res.Facts)
	}
}

func TestSchemaContractUnsupportedCapabilityPreflight(t *testing.T) {
	setSchemaUsers(t, []harnessColumnMeta{
		{Name: "id", DatabaseTypeName: "INTEGER", Nullable: boolPtr(false)},
	})

	for _, tc := range []struct {
		name    string
		dialect Dialect
		label   string
	}{
		{"sqlite", SQLite(), "sqlite"},
		{"postgres", Postgres(), "postgres"},
		{"duckdb", DuckDB(), "duckdb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			counter := openCountingHarnessDB(t)
			_, err := NewSuite(
				ColumnNullability("id").NotNullable(),
				ColumnType("id").ReportedAs("INTEGER"),
			).ValidateTable(context.Background(), counter, Table("users"), WithDialect(tc.dialect))
			if err == nil {
				t.Fatal("expected unsupported nullability preflight")
			}
			var pf *PreflightErrors
			if !errors.As(err, &pf) {
				t.Fatalf("err = %T (%v)", err, err)
			}
			if len(pf.Issues) != 1 {
				t.Fatalf("issues = %#v", pf.Issues)
			}
			var unsupported *UnsupportedCapabilityError
			if !errors.As(pf.Issues[0].Err, &unsupported) {
				t.Fatalf("issue = %#v", pf.Issues[0])
			}
			if unsupported.Kind != KindColumnNullability || unsupported.Dialect != tc.label || unsupported.Capability != nullabilityCapabilityName {
				t.Fatalf("unsupported = %#v", unsupported)
			}
			if counter.queries != 0 {
				t.Fatalf("queries = %d, want 0", counter.queries)
			}

			rep, err := NewSuite(
				ColumnNullability("id").NotNullable(),
				ColumnType("id").ReportedAs("INTEGER"),
				RequiredColumns("id"),
			).ValidateTable(
				context.Background(), counter, Table("users"),
				WithDialect(tc.dialect), ContinueOnError(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if rep.Results[0].Err == nil || !errors.Is(rep.Results[0].Err, ErrCategoryUnsupported) {
				t.Fatalf("nullability slot = %#v", rep.Results[0])
			}
			assertSchemaTypePass(t, rep.Results[1], "id", "INTEGER")
			assertStructuralPass(t, rep.Results[2], KindRequiredColumns, []string{"id"})
		})
	}
}

func TestSchemaContractScopeAndToleranceRejected(t *testing.T) {
	setSchemaUsers(t, []harnessColumnMeta{
		{Name: "id", DatabaseTypeName: "integer", Nullable: boolPtr(false)},
	})
	scope := TrustedScope("tenant-a", "tenant_id = ?", "tenant-a")
	counter := openCountingHarnessDB(t)

	_, err := NewSuite(
		ColumnNullability("id").NotNullable(),
		ColumnType("id").ReportedAs("integer"),
	).ValidateTable(
		context.Background(), counter, Table("users"),
		WithDialect(MySQL()), WithScope(scope),
	)
	if err == nil {
		t.Fatal("expected scope preflight error")
	}
	if !strings.Contains(err.Error(), "population filters are incompatible with structural column expectations") {
		t.Fatalf("message = %v", err)
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0", counter.queries)
	}

	_, err = NewSuite(
		WithMaxFailedCount(1, ColumnNullability("id").NotNullable()),
		WithPolicy(ColumnType("id").ReportedAs("integer"), Policy{Tolerance: MaxFailedPercent(10)}),
	).ValidateTable(context.Background(), counter, Table("users"), WithDialect(MySQL()))
	if err == nil {
		t.Fatal("expected tolerance preflight errors")
	}
	var pf *PreflightErrors
	if !errors.As(err, &pf) || len(pf.Issues) != 2 {
		t.Fatalf("err = %#v", err)
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0", counter.queries)
	}
}

func TestSchemaContractPreservesNameContracts(t *testing.T) {
	setSchemaUsers(t, []harnessColumnMeta{
		{Name: "id", DatabaseTypeName: "integer", Nullable: boolPtr(false)},
		{Name: "email", DatabaseTypeName: "text", Nullable: boolPtr(true)},
	})
	db := openRecordingHarnessDB(t)

	required := []string{"id", "email"}
	rep, err := NewSuite(
		RequiredColumns(required...),
		ExactColumns(required...),
		ColumnNullability("email").Nullable(),
		ColumnType("id").ReportedAs("integer"),
	).ValidateTable(context.Background(), db, Table("users"), WithDialect(MySQL()))
	if err != nil {
		t.Fatal(err)
	}
	assertStructuralPass(t, rep.Results[0], KindRequiredColumns, required)
	assertStructuralPass(t, rep.Results[1], KindExactColumns, required)
	assertSchemaNullabilityPass(t, rep.Results[2], "email", CatalogNullabilityNullable)
	assertSchemaTypePass(t, rep.Results[3], "id", "integer")
	for _, q := range db.queries {
		if q.text != "SELECT * FROM `users` WHERE 1 = 0" {
			t.Fatalf("unexpected query %q", q.text)
		}
		if len(q.args) != 0 {
			t.Fatalf("args = %#v", q.args)
		}
	}
}

func TestSchemaContractNoSamplesOrFailedKeys(t *testing.T) {
	setSchemaUsers(t, []harnessColumnMeta{
		{Name: "id", DatabaseTypeName: "integer", Nullable: boolPtr(true)},
	})
	db := openHarnessDB(t)

	rep, err := NewSuite(ColumnNullability("id").NotNullable()).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(MySQL()), WithKey("id"), WithSampleCap(10), WithFailedKeysCap(10),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertSchemaNullabilityFail(t, rep.Results[0], "id", CatalogNullabilityNotNullable, CatalogNullabilityNullable)
}

func TestExportSchemaContractFactsAndPrivacy(t *testing.T) {
	rep := Report{
		Target: &TableRef{Name: "users"},
		Results: []Result{
			{
				Kind:           KindColumnNullability,
				Name:           "email not nullable: mismatched nullability",
				Column:         "email",
				Success:        false,
				RowDenominator: RowDenominatorUnavailable,
				SampleValues:   []any{"secret-sample"},
				FailedKeys:     []RowKey{{"secret-key"}},
				diagnostics: &resultDiagnostics{
					query: "SELECT secret_query",
					args:  []any{"secret-arg"},
				},
				Facts: ResultFacts{
					ConfiguredNullability: CatalogNullabilityNotNullable,
					ObservedNullability:   CatalogNullabilityNullable,
				},
			},
			{
				Kind:           KindColumnType,
				Name:           "id reported as integer",
				Column:         "id",
				Success:        true,
				RowDenominator: RowDenominatorUnavailable,
				Facts: ResultFacts{
					ConfiguredReportedType: "integer",
					ObservedReportedType:   "integer",
				},
			},
		},
	}

	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	if dto.Results[0].Facts == nil || dto.Results[1].Facts == nil {
		t.Fatalf("facts omitted: %#v", dto.Results)
	}
	if dto.Results[0].Facts.ConfiguredNullability != CatalogNullabilityNotNullable ||
		dto.Results[0].Facts.ObservedNullability != CatalogNullabilityNullable {
		t.Fatalf("nullability facts = %#v", dto.Results[0].Facts)
	}
	if dto.Results[1].Facts.ConfiguredReportedType != "integer" ||
		dto.Results[1].Facts.ObservedReportedType != "integer" {
		t.Fatalf("type facts = %#v", dto.Results[1].Facts)
	}

	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(data)
	for _, want := range []string{
		`"configured_nullability":"not_nullable"`,
		`"observed_nullability":"nullable"`,
		`"configured_reported_type":"integer"`,
		`"observed_reported_type":"integer"`,
		`"kind":"column_nullability"`,
		`"kind":"column_type"`,
	} {
		if !strings.Contains(jsonText, want) {
			t.Fatalf("export JSON missing %s in %s", want, jsonText)
		}
	}
	for _, forbidden := range []string{
		"secret-sample", "secret-key", "secret_query", "secret-arg",
		`"samples"`, `"failed_keys"`, `"diagnostics"`,
	} {
		if strings.Contains(jsonText, forbidden) {
			t.Fatalf("default export leaked %q in %s", forbidden, jsonText)
		}
	}
}

func assertSchemaNullabilityPass(t *testing.T, res Result, column string, expected CatalogNullability) {
	t.Helper()
	if res.Err != nil {
		t.Fatalf("Err = %v", res.Err)
	}
	if !res.Success || res.Kind != KindColumnNullability || res.Column != column {
		t.Fatalf("result = %#v", res)
	}
	if res.RowDenominator != RowDenominatorUnavailable || len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
		t.Fatalf("shape = %#v", res)
	}
	if res.Facts.ConfiguredNullability != expected || res.Facts.ObservedNullability != expected {
		t.Fatalf("facts = %#v", res.Facts)
	}
	if len(res.Facts.MissingColumns) != 0 {
		t.Fatalf("MissingColumns = %#v", res.Facts.MissingColumns)
	}
}

func assertSchemaNullabilityFail(t *testing.T, res Result, column string, expected, observed CatalogNullability) {
	t.Helper()
	if res.Err != nil {
		t.Fatalf("Err = %v", res.Err)
	}
	if res.Success || res.Kind != KindColumnNullability || res.Column != column {
		t.Fatalf("result = %#v", res)
	}
	if res.Facts.ConfiguredNullability != expected || res.Facts.ObservedNullability != observed {
		t.Fatalf("facts = %#v", res.Facts)
	}
	if len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
		t.Fatalf("diagnostics = %#v %#v", res.SampleValues, res.FailedKeys)
	}
}

func assertSchemaTypePass(t *testing.T, res Result, column, reported string) {
	t.Helper()
	if res.Err != nil {
		t.Fatalf("Err = %v", res.Err)
	}
	if !res.Success || res.Kind != KindColumnType || res.Column != column {
		t.Fatalf("result = %#v", res)
	}
	if res.Facts.ConfiguredReportedType != reported || res.Facts.ObservedReportedType != reported {
		t.Fatalf("facts = %#v", res.Facts)
	}
	if res.RowDenominator != RowDenominatorUnavailable || len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
		t.Fatalf("shape = %#v", res)
	}
}

func assertSchemaTypeFail(t *testing.T, res Result, column, expected, observed string) {
	t.Helper()
	if res.Err != nil {
		t.Fatalf("Err = %v", res.Err)
	}
	if res.Success || res.Kind != KindColumnType || res.Column != column {
		t.Fatalf("result = %#v", res)
	}
	if res.Facts.ConfiguredReportedType != expected || res.Facts.ObservedReportedType != observed {
		t.Fatalf("facts = %#v", res.Facts)
	}
}
