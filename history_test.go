package gxsql

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMeasurementRecordsFromExportIdentityMapping(t *testing.T) {
	report := ExportedReport{
		SchemaVersion: ExportSchemaVersion,
		Results: []ExportedResult{{
			ID:               "rows.equal",
			Kind:             KindRowCountEqual,
			DisplayName:      "row count = 2: got 2",
			Severity:         "error",
			PolicyVerdict:    PolicyVerdictPass,
			ExecutionOutcome: ExecutionOutcomeOK,
			RowDenominator:   RowDenominatorUnavailable,
			Facts: &ExportedFacts{
				ObservedCount:   intPtr(2),
				ConfiguredCount: intPtr(2),
			},
		}},
	}

	recs, err := MeasurementRecordsFromExport(report)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("len = %d, want 1", len(recs))
	}
	rec := recs[0]
	if rec.ResultID != "rows.equal" {
		t.Fatalf("ResultID = %q", rec.ResultID)
	}
	if rec.SegmentID != "" {
		t.Fatalf("SegmentID = %q, want empty for unsegmented", rec.SegmentID)
	}
	if rec.Kind != KindRowCountEqual {
		t.Fatalf("Kind = %q, want %q", rec.Kind, KindRowCountEqual)
	}
	if rec.PolicyVerdict != PolicyVerdictPass {
		t.Fatalf("PolicyVerdict = %q", rec.PolicyVerdict)
	}
	if rec.ExecutionOutcome != ExecutionOutcomeOK {
		t.Fatalf("ExecutionOutcome = %q", rec.ExecutionOutcome)
	}
}

func TestMeasurementRecordsFromExportTargetAndScope(t *testing.T) {
	report := ExportedReport{
		SchemaVersion: ExportSchemaVersion,
		Target:        &ExportedTarget{Schema: "analytics", Table: "orders"},
		Scope:         &ExportedScope{ID: "tenant:42"},
		Results: []ExportedResult{{
			ID:               "orders.amount.not_null",
			Kind:             KindNotNull,
			Column:           "amount",
			Severity:         "error",
			PolicyVerdict:    PolicyVerdictPass,
			ExecutionOutcome: ExecutionOutcomeOK,
			RowDenominator:   RowDenominatorAvailable,
			Counts: &ExportedCounts{
				Total:         intPtr(10),
				Failed:        intPtr(0),
				FailedPercent: floatPtr(0),
			},
		}},
	}

	recs, err := MeasurementRecordsFromExport(report)
	if err != nil {
		t.Fatal(err)
	}
	rec := recs[0]
	if rec.TargetSchema != "analytics" || rec.TargetTable != "orders" {
		t.Fatalf("target = %q.%q", rec.TargetSchema, rec.TargetTable)
	}
	if rec.ScopeID != "tenant:42" {
		t.Fatalf("ScopeID = %q", rec.ScopeID)
	}
	if rec.Column != "amount" {
		t.Fatalf("Column = %q", rec.Column)
	}
}

func TestMeasurementRecordsFromExportSeparateTimes(t *testing.T) {
	dataTime := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	evalTime := time.Date(2026, 8, 17, 1, 2, 3, 456789000, time.UTC)
	report := ExportedReport{
		SchemaVersion:  ExportSchemaVersion,
		DataTime:       &dataTime,
		EvaluationTime: &evalTime,
		Results: []ExportedResult{{
			ID:               "freshness",
			Kind:             KindTimestampFreshSince,
			Severity:         "error",
			PolicyVerdict:    PolicyVerdictPass,
			ExecutionOutcome: ExecutionOutcomeOK,
			RowDenominator:   RowDenominatorUnavailable,
		}},
	}

	recs, err := MeasurementRecordsFromExport(report)
	if err != nil {
		t.Fatal(err)
	}
	rec := recs[0]
	if rec.DataTime == nil || !rec.DataTime.Equal(dataTime) {
		t.Fatalf("DataTime = %v, want %v", rec.DataTime, dataTime)
	}
	if rec.EvaluationTime == nil || !rec.EvaluationTime.Equal(evalTime) {
		t.Fatalf("EvaluationTime = %v, want %v", rec.EvaluationTime, evalTime)
	}
	if rec.DataTime == rec.EvaluationTime {
		t.Fatal("DataTime and EvaluationTime must be distinct pointers")
	}
	if rec.DataTime == report.DataTime || rec.EvaluationTime == report.EvaluationTime {
		t.Fatal("times must be copied, not aliased to the report")
	}

	missing := ExportedReport{
		SchemaVersion: ExportSchemaVersion,
		DataTime:      &dataTime,
		Results: []ExportedResult{{
			ID:               "only-data-time",
			Kind:             KindRowCountEqual,
			Severity:         "error",
			PolicyVerdict:    PolicyVerdictPass,
			ExecutionOutcome: ExecutionOutcomeOK,
			RowDenominator:   RowDenominatorUnavailable,
		}},
	}
	recs, err = MeasurementRecordsFromExport(missing)
	if err != nil {
		t.Fatal(err)
	}
	if recs[0].DataTime == nil || !recs[0].DataTime.Equal(dataTime) {
		t.Fatalf("DataTime = %v", recs[0].DataTime)
	}
	if recs[0].EvaluationTime != nil {
		t.Fatalf("EvaluationTime = %v, want nil when omitted", recs[0].EvaluationTime)
	}
}

