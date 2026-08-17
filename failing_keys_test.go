package gxsql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func collectFailingKeys(t *testing.T, iter *FailureKeyIterator) []RowKey {
	t.Helper()
	var keys []RowKey
	for iter.Next() {
		key := iter.Key()
		keys = append(keys, append(RowKey(nil), key...))
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("iterator Err: %v", err)
	}
	if err := iter.Close(); err != nil {
		t.Fatalf("iterator Close: %v", err)
	}
	return keys
}

func assertNoWriteSQL(t *testing.T, queries []recordedQuery) {
	t.Helper()
	for _, q := range queries {
		upper := strings.ToUpper(q.text)
		for _, verb := range []string{"INSERT", "UPDATE", "DELETE", "MERGE", "CREATE", "DROP", "ALTER", "TRUNCATE"} {
			if strings.Contains(upper, verb) {
				t.Fatalf("retrieval issued write/DDL verb %s in %q", verb, q.text)
			}
		}
	}
}

func TestFailingKeysCompleteCardinalityVsDefaultFailedKeysCap(t *testing.T) {
	const failCount = DefaultFailedKeysCap + 25
	rows := make([]map[string]any, failCount)
	for i := range rows {
		rows[i] = map[string]any{"id": int64(i + 1), "age": int64(200)}
	}
	setHarnessData(t, map[string][]map[string]any{"users": rows})
	db := openHarnessDB(t)
	table := Table("users")

	rep, err := NewSuite(WithID("users.age.between", Int("age").Between(0, 120))).ValidateTable(
		context.Background(), db, table,
		WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.FailedCount != failCount {
		t.Fatalf("FailedCount = %d, want %d", res.FailedCount, failCount)
	}
	if len(res.FailedKeys) != DefaultFailedKeysCap {
		t.Fatalf("FailedKeys len = %d, want default cap %d", len(res.FailedKeys), DefaultFailedKeysCap)
	}

	// FailingKeys uses selectors + dialect only; key columns come from ValidateTable.
	iter, err := FailingKeys(context.Background(), db, table, rep,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatalf("FailingKeys: %v", err)
	}
	got := collectFailingKeys(t, iter)
	if len(got) != failCount {
		t.Fatalf("FailingKeys cardinality = %d, want %d (FailedCount)", len(got), failCount)
	}
	for i, key := range got {
		want := RowKey{int64(i + 1)}
		if !reflect.DeepEqual(key, want) {
			t.Fatalf("FailingKeys[%d] = %#v, want %#v (stable order)", i, key, want)
		}
	}
	if !reflect.DeepEqual(got[:DefaultFailedKeysCap], res.FailedKeys) {
		t.Fatalf("capped FailedKeys prefix mismatch vs complete stream")
	}
}

func TestSummaryOnlyBoundedReportThenFailingKeysRetrieval(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(200)},
		map[string]any{"id": int64(3), "age": int64(300)},
		map[string]any{"id": int64(4), "age": int64(400)},
	))
	db := openHarnessDB(t)
	table := Table("users")

	rep, err := NewSuite(WithID("users.age.between", Int("age").Between(0, 120))).ValidateTable(
		context.Background(), db, table,
		WithDialect(Postgres()),
		WithKey("id"),
		SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.FailedCount != 3 {
		t.Fatalf("FailedCount = %d, want 3", res.FailedCount)
	}
	if len(res.FailedKeys) != 0 {
		t.Fatalf("SummaryOnly FailedKeys = %#v, want empty (bounded report)", res.FailedKeys)
	}

	iter, err := FailingKeys(context.Background(), db, table, rep,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatalf("FailingKeys after SummaryOnly: %v", err)
	}
	got := collectFailingKeys(t, iter)
	want := []RowKey{{int64(2)}, {int64(3)}, {int64(4)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("retrieved keys = %#v, want %#v", got, want)
	}
	if len(rep.Results[0].FailedKeys) != 0 {
		t.Fatal("retrieval must not mutate report FailedKeys")
	}
}

// TestSummaryOnlyPreservesWithKeyForFailingKeysWithoutRePassingWithKey locks Main's
// inference: SummaryOnly suppresses FailedKeys retention but preserves WithKey
// column selection so FailingKeys needs only selectors + WithDialect.
//
// CORRECTION FLAG: if FailingKeys requires an explicit WithKey option to succeed
// here, SummaryOnly/ValidateTable is not preserving key-column selection and
// CoreRetrieval must fix that rather than forcing callers to re-pass WithKey.
func TestSummaryOnlyPreservesWithKeyForFailingKeysWithoutRePassingWithKey(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(200)},
		map[string]any{"id": int64(3), "age": int64(300)},
	))
	db := openHarnessDB(t)
	table := Table("users")

	rep, err := NewSuite(WithID("users.age.between", Int("age").Between(0, 120))).ValidateTable(
		context.Background(), db, table,
		WithDialect(Postgres()),
		WithKey("id"),
		SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results[0].FailedKeys) != 0 {
		t.Fatalf("SummaryOnly must suppress retention: FailedKeys=%#v", rep.Results[0].FailedKeys)
	}
	if rep.Results[0].FailedCount != 2 {
		t.Fatalf("FailedCount = %d, want 2", rep.Results[0].FailedCount)
	}

	iter, err := FailingKeys(context.Background(), db, table, rep,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
		// intentionally no WithKey — columns must come from ValidateTable
	)
	if err != nil {
		t.Fatalf("CORRECTION FLAG: FailingKeys required WithKey after ValidateTable(WithKey, SummaryOnly): %v", err)
	}
	got := collectFailingKeys(t, iter)
	want := []RowKey{{int64(2)}, {int64(3)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("retrieved keys = %#v, want %#v", got, want)
	}
}

