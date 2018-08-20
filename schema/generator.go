package schema

import (
	"fmt"
)

// This struct holds simulated schema states during GenerateIdempotentDDLs().
// TODO: This should have the "desired" state and "current" state
type Generator struct {
	tables []string
}

func GenerateIdempotentDDLs(sql string, tables []string) ([]string, error) {
	destDdls, err := parseDDLs(sql)
	if err != nil {
		return nil, err
	}

	generator := Generator{
		tables: tables,
	}
	return generator.generateDDLs(destDdls)
}

func (g *Generator) generateDDLs(destDdls []DDL) ([]string, error) {
	desiredTables := []string{}
	ddls := []string{}

	for _, ddl := range destDdls {
		switch ddl := ddl.(type) {
		case *CreateTable:
			desiredTables = append(desiredTables, ddl.table.name)
			if !containsString(g.tables, ddl.table.name) {
				g.tables = append(g.tables, ddl.table.name)
				ddls = append(ddls, ddl.statement)
			}
		default:
			return nil, fmt.Errorf("unexpected ddl type in generateDDLs: %v", ddl)
		}
	}

	// Clean up obsoleted tables
	for _, table := range g.tables {
		if !containsString(desiredTables, table) {
			// TODO: support postgresql?
			ddls = append(ddls, fmt.Sprintf("DROP TABLE %s;", table)) // TODO: escape table name
		}
	}

	return ddls, nil
}

func containsString(strs []string, str string) bool {
	for _, s := range strs {
		if s == str {
			return true
		}
	}
	return false
}
