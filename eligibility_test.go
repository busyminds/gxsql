package gxsql

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestTrustedEligibilityCopiesValuesOnConstruction(t *testing.T) {
	payload := []byte("shipped")
	args := []any{payload, int64(7)}
	elig := TrustedEligibility("status-shipped", "status = ? AND priority = ?", args...)

	args[0] = "mutated"
	args[1] = int64(0)
	payload[0] = 'x'

	if elig.identity != "status-shipped" {
		t.Fatalf("identity = %q, want status-shipped", elig.identity)
	}
	if elig.predicate != "status = ? AND priority = ?" {
		t.Fatalf("predicate = %q", elig.predicate)
	}
	got, ok := elig.values[0].([]byte)
	if !ok {
		t.Fatalf("value[0] type = %T, want []byte", elig.values[0])
	}
	if string(got) != "shipped" {
		t.Fatalf("value[0] = %q, want shipped", got)
	}
	if elig.values[1] != int64(7) {
		t.Fatalf("value[1] = %v, want 7", elig.values[1])
	}
}

func TestWhenPerRowInterleavedEligibleIneligible(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "shipped", "shipped_at": "2024-01-01"},
		map[string]any{"id": int64(2), "status": "pending", "shipped_at": nil},
		map[string]any{"id": int64(3), "status": "shipped", "shipped_at": nil},
		map[string]any{"id": int64(4), "status": "pending", "shipped_at": "2024-02-01"},
		map[string]any{"id": int64(5), "status": "shipped", "shipped_at": "2024-03-01"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(
		When(
			TrustedEligibility("status-shipped", "status = ?", "shipped"),
			Column("shipped_at").NotNull(),
		),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatalf("ValidateTable: %v", err)
	}
	res := rep.Results[0]
	if res.Success {
		t.Fatal("expected eligible failure")
	}
	if res.Total != 3 {
		t.Fatalf("Total = %d, want 3 eligible rows", res.Total)
	}
	if res.FailedCount != 1 {
		t.Fatalf("FailedCount = %d, want 1 eligible failure", res.FailedCount)
	}
	wantPercent := float64(1) / float64(3) * 100
	if res.FailedPercent != wantPercent {
		t.Fatalf("FailedPercent = %v, want %v from eligible denominator", res.FailedPercent, wantPercent)
	}
	wantKeys := []RowKey{{int64(3)}}
	if !reflect.DeepEqual(res.FailedKeys, wantKeys) {
		t.Fatalf("FailedKeys = %#v, want %#v (ineligible rows must be absent)", res.FailedKeys, wantKeys)
	}
	if len(res.SampleValues) != 1 || res.SampleValues[0] != nil {
		t.Fatalf("SampleValues = %#v, want one nil from eligible failing row", res.SampleValues)
	}
	if rep.ScopeID != "" {
		t.Fatalf("ScopeID = %q, want empty without suite scope", rep.ScopeID)
	}
}

func TestWhenWithSuiteScopePreservesScopeID(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "status": "shipped", "age": int64(25)},
		map[string]any{"id": int64(2), "status": "shipped", "age": int64(200)},
		map[string]any{"id": int64(3), "status": "pending", "age": int64(300)},
		map[string]any{"id": int64(4), "tenant_id": "t2", "status": "shipped", "age": int64(400)},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(
		When(
			TrustedEligibility("status-shipped", "status = ?", "shipped"),
			Int("age").Between(0, 120),
		),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(TrustedScope(" tenant-run ", "tenant_id = ?", "t1")),
		WithKey("id"),
	)
	if err != nil {
		t.Fatalf("ValidateTable: %v", err)
	}
	if rep.ScopeID != "tenant-run" {
		t.Fatalf("ScopeID = %q, want suite identity tenant-run", rep.ScopeID)
	}
	if strings.Contains(rep.ScopeID, "shipped") || strings.Contains(rep.ScopeID, "status") {
		t.Fatalf("ScopeID leaked eligibility material: %q", rep.ScopeID)
	}

	res := rep.Results[0]
	if res.Success {
		t.Fatal("expected eligible scoped failure")
	}
	if res.Total != 2 {
		t.Fatalf("Total = %d, want 2 rows in scope ∩ eligibility", res.Total)
	}
	if res.FailedCount != 1 {
		t.Fatalf("FailedCount = %d, want 1", res.FailedCount)
	}
	wantKeys := []RowKey{{int64(2)}}
	if !reflect.DeepEqual(res.FailedKeys, wantKeys) {
		t.Fatalf("FailedKeys = %#v, want %#v", res.FailedKeys, wantKeys)
	}

	if len(db.queries) < 2 {
		t.Fatalf("queries = %d, want at least total and failed counts", len(db.queries))
	}
	for _, q := range db.queries[:2] {
		if !strings.Contains(q.text, "tenant_id") || !strings.Contains(q.text, "status") {
			t.Fatalf("query missing scope+eligibility conjuncts: %q", q.text)
		}
		if !strings.Contains(q.text, ") AND (") {
			t.Fatalf("query missing parenthesized conjunct composition: %q", q.text)
		}
	}
}

