package gxsql

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// ExportSchemaVersion is the top-level schema version for ExportedReport JSON.
const ExportSchemaVersion = "gxsql.report.v1"

// MaxExportedQueryTextRunes is the maximum rune length of exported SQL text.
const MaxExportedQueryTextRunes = 4096

// MaxExportedArgumentCount is the maximum number of exported bound arguments.
const MaxExportedArgumentCount = 256

// MaxExportedErrorMessageRunes is the maximum rune length of export-safe errors.
const MaxExportedErrorMessageRunes = 512

// PolicyVerdict is the exported policy state of an expectation: pass, fail, or
// unevaluated when the source Result has an error.
type PolicyVerdict string

const (
	// PolicyVerdictPass means the expectation ran and its policy passed.
	PolicyVerdictPass PolicyVerdict = "pass"
	// PolicyVerdictFail means the expectation ran and its policy failed.
	PolicyVerdictFail PolicyVerdict = "fail"
	// PolicyVerdictUnevaluated means the source Result has an execution or
	// configuration error, so its policy outcome is unavailable.
	PolicyVerdictUnevaluated PolicyVerdict = "unevaluated"
)

// ExecutionOutcome classifies how validation ran, distinct from policy verdict.
type ExecutionOutcome string

const (
	// ExecutionOutcomeOK means the expectation ran and its policy passed.
	ExecutionOutcomeOK ExecutionOutcome = "ok"
	// ExecutionOutcomePolicyFailure means the expectation ran and its policy failed.
	ExecutionOutcomePolicyFailure ExecutionOutcome = "policy_failure"
	// ExecutionOutcomeExecutionFailure means execution failed after evaluation began.
	ExecutionOutcomeExecutionFailure ExecutionOutcome = "execution_failure"
	// ExecutionOutcomeConfigFailure means preflight configuration prevented execution.
	ExecutionOutcomeConfigFailure ExecutionOutcome = "config_failure"
)

// ExportedReport is the versioned JSON DTO produced by ExportReport.
type ExportedReport struct {
	// SchemaVersion identifies the export contract. Always ExportSchemaVersion.
	SchemaVersion string `json:"schema_version"`
	// DataTime is the caller-owned business/as-of time of the validated
	// population. Non-zero values are UTC copies; omitted when unset or zero.
	// JSON encoding uses RFC3339Nano.
	DataTime *time.Time `json:"data_time,omitempty"`
	// EvaluationTime is the caller-owned time when validation or export ran.
	// Non-zero values are UTC copies; omitted when unset or zero. JSON encoding
	// uses RFC3339Nano.
	EvaluationTime *time.Time `json:"evaluation_time,omitempty"`
	// Target names the validated table when Report.Target is set; omitted when unavailable.
	Target *ExportedTarget `json:"target,omitempty"`
	// Scope names the validation scope when available; omitted when unavailable.
	Scope *ExportedScope `json:"scope,omitempty"`
	// Results preserves declaration order from Report.Results.
	Results []ExportedResult `json:"results"`
}

// ExportedTarget identifies the table validated by ValidateTable.
type ExportedTarget struct {
	// Schema is the optional schema qualifier; omitted when empty.
	Schema string `json:"schema,omitempty"`
	// Table is the table name.
	Table string `json:"table"`
}

// ExportedScope identifies a validation scope by its stable ID. Predicate and
// bound arguments are not included in exports.
type ExportedScope struct {
	// ID is a stable scope identifier when scope is available.
	ID string `json:"id,omitempty"`
}

// ExportedResult is one exported expectation outcome.
type ExportedResult struct {
	// ID is the caller-supplied stable result identifier; omitted when empty.
	ID string `json:"id,omitempty"`
	// Kind is the library-defined expectation kind.
	Kind ExpectationKind `json:"kind"`
	// DisplayName is the human-oriented result name with configured bounds redacted.
	DisplayName string `json:"display_name"`
	// Column is the validated column when applicable; omitted when empty.
	Column string `json:"column,omitempty"`
	// Severity is the policy severity name.
	Severity string `json:"severity"`
	// Description is optional policy metadata.
	Description string `json:"description,omitempty"`
	// Tags are normalized, sorted policy metadata.
	Tags []string `json:"tags,omitempty"`
	// PolicyVerdict is pass, fail, or unevaluated when no policy verdict was produced.
	PolicyVerdict PolicyVerdict `json:"policy_verdict"`
	// ExecutionOutcome distinguishes policy failure from execution/config failure.
	ExecutionOutcome ExecutionOutcome `json:"execution_outcome"`
	// Tolerated is true when a nonzero raw failure count passed within an
	// allowance. Omitted unless true.
	Tolerated bool `json:"tolerated,omitempty"`
	// RowDenominator reports whether total and failed_percent are meaningful.
	RowDenominator RowDenominator `json:"row_denominator"`
	// Counts holds row counts when applicable.
	Counts *ExportedCounts `json:"counts,omitempty"`
	// Facts holds machine-readable observations separate from display text.
	Facts *ExportedFacts `json:"facts,omitempty"`
	// Caps reports diagnostic truncation when samples or keys are exported.
	Caps *ExportedCaps `json:"caps,omitempty"`
	// Samples holds normalized failing sample values; omitted unless explicitly included.
	Samples []NormalizedValue `json:"samples,omitempty"`
	// FailedKeys holds normalized failing row keys; omitted unless explicitly included.
	FailedKeys []NormalizedValue `json:"failed_keys,omitempty"`
	// Diagnostics holds redacted query diagnostics; omitted unless explicitly included.
	Diagnostics *ExportedDiagnostics `json:"diagnostics,omitempty"`
	// Errors holds categorized failures in stable order; omitted when empty.
	Errors []ExportedError `json:"errors,omitempty"`
}

// ExportedCounts holds row population metrics.
type ExportedCounts struct {
	// Total is the evaluated row population; omitted when row_denominator is unavailable.
	Total *int `json:"total,omitempty"`
	// Failed is the number of failing rows when row_denominator is available.
	Failed *int `json:"failed,omitempty"`
	// FailedPercent is the percentage of failing rows; omitted when unavailable.
	FailedPercent *float64 `json:"failed_percent,omitempty"`
}

