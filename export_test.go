package gxsql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var updateGolden = flag.Bool("update", false, "update golden export fixtures")

func TestExportReportGoldenFixtures(t *testing.T) {
	cases := []struct {
		name    string
		report  Report
		opts    []ExportOption
		wantErr bool
	}{
		{
			name: "export_policy_pass",
			report: Report{
				Target: &TableRef{Name: "users"},
				Results: []Result{{
					ID:             "rows",
					Kind:           KindRowCountEqual,
					Name:           "row count = 2: got 2",
					Success:        true,
					RowDenominator: RowDenominatorUnavailable,
					Facts: ResultFacts{
						ObservedCount:   intPtr(2),
						ConfiguredCount: intPtr(2),
					},
				}},
			},
		},
		{
			name: "export_policy_failure",
			report: Report{
				Target: &TableRef{Name: "users"},
				Results: []Result{{
					ID:             "age-range",
					Kind:           KindBetween,
					Name:           "age between [0,120]",
					Column:         "age",
					Success:        false,
					RowDenominator: RowDenominatorAvailable,
					Total:          2,
					FailedCount:    1,
					FailedPercent:  50,
					SampleValues:   []any{int64(200)},
					FailedKeys:     []RowKey{{int64(2)}},
					Facts: ResultFacts{
						ConfiguredBoundLower: 0,
						ConfiguredBoundUpper: 120,
					},
				}},
			},
		},
		{
			name: "export_execution_failure",
			report: Report{
				Target: &TableRef{Schema: "public", Name: "users"},
				Results: []Result{{
					ID:             "email",
					Kind:           KindNotEmpty,
					Name:           "email not empty",
					Column:         "email",
					Success:        false,
					RowDenominator: RowDenominatorUnavailable,
					Err:            &CategorizedError{Category: CategoryDatabase, Err: errors.New("connection reset")},
				}},
			},
		},
		{
			name: "export_config_failure",
			report: Report{
				Results: []Result{{
					ID:             "bad-in",
					Kind:           KindIn,
					Name:           "status in (...)",
					Success:        false,
					RowDenominator: RowDenominatorUnavailable,
					Err:            newConfigError(errors.New("in requires at least one value")),
				}},
			},
		},
		{
			name: "export_table_level_unavailable",
			report: Report{
				Target: &TableRef{Name: "orders"},
				Results: []Result{{
					Kind:           KindDistinctCountEqual,
					Name:           "id distinct count = 3: got 3",
					Column:         "id",
					Success:        true,
					RowDenominator: RowDenominatorUnavailable,
					Facts: ResultFacts{
						ObservedCount:   intPtr(3),
						ConfiguredCount: intPtr(3),
					},
				}},
			},
		},
		{
			name: "export_default_suppression",
			report: Report{
				Target: &TableRef{Name: "users"},
				Results: []Result{{
					ID:             "age-range",
					Kind:           KindBetween,
					Name:           "age between [0,120]",
					Column:         "age",
					Success:        false,
					RowDenominator: RowDenominatorAvailable,
					Total:          2,
					FailedCount:    1,
					FailedPercent:  50,
					SampleValues:   []any{int64(200)},
					FailedKeys:     []RowKey{{int64(2)}},
					diagnostics:    &resultDiagnostics{query: "SELECT COUNT(*) FROM users WHERE age < $1", args: []any{0}},
				}},
			},
		},
		{
			name: "export_include_diagnostics_redacted",
			report: Report{
				Target: &TableRef{Name: "users"},
				Results: []Result{{
					ID:             "age-range",
					Kind:           KindBetween,
					Name:           "age between [0,120]",
					Column:         "age",
					Success:        false,
					RowDenominator: RowDenominatorAvailable,
					Total:          2,
					FailedCount:    1,
					FailedPercent:  50,
					SampleValues:   []any{int64(200)},
					FailedKeys:     []RowKey{{int64(2)}},
					diagnostics:    &resultDiagnostics{query: "SELECT COUNT(*) FROM users WHERE age < $1 OR age > $2", args: []any{0, 120}},
				}},
			},
			opts: []ExportOption{
				IncludeSamples(),
				IncludeFailedKeys(),
				IncludeCapturedArguments(),
				WithQueryRedactor(func(v any) (any, error) {
					return "[REDACTED SQL]", nil
				}),
			},
		},
		{
			name: "export_supported_values",
			report: Report{
				Target: &TableRef{Name: "probe"},
				Results: []Result{{
					ID:             "values",
					Kind:           KindCustom,
					Name:           "value probe",
					Success:        true,
					RowDenominator: RowDenominatorAvailable,
					Total:          1,
					SampleValues: []any{
						true,
						"text",
						int64(42),
						int64(9007199254740992),
						[]byte{0xDE, 0xAD},
						time.Date(2024, 1, 2, 3, 4, 5, 123456789, time.FixedZone("X", -8*3600)),
						RowKey{int64(1), "a"},
					},
				}},
			},
			opts: []ExportOption{IncludeSamples()},
		},
		{
			name: "export_non_finite_unsupported",
			report: Report{
				Target: &TableRef{Name: "probe"},
				Results: []Result{{
					ID:             "special",
					Kind:           KindCustom,
					Name:           "special floats",
					Success:        true,
					RowDenominator: RowDenominatorUnavailable,
					Facts:          ResultFacts{ObservedFloat: floatPtr(math.NaN())},
					SampleValues:   []any{math.NaN(), math.Inf(1), make(chan int)},
				}},
			},
			opts: []ExportOption{IncludeSamples()},
		},
		{
			name: "export_stable_ordering",
			report: Report{
				Target: &TableRef{Name: "users"},
				Results: []Result{
					{ID: "first", Kind: KindNotNull, Name: "id not null", Column: "id", Success: true, RowDenominator: RowDenominatorAvailable, Total: 3},
					{ID: "second", Kind: KindBetween, Name: "age between [0,120]", Column: "age", Success: false, RowDenominator: RowDenominatorAvailable, Total: 3, FailedCount: 1, FailedPercent: 100.0 / 3.0, Facts: ResultFacts{ConfiguredBoundLower: 0, ConfiguredBoundUpper: 120}},
					{ID: "third", Kind: KindUnique, Name: "email unique", Column: "email", Success: false, RowDenominator: RowDenominatorUnavailable, Err: &CategorizedError{Category: CategoryDatabase, Err: errors.New("timeout")}},
				},
			},
		},
		{
			name: "export_capped_diagnostics",
			report: Report{
				Target: &TableRef{Name: "users"},
				Results: []Result{{
					ID:             "age-range",
					Kind:           KindBetween,
					Name:           "age between [0,120]",
					Column:         "age",
					Success:        false,
					RowDenominator: RowDenominatorAvailable,
					Total:          10,
					FailedCount:    5,
					FailedPercent:  50,
					SampleValues:   []any{int64(1), int64(2)},
					FailedKeys:     []RowKey{{int64(1)}, {int64(2)}},
				}},
			},
			opts: []ExportOption{IncludeSamples(), IncludeFailedKeys()},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dto, err := ExportReport(tc.report, tc.opts...)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if dto.SchemaVersion != ExportSchemaVersion {
				t.Fatalf("schema_version = %q, want %q", dto.SchemaVersion, ExportSchemaVersion)
			}
			data, err := json.Marshal(dto)
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", tc.name+".json")
			if *updateGolden {
				if err := os.WriteFile(golden, data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden: %v (run with -update)", err)
			}
			if !bytes.Equal(data, want) {
				t.Fatalf("export JSON mismatch:\n got: %s\nwant: %s", data, want)
			}
		})
	}
}