func TestWhenExportOmitsPredicateAndArgumentsByDefault(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "shipped", "shipped_at": nil},
	))
	report, err := NewSuite(
		WithID("acme.orders.shipped_at", When(
			TrustedEligibility("status-shipped", "status = ?", "shipped"),
			Column("shipped_at").NotNull(),
		)),
	).ValidateTable(context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatalf("ValidateTable: %v", err)
	}
	exported, err := ExportReport(report)
	if err != nil {
		t.Fatalf("ExportReport: %v", err)
	}
	encoded, err := json.Marshal(exported)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	output := string(encoded)
	for _, secret := range []string{"status = ?", `"shipped"`} {
		if strings.Contains(output, secret) {
			t.Fatalf("default export leaked eligibility material %q: %s", secret, output)
		}
	}
}

func TestWhenEligibleTotalsFailuresSamplesKeys(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "active", "age": int64(25)},
		map[string]any{"id": int64(2), "status": "active", "age": int64(150)},
		map[string]any{"id": int64(3), "status": "active", "age": int64(200)},
		map[string]any{"id": int64(4), "status": "inactive", "age": int64(300)},
		map[string]any{"id": int64(5), "status": "inactive", "age": nil},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(
		When(
			TrustedEligibility("active-users", "status = ?", "active"),
			Int("age").Between(0, 120),
		),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"), WithSampleCap(5),
	)
	if err != nil {
		t.Fatalf("ValidateTable: %v", err)
	}
	res := rep.Results[0]
	if res.Total != 3 || res.FailedCount != 2 {
		t.Fatalf("Total=%d FailedCount=%d, want 3 and 2", res.Total, res.FailedCount)
	}
	if res.FailedPercent != float64(2)/float64(3)*100 {
		t.Fatalf("FailedPercent = %v, want eligible ratio", res.FailedPercent)
	}
	wantKeys := []RowKey{{int64(2)}, {int64(3)}}
	if !reflect.DeepEqual(res.FailedKeys, wantKeys) {
		t.Fatalf("FailedKeys = %#v, want %#v", res.FailedKeys, wantKeys)
	}
	if len(res.SampleValues) != 2 {
		t.Fatalf("SampleValues len = %d, want 2", len(res.SampleValues))
	}
	for _, sample := range res.SampleValues {
		switch sample {
		case int64(150), int64(200):
		default:
			t.Fatalf("unexpected sample %v (ineligible ages must not appear)", sample)
		}
	}
}

func TestWhenZeroEligibleVacuousPass(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "pending", "age": int64(200)},
		map[string]any{"id": int64(2), "status": "pending", "age": nil},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(
		WithMaxFailedCount(0,
			When(
				TrustedEligibility("status-shipped", "status = ?", "shipped"),
				Int("age").Between(0, 120),
			),
		),
		WithPolicy(
			When(
				TrustedEligibility("status-shipped-pct", "status = ?", "shipped"),
				Column("age").NotNull(),
			),
			Policy{Severity: SeverityWarning, Tolerance: MaxFailedPercent(1)},
		),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatalf("ValidateTable: %v", err)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(rep.Results))
	}
	for i, res := range rep.Results {
		if !res.Success {
			t.Fatalf("result[%d] should vacuous-pass: %#v", i, res)
		}
		if res.Total != 0 || res.FailedCount != 0 || res.FailedPercent != 0 {
			t.Fatalf("result[%d] Total=%d FailedCount=%d FailedPercent=%v, want zeros", i, res.Total, res.FailedCount, res.FailedPercent)
		}
		if res.Tolerated {
			t.Fatalf("result[%d] must not be Tolerated on zero eligible rows", i)
		}
		if len(res.SampleValues) != 0 || len(res.FailedKeys) != 0 {
			t.Fatalf("result[%d] diagnostics must be empty: samples=%#v keys=%#v", i, res.SampleValues, res.FailedKeys)
		}
	}
}

