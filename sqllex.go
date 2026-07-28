package gxsql

import "strings"

type sqlTextHandlers struct {
	rejectLiteral  func(msg string) error
	onQuestionMark func(pos int) error
	onDoubleBrace  func(pos int) (advance int, err error)
}

type sqlTextWalker struct {
	fragment string
	render   bool
	b        strings.Builder
	start    int
}

func newSQLTextWalker(fragment string, render bool) *sqlTextWalker {
	w := &sqlTextWalker{fragment: fragment, render: render}
	if render {
		w.b.Grow(len(fragment) + 32)
	}
	return w
}

func (w *sqlTextWalker) flush(end int) {
	if w.render && end > w.start {
		w.b.WriteString(w.fragment[w.start:end])
	}
}

func (w *sqlTextWalker) writeString(s string) {
	if w.render {
		w.b.WriteString(s)
	}
}

func (w *sqlTextWalker) result() string {
	if w.render {
		return w.b.String()
	}
	return ""
}

func (w *sqlTextWalker) walk(h sqlTextHandlers) error {
	reject := h.rejectLiteral
	if reject == nil {
		reject = func(msg string) error {
			return unsupportedScopePredicateError(msg)
		}
	}

	var quote byte
	for i := 0; i < len(w.fragment); i++ {
		c := w.fragment[i]
		if quote != 0 {
			if c == '?' {
				return reject("literal ? in quoted text is unsupported")
			}
			if quote == '\'' && c == '\\' && i+1 < len(w.fragment) {
				if w.fragment[i+1] == '?' {
					return reject("literal ? in quoted text is unsupported")
				}
				i++
				continue
			}
			if c == quote {
				if i+1 < len(w.fragment) && w.fragment[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		if c == '$' {
			if end := dollarQuoteDelimiterEnd(w.fragment, i); end >= 0 {
				delimiter := w.fragment[i : end+1]
				closeAt := strings.Index(w.fragment[end+1:], delimiter)
				if closeAt < 0 {
					if strings.Contains(w.fragment[end+1:], "?") {
						return reject("literal ? in dollar-quoted text is unsupported")
					}
					break
				}
				bodyEnd := end + 1 + closeAt
				if strings.Contains(w.fragment[end+1:bodyEnd], "?") {
					return reject("literal ? in dollar-quoted text is unsupported")
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
			for i += 1; i < len(w.fragment) && w.fragment[i] != '\n'; i++ {
				if w.fragment[i] == '?' {
					return reject("literal ? in comment is unsupported")
				}
			}
			continue
		case '-':
			if i+1 < len(w.fragment) && w.fragment[i+1] == '-' {
				for i += 2; i < len(w.fragment) && w.fragment[i] != '\n'; i++ {
					if w.fragment[i] == '?' {
						return reject("literal ? in comment is unsupported")
					}
				}
				continue
			}
		case '/':
			if i+1 < len(w.fragment) && w.fragment[i+1] == '*' {
				depth := 1
				for i += 2; i < len(w.fragment); i++ {
					if w.fragment[i] == '?' {
						return reject("literal ? in comment is unsupported")
					}
					if i+1 >= len(w.fragment) {
						continue
					}
					if w.fragment[i] == '/' && w.fragment[i+1] == '*' {
						depth++
						i++
						continue
					}
					if w.fragment[i] == '*' && w.fragment[i+1] == '/' {
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
			if h.onDoubleBrace != nil && i+1 < len(w.fragment) && w.fragment[i+1] == '{' {
				advance, err := h.onDoubleBrace(i)
				if err != nil {
					return err
				}
				if advance > 0 {
					i += advance - 1
					continue
				}
			}
		}
		if c != '?' {
			continue
		}
		if err := validateExecutableQuestionMark(w.fragment, i); err != nil {
			return err
		}
		if err := h.onQuestionMark(i); err != nil {
			return err
		}
	}
	w.flush(len(w.fragment))
	return nil
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

func validateExecutableQuestionMark(fragment string, i int) error {
	if i > 0 && fragment[i-1] == '@' {
		return unsupportedScopePredicateError("? operator is unsupported")
	}
	if i+1 < len(fragment) {
		switch fragment[i+1] {
		case '?', '|', '&':
			return unsupportedScopePredicateError("? operator is unsupported")
		}
	}
	if i > 0 && fragment[i-1] == '?' {
		return unsupportedScopePredicateError("? operator is unsupported")
	}
	next := i + 1
	for next < len(fragment) && (fragment[next] == ' ' || fragment[next] == '\t' || fragment[next] == '\n' || fragment[next] == '\r') {
		next++
	}
	if next < len(fragment) && (fragment[next] == '\'' || fragment[next] == '"') {
		return unsupportedScopePredicateError("? operator is unsupported")
	}
	return nil
}
