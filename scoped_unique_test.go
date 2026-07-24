package gxsql

import (
	"strings"
	"testing"
)

func TestUniqueScopedOutOfScopeDuplicatesIgnored(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "email": "dup"},
		map[string]any{"id": int64(2), "email": nil},
		map[string]any{"id": int64(3), "tenant_id": "t2", "email": "dup"},
		map[string]any{"id": int64(4), "tenant_id": "t2", "email": "dup"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	res := evalPerRowWithScope(t, db, Column("email").Unique(), scope)
	if !res.Success {
		t.Fatalf("scoped unique failed: %+v", res)
	}
	if res.Total != 2 || res.FailedCount != 0 {
		t.Fatalf("Total=%d FailedCount=%d, want 2 and 0", res.Total, res.FailedCount)
	}
	if len(db.queries) != 2 {
		t.Fatalf("queries=%d, want total and failure counts", len(db.queries))
	}
	if got := db.queries[0].args; len(got) != 1 || got[0] != "t1" {
		t.Fatalf("total args=%v, want [t1]", got)
	}
	if got := db.queries[1].args; len(got) != 2 || got[0] != "t1" || got[1] != "t1" {
		t.Fatalf("failure args=%v, want [t1 t1]", got)
	}
}
func TestUniqueScopedCompositeScopeOffsetsInnerPlaceholders(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "region": "eu", "email": "dup"},
		map[string]any{"id": int64(2), "tenant_id": "t2", "region": "us", "email": "dup"},
		map[string]any{"id": int64(3), "tenant_id": "t2", "region": "eu", "email": "dup"},
		map[string]any{"id": int64(4), "region": "ap", "email": "unique"},
	))
	scope := mustTestScope(t, "tenant_id = ? OR region = ?", "t1", "us")
	db := openRecordingHarnessDB(t)

	res := evalPerRowWithScope(t, db, Column("email").Unique(), scope, func(opts *evalOptions) {
		opts.sampleCap = 0
	})
	if res.Success || res.Total != 3 || res.FailedCount != 2 {
		t.Fatalf("got %+v, want scoped total 3 and failed count 2", res)
	}
	if len(db.queries) != 2 {
		t.Fatalf("queries=%d, want total and failure counts", len(db.queries))
	}

	total, failure := db.queries[0], db.queries[1]
	if len(total.args) != 2 || total.args[0] != "t1" || total.args[1] != "us" {
		t.Fatalf("total args=%v, want [t1 us]", total.args)
	}
	if len(failure.args) != 4 || failure.args[0] != "t1" || failure.args[1] != "us" || failure.args[2] != "t1" || failure.args[3] != "us" {
		t.Fatalf("failure args=%v, want [t1 us t1 us]", failure.args)
	}
	if !strings.Contains(failure.text, `(tenant_id = $1 OR region = $2) AND (`) {
		t.Fatalf("failure query lacks outer composite scope: %q", failure.text)
	}
	if !strings.Contains(failure.text, `WHERE (tenant_id = $3 OR region = $4)`) {
		t.Fatalf("failure query lacks offset inner composite scope: %q", failure.text)
	}
}

func TestUniqueScopedExecutesOnBuiltInDialects(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "email": "dup"},
		map[string]any{"id": int64(2), "email": "dup"},
		map[string]any{"id": int64(3), "tenant_id": "t2", "email": "dup"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")

	tests := []struct {
		name    string
		dialect Dialect
	}{
		{name: "postgres", dialect: Postgres()},
		{name: "sqlite", dialect: SQLite()},
		{name: "duckdb", dialect: DuckDB()},
		{name: "mysql", dialect: MySQL()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := openRecordingHarnessDB(t)
			res := evalPerRowWithScope(t, db, Column("email").Unique(), scope, func(opts *evalOptions) {
				opts.dialect = tc.dialect
				opts.sampleCap = 0
			})
			if res.Success || res.Total != 2 || res.FailedCount != 2 {
				t.Fatalf("result = %#v, want scoped duplicate failure for two rows", res)
			}
			if len(db.queries) != 2 {
				t.Fatalf("queries = %d, want total and failure counts", len(db.queries))
			}
			if len(db.queries[0].args) != 1 || db.queries[0].args[0] != "t1" {
				t.Fatalf("total args = %#v, want [t1]", db.queries[0].args)
			}
			if len(db.queries[1].args) != 2 || db.queries[1].args[0] != "t1" || db.queries[1].args[1] != "t1" {
				t.Fatalf("failure args = %#v, want [t1 t1]", db.queries[1].args)
			}
		})
	}
}