func TestWhenPolicyAndToleranceMetadataOnEligibleRows(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "active", "age": int64(25)},
		map[string]any{"id": int64(2), "status": "active", "age": int64(150)},
		map[string]any{"id": int64(3), "status": "inactive", "age": int64(300)},
	))
	db := openHarnessDB(t)

	cases := []struct {
		name          string
		exp           Expectation
		wantSuccess   bool
		wantTolerated bool
		wantSeverity  Severity
		wantDesc      string
		wantTags      []string
		checkFacts    func(t *testing.T, res Result)
	}{
		{
			name: "maxFailedCountWithinEligible",
			exp: WithMaxFailedCount(1, When(
				TrustedEligibility("active", "status = ?", "active"),
				Int("age").Between(0, 120),
			)),
			wantSuccess:   true,
			wantTolerated: true,
			wantSeverity:  SeverityError,
			checkFacts: func(t *testing.T, res Result) {
				t.Helper()
				if res.Facts.ConfiguredMaxFailedCount == nil || *res.Facts.ConfiguredMaxFailedCount != 1 {
					t.Fatalf("ConfiguredMaxFailedCount = %v, want 1", res.Facts.ConfiguredMaxFailedCount)
				}
			},
		},
		{
			name: "maxFailedPercentOnEligible",
			exp: WithPolicy(
				When(
					TrustedEligibility("active", "status = ?", "active"),
					Int("age").Between(0, 120),
				),
				Policy{
					Severity:    SeverityWarning,
					Description: "active ages",
					Tags:        []string{"eligibility", "age"},
					Tolerance:   MaxFailedPercent(50),
				},
			),
			wantSuccess:   true,
			wantTolerated: true,
			wantSeverity:  SeverityWarning,
			wantDesc:      "active ages",
			wantTags:      []string{"age", "eligibility"},
			checkFacts: func(t *testing.T, res Result) {
				t.Helper()
				if res.Facts.ConfiguredMaxFailedPercent == nil || *res.Facts.ConfiguredMaxFailedPercent != 50 {
					t.Fatalf("ConfiguredMaxFailedPercent = %v, want 50", res.Facts.ConfiguredMaxFailedPercent)
				}
			},
		},
		{
			name: "withIDOutsideWhen",
			exp: WithID("age-elig", WithPolicy(
				When(
					TrustedEligibility("active", "status = ?", "active"),
					Int("age").Between(0, 120),
				),
				Policy{Severity: SeverityInfo, Tolerance: MaxFailedPercent(50)},
			)),
			wantSuccess:   true,
			wantTolerated: true,
			wantSeverity:  SeverityInfo,
			checkFacts: func(t *testing.T, res Result) {
				t.Helper()
				if res.ID != "age-elig" {
					t.Fatalf("ID = %q, want age-elig", res.ID)
				}
				if res.Kind != KindBetween {
					t.Fatalf("Kind = %q, want %q", res.Kind, KindBetween)
				}
			},
		},
		{
			name: "whenOutsideWithIDAndTolerance",
			exp: When(
				TrustedEligibility("active", "status = ?", "active"),
				WithMaxFailedCount(1, WithID("inner-age", Int("age").Between(0, 120))),
			),
			wantSuccess:   true,
			wantTolerated: true,
			wantSeverity:  SeverityError,
			checkFacts: func(t *testing.T, res Result) {
				t.Helper()
				if res.ID != "inner-age" {
					t.Fatalf("ID = %q, want inner-age", res.ID)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := NewSuite(tc.exp).ValidateTable(
				context.Background(), db, Table("users"),
				WithDialect(Postgres()), WithKey("id"),
			)
			if err != nil {
				t.Fatalf("ValidateTable: %v", err)
			}
			res := rep.Results[0]
			if res.Total != 2 || res.FailedCount != 1 {
				t.Fatalf("Total=%d FailedCount=%d, want eligible 2/1", res.Total, res.FailedCount)
			}
			if res.Success != tc.wantSuccess {
				t.Fatalf("Success = %v, want %v", res.Success, tc.wantSuccess)
			}
			if res.Tolerated != tc.wantTolerated {
				t.Fatalf("Tolerated = %v, want %v", res.Tolerated, tc.wantTolerated)
			}
			if res.Severity != tc.wantSeverity {
				t.Fatalf("Severity = %v, want %v", res.Severity, tc.wantSeverity)
			}
			if tc.wantDesc != "" && res.Description != tc.wantDesc {
				t.Fatalf("Description = %q, want %q", res.Description, tc.wantDesc)
			}
			if tc.wantTags != nil && !reflect.DeepEqual(res.Tags, tc.wantTags) {
				t.Fatalf("Tags = %#v, want %#v", res.Tags, tc.wantTags)
			}
			if len(res.FailedKeys) != 1 || !reflect.DeepEqual(res.FailedKeys[0], RowKey{int64(2)}) {
				t.Fatalf("FailedKeys = %#v, want only eligible failure id=2", res.FailedKeys)
			}
			if tc.checkFacts != nil {
				tc.checkFacts(t, res)
			}
		})
	}
}

