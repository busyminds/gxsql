package gxsql

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestExportStructuredKeyAndReferenceFactsJSON(t *testing.T) {
	rep := Report{
		Target: &TableRef{Name: "orders"},
		Results: []Result{
			{
				Kind:           KindCompositeUnique,
				Column:         "", // tuples must not be encoded as comma-separated Column text
				Success:        true,
				RowDenominator: RowDenominatorAvailable,
				Total:          1,
				Facts: ResultFacts{
					KeyColumns: []string{"tenant_id", "order_id"},
				},
			},
			{
				Kind:           KindReference,
				Column:         "",
				Success:        false,
				RowDenominator: RowDenominatorAvailable,
				Total:          2,
				FailedCount:    1,
				FailedPercent:  50,
				SampleValues:   []any{"local-sample"},
				FailedKeys:     []RowKey{{"tenant-a", "orphan-1"}},
				Facts: ResultFacts{
					Reference: &ReferenceFacts{
						LocalColumns:  []string{"tenant_id", "customer_id"},
						Parent:        SchemaTable("public", "customers"),
						ParentColumns: []string{"tenant_id", "id"},
					},
				},
			},
		},
	}

	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	if dto.SchemaVersion != ExportSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", dto.SchemaVersion, ExportSchemaVersion)
	}
	if ExportSchemaVersion != "gxsql.report.v1" {
		t.Fatalf("ExportSchemaVersion = %q, want gxsql.report.v1", ExportSchemaVersion)
	}

	unique := dto.Results[0]
	if unique.Column != "" {
		t.Fatalf("unique column = %q, want empty (no comma-separated tuple encoding)", unique.Column)
	}
	if unique.Facts == nil {
		t.Fatal("expected unique facts")
	}
	if got, want := unique.Facts.KeyColumns, []string{"tenant_id", "order_id"}; !stringSlicesEqual(got, want) {
		t.Fatalf("key_columns = %#v, want %#v", got, want)
	}
	if unique.Facts.Reference != nil {
		t.Fatalf("unique reference = %#v, want nil", unique.Facts.Reference)
	}

	ref := dto.Results[1]
	if ref.Column != "" {
		t.Fatalf("reference column = %q, want empty", ref.Column)
	}
	if ref.Facts == nil || ref.Facts.Reference == nil {
		t.Fatalf("reference facts = %#v", ref.Facts)
	}
	rf := ref.Facts.Reference
	if got, want := rf.LocalColumns, []string{"tenant_id", "customer_id"}; !stringSlicesEqual(got, want) {
		t.Fatalf("local_columns = %#v, want %#v", got, want)
	}
	if got, want := rf.ParentColumns, []string{"tenant_id", "id"}; !stringSlicesEqual(got, want) {
		t.Fatalf("parent_columns = %#v, want %#v", got, want)
	}
	if rf.Parent.Schema != "public" || rf.Parent.Table != "customers" {
		t.Fatalf("parent = %#v, want schema-qualified public.customers", rf.Parent)
	}

	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		`"schema_version":"gxsql.report.v1"`,
		`"kind":"composite_unique"`,
		`"kind":"reference"`,
		`"key_columns":["tenant_id","order_id"]`,
		`"local_columns":["tenant_id","customer_id"]`,
		`"parent_columns":["tenant_id","id"]`,
		`"parent":{"schema":"public","table":"customers"}`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("export JSON missing %s in %s", want, s)
		}
	}
	for _, forbidden := range []string{
		`"column":"tenant_id,order_id"`,
		`"column":"tenant_id, customer_id"`,
		"local-sample",
		"orphan-1",
		"samples",
		"failed_keys",
		"diagnostics",
	} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("default export unexpectedly contained %q in %s", forbidden, s)
		}
	}
}