func TestExportDefaultOmitsSensitiveFields(t *testing.T) {
	rep := Report{
		Target: &TableRef{Name: "users"},
		Results: []Result{{
			SampleValues: []any{"secret"},
			FailedKeys:   []RowKey{{"secret-id"}},
			diagnostics:  &resultDiagnostics{query: "SELECT secret", args: []any{"secret-arg"}},
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
	s := string(data)
	for _, forbidden := range []string{"secret", "SELECT", "samples", "failed_keys", "diagnostics", "args"} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Fatalf("default export leaked %q in %s", forbidden, s)
		}
	}
}

func TestExportRedactorErrorReturnsNoDTO(t *testing.T) {
	rep := Report{
		Results: []Result{{
			diagnostics: &resultDiagnostics{query: "SELECT 1"},
		}},
	}
	_, err := ExportReport(rep,
		IncludeCapturedDiagnostics(),
		WithQueryRedactor(func(any) (any, error) { return nil, errors.New("boom") }),
	)
	if err == nil {
		t.Fatal("expected redactor error")
	}
	if !errors.Is(err, ErrCategoryObserver) {
		t.Fatalf("got %v", err)
	}
	var dto ExportedReport
	if dto.SchemaVersion != "" {
		t.Fatal("expected zero DTO on error")
	}
}

func TestExportRedactorPanicReturnsNoDTO(t *testing.T) {
	rep := Report{
		Results: []Result{{
			SampleValues: []any{"x"},
		}},
	}
	_, err := ExportReport(rep,
		IncludeSamples(),
		WithSampleRedactor(func(any) (any, error) { panic("leak") }),
	)
	if err == nil {
		t.Fatal("expected panic to fail export")
	}
	if !errors.Is(err, ErrCategoryObserver) {
		t.Fatalf("got %v", err)
	}
}

func TestExportArgsRedactorTransformsCapturedArguments(t *testing.T) {
	rep := Report{Results: []Result{{
		diagnostics: &resultDiagnostics{query: "SELECT 1", args: []any{"secret-arg"}},
	}}}
	dto, err := ExportReport(rep,
		IncludeCapturedArguments(),
		WithArgsRedactor(func(v any) (any, error) { return "REDACTED-ARG", nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	args := dto.Results[0].Diagnostics.Args
	if len(args) != 1 {
		t.Fatalf("args len = %d, want 1", len(args))
	}
	if args[0].Value != "REDACTED-ARG" {
		t.Fatalf("args[0].Value = %#v, want REDACTED-ARG", args[0].Value)
	}
	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("secret-arg")) {
		t.Fatalf("export leaked original arg: %s", data)
	}
}

func TestExportKeyRedactorTransformsFailedKeys(t *testing.T) {
	rep := Report{Results: []Result{{
		FailedCount: 1,
		FailedKeys:  []RowKey{{"secret-key"}},
	}}}
	dto, err := ExportReport(rep,
		IncludeFailedKeys(),
		WithKeyRedactor(func(v any) (any, error) { return "REDACTED-KEY", nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	keys := dto.Results[0].FailedKeys
	if len(keys) != 1 {
		t.Fatalf("failed_keys len = %d, want 1", len(keys))
	}
	if keys[0].Value != "REDACTED-KEY" {
		t.Fatalf("failed_keys[0].Value = %#v, want REDACTED-KEY", keys[0].Value)
	}
	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("secret-key")) {
		t.Fatalf("export leaked original key: %s", data)
	}
}

func TestExportArgsRedactorErrorReturnsNoDTO(t *testing.T) {
	rep := Report{Results: []Result{{
		diagnostics: &resultDiagnostics{query: "SELECT 1", args: []any{"x"}},
	}}}
	_, err := ExportReport(rep,
		IncludeCapturedArguments(),
		WithArgsRedactor(func(any) (any, error) { return nil, errors.New("boom") }),
	)
	if err == nil {
		t.Fatal("expected redactor error")
	}
	if !errors.Is(err, ErrCategoryObserver) {
		t.Fatalf("got %v", err)
	}
}

func TestExportKeyRedactorPanicReturnsNoDTO(t *testing.T) {
	rep := Report{Results: []Result{{
		FailedKeys: []RowKey{{"x"}},
	}}}
	_, err := ExportReport(rep,
		IncludeFailedKeys(),
		WithKeyRedactor(func(any) (any, error) { panic("leak") }),
	)
	if err == nil {
		t.Fatal("expected panic to fail export")
	}
	if !errors.Is(err, ErrCategoryObserver) {
		t.Fatalf("got %v", err)
	}
}

func TestExportCapturedDiagnosticsIntegration(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(200)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), CaptureQueryDiagnostics(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Target == nil || rep.Target.Name != "users" {
		t.Fatalf("target = %#v", rep.Target)
	}
	if rep.Results[0].diagnostics == nil {
		t.Fatal("expected captured diagnostics on result")
	}

	defaultDTO, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	defaultJSON, _ := json.Marshal(defaultDTO)
	if bytes.Contains(defaultJSON, []byte("SELECT")) {
		t.Fatalf("default export leaked query: %s", defaultJSON)
	}

	redacted, err := ExportReport(rep,
		IncludeCapturedDiagnostics(),
		WithQueryRedactor(func(v any) (any, error) { return "REDACTED", nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	if redacted.Results[0].Diagnostics == nil || redacted.Results[0].Diagnostics.Query != "REDACTED" {
		t.Fatalf("diagnostics = %#v", redacted.Results[0].Diagnostics)
	}
}

func TestManualReportTargetUnavailable(t *testing.T) {
	dto, err := ExportReport(Report{Results: []Result{{Kind: KindCustom, Name: "x", Success: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if dto.Target != nil {
		t.Fatalf("target = %#v, want nil", dto.Target)
	}
}

func intPtr(v int) *int { return &v }

func floatPtr(v float64) *float64 { return &v }

func TestExportDisplayNameRedactsConfiguredBounds(t *testing.T) {
	rep := Report{
		Results: []Result{{
			Kind:   KindIn,
			Name:   "api_key in [secret]",
			Column: "api_key",
			Facts:  ResultFacts{},
		}},
	}
	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	if dto.Results[0].DisplayName != "api_key in (...)" {
		t.Fatalf("display_name = %q", dto.Results[0].DisplayName)
	}
}

func TestExportFactsIncludeConfiguredThresholdsPerRowAndAggregate(t *testing.T) {
	lo, hi := 0.0, 100.0
	rep := Report{
		Results: []Result{
			{
				Kind:   KindBetween,
				Column: "age",
				Facts: ResultFacts{
					ConfiguredBoundLower: 0,
					ConfiguredBoundUpper: 120,
				},
			},
			{
				Kind: KindAverageBetween,
				Facts: ResultFacts{
					ConfiguredFloatLower: &lo,
					ConfiguredFloatUpper: &hi,
					ObservedFloat:        floatPtr(42.5),
				},
			},
		},
	}
	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	between := dto.Results[0].Facts
	if between == nil || between.ConfiguredBoundLower == nil || between.ConfiguredBoundUpper == nil {
		t.Fatalf("between facts = %#v", between)
	}
	if between.ConfiguredBoundLower.Kind != "json_integer" || between.ConfiguredBoundUpper.Kind != "json_integer" {
		t.Fatalf("between bounds = %#v, %#v", between.ConfiguredBoundLower, between.ConfiguredBoundUpper)
	}
	avg := dto.Results[1].Facts
	if avg == nil || avg.ConfiguredFloatLower == nil || avg.ConfiguredFloatUpper == nil || avg.ObservedFloat == nil {
		t.Fatalf("aggregate facts = %#v", avg)
	}
}

func TestExportErrorMessageRedactsSensitiveDetail(t *testing.T) {
	rep := Report{Results: []Result{{
		Err: &CategorizedError{Category: CategoryDatabase, Err: errors.New("SELECT * FROM users WHERE secret = 'leak'")},
	}}}
	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	msg := dto.Results[0].Errors[0].Message
	if msg != "gxsql: database" {
		t.Fatalf("message = %q", msg)
	}
	if strings.Contains(msg, "secret") || strings.Contains(msg, "SELECT") {
		t.Fatalf("leaked sensitive detail: %q", msg)
	}
}

func TestExportDiagnosticCapsWhenSampleCapZero(t *testing.T) {
	rep := Report{
		Results: []Result{{
			Success:        false,
			RowDenominator: RowDenominatorAvailable,
			Total:          5,
			FailedCount:    3,
			SampleValues:   nil,
		}},
	}
	dto, err := ExportReport(rep, IncludeSamples())
	if err != nil {
		t.Fatal(err)
	}
	if dto.Results[0].Caps == nil || !dto.Results[0].Caps.SamplesTruncated {
		t.Fatalf("caps = %#v", dto.Results[0].Caps)
	}
	if dto.Results[0].Caps.SamplesReturned != 0 {
		t.Fatalf("samples_returned = %d, want 0", dto.Results[0].Caps.SamplesReturned)
	}
}

func TestExportOperationalErrorPolicyVerdictUnevaluated(t *testing.T) {
	rep := Report{Results: []Result{{
		Success: false,
		Err:     &CategorizedError{Category: CategoryDatabase, Err: errors.New("timeout")},
	}}}
	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	if dto.Results[0].PolicyVerdict != PolicyVerdictUnevaluated {
		t.Fatalf("policy_verdict = %q", dto.Results[0].PolicyVerdict)
	}
	if dto.Results[0].ExecutionOutcome != ExecutionOutcomeExecutionFailure {
		t.Fatalf("execution_outcome = %q", dto.Results[0].ExecutionOutcome)
	}
}

func TestExportRowLevelZeroFailedCountExplicit(t *testing.T) {
	rep := Report{Results: []Result{{
		Success:        true,
		RowDenominator: RowDenominatorAvailable,
		Total:          3,
		FailedCount:    0,
	}}}
	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	if dto.Results[0].Counts == nil || dto.Results[0].Counts.Failed == nil {
		t.Fatal("expected explicit failed count")
	}
	if *dto.Results[0].Counts.Failed != 0 {
		t.Fatalf("failed = %d", *dto.Results[0].Counts.Failed)
	}
}

func TestExportSuccessfulEvaluationOmitsFalseCaps(t *testing.T) {
	rep := Report{Results: []Result{{
		Success:        true,
		RowDenominator: RowDenominatorAvailable,
		Total:          1,
		SampleValues:   []any{"ok"},
	}}}
	dto, err := ExportReport(rep, IncludeSamples())
	if err != nil {
		t.Fatal(err)
	}
	if dto.Results[0].Caps != nil {
		t.Fatalf("caps = %#v, want nil on successful evaluation", dto.Results[0].Caps)
	}
}

func TestExportQueryRedactsTableIdentifier(t *testing.T) {
	rep := Report{
		Target: &TableRef{Schema: "public", Name: "users"},
		Results: []Result{{
			diagnostics: &resultDiagnostics{query: `SELECT COUNT(*) FROM "public"."users" WHERE age < $1`},
		}},
	}
	dto, err := ExportReport(rep, IncludeCapturedDiagnostics())
	if err != nil {
		t.Fatal(err)
	}
	q := dto.Results[0].Diagnostics.Query
	if strings.Contains(q, "users") || strings.Contains(q, "public") {
		t.Fatalf("query leaked table identity: %q", q)
	}
	if !strings.Contains(q, "<table>") {
		t.Fatalf("query = %q, want <table> placeholder", q)
	}
}

func TestExportCapturedArgumentsRequireExplicitOptIn(t *testing.T) {
	rep := Report{Results: []Result{{
		diagnostics: &resultDiagnostics{query: "SELECT 1", args: []any{"secret"}},
	}}}
	withDiag, err := ExportReport(rep, IncludeCapturedDiagnostics())
	if err != nil {
		t.Fatal(err)
	}
	if withDiag.Results[0].Diagnostics == nil || len(withDiag.Results[0].Diagnostics.Args) != 0 {
		t.Fatalf("args without opt-in = %#v", withDiag.Results[0].Diagnostics)
	}
	withArgs, err := ExportReport(rep, IncludeCapturedArguments())
	if err != nil {
		t.Fatal(err)
	}
	if len(withArgs.Results[0].Diagnostics.Args) != 1 {
		t.Fatalf("args = %#v", withArgs.Results[0].Diagnostics.Args)
	}
}

func TestExportDiagnosticQueryAndArgumentLengthCaps(t *testing.T) {
	long := strings.Repeat("x", MaxExportedQueryTextRunes+10)
	args := make([]any, MaxExportedArgumentCount+5)
	for i := range args {
		args[i] = i
	}
	rep := Report{Results: []Result{{
		diagnostics: &resultDiagnostics{query: long, args: args},
	}}}
	dto, err := ExportReport(rep, IncludeCapturedArguments())
	if err != nil {
		t.Fatal(err)
	}
	diag := dto.Results[0].Diagnostics
	if !diag.QueryTruncated {
		t.Fatal("expected query truncation flag")
	}
	if utf8.RuneCountInString(diag.Query) != MaxExportedQueryTextRunes {
		t.Fatalf("query runes = %d", utf8.RuneCountInString(diag.Query))
	}
	if !diag.ArgsTruncated || len(diag.Args) != MaxExportedArgumentCount {
		t.Fatalf("args truncated=%v len=%d", diag.ArgsTruncated, len(diag.Args))
	}
}

func TestExportTableLevelDisplayPreservesObservedSuffix(t *testing.T) {
	rep := Report{Results: []Result{{
		Kind:           KindRowCountEqual,
		Name:           "row count == 2: got 2",
		RowDenominator: RowDenominatorUnavailable,
		Facts:          ResultFacts{ObservedCount: intPtr(2), ConfiguredCount: intPtr(2)},
	}}}
	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	if dto.Results[0].DisplayName != "row count: got 2" {
		t.Fatalf("display_name = %q", dto.Results[0].DisplayName)
	}
}

func TestExportCapturedArgumentsFromValidateTableIntegration(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25)},
		map[string]any{"id": int64(2), "age": int64(200)},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Int("age").Between(0, 120)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), CaptureQueryDiagnostics(),
	)
	if err != nil {
		t.Fatal(err)
	}

	dto, err := ExportReport(rep,
		IncludeCapturedArguments(),
		WithQueryRedactor(func(v any) (any, error) { return "[REDACTED SQL]", nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	args := dto.Results[0].Diagnostics.Args
	if len(args) != 2 || args[0].Value != int64(0) || args[1].Value != int64(120) {
		t.Fatalf("args = %#v", args)
	}
}

func TestExportDiagnosticQueryCapAfterRedaction(t *testing.T) {
	longRedacted := strings.Repeat("y", MaxExportedQueryTextRunes+10)
	rep := Report{Results: []Result{{
		diagnostics: &resultDiagnostics{query: "SELECT 1"},
	}}}
	dto, err := ExportReport(rep,
		IncludeCapturedDiagnostics(),
		WithQueryRedactor(func(v any) (any, error) { return longRedacted, nil }),
	)
	if err != nil {
		t.Fatal(err)
	}
	diag := dto.Results[0].Diagnostics
	if diag == nil {
		t.Fatal("expected diagnostics")
	}
	if !diag.QueryTruncated {
		t.Fatal("expected query_truncated after redaction cap")
	}
	if utf8.RuneCountInString(diag.Query) != MaxExportedQueryTextRunes {
		t.Fatalf("query runes = %d, want %d", utf8.RuneCountInString(diag.Query), MaxExportedQueryTextRunes)
	}
}
