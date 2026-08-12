package gxsql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestMaxFailedPercentUsesInclusiveUnroundedBoundary(t *testing.T) {
	cases := []struct {
		name       string
		failedRows int
		threshold  float64
		wantPass   bool
	}{
		{name: "below", failedRows: 1, threshold: 50, wantPass: true},
		{name: "at", failedRows: 2, threshold: 50, wantPass: true},
		{name: "above", failedRows: 3, threshold: 50, wantPass: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := make([]map[string]any, 4)
			for i := range rows {
				age := int64(25)
				if i < tc.failedRows {
					age = 150
				}
				rows[i] = map[string]any{"id": int64(i + 1), "age": age}
			}
			setHarnessData(t, harnessUsers(rows...))

			rep, err := NewSuite(
				WithPolicy(
					Int("age").Between(0, 120),
					Policy{Tolerance: MaxFailedPercent(tc.threshold)},
				),
			).ValidateTable(context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()))
			if err != nil {
				t.Fatalf("ValidateTable error = %v", err)
			}
			res := rep.Results[0]
			if res.Success != tc.wantPass {
				t.Fatalf("Success = %v, want %v", res.Success, tc.wantPass)
			}
			if res.FailedCount != tc.failedRows {
				t.Fatalf("FailedCount = %d, want %d", res.FailedCount, tc.failedRows)
			}
			if tc.failedRows > 0 && tc.wantPass != res.Tolerated {
				t.Fatalf("Tolerated = %v, want %v", res.Tolerated, tc.wantPass)
			}
			if res.Facts.ConfiguredMaxFailedPercent == nil || *res.Facts.ConfiguredMaxFailedPercent != tc.threshold {
				t.Fatalf("ConfiguredMaxFailedPercent = %v, want %v", res.Facts.ConfiguredMaxFailedPercent, tc.threshold)
			}
		})
	}
}

func TestPolicyWarningMetadataAndErrorGating(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(150)},
	))

	rep, err := NewSuite(
		WithPolicy(
			Int("age").Between(0, 120),
			Policy{
				Severity:    SeverityWarning,
				Description: "  age should be bounded  ",
				Tags:        []string{" pii ", "customer"},
			},
		),
	).ValidateTable(context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatalf("ValidateTable error = %v", err)
	}
	res := rep.Results[0]
	if res.Severity != SeverityWarning || res.Description != "age should be bounded" {
		t.Fatalf("metadata = severity %v description %q", res.Severity, res.Description)
	}
	if len(res.Tags) != 2 || res.Tags[0] != "customer" || res.Tags[1] != "pii" {
		t.Fatalf("Tags = %#v, want sorted normalized tags", res.Tags)
	}
	if res.Success || rep.Err() != nil || !rep.OK() {
		t.Fatalf("warning gating = Success:%v OK:%v Err:%v", res.Success, rep.OK(), rep.Err())
	}
}

func TestPolicyExecutionErrorAlwaysGates(t *testing.T) {
	rep, err := NewSuite(
		WithPolicy(
			Int("age").Between(0, 120),
			Policy{Severity: SeverityInfo, Tolerance: MaxFailedPercent(50)},
		),
	).ValidateTable(
		context.Background(), openErrorDB(t), Table("users"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatalf("ContinueOnError error = %v", err)
	}
	res := rep.Results[0]
	if res.Severity != SeverityInfo || res.Err == nil || res.Success || res.Tolerated {
		t.Fatalf("execution result = severity:%v err:%v success:%v tolerated:%v", res.Severity, res.Err, res.Success, res.Tolerated)
	}
	if rep.OK() || rep.Err() == nil {
		t.Fatal("execution failure must gate regardless of severity")
	}
}

func TestPolicyRejectsUnsupportedRateAndDuplicateTagsBeforeSQL(t *testing.T) {
	counter := openCountingHarnessDB(t)
	_, err := NewSuite(
		WithPolicy(RowCount().Equal(1), Policy{Tolerance: MaxFailedPercent(1)}),
		WithPolicy(Int("age").Between(0, 120), Policy{Tags: []string{"x", " x "}}),
		WithMaxFailedCount(1, WithPolicy(
			Int("age").Between(0, 120),
			Policy{Tolerance: MaxFailedPercent(50)},
		)),
	).ValidateTable(context.Background(), counter, Table("users"), WithDialect(Postgres()))
	if err == nil {
		t.Fatal("expected preflight configuration error")
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want no SQL on preflight failure", counter.queries)
	}
}

func TestMaxFailedPercentIgnoresDisplayRounding(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(150)},
		map[string]any{"id": int64(2), "age": int64(25)},
		map[string]any{"id": int64(3), "age": int64(25)},
	))

	thresholds := []struct {
		name string
		pct  float64
		pass bool
	}{
		{name: "just below", pct: 33.333332, pass: false},
		{name: "just above", pct: 33.333334, pass: true},
	}
	for _, tc := range thresholds {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := NewSuite(
				WithPolicy(
					Int("age").Between(0, 120),
					Policy{Tolerance: MaxFailedPercent(tc.pct)},
				),
			).ValidateTable(context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()))
			if err != nil {
				t.Fatal(err)
			}
			res := rep.Results[0]
			if res.Success != tc.pass {
				t.Fatalf("Success = %v, want %v (FailedPercent=%v)", res.Success, tc.pass, res.FailedPercent)
			}
			if math.Abs(res.FailedPercent-100.0/3.0) > 1e-12 {
				t.Fatalf("FailedPercent = %.12f, want %.12f", res.FailedPercent, 100.0/3.0)
			}
		})
	}
}