func TestWithFailedKeysCapZeroRemainsNonStreamingRetention(t *testing.T) {
	const failCount = DefaultFailedKeysCap + 5
	rows := make([]map[string]any, failCount)
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
	if res.FailedCount != failCount {
		t.Fatalf("FailedCount = %d, want %d", res.FailedCount, failCount)
	}
	if len(res.FailedKeys) != failCount {
		t.Fatalf("WithFailedKeysCap(0) FailedKeys len = %d, want full retention %d (non-streaming)", len(res.FailedKeys), failCount)
	}
	// Cap-zero retention is an in-report slice, not an iterator contract.
	if _, ok := any(res.FailedKeys).([]RowKey); !ok {
		t.Fatal("unlimited retention must remain []RowKey on Result")
	}
}

func TestFailingKeysSelectsStableIDAndIndexInMultiExpectationSuite(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(200), "email": ""},
		map[string]any{"id": int64(2), "age": int64(25), "email": "ok@example.com"},
		map[string]any{"id": int64(3), "age": int64(300), "email": ""},
	))
	db := openHarnessDB(t)
	table := Table("users")

	rep, err := NewSuite(
		WithID("users.age.between", Int("age").Between(0, 120)),
		WithID("users.email.not-empty", String("email").NotEmpty()),
		Column("id").NotNull(),
	).ValidateTable(
		context.Background(), db, table,
		WithDialect(Postgres()),
		WithKey("id"),
		SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 3 {
		t.Fatalf("results len = %d, want 3", len(rep.Results))
	}

	byID, err := FailingKeys(context.Background(), db, table, rep,
		ForResultID("users.email.not-empty"),
		WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatalf("ForResultID: %v", err)
	}
	gotID := collectFailingKeys(t, byID)
	wantEmail := []RowKey{{int64(1)}, {int64(3)}}
	if !reflect.DeepEqual(gotID, wantEmail) {
		t.Fatalf("ForResultID keys = %#v, want %#v", gotID, wantEmail)
	}

	byIndex, err := FailingKeys(context.Background(), db, table, rep,
		ForResultIndex(0),
		WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatalf("ForResultIndex: %v", err)
	}
	gotIndex := collectFailingKeys(t, byIndex)
	wantAge := []RowKey{{int64(1)}, {int64(3)}}
	if !reflect.DeepEqual(gotIndex, wantAge) {
		t.Fatalf("ForResultIndex(0) keys = %#v, want %#v", gotIndex, wantAge)
	}

	byKind, err := FailingKeys(context.Background(), db, table, rep,
		ForKind(KindNotEmpty),
		WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatalf("ForKind: %v", err)
	}
	gotKind := collectFailingKeys(t, byKind)
	if !reflect.DeepEqual(gotKind, wantEmail) {
		t.Fatalf("ForKind keys = %#v, want %#v", gotKind, wantEmail)
	}
}

