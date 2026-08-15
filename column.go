package gxsql

import (
	"context"
	"fmt"
)

// ColumnBuilder is the entry point for column checks that apply to any SQL type.
// Construct one with [Column]. Most methods return per-row [Expectation] values
// evaluated with [RowDenominatorAvailable]; [ColumnBuilder.DistinctCount] returns
// a table-level expectation evaluated with [RowDenominatorUnavailable]. Invalid
// column identifiers fail suite preflight before SQL runs.
type ColumnBuilder struct {
	column string
}

// Column returns a builder for nullability, membership, uniqueness, and
// distinct-count checks on name. name must satisfy [Dialect] identifier rules.
func Column(name string) ColumnBuilder {
	return ColumnBuilder{column: name}
}

// ColumnsBuilder is the entry point for multi-column checks such as composite
// uniqueness and referential integrity. Construct one with [Columns].
// Identifiers are validated at suite preflight before SQL runs.
type ColumnsBuilder struct {
	columns []string
}

// Columns returns a builder for composite uniqueness and referential checks on
// separately supplied identifiers. Each name must satisfy [Dialect] identifier
// rules. [ColumnsBuilder.Unique] requires at least two columns;
// [ColumnsBuilder.References] accepts one or more. Duplicate or empty names are
// rejected at suite preflight.
func Columns(names ...string) ColumnsBuilder {
	return ColumnsBuilder{columns: append([]string(nil), names...)}
}

// NumberColumn is the entry point for ordered numeric comparisons and aggregates
// on one column. Construct one with [Int] or [Float].
type NumberColumn struct {
	column  string
	integer bool
}

// Int returns a builder for integer or general numeric column checks and
// table-level aggregates on name.
func Int(name string) NumberColumn {
	return NumberColumn{column: name, integer: true}
}

// Float returns a builder for floating-point column checks and table-level
// aggregates on name. Aggregate comparisons use float64 thresholds.
func Float(name string) NumberColumn {
	return NumberColumn{column: name}
}

// StringColumn is the entry point for string emptiness, length, and portable
// pattern checks on one column. Construct one with [String]. Length predicates
// use the active [Dialect.StringLength] expression. LIKE-family builders and
// capability-gated [StringColumn.Regex] live alongside these methods.
type StringColumn struct {
	column string
}

// String returns a builder for string column checks on name, including
// emptiness, length, LIKE-family, and capability-gated regex checks.
func String(name string) StringColumn {
	return StringColumn{column: name}
}

// IsNull returns a per-row expectation that the column is SQL NULL. Non-NULL
// values fail. An empty table passes vacuously. Results use [KindIsNull].
func (c ColumnBuilder) IsNull() Expectation {
	return perRowExpectation{column: c.column, name: c.column + " is null", kind: KindIsNull, build: isNullPredicate}
}

// NotNull returns a per-row expectation that the column is not SQL NULL. NULL
// values fail. An empty table passes vacuously. Results use [KindNotNull].
func (c ColumnBuilder) NotNull() Expectation {
	return perRowExpectation{column: c.column, name: c.column + " not null", kind: KindNotNull, build: notNullPredicate}
}

// In asserts the column value is one of vals. SQL NULL in the column fails.
// Each entry in vals must be non-nil; an empty list or a nil entry returns a
// configuration error. Very large lists expand the query (many placeholders);
// keep lists in the low thousands or use a lookup-table join outside gxsql.
func (c ColumnBuilder) In(vals ...any) Expectation {
	return perRowExpectation{
		column: c.column,
		name:   fmt.Sprintf("%s in %v", c.column, vals),
		kind:   KindIn,
		preflightCheck: func() error {
			return newConfigError(validateInListValues(c.column, "in", vals))
		},
		build: func(d Dialect, col string, scope *trustedScope) (rowPredicate, error) {
			return inPredicate(d, col, vals, scope)
		},
	}
}

