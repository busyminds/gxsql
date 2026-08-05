package gxsql

import (
	"fmt"
	"strings"
	"time"
)

// RowDenominator reports whether Total and FailedPercent are meaningful for a
// result. Table-level checks use RowDenominatorUnavailable so zero totals are
// not confused with an empty evaluated population.
type RowDenominator string

const (
	// RowDenominatorAvailable marks per-row expectations where Total is the
	// table row count and FailedPercent is computed from FailedCount.
	RowDenominatorAvailable RowDenominator = "available"
	// RowDenominatorUnavailable marks table-level expectations where row counts
	// and percentages are not meaningful.
	RowDenominatorUnavailable RowDenominator = "unavailable"
)

// ResultFacts holds machine-readable observations and configured thresholds
// separate from display text. Threshold fields are populated by built-in
// expectations at construction time and must not be parsed from Name.
type ResultFacts struct {
	// ObservedCount is a table-level integer observation when set.
	ObservedCount *int
	// ObservedFloat is a floating-point aggregate observation when set.
	ObservedFloat *float64
	// ObservedTime is the observed maximum timestamp for freshness checks when
	// ObservedTimePresent is true. Nil with Present false means explicit
	// absence (empty or all-NULL scope). Retains nanosecond precision.
	ObservedTime *time.Time

	// ConfiguredCount is the exact-count threshold for equal-style expectations.
	ConfiguredCount *int
	// ConfiguredCountLower is the inclusive lower integer bound.
	ConfiguredCountLower *int
	// ConfiguredCountUpper is the inclusive upper integer bound.
	ConfiguredCountUpper *int
	// ConfiguredFloatLower is the inclusive lower floating-point bound.
	ConfiguredFloatLower *float64
	// ConfiguredFloatUpper is the inclusive upper floating-point bound.
	ConfiguredFloatUpper *float64
	// ConfiguredFloatBound is the single-sided floating-point threshold.
	ConfiguredFloatBound *float64
	// ConfiguredBound is the per-row comparison threshold with a driver-bound type.
	ConfiguredBound any
	// ConfiguredBoundLower is the inclusive per-row lower bound.
	ConfiguredBoundLower any
	// ConfiguredBoundUpper is the inclusive per-row upper bound.
	ConfiguredBoundUpper any
	// ConfiguredTimeStart is the caller-configured half-open window start.
	ConfiguredTimeStart *time.Time
	// ConfiguredTimeEnd is the caller-configured half-open window end.
	ConfiguredTimeEnd *time.Time
	// ConfiguredTimeCutoff is the caller-configured freshness cutoff.
	ConfiguredTimeCutoff *time.Time

	// ConfiguredMaxFailedCount is the inclusive maximum failed-row bound when a
	// WithMaxFailedCount policy decorated this result. Nil means no tolerance
	// was applied.
	ConfiguredMaxFailedCount *int

	// ObservedTimePresent marks whether a freshness observation was produced.
	// Nil means observation is not applicable (non-freshness results). A
	// pointer to false means the maximum is explicitly absent; true means
	// ObservedTime holds the observed maximum.
	ObservedTimePresent *bool

	// KeyColumns names local composite-key components in declaration order.
	// Used by multi-column uniqueness (and similar) so tuples are not encoded
	// as comma-separated Result.Column text. Empty when unset.
	KeyColumns []string
	// Reference holds local-to-parent mapping facts for referential integrity
	// expectations. Nil when this result is not a reference check.
	Reference *ReferenceFacts
	// Comparison holds same-row operand and relationship facts.
	Comparison *ComparisonFacts
	// Ratio holds same-row integer ratio facts.
	Ratio *RatioFacts
}

// ComparisonFacts identifies the operands and fixed relationship of a direct
// same-row comparison.
type ComparisonFacts struct {
	LeftColumn   string
	RightColumn  string
	Relationship string
}

// RatioFacts identifies the operands and integral bound of a ratio equality.
type RatioFacts struct {
	LeftColumn  string
	RightColumn string
	Bound       int64
}

// ReferenceFacts describes a local-to-parent column mapping for a referential
// integrity expectation. Column slices preserve declaration order. Parent is a
// structured TableRef so schema-qualified targets stay structured rather than
// rendered into Column text.
type ReferenceFacts struct {
	// LocalColumns are the local foreign-key components in declaration order.
	LocalColumns []string
	// Parent is the unscoped parent target identified by Schema and Name.
	Parent TableRef
	// ParentColumns are the parent key components mapped 1:1 to LocalColumns.
	ParentColumns []string
}

