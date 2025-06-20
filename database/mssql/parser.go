package mssql

import (
	"regexp"
	"strings"

	"github.com/sqldef/sqldef/v2/database"
	"github.com/sqldef/sqldef/v2/parser"
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
		s := strings.TrimSpace(batch)
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
