package gxsql

import (
	"context"
	"fmt"
	"strings"
)

// ColumnNullabilityBuilder constructs catalog nullability contracts for one
// column. Build with [ColumnNullability], then call [ColumnNullabilityBuilder.Nullable]
// or [ColumnNullabilityBuilder.NotNullable]. These assert driver-/catalog-reported
// nullability, not row-value NULL rates ([ColumnBuilder.IsNull] /
// [ColumnBuilder.NotNull]).
type ColumnNullabilityBuilder struct {
	column string
}

// ColumnNullability starts a catalog nullability contract for name. name must
// satisfy identifier validation at suite preflight.
func ColumnNullability(name string) ColumnNullabilityBuilder {
	return ColumnNullabilityBuilder{column: name}
}

// NotNullable returns a table-level expectation that the column is advertised
// NOT NULL. Missing columns are structural policy misses. Unknown nullability
// fails closed as a typed metadata error. Results use [KindColumnNullability].
// [WithScope] is incompatible.
func (b ColumnNullabilityBuilder) NotNullable() Expectation {
	return columnNullabilityExpectation{
		column:   b.column,
		expected: CatalogNullabilityNotNullable,
	}
}

// Nullable returns a table-level expectation that the column is advertised
// NULL-capable. Missing columns are structural policy misses. Unknown
// nullability fails closed as a typed metadata error. Results use
// [KindColumnNullability]. [WithScope] is incompatible.
func (b ColumnNullabilityBuilder) Nullable() Expectation {
	return columnNullabilityExpectation{
		column:   b.column,
		expected: CatalogNullabilityNullable,
	}
}

// ColumnTypeBuilder constructs exact reported-type contracts for one column.
// Build with [ColumnType], then call [ColumnTypeBuilder.ReportedAs] with the
// dialect-exact spelling required by the selected engine.
type ColumnTypeBuilder struct {
	column string
}

// ColumnType starts an exact reported-type contract for name. name must satisfy
// identifier validation at suite preflight.
func ColumnType(name string) ColumnTypeBuilder {
	return ColumnTypeBuilder{column: name}
}

// ReportedAs returns a table-level expectation that the driver-/catalog-reported
// type name equals typeName byte-for-byte. Blank or whitespace-padded type names
// fail suite preflight before SQL runs. Missing columns are structural policy
// misses. Unsupported dialects fail closed at suite preflight. Results use
// [KindColumnType]. [WithScope] is incompatible.
func (b ColumnTypeBuilder) ReportedAs(typeName string) Expectation {
	return columnTypeExpectation{
		column:   b.column,
		expected: typeName,
	}
}

type columnNullabilityExpectation struct {
	column   string
	expected CatalogNullability
}

func (e columnNullabilityExpectation) Name() string {
	switch e.expected {
	case CatalogNullabilityNullable:
		return e.column + " nullable"
	default:
		return e.column + " not nullable"
	}
}

func (e columnNullabilityExpectation) expectationKind() ExpectationKind {
	return KindColumnNullability
}

func (e columnNullabilityExpectation) preflight() error {
	if err := validateIdent(e.column); err != nil {
		return newConfigError(err)
	}
	switch e.expected {
	case CatalogNullabilityNullable, CatalogNullabilityNotNullable:
		return nil
	default:
		return newConfigError(fmt.Errorf("unsupported catalog nullability claim %q", e.expected))
	}
}

func (e columnNullabilityExpectation) rejectsScope() {}

var (
	_ Expectation     = columnNullabilityExpectation{}
	_ metaExpectation = columnNullabilityExpectation{}
	_ rejectsScope    = columnNullabilityExpectation{}
)

func (e columnNullabilityExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	kind := KindColumnNullability
	label := e.Name()
	facts := ResultFacts{
		ConfiguredNullability: e.expected,
	}
	base := Result{
		Kind:           kind,
		Name:           label,
		Column:         e.column,
		RowDenominator: RowDenominatorUnavailable,
		Facts:          facts,
	}

	discovered, query, err := discoverTableColumnMetadata(ctx, db, table, opts)
	if err != nil {
		captureDiagnostics(&base, opts, query, nil)
		return base, err
	}

	meta, ok := findDiscoveredColumn(discovered, e.column)
	if !ok {
		facts.MissingColumns = []string{e.column}
		res := tableLevelResult(kind, e.column, label+": missing "+e.column, false, facts)
		captureDiagnostics(&res, opts, query, nil)
		return res, nil
	}

	facts.ObservedNullability = meta.Nullability
	if meta.Nullability == CatalogNullabilityUnknown {
		res := tableLevelResult(kind, e.column, label, false, facts)
		captureDiagnostics(&res, opts, query, nil)
		return res, unknownMetadataError(kind, opts.dialect, e.column, nullabilityCapabilityName)
	}
	success := meta.Nullability == e.expected
	name := label
	if !success {
		name = label + ": mismatched nullability"
	}
	res := tableLevelResult(kind, e.column, name, success, facts)
	captureDiagnostics(&res, opts, query, nil)
	return res, nil
}

type columnTypeExpectation struct {
	column   string
	expected string
}

func (e columnTypeExpectation) Name() string {
	if e.expected == "" {
		return e.column + " reported type"
	}
	return e.column + " reported as " + e.expected
}

func (e columnTypeExpectation) expectationKind() ExpectationKind {
	return KindColumnType
}

func (e columnTypeExpectation) preflight() error {
	if err := validateIdent(e.column); err != nil {
		return newConfigError(err)
	}
	trimmed := strings.TrimSpace(e.expected)
	if trimmed == "" {
		return newConfigError(fmt.Errorf("reported type name is required"))
	}
	if trimmed != e.expected {
		return newConfigError(fmt.Errorf("reported type name must equal its trimmed form"))
	}
	return nil
}

func (e columnTypeExpectation) rejectsScope() {}

var (
	_ Expectation     = columnTypeExpectation{}
	_ metaExpectation = columnTypeExpectation{}
	_ rejectsScope    = columnTypeExpectation{}
)

func (e columnTypeExpectation) evaluateSQL(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (Result, error) {
	kind := KindColumnType
	label := e.Name()
	facts := ResultFacts{
		ConfiguredReportedType: e.expected,
	}
	base := Result{
		Kind:           kind,
		Name:           label,
		Column:         e.column,
		RowDenominator: RowDenominatorUnavailable,
		Facts:          facts,
	}

	discovered, query, err := discoverTableColumnMetadata(ctx, db, table, opts)
	if err != nil {
		captureDiagnostics(&base, opts, query, nil)
		return base, err
	}

	meta, ok := findDiscoveredColumn(discovered, e.column)
	if !ok {
		facts.MissingColumns = []string{e.column}
		res := tableLevelResult(kind, e.column, label+": missing "+e.column, false, facts)
		captureDiagnostics(&res, opts, query, nil)
		return res, nil
	}

	facts.ObservedReportedType = meta.ReportedType
	if meta.ReportedType == "" {
		res := tableLevelResult(kind, e.column, label, false, facts)
		captureDiagnostics(&res, opts, query, nil)
		return res, unknownMetadataError(kind, opts.dialect, e.column, exactReportedTypeCapabilityName)
	}
	success := meta.ReportedType == e.expected
	name := label
	if !success {
		name = label + ": mismatched type"
	}
	res := tableLevelResult(kind, e.column, name, success, facts)
	captureDiagnostics(&res, opts, query, nil)
	return res, nil
}
