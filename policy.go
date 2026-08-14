package gxsql

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Severity classifies how a policy result participates in report gating.
type Severity uint8

const (
	// SeverityError makes a policy failure gate the report. It is the zero value.
	SeverityError Severity = iota
	// SeverityWarning keeps a policy failure queryable without gating the report.
	SeverityWarning
	// SeverityInfo keeps a policy failure queryable without gating the report.
	SeverityInfo
)

func (s Severity) valid() bool {
	return s <= SeverityInfo
}

// Tolerance is the allowance configured on a Policy. Use MaxFailedPercent to
// construct a rate tolerance. A zero Tolerance means no rate allowance.
type Tolerance struct {
	maxFailedPercent *float64
}

// MaxFailedPercent returns an inclusive maximum failed-row percentage
// tolerance. The value must be in the range [0, 100] when the policy is
// validated by ValidateTable.
func MaxFailedPercent(percent float64) Tolerance {
	return Tolerance{maxFailedPercent: &percent}
}

// Policy decorates an expectation with severity, display metadata, and at most
// one rate-based tolerance. WithMaxFailedCount remains the dedicated count
// tolerance wrapper.
type Policy struct {
	Severity    Severity
	Description string
	Tags        []string
	Tolerance   Tolerance
}

// WithPolicy decorates exp with an immutable copy of policy. Invalid policy
// values are reported during suite preflight, before SQL runs.
func WithPolicy(exp Expectation, policy Policy) Expectation {
	policy.Tags = append([]string(nil), policy.Tags...)
	return &policyExpectation{inner: exp, policy: policy}
}

type policyExpectation struct {
	inner  Expectation
	policy Policy
}

func (e *policyExpectation) Name() string {
	if e.inner == nil {
		return "<configuration error>"
	}
	return e.inner.Name()
}

func (e *policyExpectation) expectationKind() ExpectationKind {
	return expectationKind(e.inner)
}

func (e *policyExpectation) preflight() error {
	if e.inner == nil {
		return newConfigError(fmt.Errorf("nil expectation"))
	}
	policy, err := normalizePolicy(e.policy)
	if err != nil {
		return err
	}
	if policy.Tolerance.maxFailedPercent != nil {
		if containsMaxFailedCount(e.inner) {
			return newConfigError(fmt.Errorf("multiple tolerance forms are not supported"))
		}
		core := unwrapExpectation(e.inner)
		if _, ok := core.(toleranceEligible); !ok {
			return newConfigError(fmt.Errorf("maximum failed percent requires a row denominator"))
		}
	}
	if containsPolicy(e.inner) {
		return newConfigError(fmt.Errorf("policy already applied"))
	}
	return preflightExpectation(e.inner)
}

func (e *policyExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	res, err := e.inner.evaluateSQL(ctx, db, table, opts)
	if err != nil && res.Err == nil {
		res.Err = err
	}
	policy, policyErr := normalizePolicy(e.policy)
	if policyErr != nil {
		res.Err = policyErr
		res.Success = false
		return res, policyErr
	}
	return applyPolicy(res, policy), err
}

func normalizePolicy(policy Policy) (Policy, error) {
	if !policy.Severity.valid() {
		return Policy{}, newConfigError(fmt.Errorf("invalid policy severity %d", policy.Severity))
	}
	policy.Description = strings.TrimSpace(policy.Description)
	if policy.Tolerance.maxFailedPercent != nil {
		percent := *policy.Tolerance.maxFailedPercent
		if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 || percent > 100 {
			return Policy{}, newConfigError(fmt.Errorf("maximum failed percent must be between 0 and 100"))
		}
		policy.Tolerance.maxFailedPercent = &percent
	}
	if len(policy.Tags) == 0 {
		policy.Tags = nil
		return policy, nil
	}
	tags := make([]string, len(policy.Tags))
	for i, tag := range policy.Tags {
		tags[i] = strings.TrimSpace(tag)
		if tags[i] == "" {
			return Policy{}, newConfigError(fmt.Errorf("policy tags must not be blank"))
		}
	}
	sort.Strings(tags)
	for i := 1; i < len(tags); i++ {
		if tags[i] == tags[i-1] {
			return Policy{}, newConfigError(fmt.Errorf("duplicate policy tag %q", tags[i]))
		}
	}
	policy.Tags = tags
	return policy, nil
}

