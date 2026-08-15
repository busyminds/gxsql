package gxsql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestValidateTableObserverReceivesTypedStatementEvent(t *testing.T) {
	setHarnessData(t, harnessUsers(map[string]any{"id": 1}))
	db := openHarnessDB(t)

	var events []QueryEvent
	observer := ObserverFunc(func(event QueryEvent) {
		events = append(events, event)
	})

	_, err := NewSuite(WithID("orders.total", RowCount().GreaterOrEqual(1))).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithObserver(observer),
	)
	if err != nil {
		t.Fatalf("ValidateTable returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("observer events = %d, want 1", len(events))
	}

	event := events[0]
	if event.ID != "orders.total" {
		t.Fatalf("event ID = %q, want %q", event.ID, "orders.total")
	}
	if event.Kind != KindRowCountGreaterEqual {
		t.Fatalf("event kind = %q, want %q", event.Kind, KindRowCountGreaterEqual)
	}
	if event.Category != QueryCategoryTotalCount {
		t.Fatalf("event category = %q, want %q", event.Category, QueryCategoryTotalCount)
	}
	if event.Status != QueryStatusSuccess {
		t.Fatalf("event status = %q, want %q", event.Status, QueryStatusSuccess)
	}
	if event.Duration < 0 {
		t.Fatalf("event duration = %s, want non-negative", event.Duration)
	}
}

func TestValidateTableObserverPanicReturnsTypedRunFailure(t *testing.T) {
	setHarnessData(t, harnessUsers(map[string]any{"id": 1}))
	db := openHarnessDB(t)

	observer := ObserverFunc(func(QueryEvent) {
		panic("observer failure")
	})
	report, err := NewSuite(RowCount().GreaterOrEqual(1)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		ContinueOnError(),
		WithObserver(observer),
	)
	if !errors.Is(err, ErrCategoryObserver) {
		t.Fatalf("ValidateTable error = %v, want observer category", err)
	}
	if report.Results != nil {
		t.Fatalf("report results = %#v, want nil on observer failure", report.Results)
	}
}

func TestValidateTableObserverPanicAbortsSharedScalarRun(t *testing.T) {
	setHarnessData(t, harnessUsers(map[string]any{"id": 1, "name": "ok"}))
	db := openHarnessDB(t)
	observer := ObserverFunc(func(QueryEvent) {
		panic("observer failure")
	})
	report, err := NewSuite(
		String("name").NotEmpty(),
		String("name").Empty(),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithSharedScalarEvaluation(),
		ContinueOnError(),
		WithObserver(observer),
	)
	if !errors.Is(err, ErrCategoryObserver) {
		t.Fatalf("ValidateTable error = %v, want observer category", err)
	}
	if report.Results != nil {
		t.Fatalf("report results = %#v, want nil on observer failure", report.Results)
	}
}

func TestValidateTableObserverReportsQueryCategories(t *testing.T) {
	setHarnessColumns(t, map[string][]string{"users": {"id", "name", "value"}})
	setHarnessData(t, harnessUsers(
		map[string]any{"id": 1, "name": "", "value": 2},
		map[string]any{"id": 2, "name": "ok", "value": 4},
	))
	db := openHarnessDB(t)

	var events []QueryEvent
	observer := ObserverFunc(func(event QueryEvent) {
		events = append(events, event)
	})
	_, err := NewSuite(
		RequiredColumns("id"),
		RowCount().GreaterOrEqual(1),
		String("name").NotEmpty(),
		Column("name").DistinctCount().Equal(2),
		Column("id").Unique(),
		Float("value").AverageBetween(1, 4),
		CustomCount("custom", TrustedCountQuery(
			"SELECT COUNT(*) FROM {{target}} WHERE {{scope}}",
		)),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithKey("id"),
		WithSampleCap(1),
		ContinueOnError(),
		WithObserver(observer),
	)
	if err != nil {
		t.Fatalf("ValidateTable returned error: %v", err)
	}

	got := make(map[QueryCategory]bool)
	for _, event := range events {
		got[event.Category] = true
		if event.Status == QueryStatusUnknown {
			t.Fatalf("event has unknown status: %#v", event)
		}
	}
	for _, category := range []QueryCategory{
		QueryCategoryStructuralDiscovery,
		QueryCategoryTotalCount,
		QueryCategoryFailureCount,
		QueryCategorySample,
		QueryCategoryFailedKeys,
		QueryCategoryDistinctCount,
		QueryCategoryUniqueness,
		QueryCategoryAggregate,
		QueryCategoryCustomCount,
	} {
		if !got[category] {
			t.Fatalf("missing query category %q in events %#v", category, events)
		}
	}
}

