package postgres

import (
	"fmt"
	"github.com/k0kubun/sqldef/database"
	"github.com/k0kubun/sqldef/parser"
	pgquery "github.com/pganalyze/pg_query_go/v2"
	"strings"
)

type PostgresParser struct {
	parser database.GenericParser
}

func NewParser() PostgresParser {
	return PostgresParser{
		parser: database.NewParser(parser.ParserModePostgres),
	}
}

func (p PostgresParser) Parse(sql string) ([]database.DDLStatement, error) {
	// Attempt to parse sql with PostgreSQL's parser first. If it works, use the result.
	stmts, err := p.parseStmts(sql)
	if err == nil {
		return stmts, nil
	}

	// Otherwise, use the generic parser. We intend to deprecate this path in the future.
	return p.parser.Parse(sql)
}

func (p PostgresParser) parseStmts(sql string) ([]database.DDLStatement, error) {
	result, err := pgquery.Parse(sql)
	if err != nil {
		return nil, err
	}

	var stmts []database.DDLStatement
	for _, rawStmt := range result.Stmts {
		stmt, err := p.parseStmt(rawStmt.Stmt)
		if err != nil {
			return nil, err
		}

		ddl := sql[rawStmt.StmtLocation : rawStmt.StmtLocation+rawStmt.StmtLen]
		ddl = strings.TrimSpace(ddl)
		stmts = append(stmts, database.DDLStatement{
			DDL:       ddl,
			Statement: stmt,
		})
	}

	return stmts, nil
}

func (p PostgresParser) parseStmt(node *pgquery.Node) (parser.Statement, error) {
	switch stmt := node.Node.(type) {
	case *pgquery.Node_CreateStmt:
		return p.parseCreateStmt(stmt.CreateStmt)
	default:
		return nil, fmt.Errorf("unknown node in parseStmt: %#v", stmt)
	}
}

func (p PostgresParser) parseCreateStmt(stmt *pgquery.CreateStmt) (parser.Statement, error) {
	if stmt.Constraints != nil {
		return nil, fmt.Errorf("unhandled node in parseCreateStmt: %#v", stmt)
	}

	tableName, err := p.parseTableName(stmt.Relation)
	if err != nil {
		return nil, err
	}

	var columns []*parser.ColumnDefinition
	for _, elt := range stmt.TableElts {
		switch node := elt.Node.(type) {
		case *pgquery.Node_ColumnDef:
			column, err := p.parseColumnDef(node.ColumnDef)
			if err != nil {
				return nil, err
			}
			columns = append(columns, column)
		default:
			return nil, fmt.Errorf("unknown node in parseCreateStmt: %#v", node)
		}
	}

	return &parser.DDL{
		Action: parser.CreateStr,
		NewName: parser.TableName{
			Name: parser.NewTableIdent(tableName),
		},
		TableSpec: &parser.TableSpec{
			Columns: columns,
		},
	}, nil
}

func (p PostgresParser) parseTableName(relation *pgquery.RangeVar) (string, error) {
	if relation.Schemaname != "" || relation.Catalogname != "" {
		return "", fmt.Errorf("unhandled node in parseTableName: %#v", relation)
	}
	return relation.Relname, nil
}

func (p PostgresParser) parseColumnDef(columnDef *pgquery.ColumnDef) (*parser.ColumnDefinition, error) {
	if columnDef.Constraints != nil || columnDef.Inhcount != 0 || columnDef.Identity != "" || columnDef.Generated != "" || columnDef.Storage != "" || columnDef.CollClause != nil {
		return nil, fmt.Errorf("unhandled node in parseColumnDef: %#v", columnDef)
	}
	typeName, err := p.parseTypeName(columnDef.TypeName)
	if err != nil {
		return nil, err
	}
	return &parser.ColumnDefinition{
		Name: parser.NewColIdent(columnDef.Colname),
		Type: parser.ColumnType{
			Type: typeName,
		},
	}, nil
}

func (p PostgresParser) parseTypeName(node *pgquery.TypeName) (string, error) {
	var typeNames []string
	for _, name := range node.Names {
		if n, ok := name.Node.(*pgquery.Node_String_); ok {
			typeNames = append(typeNames, n.String_.Str)
		} else {
			return "", fmt.Errorf("non-Node_String_ name in parseCreateStmt: %#v", name)
		}
	}

	if len(typeNames) == 1 {
		return typeNames[0], nil
	} else if len(typeNames) == 2 {
		if typeNames[0] == "pg_catalog" {
			if typeNames[1] == "int8" {
				return "bigint", nil
			} else {
				return "", fmt.Errorf("unknown typeName in parseTypeName: %#v", typeNames)
			}
		} else {
			return "", fmt.Errorf("unknown schema in parseTypeName: %#v", typeNames)
		}
	} else {
		return "", fmt.Errorf("unexpected length in parseTypeName: %d", len(typeNames))
	}
}
