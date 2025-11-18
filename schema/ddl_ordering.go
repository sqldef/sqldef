package schema

import (
	"github.com/sqldef/sqldef/v3/parser"
)

// topologicalSort performs a topological sort on items based on their dependencies using
// depth-first search (DFS). It returns the sorted items in dependency order, or an empty
// slice if a circular dependency is detected.
//
// The algorithm uses DFS with three-color marking (unvisited, visiting, visited) to detect
// cycles and ensure each node is processed only once.
func topologicalSort[T any](items []T, dependencies map[string][]string, getID func(T) string) []T {
	var sorted []T
	visited := make(map[string]bool)
	visiting := make(map[string]bool)
	itemMap := make(map[string]T)

	// Build item map for quick lookup
	for _, item := range items {
		id := getID(item)
		itemMap[id] = item
	}

	// DFS visit function
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			// Circular dependency detected
			return false
		}
		if visited[id] {
			return true
		}

		visiting[id] = true

		// Visit dependencies first
		for _, dep := range dependencies[id] {
			// Only visit if the dependency is in our current set of items
			if _, exists := itemMap[dep]; exists {
				if !visit(dep) {
					// Circular dependency - abandon sort
					return false
				}
			}
		}

		visiting[id] = false
		visited[id] = true

		if item, exists := itemMap[id]; exists {
			sorted = append(sorted, item)
		}
		return true
	}

	// Visit all items
	for _, item := range items {
		id := getID(item)
		if !visited[id] {
			if !visit(id) {
				// Circular dependency detected, return empty slice
				return []T{}
			}
		}
	}

	return sorted
}

// SortTablesByDependencies sorts CREATE TABLE DDLs by foreign key dependencies
// and VIEW DDLs by view-to-view/view-to-table dependencies
// to ensure objects are created in the correct order (dependencies before dependents)
// Also ensures CREATE TYPE statements are placed before CREATE TABLE statements that use them
// and CREATE SCHEMA statements are placed at the beginning
func SortTablesByDependencies(ddls []DDL, defaultSchema string) []DDL {
	// Extract DDLs by type: extensions, schemas, types, domains, tables, views, and other DDLs
	var createExtensions []*Extension
	var createSchemas []*Schema
	var createTypes []*Type
	var createDomains []*Domain
	var createTables []*CreateTable
	var views []*View
	var otherDDLs []DDL

	for _, ddl := range ddls {
		if ext, ok := ddl.(*Extension); ok {
			createExtensions = append(createExtensions, ext)
		} else if schema, ok := ddl.(*Schema); ok {
			createSchemas = append(createSchemas, schema)
		} else if typ, ok := ddl.(*Type); ok {
			createTypes = append(createTypes, typ)
		} else if domain, ok := ddl.(*Domain); ok {
			createDomains = append(createDomains, domain)
		} else if ct, ok := ddl.(*CreateTable); ok {
			createTables = append(createTables, ct)
		} else if view, ok := ddl.(*View); ok {
			views = append(views, view)
		} else {
			otherDDLs = append(otherDDLs, ddl)
		}
	}

	// Sort tables by foreign key dependencies
	var sortedTables []*CreateTable
	if len(createTables) > 1 {
		// Build dependency graph
		tableDependencies := make(map[string][]string)
		for _, ct := range createTables {
			tableName := ct.table.name
			// Extract foreign key dependencies
			deps := []string{}
			for _, fk := range ct.table.foreignKeys {
				if fk.referenceName != "" && fk.referenceName != tableName {
					deps = append(deps, fk.referenceName)
				}
			}
			tableDependencies[tableName] = deps
		}

		sorted := topologicalSort(createTables, tableDependencies, func(ct *CreateTable) string {
			return ct.table.name
		})

		// If circular dependency detected, keep original order
		if len(sorted) == 0 {
			sortedTables = createTables
		} else {
			sortedTables = sorted
		}
	} else {
		sortedTables = createTables
	}

	// Sort views by view-to-view and view-to-table dependencies
	var sortedViews []*View
	if len(views) > 1 {
		// Build dependency graph for views
		viewDependencies := make(map[string][]string)

		// Create a set of all table and view names for quick lookup
		allObjectNames := make(map[string]bool)
		for _, ct := range createTables {
			allObjectNames[ct.table.name] = true
		}
		for _, view := range views {
			allObjectNames[view.name] = true
		}

		// Extract dependencies for each view
		for _, view := range views {
			// Use extractViewDependencies to get all dependencies from the view definition
			deps := extractViewDependencies(view.definition, defaultSchema)

			// Filter to only include dependencies that are in our current set of objects
			var filteredDeps []string
			for _, dep := range deps {
				if allObjectNames[dep] {
					filteredDeps = append(filteredDeps, dep)
				}
			}
			viewDependencies[view.name] = filteredDeps
		}

		sorted := topologicalSort(views, viewDependencies, func(v *View) string {
			return v.name
		})

		// If circular dependency detected, keep original order
		if len(sorted) == 0 {
			sortedViews = views
		} else {
			sortedViews = sorted
		}
	} else {
		sortedViews = views
	}

	// Rebuild the DDL list in dependency order:
	// 1. CREATE EXTENSIONs (must exist before functions/types that use them)
	// 2. CREATE SCHEMAs (must exist before any objects in those schemas)
	// 3. CREATE TYPEs (may be used by tables and domains)
	// 4. CREATE DOMAINs (may be used by tables)
	// 5. CREATE TABLEs (sorted by FK dependencies)
	// 6. VIEWs (sorted by view dependencies)
	// 7. Other DDLs (triggers, comments, indexes, foreign keys, etc.)
	var result []DDL
	for _, ext := range createExtensions {
		result = append(result, ext)
	}
	for _, schema := range createSchemas {
		result = append(result, schema)
	}
	for _, typ := range createTypes {
		result = append(result, typ)
	}
	for _, domain := range createDomains {
		result = append(result, domain)
	}
	for _, ct := range sortedTables {
		result = append(result, ct)
	}
	for _, view := range sortedViews {
		result = append(result, view)
	}
	result = append(result, otherDDLs...)

	return result
}