// NotIn asserts the column value is not one of vals. SQL NULL in the column
// fails. Each entry in vals must be non-nil; an empty list or a nil entry
// returns a configuration error. Very large lists expand the query (many
// placeholders); keep lists in the low thousands or split checks.
func (c ColumnBuilder) NotIn(vals ...any) Expectation {
	return perRowExpectation{
		column: c.column,
		name:   fmt.Sprintf("%s not in %v", c.column, vals),
		kind:   KindNotIn,
		preflightCheck: func() error {
			return newConfigError(validateInListValues(c.column, "not in", vals))
		},
		build: func(d Dialect, col string, scope *trustedScope) (rowPredicate, error) {
			return notInPredicate(d, col, vals, scope)
		},
	}
}

// Unique returns a per-row expectation that each non-NULL value appears at most
// once. NULL values are ignored and do not participate in duplicate detection.
// Every row in a duplicate group fails. An empty table passes vacuously.
// Failing rows may include sample values (capped by [DefaultSampleCap] or
// [WithSampleCap]) and failed keys when [WithKey] is set. Results use
// [KindUnique].
func (c ColumnBuilder) Unique() Expectation {
	return uniqueExpectation(c)
}

// Unique returns a per-row expectation that each complete non-NULL tuple of the
// builder columns appears at most once within the scoped local population.
// Tuples with any NULL component are ignored. Every row in a duplicate
// participating tuple fails. An empty scoped population passes vacuously.
// Results use [KindCompositeUnique], leave [Result.Column] empty, and populate
// [ResultFacts.KeyColumns] in declaration order.
func (c ColumnsBuilder) Unique() Expectation {
	return compositeUniqueExpectation{columns: append([]string(nil), c.columns...)}
}

// References returns a per-row expectation that every complete non-NULL local
// foreign-key tuple resolves to at least one row in parent. A local row with
// any NULL component passes (nullable foreign-key policy). Parent lookup is
// intentionally unscoped; [WithScope] applies only to the local target.
// Results use [KindReference], leave [Result.Column] empty, and populate
// [ResultFacts.Reference]. Samples and failed keys remain local-only.
func (c ColumnBuilder) References(parent TableRef, parentColumns ...string) Expectation {
	return referenceExpectation{
		localColumns:  []string{c.column},
		parent:        parent,
		parentColumns: append([]string(nil), parentColumns...),
	}
}

// References returns a per-row expectation that every complete non-NULL local
// foreign-key tuple resolves to at least one row in parent. Local and parent
// column lists must have equal arity. A local row with any NULL component
// passes. Parent lookup is intentionally unscoped; [WithScope] applies only to
// the local target. Results use [KindReference], leave [Result.Column] empty,
// and populate [ResultFacts.Reference]. Samples and failed keys remain local-only.
func (c ColumnsBuilder) References(parent TableRef, parentColumns ...string) Expectation {
	return referenceExpectation{
		localColumns:  append([]string(nil), c.columns...),
		parent:        parent,
		parentColumns: append([]string(nil), parentColumns...),
	}
}

// Between returns a per-row expectation that lo <= value <= hi (inclusive).
// SQL NULL fails. An empty table passes vacuously. Results use [KindBetween].
func (c NumberColumn) Between(lo, hi any) Expectation {
	return perRowExpectation{
		column: c.column,
		name:   fmt.Sprintf("%s between [%v,%v]", c.column, lo, hi),
		kind:   KindBetween,
		facts:  ResultFacts{ConfiguredBoundLower: lo, ConfiguredBoundUpper: hi},
		build: func(d Dialect, col string, scope *trustedScope) (rowPredicate, error) {
			return orderedBetweenPredicate(d, col, lo, hi, scope)
		},
	}
}

// GreaterThan returns a per-row expectation that value > bound. SQL NULL fails.
// An empty table passes vacuously. Results use [KindGreaterThan].
func (c NumberColumn) GreaterThan(bound any) Expectation {
	return numberComparison(c.column, ">", bound)
}

