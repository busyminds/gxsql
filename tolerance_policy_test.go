package gxsql

import (
	"context"
	"errors"
	"testing"
)

func TestTolerancePolicyBelowExactAbove(t *testing.T) {
	const max = 2

	cases := []struct {
		name          string
		rows          []map[string]any
		wantFailed    int
		wantSuccess   bool
		wantTolerated bool
	}{
		{
			name: "below",
			rows: []map[string]any{
				{"id": int64(1), "age": int64(25)},
				{"id": int64(2), "age": int64(150)},
			},
			wantFailed:    1,
			wantSuccess:   true,
			wantTolerated: true,
		},
		{
			name: "exact",
			rows: []map[string]any{
				{"id": int64(1), "age": int64(25)},
				{"id": int64(2), "age": int64(150)},
				{"id": int64(3), "age": int64(200)},
			},
			wantFailed:    2,
			wantSuccess:   true,
			wantTolerated: true,
		},
		{
			name: "above",
			rows: []map[string]any{
				{"id": int64(1), "age": int64(25)},
				{"id": int64(2), "age": int64(150)},
				{"id": int64(3), "age": int64(200)},
				{"id": int64(4), "age": int64(300)},
			},
			wantFailed:    3,
			wantSuccess:   false,
			wantTolerated: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setHarnessData(t, harnessUsers(tc.rows...))
			db := openHarnessDB(t)

			rep, err := NewSuite(
				WithMaxFailedCount(max, Int("age").Between(0, 120)),
			).ValidateTable(
				context.Background(), db, Table("users"),
				WithDialect(Postgres()), WithKey("id"),
			)
			if err != nil {
				t.Fatalf("ValidateTable error = %v", err)
			}
			if len(rep.Results) != 1 {
				t.Fatalf("results len = %d, want 1", len(rep.Results))
			}
			res := rep.Results[0]
			if res.FailedCount != tc.wantFailed {
				t.Fatalf("FailedCount = %d, want %d", res.FailedCount, tc.wantFailed)
			}
			if res.Total != len(tc.rows) {
				t.Fatalf("Total = %d, want %d", res.Total, len(tc.rows))
			}
			if res.Success != tc.wantSuccess {
				t.Fatalf("Success = %v, want %v", res.Success, tc.wantSuccess)
			}
			if res.Tolerated != tc.wantTolerated {
				t.Fatalf("tolerated = %v, want %v", res.Tolerated, tc.wantTolerated)
			}
			if res.Facts.ConfiguredMaxFailedCount == nil || *res.Facts.ConfiguredMaxFailedCount != max {
				t.Fatalf("ConfiguredMaxFailedCount = %v, want %d", res.Facts.ConfiguredMaxFailedCount, max)
			}
			if res.Kind != KindBetween || res.Column != "age" {
				t.Fatalf("identity Kind=%q Column=%q", res.Kind, res.Column)
			}
			if tc.wantFailed > 0 && len(res.SampleValues) == 0 {
				t.Fatal("raw sample diagnostics must remain populated")
			}
			if tc.wantFailed > 0 && len(res.FailedKeys) == 0 {
				t.Fatal("raw failed keys must remain populated")
			}
		})
	}
}

func TestTolerancePolicyZeroAndEmpty(t *testing.T) {
	cases := []struct {
		name string
		rows []map[string]any
	}{
		{
			name: "zeroFailures",
			rows: []map[string]any{
				{"id": int64(1), "age": int64(25)},
				{"id": int64(2), "age": int64(40)},
			},
		},
		{
			name: "emptyPopulation",
			rows: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setHarnessData(t, harnessUsers(tc.rows...))
			db := openHarnessDB(t)

			rep, err := NewSuite(
				WithMaxFailedCount(3, Int("age").Between(0, 120)),
			).ValidateTable(
				context.Background(), db, Table("users"), WithDialect(Postgres()),
			)
			if err != nil {
				t.Fatalf("ValidateTable error = %v", err)
			}
			res := rep.Results[0]
			if !res.Success {
				t.Fatal("raw-zero / empty population must pass")
			}
			if res.Tolerated {
				t.Fatal("raw-zero / empty population must never be tolerated")
			}
			if res.FailedCount != 0 || res.FailedPercent != 0 {
				t.Fatalf("FailedCount=%d FailedPercent=%v, want zeros", res.FailedCount, res.FailedPercent)
			}
			if res.Total != len(tc.rows) {
				t.Fatalf("Total = %d, want %d", res.Total, len(tc.rows))
			}
			if res.Facts.ConfiguredMaxFailedCount == nil || *res.Facts.ConfiguredMaxFailedCount != 3 {
				t.Fatalf("ConfiguredMaxFailedCount = %v, want 3", res.Facts.ConfiguredMaxFailedCount)
			}
		})
	}
}

