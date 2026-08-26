package gxsql

import (
	"errors"
	"fmt"
	"strings"

	"github.com/busyminds/gxsql/internal/sqltext"
)

// Scope is an immutable scope definition containing a caller identity, a
// dialect-neutral predicate authored with ? placeholders, and bound values.
// Its fields are intentionally unexported so scope data cannot be modified
// after construction.
type Scope struct {
	identity  string
	predicate string
	values    []any
}

// MaxSegments is the maximum number of named populations in one validation run.
const MaxSegments = 32

// Segment is an immutable named population definition containing a trusted
// dialect-neutral predicate and its bound values.
type Segment struct {
	identity  string
	predicate string
	values    []any
}

// TrustedSegment constructs a Segment from trusted Go-code predicate input.
// Validation is deferred until the segment is attached to ValidateTable.
func TrustedSegment(id, predicate string, args ...any) Segment {
	return Segment{
		identity:  id,
		predicate: predicate,
		values:    copyScopeValues(args),
	}
}

// validateSegments normalizes and validates every segment before evaluation.
// The returned values are independent of the caller's Segment storage.
func validateSegments(segments []Segment) ([]Segment, error) {
	if len(segments) == 0 {
		return nil, newConfigError(errors.New("at least one segment is required"))
	}
	if len(segments) > MaxSegments {
		return nil, newConfigError(fmt.Errorf("at most %d segments are allowed", MaxSegments))
	}

	validated := make([]Segment, len(segments))
	seen := make(map[string]int, len(segments))
	var validationErrs []error
	for i, segment := range segments {
		id := strings.TrimSpace(segment.identity)
		if id == "" {
			validationErrs = append(validationErrs,
				newConfigError(errors.New("segment identity is required")))
		} else if previous, ok := seen[id]; ok {
			validationErrs = append(validationErrs, newConfigError(fmt.Errorf(
				"duplicate segment id %q (also at index %d)", id, previous,
			)))
		} else {
			seen[id] = i
		}

		trimmedPredicate := strings.TrimSpace(segment.predicate)
		if trimmedPredicate == "" {
			if len(segment.values) > 0 {
				validationErrs = append(validationErrs,
					newConfigError(errors.New("segment values require a predicate")))
			} else {
				validationErrs = append(validationErrs,
					newConfigError(errors.New("segment predicate is required")))
			}
		} else {
			slots, err := scanNeutralSlots(segment.predicate)
			if err != nil {
				validationErrs = append(validationErrs, err)
			} else if slots != len(segment.values) {
				validationErrs = append(validationErrs, newConfigError(fmt.Errorf(
					"segment predicate has %d placeholders but %d values",
					slots, len(segment.values),
				)))
			}
		}

		validated[i] = Segment{
			identity:  id,
			predicate: segment.predicate,
			values:    copyScopeValues(segment.values),
		}
	}
	if len(validationErrs) > 0 {
		return nil, errors.Join(validationErrs...)
	}
	return validated, nil
}

// composeSegmentScope adds one segment population after the run-wide scope.
// The run-wide identity remains the only scope identity published by reports.
func composeSegmentScope(scope *trustedScope, segment Segment) trustedScope {
	if scope == nil {
		return trustedScope{
			predicate: segment.predicate,
			values:    copyScopeValues(segment.values),
		}
	}
	return trustedScope{
		identity:  scope.identity,
		predicate: "(" + scope.predicate + ") AND (" + segment.predicate + ")",
		values:    append(append([]any(nil), scope.values...), segment.values...),
	}
}

// trustedScope preserves the internal Spec 05 scope name while exposing the
// validated Scope representation to existing package code.
type trustedScope = Scope

// TrustedScope constructs a Scope from trusted Go-code predicate input.
// Validation is deferred until the scope is attached to ValidateTable.
func TrustedScope(id, predicate string, args ...any) Scope {
	return Scope{
		identity:  id,
		predicate: predicate,
		values:    copyScopeValues(args),
	}
}