func TestWhenUniqueCompositeAndReferenceUseEligiblePopulation(t *testing.T) {
	t.Run("unique", func(t *testing.T) {
		setHarnessData(t, harnessUsers(
			map[string]any{"id": int64(1), "status": "active", "email": "dup"},
			map[string]any{"id": int64(2), "status": "active", "email": "dup"},
			map[string]any{"id": int64(3), "status": "inactive", "email": "dup"},
			map[string]any{"id": int64(4), "status": "inactive", "email": "dup"},
			map[string]any{"id": int64(5), "status": "active", "email": "unique"},
		))
		db := openHarnessDB(t)

		rep, err := NewSuite(
			When(
				TrustedEligibility("active", "status = ?", "active"),
				Column("email").Unique(),
			),
		).ValidateTable(
			context.Background(), db, Table("users"),
			WithDialect(Postgres()), WithKey("id"),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := rep.Results[0]
		if res.Kind != KindUnique {
			t.Fatalf("Kind = %q, want %q", res.Kind, KindUnique)
		}
		if res.Success || res.Total != 3 || res.FailedCount != 2 {
			t.Fatalf("got %#v, want eligible unique failure Total=3 FailedCount=2", res)
		}
		wantKeys := []RowKey{{int64(1)}, {int64(2)}}
		if !reflect.DeepEqual(res.FailedKeys, wantKeys) {
			t.Fatalf("FailedKeys = %#v, want %#v", res.FailedKeys, wantKeys)
		}
	})

	t.Run("compositeUnique", func(t *testing.T) {
		setHarnessData(t, harnessUsers(
			map[string]any{"id": int64(1), "status": "open", "tenant_id": "t1", "order_id": "o1"},
			map[string]any{"id": int64(2), "status": "open", "tenant_id": "t1", "order_id": "o1"},
			map[string]any{"id": int64(3), "status": "closed", "tenant_id": "t1", "order_id": "o1"},
			map[string]any{"id": int64(4), "status": "closed", "tenant_id": "t1", "order_id": "o1"},
			map[string]any{"id": int64(5), "status": "open", "tenant_id": "t1", "order_id": "o2"},
		))
		db := openHarnessDB(t)

		rep, err := NewSuite(
			When(
				TrustedEligibility("open-orders", "status = ?", "open"),
				Columns("tenant_id", "order_id").Unique(),
			),
		).ValidateTable(
			context.Background(), db, Table("users"),
			WithDialect(Postgres()), WithKey("id"),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := rep.Results[0]
		if res.Kind != KindCompositeUnique {
			t.Fatalf("Kind = %q, want %q", res.Kind, KindCompositeUnique)
		}
		if res.Success || res.Total != 3 || res.FailedCount != 2 {
			t.Fatalf("got %#v, want eligible composite unique Total=3 FailedCount=2", res)
		}
		wantKeys := []RowKey{{int64(1)}, {int64(2)}}
		if !reflect.DeepEqual(res.FailedKeys, wantKeys) {
			t.Fatalf("FailedKeys = %#v, want %#v", res.FailedKeys, wantKeys)
		}
	})

	t.Run("reference", func(t *testing.T) {
		setHarnessData(t, map[string][]map[string]any{
			"customers": {
				{"id": int64(1)},
				{"id": int64(2)},
			},
			"orders": {
				{"id": int64(1), "status": "open", "customer_id": int64(1)},
				{"id": int64(2), "status": "open", "customer_id": int64(99)},
				{"id": int64(3), "status": "closed", "customer_id": int64(98)},
				{"id": int64(4), "status": "open", "customer_id": int64(2)},
				{"id": int64(5), "status": "closed", "customer_id": int64(97)},
			},
		})
		db := openHarnessDB(t)

		rep, err := NewSuite(
			When(
				TrustedEligibility("open-orders", "status = ?", "open"),
				Column("customer_id").References(Table("customers"), "id"),
			),
		).ValidateTable(
			context.Background(), db, Table("orders"),
			WithDialect(Postgres()), WithKey("id"),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := rep.Results[0]
		if res.Kind != KindReference {
			t.Fatalf("Kind = %q, want %q", res.Kind, KindReference)
		}
		if res.Success || res.Total != 3 || res.FailedCount != 1 {
			t.Fatalf("got %#v, want eligible reference Total=3 FailedCount=1", res)
		}
		wantKeys := []RowKey{{int64(2)}}
		if !reflect.DeepEqual(res.FailedKeys, wantKeys) {
			t.Fatalf("FailedKeys = %#v, want %#v", res.FailedKeys, wantKeys)
		}
	})
}

func TestWhenUnsupportedShapesFailBeforeSQL(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25), "amount": float64(1), "status": "active"},
	))
	elig := TrustedEligibility("active", "status = ?", "active")

	cases := []struct {
		name string
		exp  Expectation
	}{
		{name: "rowCount", exp: When(elig, RowCount().Equal(1))},
		{name: "distinctCount", exp: When(elig, Column("status").DistinctCount().Equal(1))},
		{name: "aggregate", exp: When(elig, Float("amount").AverageBetween(0, 10))},
		{name: "aggregateBound", exp: When(elig, Float("amount").MinGreaterOrEqual(0))},
		{name: "customCount", exp: When(elig, CustomCount(
			"pending",
			TrustedCountQuery("SELECT COUNT(*) FROM {{target}} WHERE {{scope}} AND status = ?", "pending"),
		))},
		{name: "requiredColumns", exp: When(elig, RequiredColumns("id"))},
		{name: "exactColumns", exp: When(elig, ExactColumns("id"))},
		{name: "customTest", exp: When(elig, customTestExpectation{name: "probe"})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counter := openCountingHarnessDB(t)
			_, err := NewSuite(tc.exp).ValidateTable(
				context.Background(), counter, Table("users"), WithDialect(Postgres()),
			)
			if err == nil {
				t.Fatal("expected unsupported eligibility preflight error")
			}
			var pf *PreflightErrors
			if !errors.As(err, &pf) {
				t.Fatalf("got %T, want *PreflightErrors", err)
			}
			if !errors.Is(err, ErrCategoryInvalidConfig) {
				t.Fatalf("category = %v", err)
			}
			if !strings.Contains(err.Error(), "does not support eligibility") {
				t.Fatalf("error = %v, want unsupported-shape message", err)
			}
			if counter.queries != 0 {
				t.Fatalf("queries = %d, want 0 before SQL", counter.queries)
			}
		})
	}
}

