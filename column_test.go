package gxsql

import (
	"context"
	"reflect"
	"testing"
)

func TestColumnEmptyTableVacuousPass(t *testing.T) {
	setHarnessData(t, harnessUsers())
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatalf("ValidateTable error: %v", err)
	}
	res := rep.Results[0]
	if !res.Success {
		t.Fatal("empty table should pass per-row column check vacuously")
	}
	if res.Total != 0 || res.FailedCount != 0 {
		t.Fatalf("Total=%d FailedCount=%d, want 0 and 0", res.Total, res.FailedCount)
	}
	if len(res.SampleValues) != 0 {
		t.Fatalf("SampleValues=%v, want empty", res.SampleValues)
	}
}

func TestIntBetweenCountsFailures(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(150)},
		map[string]any{"id": int64(3), "age": int64(10)},
		map[string]any{"id": int64(4), "age": nil},
	))
	db := openHarnessDB(t)

	suite := NewSuite(Int("age").Between(0, 120))
	rep, err := suite.ValidateTable(context.Background(), db, Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatalf("ValidateTable error: %v", err)
	}
	if len(rep.Results) != 1 {
		t.Fatalf("results len = %d", len(rep.Results))
	}
	res := rep.Results[0]
	if res.Success {
		t.Fatal("expected failure")
	}
	if res.Total != 4 {
		t.Fatalf("Total = %d, want 4", res.Total)
	}
	if res.FailedCount != 2 {
		t.Fatalf("FailedCount = %d, want 2 (out of range + null)", res.FailedCount)
	}
	if res.FailedPercent != 50 {
		t.Fatalf("FailedPercent = %v, want 50", res.FailedPercent)
	}
}

func TestStringNotEmptyFailsNullAndBlank(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "email": "a@b.com"},
		map[string]any{"id": int64(2), "email": ""},
		map[string]any{"id": int64(3), "email": nil},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(String("email").NotEmpty()).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.FailedCount != 2 {
		t.Fatalf("FailedCount = %d, want 2", res.FailedCount)
	}
}

func TestColumnIsNullAndNotNull(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "deleted_at": nil},
		map[string]any{"id": int64(2), "deleted_at": "2024-01-01"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(
		Column("deleted_at").IsNull(),
		Column("deleted_at").NotNull(),
	).ValidateTable(context.Background(), db, Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].Success || rep.Results[0].FailedCount != 1 {
		t.Fatalf("IsNull result = %#v", rep.Results[0])
	}
	if rep.Results[1].Success || rep.Results[1].FailedCount != 1 {
		t.Fatalf("NotNull result = %#v", rep.Results[1])
	}
}

func TestColumnInAndNotIn(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "active"},
		map[string]any{"id": int64(2), "status": "deleted"},
		map[string]any{"id": int64(3), "status": nil},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(
		Column("status").In("active", "pending"),
		Column("status").NotIn("deleted"),
	).ValidateTable(context.Background(), db, Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].FailedCount != 2 {
		t.Fatalf("In FailedCount = %d, want 2", rep.Results[0].FailedCount)
	}
	if rep.Results[1].FailedCount != 2 {
		t.Fatalf("NotIn FailedCount = %d, want 2", rep.Results[1].FailedCount)
	}
}

func TestStringLenEqual(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "country_code": "US"},
		map[string]any{"id": int64(2), "country_code": "USA"},
		map[string]any{"id": int64(3), "country_code": nil},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(String("country_code").LenEqual(2)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success {
		t.Fatal("expected failure")
	}
	if res.FailedCount != 2 {
		t.Fatalf("FailedCount = %d, want 2 (wrong length + null)", res.FailedCount)
	}
}

func TestStringLenBetweenExact(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "country_code": "US"},
		map[string]any{"id": int64(2), "country_code": "USA"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(String("country_code").LenBetween(2, 2)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].FailedCount != 1 {
		t.Fatalf("FailedCount = %d, want 1", rep.Results[0].FailedCount)
	}
}

