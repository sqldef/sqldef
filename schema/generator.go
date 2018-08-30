package schema

import (
	"fmt"
	"log"
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
			ddls = append(ddls, fmt.Sprintf("DROP TABLE %s", currentTable.name)) // TODO: escape table name
			g.currentTables = removeTableByName(g.currentTables, currentTable.name)
			continue
		}

		// Table is expected to exist. Check indexes.
		for _, index := range currentTable.indexes {
			if containsString(convertIndexesToIndexNames(desiredTable.indexes), index.name) {
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
	for _, desiredColumn := range desired.table.columns {
		currentColumn := findColumnByName(currentTable.columns, desiredColumn.name)
		if currentColumn == nil {
			definition, err := g.generateColumnDefinition(desiredColumn) // TODO: Parse DEFAULt NULL and share this with else
			if err != nil {
				return ddls, err
			}

			// Column not found, add column.
			ddl := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", desired.table.name, definition) // TODO: escape
			ddls = append(ddls, ddl)
		} else {
			// Column is found, change primary key first as needed.
			if g.mode == GeneratorModeMysql { // DDL is not compatible. TODO: support postgresql
				if isPrimaryKey(*currentColumn, currentTable) && !isPrimaryKey(desiredColumn, desired.table) {
					// TODO: `DROP PRIMARY KEY` should always come earlier than `ADD PRIMARY KEY` regardless of the order of columns
					ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP PRIMARY KEY", desired.table.name)) // TODO: escape
					currentColumn.keyOption = desiredColumn.keyOption
				}
				if !isPrimaryKey(*currentColumn, currentTable) && isPrimaryKey(desiredColumn, desired.table) {
					ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY(%s)", desired.table.name, desiredColumn.name)) // TODO: escape, support multi-columns?
					currentColumn.notNull = true
					currentColumn.keyOption = ColumnKeyPrimary
				}
			}

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

	// Examine each index
	for _, index := range desired.table.indexes {
		if containsString(convertIndexesToIndexNames(currentTable.indexes), index.name) {
			// TODO: Compare types and change column type!!!
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
	if column.notNull {
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
		default:
			return "", fmt.Errorf("unsupported default value type (valueType: '%d') in column: %#v", column.defaultVal.valueType, column)
		}
	}

	if column.autoIncrement {
		definition += "AUTO_INCREMENT "
	}

	switch column.keyOption {
	case ColumnKeyNone:
		// noop
	case ColumnKeyUnique:
		definition += "UNIQUE "
	case ColumnKeyUniqueKey:
		definition += "UNIQUE KEY "
	case ColumnKeyPrimary:
		definition += "PRIMARY KEY "
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

	definition += fmt.Sprintf(
		" %s(%s)",
		index.name,
		strings.Join(columns, ", "), // TODO: escape
	)
	return definition, nil
}

func (g *Generator) generateDropIndex(tableName string, indexName string) string {
	if g.mode == GeneratorModePostgres {
		return fmt.Sprintf("DROP INDEX %s", indexName) // TODO: escape
	} else {
		return fmt.Sprintf("ALTER TABLE %s DROP INDEX %s", tableName, indexName) // TODO: escape
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

func haveSameDataType(current Column, desired Column) bool {
	return (normalizeDataType(current.typeName) == normalizeDataType(desired.typeName)) &&
		(current.unsigned == desired.unsigned) &&
		(current.notNull == (desired.notNull || desired.keyOption == ColumnKeyPrimary)) && // `PRIMARY KEY` implies `NOT NULL`
		(current.autoIncrement == desired.autoIncrement)

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
