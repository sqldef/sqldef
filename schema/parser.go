// This package has SQL parser, its abstraction and SQL generator.
// Never touch database.
package schema

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/k0kubun/sqldef/sqlparser"
)

// Convert back `type BoolVal bool`
func castBool(val sqlparser.BoolVal) bool {
	ret, _ := strconv.ParseBool(fmt.Sprint(val))
	return ret
}

func castBoolPtr(val *sqlparser.BoolVal) *bool {
	if val == nil {
		return nil
	}
	ret := castBool(*val)
	return &ret
}

func parseValue(val *sqlparser.SQLVal) *Value {
	if val == nil {
		return nil
	}

	var valueType ValueType
	if val.Type == sqlparser.StrVal {
		valueType = ValueTypeStr
	} else if val.Type == sqlparser.IntVal {
		valueType = ValueTypeInt
	} else if val.Type == sqlparser.FloatVal {
		valueType = ValueTypeFloat
	} else if val.Type == sqlparser.HexNum {
		valueType = ValueTypeHexNum
	} else if val.Type == sqlparser.HexVal {
		valueType = ValueTypeHex
	} else if val.Type == sqlparser.ValArg {
		valueType = ValueTypeValArg
	} else if val.Type == sqlparser.BitVal {
		valueType = ValueTypeBit
	} else {
		return nil // TODO: Unreachable, but handle this properly...
	}

	ret := Value{
		valueType: valueType,
		raw:       val.Val,
	}

	switch valueType {
	case ValueTypeStr:
		ret.strVal = string(val.Val)
	case ValueTypeInt:
		intVal, _ := strconv.Atoi(string(val.Val)) // TODO: handle error
		ret.intVal = intVal
	case ValueTypeFloat:
		floatVal, _ := strconv.ParseFloat(string(val.Val), 64) // TODO: handle error
		ret.floatVal = floatVal
	case ValueTypeBit:
		if string(val.Val) == "1" {
			ret.bitVal = true
		} else {
			ret.bitVal = false
		}
	}

	return &ret
}

func parseTable(mode GeneratorMode, stmt *sqlparser.DDL) Table {
	columns := []Column{}
	indexes := []Index{}
	foreignKeys := []ForeignKey{}

	for i, parsedCol := range stmt.TableSpec.Columns {
		column := Column{
			name:          parsedCol.Name.String(),
			position:      i,
			typeName:      parsedCol.Type.Type,
			unsigned:      castBool(parsedCol.Type.Unsigned),
			notNull:       castBoolPtr(parsedCol.Type.NotNull),
			autoIncrement: castBool(parsedCol.Type.Autoincrement),
			array:         castBool(parsedCol.Type.Array),
			defaultVal:    parseValue(parsedCol.Type.Default),
			length:        parseValue(parsedCol.Type.Length),
			scale:         parseValue(parsedCol.Type.Scale),
			charset:       parsedCol.Type.Charset,
			collate:       normalizeCollate(parsedCol.Type.Collate, *stmt.TableSpec),
			timezone:      castBool(parsedCol.Type.Timezone),
			keyOption:     ColumnKeyOption(parsedCol.Type.KeyOpt), // FIXME: tight coupling in enum order
			onUpdate:      parseValue(parsedCol.Type.OnUpdate),
			enumValues:    parsedCol.Type.EnumValues,
			references:    parsedCol.Type.References,
		}
		columns = append(columns, column)
	}

	for _, indexDef := range stmt.TableSpec.Indexes {
		indexColumns := []IndexColumn{}
		for _, column := range indexDef.Columns {
			indexColumns = append(
				indexColumns,
				IndexColumn{
					column: column.Column.String(),
					length: parseValue(column.Length),
				},
			)
		}

		index := Index{
			name:      indexDef.Info.Name.String(),
			indexType: indexDef.Info.Type,
			columns:   indexColumns,
			primary:   indexDef.Info.Primary,
			unique:    indexDef.Info.Unique,
		}
		indexes = append(indexes, index)
	}

	for _, foreignKeyDef := range stmt.TableSpec.ForeignKeys {
		indexColumns := []string{}
		for _, indexColumn := range foreignKeyDef.IndexColumns {
			indexColumns = append(indexColumns, indexColumn.String())
		}

		referenceColumns := []string{}
		for _, referenceColumn := range foreignKeyDef.ReferenceColumns {
			referenceColumns = append(referenceColumns, referenceColumn.String())
		}

		foreignKey := ForeignKey{
			constraintName:   foreignKeyDef.ConstraintName.String(),
			indexName:        foreignKeyDef.IndexName.String(),
			indexColumns:     indexColumns,
			referenceName:    foreignKeyDef.ReferenceName.String(),
			referenceColumns: referenceColumns,
			onDelete:         foreignKeyDef.OnDelete.String(),
			onUpdate:         foreignKeyDef.OnUpdate.String(),
		}
		foreignKeys = append(foreignKeys, foreignKey)
	}

	return Table{
		name:        normalizedTableName(mode, stmt.NewName),
		columns:     columns,
		indexes:     indexes,
		foreignKeys: foreignKeys,
	}
}

