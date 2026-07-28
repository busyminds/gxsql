package gxsql

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestWithMaxFailedCountAPI(t *testing.T) {
	exp := WithMaxFailedCount(2, Int("age").Between(0, 120))
	if exp == nil {
		t.Fatal("WithMaxFailedCount returned nil")
	}
	if got := exp.Name(); got != Int("age").Between(0, 120).Name() {
		t.Fatalf("Name() = %q, want inner display name", got)
	}

	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(150)},
		map[string]any{"id": int64(3), "age": int64(200)},
	))
	db := openHarnessDB(t)

	for _, tc := range []struct {
		name string
		exp  Expectation
	}{
		{name: "toleranceOuter", exp: WithID("age-tol", WithMaxFailedCount(2, Int("age").Between(0, 120)))},
		{name: "toleranceInner", exp: WithMaxFailedCount(2, WithID("age-tol", Int("age").Between(0, 120)))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rep, err := NewSuite(tc.exp).ValidateTable(
				context.Background(), db, Table("users"),
				WithDialect(Postgres()), WithKey("id"),
			)
			if err != nil {
				t.Fatalf("ValidateTable error = %v", err)
			}
			res := rep.Results[0]
			if !res.Success || !res.Tolerated || res.FailedCount != 2 {
				t.Fatalf("Success=%v Tolerated=%v FailedCount=%d", res.Success, res.Tolerated, res.FailedCount)
			}
			if res.ID != "age-tol" {
				t.Fatalf("ID = %q, want age-tol", res.ID)
			}
			if res.Facts.ConfiguredMaxFailedCount == nil || *res.Facts.ConfiguredMaxFailedCount != 2 {
				t.Fatalf("ConfiguredMaxFailedCount = %v, want 2", res.Facts.ConfiguredMaxFailedCount)
			}
		})
	}
}

func TestWithMaxFailedCountBoundary(t *testing.T) {
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
			wantFailed: 1, wantSuccess: true, wantTolerated: true,
		},
		{
			name: "exact",
			rows: []map[string]any{
				{"id": int64(1), "age": int64(25)},
				{"id": int64(2), "age": int64(150)},
				{"id": int64(3), "age": int64(200)},
			},
			wantFailed: 2, wantSuccess: true, wantTolerated: true,
		},
		{
			name: "above",
			rows: []map[string]any{
				{"id": int64(1), "age": int64(25)},
				{"id": int64(2), "age": int64(150)},
				{"id": int64(3), "age": int64(200)},
				{"id": int64(4), "age": int64(300)},
			},
			wantFailed: 3, wantSuccess: false, wantTolerated: false,
		},
		{
			name: "zero",
			rows: []map[string]any{
				{"id": int64(1), "age": int64(25)},
				{"id": int64(2), "age": int64(40)},
			},
			wantFailed: 0, wantSuccess: true, wantTolerated: false,
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
			res := rep.Results[0]
			if res.FailedCount != tc.wantFailed || res.Success != tc.wantSuccess || res.Tolerated != tc.wantTolerated {
				t.Fatalf("FailedCount=%d Success=%v Tolerated=%v, want %d/%v/%v",
					res.FailedCount, res.Success, res.Tolerated, tc.wantFailed, tc.wantSuccess, tc.wantTolerated)
			}
			if res.Facts.ConfiguredMaxFailedCount == nil || *res.Facts.ConfiguredMaxFailedCount != max {
				t.Fatalf("ConfiguredMaxFailedCount = %v, want %d", res.Facts.ConfiguredMaxFailedCount, max)
			}
			if tc.wantFailed > 0 {
				if len(res.SampleValues) == 0 || len(res.FailedKeys) == 0 {
					t.Fatal("raw samples and failed keys must remain intact")
				}
			}
		})
	}
}

