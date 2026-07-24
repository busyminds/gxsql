package gxsql

import (
	"errors"
	"strings"
	"testing"
)

func TestReportOKAndFailures(t *testing.T) {
	rep := Report{Results: []Result{
		{Name: "a", Success: true},
		{Name: "b", Success: false},
	}}
	if rep.OK() {
		t.Fatal("expected not OK")
	}
	if len(rep.Failures()) != 1 || rep.Failures()[0].Name != "b" {
		t.Fatalf("Failures() = %#v", rep.Failures())
	}
}

func TestReportErrNilOnSuccess(t *testing.T) {
	rep := Report{Results: []Result{{Name: "a", Success: true}}}
	if err := rep.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
}

func TestReportErrValidationError(t *testing.T) {
	rep := Report{
		ScopeID: "tenant-acme",
		Results: []Result{{Name: "age between [0,120]", Success: false}},
	}
	err := rep.Err()
	if err == nil {
		t.Fatal("expected validation error")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("got %T", err)
	}
	if ve.Report.ScopeID != rep.ScopeID {
		t.Fatalf("validation error ScopeID = %q, want %q", ve.Report.ScopeID, rep.ScopeID)
	}
	if !strings.Contains(err.Error(), "age between") {
		t.Fatalf("error text = %q", err.Error())
	}
}