func TestSampleCapLimitsSampleValues(t *testing.T) {
	rows := make([]map[string]any, 10)
	for i := range rows {
		rows[i] = map[string]any{"id": int64(i + 1), "age": int64(200)}
	}
	setHarnessData(t, map[string][]map[string]any{"users": rows})
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).WithSampleCap(3).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if len(res.SampleValues) > 3 {
		t.Fatalf("SampleValues len = %d, want <=3", len(res.SampleValues))
	}
	if res.FailedCount != 10 {
		t.Fatalf("FailedCount = %d, want 10", res.FailedCount)
	}
}

func TestKeyModeReturnsCompleteFailedKeys(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(200)},
		map[string]any{"id": int64(3), "age": int64(300)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if len(res.FailedKeys) != 2 {
		t.Fatalf("FailedKeys len = %d, want 2", len(res.FailedKeys))
	}
	want := []RowKey{{int64(2)}, {int64(3)}}
	if !reflect.DeepEqual(res.FailedKeys, want) {
		t.Fatalf("FailedKeys = %#v, want %#v", res.FailedKeys, want)
	}
}

func TestFailedKeysCapLimitsReturnedKeys(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(200)},
		map[string]any{"id": int64(3), "age": int64(300)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"), WithFailedKeysCap(1),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.FailedCount != 2 {
		t.Fatalf("FailedCount = %d, want 2", res.FailedCount)
	}
	if len(res.FailedKeys) != 1 {
		t.Fatalf("FailedKeys len = %d, want 1", len(res.FailedKeys))
	}
}

func TestSuiteWithFailedKeysCapLimitsReturnedKeys(t *testing.T) {
	rows := make([]map[string]any, 10)
	for i := range rows {
		rows[i] = map[string]any{"id": int64(i + 1), "age": int64(200)}
	}
	setHarnessData(t, map[string][]map[string]any{"users": rows})
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).WithFailedKeysCap(1).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.FailedCount != 10 {
		t.Fatalf("FailedCount = %d, want 10", res.FailedCount)
	}
	if len(res.FailedKeys) != 1 {
		t.Fatalf("FailedKeys len = %d, want 1", len(res.FailedKeys))
	}
}

func TestWithFailedKeysCapZeroUnlimited(t *testing.T) {
	rows := make([]map[string]any, 105)
	for i := range rows {
		rows[i] = map[string]any{"id": int64(i + 1), "age": int64(200)}
	}
	setHarnessData(t, map[string][]map[string]any{"users": rows})
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"), WithFailedKeysCap(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.FailedCount != 105 {
		t.Fatalf("FailedCount = %d, want 105", res.FailedCount)
	}
	if len(res.FailedKeys) != 105 {
		t.Fatalf("FailedKeys len = %d, want 105 (unlimited)", len(res.FailedKeys))
	}
}

func TestDefaultFailedKeysCapBoundsLargeFailureSets(t *testing.T) {
	rows := make([]map[string]any, 105)
	for i := range rows {
		rows[i] = map[string]any{"id": int64(i + 1), "age": int64(200)}
	}
	setHarnessData(t, map[string][]map[string]any{"users": rows})
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.FailedCount != 105 {
		t.Fatalf("FailedCount = %d, want 105", res.FailedCount)
	}
	if len(res.FailedKeys) != DefaultFailedKeysCap {
		t.Fatalf("FailedKeys len = %d, want default cap %d", len(res.FailedKeys), DefaultFailedKeysCap)
	}
}

func TestSummaryOnlyLeavesFailedKeysEmpty(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(200)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results[0].FailedKeys) != 0 {
		t.Fatalf("FailedKeys = %#v, want empty in summary mode", rep.Results[0].FailedKeys)
	}
}

func TestDefaultModeIsSummaryWithoutKeys(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(200)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results[0].FailedKeys) != 0 {
		t.Fatal("default mode should not populate FailedKeys")
	}
}