func TestMeasurementRecordsFromExportStructuredFactsAndCounts(t *testing.T) {
	boundLower := NormalizedValue{Kind: "json_integer", Value: float64(0), Exact: true}
	boundUpper := NormalizedValue{Kind: "json_integer", Value: float64(120), Exact: true}
	report := ExportedReport{
		SchemaVersion: ExportSchemaVersion,
		Results: []ExportedResult{{
			ID:               "age.between",
			Kind:             KindBetween,
			Column:           "age",
			Severity:         "warning",
			Description:      "age window",
			Tags:             []string{"pii", "core"},
			PolicyVerdict:    PolicyVerdictFail,
			ExecutionOutcome: ExecutionOutcomePolicyFailure,
			Tolerated:        false,
			RowDenominator:   RowDenominatorAvailable,
			Counts: &ExportedCounts{
				Total:         intPtr(4),
				Failed:        intPtr(1),
				FailedPercent: floatPtr(25),
			},
			Facts: &ExportedFacts{
				ConfiguredBoundLower: &boundLower,
				ConfiguredBoundUpper: &boundUpper,
				ObservedCount:        intPtr(3),
			},
		}},
	}

	recs, err := MeasurementRecordsFromExport(report)
	if err != nil {
		t.Fatal(err)
	}
	rec := recs[0]
	if rec.Severity != "warning" || rec.Description != "age window" {
		t.Fatalf("severity/description = %q / %q", rec.Severity, rec.Description)
	}
	if !reflect.DeepEqual(rec.Tags, []string{"pii", "core"}) {
		t.Fatalf("Tags = %#v", rec.Tags)
	}
	if rec.Counts == nil || *rec.Counts.Total != 4 || *rec.Counts.Failed != 1 || *rec.Counts.FailedPercent != 25 {
		t.Fatalf("Counts = %#v", rec.Counts)
	}
	if rec.Facts == nil || rec.Facts.ConfiguredBoundLower == nil || rec.Facts.ConfiguredBoundUpper == nil {
		t.Fatalf("Facts = %#v", rec.Facts)
	}
	if rec.Facts.ObservedCount == nil || *rec.Facts.ObservedCount != 3 {
		t.Fatalf("ObservedCount = %#v", rec.Facts.ObservedCount)
	}

	report.Results[0].Tags[0] = "mutated"
	*report.Results[0].Counts.Total = 99
	*report.Results[0].Facts.ObservedCount = 42
	if rec.Tags[0] != "pii" {
		t.Fatal("Tags must be copied, not aliased")
	}
	if *rec.Counts.Total != 4 {
		t.Fatal("Counts must be copied, not aliased")
	}
	if *rec.Facts.ObservedCount != 3 {
		t.Fatal("Facts must be copied, not aliased")
	}
}