func TestFailingKeysHonorsWhenEligibilityAndScopeIdentity(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "status": "shipped", "age": int64(25)},
		map[string]any{"id": int64(2), "status": "shipped", "age": int64(200)},
		map[string]any{"id": int64(3), "status": "pending", "age": int64(300)},
		map[string]any{"id": int64(4), "tenant_id": "t2", "status": "shipped", "age": int64(400)},
	))
	db := openHarnessDB(t)
	table := Table("users")

	rep, err := NewSuite(
		WithID("users.shipped.age", When(
			TrustedEligibility("status-shipped", "status = ?", "shipped"),
			Int("age").Between(0, 120),
		)),
	).ValidateTable(
		context.Background(), db, table,
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant-run", "tenant_id = ?", "t1")),
		WithKey("id"),
		SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ScopeID != "tenant-run" {
		t.Fatalf("ScopeID = %q, want tenant-run", rep.ScopeID)
	}
	res := rep.Results[0]
	if res.FailedCount != 1 {
		t.Fatalf("FailedCount = %d, want 1 (scope ∩ eligibility)", res.FailedCount)
	}
	if len(res.FailedKeys) != 0 {
		t.Fatalf("SummaryOnly FailedKeys = %#v, want empty", res.FailedKeys)
	}

	iter, err := FailingKeys(context.Background(), db, table, rep,
		ForResultID("users.shipped.age"),
		WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatalf("FailingKeys: %v", err)
	}
	got := collectFailingKeys(t, iter)
	want := []RowKey{{int64(2)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("retrieved keys = %#v, want %#v (eligible scoped failure only)", got, want)
	}
}

func TestFailingKeysCancellationCloseAndNoWrite(t *testing.T) {
	rows := make([]map[string]any, 40)
	for i := range rows {
		rows[i] = map[string]any{"id": int64(i + 1), "age": int64(200)}
	}
	setHarnessData(t, map[string][]map[string]any{"users": rows})
	db := openRecordingHarnessDB(t)
	table := Table("users")

	rep, err := NewSuite(WithID("users.age.between", Int("age").Between(0, 120))).ValidateTable(
		context.Background(), db, table,
		WithDialect(Postgres()),
		WithKey("id"),
		SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	validateQueries := len(db.queries)

	ctx, cancel := context.WithCancel(context.Background())
	iter, err := FailingKeys(ctx, db, table, rep,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatalf("FailingKeys: %v", err)
	}

	if !iter.Next() {
		_ = iter.Close()
		t.Fatal("expected at least one failing key before cancel")
	}
	cancel()

	sawCancel := false
	for iter.Next() {
		_ = iter.Key()
	}
	if err := iter.Err(); err != nil {
		if errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "cancel") {
			sawCancel = true
		} else {
			t.Fatalf("unexpected iterator error after cancel: %v", err)
		}
	}
	if err := iter.Close(); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Close after cancel: %v", err)
	}
	if !sawCancel && ctx.Err() == nil {
		t.Fatal("expected cancellation to surface on iterator or context")
	}

	assertNoWriteSQL(t, db.queries[validateQueries:])

	// Close is idempotent / safe after drain.
	closed, err := FailingKeys(context.Background(), db, table, rep,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !closed.Next() {
		t.Fatal("expected keys")
	}
	if err := closed.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if closed.Next() {
		t.Fatal("Next after Close must not yield more keys")
	}
	_ = closed.Close()
	assertNoWriteSQL(t, db.queries[validateQueries:])
}

func TestFailingKeysCloseAfterDrainIgnoresLaterCancellation(t *testing.T) {
	setHarnessData(t, harnessUsers(map[string]any{"id": int64(1), "age": int64(200)}))
	db := openHarnessDB(t)
	table := Table("users")
	report, err := NewSuite(WithID("users.age.between", Int("age").Between(0, 120))).ValidateTable(
		context.Background(), db, table,
		WithDialect(Postgres()),
		WithKey("id"),
		SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	iter, err := FailingKeys(ctx, db, table, report,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	for iter.Next() {
		_ = iter.Key()
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("drained iterator Err = %v", err)
	}
	cancel()
	if err := iter.Close(); err != nil {
		t.Fatalf("Close after drain and cancellation: %v", err)
	}
}

func TestFailingKeysUnsupportedTargetAndMissingKeyErrors(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(200)},
		map[string]any{"id": int64(2), "age": int64(25)},
	))
	db := openHarnessDB(t)
	table := Table("users")

	rep, err := NewSuite(
		WithID("users.row-count", RowCount().Equal(2)),
		WithID("users.age.between", Int("age").Between(0, 120)),
	).ValidateTable(
		context.Background(), db, table,
		WithDialect(Postgres()),
		WithKey("id"),
		SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = FailingKeys(context.Background(), db, table, rep,
		ForResultID("users.row-count"),
		WithDialect(Postgres()),
	)
	assertFailingKeysCategorized(t, err, CategoryUnsupported)

	_, err = FailingKeys(context.Background(), db, table, rep,
		ForResultID("missing.result.id"),
		WithDialect(Postgres()),
	)
	assertFailingKeysCategorized(t, err, CategoryInvalidConfig)

	_, err = FailingKeys(context.Background(), db, table, rep,
		ForResultIndex(99),
		WithDialect(Postgres()),
	)
	assertFailingKeysCategorized(t, err, CategoryInvalidConfig)

	_, err = FailingKeys(context.Background(), db, table, rep,
		ForResultID("users.age.between"),
		ForResultIndex(1),
		WithDialect(Postgres()),
	)
	assertFailingKeysCategorized(t, err, CategoryInvalidConfig)

	// No WithKey on ValidateTable → no preserved key columns → retrieval fails.
	noKeyRep, err := NewSuite(WithID("users.age.between", Int("age").Between(0, 120))).ValidateTable(
		context.Background(), db, table,
		WithDialect(Postgres()),
		SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = FailingKeys(context.Background(), db, table, noKeyRep,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
	)
	assertFailingKeysCategorized(t, err, CategoryInvalidConfig)

	_, err = FailingKeys(context.Background(), db, Table("orders"), rep,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
	)
	assertFailingKeysCategorized(t, err, CategoryInvalidConfig)
}

func TestFailingKeysDefaultExportAndStringPrivacy(t *testing.T) {
	const failCount = 30
	rows := make([]map[string]any, failCount)
	for i := range rows {
		rows[i] = map[string]any{
			"id":  int64(i + 1),
			"age": int64(200),
			"ssn": fmt.Sprintf("ssn-%03d", i+1),
		}
	}
	setHarnessData(t, map[string][]map[string]any{"users": rows})
	db := openHarnessDB(t)
	table := Table("users")

	rep, err := NewSuite(WithID("users.age.between", Int("age").Between(0, 120))).ValidateTable(
		context.Background(), db, table,
		WithDialect(Postgres()),
		WithKey("id", "ssn"),
		SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results[0].FailedKeys) != 0 {
		t.Fatal("SummaryOnly must keep FailedKeys empty")
	}

	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "failed_keys") || strings.Contains(string(raw), "ssn-") {
		t.Fatalf("default export must omit failed keys / PII: %s", raw)
	}

	display := rep.String()
	if strings.Contains(display, "ssn-") {
		t.Fatalf("Report.String must not dump complete key set / PII: %q", display)
	}

	iter, err := FailingKeys(context.Background(), db, table, rep,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	got := collectFailingKeys(t, iter)
	if len(got) != failCount {
		t.Fatalf("retrieved %d keys, want %d", len(got), failCount)
	}
	if len(got[0]) != 2 || got[0][1] != "ssn-001" {
		t.Fatalf("composite key order = %#v, want [id ssn]", got[0])
	}

	// Opt-in export still only sees report-retained keys (none under SummaryOnly).
	withKeys, err := ExportReport(rep, IncludeFailedKeys())
	if err != nil {
		t.Fatal(err)
	}
	rawOptIn, err := json.Marshal(withKeys)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawOptIn), "ssn-") {
		t.Fatalf("IncludeFailedKeys must not invent complete retrieval keys onto the report export: %s", rawOptIn)
	}
}