func TestTolerancePolicyErrorNeverTolerated(t *testing.T) {
	t.Run("defaultAbortsZeroReport", func(t *testing.T) {
		db := openErrorDB(t)
		rep, err := NewSuite(
			WithMaxFailedCount(5, Int("age").Between(0, 120)),
		).ValidateTable(context.Background(), db, Table("users"), WithDialect(Postgres()))
		if err == nil {
			t.Fatal("expected database error")
		}
		if len(rep.Results) != 0 {
			t.Fatalf("partial results len = %d, want 0 on execution error", len(rep.Results))
		}
	})

	t.Run("continueOnErrorRecordsFailure", func(t *testing.T) {
		db := openErrorDB(t)
		rep, err := NewSuite(
			WithMaxFailedCount(5, Int("age").Between(0, 120)),
			WithMaxFailedCount(1, String("email").NotEmpty()),
		).ValidateTable(
			context.Background(), db, Table("users"),
			WithDialect(Postgres()), ContinueOnError(),
		)
		if err != nil {
			t.Fatalf("ContinueOnError should not return top-level error, got %v", err)
		}
		if len(rep.Results) != 2 {
			t.Fatalf("results len = %d, want 2", len(rep.Results))
		}
		wantMax := []int{5, 1}
		for i, res := range rep.Results {
			if res.Err == nil {
				t.Fatalf("result[%d] Err = nil, want recorded execution error", i)
			}
			if res.Success {
				t.Fatalf("result[%d] Success = true, error must not become a policy pass", i)
			}
			if res.Tolerated {
				t.Fatalf("result[%d] tolerated = true, errors must never be tolerated", i)
			}
			if res.Facts.ConfiguredMaxFailedCount == nil || *res.Facts.ConfiguredMaxFailedCount != wantMax[i] {
				t.Fatalf("result[%d] ConfiguredMaxFailedCount = %v, want %d", i, res.Facts.ConfiguredMaxFailedCount, wantMax[i])
			}
		}
		if rep.OK() || rep.Err() == nil || len(rep.Failures()) != 2 {
			t.Fatal("report gates must treat execution errors as failures")
		}
	})
}

func TestTolerancePolicyWithIDNesting(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(150)},
	))
	db := openHarnessDB(t)

	cases := []struct {
		name string
		exp  Expectation
		id   string
	}{
		{
			name: "withIDOutside",
			exp:  WithID("age-tol", WithMaxFailedCount(1, Int("age").Between(0, 120))),
			id:   "age-tol",
		},
		{
			name: "withIDInside",
			exp:  WithMaxFailedCount(1, WithID("age-tol", Int("age").Between(0, 120))),
			id:   "age-tol",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := NewSuite(tc.exp).ValidateTable(
				context.Background(), db, Table("users"),
				WithDialect(Postgres()), WithKey("id"),
			)
			if err != nil {
				t.Fatal(err)
			}
			res := rep.Results[0]
			if res.ID != tc.id {
				t.Fatalf("ID = %q, want %q", res.ID, tc.id)
			}
			if res.Kind != KindBetween {
				t.Fatalf("Kind = %q, want %q", res.Kind, KindBetween)
			}
			if res.FailedCount != 1 || !res.Success || !res.Tolerated {
				t.Fatalf("FailedCount=%d Success=%v tolerated=%v, want 1/true/true",
					res.FailedCount, res.Success, res.Tolerated)
			}
			if res.Facts.ConfiguredMaxFailedCount == nil || *res.Facts.ConfiguredMaxFailedCount != 1 {
				t.Fatalf("ConfiguredMaxFailedCount = %v, want 1", res.Facts.ConfiguredMaxFailedCount)
			}
		})
	}
}

func TestToleranceReportGatingTolerated(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25), "email": "a@b.com"},
		map[string]any{"id": int64(2), "age": int64(150), "email": "b@b.com"},
		map[string]any{"id": int64(3), "age": int64(40), "email": "c@b.com"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(
		WithMaxFailedCount(1, Int("age").Between(0, 120)),
		Column("email").Unique(),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatalf("ValidateTable error = %v", err)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(rep.Results))
	}

	tol := rep.Results[0]
	if tol.FailedCount != 1 || !tol.Success || !tol.Tolerated {
		t.Fatalf("tolerated result FailedCount=%d Success=%v tolerated=%v",
			tol.FailedCount, tol.Success, tol.Tolerated)
	}
	if !rep.OK() {
		t.Fatal("tolerated results must count as passing for OK")
	}
	if rep.Err() != nil {
		t.Fatalf("Err() = %v, want nil when only tolerated failures remain", rep.Err())
	}
	if len(rep.Failures()) != 0 {
		t.Fatalf("Failures() len = %d, want 0 for tolerated policy passes", len(rep.Failures()))
	}
	if rep.Results[0].FailedCount != 1 {
		t.Fatal("ordered Results must retain raw FailedCount observation")
	}
}

