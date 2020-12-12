// TODO: Normalize implicit things in input first, and then compare
package schema

import (
	"fmt"
	"log"
	"reflect"
	"sort"
	"strings"
)

type GeneratorMode int

const (
	GeneratorModeMysql = GeneratorMode(iota)
	GeneratorModePostgres
	GeneratorModeSQLite3
)

var (
	dataTypeAliases = map[string]string{
		"bool":    "boolean",
		"int":     "integer",
		"char":    "character",
		"varchar": "character varying",
	}
	mysqlDataTypeAliases = map[string]string{
		"boolean": "tinyint",
	}
)

// This struct holds simulated schema states during GenerateIdempotentDDLs().
type Generator struct {
	mode          GeneratorMode
	desiredTables []*Table
	currentTables []*Table

	desiredViews []*View
	currentViews []*View
}

// Parse argument DDLs and call `generateDDLs()`
func GenerateIdempotentDDLs(mode GeneratorMode, desiredSQL string, currentSQL string) ([]string, error) {
	// TODO: invalidate duplicated tables, columns
	desiredDDLs, err := parseDDLs(mode, desiredSQL)
	if err != nil {
		return nil, err
	}

	currentDDLs, err := parseDDLs(mode, currentSQL)
	if err != nil {
		return nil, err
	}

	tables, err := convertDDLsToTables(currentDDLs)
	if err != nil {
		return nil, err
	}

	views := convertDDLsToViews(currentDDLs)

	generator := Generator{
		mode:          mode,
		desiredTables: []*Table{},
		currentTables: tables,
		desiredViews:  []*View{},
		currentViews:  views,
	}
	return generator.generateDDLs(desiredDDLs)
}

// Main part of DDL genearation
func (g *Generator) generateDDLs(desiredDDLs []DDL) ([]string, error) {
	ddls := []string{}

	// Incrementally examine desiredDDLs
	for _, ddl := range desiredDDLs {
		switch desired := ddl.(type) {
		case *CreateTable:
			if currentTable := findTableByName(g.currentTables, desired.table.name); currentTable != nil {
				// Table already exists, guess required DDLs.
				tableDDLs, err := g.generateDDLsForCreateTable(*currentTable, *desired)
				if err != nil {
					return ddls, err
				}
				ddls = append(ddls, tableDDLs...)
				mergeTable(currentTable, desired.table)
			} else {
				// Table not found, create table.
				ddls = append(ddls, desired.statement)
				table := desired.table // copy table
				g.currentTables = append(g.currentTables, &table)
			}
			table := desired.table // copy table
			g.desiredTables = append(g.desiredTables, &table)
		case *CreateIndex:
			indexDDLs, err := g.generateDDLsForCreateIndex(desired.tableName, desired.index, "CREATE INDEX", ddl.Statement())
			if err != nil {
				return ddls, err
			}
			ddls = append(ddls, indexDDLs...)
		case *AddIndex:
			indexDDLs, err := g.generateDDLsForCreateIndex(desired.tableName, desired.index, "ALTER TABLE", ddl.Statement())
			if err != nil {
				return ddls, err
			}
			ddls = append(ddls, indexDDLs...)
		case *AddPolicy:
			policyDDLs, err := g.generateDDLsForCreatePolicy(desired.tableName, desired.policy, "ALTER POLICY", ddl.Statement())
			if err != nil {
				return ddls, err
			}
			ddls = append(ddls, policyDDLs...)
		case *View:
			viewDDLs, err := g.generateDDLsForCreateView(desired.name, desired)
			if err != nil {
				return ddls, err
			}
			ddls = append(ddls, viewDDLs...)
		default:
			return nil, fmt.Errorf("unexpected ddl type in generateDDLs: %v", desired)
		}
	}

	// Clean up obsoleted tables, indexes, columns
	for _, currentTable := range g.currentTables {
		desiredTable := findTableByName(g.desiredTables, currentTable.name)
		if desiredTable == nil {
			// Obsoleted table found. Drop table.
			ddls = append(ddls, fmt.Sprintf("DROP TABLE %s", g.escapeTableName(currentTable.name)))
			g.currentTables = removeTableByName(g.currentTables, currentTable.name)
			continue
		}

		// Table is expected to exist. Drop foreign keys prior to index deletion
		for _, foreignKey := range currentTable.foreignKeys {
			if containsString(convertForeignKeysToConstraintNames(desiredTable.foreignKeys), foreignKey.constraintName) {
				continue // Foreign key is expected to exist.
			}

			// The foreign key seems obsoleted. Check and drop it as needed.
			foreignKeyDDLs := g.generateDDLsForAbsentForeignKey(foreignKey, *currentTable, *desiredTable)
			ddls = append(ddls, foreignKeyDDLs...)
			// TODO: simulate to remove foreign key from `currentTable.foreignKeys`?
		}

		// Check indexes
		for _, index := range currentTable.indexes {
			if containsString(convertIndexesToIndexNames(desiredTable.indexes), index.name) ||
				containsString(convertForeignKeysToIndexNames(desiredTable.foreignKeys), index.name) {
				continue // Index is expected to exist.
			}

			// The index seems obsoleted. Check and drop it as needed.
			indexDDLs, err := g.generateDDLsForAbsentIndex(index, *currentTable, *desiredTable)
			if err != nil {
				return ddls, err
			}
			ddls = append(ddls, indexDDLs...)
			// TODO: simulate to remove index from `currentTable.indexes`?
		}

		// Check columns.
		for _, column := range currentTable.columns {
			if containsString(convertColumnsToColumnNames(desiredTable.columns), column.name) {
				continue // Column is expected to exist.
			}

			// Column is obsoleted. Drop column.
			ddl := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", g.escapeTableName(desiredTable.name), g.escapeSQLName(column.name))
			ddls = append(ddls, ddl)
			// TODO: simulate to remove column from `currentTable.columns`?
		}

		// Check policies.
		for _, policy := range currentTable.policies {
			if containsString(convertPolicyNames(desiredTable.policies), policy.name) {
				continue
			}
			ddls = append(ddls, fmt.Sprintf("DROP POLICY %s ON %s", g.escapeSQLName(policy.name), g.escapeTableName(currentTable.name)))
		}
	}

	// Clean up obsoleted views
	for _, currentView := range g.currentViews {
		if containsString(convertViewNames(g.desiredViews), currentView.name) {
			continue
		}
		ddls = append(ddls, fmt.Sprintf("DROP VIEW %s", g.escapeTableName(currentView.name)))
	}

	return ddls, nil
}

