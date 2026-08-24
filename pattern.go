package gxsql

import (
	"fmt"
	"strings"
)

const likeEscapeRune = '\\'

// HasPrefix returns a per-row expectation that the column value starts with
// prefix. The bound argument is a literal fragment: LIKE wildcards (% and _)
// and backslash are escaped, and a dialect-safe ESCAPE clause is rendered so
// caller data never becomes wildcards by default. SQL NULL fails. An empty
// table passes vacuously. Results use [KindHasPrefix]. Pattern literals are not
// published in [ResultFacts]; export display names redact them.
func (c StringColumn) HasPrefix(prefix string) Expectation {
	return patternExpectation(c.column, KindHasPrefix, c.column+" has prefix "+fmt.Sprintf("%q", prefix), prefix, likeLiteralPrefix)
}

// HasSuffix returns a per-row expectation that the column value ends with
// suffix. The bound argument is a literal fragment with LIKE wildcards and
// backslash escaped. SQL NULL fails. An empty table passes vacuously. Results
// use [KindHasSuffix]. Pattern literals are not published in [ResultFacts].
func (c StringColumn) HasSuffix(suffix string) Expectation {
	return patternExpectation(c.column, KindHasSuffix, c.column+" has suffix "+fmt.Sprintf("%q", suffix), suffix, likeLiteralSuffix)
}

// Contains returns a per-row expectation that the column value contains substr.
// The bound argument is a literal fragment with LIKE wildcards and backslash
// escaped. SQL NULL fails. An empty table passes vacuously. Results use
// [KindContains]. Pattern literals are not published in [ResultFacts].
func (c StringColumn) Contains(substr string) Expectation {
	return patternExpectation(c.column, KindContains, c.column+" contains "+fmt.Sprintf("%q", substr), substr, likeLiteralContains)
}

// Like returns a per-row expectation that the column value matches pattern
// under SQL LIKE. Callers own wildcards; the pattern is bound without
// automatic escaping or an ESCAPE clause. SQL NULL fails. An empty table
// passes vacuously. Results use [KindLike]. Pattern literals are not published
// in [ResultFacts].
func (c StringColumn) Like(pattern string) Expectation {
	return patternExpectation(c.column, KindLike, c.column+" like "+fmt.Sprintf("%q", pattern), pattern, likeRawMatch)
}

// NotLike returns a per-row expectation that the column value does not match
// pattern under SQL LIKE. Callers own wildcards; the pattern is bound without
// automatic escaping or an ESCAPE clause. SQL NULL fails. An empty table
// passes vacuously. Results use [KindNotLike]. Pattern literals are not
// published in [ResultFacts].
func (c StringColumn) NotLike(pattern string) Expectation {
	return patternExpectation(c.column, KindNotLike, c.column+" not like "+fmt.Sprintf("%q", pattern), pattern, likeRawNotMatch)
}

// Regex returns a per-row expectation that the column value matches pattern
// under the dialect's advertised regular-expression capability. Dialects
// must implement [RegexDialect] with complete [RegexCapability] metadata;
// incomplete, missing, or unsupported capability metadata fails closed at
// suite preflight with [UnsupportedCapabilityError] and never rewrites to
// LIKE or issues SQL. Non-empty Flags and RegexMatchFull are rejected until
// rendering supports them. SQL NULL fails. An empty table passes vacuously.
// Results use [KindRegex]. Case folding and Unicode behavior follow the
// advertised capability; gxsql does not claim cross-engine regex parity.
// Pattern literals are not published in [ResultFacts].
func (c StringColumn) Regex(pattern string) Expectation {
	return patternExpectation(c.column, KindRegex, c.column+" regex "+fmt.Sprintf("%q", pattern), pattern, regexMatch)
}

type patternMode int

const (
	likeLiteralPrefix patternMode = iota
	likeLiteralSuffix
	likeLiteralContains
	likeRawMatch
	likeRawNotMatch
	regexMatch
)

func patternExpectation(column string, kind ExpectationKind, name, pattern string, mode patternMode) Expectation {
	// Spec 04 privacy: omit ResultFacts entirely so default export never
	// serializes pattern literals.
	return perRowExpectation{
		column: column,
		name:   name,
		kind:   kind,
		build: func(d Dialect, col string, scope *trustedScope) (rowPredicate, error) {
			return patternPredicate(d, col, pattern, mode, scope)
		},
	}
}

