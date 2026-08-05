package gxsql

import (
	"context"
	"fmt"
	"strings"
)

// RequiredColumns returns a table-level expectation that every named column
// exists on the target. Additional discovered columns are allowed. Each name is
// a separately validated identifier compared byte-for-byte against
// dialect/driver-reported [database/sql.Rows.Columns] names with no case
// folding. Duplicate or invalid identifiers fail suite preflight before SQL
// runs. Discovery uses a read-only zero-row probe and never scans row values.
// Results use [KindRequiredColumns], [RowDenominatorUnavailable], and publish
// [ResultFacts.RequiredColumns] plus ordered [ResultFacts.MissingColumns].
// [WithScope] is incompatible and must be rejected at ValidateTable preflight.
func RequiredColumns(names ...string) Expectation {
	return structuralColumnsExpectation{
		exact:   false,
		columns: append([]string(nil), names...),
	}
}

// ExactColumns returns a table-level expectation that the discovered column set
// matches names exactly: no missing and no unexpected names. Each name is a
// separately validated identifier compared byte-for-byte against
// dialect/driver-reported [database/sql.Rows.Columns] names with no case
// folding. Column order never changes the verdict. Duplicate or invalid
// identifiers fail suite preflight before SQL runs. Discovery uses a read-only
// zero-row probe and never scans row values. Results use [KindExactColumns],
// [RowDenominatorUnavailable], and publish [ResultFacts.RequiredColumns] plus
// ordered [ResultFacts.MissingColumns] and [ResultFacts.UnexpectedColumns].
// [WithScope] is incompatible and must be rejected at ValidateTable preflight.
func ExactColumns(names ...string) Expectation {
	return structuralColumnsExpectation{
		exact:   true,
		columns: append([]string(nil), names...),
	}
}

// structuralColumnsExpectation is the sealed table-level column-set check used
// by RequiredColumns and ExactColumns.
type structuralColumnsExpectation struct {
	exact   bool
	columns []string
}

func (e structuralColumnsExpectation) Name() string {
	joined := strings.Join(e.columns, ", ")
	if e.exact {
		if joined == "" {
			return "exact columns"
		}
		return "exact columns: " + joined
	}
	if joined == "" {
		return "required columns"
	}
	return "required columns: " + joined
}

func (e structuralColumnsExpectation) expectationKind() ExpectationKind {
	if e.exact {
		return KindExactColumns
	}
	return KindRequiredColumns
}

func (e structuralColumnsExpectation) preflight() error {
	if len(e.columns) == 0 {
		return newConfigError(fmt.Errorf("structural column expectation requires at least one column"))
	}
	if err := validateDistinctIdents(e.columns, "expected column"); err != nil {
		return newConfigError(err)
	}
	return nil
}

// rejectsScope marks structural discovery as incompatible with WithScope.
// Suite preflight should reject the combination rather than ignore scope.
func (e structuralColumnsExpectation) rejectsScope() {}

var (
	_ Expectation     = structuralColumnsExpectation{}
	_ metaExpectation = structuralColumnsExpectation{}
	_ rejectsScope    = structuralColumnsExpectation{}
)

func (e structuralColumnsExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	kind := e.expectationKind()
	label := e.Name()
	facts := ResultFacts{
		RequiredColumns: append([]string(nil), e.columns...),
	}
	base := Result{
		Kind:           kind,
		Name:           label,
		RowDenominator: RowDenominatorUnavailable,
		Facts:          facts,
	}

	discovered, query, err := discoverTableColumns(ctx, db, table, opts)
	if err != nil {
		captureDiagnostics(&base, opts, query, nil)
		return base, err
	}

	missing, unexpected := diffStructuralColumns(e.columns, discovered, e.exact)
	if len(missing) > 0 {
		facts.MissingColumns = missing
	}
	if len(unexpected) > 0 {
		facts.UnexpectedColumns = unexpected
	}

	success := len(missing) == 0 && len(unexpected) == 0
	name := structuralResultName(label, missing, unexpected)
	res := tableLevelResult(kind, "", name, success, facts)
	captureDiagnostics(&res, opts, query, nil)
	return res, nil
}

func structuralResultName(label string, missing, unexpected []string) string {
	if len(missing) == 0 && len(unexpected) == 0 {
		return label
	}
	parts := make([]string, 0, 2)
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf("missing %s", strings.Join(missing, ", ")))
	}
	if len(unexpected) > 0 {
		parts = append(parts, fmt.Sprintf("unexpected %s", strings.Join(unexpected, ", ")))
	}
	return label + ": " + strings.Join(parts, "; ")
}

// diffStructuralColumns reports missing names in caller declaration order and,
// when exact is true, unexpected names in discovery order. Comparisons are
// byte-for-byte with no case folding.
func diffStructuralColumns(expected, discovered []string, exact bool) (missing, unexpected []string) {
	have := make(map[string]struct{}, len(discovered))
	for _, name := range discovered {
		have[name] = struct{}{}
	}
	for _, name := range expected {
		if _, ok := have[name]; !ok {
			missing = append(missing, name)
		}
	}
	if !exact {
		return missing, nil
	}
	want := make(map[string]struct{}, len(expected))
	for _, name := range expected {
		want[name] = struct{}{}
	}
	for _, name := range discovered {
		if _, ok := want[name]; !ok {
			unexpected = append(unexpected, name)
		}
	}
	return missing, unexpected
}
