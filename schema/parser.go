// This package has SQL parser, its abstraction and SQL generator.
// Never touch database.
package schema

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sqldef/sqldef/database"
	"github.com/sqldef/sqldef/parser"
)

// Parse `ddls`, which is expected to `;`-concatenated DDLs
// and not to include destructive DDL.
func ParseDDLs(mode GeneratorMode, sqlParser database.Parser, sql string, defaultSchema string) ([]DDL, error) {
	ddls, err := sqlParser.Parse(sql)
	if err != nil {
		return nil, err
	}

	var result []DDL
	for _, ddl := range ddls {
		parsed, err := parseDDL(mode, ddl.DDL, ddl.Statement, defaultSchema)
		if err != nil {
			return result, err
		}
		result = append(result, parsed)
	}
	return result, nil
}

// Parse DDL like `CREATE TABLE` or `ALTER TABLE`.
// This doesn't support destructive DDL like `DROP TABLE`.
func parseDDL(mode GeneratorMode, ddl string, stmt parser.Statement, defaultSchema string) (DDL, error) {
	switch stmt := stmt.(type) {
	case *parser.DDL:
		if stmt.Action == parser.CreateTable {
			// TODO: handle other create DDL as error?
			table, err := parseTable(mode, stmt, defaultSchema)
			if err != nil {
				return nil, err
			}
			return &CreateTable{
				statement: ddl,
				table:     table,
			}, nil
		} else if stmt.Action == parser.CreateIndex {
			index, err := parseIndex(stmt)
			if err != nil {
				return nil, err
			}
			return &CreateIndex{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table, defaultSchema),
				index:     index,
			}, nil
		} else if stmt.Action == parser.AddIndex {
			index, err := parseIndex(stmt)
			if err != nil {
				return nil, err
			}
			return &AddIndex{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table, defaultSchema),
				index:     index,
			}, nil
		} else if stmt.Action == parser.AddPrimaryKey {
			index, err := parseIndex(stmt)
			if err != nil {
				return nil, err
			}
			return &AddPrimaryKey{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table, defaultSchema),
				index:     index,
			}, nil
		} else if stmt.Action == parser.AddForeignKey {
			indexColumns := []string{}
			for _, indexColumn := range stmt.ForeignKey.IndexColumns {
				indexColumns = append(indexColumns, indexColumn.String())
			}
			referenceColumns := []string{}
			for _, referenceColumn := range stmt.ForeignKey.ReferenceColumns {
				referenceColumns = append(referenceColumns, referenceColumn.String())
			}
			var constraintOptions *ConstraintOptions
			if stmt.ForeignKey.ConstraintOptions != nil {
				constraintOptions = &ConstraintOptions{
					deferrable:        stmt.ForeignKey.ConstraintOptions.Deferrable,
					initiallyDeferred: stmt.ForeignKey.ConstraintOptions.InitiallyDeferred,
				}
			}

			return &AddForeignKey{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table, defaultSchema),
				foreignKey: ForeignKey{
					constraintName:    stmt.ForeignKey.ConstraintName.String(),
					indexName:         stmt.ForeignKey.IndexName.String(),
					indexColumns:      indexColumns,
					referenceName:     normalizedTableName(mode, stmt.ForeignKey.ReferenceName, defaultSchema),
					referenceColumns:  referenceColumns,
					onDelete:          stmt.ForeignKey.OnDelete.String(),
					onUpdate:          stmt.ForeignKey.OnUpdate.String(),
					notForReplication: stmt.ForeignKey.NotForReplication,
					constraintOptions: constraintOptions,
				},
			}, nil
		} else if stmt.Action == parser.CreatePolicy {
			scope := make([]string, len(stmt.Policy.To))
			for i, to := range stmt.Policy.To {
				scope[i] = to.String()
			}
			var using, withCheck string
			if stmt.Policy.Using != nil {
				using = parser.String(stmt.Policy.Using.Expr)
			}
			if stmt.Policy.WithCheck != nil {
				withCheck = parser.String(stmt.Policy.WithCheck.Expr)
			}
			return &AddPolicy{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table, defaultSchema),
				policy: Policy{
					name:       stmt.Policy.Name.String(),
					permissive: string(stmt.Policy.Permissive),
					scope:      string(stmt.Policy.Scope),
					roles:      scope,
					using:      using,
					withCheck:  withCheck,
				},
			}, nil
		} else if stmt.Action == parser.CreateView {
			columns := []string{}
			if expr, ok := stmt.View.Definition.(*parser.Select); ok {
				for _, s := range expr.SelectExprs {
					columns = append(columns, parser.String(s))
				}
			}
			return &View{
				statement:    ddl,
				viewType:     strings.ToUpper(stmt.View.Type),
				securityType: strings.ToUpper(stmt.View.SecurityType),
				name:         normalizedTableName(mode, stmt.View.Name, defaultSchema),
				definition:   parser.String(stmt.View.Definition),
				columns:      columns,
			}, nil
		} else if stmt.Action == parser.CreateTrigger {
			body := []string{}
			for _, triggerStatement := range stmt.Trigger.Body {
				body = append(body, parser.String(triggerStatement))
			}

			return &Trigger{
				statement: ddl,
				name:      stmt.Trigger.Name.String(),
				tableName: stmt.Trigger.TableName.Name.String(),
				time:      stmt.Trigger.Time,
				event:     stmt.Trigger.Event,
				body:      body,
			}, nil
		} else if stmt.Action == parser.CreateType {
			return &Type{
				name:       normalizedTableName(mode, stmt.Type.Name, defaultSchema),
				statement:  ddl,
				enumValues: stmt.Type.Type.EnumValues,
			}, nil
		} else if stmt.Action == parser.CommentOn {
			return &Comment{
				statement: ddl,
				comment:   *stmt.Comment,
			}, nil
		} else if stmt.Action == parser.CreateExtension {
			return &Extension{
				statement: ddl,
				extension: *stmt.Extension,
			}, nil
		} else if stmt.Action == parser.CreateSchema {
			return &Schema{
				statement: ddl,
				schema:    *stmt.Schema,
			}, nil
		} else {
			return nil, fmt.Errorf(
				"unsupported type of DDL action '%d': %s",
				stmt.Action, ddl,
			)
		}
	default:
		return nil, fmt.Errorf("unsupported type of SQL (only DDL is supported): %s", ddl)
	}
}