func TestWhenInvalidPredicateArityNilNestedFailBeforeSQL(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "active", "age": int64(25)},
	))

	cases := []struct {
		name       string
		exp        Expectation
		wantSubstr string
		wantCat    error
	}{
		{
			name:       "blankIdentity",
			exp:        When(TrustedEligibility(" ", "status = ?", "active"), Int("age").Between(0, 120)),
			wantSubstr: "eligibility identity is required",
			wantCat:    ErrCategoryInvalidConfig,
		},
		{
			name:       "missingPredicate",
			exp:        When(TrustedEligibility("active", ""), Int("age").Between(0, 120)),
			wantSubstr: "eligibility predicate is required",
			wantCat:    ErrCategoryInvalidConfig,
		},
		{
			name:       "valuesWithoutPredicate",
			exp:        When(TrustedEligibility("active", "  ", "active"), Int("age").Between(0, 120)),
			wantSubstr: "eligibility values require a predicate",
			wantCat:    ErrCategoryInvalidConfig,
		},
		{
			name:       "arityExtra",
			exp:        When(TrustedEligibility("active", "status = ?", "a", "b"), Int("age").Between(0, 120)),
			wantSubstr: "1 placeholders but 2 values",
			wantCat:    ErrCategoryInvalidConfig,
		},
		{
			name:       "arityMissing",
			exp:        When(TrustedEligibility("active", "status = ?"), Int("age").Between(0, 120)),
			wantSubstr: "1 placeholders but 0 values",
			wantCat:    ErrCategoryInvalidConfig,
		},
		{
			name:       "literalQuestionMark",
			exp:        When(TrustedEligibility("active", "note = 'what?'"), Int("age").Between(0, 120)),
			wantSubstr: "?",
			wantCat:    ErrCategoryUnsupported,
		},
		{
			name:       "nilInner",
			exp:        When(TrustedEligibility("active", "status = ?", "active"), nil),
			wantSubstr: "nil expectation",
			wantCat:    ErrCategoryInvalidConfig,
		},
		{
			name: "nestedDirect",
			exp: When(
				TrustedEligibility("outer", "status = ?", "active"),
				When(TrustedEligibility("inner", "age IS NOT NULL"), Int("age").Between(0, 120)),
			),
			wantSubstr: "eligibility already applied",
			wantCat:    ErrCategoryInvalidConfig,
		},
		{
			name: "nestedThroughWithID",
			exp: When(
				TrustedEligibility("outer", "status = ?", "active"),
				WithID("inner", When(TrustedEligibility("inner", "age IS NOT NULL"), Int("age").Between(0, 120))),
			),
			wantSubstr: "eligibility already applied",
			wantCat:    ErrCategoryInvalidConfig,
		},
		{
			name: "nestedThroughPolicy",
			exp: When(
				TrustedEligibility("outer", "status = ?", "active"),
				WithPolicy(
					When(TrustedEligibility("inner", "age IS NOT NULL"), Int("age").Between(0, 120)),
					Policy{Severity: SeverityWarning},
				),
			),
			wantSubstr: "eligibility already applied",
			wantCat:    ErrCategoryInvalidConfig,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counter := openCountingHarnessDB(t)
			_, err := NewSuite(tc.exp).ValidateTable(
				context.Background(), counter, Table("users"), WithDialect(Postgres()),
			)
			if err == nil {
				t.Fatal("expected eligibility configuration preflight error")
			}
			var pf *PreflightErrors
			if !errors.As(err, &pf) {
				t.Fatalf("got %T (%v), want *PreflightErrors", err, err)
			}
			if !errors.Is(err, tc.wantCat) {
				t.Fatalf("category = %v, want %v", err, tc.wantCat)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("error = %v, want substring %q", err, tc.wantSubstr)
			}
			if counter.queries != 0 {
				t.Fatalf("queries = %d, want 0 before SQL", counter.queries)
			}
		})
	}
}

