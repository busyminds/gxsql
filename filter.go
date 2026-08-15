package gxsql

import (
	"fmt"
	"strings"
)

// ParentFilter is an immutable trusted parent-side predicate for referential
// checks. Construct it with [TrustedParentFilter]. It is intentionally a
// distinct type from [Scope] so suite scope cannot be reused as a parent filter.
type ParentFilter struct {
	identity  string
	predicate string
	values    []any
}

// SecondaryFilter is an immutable trusted predicate for the secondary table in
// count reconciliation. Construct it with [TrustedSecondaryFilter]. It is
// intentionally a distinct type from [Scope] and [ParentFilter].
type SecondaryFilter struct {
	identity  string
	predicate string
	values    []any
}

// TrustedParentFilter returns an immutable [ParentFilter] from trusted Go-code
// predicate input. Validation is deferred until the filter is attached to a
// referential expectation and preflighted by [Suite.ValidateTable].
func TrustedParentFilter(id, predicate string, args ...any) ParentFilter {
	return ParentFilter{
		identity:  id,
		predicate: predicate,
		values:    copyScopeValues(args),
	}
}

// TrustedSecondaryFilter returns an immutable [SecondaryFilter] from trusted
// Go-code predicate input. Validation is deferred until reconciliation
// preflight during [Suite.ValidateTable].
func TrustedSecondaryFilter(id, predicate string, args ...any) SecondaryFilter {
	return SecondaryFilter{
		identity:  id,
		predicate: predicate,
		values:    copyScopeValues(args),
	}
}

func validateParentFilter(filter ParentFilter) (ParentFilter, error) {
	validated, err := validateTrustedFilter(
		filter.identity,
		filter.predicate,
		filter.values,
		errParentFilterIdentityRequired,
		errParentFilterPredicateRequired,
		errParentFilterValuesWithoutPredicate,
		parentFilterArityError,
	)
	if err != nil {
		return ParentFilter{}, err
	}
	return ParentFilter(validated), nil
}

func validateSecondaryFilter(filter SecondaryFilter) (SecondaryFilter, error) {
	validated, err := validateTrustedFilter(
		filter.identity,
		filter.predicate,
		filter.values,
		errSecondaryFilterIdentityRequired,
		errSecondaryFilterPredicateRequired,
		errSecondaryFilterValuesWithoutPredicate,
		secondaryFilterArityError,
	)
	if err != nil {
		return SecondaryFilter{}, err
	}
	return SecondaryFilter(validated), nil
}

type trustedFilter struct {
	identity  string
	predicate string
	values    []any
}

func validateTrustedFilter(
	identity, predicate string,
	values []any,
	errIdentity, errPredicate, errValuesWithoutPredicate error,
	arity func(slots, n int) error,
) (trustedFilter, error) {
	id := strings.TrimSpace(identity)
	if id == "" {
		return trustedFilter{}, newConfigError(errIdentity)
	}

	trimmedPredicate := strings.TrimSpace(predicate)
	if trimmedPredicate == "" {
		if len(values) > 0 {
			return trustedFilter{}, newConfigError(errValuesWithoutPredicate)
		}
		return trustedFilter{}, newConfigError(errPredicate)
	}

	slots, err := scanNeutralSlots(predicate)
	if err != nil {
		return trustedFilter{}, err
	}
	if slots != len(values) {
		return trustedFilter{}, newConfigError(arity(slots, len(values)))
	}

	return trustedFilter{
		identity:  id,
		predicate: predicate,
		values:    copyScopeValues(values),
	}, nil
}

func (f ParentFilter) renderAt(d Dialect, offset int) (rowPredicate, error) {
	if d == nil {
		return rowPredicate{}, fmt.Errorf("gxsql: dialect is required")
	}
	where, err := renderNeutralPredicateAt(d, f.predicate, offset)
	if err != nil {
		return rowPredicate{}, err
	}
	return withWhere(where, append([]any(nil), f.values...)), nil
}

func (f SecondaryFilter) render(d Dialect) (rowPredicate, error) {
	return f.renderAt(d, 0)
}

func (f SecondaryFilter) renderAt(d Dialect, offset int) (rowPredicate, error) {
	if d == nil {
		return rowPredicate{}, fmt.Errorf("gxsql: dialect is required")
	}
	where, err := renderNeutralPredicateAt(d, f.predicate, offset)
	if err != nil {
		return rowPredicate{}, err
	}
	return withWhere(where, append([]any(nil), f.values...)), nil
}

func parentFilterArityError(slots, values int) error {
	return fmt.Errorf("parent filter predicate has %d placeholders but %d values", slots, values)
}

func secondaryFilterArityError(slots, values int) error {
	return fmt.Errorf("secondary filter predicate has %d placeholders but %d values", slots, values)
}