func TestFailingKeysEmptyFailuresYieldsEmptyIterator(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	db := openHarnessDB(t)
	table := Table("users")

	rep, err := NewSuite(WithID("users.age.between", Int("age").Between(0, 120))).ValidateTable(
		context.Background(), db, table,
		WithDialect(Postgres()),
		WithKey("id"),
		SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].FailedCount != 0 {
		t.Fatalf("FailedCount = %d, want 0", rep.Results[0].FailedCount)
	}

	iter, err := FailingKeys(context.Background(), db, table, rep,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatalf("FailingKeys empty failures: %v", err)
	}
	got := collectFailingKeys(t, iter)
	if len(got) != 0 {
		t.Fatalf("keys = %#v, want empty", got)
	}
}

func TestFailingKeysKeyCopyIsIndependent(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(2), "age": int64(200)},
		map[string]any{"id": int64(3), "age": int64(300)},
	))
	db := openHarnessDB(t)
	table := Table("users")

	rep, err := NewSuite(WithID("users.age.between", Int("age").Between(0, 120))).ValidateTable(
		context.Background(), db, table,
		WithDialect(Postgres()),
		WithKey("id"),
		SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	iter, err := FailingKeys(ctx, db, table, rep,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = iter.Close() }()

	if !iter.Next() {
		t.Fatal("expected first key")
	}
	first := iter.Key()
	first[0] = int64(999)
	if !iter.Next() {
		t.Fatal("expected second key")
	}
	second := iter.Key()
	if reflect.DeepEqual(second, RowKey{int64(999)}) {
		t.Fatal("Key() must return a copy; mutating prior Key must not affect later keys")
	}
	if !reflect.DeepEqual(second, RowKey{int64(3)}) {
		t.Fatalf("second key = %#v, want [3]", second)
	}
}