func parseIndex(stmt *sqlparser.DDL) (Index, error) {
	if stmt.IndexSpec == nil {
		return Index{}, fmt.Errorf("stmt.IndexSpec was null on parseIndex: %#v", stmt)
	}

	indexColumns := []IndexColumn{}
	for _, colIdent := range stmt.IndexCols {
		indexColumns = append(
			indexColumns,
			IndexColumn{
				column: colIdent.String(),
				length: nil,
			},
		)
	}

	where := ""
	if stmt.IndexSpec.Where != nil && stmt.IndexSpec.Where.Type == sqlparser.WhereStr {
		expr := stmt.IndexSpec.Where.Expr
		// remove root paren expression
		if parenExpr, ok := expr.(*sqlparser.ParenExpr); ok {
			expr = parenExpr.Expr
		}
		where = sqlparser.String(expr)
	}

	return Index{
		name:      stmt.IndexSpec.Name.String(),
		indexType: "", // not supported in parser yet
		columns:   indexColumns,
		primary:   false, // not supported in parser yet
		unique:    stmt.IndexSpec.Unique,
		where:     where,
	}, nil
}

// Parse DDL like `CREATE TABLE` or `ALTER TABLE`.
// This doesn't support destructive DDL like `DROP TABLE`.
func parseDDL(mode GeneratorMode, ddl string) (DDL, error) {
	var parserMode sqlparser.ParserMode
	switch mode {
	case GeneratorModeMysql:
		parserMode = sqlparser.ParserModeMysql
	case GeneratorModePostgres:
		parserMode = sqlparser.ParserModePostgres
	case GeneratorModeSQLite3:
		parserMode = sqlparser.ParserModeSQLite3
	default:
		panic("unrecognized parser mode")
	}

	stmt, err := sqlparser.ParseStrictDDLWithMode(ddl, parserMode)
	if err != nil {
		return nil, err
	}

	switch stmt := stmt.(type) {
	case *sqlparser.DDL:
		if stmt.Action == "create" {
			// TODO: handle other create DDL as error?
			return &CreateTable{
				statement: ddl,
				table:     parseTable(mode, stmt),
			}, nil
		} else if stmt.Action == "create index" {
			index, err := parseIndex(stmt)
			if err != nil {
				return nil, err
			}
			return &CreateIndex{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table),
				index:     index,
			}, nil
		} else if stmt.Action == "add index" {
			index, err := parseIndex(stmt)
			if err != nil {
				return nil, err
			}
			return &AddIndex{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table),
				index:     index,
			}, nil
		} else if stmt.Action == "add primary key" {
			index, err := parseIndex(stmt)
			if err != nil {
				return nil, err
			}
			return &AddPrimaryKey{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table),
				index:     index,
			}, nil
		} else if stmt.Action == "add foreign key" {
			indexColumns := []string{}
			for _, indexColumn := range stmt.ForeignKey.IndexColumns {
				indexColumns = append(indexColumns, indexColumn.String())
			}
			referenceColumns := []string{}
			for _, referenceColumn := range stmt.ForeignKey.ReferenceColumns {
				referenceColumns = append(referenceColumns, referenceColumn.String())
			}

			return &AddForeignKey{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table),
				foreignKey: ForeignKey{
					constraintName:   stmt.ForeignKey.ConstraintName.String(),
					indexName:        stmt.ForeignKey.IndexName.String(),
					indexColumns:     indexColumns,
					referenceName:    stmt.ForeignKey.ReferenceName.String(),
					referenceColumns: referenceColumns,
					onDelete:         stmt.ForeignKey.OnDelete.String(),
					onUpdate:         stmt.ForeignKey.OnUpdate.String(),
				},
			}, nil
		} else if stmt.Action == "create policy" {
			scope := make([]string, len(stmt.Policy.To))
			for i, to := range stmt.Policy.To {
				scope[i] = to.String()
			}
			var using, withCheck string
			if stmt.Policy.Using != nil {
				using = sqlparser.String(stmt.Policy.Using.Expr)
			}
			if stmt.Policy.WithCheck != nil {
				withCheck = sqlparser.String(stmt.Policy.WithCheck.Expr)
			}
			return &AddPolicy{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table),
				policy: Policy{
					name:       stmt.Policy.Name.String(),
					permissive: stmt.Policy.Permissive.Raw(),
					scope:      string(stmt.Policy.Scope),
					roles:      scope,
					using:      using,
					withCheck:  withCheck,
				},
			}, nil
		} else if stmt.Action == "create view" {
			return &View{
				statement:  ddl,
				name:       stmt.View.Name.Name.String(),
				definition: sqlparser.String(stmt.View.Definition),
			}, nil
		} else {
			return nil, fmt.Errorf(
				"unsupported type of DDL action (only 'CREATE TABLE', 'CREATE INDEX' and 'ALTER TABLE ADD INDEX' are supported) '%s': %s",
				stmt.Action, ddl,
			)
		}
	default:
		return nil, fmt.Errorf("unsupported type of SQL (only DDL is supported): %s", ddl)
	}
}