func parseTable(mode GeneratorMode, stmt *parser.DDL, defaultSchema string) (Table, error) {
	var columns []Column
	var indexes []Index
	var checks []CheckDefinition
	var foreignKeys []ForeignKey

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
			sridDef:       parseSridDefinition(parsedCol.Type.Srid),
			length:        parseValue(parsedCol.Type.Length),
			scale:         parseValue(parsedCol.Type.Scale),
			displayWidth:  parseValue(parsedCol.Type.DisplayWidth),
			charset:       parsedCol.Type.Charset,
			collate:       normalizeCollate(parsedCol.Type.Collate, *stmt.TableSpec),
			timezone:      castBool(parsedCol.Type.Timezone),
			keyOption:     ColumnKeyOption(parsedCol.Type.KeyOpt), // FIXME: tight coupling in enum order
			onUpdate:      parseValue(parsedCol.Type.OnUpdate),
			comment:       parseValue(parsedCol.Type.Comment),
			enumValues:    parsedCol.Type.EnumValues,
			references:    normalizedTable(mode, parsedCol.Type.References, defaultSchema),
			identity:      parseIdentity(parsedCol.Type.Identity),
			sequence:      parseIdentitySequence(parsedCol.Type.Identity),
			generated:     parseGenerated(parsedCol.Type.Generated),
		}
		if parsedCol.Type.Check != nil {
			column.check = &CheckDefinition{
				definition:        parser.String(parsedCol.Type.Check.Where.Expr),
				constraintName:    parser.String(parsedCol.Type.Check.ConstraintName),
				notForReplication: parsedCol.Type.Check.NotForReplication,
				noInherit:         castBool(parsedCol.Type.Check.NoInherit),
			}
		}
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

		name := indexDef.Info.Name.String()
		if name == "" { // For MySQL
			name = indexColumns[0].column
		}

		var constraintOptions *ConstraintOptions
		if indexDef.ConstraintOptions != nil {
			constraintOptions = &ConstraintOptions{
				deferrable:        indexDef.ConstraintOptions.Deferrable,
				initiallyDeferred: indexDef.ConstraintOptions.InitiallyDeferred,
			}
		}

		index := Index{
			name:      name,
			indexType: indexDef.Info.Type,
			columns:   indexColumns,
			primary:   indexDef.Info.Primary,
			unique:    indexDef.Info.Unique,
			clustered: bool(indexDef.Info.Clustered),
			options:   indexOptions,
			partition: indexPartition,

			// FIXME: existence of constraintOptions doesn't mean it's a
			// constraint but other parts of the code doesn't mark it as a
			// constraint so we have to leave it as is for now.
			constraint:        constraintOptions != nil,
			constraintOptions: constraintOptions,
		}
		indexes = append(indexes, index)
	}

	for _, checkDef := range stmt.TableSpec.Checks {
		check := CheckDefinition{
			definition:        parser.String(checkDef.Where.Expr),
			constraintName:    parser.String(checkDef.ConstraintName),
			notForReplication: checkDef.NotForReplication,
			noInherit:         castBool(checkDef.NoInherit),
		}
		checks = append(checks, check)
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

		var constraintOptions *ConstraintOptions
		if foreignKeyDef.ConstraintOptions != nil {
			constraintOptions = &ConstraintOptions{
				deferrable:        foreignKeyDef.ConstraintOptions.Deferrable,
				initiallyDeferred: foreignKeyDef.ConstraintOptions.InitiallyDeferred,
			}
		}

		foreignKey := ForeignKey{
			constraintName:    foreignKeyDef.ConstraintName.String(),
			indexName:         foreignKeyDef.IndexName.String(),
			indexColumns:      indexColumns,
			referenceName:     normalizedTableName(mode, foreignKeyDef.ReferenceName, defaultSchema),
			referenceColumns:  referenceColumns,
			onDelete:          foreignKeyDef.OnDelete.String(),
			onUpdate:          foreignKeyDef.OnUpdate.String(),
			notForReplication: foreignKeyDef.NotForReplication,
			constraintOptions: constraintOptions,
		}
		foreignKeys = append(foreignKeys, foreignKey)
	}

	return Table{
		name:        normalizedTableName(mode, stmt.NewName, defaultSchema),
		columns:     columns,
		indexes:     indexes,
		checks:      checks,
		foreignKeys: foreignKeys,
		options:     stmt.TableSpec.Options,
	}, nil
}

