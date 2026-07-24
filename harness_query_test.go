package gxsql

import (
	"database/sql/driver"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	countRe         = regexp.MustCompile(`(?is)^SELECT\s+COUNT\(\*\)\s+FROM\s+(.+?)(?:\s+WHERE\s+(.+))?$`)
	countDistinctRe = regexp.MustCompile(`(?is)^SELECT\s+COUNT\(DISTINCT\s+(.+?)\)\s+FROM\s+(.+?)(?:\s+WHERE\s+(.+))?$`)
	aggRe           = regexp.MustCompile(`(?is)^SELECT\s+(AVG|MIN|MAX)\((.+?)\)\s+FROM\s+(.+?)(?:\s+WHERE\s+(.+))?$`)
	selectRe        = regexp.MustCompile(`(?is)^SELECT\s+(.+?)\s+FROM\s+(.+?)(?:\s+WHERE\s+(.+?))?(?:\s+ORDER\s+BY\s+(.+?))?(?:\s+LIMIT\s+(.+))?$`)
	subUniqueRe     = regexp.MustCompile(
		`(?is)^(.+?)\s+IS\s+NOT\s+NULL\s+AND\s+(.+?)\s+IN\s+\(\s*SELECT\s+(.+?)\s+FROM\s+(.+?)(?:\s+WHERE\s+(.+?))?\s+GROUP\s+BY\s+(.+?)\s+HAVING\s+COUNT\(\*\)\s*>\s*1\s*\)$`,
	)
)

func executeHarnessQuery(query string, args []any, tables map[string][]map[string]any) ([]string, [][]driver.Value, error) {
	q := collapseSpaces(query)

	if m := countRe.FindStringSubmatch(q); m != nil {
		table, err := resolveTable(m[1], tables)
		if err != nil {
			return nil, nil, err
		}
		where := strings.TrimSpace(m[2])
		n := 0
		for _, row := range table {
			if where == "" || rowMatchesWhere(where, args, row, m[1], table) {
				n++
			}
		}
		return []string{"count"}, [][]driver.Value{{int64(n)}}, nil
	}

	if m := countDistinctRe.FindStringSubmatch(q); m != nil {
		col := unquoteIdent(m[1])
		table, err := resolveTable(m[2], tables)
		if err != nil {
			return nil, nil, err
		}
		where := strings.TrimSpace(m[3])
		if where != "" {
			filtered := make([]map[string]any, 0, len(table))
			for _, row := range table {
				if rowMatchesWhere(where, args, row, m[2], table) {
					filtered = append(filtered, row)
				}
			}
			table = filtered
		}
		n := distinctCountColumn(table, col)
		return []string{"count"}, [][]driver.Value{{int64(n)}}, nil
	}

	if m := aggRe.FindStringSubmatch(q); m != nil {
		agg := strings.ToUpper(m[1])
		col := unquoteIdent(m[2])
		table, err := resolveTable(m[3], tables)
		if err != nil {
			return nil, nil, err
		}
		where := strings.TrimSpace(m[4])
		if where != "" {
			filtered := make([]map[string]any, 0, len(table))
			for _, row := range table {
				if rowMatchesWhere(where, args, row, m[3], table) {
					filtered = append(filtered, row)
				}
			}
			table = filtered
		}
		val, ok := aggregateColumn(table, col, agg)
		if !ok {
			return []string{strings.ToLower(agg)}, [][]driver.Value{{nil}}, nil
		}
		return []string{strings.ToLower(agg)}, [][]driver.Value{{val}}, nil
	}

	if m := selectRe.FindStringSubmatch(q); m != nil {
		cols := parseSelectList(m[1])
		table, err := resolveTable(m[2], tables)
		if err != nil {
			return nil, nil, err
		}
		where := strings.TrimSpace(m[3])
		orderBy := strings.TrimSpace(m[4])
		limit := 0
		whereArgs := args
		if m[5] != "" {
			if n, err := strconv.Atoi(m[5]); err == nil {
				limit = n
			} else if len(args) > 0 {
				whereArgs = args[:len(args)-1]
				if n, ok := toInt(args[len(args)-1]); ok {
					limit = n
				}
			}
		}
		_ = orderBy

		var out [][]driver.Value
		for _, row := range table {
			if where != "" && !rowMatchesWhere(where, whereArgs, row, m[2], table) {
				continue
			}
			vals := make([]driver.Value, len(cols))
			for i, col := range cols {
				vals[i] = row[col]
			}
			out = append(out, vals)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
		quotedCols := make([]string, len(cols))
		for i, c := range cols {
			quotedCols[i] = `"` + c + `"`
		}
		return quotedCols, out, nil
	}

	return nil, nil, fmt.Errorf("gxsqltest: unsupported query: %s", query)
}

func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func parseSelectList(list string) []string {
	var cols []string
	for _, part := range strings.Split(list, ",") {
		cols = append(cols, unquoteIdent(strings.TrimSpace(part)))
	}
	return cols
}

func unquoteIdent(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
	}
	if strings.HasPrefix(s, "`") && strings.HasSuffix(s, "`") {
		return strings.ReplaceAll(s[1:len(s)-1], "``", "`")
	}
	return s
}
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func resolveTable(ref string, tables map[string][]map[string]any) ([]map[string]any, error) {
	ref = strings.TrimSpace(ref)
	if rows, ok := tables[ref]; ok {
		return rows, nil
	}
	if strings.Contains(ref, ".") {
		parts := strings.Split(ref, ".")
		simple := unquoteIdent(parts[len(parts)-1])
		if rows, ok := tables[simple]; ok {
			return rows, nil
		}
	}
	if rows, ok := tables[unquoteIdent(ref)]; ok {
		return rows, nil
	}
	return nil, fmt.Errorf("gxsqltest: unknown table %s", ref)
}

