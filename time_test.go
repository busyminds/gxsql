package gxsql

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func temporalAnchor() time.Time {
	return time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
}

func assertNoCurrentTimeSQL(t *testing.T, db *recordingDB) {
	t.Helper()
	for i, q := range db.queries {
		lower := strings.ToLower(q.text)
		for _, forbidden := range []string{
			"now()",
			"current_timestamp",
			"current_time",
			"localtimestamp",
			"localtime",
			"sysdate",
			"gettime()",
			"getdate()",
		} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("query %d embeds current-time SQL %q: %q", i, forbidden, q.text)
			}
		}
	}
}

func assertObservedPresent(t *testing.T, got *bool, want bool) {
	t.Helper()
	if got == nil {
		t.Fatal("ObservedTimePresent = nil, want non-nil")
	}
	if *got != want {
		t.Fatalf("ObservedTimePresent = %v, want %v", *got, want)
	}
}

func assertObservedAbsent(t *testing.T, facts ResultFacts) {
	t.Helper()
	if facts.ObservedTime != nil {
		t.Fatalf("ObservedTime = %v, want nil", facts.ObservedTime)
	}
	assertObservedPresent(t, facts.ObservedTimePresent, false)
}

func assertTimePtrEqual(t *testing.T, got *time.Time, want time.Time, label string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %s", label, want.Format(time.RFC3339Nano))
	}
	if !got.Equal(want) {
		t.Fatalf("%s = %s, want %s", label, got.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
}

func TestFreshSinceExactCutoffPasses(t *testing.T) {
	cutoff := temporalAnchor()
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "ingested_at": cutoff.Add(-time.Hour)},
		map[string]any{"id": int64(2), "ingested_at": cutoff},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Timestamp("ingested_at").FreshSince(cutoff)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || res.Kind != KindTimestampFreshSince || res.RowDenominator != RowDenominatorUnavailable {
		t.Fatalf("result = %#v", res)
	}
	assertTimePtrEqual(t, res.Facts.ConfiguredTimeCutoff, cutoff, "ConfiguredTimeCutoff")
	assertTimePtrEqual(t, res.Facts.ObservedTime, cutoff, "ObservedTime")
	assertObservedPresent(t, res.Facts.ObservedTimePresent, true)
	assertNoCurrentTimeSQL(t, db)
}

func TestFreshSinceJustBelowCutoffFails(t *testing.T) {
	cutoff := temporalAnchor()
	observed := cutoff.Add(-time.Nanosecond)
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "ingested_at": observed},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Timestamp("ingested_at").FreshSince(cutoff)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success {
		t.Fatalf("expected fail, got %#v", res)
	}
	assertTimePtrEqual(t, res.Facts.ObservedTime, observed, "ObservedTime")
	assertObservedPresent(t, res.Facts.ObservedTimePresent, true)
	assertNoCurrentTimeSQL(t, db)
}

func TestFreshSinceJustAboveCutoffPasses(t *testing.T) {
	cutoff := temporalAnchor()
	observed := cutoff.Add(time.Nanosecond)
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "ingested_at": observed},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Timestamp("ingested_at").FreshSince(cutoff)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Results[0].Success {
		t.Fatalf("expected pass, got %#v", rep.Results[0])
	}
	assertTimePtrEqual(t, rep.Results[0].Facts.ObservedTime, observed, "ObservedTime")
	assertNoCurrentTimeSQL(t, db)
}

func TestFreshSinceFutureMaxPasses(t *testing.T) {
	cutoff := temporalAnchor()
	future := cutoff.Add(365 * 24 * time.Hour)
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "ingested_at": future},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Timestamp("ingested_at").FreshSince(cutoff)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success {
		t.Fatalf("future max should pass, got %#v", res)
	}
	assertTimePtrEqual(t, res.Facts.ObservedTime, future, "ObservedTime")
	assertNoCurrentTimeSQL(t, db)
}

