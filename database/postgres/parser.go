package postgres

import (
	"fmt"
	"strings"

	"github.com/k0kubun/sqldef/database"
	"github.com/k0kubun/sqldef/parser"
	pgquery "github.com/pganalyze/pg_query_go/v2"
)

type PostgresParser struct {
	parser  database.GenericParser
	testing bool
}

func NewParser() PostgresParser {
	return PostgresParser{
		parser:  database.NewParser(parser.ParserModePostgres),
		testing: false,
	}
}

func (p PostgresParser) Parse(sql string) ([]database.DDLStatement, error) {
	result, err := pgquery.Parse(sql)
	if err != nil {
		return nil, err
	}

	var statements []database.DDLStatement
	for _, rawStmt := range result.Stmts {
		var ddl string
		if rawStmt.StmtLen == 0 {
			ddl = sql[rawStmt.StmtLocation:]
		} else {
			ddl = sql[rawStmt.StmtLocation : rawStmt.StmtLocation+rawStmt.StmtLen]
		}
		ddl = strings.TrimSpace(ddl)

		// First, attempt to parse it with the wrapper of PostgreSQL's parser. If it works, use the result.
		stmt, err := p.parseStmt(rawStmt.Stmt)
		if p.testing && err != nil {
			return nil, err
		}
		if err != nil {
			// Otherwise, fallback to the generic parser. We intend to deprecate this path in the future.
			stmts, err := p.parser.Parse(ddl)
			if err != nil {
				return nil, err
			}

			statements = append(statements, stmts...)
			continue
		}

		statements = append(statements, database.DDLStatement{
			DDL:       ddl,
			Statement: stmt,
		})
	}

	return statements, nil
}

func (p PostgresParser) parseStmt(node *pgquery.Node) (parser.Statement, error) {
	switch stmt := node.Node.(type) {
	case *pgquery.Node_CreateStmt:
		return p.parseCreateStmt(stmt.CreateStmt)
	case *pgquery.Node_ViewStmt:
		return p.parseViewStmt(stmt.ViewStmt)
	case *pgquery.Node_CommentStmt:
		return p.parseCommentStmt(stmt.CommentStmt)
	default:
		return nil, fmt.Errorf("unknown node in parseStmt: %#v", stmt)
	}
}

func (p PostgresParser) parseCreateStmt(stmt *pgquery.CreateStmt) (parser.Statement, error) {
	if stmt.Constraints != nil {
		return nil, fmt.Errorf("unhandled node in parseCreateStmt: %#v", stmt)
	}

	schemaName, tableName, err := p.parseTableName(stmt.Relation)
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
			Qualifier: parser.NewTableIdent(schemaName),
			Name:      parser.NewTableIdent(tableName),
		},
		TableSpec: &parser.TableSpec{
			Columns: columns,
		},
	}, nil
}

func (p PostgresParser) parseViewStmt(stmt *pgquery.ViewStmt) (parser.Statement, error) {
	schemaName, viewName, err := p.parseTableName(stmt.View)
	if err != nil {
		return nil, err
	}

	var definition parser.SelectStatement
	switch node := stmt.Query.Node.(type) {
	case *pgquery.Node_SelectStmt:
		definition, err = p.parseSelectStmt(node.SelectStmt)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown node in parseViewStmt: %#v", node)
	}

	return &parser.DDL{
		Action: parser.CreateViewStr,
		View: &parser.View{
			Action: parser.CreateViewStr,
			Name: parser.TableName{
				Qualifier: parser.NewTableIdent(schemaName),
				Name:      parser.NewTableIdent(viewName),
			},
			Definition: definition,
		},
	}, nil
}

func (p PostgresParser) parseSelectStmt(stmt *pgquery.SelectStmt) (parser.SelectStatement, error) {
	if stmt.DistinctClause != nil || stmt.IntoClause != nil || stmt.WhereClause != nil || stmt.GroupClause != nil || stmt.HavingClause != nil ||
		stmt.WindowClause != nil || stmt.ValuesLists != nil || stmt.SortClause != nil || stmt.LimitOffset != nil || stmt.LimitCount != nil ||
		stmt.LimitOption != 1 || stmt.LockingClause != nil || stmt.WithClause != nil || stmt.Op != 1 || stmt.All != false || stmt.Larg != nil || stmt.Rarg != nil {
		return nil, fmt.Errorf("unhandled node in parseSelectStmt: %#v", stmt)
	}

	var selectExprs parser.SelectExprs
	for _, target := range stmt.TargetList {
		switch node := target.Node.(type) {
		case *pgquery.Node_ResTarget:
			selectExpr, err := p.parseResTarget(node.ResTarget)
			if err != nil {
				return nil, err
			}
			selectExprs = append(selectExprs, selectExpr)
		default:
			return nil, fmt.Errorf("unknown node in parseSelectStmt: %#v", node)
		}
	}

	var fromSchema, fromTable string
	var err error
	switch node := stmt.FromClause[0].Node.(type) {
	case *pgquery.Node_RangeVar:
		fromSchema, fromTable, err = p.parseTableName(node.RangeVar)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown node in parseSelectStmt: %#v", node)
	}

	return &parser.Select{
		SelectExprs: selectExprs,
		From: parser.TableExprs{
			&parser.AliasedTableExpr{
				Expr: parser.TableName{
					Qualifier: parser.NewTableIdent(fromSchema),
					Name:      parser.NewTableIdent(fromTable),
				},
			},
		},
	}, nil
}

func (p PostgresParser) parseResTarget(stmt *pgquery.ResTarget) (parser.SelectExpr, error) {
	expr, err := p.parseExpr(stmt.Val)
	if err != nil {
		return nil, err
	}

	return &parser.AliasedExpr{
		Expr: expr,
		As:   parser.NewColIdent(stmt.Name),
	}, nil
}

func (p PostgresParser) parseExpr(stmt *pgquery.Node) (parser.Expr, error) {
	switch node := stmt.Node.(type) {
	case *pgquery.Node_ColumnRef:
		return &parser.ColName{
			Name: parser.NewColIdent(node.ColumnRef.Fields[0].Node.(*pgquery.Node_String_).String_.Str),
		}, nil
	default:
		return nil, fmt.Errorf("unknown node in parseExpr: %#v", node)
	}
}

func (p PostgresParser) parseCommentStmt(stmt *pgquery.CommentStmt) (parser.Statement, error) {
	var object string
	switch node := stmt.Object.Node.(type) {
	case *pgquery.Node_List:
		var err error
		object, err = p.parseStringList(node.List)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown node in parseColumnStmt: %#v", node)
	}

	return &parser.DDL{
		Action: parser.CommentStr,
		Comment: &parser.Comment{
			ObjectType: pgquery.ObjectType_name[int32(stmt.Objtype)],
			Object:     object,
			Comment:    stmt.Comment,
		},
	}, nil
}

func (p PostgresParser) parseTableName(relation *pgquery.RangeVar) (string, string, error) {
	if relation.Catalogname != "" {
		return "", "", fmt.Errorf("unhandled node in parseTableName: %#v", relation)
	}
	return relation.Schemaname, relation.Relname, nil
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

func (p PostgresParser) parseStringList(list *pgquery.List) (string, error) {
	var objects []string
	for _, node := range list.Items {
		switch n := node.Node.(type) {
		case *pgquery.Node_String_:
			objects = append(objects, n.String_.Str)
		}
	}
	return strings.Join(objects, "."), nil
}