func rowMatchesWhere(where string, args []any, row map[string]any, tableRef string, allRows []map[string]any) bool {
	where = collapseSpaces(where)
	where, _ = bindQuestionMarks(where, args)
	for {
		inner, ok := stripOuterParens(where)
		if !ok {
			break
		}
		where = inner
	}

	if m := subUniqueRe.FindStringSubmatch(where); m != nil {
		col := unquoteIdent(m[1])
		subWhere := strings.TrimSpace(m[5])
		return isDuplicateColumnValueScoped(col, row[col], subWhere, args, allRows, tableRef)
	}

	if andParts := splitTopLevel(where, " AND "); len(andParts) > 1 {
		for _, part := range andParts {
			if !rowMatchesWhere(strings.TrimSpace(part), args, row, tableRef, allRows) {
				return false
			}
		}
		return true
	}

	if orParts := splitTopLevel(where, " OR "); len(orParts) > 1 {
		for _, part := range orParts {
			if evalWhereAtom(strings.TrimSpace(part), args, row, tableRef, allRows) {
				return true
			}
		}
		return false
	}

	return evalWhereAtom(where, args, row, tableRef, allRows)
}

func isDuplicateColumnValueScoped(
	col string,
	val any,
	where string,
	args []any,
	allRows []map[string]any,
	tableRef string,
) bool {
	if val == nil {
		return false
	}
	n := 0
	for _, other := range allRows {
		if where != "" && !rowMatchesWhere(where, args, other, tableRef, allRows) {
			continue
		}
		if valuesEqual(other[col], val) {
			n++
		}
	}
	return n > 1
}

func splitTopLevel(where, sep string) []string {
	var parts []string
	depth := 0
	start := 0
	sepLen := len(sep)
	for i := 0; i <= len(where)-sepLen; i++ {
		switch where[i] {
		case '(':
			depth++
		case ')':
			depth--
		default:
			if depth == 0 && strings.HasPrefix(where[i:], sep) {
				parts = append(parts, where[start:i])
				start = i + sepLen
				i += sepLen - 1
			}
		}
	}
	parts = append(parts, where[start:])
	return parts
}