func TestPolicyComposesWithCountToleranceAndID(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(150)},
		map[string]any{"id": int64(2), "age": int64(25)},
	))

	forms := []Expectation{
		WithID("age-policy", WithPolicy(
			WithMaxFailedCount(1, Int("age").Between(0, 120)),
			Policy{Severity: SeverityWarning, Tags: []string{"z", "a"}},
		)),
		WithPolicy(
			WithID("age-policy", WithMaxFailedCount(1, Int("age").Between(0, 120))),
			Policy{Severity: SeverityWarning, Tags: []string{"z", "a"}},
		),
	}
	for i, exp := range forms {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			rep, err := NewSuite(exp).ValidateTable(
				context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()),
			)
			if err != nil {
				t.Fatal(err)
			}
			res := rep.Results[0]
			if res.ID != "age-policy" || res.Severity != SeverityWarning || !res.Success || !res.Tolerated {
				t.Fatalf("result = ID:%q severity:%v success:%v tolerated:%v", res.ID, res.Severity, res.Success, res.Tolerated)
			}
			if res.Facts.ConfiguredMaxFailedCount == nil || *res.Facts.ConfiguredMaxFailedCount != 1 {
				t.Fatalf("ConfiguredMaxFailedCount = %v, want 1", res.Facts.ConfiguredMaxFailedCount)
			}
		})
	}
}

func TestPolicySharedScalarPreservesDecoration(t *testing.T) {
	rows := harnessUsers(
		map[string]any{"id": int64(1), "age": int64(150), "email": ""},
		map[string]any{"id": int64(2), "age": int64(25), "email": "ok@example.com"},
	)

	run := func(t *testing.T, shared bool) Report {
		t.Helper()
		setHarnessData(t, rows)
		opts := []Option{WithDialect(Postgres()), SummaryOnly()}
		if shared {
			opts = append(opts, WithSharedScalarEvaluation())
		}
		rep, err := NewSuite(
			WithPolicy(
				Int("age").Between(0, 120),
				Policy{Severity: SeverityWarning, Description: "age", Tags: []string{"b", "a"}, Tolerance: MaxFailedPercent(50)},
			),
			WithPolicy(
				String("email").NotEmpty(),
				Policy{Severity: SeverityInfo, Description: "email"},
			),
		).ValidateTable(context.Background(), openHarnessDB(t), Table("users"), opts...)
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}

	sequential := run(t, false)
	shared := run(t, true)
	for i := range sequential.Results {
		got, want := shared.Results[i], sequential.Results[i]
		if got.Success != want.Success || got.FailedCount != want.FailedCount ||
			got.Severity != want.Severity || got.Description != want.Description {
			t.Fatalf("result[%d] shared=%+v sequential=%+v", i, got, want)
		}
		if len(got.Tags) != len(want.Tags) {
			t.Fatalf("result[%d] tags shared=%v sequential=%v", i, got.Tags, want.Tags)
		}
		if len(got.Tags) > 0 && got.Tags[0] != want.Tags[0] {
			t.Fatalf("result[%d] tags shared=%v sequential=%v", i, got.Tags, want.Tags)
		}
	}
}

func TestReportPolicyFilters(t *testing.T) {
	report := Report{Results: []Result{
		{Success: false, Severity: SeverityWarning, FailedCount: 1},
		{Success: false, Severity: SeverityInfo},
		{Success: false, Severity: SeverityError},
		{Success: true, Severity: SeverityWarning, Tolerated: true, FailedCount: 1},
	}}

	if report.OK() || report.Err() == nil {
		t.Fatal("error-severity failure must gate")
	}
	if len(report.Failures()) != 3 || len(report.GatingFailures()) != 1 || len(report.PolicyFailures()) != 3 {
		t.Fatalf("filter counts failures=%d gating=%d policy=%d", len(report.Failures()), len(report.GatingFailures()), len(report.PolicyFailures()))
	}
	if len(report.Warnings()) != 2 || len(report.Infos()) != 1 || len(report.Unexpected()) != 2 || len(report.ToleratedResults()) != 1 {
		t.Fatalf("filter counts warnings=%d infos=%d unexpected=%d tolerated=%d", len(report.Warnings()), len(report.Infos()), len(report.Unexpected()), len(report.ToleratedResults()))
	}
}

