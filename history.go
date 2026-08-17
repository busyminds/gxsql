package gxsql

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MeasurementKey identifies a measurement series for [BaselineStore] lookup.
// ResultID is the primary join field from [WithID]. Kind, ScopeID, TargetSchema,
// and TargetTable are optional conflict checks callers may use when selecting
// prior records; empty optional fields do not constrain a caller-owned store.
type MeasurementKey struct {
	ResultID     string
	Kind         ExpectationKind
	ScopeID      string
	TargetSchema string
	TargetTable  string
}

// MeasurementRecord is one privacy-safe measurement snapshot suitable for
// caller-owned history storage and baseline comparison. It carries stable
// identity, caller-owned data-time and evaluation-time, and structured
// facts/counts/verdict fields from an [ExportedReport]. Samples, failed keys,
// caps, and diagnostics are never included.
//
// gxsql does not persist MeasurementRecord values. Callers own storage,
// windowing, and any drift enforcement.
type MeasurementRecord struct {
	ResultID         string
	Kind             ExpectationKind
	ScopeID          string
	TargetSchema     string
	TargetTable      string
	DataTime         *time.Time
	EvaluationTime   *time.Time
	Column           string
	Severity         string
	Description      string
	Tags             []string
	PolicyVerdict    PolicyVerdict
	ExecutionOutcome ExecutionOutcome
	Tolerated        bool
	RowDenominator   RowDenominator
	Counts           *ExportedCounts
	Facts            *ExportedFacts
	Errors           []ExportedError
}

// BaselineStore is the caller-implemented lookup shape for prior measurements.
// Core validation never requires an implementation. Window selection, append,
// and enforcement remain outside gxsql.
type BaselineStore interface {
	Get(ctx context.Context, key MeasurementKey) ([]MeasurementRecord, error)
}

// MeasurementRecordsFromExport maps an [ExportedReport] into privacy-safe
// [MeasurementRecord] values for caller-owned history storage.
//
// Every result must carry a non-blank [ExportedResult.ID]; blank and duplicate
// IDs are rejected with [CategoryInvalidConfig]. Report-level target, scope,
// data-time, and evaluation-time are applied to each record. Tags, errors,
// counts, and facts are copied. Samples, failed keys, caps, and diagnostics
// are never copied, even when present on the export. [PolicyVerdictUnevaluated]
// is preserved for error slots and is not rewritten into a policy failure.
//
// This helper does not read or write a baseline store.
func MeasurementRecordsFromExport(report ExportedReport) ([]MeasurementRecord, error) {
	targetSchema, targetTable := "", ""
	if report.Target != nil {
		targetSchema = report.Target.Schema
		targetTable = report.Target.Table
	}
	scopeID := ""
	if report.Scope != nil {
		scopeID = report.Scope.ID
	}

	seen := make(map[string]int, len(report.Results))
	out := make([]MeasurementRecord, 0, len(report.Results))
	for i, res := range report.Results {
		id := strings.TrimSpace(res.ID)
		if id == "" {
			return nil, newConfigError(fmt.Errorf("measurement result id is required"))
		}
		if prev, ok := seen[id]; ok {
			return nil, newConfigError(fmt.Errorf(
				"duplicate measurement result id %q (also at index %d)", id, prev,
			))
		}
		seen[id] = i

		out = append(out, MeasurementRecord{
			ResultID:         res.ID,
			Kind:             res.Kind,
			ScopeID:          scopeID,
			TargetSchema:     targetSchema,
			TargetTable:      targetTable,
			DataTime:         cloneTimePtr(report.DataTime),
			EvaluationTime:   cloneTimePtr(report.EvaluationTime),
			Column:           res.Column,
			Severity:         res.Severity,
			Description:      res.Description,
			Tags:             cloneStringSlice(res.Tags),
			PolicyVerdict:    res.PolicyVerdict,
			ExecutionOutcome: res.ExecutionOutcome,
			Tolerated:        res.Tolerated,
			RowDenominator:   res.RowDenominator,
			Counts:           cloneExportedCounts(res.Counts),
			Facts:            cloneExportedFacts(res.Facts),
			Errors:           cloneExportedErrors(res.Errors),
		})
	}
	return out, nil
}

func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	cp := t.UTC()
	return &cp
}

func cloneStringSlice(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string(nil), in...)
}