func TestMeasurementRecordsFromExportBlankAndDuplicateIDRejected(t *testing.T) {
	blank := ExportedReport{
		Results: []ExportedResult{{
			ID:               "   ",
			Kind:             KindRowCountEqual,
			Severity:         "error",
			PolicyVerdict:    PolicyVerdictPass,
			ExecutionOutcome: ExecutionOutcomeOK,
			RowDenominator:   RowDenominatorUnavailable,
		}},
	}
	_, err := MeasurementRecordsFromExport(blank)
	if err == nil {
		t.Fatal("expected blank id error")
	}
	var ce *CategorizedError
	if !errors.As(err, &ce) || ce.Category != CategoryInvalidConfig {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "measurement result id is required") {
		t.Fatalf("error = %v", err)
	}

	dup := ExportedReport{
		Results: []ExportedResult{
			{
				ID:               "same",
				Kind:             KindRowCountEqual,
				Severity:         "error",
				PolicyVerdict:    PolicyVerdictPass,
				ExecutionOutcome: ExecutionOutcomeOK,
				RowDenominator:   RowDenominatorUnavailable,
			},
			{
				ID:               "same",
				Kind:             KindNotNull,
				Column:           "email",
				Severity:         "error",
				PolicyVerdict:    PolicyVerdictPass,
				ExecutionOutcome: ExecutionOutcomeOK,
				RowDenominator:   RowDenominatorAvailable,
			},
		},
	}
	_, err = MeasurementRecordsFromExport(dup)
	if err == nil {
		t.Fatal("expected duplicate id error")
	}
	if !errors.As(err, &ce) || ce.Category != CategoryInvalidConfig {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), `duplicate measurement result id "same"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestMeasurementRecordsFromExportSegmentedCompositeIdentity(t *testing.T) {
	ok := ExportedReport{
		SchemaVersion: ExportSchemaVersion,
		Results: []ExportedResult{
			{
				ID:               "rows",
				SegmentID:        "eu",
				Kind:             KindRowCountEqual,
				Severity:         "error",
				PolicyVerdict:    PolicyVerdictPass,
				ExecutionOutcome: ExecutionOutcomeOK,
				RowDenominator:   RowDenominatorUnavailable,
			},
			{
				ID:               "rows",
				SegmentID:        "us",
				Kind:             KindRowCountEqual,
				Severity:         "error",
				PolicyVerdict:    PolicyVerdictPass,
				ExecutionOutcome: ExecutionOutcomeOK,
				RowDenominator:   RowDenominatorUnavailable,
			},
		},
	}
	recs, err := MeasurementRecordsFromExport(ok)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("len = %d, want 2", len(recs))
	}
	if recs[0].ResultID != "rows" || recs[0].SegmentID != "eu" {
		t.Fatalf("rec[0] = %q / %q", recs[0].ResultID, recs[0].SegmentID)
	}
	if recs[1].ResultID != "rows" || recs[1].SegmentID != "us" {
		t.Fatalf("rec[1] = %q / %q", recs[1].ResultID, recs[1].SegmentID)
	}
	keyEU := MeasurementKey{ResultID: recs[0].ResultID, SegmentID: recs[0].SegmentID}
	keyUS := MeasurementKey{ResultID: recs[1].ResultID, SegmentID: recs[1].SegmentID}
	keyUnseg := MeasurementKey{ResultID: "rows"}
	if keyEU == keyUS || keyEU == keyUnseg || keyUS == keyUnseg {
		t.Fatalf("segmented keys must stay distinct: %#v %#v %#v", keyEU, keyUS, keyUnseg)
	}

	dup := ExportedReport{
		Results: []ExportedResult{
			{
				ID:               "rows",
				SegmentID:        "eu",
				Kind:             KindRowCountEqual,
				Severity:         "error",
				PolicyVerdict:    PolicyVerdictPass,
				ExecutionOutcome: ExecutionOutcomeOK,
				RowDenominator:   RowDenominatorUnavailable,
			},
			{
				ID:               "rows",
				SegmentID:        "eu",
				Kind:             KindNotNull,
				Column:           "email",
				Severity:         "error",
				PolicyVerdict:    PolicyVerdictPass,
				ExecutionOutcome: ExecutionOutcomeOK,
				RowDenominator:   RowDenominatorAvailable,
			},
		},
	}
	_, err = MeasurementRecordsFromExport(dup)
	if err == nil {
		t.Fatal("expected duplicate id+segment error")
	}
	var ce *CategorizedError
	if !errors.As(err, &ce) || ce.Category != CategoryInvalidConfig {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), `duplicate measurement result id "rows" segment "eu"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestMeasurementRecordsFromExportSuppressesCappedDiagnostics(t *testing.T) {
	report := ExportedReport{
		SchemaVersion: ExportSchemaVersion,
		Results: []ExportedResult{{
			ID:               "age.between",
			Kind:             KindBetween,
			Column:           "age",
			Severity:         "error",
			PolicyVerdict:    PolicyVerdictFail,
			ExecutionOutcome: ExecutionOutcomePolicyFailure,
			RowDenominator:   RowDenominatorAvailable,
			Counts: &ExportedCounts{
				Total:         intPtr(3),
				Failed:        intPtr(2),
				FailedPercent: floatPtr(66.66666666666666),
			},
			Caps: &ExportedCaps{
				SamplesReturned:  1,
				SamplesTruncated: true,
				KeysReturned:     1,
				KeysTruncated:    true,
			},
			Samples: []NormalizedValue{{
				Kind:  "json_integer",
				Value: float64(200),
				Exact: true,
			}},
			FailedKeys: []NormalizedValue{{
				Kind:  "json_integer",
				Value: float64(2),
				Exact: true,
			}},
			Diagnostics: &ExportedDiagnostics{
				Query: "SELECT age FROM users WHERE age < $1 OR age > $2",
				Args: []NormalizedValue{
					{Kind: "json_integer", Value: float64(0), Exact: true},
					{Kind: "json_integer", Value: float64(120), Exact: true},
				},
			},
		}},
	}

	recs, err := MeasurementRecordsFromExport(report)
	if err != nil {
		t.Fatal(err)
	}
	rec := recs[0]

	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		`"samples"`,
		`"failed_keys"`,
		`"caps"`,
		`"diagnostics"`,
		`"query"`,
		"SELECT age FROM users",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("record JSON must suppress %s: %s", forbidden, raw)
		}
	}

	typ := reflect.TypeOf(rec)
	for _, name := range []string{"Samples", "FailedKeys", "Caps", "Diagnostics"} {
		if _, ok := typ.FieldByName(name); ok {
			t.Fatalf("MeasurementRecord must not expose field %s", name)
		}
	}
	if rec.Counts == nil || *rec.Counts.Failed != 2 {
		t.Fatalf("structured counts should remain: %#v", rec.Counts)
	}
}

