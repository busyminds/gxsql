package gxsql

import (
	"context"
	"fmt"
	"strings"
)

// Eligibility is an immutable rule-level eligibility predicate containing a
// caller identity, a dialect-neutral predicate authored with ? placeholders,
// and bound values. Its fields are intentionally unexported so eligibility
// data cannot be modified after construction. Eligibility narrows which rows
// inside the suite scope are subject to one expectation; it is distinct from
// suite [WithScope] and never rewrites [Report.ScopeID].
type Eligibility struct {
	identity  string
	predicate string
	values    []any
}

// trustedEligibility preserves the validated Eligibility representation for
// package evaluation paths.
type trustedEligibility = Eligibility

// TrustedEligibility constructs an Eligibility from trusted Go-code predicate
// input. Validation is deferred until suite preflight or evaluation.
func TrustedEligibility(id, predicate string, args ...any) Eligibility {
	return Eligibility{
		identity:  id,
		predicate: predicate,
		values:    copyScopeValues(args),
	}
}

// When decorates exp with rule-level eligibility. Only per-row, uniqueness,
// composite uniqueness, and referential-integrity expectations qualify;
// table-level, aggregate, distinct-count, custom-count, and structural
// declarations are rejected at ValidateTable preflight. Nested eligibility is
// rejected. The wrapper is immutable and may nest with WithID, WithPolicy, and
// WithMaxFailedCount in either order.
//
// Eligible rows form the denominator for percentages and tolerance. Ineligible
// rows neither pass nor fail. Zero eligible rows pass vacuously without a
// fabricated percentage or Tolerated mark.
func When(elig Eligibility, exp Expectation) Expectation {
	return &eligibilityExpectation{
		elig: Eligibility{
			identity:  elig.identity,
			predicate: elig.predicate,
			values:    copyScopeValues(elig.values),
		},
		inner: exp,
	}
}

// eligibilityExpectation is the immutable internal eligibility declaration
// constructed by When.
type eligibilityExpectation struct {
	elig  Eligibility
	inner Expectation
}

func (e *eligibilityExpectation) Name() string {
	if e.inner == nil {
		return "<configuration error>"
	}
	return e.inner.Name()
}

func (e *eligibilityExpectation) expectationKind() ExpectationKind {
	return expectationKind(e.inner)
}

func (e *eligibilityExpectation) preflight() error {
	if _, err := validateEligibility(e.elig); err != nil {
		return err
	}
	if e.inner == nil {
		return newConfigError(fmt.Errorf("nil expectation"))
	}
	if containsEligibility(e.inner) {
		return newConfigError(fmt.Errorf("eligibility already applied"))
	}
	core := unwrapExpectation(e.inner)
	if core == nil {
		return newConfigError(fmt.Errorf("nil expectation"))
	}
	if _, ok := core.(eligibilityEligible); !ok {
		return newConfigError(fmt.Errorf("expectation does not support eligibility"))
	}
	return preflightExpectation(e.inner)
}

func (e *eligibilityExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	elig, err := validateEligibility(e.elig)
	if err != nil {
		res := configErrorResult(e, err)
		return res, err
	}
	innerOpts := opts
	// Private effective Scope: suite scope ∧ eligibility. Report.ScopeID stays
	// on the suite identity only.
	innerOpts.scope = composeEffectiveScope(opts.scope, &elig)
	// Per-wrapper cache for the effective population total. Do not reuse the
	// suite-scoped COUNT(*) cache; sharedScalarPlanFor already skips When.
	innerOpts.scopedTotal = &scopedTotalCache{}
	return e.inner.evaluateSQL(ctx, db, table, innerOpts)
}

// validateEligibility normalizes and validates eligibility at the preflight or
// evaluation boundary. The returned value is independent of the caller's
// Eligibility storage.
func validateEligibility(elig Eligibility) (trustedEligibility, error) {
	id := strings.TrimSpace(elig.identity)
	if id == "" {
		return trustedEligibility{}, newConfigError(errEligibilityIdentityRequired)
	}

	trimmedPredicate := strings.TrimSpace(elig.predicate)
	if trimmedPredicate == "" {
		if len(elig.values) > 0 {
			return trustedEligibility{}, newConfigError(errEligibilityValuesWithoutPredicate)
		}
		return trustedEligibility{}, newConfigError(errEligibilityPredicateRequired)
	}

	slots, err := scanNeutralSlots(elig.predicate)
	if err != nil {
		return trustedEligibility{}, err
	}
	if slots != len(elig.values) {
		return trustedEligibility{}, newConfigError(eligibilityArityError(slots, len(elig.values)))
	}

	return trustedEligibility{
		identity:  id,
		predicate: elig.predicate,
		values:    copyScopeValues(elig.values),
	}, nil
}

// composeEffectiveScope returns the population filter for one rule: suite
// scope and rule eligibility as independent conjuncts. Binding order is scope
// values, then eligibility values, then expectation values (via
// newScopedArgBinder offsets on the combined value count). Eligibility
// identity never becomes Report.ScopeID.
func composeEffectiveScope(scope *trustedScope, elig *trustedEligibility) *trustedScope {
	if elig == nil {
		return scope
	}
	if scope == nil {
		return &trustedScope{
			predicate: elig.predicate,
			values:    copyScopeValues(elig.values),
		}
	}
	return &trustedScope{
		identity:  scope.identity,
		predicate: "(" + scope.predicate + ") AND (" + elig.predicate + ")",
		values:    append(append([]any(nil), scope.values...), elig.values...),
	}
}

// eligibilityEligible marks declarations that may carry a When wrapper. Per-row,
// uniqueness, composite-key, and reference expectations implement it.
type eligibilityEligible interface {
	eligibilityEligible()
}

func (perRowExpectation) eligibilityEligible()          {}
func (uniqueExpectation) eligibilityEligible()          {}
func (compositeUniqueExpectation) eligibilityEligible() {}
func (referenceExpectation) eligibilityEligible()       {}

func containsEligibility(exp Expectation) bool {
	for {
		switch w := exp.(type) {
		case *idExpectation:
			if w == nil {
				return false
			}
			exp = w.inner
		case *maxFailedCountExpectation:
			if w == nil {
				return false
			}
			exp = w.inner
		case *policyExpectation:
			if w == nil {
				return false
			}
			exp = w.inner
		case *eligibilityExpectation:
			return true
		default:
			return false
		}
	}
}