func parseIndex(stmt *parser.DDL) (Index, error) {
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
	if stmt.IndexSpec.Where != nil && stmt.IndexSpec.Where.Type == parser.WhereStr {
		expr := stmt.IndexSpec.Where.Expr
		// remove root paren expression
		if parenExpr, ok := expr.(*parser.ParenExpr); ok {
			expr = parenExpr.Expr
		}
		where = parser.String(expr)
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

	var constraintOptions *ConstraintOptions
	if stmt.IndexSpec.ConstraintOptions != nil {
		constraintOptions = &ConstraintOptions{
			deferrable:        stmt.IndexSpec.ConstraintOptions.Deferrable,
			initiallyDeferred: stmt.IndexSpec.ConstraintOptions.InitiallyDeferred,
		}
	}

	name := stmt.IndexSpec.Name.String()
	if name == "" {
		name = stmt.Table.Name.String()
		for _, indexColumn := range indexColumns {
			name += fmt.Sprintf("_%s", indexColumn.column)
		}
		name += "_idx"
	}
	return Index{
		name:              name,
		indexType:         "", // not supported in parser yet
		columns:           indexColumns,
		primary:           false, // not supported in parser yet
		unique:            stmt.IndexSpec.Unique,
		constraint:        stmt.IndexSpec.Constraint,
		constraintOptions: constraintOptions,
		clustered:         stmt.IndexSpec.Clustered,
		where:             where,
		included:          includedColumns,
		options:           indexOptions,
		partition:         indexParition,
	}, nil
}

func parseValue(val *parser.SQLVal) *Value {
	if val == nil {
		return nil
	}

	var valueType ValueType
	if val.Type == parser.StrVal {
		valueType = ValueTypeStr
	} else if val.Type == parser.IntVal {
		valueType = ValueTypeInt
	} else if val.Type == parser.FloatVal {
		valueType = ValueTypeFloat
	} else if val.Type == parser.HexNum {
		valueType = ValueTypeHexNum
	} else if val.Type == parser.HexVal {
		valueType = ValueTypeHex
	} else if val.Type == parser.ValArg {
		valueType = ValueTypeValArg
	} else if val.Type == parser.BitVal {
		valueType = ValueTypeBit
	} else if val.Type == parser.ValBool {
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
func parseLength(val *parser.SQLVal) (*int, error) {
	if val == nil {
		return nil, nil
	}
	if val.Type != parser.IntVal {
		return nil, fmt.Errorf("Expected a length to be int, but got ValType: %d (%#v)", val.Type, val.Val)
	}
	intVal, err := strconv.Atoi(string(val.Val)) // TODO: handle error
	if err != nil {
		return nil, err
	}
	return &intVal, nil
}

func parseIdentity(opt *parser.IdentityOpt) *Identity {
	if opt == nil {
		return nil
	}
	return &Identity{behavior: strings.ToUpper(opt.Behavior), notForReplication: opt.NotForReplication}
}

func parseDefaultDefinition(opt *parser.DefaultDefinition) *DefaultDefinition {
	if opt == nil || (opt.ValueOrExpression.Value == nil && opt.ValueOrExpression.Expr == nil) {
		return nil
	}

	var constraintName string
	if opt.ConstraintName.String() != "" {
		constraintName = opt.ConstraintName.String()
	}

	if opt.ValueOrExpression.Value != nil {
		defaultVal := parseValue(opt.ValueOrExpression.Value)
		return &DefaultDefinition{constraintName: constraintName, value: defaultVal}
	} else {
		defaultExpr := parser.String(opt.ValueOrExpression.Expr)
		return &DefaultDefinition{constraintName: constraintName, expression: defaultExpr}
	}
}

func parseSridDefinition(opt *parser.SridDefinition) *SridDefinition {
	if opt == nil || opt.Value == nil {
		return nil
	}
	srid := parseValue(opt.Value)
	return &SridDefinition{value: srid}
}

func parseIdentitySequence(opt *parser.IdentityOpt) *Sequence {
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

func parseGenerated(genc *parser.GeneratedColumn) *Generated {
	if genc == nil {
		return nil
	}
	var typ GeneratedType
	switch genc.GeneratedType {
	case "VIRTUAL":
		typ = GeneratedTypeVirtual
	case "STORED":
		typ = GeneratedTypeStored
	}
	return &Generated{
		expr:          parser.String(genc.Expr),
		generatedType: typ,
	}
}

// Qualify Postgres/Mssql schema
func normalizedTableName(mode GeneratorMode, tableName parser.TableName, defaultSchema string) string {
	table := tableName.Name.String()
	if mode == GeneratorModePostgres || mode == GeneratorModeMssql {
		if len(tableName.Schema.String()) > 0 {
			table = tableName.Schema.String() + "." + table
		} else {
			table = defaultSchema + "." + table
		}
	}
	return table
}

func normalizedTable(mode GeneratorMode, tableName string, defaultSchema string) string {
	switch mode {
	case GeneratorModePostgres, GeneratorModeMssql:
		schema, table := splitTableName(tableName, defaultSchema)
		return fmt.Sprintf("%s.%s", schema, table)
	default:
		return tableName
	}
}

// Replace pseudo collation "binary" with "{charset}_bin"
func normalizeCollate(collate string, table parser.TableSpec) string {
	if collate == "binary" {
		return table.Options["default charset"] + "_bin"
	} else {
		return collate
	}
}

// Convert back `type BoolVal bool`
func castBool(val parser.BoolVal) bool {
	ret, _ := strconv.ParseBool(fmt.Sprint(val))
	return ret
}

func castBoolPtr(val *parser.BoolVal) *bool {
	if val == nil {
		return nil
	}
	ret := castBool(*val)
	return &ret
}
