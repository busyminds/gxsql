package conformance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/busyminds/gxsql"
)

func temporalWindowBounds() (start, end time.Time) {
	start = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end = start.Add(24 * time.Hour)
	return start, end
}

func temporalCutoff() time.Time {
	return time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
}

func temporalRowsTable(cfg Config) gxsql.TableRef {
	return gxsql.TableRef{Schema: cfg.Table.Schema, Name: "temporal_rows"}
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

func assertTimeFact(t *testing.T, got *time.Time, want time.Time, label string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %s", label, want.UTC().Format(time.RFC3339Nano))
	}
	if !got.Equal(want) {
		t.Fatalf("%s = %s, want %s", label, got.UTC().Format(time.RFC3339Nano), want.UTC().Format(time.RFC3339Nano))
	}
}

func runTemporalAndFreshness(t *testing.T, cfg Config) {
	t.Helper()
	start, end := temporalWindowBounds()
	cutoff := temporalCutoff()
	table := temporalRowsTable(cfg)

	t.Run("temporal/window exact start interior end null empty scoped", func(t *testing.T) {
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.Timestamp("event_at").InWindow(start, end),
		).ValidateTable(
			context.Background(), db, table,
			gxsql.WithDialect(cfg.Dialect),
			gxsql.WithScope(gxsql.TrustedScope("tenant-a", "tenant_id = ?", "tenant-a")),
			gxsql.WithKey("id"),
			gxsql.WithFailedKeysCap(0),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := report.Results[0]
		if res.Kind != gxsql.KindTimestampInWindow {
			t.Fatalf("kind = %q, want %q", res.Kind, gxsql.KindTimestampInWindow)
		}
		assertTimeFact(t, res.Facts.ConfiguredTimeStart, start, "ConfiguredTimeStart")
		assertTimeFact(t, res.Facts.ConfiguredTimeEnd, end, "ConfiguredTimeEnd")
		// Core fixture rows 1..6 under tenant-a: start, interior, exact end, below, above, NULL.
		if res.Total != 6 {
			t.Fatalf("total = %d, want 6", res.Total)
		}
		if res.FailedCount != 4 {
			t.Fatalf("failed = %d, want 4 (exact end, below, above, NULL)", res.FailedCount)
		}
		wantKeys := []gxsql.RowKey{{int64(3)}, {int64(4)}, {int64(5)}, {int64(6)}}
		if !rowKeysEqual(res.FailedKeys, wantKeys) {
			t.Fatalf("FailedKeys = %#v, want %#v", res.FailedKeys, wantKeys)
		}
		assertNoCurrentTimeSQL(t, db)
	})

	t.Run("temporal/window empty population vacuous pass", func(t *testing.T) {
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.Timestamp("event_at").InWindow(start, end),
		).ValidateTable(
			context.Background(), db, table,
			gxsql.WithDialect(cfg.Dialect),
			gxsql.WithScope(gxsql.TrustedScope("empty-temporal", "tenant_id = ?", "nobody")),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := report.Results[0]
		if !res.Success || res.Total != 0 || res.FailedCount != 0 {
			t.Fatalf("empty window = %#v, want vacuous pass", res)
		}
		assertNoCurrentTimeSQL(t, db)
	})

	t.Run("temporal/window scoped population", func(t *testing.T) {
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.Timestamp("event_at").InWindow(start, end),
		).ValidateTable(
			context.Background(), db, table,
			gxsql.WithDialect(cfg.Dialect),
			gxsql.WithScope(gxsql.TrustedScope("tenant-b", "tenant_id = ?", "tenant-b")),
			gxsql.WithKey("id"),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := report.Results[0]
		// tenant-b has id=7 interior pass and id=8 exact-end fail.
		if res.Total != 2 || res.FailedCount != 1 {
			t.Fatalf("scoped window = %#v", res)
		}
		if !rowKeysEqual(res.FailedKeys, []gxsql.RowKey{{int64(8)}}) {
			t.Fatalf("FailedKeys = %#v", res.FailedKeys)
		}
		assertNoCurrentTimeSQL(t, db)
	})

	t.Run("temporal/freshness cutoff boundaries null empty future", func(t *testing.T) {
		db := &recordingDB{DB: cfg.DB}
		report, err := gxsql.NewSuite(
			gxsql.Timestamp("ingested_at").FreshSince(cutoff),
		).ValidateTable(
			context.Background(), db, table,
			gxsql.WithDialect(cfg.Dialect),
			gxsql.WithScope(gxsql.TrustedScope("tenant-a", "tenant_id = ?", "tenant-a")),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		res := report.Results[0]
		if res.Kind != gxsql.KindTimestampFreshSince {
			t.Fatalf("kind = %q, want %q", res.Kind, gxsql.KindTimestampFreshSince)
		}
		assertTimeFact(t, res.Facts.ConfiguredTimeCutoff, cutoff, "ConfiguredTimeCutoff")
		// tenant-a max is future id=2 value 2026-07-02T00:00:00Z.
		wantObserved := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
		assertTimeFact(t, res.Facts.ObservedTime, wantObserved, "ObservedTime")
		if res.Facts.ObservedTimePresent == nil || !*res.Facts.ObservedTimePresent || !res.Success {
			t.Fatalf("freshness = %#v", res)
		}
		assertNoCurrentTimeSQL(t, db)
	})

	t.Run("temporal/freshness exact cutoff and just below", func(t *testing.T) {
		exact := cutoff
		report, err := gxsql.NewSuite(gxsql.Timestamp("ingested_at").FreshSince(exact)).ValidateTable(
			context.Background(), cfg.DB, table,
			gxsql.WithDialect(cfg.Dialect),
			gxsql.WithScope(gxsql.TrustedScope("exact-cutoff", "id = ?", int64(1))),
		)
		if err != nil {
			t.Fatalf("exact ValidateTable: %v", err)
		}
		if !report.Results[0].Success {
			t.Fatalf("exact cutoff should pass, got %#v", report.Results[0])
		}
		assertTimeFact(t, report.Results[0].Facts.ObservedTime, exact, "ObservedTime")

		// MySQL DATETIME(6) is the least precise integration timestamp type.
		belowCutoff := cutoff.Add(time.Microsecond)
		report, err = gxsql.NewSuite(gxsql.Timestamp("ingested_at").FreshSince(belowCutoff)).ValidateTable(
			context.Background(), cfg.DB, table,
			gxsql.WithDialect(cfg.Dialect),
			gxsql.WithScope(gxsql.TrustedScope("just-below", "id = ?", int64(1))),
		)
		if err != nil {
			t.Fatalf("below ValidateTable: %v", err)
		}
		if report.Results[0].Success {
			t.Fatalf("just below should fail, got %#v", report.Results[0])
		}
	})

	t.Run("temporal/freshness empty and all-null fail with absence facts", func(t *testing.T) {
		report, err := gxsql.NewSuite(gxsql.Timestamp("ingested_at").FreshSince(cutoff)).ValidateTable(
			context.Background(), cfg.DB, table,
			gxsql.WithDialect(cfg.Dialect),
			gxsql.WithScope(gxsql.TrustedScope("empty-fresh", "tenant_id = ?", "nobody")),
		)
		if err != nil {
			t.Fatalf("empty ValidateTable: %v", err)
		}
		res := report.Results[0]
		if res.Success || res.Facts.ObservedTime != nil || res.Facts.ObservedTimePresent == nil || *res.Facts.ObservedTimePresent {
			t.Fatalf("empty freshness = %#v", res)
		}
		assertTimeFact(t, res.Facts.ConfiguredTimeCutoff, cutoff, "ConfiguredTimeCutoff")

		report, err = gxsql.NewSuite(gxsql.Timestamp("ingested_at").FreshSince(cutoff)).ValidateTable(
			context.Background(), cfg.DB, table,
			gxsql.WithDialect(cfg.Dialect),
			gxsql.WithScope(gxsql.TrustedScope("all-null", "id = ?", int64(6))),
		)
		if err != nil {
			t.Fatalf("all-null ValidateTable: %v", err)
		}
		res = report.Results[0]
		if res.Success || res.Facts.ObservedTime != nil || res.Facts.ObservedTimePresent == nil || *res.Facts.ObservedTimePresent {
			t.Fatalf("all-null freshness = %#v", res)
		}
	})

	t.Run("temporal/precision fractional interior and timezone instant", func(t *testing.T) {
		// Row 9 is a fractional-second interior instant (and an offset-equal
		// instant on engines that preserve timestamptz). It must remain inside
		// the half-open window.
		report, err := gxsql.NewSuite(
			gxsql.Timestamp("event_at").InWindow(start, end),
		).ValidateTable(
			context.Background(), cfg.DB, table,
			gxsql.WithDialect(cfg.Dialect),
			gxsql.WithScope(gxsql.TrustedScope("precision", "id = ?", int64(9))),
			gxsql.WithKey("id"),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		if !report.Results[0].Success || report.Results[0].FailedCount != 0 {
			t.Fatalf("precision row should pass inside window, got %#v", report.Results[0])
		}
	})

	t.Run("temporal/structured export facts", func(t *testing.T) {
		report, err := gxsql.NewSuite(
			gxsql.Timestamp("event_at").InWindow(start, end),
			gxsql.Timestamp("ingested_at").FreshSince(cutoff),
		).ValidateTable(
			context.Background(), cfg.DB, table,
			gxsql.WithDialect(cfg.Dialect),
			gxsql.WithScope(gxsql.TrustedScope("tenant-a", "tenant_id = ?", "tenant-a")),
		)
		if err != nil {
			t.Fatalf("ValidateTable: %v", err)
		}
		dto, err := gxsql.ExportReport(report)
		if err != nil {
			t.Fatalf("ExportReport: %v", err)
		}
		data, err := json.Marshal(dto)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		text := string(data)
		for _, want := range []string{
			`"configured_time_start"`,
			`"configured_time_end"`,
			`"configured_time_cutoff"`,
			`"observed_time"`,
			`"observed_time_present":true`,
			`"kind":"timestamp_in_window"`,
			`"kind":"timestamp_fresh_since"`,
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("export JSON missing %s in %s", want, text)
			}
		}
		if strings.Contains(text, `"samples"`) || strings.Contains(text, `"diagnostics"`) {
			t.Fatalf("default export leaked diagnostics: %s", text)
		}
	})
}

func rowKeysEqual(got, want []gxsql.RowKey) bool {
	if len(got) != len(want) {
		return false
	}
	for _, gotKey := range got {
		wantKey := want[0]
		want = want[1:]
		if len(gotKey) != len(wantKey) {
			return false
		}
		for _, gotValue := range gotKey {
			wantValue := wantKey[0]
			wantKey = wantKey[1:]
			if !equalScopeValue(gotValue, wantValue) {
				return false
			}
		}
	}
	return true
}