func TestMeasurementRecordsFromExportPreservesUnevaluatedVerdict(t *testing.T) {
	report := ExportedReport{
		SchemaVersion: ExportSchemaVersion,
		Results: []ExportedResult{{
			ID:               "email.not_empty",
			Kind:             KindNotEmpty,
			Column:           "email",
			Severity:         "error",
			PolicyVerdict:    PolicyVerdictUnevaluated,
			ExecutionOutcome: ExecutionOutcomeExecutionFailure,
			RowDenominator:   RowDenominatorUnavailable,
			Errors: []ExportedError{{
				Category: CategoryDatabase,
				Message:  "connection reset",
			}},
		}},
	}

	recs, err := MeasurementRecordsFromExport(report)
	if err != nil {
		t.Fatal(err)
	}
	rec := recs[0]
	if rec.PolicyVerdict != PolicyVerdictUnevaluated {
		t.Fatalf("PolicyVerdict = %q, want unevaluated", rec.PolicyVerdict)
	}
	if rec.ExecutionOutcome != ExecutionOutcomeExecutionFailure {
		t.Fatalf("ExecutionOutcome = %q", rec.ExecutionOutcome)
	}
	if len(rec.Errors) != 1 || rec.Errors[0].Category != CategoryDatabase {
		t.Fatalf("Errors = %#v", rec.Errors)
	}
	wantMsg := exportSafeErrorMessage(&CategorizedError{Category: CategoryDatabase})
	if rec.Errors[0].Message != wantMsg {
		t.Fatalf("Errors[0].Message = %q, want %q", rec.Errors[0].Message, wantMsg)
	}
	report.Results[0].Errors[0].Message = "mutated"
	if rec.Errors[0].Message != wantMsg {
		t.Fatal("Errors must be copied, not aliased")
	}
}