func patternPredicate(d Dialect, column, pattern string, mode patternMode, scope *trustedScope) (rowPredicate, error) {
	col, err := quoteIdent(d, column)
	if err != nil {
		return rowPredicate{}, err
	}
	if mode == regexMatch {
		return regexFailPredicate(d, col, pattern, scope)
	}

	b := newScopedArgBinder(d, scope)
	var (
		bound   string
		escape  string
		failSQL string
	)
	switch mode {
	case likeLiteralPrefix:
		bound = escapeLikeLiteral(pattern) + "%"
		escape = likeEscapeClause(d)
		failSQL = "%s IS NULL OR %s NOT LIKE %s%s"
	case likeLiteralSuffix:
		bound = "%" + escapeLikeLiteral(pattern)
		escape = likeEscapeClause(d)
		failSQL = "%s IS NULL OR %s NOT LIKE %s%s"
	case likeLiteralContains:
		bound = "%" + escapeLikeLiteral(pattern) + "%"
		escape = likeEscapeClause(d)
		failSQL = "%s IS NULL OR %s NOT LIKE %s%s"
	case likeRawMatch:
		bound = pattern
		failSQL = "%s IS NULL OR %s NOT LIKE %s%s"
	case likeRawNotMatch:
		bound = pattern
		failSQL = "%s IS NULL OR %s LIKE %s%s"
	default:
		return rowPredicate{}, fmt.Errorf("gxsql: unsupported pattern mode %d", mode)
	}
	ph := b.bind(bound)
	where := fmt.Sprintf(failSQL, col, col, ph, escape)
	return withWhere(where, b.args), nil
}

func regexFailPredicate(d Dialect, quotedColumn, pattern string, scope *trustedScope) (rowPredicate, error) {
	capability, err := regexCapabilityFor(d)
	if err != nil {
		return rowPredicate{}, err
	}
	b := newScopedArgBinder(d, scope)
	ph := b.bind(pattern)
	match, err := renderRegexMatch(capability, quotedColumn, ph)
	if err != nil {
		return rowPredicate{}, unsupportedCapabilityError(KindRegex, d, "regex.operator")
	}
	where := fmt.Sprintf("%s IS NULL OR NOT (%s)", quotedColumn, match)
	return withWhere(where, b.args), nil
}

// renderRegexMatch builds a boolean SQL expression from advertised capability
// metadata so custom RegexDialect implementations can execute without an
// unexported renderer. Incomplete or unsupported metadata fails closed.
func renderRegexMatch(cap RegexCapability, expr, patternPlaceholder string) (string, error) {
	if err := validateRegexCapabilityShape(cap); err != nil {
		return "", err
	}
	op := strings.TrimSpace(cap.Operator)
	if cap.Function {
		return op + "(" + expr + ", " + patternPlaceholder + ")", nil
	}
	switch op {
	case "~", "REGEXP":
		return expr + " " + op + " " + patternPlaceholder, nil
	default:
		return "", fmt.Errorf("regex.operator")
	}
}

func validateRegexCapabilityShape(cap RegexCapability) error {
	if cap.Name != regexCapabilityName {
		return fmt.Errorf("regex.name")
	}
	if strings.TrimSpace(cap.Operator) == "" {
		return fmt.Errorf("regex.operator")
	}
	// Non-empty Flags are rejected until rendering supports flag arguments.
	if cap.Flags != "" {
		return fmt.Errorf("regex.flags")
	}
	// renderRegexMatch only supports substring operators; full-match metadata
	// is rejected until rendering supports it.
	if cap.Match != RegexMatchSubstring {
		return fmt.Errorf("regex.match")
	}
	if cap.NullBehavior != RegexNullsFail {
		return fmt.Errorf("regex.null_behavior")
	}
	if strings.TrimSpace(cap.UnicodeLimits) == "" {
		return fmt.Errorf("regex.unicode_limits")
	}
	return nil
}

func validateRegexCapability(cap RegexCapability) error {
	if err := validateRegexCapabilityShape(cap); err != nil {
		return err
	}
	if cap.Function {
		if !validIdent(strings.TrimSpace(cap.Operator)) {
			return fmt.Errorf("regex.operator")
		}
		return nil
	}
	switch strings.TrimSpace(cap.Operator) {
	case "~", "REGEXP":
		return nil
	default:
		return fmt.Errorf("regex.operator")
	}
}

