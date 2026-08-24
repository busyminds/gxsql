// Package sqltext lexically walks SQL text, distinguishing executable
// question marks and template markers from quoted and commented text.
package sqltext

import (
	"errors"
	"strings"
)

// Handlers receive events for executable SQL constructs. A question-mark
// handler returns the replacement text to render. A double-brace handler
// returns the number of source bytes consumed and its replacement text; an
// advance of zero leaves the source untouched.
type Handlers struct {
	RejectLiteral  func(msg string) error
	OnQuestionMark func(pos int) (replacement string, err error)
	OnDoubleBrace  func(pos int) (replacement string, advance int, err error)
}

// Walk scans fragment, calling handlers for executable constructs. When
// render is false it validates and invokes handlers without allocating a
// rendered result. Quoted and commented source bytes are never interpreted as
// executable SQL.
func Walk(fragment string, render bool, h Handlers) (string, error) {
	reject := h.RejectLiteral
	if reject == nil {
		reject = errors.New
	}

	var b strings.Builder
	start := 0
	if render {
		b.Grow(len(fragment) + 32)
	}
	flush := func(end int) {
		if render && end > start {
			b.WriteString(fragment[start:end])
		}
	}

	var quote byte
	for i := 0; i < len(fragment); i++ {
		c := fragment[i]
		if quote != 0 {
			if c == '?' {
				return "", reject("literal ? in quoted text is unsupported")
			}
			if quote == '\'' && c == '\\' && i+1 < len(fragment) {
				if fragment[i+1] == '?' {
					return "", reject("literal ? in quoted text is unsupported")
				}
				i++
				continue
			}
			if c == quote {
				if i+1 < len(fragment) && fragment[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		if c == '$' {
			if end := dollarQuoteDelimiterEnd(fragment, i); end >= 0 {
				delimiter := fragment[i : end+1]
				closeAt := strings.Index(fragment[end+1:], delimiter)
				if closeAt < 0 {
					if strings.Contains(fragment[end+1:], "?") {
						return "", reject("literal ? in dollar-quoted text is unsupported")
					}
					break
				}
				bodyEnd := end + 1 + closeAt
				if strings.Contains(fragment[end+1:bodyEnd], "?") {
					return "", reject("literal ? in dollar-quoted text is unsupported")
				}
				i = bodyEnd + len(delimiter) - 1
				continue
			}
		}
		switch c {
		case '\'', '"', '`':
			quote = c
			continue
		case '#':
			for i += 1; i < len(fragment) && fragment[i] != '\n'; i++ {
				if fragment[i] == '?' {
					return "", reject("literal ? in comment is unsupported")
				}
			}
			continue
		case '-':
			if i+1 < len(fragment) && fragment[i+1] == '-' {
				for i += 2; i < len(fragment) && fragment[i] != '\n'; i++ {
					if fragment[i] == '?' {
						return "", reject("literal ? in comment is unsupported")
					}
				}
				continue
			}
		case '/':
			if i+1 < len(fragment) && fragment[i+1] == '*' {
				depth := 1
				for i += 2; i < len(fragment); i++ {
					if fragment[i] == '?' {
						return "", reject("literal ? in comment is unsupported")
					}
					if i+1 >= len(fragment) {
						continue
					}
					if fragment[i] == '/' && fragment[i+1] == '*' {
						depth++
						i++
						continue
					}
					if fragment[i] == '*' && fragment[i+1] == '/' {
						depth--
						i++
						if depth == 0 {
							break
						}
					}
				}
				continue
			}
		case '{':
			if h.OnDoubleBrace != nil && i+1 < len(fragment) && fragment[i+1] == '{' {
				replacement, advance, err := h.OnDoubleBrace(i)
				if err != nil {
					return "", err
				}
				if advance > 0 {
					flush(i)
					if render {
						b.WriteString(replacement)
					}
					start = i + advance
					i += advance - 1
					continue
				}
			}
		}
		if c != '?' {
			continue
		}
		if unsupportedQuestionMark(fragment, i) {
			return "", reject("? operator is unsupported")
		}
		if h.OnQuestionMark == nil {
			continue
		}
		replacement, err := h.OnQuestionMark(i)
		if err != nil {
			return "", err
		}
		flush(i)
		if render {
			b.WriteString(replacement)
		}
		start = i + 1
	}
	flush(len(fragment))
	if render {
		return b.String(), nil
	}
	return "", nil
}

func dollarQuoteDelimiterEnd(fragment string, start int) int {
	if start >= len(fragment) || fragment[start] != '$' {
		return -1
	}
	if start > 0 && (isDollarTagPart(fragment[start-1]) || fragment[start-1] == '$') {
		return -1
	}
	i := start + 1
	if i < len(fragment) && fragment[i] == '$' {
		return i
	}
	if i >= len(fragment) || !isDollarTagStart(fragment[i]) {
		return -1
	}
	for i < len(fragment) && isDollarTagPart(fragment[i]) {
		i++
	}
	if i < len(fragment) && fragment[i] == '$' {
		return i
	}
	return -1
}

func isDollarTagStart(c byte) bool {
	return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func isDollarTagPart(c byte) bool {
	return isDollarTagStart(c) || c >= '0' && c <= '9'
}

func unsupportedQuestionMark(fragment string, i int) bool {
	if i > 0 && fragment[i-1] == '@' {
		return true
	}
	if i+1 < len(fragment) {
		switch fragment[i+1] {
		case '?', '|', '&':
			return true
		}
	}
	if i > 0 && fragment[i-1] == '?' {
		return true
	}
	next := i + 1
	for next < len(fragment) && (fragment[next] == ' ' || fragment[next] == '\t' || fragment[next] == '\n' || fragment[next] == '\r') {
		next++
	}
	return next < len(fragment) && (fragment[next] == '\'' || fragment[next] == '"')
}
