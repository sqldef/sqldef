// This package has SQL parser, its abstraction and SQL generator.
// Never touch database.
package schema

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/parser"
	"github.com/sqldef/sqldef/v3/util"
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
		// Check if this is a MultiStatement (e.g., from multi-table GRANT)
		if multiStmt, ok := ddl.Statement.(*parser.MultiStatement); ok {
			// Expand MultiStatement into individual DDLs
			for _, stmt := range multiStmt.Statements {
				parsed, err := parseDDL(mode, ddl.DDL, stmt, defaultSchema)
				if err != nil {
					return result, err
				}
				result = append(result, parsed)
			}
		} else {
			parsed, err := parseDDL(mode, ddl.DDL, ddl.Statement, defaultSchema)
			if err != nil {
				return result, err
			}
			result = append(result, parsed)
		}
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
			table, err := parseTable(mode, stmt, defaultSchema, ddl)
			if err != nil {
				return nil, err
			}
			return &CreateTable{
				statement: ddl,
				table:     table,
			}, nil
		} else if stmt.Action == parser.CreateIndex {
			index, err := parseIndex(stmt, ddl, mode)
			if err != nil {
				return nil, err
			}
			return &CreateIndex{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table, defaultSchema),
				index:     index,
			}, nil
		} else if stmt.Action == parser.AddIndex {
			index, err := parseIndex(stmt, ddl, mode)
			if err != nil {
				return nil, err
			}
			return &AddIndex{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table, defaultSchema),
				index:     index,
			}, nil
		} else if stmt.Action == parser.AddPrimaryKey {
			index, err := parseIndex(stmt, ddl, mode)
			if err != nil {
				return nil, err
			}
			return &AddPrimaryKey{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table, defaultSchema),
				index:     index,
			}, nil
		} else if stmt.Action == parser.AddForeignKey {

			indexColumns := util.TransformSlice(stmt.ForeignKey.IndexColumns, func(indexColumn parser.ColIdent) string {
				return indexColumn.String()
			})
			referenceColumns := util.TransformSlice(stmt.ForeignKey.ReferenceColumns, func(referenceColumn parser.ColIdent) string {
				return referenceColumn.String()
			})
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
		} else if stmt.Action == parser.AddExclusion {
			return &AddExclusion{
				statement: ddl,
				tableName: normalizedTableName(mode, stmt.Table, defaultSchema),
				exclusion: parseExclusion(stmt.Exclusion),
			}, nil
		} else if stmt.Action == parser.CreatePolicy {
			scope := util.TransformSlice(stmt.Policy.To, func(to parser.ColIdent) string {
				return to.String()
			})
			var using, withCheck parser.Expr
			if stmt.Policy.Using != nil {
				using = stmt.Policy.Using.Expr
			}
			if stmt.Policy.WithCheck != nil {
				withCheck = stmt.Policy.WithCheck.Expr
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
				definition:   stmt.View.Definition,
				columns:      columns,
				withData:     stmt.View.WithData,
				withNoData:   stmt.View.WithNoData,
			}, nil
		} else if stmt.Action == parser.CreateTrigger {
			body := []string{}
			for _, triggerStatement := range stmt.Trigger.Body {
				body = append(body, parser.String(triggerStatement))
			}

			return &Trigger{
				statement: ddl,
				name:      parser.String(stmt.Trigger.Name),
				tableName: stmt.Trigger.TableName.Name.String(),
				time:      stmt.Trigger.Time,
				event:     stmt.Trigger.Event,
				body:      body,
			}, nil
		} else if stmt.Action == parser.CreateType {
			return &Type{
				name:       normalizedObjectName(mode, stmt.Type.Name, defaultSchema),
				statement:  ddl,
				enumValues: stmt.Type.Type.EnumValues,
			}, nil
		} else if stmt.Action == parser.CreateDomain {
			var constraints []DomainConstraint
			for _, check := range stmt.Domain.Constraints {
				constraints = append(constraints, DomainConstraint{
					name:       parser.String(check.ConstraintName),
					expression: check.Where.Expr,
				})
			}

			return &Domain{
				name:         normalizedObjectName(mode, stmt.Domain.Name, defaultSchema),
				statement:    ddl,
				dataType:     parser.String(&stmt.Domain.DataType),
				defaultValue: parseDefaultDefinition(stmt.Domain.Default),
				notNull:      stmt.Domain.NotNull,
				collation:    stmt.Domain.Collation,
				constraints:  constraints,
			}, nil
		} else if stmt.Action == parser.CommentOn {
			return &Comment{
				statement: normalizeTableInCommentOnStmt(mode, stmt.Comment, ddl, defaultSchema),
				comment:   *normalizeTableInComment(mode, stmt.Comment, defaultSchema),
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
		} else if stmt.Action == parser.GrantPrivilege {
			if stmt.Grant.WithGrantOption {
				return nil, fmt.Errorf("WITH GRANT OPTION is not supported yet")
			}

			grantees := stmt.Grant.Grantees

			if len(grantees) > 0 {
				// Normalize privilege names to uppercase for consistency
				normalizedPrivileges := util.TransformSlice(stmt.Grant.Privileges, strings.ToUpper)
				return &GrantPrivilege{
					statement:  ddl,
					tableName:  normalizedTableName(mode, stmt.Table, defaultSchema),
					grantees:   grantees,
					privileges: normalizedPrivileges,
				}, nil
			}
			return nil, fmt.Errorf("no grantees specified in GRANT statement")
		} else if stmt.Action == parser.RevokePrivilege {
			if stmt.Grant.CascadeOption {
				return nil, fmt.Errorf("CASCADE/RESTRICT options are not supported yet")
			}

			grantees := stmt.Grant.Grantees

			// For now, return the first grantee as a single statement
			if len(grantees) > 0 {
				// Normalize privilege names to uppercase for consistency
				normalizedPrivileges := util.TransformSlice(stmt.Grant.Privileges, strings.ToUpper)
				return &RevokePrivilege{
					statement:     ddl,
					tableName:     normalizedTableName(mode, stmt.Table, defaultSchema),
					grantees:      grantees,
					privileges:    normalizedPrivileges,
					cascadeOption: stmt.Grant.CascadeOption,
				}, nil
			}
			return nil, fmt.Errorf("no grantees specified in REVOKE statement")
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

func parseTable(mode GeneratorMode, stmt *parser.DDL, defaultSchema string, rawDDL string) (Table, error) {
	var columns = map[string]*Column{}
	var indexes []Index
	var checks []CheckDefinition
	var foreignKeys []ForeignKey
	var exclusions []Exclusion

	columnComments := extractColumnComments(rawDDL, mode)
	indexComments := extractIndexComments(rawDDL, mode)

	for i, parsedCol := range stmt.TableSpec.Columns {
		// Normalize PostgreSQL type aliases from generic parser
		typeName := parsedCol.Type.Type
		timezone := castBool(parsedCol.Type.Timezone)
		references := parsedCol.Type.References

		if mode == GeneratorModePostgres {
			// Handle short timezone forms: timestamptz -> timestamp, timetz -> time
			// The generic parser may parse these as identifiers without setting Timezone flag
			switch typeName {
			case "timestamptz":
				typeName = "timestamp"
				timezone = true
			case "timetz":
				typeName = "time"
				timezone = true
			}

			// Handle schema-qualified types from generic parser
			// Generic parser stores "schema.type" in typeName field
			// pgquery parser stores "schema." in references and "type" in typeName
			// Normalize to the pgquery format for consistent comparison
			if strings.Contains(typeName, ".") && references == "" {
				parts := strings.SplitN(typeName, ".", 2)
				if len(parts) == 2 {
					// Store schema with trailing dot to match pgquery format
					references = parts[0] + "."
					typeName = parts[1]
				}
			}
		}

		column := Column{
			name:                       parsedCol.Name.String(),
			position:                   i,
			typeName:                   typeName,
			unsigned:                   castBool(parsedCol.Type.Unsigned),
			notNull:                    castBoolPtr(parsedCol.Type.NotNull),
			autoIncrement:              castBool(parsedCol.Type.Autoincrement),
			array:                      castBool(parsedCol.Type.Array),
			defaultDef:                 parseDefaultDefinition(parsedCol.Type.Default),
			sridDef:                    parseSridDefinition(parsedCol.Type.Srid),
			length:                     parseValue(parsedCol.Type.Length),
			scale:                      parseValue(parsedCol.Type.Scale),
			displayWidth:               parseValue(parsedCol.Type.DisplayWidth),
			charset:                    parsedCol.Type.Charset,
			collate:                    normalizeCollate(parsedCol.Type.Collate, *stmt.TableSpec),
			timezone:                   timezone,
			keyOption:                  ColumnKeyOption(parsedCol.Type.KeyOpt), // FIXME: tight coupling in enum order
			onUpdate:                   parseValue(parsedCol.Type.OnUpdate),
			comment:                    parseValue(parsedCol.Type.Comment),
			enumValues:                 parsedCol.Type.EnumValues,
			references:                 normalizedTable(mode, references, defaultSchema),
			referenceDeferrable:        castBoolPtr(parsedCol.Type.ReferenceDeferrable),
			referenceInitiallyDeferred: castBoolPtr(parsedCol.Type.ReferenceInitDeferred),
			identity:                   parseIdentity(parsedCol.Type.Identity),
			sequence:                   parseIdentitySequence(parsedCol.Type.Identity),
			generated:                  parseGenerated(parsedCol.Type.Generated),
		}

		// Parse @renamed annotation for each column
		if comment, ok := columnComments[parsedCol.Name.String()]; ok {
			column.renamedFrom = extractRenameFrom(comment)
		}

		if parsedCol.Type.Check != nil {
			column.check = &CheckDefinition{
				definition:        parsedCol.Type.Check.Where.Expr,
				constraintName:    parser.String(parsedCol.Type.Check.ConstraintName),
				notForReplication: parsedCol.Type.Check.NotForReplication,
				noInherit:         castBool(parsedCol.Type.Check.NoInherit),
			}
		}
		columns[parsedCol.Name.String()] = &column
	}

	// Convert inline foreign key references to ForeignKey objects
	// This handles syntax like: column_name TYPE REFERENCES table_name(column_name)
	// Note: We only convert when reference columns are explicitly specified.
	// If not specified (e.g., "REFERENCES table_name"), we leave it as-is
	// for the database-specific parser to handle (it will infer the primary key).
	for _, parsedCol := range stmt.TableSpec.Columns {
		// Skip if no inline foreign key reference or if it's missing column names
		// (empty ReferenceNames means it will use the primary key, which is database-specific)
		if parsedCol.Type.References == "" || len(parsedCol.Type.ReferenceNames) == 0 {
			continue
		}

		column := columns[parsedCol.Name.String()]

		// Build the foreign key object
		indexColumns := []string{parsedCol.Name.String()}

		referenceColumns := util.TransformSlice(parsedCol.Type.ReferenceNames, func(refCol parser.ColIdent) string {
			return refCol.String()
		})

		constraintName := buildPostgresConstraintName(stmt.NewName.Name.String(), parsedCol.Name.String(), "fkey")

		// Only create constraintOptions if DEFERRABLE or INITIALLY DEFERRED is explicitly set to true
		// This ensures we don't create an empty constraintOptions struct that would differ from
		// database-parsed FKs (which have nil constraintOptions when not deferrable)
		var constraintOptions *ConstraintOptions
		var deferrableVal, initDeferredVal bool
		if parsedCol.Type.ReferenceDeferrable != nil {
			deferrableVal = castBool(*parsedCol.Type.ReferenceDeferrable)
		}
		if parsedCol.Type.ReferenceInitDeferred != nil {
			initDeferredVal = castBool(*parsedCol.Type.ReferenceInitDeferred)
		}
		if (parsedCol.Type.ReferenceDeferrable != nil || parsedCol.Type.ReferenceInitDeferred != nil) && (deferrableVal || initDeferredVal) {
			deferrable := false
			if parsedCol.Type.ReferenceDeferrable != nil {
				deferrable = castBool(*parsedCol.Type.ReferenceDeferrable)
			}
			initiallyDeferred := false
			if parsedCol.Type.ReferenceInitDeferred != nil {
				initiallyDeferred = castBool(*parsedCol.Type.ReferenceInitDeferred)
			}
			constraintOptions = &ConstraintOptions{
				deferrable:        deferrable,
				initiallyDeferred: initiallyDeferred,
			}
		}

		foreignKey := ForeignKey{
			constraintName:    constraintName,
			indexColumns:      indexColumns,
			referenceName:     normalizedTableName(mode, parser.TableName{Name: parser.NewTableIdent(parsedCol.Type.References)}, defaultSchema),
			referenceColumns:  referenceColumns,
			onDelete:          parser.String(parsedCol.Type.ReferenceOnDelete),
			onUpdate:          parser.String(parsedCol.Type.ReferenceOnUpdate),
			constraintOptions: constraintOptions,
		}
		foreignKeys = append(foreignKeys, foreignKey)

		// Clear the references field from the column since it's now represented as a foreign key
		// This prevents it from being used for type qualification
		column.references = ""
		column.referenceDeferrable = nil
		column.referenceInitiallyDeferred = nil
	}

	for _, indexDef := range stmt.TableSpec.Indexes {
		indexColumns := []IndexColumn{}
		for _, column := range indexDef.Columns {
			length, err := parseLength(column.Length)
			if err != nil {
				return Table{}, err
			}

			var columnExpr parser.Expr
			if column.Expression != nil {
				columnExpr = column.Expression
			} else {
				columnExpr = &parser.ColName{Name: column.Column}
			}

			indexColumns = append(
				indexColumns,
				IndexColumn{
					columnExpr: columnExpr,
					length:     length,
					direction:  column.Direction,
				},
			)

			// MSSQL and MySQL: all columns participating in a PRIMARY KEY constraint have their nullability set to NOT NULL
			// MSSQL: https://learn.microsoft.com/en-us/sql/relational-databases/tables/create-primary-keys#limitations
			// MySQL: https://dev.mysql.com/doc/refman/8.4/en/create-table.html
			if indexDef.Info.Primary && (mode == GeneratorModeMssql || mode == GeneratorModeMysql) {
				if column, ok := columns[column.Column.String()]; ok {
					val := true
					column.notNull = &val
				}
			}
		}

		indexOptions := util.TransformSlice(indexDef.Options, func(option *parser.IndexOption) IndexOption {
			return IndexOption{
				optionName: option.Name,
				value:      parseValue(option.Value),
			}
		})

		indexPartition := IndexPartition{}
		if indexDef.Partition != nil {
			indexPartition.partitionName = indexDef.Partition.Name
			indexPartition.column = indexDef.Partition.Column
		}

		name := indexDef.Info.Name.String()
		if name == "" {
			// Auto-generate index/constraint name based on database conventions
			tableName := stmt.Table.Name.String()
			if tableName == "" {
				tableName = stmt.NewName.Name.String()
			}
			columnName := indexColumns[0].ColumnName()

			if mode == GeneratorModePostgres && indexDef.Info.Unique && len(indexColumns) == 1 {
				name = buildPostgresConstraintName(tableName, columnName, "key")
			} else {
				// For MySQL or multi-column constraints, use just the column name
				name = columnName
			}
		}

		var constraintOptions *ConstraintOptions
		if indexDef.ConstraintOptions != nil {
			constraintOptions = &ConstraintOptions{
				deferrable:        indexDef.ConstraintOptions.Deferrable,
				initiallyDeferred: indexDef.ConstraintOptions.InitiallyDeferred,
			}
		}

		// Determine if this is a constraint
		// Constraints have constraintOptions (set when CONSTRAINT keyword is used)
		// For PostgreSQL: Constraints have constraintOptions
		isConstraint := constraintOptions != nil

		// For MSSQL, PRIMARY KEY is always a constraint
		if mode == GeneratorModeMssql && indexDef.Info.Primary {
			isConstraint = true
		}

		index := Index{
			name:      name,
			indexType: indexDef.Info.Type,
			columns:   indexColumns,
			primary:   indexDef.Info.Primary,
			unique:    indexDef.Info.Unique,
			vector:    indexDef.Info.Vector,
			clustered: bool(indexDef.Info.Clustered),
			options:   indexOptions,
			partition: indexPartition,

			// Mark as constraint based on database-specific logic
			constraint:        isConstraint,
			constraintOptions: constraintOptions,
		}

		// Parse @renamed annotation for this index
		if comment, ok := indexComments[name]; ok {
			index.renamedFrom = extractRenameFrom(comment)
		}

		indexes = append(indexes, index)
	}

	for _, checkDef := range stmt.TableSpec.Checks {
		check := CheckDefinition{
			definition:        checkDef.Where.Expr,
			constraintName:    parser.String(checkDef.ConstraintName),
			notForReplication: checkDef.NotForReplication,
			noInherit:         castBool(checkDef.NoInherit),
		}
		checks = append(checks, check)
	}

	for _, foreignKeyDef := range stmt.TableSpec.ForeignKeys {
		indexColumns := util.TransformSlice(foreignKeyDef.IndexColumns, func(indexColumn parser.ColIdent) string {
			return indexColumn.String()
		})

		referenceColumns := util.TransformSlice(foreignKeyDef.ReferenceColumns, func(referenceColumn parser.ColIdent) string {
			return referenceColumn.String()
		})

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

	for _, exclusionDef := range stmt.TableSpec.Exclusions {
		exclusion := parseExclusion(exclusionDef)
		exclusions = append(exclusions, exclusion)
	}

	tableComment := extractTableComment(rawDDL, mode)
	tableRenameFrom := ""
	if tableComment != "" {
		tableRenameFrom = extractRenameFrom(tableComment)
	}

	return Table{
		name:        normalizedTableName(mode, stmt.NewName, defaultSchema),
		columns:     columns,
		indexes:     indexes,
		checks:      checks,
		foreignKeys: foreignKeys,
		exclusions:  exclusions,
		options:     stmt.TableSpec.Options,
		renamedFrom: tableRenameFrom,
	}, nil
}

func parseIndex(stmt *parser.DDL, rawDDL string, mode GeneratorMode) (Index, error) {
	if stmt.IndexSpec == nil {
		return Index{}, fmt.Errorf("stmt.IndexSpec was null on parseIndex: %#v", stmt)
	}

	indexColumns := []IndexColumn{}
	for _, column := range stmt.IndexCols {
		length, err := parseLength(column.Length)
		if err != nil {
			return Index{}, err
		}

		var columnExpr parser.Expr
		if column.Expression != nil {
			columnExpr = column.Expression
		} else {
			columnExpr = &parser.ColName{Name: column.Column}
		}

		indexColumns = append(
			indexColumns,
			IndexColumn{
				columnExpr: columnExpr,
				length:     length,
				direction:  column.Direction,
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

	includedColumns := util.TransformSlice(stmt.IndexSpec.Included, func(includedColumn parser.ColIdent) string {
		return includedColumn.String()
	})

	indexOptions := util.TransformSlice(stmt.IndexSpec.Options, func(option *parser.IndexOption) IndexOption {
		return IndexOption{
			optionName: option.Name,
			value:      parseValue(option.Value),
		}
	})

	indexPartition := IndexPartition{}
	if stmt.IndexSpec.Partition != nil {
		indexPartition.partitionName = stmt.IndexSpec.Partition.Name
		indexPartition.column = stmt.IndexSpec.Partition.Column
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
			name += fmt.Sprintf("_%s", indexColumn.ColumnName())
		}
		// Use PostgreSQL naming convention for UNIQUE constraints
		if mode == GeneratorModePostgres && stmt.IndexSpec.Unique && len(indexColumns) == 1 {
			name += "_key"
		} else {
			name += "_idx"
		}
	}

	// Extract index comments and look for @renamed annotation
	indexComments := extractIndexComments(rawDDL, mode)
	renameFrom := ""
	if comment, ok := indexComments[name]; ok {
		renameFrom = extractRenameFrom(comment)
	}

	return Index{
		name:              name,
		indexType:         "", // not supported in parser yet
		columns:           indexColumns,
		primary:           false, // not supported in parser yet
		unique:            stmt.IndexSpec.Unique,
		vector:            stmt.IndexSpec.Vector,
		constraint:        stmt.IndexSpec.Constraint,
		async:             stmt.IndexSpec.Async,
		concurrently:      stmt.IndexSpec.Concurrently,
		constraintOptions: constraintOptions,
		clustered:         stmt.IndexSpec.Clustered,
		where:             where,
		included:          includedColumns,
		options:           indexOptions,
		partition:         indexPartition,
		renamedFrom:       renameFrom,
	}, nil
}

func mustConvertToInt(val []byte) int {
	intVal, err := strconv.Atoi(string(val))
	if err != nil {
		panic(fmt.Errorf("failed to convert %s to int: %w", string(val), err))
	}
	return intVal
}

func mustConvertToFloat(val []byte) float64 {
	floatVal, err := strconv.ParseFloat(string(val), 64)
	if err != nil {
		panic(fmt.Errorf("failed to convert %s to float: %w", string(val), err))
	}
	return floatVal
}

func parseValue(val *parser.SQLVal) *Value {
	if val == nil {
		return nil
	}

	var valueType ValueType
	switch val.Type {
	case parser.StrVal:
		valueType = ValueTypeStr
	case parser.IntVal:
		valueType = ValueTypeInt
	case parser.FloatVal:
		valueType = ValueTypeFloat
	case parser.HexNum:
		valueType = ValueTypeHexNum
	case parser.HexVal:
		valueType = ValueTypeHex
	case parser.ValArg:
		valueType = ValueTypeValArg
	case parser.BitVal:
		valueType = ValueTypeBit
	case parser.ValBool:
		valueType = ValueTypeBool
	default:
		panic(fmt.Errorf("unexpected value type: %d (%#v)", val.Type, val.Val))
	}

	ret := Value{
		valueType: valueType,
		raw:       string(val.Val),
	}

	switch valueType {
	case ValueTypeStr:
		ret.strVal = string(val.Val)
	case ValueTypeInt:
		ret.intVal = mustConvertToInt(val.Val)
	case ValueTypeFloat:
		ret.floatVal = mustConvertToFloat(val.Val)
	case ValueTypeBool:
		ret.bitVal = string(val.Val) == "true"
	case ValueTypeBit:
		ret.bitVal = string(val.Val) == "1"
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
		return nil, fmt.Errorf("expected a length to be int, but got ValType: %d (%#v)", val.Type, val.Val)
	}
	intVal := mustConvertToInt(val.Val)
	return &intVal, nil
}

func parseIdentity(opt *parser.IdentityOpt) *Identity {
	if opt == nil {
		return nil
	}
	return &Identity{behavior: strings.ToUpper(opt.Behavior), notForReplication: opt.NotForReplication}
}

func parseDefaultDefinition(opt *parser.DefaultDefinition) *DefaultDefinition {
	if opt == nil || opt.Expression.Expr == nil {
		return nil
	}

	var constraintName string
	if opt.ConstraintName.String() != "" {
		constraintName = opt.ConstraintName.String()
	}

	return &DefaultDefinition{
		constraintName: constraintName,
		expression:     opt.Expression.Expr,
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

func parseExclusion(exclusion *parser.ExclusionDefinition) Exclusion {
	var exs []ExclusionPair
	for _, exclusion := range exclusion.Exclusions {
		exs = append(exs, ExclusionPair{
			expression: parser.String(exclusion.Expression),
			operator:   exclusion.Operator,
		})
	}
	var where string
	if exclusion.Where != nil {
		where = parser.String(exclusion.Where.Expr)
	}
	// PostgreSQL defaults to btree when no index method is specified
	indexType := strings.ToUpper(exclusion.IndexType.String())
	if indexType == "" {
		indexType = "BTREE"
	}
	return Exclusion{
		constraintName: exclusion.ConstraintName.String(),
		indexType:      indexType,
		exclusions:     exs,
		where:          where,
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

// Qualify Postgres/Mssql schema for object names (domains, types, functions, etc.)
func normalizedObjectName(mode GeneratorMode, objectName parser.ObjectName, defaultSchema string) string {
	name := objectName.Name.String()
	if mode == GeneratorModePostgres || mode == GeneratorModeMssql {
		if len(objectName.Schema.String()) > 0 {
			name = objectName.Schema.String() + "." + name
		} else {
			name = defaultSchema + "." + name
		}
	}
	return name
}

func normalizedTable(mode GeneratorMode, tableName string, defaultSchema string) string {
	switch mode {
	case GeneratorModePostgres, GeneratorModeMssql:
		if tableName == "" { // avoid qualifying empty references (e.g., built-in types)
			return ""
		}
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

func normalizeTableInComment(mode GeneratorMode, comment *parser.Comment, defaultSchema string) *parser.Comment {
	switch mode {
	case GeneratorModePostgres:
		// Expected format is [schema.]table.column
		objs := strings.Split(comment.Object, ".")
		switch len(objs) {
		case 1, 2:
			switch comment.ObjectType {
			case "OBJECT_TABLE":
				if len(objs) == 1 {
					// table -> defaultSchema.table
					objs = append([]string{defaultSchema}, objs...)
				}
			case "OBJECT_COLUMN":
				if len(objs) == 2 {
					// table.column -> defaultSchema.table.column
					objs = append([]string{defaultSchema}, objs...)
				}
			}
			return &parser.Comment{
				ObjectType: comment.ObjectType,
				Object:     strings.Join(objs, "."),
				Comment:    comment.Comment,
			}
		case 3: // complete-case (schema.table.column). no-op
			return comment
		default: // abnormal-case. fallback
			return comment
		}
	default:
		return comment
	}
}

var regexCommentOnClause = regexp.MustCompile(`(?i)^(\s*COMMENT\s+ON\s+(?:TABLE|COLUMN)\s+)(?P<dotConcatTblObjs>.*)(\s+IS\s+(?:'[^']*'|NULL)\s*$)`)

// Assume that give 'defaultSchema' is not quoted with double-quote and not surrounded with whitespaces.
func normalizeTableInCommentOnStmt(mode GeneratorMode, comment *parser.Comment, ddl string, defaultSchema string) string {
	if defaultSchema == "" {
		return ddl // fallback
	}
	if mode != GeneratorModePostgres {
		return ddl // no special handling for non-Postgres
	} else {
		// Ignore line comment
		if ok, _ := regexp.MatchString(`^\s*--`, ddl); ok {
			// err is returned from MatchString only when pattern is invalid, so just ignore.
			return ddl
		}
		matches := regexCommentOnClause.FindStringSubmatch(ddl)
		if len(matches) != 4 {
			// Neither table nor column name is found in COMMENT
			return ddl // fallback
		}
		objs := make([]string, 0, 3) // objects of 'schema, table, and column'
		sb := strings.Builder{}
		q := false // true if in double quoting.
		for _, c := range matches[2] {
			switch c {
			case '.':
				if q { // '.' is a char if double-quoted.
					sb.WriteRune(c)
				} else { // "." is a separator.
					if sb.Len() > 0 { // separate with '.' if not separated by `"` previously.
						objs = append(objs, sb.String())
						sb.Reset()
					}
				}
			case '"': // If either schema, table or column is double-quoted.
				sb.WriteRune(c)
				if q { // End double quoting.
					objs = append(objs, sb.String())
					sb.Reset()
				}
				q = !q
			default:
				sb.WriteRune(c)
			}
		}
		if sb.Len() > 0 { // flush buffer.
			objs = append(objs, sb.String())
			sb.Reset()
		}
		switch l := len(objs); {
		case l == 1 || l == 2:
			switch comment.ObjectType {
			case "OBJECT_TABLE":
				if len(objs) == 1 {
					return fmt.Sprintf(`%s%s.%s%s`, matches[1], defaultSchema, objs[0], matches[3])
				}
			case "OBJECT_COLUMN":
				if len(objs) == 2 {
					return fmt.Sprintf(`%s%s.%s.%s%s`, matches[1], defaultSchema, objs[0], objs[1], matches[3])
				}
			}
		case l == 3:
			return ddl // no need to normalize.
		}
		// fallback in other exceptional cases
		return ddl
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

// extractRenameFrom extracts the rename annotation from a comment string
// Supports both @renamed (preferred) and @rename (deprecated)
// e.g. "-- @renamed from=old_column_name" -> "old_column_name"
// e.g. "-- @rename from=\"foo bar\"" -> "foo bar"
func extractRenameFrom(comment string) string {
	// First try to match @renamed (preferred)
	reRenamed := regexp.MustCompile(`@renamed\s+from=(?:"([^"]+)"|(\S+))`)
	matches := reRenamed.FindStringSubmatch(comment)

	// If @renamed not found, try @rename (deprecated) for backward compatibility
	if len(matches) == 0 {
		reRename := regexp.MustCompile(`@rename\s+from=(?:"([^"]+)"|(\S+))`)
		matches = reRename.FindStringSubmatch(comment)

		// If @rename is found, issue a deprecation warning
		if len(matches) > 0 {
			fmt.Fprintf(os.Stderr, "-- WARNING: @rename is deprecated. Please use @renamed instead.\n")
		}
	}

	// The regex has 2 capture groups (double quotes or unquoted)
	// matches[0] = full match, matches[1] = quoted, matches[2] = unquoted
	if len(matches) >= 3 {
		if matches[1] != "" {
			return matches[1] // double-quoted identifier
		}
		if matches[2] != "" {
			return matches[2] // unquoted identifier
		}
	}
	return ""
}

// generatorModeToParserMode converts GeneratorMode to ParserMode
func generatorModeToParserMode(mode GeneratorMode) parser.ParserMode {
	switch mode {
	case GeneratorModeMysql:
		return parser.ParserModeMysql
	case GeneratorModePostgres:
		return parser.ParserModePostgres
	case GeneratorModeSQLite3:
		return parser.ParserModeSQLite3
	case GeneratorModeMssql:
		return parser.ParserModeMssql
	default:
		return parser.ParserModeMysql
	}
}

func extractTableComment(rawDDL string, mode GeneratorMode) string {
	tokenizer := parser.NewTokenizer(rawDDL, generatorModeToParserMode(mode))
	tokenizer.AllowComments = true

	var foundCreate, foundTable bool
	var firstComment string // Store the first comment after CREATE TABLE

	for {
		tok, val := tokenizer.Scan()
		if tok == 0 {
			break // EOF
		}

		// Look for CREATE keyword
		if tok == parser.CREATE {
			foundCreate = true
			continue
		}

		// Look for TABLE keyword after CREATE
		if foundCreate && tok == parser.TABLE {
			foundTable = true
			foundCreate = false
			continue
		}

		// After CREATE TABLE, capture the first comment we encounter
		// This could be before or after the opening parenthesis
		if foundTable && tok == parser.COMMENT && firstComment == "" {
			comment := string(val)
			comment = strings.TrimSpace(comment)
			firstComment = comment
			// Continue scanning to handle all cases
		}

		// Reset if we found CREATE but next token is not TABLE
		if foundCreate && tok != parser.TABLE {
			foundCreate = false
		}
	}

	return firstComment
}

// extractColumnComments extracts inline comments (-- comments) from a CREATE TABLE statement
// and maps them to column names
func extractColumnComments(rawDDL string, mode GeneratorMode) map[string]string {
	comments := make(map[string]string)

	tokenizer := parser.NewTokenizer(rawDDL, generatorModeToParserMode(mode))
	tokenizer.AllowComments = true

	var foundCreate bool
	var inCreateTable bool
	var parenDepth int
	var currentColumnName string
	var expectingColumnDef bool

	for {
		tok, val := tokenizer.Scan()
		if tok == 0 {
			break // EOF
		}

		// Track CREATE TABLE statements
		if tok == parser.CREATE {
			foundCreate = true
			continue
		}

		if foundCreate && tok == parser.TABLE {
			foundCreate = false
			inCreateTable = true
			parenDepth = 0
			currentColumnName = ""
			expectingColumnDef = false
			continue
		}

		// Reset if we found CREATE but next token is not TABLE
		if foundCreate && tok != parser.TABLE {
			foundCreate = false
		}

		// Track parentheses depth to know when we're inside column definitions
		if inCreateTable {
			switch tok {
			case '(':
				parenDepth++
				if parenDepth == 1 {
					expectingColumnDef = true
				}
			case ')':
				parenDepth--
				if parenDepth == 0 {
					inCreateTable = false
				}
			case ',':
				// After a comma inside the table definition, expect a new column
				if parenDepth == 1 {
					expectingColumnDef = true
					// Don't clear currentColumnName yet - the comment might come after the comma
				}
			case parser.ID:
				// Capture potential column name at the start of a column definition
				if expectingColumnDef && parenDepth == 1 {
					currentColumnName = string(val)
					expectingColumnDef = false
				}
			case parser.COMMENT:
				// Associate comment with the current column name
				// Comments can appear after the column definition but before the next column
				if inCreateTable && currentColumnName != "" && parenDepth == 1 {
					comment := string(val)
					comment = strings.TrimSpace(comment)
					// Only store if we haven't already stored a comment for this column
					if _, exists := comments[currentColumnName]; !exists {
						comments[currentColumnName] = comment
					}
				}
			}
		}
	}

	return comments
}

func extractIndexComments(rawDDL string, mode GeneratorMode) map[string]string {
	comments := make(map[string]string)

	tokenizer := parser.NewTokenizer(rawDDL, generatorModeToParserMode(mode))
	tokenizer.AllowComments = true

	var inCreateTable bool
	var parenDepth int
	var expectingIndexDef bool
	var currentIndexName string
	var afterIndexKeyword bool
	var afterUniqueKeyword bool
	var keyKeywordSeen bool
	var afterConstraintKeyword bool
	var constraintName string

	for {
		tok, val := tokenizer.Scan()
		if tok == 0 {
			break // EOF
		}

		// Track CREATE TABLE statements
		if tok == parser.CREATE {
			// Scan ahead to see if it's CREATE TABLE
			for {
				nextTok, _ := tokenizer.Scan()
				if nextTok == 0 {
					break
				}
				if nextTok == parser.TABLE {
					inCreateTable = true
					parenDepth = 0
					currentIndexName = ""
					expectingIndexDef = false
					afterIndexKeyword = false
					afterUniqueKeyword = false
					keyKeywordSeen = false
					break
				}
				if nextTok != parser.IF && nextTok != parser.NOT && nextTok != parser.EXISTS {
					break
				}
			}
			continue
		}

		// Track parentheses depth to know when we're inside table definition
		if inCreateTable {
			switch tok {
			case '(':
				parenDepth++
			case ')':
				parenDepth--
				if parenDepth == 0 {
					inCreateTable = false
					currentIndexName = ""
				}
			case ',':
				// After a comma inside the table definition, reset index tracking
				if parenDepth == 1 {
					expectingIndexDef = false
					afterIndexKeyword = false
					afterUniqueKeyword = false
					keyKeywordSeen = false
					afterConstraintKeyword = false
					constraintName = ""
					// Don't clear currentIndexName yet - the comment might come after the comma
				}
			case parser.INDEX, parser.KEY:
				// Found an INDEX or KEY keyword inside CREATE TABLE
				if parenDepth == 1 {
					afterIndexKeyword = true
					keyKeywordSeen = (tok == parser.KEY)
					expectingIndexDef = true
					currentIndexName = ""
				}
			case parser.UNIQUE:
				// Found UNIQUE keyword which might be followed by INDEX or KEY
				if parenDepth == 1 {
					if afterConstraintKeyword && constraintName != "" {
						// This is a CONSTRAINT ... UNIQUE definition
						// Use the constraint name as the index name
						currentIndexName = constraintName
						afterConstraintKeyword = false
						constraintName = ""
					} else {
						afterUniqueKeyword = true
						expectingIndexDef = true
						currentIndexName = ""
					}
				}
			case parser.CONSTRAINT:
				// CONSTRAINT can be followed by a name and then UNIQUE, which creates an index
				if parenDepth == 1 {
					expectingIndexDef = false
					afterIndexKeyword = false
					afterUniqueKeyword = false
					keyKeywordSeen = false
					afterConstraintKeyword = true
					currentIndexName = ""
					constraintName = ""
				}
			case parser.PRIMARY, parser.FOREIGN, parser.CHECK:
				// These indicate other types of constraints, not regular indexes
				if parenDepth == 1 {
					expectingIndexDef = false
					afterIndexKeyword = false
					afterUniqueKeyword = false
					keyKeywordSeen = false
					afterConstraintKeyword = false
					constraintName = ""
					currentIndexName = ""
				}
			case parser.ID:
				// Capture potential index name or constraint name
				if parenDepth == 1 {
					if afterConstraintKeyword && constraintName == "" {
						// This is the constraint name
						constraintName = string(val)
						// Keep afterConstraintKeyword true to catch UNIQUE keyword next
					} else if expectingIndexDef {
						if afterIndexKeyword || (afterUniqueKeyword && keyKeywordSeen) {
							// This is the index name
							currentIndexName = string(val)
							expectingIndexDef = false
							afterIndexKeyword = false
							afterUniqueKeyword = false
						} else if afterUniqueKeyword {
							// Check if this ID is "KEY" or "INDEX"
							idStr := string(val)
							if strings.EqualFold(idStr, "KEY") || strings.EqualFold(idStr, "INDEX") {
								keyKeywordSeen = true
								// Next ID will be the index name
							} else {
								// This is the index name for UNIQUE without KEY/INDEX keyword
								currentIndexName = idStr
								expectingIndexDef = false
								afterUniqueKeyword = false
							}
						}
					}
				}
			case parser.COMMENT:
				// Associate comment with the current index name
				if inCreateTable && currentIndexName != "" && parenDepth == 1 {
					comment := string(val)
					comment = strings.TrimSpace(comment)
					// Only store if we haven't already stored a comment for this index
					if _, exists := comments[currentIndexName]; !exists {
						comments[currentIndexName] = comment
					}
				}
			}
		}
	}

	// Now handle standalone CREATE INDEX statements
	tokenizer = parser.NewTokenizer(rawDDL, generatorModeToParserMode(mode))
	tokenizer.AllowComments = true

	var foundCreate bool
	var foundIndex bool
	var foundIndexName bool
	var indexName string

	for {
		tok, val := tokenizer.Scan()
		if tok == 0 {
			break // EOF
		}

		switch tok {
		case parser.CREATE:
			foundCreate = true
			foundIndex = false
			foundIndexName = false
			indexName = ""
		case parser.UNIQUE:
			// UNIQUE can appear after CREATE
			if foundCreate {
				// Continue looking for INDEX
			}
		case parser.INDEX:
			if foundCreate {
				foundIndex = true
				foundCreate = false
			}
		case parser.IF:
			// Part of CREATE INDEX IF NOT EXISTS
			// Next tokens will be NOT and EXISTS
		case parser.NOT, parser.EXISTS:
			// Part of IF NOT EXISTS
			continue
		case parser.ID:
			if foundIndex && !foundIndexName {
				// This is the index name
				indexName = string(val)
				foundIndexName = true
			}
		case parser.COMMENT:
			// Associate comment with the index from CREATE INDEX statement
			if foundIndexName && indexName != "" {
				comment := string(val)
				comment = strings.TrimSpace(comment)
				// Only store if we haven't already stored a comment for this index
				if _, exists := comments[indexName]; !exists {
					comments[indexName] = comment
				}
				// Reset for next potential index
				foundIndexName = false
				indexName = ""
			}
		case parser.ON:
			// After ON keyword, we're past the index name
			if foundIndex {
				foundIndex = false
				foundIndexName = false
			}
		}
	}

	return comments
}
