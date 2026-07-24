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
	w, ok := exp.(*idExpectation)
	if !ok {
		return ""
	}
	return w.id
}

// unwrapExpectation peels WithID wrappers to the underlying expectation.
func unwrapExpectation(exp Expectation) Expectation {
	for {
		w, ok := exp.(*idExpectation)
		if !ok || w == nil {
			return exp
		}
		exp = w.inner
	}
}

// usesRowDenominator reports whether exp needs a row-population total COUNT(*).
func usesRowDenominator(exp Expectation) bool {
	switch unwrapExpectation(exp).(type) {
	case perRowExpectation, uniqueExpectation:
		return true
	default:
		return false
	}
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