func TestToleratedResultString(t *testing.T) {
	samples := make([]any, 12)
	keys := make([]RowKey, 12)
	for i := range samples {
		samples[i] = i
		keys[i] = RowKey{int64(i)}
	}
	res := Result{
		Name:          "email not empty",
		Success:       true,
		Tolerated:     true,
		Total:         100,
		FailedCount:   12,
		FailedPercent: 12.0,
		SampleValues:  samples,
		FailedKeys:    keys,
	}
	got := res.String()
	if !strings.HasPrefix(got, "✓ email not empty") {
		t.Fatalf("missing pass mark/name: %q", got)
	}
	if !strings.Contains(got, "tolerated") {
		t.Fatalf("missing tolerated marker: %q", got)
	}
	if !strings.Contains(got, "12/100 failed (12.0%)") {
		t.Fatalf("missing raw counts: %q", got)
	}
	if !strings.Contains(got, "e.g.") || !strings.Contains(got, "…") {
		t.Fatalf("missing capped samples/keys: %q", got)
	}

	clean := Result{Name: "id unique", Success: true, Total: 100}
	if strings.Contains(clean.String(), "tolerated") {
		t.Fatalf("clean pass must not say tolerated: %q", clean.String())
	}
}

func TestReportStringTolerated(t *testing.T) {
	rep := Report{Results: []Result{
		{Name: "clean", Success: true, Total: 10},
		{
			Name: "email not empty", Success: true, Tolerated: true,
			Total: 100, FailedCount: 2, FailedPercent: 2.0,
			SampleValues: []any{""}, FailedKeys: []RowKey{{int64(1)}},
		},
		{Name: "above", Success: false, Total: 10, FailedCount: 5, FailedPercent: 50},
	}}
	got := rep.String()
	if !strings.HasPrefix(got, "gxsql report: 2/3 expectations passed") {
		t.Fatalf("report header = %q", got)
	}
	if !strings.Contains(got, "tolerated") {
		t.Fatalf("report missing tolerated line: %q", got)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("line count = %d, want 4 in declaration order: %q", len(lines), got)
	}
}

