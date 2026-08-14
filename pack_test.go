package gxsql

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func orderPolicyPack(prefix string) []Expectation {
	return []Expectation{
		WithID(prefix+".age", Int("age").Between(0, 120)),
		WithID(prefix+".shipped_at", When(
			TrustedEligibility("status-shipped", "status = ?", "shipped"),
			Column("shipped_at").NotNull(),
		)),
	}
}

func TestPolicyPackCallsReturnIndependentSlices(t *testing.T) {
	first := orderPolicyPack("acme.orders")
	first[0] = nil

	second := orderPolicyPack("acme.orders")
	if second[0] == nil {
		t.Fatal("mutating one pack result changed a later call")
	}
	if second[1] == nil {
		t.Fatal("later pack call returned a nil expectation")
	}
}

func TestPolicyPackCompositionMatchesFlatSuite(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25), "status": "shipped", "shipped_at": "2024-01-01"},
		map[string]any{"id": int64(2), "age": int64(150), "status": "shipped", "shipped_at": nil},
		map[string]any{"id": int64(3), "age": int64(30), "status": "pending", "shipped_at": nil},
	))

	composed := append(append(orderPolicyPack("acme.orders"),
		WithID("acme.orders.rows", RowCount().GreaterOrEqual(1)),
	), WithID("local.orders.email", String("email").NotEmpty()))
	flat := append(orderPolicyPack("acme.orders"),
		WithID("acme.orders.rows", RowCount().GreaterOrEqual(1)),
		WithID("local.orders.email", String("email").NotEmpty()),
	)

	composedReport, err := NewSuite(composed...).ValidateTable(
		context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatalf("composed ValidateTable: %v", err)
	}
	flatReport, err := NewSuite(flat...).ValidateTable(
		context.Background(), openHarnessDB(t), Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatalf("flat ValidateTable: %v", err)
	}
	if !reflect.DeepEqual(composedReport.Results, flatReport.Results) {
		t.Fatalf("composed results differ from flat results:\ncomposed=%#v\nflat=%#v", composedReport.Results, flatReport.Results)
	}
}

func TestDuplicateIDsAcrossPolicyPacksFailBeforeSQL(t *testing.T) {
	setHarnessData(t, harnessUsers(map[string]any{"id": int64(1), "age": int64(25)}))
	counter := openCountingHarnessDB(t)

	pack := func() []Expectation {
		return []Expectation{WithID("acme.orders.age", Int("age").Between(0, 120))}
	}
	_, err := NewSuite(append(pack(), pack()...)...).ValidateTable(
		context.Background(), counter, Table("users"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected duplicate ID preflight error")
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want zero before duplicate-ID preflight", counter.queries)
	}
	var preflight *PreflightErrors
	if !errors.As(err, &preflight) {
		t.Fatalf("error = %T, want *PreflightErrors", err)
	}
	if len(preflight.Issues) < 2 {
		t.Fatalf("issues = %d, want both duplicate slots", len(preflight.Issues))
	}
}

func TestDuplicatePackIDsContinueOnErrorPreserveSlots(t *testing.T) {
	setHarnessData(t, harnessUsers(map[string]any{"id": int64(1), "age": int64(25)}))
	pack := func() []Expectation {
		return []Expectation{WithID("acme.orders.age", Int("age").Between(0, 120))}
	}
	expectations := append(append(pack(), pack()...), WithID("acme.orders.rows", RowCount().Equal(1)))
	report, err := NewSuite(expectations...).ValidateTable(
		context.Background(), openHarnessDB(t), Table("users"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatalf("ValidateTable: %v", err)
	}
	if len(report.Results) != 3 {
		t.Fatalf("results = %d, want three declaration-order slots", len(report.Results))
	}
	if report.Results[0].Err == nil || report.Results[1].Err == nil {
		t.Fatal("duplicate pack slots must retain configuration errors")
	}
	if report.Results[2].Err != nil || !report.Results[2].Success {
		t.Fatalf("valid sibling result = %#v, want executed pass", report.Results[2])
	}
	if report.Results[2].ID != "acme.orders.rows" {
		t.Fatalf("valid sibling ID = %q, want acme.orders.rows", report.Results[2].ID)
	}
}

func TestPolicyPackSuiteConcurrentReuse(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "age": int64(25), "status": "shipped", "shipped_at": "2024-01-01"},
		map[string]any{"id": int64(2), "age": int64(30), "status": "pending", "shipped_at": nil},
	))
	suite := NewSuite(orderPolicyPack("acme.orders")...)
	db := openHarnessDB(t)

	const workers = 8
	errCh := make(chan error, workers)
	for range workers {
		go func() {
			report, err := suite.ValidateTable(
				context.Background(), db, Table("users"), WithDialect(Postgres()),
			)
			if err != nil {
				errCh <- err
				return
			}
			if len(report.Results) != 2 || !report.Results[0].Success || !report.Results[1].Success {
				errCh <- errors.New("concurrent policy-pack validation returned an unexpected report")
				return
			}
			errCh <- nil
		}()
	}
	for range workers {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}