// In the caller, `mergeTable` manages `g.currentTables`.
func (g *Generator) generateDDLsForCreateTable(currentTable Table, desired CreateTable) ([]string, error) {
	ddls := []string{}

	// Examine each column
	for i, desiredColumn := range desired.table.columns {
		currentColumn := findColumnByName(currentTable.columns, desiredColumn.name)
		if currentColumn == nil || !currentColumn.autoIncrement {
			// We may not be able to add AUTO_INCREMENT yet. It will be added after adding keys (primary or not) at the "Add new AUTO_INCREMENT" place.
			desiredColumn.autoIncrement = false
		}
		if currentColumn == nil {
			definition, err := g.generateColumnDefinition(desiredColumn, true)
			if err != nil {
				return ddls, err
			}

			// Column not found, add column.
			ddl := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", g.escapeTableName(desired.table.name), definition)

			if g.mode == GeneratorModeMysql {
				after := " FIRST"
				if i > 0 {
					after = " AFTER " + g.escapeSQLName(desired.table.columns[i-1].name)
				}
				ddl += after
			}

			ddls = append(ddls, ddl)
		} else {
			// Change column data type or order as needed.
			switch g.mode {
			case GeneratorModeMysql:
				currentPos := currentColumn.position
				desiredPos := desiredColumn.position
				changeOrder := currentPos > desiredPos && currentPos-desiredPos > len(currentTable.columns)-len(desired.table.columns)

				// Change column type and orders, *except* AUTO_INCREMENT and UNIQUE KEY.
				if !g.haveSameColumnDefinition(*currentColumn, desiredColumn) || !haveSameValue(currentColumn.defaultVal, desiredColumn.defaultVal) || changeOrder {
					definition, err := g.generateColumnDefinition(desiredColumn, false)
					if err != nil {
						return ddls, err
					}

					ddl := fmt.Sprintf("ALTER TABLE %s CHANGE COLUMN %s %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentColumn.name), definition)
					if changeOrder {
						after := " FIRST"
						if i > 0 {
							after = " AFTER " + g.escapeSQLName(desired.table.columns[i-1].name)
						}
						ddl += after
					}
					ddls = append(ddls, ddl)
				}

				// Add UNIQUE KEY. TODO: Probably it should be just normalized to an index after the parser phase.
				currentIndex := findIndexByName(currentTable.indexes, desiredColumn.name)
				if desiredColumn.keyOption.isUnique() && !currentColumn.keyOption.isUnique() && currentIndex == nil { // TODO: deal with a case that the index is not a UNIQUE KEY.
					ddl := fmt.Sprintf("ALTER TABLE %s ADD UNIQUE KEY %s(%s)", g.escapeTableName(desired.table.name), g.escapeSQLName(desiredColumn.name), g.escapeSQLName(desiredColumn.name))
					ddls = append(ddls, ddl)
				}
			case GeneratorModePostgres:
				if !g.haveSameDataType(*currentColumn, desiredColumn) {
					// Change type
					ddl := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentColumn.name), generateDataType(desiredColumn))
					ddls = append(ddls, ddl)
				}

				// TODO: support adding a column's `references`
				// TODO: support SET/DROP NOT NULL and other properties
			default:
			}
		}
	}

	// Remove old AUTO_INCREMENT from deleted column before deleting key (primary or not)
	if g.mode == GeneratorModeMysql {
		for _, currentColumn := range currentTable.columns {
			desiredColumn := findColumnByName(desired.table.columns, currentColumn.name)
			if currentColumn.autoIncrement && (desiredColumn == nil || !desiredColumn.autoIncrement) {
				currentColumn.autoIncrement = false
				definition, err := g.generateColumnDefinition(currentColumn, false)
				if err != nil {
					return ddls, err
				}
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s CHANGE COLUMN %s %s", g.escapeTableName(currentTable.name), g.escapeSQLName(currentColumn.name), definition))
			}
		}
	}

	// Examine primary key
	currentPrimaryKey := currentTable.PrimaryKey()
	desiredPrimaryKey := desired.table.PrimaryKey()
	if !areSamePrimaryKeys(currentPrimaryKey, desiredPrimaryKey) {
		if currentPrimaryKey != nil {
			switch g.mode {
			case GeneratorModeMysql:
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP PRIMARY KEY", g.escapeTableName(desired.table.name)))
			case GeneratorModePostgres:
				tableName := strings.SplitN(desired.table.name, ".", 2)[1] // without schema
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(desired.table.name), g.escapeSQLName(tableName+"_pkey")))
			default:
			}
		}
		if desiredPrimaryKey != nil {
			definition := g.generateIndexDefinition(*desiredPrimaryKey)
			ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ADD %s", g.escapeTableName(desired.table.name), definition))
		}
	}

	// Examine each index
	for _, desiredIndex := range desired.table.indexes {
		if desiredIndex.name == "PRIMARY" {
			continue
		}

		if currentIndex := findIndexByName(currentTable.indexes, desiredIndex.name); currentIndex != nil {
			// Drop and add index as needed.
			if !areSameIndexes(*currentIndex, desiredIndex) {
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP INDEX %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentIndex.name)))
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ADD %s", g.escapeTableName(desired.table.name), g.generateIndexDefinition(desiredIndex)))
			}
		} else {
			// Index not found, add index.
			ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ADD %s", g.escapeTableName(desired.table.name), g.generateIndexDefinition(desiredIndex)))
		}
	}

	// Add new AUTO_INCREMENT after adding index and primary key
	if g.mode == GeneratorModeMysql {
		for _, desiredColumn := range desired.table.columns {
			currentColumn := findColumnByName(currentTable.columns, desiredColumn.name)
			if desiredColumn.autoIncrement && (currentColumn == nil || !currentColumn.autoIncrement) {
				definition, err := g.generateColumnDefinition(desiredColumn, false)
				if err != nil {
					return ddls, err
				}
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s CHANGE COLUMN %s %s", g.escapeTableName(currentTable.name), g.escapeSQLName(desiredColumn.name), definition))
			}
		}
	}

	// Examine each foreign key
	for _, desiredForeignKey := range desired.table.foreignKeys {
		if len(desiredForeignKey.constraintName) == 0 {
			return ddls, fmt.Errorf(
				"Foreign key without constraint symbol was found in table '%s' (index name: '%s', columns: %v). "+
					"Specify the constraint symbol to identify the foreign key.",
				desired.table.name, desiredForeignKey.indexName, desiredForeignKey.indexColumns,
			)
		}

		if currentForeignKey := findForeignKeyByName(currentTable.foreignKeys, desiredForeignKey.constraintName); currentForeignKey != nil {
			// Drop and add foreign key as needed.
			if !g.areSameForeignKeys(*currentForeignKey, desiredForeignKey) {
				switch g.mode {
				case GeneratorModeMysql:
					ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP FOREIGN KEY %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentForeignKey.constraintName)))
				case GeneratorModePostgres:
					ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentForeignKey.constraintName)))
				default:
				}
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ADD %s", g.escapeTableName(desired.table.name), g.generateForeignKeyDefinition(desiredForeignKey)))
			}
		} else {
			// Foreign key not found, add foreign key.
			definition := g.generateForeignKeyDefinition(desiredForeignKey)
			ddl := fmt.Sprintf("ALTER TABLE %s ADD %s", g.escapeTableName(desired.table.name), definition)
			ddls = append(ddls, ddl)
		}
	}

	return ddls, nil
}

