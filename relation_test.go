package gxsql

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func referenceFixture() map[string][]map[string]any {
	return map[string][]map[string]any{
		"customers": {
			{"id": int64(1), "tenant_id": "t1"},
			{"id": int64(2), "tenant_id": "t1"},
			{"id": int64(9), "tenant_id": "t2"},
		},
		"orders": {
			{"id": int64(1), "tenant_id": "t1", "customer_id": int64(1)},
			{"id": int64(2), "tenant_id": "t1", "customer_id": int64(2)},
			{"id": int64(3), "tenant_id": "t1", "customer_id": int64(99)}, // orphan
			{"id": int64(4), "tenant_id": "t1", "customer_id": nil},       // any-NULL passes
			{"id": int64(5), "tenant_id": "t2", "customer_id": int64(9)},  // outside local scope
			{"id": int64(6), "tenant_id": "t2", "customer_id": int64(99)}, // out-of-scope orphan
		},
	}
}

func TestReferenceSingleColumnMatchesAndOrphans(t *testing.T) {
	setHarnessData(t, map[string][]map[string]any{
		"customers": {
			{"id": int64(1)},
			{"id": int64(2)},
		},
		"orders": {
			{"id": int64(1), "customer_id": int64(1)},
			{"id": int64(2), "customer_id": int64(99)},
			{"id": int64(3), "customer_id": int64(2)},
			{"id": int64(4), "customer_id": int64(98)},
		},
	})
	db := openHarnessDB(t)

	rep, err := NewSuite(Column("customer_id").References(Table("customers"), "id")).ValidateTable(
		context.Background(), db, Table("orders"),
		WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Kind != KindReference {
		t.Fatalf("Kind = %q, want %q", res.Kind, KindReference)
	}
	if res.Column != "" {
		t.Fatalf("Column = %q, want empty", res.Column)
	}
	if res.Success || res.Total != 4 || res.FailedCount != 2 {
		t.Fatalf("got %#v, want 2 orphans of 4", res)
	}
	if res.Facts.Reference == nil {
		t.Fatal("expected Reference facts")
	}
	wantFacts := &ReferenceFacts{
		LocalColumns:  []string{"customer_id"},
		Parent:        Table("customers"),
		ParentColumns: []string{"id"},
	}
	if !reflect.DeepEqual(res.Facts.Reference, wantFacts) {
		t.Fatalf("Reference facts = %#v, want %#v", res.Facts.Reference, wantFacts)
	}
	wantKeys := []RowKey{{int64(2)}, {int64(4)}}
	if !reflect.DeepEqual(res.FailedKeys, wantKeys) {
		t.Fatalf("FailedKeys = %#v, want %#v", res.FailedKeys, wantKeys)
	}
	for _, sample := range res.SampleValues {
		if sample == nil {
			t.Fatal("samples must be local non-null FK values, not nil")
		}
	}
}

func TestReferenceCompositeMatchesAndOrphans(t *testing.T) {
	setHarnessData(t, referenceFixture())
	db := openHarnessDB(t)

	rep, err := NewSuite(
		Columns("tenant_id", "customer_id").References(Table("customers"), "tenant_id", "id"),
	).ValidateTable(
		context.Background(), db, Table("orders"),
		WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success || res.Total != 6 || res.FailedCount != 2 {
		t.Fatalf("got %#v, want 2 orphans of 6 (null row passes)", res)
	}
	wantKeys := []RowKey{{int64(3)}, {int64(6)}}
	if !reflect.DeepEqual(res.FailedKeys, wantKeys) {
		t.Fatalf("FailedKeys = %#v, want %#v", res.FailedKeys, wantKeys)
	}
	if got := res.Facts.Reference; got == nil || !reflect.DeepEqual(got.LocalColumns, []string{"tenant_id", "customer_id"}) {
		t.Fatalf("facts = %#v", res.Facts.Reference)
	}
}

func TestReferenceAnyNullLocalTuplePasses(t *testing.T) {
	setHarnessData(t, map[string][]map[string]any{
		"customers": {{"id": int64(1), "tenant_id": "t1"}},
		"orders": {
			{"id": int64(1), "tenant_id": "t1", "customer_id": nil},
			{"id": int64(2), "tenant_id": nil, "customer_id": int64(1)},
			{"id": int64(3), "tenant_id": nil, "customer_id": nil},
			{"id": int64(4), "tenant_id": "t1", "customer_id": int64(1)},
		},
	})
	db := openHarnessDB(t)

	rep, err := NewSuite(
		Columns("tenant_id", "customer_id").References(Table("customers"), "tenant_id", "id"),
	).ValidateTable(
		context.Background(), db, Table("orders"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || res.Total != 4 || res.FailedCount != 0 {
		t.Fatalf("got %#v, want vacuous pass for any-NULL local tuples", res)
	}
}

func TestReferenceLocalScopeIgnoresParentOutsideScope(t *testing.T) {
	setHarnessData(t, referenceFixture())
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(
		Columns("tenant_id", "customer_id").References(Table("customers"), "tenant_id", "id"),
	).ValidateTable(
		context.Background(), db, Table("orders"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant", "tenant_id = ?", "t1")),
		WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	// Scoped local rows: ids 1,2,3,4. Orphan is id 3 only. Parent tenant t2 row must remain visible.
	if res.Success || res.Total != 4 || res.FailedCount != 1 {
		t.Fatalf("got %#v, want scoped total 4 and 1 orphan", res)
	}
	if !reflect.DeepEqual(res.FailedKeys, []RowKey{{int64(3)}}) {
		t.Fatalf("FailedKeys = %#v, want [[3]]", res.FailedKeys)
	}
	for _, q := range db.queries {
		if strings.Contains(q.text, "NOT EXISTS") && strings.Contains(strings.ToLower(q.text), "tenant_id =") {
			// Parent subquery must not contain the local scope predicate.
			notExistsIdx := strings.Index(q.text, "NOT EXISTS")
			parentSQL := q.text[notExistsIdx:]
			if strings.Contains(parentSQL, "tenant_id = $") || strings.Contains(parentSQL, "tenant_id = ?") {
				t.Fatalf("parent subquery leaked local scope: %s", q.text)
			}
		}
		for _, arg := range q.args {
			if arg == "t2" {
				t.Fatalf("unexpected t2 scope binding in query args: %#v", q.args)
			}
		}
	}
}

func TestReferenceQualifiedParentRendering(t *testing.T) {
	setHarnessData(t, map[string][]map[string]any{
		"customers": {{"id": int64(1)}},
		"orders": {
			{"id": int64(1), "customer_id": int64(1)},
			{"id": int64(2), "customer_id": int64(9)},
		},
	})
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(
		Column("customer_id").References(SchemaTable("sales", "customers"), "id"),
	).ValidateTable(
		context.Background(), db, SchemaTable("sales", "orders"),
		WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success || res.FailedCount != 1 {
		t.Fatalf("got %#v, want one orphan", res)
	}
	if res.Facts.Reference == nil || res.Facts.Reference.Parent != (TableRef{Schema: "sales", Name: "customers"}) {
		t.Fatalf("parent facts = %#v", res.Facts.Reference)
	}
	foundParent, foundLocal := false, false
	for _, q := range db.queries {
		if strings.Contains(q.text, `"sales"."customers" AS "__gx_parent"`) && strings.Contains(q.text, "NOT EXISTS") {
			foundParent = true
		}
		if strings.Contains(q.text, `"sales"."orders" AS "__gx_local"`) {
			foundLocal = true
		}
	}
	if !foundParent || !foundLocal {
		t.Fatalf("expected qualified aliased local/parent SQL: %#v", db.queries)
	}
}

func TestReferenceSameNameLocalAndParentColumns(t *testing.T) {
	setHarnessData(t, map[string][]map[string]any{
		"parents": {
			{"id": int64(1), "name": "a"},
			{"id": int64(2), "name": "b"},
		},
		"children": {
			{"id": int64(10), "parent_name": "x"}, // orphan: id 10 missing in parents
			{"id": int64(1), "parent_name": "a"},
			{"id": int64(2), "parent_name": "b"},
		},
	})
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(
		Column("id").References(Table("parents"), "id"),
	).ValidateTable(
		context.Background(), db, Table("children"),
		WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success || res.Total != 3 || res.FailedCount != 1 {
		t.Fatalf("got %#v, want one same-name orphan", res)
	}
	if !reflect.DeepEqual(res.FailedKeys, []RowKey{{int64(10)}}) {
		t.Fatalf("FailedKeys = %#v, want [[10]]", res.FailedKeys)
	}

	var failSQL string
	for _, q := range db.queries {
		if strings.Contains(q.text, "NOT EXISTS") {
			failSQL = q.text
			break
		}
	}
	if failSQL == "" {
		t.Fatal("expected NOT EXISTS failure SQL")
	}
	if !strings.Contains(failSQL, `"__gx_parent"."id" = "__gx_local"."id"`) {
		t.Fatalf("expected aliased same-name equality, got %q", failSQL)
	}
	if !strings.Contains(failSQL, `FROM "children" AS "__gx_local"`) {
		t.Fatalf("expected local alias FROM, got %q", failSQL)
	}
}

func TestReferenceSchemaQualifiedLocalAndAliasedDiagnostics(t *testing.T) {
	setHarnessData(t, map[string][]map[string]any{
		"customers": {
			{"id": int64(1)},
			{"id": int64(2)},
		},
		"orders": {
			{"id": int64(1), "customer_id": int64(1)},
			{"id": int64(2), "customer_id": int64(9)},
			{"id": int64(3), "customer_id": int64(2)},
		},
	})
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(
		Column("customer_id").References(SchemaTable("sales", "customers"), "id"),
	).ValidateTable(
		context.Background(), db, SchemaTable("sales", "orders"),
		WithDialect(Postgres()), WithKey("id"), CaptureQueryDiagnostics(),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success || res.FailedCount != 1 || res.Total != 3 {
		t.Fatalf("got %#v, want one orphan of three", res)
	}
	if !reflect.DeepEqual(res.FailedKeys, []RowKey{{int64(2)}}) {
		t.Fatalf("FailedKeys = %#v", res.FailedKeys)
	}
	if !reflect.DeepEqual(res.SampleValues, []any{int64(9)}) {
		t.Fatalf("SampleValues = %#v, want [9]", res.SampleValues)
	}

	var sawCount, sawSample, sawKeys bool
	for _, q := range db.queries {
		text := q.text
		if strings.Contains(text, `COUNT(*)`) && strings.Contains(text, "NOT EXISTS") {
			sawCount = true
			if !strings.Contains(text, `"sales"."orders" AS "__gx_local"`) {
				t.Fatalf("count missing schema-qualified local alias: %q", text)
			}
			if !strings.Contains(text, `FROM "sales"."customers" AS "__gx_parent"`) {
				t.Fatalf("count missing schema-qualified parent alias: %q", text)
			}
			if strings.Count(text, "NOT EXISTS") != 1 {
				t.Fatalf("count should parse one NOT EXISTS, got %q", text)
			}
		}
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(text)), "SELECT ") &&
			!strings.Contains(text, "COUNT(*)") &&
			strings.Contains(text, "NOT EXISTS") {
			if strings.Contains(text, `SELECT "__gx_local"."customer_id" FROM`) {
				sawSample = true
				if !strings.Contains(text, `ORDER BY "__gx_local"."id"`) {
					t.Fatalf("sample ORDER BY missing local alias: %q", text)
				}
			}
			if strings.Contains(text, `SELECT "__gx_local"."id" FROM`) {
				sawKeys = true
			}
			if !strings.Contains(text, `"sales"."orders" AS "__gx_local"`) {
				t.Fatalf("diagnostic query missing aliased local FROM: %q", text)
			}
			if strings.Contains(text, `"__gx_parent"`) && strings.Contains(strings.ToUpper(text), "SELECT \"__GX_PARENT\"") {
				t.Fatalf("diagnostics must not project parent columns: %q", text)
			}
		}
	}
	if !sawCount || !sawSample || !sawKeys {
		t.Fatalf("expected aliased count/sample/key SQL, sawCount=%v sawSample=%v sawKeys=%v queries=%#v",
			sawCount, sawSample, sawKeys, db.queries)
	}
}

func TestReferenceSchemaQualifiedLocalScopeRewrite(t *testing.T) {
	setHarnessData(t, referenceFixture())
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(
		Columns("tenant_id", "customer_id").References(SchemaTable("sales", "customers"), "tenant_id", "id"),
	).ValidateTable(
		context.Background(), db, SchemaTable("sales", "orders"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant", "sales.orders.tenant_id = ?", "t1")),
		WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success || res.Total != 4 || res.FailedCount != 1 {
		t.Fatalf("got %#v, want scoped total 4 and 1 orphan", res)
	}
	if !reflect.DeepEqual(res.FailedKeys, []RowKey{{int64(3)}}) {
		t.Fatalf("FailedKeys = %#v, want [[3]]", res.FailedKeys)
	}

	var found bool
	for _, q := range db.queries {
		if strings.Contains(q.text, "NOT EXISTS") {
			if strings.Contains(q.text, `sales.__gx_local`) {
				t.Fatalf("qualified local scope was rewritten incorrectly: %q", q.text)
			}
			if strings.Contains(q.text, `"__gx_local".tenant_id = $1`) {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected schema-qualified local scope to target the local alias: %#v", db.queries)
	}

}
func TestReferenceInvalidPreflightAndContinueOnError(t *testing.T) {
	setHarnessData(t, map[string][]map[string]any{
		"customers": {{"id": int64(1)}},
		"orders":    {{"id": int64(1), "customer_id": int64(1)}},
	})
	counter := openCountingHarnessDB(t)

	_, err := NewSuite(
		Columns("tenant_id", "customer_id").References(Table("customers"), "id"),
	).ValidateTable(
		context.Background(), counter, Table("orders"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected arity preflight error")
	}
	var pf *PreflightErrors
	if !errors.As(err, &pf) {
		t.Fatalf("err = %v, want PreflightErrors", err)
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("category = %v, want invalid_config", err)
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0 before SQL", counter.queries)
	}

	counter = openCountingHarnessDB(t)
	rep, err := NewSuite(
		Column("customer_id").References(Table("customers"), "id"),
		Columns("bad-id").References(Table("customers"), "id"),
		RowCount().Equal(1),
	).ValidateTable(
		context.Background(), counter, Table("orders"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 3 {
		t.Fatalf("results = %d", len(rep.Results))
	}
	if !rep.Results[0].Success || rep.Results[0].Err != nil {
		t.Fatalf("valid reference should pass: %#v", rep.Results[0])
	}
	if rep.Results[1].Success || rep.Results[1].Err == nil || !errors.Is(rep.Results[1].Err, ErrCategoryInvalidConfig) {
		t.Fatalf("invalid reference config slot = %#v", rep.Results[1])
	}
	if !rep.Results[2].Success {
		t.Fatalf("later expectation should still run: %#v", rep.Results[2])
	}
}

func TestReferenceUnsupportedDialectPreflight(t *testing.T) {
	setHarnessData(t, map[string][]map[string]any{
		"customers": {{"id": int64(1)}},
		"orders":    {{"id": int64(1), "customer_id": int64(1)}},
	})
	counter := openCountingHarnessDB(t)

	_, err := NewSuite(
		Column("customer_id").References(Table("customers"), "id"),
	).ValidateTable(
		context.Background(), counter, Table("orders"),
		WithDialect(stubDialect{}),
	)
	if err == nil {
		t.Fatal("expected unsupported relational dialect preflight error")
	}
	if !errors.Is(err, ErrCategoryUnsupported) {
		t.Fatalf("category = %v, want unsupported", err)
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0", counter.queries)
	}

	counter = openCountingHarnessDB(t)
	rep, err := NewSuite(
		Column("customer_id").NotNull(),
		Column("customer_id").References(Table("customers"), "id"),
	).ValidateTable(
		context.Background(), counter, Table("orders"),
		WithDialect(stubDialect{}), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Results[0].Success {
		t.Fatalf("non-relational check should run: %#v", rep.Results[0])
	}
	if rep.Results[1].Success || !errors.Is(rep.Results[1].Err, ErrCategoryUnsupported) {
		t.Fatalf("reference unsupported slot = %#v", rep.Results[1])
	}
}

func TestReferenceNoDiagnosticStatementsAfterZeroFailures(t *testing.T) {
	setHarnessData(t, map[string][]map[string]any{
		"customers": {{"id": int64(1)}},
		"orders":    {{"id": int64(1), "customer_id": int64(1)}},
	})
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(
		Column("customer_id").References(Table("customers"), "id"),
	).ValidateTable(
		context.Background(), db, Table("orders"),
		WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Results[0].Success {
		t.Fatalf("expected pass: %#v", rep.Results[0])
	}
	if len(db.queries) != 2 {
		t.Fatalf("queries = %d, want total + failure count only; got %#v", len(db.queries), db.queries)
	}
}

type stubDialect struct{}

func (stubDialect) QuoteIdent(name string) (string, error) {
	if err := validateIdent(name); err != nil {
		return "", err
	}
	return `"` + name + `"`, nil
}
func (stubDialect) Placeholder(n int) string { return "?" }
func (stubDialect) StringLength(expr string) string {
	return "LENGTH(" + expr + ")"
}