func TestWhenInvalidContinueOnErrorPreservesSlots(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "status": "active", "age": int64(25), "email": "a@b.com"},
	))
	counter := openCountingHarnessDB(t)

	rep, err := NewSuite(
		When(TrustedEligibility(" ", "status = ?", "active"), Int("age").Between(0, 120)),
		When(TrustedEligibility("active", "status = ?", "active"), Int("age").Between(0, 120)),
		When(TrustedEligibility("active", "status = ?", "active"), RowCount().Equal(1)),
		Column("email").Unique(),
	).ValidateTable(
		context.Background(), counter, Table("users"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatalf("ContinueOnError should not return top-level error, got %v", err)
	}
	if len(rep.Results) != 4 {
		t.Fatalf("results len = %d, want 4", len(rep.Results))
	}
	if rep.Results[0].Success || rep.Results[0].Err == nil {
		t.Fatal("index 0 invalid eligibility should be configuration failure slot")
	}
	if !errors.Is(rep.Results[0].Err, ErrCategoryInvalidConfig) {
		t.Fatalf("index 0 category = %v", rep.Results[0].Err)
	}
	if !rep.Results[1].Success || rep.Results[1].Err != nil {
		t.Fatal("index 1 valid eligible rule should run and pass")
	}
	if rep.Results[1].Kind != KindBetween || rep.Results[1].Total != 1 {
		t.Fatalf("index 1 = %#v, want eligible between pass", rep.Results[1])
	}
	if rep.Results[2].Success || rep.Results[2].Err == nil {
		t.Fatal("index 2 unsupported shape should be configuration failure slot")
	}
	if !rep.Results[3].Success {
		t.Fatal("index 3 undecorated unique should run and pass")
	}
	if counter.queries == 0 {
		t.Fatal("valid later declarations should execute SQL under ContinueOnError")
	}
}