// LessThan returns a per-row expectation that value < bound. SQL NULL fails.
// An empty table passes vacuously. Results use [KindLessThan].
func (c NumberColumn) LessThan(bound any) Expectation {
	return numberComparison(c.column, "<", bound)
}

// GreaterOrEqual returns a per-row expectation that value >= bound. SQL NULL
// fails. An empty table passes vacuously. Results use [KindGreaterOrEqual].
func (c NumberColumn) GreaterOrEqual(bound any) Expectation {
	return numberComparison(c.column, ">=", bound)
}

// LessOrEqual returns a per-row expectation that value <= bound. SQL NULL
// fails. An empty table passes vacuously. Results use [KindLessOrEqual].
func (c NumberColumn) LessOrEqual(bound any) Expectation {
	return numberComparison(c.column, "<=", bound)
}

// EqualColumn asserts that both columns are non-NULL and equal in each row.
// An empty table passes vacuously. Results use [KindEqualColumn].
func (c ColumnBuilder) EqualColumn(right string) Expectation {
	return crossColumnComparison(c.column, right, "=", KindEqualColumn)
}

// NotEqualColumn asserts that both columns are non-NULL and different in each
// row. An empty table passes vacuously. Results use [KindNotEqualColumn].
func (c ColumnBuilder) NotEqualColumn(right string) Expectation {
	return crossColumnComparison(c.column, right, "<>", KindNotEqualColumn)
}

// LessThanColumn asserts that both columns are non-NULL and left is less than
// right in each row. Results use [KindLessThanColumn].
func (c ColumnBuilder) LessThanColumn(right string) Expectation {
	return crossColumnComparison(c.column, right, "<", KindLessThanColumn)
}

// LessOrEqualColumn asserts that both columns are non-NULL and left is less
// than or equal to right in each row. Results use [KindLessOrEqualColumn].
func (c ColumnBuilder) LessOrEqualColumn(right string) Expectation {
	return crossColumnComparison(c.column, right, "<=", KindLessOrEqualColumn)
}

// GreaterThanColumn asserts that both columns are non-NULL and left is greater
// than right in each row. Results use [KindGreaterThanColumn].
func (c ColumnBuilder) GreaterThanColumn(right string) Expectation {
	return crossColumnComparison(c.column, right, ">", KindGreaterThanColumn)
}

// GreaterOrEqualColumn asserts that both columns are non-NULL and left is
// greater than or equal to right in each row. Results use [KindGreaterOrEqualColumn].
func (c ColumnBuilder) GreaterOrEqualColumn(right string) Expectation {
	return crossColumnComparison(c.column, right, ">=", KindGreaterOrEqualColumn)
}

// RatioEqual asserts actual == planned * bound for non-NULL integer columns.
// A zero denominator fails the row, and the algebraic form avoids integer
// division truncation. Results use [KindRatioEqual].
func (c NumberColumn) RatioEqual(right string, bound int64) Expectation {
	return ratioEqualExpectation(c.column, right, bound, c.integer)
}

// NotEmpty returns a per-row expectation that the string is non-empty after
// trimming is not applied—only SQL NULL and the empty string fail. An empty
// table passes vacuously. Results use [KindNotEmpty].
func (c StringColumn) NotEmpty() Expectation {
	return perRowExpectation{column: c.column, name: c.column + " not empty", kind: KindNotEmpty, build: notEmptyPredicate}
}

// Empty returns a per-row expectation that the string is empty (SQL NULL
// fails). An empty table passes vacuously. Results use [KindEmpty].
func (c StringColumn) Empty() Expectation {
	return perRowExpectation{column: c.column, name: c.column + " empty", kind: KindEmpty, build: emptyPredicate}
}

// LenEqual returns a per-row expectation that the dialect string-length of the
// column equals n. SQL NULL fails. Length uses [Dialect.StringLength]. An
// empty table passes vacuously. Results use [KindLenEqual].
func (c StringColumn) LenEqual(n int) Expectation {
	return perRowExpectation{
		column: c.column,
		name:   fmt.Sprintf("%s length == %d", c.column, n),
		kind:   KindLenEqual,
		facts:  ResultFacts{ConfiguredCount: intFact(n)},
		build: func(d Dialect, col string, scope *trustedScope) (rowPredicate, error) {
			return stringLenComparePredicate(d, col, "=", n, scope)
		},
	}
}

