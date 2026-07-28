package gxsql

import (
	"context"
	"fmt"
	"strings"
)

// CountQuery is an immutable trusted SQL count template with bound arguments.
// Construct it with [TrustedCountQuery]. Validation is deferred until the
// query is used with [CustomCount] and evaluated by [Suite.ValidateTable].
type CountQuery struct {
	template string
	args     []any
}

// TrustedCountQuery returns an immutable [CountQuery] from trusted Go-code SQL
// template input and bound argument values. The template must contain exactly
// one {{target}} and one {{scope}} marker; validation is deferred until
// [Suite.ValidateTable].
func TrustedCountQuery(template string, args ...any) CountQuery {
	return CountQuery{
		template: template,
		args:     copyScopeValues(args),
	}
}

// CustomCount returns an expectation that executes query and treats the
// returned count as the expectation-specific failure count. Results use
// [KindCustom] with [RowDenominatorUnavailable] and expose [Result.FailedCount]
// without a row denominator, samples, or failed keys. name must be non-blank;
// validation is deferred until [Suite.ValidateTable].
func CustomCount(name string, query CountQuery) Expectation {
	return newCustomCountExpectation(name, query.template, query.args)
}

// customCountExpectation is the internal CustomCount declaration: a trusted
// count template, bound custom arguments, and a display name evaluated by
// evalCustomCount.
type customCountExpectation struct {
	displayName string
	template    string
	args        []any
}

func newCustomCountExpectation(displayName, template string, args []any) customCountExpectation {
	return customCountExpectation{
		displayName: displayName,
		template:    template,
		args:        copyScopeValues(args),
	}
}

// asCustomCountExpectation is the internal construction hook used by package tests.
func asCustomCountExpectation(displayName, template string, args ...any) Expectation {
	return newCustomCountExpectation(displayName, template, args)
}

// customCountResultProfile reports whether res matches the CustomCount result
// profile rather than a generic KindCustom result. Export and display helpers
// share this profile.
func customCountResultProfile(res Result) bool {
	if res.shape != resultShapeCustomCount {
		return false
	}
	if res.Kind != KindCustom || res.Err != nil {
		return false
	}
	if res.RowDenominator != RowDenominatorUnavailable || res.Column != "" {
		return false
	}
	if len(res.SampleValues) > 0 || len(res.FailedKeys) > 0 {
		return false
	}
	return true
}

// customCountResult builds a CustomCount result with a complete failed count
// and unavailable row denominator semantics.
func customCountResult(displayName string, failed int) (Result, error) {
	if strings.TrimSpace(displayName) == "" {
		return Result{}, customCountResultContractError(
			fmt.Errorf("custom count display name is required"),
		)
	}
	if failed < 0 {
		return Result{}, customCountResultContractError(errCustomCountResultFailedNegative)
	}
	res := Result{
		Kind:           KindCustom,
		Name:           displayName,
		Column:         "",
		Success:        failed == 0,
		RowDenominator: RowDenominatorUnavailable,
		FailedCount:    failed,
		shape:          resultShapeCustomCount,
	}
	if err := validateCustomCountResult(res); err != nil {
		return Result{}, err
	}
	return res, nil
}

// validateCustomCountResult checks CustomCount result invariants.
func validateCustomCountResult(res Result) error {
	switch {
	case res.Kind != KindCustom:
		return customCountResultContractError(errCustomCountResultKind)
	case res.Column != "":
		return customCountResultContractError(errCustomCountResultColumn)
	case res.RowDenominator != RowDenominatorUnavailable:
		return customCountResultContractError(errCustomCountResultDenominator)
	case res.Total != 0:
		return customCountResultContractError(errCustomCountResultTotal)
	case res.FailedPercent != 0:
		return customCountResultContractError(errCustomCountResultFailedPercent)
	case len(res.SampleValues) > 0:
		return customCountResultContractError(errCustomCountResultSamples)
	case len(res.FailedKeys) > 0:
		return customCountResultContractError(errCustomCountResultFailedKeys)
	case res.FailedCount < 0:
		return customCountResultContractError(errCustomCountResultFailedNegative)
	default:
		return nil
	}
}

// customCountDisplayFailed returns the failed count when res matches the custom-count
// profile and contract without changing generic KindCustom rendering.
func customCountDisplayFailed(res Result) (int, bool) {
	if !customCountResultProfile(res) {
		return 0, false
	}
	if err := validateCustomCountResult(res); err != nil {
		return 0, false
	}
	return res.FailedCount, true
}

func (e customCountExpectation) Name() string { return e.displayName }

func (e customCountExpectation) expectationKind() ExpectationKind { return KindCustom }

func (e customCountExpectation) preflight() error {
	if strings.TrimSpace(e.displayName) == "" {
		return newConfigError(fmt.Errorf("custom count display name is required"))
	}
	return preflightTrustedCount(e.template, e.args)
}

func (e customCountExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	return evalCustomCount(ctx, db, table, opts, e.Name(), e.template, e.boundArgs())
}

func (e customCountExpectation) boundArgs() []any { return copyScopeValues(e.args) }

var (
	_ Expectation     = customCountExpectation{}
	_ metaExpectation = customCountExpectation{}
)
