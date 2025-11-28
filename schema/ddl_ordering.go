package schema

import (
	"sort"

	"github.com/sqldef/sqldef/v3/parser"
)

// topologicalSort performs a stable topological sort on items based on their dependencies
// using Kahn's algorithm (BFS-based). It returns the sorted items in dependency order,
// or an empty slice if a circular dependency is detected.
//
// The algorithm is stable: when multiple items have no dependencies between them
// (independent items), they are output in their original input order. This ensures
// deterministic and predictable output.
//
// Time complexity: O(V + E) where V is the number of items and E is the number of dependencies.
func topologicalSort[T any](items []T, dependencies map[string][]string, getID func(T) string) []T {
	// Build item map and track original indices for stable sorting
	itemMap := make(map[string]T)
	itemIndices := make(map[string]int)
	inDegree := make(map[string]int)
	dependents := make(map[string][]string)

	for i, item := range items {
		id := getID(item)
		itemMap[id] = item
		itemIndices[id] = i
		inDegree[id] = 0
		dependents[id] = []string{}
	}

	// Calculate in-degrees (number of dependencies each item has)
	// and build reverse dependency map (dependents) for efficiency
	// Use items order (not map iteration) for deterministic behavior
	for _, item := range items {
		id := getID(item)
		for _, dep := range dependencies[id] {
			if _, exists := itemMap[dep]; exists {
				inDegree[id]++
				dependents[dep] = append(dependents[dep], id)
			}
		}
	}

	// Priority queue: nodes with zero in-degree, maintained in sorted order by original index
	// Using a simple slice here; items are kept sorted by original index for stable output
	type queueItem struct {
		id    string
		index int
	}
	var queue []queueItem

	// Initialize queue with all nodes that have no dependencies
	for _, item := range items {
		id := getID(item)
		if inDegree[id] == 0 {
			queue = append(queue, queueItem{id, itemIndices[id]})
		}
	}

	// Sort initial queue by original index to ensure stable output
	sort.Slice(queue, func(i, j int) bool {
		return queue[i].index < queue[j].index
	})

	var sorted []T

	for len(queue) > 0 {
		// Process node with smallest original index (maintains input order for independent items)
		curr := queue[0]
		queue = queue[1:]

		sorted = append(sorted, itemMap[curr.id])

		// Reduce in-degree for all items that depend on curr
		for _, dependentID := range dependents[curr.id] {
			inDegree[dependentID]--
			if inDegree[dependentID] == 0 {
				// This item is now ready (all its dependencies have been processed)
				// Insert into queue maintaining sorted order by original index
				newItem := queueItem{dependentID, itemIndices[dependentID]}

				// Binary search to find insertion position
				pos := sort.Search(len(queue), func(i int) bool {
					return queue[i].index > newItem.index
				})

				// Insert at position while maintaining order
				queue = append(queue[:pos], append([]queueItem{newItem}, queue[pos:]...)...)
			}
		}
	}

	// Check if all nodes were processed (if not, there's a circular dependency)
	if len(sorted) != len(items) {
		return []T{} // Circular dependency detected
	}

	return sorted
}

// SortTablesByDependencies sorts CREATE TABLE DDLs by foreign key dependencies
// and VIEW DDLs by view-to-view/view-to-table dependencies
// to ensure objects are created in the correct order (dependencies before dependents)
// Also ensures CREATE TYPE statements are placed before CREATE TABLE statements that use them
// and CREATE SCHEMA statements are placed at the beginning
func SortTablesByDependencies(ddls []DDL, defaultSchema string, mode GeneratorMode, legacyIgnoreQuotes bool) []DDL {
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
			tableName := ct.table.name.String()
			// Extract foreign key dependencies
			deps := []string{}
			for _, fk := range ct.table.foreignKeys {
				// Skip self-referential FKs using quote-aware comparison
				if qualifiedTableNamesEqual(ct.table.name, fk.referenceTableName, defaultSchema, mode, legacyIgnoreQuotes) {
					continue
				}
				refTableName := fk.referenceTableName.String()
				if refTableName != "" {
					deps = append(deps, refTableName)
				}
			}
			tableDependencies[tableName] = deps
		}

		sorted := topologicalSort(createTables, tableDependencies, func(ct *CreateTable) string {
			return ct.table.name.String()
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
			allObjectNames[ct.table.name.String()] = true
		}
		for _, view := range views {
			allObjectNames[view.name.String()] = true
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
			viewDependencies[view.name.String()] = filteredDeps
		}

		sorted := topologicalSort(views, viewDependencies, func(v *View) string {
			return v.name.String()
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