func applyPolicy(res Result, policy Policy) Result {
	res.Severity = policy.Severity
	res.Description = policy.Description
	res.Tags = append([]string(nil), policy.Tags...)
	if policy.Tolerance.maxFailedPercent != nil {
		res = applyMaxFailedPercent(res, *policy.Tolerance.maxFailedPercent)
	}
	return res
}

func applyMaxFailedPercent(res Result, max float64) Result {
	res.Facts.ConfiguredMaxFailedPercent = floatFact(max)
	res.Tolerated = false
	if res.Err != nil {
		res.Success = false
		return res
	}
	if res.RowDenominator != RowDenominatorAvailable {
		return res
	}
	if res.Total <= 0 || res.FailedCount == 0 {
		res.Success = true
		return res
	}
	if float64(res.FailedCount)*100 <= max*float64(res.Total) {
		res.Success = true
		res.Tolerated = true
		return res
	}
	res.Success = false
	return res
}

func containsPolicy(exp Expectation) bool {
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
			return true
		case *eligibilityExpectation:
			if w == nil {
				return false
			}
			exp = w.inner
		default:
			return false
		}
	}
}

func containsMaxFailedCount(exp Expectation) bool {
	for {
		switch w := exp.(type) {
		case *idExpectation:
			if w == nil {
				return false
			}
			exp = w.inner
		case *policyExpectation:
			if w == nil {
				return false
			}
			exp = w.inner
		case *maxFailedCountExpectation:
			return true
		case *eligibilityExpectation:
			if w == nil {
				return false
			}
			exp = w.inner
		default:
			return false
		}
	}
}

func containsRateTolerance(exp Expectation) bool {
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
			if w.policy.Tolerance.maxFailedPercent != nil {
				return true
			}
			exp = w.inner
		case *eligibilityExpectation:
			if w == nil {
				return false
			}
			exp = w.inner
		default:
			return false
		}
	}
}

func policyForExpectation(exp Expectation) (Policy, bool) {
	for {
		switch w := exp.(type) {
		case *idExpectation:
			if w == nil {
				return Policy{}, false
			}
			exp = w.inner
		case *maxFailedCountExpectation:
			if w == nil {
				return Policy{}, false
			}
			exp = w.inner
		case *policyExpectation:
			if w == nil {
				return Policy{}, false
			}
			return w.policy, true
		case *eligibilityExpectation:
			if w == nil {
				return Policy{}, false
			}
			exp = w.inner
		default:
			return Policy{}, false
		}
	}
}

func applyPolicyMetadata(res Result, exp Expectation) Result {
	if max, ok := maxFailedCountForExpectation(exp); ok {
		res.Facts.ConfiguredMaxFailedCount = intFact(max)
	}
	policy, ok := policyForExpectation(exp)
	if !ok {
		return res
	}
	normalized, normErr := normalizePolicy(policy)
	if normErr == nil {
		policy = normalized
	} else {
		policy.Description = strings.TrimSpace(policy.Description)
		policy.Tags = append([]string(nil), policy.Tags...)
		for i := range policy.Tags {
			policy.Tags[i] = strings.TrimSpace(policy.Tags[i])
		}
		sort.Strings(policy.Tags)
	}
	res.Severity = policy.Severity
	res.Description = policy.Description
	res.Tags = append([]string(nil), policy.Tags...)
	if normErr == nil && policy.Tolerance.maxFailedPercent != nil {
		res.Facts.ConfiguredMaxFailedPercent = floatFact(*policy.Tolerance.maxFailedPercent)
	}
	return res
}

func maxFailedCountForExpectation(exp Expectation) (int, bool) {
	for {
		switch w := exp.(type) {
		case *idExpectation:
			if w == nil {
				return 0, false
			}
			exp = w.inner
		case *maxFailedCountExpectation:
			if w == nil {
				return 0, false
			}
			return w.max, true
		case *policyExpectation:
			if w == nil {
				return 0, false
			}
			exp = w.inner
		case *eligibilityExpectation:
			if w == nil {
				return 0, false
			}
			exp = w.inner
		default:
			return 0, false
		}
	}
}