// escapeLikeLiteral escapes LIKE wildcards and the escape character itself so
// a caller-supplied fragment remains literal under [likeEscapeClause].
func escapeLikeLiteral(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case likeEscapeRune, '%', '_':
			b.WriteRune(likeEscapeRune)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// likeEscapeClause returns a dialect-safe ESCAPE fragment for literal-fragment
// LIKE predicates. The escape character is backslash. Custom dialects that need
// a non-default SQL spelling may implement likeEscapeClause().
func likeEscapeClause(d Dialect) string {
	type escapeClause interface {
		likeEscapeClause() string
	}
	if e, ok := d.(escapeClause); ok {
		return e.likeEscapeClause()
	}
	return ` ESCAPE '\'`
}

func (postgresDialect) likeEscapeClause() string { return ` ESCAPE '\'` }
func (sqliteDialect) likeEscapeClause() string   { return ` ESCAPE '\'` }
func (duckdbDialect) likeEscapeClause() string   { return ` ESCAPE '\'` }

// MySQL string literals treat backslash as an escape by default, so the SQL
// text must double it to yield a single-character escape of '\'.
func (mysqlDialect) likeEscapeClause() string { return ` ESCAPE '\\'` }

const regexCapabilityName = "regex"

// RegexMatchMode describes whether the advertised engine operation matches
// substrings or complete values by default.
type RegexMatchMode string

const (
	// RegexMatchSubstring means the engine matches anywhere unless the caller
	// anchors the pattern.
	RegexMatchSubstring RegexMatchMode = "substring"
	// RegexMatchFull means the engine requires a whole-string match.
	// Advertised full-match capability is rejected until rendering supports
	// it; keep [RegexMatchSubstring] and anchor patterns when needed.
	RegexMatchFull RegexMatchMode = "full"
)

// RegexNullBehavior describes how the advertised operation treats SQL NULL.
type RegexNullBehavior string

const (
	// RegexNullsFail means SQL NULL values fail the expectation, matching other
	// string checks.
	RegexNullsFail RegexNullBehavior = "null_fails"
)

// RegexCapability documents a dialect's advertised regular-expression
// matching. Metadata is validated at suite preflight and used to render SQL,
// so custom dialects that implement [RegexDialect] can execute when the
// advertised operator/function is complete and supported. Unsupported
// metadata (non-empty Flags, RegexMatchFull) is rejected until rendering
// supports it. gxsql does not claim identical regex dialects across engines.
type RegexCapability struct {
	// Name is the stable capability identifier ("regex").
	Name string
	// Operator is the SQL operator or function name used for matching
	// (for example "~", "REGEXP", or a function name when Function is true).
	Operator string
	// Function reports whether Operator is rendered as a two-argument
	// function (Operator(expr, pattern)) rather than an infix operator.
	Function bool
	// Flags lists advertised default/available flags. Empty means Regex has
	// no flag argument and none are selected. Non-empty values are rejected
	// until rendering supports flag arguments.
	Flags string
	// Match describes full-string or substring matching semantics. Only
	// [RegexMatchSubstring] is accepted until rendering supports full match.
	Match RegexMatchMode
	// NullBehavior describes the advertised SQL NULL behavior.
	NullBehavior RegexNullBehavior
	// UnicodeLimits records engine-specific Unicode limitations (required;
	// use an explicit phrase such as "engine-defined").
	UnicodeLimits string
}

// RegexDialect is an optional dialect extension that advertises regex support.
// It does not widen [Dialect]; dialects that omit it, or that advertise
// incomplete or unsupported [RegexCapability] metadata, fail
// [StringColumn.Regex] closed at suite preflight with
// [UnsupportedCapabilityError] naming the missing or unsupported field.
type RegexDialect interface {
	// RegexCapability returns the regex matching metadata this dialect
	// advertises for [StringColumn.Regex].
	RegexCapability() RegexCapability
}

func regexCapabilityFor(d Dialect) (RegexCapability, error) {
	rd, ok := d.(RegexDialect)
	if !ok {
		return RegexCapability{}, unsupportedCapabilityError(KindRegex, d, regexCapabilityName)
	}
	capability := rd.RegexCapability()
	if err := validateRegexCapability(capability); err != nil {
		return RegexCapability{}, unsupportedCapabilityError(KindRegex, d, err.Error())
	}
	return capability, nil
}

func regexCapabilityError(d Dialect) error {
	_, err := regexCapabilityFor(d)
	return err
}

func requiresRegexDialect(exp Expectation) bool {
	e, ok := unwrapExpectation(exp).(perRowExpectation)
	return ok && e.kind == KindRegex
}

func (postgresDialect) RegexCapability() RegexCapability {
	return RegexCapability{
		Name:          regexCapabilityName,
		Operator:      "~",
		Flags:         "", // no flag argument
		Match:         RegexMatchSubstring,
		NullBehavior:  RegexNullsFail,
		UnicodeLimits: "engine-defined POSIX/Unicode behavior",
	}
}

func (duckdbDialect) RegexCapability() RegexCapability {
	return RegexCapability{
		Name:          regexCapabilityName,
		Operator:      "~",
		Flags:         "", // no flag argument
		Match:         RegexMatchSubstring,
		NullBehavior:  RegexNullsFail,
		UnicodeLimits: "engine-defined Unicode behavior",
	}
}

func (mysqlDialect) RegexCapability() RegexCapability {
	return RegexCapability{
		Name:          regexCapabilityName,
		Operator:      "REGEXP",
		Flags:         "", // no flag argument
		Match:         RegexMatchSubstring,
		NullBehavior:  RegexNullsFail,
		UnicodeLimits: "engine-defined Unicode behavior",
	}
}

func dialectLabel(d Dialect) string {
	switch d.(type) {
	case postgresDialect:
		return "postgres"
	case sqliteDialect:
		return "sqlite"
	case mysqlDialect:
		return "mysql"
	case duckdbDialect:
		return "duckdb"
	default:
		if d == nil {
			return "<nil>"
		}
		return fmt.Sprintf("%T", d)
	}
}
