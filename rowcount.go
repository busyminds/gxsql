package gxsql

import (
	"context"
	"fmt"
)

// RowCountBuilder builds table-level COUNT(*) expectations. Start with
// [RowCount]. An empty table yields count 0 (not a vacuous pass). Results set
// [RowDenominatorUnavailable], append the observed count to Name, and store it
// in [ResultFacts.ObservedCount].
type RowCountBuilder struct{}

// RowCount returns a builder for table row-count checks.
func RowCount() RowCountBuilder { return RowCountBuilder{} }

// Equal returns a table-level expectation of exactly want rows. Results use
// [KindRowCountEqual].
func (RowCountBuilder) Equal(want int) Expectation {
	return rowCountExpectation{
		label: fmt.Sprintf("row count == %d", want),
		kind:  KindRowCountEqual,
		check: func(n int) bool { return n == want },
		facts: ResultFacts{ConfiguredCount: intFact(want)},
	}
}

// Between returns a table-level expectation that lo <= row count <= hi
// (inclusive). Results use [KindRowCountBetween].
func (RowCountBuilder) Between(lo, hi int) Expectation {
	return rowCountExpectation{
		label: fmt.Sprintf("row count in [%d,%d]", lo, hi),
		kind:  KindRowCountBetween,
		check: func(n int) bool { return n >= lo && n <= hi },
		facts: ResultFacts{ConfiguredCountLower: intFact(lo), ConfiguredCountUpper: intFact(hi)},
	}
}

// GreaterThan returns a table-level expectation that row count > bound.
// Results use [KindRowCountGreaterThan].
func (RowCountBuilder) GreaterThan(bound int) Expectation {
	return rowCountExpectation{
		label: fmt.Sprintf("row count > %d", bound),
		kind:  KindRowCountGreaterThan,
		check: func(n int) bool { return n > bound },
		facts: ResultFacts{ConfiguredCount: intFact(bound)},
	}
}

// GreaterOrEqual returns a table-level expectation that row count >= bound.
// Results use [KindRowCountGreaterEqual].
func (RowCountBuilder) GreaterOrEqual(bound int) Expectation {
	return rowCountExpectation{
		label: fmt.Sprintf("row count >= %d", bound),
		kind:  KindRowCountGreaterEqual,
		check: func(n int) bool { return n >= bound },
		facts: ResultFacts{ConfiguredCount: intFact(bound)},
	}
}

// LessThan returns a table-level expectation that row count < bound. Results use
// [KindRowCountLessThan].
func (RowCountBuilder) LessThan(bound int) Expectation {
	return rowCountExpectation{
		label: fmt.Sprintf("row count < %d", bound),
		kind:  KindRowCountLessThan,
		check: func(n int) bool { return n < bound },
		facts: ResultFacts{ConfiguredCount: intFact(bound)},
	}
}

// LessOrEqual returns a table-level expectation that row count <= bound.
// Results use [KindRowCountLessEqual].
func (RowCountBuilder) LessOrEqual(bound int) Expectation {
	return rowCountExpectation{
		label: fmt.Sprintf("row count <= %d", bound),
		kind:  KindRowCountLessEqual,
		check: func(n int) bool { return n <= bound },
		facts: ResultFacts{ConfiguredCount: intFact(bound)},
	}
}

type rowCountExpectation struct {
	label string
	kind  ExpectationKind
	check func(int) bool
	facts ResultFacts
}

func (e rowCountExpectation) Name() string { return e.label }

func (e rowCountExpectation) expectationKind() ExpectationKind { return e.kind }

func (e rowCountExpectation) preflight() error { return nil }

func (e rowCountExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	return evalTableCount(ctx, db, table, opts, e.kind, e.label, e.check, e.facts)
}