// LenBetween returns a per-row expectation that lo <= dialect string-length <= hi
// (inclusive). SQL NULL fails. Length uses [Dialect.StringLength]. An empty
// table passes vacuously. Results use [KindLenBetween].
func (c StringColumn) LenBetween(lo, hi int) Expectation {
	return perRowExpectation{
		column: c.column,
		name:   fmt.Sprintf("%s length in [%d,%d]", c.column, lo, hi),
		kind:   KindLenBetween,
		facts:  ResultFacts{ConfiguredCountLower: intFact(lo), ConfiguredCountUpper: intFact(hi)},
		build: func(d Dialect, col string, scope *trustedScope) (rowPredicate, error) {
			return stringLenBetweenPredicate(d, col, lo, hi, scope)
		},
	}
}

type predicateBuilder func(d Dialect, column string, scope *trustedScope) (rowPredicate, error)

type crossColumnPredicateBuilder func(
	d Dialect, left, right string, scope *trustedScope,
) (rowPredicate, error)

type perRowExpectation struct {
	column         string
	rightColumn    string
	name           string
	kind           ExpectationKind
	facts          ResultFacts
	build          predicateBuilder
	buildColumns   crossColumnPredicateBuilder
	preflightCheck func() error
}

func (e perRowExpectation) Name() string { return e.name }

func (e perRowExpectation) expectationKind() ExpectationKind { return e.kind }

func (e perRowExpectation) preflight() error {
	if err := validateIdent(e.column); err != nil {
		return newConfigError(err)
	}
	if e.buildColumns != nil || e.rightColumn != "" {
		if err := validateIdent(e.rightColumn); err != nil {
			return newConfigError(err)
		}
		if e.column == e.rightColumn {
			return newConfigError(fmt.Errorf("gxsql: cross-column expectation cannot compare %q with itself", e.column))
		}
	}
	if e.preflightCheck != nil {
		return e.preflightCheck()
	}
	return nil
}

func (e perRowExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	var (
		pred rowPredicate
		err  error
	)
	if e.buildColumns != nil {
		pred, err = e.buildColumns(opts.dialect, e.column, e.rightColumn, opts.scope)
	} else {
		pred, err = e.build(opts.dialect, e.column, opts.scope)
	}
	if err != nil {
		return Result{Kind: e.kind, Name: e.name, Column: e.column, RowDenominator: RowDenominatorUnavailable}, categorizeRenderError(err)
	}
	return evalPerRow(ctx, db, table, opts, e.kind, e.name, e.column, e.facts, pred)
}

func numberComparison(column, op string, bound any) Expectation {
	return perRowExpectation{
		column: column,
		name:   fmt.Sprintf("%s %s %v", column, op, bound),
		kind:   comparisonKind(op),
		facts:  ResultFacts{ConfiguredBound: bound},
		build: func(d Dialect, col string, scope *trustedScope) (rowPredicate, error) {
			return orderedComparePredicate(d, col, op, bound, scope)
		},
	}
}

func crossColumnComparison(left, right, op string, kind ExpectationKind) Expectation {
	return perRowExpectation{
		column:      left,
		rightColumn: right,
		name:        fmt.Sprintf("%s %s %s", left, op, right),
		kind:        kind,
		facts: ResultFacts{
			Comparison: &ComparisonFacts{
				LeftColumn:   left,
				RightColumn:  right,
				Relationship: op,
			},
		},
		buildColumns: func(d Dialect, left, right string, scope *trustedScope) (rowPredicate, error) {
			return crossColumnComparisonPredicate(d, left, right, op, scope)
		},
	}
}

