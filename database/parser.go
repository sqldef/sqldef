package database

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/sqldef/sqldef/v3/parser"
)

// A tuple of an original DDL and a Statement
type DDLStatement struct {
	DDL       string
	Statement parser.Statement
}

type Parser interface {
	Parse(sql string) ([]DDLStatement, error)
}

type GenericParser struct {
	mode parser.ParserMode
}

func NewParser(mode parser.ParserMode) GenericParser {
	return GenericParser{
		mode: mode,
	}
}

func (p GenericParser) Parse(sql string) ([]DDLStatement, error) {
	ddls, err := p.splitDDLs(sql)
	if err != nil {
		return nil, err
	}

	var result []DDLStatement
	for _, ddl := range ddls {
		ddl = trimMarginComments(ddl)
		stmt, err := parser.ParseDDL(ddl, p.mode)
		if err != nil {
			return result, err
		}
		result = append(result, DDLStatement{DDL: ddl, Statement: stmt})
	}
	return result, nil
}

func (p GenericParser) splitDDLs(str string) ([]string, error) {
	re := regexp.MustCompilePOSIX("^--.*")
	str = re.ReplaceAllString(str, "")

	ddls := strings.Split(str, ";")
	var result []string

	for len(ddls) > 0 {
		// Right now, the parser isn't capable of splitting statements by itself.
		// So we just attempt parsing until it succeeds. I'll let the parser do it in the future.
		var ddl string
		var err error
		i := 1
		for {
			ddl = strings.Join(ddls[0:i], ";")
			ddl = trimMarginComments(ddl)
			ddl = strings.TrimSuffix(ddl, ";")
			if ddl == "" {
				break
			}
			_, err = parser.ParseDDL(ddl, p.mode)
			if err == nil || i == len(ddls) {
				break
			}
			i++
		}

		if err != nil {
			return result, err
		}
		if ddl != "" {
			result = append(result, ddl)
		}

		if i < len(ddls) {
			ddls = ddls[i:] // remove scanned tokens
		} else {
			break
		}
	}
	return result, nil
}

// trimMarginComments pulls out any leading or trailing comments from a raw sql query.
// This function also trims leading (if there's a comment) and trailing whitespace.
func trimMarginComments(sql string) string {
	trailingStart := trailingCommentStart(sql)
	leadingEnd := leadingCommentEnd(sql[:trailingStart])
	return strings.TrimFunc(sql[leadingEnd:trailingStart], unicode.IsSpace)
}

// trailingCommentStart returns the first index of trailing comments.
// If there are no trailing comments, returns the length of the input string.
// NOTE: MySQL version comments (/*!NNNNN ... */) are NOT treated as comments
// because they contain SQL code that should be executed.
func trailingCommentStart(text string) (start int) {
	hasComment := false
	reducedLen := len(text)
	for reducedLen > 0 {
		// Eat up any whitespace. Leading whitespace will be considered part of
		// the trailing comments.
		nextReducedLen := strings.LastIndexFunc(text[:reducedLen], isNonSpace) + 1
		if nextReducedLen == 0 {
			break
		}
		reducedLen = nextReducedLen
		if reducedLen < 4 || text[reducedLen-2:reducedLen] != "*/" {
			break
		}

		// Find the beginning of the comment
		startCommentPos := strings.LastIndex(text[:reducedLen-2], "/*")
		if startCommentPos < 0 {
			// Badly formatted sql :/
			break
		}

		// Check if this is a MySQL version comment (/*!NNNNN ... */)
		// These are NOT actual comments - they contain SQL code that should be executed
		// when the server version is >= NNNNN
		commentStart := text[startCommentPos:]
		if len(commentStart) >= 3 && commentStart[2] == '!' {
			// This is a MySQL version comment, don't treat it as a trailing comment
			break
		}

		hasComment = true
		reducedLen = startCommentPos
	}

	if hasComment {
		return reducedLen
	}
	return len(text)
}

// leadingCommentEnd returns the first index after all leading comments, or
// 0 if there are no leading comments.
func leadingCommentEnd(text string) (end int) {
	hasComment := false
	pos := 0
	for pos < len(text) {
		// Eat up any whitespace. Trailing whitespace will be considered part of
		// the leading comments.
		nextVisibleOffset := strings.IndexFunc(text[pos:], isNonSpace)
		if nextVisibleOffset < 0 {
			break
		}
		pos += nextVisibleOffset
		remainingText := text[pos:]

		// Found visible characters. Look for '/*' at the beginning
		// and '*/' somewhere after that.
		if len(remainingText) < 4 || remainingText[:2] != "/*" {
			break
		}
		commentLength := 4 + strings.Index(remainingText[2:], "*/")
		if commentLength < 4 {
			// Missing end comment :/
			break
		}

		hasComment = true
		pos += commentLength
	}

	if hasComment {
		return pos
	}
	return 0
}

func isNonSpace(r rune) bool {
	return !unicode.IsSpace(r)
}
