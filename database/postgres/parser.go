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
	// Workaround for comments (not needed?)
	//re := regexp.MustCompilePOSIX("^ *--.*")
	//sql = re.ReplaceAllString(sql, "")

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
		if err != nil {
			// Otherwise, fallback to the generic parser. We intend to deprecate this path in the future.
			var stmts []database.DDLStatement
			if !p.testing { // Disable fallback in parser tests
				stmts, err = p.parser.Parse(ddl)
			}
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
	case *pgquery.Node_IndexStmt:
		return p.parseIndexStmt(stmt.IndexStmt)
	case *pgquery.Node_ViewStmt:
		return p.parseViewStmt(stmt.ViewStmt)
	case *pgquery.Node_CommentStmt:
		return p.parseCommentStmt(stmt.CommentStmt)
	case *pgquery.Node_CreateExtensionStmt:
		return p.parseExtensionStmt(stmt.CreateExtensionStmt)
	case *pgquery.Node_AlterTableStmt:
		return p.parseAlterTableStmt(stmt.AlterTableStmt)
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
		Action:  parser.CreateStr,
		NewName: tableName,
		TableSpec: &parser.TableSpec{
			Columns: columns,
			Options: map[string]string{},
		},
	}, nil
}

func (p PostgresParser) parseIndexStmt(stmt *pgquery.IndexStmt) (parser.Statement, error) {
	table, err := p.parseTableName(stmt.Relation)
	if err != nil {
		return nil, err
	}

	var where *parser.Where
	if stmt.WhereClause != nil {
		whereExpr, err := p.parseExpr(stmt.WhereClause)
		if err != nil {
			return nil, err
		}
		where = &parser.Where{
			Type: "where",
			Expr: whereExpr,
		}
	}

	var indexCols []parser.IndexColumn
	for _, indexParam := range stmt.IndexParams {
		indexCol, err := p.parseIndexColumn(indexParam)
		if err != nil {
			return nil, err
		}
		indexCols = append(indexCols, indexCol)
	}

	return &parser.DDL{
		Action:  parser.CreateIndexStr,
		Table:   table,
		NewName: table,
		IndexSpec: &parser.IndexSpec{
			Name:   parser.NewColIdent(stmt.Idxname),
			Type:   parser.NewColIdent(stmt.AccessMethod),
			Unique: stmt.Unique,
			Where:  where,
		},
		IndexCols: indexCols,
	}, nil
}

func (p PostgresParser) parseViewStmt(stmt *pgquery.ViewStmt) (parser.Statement, error) {
	viewName, err := p.parseTableName(stmt.View)
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
			Action:     parser.CreateViewStr,
			Name:       viewName,
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

	var fromTable parser.TableName
	var err error
	switch node := stmt.FromClause[0].Node.(type) {
	case *pgquery.Node_RangeVar:
		fromTable, err = p.parseTableName(node.RangeVar)
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
				Expr:       fromTable,
				TableHints: []string{},
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
	case *pgquery.Node_AArrayExpr:
		var elements parser.ArrayElements
		for _, element := range node.AArrayExpr.Elements {
			node, err := p.parseExpr(element)
			if err != nil {
				return nil, err
			}

			elem, err := p.parseArrayElement(node)
			if err != nil {
				return nil, err
			}
			elements = append(elements, elem)
		}
		return &parser.ArrayConstructor{
			Elements: elements,
		}, nil
	case *pgquery.Node_AConst:
		return p.parseExpr(node.AConst.Val)
	case *pgquery.Node_BoolExpr:
		arg1, err := p.parseExpr(node.BoolExpr.Args[0])
		if err != nil {
			return nil, err
		}

		arg2, err := p.parseExpr(node.BoolExpr.Args[1])
		if err != nil {
			return nil, err
		}

		switch node.BoolExpr.Boolop {
		case pgquery.BoolExprType_AND_EXPR:
			return &parser.AndExpr{
				Left:  arg1,
				Right: arg2,
			}, nil
		default:
			return nil, fmt.Errorf("unexpected boolop: %d", node.BoolExpr.Boolop)
		}
	case *pgquery.Node_CaseExpr:
		caseStmt := stmt.GetCaseExpr()

		caseExpr, err := p.parseExpr(caseStmt.Arg)
		if err != nil {
			return nil, err
		}

		var whenExprs []*parser.When
		for _, arg := range caseStmt.Args {
			caseWhen := arg.Node.(*pgquery.Node_CaseWhen).CaseWhen

			cond, err := p.parseExpr(caseWhen.Expr)
			if err != nil {
				return nil, err
			}

			result, err := p.parseExpr(caseWhen.Result)
			if err != nil {
				return nil, err
			}

			whenExpr := &parser.When{
				Cond: cond,
				Val:  result,
			}
			whenExprs = append(whenExprs, whenExpr)
		}

		var elseExpr parser.Expr
		if caseStmt.Defresult == nil {
			elseExpr = &parser.NullVal{} // normalize
		} else {
			elseExpr, err = p.parseExpr(caseStmt.Defresult)
			if err != nil {
				return nil, err
			}
		}

		return &parser.CaseExpr{
			Expr:  caseExpr,
			Whens: whenExprs,
			Else:  elseExpr,
		}, nil
	case *pgquery.Node_ColumnRef:
		field := node.ColumnRef.Fields[len(node.ColumnRef.Fields)-1] // Ignore table name for easy comparison
		return &parser.ColName{
			Name: parser.NewColIdent(field.Node.(*pgquery.Node_String_).String_.Str),
		}, nil
	case *pgquery.Node_FuncCall:
		var exprs parser.SelectExprs

		for _, arg := range stmt.GetFuncCall().Args {
			expr, err := p.parseExpr(arg)
			if err != nil {
				return nil, err
			}

			exprs = append(exprs, &parser.AliasedExpr{
				Expr: expr,
			})
		}

		return &parser.FuncExpr{
			Name:  parser.NewColIdent(node.FuncCall.Funcname[0].Node.(*pgquery.Node_String_).String_.Str),
			Exprs: exprs,
		}, nil
	case *pgquery.Node_Integer:
		return parser.NewIntVal([]byte(fmt.Sprint(node.Integer.Ival))), nil
	case *pgquery.Node_Null:
		return &parser.NullVal{}, nil
	case *pgquery.Node_NullTest:
		expr, err := p.parseExpr(node.NullTest.Arg)
		if err != nil {
			return nil, err
		}

		switch node.NullTest.Nulltesttype {
		case pgquery.NullTestType_IS_NOT_NULL:
			return &parser.IsExpr{
				Operator: parser.IsNotNullStr,
				Expr:     expr,
			}, nil
		case pgquery.NullTestType_IS_NULL:
			return &parser.IsExpr{
				Operator: parser.IsNullStr,
				Expr:     expr,
			}, nil
		default:
			return nil, fmt.Errorf("unexpected nulltesttype: %d", node.NullTest.Nulltesttype)
		}
	case *pgquery.Node_String_:
		return parser.NewStrVal([]byte(node.String_.Str)), nil
	case *pgquery.Node_TypeCast:
		expr, err := p.parseExpr(node.TypeCast.Arg)
		if err != nil {
			return nil, err
		}
		return &parser.CollateExpr{ // compatibility with the legacy parser, but there'd be a better node
			Expr: expr,
		}, nil
	default:
		return nil, fmt.Errorf("unknown node in parseExpr: %#v", node)
	}
}

