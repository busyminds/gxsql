package gxsql

import (
	"context"
	"database/sql"
)

const (
	schemaMetadataCapabilityName    = "schema_metadata"
	nullabilityCapabilityName       = "nullability"
	exactReportedTypeCapabilityName = "exact_reported_type"
)

// SchemaMetadataCapability describes which catalog schema claims a dialect can
// evaluate. Built-ins advertise support only for claims proven via
// database/sql.Rows.ColumnTypes on the existing zero-row structural probe.
type SchemaMetadataCapability struct {
	// Name is the capability family label. Built-ins use "schema_metadata".
	Name string
	// Nullability reports whether catalog nullability claims are supported.
	Nullability bool
	// ExactReportedType reports whether exact driver-reported type name
	// comparisons are supported.
	ExactReportedType bool
}

// SchemaMetadataDialect is an optional dialect extension that advertises
// catalog nullability and exact reported-type support. It does not widen
// [Dialect]; dialects that omit a requested claim fail the corresponding
// schema contract closed at suite preflight with [UnsupportedCapabilityError].
type SchemaMetadataDialect interface {
	// SchemaMetadataCapability returns the catalog claims this dialect can
	// evaluate.
	SchemaMetadataCapability() SchemaMetadataCapability
}

type schemaMetadataClaim uint8

const (
	schemaMetadataClaimNullability schemaMetadataClaim = iota + 1
	schemaMetadataClaimExactReportedType
)

type discoveredColumnMetadata struct {
	Name         string
	ReportedType string
	Nullability  CatalogNullability
}

func schemaMetadataCapabilityFor(d Dialect) (SchemaMetadataCapability, bool) {
	sd, ok := d.(SchemaMetadataDialect)
	if !ok {
		return SchemaMetadataCapability{}, false
	}
	capability := sd.SchemaMetadataCapability()
	if capability.Name == "" {
		capability.Name = schemaMetadataCapabilityName
	}
	return capability, true
}

func schemaMetadataCapabilityError(kind ExpectationKind, d Dialect, claim schemaMetadataClaim) error {
	capability, ok := schemaMetadataCapabilityFor(d)
	missing := ""
	switch claim {
	case schemaMetadataClaimNullability:
		if ok && capability.Nullability {
			return nil
		}
		missing = nullabilityCapabilityName
	case schemaMetadataClaimExactReportedType:
		if ok && capability.ExactReportedType {
			return nil
		}
		missing = exactReportedTypeCapabilityName
	default:
		missing = schemaMetadataCapabilityName
	}
	return unsupportedCapabilityError(kind, d, missing)
}

func requiresSchemaMetadataClaim(exp Expectation) (ExpectationKind, schemaMetadataClaim, bool) {
	switch unwrapExpectation(exp).(type) {
	case columnNullabilityExpectation:
		return KindColumnNullability, schemaMetadataClaimNullability, true
	case columnTypeExpectation:
		return KindColumnType, schemaMetadataClaimExactReportedType, true
	default:
		return "", 0, false
	}
}

func (postgresDialect) SchemaMetadataCapability() SchemaMetadataCapability {
	// pgx and common Postgres drivers typically omit ColumnTypeNullable, so
	// Rows.ColumnTypes leaves nullability unknown. Refuse rather than always
	// failing closed as UnknownMetadataError after evaluation starts.
	return SchemaMetadataCapability{
		Name:              schemaMetadataCapabilityName,
		Nullability:       false,
		ExactReportedType: true,
	}
}

func (mysqlDialect) SchemaMetadataCapability() SchemaMetadataCapability {
	// The conformance-supported MySQL driver reports ColumnType.Nullable
	// through Rows.ColumnTypes. The dialect assumes callers use a driver with
	// that contract; ok=false still fails closed at evaluation.
	return SchemaMetadataCapability{
		Nullability:       true,
		ExactReportedType: true,
	}
}

func (duckdbDialect) SchemaMetadataCapability() SchemaMetadataCapability {
	// duckdb-go ColumnTypes returns Nullable with ok=false. Refuse nullability
	// claims at preflight rather than guessing.
	return SchemaMetadataCapability{
		Name:              schemaMetadataCapabilityName,
		Nullability:       false,
		ExactReportedType: true,
	}
}

func (sqliteDialect) SchemaMetadataCapability() SchemaMetadataCapability {
	// modernc/sqlite may report Nullable ok=true while always returning
	// nullable=true, including for NOT NULL columns. Refuse rather than trust
	// untruthful metadata. Exact reported type names remain available.
	return SchemaMetadataCapability{
		Name:              schemaMetadataCapabilityName,
		Nullability:       false,
		ExactReportedType: true,
	}
}

// discoverTableColumnMetadata runs a read-only zero-row probe and returns
// driver-reported column names, DatabaseTypeName values, and nullability from
// Rows.ColumnTypes. It never scans row values or issues schema writes.
func discoverTableColumnMetadata(
	ctx context.Context, db DB, table TableRef, opts evalOptions,
) (columns []discoveredColumnMetadata, query string, err error) {
	query, err = withZeroRowDiscovery(ctx, db, table, opts, func(rows *sql.Rows) error {
		types, e := rows.ColumnTypes()
		if e != nil {
			return categorizeScanError(ctx, e)
		}
		columns = make([]discoveredColumnMetadata, 0, len(types))
		for _, ct := range types {
			meta := discoveredColumnMetadata{
				Name:         ct.Name(),
				ReportedType: ct.DatabaseTypeName(),
				Nullability:  CatalogNullabilityUnknown,
			}
			if nullable, ok := ct.Nullable(); ok {
				if nullable {
					meta.Nullability = CatalogNullabilityNullable
				} else {
					meta.Nullability = CatalogNullabilityNotNullable
				}
			}
			columns = append(columns, meta)
		}
		return nil
	})
	if err != nil {
		return nil, query, err
	}
	return columns, query, nil
}

func findDiscoveredColumn(columns []discoveredColumnMetadata, name string) (discoveredColumnMetadata, bool) {
	for _, col := range columns {
		if col.Name == name {
			return col, true
		}
	}
	return discoveredColumnMetadata{}, false
}
