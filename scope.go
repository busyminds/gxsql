package gxsql

import (
	"fmt"
	"strings"
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
	walker := newSQLTextWalker(fragment, render)
	count := 0
	err := walker.walk(sqlTextHandlers{
		onQuestionMark: func(pos int) error {
			walker.flush(pos)
			count++
			if render {
				walker.writeString(d.Placeholder(offset + count))
			}
			walker.start = pos + 1
			return nil
		},
	})
	if err != nil {
		return "", 0, err
	}
	return walker.result(), count, nil
}
