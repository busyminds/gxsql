package gxsql

import (
	"fmt"
	"strings"
)

type metaExpectation interface {
	Expectation
	expectationKind() ExpectationKind
	preflight() error
}

func expectationKind(exp Expectation) ExpectationKind {
	if exp == nil {
		return KindCustom
	}
	if m, ok := exp.(metaExpectation); ok {
		return m.expectationKind()
	}
	return KindCustom
}

func preflightExpectation(exp Expectation) error {
	if exp == nil {
		return newConfigError(fmt.Errorf("nil expectation"))
	}
	if m, ok := exp.(metaExpectation); ok {
		return m.preflight()
	}
	return nil
}

func expectationID(exp Expectation) string {
	for {
		switch w := exp.(type) {
		case *idExpectation:
			if w == nil {
				return ""
			}
			return w.id
		case *maxFailedCountExpectation:
			if w == nil {
				return ""
			}
			exp = w.inner
		default:
			return ""
		}
	}
}

// unwrapExpectation peels WithID and tolerance wrappers to the underlying
// expectation. Denominator detection still depends on the core declaration,
// not merely on the presence of a wrapper.
func unwrapExpectation(exp Expectation) Expectation {
	for {
		switch w := exp.(type) {
		case *idExpectation:
			if w == nil {
				return exp
			}
			exp = w.inner
		case *maxFailedCountExpectation:
			if w == nil {
				return exp
			}
			exp = w.inner
		default:
			return exp
		}
	}
}

// usesRowDenominator reports whether exp needs a row-population total COUNT(*).
func usesRowDenominator(exp Expectation) bool {
	switch unwrapExpectation(exp).(type) {
	case perRowExpectation, uniqueExpectation, compositeUniqueExpectation, referenceExpectation:
		return true
	default:
		return false
	}
}

// rejectsScope marks expectations that cannot run under WithScope. Structural
// column contracts implement this so suite preflight can peel WithID and
// WithMaxFailedCount wrappers before rejecting the combination.
type rejectsScope interface {
	rejectsScope()
}

// isStructuralExpectation reports whether exp is a structural column contract
// (RequiredColumns / ExactColumns), including when wrapped by WithID or
// WithMaxFailedCount.
func isStructuralExpectation(exp Expectation) bool {
	_, ok := unwrapExpectation(exp).(rejectsScope)
	return ok
}

func structuralScopeIncompatibleError() error {
	return newConfigError(fmt.Errorf("WithScope is incompatible with structural column expectations"))
}

func configErrorResult(exp Expectation, err error) Result {
	kind := expectationKind(exp)
	name := "<configuration error>"
	if exp != nil {
		if w, ok := exp.(*idExpectation); ok && w.inner == nil {
			name = "<configuration error>"
		} else {
			name = exp.Name()
		}
	}
	return Result{
		ID:             expectationID(exp),
		Kind:           kind,
		Name:           name,
		Success:        false,
		RowDenominator: RowDenominatorUnavailable,
		Err:            err,
	}
}

type preflightState struct {
	seenIDs map[string]int
	issues  []PreflightIssue
}

func newPreflightState() *preflightState {
	return &preflightState{seenIDs: make(map[string]int)}
}

func (s *preflightState) check(index int, exp Expectation) {
	if err := preflightExpectation(exp); err != nil {
		s.issues = append(s.issues, PreflightIssue{
			Index: index,
			ID:    expectationID(exp),
			Err:   err,
		})
	}
	id := strings.TrimSpace(expectationID(exp))
	if id == "" {
		return
	}
	if prev, ok := s.seenIDs[id]; ok {
		s.issues = append(s.issues, PreflightIssue{
			Index: prev,
			ID:    id,
			Err: newConfigError(fmt.Errorf(
				"duplicate expectation id %q (also at index %d)", id, index,
			)),
		})
		s.issues = append(s.issues, PreflightIssue{
			Index: index,
			ID:    id,
			Err: newConfigError(fmt.Errorf(
				"duplicate expectation id %q (also at index %d)", id, prev,
			)),
		})
		return
	}
	s.seenIDs[id] = index
}

func (s *preflightState) hasIssueAt(index int) bool {
	for _, iss := range s.issues {
		if iss.Index == index {
			return true
		}
	}
	return false
}

func (s *preflightState) errAt(index int) error {
	for _, iss := range s.issues {
		if iss.Index == index {
			return iss.Err
		}
	}
	return nil
}
