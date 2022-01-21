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
	} else if val.Type == sqlparser.ValBool {
		valueType = ValueTypeBool
	} else {
		return nil // TODO: Unreachable, but handle this properly...
	}

	ret := Value{
		valueType: valueType,
		raw:       val.Val,
	}

	switch valueType {
	case ValueTypeStr, ValueTypeBool:
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

// Assume an integer length. Maybe useful only for index lengths.
// TODO: Change IndexColumn.Length in parser.y to integer in the first place
func parseLength(val *sqlparser.SQLVal) (*int, error) {
	if val == nil {
		return nil, nil
	}
	if val.Type != sqlparser.IntVal {
		return nil, fmt.Errorf("Expected a length to be int, but got ValType: %d (%#v)", val.Type, val.Val)
	}
	intVal, err := strconv.Atoi(string(val.Val)) // TODO: handle error
	if err != nil {
		return nil, err
	}
	return &intVal, nil
}

func parseTable(mode GeneratorMode, stmt *sqlparser.DDL) (Table, error) {
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
			defaultDef:    parseDefaultDefinition(parsedCol.Type.Default),
			length:        parseValue(parsedCol.Type.Length),
			scale:         parseValue(parsedCol.Type.Scale),
			charset:       parsedCol.Type.Charset,
			collate:       normalizeCollate(parsedCol.Type.Collate, *stmt.TableSpec),
			timezone:      castBool(parsedCol.Type.Timezone),
			keyOption:     ColumnKeyOption(parsedCol.Type.KeyOpt), // FIXME: tight coupling in enum order
			onUpdate:      parseValue(parsedCol.Type.OnUpdate),
			comment:       parseValue(parsedCol.Type.Comment),
			enumValues:    parsedCol.Type.EnumValues,
			references:    parsedCol.Type.References,
			identity:      parseIdentity(parsedCol.Type.Identity),
			sequence:      parseIdentitySequence(parsedCol.Type.Identity),
		}
		if parsedCol.Type.Check != nil {
			column.check = &CheckDefinition{
				definition:        sqlparser.String(parsedCol.Type.Check.Where.Expr),
				constraintName:    sqlparser.String(parsedCol.Type.Check.ConstraintName),
				notForReplication: parsedCol.Type.Check.NotForReplication,
			}
		}
		column.checkNoInherit = castBool(parsedCol.Type.CheckNoInherit)
		columns = append(columns, column)
	}

	for _, indexDef := range stmt.TableSpec.Indexes {
		indexColumns := []IndexColumn{}
		for _, column := range indexDef.Columns {
			length, err := parseLength(column.Length)
			if err != nil {
				return Table{}, err
			}
			indexColumns = append(
				indexColumns,
				IndexColumn{
					column:    column.Column.String(),
					length:    length,
					direction: column.Direction,
				},
			)
		}

		indexOptions := []IndexOption{}
		for _, option := range indexDef.Options {
			indexOptions = append(
				indexOptions,
				IndexOption{
					optionName: option.Name,
					value:      parseValue(option.Value),
				},
			)
		}

		indexPartition := IndexPartition{}
		if indexDef.Partition != nil {
			indexPartition.partitionName = indexDef.Partition.Name
			indexPartition.column = indexDef.Partition.Column
		}

		index := Index{
			name:      indexDef.Info.Name.String(),
			indexType: indexDef.Info.Type,
			columns:   indexColumns,
			primary:   indexDef.Info.Primary,
			unique:    indexDef.Info.Unique,
			clustered: bool(indexDef.Info.Clustered),
			options:   indexOptions,
			partition: indexPartition,
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
			constraintName:    foreignKeyDef.ConstraintName.String(),
			indexName:         foreignKeyDef.IndexName.String(),
			indexColumns:      indexColumns,
			referenceName:     foreignKeyDef.ReferenceName.String(),
			referenceColumns:  referenceColumns,
			onDelete:          foreignKeyDef.OnDelete.String(),
			onUpdate:          foreignKeyDef.OnUpdate.String(),
			notForReplication: foreignKeyDef.NotForReplication,
		}
		foreignKeys = append(foreignKeys, foreignKey)
	}

	return Table{
		name:        normalizedTableName(mode, stmt.NewName),
		columns:     columns,
		indexes:     indexes,
		foreignKeys: foreignKeys,
	}, nil
}