// Shared by `CREATE INDEX` and `ALTER TABLE ADD INDEX`.
// This manages `g.currentTables` unlike `generateDDLsForCreateTable`...
func (g *Generator) generateDDLsForCreateIndex(tableName string, desiredIndex Index, action string, statement string) ([]string, error) {
	ddls := []string{}

	currentTable := findTableByName(g.currentTables, tableName)
	if currentTable == nil {
		return nil, fmt.Errorf("%s is performed for inexistent table '%s': '%s'", action, tableName, statement)
	}

	currentIndex := findIndexByName(currentTable.indexes, desiredIndex.name)
	if currentIndex == nil {
		// Index not found, add index.
		ddls = append(ddls, statement)
		currentTable.indexes = append(currentTable.indexes, desiredIndex)
	} else {
		// Index found. If it's different, drop and add index.
		if !areSameIndexes(*currentIndex, desiredIndex) {
			ddls = append(ddls, g.generateDropIndex(currentTable.name, currentIndex.name))
			ddls = append(ddls, statement)

			newIndexes := []Index{}
			for _, currentIndex := range currentTable.indexes {
				if currentIndex.name == desiredIndex.name {
					newIndexes = append(newIndexes, desiredIndex)
				} else {
					newIndexes = append(newIndexes, currentIndex)
				}
			}
			currentTable.indexes = newIndexes // simulate index change. TODO: use []*Index in table and destructively modify it
		}
	}

	// Examine indexes in desiredTable to delete obsoleted indexes later
	desiredTable := findTableByName(g.desiredTables, tableName)
	if desiredTable == nil {
		return nil, fmt.Errorf("%s is performed before create table '%s': '%s'", action, tableName, statement)
	}
	if containsString(convertIndexesToIndexNames(desiredTable.indexes), desiredIndex.name) {
		return nil, fmt.Errorf("index '%s' is doubly created against table '%s': '%s'", desiredIndex.name, tableName, statement)
	}
	desiredTable.indexes = append(desiredTable.indexes, desiredIndex)

	return ddls, nil
}

