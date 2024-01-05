package database

import (
	"regexp"
	"strings"

	"github.com/sqldef/sqldef/parser"
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
		ddl, _ = parser.SplitMarginComments(ddl)
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
			ddl, _ = parser.SplitMarginComments(ddl)
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
