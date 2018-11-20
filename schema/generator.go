// TODO: Normalize implicit things in input first, and then compare
package schema

import (
	"fmt"
	"log"
	"reflect"
	"strings"
)

type GeneratorMode int

const (
	GeneratorModeMysql = GeneratorMode(iota)
	GeneratorModePostgres
)

var (
	dataTypeAliases = map[string]string{
		"bool":    "boolean",
		"int":     "integer",
		"char":    "character",
		"varchar": "character varying",
	}
)

// This struct holds simulated schema states during GenerateIdempotentDDLs().
type Generator struct {
	mode          GeneratorMode
	desiredTables []*Table
	currentTables []*Table
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

	generator := Generator{
		mode:          mode,
		desiredTables: []*Table{},
		currentTables: tables,
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
		default:
			return nil, fmt.Errorf("unexpected ddl type in generateDDLs: %v", desired)
		}
	}

	// Clean up obsoleted tables, indexes, columns
	for _, currentTable := range g.currentTables {
		desiredTable := findTableByName(g.desiredTables, currentTable.name)
		if desiredTable == nil {
			// Obsoleted table found. Drop table.
			ddls = append(ddls, fmt.Sprintf("DROP TABLE %s", g.escapeSQLName(currentTable.name)))
			g.currentTables = removeTableByName(g.currentTables, currentTable.name)
			continue
		}

		// Table is expected to exist. Drop foreign keys prior to index deletion
		for _, foreignKey := range currentTable.foreignKeys {
			if containsString(convertForeignKeysToConstraintNames(desiredTable.foreignKeys), foreignKey.constraintName) {
				continue // Foreign key is expected to exist.
			}

			var ddl string
			if g.mode == GeneratorModePostgres {
				ddl = fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", currentTable.name, foreignKey.constraintName)
			} else {
				ddl = fmt.Sprintf("ALTER TABLE %s DROP FOREIGN KEY %s", currentTable.name, foreignKey.constraintName)
			}
			ddls = append(ddls, ddl)
			// TODO: simulate to remove foreign key from `currentTable.foreignKeys`?
		}

		// Check indexes
		for _, index := range currentTable.indexes {
			if containsString(convertIndexesToIndexNames(desiredTable.indexes), index.name) ||
				containsString(convertForeignKeysToIndexNames(desiredTable.foreignKeys), index.name) {
				continue // Index is expected to exist.
			}

			// Index is obsoleted. Check and drop index as needed.
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
			ddl := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", desiredTable.name, column.name) // TODO: escape
			ddls = append(ddls, ddl)
			// TODO: simulate to remove column from `currentTable.columns`?
		}
	}

	return ddls, nil
}