func TestWhenPlaceholderBindingOrderPostgresAndSQLite(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "status": "shipped", "age": int64(25)},
		map[string]any{"id": int64(2), "status": "shipped", "age": int64(200)},
		map[string]any{"id": int64(3), "status": "pending", "age": int64(300)},
		map[string]any{"id": int64(4), "tenant_id": "t2", "status": "shipped", "age": int64(15)},
	))

	tests := []struct {
		name    string
		dialect Dialect
	}{
		{name: "postgres", dialect: Postgres()},
		{name: "sqlite", dialect: SQLite()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openRecordingHarnessDB(t)
			rep, err := NewSuite(
				When(
					TrustedEligibility("status-shipped", "status = ?", "shipped"),
					Int("age").Between(0, 120),
				),
			).ValidateTable(
				context.Background(), db, Table("users"),
				WithDialect(tc.dialect),
				WithScope(TrustedScope("tenant-run", "tenant_id = ?", "t1")),
				WithKey("id"),
				WithSampleCap(0),
			)
			if err != nil {
				t.Fatalf("ValidateTable: %v", err)
			}
			res := rep.Results[0]
			if res.Success || res.Total != 2 || res.FailedCount != 1 {
				t.Fatalf("result = %#v, want scoped eligible failure", res)
			}
			if rep.ScopeID != "tenant-run" {
				t.Fatalf("ScopeID = %q, want tenant-run", rep.ScopeID)
			}
			if len(db.queries) < 2 {
				t.Fatalf("queries = %d, want at least total and failed", len(db.queries))
			}

			total, failure := db.queries[0], db.queries[1]
			if got := total.args; len(got) != 2 || got[0] != "t1" || got[1] != "shipped" {
				t.Fatalf("total args = %#v, want scope then eligibility [t1 shipped]", got)
			}
			if got := failure.args; len(got) != 4 || got[0] != "t1" || got[1] != "shipped" || got[2] != 0 || got[3] != 120 {
				t.Fatalf("failure args = %#v, want [t1 shipped 0 120]", got)
			}

			switch tc.name {
			case "postgres":
				if !strings.Contains(total.text, "tenant_id = $1") || !strings.Contains(total.text, "status = $2") {
					t.Fatalf("postgres total placeholders incorrect: %q", total.text)
				}
				if !strings.Contains(failure.text, "$3") || !strings.Contains(failure.text, "$4") {
					t.Fatalf("postgres failure missing expectation placeholders: %q", failure.text)
				}
			case "sqlite":
				if strings.Contains(total.text, "$") || strings.Contains(failure.text, "$") {
					t.Fatalf("sqlite queries must keep ? placeholders: total=%q failure=%q", total.text, failure.text)
				}
				if !strings.Contains(total.text, "tenant_id = ?") || !strings.Contains(total.text, "status = ?") {
					t.Fatalf("sqlite total missing predicates: %q", total.text)
				}
			}
		})
	}
}
