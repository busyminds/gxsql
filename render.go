package gxsql

import (
	"fmt"
	"strings"
)

const maxDisplay = 10

// String renders one [Result] as a single human-readable line prefixed with ✓
// or ✗. Successful per-row results include Total only when Total is greater
// than zero. Failing per-row results include FailedCount, FailedPercent, sample
// values, and failed keys. Table-level results omit row denominators; failing lines show Name
// only when FailedCount is zero. Sample values and failed keys are truncated to
// maxDisplay (10) entries with an ellipsis.
func (r Result) String() string {
	if r.Success {
		if r.Total > 0 {
			return fmt.Sprintf("✓ %s (%d rows)", r.Name, r.Total)
		}
		return fmt.Sprintf("✓ %s", r.Name)
	}
	if r.FailedCount == 0 {
		return fmt.Sprintf("✗ %s", r.Name)
	}
	return fmt.Sprintf("✗ %s  %d/%d failed (%.1f%%)  e.g. %s @ %s",
		r.Name,
		r.FailedCount,
		r.Total,
		r.FailedPercent,
		truncList(r.SampleValues, maxDisplay),
		truncKeys(r.FailedKeys, maxDisplay),
	)
}

// String renders a [Report] summary header ("gxsql report: passed/total
// expectations passed") followed by one indented [Result.String] line per
// entry in declaration order.
func (r Report) String() string {
	passed := 0
	for _, res := range r.Results {
		if res.Success {
			passed++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "gxsql report: %d/%d expectations passed", passed, len(r.Results))
	for _, res := range r.Results {
		b.WriteString("\n  ")
		b.WriteString(res.String())
	}
	return b.String()
}

func truncList(xs []any, max int) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range xs {
		if i >= max {
			b.WriteString(" …")
			break
		}
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%v", x)
	}
	b.WriteByte(']')
	return b.String()
}

func truncKeys(keys []RowKey, max int) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, key := range keys {
		if i >= max {
			b.WriteString(" …")
			break
		}
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%v", []any(key))
	}
	b.WriteByte(']')
	return b.String()
}
