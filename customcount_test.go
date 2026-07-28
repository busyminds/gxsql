package gxsql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCustomCountResultSemantics(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		failed int
		wantOK bool
	}{
		{name: "zero", failed: 0, wantOK: true},
		{name: "nonzero", failed: 2, wantOK: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := customCountResult("violating orders", tc.failed)
			if err != nil {
				t.Fatalf("customCountResult() error = %v", err)
			}
			if res.Kind != KindCustom {
				t.Fatalf("Kind = %q, want %q", res.Kind, KindCustom)
			}
			if res.Column != "" {
				t.Fatalf("Column = %q, want blank", res.Column)
			}
			if res.RowDenominator != RowDenominatorUnavailable {
				t.Fatalf("RowDenominator = %q, want unavailable", res.RowDenominator)
			}
			if res.FailedCount != tc.failed {
				t.Fatalf("FailedCount = %d, want %d", res.FailedCount, tc.failed)
			}
			if res.Success != tc.wantOK {
				t.Fatalf("Success = %v, want %v", res.Success, tc.wantOK)
			}
		})
	}
}

func TestCustomCountResultContractRejectsInvalid(t *testing.T) {
	t.Parallel()

	valid, err := customCountResult("violating orders", 0)
	if err != nil {
		t.Fatalf("customCountResult() error = %v", err)
	}

	cases := []struct {
		name string
		res  Result
		want error
	}{
		{
			name: "kind",
			res: func() Result {
				r := valid
				r.Kind = KindNotNull
				return r
			}(),
			want: errCustomCountResultKind,
		},
		{
			name: "column",
			res: func() Result {
				r := valid
				r.Column = "id"
				return r
			}(),
			want: errCustomCountResultColumn,
		},
		{
			name: "denominator",
			res: func() Result {
				r := valid
				r.RowDenominator = RowDenominatorAvailable
				return r
			}(),
			want: errCustomCountResultDenominator,
		},
		{
			name: "total",
			res: func() Result {
				r := valid
				r.Total = 1
				return r
			}(),
			want: errCustomCountResultTotal,
		},
		{
			name: "failed_percent",
			res: func() Result {
				r := valid
				r.FailedPercent = 1
				return r
			}(),
			want: errCustomCountResultFailedPercent,
		},
		{
			name: "samples",
			res: func() Result {
				r := valid
				r.SampleValues = []any{"x"}
				return r
			}(),
			want: errCustomCountResultSamples,
		},
		{
			name: "failed_keys",
			res: func() Result {
				r := valid
				r.FailedKeys = []RowKey{{1}}
				return r
			}(),
			want: errCustomCountResultFailedKeys,
		},
		{
			name: "negative_failed",
			res: func() Result {
				r := valid
				r.FailedCount = -1
				return r
			}(),
			want: errCustomCountResultFailedNegative,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := validateCustomCountResult(tc.res)
			if !errors.Is(err, tc.want) {
				t.Fatalf("validateCustomCountResult() = %v, want %v", err, tc.want)
			}
			if !errors.Is(err, ErrCategoryInvalidConfig) {
				t.Fatalf("category = %v, want invalid_config", err)
			}
		})
	}
}

func TestCustomCountImmutableArgs(t *testing.T) {
	t.Parallel()

	original := []any{[]byte("secret"), 42}
	exp := newCustomCountExpectation(
		"violating orders",
		"SELECT COUNT(*) FROM {{target}} WHERE {{scope}} AND status = ?",
		original,
	)

	original[0] = []byte("mutated")
	original[1] = 99

	stored := exp.boundArgs()
	if got, want := stored[1], 42; got != want {
		t.Fatalf("stored int arg = %v, want %v", got, want)
	}
	origBytes, ok := stored[0].([]byte)
	if !ok {
		t.Fatalf("stored arg type = %T", stored[0])
	}
	if string(origBytes) != "secret" {
		t.Fatalf("stored []byte = %q, want %q", origBytes, "secret")
	}

	origBytes[0] = 'X'
	again := exp.boundArgs()
	againBytes := again[0].([]byte)
	if string(againBytes) != "secret" {
		t.Fatalf("internal args mutated through returned slice: %q", againBytes)
	}
}