// validateScope normalizes and validates a scope at the ValidateTable
// boundary. The returned value is independent of the caller's Scope storage.
func validateScope(scope Scope) (trustedScope, error) {
	id := strings.TrimSpace(scope.identity)
	if id == "" {
		return trustedScope{}, newConfigError(errScopeIdentityRequired)
	}

	trimmedPredicate := strings.TrimSpace(scope.predicate)
	if trimmedPredicate == "" {
		if len(scope.values) > 0 {
			return trustedScope{}, newConfigError(errScopeValuesWithoutPredicate)
		}
		return trustedScope{}, newConfigError(errScopePredicateRequired)
	}

	slots, err := scanNeutralSlots(scope.predicate)
	if err != nil {
		return trustedScope{}, err
	}
	if slots != len(scope.values) {
		return trustedScope{}, newConfigError(scopeArityError(slots, len(scope.values)))
	}

	return trustedScope{
		identity:  id,
		predicate: scope.predicate,
		values:    copyScopeValues(scope.values),
	}, nil
}

func newTrustedScope(identity, predicate string, values []any) (trustedScope, error) {
	return validateScope(Scope{identity: identity, predicate: predicate, values: values})
}

func copyScopeValues(values []any) []any {
	storedValues := append([]any(nil), values...)
	for i, value := range storedValues {
		if bytes, ok := value.([]byte); ok {
			storedValues[i] = append([]byte(nil), bytes...)
		}
	}
	return storedValues
}

func (s trustedScope) render(d Dialect) (rowPredicate, error) {
	return s.renderAt(d, 0)
}

func (s trustedScope) renderAt(d Dialect, offset int) (rowPredicate, error) {
	if d == nil {
		return rowPredicate{}, fmt.Errorf("gxsql: dialect is required")
	}
	where, err := renderNeutralPredicateAt(d, s.predicate, offset)
	if err != nil {
		return rowPredicate{}, err
	}
	return withWhere(where, append([]any(nil), s.values...)), nil
}

// composeRowPredicateWithScope parenthesizes scope and expectation predicates
// independently and binds scope values before expectation values. The
// expectation predicate must already reserve the scope prefix through
// newScopedArgBinder; composition never rewrites arbitrary SQL. A nil scope
// returns pred unchanged.
func composeRowPredicateWithScope(scope *trustedScope, pred rowPredicate, d Dialect) (rowPredicate, error) {
	if scope == nil {
		return pred, nil
	}

	scopePred, err := scope.render(d)
	if err != nil {
		return rowPredicate{}, err
	}

	args := append(append([]any(nil), scopePred.args...), pred.args...)
	if pred.where == "" {
		return withWhere("("+scopePred.where+")", args), nil
	}
	combined := "(" + scopePred.where + ") AND (" + pred.where + ")"
	return withWhere(combined, args), nil
}

func scanNeutralSlots(fragment string) (int, error) {
	_, count, err := walkNeutralPredicate(fragment, nil)
	return count, err
}

func renderNeutralPredicate(d Dialect, fragment string) (string, error) {
	return renderNeutralPredicateAt(d, fragment, 0)
}

func renderNeutralPredicateAt(d Dialect, fragment string, offset int) (string, error) {
	rendered, _, err := walkNeutralPredicateAt(fragment, d, offset)
	return rendered, err
}

// walkNeutralPredicate is the shared lexical walk for neutral ? slots. When d is
// nil, scan-only mode returns the slot count without rendering. When d is
// non-nil, validated slots are replaced with d.Placeholder(n) and all other
// source bytes are preserved verbatim.
func walkNeutralPredicate(fragment string, d Dialect) (string, int, error) {
	return walkNeutralPredicateAt(fragment, d, 0)
}

func walkNeutralPredicateAt(fragment string, d Dialect, offset int) (string, int, error) {
	render := d != nil
	count := 0
	rendered, err := sqltext.Walk(fragment, render, sqltext.Handlers{
		RejectLiteral: func(msg string) error {
			return unsupportedScopePredicateError(msg)
		},
		OnQuestionMark: func(pos int) (string, error) {
			count++
			if !render {
				return "", nil
			}
			return d.Placeholder(offset + count), nil
		},
	})
	if err != nil {
		return "", 0, err
	}
	return rendered, count, nil
}