// In the caller, `mergeTable` manages `g.currentTables`.
func (g *Generator) generateDDLsForCreateTable(currentTable Table, desired CreateTable) ([]string, error) {
	ddls := []string{}

	// Examine each column
	for i, desiredColumn := range desired.table.columns {
		currentColumn := findColumnByName(currentTable.columns, desiredColumn.name)
		if currentColumn == nil {
			definition, err := g.generateColumnDefinition(desiredColumn) // TODO: Parse DEFAULT NULL and share this with else
			if err != nil {
				return ddls, err
			}

			// Column not found, add column.
			ddl := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", desired.table.name, definition) // TODO: escape

			if g.mode == GeneratorModeMysql {
				after := " FIRST"
				if i > 0 {
					after = " AFTER " + desired.table.columns[i-1].name
				}
				ddl += after
			}

			ddls = append(ddls, ddl)
		} else {
			// Change column data type as needed.
			if !haveSameDataType(*currentColumn, desiredColumn) {
				definition, err := g.generateColumnDefinition(desiredColumn) // TODO: Parse DEFAULT NULL and share this with else
				if err != nil {
					return ddls, err
				}

				if g.mode == GeneratorModeMysql { // DDL is not compatible. TODO: support PostgreSQL
					ddl := fmt.Sprintf("ALTER TABLE %s CHANGE COLUMN %s %s", desired.table.name, currentColumn.name, definition) // TODO: escape
					ddls = append(ddls, ddl)
				}
			}

			// TODO: Add unique index if existing column does not have unique flag and there's no unique index!!!!
		}
	}

	// Examine primary key
	currentKey := currentTable.PrimaryKey()
	desiredKey := desired.table.PrimaryKey()
	areSameKey := currentKey != nil && desiredKey != nil && areSameIndexes(*currentKey, *desiredKey)
	areNilKey := currentKey == nil && desiredKey == nil
	if !(areSameKey || areNilKey) {
		if currentKey != nil {
			if g.mode == GeneratorModeMysql {
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP PRIMARY KEY", desired.table.name)) // TODO: escape
			} else {
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s_pkey", desired.table.name, desired.table.name)) // TODO: escape
			}
		}
		if desiredKey != nil {
			definition, err := g.generateIndexDefinition(*desiredKey)
			if err != nil {
				return ddls, err
			}
			ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ADD %s", desired.table.name, definition)) // TODO: escape
		}
	}

	// Examine each index
	for _, index := range desired.table.indexes {
		if index.name == "PRIMARY" {
			continue
		}

		if containsString(convertIndexesToIndexNames(currentTable.indexes), index.name) {
			// TODO: Compare types and change index type!!!
		} else {
			// Index not found, add index.
			definition, err := g.generateIndexDefinition(index)
			if err != nil {
				return ddls, err
			}
			ddl := fmt.Sprintf("ALTER TABLE %s ADD %s", desired.table.name, definition) // TODO: escape
			ddls = append(ddls, ddl)
		}
	}

	// Examine each foreign key
	for _, foreignKey := range desired.table.foreignKeys {
		if len(foreignKey.constraintName) == 0 {
			return ddls, fmt.Errorf(
				"Foreign key without constraint symbol was found in table '%s' (index name: '%s', columns: %v). "+
					"Specify the constraint symbol to identify the foreign key.",
				desired.table.name, foreignKey.indexName, foreignKey.indexColumns,
			)
		}

		if containsString(convertForeignKeysToConstraintNames(currentTable.foreignKeys), foreignKey.constraintName) {
			// TODO: Compare foreign key columns/options and change foreign key!!!
		} else {
			// Foreign key not found, add foreign key.
			definition, err := g.generateForeignKeyDefinition(foreignKey)
			if err != nil {
				return ddls, err
			}
			ddl := fmt.Sprintf("ALTER TABLE %s ADD %s", desired.table.name, definition) // TODO: escape
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

// Even though simulated table doesn't have index, primary or unique could exist in column definitions.
// This carefully generates DROP INDEX for such situations.
func (g *Generator) generateDDLsForAbsentIndex(currentIndex Index, currentTable Table, desiredTable Table) ([]string, error) {
	ddls := []string{}

	if currentIndex.primary {
		//pp(currentIndex)
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
			if column.name == currentIndex.columns[0].column && (column.keyOption == ColumnKeyUnique || column.keyOption == ColumnKeyUniqueKey) {
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

func (g *Generator) generateColumnDefinition(column Column) (string, error) {
	// TODO: make string concatenation faster?
	// TODO: consider escape?

	definition := fmt.Sprintf("%s ", column.name)

	if column.length != nil {
		if column.scale != nil {
			definition += fmt.Sprintf("%s(%s, %s) ", column.typeName, string(column.length.raw), string(column.scale.raw))
		} else {
			definition += fmt.Sprintf("%s(%s) ", column.typeName, string(column.length.raw))
		}
	} else {
		definition += fmt.Sprintf("%s ", column.typeName)
	}

	if column.unsigned {
		definition += "UNSIGNED "
	}
	if column.notNull || column.keyOption == ColumnKeyPrimary {
		definition += "NOT NULL "
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
		case ValueTypeValArg: // NULL
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
		definition += "UNIQUE "
	case ColumnKeyUniqueKey:
		definition += "UNIQUE KEY "
	case ColumnKeyPrimary:
		// noop
	default:
		return "", fmt.Errorf("unsupported column key (keyOption: '%d') in column: %#v", column.keyOption, column)
	}

	definition = strings.TrimSuffix(definition, " ")
	return definition, nil
}

// For CREATE TABLE.
func (g *Generator) generateIndexDefinition(index Index) (string, error) {
	definition := index.indexType // indexType is only available on `CREATE TABLE`, but only `generateDDLsForCreateTable` is using this

	columns := []string{}
	for _, indexColumn := range index.columns {
		columns = append(columns, indexColumn.column)
	}

	if index.primary {
		definition += fmt.Sprintf(
			" (%s)",
			strings.Join(columns, ", "), // TODO: escape
		)
	} else {
		definition += fmt.Sprintf(
			" %s(%s)",
			index.name,
			strings.Join(columns, ", "), // TODO: escape
		)
	}
	return definition, nil
}

func (g *Generator) generateForeignKeyDefinition(foreignKey ForeignKey) (string, error) {
	// TODO: make string concatenation faster?
	// TODO: consider escape?

	// Empty constraint name is already invalidated in generateDDLsForCreateIndex
	definition := fmt.Sprintf("CONSTRAINT %s FOREIGN KEY ", foreignKey.constraintName)

	if len(foreignKey.indexName) > 0 {
		definition += fmt.Sprintf("%s ", foreignKey.indexName)
	}

	definition += fmt.Sprintf(
		"(%s) REFERENCES %s (%s) ",
		strings.Join(foreignKey.indexColumns, ","), foreignKey.referenceName,
		strings.Join(foreignKey.referenceColumns, ","),
	)

	if len(foreignKey.onDelete) > 0 {
		definition += fmt.Sprintf("ON DELETE %s ", foreignKey.onDelete)
	}
	if len(foreignKey.onUpdate) > 0 {
		definition += fmt.Sprintf("ON UPDATE %s ", foreignKey.onUpdate)
	}

	definition = strings.TrimSuffix(definition, " ")
	return definition, nil
}

func (g *Generator) generateDropIndex(tableName string, indexName string) string {
	if g.mode == GeneratorModePostgres {
		return fmt.Sprintf("DROP INDEX %s", indexName) // TODO: escape
	} else {
		return fmt.Sprintf("ALTER TABLE %s DROP INDEX %s", tableName, indexName) // TODO: escape
	}
}

func (g *Generator) escapeSQLName(name string) string {
	if g.mode == GeneratorModePostgres {
		return fmt.Sprintf("\"%s\"", name)
	} else {
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
		default:
			return nil, fmt.Errorf("unexpected ddl type in convertDDLsToTables: %v", stmt)
		}
	}
	return tables, nil
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

func findPrimaryKey(indexes []Index) *Index {
	for _, index := range indexes {
		if index.primary {
			return &index
		}
	}
	return nil
}

func haveSameDataType(current Column, desired Column) bool {
	return (normalizeDataType(current.typeName) == normalizeDataType(desired.typeName)) &&
		(current.unsigned == desired.unsigned) &&
		(current.notNull == (desired.notNull || desired.keyOption == ColumnKeyPrimary)) && // `PRIMARY KEY` implies `NOT NULL`
		(current.autoIncrement == desired.autoIncrement) &&
		reflect.DeepEqual(current.onUpdate, desired.onUpdate)

	// TODO: check defaultVal, length, scale

	// TODO: Examine unique key properly with table indexes (primary key is already examined)
	//	(current.keyOption == desired.keyOption)
}

func normalizeDataType(dataType string) string {
	alias, ok := dataTypeAliases[dataType]
	if ok {
		return alias
	} else {
		return dataType
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
	return true
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