func (g *Generator) generateDDLsForCreatePolicy(tableName string, desiredPolicy Policy, action string, statement string) ([]string, error) {
	var ddls []string

	currentTable := findTableByName(g.currentTables, tableName)
	if currentTable == nil {
		return nil, fmt.Errorf("%s is performed for inexistent table '%s': '%s'", action, tableName, statement)
	}

	currentPolicy := findPolicyByName(currentTable.policies, desiredPolicy.name)
	if currentPolicy == nil {
		// Policy not found, add policy.
		ddls = append(ddls, statement)
		currentTable.policies = append(currentTable.policies, desiredPolicy)
	} else {
		// policy found. If it's different, drop and add or alter policy.
		if !areSamePolicies(*currentPolicy, desiredPolicy) {
			ddls = append(ddls, fmt.Sprintf("DROP POLICY %s ON %s", g.escapeSQLName(currentPolicy.name), g.escapeTableName(currentTable.name)))
			ddls = append(ddls, statement)
		}
	}

	// Examine policies in desiredTable to delete obsoleted policies later
	desiredTable := findTableByName(g.desiredTables, tableName)
	if desiredTable == nil {
		return nil, fmt.Errorf("%s is performed before create table '%s': '%s'", action, tableName, statement)
	}
	if containsString(convertPolicyNames(desiredTable.policies), desiredPolicy.name) {
		return nil, fmt.Errorf("policy '%s' is doubly created against table '%s': '%s'", desiredPolicy.name, tableName, statement)
	}
	desiredTable.policies = append(desiredTable.policies, desiredPolicy)

	return ddls, nil
}

func (g *Generator) generateDDLsForCreateView(viewName string, desiredView *View) ([]string, error) {
	var ddls []string

	currentView := findViewByName(g.currentViews, viewName)
	if currentView == nil {
		// View not found, add view.
		ddls = append(ddls, desiredView.statement)
	} else {
		// View found. If it's different, create or replace view.
		if strings.ToLower(currentView.definition) != strings.ToLower(desiredView.definition) {
			if g.mode == GeneratorModeSQLite3 {
				ddls = append(ddls, fmt.Sprintf("DROP VIEW %s", g.escapeTableName(viewName)))
				ddls = append(ddls, fmt.Sprintf("CREATE VIEW %s AS %s", g.escapeTableName(viewName), desiredView.definition))
			} else {
				ddls = append(ddls, fmt.Sprintf("CREATE OR REPLACE VIEW %s AS %s", g.escapeTableName(viewName), desiredView.definition))
			}
		}
	}

	// Examine policies in desiredTable to delete obsoleted policies later
	if containsString(convertViewNames(g.desiredViews), desiredView.name) {
		return nil, fmt.Errorf("view '%s' is doubly created: '%s'", desiredView.name, desiredView.statement)
	}
	g.desiredViews = append(g.desiredViews, desiredView)

	return ddls, nil
}

// Even though simulated table doesn't have a foreign key, references could exist in column definitions.
// This carefully generates DROP CONSTRAINT for such situations.
func (g *Generator) generateDDLsForAbsentForeignKey(currentForeignKey ForeignKey, currentTable Table, desiredTable Table) []string {
	ddls := []string{}

	switch g.mode {
	case GeneratorModeMysql:
		ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP FOREIGN KEY %s", g.escapeTableName(currentTable.name), g.escapeSQLName(currentForeignKey.constraintName)))
	case GeneratorModePostgres:
		var referencesColumn *Column
		for _, column := range desiredTable.columns {
			if column.references == currentForeignKey.referenceName {
				referencesColumn = &column
				break
			}
		}

		if referencesColumn == nil {
			ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(currentTable.name), g.escapeSQLName(currentForeignKey.constraintName)))
		}
	default:
	}

	return ddls
}