func TestCustomCountDisplayFailed(t *testing.T) {
	t.Parallel()

	res, err := customCountResult("violating orders", 2)
	if err != nil {
		t.Fatalf("customCountResult() error = %v", err)
	}
	if got, ok := customCountDisplayFailed(res); !ok || got != 2 {
		t.Fatalf("customCountDisplayFailed() = (%d, %v), want (2, true)", got, ok)
	}

	probe := Result{
		Kind:           KindCustom,
		Name:           "special floats",
		Success:        true,
		RowDenominator: RowDenominatorUnavailable,
		SampleValues:   []any{"x"},
	}
	if got, ok := customCountDisplayFailed(probe); ok || got != 0 {
		t.Fatalf("customCountDisplayFailed(probe) = (%d, %v), want (0, false)", got, ok)
	}
}

func TestCustomCountAsExpectationHook(t *testing.T) {
	t.Parallel()

	exp := asCustomCountExpectation(
		"violating orders",
		"SELECT COUNT(*) FROM {{target}} WHERE {{scope}} AND status = ?",
		"pending",
	)
	if exp.Name() != "violating orders" {
		t.Fatalf("Name() = %q", exp.Name())
	}
}

func TestGenericKindCustomExportUnchanged(t *testing.T) {
	t.Parallel()

	res := Result{
		Kind:           KindCustom,
		Name:           "x",
		Success:        true,
		SampleValues:   []any{"probe"},
		RowDenominator: RowDenominatorUnavailable,
		// Uncomparable dynamic field must not panic during profile/export.
		Facts: ResultFacts{ConfiguredBound: map[string]int{"n": 1}},
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ExportReport() panicked on generic KindCustom: %v", r)
			}
		}()
		dto, err := ExportReport(Report{Results: []Result{res}})
		if err != nil {
			t.Fatalf("ExportReport() error = %v", err)
		}
		if dto.Results[0].Counts != nil {
			t.Fatalf("generic KindCustom unexpectedly exported custom-count counts: %#v", dto.Results[0].Counts)
		}
	}()
}

func TestCustomCountExportUncomparableFacts(t *testing.T) {
	t.Parallel()

	res, err := customCountResult("violating orders", 2)
	if err != nil {
		t.Fatalf("customCountResult() error = %v", err)
	}
	res.Facts = ResultFacts{ConfiguredBound: []string{"probe"}}

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ExportReport() panicked: %v", r)
			}
		}()
		dto, err := ExportReport(Report{Results: []Result{res}})
		if err != nil {
			t.Fatalf("ExportReport() error = %v", err)
		}
		counts := dto.Results[0].Counts
		if counts == nil || counts.Failed == nil || *counts.Failed != 2 {
			t.Fatalf("counts.failed = %#v, want explicit 2", counts)
		}
	}()
}