// ExportedFacts holds structured observations and configured thresholds.
type ExportedFacts struct {
	// ObservedCount is a table-level integer observation when set.
	ObservedCount *int `json:"observed_count,omitempty"`
	// ObservedFloat is a normalized floating-point observation when set.
	ObservedFloat *NormalizedValue `json:"observed_float,omitempty"`
	// ObservedTime is the normalized observed maximum timestamp when present.
	ObservedTime *NormalizedValue `json:"observed_time,omitempty"`
	// ConfiguredCount is the exact-count threshold for equal-style expectations.
	ConfiguredCount *int `json:"configured_count,omitempty"`
	// ConfiguredCountLower is the inclusive lower integer bound.
	ConfiguredCountLower *int `json:"configured_count_lower,omitempty"`
	// ConfiguredCountUpper is the inclusive upper integer bound.
	ConfiguredCountUpper *int `json:"configured_count_upper,omitempty"`
	// ConfiguredFloatLower is the inclusive lower floating-point bound.
	ConfiguredFloatLower *NormalizedValue `json:"configured_float_lower,omitempty"`
	// ConfiguredFloatUpper is the inclusive upper floating-point bound.
	ConfiguredFloatUpper *NormalizedValue `json:"configured_float_upper,omitempty"`
	// ConfiguredFloatBound is the single-sided floating-point threshold.
	ConfiguredFloatBound *NormalizedValue `json:"configured_float_bound,omitempty"`
	// ConfiguredBound is the per-row comparison threshold with a driver-bound type.
	ConfiguredBound *NormalizedValue `json:"configured_bound,omitempty"`
	// ConfiguredBoundLower is the inclusive per-row lower bound.
	ConfiguredBoundLower *NormalizedValue `json:"configured_bound_lower,omitempty"`
	// ConfiguredBoundUpper is the inclusive per-row upper bound.
	ConfiguredBoundUpper *NormalizedValue `json:"configured_bound_upper,omitempty"`
	// ConfiguredTimeStart is the caller-configured half-open window start.
	ConfiguredTimeStart *NormalizedValue `json:"configured_time_start,omitempty"`
	// ConfiguredTimeEnd is the caller-configured half-open window end.
	ConfiguredTimeEnd *NormalizedValue `json:"configured_time_end,omitempty"`
	// ConfiguredTimeCutoff is the freshness cutoff when set.
	ConfiguredTimeCutoff *NormalizedValue `json:"configured_time_cutoff,omitempty"`
	// ConfiguredMaxFailedCount is the inclusive WithMaxFailedCount bound when
	// that decorator was applied.
	ConfiguredMaxFailedCount *int `json:"configured_max_failed_count,omitempty"`
	// ConfiguredMaxFailedPercent is the inclusive MaxFailedPercent bound when
	// that policy was applied.
	ConfiguredMaxFailedPercent *float64 `json:"configured_max_failed_percent,omitempty"`
	// ObservedTimePresent is the explicit freshness observation marker.
	ObservedTimePresent *bool `json:"observed_time_present,omitempty"`
	// KeyColumns names local composite-key components in declaration order.
	KeyColumns []string `json:"key_columns,omitempty"`
	// Reference holds local-to-parent mapping facts for referential integrity.
	Reference *ExportedReferenceFacts `json:"reference,omitempty"`
	// Comparison holds same-row operand and relationship facts.
	Comparison *ExportedComparisonFacts `json:"comparison,omitempty"`
	// Ratio holds same-row integer ratio facts.
	Ratio *ExportedRatioFacts `json:"ratio,omitempty"`
	// Reconcile holds dual-side count reconciliation facts.
	Reconcile *ExportedReconcileFacts `json:"reconcile,omitempty"`
	// Sum holds structured SUM observations and bounds.
	Sum *ExportedSumFacts `json:"sum,omitempty"`
	// PopulationStdDev holds structured population standard-deviation facts.
	PopulationStdDev *ExportedPopulationStdDevFacts `json:"population_stddev,omitempty"`
	// Completeness holds non-NULL numerator, denominator, and rate facts.
	Completeness *ExportedRateFacts `json:"completeness,omitempty"`
	// DuplicateRate holds duplicate-row numerator, denominator, and rate facts.
	DuplicateRate *ExportedRateFacts `json:"duplicate_rate,omitempty"`
	// Frequency holds one category's count and share facts.
	Frequency *ExportedFrequencyFacts `json:"frequency,omitempty"`
	// DominantShare holds maximum share and tie-count facts.
	DominantShare *ExportedDominantShareFacts `json:"dominant_share,omitempty"`
	// RequiredColumns lists caller-configured expected column names in
	// declaration order for structural column expectations.
	RequiredColumns []string `json:"required_columns,omitempty"`
	// MissingColumns lists expected names absent from discovery, in caller
	// declaration order.
	MissingColumns []string `json:"missing_columns,omitempty"`
	// UnexpectedColumns lists discovered names absent from the expected set,
	// in discovery order.
	UnexpectedColumns []string `json:"unexpected_columns,omitempty"`
	// ConfiguredNullability is the caller-configured catalog nullability claim.
	ConfiguredNullability CatalogNullability `json:"configured_nullability,omitempty"`
	// ObservedNullability is the driver-/catalog-reported nullability.
	ObservedNullability CatalogNullability `json:"observed_nullability,omitempty"`
	// ConfiguredReportedType is the caller-configured exact reported type spelling.
	ConfiguredReportedType string `json:"configured_reported_type,omitempty"`
	// ObservedReportedType is the driver-/catalog-reported type name.
	ObservedReportedType string `json:"observed_reported_type,omitempty"`
}

// ExportedSumFacts is the encoded form of [SumFacts].
type ExportedSumFacts struct {
	// Observed is the exact integer SUM when present.
	Observed *int `json:"observed,omitempty"`
	// ObservedFloat is the float64 SUM when present.
	ObservedFloat *NormalizedValue `json:"observed_float,omitempty"`
	// ConfiguredLower is the inclusive lower integer bound when set.
	ConfiguredLower *int `json:"configured_lower,omitempty"`
	// ConfiguredUpper is the inclusive upper integer bound when set.
	ConfiguredUpper *int `json:"configured_upper,omitempty"`
	// ConfiguredFloatLower is the inclusive lower float bound when set.
	ConfiguredFloatLower *NormalizedValue `json:"configured_float_lower,omitempty"`
	// ConfiguredFloatUpper is the inclusive upper float bound when set.
	ConfiguredFloatUpper *NormalizedValue `json:"configured_float_upper,omitempty"`
	// Exactness labels the observation path.
	Exactness string `json:"exactness,omitempty"`
}