func stripOuterParens(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "(") || !strings.HasSuffix(s, ")") {
		return s, false
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i < len(s)-1 {
				return s, false
			}
		}
	}
	if depth != 0 {
		return s, false
	}
	return strings.TrimSpace(s[1 : len(s)-1]), true
}

func bindQuestionMarks(atom string, args []any) (string, []any) {
	if !strings.Contains(atom, "?") {
		return atom, args
	}
	qidx := 0
	var out strings.Builder
	for i := 0; i < len(atom); i++ {
		if atom[i] == '?' {
			if qidx < len(args) {
				fmt.Fprintf(&out, "%v", args[qidx])
				qidx++
			} else {
				out.WriteByte('?')
			}
		} else {
			out.WriteByte(atom[i])
		}
	}
	return out.String(), args[qidx:]
}

func evalWhereAtom(atom string, args []any, row map[string]any, tableRef string, allRows []map[string]any) bool {
	atom = strings.TrimSpace(atom)
	if inner, ok := stripOuterParens(atom); ok {
		return rowMatchesWhere(inner, args, row, tableRef, allRows)
	}

	if before, ok := strings.CutSuffix(atom, " IS NULL"); ok {
		col := unquoteIdent(before)
		return row[col] == nil
	}
	if before, ok := strings.CutSuffix(atom, " IS NOT NULL"); ok {
		col := unquoteIdent(before)
		return row[col] != nil
	}

	if m := regexp.MustCompile(`^(.+?) = ''$`).FindStringSubmatch(atom); m != nil {
		col := unquoteIdent(m[1])
		v, _ := row[col].(string)
		return v == ""
	}
	if m := regexp.MustCompile(`^(.+?) <> ''$`).FindStringSubmatch(atom); m != nil {
		col := unquoteIdent(m[1])
		v, _ := row[col].(string)
		return v != ""
	}

	if m := regexp.MustCompile(`(?is)^(.+?)\s+(NOT IN|IN)\s*\((.+)\)$`).FindStringSubmatch(atom); m != nil {
		col := unquoteIdent(m[1])
		inSet := strings.EqualFold(m[2], "IN")
		val := row[col]
		if val == nil {
			return inSet
		}
		placeholders := strings.Split(m[3], ",")
		qidx := 0
		for _, ph := range placeholders {
			ph = strings.TrimSpace(ph)
			var bound any
			if ph == "?" {
				if qidx < len(args) {
					bound = args[qidx]
					qidx++
				}
			} else if strings.HasPrefix(ph, "$") {
				idx, _ := strconv.Atoi(ph[1:])
				if idx > 0 && idx <= len(args) {
					bound = args[idx-1]
				}
			}
			if valuesEqual(val, bound) {
				return inSet
			}
		}
		return !inSet
	}

	if m := regexp.MustCompile(`(?is)^(CHAR_LENGTH|LENGTH)\((.+?)\)\s*(<=|>=|<>|<|>|=)\s*(-?\d+)$`).FindStringSubmatch(atom); m != nil {
		col := unquoteIdent(m[2])
		op := m[3]
		n, _ := strconv.Atoi(m[4])
		s, _ := row[col].(string)
		return compareInt(len(s), op, n)
	}

	if m := regexp.MustCompile(`(?is)^(CHAR_LENGTH|LENGTH)\((.+?)\)\s*<\s*(-?\d+)$`).FindStringSubmatch(atom); m != nil {
		col := unquoteIdent(m[2])
		lo, _ := strconv.Atoi(m[3])
		s, _ := row[col].(string)
		return len(s) < lo
	}
	if m := regexp.MustCompile(`(?is)^(CHAR_LENGTH|LENGTH)\((.+?)\)\s*>\s*(-?\d+)$`).FindStringSubmatch(atom); m != nil {
		col := unquoteIdent(m[2])
		hi, _ := strconv.Atoi(m[3])
		s, _ := row[col].(string)
		return len(s) > hi
	}

	if m := regexp.MustCompile(`^(.+?)\s*=\s*(\$\d+|\?|'.+?'|-?\d+(?:\.\d+)?|\S+)$`).FindStringSubmatch(atom); m != nil {
		col := unquoteIdent(m[1])
		bound := resolveBound(m[2], args)
		return valuesEqual(row[col], bound)
	}

	if m := regexp.MustCompile(`^(.+?)\s*(<=|>=|<>|<|>)\s*(\$\d+|\?|'.+?'|-?\d+(?:\.\d+)?)$`).FindStringSubmatch(atom); m != nil {
		col := unquoteIdent(m[1])
		if strings.HasPrefix(col, "CHAR_LENGTH") || strings.HasPrefix(col, "LENGTH") {
			return false
		}
		op := m[2]
		bound := resolveBound(m[3], args)
		return compareValues(row[col], op, bound)
	}

	return false
}