func TestMeasurementRecordsFromExportResanitizesHandBuiltErrorMessage(t *testing.T) {
	report := ExportedReport{
		SchemaVersion: ExportSchemaVersion,
		Results: []ExportedResult{{
			ID:               "secret.slot",
			Kind:             KindNotNull,
			Severity:         "error",
			PolicyVerdict:    PolicyVerdictUnevaluated,
			ExecutionOutcome: ExecutionOutcomeExecutionFailure,
			RowDenominator:   RowDenominatorUnavailable,
			Errors: []ExportedError{{
				Category: CategoryDatabase,
				Message:  "password=hunter2 token=deadbeef",
			}},
		}},
	}

	recs, err := MeasurementRecordsFromExport(report)
	if err != nil {
		t.Fatal(err)
	}
	rec := recs[0]
	if len(rec.Errors) != 1 {
		t.Fatalf("Errors = %#v", rec.Errors)
	}
	if rec.Errors[0].Category != CategoryDatabase {
		t.Fatalf("Category = %q", rec.Errors[0].Category)
	}
	want := "gxsql: database"
	if rec.Errors[0].Message != want {
		t.Fatalf("Message = %q, want %q", rec.Errors[0].Message, want)
	}
	if strings.Contains(rec.Errors[0].Message, "password") || strings.Contains(rec.Errors[0].Message, "hunter2") {
		t.Fatalf("secret leaked in Message: %q", rec.Errors[0].Message)
	}
}

func TestMeasurementRecordsFromExportPreservesTypedNormalizedValues(t *testing.T) {
	wantInt := NormalizedValue{Kind: "json_integer", Value: int64(42), Exact: true}
	wantParts := []NormalizedValue{
		{Kind: "json_integer", Value: int64(7), Exact: true},
		{Kind: "string", Value: "part", Exact: true},
	}
	wantComposite := NormalizedValue{
		Kind:  "composite",
		Value: append([]NormalizedValue(nil), wantParts...),
		Exact: true,
	}
	srcParts := append([]NormalizedValue(nil), wantParts...)
	srcInt := wantInt
	srcComposite := NormalizedValue{Kind: "composite", Value: srcParts, Exact: true}
	report := ExportedReport{
		SchemaVersion: ExportSchemaVersion,
		Results: []ExportedResult{{
			ID:               "typed.values",
			Kind:             KindBetween,
			Severity:         "error",
			PolicyVerdict:    PolicyVerdictPass,
			ExecutionOutcome: ExecutionOutcomeOK,
			RowDenominator:   RowDenominatorUnavailable,
			Facts: &ExportedFacts{
				ConfiguredBound:      &srcInt,
				ConfiguredBoundLower: &srcComposite,
				KeyColumns:           []string{"a", "b"},
				Reference: &ExportedReferenceFacts{
					LocalColumns:  []string{"local_id"},
					Parent:        ExportedTarget{Schema: "public", Table: "parents"},
					ParentColumns: []string{"id"},
				},
			},
		}},
	}

	recs, err := MeasurementRecordsFromExport(report)
	if err != nil {
		t.Fatal(err)
	}
	rec := recs[0]
	if rec.Facts == nil || rec.Facts.ConfiguredBound == nil || rec.Facts.ConfiguredBoundLower == nil {
		t.Fatalf("Facts = %#v", rec.Facts)
	}
	if !reflect.DeepEqual(*rec.Facts.ConfiguredBound, wantInt) {
		t.Fatalf("ConfiguredBound = %#v, want %#v", *rec.Facts.ConfiguredBound, wantInt)
	}
	if _, ok := rec.Facts.ConfiguredBound.Value.(int64); !ok {
		t.Fatalf("ConfiguredBound.Value type = %T, want int64", rec.Facts.ConfiguredBound.Value)
	}
	if !reflect.DeepEqual(*rec.Facts.ConfiguredBoundLower, wantComposite) {
		t.Fatalf("ConfiguredBoundLower = %#v, want %#v", *rec.Facts.ConfiguredBoundLower, wantComposite)
	}
	parts, ok := rec.Facts.ConfiguredBoundLower.Value.([]NormalizedValue)
	if !ok {
		t.Fatalf("composite Value type = %T", rec.Facts.ConfiguredBoundLower.Value)
	}
	if _, ok := parts[0].Value.(int64); !ok {
		t.Fatalf("composite[0].Value type = %T, want int64", parts[0].Value)
	}

	srcInt.Value = int64(99)
	report.Results[0].Facts.KeyColumns[0] = "mutated"
	srcParts[0].Value = int64(0)
	if !reflect.DeepEqual(*rec.Facts.ConfiguredBound, wantInt) {
		t.Fatal("ConfiguredBound must be copied, not aliased")
	}
	if rec.Facts.KeyColumns[0] != "a" {
		t.Fatal("KeyColumns must be copied, not aliased")
	}
	if !reflect.DeepEqual(*rec.Facts.ConfiguredBoundLower, wantComposite) {
		t.Fatal("composite NormalizedValue must be deeply copied, not aliased")
	}
	report.Results[0].Facts.Reference.LocalColumns[0] = "mutated"
	if rec.Facts.Reference.LocalColumns[0] != "local_id" {
		t.Fatal("Reference.LocalColumns must be copied, not aliased")
	}
}