// ExportedPopulationStdDevFacts is the encoded form of [PopulationStdDevFacts].
type ExportedPopulationStdDevFacts struct {
	// Observed is the population standard deviation when present.
	Observed *NormalizedValue `json:"observed,omitempty"`
	// ConfiguredLower is the inclusive lower float bound when set.
	ConfiguredLower *NormalizedValue `json:"configured_lower,omitempty"`
	// ConfiguredUpper is the inclusive upper float bound when set.
	ConfiguredUpper *NormalizedValue `json:"configured_upper,omitempty"`
	// Algorithm names the exact population algorithm used by the dialect.
	Algorithm string `json:"algorithm,omitempty"`
	// Exactness labels the observation contract.
	Exactness string `json:"exactness,omitempty"`
}

// ExportedRateFacts is the encoded form of completeness or duplicate-rate
// observations.
type ExportedRateFacts struct {
	// NonNullCount is the completeness numerator when observed.
	NonNullCount *int `json:"non_null_count,omitempty"`
	// DuplicateCount is the duplicate-rate numerator when observed.
	DuplicateCount *int `json:"duplicate_count,omitempty"`
	// TotalCount is the scoped-row denominator when observed.
	TotalCount *int `json:"total_count,omitempty"`
	// Rate is the derived rate when computable.
	Rate *NormalizedValue `json:"rate,omitempty"`
	// ConfiguredBound is the inclusive rate bound for single-sided checks.
	ConfiguredBound *NormalizedValue `json:"configured_bound,omitempty"`
	// ConfiguredLower is the inclusive lower rate bound for Between.
	ConfiguredLower *NormalizedValue `json:"configured_lower,omitempty"`
	// ConfiguredUpper is the inclusive upper rate bound for Between.
	ConfiguredUpper *NormalizedValue `json:"configured_upper,omitempty"`
}

// ExportedFrequencyFacts is the encoded form of [FrequencyFacts].
type ExportedFrequencyFacts struct {
	// ConfiguredValue is the requested category when non-NULL.
	ConfiguredValue *NormalizedValue `json:"configured_value,omitempty"`
	// ConfiguredNull marks SQL NULL as the requested category.
	ConfiguredNull bool `json:"configured_null,omitempty"`
	// ValueCount is the matching-category count when observed.
	ValueCount *int `json:"value_count,omitempty"`
	// TotalCount is the documented denominator when observed.
	TotalCount *int `json:"total_count,omitempty"`
	// Share is the category share when computable.
	Share *NormalizedValue `json:"share,omitempty"`
	// ConfiguredBound is the inclusive share bound for single-sided checks.
	ConfiguredBound *NormalizedValue `json:"configured_bound,omitempty"`
	// ConfiguredLower is the inclusive lower share bound for Between.
	ConfiguredLower *NormalizedValue `json:"configured_lower,omitempty"`
	// ConfiguredUpper is the inclusive upper share bound for Between.
	ConfiguredUpper *NormalizedValue `json:"configured_upper,omitempty"`
}

// ExportedDominantShareFacts is the encoded form of [DominantShareFacts].
type ExportedDominantShareFacts struct {
	// DominantCount is the maximum category count when observed.
	DominantCount *int `json:"dominant_count,omitempty"`
	// TotalCount is the documented denominator when observed.
	TotalCount *int `json:"total_count,omitempty"`
	// Share is the maximum category share when computable.
	Share *NormalizedValue `json:"share,omitempty"`
	// TieCount is the number of categories tied at the maximum share.
	TieCount *int `json:"tie_count,omitempty"`
	// ConfiguredBound is the inclusive share bound for single-sided checks.
	ConfiguredBound *NormalizedValue `json:"configured_bound,omitempty"`
	// ConfiguredLower is the inclusive lower share bound for Between.
	ConfiguredLower *NormalizedValue `json:"configured_lower,omitempty"`
	// ConfiguredUpper is the inclusive upper share bound for Between.
	ConfiguredUpper *NormalizedValue `json:"configured_upper,omitempty"`
}

// ExportedComparisonFacts is the JSON form of ComparisonFacts.
type ExportedComparisonFacts struct {
	// LeftColumn is the left operand identifier.
	LeftColumn string `json:"left_column"`
	// RightColumn is the right operand identifier.
	RightColumn string `json:"right_column"`
	// Relationship is the fixed relationship name.
	Relationship string `json:"relationship"`
}

// ExportedRatioFacts is the JSON form of RatioFacts.
type ExportedRatioFacts struct {
	// LeftColumn is the actual-value operand identifier.
	LeftColumn string `json:"left_column"`
	// RightColumn is the denominator operand identifier.
	RightColumn string `json:"right_column"`
	// Bound is the integral ratio bound.
	Bound int64 `json:"bound"`
}

// ExportedReferenceFacts is the JSON form of ReferenceFacts.
type ExportedReferenceFacts struct {
	// LocalColumns are the local foreign-key components in declaration order.
	LocalColumns []string `json:"local_columns,omitempty"`
	// Parent is the parent target; schema is omitted when empty.
	Parent ExportedTarget `json:"parent"`
	// ParentColumns are the parent key components in declaration order.
	ParentColumns []string `json:"parent_columns,omitempty"`
	// ParentFilterID is the parent-filter identity when set. Predicate text and
	// args are never exported by default.
	ParentFilterID string `json:"parent_filter_id,omitempty"`
}

// ExportedReconcileFacts is the JSON form of ReconcileFacts.
type ExportedReconcileFacts struct {
	// Left is the ValidateTable target; schema is omitted when empty.
	Left ExportedTarget `json:"left"`
	// Right is the secondary target; schema is omitted when empty.
	Right ExportedTarget `json:"right"`
	// ObservedLeftCount is the left-side COUNT(*) when observed.
	ObservedLeftCount *int `json:"observed_left_count,omitempty"`
	// ObservedRightCount is the right-side COUNT(*) when observed.
	ObservedRightCount *int `json:"observed_right_count,omitempty"`
	// Relationship is the fixed comparison name.
	Relationship string `json:"relationship"`
	// LeftScopeID is the suite scope identity when set. Predicate text and args
	// are never exported by default.
	LeftScopeID string `json:"left_scope_id,omitempty"`
	// SecondaryFilterID is the secondary-filter identity when set. Predicate
	// text and args are never exported by default.
	SecondaryFilterID string `json:"secondary_filter_id,omitempty"`
}

