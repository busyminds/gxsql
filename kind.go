package gxsql

// ExpectationKind is a stable machine identifier for a built-in expectation type.
// Custom expectations use KindCustom unless a decorator assigns another kind.
type ExpectationKind string

const (
	// KindRowCountEqual marks an expectation from RowCount().Equal.
	KindRowCountEqual ExpectationKind = "row_count_equal"
	// KindRowCountBetween marks an expectation from RowCount().Between.
	KindRowCountBetween ExpectationKind = "row_count_between"
	// KindRowCountGreaterThan marks an expectation from RowCount().GreaterThan.
	KindRowCountGreaterThan ExpectationKind = "row_count_greater_than"
	// KindRowCountGreaterEqual marks an expectation from RowCount().GreaterOrEqual.
	KindRowCountGreaterEqual ExpectationKind = "row_count_greater_or_equal"
	// KindRowCountLessThan marks an expectation from RowCount().LessThan.
	KindRowCountLessThan ExpectationKind = "row_count_less_than"
	// KindRowCountLessEqual marks an expectation from RowCount().LessOrEqual.
	KindRowCountLessEqual ExpectationKind = "row_count_less_or_equal"

	// KindIsNull marks an expectation from Column().IsNull.
	KindIsNull ExpectationKind = "is_null"
	// KindNotNull marks an expectation from Column().NotNull.
	KindNotNull ExpectationKind = "not_null"
	// KindIn marks an expectation from Column().In.
	KindIn ExpectationKind = "in"
	// KindNotIn marks an expectation from Column().NotIn.
	KindNotIn ExpectationKind = "not_in"
	// KindUnique marks an expectation from Column().Unique.
	KindUnique ExpectationKind = "unique"
	// KindBetween marks an expectation from NumberColumn.Between.
	KindBetween ExpectationKind = "between"
	// KindGreaterThan marks an expectation from NumberColumn.GreaterThan.
	KindGreaterThan ExpectationKind = "greater_than"
	// KindLessThan marks an expectation from NumberColumn.LessThan.
	KindLessThan ExpectationKind = "less_than"
	// KindGreaterOrEqual marks an expectation from NumberColumn.GreaterOrEqual.
	KindGreaterOrEqual ExpectationKind = "greater_or_equal"
	// KindLessOrEqual marks an expectation from NumberColumn.LessOrEqual.
	KindLessOrEqual ExpectationKind = "less_or_equal"
	// KindNotEmpty marks an expectation from StringColumn.NotEmpty.
	KindNotEmpty ExpectationKind = "not_empty"
	// KindEmpty marks an expectation from StringColumn.Empty.
	KindEmpty ExpectationKind = "empty"
	// KindLenEqual marks an expectation from StringColumn.LenEqual.
	KindLenEqual ExpectationKind = "len_equal"
	// KindLenBetween marks an expectation from StringColumn.LenBetween.
	KindLenBetween ExpectationKind = "len_between"

	// KindDistinctCountEqual marks an expectation from DistinctCountBuilder.Equal.
	KindDistinctCountEqual ExpectationKind = "distinct_count_equal"
	// KindDistinctCountBetween marks an expectation from DistinctCountBuilder.Between.
	KindDistinctCountBetween ExpectationKind = "distinct_count_between"
	// KindDistinctCountGreaterThan marks an expectation from DistinctCountBuilder.GreaterThan.
	KindDistinctCountGreaterThan ExpectationKind = "distinct_count_greater_than"
	// KindDistinctCountGreaterEqual marks an expectation from DistinctCountBuilder.GreaterOrEqual.
	KindDistinctCountGreaterEqual ExpectationKind = "distinct_count_greater_or_equal"
	// KindDistinctCountLessThan marks an expectation from DistinctCountBuilder.LessThan.
	KindDistinctCountLessThan ExpectationKind = "distinct_count_less_than"
	// KindDistinctCountLessEqual marks an expectation from DistinctCountBuilder.LessOrEqual.
	KindDistinctCountLessEqual ExpectationKind = "distinct_count_less_or_equal"

	// KindAverageBetween marks an expectation from NumberColumn.AverageBetween.
	KindAverageBetween ExpectationKind = "average_between"
	// KindMinGreaterOrEqual marks an expectation from NumberColumn.MinGreaterOrEqual.
	KindMinGreaterOrEqual ExpectationKind = "min_greater_or_equal"
	// KindMaxLessOrEqual marks an expectation from NumberColumn.MaxLessOrEqual.
	KindMaxLessOrEqual ExpectationKind = "max_less_or_equal"

	// KindCustom is the explicit kind for expectations without built-in metadata.
	KindCustom ExpectationKind = "custom"
)