// Even though simulated table doesn't have an index, primary or unique could exist in column definitions.
// This carefully generates DROP INDEX for such situations.
func (g *Generator) generateDDLsForAbsentIndex(currentIndex Index, currentTable Table, desiredTable Table) ([]string, error) {
	ddls := []string{}

	if currentIndex.primary {
		var primaryKeyColumn *Column
		for _, column := range desiredTable.columns {
			if column.keyOption == ColumnKeyPrimary {
				primaryKeyColumn = &column
				break
			}
		}

		// If nil, it will be `DROP COLUMN`-ed. Ignore it.
		if primaryKeyColumn != nil && primaryKeyColumn.name != currentIndex.columns[0].column { // TODO: check length of currentIndex.columns
			// TODO: handle this. Rename primary key column...?
			return ddls, fmt.Errorf(
				"primary key column name of '%s' should be '%s' but currently '%s'. This is not handled yet.",
				currentTable.name, primaryKeyColumn.name, currentIndex.columns[0].column,
			)
		}
	} else if currentIndex.unique {
		var uniqueKeyColumn *Column
		for _, column := range desiredTable.columns {
			if column.name == currentIndex.columns[0].column && column.keyOption.isUnique() {
				uniqueKeyColumn = &column
				break
			}
		}

		if uniqueKeyColumn == nil {
			// No unique column. Drop unique key index.
			ddls = append(ddls, g.generateDropIndex(currentTable.name, currentIndex.name))
		}
	} else {
		ddls = append(ddls, g.generateDropIndex(currentTable.name, currentIndex.name))
	}

	return ddls, nil
}

func generateDataType(column Column) string {
	suffix := ""
	if column.array {
		suffix = "[]"
	}

	if column.length != nil {
		if column.scale != nil {
			return fmt.Sprintf("%s(%s, %s)%s", column.typeName, string(column.length.raw), string(column.scale.raw), suffix)
		} else {
			return fmt.Sprintf("%s(%s)%s", column.typeName, string(column.length.raw), suffix)
		}
	} else {
		switch column.typeName {
		case "enum":
			return fmt.Sprintf("%s(%s)%s", column.typeName, strings.Join(column.enumValues, ", "), suffix)
		default:
			return fmt.Sprintf("%s%s", column.typeName, suffix)
		}
	}
}

func (g *Generator) generateColumnDefinition(column Column, enableUnique bool) (string, error) {
	// TODO: make string concatenation faster?

	definition := fmt.Sprintf("%s %s ", g.escapeSQLName(column.name), generateDataType(column))

	if column.unsigned {
		definition += "UNSIGNED "
	}
	if column.timezone {
		definition += "WITH TIME ZONE "
	}

	// [CHARACTER SET] and [COLLATE] should be placed before [NOT NULL | NULL] on MySQL
	if column.charset != "" {
		definition += fmt.Sprintf("CHARACTER SET %s ", column.charset)
	}
	if column.collate != "" {
		definition += fmt.Sprintf("COLLATE %s ", column.collate)
	}

	if (column.notNull != nil && *column.notNull) || column.keyOption == ColumnKeyPrimary {
		definition += "NOT NULL "
	} else if column.notNull != nil && !*column.notNull {
		definition += "NULL "
	}

	if column.defaultVal != nil {
		switch column.defaultVal.valueType {
		case ValueTypeStr:
			definition += fmt.Sprintf("DEFAULT '%s' ", column.defaultVal.strVal)
		case ValueTypeInt:
			definition += fmt.Sprintf("DEFAULT %d ", column.defaultVal.intVal)
		case ValueTypeFloat:
			definition += fmt.Sprintf("DEFAULT %f ", column.defaultVal.floatVal)
		case ValueTypeBit:
			if column.defaultVal.bitVal {
				definition += "DEFAULT b'1' "
			} else {
				definition += "DEFAULT b'0' "
			}
		case ValueTypeValArg: // NULL, CURRENT_TIMESTAMP, ...
			definition += fmt.Sprintf("DEFAULT %s ", string(column.defaultVal.raw))
		default:
			return "", fmt.Errorf("unsupported default value type (valueType: '%d') in column: %#v", column.defaultVal.valueType, column)
		}
	}

	if column.autoIncrement {
		definition += "AUTO_INCREMENT "
	}

	if column.onUpdate != nil {
		definition += fmt.Sprintf("ON UPDATE %s ", string(column.onUpdate.raw))
	}

	switch column.keyOption {
	case ColumnKeyNone:
		// noop
	case ColumnKeyUnique:
		if enableUnique {
			definition += "UNIQUE "
		}
	case ColumnKeyUniqueKey:
		if enableUnique {
			definition += "UNIQUE KEY "
		}
	case ColumnKeyPrimary:
		// noop
	default:
		return "", fmt.Errorf("unsupported column key (keyOption: '%d') in column: %#v", column.keyOption, column)
	}

	definition = strings.TrimSuffix(definition, " ")
	return definition, nil
}