// ExportedCaps reports diagnostic truncation metadata.
type ExportedCaps struct {
	// SamplesReturned is the number of exported samples.
	SamplesReturned int `json:"samples_returned,omitempty"`
	// SamplesTruncated is true when more samples existed than were retained.
	SamplesTruncated bool `json:"samples_truncated,omitempty"`
	// KeysReturned is the number of exported failed keys.
	KeysReturned int `json:"keys_returned,omitempty"`
	// KeysTruncated is true when more failing keys existed than were retained.
	KeysTruncated bool `json:"keys_truncated,omitempty"`
}

// ExportedDiagnostics holds optional redacted SQL diagnostics.
type ExportedDiagnostics struct {
	// Query is redacted SQL text when IncludeCapturedDiagnostics is enabled.
	Query string `json:"query,omitempty"`
	// Args holds normalized bound arguments when IncludeCapturedArguments is enabled.
	Args []NormalizedValue `json:"args,omitempty"`
	// QueryTruncated is true when query text exceeded MaxExportedQueryTextRunes.
	QueryTruncated bool `json:"query_truncated,omitempty"`
	// ArgsTruncated is true when argument count exceeded MaxExportedArgumentCount.
	ArgsTruncated bool `json:"args_truncated,omitempty"`
}

// ExportedError is a categorized export-safe error representation.
type ExportedError struct {
	// Category is the machine-facing failure class.
	Category ErrorCategory `json:"category"`
	// Message is diagnostic detail safe for export.
	Message string `json:"message"`
}

// Redactor transforms a value before export. A returned error or panic fails
// export closed without emitting raw diagnostic content.
type Redactor func(any) (any, error)

type exportConfig struct {
	includeSamples          bool
	includeFailedKeys       bool
	includeQueryDiagnostics bool
	includeCapturedArgs     bool
	queryRedactor           Redactor
	argsRedactor            Redactor
	sampleRedactor          Redactor
	keyRedactor             Redactor
	dataTime                time.Time
	evaluationTime          time.Time
}

// ExportOption configures ExportReport.
type ExportOption func(*exportConfig)

// IncludeSamples exports normalized SampleValues. Omitted by default.
func IncludeSamples() ExportOption {
	return func(cfg *exportConfig) { cfg.includeSamples = true }
}

// IncludeFailedKeys exports normalized FailedKeys. Omitted by default.
func IncludeFailedKeys() ExportOption {
	return func(cfg *exportConfig) { cfg.includeFailedKeys = true }
}

// IncludeCapturedDiagnostics exports captured SQL text from results that were
// validated with CaptureQueryDiagnostics. Omitted by default.
func IncludeCapturedDiagnostics() ExportOption {
	return func(cfg *exportConfig) { cfg.includeQueryDiagnostics = true }
}

// IncludeCapturedArguments exports normalized bound arguments alongside captured
// query text. Requires CaptureQueryDiagnostics at validation time.
func IncludeCapturedArguments() ExportOption {
	return func(cfg *exportConfig) {
		cfg.includeQueryDiagnostics = true
		cfg.includeCapturedArgs = true
	}
}

// WithQueryRedactor applies fn to captured query text after identifiers are
// redacted and query text may be truncated to MaxExportedQueryTextRunes; fn runs
// after that initial truncation and its output is truncated again. fn must return
// a string; errors and panics fail export closed.
func WithQueryRedactor(fn Redactor) ExportOption {
	return func(cfg *exportConfig) { cfg.queryRedactor = fn }
}

// WithArgsRedactor applies fn to each captured argument before export.
func WithArgsRedactor(fn Redactor) ExportOption {
	return func(cfg *exportConfig) { cfg.argsRedactor = fn }
}

// WithSampleRedactor applies fn to each sample value before export.
func WithSampleRedactor(fn Redactor) ExportOption {
	return func(cfg *exportConfig) { cfg.sampleRedactor = fn }
}

// WithKeyRedactor applies fn to each failed key before export.
func WithKeyRedactor(fn Redactor) ExportOption {
	return func(cfg *exportConfig) { cfg.keyRedactor = fn }
}

// WithDataTime sets the caller-owned data/as-of time on the exported report.
// Non-zero values are copied and normalized to UTC. Zero values are omitted
// from JSON. Encoding uses RFC3339Nano via encoding/json time.Time rules.
func WithDataTime(t time.Time) ExportOption {
	return func(cfg *exportConfig) { cfg.dataTime = t }
}

// WithEvaluationTime sets the caller-owned evaluation/run time on the exported
// report. Non-zero values are copied and normalized to UTC. Zero values are
// omitted from JSON. Encoding uses RFC3339Nano via encoding/json time.Time rules.
func WithEvaluationTime(t time.Time) ExportOption {
	return func(cfg *exportConfig) { cfg.evaluationTime = t }
}

// ExportReport converts report into a versioned JSON DTO. On error, no partial
// DTO is returned. Query text, bound arguments, samples, and failed keys are
// omitted unless explicitly enabled via ExportOption.
func ExportReport(report Report, opts ...ExportOption) (ExportedReport, error) {
	cfg := exportConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	out := ExportedReport{
		SchemaVersion:  ExportSchemaVersion,
		DataTime:       exportCallerTime(cfg.dataTime),
		EvaluationTime: exportCallerTime(cfg.evaluationTime),
		Results:        make([]ExportedResult, 0, len(report.Results)),
	}
	if report.Target != nil {
		out.Target = &ExportedTarget{
			Schema: report.Target.Schema,
			Table:  report.Target.Name,
		}
	}
	if report.ScopeID != "" {
		out.Scope = &ExportedScope{ID: report.ScopeID}
	}

	for _, res := range report.Results {
		expRes, err := exportResult(res, report.Target, cfg)
		if err != nil {
			return ExportedReport{}, err
		}
		out.Results = append(out.Results, expRes)
	}
	return out, nil
}

// exportCallerTime returns a UTC copy of a caller-owned timestamp. Zero values
// return nil so omitempty drops the JSON field. encoding/json encodes time.Time
// as RFC3339Nano.
func exportCallerTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	utc := t.UTC()
	return &utc
}