func TestPolicyExportIncludesPolicyFieldsByDefault(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(150)},
		map[string]any{"id": int64(2), "age": int64(25)},
	))
	rep, err := NewSuite(
		WithPolicy(
			Int("age").Between(0, 120),
			Policy{
				Severity:    SeverityWarning,
				Description: "age quality",
				Tags:        []string{"z", "a"},
				Tolerance:   MaxFailedPercent(50),
			},
		),
	).ValidateTable(context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatal(err)
	}
	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	res := dto.Results[0]
	if res.Severity != "warning" || res.Description != "age quality" || len(res.Tags) != 2 || res.Tags[0] != "a" {
		t.Fatalf("export policy fields = severity:%q description:%q tags:%v", res.Severity, res.Description, res.Tags)
	}
	if res.Facts == nil || res.Facts.ConfiguredMaxFailedPercent == nil || *res.Facts.ConfiguredMaxFailedPercent != 50 {
		t.Fatalf("export configured percent facts = %#v", res.Facts)
	}
	if len(res.Samples) != 0 || len(res.FailedKeys) != 0 || res.Diagnostics != nil {
		t.Fatal("default policy export must remain privacy-safe")
	}
}

func TestMaxFailedPercentRejectsInvalidValues(t *testing.T) {
	for _, percent := range []float64{-1, 101, math.NaN(), math.Inf(1)} {
		t.Run(fmt.Sprintf("%v", percent), func(t *testing.T) {
			counter := openCountingHarnessDB(t)
			_, err := NewSuite(
				WithPolicy(
					Int("age").Between(0, 120),
					Policy{Tolerance: MaxFailedPercent(percent)},
				),
			).ValidateTable(context.Background(), counter, Table("users"), WithDialect(Postgres()))
			if err == nil {
				t.Fatalf("percent %v: expected configuration error", percent)
			}
			if counter.queries != 0 {
				t.Fatalf("percent %v: queries = %d, want 0", percent, counter.queries)
			}
		})
	}
}

func TestMaxFailedPercentEmptyPopulationPassesWithoutTolerance(t *testing.T) {
	setHarnessData(t, harnessUsers())
	rep, err := NewSuite(
		WithPolicy(
			Int("age").Between(0, 120),
			Policy{Tolerance: MaxFailedPercent(1)},
		),
	).ValidateTable(context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()))
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || res.Tolerated || res.Total != 0 || res.FailedCount != 0 || res.FailedPercent != 0 {
		t.Fatalf("empty result = %#v, want clean zero pass", res)
	}
}

func TestMaxFailedPercentContinueOnErrorOmitsNonFiniteConfiguredPercent(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
	))
	for _, percent := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		t.Run(fmt.Sprintf("%v", percent), func(t *testing.T) {
			counter := openCountingHarnessDB(t)
			rep, err := NewSuite(
				WithPolicy(
					Int("age").Between(0, 120),
					Policy{
						Severity:    SeverityWarning,
						Description: "  age quality  ",
						Tags:        []string{" z ", "a"},
						Tolerance:   MaxFailedPercent(percent),
					},
				),
				RowCount().Equal(1),
			).ValidateTable(
				context.Background(), counter, Table("users"),
				WithDialect(Postgres()), ContinueOnError(),
			)
			if err != nil {
				t.Fatalf("ContinueOnError top-level error = %v", err)
			}
			if len(rep.Results) != 2 {
				t.Fatalf("results len = %d, want 2", len(rep.Results))
			}
			res := rep.Results[0]
			if res.Success || res.Err == nil {
				t.Fatal("invalid max failed percent must remain configuration failure")
			}
			if !errors.Is(res.Err, ErrCategoryInvalidConfig) {
				t.Fatalf("category = %v", res.Err)
			}
			if res.Facts.ConfiguredMaxFailedPercent != nil {
				t.Fatalf("ConfiguredMaxFailedPercent = %v, want nil", res.Facts.ConfiguredMaxFailedPercent)
			}
			if res.Severity != SeverityWarning || res.Description != "age quality" {
				t.Fatalf("metadata = severity:%v description:%q", res.Severity, res.Description)
			}
			if len(res.Tags) != 2 || res.Tags[0] != "a" || res.Tags[1] != "z" {
				t.Fatalf("Tags = %#v", res.Tags)
			}
			if !rep.Results[1].Success {
				t.Fatal("later expectation should run under ContinueOnError")
			}
			if counter.queries == 0 {
				t.Fatal("valid later declarations should execute SQL under ContinueOnError")
			}
			dto, err := ExportReport(rep)
			if err != nil {
				t.Fatalf("ExportReport = %v", err)
			}
			data, err := json.Marshal(dto)
			if err != nil {
				t.Fatalf("json.Marshal = %v", err)
			}
			s := string(data)
			for _, forbidden := range []string{"NaN", "+Inf", "-Inf", "Inf", "configured_max_failed_percent"} {
				if strings.Contains(s, forbidden) {
					t.Fatalf("export leaked %q in %s", forbidden, s)
				}
			}
		})
	}
}
