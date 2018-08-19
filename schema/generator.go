package schema

import (
	"fmt"
)

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
	ddls := []string{}
	for _, ddl := range destDdls {
		switch ddl := ddl.(type) {
		case *CreateTable:
			if !containsString(g.tables, ddl.tableName) {
				ddls = append(ddls, ddl.statement)
			}
		default:
			return nil, fmt.Errorf("unexpected ddl type in generateDDLs: %v", ddl)
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
