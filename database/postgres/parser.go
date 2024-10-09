package postgres

import (
	"fmt"
	"strings"

	pgquery "github.com/pganalyze/pg_query_go/v5"
	"github.com/sqldef/sqldef/database"
	"github.com/sqldef/sqldef/parser"
	go_pgquery "github.com/wasilibs/go-pgquery"
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

	result, err := go_pgquery.Parse(sql)
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
	case *pgquery.Node_CreateSchemaStmt:
		return p.parseCreateSchemaStmt(stmt.CreateSchemaStmt)
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
	var indexes []*parser.IndexDefinition
	var foreignKeys []*parser.ForeignKeyDefinition
	var checks []*parser.CheckDefinition
	for _, elt := range stmt.TableElts {
		switch node := elt.Node.(type) {
		case *pgquery.Node_ColumnDef:
			column, foreignKey, err := p.parseColumnDef(node.ColumnDef, tableName)
			if err != nil {
				return nil, err
			}
			columns = append(columns, column)
			if foreignKey != nil {
				foreignKeys = append(foreignKeys, foreignKey)
			}
		case *pgquery.Node_Constraint:
			var indexCols []parser.IndexColumn
			for _, key := range node.Constraint.Keys {
				indexCol := parser.IndexColumn{
					Column:    parser.NewColIdent(key.Node.(*pgquery.Node_String_).String_.Sval),
					Direction: "asc",
				}
				indexCols = append(indexCols, indexCol)
			}
			switch node.Constraint.Contype {
			case pgquery.ConstrType_CONSTR_PRIMARY:
				index := &parser.IndexDefinition{
					Info: &parser.IndexInfo{
						Type:      "primary key",
						Name:      parser.NewColIdent(node.Constraint.Conname),
						Unique:    true,
						Primary:   true,
						Clustered: true,
					},
					Columns: indexCols,
					Options: []*parser.IndexOption{},
				}
				indexes = append(indexes, index)
			case pgquery.ConstrType_CONSTR_UNIQUE:
				index := &parser.IndexDefinition{
					Info: &parser.IndexInfo{
						Type:   "unique key",
						Name:   parser.NewColIdent(node.Constraint.Conname),
						Unique: true,
					},
					Columns: indexCols,
					Options: []*parser.IndexOption{},
					ConstraintOptions: &parser.ConstraintOptions{
						Deferrable:        node.Constraint.Deferrable,
						InitiallyDeferred: node.Constraint.Initdeferred,
					},
				}
				indexes = append(indexes, index)
			case pgquery.ConstrType_CONSTR_FOREIGN:
				fk, err := p.parseForeignKey(node.Constraint)
				if err != nil {
					return nil, err
				}
				foreignKeys = append(foreignKeys, fk)
			case pgquery.ConstrType_CONSTR_CHECK:
				expr, err := p.parseExpr(node.Constraint.RawExpr)
				if err != nil {
					return nil, err
				}
				check := &parser.CheckDefinition{
					Where:          *parser.NewWhere(parser.WhereStr, expr),
					ConstraintName: parser.NewColIdent(node.Constraint.Conname),
				}
				checks = append(checks, check)
			default:
				return nil, fmt.Errorf("unknown Constraint type: %#v", node)
			}
		default:
			return nil, fmt.Errorf("unknown node in parseCreateStmt: %#v", node)
		}
	}

	return &parser.DDL{
		Action:  parser.CreateTable,
		NewName: tableName,
		TableSpec: &parser.TableSpec{
			Columns:     columns,
			Indexes:     indexes,
			ForeignKeys: foreignKeys,
			Checks:      checks,
			Options:     map[string]string{},
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
		Action:  parser.CreateIndex,
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
		Action: parser.CreateView,
		View: &parser.View{
			Type:       parser.ViewStr,
			Name:       viewName,
			Definition: definition,
		},
	}, nil
}

func (p PostgresParser) parseSelectStmt(stmt *pgquery.SelectStmt) (parser.SelectStatement, error) {
	unhandled := stmt.IntoClause != nil ||
		stmt.WindowClause != nil ||
		stmt.SortClause != nil ||
		stmt.ValuesLists != nil ||
		stmt.LimitOffset != nil ||
		stmt.LimitCount != nil ||
		stmt.LimitOption != 1 ||
		stmt.LockingClause != nil ||
		stmt.WithClause != nil ||
		stmt.Op != 1 ||
		stmt.All ||
		stmt.Larg != nil ||
		stmt.Rarg != nil
	if unhandled {
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
	var aliasName string
	if len(stmt.FromClause) == 0 {
		fromTable = parser.TableName{
			Name:   parser.NewTableIdent(""),
			Schema: parser.NewTableIdent(""),
		}
	} else {
		var err error
		switch node := stmt.FromClause[0].Node.(type) {
		case *pgquery.Node_RangeVar:
			fromTable, err = p.parseTableName(node.RangeVar)
			if err != nil {
				return nil, err
			}
			if node.RangeVar.Alias != nil {
				aliasName = node.RangeVar.Alias.Aliasname
			}
		default:
			return nil, fmt.Errorf("unknown node in parseSelectStmt: %#v", node)
		}
	}

	var distinct string
	if stmt.DistinctClause != nil {
		distinct = parser.DistinctStr
	}

	var where *parser.Where
	if stmt.WhereClause != nil {
		expr, err := p.parseExpr(stmt.WhereClause)
		if err != nil {
			return nil, err
		}
		where = &parser.Where{
			Type: parser.WhereStr,
			Expr: expr,
		}
	}

	var groupBy parser.GroupBy
	if stmt.GroupClause != nil {
		for _, group := range stmt.GroupClause {
			expr, err := p.parseExpr(group)
			if err != nil {
				return nil, err
			}
			groupBy = append(groupBy, expr)
		}
	}

	var having *parser.Where
	if stmt.HavingClause != nil {
		expr, err := p.parseExpr(stmt.HavingClause)
		if err != nil {
			return nil, err
		}
		having = &parser.Where{
			Type: parser.HavingStr,
			Expr: expr,
		}
	}

	return &parser.Select{
		SelectExprs: selectExprs,
		Distinct:    distinct,
		From: parser.TableExprs{
			&parser.AliasedTableExpr{
				Expr:       fromTable,
				TableHints: []string{},
				As:         parser.NewTableIdent(aliasName),
			},
		},
		Where:   where,
		GroupBy: groupBy,
		Having:  having,
	}, nil
}

func (p PostgresParser) parseResTarget(stmt *pgquery.ResTarget) (parser.SelectExpr, error) {
	if node, ok := stmt.Val.Node.(*pgquery.Node_ColumnRef); ok {
		fields := node.ColumnRef.Fields
		fieldsLen := len(fields)
		column := fields[fieldsLen-1]
		if _, ok := column.Node.(*pgquery.Node_AStar); ok {
			var tableName string
			var schemaName string
			if fieldsLen >= 2 {
				tableField := fields[fieldsLen-2]
				tableNode, ok := tableField.Node.(*pgquery.Node_String_)
				if !ok {
					return nil, fmt.Errorf("Invalid table field node type: %#v", tableField)
				}
				tableName = tableNode.String_.Sval

				if fieldsLen >= 3 {
					schemaField := fields[fieldsLen-3]
					schemaNode, ok := schemaField.Node.(*pgquery.Node_String_)
					if !ok {
						return nil, fmt.Errorf("Invalid schema field node type: %#v", schemaField)
					}
					schemaName = schemaNode.String_.Sval
				}
			}

			return &parser.StarExpr{
				TableName: parser.TableName{
					Name:   parser.NewTableIdent(tableName),
					Schema: parser.NewTableIdent(schemaName),
				},
			}, nil
		}
	}

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
		switch cNode := node.AConst.Val.(type) {
		case *pgquery.A_Const_Ival:
			return parser.NewIntVal([]byte(fmt.Sprint(cNode.Ival.Ival))), nil
		case *pgquery.A_Const_Fval:
			return parser.NewFloatVal([]byte(fmt.Sprint(cNode.Fval.Fval))), nil
		case *pgquery.A_Const_Boolval:
			return parser.NewBoolVal(cNode.Boolval.Boolval), nil
		case *pgquery.A_Const_Sval:
			return parser.NewStrVal([]byte(cNode.Sval.Sval)), nil
		case *pgquery.A_Const_Bsval:
			return parser.NewBitVal([]byte(cNode.Bsval.Bsval)), nil
		case nil:
			return &parser.NullVal{}, nil
		default:
			return nil, fmt.Errorf("unknown AConst val type in parseExpr: %#v", cNode)
		}
	case *pgquery.Node_BoolExpr:
		arg1, err := p.parseExpr(node.BoolExpr.Args[0])
		if err != nil {
			return nil, err
		}

		if node.BoolExpr.Boolop == pgquery.BoolExprType_NOT_EXPR {
			return &parser.NotExpr{
				Expr: arg1,
			}, nil
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
		case pgquery.BoolExprType_OR_EXPR:
			return &parser.OrExpr{
				Left:  arg1,
				Right: arg2,
			}, nil
		default:
			return nil, fmt.Errorf("unexpected boolop: %d", node.BoolExpr.Boolop)
		}
	case *pgquery.Node_CaseExpr:
		caseStmt := stmt.GetCaseExpr()

		var caseExpr parser.Expr
		var err error
		if caseStmt.Arg != nil {
			caseExpr, err = p.parseExpr(caseStmt.Arg)
			if err != nil {
				return nil, err
			}
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
			Name: parser.NewColIdent(field.Node.(*pgquery.Node_String_).String_.Sval),
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
		var tableName string
		var funcName string
		switch len(node.FuncCall.Funcname) {
		case 1:
			tableName = ""
			funcName = node.FuncCall.Funcname[0].Node.(*pgquery.Node_String_).String_.Sval
		case 2:
			tableName = node.FuncCall.Funcname[0].Node.(*pgquery.Node_String_).String_.Sval
			funcName = node.FuncCall.Funcname[1].Node.(*pgquery.Node_String_).String_.Sval
		default:
			return nil, fmt.Errorf("unexpected number of funcname: %#v", node.FuncCall.Funcname)
		}

		return &parser.FuncExpr{
			Qualifier: parser.NewTableIdent(tableName),
			Name:      parser.NewColIdent(funcName),
			Exprs:     exprs,
		}, nil
	case *pgquery.Node_Integer:
		return parser.NewIntVal([]byte(fmt.Sprint(node.Integer.Ival))), nil
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
		return parser.NewStrVal([]byte(node.String_.Sval)), nil
	case *pgquery.Node_TypeCast:
		expr, err := p.parseExpr(node.TypeCast.Arg)
		if err != nil {
			return nil, err
		}
		columnType, err := p.parseTypeName(node.TypeCast.TypeName)
		if err != nil {
			return nil, err
		}
		if shouldDeleteTypeCast(node.TypeCast.Arg, columnType) {
			return expr, nil
		} else {
			typeName := columnType.Type
			if columnType.Array {
				typeName += "[]"
			}
			return &parser.CastExpr{
				Type: &parser.ConvertType{
					Type:    typeName,
					Length:  columnType.Length,
					Scale:   columnType.Scale,
					Charset: columnType.Charset,
				},
				Expr: expr,
			}, nil
		}
	case *pgquery.Node_SqlvalueFunction:
		switch node.SqlvalueFunction.Op {
		case pgquery.SQLValueFunctionOp_SVFOP_CURRENT_TIMESTAMP:
			return &parser.SQLVal{
				Type: parser.ValArg,
				Val:  []byte("current_timestamp"),
			}, nil
		case pgquery.SQLValueFunctionOp_SVFOP_CURRENT_TIME:
			return &parser.SQLVal{
				Type: parser.ValArg,
				Val:  []byte("current_time"),
			}, nil
		case pgquery.SQLValueFunctionOp_SVFOP_CURRENT_DATE:
			return &parser.SQLVal{
				Type: parser.ValArg,
				Val:  []byte("current_date"),
			}, nil
		default:
			return nil, fmt.Errorf("unexpected SqlvalueFunction: %#v", node)
		}
	case *pgquery.Node_AExpr:
		opNode, ok := node.AExpr.GetName()[0].Node.(*pgquery.Node_String_)
		if !ok {
			return nil, fmt.Errorf("unexpected AExpr operation node: %#v", node)
		}

		// Convert lower case for compatibility with legacy parser
		op := strings.ToLower(opNode.String_.Sval)

		switch node.AExpr.Kind {
		case pgquery.A_Expr_Kind_AEXPR_OP,
			pgquery.A_Expr_Kind_AEXPR_LIKE,
			pgquery.A_Expr_Kind_AEXPR_ILIKE,
			pgquery.A_Expr_Kind_AEXPR_OP_ALL,
			pgquery.A_Expr_Kind_AEXPR_OP_ANY:
			left, err := p.parseExpr(node.AExpr.GetLexpr())
			if err != nil {
				return nil, err
			}
			right, err := p.parseExpr(node.AExpr.GetRexpr())
			if err != nil {
				return nil, err
			}
			return &parser.ComparisonExpr{
				Operator: op,
				Left:     left,
				Right:    right,
				All:      node.AExpr.Kind == pgquery.A_Expr_Kind_AEXPR_OP_ALL,
				Any:      node.AExpr.Kind == pgquery.A_Expr_Kind_AEXPR_OP_ANY,
			}, nil
		default:
			return nil, fmt.Errorf("unknown AExpr kind in parseExpr: %#v", node.AExpr)
		}
	case *pgquery.Node_CoalesceExpr:
		var selectExprs parser.SelectExprs
		for _, arg := range node.CoalesceExpr.Args {
			expr, err := p.parseExpr(arg)
			if err != nil {
				return nil, err
			}
			selectExprs = append(selectExprs, &parser.AliasedExpr{
				Expr: expr,
			})
		}
		return &parser.FuncExpr{
			Name:  parser.NewColIdent("coalesce"),
			Exprs: selectExprs,
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
	case *parser.CastExpr:
		return node, nil
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
		Action: parser.CommentOn,
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
		Schema: parser.NewTableIdent(relation.Schemaname),
		Name:   parser.NewTableIdent(relation.Relname),
	}, nil
}

func (p PostgresParser) parseExtensionStmt(stmt *pgquery.CreateExtensionStmt) (parser.Statement, error) {
	return &parser.DDL{
		Action: parser.CreateExtension,
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
				Column:    parser.NewColIdent(key.Node.(*pgquery.Node_String_).String_.Sval),
				Direction: "asc",
			}
		}
		return &parser.DDL{
			Action:  parser.AddIndex,
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
	case pgquery.ConstrType_CONSTR_FOREIGN:
		fk, err := p.parseForeignKey(constraint)
		if err != nil {
			return nil, err
		}
		return &parser.DDL{
			Action:     parser.AddForeignKey,
			Table:      tableName,
			NewName:    tableName,
			ForeignKey: fk,
		}, nil
	default:
		return nil, fmt.Errorf("unhandled constraint type in parseAlterTableStmt: %d", constraint.Contype)
	}
}

func (p PostgresParser) parseForeignKey(constraint *pgquery.Constraint) (*parser.ForeignKeyDefinition, error) {
	idxCols := make([]parser.ColIdent, len(constraint.FkAttrs))
	for i, fkAttr := range constraint.FkAttrs {
		v := fkAttr.Node.(*pgquery.Node_String_).String_.Sval
		idxCols[i] = parser.NewColIdent(v)
	}
	refCols := make([]parser.ColIdent, len(constraint.PkAttrs))
	for i, pkAttr := range constraint.PkAttrs {
		v := pkAttr.Node.(*pgquery.Node_String_).String_.Sval
		refCols[i] = parser.NewColIdent(v)
	}

	refName, err := p.parseTableName(constraint.Pktable)
	if err != nil {
		return nil, err
	}
	return &parser.ForeignKeyDefinition{
		ConstraintName:   parser.NewColIdent(constraint.Conname),
		IndexColumns:     idxCols,
		ReferenceColumns: refCols,
		ReferenceName:    refName,
		OnDelete:         p.parseFkAction(constraint.FkDelAction),
		OnUpdate:         p.parseFkAction(constraint.FkUpdAction),
		ConstraintOptions: &parser.ConstraintOptions{
			Deferrable:        constraint.Deferrable,
			InitiallyDeferred: constraint.Initdeferred,
		},
	}, nil
}

func (p PostgresParser) parseFkAction(action string) parser.ColIdent {
	// https://github.com/pganalyze/pg_query_go/blob/v2.2.0/parser/include/nodes/parsenodes.h#L2145-L2149C23
	switch action {
	case "a":
		// pgquery cannot distinguish between unspecified action and no action.
		// Empty for no action to match existing behavior.
		return parser.NewColIdent("")
	case "r":
		return parser.NewColIdent("RESTRICT")
	case "c":
		return parser.NewColIdent("CASCADE")
	case "n":
		return parser.NewColIdent("SET NULL")
	case "d":
		return parser.NewColIdent("SET DEFAULT")
	default:
		return parser.NewColIdent("")
	}
}

func (p PostgresParser) parseColumnDef(columnDef *pgquery.ColumnDef, tableName parser.TableName) (*parser.ColumnDefinition, *parser.ForeignKeyDefinition, error) {
	if columnDef.Inhcount != 0 || columnDef.Identity != "" || columnDef.Generated != "" || columnDef.Storage != "" || columnDef.CollClause != nil {
		return nil, nil, fmt.Errorf("unhandled node in parseColumnDef: %#v", columnDef)
	}

	columnType, err := p.parseTypeName(columnDef.TypeName)
	if err != nil {
		return nil, nil, err
	}

	var foreignKey *parser.ForeignKeyDefinition

	for _, columnConstraint := range columnDef.Constraints {
		constraint := columnConstraint.Node.(*pgquery.Node_Constraint).Constraint
		switch constraint.Contype {
		case pgquery.ConstrType_CONSTR_NULL:
			columnType.NotNull = parser.NewBoolVal(false)
		case pgquery.ConstrType_CONSTR_NOTNULL:
			columnType.NotNull = parser.NewBoolVal(true)
		case pgquery.ConstrType_CONSTR_DEFAULT:
			defaultValue, err := p.parseDefaultValue(constraint.RawExpr)
			if err != nil {
				return nil, nil, err
			}
			columnType.Default = defaultValue
		case pgquery.ConstrType_CONSTR_CHECK:
			check, err := p.parseCheckConstraint(constraint)
			if err != nil {
				return nil, nil, err
			}
			columnType.Check = check
			if constraint.Conname == "" {
				name, truncated := p.absentConstraintName(tableName.Name.String(), columnDef.Colname, "check")
				if truncated {
					check.ConstraintName = parser.NewColIdent(name)
				}
			}
		case pgquery.ConstrType_CONSTR_PRIMARY:
			columnType.KeyOpt = parser.ColumnKeyOption(1)
		case pgquery.ConstrType_CONSTR_UNIQUE:
			columnType.KeyOpt = parser.ColumnKeyOption(3)
		case pgquery.ConstrType_CONSTR_FOREIGN:
			foreignKey, err = p.parseForeignKey(constraint)
			if err != nil {
				return nil, nil, err
			}
			foreignKey.IndexColumns = []parser.ColIdent{parser.NewColIdent(columnDef.Colname)}
			if constraint.Conname == "" {
				name, _ := p.absentConstraintName(tableName.Name.String(), columnDef.Colname, "fkey")
				foreignKey.ConstraintName = parser.NewColIdent(name)
			}
		case pgquery.ConstrType_CONSTR_ATTR_DEFERRABLE:
			foreignKey.ConstraintOptions.Deferrable = true
		case pgquery.ConstrType_CONSTR_ATTR_NOT_DEFERRABLE:
			foreignKey.ConstraintOptions.Deferrable = false
		case pgquery.ConstrType_CONSTR_ATTR_DEFERRED:
			foreignKey.ConstraintOptions.InitiallyDeferred = true
		case pgquery.ConstrType_CONSTR_ATTR_IMMEDIATE:
			foreignKey.ConstraintOptions.InitiallyDeferred = false
		case pgquery.ConstrType_CONSTR_GENERATED:
			expr, err := p.parseExpr(constraint.RawExpr)
			if err != nil {
				return nil, nil, err
			}
			columnType.Generated = &parser.GeneratedColumn{
				Expr: expr,
				// Postgres only supports stored generated column
				GeneratedType: "STORED",
			}
		default:
			return nil, nil, fmt.Errorf("unhandled contype: %d", constraint.Contype)
		}
	}

	return &parser.ColumnDefinition{
		Name: parser.NewColIdent(columnDef.Colname),
		Type: columnType,
	}, foreignKey, nil
}

func (p PostgresParser) absentConstraintName(tableName, columnName, suffix string) (string, bool) {
	if name := fmt.Sprintf("%s_%s_%s", tableName, columnName, suffix); len(name) <= 63 {
		return name, false
	}

	var tableThreshold, columnThreshold = 33 - len(suffix), 28
	var maxSum = tableThreshold + columnThreshold

	if len(tableName) <= tableThreshold {
		columnName = columnName[:maxSum-len(tableName)]
	} else if len(columnName) <= columnThreshold {
		tableName = tableName[:maxSum-len(columnName)]
	} else {
		tableName = tableName[:tableThreshold]
		columnName = columnName[:columnThreshold]
	}

	return fmt.Sprintf("%s_%s_%s", tableName, columnName, suffix), true
}

func (p PostgresParser) parseDefaultValue(rawExpr *pgquery.Node) (*parser.DefaultDefinition, error) {
	node, err := p.parseExpr(rawExpr)
	if err != nil {
		return nil, err
	}
	switch expr := node.(type) {
	case *parser.NullVal:
		return &parser.DefaultDefinition{
			ValueOrExpression: parser.DefaultValueOrExpression{
				Value: parser.NewValArg([]byte("null")),
			},
		}, nil
	case *parser.SQLVal:
		return &parser.DefaultDefinition{
			ValueOrExpression: parser.DefaultValueOrExpression{
				Value: expr,
			},
		}, nil
	case *parser.BoolVal:
		return &parser.DefaultDefinition{
			ValueOrExpression: parser.DefaultValueOrExpression{
				Value: parser.NewBoolSQLVal(bool(*expr)),
			},
		}, nil
	case *parser.ArrayConstructor:
		return &parser.DefaultDefinition{
			ValueOrExpression: parser.DefaultValueOrExpression{
				Expr: expr,
			},
		}, nil
	case *parser.CastExpr:
		switch castExpr := expr.Expr.(type) {
		case *parser.SQLVal:
			return &parser.DefaultDefinition{
				ValueOrExpression: parser.DefaultValueOrExpression{
					Value: castExpr,
				},
			}, nil
		case *parser.CastExpr, *parser.ArrayConstructor:
			return &parser.DefaultDefinition{
				ValueOrExpression: parser.DefaultValueOrExpression{
					Expr: castExpr,
				},
			}, nil
		default:
			return nil, fmt.Errorf("unhandled default CastExpr node: %#v", castExpr)
		}
	case *parser.CollateExpr:
		switch expr := expr.Expr.(type) {
		case *parser.SQLVal:
			return &parser.DefaultDefinition{
				ValueOrExpression: parser.DefaultValueOrExpression{
					Value: expr,
				},
			}, nil
		case *parser.ArrayConstructor:
			return &parser.DefaultDefinition{
				ValueOrExpression: parser.DefaultValueOrExpression{
					Expr: expr,
				},
			}, nil
		default:
			return nil, fmt.Errorf("unhandled default CollateExpr node: %#v", expr)
		}
	case *parser.ComparisonExpr, *parser.FuncExpr:
		return &parser.DefaultDefinition{
			ValueOrExpression: parser.DefaultValueOrExpression{
				Expr: expr,
			},
		}, nil
	default:
		return nil, fmt.Errorf("unhandled default node: %#v", expr)
	}
}

func (p PostgresParser) parseTypeName(node *pgquery.TypeName) (parser.ColumnType, error) {
	columnType := parser.ColumnType{}
	if node.TypeOid != 0 || node.Setof != false || node.PctType != false || node.Typemod != -1 {
		return columnType, fmt.Errorf("unhandled node in parseTypeName: %#v", node)
	}

	if node.ArrayBounds != nil {
		columnType.Array = true
	}

	var typeNames []string
	for _, name := range node.Names {
		if n, ok := name.Node.(*pgquery.Node_String_); ok {
			typeNames = append(typeNames, n.String_.Sval)
		} else {
			return columnType, fmt.Errorf("non-Node_String_ name in parseCreateStmt: %#v", name)
		}
	}

	if len(typeNames) == 1 || (len(typeNames) == 2 && typeNames[0] == "pg_catalog") {
		typeName := typeNames[len(typeNames)-1]
		switch typeName {
		case "int2":
			columnType.Type = "smallint"
		case "int4":
			columnType.Type = "integer"
		case "int8":
			columnType.Type = "bigint"
		case "float4":
			columnType.Type = "real"
		case "float8":
			columnType.Type = "double precision"
		case "bool":
			if len(typeNames) == 1 {
				// For test compatibility, keep bool as bool.
				// TODO: Delete this exception.
				columnType.Type = typeName
			} else {
				columnType.Type = "boolean"
			}
		case "bpchar":
			columnType.Type = "character"
		case "boolean", "varchar", "interval", "numeric", "timestamp", "time": // TODO: use this pattern more, fixing failed tests as well
			columnType.Type = typeName
		case "timetz":
			columnType.Type = "time"
			columnType.Timezone = true
		case "timestamptz":
			columnType.Type = "timestamp"
			columnType.Timezone = true
		default:
			if len(typeNames) == 2 {
				return columnType, fmt.Errorf("unhandled type in parseTypeName: %s", typeName)
			} else {
				// TODO: Whitelist types explicitly. We're missing 'json' and 'text' at least.
				columnType.Type = typeName
			}
		}
	} else if len(typeNames) == 2 {
		columnType.References = typeNames[0] + "."
		columnType.Type = typeNames[1]
	} else {
		return columnType, fmt.Errorf("unexpected length in parseTypeName: %d", len(typeNames))
	}

	typmods, err := p.parseTypmods(node.Typmods)
	if err != nil {
		return columnType, err
	}
	switch len(typmods) {
	case 1:
		columnType.Length = typmods[0]
	case 2:
		columnType.Length = typmods[0]
		columnType.Scale = typmods[1]
	}

	return columnType, nil
}

func (p PostgresParser) parseTypmods(typmods []*pgquery.Node) ([]*parser.SQLVal, error) {
	if typmods == nil {
		return []*parser.SQLVal{}, nil
	}

	values := make([]*parser.SQLVal, len(typmods))
	for i, mod := range typmods {
		modExpr, err := p.parseExpr(mod)
		if err != nil {
			return nil, err
		}

		switch expr := modExpr.(type) {
		case *parser.SQLVal:
			if expr.Type == parser.IntVal {
				values[i] = expr
			} else {
				return nil, fmt.Errorf("unexpected SQLVal type in parseTypeName: %d", expr.Type)
			}
		default:
			return nil, fmt.Errorf("unexpected typmod type in parseTypeName: %#v", expr)
		}
	}

	return values, nil
}

func (p PostgresParser) parseStringList(list *pgquery.List) (string, error) {
	var objects []string
	for _, node := range list.Items {
		switch n := node.Node.(type) {
		case *pgquery.Node_String_:
			objects = append(objects, n.String_.Sval)
		}
	}
	return strings.Join(objects, "."), nil
}

func (p PostgresParser) parseCheckConstraint(constraint *pgquery.Constraint) (*parser.CheckDefinition, error) {
	expr, err := p.parseExpr(constraint.RawExpr)

	if err != nil {
		return nil, err
	}

	return &parser.CheckDefinition{
		Where:          *parser.NewWhere(parser.WhereStr, expr),
		ConstraintName: parser.NewColIdent(constraint.Conname),
		NoInherit:      parser.BoolVal(constraint.IsNoInherit),
	}, nil
}

func (p PostgresParser) parseCreateSchemaStmt(stmt *pgquery.CreateSchemaStmt) (parser.Statement, error) {
	return &parser.DDL{
		Action: parser.CreateSchema,
		Schema: &parser.Schema{
			Name: stmt.Schemaname,
		},
	}, nil
}

// This is a workaround to handle cases where PostgreSQL automatically adds or removes type casting.
//
// Example:
//
// ```
// $ cat schema.sql
// CREATE TABLE test (
// t text CHECK (t ~ '[0-9]'),
// i integer CHECK (i = ANY (ARRAY[1,2,3]::integer[]))
// );
//
// $ psql sandbox < schema.sql
// $ psqldef sandbox --export
// CREATE TABLE "public"."test" (
// "t" text CONSTRAINT test_t_check CHECK (t ~ '[0-9]'::text),
// "i" integer CONSTRAINT test_i_check CHECK (i = ANY (ARRAY[1, 2, 3]))
// );
// ```
//
// Looking at the export result, PostgreSQL automatically adds `::text` type casting to '[0-9]',
// and removes `::integer[]` from `ARRAY[1,2,3]`. In such cases, if you don't remove the type casting,
// the generator will fail to calculate the diff.
//
// Ideally, the generator should be smart enough to handle the calculation of diff while keeping the type casting.
// However, as a workaround, it is handled by the parser.
//
// Since this function's support is not complete, updates will be necessary in the future.
func shouldDeleteTypeCast(sourceNode *pgquery.Node, targetType parser.ColumnType) bool {
	switch sourceNode.Node.(type) {
	case *pgquery.Node_AConst:
		if targetType.Array {
			// Do not delete type cast from '{1,2,3}'::integer[]
			return false
		}
		// Delete type cast from '[0-9]'::text
		if targetType.Type == "text" {
			return true
		}
		// Delete type cast from '2022-01-01'::date
		if targetType.Type == "date" {
			return true
		}
		// Do not delete type cast from '1 day'::interval
		return false
	case *pgquery.Node_AArrayExpr, *pgquery.Node_TypeCast:
		// Delete type cast from ARRAY[1,2,3]::integer[]
		return true
	default:
		return false
	}
}
