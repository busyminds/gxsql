package gxsql

import (
	"context"
	"fmt"
)

// DistinctCountBuilder builds table-level COUNT(DISTINCT column) expectations.
// Start from [ColumnBuilder.DistinctCount]. SQL NULL values are excluded from the
// count; an empty table or all-NULL column yields 0. Results set
// [RowDenominatorUnavailable], append the observed count to Name, and store it
// in [ResultFacts.ObservedCount]. Invalid column identifiers fail suite
// preflight before SQL runs.
type DistinctCountBuilder struct {
	column string
}

// DistinctCount starts a distinct-count expectation builder for the column.
func (c ColumnBuilder) DistinctCount() DistinctCountBuilder {
	return DistinctCountBuilder(c)
}

// Equal asserts the column has exactly want distinct non-null values.
func (b DistinctCountBuilder) Equal(want int) Expectation {
	return distinctCountExpectation{
		column: b.column,
		label:  fmt.Sprintf("%s distinct count == %d", b.column, want),
		kind:   KindDistinctCountEqual,
		check:  func(n int) bool { return n == want },
		facts:  ResultFacts{ConfiguredCount: intFact(want)},
	}
}

// Between asserts lo <= distinct count <= hi (inclusive).
func (b DistinctCountBuilder) Between(lo, hi int) Expectation {
	return distinctCountExpectation{
		column: b.column,
		label:  fmt.Sprintf("%s distinct count in [%d,%d]", b.column, lo, hi),
		kind:   KindDistinctCountBetween,
		check:  func(n int) bool { return n >= lo && n <= hi },
		facts:  ResultFacts{ConfiguredCountLower: intFact(lo), ConfiguredCountUpper: intFact(hi)},
	}
}

// GreaterThan asserts distinct count > bound.
func (b DistinctCountBuilder) GreaterThan(bound int) Expectation {
	return distinctCountExpectation{
		column: b.column,
		label:  fmt.Sprintf("%s distinct count > %d", b.column, bound),
		kind:   KindDistinctCountGreaterThan,
		check:  func(n int) bool { return n > bound },
		facts:  ResultFacts{ConfiguredCount: intFact(bound)},
	}
}

// GreaterOrEqual asserts distinct count >= bound.
func (b DistinctCountBuilder) GreaterOrEqual(bound int) Expectation {
	return distinctCountExpectation{
		column: b.column,
		label:  fmt.Sprintf("%s distinct count >= %d", b.column, bound),
		kind:   KindDistinctCountGreaterEqual,
		check:  func(n int) bool { return n >= bound },
		facts:  ResultFacts{ConfiguredCount: intFact(bound)},
	}
}

// LessThan asserts distinct count < bound.
func (b DistinctCountBuilder) LessThan(bound int) Expectation {
	return distinctCountExpectation{
		column: b.column,
		label:  fmt.Sprintf("%s distinct count < %d", b.column, bound),
		kind:   KindDistinctCountLessThan,
		check:  func(n int) bool { return n < bound },
		facts:  ResultFacts{ConfiguredCount: intFact(bound)},
	}
}

// LessOrEqual asserts distinct count <= bound.
func (b DistinctCountBuilder) LessOrEqual(bound int) Expectation {
	return distinctCountExpectation{
		column: b.column,
		label:  fmt.Sprintf("%s distinct count <= %d", b.column, bound),
		kind:   KindDistinctCountLessEqual,
		check:  func(n int) bool { return n <= bound },
		facts:  ResultFacts{ConfiguredCount: intFact(bound)},
	}
}

type distinctCountExpectation struct {
	column string
	label  string
	kind   ExpectationKind
	check  func(int) bool
	facts  ResultFacts
}

func (e distinctCountExpectation) Name() string { return e.label }

func (e distinctCountExpectation) expectationKind() ExpectationKind { return e.kind }

func (e distinctCountExpectation) preflight() error {
	if err := validateIdent(e.column); err != nil {
		return newConfigError(err)
	}
	return nil
}

func (e distinctCountExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	return evalDistinctCount(ctx, db, table, opts, e.kind, e.column, e.label, e.check, e.facts)
}