// Parse `ddls`, which is expected to `;`-concatenated DDLs
// and not to include destructive DDL.
func parseDDLs(mode GeneratorMode, str string) ([]DDL, error) {
	re := regexp.MustCompilePOSIX("^--.*")
	str = re.ReplaceAllString(str, "")

	ddls := strings.Split(str, ";")
	result := []DDL{}

	for _, ddl := range ddls {
		ddl = strings.TrimSpace(ddl) // TODO: trim trailing comment as well, or ignore it by parser somehow?
		if len(ddl) == 0 {
			continue
		}

		parsed, err := parseDDL(mode, ddl)
		if err != nil {
			return result, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

// Replace pseudo collation "binary" with "{charset}_bin"
func normalizeCollate(collate string, table sqlparser.TableSpec) string {
	if collate == "binary" {
		return detectCharset(table) + "_bin"
	} else {
		return collate
	}
}

// Qualify Postgres schema
func normalizedTableName(mode GeneratorMode, tableName sqlparser.TableName) string {
	table := tableName.Name.String()
	if mode == GeneratorModePostgres {
		if len(tableName.Qualifier.String()) > 0 {
			table = tableName.Qualifier.String() + "." + table
		} else {
			table = "public." + table
		}
	}
	return table
}

// TODO: parse charset in parser.y instead of "detecting" it
func detectCharset(table sqlparser.TableSpec) string {
	for _, option := range strings.Split(table.Options, " ") {
		if strings.HasPrefix(option, "charset=") {
			return strings.TrimLeft(option, "charset=")
		}
	}
	// TODO: consider returning err when charset is missing
	return ""
}
