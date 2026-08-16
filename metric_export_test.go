package gxsql

import (
	"math"
	"testing"
)

func TestExportReportIncludesSpec07Facts(t *testing.T) {
	nonNull, total, rate := 2, 3, 2.0/3.0
	bound := 0.99
	rep := Report{Results: []Result{{
		Kind:    KindCompletenessRate,
		Name:    "email completeness rate >= 0.99",
		Success: true,
		Facts: ResultFacts{Completeness: &CompletenessFacts{
			NonNullCount:    &nonNull,
			TotalCount:      &total,
			Rate:            &rate,
			ConfiguredBound: &bound,
		}},
	}}}
	exported, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	facts := exported.Results[0].Facts
	if facts == nil || facts.Completeness == nil || facts.Completeness.NonNullCount == nil || facts.Completeness.Rate == nil {
		t.Fatalf("exported completeness facts = %#v", facts)
	}
	if facts.Completeness.NonNullCount == nil || *facts.Completeness.NonNullCount != 2 {
		t.Fatalf("exported non-null count = %#v", facts.Completeness)
	}
}
func TestExportReportIncludesBetweenMetricBounds(t *testing.T) {
	lower, upper := 0.25, 0.75
	rep := Report{Results: []Result{{
		Kind: KindCompletenessRate,
		Facts: ResultFacts{Completeness: &CompletenessFacts{
			ConfiguredLower: &lower,
			ConfiguredUpper: &upper,
		}},
	}}}
	exported, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	facts := exported.Results[0].Facts
	if facts == nil || facts.Completeness == nil ||
		facts.Completeness.ConfiguredLower == nil || facts.Completeness.ConfiguredUpper == nil {
		t.Fatalf("exported completeness bounds = %#v", facts)
	}
	if facts.Completeness.ConfiguredLower.Kind != "json_number" || facts.Completeness.ConfiguredUpper.Kind != "json_number" {
		t.Fatalf("exported completeness bounds = %#v", facts.Completeness)
	}
}

func TestExportReportNormalizesNonFiniteSpec07Facts(t *testing.T) {
	bad := math.NaN()
	exported, err := ExportReport(Report{Results: []Result{{
		Kind:    KindDominantShare,
		Success: true,
		Facts:   ResultFacts{DominantShare: &DominantShareFacts{Share: &bad}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	facts := exported.Results[0].Facts
	if facts == nil || facts.DominantShare == nil || facts.DominantShare.Share == nil || facts.DominantShare.Share.Kind != "non_finite" {
		t.Fatalf("non-finite dominant share = %#v", facts)
	}
}