// For CREATE TABLE.
func (g *Generator) generateIndexDefinition(index Index) string {
	definition := index.indexType // indexType is only available on `CREATE TABLE`, but only `generateDDLsForCreateTable` is using this

	columns := []string{}
	for _, indexColumn := range index.columns {
		columns = append(columns, g.escapeSQLName(indexColumn.column))
	}

	if index.primary {
		definition += fmt.Sprintf(
			" (%s)",
			strings.Join(columns, ", "),
		)
	} else {
		definition += fmt.Sprintf(
			" %s(%s)",
			g.escapeSQLName(index.name),
			strings.Join(columns, ", "),
		)
	}
	return definition
}

func (g *Generator) generateForeignKeyDefinition(foreignKey ForeignKey) string {
	// TODO: make string concatenation faster?

	// Empty constraint name is already invalidated in generateDDLsForCreateIndex
	definition := fmt.Sprintf("CONSTRAINT %s FOREIGN KEY ", g.escapeSQLName(foreignKey.constraintName))

	if len(foreignKey.indexName) > 0 {
		definition += fmt.Sprintf("%s ", g.escapeSQLName(foreignKey.indexName))
	}

	var indexColumns, referenceColumns []string
	for _, column := range foreignKey.indexColumns {
		indexColumns = append(indexColumns, g.escapeSQLName(column))
	}
	for _, column := range foreignKey.referenceColumns {
		referenceColumns = append(referenceColumns, g.escapeSQLName(column))
	}

	definition += fmt.Sprintf(
		"(%s) REFERENCES %s (%s) ",
		strings.Join(indexColumns, ","), g.escapeSQLName(foreignKey.referenceName),
		strings.Join(referenceColumns, ","),
	)

	if len(foreignKey.onDelete) > 0 {
		definition += fmt.Sprintf("ON DELETE %s ", foreignKey.onDelete)
	}
	if len(foreignKey.onUpdate) > 0 {
		definition += fmt.Sprintf("ON UPDATE %s ", foreignKey.onUpdate)
	}

	return strings.TrimSuffix(definition, " ")
}

func (g *Generator) generateDropIndex(tableName string, indexName string) string {
	switch g.mode {
	case GeneratorModeMysql:
		return fmt.Sprintf("ALTER TABLE %s DROP INDEX %s", g.escapeTableName(tableName), g.escapeSQLName(indexName))
	case GeneratorModePostgres:
		return fmt.Sprintf("DROP INDEX %s", g.escapeSQLName(indexName))
	default:
		return ""
	}
}

func (g *Generator) escapeTableName(name string) string {
	switch g.mode {
	case GeneratorModePostgres:
		schemaTable := strings.SplitN(name, ".", 2)
		var schemaName, tableName string
		if len(schemaTable) == 1 {
			schemaName, tableName = "public", schemaTable[0]
		} else {
			schemaName, tableName = schemaTable[0], schemaTable[1]
		}

		return g.escapeSQLName(schemaName) + "." + g.escapeSQLName(tableName)
	default:
		return g.escapeSQLName(name)
	}
}

func (g *Generator) escapeSQLName(name string) string {
	switch g.mode {
	case GeneratorModePostgres:
		return fmt.Sprintf("\"%s\"", name)
	default:
		return fmt.Sprintf("`%s`", name)
	}
}

func isPrimaryKey(column Column, table Table) bool {
	if column.keyOption == ColumnKeyPrimary {
		return true
	}

	for _, index := range table.indexes {
		if index.primary && index.columns[0].column == column.name {
			return true
		}
	}
	return false
}

// Destructively modify table1 to have table2 columns/indexes
func mergeTable(table1 *Table, table2 Table) {
	for _, column := range table2.columns {
		if containsString(convertColumnsToColumnNames(table1.columns), column.name) {
			table1.columns = append(table1.columns, column)
		}
	}

	for _, index := range table2.indexes {
		if containsString(convertIndexesToIndexNames(table1.indexes), index.name) {
			table1.indexes = append(table1.indexes, index)
		}
	}
}

func convertDDLsToTables(ddls []DDL) ([]*Table, error) {
	tables := []*Table{}
	for _, ddl := range ddls {
		switch stmt := ddl.(type) {
		case *CreateTable:
			table := stmt.table // copy table
			tables = append(tables, &table)
		case *CreateIndex:
			table := findTableByName(tables, stmt.tableName)
			if table == nil {
				return nil, fmt.Errorf("CREATE INDEX is performed before CREATE TABLE: %s", ddl.Statement())
			}
			// TODO: check duplicated creation
			table.indexes = append(table.indexes, stmt.index)
		case *AddPrimaryKey:
			table := findTableByName(tables, stmt.tableName)
			if table == nil {
				return nil, fmt.Errorf("ADD PRIMARY KEY is performed before CREATE TABLE: %s", ddl.Statement())
			}

			newColumns := []Column{}
			for _, column := range table.columns {
				if column.name == stmt.index.columns[0].column { // TODO: multi-column primary key?
					column.keyOption = ColumnKeyPrimary
				}
				newColumns = append(newColumns, column)
			}
			table.columns = newColumns
		case *AddForeignKey:
			table := findTableByName(tables, stmt.tableName)
			if table == nil {
				return nil, fmt.Errorf("ADD FOREIGN KEY is performed before CREATE TABLE: %s", ddl.Statement())
			}

			table.foreignKeys = append(table.foreignKeys, stmt.foreignKey)
		case *AddPolicy:
			table := findTableByName(tables, stmt.tableName)
			if table == nil {
				return nil, fmt.Errorf("ADD POLICY performed before CREATE TABLE: %s", ddl.Statement())
			}

			table.policies = append(table.policies, stmt.policy)
		case *View:
			// do nothing
		default:
			return nil, fmt.Errorf("unexpected ddl type in convertDDLsToTables: %v", stmt)
		}
	}
	return tables, nil
}

