package schema

import (
	"fmt"
)

// This struct holds simulated schema states during GenerateIdempotentDDLs().
type Generator struct {
	currentTables []Table
}

// Parse argument DDLs and call `generateDDLs()`
func GenerateIdempotentDDLs(desiredSQL string, currentSQL string) ([]string, error) {
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

	generator := Generator{currentTables: tables}
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
			ddls = append(ddls, fmt.Sprintf("DROP TABLE %s;", tableName)) // TODO: escape table name
			g.currentTables = removeTableByName(g.currentTables, tableName)
		}
	}

	// Incrementally examine desiredDDLs
	for _, ddl := range desiredDDLs {
		switch ddl := ddl.(type) {
		case *CreateTable:
			if !containsString(convertTablesToTableNames(g.currentTables), ddl.table.name) {
				ddls = append(ddls, ddl.statement)
				g.currentTables = append(g.currentTables, ddl.table)
			}
		default:
			return nil, fmt.Errorf("unexpected ddl type in generateDDLs: %v", ddl)
		}
	}

	return ddls, nil
}

func convertDDLsToTables(ddls []DDL) ([]Table, error) {
	tables := []Table{}
	for _, ddl := range ddls {
		switch ddl := ddl.(type) {
		case *CreateTable:
			tables = append(tables, ddl.table)
		default:
			return nil, fmt.Errorf("unexpected ddl type in convertDDLsToTables: %v", ddl)
		}
	}
	return tables, nil
}

func convertTablesToTableNames(tables []Table) []string {
	tableNames := []string{}
	for _, table := range tables {
		tableNames = append(tableNames, table.name)
	}
	return tableNames
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