func assertFailingKeysCategorized(t *testing.T, err error, want ErrorCategory) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected categorized %s error, got nil", want)
	}
	var ce *CategorizedError
	if !errors.As(err, &ce) {
		t.Fatalf("errors.As(*CategorizedError) failed for %T (%v)", err, err)
	}
	if ce.Category != want {
		t.Fatalf("Category = %q, want %q (err=%v)", ce.Category, want, err)
	}
	switch want {
	case CategoryUnsupported:
		if !errors.Is(err, ErrCategoryUnsupported) {
			t.Fatalf("errors.Is(ErrCategoryUnsupported) = false for %v", err)
		}
	case CategoryInvalidConfig:
		if !errors.Is(err, ErrCategoryInvalidConfig) {
			t.Fatalf("errors.Is(ErrCategoryInvalidConfig) = false for %v", err)
		}
	case CategoryObserver:
		if !errors.Is(err, ErrCategoryObserver) {
			t.Fatalf("errors.Is(ErrCategoryObserver) = false for %v", err)
		}
	}
}

func TestFailingKeysUniqueCompositeAndReferenceExpectationMatrix(t *testing.T) {
	setHarnessData(t, map[string][]map[string]any{
		"customers": {
			{"id": int64(1)},
			{"id": int64(2)},
		},
		"orders": {
			{"id": int64(10), "customer_id": int64(1), "tenant_id": "t1", "order_id": "o1"},
			{"id": int64(11), "customer_id": int64(99), "tenant_id": "t1", "order_id": "o1"}, // orphan + composite dup
			{"id": int64(12), "customer_id": int64(2), "tenant_id": "t1", "order_id": "o1"},  // composite dup
			{"id": int64(13), "customer_id": int64(98), "tenant_id": "t2", "order_id": "o9"}, // orphan
			{"id": int64(14), "customer_id": int64(1), "tenant_id": "t2", "order_id": "unique"},
		},
		"users": {
			{"id": int64(1), "email": "dup@example.com"},
			{"id": int64(2), "email": "dup@example.com"},
			{"id": int64(3), "email": "ok@example.com"},
			{"id": int64(4), "email": nil},
		},
	})
	db := openHarnessDB(t)

	t.Run("unique", func(t *testing.T) {
		table := Table("users")
		rep, err := NewSuite(WithID("users.email.unique", Column("email").Unique())).ValidateTable(
			context.Background(), db, table,
			WithDialect(Postgres()),
			WithKey("id"),
			SummaryOnly(),
		)
		if err != nil {
			t.Fatal(err)
		}
		res := rep.Results[0]
		if res.FailedCount != 2 {
			t.Fatalf("FailedCount = %d, want 2 duplicate rows", res.FailedCount)
		}
		iter, err := FailingKeys(context.Background(), db, table, rep,
			ForResultID("users.email.unique"),
			WithDialect(Postgres()),
		)
		if err != nil {
			t.Fatalf("FailingKeys unique: %v", err)
		}
		got := collectFailingKeys(t, iter)
		want := []RowKey{{int64(1)}, {int64(2)}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unique keys = %#v, want %#v (cardinality+order+shape)", got, want)
		}
		if len(got) != res.FailedCount {
			t.Fatalf("unique cardinality = %d, want FailedCount %d", len(got), res.FailedCount)
		}
	})

	t.Run("composite_unique", func(t *testing.T) {
		table := Table("orders")
		rep, err := NewSuite(WithID("orders.tenant_order.unique", Columns("tenant_id", "order_id").Unique())).ValidateTable(
			context.Background(), db, table,
			WithDialect(Postgres()),
			WithKey("id", "tenant_id"),
			SummaryOnly(),
		)
		if err != nil {
			t.Fatal(err)
		}
		res := rep.Results[0]
		if res.FailedCount != 3 {
			t.Fatalf("FailedCount = %d, want 3 duplicate participating rows", res.FailedCount)
		}
		iter, err := FailingKeys(context.Background(), db, table, rep,
			ForResultID("orders.tenant_order.unique"),
			WithDialect(Postgres()),
		)
		if err != nil {
			t.Fatalf("FailingKeys composite unique: %v", err)
		}
		got := collectFailingKeys(t, iter)
		want := []RowKey{
			{int64(10), "t1"},
			{int64(11), "t1"},
			{int64(12), "t1"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("composite-unique keys = %#v, want %#v (cardinality+order+shape)", got, want)
		}
		if len(got) != res.FailedCount {
			t.Fatalf("composite-unique cardinality = %d, want FailedCount %d", len(got), res.FailedCount)
		}
	})

	t.Run("reference", func(t *testing.T) {
		table := Table("orders")
		rep, err := NewSuite(WithID("orders.customer.ref", Column("customer_id").References(Table("customers"), "id"))).ValidateTable(
			context.Background(), db, table,
			WithDialect(Postgres()),
			WithKey("id"),
			SummaryOnly(),
		)
		if err != nil {
			t.Fatal(err)
		}
		res := rep.Results[0]
		if res.FailedCount != 2 {
			t.Fatalf("FailedCount = %d, want 2 local orphans", res.FailedCount)
		}
		iter, err := FailingKeys(context.Background(), db, table, rep,
			ForResultID("orders.customer.ref"),
			WithDialect(Postgres()),
		)
		if err != nil {
			t.Fatalf("FailingKeys reference: %v", err)
		}
		got := collectFailingKeys(t, iter)
		want := []RowKey{{int64(11)}, {int64(13)}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("reference keys = %#v, want %#v (cardinality+order+shape)", got, want)
		}
		if len(got) != res.FailedCount {
			t.Fatalf("reference cardinality = %d, want FailedCount %d", len(got), res.FailedCount)
		}
	})
}

// TestFailingKeysWithScopeOnUnscopedReportAndMismatchRejection locks the
// asymmetric WithScope check against immutable plan.scopeID: unscoped plans
// reject supplied WithScope; scoped plans reject a mismatched identity; omitting
// WithScope reuses the captured validated scope; matching WithScope remains OK.
func TestFailingKeysWithScopeOnUnscopedReportAndMismatchRejection(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(200)},
		map[string]any{"id": int64(2), "age": int64(25)},
		map[string]any{"id": int64(3), "tenant_id": "t2", "age": int64(300)},
	))
	db := openHarnessDB(t)
	table := Table("users")

	unscoped, err := NewSuite(WithID("users.age.between", Int("age").Between(0, 120))).ValidateTable(
		context.Background(), db, table,
		WithDialect(Postgres()),
		WithKey("id"),
		SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if unscoped.ScopeID != "" {
		t.Fatalf("ScopeID = %q, want empty for unscoped report", unscoped.ScopeID)
	}

	_, err = FailingKeys(context.Background(), db, table, unscoped,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant-run", "tenant_id = ?", "t1")),
	)
	assertFailingKeysCategorized(t, err, CategoryInvalidConfig)

	scoped, err := NewSuite(WithID("users.age.between", Int("age").Between(0, 120))).ValidateTable(
		context.Background(), db, table,
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant-run", "tenant_id = ?", "t1")),
		WithKey("id"),
		SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if scoped.ScopeID != "tenant-run" {
		t.Fatalf("ScopeID = %q, want tenant-run", scoped.ScopeID)
	}

	_, err = FailingKeys(context.Background(), db, table, scoped,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("other-tenant", "tenant_id = ?", "t1")),
	)
	assertFailingKeysCategorized(t, err, CategoryInvalidConfig)

	// Omission must reuse the captured scoped plan (presence not required).
	omitted, err := FailingKeys(context.Background(), db, table, scoped,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatalf("omitted WithScope on scoped plan: %v", err)
	}
	want := []RowKey{{int64(1)}}
	gotOmitted := collectFailingKeys(t, omitted)
	if !reflect.DeepEqual(gotOmitted, want) {
		t.Fatalf("omitted WithScope keys = %#v, want %#v (reuse captured scope)", gotOmitted, want)
	}

	iter, err := FailingKeys(context.Background(), db, table, scoped,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("tenant-run", "tenant_id = ?", "t1")),
	)
	if err != nil {
		t.Fatalf("matching WithScope on scoped report: %v", err)
	}
	got := collectFailingKeys(t, iter)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped retrieval keys = %#v, want %#v", got, want)
	}
}