func parseIndex(stmt *sqlparser.DDL) (Index, error) {
	if stmt.IndexSpec == nil {
		return Index{}, fmt.Errorf("stmt.IndexSpec was null on parseIndex: %#v", stmt)
	}

	indexColumns := []IndexColumn{}
	for _, column := range stmt.IndexCols {
		length, err := parseLength(column.Length)
		if err != nil {
			return Index{}, err
		}
		indexColumns = append(
			indexColumns,
			IndexColumn{
				column:    column.Column.String(),
				length:    length,
				direction: column.Direction,
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

	includedColumns := []string{}
	for _, includedColumn := range stmt.IndexSpec.Included {
		includedColumns = append(includedColumns, includedColumn.String())
	}

	indexOptions := []IndexOption{}
	for _, option := range stmt.IndexSpec.Options {
		indexOptions = append(
			indexOptions,
			IndexOption{
				optionName: option.Name,
				value:      parseValue(option.Value),
			},
		)
	}

	indexParition := IndexPartition{}
	if stmt.IndexSpec.Partition != nil {
		indexParition.partitionName = stmt.IndexSpec.Partition.Name
		indexParition.column = stmt.IndexSpec.Partition.Column
	}

	return Index{
		name:       stmt.IndexSpec.Name.String(),
		indexType:  "", // not supported in parser yet
		columns:    indexColumns,
		primary:    false, // not supported in parser yet
		unique:     stmt.IndexSpec.Unique,
		constraint: stmt.IndexSpec.Constraint,
		clustered:  stmt.IndexSpec.Clustered,
		where:      where,
		included:   includedColumns,
		options:    indexOptions,
		partition:  indexParition,
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
	case GeneratorModeMssql:
		parserMode = sqlparser.ParserModeMssql
	default:
		panic("unrecognized parser mode")
	}

	stmt, err := sqlparser.ParseStrictDDLWithMode(ddl, parserMode)
	if err != nil {
		return nil, err
	}

	switch stmt := stmt.(type) {
	case *sqlparser.DDL:
		if stmt.Action == sqlparser.CreateStr {
			// TODO: handle other create DDL as error?
			table, err := parseTable(mode, stmt)
			if err != nil {
				return nil, err
			}
			return &CreateTable{
				statement: ddl,
				table:     table,
			}, nil
		} else if stmt.Action == sqlparser.CreateIndexStr {
			index, err := parseIndex(stmt)
			if err != nil {
				return nil, err
			}
			return &CreateIndex{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table),
				index:     index,
			}, nil
		} else if stmt.Action == sqlparser.AddIndexStr {
			index, err := parseIndex(stmt)
			if err != nil {
				return nil, err
			}
			return &AddIndex{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table),
				index:     index,
			}, nil
		} else if stmt.Action == sqlparser.AddPrimaryKeyStr {
			index, err := parseIndex(stmt)
			if err != nil {
				return nil, err
			}
			return &AddPrimaryKey{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table),
				index:     index,
			}, nil
		} else if stmt.Action == sqlparser.AddForeignKeyStr {
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
					constraintName:    stmt.ForeignKey.ConstraintName.String(),
					indexName:         stmt.ForeignKey.IndexName.String(),
					indexColumns:      indexColumns,
					referenceName:     stmt.ForeignKey.ReferenceName.String(),
					referenceColumns:  referenceColumns,
					onDelete:          stmt.ForeignKey.OnDelete.String(),
					onUpdate:          stmt.ForeignKey.OnUpdate.String(),
					notForReplication: stmt.ForeignKey.NotForReplication,
				},
			}, nil
		} else if stmt.Action == sqlparser.CreatePolicyStr {
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
		} else if stmt.Action == sqlparser.CreateViewStr {
			return &View{
				statement:  ddl,
				name:       stmt.View.Name.Name.String(),
				definition: sqlparser.String(stmt.View.Definition),
			}, nil
		} else if stmt.Action == sqlparser.CreateTriggerStr {
			body := []string{}
			for _, triggerStatement := range stmt.Trigger.Body {
				body = append(body, sqlparser.String(triggerStatement))
			}

			return &Trigger{
				statement: ddl,
				name:      stmt.Trigger.Name.String(),
				tableName: stmt.Trigger.TableName.Name.String(),
				time:      stmt.Trigger.Time,
				event:     stmt.Trigger.Event,
				body:      body,
			}, nil
		} else if stmt.Action == sqlparser.CreateTypeStr {
			return &Type{
				name:      normalizedTableName(mode, stmt.Type.Name),
				statement: ddl,
			}, nil
		} else {
			return nil, fmt.Errorf(
				"unsupported type of DDL action '%s': %s",
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

	for len(ddls) > 0 {
		// Unfortunately, there's no easy way to let sqlparser recognize which ';' is the end of a DDL.
		// So we just attempt parsing until it succeeds. I'll let the parser do it in the future.
		var parsed DDL
		var err error
		i := 1
		for {
			ddl := strings.Join(ddls[0:i], ";")
			ddl = strings.TrimSpace(ddl)
			ddl = strings.TrimSuffix(ddl, ";")
			if ddl == "" {
				break
			}

			parsed, err = parseDDL(mode, ddl)
			if err == nil || i == len(ddls) {
				break
			}
			i++
		}

		if err != nil {
			return result, err
		}
		if parsed != nil {
			result = append(result, parsed)
		}

		if i < len(ddls) {
			ddls = ddls[i:]
		} else {
			break
		}
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

func parseIdentity(opt *sqlparser.IdentityOpt) *Identity {
	if opt == nil {
		return nil
	}
	return &Identity{behavior: strings.ToUpper(opt.Behavior), notForReplication: opt.NotForReplication}
}

func parseDefaultDefinition(opt *sqlparser.DefaultDefinition) *DefaultDefinition {
	if opt == nil || opt.Value == nil {
		return nil
	}
	defaultVal := parseValue(opt.Value)

	var constraintName string
	if opt.ConstraintName.String() != "" {
		constraintName = opt.ConstraintName.String()
	}
	return &DefaultDefinition{constraintName: constraintName, value: defaultVal}
}

func parseIdentitySequence(opt *sqlparser.IdentityOpt) *Sequence {
	if opt == nil || opt.Sequence == nil {
		return nil
	}
	seq := &Sequence{
		Name:        opt.Sequence.Name,
		IfNotExists: opt.Sequence.IfNotExists,
		Type:        opt.Sequence.Type,
		OwnedBy:     opt.Sequence.OwnedBy,
	}
	if opt.Sequence.IncrementBy != nil {
		seq.IncrementBy = &parseValue(opt.Sequence.IncrementBy).intVal
	}
	if opt.Sequence.MinValue != nil {
		seq.MinValue = &parseValue(opt.Sequence.MinValue).intVal
	}
	if opt.Sequence.MaxValue != nil {
		seq.MaxValue = &parseValue(opt.Sequence.MaxValue).intVal
	}
	if opt.Sequence.StartWith != nil {
		seq.StartWith = &parseValue(opt.Sequence.StartWith).intVal
	}
	if opt.Sequence.Cache != nil {
		seq.Cache = &parseValue(opt.Sequence.Cache).intVal
	}
	if opt.Sequence.NoMinValue != nil {
		seq.NoMinValue = true
	}
	if opt.Sequence.NoMaxValue != nil {
		seq.NoMaxValue = true
	}
	if opt.Sequence.Cycle != nil {
		seq.Cycle = true
	}
	if opt.Sequence.NoCycle != nil {
		seq.NoCycle = true
	}
	return seq
}