func convertDDLsToViews(ddls []DDL) []*View {
	var views []*View
	for _, ddl := range ddls {
		if view, ok := ddl.(*View); ok {
			views = append(views, view)
		}
	}
	return views
}

func findTableByName(tables []*Table, name string) *Table {
	for _, table := range tables {
		if table.name == name {
			return table
		}
	}
	return nil
}

func findColumnByName(columns []Column, name string) *Column {
	for _, column := range columns {
		if column.name == name {
			return &column
		}
	}
	return nil
}

func findIndexByName(indexes []Index, name string) *Index {
	for _, index := range indexes {
		if index.name == name {
			return &index
		}
	}
	return nil
}

func findForeignKeyByName(foreignKeys []ForeignKey, constraintName string) *ForeignKey {
	for _, foreignKey := range foreignKeys {
		if foreignKey.constraintName == constraintName {
			return &foreignKey
		}
	}
	return nil
}

func findPrimaryKey(indexes []Index) *Index {
	for _, index := range indexes {
		if index.primary {
			return &index
		}
	}
	return nil
}

func findPolicyByName(policies []Policy, name string) *Policy {
	for _, policy := range policies {
		if policy.name == name {
			return &policy
		}
	}
	return nil
}

func findViewByName(views []*View, name string) *View {
	for _, view := range views {
		if view.name == name {
			return view
		}
	}
	return nil
}
func (g *Generator) haveSameColumnDefinition(current Column, desired Column) bool {
	// Not examining AUTO_INCREMENT and UNIQUE KEY because it'll be added in a later stage
	return g.haveSameDataType(current, desired) &&
		(current.unsigned == desired.unsigned) &&
		((current.notNull != nil && *current.notNull) == ((desired.notNull != nil && *desired.notNull) || desired.keyOption == ColumnKeyPrimary)) && // `PRIMARY KEY` implies `NOT NULL`
		(current.timezone == desired.timezone) &&
		(desired.charset == "" || current.charset == desired.charset) && // detect change column only when set explicitly. TODO: can we calculate implicit charset?
		(desired.collate == "" || current.collate == desired.collate) && // detect change column only when set explicitly. TODO: can we calculate implicit collate?
		reflect.DeepEqual(current.onUpdate, desired.onUpdate)
}

func (g *Generator) haveSameDataType(current Column, desired Column) bool {
	return g.normalizeDataType(current.typeName) == g.normalizeDataType(desired.typeName) &&
		(current.length == nil || desired.length == nil || current.length.intVal == desired.length.intVal) && // detect change column only when both are set explicitly. TODO: maybe `current.length == nil` case needs another care
		current.array == desired.array
	// TODO: scale
}

func haveSameValue(current *Value, desired *Value) bool {
	// Normalize `DEFAULT NULL` to nil (missing DEFAULT)
	if current != nil && current.valueType == ValueTypeValArg && string(current.raw) == "null" {
		current = nil
	}
	if desired != nil && desired.valueType == ValueTypeValArg && string(desired.raw) == "null" {
		desired = nil
	}

	if current == nil && desired == nil {
		return true
	}
	if current == nil || desired == nil {
		return false
	}

	// NOTE: -1 can be changed to '-1' in show create table and valueType is not reliable
	currentRaw := string(current.raw)
	desiredRaw := string(desired.raw)
	if desired.valueType == ValueTypeFloat && len(currentRaw) > len(desiredRaw) {
		// Round "0.00" to "0.0" for comparison with desired.
		// Ideally we should do this seeing precision in a data type.
		currentRaw = currentRaw[0:len(desiredRaw)]
	}
	return currentRaw == desiredRaw
}

func (g *Generator) normalizeDataType(dataType string) string {
	alias, ok := dataTypeAliases[dataType]
	if ok {
		dataType = alias
	}
	if g.mode == GeneratorModeMysql {
		alias, ok = mysqlDataTypeAliases[dataType]
		if ok {
			dataType = alias
		}
	}
	return dataType
}