func exportResult(res Result, target *TableRef, cfg exportConfig) (ExportedResult, error) {
	out := ExportedResult{
		ID:               res.ID,
		Kind:             res.Kind,
		DisplayName:      exportDisplayName(res),
		Column:           res.Column,
		Severity:         exportSeverity(res.Severity),
		Description:      res.Description,
		Tags:             append([]string(nil), res.Tags...),
		PolicyVerdict:    policyVerdict(res),
		ExecutionOutcome: executionOutcome(res),
		Tolerated:        res.Tolerated,
		RowDenominator:   res.RowDenominator,
	}

	if customCountResultProfile(res) {
		if err := validateCustomCountResult(res); err != nil {
			return ExportedResult{}, err
		}
		if res.Err == nil {
			failed := res.FailedCount
			out.Counts = &ExportedCounts{Failed: &failed}
		}
	} else if reconcileResultProfile(res) {
		if res.Err == nil {
			failed := res.FailedCount
			out.Counts = &ExportedCounts{Failed: &failed}
		}
	} else if res.RowDenominator == RowDenominatorAvailable {
		total := res.Total
		failed := res.FailedCount
		out.Counts = &ExportedCounts{
			Total:  &total,
			Failed: &failed,
		}
		if res.Total > 0 {
			pct := res.FailedPercent
			out.Counts.FailedPercent = &pct
		}
	} else if res.FailedCount > 0 {
		failed := res.FailedCount
		out.Counts = &ExportedCounts{Failed: &failed}
	}

	facts, err := exportFacts(res.Facts)
	if err != nil {
		return ExportedResult{}, err
	}
	out.Facts = facts

	if cfg.includeSamples {
		samples, caps, err := exportSamples(res, cfg)
		if err != nil {
			return ExportedResult{}, err
		}
		out.Samples = samples
		if caps != nil {
			out.Caps = caps
		}
	}

	if cfg.includeFailedKeys {
		keys, caps, err := exportFailedKeys(res, cfg)
		if err != nil {
			return ExportedResult{}, err
		}
		out.FailedKeys = keys
		if caps != nil {
			if out.Caps == nil {
				out.Caps = caps
			} else {
				out.Caps.KeysReturned = caps.KeysReturned
				out.Caps.KeysTruncated = caps.KeysTruncated
			}
		}
	}

	if cfg.includeQueryDiagnostics && res.diagnostics != nil {
		diag, err := exportDiagnostics(res.diagnostics, target, cfg)
		if err != nil {
			return ExportedResult{}, err
		}
		out.Diagnostics = diag
	}

	if res.Err != nil {
		out.Errors = []ExportedError{exportError(res.Err)}
	}

	return out, nil
}

func exportSeverity(severity Severity) string {
	switch severity {
	case SeverityWarning:
		return "warning"
	case SeverityInfo:
		return "info"
	default:
		return "error"
	}
}

func policyVerdict(res Result) PolicyVerdict {
	if res.Err != nil {
		return PolicyVerdictUnevaluated
	}
	if res.Success {
		return PolicyVerdictPass
	}
	return PolicyVerdictFail
}

func executionOutcome(res Result) ExecutionOutcome {
	if res.Err != nil {
		var ce *CategorizedError
		if errors.As(res.Err, &ce) && ce.Category == CategoryInvalidConfig {
			return ExecutionOutcomeConfigFailure
		}
		return ExecutionOutcomeExecutionFailure
	}
	if !res.Success {
		return ExecutionOutcomePolicyFailure
	}
	return ExecutionOutcomeOK
}

func exportError(err error) ExportedError {
	var ce *CategorizedError
	if errors.As(err, &ce) {
		return ExportedError{Category: ce.Category, Message: exportSafeErrorMessage(ce)}
	}
	return ExportedError{Category: CategoryObserver, Message: exportSafeErrorMessage(&CategorizedError{Category: CategoryObserver, Err: err})}
}

func exportSafeErrorMessage(ce *CategorizedError) string {
	return truncateRunes(fmt.Sprintf("gxsql: %s", ce.Category), MaxExportedErrorMessageRunes)
}

