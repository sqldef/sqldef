package schema

import (
	"fmt"
)

// This struct holds simulated schema states during GenerateIdempotentDDLs().
type Generator struct {
	desiredTables []Table
	currentTables []Table
}

// Parse argument DDLs and call `generateDDLs()`
func GenerateIdempotentDDLs(desiredSQL string, currentSQL string) ([]string, error) {
	// TODO: invalidate duplicated tables, columns
	desiredDDLs, err := parseDDLs(desiredSQL)
	if err != nil {
		return nil, err
	}

	currentDDLs, err := parseDDLs(currentSQL)
	if err != nil {
		return nil, err
	}

	tables, err := convertDDLsToTables(currentDDLs)
	if err != nil {
		return nil, err
	}

	generator := Generator{
		desiredTables: []Table{},
		currentTables: tables,
	}
	return generator.generateDDLs(desiredDDLs)
}

// Main part of DDL genearation
func (g *Generator) generateDDLs(desiredDDLs []DDL) ([]string, error) {
	ddls := []string{}

	desiredTables, err := convertDDLsToTables(desiredDDLs)
	if err != nil {
		return nil, err
	}

	// Clean up unnecessary tables
	desiredTableNames := convertTablesToTableNames(desiredTables)
	currentTableNames := convertTablesToTableNames(g.currentTables)
	for _, tableName := range currentTableNames {
		if !containsString(desiredTableNames, tableName) {
			ddls = append(ddls, fmt.Sprintf("DROP TABLE %s", tableName)) // TODO: escape table name
			g.currentTables = removeTableByName(g.currentTables, tableName)
		}
	}

	// Incrementally examine desiredDDLs
	for _, ddl := range desiredDDLs {
		switch desired := ddl.(type) {
		case *CreateTable:
			if currentTable := findTableByName(g.currentTables, desired.table.name); currentTable != nil {
				// Table already exists, guess required DDLs.
				ddls = append(ddls, g.generateDDLsForCreateTable(*currentTable, *desired)...)
				mergeTable(currentTable, desired.table)
			} else {
				// Table not found, create table.
				ddls = append(ddls, desired.statement)
				g.currentTables = append(g.currentTables, desired.table)
			}
			g.desiredTables = append(g.desiredTables, desired.table)
		case *AddIndex:
			currentTable := findTableByName(g.currentTables, desired.tableName)
			if currentTable == nil {
				return nil, fmt.Errorf("alter table is performed for inexistent table '%s': '%s'", desired.tableName, ddl.Statement())
			}
			if containsString(convertIndexesToIndexNames(currentTable.indexes), desired.index.name) {
				// TODO: compare index definition and add/drop if necessary
			} else {
				// Index not found, add index.
				ddls = append(ddls, ddl.Statement())
				currentTable.indexes = append(currentTable.indexes, desired.index)
			}

			// Examine indexes in desiredTable to delete obsoleted indexes later
			desiredTable := findTableByName(g.desiredTables, desired.tableName)
			if desiredTable == nil {
				return nil, fmt.Errorf("alter table is performed before create table '%s': '%s'", desired.tableName, ddl.Statement())
			}
			if containsString(convertIndexesToIndexNames(desiredTable.indexes), desired.index.name) {
				return nil, fmt.Errorf("index '%s' is doubly created against table '%s': '%s'", desired.index.name, desired.tableName, ddl.Statement())
			}
			desiredTable.indexes = append(desiredTable.indexes, desired.index)
		default:
			return nil, fmt.Errorf("unexpected ddl type in generateDDLs: %v", desired)
		}
	}

	return ddls, nil
}

func (g *Generator) generateDDLsForCreateTable(currentTable Table, desired CreateTable) []string {
	ddls := []string{}

	// NOTE: g.generateDDLs replace the whole table in g.currentTables,
	// so this function does not need to update g.currentTables.

	// Clean up unnecessary columns
	// This can be examined here because sqldef doesn't allow add column DDL in schema.sql.
	desiredColumnNames := convertColumnsToColumnNames(desired.table.columns)
	currentColumnNames := convertColumnsToColumnNames(currentTable.columns)
	for _, columnName := range currentColumnNames {
		if !containsString(desiredColumnNames, columnName) {
			ddl := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", desired.table.name, columnName) // TODO: escape
			ddls = append(ddls, ddl)
		}
	}

	// Examine each columns
	for _, column := range desired.table.columns {
		if containsString(currentColumnNames, column.name) {
			// TODO: Compare types and change column type!!!
		} else {
			// Column not found, add column.
			ddl := fmt.Sprintf(
				"ALTER TABLE %s ADD COLUMN %s",
				desired.table.name, g.generateColumnDefinition(column),
			) // TODO: escape
			ddls = append(ddls, ddl)
		}
	}

	return ddls
}

func (g *Generator) generateColumnDefinition(column Column) string {
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
			// TODO: should this be an error?
			definition += fmt.Sprintf("DEFAULT %s ", string(column.defaultVal.raw))
		}
	}

	if column.autoIncrement {
		definition += "AUTO_INCREMENT "
	}

	switch column.keyOption {
	case ColumnKeyUnique:
		definition += "UNIQUE "
	case ColumnKeyUniqueKey:
		definition += "UNIQUE KEY "
	case ColumnKeyPrimary:
		definition += "PRIMARY KEY "
	default:
		// TODO: return error
	}

	return definition
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

func convertDDLsToTables(ddls []DDL) ([]Table, error) {
	tables := []Table{}
	for _, ddl := range ddls {
		switch ddl := ddl.(type) {
		case *CreateTable:
			tables = append(tables, ddl.table)
		case *AddIndex:
			// TODO: Add column, etc.
		default:
			return nil, fmt.Errorf("unexpected ddl type in convertDDLsToTables: %v", ddl)
		}
	}
	return tables, nil
}

func findTableByName(tables []Table, name string) *Table {
	for _, table := range tables {
		if table.name == name {
			return &table
		}
	}
	return nil
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

// TODO: Is there more efficient way?
func removeTableByName(tables []Table, name string) []Table {
	ret := []Table{}
	for _, table := range tables {
		if name != table.name {
			ret = append(ret, table)
		}
	}
	// TODO: no need to assert really removed one table?
	return ret
}