func TestCustomCountStringCompact(t *testing.T) {
	t.Parallel()

	res, err := customCountResult("violating orders", 2)
	if err != nil {
		t.Fatalf("customCountResult() error = %v", err)
	}
	got := res.String()
	want := "✗ violating orders  2 failed"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestCustomCountExecuteSuccess(t *testing.T) {
	setHarnessData(t, map[string][]map[string]any{
		"orders": {
			{"id": int64(1), "status": "pending"},
			{"id": int64(2), "status": "active"},
			{"id": int64(3), "status": "pending"},
		},
	})
	exp := newCustomCountExpectation(
		"violating orders",
		"SELECT COUNT(*) FROM {{target}} WHERE {{scope}} AND status = ?",
		[]any{"pending"},
	)

	res, err := exp.evaluateSQL(
		context.Background(), openHarnessDB(t), Table("orders"),
		evalOptions{dialect: Postgres()},
	)
	if err != nil {
		t.Fatalf("evaluateSQL() error = %v", err)
	}
	if res.FailedCount != 2 || res.Success {
		t.Fatalf("result = %#v, want failed=2 and Success=false", res)
	}
	if res.Total != 0 || len(res.SampleValues) > 0 || len(res.FailedKeys) > 0 {
		t.Fatalf("unexpected diagnostics on custom count: %#v", res)
	}
}

func TestCustomCountExecuteZero(t *testing.T) {
	setHarnessData(t, map[string][]map[string]any{
		"orders": {
			{"id": int64(1), "status": "active"},
		},
	})
	exp := newCustomCountExpectation(
		"violating orders",
		"SELECT COUNT(*) FROM {{target}} WHERE {{scope}} AND status = ?",
		[]any{"pending"},
	)

	res, err := exp.evaluateSQL(
		context.Background(), openHarnessDB(t), Table("orders"),
		evalOptions{dialect: Postgres()},
	)
	if err != nil {
		t.Fatalf("evaluateSQL() error = %v", err)
	}
	if res.FailedCount != 0 || !res.Success {
		t.Fatalf("result = %#v, want failed=0 and Success=true", res)
	}
}

func TestCustomCountPreflight(t *testing.T) {
	counter := openCountingHarnessDB(t)
	_, err := NewSuite(newCustomCountExpectation(
		"violating orders",
		"SELECT COUNT(*) FROM {{target}} WHERE {{target}} AND {{scope}}",
		nil,
	)).ValidateTable(
		context.Background(), counter, Table("orders"), WithDialect(Postgres()),
	)
	if err == nil {
		t.Fatal("expected preflight error")
	}
	if counter.queries != 0 {
		t.Fatalf("queries = %d, want 0 before preflight failure", counter.queries)
	}
}

func TestCustomCountContinueOnError(t *testing.T) {
	setHarnessData(t, harnessUsers(map[string]any{"id": int64(1), "age": int64(25)}))
	counter := openCountingHarnessDB(t)

	rep, err := NewSuite(
		newCustomCountExpectation(
			"violating orders",
			"SELECT COUNT(*) FROM {{target}} WHERE {{target}} AND {{scope}}",
			nil,
		),
		WithID("good", Int("age").Between(0, 120)),
	).ValidateTable(
		context.Background(), counter, Table("users"),
		WithDialect(Postgres()), ContinueOnError(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results len = %d, want 2", len(rep.Results))
	}
	if rep.Results[0].Err == nil || rep.Results[1].Err != nil || !rep.Results[1].Success {
		t.Fatalf("results = %#v", rep.Results)
	}
	if counter.queries != 2 {
		t.Fatalf("queries = %d, want 2 for valid later declaration", counter.queries)
	}
}

func TestCustomCountScanError(t *testing.T) {
	cases := []struct {
		name string
		db   *sql.DB
		want error
	}{
		{name: "multiple_rows", db: openCustomCountRowsDB(t, [][]driver.Value{{int64(1)}, {int64(2)}}), want: errCustomCountMultipleRows},
		{name: "negative", db: openCustomCountRowsDB(t, [][]driver.Value{{int64(-1)}}), want: errCustomCountNegative},
		{name: "two_columns", db: openCustomCountColsDB(t, []string{"a", "b"}), want: errCustomCountWrongColumnCount},
	}

	exp := newCustomCountExpectation(
		"violating orders",
		"SELECT COUNT(*) FROM {{target}} WHERE {{scope}}",
		nil,
	)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := exp.evaluateSQL(
				context.Background(), tc.db, Table("orders"),
				evalOptions{dialect: Postgres()},
			)
			if err == nil {
				t.Fatal("expected scan error")
			}
			if !errors.Is(err, ErrCategoryScan) {
				t.Fatalf("category = %v", err)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), "SELECT") {
				t.Fatalf("leaked query text: %v", err)
			}
		})
	}
}

func TestCustomCountDriverContextError(t *testing.T) {
	exp := newCustomCountExpectation(
		"violating orders",
		"SELECT COUNT(*) FROM {{target}} WHERE {{scope}}",
		nil,
	)
	_, err := exp.evaluateSQL(
		context.Background(),
		openCustomCountNextErrorDB(t, context.Canceled),
		Table("orders"),
		evalOptions{dialect: Postgres()},
	)
	if err == nil {
		t.Fatal("expected context error")
	}
	if !errors.Is(err, ErrCategoryContext) {
		t.Fatalf("category = %v, want context", err)
	}
	if errors.Is(err, ErrCategoryScan) {
		t.Fatalf("category = %v, want no scan category", err)
	}
	if !errors.Is(err, errCustomCountContextFailed) {
		t.Fatalf("error = %v, want privacy-safe context sentinel", err)
	}
	if strings.Contains(err.Error(), "SELECT") {
		t.Fatalf("leaked query text: %v", err)
	}
}