// TestFailingKeysMutableReportTargetCannotRedirectRetrieval locks that mutating
// the exported Report.Target pointer cannot retarget the attached failure plan.
// Retrieval with the validate-time table must still succeed after Target mutation,
// and aligning the call-site table with a mutated Target must not redirect to a
// different table's identities.
//
// CORRECTION FLAG: if mutating *Report.Target breaks FailingKeys against the
// validate-time table, CoreRetrieval is treating the mutable pointer as the
// authoritative target instead of the validate-time identity/plan.
func TestFailingKeysMutableReportTargetCannotRedirectRetrieval(t *testing.T) {
	setHarnessData(t, map[string][]map[string]any{
		"users": {
			{"id": int64(1), "age": int64(200)},
			{"id": int64(2), "age": int64(300)},
			{"id": int64(3), "age": int64(25)},
		},
		"orders": {
			{"id": int64(100), "customer_id": int64(99)},
			{"id": int64(101), "customer_id": int64(98)},
		},
		"customers": {
			{"id": int64(1)},
		},
	})
	db := openHarnessDB(t)
	users := Table("users")

	rep, err := NewSuite(WithID("users.age.between", Int("age").Between(0, 120))).ValidateTable(
		context.Background(), db, users,
		WithDialect(Postgres()),
		WithKey("id"),
		SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Target == nil || *rep.Target != users {
		t.Fatalf("Target = %#v, want users", rep.Target)
	}
	wantUsers := []RowKey{{int64(1)}, {int64(2)}}

	*rep.Target = Table("orders")

	iter, err := FailingKeys(context.Background(), db, users, rep,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatalf("CORRECTION FLAG: mutating Report.Target broke validate-time table retrieval: %v", err)
	}
	got := collectFailingKeys(t, iter)
	if !reflect.DeepEqual(got, wantUsers) {
		t.Fatalf("after Target mutation, keys = %#v, want validate-time %#v", got, wantUsers)
	}

	// Aligning the call-site table with the mutated Target must not redirect the plan.
	redirected, err := FailingKeys(context.Background(), db, Table("orders"), rep,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
	)
	if err == nil {
		gotRedirect := collectFailingKeys(t, redirected)
		if reflect.DeepEqual(gotRedirect, []RowKey{{int64(100)}, {int64(101)}}) {
			t.Fatalf("mutated Target redirected retrieval to orders keys %#v", gotRedirect)
		}
		if !reflect.DeepEqual(gotRedirect, wantUsers) {
			t.Fatalf("non-redirected keys = %#v, want validate-time %#v", gotRedirect, wantUsers)
		}
	} else {
		assertFailingKeysCategorized(t, err, CategoryInvalidConfig)
	}
}

// TestFailingKeysObserverFailureSurfacesThroughIteratorErr locks Spec 03 observer
// recovery on the retrieval path: a panicking QueryCategoryFailingKeys observer
// must surface as CategoryObserver through FailureKeyIterator.Err after drain.
//
// CORRECTION FLAG: if Next swallows finishObserve observer errors, CoreRetrieval
// must propagate them onto the iterator instead of clearing them via observed=true.
func TestFailingKeysObserverFailureSurfacesThroughIteratorErr(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(200)},
		map[string]any{"id": int64(2), "age": int64(300)},
	))
	db := openHarnessDB(t)
	table := Table("users")

	rep, err := NewSuite(WithID("users.age.between", Int("age").Between(0, 120))).ValidateTable(
		context.Background(), db, table,
		WithDialect(Postgres()),
		WithKey("id"),
		SummaryOnly(),
	)
	if err != nil {
		t.Fatal(err)
	}

	observer := ObserverFunc(func(QueryEvent) {
		panic("failing-keys observer failure")
	})
	iter, err := FailingKeys(context.Background(), db, table, rep,
		ForResultID("users.age.between"),
		WithDialect(Postgres()),
		WithObserver(observer),
	)
	if err != nil {
		assertFailingKeysCategorized(t, err, CategoryObserver)
		return
	}

	for iter.Next() {
		_ = iter.Key()
	}
	closeErr := iter.Close()
	err = iter.Err()
	if err == nil {
		err = closeErr
	}
	assertFailingKeysCategorized(t, err, CategoryObserver)
}
