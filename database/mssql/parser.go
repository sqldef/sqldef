package mssql

import (
	"regexp"
	"strings"

	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/parser"
)

type MssqlParser struct {
	parser database.GenericParser
}

var _ database.Parser = (*MssqlParser)(nil)

func NewParser() MssqlParser {
	return MssqlParser{
		parser: database.NewParser(parser.ParserModeMssql),
	}
}

func (p MssqlParser) Parse(sql string) ([]database.DDLStatement, error) {
	re := regexp.MustCompile(`(?im)^\s*GO\s*$|\z`)
	batches := re.Split(sql, -1)
	var result []database.DDLStatement

	for _, batch := range batches {
		s := strings.TrimSpace(normalizeMssqlSyntax(batch))
		if s == "" {
			continue
		}

		stmts, err := p.parser.Parse(s)
		if err != nil {
			return nil, err
		}

		result = append(result, stmts...)
	}

	return result, nil
}

func normalizeMssqlSyntax(sql string) string {
	sql = normalizeDefaultExpressions(sql)
	sql = normalizeBracketedCastTypes(sql)
	sql = normalizeBareReservedIdentifiers(sql)
	return normalizeQualifiedReservedIdentifiers(sql)
}

func normalizeDefaultExpressions(sql string) string {
	var b strings.Builder

	for i := 0; i < len(sql); {
		switch {
		case hasLineCommentPrefix(sql, i):
			next := scanLineComment(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case hasBlockCommentPrefix(sql, i):
			next := scanBlockComment(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case sql[i] == '\'':
			next := scanSingleQuotedString(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case sql[i] == '"':
			next := scanDoubleQuotedString(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case sql[i] == '[':
			next := scanBracketIdentifier(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case hasKeywordAt(sql, i, "DEFAULT"):
			end := i + len("DEFAULT")
			b.WriteString(sql[i:end])

			j := end
			for j < len(sql) && isSpace(sql[j]) {
				b.WriteByte(sql[j])
				j++
			}

			if j < len(sql) && sql[j] == '(' {
				expr, next, ok := scanBalancedParenthesizedSQL(sql, j)
				if ok {
					b.WriteString(stripOuterSQLParens(expr))
					i = next
					continue
				}
			}

			i = j
		default:
			b.WriteByte(sql[i])
			i++
		}
	}

	return b.String()
}

func normalizeQualifiedReservedIdentifiers(sql string) string {
	var b strings.Builder

	for i := 0; i < len(sql); {
		switch {
		case hasLineCommentPrefix(sql, i):
			next := scanLineComment(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case hasBlockCommentPrefix(sql, i):
			next := scanBlockComment(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case sql[i] == '\'':
			next := scanSingleQuotedString(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case sql[i] == '"':
			next := scanDoubleQuotedString(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case sql[i] == '[':
			next := scanBracketIdentifier(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case sql[i] == '.':
			if ident, next, ok := scanReservedQualifiedIdentifier(sql, i+1); ok {
				b.WriteString(".[")
				b.WriteString(ident)
				b.WriteByte(']')
				i = next
				continue
			}
			b.WriteByte(sql[i])
			i++
		default:
			b.WriteByte(sql[i])
			i++
		}
	}

	return b.String()
}

func normalizeBareReservedIdentifiers(sql string) string {
	var b strings.Builder

	for i := 0; i < len(sql); {
		switch {
		case hasLineCommentPrefix(sql, i):
			next := scanLineComment(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case hasBlockCommentPrefix(sql, i):
			next := scanBlockComment(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case sql[i] == '\'':
			next := scanSingleQuotedString(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case sql[i] == '"':
			next := scanDoubleQuotedString(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case sql[i] == '[':
			next := scanBracketIdentifier(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case hasKeywordAt(sql, i, "COMMENT"):
			b.WriteString("[Comment]")
			i += len("COMMENT")
		default:
			b.WriteByte(sql[i])
			i++
		}
	}

	return b.String()
}

func normalizeBracketedCastTypes(sql string) string {
	var b strings.Builder

	for i := 0; i < len(sql); {
		switch {
		case hasLineCommentPrefix(sql, i):
			next := scanLineComment(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case hasBlockCommentPrefix(sql, i):
			next := scanBlockComment(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case sql[i] == '\'':
			next := scanSingleQuotedString(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case sql[i] == '"':
			next := scanDoubleQuotedString(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case sql[i] == '[':
			next := scanBracketIdentifier(sql, i)
			b.WriteString(sql[i:next])
			i = next
		case hasKeywordAt(sql, i, "AS"):
			end := i + len("AS")
			b.WriteString(sql[i:end])

			j := end
			for j < len(sql) && isSpace(sql[j]) {
				b.WriteByte(sql[j])
				j++
			}

			if j < len(sql) && sql[j] == '[' {
				ident, next, ok := scanBracketedCastType(sql, j)
				if ok {
					b.WriteString(ident)
					i = next
					continue
				}
			}

			i = j
		default:
			b.WriteByte(sql[i])
			i++
		}
	}

	return b.String()
}

func scanBracketedCastType(sql string, start int) (string, int, bool) {
	end := scanBracketIdentifier(sql, start)
	if end <= start+2 {
		return "", start, false
	}

	ident := sql[start+1 : end-1]
	switch {
	case strings.EqualFold(ident, "date"):
		return "date", end, true
	case strings.EqualFold(ident, "time"):
		return "time", end, true
	case strings.EqualFold(ident, "timestamp"):
		return "timestamp", end, true
	default:
		return "", start, false
	}
}

func scanReservedQualifiedIdentifier(sql string, start int) (string, int, bool) {
	reserved := []string{"date", "time", "timestamp", "key"}
	for _, keyword := range reserved {
		end := start + len(keyword)
		if end > len(sql) {
			continue
		}
		if !strings.EqualFold(sql[start:end], keyword) {
			continue
		}
		if end < len(sql) && isIdentChar(sql[end]) {
			continue
		}
		return sql[start:end], end, true
	}
	return "", start, false
}

func stripOuterSQLParens(expr string) string {
	trimmed := strings.TrimSpace(expr)
	for len(trimmed) > 0 && trimmed[0] == '(' {
		_, end, ok := scanBalancedParenthesizedSQL(trimmed, 0)
		if !ok || end != len(trimmed) {
			break
		}
		trimmed = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	}
	return trimmed
}

func scanBalancedParenthesizedSQL(sql string, start int) (string, int, bool) {
	if start >= len(sql) || sql[start] != '(' {
		return "", start, false
	}

	depth := 0
	for i := start; i < len(sql); {
		switch {
		case hasLineCommentPrefix(sql, i):
			i = scanLineComment(sql, i)
		case hasBlockCommentPrefix(sql, i):
			i = scanBlockComment(sql, i)
		case sql[i] == '\'':
			i = scanSingleQuotedString(sql, i)
		case sql[i] == '"':
			i = scanDoubleQuotedString(sql, i)
		case sql[i] == '[':
			i = scanBracketIdentifier(sql, i)
		case sql[i] == '(':
			depth++
			i++
		case sql[i] == ')':
			depth--
			i++
			if depth == 0 {
				return sql[start:i], i, true
			}
		default:
			i++
		}
	}

	return "", start, false
}

func hasKeywordAt(sql string, pos int, keyword string) bool {
	if pos < 0 || pos+len(keyword) > len(sql) {
		return false
	}
	if !strings.EqualFold(sql[pos:pos+len(keyword)], keyword) {
		return false
	}
	if pos > 0 && isIdentChar(sql[pos-1]) {
		return false
	}
	end := pos + len(keyword)
	return end == len(sql) || !isIdentChar(sql[end])
}

func isIdentChar(ch byte) bool {
	return ch == '_' ||
		(ch >= '0' && ch <= '9') ||
		(ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z')
}

func isSpace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}

func hasLineCommentPrefix(sql string, pos int) bool {
	return pos+1 < len(sql) && sql[pos] == '-' && sql[pos+1] == '-'
}

func hasBlockCommentPrefix(sql string, pos int) bool {
	return pos+1 < len(sql) && sql[pos] == '/' && sql[pos+1] == '*'
}

func scanLineComment(sql string, start int) int {
	i := start + 2
	for i < len(sql) && sql[i] != '\n' {
		i++
	}
	return i
}

func scanBlockComment(sql string, start int) int {
	i := start + 2
	for i+1 < len(sql) {
		if sql[i] == '*' && sql[i+1] == '/' {
			return i + 2
		}
		i++
	}
	return len(sql)
}

func scanSingleQuotedString(sql string, start int) int {
	i := start + 1
	for i < len(sql) {
		if sql[i] == '\'' {
			if i+1 < len(sql) && sql[i+1] == '\'' {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return len(sql)
}

func scanDoubleQuotedString(sql string, start int) int {
	i := start + 1
	for i < len(sql) {
		if sql[i] == '"' {
			if i+1 < len(sql) && sql[i+1] == '"' {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return len(sql)
}

func scanBracketIdentifier(sql string, start int) int {
	i := start + 1
	for i < len(sql) {
		if sql[i] == ']' {
			if i+1 < len(sql) && sql[i+1] == ']' {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return len(sql)
}
