package gxsql

import (
	"context"
	"errors"
	"testing"
)

func TestTolerancePreflightEligibleShapes(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
	))
	counter := openCountingHarnessDB(t)

	eligible := []Expectation{
		WithMaxFailedCount(1, Int("age").Between(0, 120)),
		WithMaxFailedCount(0, String("email").NotEmpty()),
		WithMaxFailedCount(2, Column("email").Unique()),
		WithMaxFailedCount(1, Column("age").NotNull()),
	}
	rep, err := NewSuite(eligible...).ValidateTable(
		context.Background(), counter, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatalf("eligible tolerance preflight failed: %v", err)
	}
	if len(rep.Results) != len(eligible) {
		t.Fatalf("results len = %d, want %d", len(rep.Results), len(eligible))
	}
	if counter.queries == 0 {
		t.Fatal("eligible declarations should execute SQL after preflight")
	}
}

func TestTolerancePreflightUnsupportedShapesFailBeforeSQL(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25), "amount": float64(1), "status": "active"},
	))

	cases := []struct {
		name string
		exp  Expectation
	}{
		{name: "rowCount", exp: WithMaxFailedCount(1, RowCount().Equal(1))},
		{name: "distinctCount", exp: WithMaxFailedCount(1, Column("status").DistinctCount().Equal(1))},
		{name: "aggregate", exp: WithMaxFailedCount(1, Float("amount").AverageBetween(0, 10))},
		{name: "aggregateBound", exp: WithMaxFailedCount(1, Float("amount").MinGreaterOrEqual(0))},
		{name: "customCount", exp: WithMaxFailedCount(1, CustomCount(
			"pending",
			TrustedCountQuery("SELECT COUNT(*) FROM {{target}} WHERE {{scope}} AND status = ?", "pending"),
		))},
		{name: "requiredColumns", exp: WithMaxFailedCount(1, RequiredColumns("id"))},
		{name: "exactColumns", exp: WithMaxFailedCount(1, ExactColumns("id"))},
		{name: "nil", exp: WithMaxFailedCount(1, nil)},
		{name: "customTest", exp: WithMaxFailedCount(1, customTestExpectation{name: "probe"})},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counter := openCountingHarnessDB(t)
			_, err := NewSuite(tc.exp).ValidateTable(
				context.Background(), counter, Table("users"), WithDialect(Postgres()),
			)
			if err == nil {
				t.Fatal("expected unsupported tolerance preflight error")
			}
			var pf *PreflightErrors
			if !errors.As(err, &pf) {
				t.Fatalf("got %T, want *PreflightErrors", err)
			}
			if !errors.Is(err, ErrCategoryInvalidConfig) {
				t.Fatalf("category = %v", err)
			}
			if counter.queries != 0 {
				t.Fatalf("queries = %d, want 0 before SQL", counter.queries)
			}
		})
	}
}

func TestTolerancePreflightInvalidBoundsNilAndNested(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
	))

	cases := []struct {
		name string
		exp  Expectation
	}{
		{name: "negative", exp: WithMaxFailedCount(-1, Int("age").Between(0, 120))},
		{name: "nilInner", exp: WithMaxFailedCount(1, nil)},
		{name: "nestedDirect", exp: WithMaxFailedCount(1, WithMaxFailedCount(1, Int("age").Between(0, 120)))},
		{name: "nestedThroughWithID", exp: WithMaxFailedCount(1, WithID("inner", WithMaxFailedCount(1, Int("age").Between(0, 120))))},
		{name: "nestedDeepWithID", exp: WithMaxFailedCount(1, WithID("a", WithID("b", WithMaxFailedCount(0, Int("age").Between(0, 120)))))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counter := openCountingHarnessDB(t)
			_, err := NewSuite(tc.exp).ValidateTable(
				context.Background(), counter, Table("users"), WithDialect(Postgres()),
			)
			if err == nil {
				t.Fatal("expected invalid tolerance preflight error")
			}
			var pf *PreflightErrors
			if !errors.As(err, &pf) {
				t.Fatalf("got %T, want *PreflightErrors", err)
			}
			if !errors.Is(err, ErrCategoryInvalidConfig) {
				t.Fatalf("category = %v", err)
			}
			if counter.queries != 0 {
				t.Fatalf("queries = %d, want 0 before SQL", counter.queries)
			}
		})
	}
}