func TestFreshSinceEmptyScopeFails(t *testing.T) {
	cutoff := temporalAnchor()
	setHarnessData(t, harnessUsers())
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Timestamp("ingested_at").FreshSince(cutoff)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success {
		t.Fatalf("empty scope should fail, got %#v", res)
	}
	assertTimePtrEqual(t, res.Facts.ConfiguredTimeCutoff, cutoff, "ConfiguredTimeCutoff")
	assertObservedAbsent(t, res.Facts)
	assertNoCurrentTimeSQL(t, db)
}

func TestFreshSinceAllNullFails(t *testing.T) {
	cutoff := temporalAnchor()
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "ingested_at": nil},
		map[string]any{"id": int64(2), "ingested_at": nil},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Timestamp("ingested_at").FreshSince(cutoff)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success {
		t.Fatalf("all-NULL should fail, got %#v", res)
	}
	assertObservedAbsent(t, res.Facts)
	assertNoCurrentTimeSQL(t, db)
}
func TestFreshSinceQueryFailureOmitsObservationFacts(t *testing.T) {
	cutoff := temporalAnchor()
	rep, err := NewSuite(Timestamp("ingested_at").FreshSince(cutoff)).ValidateTable(
		context.Background(), openErrorDB(t), Table("users"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(rep.Results))
	}
	res := rep.Results[0]
	if res.Err == nil {
		t.Fatal("expected query error")
	}
	if !errors.Is(res.Err, ErrCategoryDatabase) {
		t.Fatalf("error category = %v, want database", res.Err)
	}
	assertTimePtrEqual(t, res.Facts.ConfiguredTimeCutoff, cutoff, "ConfiguredTimeCutoff")
	if res.Facts.ObservedTime != nil {
		t.Fatalf("ObservedTime = %v, want nil", res.Facts.ObservedTime)
	}
	if res.Facts.ObservedTimePresent != nil {
		t.Fatalf("ObservedTimePresent = %v, want nil on query error", *res.Facts.ObservedTimePresent)
	}
}

func TestFreshSinceIgnoresNullWhenMaxExists(t *testing.T) {
	cutoff := temporalAnchor()
	observed := cutoff.Add(time.Minute)
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "ingested_at": nil},
		map[string]any{"id": int64(2), "ingested_at": observed},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Timestamp("ingested_at").FreshSince(cutoff)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success {
		t.Fatalf("expected pass ignoring NULL rows, got %#v", res)
	}
	assertTimePtrEqual(t, res.Facts.ObservedTime, observed, "ObservedTime")
	assertNoCurrentTimeSQL(t, db)
}

func TestFreshSinceScopedPopulation(t *testing.T) {
	cutoff := temporalAnchor()
	inScope := cutoff.Add(time.Hour)
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "tenant_id": "acme", "ingested_at": inScope},
		map[string]any{"id": int64(2), "tenant_id": "other", "ingested_at": cutoff.Add(24 * time.Hour)},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Timestamp("ingested_at").FreshSince(cutoff)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("acme", "tenant_id = ?", "acme")),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success {
		t.Fatalf("scoped freshness = %#v", res)
	}
	assertTimePtrEqual(t, res.Facts.ObservedTime, inScope, "ObservedTime")
	assertNoCurrentTimeSQL(t, db)
}

func TestFreshSinceZeroCutoffPreflight(t *testing.T) {
	setHarnessData(t, harnessUsers(map[string]any{"id": int64(1), "ingested_at": temporalAnchor()}))
	db := openHarnessDB(t)

	_, err := NewSuite(Timestamp("ingested_at").FreshSince(time.Time{})).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected preflight error for zero cutoff")
	}
	if !errors.Is(err, ErrCategoryInvalidConfig) {
		t.Fatalf("error = %v, want invalid_config", err)
	}
}

func TestFreshSinceExportFacts(t *testing.T) {
	cutoff := temporalAnchor()
	observed := cutoff.Add(2 * time.Minute)
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "ingested_at": observed},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Timestamp("ingested_at").FreshSince(cutoff)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	facts := dto.Results[0].Facts
	if facts == nil || facts.ConfiguredTimeCutoff == nil || facts.ObservedTime == nil || facts.ObservedTimePresent == nil {
		t.Fatalf("exported facts = %#v", facts)
	}
	if facts.ConfiguredTimeCutoff.Value != cutoff.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("configured_time_cutoff = %#v", facts.ConfiguredTimeCutoff)
	}
	if facts.ObservedTime.Value != observed.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("observed_time = %#v", facts.ObservedTime)
	}
	if !*facts.ObservedTimePresent {
		t.Fatal("observed_time_present = false, want true")
	}
}