func ratioEqualExpectation(left, right string, bound int64, integer bool) Expectation {
	return perRowExpectation{
		column:      left,
		rightColumn: right,
		name:        fmt.Sprintf("%s == %s * %d", left, right, bound),
		kind:        KindRatioEqual,
		facts: ResultFacts{
			Ratio: &RatioFacts{
				LeftColumn:  left,
				RightColumn: right,
				Bound:       bound,
			},
		},
		buildColumns: func(d Dialect, left, right string, scope *trustedScope) (rowPredicate, error) {
			return ratioEqualPredicate(d, left, right, bound, scope)
		},
		preflightCheck: func() error {
			if !integer {
				return newConfigError(fmt.Errorf("gxsql: RatioEqual requires an Int builder"))
			}
			return nil
		},
	}
}

func comparisonKind(op string) ExpectationKind {
	switch op {
	case ">":
		return KindGreaterThan
	case "<":
		return KindLessThan
	case ">=":
		return KindGreaterOrEqual
	case "<=":
		return KindLessOrEqual
	default:
		return KindCustom
	}
}

func isNullPredicate(d Dialect, column string, scope *trustedScope) (rowPredicate, error) {
	col, err := quoteIdent(d, column)
	if err != nil {
		return rowPredicate{}, err
	}
	return withWhere(col+" IS NOT NULL", nil), nil
}

func notNullPredicate(d Dialect, column string, scope *trustedScope) (rowPredicate, error) {
	col, err := quoteIdent(d, column)
	if err != nil {
		return rowPredicate{}, err
	}
	return withWhere(col+" IS NULL", nil), nil
}

func validateInListValues(column, op string, vals []any) error {
	if len(vals) == 0 {
		return fmt.Errorf("gxsql: %s %s requires at least one value", column, op)
	}
	for i, v := range vals {
		if v == nil {
			return fmt.Errorf("gxsql: %s %s value at index %d is nil", column, op, i)
		}
	}
	return nil
}

func inPredicate(d Dialect, column string, vals []any, scope *trustedScope) (rowPredicate, error) {
	col, err := quoteIdent(d, column)
	if err != nil {
		return rowPredicate{}, err
	}
	if err := validateInListValues(column, "in", vals); err != nil {
		return rowPredicate{}, err
	}
	b := newScopedArgBinder(d, scope)
	placeholders := make([]string, len(vals))
	for i, v := range vals {
		placeholders[i] = b.bind(v)
	}
	where := fmt.Sprintf("%s IS NULL OR %s NOT IN (%s)", col, col, joinQuoted(placeholders))
	return withWhere(where, b.args), nil
}

func notInPredicate(d Dialect, column string, vals []any, scope *trustedScope) (rowPredicate, error) {
	col, err := quoteIdent(d, column)
	if err != nil {
		return rowPredicate{}, err
	}
	if err := validateInListValues(column, "not in", vals); err != nil {
		return rowPredicate{}, err
	}
	b := newScopedArgBinder(d, scope)
	placeholders := make([]string, len(vals))
	for i, v := range vals {
		placeholders[i] = b.bind(v)
	}
	where := fmt.Sprintf("%s IS NULL OR %s IN (%s)", col, col, joinQuoted(placeholders))
	return withWhere(where, b.args), nil
}

func orderedBetweenPredicate(d Dialect, column string, lo, hi any, scope *trustedScope) (rowPredicate, error) {
	col, err := quoteIdent(d, column)
	if err != nil {
		return rowPredicate{}, err
	}
	b := newScopedArgBinder(d, scope)
	pLo := b.bind(lo)
	pHi := b.bind(hi)
	where := fmt.Sprintf("%s IS NULL OR %s < %s OR %s > %s", col, col, pLo, col, pHi)
	return withWhere(where, b.args), nil
}

