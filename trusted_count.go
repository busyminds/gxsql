package gxsql

import (
	"fmt"
	"strings"

	"github.com/busyminds/gxsql/internal/sqltext"
)

const (
	markerTarget = "{{target}}"
	markerScope  = "{{scope}}"
)

type trustedCountScan struct {
	targetCount     int
	scopeCount      int
	scopeMarkerEnd  int
	customSlotCount int
}

// scanTrustedCountWithArgs validates a trusted-count SQL template and custom-arg
// arity without rendering or executing SQL.
func scanTrustedCountWithArgs(template string, customArgs []any) (trustedCountScan, error) {
	scan, err := scanTrustedCountTemplate(template)
	if err != nil {
		return trustedCountScan{}, err
	}
	if scan.customSlotCount != len(customArgs) {
		return trustedCountScan{}, newConfigError(trustedCountArityError(scan.customSlotCount, len(customArgs)))
	}
	return scan, nil
}

func preflightTrustedCount(template string, customArgs []any) error {
	_, err := scanTrustedCountWithArgs(template, customArgs)
	return err
}

// renderTrustedCount renders a trusted-count SQL template for the validated
// table and scope. Returned args bind scope values first, then custom values.
func renderTrustedCount(
	d Dialect,
	table TableRef,
	scope *trustedScope,
	template string,
	customArgs []any,
) (string, []any, error) {
	scan, err := scanTrustedCountWithArgs(template, customArgs)
	if err != nil {
		return "", nil, err
	}
	if d == nil {
		return "", nil, categorizeRenderError(fmt.Errorf("gxsql: dialect is required"))
	}

	renderedTable, err := renderTable(d, table)
	if err != nil {
		return "", nil, categorizeRenderError(err)
	}

	scopeSQL, scopeArgs, scopeSlots, err := renderTrustedCountScope(d, scope)
	if err != nil {
		return "", nil, err
	}

	sql, err := renderTrustedCountTemplate(d, template, scan, renderedTable, scopeSQL, scopeSlots)
	if err != nil {
		return "", nil, err
	}

	args := append(append([]any(nil), scopeArgs...), customArgs...)
	return sql, args, nil
}

func renderTrustedCountScope(d Dialect, scope *trustedScope) (sql string, args []any, slotCount int, err error) {
	if scope == nil {
		return "TRUE", nil, 0, nil
	}
	pred, err := scope.render(d)
	if err != nil {
		return "", nil, 0, err
	}
	return "(" + pred.where + ")", append([]any(nil), pred.args...), len(pred.args), nil
}

func scanTrustedCountTemplate(template string) (trustedCountScan, error) {
	_, scan, err := walkTrustedCountTemplate(template, nil, trustedCountScan{scopeMarkerEnd: -1}, "", "", 0)
	if err != nil {
		return trustedCountScan{}, err
	}
	if scan.targetCount == 0 {
		return trustedCountScan{}, newConfigError(errTrustedCountTargetMarkerRequired)
	}
	if scan.scopeCount == 0 {
		return trustedCountScan{}, newConfigError(errTrustedCountScopeMarkerRequired)
	}
	if scan.targetCount > 1 {
		return trustedCountScan{}, newConfigError(errTrustedCountDuplicateTargetMarker)
	}
	if scan.scopeCount > 1 {
		return trustedCountScan{}, newConfigError(errTrustedCountDuplicateScopeMarker)
	}
	return scan, nil
}

func renderTrustedCountTemplate(
	d Dialect,
	template string,
	scan trustedCountScan,
	renderedTable, scopeSQL string,
	scopeSlots int,
) (string, error) {
	rendered, _, err := walkTrustedCountTemplate(template, d, scan, renderedTable, scopeSQL, scopeSlots)
	return rendered, err
}

func walkTrustedCountTemplate(
	template string,
	d Dialect,
	scan trustedCountScan,
	renderedTable, scopeSQL string,
	scopeSlots int,
) (string, trustedCountScan, error) {
	render := d != nil
	customRendered := 0

	rendered, err := sqltext.Walk(template, render, sqltext.Handlers{
		RejectLiteral: func(msg string) error {
			return unsupportedScopePredicateError(msg)
		},
		OnDoubleBrace: func(pos int) (string, int, error) {
			marker, length, err := parseExecutableMarker(template, pos)
			if err != nil {
				return "", 0, err
			}
			if length == 0 {
				return "", 0, nil
			}
			var replacement string
			switch marker {
			case "target":
				scan.targetCount++
				replacement = renderedTable
			case "scope":
				scan.scopeCount++
				scan.scopeMarkerEnd = pos + length
				replacement = scopeSQL
			}
			return replacement, length, nil
		},
		OnQuestionMark: func(pos int) (string, error) {
			if scan.scopeMarkerEnd < 0 || pos < scan.scopeMarkerEnd {
				return "", newConfigError(errTrustedCountCustomPlaceholderBeforeScope)
			}
			scan.customSlotCount++
			if !render {
				return "", nil
			}
			customRendered++
			return d.Placeholder(scopeSlots + customRendered), nil
		},
	})
	if err != nil {
		return "", scan, err
	}
	return rendered, scan, nil
}

func parseExecutableMarker(fragment string, start int) (name string, length int, err error) {
	if start+1 >= len(fragment) || fragment[start+1] != '{' {
		return "", 0, nil
	}
	if start+2 >= len(fragment) {
		return "", 0, newConfigError(errTrustedCountMalformedMarker)
	}
	close := strings.Index(fragment[start+2:], "}}")
	if close < 0 {
		return "", 0, newConfigError(errTrustedCountMalformedMarker)
	}
	name = fragment[start+2 : start+2+close]
	length = 2 + close + 2
	switch fragment[start : start+length] {
	case markerTarget:
		return "target", length, nil
	case markerScope:
		return "scope", length, nil
	}
	if isTemplateMarkerName(name) {
		return "", length, newConfigError(errTrustedCountUnsupportedMarker)
	}
	return "", 0, newConfigError(errTrustedCountMalformedMarker)
}

func isTemplateMarkerName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if i == 0 {
			if !isTemplateMarkerStart(c) {
				return false
			}
			continue
		}
		if !isTemplateMarkerPart(c) {
			return false
		}
	}
	return true
}

func isTemplateMarkerStart(c byte) bool {
	return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func isTemplateMarkerPart(c byte) bool {
	return isTemplateMarkerStart(c) || c >= '0' && c <= '9'
}