// resultDiagnostics holds captured SQL text and bound arguments for export.
// Populated only when ValidateTable runs with CaptureQueryDiagnostics; never
// included in default Result serialization paths.
type resultDiagnostics struct {
	query string
	args  []any
}

// resultShape discriminates published result profiles that share a Kind value.
type resultShape uint8

const (
	resultShapeNone resultShape = iota
	resultShapeCustomCount
)

// Result is the outcome of one expectation over a single ValidateTable run.
//
// Per-row SQL expectations set RowDenominatorAvailable, Total to the table row
// count, and populate FailedCount, FailedPercent, SampleValues, and optionally
// FailedKeys on failure. FailedKeys are capped unless WithFailedKeysCap(0)
// selects unlimited retention. Table-level checks use RowDenominatorUnavailable;
// custom counts still expose their expectation-specific FailedCount while
// Success carries the verdict and Facts carry observed values while Name holds
// human-oriented display text.
type Result struct {
	// ID is the optional caller-supplied stable identifier from WithID.
	ID string
	// Kind is the library-defined machine identifier for the expectation.
	Kind ExpectationKind
	// Name is human-readable display text and is not machine identity.
	Name string
	// Column is the validated SQL column for per-row checks and aggregates.
	Column string
	// Success is the policy verdict. False when the check fails or Result.Err is set.
	Success bool
	// RowDenominator states whether Total and FailedPercent describe rows.
	RowDenominator RowDenominator
	// Total is the evaluated row population when RowDenominator is available.
	Total int
	// FailedCount is the complete expectation-specific failure count; for per-row
	// checks it counts failing rows, while CustomCount may count groups or other
	// query-defined failures.
	FailedCount int
	// FailedPercent is the percentage of failing rows when the denominator is available.
	FailedPercent float64
	// Facts contains structured observations and configured thresholds.
	Facts ResultFacts
	// SampleValues holds capped offending column values on per-row failure.
	SampleValues []any
	// FailedKeys holds failing row keys in WithKey column order, capped unless
	// WithFailedKeysCap(0) selects unlimited retention.
	FailedKeys []RowKey
	// Err is a categorized configuration or execution failure when non-nil.
	Err error

	// Tolerated is true when a nonzero raw FailedCount passed within a
	// configured WithMaxFailedCount bound. Clean passes, above-bound failures,
	// empty populations, and errors are never tolerated.
	Tolerated bool

	shape       resultShape
	diagnostics *resultDiagnostics
}

// RowKey identifies a failing table row by caller-supplied key column values in
// the same order as the WithKey columns passed to ValidateTable.
type RowKey []any

// Report aggregates the results of every expectation in a suite.
type Report struct {
	// Results preserves declaration order, including slots recorded under
	// ContinueOnError.
	Results []Result
	// Target names the validated table. Set by ValidateTable; nil when unavailable
	// (for example when a Report is assembled manually).
	Target *TableRef
	// ScopeID identifies the validation scope. It is empty when validation is
	// unscoped.
	ScopeID string
}

// OK reports whether every expectation passed (Success is true for all results).
func (r Report) OK() bool {
	for _, res := range r.Results {
		if !res.Success {
			return false
		}
	}
	return true
}

// Failures returns results with Success false, including configuration and
// execution failures recorded under ContinueOnError.
func (r Report) Failures() []Result {
	var out []Result
	for _, res := range r.Results {
		if !res.Success {
			out = append(out, res)
		}
	}
	return out
}

// Err returns nil when the report is OK, otherwise a *ValidationError carrying
// the full report for gating and inspection.
func (r Report) Err() error {
	if r.OK() {
		return nil
	}
	return &ValidationError{Report: r}
}

// ValidationError wraps a failed Report as an error for runtime gating.
// Recover the full report via errors.As and the Report field.
type ValidationError struct {
	// Report is the complete validation outcome, including passing results.
	Report Report
}

// Error summarizes the number and display names of failed expectations.
func (e *ValidationError) Error() string {
	failures := e.Report.Failures()
	names := make([]string, len(failures))
	for i, res := range failures {
		names[i] = res.Name
	}
	return fmt.Sprintf("gxsql: %d expectation(s) failed: %s",
		len(failures), strings.Join(names, "; "))
}