func TestCustomCountPrivacy(t *testing.T) {
	t.Parallel()

	setHarnessData(t, map[string][]map[string]any{
		"orders": {{"id": int64(1), "status": "pending"}},
	})
	exp := newCustomCountExpectation(
		"violating orders",
		"SELECT COUNT(*) FROM {{target}} WHERE {{scope}} AND status = ?",
		[]any{"secret"},
	)
	_, err := exp.evaluateSQL(
		context.Background(), openErrorDB(t), Table("orders"),
		evalOptions{dialect: Postgres()},
	)
	if err == nil {
		t.Fatal("expected execution error")
	}
	if !errors.Is(err, ErrCategoryDatabase) {
		t.Fatalf("category = %v", err)
	}
	msg := err.Error()
	for _, forbidden := range []string{"SELECT", "secret", "{{target}}", "{{scope}}"} {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(forbidden)) {
			t.Fatalf("error leaked %q: %s", forbidden, msg)
		}
	}

	res, err := customCountResult(exp.Name(), 0)
	if err != nil {
		t.Fatalf("customCountResult() error = %v", err)
	}
	dto, err := ExportReport(Report{Results: []Result{res}})
	if err != nil {
		t.Fatalf("ExportReport() error = %v", err)
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	payload := string(raw)
	if strings.Contains(payload, "SELECT") || strings.Contains(payload, "secret") {
		t.Fatalf("export leaked custom-count query or args: %s", payload)
	}
}

func TestCustomCountExportZeroFailed(t *testing.T) {
	t.Parallel()

	res, err := customCountResult("violating orders", 0)
	if err != nil {
		t.Fatalf("customCountResult() error = %v", err)
	}

	dto, err := ExportReport(Report{Results: []Result{res}})
	if err != nil {
		t.Fatalf("ExportReport() error = %v", err)
	}
	counts := dto.Results[0].Counts
	if counts == nil || counts.Failed == nil || *counts.Failed != 0 {
		t.Fatalf("counts.failed = %#v, want explicit zero", counts)
	}
}

var customCountDriverSeq atomic.Uint64

const customCountRowsDriverName = "gxsqlcustomcountrows"

type customCountRowsDriver struct {
	rows    [][]driver.Value
	columns []string
	nextErr error
}

func (d customCountRowsDriver) Open(string) (driver.Conn, error) {
	return customCountRowsConn(d), nil
}

type customCountRowsConn struct {
	rows    [][]driver.Value
	columns []string
	nextErr error
}

func (customCountRowsConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (customCountRowsConn) Close() error                        { return nil }
func (customCountRowsConn) Begin() (driver.Tx, error) {
	return nil, errors.New("gxsqltest: transactions not supported")
}

func (c customCountRowsConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	cols := c.columns
	if len(cols) == 0 {
		cols = []string{"count"}
	}
	return &customCountRowsRows{
		columns: cols,
		rows:    c.rows,
		nextErr: c.nextErr,
	}, nil
}

type customCountRowsRows struct {
	columns []string
	rows    [][]driver.Value
	idx     int
	nextErr error
}

func (r *customCountRowsRows) Columns() []string { return r.columns }
func (r *customCountRowsRows) Close() error      { return nil }
func (r *customCountRowsRows) Next(dest []driver.Value) error {
	if r.nextErr != nil {
		err := r.nextErr
		r.nextErr = nil
		return err
	}
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.idx])
	r.idx++
	return nil
}

func openCustomCountRowsDB(t *testing.T, rows [][]driver.Value) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("%s%d", customCountRowsDriverName, customCountDriverSeq.Add(1))
	sql.Register(name, customCountRowsDriver{rows: rows})
	db, err := sql.Open(name, "test")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openCustomCountColsDB(t *testing.T, columns []string) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("%s%d", customCountRowsDriverName, customCountDriverSeq.Add(1))
	sql.Register(name, customCountRowsDriver{
		rows:    [][]driver.Value{{int64(1), int64(2)}},
		columns: columns,
	})
	db, err := sql.Open(name, "test")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func openCustomCountNextErrorDB(t *testing.T, err error) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("%s%d", customCountRowsDriverName, customCountDriverSeq.Add(1))
	sql.Register(name, customCountRowsDriver{nextErr: err})
	db, openErr := sql.Open(name, "test")
	if openErr != nil {
		t.Fatalf("sql.Open: %v", openErr)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
