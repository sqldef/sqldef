package schema

import (
	"sort"
	"strings"

	"github.com/sqldef/sqldef/v3/parser"
	"github.com/sqldef/sqldef/v3/util"
)

// normalizeIdentKey returns a normalized string representation of an Ident
// for use as a map key in dependency graphs. This ensures that identifiers
// which refer to the same database object produce the same key.
//
// When legacyIgnoreQuotes is true, all identifiers are normalized to lowercase
// for case-insensitive matching (backward compatible behavior).
//
// When legacyIgnoreQuotes is false, behavior per database:
//   - PostgreSQL: Unquoted fold to lowercase, quoted preserve case
//   - MySQL: Respects mysqlLowerCaseTableNames (0=case-sensitive, 1/2=case-insensitive)
//   - MSSQL/SQLite3: Always case-insensitive (fold all to lowercase)
func normalizeIdentKey(ident Ident, mode GeneratorMode, legacyIgnoreQuotes bool, mysqlLowerCaseTableNames int) string {
	// Legacy mode: case-insensitive matching for all databases
	if legacyIgnoreQuotes {
		return strings.ToLower(ident.Name)
	}

	switch mode {
	case GeneratorModePostgres:
		if ident.Quoted {
			return ident.Name
		}
		return strings.ToLower(ident.Name)
	case GeneratorModeMysql:
		if mysqlLowerCaseTableNames == 0 {
			// Case-sensitive: preserve case
			return ident.Name
		}
		// Case-insensitive (1 or 2): fold to lowercase
		return strings.ToLower(ident.Name)
	default:
		// MSSQL/SQLite3: always case-insensitive
		return strings.ToLower(ident.Name)
	}
}