func TestInWindowExactStartPasses(t *testing.T) {
	start := temporalAnchor()
	end := start.Add(time.Hour)
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "event_time": start},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Timestamp("event_time").InWindow(start, end)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || res.FailedCount != 0 || res.Kind != KindTimestampInWindow {
		t.Fatalf("result = %#v", res)
	}
	assertTimePtrEqual(t, res.Facts.ConfiguredTimeStart, start, "ConfiguredTimeStart")
	assertTimePtrEqual(t, res.Facts.ConfiguredTimeEnd, end, "ConfiguredTimeEnd")
	assertNoCurrentTimeSQL(t, db)
}

func TestInWindowExactEndFails(t *testing.T) {
	start := temporalAnchor()
	end := start.Add(time.Hour)
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "event_time": end},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Timestamp("event_time").InWindow(start, end)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Success || res.FailedCount != 1 {
		t.Fatalf("exact end should fail half-open window, got %#v", res)
	}
	assertNoCurrentTimeSQL(t, db)
}

func TestInWindowInteriorExterior(t *testing.T) {
	start := temporalAnchor()
	end := start.Add(time.Hour)
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "event_time": start.Add(-time.Nanosecond)},
		map[string]any{"id": int64(2), "event_time": start.Add(time.Minute)},
		map[string]any{"id": int64(3), "event_time": end.Add(-time.Nanosecond)},
		map[string]any{"id": int64(4), "event_time": end},
		map[string]any{"id": int64(5), "event_time": end.Add(time.Nanosecond)},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Timestamp("event_time").InWindow(start, end)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"), WithFailedKeysCap(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Total != 5 || res.FailedCount != 3 || res.Success {
		t.Fatalf("result = %#v", res)
	}
	wantKeys := []RowKey{{int64(1)}, {int64(4)}, {int64(5)}}
	if !rowKeysEqual(res.FailedKeys, wantKeys) {
		t.Fatalf("FailedKeys = %#v, want %#v", res.FailedKeys, wantKeys)
	}
	assertNoCurrentTimeSQL(t, db)
}

func TestInWindowNullFails(t *testing.T) {
	start := temporalAnchor()
	end := start.Add(time.Hour)
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "event_time": nil},
		map[string]any{"id": int64(2), "event_time": start},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Timestamp("event_time").InWindow(start, end)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()), WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.FailedCount != 1 || res.Success {
		t.Fatalf("NULL should fail, got %#v", res)
	}
	if !rowKeysEqual(res.FailedKeys, []RowKey{{int64(1)}}) {
		t.Fatalf("FailedKeys = %#v", res.FailedKeys)
	}
	assertNoCurrentTimeSQL(t, db)
}

func TestInWindowEmptyVacuousPass(t *testing.T) {
	start := temporalAnchor()
	end := start.Add(time.Hour)
	setHarnessData(t, harnessUsers())
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Timestamp("event_time").InWindow(start, end)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if !res.Success || res.Total != 0 || res.FailedCount != 0 {
		t.Fatalf("empty should pass vacuously, got %#v", res)
	}
	assertTimePtrEqual(t, res.Facts.ConfiguredTimeStart, start, "ConfiguredTimeStart")
	assertTimePtrEqual(t, res.Facts.ConfiguredTimeEnd, end, "ConfiguredTimeEnd")
	assertNoCurrentTimeSQL(t, db)
}