func TestExportStructuredFactsPreserveDeclarationOrderAndCopy(t *testing.T) {
	keys := []string{"b", "a", "c"}
	local := []string{"l2", "l1"}
	parentCols := []string{"p2", "p1"}
	rep := Report{Results: []Result{{
		Facts: ResultFacts{
			KeyColumns: keys,
			Reference: &ReferenceFacts{
				LocalColumns:  local,
				Parent:        Table("parents"),
				ParentColumns: parentCols,
			},
		},
	}}}

	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	facts := dto.Results[0].Facts
	if facts == nil || facts.Reference == nil {
		t.Fatalf("facts = %#v", facts)
	}
	if !stringSlicesEqual(facts.KeyColumns, []string{"b", "a", "c"}) {
		t.Fatalf("key order = %#v", facts.KeyColumns)
	}
	if !stringSlicesEqual(facts.Reference.LocalColumns, []string{"l2", "l1"}) {
		t.Fatalf("local order = %#v", facts.Reference.LocalColumns)
	}
	if !stringSlicesEqual(facts.Reference.ParentColumns, []string{"p2", "p1"}) {
		t.Fatalf("parent order = %#v", facts.Reference.ParentColumns)
	}

	keys[0] = "mutated"
	local[0] = "mutated"
	parentCols[0] = "mutated"
	if facts.KeyColumns[0] != "b" || facts.Reference.LocalColumns[0] != "l2" || facts.Reference.ParentColumns[0] != "p2" {
		t.Fatalf("exportFacts mutated caller slices: %#v", facts)
	}
}

func TestExportStructuredFactsDefaultAndOptInPrivacy(t *testing.T) {
	parentSecret := "parent-row-value-secret"
	rep := Report{
		Target: &TableRef{Name: "orders"},
		Results: []Result{{
			SampleValues: []any{"local-sample-secret"},
			FailedKeys:   []RowKey{{"local-key-secret"}},
			diagnostics: &resultDiagnostics{
				query: `SELECT 1 FROM "orders" WHERE orphan`,
				args:  []any{"local-arg-secret"},
			},
			Facts: ResultFacts{
				Reference: &ReferenceFacts{
					LocalColumns:  []string{"customer_id"},
					Parent:        SchemaTable("sales", "customers"),
					ParentColumns: []string{"id"},
				},
			},
		}},
	}

	defaultDTO, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	defaultJSON, err := json.Marshal(defaultDTO)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"local-sample-secret",
		"local-key-secret",
		"local-arg-secret",
		parentSecret,
		"SELECT",
		`"samples"`,
		`"failed_keys"`,
		`"diagnostics"`,
		`"args"`,
	} {
		if bytes.Contains(defaultJSON, []byte(forbidden)) {
			t.Fatalf("default export leaked %q in %s", forbidden, defaultJSON)
		}
	}
	if defaultDTO.Results[0].Facts == nil || defaultDTO.Results[0].Facts.Reference == nil {
		t.Fatal("expected reference facts under default privacy")
	}
	if defaultDTO.Results[0].Facts.Reference.Parent.Schema != "sales" {
		t.Fatalf("parent schema = %q", defaultDTO.Results[0].Facts.Reference.Parent.Schema)
	}

	optIn, err := ExportReport(rep,
		IncludeSamples(),
		IncludeFailedKeys(),
		IncludeCapturedArguments(),
	)
	if err != nil {
		t.Fatal(err)
	}
	optJSON, err := json.Marshal(optIn)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"local-sample-secret", "local-key-secret", "local-arg-secret"} {
		if !strings.Contains(string(optJSON), want) {
			t.Fatalf("opt-in export missing %q in %s", want, optJSON)
		}
	}
	if strings.Contains(string(optJSON), parentSecret) {
		t.Fatalf("opt-in export contained parent value %q in %s", parentSecret, optJSON)
	}
	if optIn.Results[0].Diagnostics == nil {
		t.Fatal("expected diagnostics with IncludeCapturedArguments")
	}
	if strings.Contains(optIn.Results[0].Diagnostics.Query, parentSecret) {
		t.Fatalf("diagnostics query contained parent value: %q", optIn.Results[0].Diagnostics.Query)
	}
}