func (p PostgresParser) parseIndexColumn(stmt *pgquery.Node) (parser.IndexColumn, error) {
	switch node := stmt.Node.(type) {
	case *pgquery.Node_IndexElem:
		if node.IndexElem.Expr != nil {
			expr, err := p.parseExpr(node.IndexElem.Expr)
			if err != nil {
				return parser.IndexColumn{}, err
			}

			return parser.IndexColumn{
				Column: parser.NewColIdent(parser.String(expr)),
			}, nil
		} else {
			var direction string
			switch node.IndexElem.Ordering {
			case pgquery.SortByDir_SORTBY_ASC:
				direction = parser.AscScr
			case pgquery.SortByDir_SORTBY_DESC:
				direction = parser.DescScr
			case pgquery.SortByDir_SORTBY_DEFAULT:
				direction = ""
			default:
				return parser.IndexColumn{}, fmt.Errorf("unexpected direction in parseIndexColumn: %d", node.IndexElem.Ordering)
			}
			return parser.IndexColumn{
				Column:    parser.NewColIdent(node.IndexElem.Name),
				Direction: direction,
			}, nil
		}
	default:
		return parser.IndexColumn{}, fmt.Errorf("unexpected node type in parseIndexColumn: %#v", stmt)
	}
}

func (p PostgresParser) parseArrayElement(node parser.Expr) (parser.ArrayElement, error) {
	switch node := node.(type) {
	case *parser.SQLVal:
		return node, nil
	case *parser.CollateExpr:
		return p.parseArrayElement(node.Expr)
	default:
		return nil, fmt.Errorf("unknown expr in parseArrayElement: %#v", node)
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

func (p PostgresParser) parseTableName(relation *pgquery.RangeVar) (parser.TableName, error) {
	if relation.Catalogname != "" {
		return parser.TableName{}, fmt.Errorf("unhandled node in parseTableName: %#v", relation)
	}
	return parser.TableName{
		Qualifier: parser.NewTableIdent(relation.Schemaname),
		Name:      parser.NewTableIdent(relation.Relname),
	}, nil
}

func (p PostgresParser) parseExtensionStmt(stmt *pgquery.CreateExtensionStmt) (parser.Statement, error) {
	return &parser.DDL{
		Action: parser.CreateExtensionStr,
		Extension: &parser.Extension{
			Name: stmt.Extname,
		},
	}, nil
}

func (p PostgresParser) parseAlterTableStmt(stmt *pgquery.AlterTableStmt) (parser.Statement, error) {
	tableName, err := p.parseTableName(stmt.Relation)
	if err != nil {
		return nil, err
	}

	if len(stmt.Cmds) > 1 {
		return nil, fmt.Errorf("multiple actions are not supported in parseAlterTableStmt")
	}

	switch node := stmt.Cmds[0].Node.(*pgquery.Node_AlterTableCmd).AlterTableCmd.Def.Node.(type) {
	case *pgquery.Node_Constraint:
		return p.parseConstraint(node.Constraint, tableName)
	default:
		return nil, fmt.Errorf("unhandled node in parseAlterTableStmt: %#v", node)
	}
}

func (p PostgresParser) parseConstraint(constraint *pgquery.Constraint, tableName parser.TableName) (parser.Statement, error) {
	switch constraint.Contype {
	case pgquery.ConstrType_CONSTR_UNIQUE:
		cols := make([]parser.IndexColumn, len(constraint.Keys))
		for i, key := range constraint.Keys {
			cols[i] = parser.IndexColumn{
				Column:    parser.NewColIdent(key.Node.(*pgquery.Node_String_).String_.Str),
				Direction: "asc",
			}
		}
		return &parser.DDL{
			Action:  parser.AddIndexStr,
			Table:   tableName,
			NewName: tableName,
			IndexSpec: &parser.IndexSpec{
				Name:       parser.NewColIdent(constraint.Conname),
				Constraint: true,
				Unique:     true,
				ConstraintOptions: &parser.ConstraintOptions{
					Deferrable:        constraint.Deferrable,
					InitiallyDeferred: constraint.Initdeferred,
				},
			},
			IndexCols: cols,
		}, nil
	default:
		return nil, fmt.Errorf("unhandled constraint type in parseAlterTableStmt: %d", constraint.Contype)
	}
}

func (p PostgresParser) parseColumnDef(columnDef *pgquery.ColumnDef) (*parser.ColumnDefinition, error) {
	if columnDef.Inhcount != 0 || columnDef.Identity != "" || columnDef.Generated != "" || columnDef.Storage != "" || columnDef.CollClause != nil {
		return nil, fmt.Errorf("unhandled node in parseColumnDef: %#v", columnDef)
	}

	columnType, err := p.parseTypeName(columnDef.TypeName)
	if err != nil {
		return nil, err
	}

	for _, columnConstraint := range columnDef.Constraints {
		constraint := columnConstraint.Node.(*pgquery.Node_Constraint).Constraint
		switch constraint.Contype {
		case pgquery.ConstrType_CONSTR_NOTNULL:
			columnType.NotNull = parser.NewBoolVal(true)
		default:
			return nil, fmt.Errorf("unhandled contype: %d", constraint.Contype)
		}
	}

	return &parser.ColumnDefinition{
		Name: parser.NewColIdent(columnDef.Colname),
		Type: columnType,
	}, nil
}

func (p PostgresParser) parseTypeName(node *pgquery.TypeName) (parser.ColumnType, error) {
	columnType := parser.ColumnType{}
	if node.TypeOid != 0 || node.Setof != false || node.PctType != false || node.Typemod != -1 || node.ArrayBounds != nil {
		return columnType, fmt.Errorf("unhandled node in parseTypeName: %#v", node)
	}

	var typeNames []string
	for _, name := range node.Names {
		if n, ok := name.Node.(*pgquery.Node_String_); ok {
			typeNames = append(typeNames, n.String_.Str)
		} else {
			return columnType, fmt.Errorf("non-Node_String_ name in parseCreateStmt: %#v", name)
		}
	}

	if len(typeNames) == 1 {
		columnType.Type = typeNames[0]
	} else if len(typeNames) == 2 {
		if typeNames[0] == "pg_catalog" {
			switch typeNames[1] {
			case "int4":
				columnType.Type = "integer"
			case "int8":
				columnType.Type = "bigint"
			case "varchar", "interval": // TODO: use this pattern more, fixing failed tests as well
				columnType.Type = typeNames[1]
			default:
				return columnType, fmt.Errorf("unhandled type in parseTypeName: %s", typeNames[1])
			}
		} else {
			return columnType, fmt.Errorf("unknown schema in parseTypeName: %#v", typeNames)
		}
	} else {
		return columnType, fmt.Errorf("unexpected length in parseTypeName: %d", len(typeNames))
	}

	if node.Typmods != nil {
		for _, mod := range node.Typmods {
			modExpr, err := p.parseExpr(mod)
			if err != nil {
				return columnType, err
			}

			switch expr := modExpr.(type) {
			case *parser.SQLVal:
				if expr.Type == parser.IntVal {
					columnType.Length = expr
				} else {
					return columnType, fmt.Errorf("unexpected SQLVal type in parseTypeName: %d", expr.Type)
				}
			default:
				return columnType, fmt.Errorf("unexpected typmod type in parseTypeName: %#v", expr)
			}
		}
	}

	return columnType, nil
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