func TestMeasurementRecordsFromExportRoundTripViaExportReportTimes(t *testing.T) {
	dataTime := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	evalTime := time.Date(2026, 8, 1, 12, 5, 0, 0, time.UTC)
	exported, err := ExportReport(Report{
		Target:  &TableRef{Schema: "public", Name: "users"},
		ScopeID: "batch:9",
		Results: []Result{{
			ID:             "rows",
			Kind:           KindRowCountEqual,
			Name:           "row count = 1: got 1",
			Success:        true,
			RowDenominator: RowDenominatorUnavailable,
			Facts: ResultFacts{
				ObservedCount:   intPtr(1),
				ConfiguredCount: intPtr(1),
			},
			Tags: []string{"nightly"},
		}},
	}, WithDataTime(dataTime), WithEvaluationTime(evalTime), IncludeSamples(), IncludeFailedKeys(), IncludeCapturedDiagnostics())
	if err != nil {
		t.Fatal(err)
	}

	recs, err := MeasurementRecordsFromExport(exported)
	if err != nil {
		t.Fatal(err)
	}
	rec := recs[0]
	if rec.ResultID != "rows" || rec.Kind != KindRowCountEqual {
		t.Fatalf("identity = %q / %q", rec.ResultID, rec.Kind)
	}
	if rec.TargetSchema != "public" || rec.TargetTable != "users" || rec.ScopeID != "batch:9" {
		t.Fatalf("target/scope = %q.%q / %q", rec.TargetSchema, rec.TargetTable, rec.ScopeID)
	}
	if rec.DataTime == nil || !rec.DataTime.Equal(dataTime) {
		t.Fatalf("DataTime = %v", rec.DataTime)
	}
	if rec.EvaluationTime == nil || !rec.EvaluationTime.Equal(evalTime) {
		t.Fatalf("EvaluationTime = %v", rec.EvaluationTime)
	}
	if !reflect.DeepEqual(rec.Tags, []string{"nightly"}) {
		t.Fatalf("Tags = %#v", rec.Tags)
	}
}

func TestBaselineStoreIsCallerOwnedInterfaceOnly(t *testing.T) {
	typ := reflect.TypeOf((*BaselineStore)(nil)).Elem()
	if typ.Kind() != reflect.Interface {
		t.Fatalf("BaselineStore kind = %v, want interface", typ.Kind())
	}
	if typ.NumMethod() != 1 || typ.Method(0).Name != "Get" {
		t.Fatalf("BaselineStore methods = %+v", typ.Method(0))
	}
	keyTyp := reflect.TypeOf(MeasurementKey{})
	for _, name := range []string{"ResultID", "SegmentID", "Kind", "ScopeID", "TargetSchema", "TargetTable"} {
		if _, ok := keyTyp.FieldByName(name); !ok {
			t.Fatalf("MeasurementKey missing %s", name)
		}
	}
	recTyp := reflect.TypeOf(MeasurementRecord{})
	if _, ok := recTyp.FieldByName("SegmentID"); !ok {
		t.Fatal("MeasurementRecord missing SegmentID")
	}
}