func TestValidateTableObserverReportsQueryStatuses(t *testing.T) {
	tests := []struct {
		name   string
		db     DB
		ctx    context.Context
		status QueryStatus
	}{
		{name: "database error", db: openErrorDB(t), ctx: context.Background(), status: QueryStatusDatabaseError},
		{name: "scan error", db: openScanErrorDB(t), ctx: context.Background(), status: QueryStatusScanError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var events []QueryEvent
			_, _ = NewSuite(RowCount().GreaterOrEqual(1)).ValidateTable(
				tt.ctx, tt.db, Table("users"),
				WithDialect(Postgres()),
				WithObserver(ObserverFunc(func(event QueryEvent) {
					events = append(events, event)
				})),
			)
			if len(events) != 1 {
				t.Fatalf("observer events = %d, want 1", len(events))
			}
			if events[0].Status != tt.status {
				t.Fatalf("event status = %q, want %q", events[0].Status, tt.status)
			}
		})
	}

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var events []QueryEvent
		_, _ = NewSuite(RowCount().GreaterOrEqual(1)).ValidateTable(
			ctx, openHarnessDB(t), Table("users"),
			WithDialect(Postgres()),
			WithObserver(ObserverFunc(func(event QueryEvent) {
				events = append(events, event)
			})),
		)
		if len(events) != 1 || events[0].Status != QueryStatusContextError {
			t.Fatalf("events = %#v, want one context error event", events)
		}
	})
}

func TestValidateTableObserverReportsSharedScalarCategory(t *testing.T) {
	setHarnessData(t, harnessUsers(
		map[string]any{"id": 1, "name": "", "value": 2},
		map[string]any{"id": 2, "name": "ok", "value": 4},
	))
	db := openHarnessDB(t)
	var events []QueryEvent
	_, err := NewSuite(
		WithID("name.present", String("name").NotEmpty()),
		WithID("name.empty", String("name").Empty()),
	).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithSharedScalarEvaluation(),
		ContinueOnError(),
		WithObserver(ObserverFunc(func(event QueryEvent) {
			events = append(events, event)
		})),
	)
	if err != nil {
		t.Fatalf("ValidateTable returned error: %v", err)
	}

	var totalEvent, sharedEvent *QueryEvent
	foundSampleID := false
	for i := range events {
		switch events[i].Category {
		case QueryCategoryTotalCount:
			if totalEvent != nil {
				t.Fatalf("events = %#v, want exactly one total_count event", events)
			}
			totalEvent = &events[i]
		case QueryCategorySharedScalar:
			if sharedEvent != nil {
				t.Fatalf("events = %#v, want exactly one shared_scalar event", events)
			}
			sharedEvent = &events[i]
		case QueryCategoryFailureCount:
			t.Fatalf("events = %#v, want no per-check failure_count for combined statement", events)
		case QueryCategorySample:
			if events[i].ID == "name.empty" {
				foundSampleID = true
			}
		}
	}
	if totalEvent == nil || sharedEvent == nil || !foundSampleID {
		t.Fatalf("events = %#v, want total_count, shared_scalar, and identified sample", events)
	}
	if totalEvent.ID != "name.present" || totalEvent.Kind != KindNotEmpty {
		t.Fatalf("total_count event = %#v, want first plan identity name.present/%q", *totalEvent, KindNotEmpty)
	}
	if sharedEvent.ID != "" || sharedEvent.Kind != "" {
		t.Fatalf("shared_scalar event = %#v, want empty ID and Kind", *sharedEvent)
	}
}

func TestValidateTableObserverCountsReportModes(t *testing.T) {
	tests := []struct {
		name     string
		options  func() []Option
		expected map[QueryCategory]int
	}{
		{
			name: "keys and samples",
			options: func() []Option {
				return []Option{WithKey("id"), WithSampleCap(1)}
			},
			expected: map[QueryCategory]int{
				QueryCategoryTotalCount:   1,
				QueryCategoryFailureCount: 1,
				QueryCategorySample:       1,
				QueryCategoryFailedKeys:   1,
			},
		},
		{
			name: "summary only",
			options: func() []Option {
				return []Option{SummaryOnly(), WithSampleCap(1)}
			},
			expected: map[QueryCategory]int{
				QueryCategoryTotalCount:   1,
				QueryCategoryFailureCount: 1,
				QueryCategorySample:       1,
			},
		},
		{
			name: "zero sample cap",
			options: func() []Option {
				return []Option{WithKey("id"), WithSampleCap(0)}
			},
			expected: map[QueryCategory]int{
				QueryCategoryTotalCount:   1,
				QueryCategoryFailureCount: 1,
				QueryCategoryFailedKeys:   1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setHarnessData(t, harnessUsers(
				map[string]any{"id": 1, "name": ""},
				map[string]any{"id": 2, "name": "ok"},
			))
			db := openHarnessDB(t)
			var events []QueryEvent
			options := append(tt.options(), WithDialect(Postgres()), WithObserver(
				ObserverFunc(func(event QueryEvent) {
					events = append(events, event)
				}),
			))
			_, err := NewSuite(String("name").NotEmpty()).ValidateTable(
				context.Background(), db, Table("users"), options...,
			)
			if err != nil {
				t.Fatalf("ValidateTable returned error: %v", err)
			}
			got := make(map[QueryCategory]int)
			for _, event := range events {
				got[event.Category]++
			}
			if len(got) != len(tt.expected) {
				t.Fatalf("category counts = %#v, want %#v", got, tt.expected)
			}
			for category, want := range tt.expected {
				if got[category] != want {
					t.Errorf("category %q count = %d, want %d", category, got[category], want)
				}
			}
		})
	}
}

func TestSQLTransactionSatisfiesDB(t *testing.T) {
	var _ DB = (*sql.Tx)(nil)
}