func TestUniqueScopedDuplicatesAndDiagnosticsUseScope(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "email": "dup"},
		map[string]any{"id": int64(2), "email": "dup"},
		map[string]any{"id": int64(3), "email": nil},
		map[string]any{"id": int64(4), "tenant_id": "t2", "email": "dup"},
		map[string]any{"id": int64(5), "tenant_id": "t2", "email": "dup"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "t1")
	db := openRecordingHarnessDB(t)

	res := evalPerRowWithScope(t, db, Column("email").Unique(), scope, func(o *evalOptions) {
		o.keyColumns = []string{"id"}
	})
	if res.Success || res.Total != 3 || res.FailedCount != 2 {
		t.Fatalf("got %+v, want scoped total 3 and failed count 2", res)
	}
	if len(res.SampleValues) != 2 || !valuesEqual(res.SampleValues[0], "dup") || !valuesEqual(res.SampleValues[1], "dup") {
		t.Fatalf("SampleValues=%v, want two in-scope duplicate values", res.SampleValues)
	}
	wantKeys := []RowKey{{int64(1)}, {int64(2)}}
	if len(res.FailedKeys) != len(wantKeys) || !valuesEqual(res.FailedKeys[0][0], wantKeys[0][0]) || !valuesEqual(res.FailedKeys[1][0], wantKeys[1][0]) {
		t.Fatalf("FailedKeys=%v, want %v", res.FailedKeys, wantKeys)
	}
	if len(db.queries) != 4 {
		t.Fatalf("queries=%d, want count, failure, samples, keys", len(db.queries))
	}

	total, failure, samples, keys := db.queries[0], db.queries[1], db.queries[2], db.queries[3]
	for name, q := range map[string]recordedQuery{"total": total, "failure": failure, "samples": samples, "keys": keys} {
		if len(q.args) < 1 || q.args[0] != "t1" {
			t.Fatalf("%s args=%v, want scope prefix", name, q.args)
		}
	}
	if len(failure.args) != 2 || failure.args[1] != "t1" {
		t.Fatalf("failure args=%v, want outer and inner scope values", failure.args)
	}
	if !strings.Contains(failure.text, `(tenant_id = $1) AND (`) {
		t.Fatalf("failure query lacks independent outer scope composition: %q", failure.text)
	}
	if !strings.Contains(failure.text, `WHERE (tenant_id = $2)`) {
		t.Fatalf("failure query lacks offset inner scope: %q", failure.text)
	}
	if !strings.Contains(samples.text, `(tenant_id = $1) AND (`) || !strings.Contains(keys.text, `(tenant_id = $1) AND (`) {
		t.Fatalf("diagnostic queries lack scoped failure predicate: samples=%q keys=%q", samples.text, keys.text)
	}
}

func TestUniqueScopedEmptyPopulationPasses(t *testing.T) {
	setHarnessData(t, scopedHarnessUsers("tenant_id", "t1",
		map[string]any{"id": int64(1), "email": nil},
		map[string]any{"id": int64(2), "tenant_id": "t2", "email": "dup"},
		map[string]any{"id": int64(3), "tenant_id": "t2", "email": "dup"},
	))
	scope := mustTestScope(t, "tenant_id = ?", "nobody")
	db := openRecordingHarnessDB(t)

	res := evalPerRowWithScope(t, db, Column("email").Unique(), scope)
	if !res.Success || res.Total != 0 || res.FailedCount != 0 {
		t.Fatalf("got %+v, want empty scoped population and no failures", res)
	}
	if len(db.queries) != 2 {
		t.Fatalf("queries=%d, want total and failure counts", len(db.queries))
	}
	if len(db.queries[1].args) != 2 || db.queries[1].args[0] != "nobody" || db.queries[1].args[1] != "nobody" {
		t.Fatalf("failure args=%v, want [nobody nobody]", db.queries[1].args)
	}
}