func TestInWindowScopedPopulation(t *testing.T) {
	start := temporalAnchor()
	end := start.Add(2 * time.Hour)
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "tenant_id": "acme", "event_time": start.Add(time.Minute)},
		map[string]any{"id": int64(2), "tenant_id": "acme", "event_time": end},
		map[string]any{"id": int64(3), "tenant_id": "other", "event_time": end},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Timestamp("event_time").InWindow(start, end)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()),
		WithScope(TrustedScope("acme", "tenant_id = ?", "acme")),
		WithKey("id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.Total != 2 || res.FailedCount != 1 {
		t.Fatalf("scoped window = %#v", res)
	}
	if !rowKeysEqual(res.FailedKeys, []RowKey{{int64(2)}}) {
		t.Fatalf("FailedKeys = %#v", res.FailedKeys)
	}
	assertNoCurrentTimeSQL(t, db)
}

func TestInWindowPrecisionAndTimezoneBounds(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 123456789, time.FixedZone("UTC-7", -7*3600))
	end := start.Add(time.Hour)
	inside := start.Add(time.Nanosecond)
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "event_time": start},
		map[string]any{"id": int64(2), "event_time": inside},
		map[string]any{"id": int64(3), "event_time": end},
	))
	db := openRecordingHarnessDB(t)

	rep, err := NewSuite(Timestamp("event_time").InWindow(start, end)).ValidateTable(
		context.Background(), db, Table("users"),
		WithDialect(Postgres()), WithKey("id"), WithFailedKeysCap(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	res := rep.Results[0]
	if res.FailedCount != 1 || !rowKeysEqual(res.FailedKeys, []RowKey{{int64(3)}}) {
		t.Fatalf("precision/timezone window = %#v", res)
	}
	assertTimePtrEqual(t, res.Facts.ConfiguredTimeStart, start, "ConfiguredTimeStart")
	assertTimePtrEqual(t, res.Facts.ConfiguredTimeEnd, end, "ConfiguredTimeEnd")
	assertNoCurrentTimeSQL(t, db)
}

func TestInWindowInvalidRangePreflight(t *testing.T) {
	start := temporalAnchor()
	setHarnessData(t, harnessUsers(map[string]any{"id": int64(1), "event_time": start}))
	db := openHarnessDB(t)

	cases := []struct {
		name string
		exp  Expectation
	}{
		{"zero start", Timestamp("event_time").InWindow(time.Time{}, start.Add(time.Hour))},
		{"zero end", Timestamp("event_time").InWindow(start, time.Time{})},
		{"end equal start", Timestamp("event_time").InWindow(start, start)},
		{"end before start", Timestamp("event_time").InWindow(start, start.Add(-time.Second))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSuite(tc.exp).ValidateTable(
				context.Background(), db, Table("users"), WithDialect(Postgres()),
			)
			if err == nil {
				t.Fatal("expected preflight error")
			}
			if !errors.Is(err, ErrCategoryInvalidConfig) {
				t.Fatalf("error = %v, want invalid_config", err)
			}
		})
	}
}

func TestInWindowExportFacts(t *testing.T) {
	start := temporalAnchor()
	end := start.Add(time.Hour)
	setHarnessData(t, harnessUsers(
		map[string]any{"id": int64(1), "event_time": start},
	))
	db := openHarnessDB(t)

	rep, err := NewSuite(Timestamp("event_time").InWindow(start, end)).ValidateTable(
		context.Background(), db, Table("users"), WithDialect(Postgres()),
	)
	if err != nil {
		t.Fatal(err)
	}
	dto, err := ExportReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	facts := dto.Results[0].Facts
	if facts == nil || facts.ConfiguredTimeStart == nil || facts.ConfiguredTimeEnd == nil {
		t.Fatalf("exported facts = %#v", facts)
	}
	if facts.ConfiguredTimeCutoff != nil || facts.ObservedTime != nil || facts.ObservedTimePresent != nil {
		t.Fatalf("window export leaked freshness fields: %#v", facts)
	}
	if facts.ConfiguredTimeStart.Value != start.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("configured_time_start = %#v", facts.ConfiguredTimeStart)
	}
	if facts.ConfiguredTimeEnd.Value != end.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("configured_time_end = %#v", facts.ConfiguredTimeEnd)
	}
}

func rowKeysEqual(got, want []RowKey) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if len(got[i]) != len(want[i]) {
			return false
		}
		for j := range got[i] {
			if got[i][j] != want[i][j] {
				return false
			}
		}
	}
	return true
}