func resolveBound(token string, args []any) any {
	token = strings.TrimSpace(token)
	if token == "?" && len(args) > 0 {
		return args[0]
	}
	if strings.HasPrefix(token, "$") {
		idx, _ := strconv.Atoi(token[1:])
		if idx > 0 && idx <= len(args) {
			return args[idx-1]
		}
	}
	if strings.HasPrefix(token, "'") && strings.HasSuffix(token, "'") {
		return token[1 : len(token)-1]
	}
	if i, err := strconv.ParseInt(token, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(token, 64); err == nil {
		return f
	}
	return token
}

func compareValues(left any, op string, right any) bool {
	li, lok := toFloat64(left)
	ri, rok := toFloat64(right)
	if lok && rok {
		return compareFloat(li, op, ri)
	}
	ls, lok := left.(string)
	rs, rok := right.(string)
	if lok && rok {
		return compareString(ls, op, rs)
	}
	return compareString(fmt.Sprint(left), op, fmt.Sprint(right))
}

func compareString(left, op, right string) bool {
	switch op {
	case "<":
		return left < right
	case "<=":
		return left <= right
	case ">":
		return left > right
	case ">=":
		return left >= right
	case "<>", "=":
		return left != right
	default:
		return false
	}
}

func compareInt(left int, op string, right int) bool {
	return compareFloat(float64(left), op, float64(right))
}

func compareFloat(left float64, op string, right float64) bool {
	switch op {
	case "<":
		return left < right
	case "<=":
		return left <= right
	case ">":
		return left > right
	case ">=":
		return left >= right
	case "<>", "=":
		return left != right
	default:
		return false
	}
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

func valuesEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	af, aok := toFloat64(a)
	bf, bok := toFloat64(b)
	if aok && bok {
		return af == bf
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func distinctCountColumn(rows []map[string]any, col string) int {
	seen := make(map[string]struct{})
	for _, row := range rows {
		v := row[col]
		if v == nil {
			continue
		}
		seen[fmt.Sprint(v)] = struct{}{}
	}
	return len(seen)
}

func aggregateColumn(rows []map[string]any, col, agg string) (float64, bool) {
	var vals []float64
	for _, row := range rows {
		if v, ok := toFloat64(row[col]); ok {
			vals = append(vals, v)
		}
	}
	if len(vals) == 0 {
		return 0, false
	}
	switch agg {
	case "AVG":
		sum := 0.0
		for _, v := range vals {
			sum += v
		}
		return sum / float64(len(vals)), true
	case "MIN":
		m := vals[0]
		for _, v := range vals[1:] {
			if v < m {
				m = v
			}
		}
		return m, true
	case "MAX":
		m := vals[0]
		for _, v := range vals[1:] {
			if v > m {
				m = v
			}
		}
		return m, true
	default:
		return 0, false
	}
}