func exportFacts(facts ResultFacts) (*ExportedFacts, error) {
	out := &ExportedFacts{}
	has := false

	if facts.ObservedCount != nil {
		out.ObservedCount = facts.ObservedCount
		has = true
	}
	if facts.ObservedFloat != nil {
		nv, err := normalizeFloat(*facts.ObservedFloat)
		if err != nil {
			return nil, err
		}
		out.ObservedFloat = &nv
		has = true
	}
	if facts.ObservedTime != nil {
		nv, err := normalizeValue(*facts.ObservedTime)
		if err != nil {
			return nil, err
		}
		out.ObservedTime = &nv
		has = true
	}
	if facts.ConfiguredCount != nil {
		out.ConfiguredCount = facts.ConfiguredCount
		has = true
	}
	if facts.ConfiguredCountLower != nil {
		out.ConfiguredCountLower = facts.ConfiguredCountLower
		has = true
	}
	if facts.ConfiguredCountUpper != nil {
		out.ConfiguredCountUpper = facts.ConfiguredCountUpper
		has = true
	}
	if facts.ConfiguredFloatLower != nil {
		nv, err := normalizeFloat(*facts.ConfiguredFloatLower)
		if err != nil {
			return nil, err
		}
		out.ConfiguredFloatLower = &nv
		has = true
	}
	if facts.ConfiguredFloatUpper != nil {
		nv, err := normalizeFloat(*facts.ConfiguredFloatUpper)
		if err != nil {
			return nil, err
		}
		out.ConfiguredFloatUpper = &nv
		has = true
	}
	if facts.ConfiguredFloatBound != nil {
		nv, err := normalizeFloat(*facts.ConfiguredFloatBound)
		if err != nil {
			return nil, err
		}
		out.ConfiguredFloatBound = &nv
		has = true
	}
	if facts.ConfiguredBound != nil {
		nv, err := normalizeValue(facts.ConfiguredBound)
		if err != nil {
			return nil, err
		}
		out.ConfiguredBound = &nv
		has = true
	}
	if facts.ConfiguredBoundLower != nil {
		nv, err := normalizeValue(facts.ConfiguredBoundLower)
		if err != nil {
			return nil, err
		}
		out.ConfiguredBoundLower = &nv
		has = true
	}
	if facts.ConfiguredBoundUpper != nil {
		nv, err := normalizeValue(facts.ConfiguredBoundUpper)
		if err != nil {
			return nil, err
		}
		out.ConfiguredBoundUpper = &nv
		has = true
	}
	if facts.ConfiguredTimeStart != nil {
		nv, err := normalizeValue(*facts.ConfiguredTimeStart)
		if err != nil {
			return nil, err
		}
		out.ConfiguredTimeStart = &nv
		has = true
	}
	if facts.ConfiguredTimeEnd != nil {
		nv, err := normalizeValue(*facts.ConfiguredTimeEnd)
		if err != nil {
			return nil, err
		}
		out.ConfiguredTimeEnd = &nv
		has = true
	}
	if facts.ConfiguredTimeCutoff != nil {
		nv, err := normalizeValue(*facts.ConfiguredTimeCutoff)
		if err != nil {
			return nil, err
		}
		out.ConfiguredTimeCutoff = &nv
		has = true
	}
	if facts.ConfiguredMaxFailedCount != nil {
		out.ConfiguredMaxFailedCount = facts.ConfiguredMaxFailedCount
		has = true
	}
	if facts.ConfiguredMaxFailedPercent != nil {
		percent := *facts.ConfiguredMaxFailedPercent
		if math.IsNaN(percent) || math.IsInf(percent, 0) {
			return nil, &CategorizedError{
				Category: CategoryObserver,
				Err:      fmt.Errorf("configured max failed percent must be finite"),
			}
		}
		out.ConfiguredMaxFailedPercent = facts.ConfiguredMaxFailedPercent
		has = true
	}
	if facts.Sum != nil {
		out.Sum = &ExportedSumFacts{
			Observed:        facts.Sum.Observed,
			ConfiguredLower: facts.Sum.ConfiguredLower,
			ConfiguredUpper: facts.Sum.ConfiguredUpper,
			Exactness:       facts.Sum.Exactness,
		}
		var err error
		out.Sum.ObservedFloat, err = exportOptionalFloat(facts.Sum.ObservedFloat)
		if err != nil {
			return nil, err
		}
		out.Sum.ConfiguredFloatLower, err = exportOptionalFloat(facts.Sum.ConfiguredFloatLower)
		if err != nil {
			return nil, err
		}
		out.Sum.ConfiguredFloatUpper, err = exportOptionalFloat(facts.Sum.ConfiguredFloatUpper)
		if err != nil {
			return nil, err
		}
		has = true
	}
	if facts.PopulationStdDev != nil {
		out.PopulationStdDev = &ExportedPopulationStdDevFacts{
			Algorithm: facts.PopulationStdDev.Algorithm,
			Exactness: facts.PopulationStdDev.Exactness,
		}
		var err error
		out.PopulationStdDev.Observed, err = exportOptionalFloat(facts.PopulationStdDev.Observed)
		if err != nil {
			return nil, err
		}
		out.PopulationStdDev.ConfiguredLower, err = exportOptionalFloat(facts.PopulationStdDev.ConfiguredLower)
		if err != nil {
			return nil, err
		}
		out.PopulationStdDev.ConfiguredUpper, err = exportOptionalFloat(facts.PopulationStdDev.ConfiguredUpper)
		if err != nil {
			return nil, err
		}
		has = true
	}
	if facts.Completeness != nil {
		var err error
		out.Completeness, err = exportRateFacts(
			facts.Completeness.NonNullCount, nil, facts.Completeness.TotalCount,
			facts.Completeness.Rate, facts.Completeness.ConfiguredBound,
			facts.Completeness.ConfiguredLower, facts.Completeness.ConfiguredUpper,
		)
		if err != nil {
			return nil, err
		}
		has = true
	}
	if facts.DuplicateRate != nil {
		var err error
		out.DuplicateRate, err = exportRateFacts(
			nil, facts.DuplicateRate.DuplicateCount, facts.DuplicateRate.TotalCount,
			facts.DuplicateRate.Rate, facts.DuplicateRate.ConfiguredBound,
			facts.DuplicateRate.ConfiguredLower, facts.DuplicateRate.ConfiguredUpper,
		)
		if err != nil {
			return nil, err
		}
		has = true
	}
	if facts.Frequency != nil {
		out.Frequency = &ExportedFrequencyFacts{
			ConfiguredNull: facts.Frequency.ConfiguredNull,
			ValueCount:     facts.Frequency.ValueCount,
			TotalCount:     facts.Frequency.TotalCount,
		}
		if facts.Frequency.ConfiguredValue != nil {
			nv, err := normalizeValue(facts.Frequency.ConfiguredValue)
			if err != nil {
				return nil, err
			}
			out.Frequency.ConfiguredValue = &nv
		}
		var err error
		out.Frequency.Share, err = exportOptionalFloat(facts.Frequency.Share)
		if err != nil {
			return nil, err
		}
		out.Frequency.ConfiguredBound, err = exportOptionalFloat(facts.Frequency.ConfiguredBound)
		if err != nil {
			return nil, err
		}
		out.Frequency.ConfiguredLower, err = exportOptionalFloat(facts.Frequency.ConfiguredLower)
		if err != nil {
			return nil, err
		}
		out.Frequency.ConfiguredUpper, err = exportOptionalFloat(facts.Frequency.ConfiguredUpper)
		if err != nil {
			return nil, err
		}
		has = true
	}
	if facts.DominantShare != nil {
		out.DominantShare = &ExportedDominantShareFacts{
			DominantCount: facts.DominantShare.DominantCount,
			TotalCount:    facts.DominantShare.TotalCount,
			TieCount:      facts.DominantShare.TieCount,
		}
		var err error
		out.DominantShare.Share, err = exportOptionalFloat(facts.DominantShare.Share)
		if err != nil {
			return nil, err
		}
		out.DominantShare.ConfiguredBound, err = exportOptionalFloat(facts.DominantShare.ConfiguredBound)
		if err != nil {
			return nil, err
		}
		out.DominantShare.ConfiguredLower, err = exportOptionalFloat(facts.DominantShare.ConfiguredLower)
		if err != nil {
			return nil, err
		}
		out.DominantShare.ConfiguredUpper, err = exportOptionalFloat(facts.DominantShare.ConfiguredUpper)
		if err != nil {
			return nil, err
		}
		has = true
	}
	if facts.ObservedTimePresent != nil {
		out.ObservedTimePresent = facts.ObservedTimePresent
		has = true
	}
	if len(facts.KeyColumns) > 0 {
		out.KeyColumns = append([]string(nil), facts.KeyColumns...)
		has = true
	}
	if facts.Reference != nil {
		out.Reference = &ExportedReferenceFacts{
			LocalColumns: append([]string(nil), facts.Reference.LocalColumns...),
			Parent: ExportedTarget{
				Schema: facts.Reference.Parent.Schema,
				Table:  facts.Reference.Parent.Name,
			},
			ParentColumns:  append([]string(nil), facts.Reference.ParentColumns...),
			ParentFilterID: facts.Reference.ParentFilterID,
		}
		has = true
	}
	if facts.Comparison != nil {
		out.Comparison = &ExportedComparisonFacts{
			LeftColumn:   facts.Comparison.LeftColumn,
			RightColumn:  facts.Comparison.RightColumn,
			Relationship: facts.Comparison.Relationship,
		}
		has = true
	}
	if facts.Ratio != nil {
		out.Ratio = &ExportedRatioFacts{
			LeftColumn:  facts.Ratio.LeftColumn,
			RightColumn: facts.Ratio.RightColumn,
			Bound:       facts.Ratio.Bound,
		}
		has = true
	}
	if facts.Reconcile != nil {
		out.Reconcile = &ExportedReconcileFacts{
			Left: ExportedTarget{
				Schema: facts.Reconcile.Left.Schema,
				Table:  facts.Reconcile.Left.Name,
			},
			Right: ExportedTarget{
				Schema: facts.Reconcile.Right.Schema,
				Table:  facts.Reconcile.Right.Name,
			},
			ObservedLeftCount:  facts.Reconcile.ObservedLeftCount,
			ObservedRightCount: facts.Reconcile.ObservedRightCount,
			Relationship:       facts.Reconcile.Relationship,
			LeftScopeID:        facts.Reconcile.LeftScopeID,
			SecondaryFilterID:  facts.Reconcile.SecondaryFilterID,
		}
		has = true
	}
	if len(facts.RequiredColumns) > 0 {
		out.RequiredColumns = append([]string(nil), facts.RequiredColumns...)
		has = true
	}
	if len(facts.MissingColumns) > 0 {
		out.MissingColumns = append([]string(nil), facts.MissingColumns...)
		has = true
	}
	if len(facts.UnexpectedColumns) > 0 {
		out.UnexpectedColumns = append([]string(nil), facts.UnexpectedColumns...)
		has = true
	}
	if facts.ConfiguredNullability != "" {
		out.ConfiguredNullability = facts.ConfiguredNullability
		has = true
	}
	if facts.ObservedNullability != "" {
		out.ObservedNullability = facts.ObservedNullability
		has = true
	}
	if facts.ConfiguredReportedType != "" {
		out.ConfiguredReportedType = facts.ConfiguredReportedType
		has = true
	}
	if facts.ObservedReportedType != "" {
		out.ObservedReportedType = facts.ObservedReportedType
		has = true
	}
	if !has {
		return nil, nil
	}
	return out, nil
}
func exportOptionalFloat(value *float64) (*NormalizedValue, error) {
	if value == nil {
		return nil, nil
	}
	nv, err := normalizeFloat(*value)
	if err != nil {
		return nil, err
	}
	return &nv, nil
}