// extractViewDependencies extracts all table/view names that a view depends on
// by walking the SelectStatement AST and collecting TableName references.
func extractViewDependencies(stmt parser.SelectStatement, defaultSchema string) []string {
	deps := make(map[string]bool)
	extractDependenciesFromSelectStatement(stmt, defaultSchema, deps)

	// Convert map to slice
	var result []string
	for dep := range deps {
		result = append(result, dep)
	}
	return result
}

// extractDependenciesFromSelectStatement recursively extracts table/view dependencies from a SelectStatement
func extractDependenciesFromSelectStatement(stmt parser.SelectStatement, defaultSchema string, deps map[string]bool) {
	switch s := stmt.(type) {
	case *parser.Select:
		// Extract from the FROM clause
		extractDependenciesFromTableExprs(s.From, defaultSchema, deps)

		// Extract from WITH clause (CTE references)
		if s.With != nil {
			extractDependenciesFromWith(s.With, defaultSchema, deps)
		}
	case *parser.Union:
		// Recursively extract from both sides of the UNION
		extractDependenciesFromSelectStatement(s.Left, defaultSchema, deps)
		extractDependenciesFromSelectStatement(s.Right, defaultSchema, deps)
	case *parser.ParenSelect:
		// Unwrap parenthesized SELECT
		extractDependenciesFromSelectStatement(s.Select, defaultSchema, deps)
	}
}

// extractDependenciesFromWith extracts dependencies from WITH clause (CTEs)
func extractDependenciesFromWith(with *parser.With, defaultSchema string, deps map[string]bool) {
	for _, cte := range with.CTEs {
		extractDependenciesFromSelectStatement(cte.Definition, defaultSchema, deps)
	}
}

// extractDependenciesFromTableExprs extracts table/view names from TableExprs
func extractDependenciesFromTableExprs(exprs parser.TableExprs, defaultSchema string, deps map[string]bool) {
	for _, expr := range exprs {
		extractDependenciesFromTableExpr(expr, defaultSchema, deps)
	}
}

// extractDependenciesFromTableExpr extracts table/view names from a single TableExpr
func extractDependenciesFromTableExpr(expr parser.TableExpr, defaultSchema string, deps map[string]bool) {
	switch te := expr.(type) {
	case *parser.AliasedTableExpr:
		// Extract from the actual table expression
		extractDependenciesFromSimpleTableExpr(te.Expr, defaultSchema, deps)
	case *parser.JoinTableExpr:
		// Recursively extract from both sides of the JOIN
		extractDependenciesFromTableExpr(te.LeftExpr, defaultSchema, deps)
		extractDependenciesFromTableExpr(te.RightExpr, defaultSchema, deps)
	case *parser.ParenTableExpr:
		// Recursively extract from parenthesized table expressions
		extractDependenciesFromTableExprs(te.Exprs, defaultSchema, deps)
	}
}

// extractDependenciesFromSimpleTableExpr extracts table/view names from SimpleTableExpr
func extractDependenciesFromSimpleTableExpr(expr parser.SimpleTableExpr, defaultSchema string, deps map[string]bool) {
	switch ste := expr.(type) {
	case parser.TableName:
		// This is an actual table/view reference
		schema := ste.Schema.String()
		if schema == "" {
			schema = defaultSchema
		}
		tableName := ste.Name.String()

		// Always use schema.tableName format for consistency
		var fullName string
		if schema != "" {
			fullName = schema + "." + tableName
		} else {
			fullName = tableName
		}
		deps[fullName] = true
	case *parser.Subquery:
		// Recursively extract from subquery
		extractDependenciesFromSelectStatement(ste.Select, defaultSchema, deps)
	}
}