// normalizeNameKey returns a normalized string representation of a
// QualifiedName for use as a map key in dependency graphs.
func normalizeNameKey(name QualifiedName, defaultSchema string, mode GeneratorMode, legacyIgnoreQuotes bool, mysqlLowerCaseTableNames int) string {
	schema := name.Schema.Name
	if schema == "" {
		schema = defaultSchema
	} else {
		schema = normalizeIdentKey(name.Schema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
	}

	tableName := normalizeIdentKey(name.Name, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)

	if schema != "" {
		return schema + "." + tableName
	}
	return tableName
}

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

// SortTablesByDependencies sorts CREATE TYPE/DOMAIN/FUNCTION/TABLE/VIEW DDLs
// by a unified dependency graph, ensuring objects are created in the correct
// order (dependencies before dependents). CREATE EXTENSION/SCHEMA stay at the
// front (no inbound edges from sorted kinds) and other DDLs stay at the tail.
//
// Edges harvested:
//   - Table -> Table (foreign keys)
//   - Table -> Function (column DEFAULT / CHECK / generated expressions)
//   - Table -> Type/Domain (column type references)
//   - Domain -> Function (Domain CHECK expressions)
//   - Domain -> Type/Domain (Domain underlying data type)
//   - Function -> Type/Domain (argument and return types)
//   - View -> Table/View (SELECT body)
func SortTablesByDependencies(ddls []DDL, defaultSchema string, mode GeneratorMode, legacyIgnoreQuotes bool, mysqlLowerCaseTableNames int) []DDL {
	// Extract DDLs by type: extensions, schemas, types, domains, functions, tables, views, and other DDLs
	var createExtensions []*Extension
	var createSchemas []*Schema
	var createTypes []*Type
	var createDomains []*Domain
	var createFunctions []*Function
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
		} else if fn, ok := ddl.(*Function); ok {
			createFunctions = append(createFunctions, fn)
		} else if ct, ok := ddl.(*CreateTable); ok {
			createTables = append(createTables, ct)
		} else if view, ok := ddl.(*View); ok {
			views = append(views, view)
		} else {
			otherDDLs = append(otherDDLs, ddl)
		}
	}

	// Build sets of known object names for edge resolution.
	knownTypes := make(map[string]bool, len(createTypes))
	for _, t := range createTypes {
		knownTypes[normalizeNameKey(t.name, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)] = true
	}
	knownDomains := make(map[string]bool, len(createDomains))
	for _, d := range createDomains {
		knownDomains[normalizeNameKey(d.name, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)] = true
	}
	knownFunctions := make(map[string]bool, len(createFunctions))
	for _, fn := range createFunctions {
		knownFunctions[normalizeNameKey(fn.name, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)] = true
	}
	knownTables := make(map[string]bool, len(createTables))
	for _, ct := range createTables {
		knownTables[normalizeNameKey(ct.table.name, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)] = true
	}
	knownViews := make(map[string]bool, len(views))
	for _, v := range views {
		knownViews[normalizeNameKey(v.name, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)] = true
	}

	// Build unified dependency map keyed by normalized object name.
	dependencies := make(map[string][]string)

	for _, fn := range createFunctions {
		fnKey := normalizeNameKey(fn.name, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
		deps := dependencies[fnKey]
		for _, arg := range fn.args {
			if depKey, ok := resolveTypeReference(arg.typ, knownTypes, knownDomains, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames); ok && depKey != fnKey {
				deps = append(deps, depKey)
			}
		}
		if depKey, ok := resolveTypeReference(fn.returnType, knownTypes, knownDomains, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames); ok && depKey != fnKey {
			deps = append(deps, depKey)
		}
		dependencies[fnKey] = deps
	}

	for _, d := range createDomains {
		domainKey := normalizeNameKey(d.name, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
		deps := dependencies[domainKey]
		if depKey, ok := resolveTypeReference(d.dataType, knownTypes, knownDomains, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames); ok && depKey != domainKey {
			deps = append(deps, depKey)
		}
		for _, c := range d.constraints {
			for _, dep := range extractFunctionCallNames(c.expression, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames) {
				if dep != domainKey {
					deps = append(deps, dep)
				}
			}
		}
		dependencies[domainKey] = deps
	}

	for _, ct := range createTables {
		tableKey := normalizeNameKey(ct.table.name, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
		deps := dependencies[tableKey]

		// Foreign keys
		for _, fk := range ct.table.foreignKeys {
			if qualifiedNamesEqual(ct.table.name, fk.referenceTableName, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames) {
				continue
			}
			refKey := normalizeNameKey(fk.referenceTableName, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
			if refKey != "" {
				deps = append(deps, refKey)
			}
		}

		// Column-level references
		for _, col := range ct.table.columns {
			if depKey, ok := resolveTypeReference(col.typeName, knownTypes, knownDomains, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames); ok && depKey != tableKey {
				deps = append(deps, depKey)
			}
			if col.defaultDef != nil {
				for _, dep := range extractFunctionCallNames(col.defaultDef.expression, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames) {
					if dep != tableKey {
						deps = append(deps, dep)
					}
				}
			}
			if col.check != nil {
				for _, dep := range extractFunctionCallNames(col.check.definition, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames) {
					if dep != tableKey {
						deps = append(deps, dep)
					}
				}
			}
			if col.generated != nil && col.generated.exprAST != nil {
				for _, dep := range extractFunctionCallNames(col.generated.exprAST, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames) {
					if dep != tableKey {
						deps = append(deps, dep)
					}
				}
			}
		}

		// Table-level CHECK constraints
		for _, chk := range ct.table.checks {
			for _, dep := range extractFunctionCallNames(chk.definition, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames) {
				if dep != tableKey {
					deps = append(deps, dep)
				}
			}
		}

		// Partial index WHERE expressions
		for _, idx := range ct.table.indexes {
			if idx.where != nil {
				for _, dep := range extractFunctionCallNames(idx.where, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames) {
					if dep != tableKey {
						deps = append(deps, dep)
					}
				}
			}
		}

		dependencies[tableKey] = deps
	}

	for _, view := range views {
		viewKey := normalizeNameKey(view.name, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
		rawDeps := extractViewDependencies(view.definition, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
		deps := dependencies[viewKey]
		for _, dep := range rawDeps {
			if dep == viewKey {
				continue
			}
			if knownTables[dep] || knownViews[dep] {
				deps = append(deps, dep)
			}
		}
		dependencies[viewKey] = deps
	}

	// Combine all sortable DDLs in fixed-bucket order (used as fallback and as the
	// stable-sort seed when items are independent).
	var sortItems []DDL
	for _, t := range createTypes {
		sortItems = append(sortItems, t)
	}
	for _, d := range createDomains {
		sortItems = append(sortItems, d)
	}
	for _, fn := range createFunctions {
		sortItems = append(sortItems, fn)
	}
	for _, ct := range createTables {
		sortItems = append(sortItems, ct)
	}
	for _, view := range views {
		sortItems = append(sortItems, view)
	}

	ddlKey := func(d DDL) string {
		switch v := d.(type) {
		case *Type:
			return normalizeNameKey(v.name, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
		case *Domain:
			return normalizeNameKey(v.name, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
		case *Function:
			return normalizeNameKey(v.name, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
		case *CreateTable:
			return normalizeNameKey(v.table.name, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
		case *View:
			return normalizeNameKey(v.name, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
		}
		return ""
	}

	sorted := topologicalSort(sortItems, dependencies, ddlKey)
	if len(sorted) == 0 {
		// Circular dependency detected: fall back to fixed-bucket order
		// (types -> domains -> functions -> tables -> views) which is what
		// sortItems is already arranged as.
		sorted = sortItems
	}

	// Rebuild the DDL list:
	// 1. CREATE EXTENSIONs (no inbound edges from sorted kinds)
	// 2. CREATE SCHEMAs (no inbound edges from sorted kinds)
	// 3. Unified sort of TYPE/DOMAIN/FUNCTION/TABLE/VIEW by per-object edges
	// 4. Other DDLs (triggers, comments, indexes, foreign keys, etc.)
	var result []DDL
	for _, ext := range createExtensions {
		result = append(result, ext)
	}
	for _, schema := range createSchemas {
		result = append(result, schema)
	}
	result = append(result, sorted...)
	result = append(result, otherDDLs...)

	return result
}

// resolveTypeReference resolves a type-name string (possibly schema-qualified)
// to a normalized dependency-graph key, returning (key, true) only if the
// reference matches an entry in knownTypes or knownDomains.
func resolveTypeReference(typeName string, knownTypes, knownDomains map[string]bool, defaultSchema string, mode GeneratorMode, legacyIgnoreQuotes bool, mysqlLowerCaseTableNames int) (string, bool) {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return "", false
	}
	// Strip trailing array brackets (e.g. "priority[]")
	typeName = strings.TrimSuffix(typeName, "[]")
	typeName = strings.TrimSpace(typeName)

	schema := defaultSchema
	name := typeName
	if idx := strings.Index(typeName, "."); idx > 0 {
		schema = strings.TrimSpace(typeName[:idx])
		name = strings.TrimSpace(typeName[idx+1:])
	}

	// Without quote information from the string form, fold according to the
	// generator mode using an unquoted Ident.
	qn := QualifiedName{
		Schema: Ident{Name: schema},
		Name:   Ident{Name: name},
	}
	key := normalizeNameKey(qn, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
	if knownTypes[key] || knownDomains[key] {
		return key, true
	}
	return "", false
}

// extractFunctionCallNames walks an expression and returns the normalized
// names of every function call that matches an entry in knownFunctions.
func extractFunctionCallNames(expr parser.Expr, knownFunctions map[string]bool, defaultSchema string, mode GeneratorMode, legacyIgnoreQuotes bool, mysqlLowerCaseTableNames int) []string {
	if expr == nil || len(knownFunctions) == 0 {
		return nil
	}
	deps := make(map[string]bool)
	walkExprForFunctionCalls(expr, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	var result []string
	for dep := range util.CanonicalMapIter(deps) {
		result = append(result, dep)
	}
	return result
}

func walkExprForFunctionCalls(expr parser.Expr, knownFunctions map[string]bool, defaultSchema string, mode GeneratorMode, legacyIgnoreQuotes bool, mysqlLowerCaseTableNames int, deps map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *parser.AndExpr:
		walkExprForFunctionCalls(e.Left, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
		walkExprForFunctionCalls(e.Right, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.OrExpr:
		walkExprForFunctionCalls(e.Left, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
		walkExprForFunctionCalls(e.Right, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.NotExpr:
		walkExprForFunctionCalls(e.Expr, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.ParenExpr:
		walkExprForFunctionCalls(e.Expr, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.ComparisonExpr:
		walkExprForFunctionCalls(e.Left, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
		walkExprForFunctionCalls(e.Right, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.RangeCond:
		walkExprForFunctionCalls(e.Left, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
		walkExprForFunctionCalls(e.From, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
		walkExprForFunctionCalls(e.To, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.IsExpr:
		walkExprForFunctionCalls(e.Expr, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.BinaryExpr:
		walkExprForFunctionCalls(e.Left, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
		walkExprForFunctionCalls(e.Right, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.UnaryExpr:
		walkExprForFunctionCalls(e.Expr, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.IntervalExpr:
		walkExprForFunctionCalls(e.Expr, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.TypedLiteral:
		walkExprForFunctionCalls(e.Value, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.CollateExpr:
		walkExprForFunctionCalls(e.Expr, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.CastExpr:
		walkExprForFunctionCalls(e.Expr, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.ConvertExpr:
		walkExprForFunctionCalls(e.Expr, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.CaseExpr:
		walkExprForFunctionCalls(e.Expr, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
		for _, when := range e.Whens {
			walkExprForFunctionCalls(when.Cond, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
			walkExprForFunctionCalls(when.Val, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
		}
		walkExprForFunctionCalls(e.Else, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.FuncExpr:
		key := normalizeNameKey(QualifiedName{Schema: e.Qualifier, Name: e.Name}, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
		if knownFunctions[key] {
			deps[key] = true
		}
		for _, arg := range e.Exprs {
			if alias, ok := arg.(*parser.AliasedExpr); ok {
				walkExprForFunctionCalls(alias.Expr, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
			}
		}
	case *parser.FuncCallExpr:
		key := normalizeNameKey(QualifiedName{Name: e.Name}, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
		if knownFunctions[key] {
			deps[key] = true
		}
		for _, arg := range e.Exprs {
			walkExprForFunctionCalls(arg, knownFunctions, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
		}
	}
}

// extractViewDependencies extracts all table/view names that a view depends on
// by walking the SelectStatement AST and collecting TableName references.
// Returns normalized names suitable for use as dependency graph keys.
func extractViewDependencies(stmt parser.SelectStatement, defaultSchema string, mode GeneratorMode, legacyIgnoreQuotes bool, mysqlLowerCaseTableNames int) []string {
	deps := make(map[string]bool)
	extractDependenciesFromSelectStatement(stmt, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)

	// Convert map to slice in deterministic order
	var result []string
	for dep := range util.CanonicalMapIter(deps) {
		result = append(result, dep)
	}
	return result
}

// extractDependenciesFromSelectStatement recursively extracts table/view dependencies from a SelectStatement
func extractDependenciesFromSelectStatement(stmt parser.SelectStatement, defaultSchema string, mode GeneratorMode, legacyIgnoreQuotes bool, mysqlLowerCaseTableNames int, deps map[string]bool) {
	switch s := stmt.(type) {
	case *parser.Select:
		// Extract from the FROM clause
		extractDependenciesFromTableExprs(s.From, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)

		// Extract from WITH clause (CTE references)
		if s.With != nil {
			extractDependenciesFromWith(s.With, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
		}
	case *parser.Union:
		// Recursively extract from both sides of the UNION
		extractDependenciesFromSelectStatement(s.Left, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
		extractDependenciesFromSelectStatement(s.Right, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.ParenSelect:
		// Unwrap parenthesized SELECT
		extractDependenciesFromSelectStatement(s.Select, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	}
}

// extractDependenciesFromWith extracts dependencies from WITH clause (CTEs)
func extractDependenciesFromWith(with *parser.With, defaultSchema string, mode GeneratorMode, legacyIgnoreQuotes bool, mysqlLowerCaseTableNames int, deps map[string]bool) {
	for _, cte := range with.CTEs {
		extractDependenciesFromSelectStatement(cte.Definition, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	}
}

// extractDependenciesFromTableExprs extracts table/view names from TableExprs
func extractDependenciesFromTableExprs(exprs parser.TableExprs, defaultSchema string, mode GeneratorMode, legacyIgnoreQuotes bool, mysqlLowerCaseTableNames int, deps map[string]bool) {
	for _, expr := range exprs {
		extractDependenciesFromTableExpr(expr, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	}
}

// extractDependenciesFromTableExpr extracts table/view names from a single TableExpr
func extractDependenciesFromTableExpr(expr parser.TableExpr, defaultSchema string, mode GeneratorMode, legacyIgnoreQuotes bool, mysqlLowerCaseTableNames int, deps map[string]bool) {
	switch te := expr.(type) {
	case *parser.AliasedTableExpr:
		// Extract from the actual table expression
		extractDependenciesFromSimpleTableExpr(te.Expr, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.JoinTableExpr:
		// Recursively extract from both sides of the JOIN
		extractDependenciesFromTableExpr(te.LeftExpr, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
		extractDependenciesFromTableExpr(te.RightExpr, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	case *parser.ParenTableExpr:
		// Recursively extract from parenthesized table expressions
		extractDependenciesFromTableExprs(te.Exprs, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	}
}

// extractDependenciesFromSimpleTableExpr extracts table/view names from SimpleTableExpr
func extractDependenciesFromSimpleTableExpr(expr parser.SimpleTableExpr, defaultSchema string, mode GeneratorMode, legacyIgnoreQuotes bool, mysqlLowerCaseTableNames int, deps map[string]bool) {
	switch ste := expr.(type) {
	case parser.TableName:
		// This is an actual table/view reference
		schema := ste.Schema.Name
		if schema == "" {
			schema = defaultSchema
		} else {
			schema = normalizeIdentKey(ste.Schema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)
		}
		tableName := normalizeIdentKey(ste.Name, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames)

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
		extractDependenciesFromSelectStatement(ste.Select, defaultSchema, mode, legacyIgnoreQuotes, mysqlLowerCaseTableNames, deps)
	}
}