func cloneIntPtr(in *int) *int {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneFloat64Ptr(in *float64) *float64 {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneBoolPtr(in *bool) *bool {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func cloneExportedCounts(in *ExportedCounts) *ExportedCounts {
	if in == nil {
		return nil
	}
	return &ExportedCounts{
		Total:         cloneIntPtr(in.Total),
		Failed:        cloneIntPtr(in.Failed),
		FailedPercent: cloneFloat64Ptr(in.FailedPercent),
	}
}

func cloneExportedErrors(in []ExportedError) []ExportedError {
	if in == nil {
		return nil
	}
	out := make([]ExportedError, len(in))
	for i, err := range in {
		out[i] = ExportedError{
			Category: err.Category,
			Message:  exportSafeErrorMessage(&CategorizedError{Category: err.Category}),
		}
	}
	return out
}

func cloneNormalizedValue(in NormalizedValue) NormalizedValue {
	out := NormalizedValue{
		Kind:  in.Kind,
		Exact: in.Exact,
	}
	switch v := in.Value.(type) {
	case []NormalizedValue:
		out.Value = cloneNormalizedValueSlice(v)
	default:
		out.Value = in.Value
	}
	return out
}

func cloneNormalizedValuePtr(in *NormalizedValue) *NormalizedValue {
	if in == nil {
		return nil
	}
	cp := cloneNormalizedValue(*in)
	return &cp
}

func cloneNormalizedValueSlice(in []NormalizedValue) []NormalizedValue {
	if in == nil {
		return nil
	}
	out := make([]NormalizedValue, len(in))
	for i := range in {
		out[i] = cloneNormalizedValue(in[i])
	}
	return out
}

func cloneExportedFacts(in *ExportedFacts) *ExportedFacts {
	if in == nil {
		return nil
	}
	return &ExportedFacts{
		ObservedCount:              cloneIntPtr(in.ObservedCount),
		ObservedFloat:              cloneNormalizedValuePtr(in.ObservedFloat),
		ObservedTime:               cloneNormalizedValuePtr(in.ObservedTime),
		ConfiguredCount:            cloneIntPtr(in.ConfiguredCount),
		ConfiguredCountLower:       cloneIntPtr(in.ConfiguredCountLower),
		ConfiguredCountUpper:       cloneIntPtr(in.ConfiguredCountUpper),
		ConfiguredFloatLower:       cloneNormalizedValuePtr(in.ConfiguredFloatLower),
		ConfiguredFloatUpper:       cloneNormalizedValuePtr(in.ConfiguredFloatUpper),
		ConfiguredFloatBound:       cloneNormalizedValuePtr(in.ConfiguredFloatBound),
		ConfiguredBound:            cloneNormalizedValuePtr(in.ConfiguredBound),
		ConfiguredBoundLower:       cloneNormalizedValuePtr(in.ConfiguredBoundLower),
		ConfiguredBoundUpper:       cloneNormalizedValuePtr(in.ConfiguredBoundUpper),
		ConfiguredTimeStart:        cloneNormalizedValuePtr(in.ConfiguredTimeStart),
		ConfiguredTimeEnd:          cloneNormalizedValuePtr(in.ConfiguredTimeEnd),
		ConfiguredTimeCutoff:       cloneNormalizedValuePtr(in.ConfiguredTimeCutoff),
		ConfiguredMaxFailedCount:   cloneIntPtr(in.ConfiguredMaxFailedCount),
		ConfiguredMaxFailedPercent: cloneFloat64Ptr(in.ConfiguredMaxFailedPercent),
		ObservedTimePresent:        cloneBoolPtr(in.ObservedTimePresent),
		KeyColumns:                 cloneStringSlice(in.KeyColumns),
		Reference:                  cloneExportedReferenceFacts(in.Reference),
		Comparison:                 cloneExportedComparisonFacts(in.Comparison),
		Ratio:                      cloneExportedRatioFacts(in.Ratio),
		Reconcile:                  cloneExportedReconcileFacts(in.Reconcile),
		Sum:                        cloneExportedSumFacts(in.Sum),
		PopulationStdDev:           cloneExportedPopulationStdDevFacts(in.PopulationStdDev),
		Completeness:               cloneExportedRateFacts(in.Completeness),
		DuplicateRate:              cloneExportedRateFacts(in.DuplicateRate),
		Frequency:                  cloneExportedFrequencyFacts(in.Frequency),
		DominantShare:              cloneExportedDominantShareFacts(in.DominantShare),
		RequiredColumns:            cloneStringSlice(in.RequiredColumns),
		MissingColumns:             cloneStringSlice(in.MissingColumns),
		UnexpectedColumns:          cloneStringSlice(in.UnexpectedColumns),
		ConfiguredNullability:      in.ConfiguredNullability,
		ObservedNullability:        in.ObservedNullability,
		ConfiguredReportedType:     in.ConfiguredReportedType,
		ObservedReportedType:       in.ObservedReportedType,
	}
}

func cloneExportedReferenceFacts(in *ExportedReferenceFacts) *ExportedReferenceFacts {
	if in == nil {
		return nil
	}
	return &ExportedReferenceFacts{
		LocalColumns:   cloneStringSlice(in.LocalColumns),
		Parent:         in.Parent,
		ParentColumns:  cloneStringSlice(in.ParentColumns),
		ParentFilterID: in.ParentFilterID,
	}
}

func cloneExportedComparisonFacts(in *ExportedComparisonFacts) *ExportedComparisonFacts {
	if in == nil {
		return nil
	}
	return &ExportedComparisonFacts{
		LeftColumn:   in.LeftColumn,
		RightColumn:  in.RightColumn,
		Relationship: in.Relationship,
	}
}

func cloneExportedRatioFacts(in *ExportedRatioFacts) *ExportedRatioFacts {
	if in == nil {
		return nil
	}
	return &ExportedRatioFacts{
		LeftColumn:  in.LeftColumn,
		RightColumn: in.RightColumn,
		Bound:       in.Bound,
	}
}

func cloneExportedReconcileFacts(in *ExportedReconcileFacts) *ExportedReconcileFacts {
	if in == nil {
		return nil
	}
	return &ExportedReconcileFacts{
		Left:               in.Left,
		Right:              in.Right,
		ObservedLeftCount:  cloneIntPtr(in.ObservedLeftCount),
		ObservedRightCount: cloneIntPtr(in.ObservedRightCount),
		Relationship:       in.Relationship,
		LeftScopeID:        in.LeftScopeID,
		SecondaryFilterID:  in.SecondaryFilterID,
	}
}

func cloneExportedSumFacts(in *ExportedSumFacts) *ExportedSumFacts {
	if in == nil {
		return nil
	}
	return &ExportedSumFacts{
		Observed:             cloneIntPtr(in.Observed),
		ObservedFloat:        cloneNormalizedValuePtr(in.ObservedFloat),
		ConfiguredLower:      cloneIntPtr(in.ConfiguredLower),
		ConfiguredUpper:      cloneIntPtr(in.ConfiguredUpper),
		ConfiguredFloatLower: cloneNormalizedValuePtr(in.ConfiguredFloatLower),
		ConfiguredFloatUpper: cloneNormalizedValuePtr(in.ConfiguredFloatUpper),
		Exactness:            in.Exactness,
	}
}

func cloneExportedPopulationStdDevFacts(in *ExportedPopulationStdDevFacts) *ExportedPopulationStdDevFacts {
	if in == nil {
		return nil
	}
	return &ExportedPopulationStdDevFacts{
		Observed:        cloneNormalizedValuePtr(in.Observed),
		ConfiguredLower: cloneNormalizedValuePtr(in.ConfiguredLower),
		ConfiguredUpper: cloneNormalizedValuePtr(in.ConfiguredUpper),
		Algorithm:       in.Algorithm,
		Exactness:       in.Exactness,
	}
}

func cloneExportedRateFacts(in *ExportedRateFacts) *ExportedRateFacts {
	if in == nil {
		return nil
	}
	return &ExportedRateFacts{
		NonNullCount:    cloneIntPtr(in.NonNullCount),
		DuplicateCount:  cloneIntPtr(in.DuplicateCount),
		TotalCount:      cloneIntPtr(in.TotalCount),
		Rate:            cloneNormalizedValuePtr(in.Rate),
		ConfiguredBound: cloneNormalizedValuePtr(in.ConfiguredBound),
		ConfiguredLower: cloneNormalizedValuePtr(in.ConfiguredLower),
		ConfiguredUpper: cloneNormalizedValuePtr(in.ConfiguredUpper),
	}
}

func cloneExportedFrequencyFacts(in *ExportedFrequencyFacts) *ExportedFrequencyFacts {
	if in == nil {
		return nil
	}
	return &ExportedFrequencyFacts{
		ConfiguredValue: cloneNormalizedValuePtr(in.ConfiguredValue),
		ConfiguredNull:  in.ConfiguredNull,
		ValueCount:      cloneIntPtr(in.ValueCount),
		TotalCount:      cloneIntPtr(in.TotalCount),
		Share:           cloneNormalizedValuePtr(in.Share),
		ConfiguredBound: cloneNormalizedValuePtr(in.ConfiguredBound),
		ConfiguredLower: cloneNormalizedValuePtr(in.ConfiguredLower),
		ConfiguredUpper: cloneNormalizedValuePtr(in.ConfiguredUpper),
	}
}

func cloneExportedDominantShareFacts(in *ExportedDominantShareFacts) *ExportedDominantShareFacts {
	if in == nil {
		return nil
	}
	return &ExportedDominantShareFacts{
		DominantCount:   cloneIntPtr(in.DominantCount),
		TotalCount:      cloneIntPtr(in.TotalCount),
		Share:           cloneNormalizedValuePtr(in.Share),
		TieCount:        cloneIntPtr(in.TieCount),
		ConfiguredBound: cloneNormalizedValuePtr(in.ConfiguredBound),
		ConfiguredLower: cloneNormalizedValuePtr(in.ConfiguredLower),
		ConfiguredUpper: cloneNormalizedValuePtr(in.ConfiguredUpper),
	}
}