func TestTolerancePreflightContinueOnError(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
	))
	counter := openCountingHarnessDB(t)

	rep, err := NewSuite(
		WithMaxFailedCount(1, RowCount().Equal(1)),
		WithMaxFailedCount(1, Int("age").Between(0, 120)),
		WithMaxFailedCount(-1, String("email").NotEmpty()),
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
		t.Fatal("index 0 unsupported tolerance should be configuration failure slot")
	}
	if !errors.Is(rep.Results[0].Err, ErrCategoryInvalidConfig) {
		t.Fatalf("index 0 category = %v", rep.Results[0].Err)
	}
	if !rep.Results[1].Success || rep.Results[1].Err != nil {
		t.Fatal("index 1 eligible tolerance should run and pass")
	}
	if rep.Results[1].Kind != KindBetween {
		t.Fatalf("index 1 kind = %q, want %q", rep.Results[1].Kind, KindBetween)
	}
	if rep.Results[2].Success || rep.Results[2].Err == nil {
		t.Fatal("index 2 negative bound should be configuration failure slot")
	}
	if !rep.Results[3].Success {
		t.Fatal("index 3 undecorated unique should run and pass")
	}
	if counter.queries == 0 {
		t.Fatal("valid later declarations should execute SQL under ContinueOnError")
	}
}

func TestToleranceWithIDEitherNestingPreservesIDAndKind(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
	))
	db := openHarnessDB(t)

	cases := []struct {
		name string
		exp  Expectation
		id   string
		kind ExpectationKind
	}{
		{
			name: "withIDOutside",
			exp:  WithID("age-tol", WithMaxFailedCount(1, Int("age").Between(0, 120))),
			id:   "age-tol",
			kind: KindBetween,
		},
		{
			name: "withIDInside",
			exp:  WithMaxFailedCount(1, WithID("age-tol", Int("age").Between(0, 120))),
			id:   "age-tol",
			kind: KindBetween,
		},
		{
			name: "uniqueWithIDOutside",
			exp:  WithID("email-unique", WithMaxFailedCount(0, Column("email").Unique())),
			id:   "email-unique",
			kind: KindUnique,
		},
		{
			name: "uniqueWithIDInside",
			exp:  WithMaxFailedCount(0, WithID("email-unique", Column("email").Unique())),
			id:   "email-unique",
			kind: KindUnique,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := expectationID(tc.exp); got != tc.id {
				t.Fatalf("expectationID = %q, want %q", got, tc.id)
			}
			if got := expectationKind(tc.exp); got != tc.kind {
				t.Fatalf("expectationKind = %q, want %q", got, tc.kind)
			}
			if !usesRowDenominator(tc.exp) {
				t.Fatal("decorated eligible expectation must still use row denominator")
			}

			rep, err := NewSuite(tc.exp).ValidateTable(
				context.Background(), db, Table("users"), WithDialect(Postgres()),
			)
			if err != nil {
				t.Fatal(err)
			}
			res := rep.Results[0]
			if res.ID != tc.id {
				t.Fatalf("Result.ID = %q, want %q", res.ID, tc.id)
			}
			if res.Kind != tc.kind {
				t.Fatalf("Result.Kind = %q, want %q", res.Kind, tc.kind)
			}
		})
	}
}

