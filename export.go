package gxsql

import (
	"errors"
	"fmt"
	"strings"
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

// ExportedScope is reserved for a validation scope and is omitted by the
// current release.
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
	// PolicyVerdict is pass, fail, or unevaluated when no policy verdict was produced.
	PolicyVerdict PolicyVerdict `json:"policy_verdict"`
	// ExecutionOutcome distinguishes policy failure from execution/config failure.
	ExecutionOutcome ExecutionOutcome `json:"execution_outcome"`
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
	// ConfiguredCount is the exact-count threshold for equal-style expectations.
	ConfiguredCount *int `json:"configured_count,omitempty"`
	// ConfiguredCountLower and ConfiguredCountUpper are inclusive integer bounds.
	ConfiguredCountLower *int `json:"configured_count_lower,omitempty"`
	ConfiguredCountUpper *int `json:"configured_count_upper,omitempty"`
	// ConfiguredFloatLower, ConfiguredFloatUpper, and ConfiguredFloatBound are
	// thresholds for floating-point aggregate expectations.
	ConfiguredFloatLower *NormalizedValue `json:"configured_float_lower,omitempty"`
	ConfiguredFloatUpper *NormalizedValue `json:"configured_float_upper,omitempty"`
	ConfiguredFloatBound *NormalizedValue `json:"configured_float_bound,omitempty"`
	// ConfiguredBound, ConfiguredBoundLower, and ConfiguredBoundUpper hold
	// per-row comparison thresholds with driver-bound types.
	ConfiguredBound      *NormalizedValue `json:"configured_bound,omitempty"`
	ConfiguredBoundLower *NormalizedValue `json:"configured_bound_lower,omitempty"`
	ConfiguredBoundUpper *NormalizedValue `json:"configured_bound_upper,omitempty"`
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

// ExportReport converts report into a versioned JSON DTO. On error, no partial
// DTO is returned. Query text, bound arguments, samples, and failed keys are
// omitted unless explicitly enabled via ExportOption.
func ExportReport(report Report, opts ...ExportOption) (ExportedReport, error) {
	cfg := exportConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	out := ExportedReport{
		SchemaVersion: ExportSchemaVersion,
		Results:       make([]ExportedResult, 0, len(report.Results)),
	}
	if report.Target != nil {
		out.Target = &ExportedTarget{
			Schema: report.Target.Schema,
			Table:  report.Target.Name,
		}
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

func exportResult(res Result, target *TableRef, cfg exportConfig) (ExportedResult, error) {
	out := ExportedResult{
		ID:               res.ID,
		Kind:             res.Kind,
		DisplayName:      exportDisplayName(res),
		Column:           res.Column,
		PolicyVerdict:    policyVerdict(res),
		ExecutionOutcome: executionOutcome(res),
		RowDenominator:   res.RowDenominator,
	}

	if res.RowDenominator == RowDenominatorAvailable {
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
	if !has {
		return nil, nil
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
	case KindLenEqual:
		return col + " length"
	case KindLenBetween:
		return col + " length between"
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
	default:
		name := res.Name
		if idx := strings.Index(name, ": got "); idx >= 0 {
			return strings.TrimSpace(name[:idx])
		}
		return name
	}
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
	replacements := make([]string, 0, 4)
	if target.Schema != "" {
		replacements = append(replacements,
			`"`+target.Schema+`"."`+target.Name+`"`,
			target.Schema+"."+target.Name,
			`"`+target.Schema+`".`,
			target.Schema+".",
		)
	}
	replacements = append(replacements, `"`+target.Name+`"`, target.Name)
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