func areSamePrimaryKeys(primaryKeyA *Index, primaryKeyB *Index) bool {
	if primaryKeyA != nil && primaryKeyB != nil {
		return areSameIndexes(*primaryKeyA, *primaryKeyB)
	} else {
		return primaryKeyA == nil && primaryKeyB == nil
	}
}

func areSameIndexes(indexA Index, indexB Index) bool {
	if indexA.unique != indexB.unique {
		return false
	}
	if indexA.primary != indexB.primary {
		return false
	}
	for len(indexA.columns) != len(indexB.columns) {
		return false
	}
	for i, indexAColumn := range indexA.columns {
		// TODO: check length?
		if indexAColumn.column != indexB.columns[i].column {
			return false
		}
	}
	if indexA.where != indexB.where {
		return false
	}
	return true
}

func (g *Generator) areSameForeignKeys(foreignKeyA ForeignKey, foreignKeyB ForeignKey) bool {
	if g.normalizeOnUpdate(foreignKeyA.onUpdate) != g.normalizeOnUpdate(foreignKeyB.onUpdate) {
		return false
	}
	if g.normalizeOnDelete(foreignKeyA.onDelete) != g.normalizeOnDelete(foreignKeyB.onDelete) {
		return false
	}
	// TODO: check index, reference
	return true
}

func areSamePolicies(policyA, policyB Policy) bool {
	if strings.ToLower(policyA.scope) != strings.ToLower(policyB.scope) {
		return false
	}
	if strings.ToLower(policyA.permissive) != strings.ToLower(policyB.permissive) {
		return false
	}
	if strings.ToLower(policyA.using) != strings.ToLower(policyB.using) {
		return fmt.Sprintf("(%s)", policyA.using) == policyB.using
	}
	if strings.ToLower(policyA.withCheck) != strings.ToLower(policyB.withCheck) {
		return fmt.Sprintf("(%s)", policyA.withCheck) == policyB.withCheck
	}
	if len(policyA.roles) != len(policyB.roles) {
		return false
	}
	sort.Slice(policyA.roles, func(i, j int) bool {
		return policyA.roles[i] <= policyA.roles[j]
	})
	sort.Slice(policyB.roles, func(i, j int) bool {
		return policyB.roles[i] <= policyB.roles[j]
	})
	for i := range policyA.roles {
		if strings.ToLower(policyA.roles[i]) != strings.ToLower(policyB.roles[i]) {
			return false
		}
	}
	return true
}

func (g *Generator) normalizeOnUpdate(onUpdate string) string {
	if g.mode == GeneratorModePostgres && onUpdate == "" {
		return "NO ACTION"
	} else {
		return onUpdate
	}
}

func (g *Generator) normalizeOnDelete(onDelete string) string {
	if g.mode == GeneratorModePostgres && onDelete == "" {
		return "NO ACTION"
	} else {
		return onDelete
	}
}

// TODO: Use interface to avoid defining following functions?

func convertTablesToTableNames(tables []Table) []string {
	tableNames := []string{}
	for _, table := range tables {
		tableNames = append(tableNames, table.name)
	}
	return tableNames
}

func convertColumnsToColumnNames(columns []Column) []string {
	columnNames := []string{}
	for _, column := range columns {
		columnNames = append(columnNames, column.name)
	}
	return columnNames
}

func convertIndexesToIndexNames(indexes []Index) []string {
	indexNames := []string{}
	for _, index := range indexes {
		indexNames = append(indexNames, index.name)
	}
	return indexNames
}

func convertForeignKeysToConstraintNames(foreignKeys []ForeignKey) []string {
	constraintNames := []string{}
	for _, foreignKey := range foreignKeys {
		constraintNames = append(constraintNames, foreignKey.constraintName)
	}
	return constraintNames
}

func convertForeignKeysToIndexNames(foreignKeys []ForeignKey) []string {
	indexNames := []string{}
	for _, foreignKey := range foreignKeys {
		if len(foreignKey.indexName) > 0 {
			indexNames = append(indexNames, foreignKey.indexName)
		} else if len(foreignKey.constraintName) > 0 {
			indexNames = append(indexNames, foreignKey.constraintName)
		} // unexpected to reach else (really?)
	}
	return indexNames
}

func convertPolicyNames(policies []Policy) []string {
	policyNames := make([]string, len(policies))
	for i, policy := range policies {
		policyNames[i] = policy.name
	}
	return policyNames
}

func convertViewNames(views []*View) []string {
	viewNames := make([]string, len(views))
	for i, view := range views {
		viewNames[i] = view.name
	}
	return viewNames
}

func containsString(strs []string, str string) bool {
	for _, s := range strs {
		if s == str {
			return true
		}
	}
	return false
}

func removeTableByName(tables []*Table, name string) []*Table {
	removed := false
	ret := []*Table{}

	for _, table := range tables {
		if name == table.name {
			removed = true
		} else {
			ret = append(ret, table)
		}
	}

	if !removed {
		log.Fatalf("Failed to removeTableByName: Table `%s` is not found in `%v`", name, tables)
	}
	return ret
}