func TestToleratedExportJSON(t *testing.T) {
	max := 2
	rep := Report{
		Target: &TableRef{Name: "users"},
		Results: []Result{{
			ID:             "age-range",
			Kind:           KindBetween,
			Name:           "age between [0,120]",
			Column:         "age",
			Success:        true,
			Tolerated:      true,
			RowDenominator: RowDenominatorAvailable,
			Total:          3,
			FailedCount:    2,
			FailedPercent:  66.66666666666667,
			SampleValues:   []any{int64(150), int64(200)},
			FailedKeys:     []RowKey{{int64(2)}, {int64(3)}},
			Facts: ResultFacts{
				ConfiguredBoundLower:     0,
				ConfiguredBoundUpper:     120,
				ConfiguredMaxFailedCount: &max,
			},
		}},
	}

	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	if dto.SchemaVersion != ExportSchemaVersion {
		t.Fatalf("schema version = %q, want %q", dto.SchemaVersion, ExportSchemaVersion)
	}
	out := dto.Results[0]
	if out.PolicyVerdict != PolicyVerdictPass || out.ExecutionOutcome != ExecutionOutcomeOK {
		t.Fatalf("verdict=%q outcome=%q", out.PolicyVerdict, out.ExecutionOutcome)
	}
	if !out.Tolerated {
		t.Fatal("exported Tolerated = false, want true")
	}
	if out.Facts == nil || out.Facts.ConfiguredMaxFailedCount == nil || *out.Facts.ConfiguredMaxFailedCount != 2 {
		t.Fatalf("exported ConfiguredMaxFailedCount = %#v", out.Facts)
	}

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"tolerated":true`)) {
		t.Fatalf("JSON missing tolerated:true: %s", data)
	}
	if !bytes.Contains(data, []byte(`"configured_max_failed_count":2`)) {
		t.Fatalf("JSON missing configured_max_failed_count: %s", data)
	}
	if bytes.Contains(data, []byte(`"samples"`)) || bytes.Contains(data, []byte(`"failed_keys"`)) {
		t.Fatalf("default export leaked diagnostics: %s", data)
	}

	clean := Result{
		Name: "clean", Success: true, RowDenominator: RowDenominatorAvailable,
		Total: 1, Kind: KindNotNull, Column: "id",
	}
	cleanDTO, err := exportResult(clean, &TableRef{Name: "users"}, exportConfig{})
	if err != nil {
		t.Fatal(err)
	}
	cleanJSON, err := json.Marshal(cleanDTO)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleanJSON, []byte(`"tolerated"`)) {
		t.Fatalf("clean pass must omit tolerated: %s", cleanJSON)
	}
	if bytes.Contains(cleanJSON, []byte(`configured_max_failed_count`)) {
		t.Fatalf("clean pass must omit configured_max_failed_count: %s", cleanJSON)
	}
}

func TestAboveBoundExportJSON(t *testing.T) {
	max := 1
	rep := Report{
		Target: &TableRef{Name: "users"},
		Results: []Result{{
			Kind:           KindBetween,
			Name:           "age between [0,120]",
			Column:         "age",
			Success:        false,
			Tolerated:      false,
			RowDenominator: RowDenominatorAvailable,
			Total:          3,
			FailedCount:    3,
			FailedPercent:  100,
			Facts: ResultFacts{
				ConfiguredBoundLower:     0,
				ConfiguredBoundUpper:     120,
				ConfiguredMaxFailedCount: &max,
			},
		}},
	}

	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	out := dto.Results[0]
	if out.PolicyVerdict != PolicyVerdictFail || out.ExecutionOutcome != ExecutionOutcomePolicyFailure {
		t.Fatalf("verdict=%q outcome=%q", out.PolicyVerdict, out.ExecutionOutcome)
	}
	if out.Tolerated {
		t.Fatal("above-bound exported Tolerated = true, want false")
	}
	if out.Facts == nil || out.Facts.ConfiguredMaxFailedCount == nil || *out.Facts.ConfiguredMaxFailedCount != 1 {
		t.Fatalf("exported ConfiguredMaxFailedCount = %#v", out.Facts)
	}

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"tolerated"`)) {
		t.Fatalf("above-bound must omit tolerated: %s", data)
	}
	if !bytes.Contains(data, []byte(`"configured_max_failed_count":1`)) {
		t.Fatalf("JSON missing configured_max_failed_count: %s", data)
	}
	if !bytes.Contains(data, []byte(`"policy_verdict":"fail"`)) {
		t.Fatalf("JSON missing policy_verdict fail: %s", data)
	}
}

func TestToleratedExportPrivacy(t *testing.T) {
	max := 1
	rep := Report{
		Target: &TableRef{Name: "users"},
		Results: []Result{{
			Kind:           KindNotEmpty,
			Name:           "email not empty",
			Column:         "email",
			Success:        true,
			Tolerated:      true,
			RowDenominator: RowDenominatorAvailable,
			Total:          10,
			FailedCount:    1,
			FailedPercent:  10,
			SampleValues:   []any{"secret@example.com"},
			FailedKeys:     []RowKey{{"secret-id"}},
			Facts:          ResultFacts{ConfiguredMaxFailedCount: &max},
			diagnostics:    &resultDiagnostics{query: "SELECT secret FROM users", args: []any{"secret-arg"}},
		}},
	}
	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret", "SELECT", "samples", "failed_keys", "diagnostics", "args"} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Fatalf("default tolerated export leaked %q in %s", forbidden, data)
		}
	}
	if !bytes.Contains(data, []byte(`"tolerated":true`)) {
		t.Fatalf("privacy export dropped tolerated flag: %s", data)
	}
	if !bytes.Contains(data, []byte(`"configured_max_failed_count":1`)) {
		t.Fatalf("privacy export dropped configured bound: %s", data)
	}
}
