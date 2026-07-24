package gxsql

import (
	"context"
	"fmt"
)

// DefaultFailedKeysCap is the default maximum failed-row keys retained per Result.
// Use WithFailedKeysCap(0) for unlimited retention when every failing key is
// required for remediation.
const DefaultFailedKeysCap = 100

// Suite is an ordered set of SQL expectations over a database table.
type Suite struct {
	expectations  []Expectation
	sampleCap     int
	failedKeysCap int
}

// NewSuite builds a suite from the given expectations, evaluated in order.
func NewSuite(exps ...Expectation) *Suite {
	return &Suite{
		expectations:  exps,
		sampleCap:     DefaultSampleCap,
		failedKeysCap: DefaultFailedKeysCap,
	}
}

// WithSampleCap sets the maximum sample values retained per Result and returns
// the suite for chaining. Zero disables sample collection.
func (s *Suite) WithSampleCap(n int) *Suite {
	s.sampleCap = n
	return s
}

// WithFailedKeysCap sets the maximum failed-row keys retained per Result and
// returns the suite for chaining. Zero means unlimited. FailedCount and
// FailedPercent remain complete when keys are capped.
func (s *Suite) WithFailedKeysCap(n int) *Suite {
	s.failedKeysCap = n
	return s
}

// Option configures a ValidateTable run.
type Option func(*validateConfig)

type validateConfig struct {
	dialect            Dialect
	sampleCap          int
	failedKeysCap      int
	keyColumns         []string
	summaryOnly        bool
	continueOnError    bool
	captureDiagnostics bool
	scope              Scope
	hasScope           bool
}

// WithScope limits every expectation to rows matching the trusted predicate.
// Scope configuration is validated when ValidateTable starts.
func WithScope(scope Scope) Option {
	return func(cfg *validateConfig) {
		cfg.scope = scope
		cfg.hasScope = true
	}
}

// WithDialect selects the SQL dialect used to render queries.
func WithDialect(d Dialect) Option {
	return func(cfg *validateConfig) { cfg.dialect = d }
}

// WithSampleCap overrides the per-result sample cap for one ValidateTable call.
// Zero disables sample collection.
func WithSampleCap(n int) Option {
	return func(cfg *validateConfig) { cfg.sampleCap = n }
}

// WithFailedKeysCap overrides the per-result failed-key cap for one
// ValidateTable call. Zero means unlimited. FailedCount and FailedPercent
// remain complete when keys are capped.
func WithFailedKeysCap(n int) Option {
	return func(cfg *validateConfig) { cfg.failedKeysCap = n }
}

// WithKey enables failed-row identity using the given key columns. Disables
// summary-only mode. Key column names must pass identifier validation. Failed
// keys are capped by DefaultFailedKeysCap unless WithFailedKeysCap(0) is set.
func WithKey(columns ...string) Option {
	return func(cfg *validateConfig) {
		cfg.keyColumns = append([]string(nil), columns...)
		cfg.summaryOnly = false
	}
}

// SummaryOnly disables complete failed-row identity and returns counts plus
// capped samples only.
func SummaryOnly() Option {
	return func(cfg *validateConfig) {
		cfg.summaryOnly = true
		cfg.keyColumns = nil
	}
}

// ContinueOnError records expectation preflight issues and execution errors on
// individual results and keeps evaluating later expectations. Preflight issues
// occupy their declaration-order slots with Result.Err set. Run-level option
// errors (invalid dialect, caps, key columns, or scope) still abort ValidateTable
// with a top-level error before evaluation starts. By default, the first
// preflight or execution error aborts ValidateTable with a zero report.
func ContinueOnError() Option {
	return func(cfg *validateConfig) { cfg.continueOnError = true }
}

// CaptureQueryDiagnostics records SQL text and bound arguments on each Result
// for optional export via IncludeCapturedDiagnostics. Captured content is never
// exposed through default Result serialization or default export.
func CaptureQueryDiagnostics() Option {
	return func(cfg *validateConfig) { cfg.captureDiagnostics = true }
}

// ValidateTable runs every expectation in declaration order (collect-all, never
// fail-fast on policy failures) and returns the aggregated Report. It uses
// Postgres when WithDialect is not supplied.
//
// Validation-policy failures are returned as (report, nil); gate with
// report.Err(), which yields a *ValidationError recoverable via errors.As.
// Run-level option errors (including invalid scope) abort with (zero Report, err)
// before evaluation starts. Expectation preflight failures collected before SQL
// starts return a zero Report and *PreflightErrors unless ContinueOnError is set,
// each affected slot records Result.Err. The first database, rendering, scan,
// or context error aborts with a zero Report and error unless ContinueOnError
// is set, when the error is recorded on that result and evaluation continues.
func (s *Suite) ValidateTable(
	ctx context.Context,
	db DB,
	table TableRef,
	opts ...Option,
) (Report, error) {
	cfg := validateConfig{
		dialect:       Postgres(),
		sampleCap:     s.sampleCap,
		failedKeysCap: s.failedKeysCap,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.dialect == nil {
		return Report{}, newConfigError(fmt.Errorf("dialect is required"))
	}
	if cfg.sampleCap < 0 {
		return Report{}, newConfigError(fmt.Errorf("sample cap must be non-negative"))
	}
	if cfg.failedKeysCap < 0 {
		return Report{}, newConfigError(fmt.Errorf("failed keys cap must be non-negative"))
	}
	var validatedScope *trustedScope
	if cfg.hasScope {
		scope, err := validateScope(cfg.scope)
		if err != nil {
			return Report{}, err
		}
		validatedScope = &scope
	}

	if !cfg.summaryOnly && len(cfg.keyColumns) == 0 {
		cfg.summaryOnly = true
	}
	if !cfg.summaryOnly {
		for _, col := range cfg.keyColumns {
			if err := validateIdent(col); err != nil {
				return Report{}, newConfigError(err)
			}
		}
	}

	pf := newPreflightState()
	for i, exp := range s.expectations {
		pf.check(i, exp)
	}
	if len(pf.issues) > 0 && !cfg.continueOnError {
		return Report{}, &PreflightErrors{Issues: pf.issues}
	}

	evalOpts := evalOptions{
		dialect:            cfg.dialect,
		sampleCap:          cfg.sampleCap,
		failedKeysCap:      cfg.failedKeysCap,
		keyColumns:         cfg.keyColumns,
		summaryOnly:        cfg.summaryOnly,
		captureDiagnostics: cfg.captureDiagnostics,
		scope:              validatedScope,
	}

	results := make([]Result, len(s.expectations))
	for i, exp := range s.expectations {
		if pf.hasIssueAt(i) {
			results[i] = configErrorResult(exp, pf.errAt(i))
			continue
		}
		if exp == nil {
			results[i] = configErrorResult(nil, newConfigError(fmt.Errorf("nil expectation at index %d", i)))
			continue
		}
		res, err := exp.evaluateSQL(ctx, db, table, evalOpts)
		if res.Kind == "" {
			res.Kind = expectationKind(exp)
		}
		if id := expectationID(exp); id != "" && res.ID == "" {
			res.ID = id
		}
		if err != nil {
			if cfg.continueOnError {
				if res.Err == nil {
					res.Err = err
				}
				res.Success = false
				results[i] = res
				continue
			}
			return Report{}, err
		}
		if res.Err != nil {
			res.Success = false
		}
		results[i] = res
	}
	scopeID := ""
	if validatedScope != nil {
		scopeID = validatedScope.identity
	}
	target := table
	return Report{Results: results, Target: &target, ScopeID: scopeID}, nil
}