func TestTolerancePolicyContinueOnError(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25), "email": "dup@b.com"},
		map[string]any{"id": int64(2), "age": int64(150), "email": "dup@b.com"},
		map[string]any{"id": int64(3), "age": int64(200), "email": "c@b.com"},
		map[string]any{"id": int64(4), "age": int64(300), "email": "d@b.com"},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(
		WithMaxFailedCount(1, Int("age").Between(0, 120)),  // above bound: 3 failures
		WithMaxFailedCount(2, Column("email").Unique()),    // tolerated: 2 duplicate rows
		WithMaxFailedCount(-1, String("email").NotEmpty()), // preflight config error
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"), ContinueOnError(),
	)
	if err != nil {
		t.Fatalf("ContinueOnError should not return top-level error, got %v", err)
	}
	if len(rep.Results) != 3 {
		t.Fatalf("results len = %d, want 3", len(rep.Results))
	}

	above := rep.Results[0]
	if above.Success || above.Tolerated || above.FailedCount != 3 {
		t.Fatalf("above-bound = Success=%v tolerated=%v FailedCount=%d",
			above.Success, above.Tolerated, above.FailedCount)
	}

	tol := rep.Results[1]
	if !tol.Success || !tol.Tolerated || tol.FailedCount != 2 {
		t.Fatalf("tolerated unique = Success=%v tolerated=%v FailedCount=%d",
			tol.Success, tol.Tolerated, tol.FailedCount)
	}

	cfg := rep.Results[2]
	if cfg.Success || cfg.Err == nil || cfg.Tolerated {
		t.Fatalf("config slot = Success=%v Err=%v tolerated=%v", cfg.Success, cfg.Err, cfg.Tolerated)
	}
	if !errors.Is(cfg.Err, ErrCategoryInvalidConfig) {
		t.Fatalf("config category = %v", cfg.Err)
	}

	if rep.OK() {
		t.Fatal("report must not be OK with above-bound and config failures")
	}
	failures := rep.Failures()
	if len(failures) != 2 {
		t.Fatalf("Failures() len = %d, want 2 (above-bound + config)", len(failures))
	}
	if failures[0].Name != above.Name || failures[1].Name != cfg.Name {
		t.Fatalf("Failures order = [%q, %q]", failures[0].Name, failures[1].Name)
	}
}

type separateErrorExpectation struct{}

func (separateErrorExpectation) Name() string { return "separate error" }

func (separateErrorExpectation) evaluateSQL(
	context.Context, DB, TableRef, evalOptions,
) (Result, error) {
	return Result{
		Kind:           KindBetween,
		Name:           "separate error",
		Success:        true,
		RowDenominator: RowDenominatorAvailable,
	}, errors.New("separate execution error")
}

func TestToleranceSeparateExecutionErrorNeverTolerated(t *testing.T) {
	exp := &maxFailedCountExpectation{
		max:   5,
		inner: separateErrorExpectation{},
	}

	res, err := exp.evaluateSQL(context.Background(), nil, TableRef{}, evalOptions{})
	if err == nil {
		t.Fatal("evaluateSQL error = nil, want separate execution error")
	}
	if res.Success || res.Tolerated {
		t.Fatalf("result Success=%v Tolerated=%v, want false/false", res.Success, res.Tolerated)
	}
	if res.Err == nil {
		t.Fatal("result Err = nil, want preserved execution error")
	}
	if res.Facts.ConfiguredMaxFailedCount == nil || *res.Facts.ConfiguredMaxFailedCount != 5 {
		t.Fatalf("ConfiguredMaxFailedCount = %v, want 5", res.Facts.ConfiguredMaxFailedCount)
	}
}

func TestToleranceStatementCountNoDuplicate(t *testing.T) {
	rows := []map[string]any{
		{"id": int64(1), "age": int64(25)},
		{"id": int64(2), "age": int64(150)},
		{"id": int64(3), "age": int64(200)},
	}

	countQueries := func(t *testing.T, exp Expectation) int {
		t.Helper()
		setHarnessData(t, harnessUsers(rows...))
		counter := openCountingHarnessDB(t)
		rep, err := NewSuite(exp).ValidateTable(
			context.Background(), counter, Table("users"),
			WithDialect(Postgres()), WithKey("id"),
		)
		if err != nil {
			t.Fatalf("ValidateTable error = %v", err)
		}
		if len(rep.Results) != 1 {
			t.Fatalf("results len = %d, want 1", len(rep.Results))
		}
		if counter.queries == 0 {
			t.Fatal("expected SQL statements")
		}
		return counter.queries
	}

	bare := countQueries(t, Int("age").Between(0, 120))
	decorated := countQueries(t, WithMaxFailedCount(2, Int("age").Between(0, 120)))
	if decorated != bare {
		t.Fatalf("decorated queries = %d, bare = %d; wrapper must not add or repeat SQL", decorated, bare)
	}

	setHarnessData(t, harnessUsers(rows...))
	counter := openCountingHarnessDB(t)
	rep, err := NewSuite(
		WithMaxFailedCount(2, Int("age").Between(0, 120)),
	).ValidateTable(
		context.Background(), counter, Table("users"),
		WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || !res.Tolerated || res.FailedCount != 2 {
		t.Fatalf("policy result Success=%v tolerated=%v FailedCount=%d",
			res.Success, res.Tolerated, res.FailedCount)
	}
	if counter.queries != bare {
		t.Fatalf("queries = %d, want %d (single inner evaluation)", counter.queries, bare)
	}
}