func TestToleranceNestedRejectedThroughWithIDOutside(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	counter := openCountingHarnessDB(t)

	_, err := NewSuite(
		WithID("dup-tol", WithMaxFailedCount(1, WithMaxFailedCount(1, Int("age").Between(0, 120)))),
	).ValidateTable(
		context.Background(), counter, Table("users"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected nested tolerance preflight error")
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("category = %v", err)
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0", counter.queries)
	}
}

func TestToleranceScopeDenominatorReuse(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(30), "email": "b@b.com"},
		map[string]any{"id": int64(3), "tenant_id": "t2", "age": int64(10), "email": "c@b.com"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(
		WithMaxFailedCount(1, Int("age").Between(0, 120)),
		WithID("email-check", WithMaxFailedCount(0, String("email").NotEmpty())),
		WithMaxFailedCount(1, WithID("email-unique", Column("email").Unique())),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(scope),
	)
	if err != nil {
		t.Fatalf("ValidateTable error = %v", err)
	}
	if len(rep.Results) != 3 {
		t.Fatalf("results len = %d, want 3", len(rep.Results))
	}

	totals := scopedDenominatorTotals(db.queries)
	if len(totals) != 1 {
		t.Fatalf("scoped denominator totals = %d, want exactly 1 shared total", len(totals))
	}
	assertScopeQuery(t, totals[0], scope, false)

	for i, res := range rep.Results {
		if res.Total != 2 {
			t.Fatalf("result[%d] Total = %d, want 2 scoped rows", i, res.Total)
		}
		if res.RowDenominator != RowDenominatorAvailable {
			t.Fatalf("result[%d] RowDenominator = %q", i, res.RowDenominator)
		}
	}
	if rep.Results[1].ID != "email-check" {
		t.Fatalf("result[1] ID = %q, want email-check", rep.Results[1].ID)
	}
	if rep.Results[2].ID != "email-unique" {
		t.Fatalf("result[2] ID = %q, want email-unique", rep.Results[2].ID)
	}
}

func TestToleranceScopeOutOfScopeIgnored(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(150), "email": "b@b.com"},
		map[string]any{"id": int64(3), "tenant_id": "t2", "age": int64(200), "email": "c@b.com"},
		map[string]any{"id": int64(4), "tenant_id": "t2", "age": int64(300), "email": "d@b.com"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openHarnessDB(t)

	unscoped, err := NewSuite(
		WithMaxFailedCount(1, Int("age").Between(0, 120)),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if unscoped.Results[0].Success || unscoped.Results[0].Tolerated || unscoped.Results[0].FailedCount != 3 {
		t.Fatalf("unscoped = %#v, want above-bound FailedCount=3", unscoped.Results[0])
	}

	scoped, err := NewSuite(
		WithMaxFailedCount(1, Int("age").Between(0, 120)),
		WithMaxFailedCount(0, Column("email").Unique()),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithScope(scope), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	age := scoped.Results[0]
	if age.Total != 2 || age.FailedCount != 1 || age.FailedPercent != 50 || !age.Success || !age.Tolerated {
		t.Fatalf("scoped age = %#v, want in-scope total=2 failed=1 tolerated pass", age)
	}
	if len(age.FailedKeys) != 1 || age.FailedKeys[0][0] != int64(2) {
		t.Fatalf("scoped age FailedKeys = %#v, want only in-scope id 2", age.FailedKeys)
	}
	unique := scoped.Results[1]
	if unique.Total != 2 || unique.FailedCount != 0 || !unique.Success || unique.Tolerated {
		t.Fatalf("scoped unique = %#v, want clean in-scope pass", unique)
	}
}

func TestToleranceScopeEmptyNotTolerated(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "nobody")
	db := openHarnessDB(t)

	rep, err := NewSuite(
		WithMaxFailedCount(0, Int("age").Between(0, 120)),
		WithMaxFailedCount(2, Column("email").Unique()),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithScope(scope), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(rep.Results))
	}
	for i, res := range rep.Results {
		if !res.Success || res.Tolerated || res.Err != nil ||
			res.Total != 0 || res.FailedCount != 0 || res.FailedPercent != 0 {
			t.Fatalf("empty-scope result[%d] = %#v, want clean zero pass without tolerance", i, res)
		}
		if res.Facts.ConfiguredMaxFailedCount == nil {
			t.Fatalf("empty-scope result[%d] missing ConfiguredMaxFailedCount", i)
		}
	}
}

func TestTolerancePreflightNoSQLOnInvalid(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	counter := openCountingHarnessDB(t)

	_, err := NewSuite(
		WithMaxFailedCount(1, RowCount().Equal(1)),
		WithMaxFailedCount(-1, Int("age").Between(0, 120)),
	).ValidateTable(
		context.Background(), counter, Table("users"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected preflight errors")
	}
	var pf *PreflightErrors
	if !errors.As(err, &pf) {
		t.Fatalf("got %T, want *PreflightErrors", err)
	}
	if len(pf.Issues) < 2 {
		t.Fatalf("issues = %d, want collected invalid declarations", len(pf.Issues))
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0 on invalid preflight without ContinueOnError", counter.queries)
	}
}

func TestToleranceUsesRowDenominatorNotWidenedByWrapper(t *testing.T) {
	t.Parallel()

	if usesRowDenominator(WithMaxFailedCount(1, RowCount().Equal(1))) {
		t.Fatal("tolerance wrapper must not widen denominator for unsupported shapes")
	}
	if !usesRowDenominator(WithMaxFailedCount(1, Int("age").Between(0, 120))) {
		t.Fatal("decorated per-row expectation must retain denominator use")
	}
	if !usesRowDenominator(WithID("u", WithMaxFailedCount(1, Column("email").Unique()))) {
		t.Fatal("WithID+tolerance unique must retain denominator use")
	}
}