func exportRateFacts(nonNull, duplicate, total *int, rate, bound, lower, upper *float64) (*ExportedRateFacts, error) {
	out := &ExportedRateFacts{
		NonNullCount:   nonNull,
		DuplicateCount: duplicate,
		TotalCount:     total,
	}
	var err error
	out.Rate, err = exportOptionalFloat(rate)
	if err != nil {
		return nil, err
	}
	out.ConfiguredBound, err = exportOptionalFloat(bound)
	if err != nil {
		return nil, err
	}
	out.ConfiguredLower, err = exportOptionalFloat(lower)
	if err != nil {
		return nil, err
	}
	out.ConfiguredUpper, err = exportOptionalFloat(upper)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func exportDisplayName(res Result) string {
	return exportDisplayBase(res) + exportObservedSuffix(res.Name)
}

func exportObservedSuffix(name string) string {
	if idx := strings.Index(name, ": got "); idx >= 0 {
		return name[idx:]
	}
	return ""
}

func exportDisplayNameFallback(name string) string {
	if idx := strings.Index(name, ": got "); idx >= 0 {
		return strings.TrimSpace(name[:idx])
	}
	return name
}

func exportDisplayBase(res Result) string {
	col := res.Column
	switch res.Kind {
	case KindIn:
		if col == "" {
			return exportDisplayNameFallback(res.Name)
		}
		return col + " in (...)"
	case KindNotIn:
		if col == "" {
			return exportDisplayNameFallback(res.Name)
		}
		return col + " not in (...)"
	case KindBetween:
		return col + " between"
	case KindGreaterThan:
		return col + " > (...)"
	case KindLessThan:
		return col + " < (...)"
	case KindGreaterOrEqual:
		return col + " >= (...)"
	case KindLessOrEqual:
		return col + " <= (...)"
	case KindEqualColumn:
		return exportComparisonDisplay(res, "=")
	case KindNotEqualColumn:
		return exportComparisonDisplay(res, "<>")
	case KindLessThanColumn:
		return exportComparisonDisplay(res, "<")
	case KindLessOrEqualColumn:
		return exportComparisonDisplay(res, "<=")
	case KindGreaterThanColumn:
		return exportComparisonDisplay(res, ">")
	case KindGreaterOrEqualColumn:
		return exportComparisonDisplay(res, ">=")
	case KindRatioEqual:
		if res.Facts.Ratio != nil {
			return res.Facts.Ratio.LeftColumn + " ratio == (...)"
		}
		return exportDisplayNameFallback(res.Name)
	case KindLenEqual:
		return col + " length"
	case KindLenBetween:
		return col + " length between"
	case KindHasPrefix:
		return exportPatternDisplay(col, "has prefix")
	case KindHasSuffix:
		return exportPatternDisplay(col, "has suffix")
	case KindContains:
		return exportPatternDisplay(col, "contains")
	case KindLike:
		return exportPatternDisplay(col, "like")
	case KindNotLike:
		return exportPatternDisplay(col, "not like")
	case KindRegex:
		return exportPatternDisplay(col, "regex")
	case KindRowCountEqual:
		return "row count"
	case KindRowCountBetween:
		return "row count between"
	case KindRowCountGreaterThan:
		return "row count > (...)"
	case KindRowCountGreaterEqual:
		return "row count >= (...)"
	case KindRowCountLessThan:
		return "row count < (...)"
	case KindRowCountLessEqual:
		return "row count <= (...)"
	case KindDistinctCountEqual:
		return col + " distinct count"
	case KindDistinctCountBetween:
		return col + " distinct count between"
	case KindDistinctCountGreaterThan:
		return col + " distinct count > (...)"
	case KindDistinctCountGreaterEqual:
		return col + " distinct count >= (...)"
	case KindDistinctCountLessThan:
		return col + " distinct count < (...)"
	case KindDistinctCountLessEqual:
		return col + " distinct count <= (...)"
	case KindAverageBetween:
		return col + " average between"
	case KindMinGreaterOrEqual:
		return col + " min >= (...)"
	case KindMaxLessOrEqual:
		return col + " max <= (...)"
	case KindTimestampInWindow:
		return col + " in window"
	case KindTimestampFreshSince:
		return col + " fresh since"
	default:
		name := res.Name
		if idx := strings.Index(name, ": got "); idx >= 0 {
			return strings.TrimSpace(name[:idx])
		}
		return name
	}
}

func exportPatternDisplay(column, operation string) string {
	if column == "" {
		return "pattern " + operation + " (...)"
	}
	return column + " " + operation + " (...)"
}

func exportComparisonDisplay(res Result, relationship string) string {
	if res.Facts.Comparison != nil {
		facts := res.Facts.Comparison
		return facts.LeftColumn + " " + relationship + " " + facts.RightColumn
	}
	return exportDisplayNameFallback(res.Name)
}

func exportSamples(res Result, cfg exportConfig) ([]NormalizedValue, *ExportedCaps, error) {
	vals := res.SampleValues
	if cfg.sampleRedactor != nil {
		redacted := make([]any, len(vals))
		for i, v := range vals {
			rv, err := applyRedactor(cfg.sampleRedactor, v)
			if err != nil {
				return nil, nil, observerExportError("sample", err)
			}
			redacted[i] = rv
		}
		vals = redacted
	}
	out, err := normalizeValues(vals)
	if err != nil {
		return nil, nil, err
	}
	var caps *ExportedCaps
	if res.FailedCount > 0 {
		caps = &ExportedCaps{SamplesReturned: len(out)}
		if res.FailedCount > len(out) {
			caps.SamplesTruncated = true
		}
	}
	return out, caps, nil
}

func exportFailedKeys(res Result, cfg exportConfig) ([]NormalizedValue, *ExportedCaps, error) {
	keys := make([]any, len(res.FailedKeys))
	for i, key := range res.FailedKeys {
		k := any(key)
		if cfg.keyRedactor != nil {
			rv, err := applyRedactor(cfg.keyRedactor, k)
			if err != nil {
				return nil, nil, observerExportError("failed key", err)
			}
			k = rv
		}
		keys[i] = k
	}
	out, err := normalizeValues(keys)
	if err != nil {
		return nil, nil, err
	}
	var caps *ExportedCaps
	if res.FailedCount > 0 {
		caps = &ExportedCaps{KeysReturned: len(out)}
		if res.FailedCount > len(out) {
			caps.KeysTruncated = true
		}
	}
	return out, caps, nil
}

func exportDiagnostics(diag *resultDiagnostics, target *TableRef, cfg exportConfig) (*ExportedDiagnostics, error) {
	out := &ExportedDiagnostics{}
	if cfg.includeQueryDiagnostics {
		query := redactQueryIdentity(diag.query, target)
		query, truncated := truncateWithFlag(query, MaxExportedQueryTextRunes)
		out.QueryTruncated = truncated
		if cfg.queryRedactor != nil {
			rv, err := applyRedactor(cfg.queryRedactor, query)
			if err != nil {
				return nil, observerExportError("query", err)
			}
			s, ok := rv.(string)
			if !ok {
				return nil, observerExportError("query", fmt.Errorf("redactor returned %T, want string", rv))
			}
			query = s
		}
		query, postTruncated := truncateWithFlag(query, MaxExportedQueryTextRunes)
		out.QueryTruncated = out.QueryTruncated || postTruncated
		out.Query = query
	}
	if cfg.includeCapturedArgs {
		args := diag.args
		if len(args) > MaxExportedArgumentCount {
			args = args[:MaxExportedArgumentCount]
			out.ArgsTruncated = true
		}
		if cfg.argsRedactor != nil {
			redacted := make([]any, len(args))
			for i, arg := range args {
				rv, err := applyRedactor(cfg.argsRedactor, arg)
				if err != nil {
					return nil, observerExportError("argument", err)
				}
				redacted[i] = rv
			}
			args = redacted
		}
		norm, err := normalizeValues(args)
		if err != nil {
			return nil, err
		}
		out.Args = norm
	}
	return out, nil
}

func redactQueryIdentity(query string, target *TableRef) string {
	if target == nil || target.Name == "" {
		return query
	}

	replacements := make([]string, 0, 8)
	if target.Schema != "" {
		for _, quote := range []string{`"`, "`"} {
			replacements = append(replacements,
				quote+target.Schema+quote+"."+quote+target.Name+quote,
				quote+target.Schema+quote+"."+target.Name,
				target.Schema+"."+quote+target.Name+quote,
			)
		}
		replacements = append(replacements, target.Schema+"."+target.Name)
	} else {
		replacements = append(replacements,
			`"`+target.Name+`"`,
			"`"+target.Name+"`",
			target.Name,
		)
	}
	for _, old := range replacements {
		query = strings.ReplaceAll(query, old, "<table>")
	}
	return query
}

func truncateRunes(s string, max int) string {
	out, _ := truncateWithFlag(s, max)
	return out
}

func truncateWithFlag(s string, max int) (string, bool) {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s, false
	}
	runes := []rune(s)
	return string(runes[:max]), true
}

func applyRedactor(fn Redactor, v any) (rv any, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("redactor panic: %v", p)
		}
	}()
	return fn(v)
}

func observerExportError(what string, err error) error {
	if err == nil {
		return nil
	}
	return &CategorizedError{
		Category: CategoryObserver,
		Err:      fmt.Errorf("export %s redaction: %w", what, err),
	}
}