func orderedComparePredicate(d Dialect, column, op string, bound any, scope *trustedScope) (rowPredicate, error) {
	col, err := quoteIdent(d, column)
	if err != nil {
		return rowPredicate{}, err
	}
	failOp, ok := failComparisonOp(op)
	if !ok {
		return rowPredicate{}, fmt.Errorf("gxsql: unsupported comparison %q", op)
	}
	b := newScopedArgBinder(d, scope)
	p := b.bind(bound)
	where := fmt.Sprintf("%s IS NULL OR %s %s %s", col, col, failOp, p)
	return withWhere(where, b.args), nil
}

func crossColumnComparisonPredicate(
	d Dialect, left, right, op string, scope *trustedScope,
) (rowPredicate, error) {
	leftQuoted, err := quoteIdent(d, left)
	if err != nil {
		return rowPredicate{}, err
	}
	rightQuoted, err := quoteIdent(d, right)
	if err != nil {
		return rowPredicate{}, err
	}
	failOp, ok := failComparisonOp(op)
	if !ok {
		return rowPredicate{}, fmt.Errorf("gxsql: unsupported cross-column comparison %q", op)
	}
	where := fmt.Sprintf(
		"%s IS NULL OR %s IS NULL OR %s %s %s",
		leftQuoted, rightQuoted, leftQuoted, failOp, rightQuoted,
	)
	return withWhere(where, nil), nil
}

func ratioEqualPredicate(
	d Dialect, left, right string, bound int64, scope *trustedScope,
) (rowPredicate, error) {
	leftQuoted, err := quoteIdent(d, left)
	if err != nil {
		return rowPredicate{}, err
	}
	rightQuoted, err := quoteIdent(d, right)
	if err != nil {
		return rowPredicate{}, err
	}
	b := newScopedArgBinder(d, scope)
	boundPlaceholder := b.bind(bound)
	where := fmt.Sprintf(
		"%s IS NULL OR %s IS NULL OR %s = 0 OR %s <> %s * %s",
		leftQuoted, rightQuoted, rightQuoted, leftQuoted, rightQuoted, boundPlaceholder,
	)
	return withWhere(where, b.args), nil
}

func failComparisonOp(op string) (string, bool) {
	switch op {
	case "=":
		return "<>", true
	case "<>":
		return "=", true
	case ">":
		return "<=", true
	case ">=":
		return "<", true
	case "<":
		return ">=", true
	case "<=":
		return ">", true
	default:
		return "", false
	}
}

func notEmptyPredicate(d Dialect, column string, scope *trustedScope) (rowPredicate, error) {
	col, err := quoteIdent(d, column)
	if err != nil {
		return rowPredicate{}, err
	}
	where := fmt.Sprintf("%s IS NULL OR %s = ''", col, col)
	return withWhere(where, nil), nil
}

func emptyPredicate(d Dialect, column string, scope *trustedScope) (rowPredicate, error) {
	col, err := quoteIdent(d, column)
	if err != nil {
		return rowPredicate{}, err
	}
	where := fmt.Sprintf("%s IS NULL OR %s <> ''", col, col)
	return withWhere(where, nil), nil
}

func stringLenComparePredicate(d Dialect, column, op string, n int, scope *trustedScope) (rowPredicate, error) {
	col, err := quoteIdent(d, column)
	if err != nil {
		return rowPredicate{}, err
	}
	failOp, ok := failComparisonOp(op)
	if !ok {
		return rowPredicate{}, fmt.Errorf("gxsql: unsupported length comparison %q", op)
	}
	lenExpr := d.StringLength(col)
	where := fmt.Sprintf("%s IS NULL OR %s %s %d", col, lenExpr, failOp, n)
	return withWhere(where, nil), nil
}

func stringLenBetweenPredicate(d Dialect, column string, lo, hi int, scope *trustedScope) (rowPredicate, error) {
	col, err := quoteIdent(d, column)
	if err != nil {
		return rowPredicate{}, err
	}
	lenExpr := d.StringLength(col)
	where := fmt.Sprintf("%s IS NULL OR %s < %d OR %s > %d", col, lenExpr, lo, lenExpr, hi)
	return withWhere(where, nil), nil
}
