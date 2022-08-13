package mysql

import (
	"github.com/k0kubun/pp/v3"
	"github.com/k0kubun/sqldef/database"
	"github.com/k0kubun/sqldef/parser"

	mysqlparser "github.com/pingcap/tidb/parser"
	_ "github.com/pingcap/tidb/types/parser_driver"
)

type MysqlParser struct {
	parser database.GenericParser
}

func NewParser() MysqlParser {
	return MysqlParser{
		parser: database.NewParser(parser.ParserModeMysql),
	}
}

func (p MysqlParser) Parse(sql string) ([]database.DDLStatement, error) {
	if false {
		parse := mysqlparser.New()
		root, _, _ := parse.Parse(sql, "", "")
		pp.Println(root)
	}

	return p.parser.Parse(sql)
}