func TestExportEmptyStructuredFactsOmitted(t *testing.T) {
	rep := Report{Results: []Result{
		{Facts: ResultFacts{}},
		{Facts: ResultFacts{KeyColumns: nil}},
		{Facts: ResultFacts{KeyColumns: []string{}}},
		{Facts: ResultFacts{Reference: nil}},
	}}
	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	if dto.SchemaVersion != "gxsql.report.v1" {
		t.Fatalf("schema_version = %q", dto.SchemaVersion)
	}
	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"facts"`)) {
		t.Fatalf("empty facts were exported: %s", data)
	}
	if bytes.Contains(data, []byte(`"key_columns"`)) || bytes.Contains(data, []byte(`"reference"`)) {
		t.Fatalf("empty structured fact fields were exported: %s", data)
	}
	for i, res := range dto.Results {
		if res.Facts != nil {
			t.Fatalf("results[%d].Facts = %#v, want nil", i, res.Facts)
		}
	}
}

func TestExportUnqualifiedReferenceParentTargetShape(t *testing.T) {
	rep := Report{Results: []Result{{
		Facts: ResultFacts{
			Reference: &ReferenceFacts{
				LocalColumns:  []string{"customer_id"},
				Parent:        Table("customers"),
				ParentColumns: []string{"id"},
			},
		},
	}}}
	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	parent := dto.Results[0].Facts.Reference.Parent
	if parent.Schema != "" || parent.Table != "customers" {
		t.Fatalf("parent = %#v", parent)
	}
	data, err := json.Marshal(dto.Results[0].Facts.Reference)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"parent":{"table":"customers"}`)) {
		t.Fatalf("unqualified parent JSON = %s", data)
	}
	if bytes.Contains(data, []byte(`"schema"`)) {
		t.Fatalf("unqualified parent unexpectedly included schema: %s", data)
	}
}

func stringSlicesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestExportCrossColumnFactsAndPrivacy(t *testing.T) {
	rep := Report{Results: []Result{
		{
			Kind:           KindGreaterOrEqualColumn,
			Name:           "end_at >= start_at",
			Column:         "end_at",
			Success:        false,
			RowDenominator: RowDenominatorAvailable,
			Total:          2,
			FailedCount:    1,
			SampleValues:   []any{"secret-sample"},
			FailedKeys:     []RowKey{{"secret-key"}},
			diagnostics: &resultDiagnostics{
				query: "SELECT secret_query",
				args:  []any{"secret-arg"},
			},
			Facts: ResultFacts{
				Comparison: &ComparisonFacts{
					LeftColumn:   "end_at",
					RightColumn:  "start_at",
					Relationship: ">=",
				},
			},
		},
		{
			Kind:           KindRatioEqual,
			Name:           "actual_units == planned_units * 2",
			Column:         "actual_units",
			Success:        true,
			RowDenominator: RowDenominatorAvailable,
			Total:          1,
			Facts: ResultFacts{
				Ratio: &RatioFacts{
					LeftColumn:  "actual_units",
					RightColumn: "planned_units",
					Bound:       2,
				},
			},
		},
	}}

	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	if got := dto.Results[0].DisplayName; got != "end_at >= start_at" {
		t.Fatalf("comparison display name = %q", got)
	}
	comparison := dto.Results[0].Facts.Comparison
	if comparison == nil || comparison.LeftColumn != "end_at" || comparison.RightColumn != "start_at" || comparison.Relationship != ">=" {
		t.Fatalf("comparison facts = %#v", comparison)
	}
	ratio := dto.Results[1].Facts.Ratio
	if ratio == nil || ratio.Bound != 2 {
		t.Fatalf("ratio facts = %#v", ratio)
	}
	if got := dto.Results[1].DisplayName; got != "actual_units ratio == (...)" {
		t.Fatalf("ratio display name = %q", got)
	}

	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(data)
	for _, forbidden := range []string{"secret-sample", "secret-key", "secret-arg", "secret_query", `"samples"`, `"failed_keys"`, `"diagnostics"`} {
		if strings.Contains(jsonText, forbidden) {
			t.Fatalf("default export leaked %q in %s", forbidden, jsonText)
		}
	}
	for _, wanted := range []string{`"comparison"`, `"left_column":"end_at"`, `"relationship":"\u003e="`, `"ratio"`, `"bound":2`} {
		if !strings.Contains(jsonText, wanted) {
			t.Fatalf("default export missing %q in %s", wanted, jsonText)
		}
	}
}
