package schema

import (
	"cmp"
	"fmt"
	"log"
	"log/slog"
	"math/big"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/parser"
	"github.com/sqldef/sqldef/v3/util"
)

type GeneratorMode int

const (
	GeneratorModeMysql = GeneratorMode(iota)
	GeneratorModePostgres
	GeneratorModeSQLite3
	GeneratorModeMssql
)

// This struct holds simulated schema states during GenerateIdempotentDDLs().
type Generator struct {
	mode          GeneratorMode
	desiredTables []*Table
	currentTables []*Table

	desiredViews []*View
	currentViews []*View

	desiredTriggers []*Trigger
	currentTriggers []*Trigger

	desiredTypes []*Type
	currentTypes []*Type

	// Track FKs that have been handled during primary key changes
	handledForeignKeys map[string]bool

	desiredComments []*Comment
	currentComments []*Comment

	desiredExtensions []*Extension
	currentExtensions []*Extension

	desiredSchemas []*Schema
	currentSchemas []*Schema

	desiredPrivileges []*GrantPrivilege
	currentPrivileges []*GrantPrivilege

	defaultSchema string

	algorithm string
	lock      string

	config database.GeneratorConfig
}

// Parse argument DDLs and call `generateDDLs()`
func GenerateIdempotentDDLs(mode GeneratorMode, sqlParser database.Parser, desiredSQL string, currentSQL string, config database.GeneratorConfig, defaultSchema string) ([]string, error) {
	// TODO: invalidate duplicated tables, columns
	desiredDDLs, err := ParseDDLs(mode, sqlParser, desiredSQL, defaultSchema)
	if err != nil {
		return nil, err
	}
	desiredDDLs = FilterTables(desiredDDLs, config)
	desiredDDLs = FilterViews(desiredDDLs, config)
	desiredDDLs = FilterPrivileges(desiredDDLs, config)

	desiredDDLs = SortTablesByDependencies(desiredDDLs)

	currentDDLs, err := ParseDDLs(mode, sqlParser, currentSQL, defaultSchema)
	if err != nil {
		return nil, err
	}
	currentDDLs = FilterTables(currentDDLs, config)
	currentDDLs = FilterViews(currentDDLs, config)
	currentDDLs = FilterPrivileges(currentDDLs, config)

	currentDDLs = SortTablesByDependencies(currentDDLs)

	aggregated, err := aggregateDDLsToSchema(currentDDLs)
	if err != nil {
		return nil, err
	}

	desiredAggregated, err := aggregateDDLsToSchema(desiredDDLs)
	if err != nil {
		return nil, err
	}

	generator := Generator{
		mode:               mode,
		desiredTables:      desiredAggregated.Tables,
		currentTables:      aggregated.Tables,
		desiredViews:       desiredAggregated.Views,
		currentViews:       aggregated.Views,
		desiredTriggers:    desiredAggregated.Triggers,
		currentTriggers:    aggregated.Triggers,
		desiredTypes:       desiredAggregated.Types,
		currentTypes:       aggregated.Types,
		desiredComments:    desiredAggregated.Comments,
		currentComments:    aggregated.Comments,
		desiredExtensions:  desiredAggregated.Extensions,
		currentExtensions:  aggregated.Extensions,
		desiredSchemas:     desiredAggregated.Schemas,
		currentSchemas:     aggregated.Schemas,
		desiredPrivileges:  desiredAggregated.Privileges,
		currentPrivileges:  aggregated.Privileges,
		defaultSchema:      defaultSchema,
		algorithm:          config.Algorithm,
		lock:               config.Lock,
		config:             config,
		handledForeignKeys: make(map[string]bool),
	}
	return generator.generateDDLs(desiredDDLs)
}

// Main part of DDL generation
func (g *Generator) generateDDLs(desiredDDLs []DDL) ([]string, error) {
	// These variables are used to control the output order of the DDL.
	// `CREATE SCHEMA` should execute first, and DDLs that add indexes and foreign keys should execute last.
	// Other DDLs are stored in interDDLs.
	createExtensionDDLs := []string{}
	createSchemaDDLs := []string{}
	interDDLs := []string{}
	indexDDLs := []string{}
	foreignKeyDDLs := []string{}
	exclusionDDLs := []string{}
	viewDDLs := []string{}

	// Incrementally examine desiredDDLs
	for _, ddl := range desiredDDLs {
		switch desired := ddl.(type) {
		case *CreateTable:
			if currentTable := findTableByName(g.currentTables, desired.table.name); currentTable != nil {
				// Table already exists, guess required DDLs.
				tableDDLs, err := g.generateDDLsForCreateTable(*currentTable, *desired)
				if err != nil {
					return nil, err
				}
				for _, tableDDL := range tableDDLs {
					if isAddConstraintForeignKey(tableDDL) {
						foreignKeyDDLs = append(foreignKeyDDLs, tableDDL)
					} else {
						interDDLs = append(interDDLs, tableDDL)
					}
				}
				mergeTable(currentTable, desired.table)
			} else {
				// Table not found. Check if it's a rename from another table.
				if desired.table.renamedFrom != "" {
					oldTableName := g.normalizeOldTableName(desired.table.renamedFrom, desired.table.name)
					oldTable := findTableByName(g.currentTables, oldTableName)
					if oldTable != nil {
						// Found the old table, generate rename DDL
						renameDDL := g.generateRenameTableDDL(oldTableName, desired.table.name)
						interDDLs = append(interDDLs, renameDDL)

						// Update the old table's name to the new name
						oldTable.name = desired.table.name

						// Now generate DDLs for any column/index changes
						tableDDLs, err := g.generateDDLsForCreateTable(*oldTable, *desired)
						if err != nil {
							return nil, err
						}
						for _, tableDDL := range tableDDLs {
							if isAddConstraintForeignKey(tableDDL) {
								foreignKeyDDLs = append(foreignKeyDDLs, tableDDL)
							} else {
								interDDLs = append(interDDLs, tableDDL)
							}
						}
						mergeTable(oldTable, desired.table)
					} else {
						// Old table not found, create as new table
						interDDLs = append(interDDLs, desired.statement)
						table := desired.table // copy table
						g.currentTables = append(g.currentTables, &table)
					}
				} else {
					// Table not found and no rename, create table.
					interDDLs = append(interDDLs, desired.statement)
					table := desired.table // copy table
					g.currentTables = append(g.currentTables, &table)
				}
			}
			// Only add to desiredTables if it doesn't already exist (it may have been pre-populated from aggregation)
			if findTableByName(g.desiredTables, desired.table.name) == nil {
				table := desired.table // copy table
				g.desiredTables = append(g.desiredTables, &table)
			}
		case *CreateIndex:
			idxDDLs, err := g.generateDDLsForCreateIndex(desired.tableName, desired.index, "CREATE INDEX", ddl.Statement())
			if err != nil {
				return nil, err
			}
			indexDDLs = append(indexDDLs, idxDDLs...)
		case *AddIndex:
			idxDDLs, err := g.generateDDLsForCreateIndex(desired.tableName, desired.index, "ALTER TABLE", ddl.Statement())
			if err != nil {
				return nil, err
			}
			indexDDLs = append(indexDDLs, idxDDLs...)
		case *AddForeignKey:
			fkeyDDLs, err := g.generateDDLsForAddForeignKey(desired.tableName, desired.foreignKey, "ALTER TABLE", ddl.Statement())
			if err != nil {
				return nil, err
			}
			foreignKeyDDLs = append(foreignKeyDDLs, fkeyDDLs...)
		case *AddExclusion:
			exDDLs, err := g.generateDDLsForAddExclusion(desired.tableName, desired.exclusion, "ALTER TABLE", ddl.Statement())
			if err != nil {
				return nil, err
			}
			exclusionDDLs = append(exclusionDDLs, exDDLs...)
		case *AddPolicy:
			policyDDLs, err := g.generateDDLsForCreatePolicy(desired.tableName, desired.policy, "CREATE POLICY", ddl.Statement())
			if err != nil {
				return nil, err
			}
			interDDLs = append(interDDLs, policyDDLs...)
		case *View:
			ddls, err := g.generateDDLsForCreateView(desired.name, desired)
			if err != nil {
				return nil, err
			}
			viewDDLs = append(viewDDLs, ddls...)
		case *Trigger:
			triggerDDLs, err := g.generateDDLsForCreateTrigger(desired.name, desired)
			if err != nil {
				return nil, err
			}
			interDDLs = append(interDDLs, triggerDDLs...)
		case *Type:
			typeDDLs, err := g.generateDDLsForCreateType(desired)
			if err != nil {
				return nil, err
			}
			interDDLs = append(interDDLs, typeDDLs...)
		case *Comment:
			commentDDLs, err := g.generateDDLsForComment(desired)
			if err != nil {
				return nil, err
			}
			interDDLs = append(interDDLs, commentDDLs...)
		case *Extension:
			extensionDDLs, err := g.generateDDLsForExtension(desired)
			if err != nil {
				return nil, err
			}
			createExtensionDDLs = append(createExtensionDDLs, extensionDDLs...)
		case *Schema:
			schemaDDLs, err := g.generateDDLsForSchema(desired)
			if err != nil {
				return nil, err
			}
			createSchemaDDLs = append(createSchemaDDLs, schemaDDLs...)
		case *GrantPrivilege:
			privilegeDDLs, err := g.generateDDLsForGrantPrivilege(desired)
			if err != nil {
				return nil, err
			}
			interDDLs = append(interDDLs, privilegeDDLs...)
		case *RevokePrivilege:
			revokeDDLs, err := g.generateDDLsForRevokePrivilege(desired)
			if err != nil {
				return nil, err
			}
			interDDLs = append(interDDLs, revokeDDLs...)
		default:
			return nil, fmt.Errorf("unexpected ddl type in generateDDLs: %v", desired)
		}
	}

	ddls := []string{}
	ddls = append(ddls, createExtensionDDLs...)
	ddls = append(ddls, createSchemaDDLs...)
	ddls = append(ddls, interDDLs...)
	ddls = append(ddls, viewDDLs...)
	ddls = append(ddls, indexDDLs...)
	ddls = append(ddls, foreignKeyDDLs...)
	ddls = append(ddls, exclusionDDLs...)

	var tablesToDrop []*Table
	for _, currentTable := range g.currentTables {
		desiredTable := findTableByName(g.desiredTables, currentTable.name)
		if desiredTable == nil {
			tablesToDrop = append(tablesToDrop, currentTable)
		}
	}

	// Sort tables to be dropped by dependencies (dependent tables first)
	if len(tablesToDrop) > 0 {
		dropTableDDLs := g.generateDropTableDDLsWithDependencies(tablesToDrop)
		ddls = append(ddls, dropTableDDLs...)

		// Remove dropped tables from currentTables
		for _, table := range tablesToDrop {
			g.currentTables = removeTableByName(g.currentTables, table.name)
		}
	}

	// Clean up obsoleted indexes, columns in remaining tables
	for _, currentTable := range g.currentTables {
		desiredTable := findTableByName(g.desiredTables, currentTable.name)
		if desiredTable == nil {
			continue // Already handled in drop tables above
		}

		// Table is expected to exist. Drop foreign keys prior to index deletion
		for _, foreignKey := range currentTable.foreignKeys {
			// Skip foreign keys without constraint names - they're likely from column-level REFERENCES
			// that haven't been fully processed yet
			if foreignKey.constraintName == "" {
				continue
			}

			// Generate constraint names for desired foreign keys (for comparison)
			desiredConstraintNames := []string{}
			for _, desiredFK := range desiredTable.foreignKeys {
				constraintName := desiredFK.constraintName
				// Auto-generate constraint name if not specified (for PostgreSQL)
				if constraintName == "" && g.mode == GeneratorModePostgres && len(desiredFK.indexColumns) > 0 {
					tableName := extractTableName(desiredTable.name)
					columnName := desiredFK.indexColumns[0]
					constraintName = buildPostgresConstraintName(tableName, columnName, "fkey")
				}
				desiredConstraintNames = append(desiredConstraintNames, constraintName)
			}

			if slices.Contains(desiredConstraintNames, foreignKey.constraintName) {
				continue // Foreign key is expected to exist.
			}

			// The foreign key seems obsoleted. Check and drop it as needed.
			foreignKeyDDLs := g.generateDDLsForAbsentForeignKey(foreignKey, *currentTable, *desiredTable)
			ddls = append(ddls, foreignKeyDDLs...)
			// TODO: simulate to remove foreign key from `currentTable.foreignKeys`?
		}

		// Table is expected to exist. Drop exclusion constraints.
		for _, exclusion := range currentTable.exclusions {
			if slices.Contains(convertExclusionToConstraintNames(desiredTable.exclusions), exclusion.constraintName) {
				continue // Exclusion constraint is expected to exist.
			}

			ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(currentTable.name), g.escapeSQLName(exclusion.constraintName)))
		}

		// Check indexes
		for _, index := range currentTable.indexes {

			// Alter statement for primary key index should be generated above.
			if index.primary {
				continue
			}

			if slices.Contains(convertIndexesToIndexNames(desiredTable.indexes), index.name) ||
				slices.Contains(convertForeignKeysToIndexNames(desiredTable.foreignKeys), index.name) {
				continue // Index is expected to exist.
			}

			// Check if this index was renamed (don't drop if it was renamed)
			isRenamed := false
			for _, desiredIndex := range desiredTable.indexes {
				if desiredIndex.renamedFrom == index.name {
					isRenamed = true
					break
				}
			}
			if isRenamed {
				continue // Index was renamed, don't drop it
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
			if _, exist := desiredTable.columns[column.name]; exist {
				continue // Column is expected to exist.
			}

			// Check if this column is being renamed (not dropped)
			isRenamed := false
			for _, desiredColumn := range desiredTable.columns {
				if desiredColumn.renamedFrom == column.name {
					isRenamed = true
					break
				}
			}

			if !isRenamed {
				// Column is obsoleted. Drop column.
				columnDDLs := g.generateDDLsForAbsentColumn(currentTable, column.name)
				ddls = append(ddls, columnDDLs...)
				// TODO: simulate to remove column from `currentTable.columns`?
			}
		}

		// Check policies.
		for _, policy := range currentTable.policies {
			if slices.Contains(convertPolicyNames(desiredTable.policies), policy.name) {
				continue
			}
			ddls = append(ddls, fmt.Sprintf("DROP POLICY %s ON %s", g.escapeSQLName(policy.name), g.escapeTableName(currentTable.name)))
		}

		// Check checks.
		for _, check := range currentTable.checks {
			// First try to find by name
			if findCheckConstraintInTable(desiredTable, check.constraintName) != nil {
				continue
			}

			// For MySQL and MSSQL, also check if this constraint matches any column-level CHECK by definition
			// This handles auto-generated constraint names for column-level CHECKs
			if (g.mode == GeneratorModeMysql || g.mode == GeneratorModeMssql) && g.findCheckConstraintByDefinition(desiredTable, &check) != nil {
				continue
			}

			switch g.mode {
			case GeneratorModePostgres, GeneratorModeMssql, GeneratorModeSQLite3:
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(currentTable.name), g.escapeSQLName(check.constraintName)))
			case GeneratorModeMysql:
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CHECK %s", g.escapeTableName(currentTable.name), g.escapeSQLName(check.constraintName)))
			}
		}
	}

	// Clean up obsoleted views
	for _, currentView := range g.currentViews {
		if slices.Contains(convertViewNames(g.desiredViews), currentView.name) {
			continue
		}
		if currentView.viewType == "MATERIALIZED VIEW" {
			ddls = append(ddls, fmt.Sprintf("DROP MATERIALIZED VIEW %s", g.escapeTableName(currentView.name)))
			continue
		}
		ddls = append(ddls, fmt.Sprintf("DROP VIEW %s", g.escapeTableName(currentView.name)))
	}

	// Clean up obsoleted extensions
	for _, currentExtension := range g.currentExtensions {
		if slices.Contains(convertExtensionNames(g.desiredExtensions), currentExtension.extension.Name) {
			continue
		}
		ddls = append(ddls, fmt.Sprintf("DROP EXTENSION %s", g.escapeSQLName(currentExtension.extension.Name)))
	}

	// Clean up obsoleted comments
	for _, currentComment := range g.currentComments {
		// Check if this comment still exists in desired comments
		desiredComment := findCommentByObject(g.desiredComments, currentComment.comment.Object)
		// Only generate NULL statement if the comment is completely absent from desired schema
		// If desiredComment exists but is empty, it means the desired schema has "COMMENT ... IS NULL",
		// which will be handled by generateDDLsForComment
		if desiredComment == nil {
			// Comment was completely removed, generate COMMENT ... IS NULL
			slog.Debug("Generating NULL statement for removed comment",
				"object", currentComment.comment.Object,
				"statement", currentComment.statement)
			nullStmt := g.generateCommentNullStatement(currentComment)
			if nullStmt != "" {
				slog.Debug("Generated NULL statement", "stmt", nullStmt)
				ddls = append(ddls, nullStmt)
			}
		}
	}

	// Clean up obsoleted triggers
	for _, currentTrigger := range g.currentTriggers {
		if g.mode != GeneratorModeSQLite3 {
			continue
		}
		desitedTrigger := findTriggerByName(g.desiredTriggers, currentTrigger.name)
		if desitedTrigger == nil {
			ddls = append(ddls, fmt.Sprintf("DROP TRIGGER %s", g.escapeSQLName(currentTrigger.name)))
			continue
		}
	}

	if g.config.EnableDrop && g.mode == GeneratorModePostgres {
		for _, currentPriv := range g.currentPrivileges {
			hasIncludedGrantee := false
			for _, grantee := range currentPriv.grantees {
				if slices.Contains(g.config.ManagedRoles, grantee) {
					hasIncludedGrantee = true
					break
				}
			}
			if len(currentPriv.grantees) > 0 && !hasIncludedGrantee {
				continue
			}

			found := false
			for _, desiredPriv := range g.desiredPrivileges {
				if currentPriv.tableName == desiredPriv.tableName &&
					len(currentPriv.grantees) > 0 && len(desiredPriv.grantees) > 0 &&
					currentPriv.grantees[0] == desiredPriv.grantees[0] {
					found = true
					break
				}
			}

			if !found {
				escapedGrantee, err := g.validateAndEscapeGrantee(currentPriv.grantees[0])
				if err != nil {
					return nil, err
				}

				revoke := fmt.Sprintf("REVOKE %s ON TABLE %s FROM %s",
					formatPrivilegesForGrant(currentPriv.privileges),
					g.escapeTableName(currentPriv.tableName),
					escapedGrantee)
				ddls = append(ddls, revoke)
			}
		}
	}

	if isValidAlgorithm(g.algorithm) {
		for i := range ddls {
			if strings.HasPrefix(ddls[i], "ALTER TABLE") {
				ddls[i] += ", ALGORITHM=" + strings.ToUpper(g.algorithm)
			}
		}
	}

	if isValidLock(g.lock) {
		for i := range ddls {
			if strings.HasPrefix(ddls[i], "ALTER TABLE") {
				ddls[i] += ", LOCK=" + strings.ToUpper(g.lock)
			}
		}
	}

	return ddls, nil
}

func (g *Generator) generateDDLsForAbsentColumn(currentTable *Table, columnName string) []string {
	ddls := []string{}

	// Only MSSQL has column default constraints. They need to be deleted before dropping the column.
	if g.mode == GeneratorModeMssql {
		for _, column := range currentTable.columns {
			if column.name == columnName && column.defaultDef != nil && column.defaultDef.constraintName != "" {
				ddl := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(currentTable.name), g.escapeSQLName(column.defaultDef.constraintName))
				ddls = append(ddls, ddl)
			}
		}
	}

	ddl := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", g.escapeTableName(currentTable.name), g.escapeSQLName(columnName))
	return append(ddls, ddl)
}

// In the caller, `mergeTable` manages `g.currentTables`.
func (g *Generator) generateDDLsForCreateTable(currentTable Table, desired CreateTable) ([]string, error) {
	ddls := []string{}
	// Track foreign keys that need to be recreated after primary key changes
	var fkRecreationDDLs []string
	var desiredColumns = make([]*Column, len(desired.table.columns))
	for _, col := range desired.table.columns {
		desiredColumns[col.position] = col
	}

	// Examine each column
	for _, desiredColumnPtr := range desiredColumns {
		// deep copy to avoid modifying the original
		desiredColumn := *desiredColumnPtr

		if desiredColumn.renamedFrom != "" {
			if _, conflictExists := desired.table.columns[desiredColumn.renamedFrom]; conflictExists {
				return ddls, fmt.Errorf("cannot rename column '%s' to '%s' - column '%s' still exists",
					desiredColumn.renamedFrom, desiredColumn.name, desiredColumn.renamedFrom)
			}
		}

		currentColumn := findColumnByName(currentTable.columns, desiredColumn.name)
		if currentColumn == nil || !currentColumn.autoIncrement {
			// We may not be able to add AUTO_INCREMENT yet. It will be added after adding keys (primary or not) at the "Add new AUTO_INCREMENT" place.
			// prevent to
			desiredColumn.autoIncrement = false
		}
		if currentColumn == nil {
			// Check if this is a renamed column
			var renameFromColumn *Column
			if desiredColumn.renamedFrom != "" {
				renameFromColumn = findColumnByName(currentTable.columns, desiredColumn.renamedFrom)
			}

			if renameFromColumn != nil {
				// Generate RENAME COLUMN DDL
				switch g.mode {
				case GeneratorModePostgres:
					ddl := fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s",
						g.escapeTableName(desired.table.name),
						g.escapeSQLName(renameFromColumn.name),
						g.escapeSQLName(desiredColumn.name))
					ddls = append(ddls, ddl)

					// After renaming, check if type/constraints need to be changed
					if !g.haveSameDataType(*renameFromColumn, desiredColumn) {
						ddl := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s",
							g.escapeTableName(desired.table.name),
							g.escapeSQLName(desiredColumn.name),
							g.generateDataType(desiredColumn))
						ddls = append(ddls, ddl)
					}

					if !isPrimaryKey(*renameFromColumn, currentTable) {
						if g.notNull(*renameFromColumn) && !g.notNull(desiredColumn) {
							ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL",
								g.escapeTableName(desired.table.name),
								g.escapeSQLName(desiredColumn.name)))
						} else if !g.notNull(*renameFromColumn) && g.notNull(desiredColumn) {
							ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL",
								g.escapeTableName(desired.table.name),
								g.escapeSQLName(desiredColumn.name)))
						}
					}
				case GeneratorModeMysql:
					// MySQL uses CHANGE COLUMN for rename
					definition, err := g.generateColumnDefinition(desiredColumn, true)
					if err != nil {
						return ddls, err
					}
					ddl := fmt.Sprintf("ALTER TABLE %s CHANGE COLUMN %s %s",
						g.escapeTableName(desired.table.name),
						g.escapeSQLName(renameFromColumn.name),
						definition)
					ddls = append(ddls, ddl)
				case GeneratorModeMssql:
					// SQL Server uses sp_rename
					// For sp_rename, we need to handle schema prefixes properly
					schema, tableName := splitTableName(desired.table.name, g.defaultSchema)
					var tableRef string
					if schema != "" && schema != g.defaultSchema {
						// Only include schema if it's not the default
						tableRef = fmt.Sprintf("%s.%s", schema, tableName)
					} else {
						tableRef = tableName
					}
					ddl := fmt.Sprintf("EXEC sp_rename '%s.%s', '%s', 'COLUMN'",
						tableRef,
						renameFromColumn.name,
						desiredColumn.name)
					ddls = append(ddls, ddl)

					// After renaming, check if type/constraints need to be changed
					if !g.haveSameDataType(*renameFromColumn, desiredColumn) ||
						!g.areSameDefaultValue(renameFromColumn.defaultDef, desiredColumn.defaultDef, desiredColumn.typeName) ||
						(g.notNull(*renameFromColumn) != g.notNull(desiredColumn)) {
						definition, err := g.generateColumnDefinition(desiredColumn, false)
						if err != nil {
							return ddls, err
						}
						// Use consistent table name format (without default schema prefix)
						var escapedTableName string
						if schema != "" && schema != g.defaultSchema {
							escapedTableName = g.escapeSQLName(schema) + "." + g.escapeSQLName(tableName)
						} else {
							escapedTableName = g.escapeSQLName(tableName)
						}
						ddl := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s",
							escapedTableName,
							definition)
						ddls = append(ddls, ddl)
					}
				case GeneratorModeSQLite3:
					// For SQLite, when type needs to change:
					// 1. Add new column with new name and type
					// 2. Copy data from old column to new column
					// 3. Drop old column
					if !g.haveSameDataType(*renameFromColumn, desiredColumn) ||
						!g.areSameDefaultValue(renameFromColumn.defaultDef, desiredColumn.defaultDef, desiredColumn.typeName) ||
						(g.notNull(*renameFromColumn) != g.notNull(desiredColumn)) {

						definition, err := g.generateColumnDefinition(desiredColumn, true)
						if err != nil {
							return ddls, err
						}

						// 1. Add new column with desired name and definition
						ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s",
							g.escapeTableName(desired.table.name),
							definition))

						// 2. Copy data from old column to new column
						ddls = append(ddls, fmt.Sprintf("UPDATE %s SET %s = %s",
							g.escapeTableName(desired.table.name),
							g.escapeSQLName(desiredColumn.name),
							g.escapeSQLName(renameFromColumn.name)))

						// 3. Drop the old column
						ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s",
							g.escapeTableName(desired.table.name),
							g.escapeSQLName(renameFromColumn.name)))
					} else {
						// Simple rename without type change
						ddl := fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s",
							g.escapeTableName(desired.table.name),
							g.escapeSQLName(renameFromColumn.name),
							g.escapeSQLName(desiredColumn.name))
						ddls = append(ddls, ddl)
					}
				default:
					// Fallback to regular ADD for unsupported databases
					definition, err := g.generateColumnDefinition(desiredColumn, true)
					if err != nil {
						return ddls, err
					}
					ddl := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", g.escapeTableName(desired.table.name), definition)
					ddls = append(ddls, ddl)
				}
			} else {
				// Regular column addition (not a rename)
				definition, err := g.generateColumnDefinition(desiredColumn, true)
				if err != nil {
					return ddls, err
				}

				// Column not found, add column.
				var ddl string
				switch g.mode {
				case GeneratorModeMssql:
					ddl = fmt.Sprintf("ALTER TABLE %s ADD %s", g.escapeTableName(desired.table.name), definition)
				default:
					ddl = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", g.escapeTableName(desired.table.name), definition)
				}

				if g.mode == GeneratorModeMysql {
					after := " FIRST"
					if desiredColumn.position > 0 {
						after = " AFTER " + g.escapeSQLName(desiredColumns[desiredColumn.position-1].name)
					}
					ddl += after
				}

				ddls = append(ddls, ddl)
			}
		} else {
			// Change column data type or order as needed.
			switch g.mode {
			case GeneratorModeMysql:
				currentPos := currentColumn.position
				desiredPos := desiredColumn.position
				changeOrder := currentPos > desiredPos && currentPos-desiredPos > len(currentTable.columns)-len(desired.table.columns)

				// Change column type and orders, *except* AUTO_INCREMENT and UNIQUE KEY.
				if !g.haveSameColumnDefinition(*currentColumn, desiredColumn) || !g.areSameDefaultValue(currentColumn.defaultDef, desiredColumn.defaultDef, desiredColumn.typeName) || !g.areSameGenerated(currentColumn.generated, desiredColumn.generated) || changeOrder {
					definition, err := g.generateColumnDefinition(desiredColumn, false)
					if err != nil {
						return ddls, err
					}

					if desiredColumn.generated != nil {
						ddl1 := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentColumn.name))
						ddl2 := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", g.escapeTableName(desired.table.name), definition)
						after := " FIRST"
						if desiredColumn.position > 0 {
							after = " AFTER " + g.escapeSQLName(desiredColumns[desiredColumn.position-1].name)
						}
						ddl2 += after
						ddls = append(ddls, ddl1, ddl2)
					} else {
						ddl := fmt.Sprintf("ALTER TABLE %s CHANGE COLUMN %s %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentColumn.name), definition)
						if changeOrder {
							after := " FIRST"
							if desiredColumn.position > 0 {
								after = " AFTER " + g.escapeSQLName(desiredColumns[desiredColumn.position-1].name)
							}
							ddl += after
						}
						ddls = append(ddls, ddl)
					}
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
					ddl := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentColumn.name), g.generateDataType(desiredColumn))
					ddls = append(ddls, ddl)
				}

				if !isPrimaryKey(*currentColumn, currentTable) { // Primary Key implies NOT NULL
					if g.notNull(*currentColumn) && !g.notNull(desiredColumn) {
						ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", g.escapeTableName(desired.table.name), g.escapeSQLName(currentColumn.name)))
					} else if !g.notNull(*currentColumn) && g.notNull(desiredColumn) {
						ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL", g.escapeTableName(desired.table.name), g.escapeSQLName(currentColumn.name)))
					}
				}

				// GENERATED AS IDENTITY
				if !areSameIdentityDefinition(currentColumn.identity, desiredColumn.identity) {
					if currentColumn.identity == nil {
						// add
						alter := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s ADD GENERATED %s AS IDENTITY", g.escapeTableName(desired.table.name), g.escapeSQLName(desiredColumn.name), desiredColumn.identity.behavior)
						if desiredColumn.sequence != nil {
							alter += " (" + generateSequenceClause(desiredColumn.sequence) + ")"
						}
						ddls = append(ddls, alter)
					} else if desiredColumn.identity == nil {
						// remove
						ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP IDENTITY IF EXISTS", g.escapeTableName(currentTable.name), g.escapeSQLName(currentColumn.name)))
					} else {
						// set
						// not support changing sequence
						ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET GENERATED %s", g.escapeTableName(desired.table.name), g.escapeSQLName(desiredColumn.name), desiredColumn.identity.behavior))
					}
				}

				// default
				if !g.areSameDefaultValue(currentColumn.defaultDef, desiredColumn.defaultDef, desiredColumn.typeName) {
					if desiredColumn.defaultDef == nil {
						// drop
						ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT", g.escapeTableName(currentTable.name), g.escapeSQLName(currentColumn.name)))
					} else {
						// set
						definition, err := g.generateDefaultDefinition(*desiredColumn.defaultDef)
						if err != nil {
							return ddls, err
						}
						ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET %s", g.escapeTableName(currentTable.name), g.escapeSQLName(currentColumn.name), definition))
					}
				}

				tableName := extractTableName(desired.table.name)
				constraintName := buildPostgresConstraintName(tableName, desiredColumn.name, "check")
				if desiredColumn.check != nil && desiredColumn.check.constraintName != "" {
					constraintName = desiredColumn.check.constraintName
				}

				currentCheck := findCheckConstraintInTable(&currentTable, constraintName)

				if !g.areSameCheckDefinition(currentCheck, desiredColumn.check) { // || currentColumn.checkNoInherit != desiredColumn.checkNoInherit {
					if currentCheck != nil {
						ddl := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(desired.table.name), constraintName)
						ddls = append(ddls, ddl)
					}
					if desiredColumn.check != nil {
						ddl := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)", g.escapeTableName(desired.table.name), constraintName, g.normalizeCheckExprString(desiredColumn.check.definition))
						if desiredColumn.check.noInherit {
							ddl += " NO INHERIT"
						}
						ddls = append(ddls, ddl)
					}
				}

				// TODO: support adding a column's `references`
			case GeneratorModeMssql:
				if !g.haveSameColumnDefinition(*currentColumn, desiredColumn) {
					// Change column definition
					definition, err := g.generateColumnDefinition(desiredColumn, false)
					if err != nil {
						return ddls, err
					}
					ddl := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s", g.escapeTableName(desired.table.name), definition)
					ddls = append(ddls, ddl)
				}

				if !g.areSameCheckDefinition(currentColumn.check, desiredColumn.check) {
					// For MSSQL, column-level CHECKs might actually be table-level CHECKs that MSSQL converted
					// Check if the current column-level CHECK matches a table-level CHECK in desired
					skipDrop := false
					if currentColumn.check != nil && desiredColumn.check == nil {
						// Current has column-level CHECK, desired doesn't
						// Check if it matches a table-level CHECK in desired
						if findCheckConstraintByName(desired.table.checks, currentColumn.check.constraintName) != nil ||
							g.findCheckConstraintByDefinitionInList(desired.table.checks, currentColumn.check) != nil {
							// This column-level CHECK is actually a table-level CHECK
							// It will be handled in the table-level CHECK processing
							skipDrop = true
						}
					}

					if !skipDrop {
						tableName := extractTableName(desired.table.name)
						constraintName := buildPostgresConstraintName(tableName, desiredColumn.name, "check")
						if currentColumn.check != nil {
							currentConstraintName := currentColumn.check.constraintName
							ddl := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(desired.table.name), currentConstraintName)
							ddls = append(ddls, ddl)
						}
						if desiredColumn.check != nil {
							desiredConstraintName := desiredColumn.check.constraintName
							if desiredConstraintName == "" {
								desiredConstraintName = constraintName
							}
							replicationDefinition := ""
							if desiredColumn.check.notForReplication {
								replicationDefinition = " NOT FOR REPLICATION"
							}
							ddl := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK%s (%s)", g.escapeTableName(desired.table.name), desiredConstraintName, replicationDefinition, g.normalizeCheckExprString(desiredColumn.check.definition))
							ddls = append(ddls, ddl)
						}
					}
				}

				// IDENTITY
				if !areSameIdentityDefinition(currentColumn.identity, desiredColumn.identity) {
					if currentColumn.identity != nil {
						// remove
						ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", g.escapeTableName(currentTable.name), g.escapeSQLName(currentColumn.name)))
					}
					if desiredColumn.identity != nil {
						definition, err := g.generateColumnDefinition(desiredColumn, true)
						if err != nil {
							return ddls, err
						}
						ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ADD %s", g.escapeTableName(desired.table.name), definition))
					}
				}

				// DEFAULT
				if !g.areSameDefaultValue(currentColumn.defaultDef, desiredColumn.defaultDef, desiredColumn.typeName) {
					if currentColumn.defaultDef != nil {
						// drop
						ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(currentTable.name), g.escapeSQLName(currentColumn.defaultDef.constraintName)))
					}
					if desiredColumn.defaultDef != nil {
						// set
						definition, err := g.generateDefaultDefinition(*desiredColumn.defaultDef)
						if err != nil {
							return ddls, err
						}
						var ddl string
						if desiredColumn.defaultDef.constraintName != "" {
							ddl = fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s FOR %s", g.escapeTableName(currentTable.name), g.escapeSQLName(desiredColumn.defaultDef.constraintName), definition, g.escapeSQLName(currentColumn.name))
						} else {
							ddl = fmt.Sprintf("ALTER TABLE %s ADD %s FOR %s", g.escapeTableName(currentTable.name), definition, g.escapeSQLName(currentColumn.name))
						}
						ddls = append(ddls, ddl)
					}
				}
			default:
			}
		}
	}

	currentPrimaryKey := currentTable.PrimaryKey()
	desiredPrimaryKey := desired.table.PrimaryKey()

	primaryKeysChanged := !g.areSamePrimaryKeys(currentPrimaryKey, desiredPrimaryKey)

	// Remove old AUTO_INCREMENT from deleted column before deleting key (primary or not)
	// and if primary key changed
	if g.mode == GeneratorModeMysql {
		for _, currentColumn := range currentTable.columns {
			desiredColumn := findColumnByName(desired.table.columns, currentColumn.name)
			if currentColumn.autoIncrement && (primaryKeysChanged || desiredColumn == nil || !desiredColumn.autoIncrement) {
				currentColumn.autoIncrement = false
				definition, err := g.generateColumnDefinition(*currentColumn, false)
				if err != nil {
					return ddls, err
				}
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s CHANGE COLUMN %s %s", g.escapeTableName(currentTable.name), g.escapeSQLName(currentColumn.name), definition))
			}
		}
	}

	// Examine primary key
	if primaryKeysChanged {
		// Check if there are foreign keys referencing this table's primary key
		referencingFKs := g.findForeignKeysReferencingTable(desired.table.name)

		var dropFKDDLs []string
		var recreateFKDDLs []string

		// If there are foreign keys referencing this table,
		// we need to drop them first before modifying the primary key
		if len(referencingFKs) > 0 {
			for _, refFK := range referencingFKs {
				var dropFKDDL string
				switch g.mode {
				case GeneratorModeMysql:
					dropFKDDL = fmt.Sprintf("ALTER TABLE %s DROP FOREIGN KEY %s",
						g.escapeTableName(refFK.tableName),
						g.escapeSQLName(refFK.foreignKey.constraintName))
				case GeneratorModePostgres, GeneratorModeMssql:
					dropFKDDL = fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s",
						g.escapeTableName(refFK.tableName),
						g.escapeSQLName(refFK.foreignKey.constraintName))
				}
				if dropFKDDL != "" {
					dropFKDDLs = append(dropFKDDLs, dropFKDDL)
				}

				// Update the current state to reflect that we've dropped this FK
				// This prevents duplicate FK creation when processing the referencing table
				for _, table := range g.currentTables {
					if table.name == refFK.tableName {
						// Remove the FK from the current table's FK list
						newFKs := []ForeignKey{}
						for _, fk := range table.foreignKeys {
							if fk.constraintName != refFK.foreignKey.constraintName {
								newFKs = append(newFKs, fk)
							}
						}
						table.foreignKeys = newFKs
						break
					}
				}

				// Also drop the index if it exists (MySQL creates implicit indexes for FKs)
				// PostgreSQL and SQL Server don't create implicit indexes for FKs
				if g.mode == GeneratorModeMysql {
					dropIndexDDL := fmt.Sprintf("ALTER TABLE %s DROP INDEX %s",
						g.escapeTableName(refFK.tableName),
						g.escapeSQLName(refFK.foreignKey.constraintName))
					dropFKDDLs = append(dropFKDDLs, dropIndexDDL)
				}

				// Look for the corresponding desired foreign key to get updated columns
				// We need to find the desired table that references our table
				var desiredFK *ForeignKey
				var desiredTableExists bool
				for _, desiredTable := range g.desiredTables {
					if desiredTable.name == refFK.tableName {
						desiredTableExists = true
						for _, fk := range desiredTable.foreignKeys {
							if fk.constraintName == refFK.foreignKey.constraintName {
								desiredFK = &fk
								break
							}
						}
						break
					}
				}

				// Only recreate the foreign key if:
				// 1. The referencing table exists in the desired schema, AND
				// 2. The foreign key exists in the desired schema
				if desiredTableExists && desiredFK != nil {
					recreateDDL := g.buildForeignKeyDDL(refFK.tableName, desiredFK)
					recreateFKDDLs = append(recreateFKDDLs, recreateDDL)
					// Mark this FK as globally handled so we don't add it again in normal FK processing
					g.handledForeignKeys[refFK.tableName+":"+desiredFK.constraintName] = true
				}
				// If the table doesn't exist in desired schema or the FK doesn't exist,
				// we don't recreate it (it will be dropped with the table)
			}

			// Add the DROP FK statements before we modify the primary key
			ddls = append(ddls, dropFKDDLs...)
		}

		if currentPrimaryKey != nil {
			switch g.mode {
			case GeneratorModeMysql:
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP PRIMARY KEY", g.escapeTableName(desired.table.name)))
			case GeneratorModePostgres:
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentPrimaryKey.name)))
			case GeneratorModeMssql:
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentPrimaryKey.name)))
			default:
			}
		}
		if desiredPrimaryKey != nil {
			ddls = append(ddls, g.generateAddIndex(desired.table.name, *desiredPrimaryKey))
		}

		// Store the FK recreation DDLs to be added at the end
		if len(recreateFKDDLs) > 0 {
			fkRecreationDDLs = append(fkRecreationDDLs, recreateFKDDLs...)
		}
	}

	// Examine each index
	for _, desiredIndex := range desired.table.indexes {
		if desiredIndex.primary {
			continue
		}

		if currentIndex := findIndexByName(currentTable.indexes, desiredIndex.name); currentIndex != nil {
			// Drop and add index as needed.
			if !g.areSameIndexes(*currentIndex, desiredIndex) {
				ddls = append(ddls, g.generateDropIndex(desired.table.name, desiredIndex.name, desiredIndex.constraint))
				ddls = append(ddls, g.generateAddIndex(desired.table.name, desiredIndex))
			}
		} else {
			// Check if this is a renamed index
			var renameFromIndex *Index
			if desiredIndex.renamedFrom != "" {
				renameFromIndex = findIndexByName(currentTable.indexes, desiredIndex.renamedFrom)
			}

			if renameFromIndex != nil {
				// Generate RENAME INDEX DDL
				renameDDLs := g.generateRenameIndex(desired.table.name, renameFromIndex.name, desiredIndex.name, &desiredIndex)
				ddls = append(ddls, renameDDLs...)
			} else {
				// Index not found and not a rename, add index.
				ddls = append(ddls, g.generateAddIndex(desired.table.name, desiredIndex))
			}
		}
	}

	// Add new AUTO_INCREMENT after adding index and primary key
	if g.mode == GeneratorModeMysql {
		for _, desiredColumn := range desired.table.columns {
			currentColumn := findColumnByName(currentTable.columns, desiredColumn.name)
			if desiredColumn.autoIncrement && (primaryKeysChanged || currentColumn == nil || !currentColumn.autoIncrement) {
				definition, err := g.generateColumnDefinition(*desiredColumn, false)
				if err != nil {
					return ddls, err
				}
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s CHANGE COLUMN %s %s", g.escapeTableName(currentTable.name), g.escapeSQLName(desiredColumn.name), definition))
			}
		}
	}

	// Examine each foreign key
	for _, desiredForeignKey := range desired.table.foreignKeys {
		// Auto-generate constraint name if not specified (for PostgreSQL)
		constraintName := desiredForeignKey.constraintName
		if constraintName == "" && g.mode == GeneratorModePostgres && len(desiredForeignKey.indexColumns) > 0 {
			tableName := extractTableName(desired.table.name)
			// Use the first column name for the constraint name (matches PostgreSQL behavior)
			columnName := desiredForeignKey.indexColumns[0]
			constraintName = buildPostgresConstraintName(tableName, columnName, "fkey")
		}

		if len(constraintName) == 0 && g.mode != GeneratorModeSQLite3 {
			return ddls, fmt.Errorf(
				"Foreign key without constraint symbol was found in table '%s' (index name: '%s', columns: %v). "+
					"Specify the constraint symbol to identify the foreign key.",
				desired.table.name, desiredForeignKey.indexName, desiredForeignKey.indexColumns,
			)
		}

		// Create a modified ForeignKey with the generated constraint name if needed
		fkWithName := desiredForeignKey
		if desiredForeignKey.constraintName == "" && constraintName != "" {
			fkWithName = ForeignKey{
				constraintName:    constraintName,
				indexName:         desiredForeignKey.indexName,
				indexColumns:      desiredForeignKey.indexColumns,
				referenceName:     desiredForeignKey.referenceName,
				referenceColumns:  desiredForeignKey.referenceColumns,
				onDelete:          desiredForeignKey.onDelete,
				onUpdate:          desiredForeignKey.onUpdate,
				notForReplication: desiredForeignKey.notForReplication,
				constraintOptions: desiredForeignKey.constraintOptions,
			}
		}

		if currentForeignKey := findForeignKeyByName(currentTable.foreignKeys, constraintName); currentForeignKey != nil {
			// Drop and add foreign key as needed.
			if !g.areSameForeignKeys(*currentForeignKey, fkWithName) {
				var dropDDL string
				switch g.mode {
				case GeneratorModeMysql:
					dropDDL = fmt.Sprintf("ALTER TABLE %s DROP FOREIGN KEY %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentForeignKey.constraintName))
				case GeneratorModePostgres, GeneratorModeMssql:
					dropDDL = fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentForeignKey.constraintName))
				default:
				}
				if dropDDL != "" {
					ddls = append(ddls, dropDDL, fmt.Sprintf("ALTER TABLE %s ADD %s%s", g.escapeTableName(desired.table.name), g.generateForeignKeyDefinition(fkWithName), g.generateConstraintOptions(fkWithName.constraintOptions)))
				}
			}
		} else {
			// Foreign key not found, add foreign key.
			// But first check if we've already handled this FK during primary key changes
			fkKey := desired.table.name + ":" + constraintName
			if !g.handledForeignKeys[fkKey] {
				definition := g.generateForeignKeyDefinition(fkWithName)
				ddl := fmt.Sprintf("ALTER TABLE %s ADD %s", g.escapeTableName(desired.table.name), definition)
				ddls = append(ddls, ddl)
			}
		}
	}

	// Examine each exclusion
	for _, desiredExclusion := range desired.table.exclusions {
		if len(desiredExclusion.constraintName) == 0 && g.mode != GeneratorModeSQLite3 {
			return ddls, fmt.Errorf(
				"Exclusion without constraint symbol was found in table '%s'. "+
					"Specify the constraint symbol to identify the exclusion.",
				desired.table.name,
			)
		}

		if currentExclusion := findExclusionByName(currentTable.exclusions, desiredExclusion.constraintName); currentExclusion != nil {
			// Drop and add exclusion as needed.
			if !g.areSameExclusions(*currentExclusion, desiredExclusion) {
				dropDDL := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentExclusion.constraintName))
				if dropDDL != "" {
					ddls = append(ddls, dropDDL, fmt.Sprintf("ALTER TABLE %s ADD %s", g.escapeTableName(desired.table.name), g.generateExclusionDefinition(desiredExclusion)))
				}
			}
		} else {
			// Exclusion not found, add exclusion.
			definition := g.generateExclusionDefinition(desiredExclusion)
			ddl := fmt.Sprintf("ALTER TABLE %s ADD %s", g.escapeTableName(desired.table.name), definition)
			ddls = append(ddls, ddl)
		}
	}

	// Examine each check
	for _, desiredCheck := range desired.table.checks {
		// First try to find by name
		currentCheck := findCheckConstraintInTable(&currentTable, desiredCheck.constraintName)

		// For MySQL and MSSQL, also try to find by definition if not found by name
		// This handles auto-generated constraint names
		if currentCheck == nil && (g.mode == GeneratorModeMysql || g.mode == GeneratorModeMssql) {
			currentCheck = g.findCheckConstraintByDefinition(&currentTable, &desiredCheck)
		}

		if currentCheck != nil {
			if !g.areSameCheckDefinition(currentCheck, &desiredCheck) {
				// Constraint exists but has different definition, need to replace it
				switch g.mode {
				case GeneratorModePostgres, GeneratorModeMssql:
					ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentCheck.constraintName)))
					ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)", g.escapeTableName(desired.table.name), g.escapeSQLName(desiredCheck.constraintName), g.normalizeCheckExprString(desiredCheck.definition)))
				case GeneratorModeMysql:
					ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CHECK %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentCheck.constraintName)))
					ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)", g.escapeTableName(desired.table.name), g.escapeSQLName(desiredCheck.constraintName), g.normalizeCheckExprString(desiredCheck.definition)))
				case GeneratorModeSQLite3:
					// SQLite does not support ALTER TABLE for CHECK constraints
					// Modifying CHECK constraints requires recreating the table, which is not supported
				}
			} else if currentCheck.constraintName != desiredCheck.constraintName {
				// Constraint exists with same definition but different name
				// Don't generate DDL for renaming - constraint names don't matter if the definition is the same
				// This handles cases where MSSQL auto-generates names like CK__table__column__hash
				// and sqldef auto-generates names like table_column_check
			}
		} else {
			// Constraint doesn't exist, add it
			ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)", g.escapeTableName(desired.table.name), g.escapeSQLName(desiredCheck.constraintName), g.normalizeCheckExprString(desiredCheck.definition)))
		}
	}

	// Examine table comment
	if currentTable.options["comment"] != desired.table.options["comment"] {
		if desired.table.options["comment"] == "" {
			ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s COMMENT = ''", g.escapeTableName(desired.table.name)))
		} else {
			ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s COMMENT = %s", g.escapeTableName(desired.table.name), desired.table.options["comment"]))
		}
	}

	// Add FK recreation DDLs at the end (they will be executed after all table modifications)
	if len(fkRecreationDDLs) > 0 {
		ddls = append(ddls, fkRecreationDDLs...)
	}

	return ddls, nil
}

// Shared by `CREATE INDEX` and `ALTER TABLE ADD INDEX`.
// This manages `g.currentTables` unlike `generateDDLsForCreateTable`...
func (g *Generator) generateDDLsForCreateIndex(tableName string, desiredIndex Index, action string, statement string) ([]string, error) {
	// Add CONCURRENTLY to CREATE [UNIQUE] INDEX statements (PostgreSQL only)
	// This can come from either:
	// 1. The config flag (g.config.CreateIndexConcurrently)
	// 2. The parsed DDL (desiredIndex.concurrently)
	if g.mode == GeneratorModePostgres && action == "CREATE INDEX" && (g.config.CreateIndexConcurrently || desiredIndex.concurrently) {
		re := regexp.MustCompile(`(?i)^(CREATE\s+(?:UNIQUE\s+)?INDEX)(?:\s+CONCURRENTLY)?(\s+.*)`)
		statement = re.ReplaceAllString(statement, "${1} CONCURRENTLY${2}")
	}

	// Add ASYNC to CREATE [UNIQUE] INDEX statements if the index has the async flag (Aurora DSQL)
	if g.mode == GeneratorModePostgres && action == "CREATE INDEX" && desiredIndex.async {
		re := regexp.MustCompile(`(?i)^(CREATE\s+(?:UNIQUE\s+)?INDEX)(?:\s+ASYNC)?(\s+.*)`)
		statement = re.ReplaceAllString(statement, "${1} ASYNC${2}")
	}

	ddls := []string{}

	currentTable := findTableByName(g.currentTables, tableName)
	if currentTable == nil { // Views or non-existent tables
		currentView := findViewByName(g.currentViews, tableName)
		if currentView != nil {
			currentIndex := findIndexByName(currentView.indexes, desiredIndex.name)
			if currentIndex == nil {
				// Index not found, add index.
				ddls = append(ddls, statement)
				currentView.indexes = append(currentView.indexes, desiredIndex)
			}
		} else {
			// Check if the view exists in desired views (might be created in the same migration)
			desiredView := findViewByName(g.desiredViews, tableName)
			if desiredView != nil {
				// View will be created, add the index
				ddls = append(ddls, statement)
				desiredView.indexes = append(desiredView.indexes, desiredIndex)
			} else {
				// Check if it's a desired table that hasn't been created yet
				desiredTable := findTableByName(g.desiredTables, tableName)
				if desiredTable != nil {
					// Table will be created, add the index
					ddls = append(ddls, statement)
					desiredTable.indexes = append(desiredTable.indexes, desiredIndex)
				} else {
					// Creating index on non-existent table/view, just add the statement
					ddls = append(ddls, statement)
				}
			}
		}
		return ddls, nil
	}

	currentIndex := findIndexByName(currentTable.indexes, desiredIndex.name)
	if currentIndex == nil {
		// Check if this is a renamed index
		var renameFromIndex *Index
		if desiredIndex.renamedFrom != "" {
			renameFromIndex = findIndexByName(currentTable.indexes, desiredIndex.renamedFrom)
		}

		if renameFromIndex != nil {
			// Generate RENAME INDEX DDL
			renameDDLs := g.generateRenameIndex(tableName, renameFromIndex.name, desiredIndex.name, &desiredIndex)
			ddls = append(ddls, renameDDLs...)

			// Update the current table's indexes to reflect the rename
			newIndexes := []Index{}
			for _, idx := range currentTable.indexes {
				if idx.name == renameFromIndex.name {
					// Replace with the renamed index
					newIndexes = append(newIndexes, desiredIndex)
				} else {
					newIndexes = append(newIndexes, idx)
				}
			}
			currentTable.indexes = newIndexes
		} else {
			// Index not found and not a rename, add index.
			ddls = append(ddls, statement)
			currentTable.indexes = append(currentTable.indexes, desiredIndex)
		}
	} else {
		// Index found. If it's different, drop and add index.
		if !g.areSameIndexes(*currentIndex, desiredIndex) {
			ddls = append(ddls, g.generateDropIndex(currentTable.name, currentIndex.name, currentIndex.constraint))
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

	desiredTable := findTableByName(g.desiredTables, tableName)
	if desiredTable != nil {
		desiredTable.indexes = append(desiredTable.indexes, desiredIndex)
	}

	return ddls, nil
}

func (g *Generator) generateDDLsForAddForeignKey(tableName string, desiredForeignKey ForeignKey, action string, statement string) ([]string, error) {
	var ddls []string

	currentTable := findTableByName(g.currentTables, tableName)
	currentForeignKey := findForeignKeyByName(currentTable.foreignKeys, desiredForeignKey.constraintName)
	if currentForeignKey == nil {
		// Foreign Key not found, add foreign key
		ddls = append(ddls, statement)
		currentTable.foreignKeys = append(currentTable.foreignKeys, desiredForeignKey)
	} else {
		// Foreign key found, If it's different, drop and add or alter foreign key.
		if !g.areSameForeignKeys(*currentForeignKey, desiredForeignKey) {
			ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(currentTable.name), g.escapeSQLName(currentForeignKey.constraintName)))
			ddls = append(ddls, statement)
		}
	}

	// Examine indexes in desiredTable to delete obsoleted indexes later
	desiredTable := findTableByName(g.desiredTables, tableName)
	// Only add to desiredTable.foreignKeys if it doesn't already exist (it may have been pre-populated from aggregation)
	if !slices.Contains(convertForeignKeysToConstraintNames(desiredTable.foreignKeys), desiredForeignKey.constraintName) {
		desiredTable.foreignKeys = append(desiredTable.foreignKeys, desiredForeignKey)
	}

	return ddls, nil
}

func (g *Generator) generateDDLsForAddExclusion(tableName string, desiredExclusion Exclusion, action string, statement string) ([]string, error) {
	var ddls []string

	currentTable := findTableByName(g.currentTables, tableName)
	currentExclusion := findExclusionByName(currentTable.exclusions, desiredExclusion.constraintName)
	if currentExclusion == nil {
		// Exclusion not found, add exclusion
		ddls = append(ddls, statement)
		currentTable.exclusions = append(currentTable.exclusions, desiredExclusion)
	} else {
		// Exclusion key found, If it's different, drop and add or alter exclusion.
		if !g.areSameExclusions(*currentExclusion, desiredExclusion) {
			ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(currentTable.name), g.escapeSQLName(currentExclusion.constraintName)))
			ddls = append(ddls, statement)
		}
	}

	// Examine indexes in desiredTable to delete obsoleted indexes later
	desiredTable := findTableByName(g.desiredTables, tableName)
	// Only add to desiredTable.exclusions if it doesn't already exist (it may have been pre-populated from aggregation)
	if !slices.Contains(convertExclusionToConstraintNames(desiredTable.exclusions), desiredExclusion.constraintName) {
		desiredTable.exclusions = append(desiredTable.exclusions, desiredExclusion)
	}

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
	// Only add to desiredTable.policies if it doesn't already exist (it may have been pre-populated from aggregation)
	if !slices.Contains(convertPolicyNames(desiredTable.policies), desiredPolicy.name) {
		desiredTable.policies = append(desiredTable.policies, desiredPolicy)
	}

	return ddls, nil
}

func (g *Generator) shouldDropAndCreateView(currentView *View, desiredView *View) bool {
	if g.mode == GeneratorModeSQLite3 || g.mode == GeneratorModeMssql {
		return true
	}

	// In the case of PostgreSQL, if there are any deletions or changes to columns,
	// you cannot use REPLACE VIEW, so you need to DROP and CREATE VIEW.
	//
	// ref: https://www.postgresql.org/docs/current/sql-createview.html
	//
	// > CREATE OR REPLACE VIEW is similar, but if a view of the same name already exists, it is replaced.
	// > The new query must generate the same columns that were generated by the existing view query
	// > (that is, the same column names in the same order and with the same data types), but it may add additional
	// > columns to the end of the list. The calculations giving rise to the output columns may be completely different.
	if g.mode == GeneratorModePostgres {
		currentNormalized := normalizeViewColumnsFromDefinition(currentView.definition, g.mode)
		desiredNormalized := normalizeViewColumnsFromDefinition(desiredView.definition, g.mode)

		// If we couldn't extract columns from the definitions, fall back to DROP and CREATE
		if currentNormalized == nil || desiredNormalized == nil {
			return true
		}

		// If columns are removed, we need to DROP and CREATE.
		if len(currentNormalized) > len(desiredNormalized) {
			return true
		}

		// If all existing columns are identical and only a new column is added, use REPLACE; otherwise, execute DROP and CREATE.
		return !reflect.DeepEqual(currentNormalized, desiredNormalized[:len(currentNormalized)])
	}

	return false
}

func (g *Generator) generateDDLsForCreateView(viewName string, desiredView *View) ([]string, error) {
	var ddls []string

	currentView := findViewByName(g.currentViews, viewName)
	if currentView == nil {
		// View not found, add view.
		ddls = append(ddls, desiredView.statement)
		view := *desiredView // copy view
		// Don't copy indexes from desired to current - they'll be added when the CREATE INDEX is processed
		view.indexes = []Index{}
		g.currentViews = append(g.currentViews, &view)
	} else if desiredView.viewType == "VIEW" { // TODO: Fix the definition comparison for materialized views and enable this
		// View found. If it's different, create or replace view.
		// Use AST-based comparison
		if currentView.definition == nil || desiredView.definition == nil {
			panic(fmt.Sprintf("View.definition must not be nil (currentView.definition=%v, desiredView.definition=%v)", currentView.definition, desiredView.definition))
		}

		currentNormalizedAST := normalizeViewDefinition(currentView.definition, g.mode)
		desiredNormalizedAST := normalizeViewDefinition(desiredView.definition, g.mode)
		currentNormalized := strings.ToLower(parser.String(currentNormalizedAST))
		desiredNormalized := strings.ToLower(parser.String(desiredNormalizedAST))

		// Post-normalization fix for generic parser: strip remaining table qualifiers
		// The generic parser's ColName.String() may not respect the empty qualifier we set
		if g.mode == GeneratorModePostgres {
			currentNormalized = stripTableQualifiers(currentNormalized)
			desiredNormalized = stripTableQualifiers(desiredNormalized)
		}
		slog.Debug("Comparing view definitions",
			"current_before_norm", parser.String(currentView.definition),
			"desired_before_norm", parser.String(desiredView.definition),
			"current_after_norm", currentNormalized,
			"desired_after_norm", desiredNormalized,
		)

		if currentNormalized != desiredNormalized {
			viewDefinition := parser.String(desiredView.definition)
			if g.shouldDropAndCreateView(currentView, desiredView) {
				ddls = append(ddls, fmt.Sprintf("DROP %s %s", desiredView.viewType, g.escapeTableName(viewName)))
				ddls = append(ddls, fmt.Sprintf("CREATE %s %s AS %s", desiredView.viewType, g.escapeTableName(viewName), viewDefinition))
			} else {
				ddls = append(ddls, fmt.Sprintf("CREATE OR REPLACE %s %s AS %s", desiredView.viewType, g.escapeTableName(viewName), viewDefinition))
			}
		}
	} else if desiredView.viewType == "SQL SECURITY" {
		// VIEW with the specified security type found. If it's different, create or replace view.
		if !strings.EqualFold(currentView.securityType, desiredView.securityType) {
			viewDefinition := parser.String(desiredView.definition)
			ddls = append(ddls, fmt.Sprintf("CREATE OR REPLACE SQL SECURITY %s VIEW %s AS %s", desiredView.securityType, g.escapeTableName(viewName), viewDefinition))
		}
	}

	// Examine policies in desiredTable to delete obsoleted policies later
	// Only add to desiredViews if it doesn't already exist (it may have been pre-populated from aggregation)
	if !slices.Contains(convertViewNames(g.desiredViews), desiredView.name) {
		g.desiredViews = append(g.desiredViews, desiredView)
	}

	return ddls, nil
}

// stripTableQualifiers removes table qualifiers from column references in SQL
// E.g., "users.name" -> "name", "t.id" -> "id"
// This is needed for PostgreSQL 13-15 where table qualifiers are included in column references.
func stripTableQualifiers(sql string) string {
	// Match table.column patterns where:
	// - table name is [a-z_][a-z0-9_]* (identifier)
	// - followed by a dot
	// - followed by column name [a-z_][a-z0-9_]* (identifier)
	// We use word boundaries to avoid matching within quoted strings
	re := regexp.MustCompile(`\b[a-z_][a-z0-9_]*\.([a-z_][a-z0-9_]*)\b`)
	// Replace "table.column" with just "column" (keeping capture group 1)
	return re.ReplaceAllString(sql, "$1")
}

func (g *Generator) generateDDLsForCreateTrigger(triggerName string, desiredTrigger *Trigger) ([]string, error) {
	var ddls []string
	currentTrigger := findTriggerByName(g.currentTriggers, triggerName)

	var triggerDefinition string
	switch g.mode {
	case GeneratorModeMssql:
		triggerDefinition += fmt.Sprintf("TRIGGER %s ON %s %s %s AS\n%s", g.escapeTableNameSimple(desiredTrigger.name, true), g.escapeTableName(desiredTrigger.tableName), desiredTrigger.time, strings.Join(desiredTrigger.event, ", "), strings.Join(desiredTrigger.body, "\n"))
	case GeneratorModeMysql:
		triggerDefinition += fmt.Sprintf("TRIGGER %s %s %s ON %s FOR EACH ROW %s", g.escapeSQLName(desiredTrigger.name), desiredTrigger.time, strings.Join(desiredTrigger.event, ", "), g.escapeTableName(desiredTrigger.tableName), strings.Join(desiredTrigger.body, "\n"))
	case GeneratorModeSQLite3:
		triggerDefinition = desiredTrigger.statement
	default:
		return ddls, nil
	}

	if currentTrigger == nil {
		// Trigger not found, add trigger.
		var createPrefix string
		if g.mode != GeneratorModeSQLite3 {
			createPrefix = "CREATE "
		}
		ddls = append(ddls, createPrefix+triggerDefinition)
	} else {
		// Trigger found. If it's different, create or replace trigger.
		if !areSameTriggerDefinition(currentTrigger, desiredTrigger) {
			if g.mode != GeneratorModeMssql {
				ddls = append(ddls, fmt.Sprintf("DROP TRIGGER %s", g.escapeSQLName(triggerName)))
			}
			var createPrefix string
			if g.mode == GeneratorModeMssql {
				createPrefix = "CREATE OR ALTER "
			} else if g.mode != GeneratorModeSQLite3 {
				createPrefix = "CREATE "
			}
			ddls = append(ddls, createPrefix+triggerDefinition)
		}
	}

	// Only add to desiredTriggers if it doesn't already exist (it may have been pre-populated from aggregation)
	if findTriggerByName(g.desiredTriggers, desiredTrigger.name) == nil {
		g.desiredTriggers = append(g.desiredTriggers, desiredTrigger)
	}

	return ddls, nil
}

func (g *Generator) generateDDLsForCreateType(desired *Type) ([]string, error) {
	ddls := []string{}

	if currentType := findTypeByName(g.currentTypes, desired.name); currentType != nil {
		// Type found. Add values if not present.
		if currentType.enumValues != nil && len(currentType.enumValues) < len(desired.enumValues) {
			for _, enumValue := range desired.enumValues {
				if !slices.Contains(currentType.enumValues, enumValue) {
					ddl := fmt.Sprintf("ALTER TYPE %s ADD VALUE %s", currentType.name, enumValue)
					ddls = append(ddls, ddl)
				}
			}
		}
	} else {
		// Type not found, add type.
		ddls = append(ddls, desired.statement)
	}
	// Only add to desiredTypes if it doesn't already exist (it may have been pre-populated from aggregation)
	if findTypeByName(g.desiredTypes, desired.name) == nil {
		g.desiredTypes = append(g.desiredTypes, desired)
	}

	return ddls, nil
}

func (g *Generator) generateDDLsForComment(desired *Comment) ([]string, error) {
	ddls := []string{}

	currentComment := findCommentByObject(g.currentComments, desired.comment.Object)

	// If both current and desired comments are NULL/empty, no change is needed.
	if currentComment == nil && desired.comment.Comment == "" {
		return ddls, nil
	}

	if currentComment == nil || currentComment.comment.Comment != desired.comment.Comment {
		// Comment not found or different, add comment.
		ddls = append(ddls, desired.statement)
	}

	return ddls, nil
}

func (g *Generator) generateDDLsForExtension(desired *Extension) ([]string, error) {
	ddls := []string{}

	if currentExtension := findExtensionByName(g.currentExtensions, desired.extension.Name); currentExtension == nil {
		// Extension not found, add extension.
		ddls = append(ddls, desired.statement)
		extension := *desired // copy extension
		g.currentExtensions = append(g.currentExtensions, &extension)
	}

	// Only add to desiredExtensions if it doesn't already exist (it may have been pre-populated from aggregation)
	if findExtensionByName(g.desiredExtensions, desired.extension.Name) == nil {
		g.desiredExtensions = append(g.desiredExtensions, desired)
	}

	return ddls, nil
}

func (g *Generator) generateDDLsForSchema(desired *Schema) ([]string, error) {
	ddls := []string{}

	if currentSchema := findSchemaByName(g.currentSchemas, desired.schema.Name); currentSchema == nil {
		// Schema not found, add schema.
		ddls = append(ddls, desired.statement)
		schema := *desired // copy schema
		g.currentSchemas = append(g.currentSchemas, &schema)
	}

	// Only add to desiredSchemas if it doesn't already exist (it may have been pre-populated from aggregation)
	if findSchemaByName(g.desiredSchemas, desired.schema.Name) == nil {
		g.desiredSchemas = append(g.desiredSchemas, desired)
	}

	return ddls, nil
}

// Even though simulated table doesn't have a foreign key, references could exist in column definitions.
// This carefully generates DROP CONSTRAINT for such situations.
func (g *Generator) generateDDLsForAbsentForeignKey(currentForeignKey ForeignKey, currentTable Table, desiredTable Table) []string {
	ddls := []string{}

	switch g.mode {
	case GeneratorModeMysql:
		ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP FOREIGN KEY %s", g.escapeTableName(currentTable.name), g.escapeSQLName(currentForeignKey.constraintName)))
	case GeneratorModePostgres, GeneratorModeMssql:
		var referencesColumn *Column
		for _, column := range desiredTable.columns {
			if column.references == currentForeignKey.referenceName {
				referencesColumn = column
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
				primaryKeyColumn = column
				break
			}
		}

		if primaryKeyColumn == nil {
			// If nil, it will be `DROP COLUMN`-ed and we can usually ignore it.
			// However, it seems like you need to explicitly drop it first for MSSQL.
			if g.mode == GeneratorModeMssql && (primaryKeyColumn == nil || primaryKeyColumn.name != currentIndex.columns[0].ColumnName()) {
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(currentTable.name), g.escapeSQLName(currentIndex.name)))
			}
		} else if primaryKeyColumn.name != currentIndex.columns[0].ColumnName() { // TODO: check length of currentIndex.columns
			// TODO: handle this. Rename primary key column...?
			return ddls, fmt.Errorf(
				"primary key column name of '%s' should be '%s' but currently '%s'. This is not handled yet.",
				currentTable.name, primaryKeyColumn.name, currentIndex.columns[0].ColumnName(),
			)
		}
	} else if currentIndex.unique {
		var uniqueKeyColumn *Column
		// Columns become empty if the index is a PostgreSQL's expression index.
		if len(currentIndex.columns) > 0 {
			for _, column := range desiredTable.columns {
				if column.name == currentIndex.columns[0].ColumnName() && column.keyOption.isUnique() {
					uniqueKeyColumn = column
					break
				}
			}
		}

		if uniqueKeyColumn == nil {
			// No unique column. Drop unique key index.
			ddls = append(ddls, g.generateDropIndex(currentTable.name, currentIndex.name, currentIndex.constraint))
		}
	} else {
		ddls = append(ddls, g.generateDropIndex(currentTable.name, currentIndex.name, currentIndex.constraint))
	}

	return ddls, nil
}

func (g *Generator) generateDataType(column Column) string {
	suffix := ""
	if column.timezone {
		suffix += " WITH TIME ZONE"
	}
	if column.array {
		suffix += "[]"
	}

	// Determine the full type name including schema qualification
	typeName := column.typeName

	// Normalize PostgreSQL shortcuts to their canonical forms for output
	// Note: We DON'T normalize general aliases like varchar->character varying or numeric->decimal
	// Those are preserved as-is in the output. We only normalize PostgreSQL-specific shortcuts.
	if g.mode == GeneratorModePostgres {
		switch typeName {
		case "int":
			typeName = "integer"
		}
		// Note: timestamptz and timetz are already normalized by the parsers
		// (they set the timezone flag and convert the type name to timestamp/time)
	}

	// Only qualify type names with schema for PostgreSQL when:
	// 1. references is not empty (including "public." for enum types)
	// 2. the type name doesn't already contain a dot
	// 3. it's not a built-in type (built-in types shouldn't have references set to non-empty schema)
	if g.mode == GeneratorModePostgres && column.references != "" && !strings.Contains(typeName, ".") {
		typeName = column.references + typeName
	}

	if column.displayWidth != nil {
		return fmt.Sprintf("%s(%s)%s", typeName, column.displayWidth.raw, suffix)
	} else if column.length != nil {
		if column.scale != nil {
			return fmt.Sprintf("%s(%s, %s)%s", typeName, column.length.raw, column.scale.raw, suffix)
		} else {
			return fmt.Sprintf("%s(%s)%s", typeName, column.length.raw, suffix)
		}
	} else {
		switch column.typeName {
		case "enum", "set":
			return fmt.Sprintf("%s(%s)%s", column.typeName, strings.Join(column.enumValues, ", "), suffix)
		default:
			return fmt.Sprintf("%s%s", typeName, suffix)
		}
	}
}

func (g *Generator) generateColumnDefinition(column Column, enableUnique bool) (string, error) {
	// TODO: make string concatenation faster?

	definition := fmt.Sprintf("%s %s ", g.escapeSQLName(column.name), g.generateDataType(column))

	if column.unsigned {
		definition += "UNSIGNED "
	}

	// [CHARACTER SET] and [COLLATE] should be placed before [NOT NULL | NULL] on MySQL
	if column.charset != "" {
		definition += fmt.Sprintf("CHARACTER SET %s ", column.charset)
	}
	if column.collate != "" {
		definition += fmt.Sprintf("COLLATE %s ", column.collate)
	}

	if column.generated == nil {
		if column.identity == nil && ((column.notNull != nil && *column.notNull) || column.keyOption == ColumnKeyPrimary) {
			definition += "NOT NULL "
		} else if column.notNull != nil && !*column.notNull {
			definition += "NULL "
		}
	}

	if column.sridDef != nil && column.sridDef.value != nil {
		def, err := generateSridDefinition(*column.sridDef.value)
		if err != nil {
			return "", fmt.Errorf("%s in column: %#v", err.Error(), column)
		}
		definition += def + " "
	}

	if column.defaultDef != nil {
		def, err := g.generateDefaultDefinition(*column.defaultDef)
		if err != nil {
			return "", fmt.Errorf("%s in column: %#v", err.Error(), column)
		}
		definition += def + " "
	}

	if column.generated != nil {
		// Generated column definitions have this syntax on MySQL
		// col_name data_type [GENERATED ALWAYS] AS (expr)
		//  [VIRTUAL | STORED] [NOT NULL | NULL]
		//  [UNIQUE [KEY]] [[PRIMARY] KEY]
		//  [COMMENT 'string']
		if column.autoIncrement {
			return "", fmt.Errorf("%s in column: %#v", "The AUTO_INCREMENT attribute cannot be used in a generated column definition.", column)
		}
		definition += "GENERATED ALWAYS AS (" + column.generated.expr + ") "
		switch column.generated.generatedType {
		case GeneratedTypeVirtual:
			definition += "VIRTUAL "
		case GeneratedTypeStored:
			definition += "STORED "
		}

		if column.identity == nil && ((column.notNull != nil && *column.notNull) || column.keyOption == ColumnKeyPrimary) {
			definition += "NOT NULL "
		} else if column.notNull != nil && !*column.notNull {
			definition += "NULL "
		}
	}

	if column.autoIncrement {
		if column.generated != nil {
			return "", fmt.Errorf("%s in column: %#v", "The AUTO_INCREMENT attribute cannot be used in a generated column definition.", column)
		}
		definition += "AUTO_INCREMENT "
	}

	if column.onUpdate != nil {
		definition += fmt.Sprintf("ON UPDATE %s ", column.onUpdate.raw)
	}

	if column.comment != nil {
		// TODO: Should this use StringConstant?
		definition += fmt.Sprintf("COMMENT '%s' ", column.comment.raw)
	}

	if column.check != nil {
		definition += "CHECK "
		if column.check.notForReplication {
			definition += "NOT FOR REPLICATION "
		}
		// Normalize CHECK expression to match PostgreSQL's output
		// This ensures typed literals are properly converted (e.g., time '...' -> '...'::time)
		definition += fmt.Sprintf("(%s) ", g.normalizeCheckExprString(column.check.definition))
		if column.check.noInherit {
			definition += "NO INHERIT "
		}
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

	if column.identity != nil && column.identity.behavior != "" {
		definition += "GENERATED " + column.identity.behavior + " AS IDENTITY "
		if column.sequence != nil {
			definition += "(" + generateSequenceClause(column.sequence) + ") "
		}
	} else if g.mode == GeneratorModeMssql && column.sequence != nil {
		definition += fmt.Sprintf("IDENTITY(%d,%d)", *column.sequence.StartWith, *column.sequence.IncrementBy)
		if column.identity.notForReplication {
			definition += " NOT FOR REPLICATION"
		}
	}

	definition = strings.TrimSuffix(definition, " ")
	return definition, nil
}

type AggregatedSchema struct {
	Tables     []*Table
	Views      []*View
	Triggers   []*Trigger
	Types      []*Type
	Comments   []*Comment
	Extensions []*Extension
	Schemas    []*Schema
	Privileges []*GrantPrivilege
}

func (g *Generator) generateAddIndex(table string, index Index) string {
	var uniqueOption string
	var clusteredOption string
	if index.unique {
		uniqueOption = " UNIQUE"
	}
	if index.clustered {
		clusteredOption = " CLUSTERED"
	} else {
		clusteredOption = " NONCLUSTERED"
	}

	columns := []string{}
	for _, indexColumn := range index.columns {
		column := g.escapeSQLName(parser.String(indexColumn.columnExpr))
		if indexColumn.length != nil {
			column += fmt.Sprintf("(%d)", *indexColumn.length)
		}
		if indexColumn.direction == DescScr {
			column += fmt.Sprintf(" %s", indexColumn.direction)
		}
		columns = append(columns, column)
	}

	optionDefinition := g.generateIndexOptionDefinition(index.options)

	switch g.mode {
	case GeneratorModeMssql:
		var ddl string
		var partition string
		if !index.primary {
			ddl = fmt.Sprintf(
				"CREATE%s%s INDEX %s ON %s",
				uniqueOption,
				clusteredOption,
				g.escapeSQLName(index.name),
				g.escapeTableName(table),
			)

			// definition of partition is valid only in the syntax `CREATE INDEX ...`
			if index.partition.partitionName != "" {
				partition += fmt.Sprintf(" ON %s", g.escapeSQLName(index.partition.partitionName))
				if index.partition.column != "" {
					partition += fmt.Sprintf(" (%s)", g.escapeSQLName(index.partition.column))
				}
			}
		} else {
			ddl = fmt.Sprintf("ALTER TABLE %s ADD", g.escapeTableName(table))

			if index.name != "PRIMARY" {
				ddl += fmt.Sprintf(" CONSTRAINT %s", g.escapeSQLName(index.name))
			}

			ddl += fmt.Sprintf(" %s%s", strings.ToUpper(index.indexType), clusteredOption)
		}
		ddl += fmt.Sprintf(" (%s)%s", strings.Join(columns, ", "), optionDefinition)
		ddl += partition
		return ddl
	case GeneratorModePostgres:
		ddl := fmt.Sprintf(
			"ALTER TABLE %s ADD ",
			g.escapeTableName(table),
		)
		if strings.EqualFold(index.indexType, "PRIMARY KEY") && index.primary &&
			(index.name != "" && index.name != "PRIMARY" && index.name != index.columns[0].ColumnName()) {
			ddl += fmt.Sprintf("CONSTRAINT %s ", g.escapeSQLName(index.name))
		}
		isUniqueConstraint := strings.EqualFold(index.indexType, "UNIQUE")
		if isUniqueConstraint {
			ddl += "CONSTRAINT"
		} else {
			ddl += strings.ToUpper(index.indexType)
		}
		if !index.primary {
			constraintName := index.name
			if isUniqueConstraint && len(index.columns) == 1 {
				columnName := index.columns[0].ColumnName()

				// If the current name is just the column name (common with generic parser),
				// replace it with the PostgreSQL convention
				if constraintName == columnName {
					expectedName := buildPostgresConstraintName(table, columnName, "key")
					slog.Debug("Auto-generating PostgreSQL UNIQUE constraint name",
						"table", table,
						"column", columnName,
						"original_name", constraintName,
						"generated_name", expectedName)
					constraintName = expectedName
				} else {
					slog.Debug("Using existing UNIQUE constraint name",
						"table", table,
						"column", columnName,
						"constraint_name", constraintName)
				}
			}
			ddl += fmt.Sprintf(" %s", g.escapeSQLName(constraintName))
		}
		if isUniqueConstraint {
			ddl += " UNIQUE"
		}
		constraintOptions := g.generateConstraintOptions(index.constraintOptions)
		ddl += fmt.Sprintf(" (%s)%s%s", strings.Join(columns, ", "), optionDefinition, constraintOptions)
		return ddl
	default:
		// Construct index type with optional VECTOR keyword for MariaDB vector indexes
		indexTypeStr := strings.ToUpper(index.indexType)
		if index.vector {
			indexTypeStr = "VECTOR INDEX"
		}

		ddl := fmt.Sprintf(
			"ALTER TABLE %s ADD %s",
			g.escapeTableName(table),
			indexTypeStr,
		)

		if !index.primary {
			ddl += fmt.Sprintf(" %s", g.escapeSQLName(index.name))
		}
		constraintOptions := g.generateConstraintOptions(index.constraintOptions)
		ddl += fmt.Sprintf(" (%s)%s%s", strings.Join(columns, ", "), optionDefinition, constraintOptions)
		return ddl
	}
}

func (g *Generator) generateIndexOptionDefinition(indexOptions []IndexOption) string {
	var optionDefinition string
	if len(indexOptions) > 0 {
		switch g.mode {
		case GeneratorModeMysql:
			// Handle multiple vector index options (M and DISTANCE)
			if len(indexOptions) > 1 {
				var mOption, distanceOption string
				for _, indexOption := range indexOptions {
					if strings.ToUpper(indexOption.optionName) == "M" {
						mOption = fmt.Sprintf("M=%s", indexOption.value.raw)
					} else if strings.ToUpper(indexOption.optionName) == "DISTANCE" {
						distanceOption = fmt.Sprintf("DISTANCE=%s", indexOption.value.raw)
					}
				}
				if mOption != "" && distanceOption != "" {
					optionDefinition = fmt.Sprintf(" %s %s", mOption, distanceOption)
				} else if mOption != "" {
					optionDefinition = fmt.Sprintf(" %s", mOption)
				} else if distanceOption != "" {
					optionDefinition = fmt.Sprintf(" %s", distanceOption)
				}
			} else {
				indexOption := indexOptions[0]
				if indexOption.optionName == "parser" {
					indexOption.optionName = "WITH " + indexOption.optionName
				}
				if strings.EqualFold(indexOption.optionName, "M") {
					optionDefinition = fmt.Sprintf(" M=%s", indexOption.value.raw)
				} else if strings.EqualFold(indexOption.optionName, "DISTANCE") {
					optionDefinition = fmt.Sprintf(" DISTANCE=%s", indexOption.value.raw)
				} else if indexOption.optionName == "comment" {
					indexOption.optionName = "COMMENT"
					optionDefinition = fmt.Sprintf(" %s '%s'", indexOption.optionName, indexOption.value.raw)
				} else {
					optionDefinition = fmt.Sprintf(" %s %s", indexOption.optionName, indexOption.value.raw)
				}
			}
		case GeneratorModeMssql:
			options := []string{}
			for _, indexOption := range indexOptions {
				var optionValue string
				switch indexOption.value.valueType {
				case ValueTypeBool:
					if indexOption.value.raw == "true" {
						optionValue = "ON"
					} else {
						optionValue = "OFF"
					}
				default:
					optionValue = indexOption.value.raw
				}
				option := fmt.Sprintf("%s = %s", indexOption.optionName, optionValue)
				options = append(options, option)
			}
			optionDefinition = fmt.Sprintf(" WITH (%s)", strings.Join(options, ", "))
		}
	}
	return optionDefinition
}

func (g *Generator) generateConstraintOptions(ConstraintOptions *ConstraintOptions) string {
	if ConstraintOptions != nil && ConstraintOptions.deferrable {
		if ConstraintOptions.initiallyDeferred {
			return " DEFERRABLE INITIALLY DEFERRED"
		} else {
			return " DEFERRABLE INITIALLY IMMEDIATE"
		}
	}
	return ""
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
		strings.Join(indexColumns, ","), g.escapeTableName(foreignKey.referenceName),
		strings.Join(referenceColumns, ","),
	)

	if len(foreignKey.onDelete) > 0 {
		definition += fmt.Sprintf("ON DELETE %s ", foreignKey.onDelete)
	}
	if len(foreignKey.onUpdate) > 0 {
		definition += fmt.Sprintf("ON UPDATE %s ", foreignKey.onUpdate)
	}

	if foreignKey.notForReplication {
		definition += "NOT FOR REPLICATION "
	}

	return strings.TrimSuffix(definition, " ")
}

func (g *Generator) generateExclusionDefinition(exclusion Exclusion) string {
	var ex []string
	for _, exclusionPair := range exclusion.exclusions {
		ex = append(ex, fmt.Sprintf("%s WITH %s", exclusionPair.expression, exclusionPair.operator))
	}
	definition := fmt.Sprintf(
		"CONSTRAINT %s EXCLUDE USING %s (%s)",
		g.escapeSQLName(exclusion.constraintName),
		exclusion.indexType,
		strings.Join(ex, ", "),
	)
	if exclusion.where != "" {
		definition += fmt.Sprintf(" WHERE (%s)", exclusion.where)
	}
	return definition
}

func (g *Generator) generateRenameIndex(tableName string, oldIndexName string, newIndexName string, desiredIndex *Index) []string {
	ddls := []string{}

	switch g.mode {
	case GeneratorModeMysql:
		// MySQL uses ALTER TABLE ... RENAME INDEX
		ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s RENAME INDEX %s TO %s",
			g.escapeTableName(tableName),
			g.escapeSQLName(oldIndexName),
			g.escapeSQLName(newIndexName)))
	case GeneratorModePostgres:
		// PostgreSQL uses ALTER INDEX ... RENAME TO
		// Qualify the old index name with schema for consistency with DROP INDEX
		schema, _ := splitTableName(tableName, g.defaultSchema)
		ddls = append(ddls, fmt.Sprintf("ALTER INDEX %s.%s RENAME TO %s",
			g.escapeSQLName(schema),
			g.escapeSQLName(oldIndexName),
			g.escapeSQLName(newIndexName)))
	case GeneratorModeMssql:
		// SQL Server uses sp_rename
		// For sp_rename, we need to handle schema prefixes properly
		schema, tableNameOnly := splitTableName(tableName, g.defaultSchema)
		var tableRef string
		if schema != "" && schema != g.defaultSchema {
			// Only include schema if it's not the default
			tableRef = fmt.Sprintf("%s.%s", schema, tableNameOnly)
		} else {
			tableRef = tableNameOnly
		}
		ddls = append(ddls, fmt.Sprintf("EXEC sp_rename '%s.%s', '%s', 'INDEX'",
			tableRef,
			oldIndexName,
			newIndexName))
	case GeneratorModeSQLite3:
		// SQLite doesn't support renaming indexes directly
		// Need to drop and recreate
		if desiredIndex != nil {
			// Drop the old index
			ddls = append(ddls, g.generateDropIndex(tableName, oldIndexName, desiredIndex.constraint))

			// Generate a CREATE INDEX statement (SQLite doesn't support ALTER TABLE ADD INDEX)
			createStmt := "CREATE"
			if desiredIndex.unique {
				createStmt += " UNIQUE"
			}
			createStmt += fmt.Sprintf(" INDEX %s ON %s", g.escapeSQLName(desiredIndex.name), g.escapeTableName(tableName))

			// Add column specifications
			columnStrs := []string{}
			for _, column := range desiredIndex.columns {
				columnStrs = append(columnStrs, g.escapeSQLName(parser.String(column.columnExpr)))
			}
			createStmt += fmt.Sprintf(" (%s)", strings.Join(columnStrs, ", "))

			// Preserve WHERE clause if present
			if desiredIndex.where != "" {
				createStmt += fmt.Sprintf(" WHERE %s", desiredIndex.where)
			}

			ddls = append(ddls, createStmt)
		} else {
			// This should not happen in practice, but handle it gracefully
			ddls = append(ddls, fmt.Sprintf("-- Warning: Cannot rename index %s to %s in SQLite without index definition",
				oldIndexName, newIndexName))
		}
	}

	return ddls
}

func (g *Generator) generateDropIndex(tableName string, indexName string, constraint bool) string {
	switch g.mode {
	case GeneratorModeMysql:
		return fmt.Sprintf("ALTER TABLE %s DROP INDEX %s", g.escapeTableName(tableName), g.escapeSQLName(indexName))
	case GeneratorModePostgres:
		if constraint {
			return fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(tableName), g.escapeSQLName(indexName))
		} else {
			schema, _ := splitTableName(tableName, g.defaultSchema)
			return fmt.Sprintf("DROP INDEX %s.%s", g.escapeSQLName(schema), g.escapeSQLName(indexName))
		}
	case GeneratorModeMssql:
		if constraint {
			return fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(tableName), g.escapeSQLName(indexName))
		} else {
			return fmt.Sprintf("DROP INDEX %s ON %s", g.escapeSQLName(indexName), g.escapeTableName(tableName))
		}
	case GeneratorModeSQLite3:
		return fmt.Sprintf("DROP INDEX %s", g.escapeSQLName(indexName))
	default:
		return ""
	}
}

func (g *Generator) escapeTableName(name string) string {
	return g.escapeTableNameSimple(name, false)
}

func (g *Generator) escapeTableNameSimple(name string, withoutSchema bool) string {
	switch g.mode {
	case GeneratorModePostgres, GeneratorModeMssql:
		schemaTable := strings.SplitN(name, ".", 2)
		var schemaName, tableName string
		if len(schemaTable) == 1 {
			if withoutSchema {
				return g.escapeSQLName(name)
			}
			schemaName, tableName = g.defaultSchema, schemaTable[0]
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
		escaped := strings.ReplaceAll(name, "\"", "\"\"")
		return fmt.Sprintf("\"%s\"", escaped)
	case GeneratorModeMssql:
		escaped := strings.ReplaceAll(name, "]", "]]")
		return fmt.Sprintf("[%s]", escaped)
	default:
		escaped := strings.ReplaceAll(name, "`", "``")
		return fmt.Sprintf("`%s`", escaped)
	}
}

// escapeAndJoinColumns escapes a list of column names and joins them with commas
func (g *Generator) escapeAndJoinColumns(columns []string) string {
	escapedColumns := util.TransformSlice(columns, func(col string) string { return g.escapeSQLName(col) })
	return strings.Join(escapedColumns, ", ")
}

// validateAndEscapeGrantee validates and escapes a grantee name to prevent SQL injection
func (g *Generator) validateAndEscapeGrantee(grantee string) (string, error) {
	// PUBLIC is a special keyword and should not be quoted
	if grantee == "PUBLIC" {
		return "PUBLIC", nil
	}

	// Check for potentially dangerous characters that shouldn't be in role names
	// PostgreSQL role names can contain letters, digits, underscores, and some special chars
	// but we'll be conservative to prevent injection
	// Note: quotes, backticks, and brackets are allowed as escapeSQLName handles them
	if strings.ContainsAny(grantee, ";\n\r\t\x00") {
		return "", fmt.Errorf("invalid characters in grantee name: %s", grantee)
	}

	// Use escapeSQLName which handles proper escaping including quotes/brackets/backticks
	return g.escapeSQLName(grantee), nil
}

func (g *Generator) normalizeOldTableName(oldName, newName string) string {
	// Normalize the old table name with schema prefix if needed
	oldTableName := oldName
	if g.mode == GeneratorModePostgres || g.mode == GeneratorModeMssql {
		// If the old name doesn't contain a schema, add the default schema
		if !strings.Contains(oldTableName, ".") {
			// Extract schema from the new table name
			parts := strings.SplitN(newName, ".", 2)
			if len(parts) == 2 {
				oldTableName = parts[0] + "." + oldTableName
			}
		}
	}
	return oldTableName
}

// extractTableName extracts the table name from a fully-qualified name (e.g., "schema.table" -> "table")
func extractTableName(fullName string) string {
	if idx := strings.LastIndex(fullName, "."); idx != -1 {
		return fullName[idx+1:]
	}
	return fullName
}

func (g *Generator) generateRenameTableDDL(oldName, newName string) string {
	switch g.mode {
	case GeneratorModePostgres:
		// For PostgreSQL, RENAME TO should only include the table name without schema
		// Extract just the table name from the full name
		newTableName := extractTableName(newName)
		return fmt.Sprintf("ALTER TABLE %s RENAME TO %s",
			g.escapeTableName(oldName),
			g.escapeSQLName(newTableName))
	case GeneratorModeMssql:
		// MSSQL uses sp_rename for renaming tables
		// Extract just the table names without schema
		oldTableName := extractTableName(oldName)
		newTableName := extractTableName(newName)
		return fmt.Sprintf("EXEC sp_rename '%s', '%s'", oldTableName, newTableName)
	case GeneratorModeMysql:
		fallthrough
	case GeneratorModeSQLite3:
		fallthrough
	default:
		return fmt.Sprintf("ALTER TABLE %s RENAME TO %s",
			g.escapeTableName(oldName),
			g.escapeTableName(newName))
	}
}

func (g *Generator) notNull(column Column) bool {
	if column.notNull == nil {
		switch g.mode {
		case GeneratorModePostgres:
			return column.typeName == "serial" || column.typeName == "bigserial"
		default:
			return false
		}
	} else {
		return *column.notNull
	}
}

func isAddConstraintForeignKey(ddl string) bool {
	if strings.HasPrefix(ddl, "ALTER TABLE") && strings.Contains(ddl, "ADD CONSTRAINT") && strings.Contains(ddl, "FOREIGN KEY") {
		return true
	}
	return false
}

func isPrimaryKey(column Column, table Table) bool {
	if column.keyOption == ColumnKeyPrimary {
		return true
	}

	for _, index := range table.indexes {
		if index.primary {
			for _, indexColumn := range index.columns {
				if indexColumn.ColumnName() == column.name {
					return true
				}
			}
		}
	}
	return false
}

// Destructively modify table1 to have table2 columns/indexes
func mergeTable(table1 *Table, table2 Table) {
	for _, column := range table2.columns {
		if _, exist := table1.columns[column.name]; exist {
			table1.columns[column.name] = column
		}
	}

	for _, index := range table2.indexes {
		if slices.Contains(convertIndexesToIndexNames(table1.indexes), index.name) {
			table1.indexes = append(table1.indexes, index)
		}
	}
}

func aggregateDDLsToSchema(ddls []DDL) (*AggregatedSchema, error) {
	aggregated := &AggregatedSchema{
		Tables:     []*Table{},
		Views:      []*View{},
		Triggers:   []*Trigger{},
		Types:      []*Type{},
		Comments:   []*Comment{},
		Extensions: []*Extension{},
		Schemas:    []*Schema{},
		Privileges: []*GrantPrivilege{},
	}
	for _, ddl := range ddls {
		switch stmt := ddl.(type) {
		case *CreateTable:
			table := stmt.table // copy table
			aggregated.Tables = append(aggregated.Tables, &table)
		case *CreateIndex:
			table := findTableByName(aggregated.Tables, stmt.tableName)
			if table == nil {
				view := findViewByName(aggregated.Views, stmt.tableName)
				if view == nil {
					return nil, fmt.Errorf("CREATE INDEX is performed before CREATE TABLE: %s", ddl.Statement())
				}
				// TODO: check duplicated creation
				view.indexes = append(view.indexes, stmt.index)
			} else {
				// TODO: check duplicated creation
				table.indexes = append(table.indexes, stmt.index)
			}
		case *AddIndex:
			table := findTableByName(aggregated.Tables, stmt.tableName)
			if table == nil {
				return nil, fmt.Errorf("ADD INDEX is performed before CREATE TABLE: %s", ddl.Statement())
			}
			// TODO: check duplicated creation
			table.indexes = append(table.indexes, stmt.index)
		case *AddPrimaryKey:
			table := findTableByName(aggregated.Tables, stmt.tableName)
			if table == nil {
				return nil, fmt.Errorf("ADD PRIMARY KEY is performed before CREATE TABLE: %s", ddl.Statement())
			}

			newColumns := map[string]*Column{}
			for _, column := range table.columns {
				if column.name == stmt.index.columns[0].ColumnName() { // TODO: multi-column primary key?
					column.keyOption = ColumnKeyPrimary
				}
				newColumns[column.name] = column
			}
			table.columns = newColumns
		case *AddForeignKey:
			table := findTableByName(aggregated.Tables, stmt.tableName)
			if table == nil {
				return nil, fmt.Errorf("ADD FOREIGN KEY is performed before CREATE TABLE: %s", ddl.Statement())
			}

			table.foreignKeys = append(table.foreignKeys, stmt.foreignKey)
		case *AddExclusion:
			table := findTableByName(aggregated.Tables, stmt.tableName)
			if table == nil {
				return nil, fmt.Errorf("ADD EXCLUDE is performed before CREATE TABLE: %s", ddl.Statement())
			}

			table.exclusions = append(table.exclusions, stmt.exclusion)
		case *AddPolicy:
			table := findTableByName(aggregated.Tables, stmt.tableName)
			if table == nil {
				return nil, fmt.Errorf("ADD POLICY performed before CREATE TABLE: %s", ddl.Statement())
			}

			table.policies = append(table.policies, stmt.policy)
		case *View:
			aggregated.Views = append(aggregated.Views, stmt)
		case *Trigger:
			aggregated.Triggers = append(aggregated.Triggers, stmt)
		case *Type:
			aggregated.Types = append(aggregated.Types, stmt)
		case *Comment:
			aggregated.Comments = append(aggregated.Comments, stmt)
		case *Extension:
			aggregated.Extensions = append(aggregated.Extensions, stmt)
		case *Schema:
			aggregated.Schemas = append(aggregated.Schemas, stmt)
		case *GrantPrivilege:
			merged := false
			for i, existing := range aggregated.Privileges {
				if existing.tableName == stmt.tableName &&
					len(existing.grantees) == len(stmt.grantees) {
					allMatch := true
					for j, grantee := range existing.grantees {
						if grantee != stmt.grantees[j] {
							allMatch = false
							break
						}
					}
					if allMatch {
						privMap := make(map[string]bool)
						for _, priv := range existing.privileges {
							privMap[priv] = true
						}
						for _, priv := range stmt.privileges {
							privMap[priv] = true
						}
						mergedPrivs := []string{}
						for priv := range privMap {
							mergedPrivs = append(mergedPrivs, priv)
						}
						slices.Sort(mergedPrivs)
						aggregated.Privileges[i].privileges = mergedPrivs
						merged = true
						break
					}
				}
			}
			if !merged {
				aggregated.Privileges = append(aggregated.Privileges, stmt)
			}
		case *RevokePrivilege:
			// Note: REVOKE statements in desired schemas are not recommended
			// The desired schema should describe the target state with GRANTs only
			// This case is kept for backwards compatibility but may be removed
		default:
			return nil, fmt.Errorf("unexpected ddl type in convertDDLsToTablesAndViews: %#v", stmt)
		}
	}
	return aggregated, nil
}

func formatPrivilegesForGrant(privileges []string) string {
	if len(privileges) == 1 && privileges[0] == "ALL" {
		return "ALL PRIVILEGES"
	}
	if len(privileges) == len(postgresTablePrivilegeList) {
		privMap := make(map[string]bool)
		for _, priv := range privileges {
			privMap[priv] = true
		}
		allPresent := true
		for _, reqPriv := range postgresTablePrivilegeList {
			if !privMap[reqPriv] {
				allPresent = false
				break
			}
		}
		if allPresent {
			return "ALL PRIVILEGES"
		}
	}
	return strings.Join(privileges, ", ")
}

func (g *Generator) generateDDLsForGrantPrivilege(desired *GrantPrivilege) ([]string, error) {
	// Grantees should already be filtered by FilterPrivileges
	// If multiple grantees made it here, they all have the same privileges to grant

	var ddls []string
	desiredNormalized := normalizePrivilegesForComparison(desired.privileges)

	// Track REVOKE operations per grantee
	revokesByGrantee := make(map[string][]string)
	// Track GRANT operations grouped by privileges to grant
	type grantGroup struct {
		privileges []string
		grantees   []string
	}
	grantsByPrivileges := make(map[string]*grantGroup) // privileges key -> grant group

	for _, grantee := range desired.grantees {
		existingPrivilegesMap := make(map[string]bool)
		for _, currentPriv := range g.currentPrivileges {
			if currentPriv.tableName == desired.tableName {
				if slices.Contains(currentPriv.grantees, grantee) {
					normalized := normalizePrivilegesForComparison(currentPriv.privileges)
					for _, priv := range normalized {
						existingPrivilegesMap[priv] = true
					}
				}
			}
		}

		var existingNormalized []string
		for priv := range existingPrivilegesMap {
			existingNormalized = append(existingNormalized, priv)
		}
		if len(existingNormalized) > 0 {
			sortPrivilegesByCanonicalOrder(existingNormalized)
		}

		if len(existingNormalized) > 0 && equalPrivileges(existingNormalized, desiredNormalized) {
			continue
		}

		var privilegesToRevoke []string
		if len(existingNormalized) > 0 {
			desiredMap := make(map[string]bool)
			for _, priv := range desiredNormalized {
				desiredMap[priv] = true
			}
			for _, priv := range existingNormalized {
				if !desiredMap[priv] {
					privilegesToRevoke = append(privilegesToRevoke, priv)
				}
			}
			if len(privilegesToRevoke) > 0 {
				sortPrivilegesByCanonicalOrder(privilegesToRevoke)
				revokesByGrantee[grantee] = privilegesToRevoke
			}
		}

		var privilegesToGrant []string
		if len(existingNormalized) > 0 {
			existingMap := make(map[string]bool)
			for _, priv := range existingNormalized {
				existingMap[priv] = true
			}
			for _, priv := range desiredNormalized {
				if !existingMap[priv] {
					privilegesToGrant = append(privilegesToGrant, priv)
				}
			}
		} else {
			privilegesToGrant = desiredNormalized
		}

		if len(privilegesToGrant) > 0 {
			privilegesCopy := make([]string, len(privilegesToGrant))
			copy(privilegesCopy, privilegesToGrant)
			sortPrivilegesByCanonicalOrder(privilegesCopy)
			privilegesKey := strings.Join(privilegesCopy, ",")

			if group, exists := grantsByPrivileges[privilegesKey]; exists {
				group.grantees = append(group.grantees, grantee)
			} else {
				grantsByPrivileges[privilegesKey] = &grantGroup{
					privileges: privilegesToGrant,
					grantees:   []string{grantee},
				}
			}
		}
	}

	if g.config.EnableDrop {
		for grantee, privileges := range revokesByGrantee {
			escapedGrantee, err := g.validateAndEscapeGrantee(grantee)
			if err != nil {
				return nil, err
			}
			revoke := fmt.Sprintf("REVOKE %s ON TABLE %s FROM %s",
				strings.Join(privileges, ", "),
				g.escapeTableName(desired.tableName),
				escapedGrantee)
			ddls = append(ddls, revoke)
		}
	}

	var privilegeKeys []string
	for key := range grantsByPrivileges {
		privilegeKeys = append(privilegeKeys, key)
	}
	slices.Sort(privilegeKeys)

	for _, key := range privilegeKeys {
		group := grantsByPrivileges[key]
		escapedGrantees := []string{}
		for _, grantee := range group.grantees {
			escapedGrantee, err := g.validateAndEscapeGrantee(grantee)
			if err != nil {
				return nil, err
			}
			escapedGrantees = append(escapedGrantees, escapedGrantee)
		}
		slices.Sort(escapedGrantees)
		grant := fmt.Sprintf("GRANT %s ON TABLE %s TO %s",
			formatPrivilegesForGrant(group.privileges),
			g.escapeTableName(desired.tableName),
			strings.Join(escapedGrantees, ", "))
		ddls = append(ddls, grant)
	}

	// DO NOT update current privileges here - this breaks idempotency
	// The state should only be updated after DDLs are successfully applied

	return ddls, nil
}

func (g *Generator) generateDDLsForRevokePrivilege(desired *RevokePrivilege) ([]string, error) {
	if len(g.config.ManagedRoles) > 0 && len(desired.grantees) > 0 {
		hasIncludedGrantee := false
		for _, grantee := range desired.grantees {
			if slices.Contains(g.config.ManagedRoles, grantee) {
				hasIncludedGrantee = true
				break
			}
		}
		if !hasIncludedGrantee {
			return []string{}, nil
		}
	}

	// Only process REVOKE if EnableDrop is true
	if !g.config.EnableDrop {
		return []string{}, nil
	}

	escapedGrantee, err := g.validateAndEscapeGrantee(desired.grantees[0])
	if err != nil {
		return nil, err
	}

	revoke := fmt.Sprintf("REVOKE %s ON TABLE %s FROM %s",
		formatPrivilegesForGrant(desired.privileges),
		g.escapeTableName(desired.tableName),
		escapedGrantee)

	if desired.cascadeOption {
		revoke += " CASCADE"
	}

	// DO NOT update current privileges here - this breaks idempotency
	// The state should only be updated after DDLs are successfully applied

	return []string{revoke}, nil
}

func equalPrivileges(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aMap := make(map[string]bool)
	for _, priv := range a {
		aMap[priv] = true
	}
	for _, priv := range b {
		if !aMap[priv] {
			return false
		}
	}
	return true
}

func findTableByName(tables []*Table, name string) *Table {
	for _, table := range tables {
		if table.name == name {
			return table
		}
	}
	return nil
}

func findColumnByName(columns map[string]*Column, name string) *Column {
	if column, ok := columns[name]; ok {
		return column
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

func findIndexOptionByName(options []IndexOption, name string) *IndexOption {
	for _, option := range options {
		if option.optionName == name {
			return &option
		}
	}
	return nil
}

func findCheckConstraintInTable(table *Table, constraintName string) *CheckDefinition {
	// First, look for table-level check constraints
	for _, check := range table.checks {
		if check.constraintName == constraintName {
			return &check
		}
	}

	// Then, look for column-level check constraints
	for _, column := range table.columns {
		if column.check != nil && column.check.constraintName == constraintName {
			return column.check
		}
	}

	return nil
}

// findCheckConstraintByName finds a CHECK constraint in a list by name
func findCheckConstraintByName(checks []CheckDefinition, constraintName string) *CheckDefinition {
	for _, check := range checks {
		if check.constraintName == constraintName {
			return &check
		}
	}
	return nil
}

// findCheckConstraintByDefinitionInList finds a CHECK constraint in a list by comparing definitions
func (g *Generator) findCheckConstraintByDefinitionInList(checks []CheckDefinition, check *CheckDefinition) *CheckDefinition {
	if check == nil {
		return nil
	}

	for _, currentCheck := range checks {
		if g.areSameCheckDefinition(&currentCheck, check) {
			return &currentCheck
		}
	}
	return nil
}

// findCheckConstraintByDefinition finds a CHECK constraint in a table by comparing definitions.
// This is used for MySQL when column-level CHECKs are converted to table-level CONSTRAINTs
// with auto-generated names.
func (g *Generator) findCheckConstraintByDefinition(table *Table, check *CheckDefinition) *CheckDefinition {
	if check == nil {
		return nil
	}

	// Search table-level checks
	for _, currentCheck := range table.checks {
		if g.areSameCheckDefinition(&currentCheck, check) {
			return &currentCheck
		}
	}

	// Search column-level checks
	for _, column := range table.columns {
		if column.check != nil && g.areSameCheckDefinition(column.check, check) {
			return column.check
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

// findForeignKeysReferencingTable finds all foreign keys from all tables that reference the given table
func (g *Generator) findForeignKeysReferencingTable(referencedTableName string) []struct {
	tableName  string
	foreignKey ForeignKey
} {
	var referencingFKs []struct {
		tableName  string
		foreignKey ForeignKey
	}

	// Check all current tables for foreign keys that reference this table
	for _, table := range g.currentTables {
		for _, fk := range table.foreignKeys {
			if fk.referenceName == referencedTableName {
				referencingFKs = append(referencingFKs, struct {
					tableName  string
					foreignKey ForeignKey
				}{
					tableName:  table.name,
					foreignKey: fk,
				})
			}
		}
	}

	return referencingFKs
}

func findExclusionByName(exclusions []Exclusion, constraintName string) *Exclusion {
	for _, exclusion := range exclusions {
		if exclusion.constraintName == constraintName {
			return &exclusion
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

func findTriggerByName(triggers []*Trigger, name string) *Trigger {
	for _, trigger := range triggers {
		if trigger.name == name {
			return trigger
		}
	}
	return nil
}

func findTypeByName(types []*Type, name string) *Type {
	for _, createType := range types {
		if createType.name == name {
			return createType
		}
	}
	return nil
}

func findCommentByObject(comments []*Comment, object string) *Comment {
	for _, comment := range comments {
		if comment.comment.Object == object {
			return comment
		}
	}
	return nil
}

// generateCommentNullStatement creates a COMMENT ... IS NULL statement.
func (g *Generator) generateCommentNullStatement(comment *Comment) string {
	// Generate the COMMENT statement directly from the AST
	// The comment.comment contains ObjectType, Object, and Comment fields
	objectType := comment.comment.ObjectType
	object := comment.comment.Object

	var sqlObjectType string
	switch objectType {
	case "OBJECT_TABLE":
		sqlObjectType = "TABLE"
	case "OBJECT_COLUMN":
		sqlObjectType = "COLUMN"
	default:
		// For other object: strip "OBJECT_" prefix if present
		sqlObjectType = strings.TrimPrefix(objectType, "OBJECT_")
	}

	// Escape the object name appropriately based on object type
	var escapedObject string
	if sqlObjectType == "COLUMN" {
		// For columns, the object is in format "schema.table.column"
		// We need to escape each part appropriately
		parts := strings.Split(object, ".")
		if len(parts) == 3 {
			// schema.table.column
			escapedObject = fmt.Sprintf("%s.%s.%s",
				g.escapeSQLName(parts[0]),
				g.escapeSQLName(parts[1]),
				g.escapeSQLName(parts[2]))
		} else if len(parts) == 2 {
			// table.column (schema was added during normalization)
			escapedObject = fmt.Sprintf("%s.%s",
				g.escapeSQLName(parts[0]),
				g.escapeSQLName(parts[1]))
		} else {
			// Fallback: escape the whole thing
			escapedObject = g.escapeSQLName(object)
		}
	} else {
		// For tables and other objects, use escapeTableName which handles schema.table format
		escapedObject = g.escapeTableName(object)
	}

	// Generate the COMMENT statement with NULL value
	// Note: We don't add a trailing semicolon as it's added by joinDDLs
	return fmt.Sprintf("COMMENT ON %s %s IS NULL", sqlObjectType, escapedObject)
}

func findExtensionByName(extensions []*Extension, name string) *Extension {
	for _, extension := range extensions {
		if extension.extension.Name == name {
			return extension
		}
	}
	return nil
}

func findSchemaByName(schemas []*Schema, name string) *Schema {
	for _, schema := range schemas {
		if schema.schema.Name == name {
			return schema
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
		// (current.check == desired.check) && /* workaround. CHECK handling in general should be improved later */
		(desired.charset == "" || current.charset == desired.charset) && // detect change column only when set explicitly. TODO: can we calculate implicit charset?
		(desired.collate == "" || current.collate == desired.collate) && // detect change column only when set explicitly. TODO: can we calculate implicit collate?
		reflect.DeepEqual(current.onUpdate, desired.onUpdate) &&
		reflect.DeepEqual(current.comment, desired.comment)
}

func (g *Generator) areSameGenerated(generatedA, generatedB *Generated) bool {
	if generatedA == nil && generatedB == nil {
		return true
	}
	if generatedA == nil || generatedB == nil {
		return false
	}
	// TODO: Difference between bracketed and unbracketed, as Expr values are not fully comparable.
	return (generatedA.expr == generatedB.expr || generatedA.expr == "("+generatedB.expr+")") &&
		generatedA.generatedType == generatedB.generatedType
}

func (g *Generator) haveSameDataType(current Column, desired Column) bool {
	if normalizeTypeName(current.typeName, g.mode) != normalizeTypeName(desired.typeName, g.mode) {
		return false
	}
	if !reflect.DeepEqual(current.enumValues, desired.enumValues) {
		return false
	}

	currentLength := current.length
	desiredLength := desired.length
	currentScale := current.scale
	desiredScale := desired.scale

	// Normalize default precision/scale for numeric/decimal types.
	if g.mode == GeneratorModeMssql || g.mode == GeneratorModeMysql {
		normalizedType := normalizeTypeName(current.typeName, g.mode)
		if normalizedType == "decimal" {
			var defaultPrecision int
			switch g.mode {
			case GeneratorModeMysql:
				defaultPrecision = 10
			case GeneratorModeMssql:
				defaultPrecision = 18
			}

			if currentLength == nil {
				currentLength = &Value{valueType: ValueTypeInt, intVal: defaultPrecision}
			}
			if desiredLength == nil {
				desiredLength = &Value{valueType: ValueTypeInt, intVal: defaultPrecision}
			}
			if currentScale == nil {
				currentScale = &Value{valueType: ValueTypeInt, intVal: 0}
			}
			if desiredScale == nil {
				desiredScale = &Value{valueType: ValueTypeInt, intVal: 0}
			}
		}
	}

	if currentLength == nil && desiredLength != nil || currentLength != nil && desiredLength == nil {
		return false
	}
	if currentLength != nil && desiredLength != nil && currentLength.intVal != desiredLength.intVal {
		return false
	}
	if currentScale == nil && (desiredScale != nil && desiredScale.intVal != 0) || (currentScale != nil && currentScale.intVal != 0) && desiredScale == nil {
		return false
	}
	if currentScale != nil && desiredScale != nil && currentScale.intVal != desiredScale.intVal {
		return false
	}
	if current.array != desired.array {
		return false
	}
	if current.timezone != desired.timezone {
		return false
	}
	return true
}

func (g *Generator) areSameCheckDefinition(checkA *CheckDefinition, checkB *CheckDefinition) bool {
	if checkA == nil && checkB == nil {
		return true
	}
	if checkA == nil || checkB == nil {
		return false
	}

	if checkA.definition == nil || checkB.definition == nil {
		panic(fmt.Sprintf("CheckDefinition.definitionAST must not be nil (checkA.definitionAST=%v, checkB.definitionAST=%v)", checkA.definition, checkB.definition))
	}

	normalizedA := normalizeCheckExpr(checkA.definition, g.mode)
	normalizedB := normalizeCheckExpr(checkB.definition, g.mode)

	// Unwrap outermost parentheses if present (MySQL adds extra parens)
	normalizedA = unwrapOutermostParenExpr(normalizedA)
	normalizedB = unwrapOutermostParenExpr(normalizedB)

	return parser.String(normalizedA) == parser.String(normalizedB) &&
		checkA.notForReplication == checkB.notForReplication &&
		checkA.noInherit == checkB.noInherit
}

// unwrapOutermostParenExpr removes the outermost ParenExpr if the expression is wrapped in one.
// This is needed because some databases (like MySQL) add extra parentheses around CHECK expressions.
// It preserves parentheses around OR expressions to maintain correct operator precedence.
func unwrapOutermostParenExpr(expr parser.Expr) parser.Expr {
	if paren, ok := expr.(*parser.ParenExpr); ok {
		// Don't unwrap if inner expression is OR (to preserve operator precedence)
		if _, isOr := paren.Expr.(*parser.OrExpr); !isOr {
			return paren.Expr
		}
	}
	return expr
}

func (g *Generator) buildForeignKeyDDL(tableName string, fk *ForeignKey) string {
	ddl := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
		g.escapeTableName(tableName),
		g.escapeSQLName(fk.constraintName),
		g.escapeAndJoinColumns(fk.indexColumns),
		g.escapeTableName(fk.referenceName),
		g.escapeAndJoinColumns(fk.referenceColumns))

	if fk.onDelete != "" {
		ddl += " ON DELETE " + fk.onDelete
	}
	if fk.onUpdate != "" {
		ddl += " ON UPDATE " + fk.onUpdate
	}

	return ddl
}

// normalizeCheckExprString returns a normalized string representation of a CHECK constraint expression
// For PostgreSQL, this converts IN (a,b,c) to = ANY (ARRAY[a,b,c])
func (g *Generator) normalizeCheckExprString(expr parser.Expr) string {
	if g.mode == GeneratorModePostgres {
		normalized := normalizeCheckExpr(expr, g.mode)
		// Unwrap outermost parentheses for consistent output (comparison does this too)
		normalized = unwrapOutermostParenExpr(normalized)
		return parser.String(normalized)
	}
	return parser.String(expr)
}

func areSameIdentityDefinition(identityA *Identity, identityB *Identity) bool {
	if identityA == nil && identityB == nil {
		return true
	}
	if identityA == nil || identityB == nil {
		return false
	}
	return identityA.behavior == identityB.behavior && identityA.notForReplication == identityB.notForReplication
}

func (g *Generator) areSameDefaultValue(currentDefault *DefaultDefinition, desiredDefault *DefaultDefinition, columnType string) bool {
	// Normalize: DEFAULT NULL is the same as no default
	currentIsNull := currentDefault == nil || isNullDefault(currentDefault)
	desiredIsNull := desiredDefault == nil || isNullDefault(desiredDefault)

	// Both null (or absent) - they're the same
	if currentIsNull && desiredIsNull {
		return true
	}
	// One is null, the other isn't - they're different
	if currentIsNull || desiredIsNull {
		return false
	}

	// Normalize expressions first to handle typed literals and other database-specific normalizations
	// This ensures that TypedLiterals are converted to their underlying values before comparison
	slog.Debug("Generator#areSameDefaultValue (before normalization)",
		"currentExpr", parser.String(currentDefault.expression),
		"desiredExpr", parser.String(desiredDefault.expression),
		"columnType", columnType,
	)
	normalizedCurrent := normalizeExpr(currentDefault.expression, g.mode)
	normalizedDesired := normalizeExpr(desiredDefault.expression, g.mode)

	// Check if both are simple SQLVal (vs complex expressions) after normalization
	currSQLVal, currentIsSQLVal := normalizedCurrent.(*parser.SQLVal)
	desSQLVal, desiredIsSQLVal := normalizedDesired.(*parser.SQLVal)

	// If both are simple values (SQLVal), use value comparison
	if currentIsSQLVal && desiredIsSQLVal {
		currentVal := parseValue(currSQLVal)
		desiredVal := parseValue(desSQLVal)

		if isNullValue(currentVal) {
			currentVal = nil
		}
		if isNullValue(desiredVal) {
			desiredVal = nil
		}

		return g.areSameValue(currentVal, desiredVal, columnType)
	}

	// If one is SQLVal and the other is not, they're different
	if currentIsSQLVal != desiredIsSQLVal {
		return false
	}

	// Both are complex expressions - use string comparison on already-normalized expressions
	var currentExprSchema, currentExpr string
	var desiredExprSchema, desiredExpr string

	currentExprSchema, currentExpr = splitTableName(parser.String(normalizedCurrent), g.defaultSchema)
	desiredExprSchema, desiredExpr = splitTableName(parser.String(normalizedDesired), g.defaultSchema)
	slog.Debug("Generator#areSameDefaultValue (complex expressions)",
		"currentExpr", currentExpr,
		"desiredExpr", desiredExpr,
		"columnType", columnType,
	)
	return strings.EqualFold(currentExprSchema, desiredExprSchema) && strings.EqualFold(currentExpr, desiredExpr)
}

// isNumericColumnType determines if a column type should be compared numerically.
// This is used to decide how to compare default values.
func (g *Generator) isNumericColumnType(typeName string) bool {
	switch normalizeTypeName(typeName, g.mode) {
	case "tinyint", "smallint", "mediumint", "integer", "bigint",
		"decimal", "float", "double", "real":
		return true
	default:
		return false
	}
}

// areSameValue compares two default values with knowledge of the column type.
func (g *Generator) areSameValue(current, desired *Value, columnType string) bool {
	slog.Debug("Generator#areSameValueForDefault",
		"current", current,
		"desired", desired,
		"columnType", columnType,
	)

	if current == nil && desired == nil {
		return true
	}
	if current == nil || desired == nil {
		return false
	}

	currentRaw := current.raw
	desiredRaw := desired.raw

	// Special handling for MySQL boolean values (BOOLEAN is stored as TINYINT(1))
	// MySQL converts: false → 0, true → 1
	if g.mode == GeneratorModeMysql && desired.valueType == ValueTypeBool {
		if strings.EqualFold(desiredRaw, "false") {
			desiredRaw = "0"
		} else if strings.EqualFold(desiredRaw, "true") {
			desiredRaw = "1"
		}
	}

	if g.isNumericColumnType(columnType) {
		// For numeric types (DECIMAL, INT, FLOAT, etc.): use numeric comparison
		// This handles DECIMAL precision normalization: '0.1' vs '0.10'
		// Use 256 bits of precision to safely handle DECIMAL(65,30) (MySQL's max)
		// 65 decimal digits * 3.32 bits/digit ≈ 216 bits, so 256 bits is safe
		if currentFloat, _, err := big.ParseFloat(currentRaw, 10, 256, big.ToNearestEven); err == nil {
			if desiredFloat, _, err := big.ParseFloat(desiredRaw, 10, 256, big.ToNearestEven); err == nil {
				return currentFloat.Cmp(desiredFloat) == 0
			}
		}
		// Fallback to string comparison if parse fails
	}

	// For string types (VARCHAR, CHAR, TEXT, etc.): use exact string comparison
	// This ensures '1.00' != '1.0000' for VARCHAR columns
	return currentRaw == desiredRaw
}

// areSameIdentifier compares two values for identifiers/keywords (case-insensitive).
func (g *Generator) areSameIdentifier(current, desired *Value) bool {
	if current == nil && desired == nil {
		return true
	}
	if current == nil || desired == nil {
		return false
	}
	return strings.EqualFold(current.raw, desired.raw)
}

func areSameTriggerDefinition(triggerA, triggerB *Trigger) bool {
	if triggerA.time != triggerB.time {
		return false
	}
	if len(triggerA.event) != len(triggerB.event) {
		return false
	}
	for i := 0; i < len(triggerA.event); i++ {
		if triggerA.event[i] != triggerB.event[i] {
			return false
		}
	}
	if triggerA.tableName != triggerB.tableName {
		return false
	}
	if len(triggerA.body) != len(triggerB.body) {
		return false
	}
	for i := 0; i < len(triggerA.body); i++ {
		bodyA := strings.ReplaceAll(triggerA.body[i], " ", "")
		bodyB := strings.ReplaceAll(triggerB.body[i], " ", "")
		if !strings.EqualFold(bodyA, bodyB) {
			return false
		}
	}
	return true
}

func isNullValue(value *Value) bool {
	return value != nil && value.valueType == ValueTypeValArg && strings.EqualFold(value.raw, "null")
}

func isNullDefault(def *DefaultDefinition) bool {
	if def == nil || def.expression == nil {
		return false
	}

	// Check if it's a direct SQLVal
	if sqlVal, ok := def.expression.(*parser.SQLVal); ok {
		val := parseValue(sqlVal)
		return isNullValue(val)
	}

	// Check if it's a CastExpr wrapping a NULL value (e.g., NULL::character varying)
	if castExpr, ok := def.expression.(*parser.CastExpr); ok {
		if sqlVal, ok := castExpr.Expr.(*parser.SQLVal); ok {
			val := parseValue(sqlVal)
			return isNullValue(val)
		}
	}

	return false
}

func (g *Generator) areSamePrimaryKeys(primaryKeyA *Index, primaryKeyB *Index) bool {
	if primaryKeyA != nil && primaryKeyB != nil {
		// For MSSQL, when comparing PRIMARY KEY constraints,
		// ignore the name if one is auto-generated (PK__*) and the other is unnamed/synthetic ("PRIMARY")
		if g.mode == GeneratorModeMssql && primaryKeyA.primary && primaryKeyB.primary {
			// Check if one has an auto-generated name and the other is synthetic
			if (strings.HasPrefix(primaryKeyA.name, "PK__") && primaryKeyB.name == "PRIMARY") ||
				(strings.HasPrefix(primaryKeyB.name, "PK__") && primaryKeyA.name == "PRIMARY") {
				// Compare everything except the name
				return g.areSamePrimaryKeyColumns(*primaryKeyA, *primaryKeyB)
			}
		}
		return g.areSameIndexes(*primaryKeyA, *primaryKeyB)
	} else {
		return primaryKeyA == nil && primaryKeyB == nil
	}
}

// areSamePrimaryKeyColumns compares primary keys without checking the name
func (g *Generator) areSamePrimaryKeyColumns(indexA Index, indexB Index) bool {
	if !indexA.primary || !indexB.primary {
		return false
	}
	if len(indexA.columns) != len(indexB.columns) {
		return false
	}
	for i, indexAColumn := range indexA.columns {
		dirA := indexAColumn.direction
		if dirA == "" {
			dirA = AscScr
		}
		dirB := indexB.columns[i].direction
		if dirB == "" {
			dirB = AscScr
		}

		normalizedA := parser.String(normalizeExpr(indexA.columns[i].columnExpr, g.mode))
		normalizedB := parser.String(normalizeExpr(indexB.columns[i].columnExpr, g.mode))
		if normalizedA != normalizedB || dirA != dirB {
			return false
		}
	}
	// For primary keys, we don't need to check other properties like where, included, options
	return true
}

func (g *Generator) areSameIndexes(indexA Index, indexB Index) bool {
	if indexA.unique != indexB.unique {
		return false
	}
	if indexA.primary != indexB.primary {
		return false
	}
	if indexA.vector != indexB.vector {
		return false
	}
	for len(indexA.columns) != len(indexB.columns) {
		return false
	}
	for i, indexAColumn := range indexA.columns {
		if indexAColumn.direction == "" {
			indexAColumn.direction = AscScr
		}
		if indexB.columns[i].direction == "" {
			indexB.columns[i].direction = AscScr
		}
		// TODO: check length?
		normalizedA := parser.String(normalizeExpr(indexA.columns[i].columnExpr, g.mode))
		normalizedB := parser.String(normalizeExpr(indexB.columns[i].columnExpr, g.mode))
		if normalizedA != normalizedB ||
			indexAColumn.direction != indexB.columns[i].direction {
			return false
		}
	}
	if indexA.where != indexB.where {
		return false
	}

	if len(indexA.included) != len(indexB.included) {
		return false
	}
	for i, indexAIncluded := range indexA.included {
		if indexAIncluded != indexB.included[i] {
			return false
		}
	}

	// For MSSQL UNIQUE constraints (not regular indexes), don't compare options
	// Inline constraints don't include WITH options in their DDL, but the database
	// still has default options that show up in sys.indexes
	// Only skip options comparison for constraints, not regular unique indexes
	if !(g.mode == GeneratorModeMssql && indexA.constraint && indexB.constraint && indexA.unique && indexB.unique) {
		indexAOptions := indexA.options
		indexBOptions := indexB.options
		// Mysql: Default Index B-Tree (but not for vector indexes)
		if g.mode == GeneratorModeMysql {
			if len(indexAOptions) == 0 && !indexA.vector {
				indexAOptions = []IndexOption{
					{optionName: "using", value: &Value{valueType: ValueTypeStr, raw: "btree", strVal: "btree"}},
				}
			}
			if len(indexBOptions) == 0 && !indexB.vector {
				indexBOptions = []IndexOption{
					{optionName: "using", value: &Value{valueType: ValueTypeStr, raw: "btree", strVal: "btree"}},
				}
			}
		}
		for _, optionB := range indexBOptions {
			if optionA := findIndexOptionByName(indexAOptions, optionB.optionName); optionA != nil {
				if !g.areSameIdentifier(optionA.value, optionB.value) {
					return false
				}
			} else {
				return false
			}
		}
	}

	// For MSSQL UNIQUE constraints, constraintOptions don't matter
	// They're only used for PostgreSQL deferrable constraints
	if g.mode != GeneratorModeMssql {
		if indexA.constraint != indexB.constraint {
			return false
		}
		if (indexA.constraintOptions != nil) != (indexB.constraintOptions != nil) {
			return false
		}
		if indexA.constraintOptions != nil && indexB.constraintOptions != nil {
			if indexA.constraintOptions.deferrable != indexB.constraintOptions.deferrable {
				return false
			}
			if indexA.constraintOptions.initiallyDeferred != indexB.constraintOptions.initiallyDeferred {
				return false
			}
		}
	}

	return true
}

func (g *Generator) areSameForeignKeys(foreignKeyA ForeignKey, foreignKeyB ForeignKey) bool {
	if g.normalizeReferenceOption(foreignKeyA.onUpdate) != g.normalizeReferenceOption(foreignKeyB.onUpdate) {
		return false
	}
	if g.normalizeReferenceOption(foreignKeyA.onDelete) != g.normalizeReferenceOption(foreignKeyB.onDelete) {
		return false
	}
	if foreignKeyA.notForReplication != foreignKeyB.notForReplication {
		return false
	}
	if (foreignKeyA.constraintOptions != nil) != (foreignKeyB.constraintOptions != nil) {
		return false
	}
	if foreignKeyA.constraintOptions != nil && foreignKeyB.constraintOptions != nil {
		if foreignKeyA.constraintOptions.deferrable != foreignKeyB.constraintOptions.deferrable {
			return false
		}
		if foreignKeyA.constraintOptions.initiallyDeferred != foreignKeyB.constraintOptions.initiallyDeferred {
			return false
		}
	}

	return true
}

func (g *Generator) areSameExclusions(exclusionA Exclusion, exclusionB Exclusion) bool {
	if exclusionA.indexType != exclusionB.indexType {
		return false
	}
	if len(exclusionA.exclusions) != len(exclusionB.exclusions) {
		return false
	}
	if exclusionA.where != exclusionB.where {
		return false
	}
	for i := range exclusionA.exclusions {
		a := exclusionA.exclusions[i]
		b := exclusionB.exclusions[i]
		if a.expression != b.expression || a.operator != b.operator {
			return false
		}
	}
	return true
}

func areSamePolicies(policyA, policyB Policy) bool {
	if !strings.EqualFold(policyA.scope, policyB.scope) {
		return false
	}
	if !strings.EqualFold(policyA.permissive, policyB.permissive) {
		return false
	}
	if normalizeUsing(policyA.using) != normalizeUsing(policyB.using) {
		return fmt.Sprintf("(%s)", policyA.using) == policyB.using
	}
	if !strings.EqualFold(policyA.withCheck, policyB.withCheck) {
		return fmt.Sprintf("(%s)", policyA.withCheck) == policyB.withCheck
	}
	if len(policyA.roles) != len(policyB.roles) {
		return false
	}
	slices.SortFunc(policyA.roles, func(a, b string) int {
		return cmp.Compare(a, b)
	})
	slices.SortFunc(policyB.roles, func(a, b string) int {
		return cmp.Compare(a, b)
	})
	for i := range policyA.roles {
		if !strings.EqualFold(policyA.roles[i], policyB.roles[i]) {
			return false
		}
	}
	return true
}

func (g *Generator) normalizeReferenceOption(action string) string {
	if g.mode == GeneratorModeMysql && action == "" {
		return "RESTRICT"
	} else if (g.mode == GeneratorModePostgres || g.mode == GeneratorModeMssql) && action == "" {
		return "NO ACTION"
	} else {
		return action
	}
}

// TODO: Use interface to avoid defining following functions?

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

func convertExclusionToConstraintNames(exclusions []Exclusion) []string {
	constraintNames := []string{}
	for _, exclusion := range exclusions {
		constraintNames = append(constraintNames, exclusion.constraintName)
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
	return util.TransformSlice(policies, func(p Policy) string { return p.name })
}

func convertViewNames(views []*View) []string {
	return util.TransformSlice(views, func(v *View) string { return v.name })
}

func convertExtensionNames(extensions []*Extension) []string {
	return util.TransformSlice(extensions, func(e *Extension) string { return e.extension.Name })
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

func generateSequenceClause(sequence *Sequence) string {
	ddl := ""
	if sequence.Name != "" {
		ddl += fmt.Sprintf("SEQUENCE NAME %s ", sequence.Name)
	}
	if sequence.StartWith != nil {
		ddl += fmt.Sprintf("START WITH %d ", *sequence.StartWith)
	}
	if sequence.IncrementBy != nil {
		ddl += fmt.Sprintf("INCREMENT BY %d ", *sequence.IncrementBy)
	}
	if sequence.MinValue != nil {
		ddl += fmt.Sprintf("MINVALUE %d ", *sequence.MinValue)
	}
	if sequence.NoMinValue {
		ddl += "NO MINVALUE "
	}
	if sequence.MaxValue != nil {
		ddl += fmt.Sprintf("MAXVALUE %d ", *sequence.MaxValue)
	}
	if sequence.NoMaxValue {
		ddl += "NO MAXVALUE "
	}
	if sequence.Cache != nil {
		ddl += fmt.Sprintf("CACHE %d ", *sequence.Cache)
	}
	if sequence.Cycle {
		ddl += "CYCLE "
	}
	if sequence.NoCycle {
		ddl += "NO CYCLE "
	}

	return strings.TrimSpace(ddl)
}

func (g *Generator) generateDefaultDefinition(defaultDefinition DefaultDefinition) (string, error) {
	if defaultDefinition.expression == nil {
		return "", fmt.Errorf("default expression is nil")
	}

	// Type assertion: Check if it's a simple SQLVal
	if sqlVal, ok := defaultDefinition.expression.(*parser.SQLVal); ok {
		// Simple value path - maintain existing formatting behavior
		defaultVal := parseValue(sqlVal)
		switch defaultVal.valueType {
		case ValueTypeStr:
			return fmt.Sprintf("DEFAULT %s", StringConstant(defaultVal.strVal)), nil
		case ValueTypeBool:
			return fmt.Sprintf("DEFAULT %s", defaultVal.strVal), nil
		case ValueTypeInt:
			return fmt.Sprintf("DEFAULT %d", defaultVal.intVal), nil
		case ValueTypeFloat:
			return fmt.Sprintf("DEFAULT %f", defaultVal.floatVal), nil
		case ValueTypeBit:
			if defaultVal.bitVal {
				return "DEFAULT b'1'", nil
			} else {
				return "DEFAULT b'0'", nil
			}
		case ValueTypeValArg: // NULL, CURRENT_TIMESTAMP, ...
			return fmt.Sprintf("DEFAULT %s", defaultVal.raw), nil
		default:
			return "", fmt.Errorf("unsupported default value type (valueType: '%d')", defaultVal.valueType)
		}
	}

	// Complex expression path
	// Normalize the expression to handle typed literals and other database-specific normalizations
	normalizedExpr := normalizeExpr(defaultDefinition.expression, g.mode)
	exprStr := parser.String(normalizedExpr)
	if g.mode == GeneratorModeMysql || g.mode == GeneratorModeSQLite3 {
		// Enclose expression with parentheses to avoid syntax error
		// https://dev.mysql.com/doc/refman/8.0/en/data-type-defaults.html#data-type-defaults-explicit
		// https://www.sqlite.org/syntax/column-constraint.html
		return fmt.Sprintf("DEFAULT(%s)", exprStr), nil
	} else {
		return fmt.Sprintf("DEFAULT %s", exprStr), nil
	}
}

func generateSridDefinition(sridVal Value) (string, error) {
	switch sridVal.valueType {
	case ValueTypeInt:
		// SRID option is only for MySQL 8.0.3 or later
		return fmt.Sprintf("/*!80003 SRID %d */", sridVal.intVal), nil
	default:
		return "", fmt.Errorf("unsupported SRID value type (valueType: '%d')", sridVal.valueType)
	}
}

func FilterTables(ddls []DDL, config database.GeneratorConfig) []DDL {
	filtered := []DDL{}

	for _, ddl := range ddls {
		tables := []string{}

		switch stmt := ddl.(type) {
		case *CreateTable:
			tables = append(tables, stmt.table.name)
		case *CreateIndex:
			tables = append(tables, stmt.tableName)
		case *AddPrimaryKey:
			tables = append(tables, stmt.tableName)
		case *AddForeignKey:
			tables = append(tables, stmt.tableName)
			tables = append(tables, stmt.foreignKey.referenceName)
		case *AddIndex:
			tables = append(tables, stmt.tableName)
		}

		if skipTables(tables, config) {
			continue
		}

		filtered = append(filtered, ddl)
	}

	return filtered
}

func skipTables(tables []string, config database.GeneratorConfig) bool {
	if config.TargetTables != nil {
		for _, t := range tables {
			if !containsRegexpString(config.TargetTables, t) {
				return true
			}
		}
	}

	for _, t := range tables {
		if containsRegexpString(config.SkipTables, t) {
			return true
		}
	}
	return false
}

func FilterViews(ddls []DDL, config database.GeneratorConfig) []DDL {
	filtered := []DDL{}

	for _, ddl := range ddls {
		views := []string{}

		switch stmt := ddl.(type) {
		case *CreateIndex:
			views = append(views, stmt.tableName)
		case *View:
			views = append(views, stmt.name)
		}

		if skipViews(views, config) {
			continue
		}

		filtered = append(filtered, ddl)
	}

	return filtered
}

func FilterPrivileges(ddls []DDL, config database.GeneratorConfig) []DDL {
	// If no roles specified, exclude all privileges
	if len(config.ManagedRoles) == 0 {
		filtered := []DDL{}
		for _, ddl := range ddls {
			switch ddl.(type) {
			case *GrantPrivilege, *RevokePrivilege:
				// Skip all privilege-related DDLs
				continue
			default:
				filtered = append(filtered, ddl)
			}
		}
		return filtered
	}

	// Filter privileges to only include specified roles
	filtered := []DDL{}
	// Map to consolidate grants by table and privileges
	grantsByTableAndPrivs := make(map[string]*GrantPrivilege)
	grantsOrder := []string{} // Track order of insertion
	revokesByTableAndGrantee := make(map[string]*RevokePrivilege)
	revokesOrder := []string{} // Track order of insertion

	for _, ddl := range ddls {
		switch stmt := ddl.(type) {
		case *GrantPrivilege:
			// Filter grantees to only include those in config
			includedGrantees := []string{}
			for _, grantee := range stmt.grantees {
				if slices.Contains(config.ManagedRoles, grantee) {
					includedGrantees = append(includedGrantees, grantee)
				}
			}

			if len(includedGrantees) > 0 {
				// Sort privileges for consistent key
				sortedPrivs := make([]string, len(stmt.privileges))
				copy(sortedPrivs, stmt.privileges)
				slices.Sort(sortedPrivs)
				key := fmt.Sprintf("%s:%s", stmt.tableName, strings.Join(sortedPrivs, ","))

				if existing, ok := grantsByTableAndPrivs[key]; ok {
					// Add grantees to existing grant with same table and privileges
					existing.grantees = append(existing.grantees, includedGrantees...)
				} else {
					// Create new grant with filtered grantees
					grantsByTableAndPrivs[key] = &GrantPrivilege{
						statement:  stmt.statement,
						tableName:  stmt.tableName,
						grantees:   includedGrantees,
						privileges: stmt.privileges,
					}
					grantsOrder = append(grantsOrder, key)
				}
			}
		case *RevokePrivilege:
			// Process each grantee separately and consolidate
			for _, grantee := range stmt.grantees {
				if slices.Contains(config.ManagedRoles, grantee) {
					key := fmt.Sprintf("%s:%s", stmt.tableName, grantee)
					if existing, ok := revokesByTableAndGrantee[key]; ok {
						// Merge privileges
						privMap := make(map[string]bool)
						for _, priv := range existing.privileges {
							privMap[priv] = true
						}
						for _, priv := range stmt.privileges {
							privMap[priv] = true
						}
						mergedPrivs := []string{}
						for priv := range privMap {
							mergedPrivs = append(mergedPrivs, priv)
						}
						slices.Sort(mergedPrivs)
						existing.privileges = mergedPrivs
					} else {
						// Create new revoke for this grantee
						revokesByTableAndGrantee[key] = &RevokePrivilege{
							statement:     stmt.statement,
							tableName:     stmt.tableName,
							grantees:      []string{grantee},
							privileges:    stmt.privileges,
							cascadeOption: stmt.cascadeOption,
						}
						revokesOrder = append(revokesOrder, key)
					}
				}
			}
		default:
			// Include all non-privilege DDLs
			filtered = append(filtered, ddl)
		}
	}

	// Add all consolidated grants to the result in original order
	for _, key := range grantsOrder {
		filtered = append(filtered, grantsByTableAndPrivs[key])
	}

	for _, key := range revokesOrder {
		filtered = append(filtered, revokesByTableAndGrantee[key])
	}

	return filtered
}

func skipViews(views []string, config database.GeneratorConfig) bool {
	for _, v := range views {
		if containsRegexpString(config.SkipViews, v) {
			return true
		}
	}
	return false
}

func containsRegexpString(strs []string, str string) bool {
	for _, s := range strs {
		if regexp.MustCompile("^" + s + "$").MatchString(str) {
			return true
		}
	}
	return false
}

// SortTablesByDependencies sorts CREATE TABLE DDLs by foreign key dependencies
// to ensure tables are created in the correct order (referenced tables before referencing tables)
// Also ensures CREATE TYPE statements are placed before CREATE TABLE statements that use them
func SortTablesByDependencies(ddls []DDL) []DDL {
	// Extract CREATE TABLE DDLs, CREATE TYPE DDLs, and other DDLs
	var createTables []*CreateTable
	var createTypes []*Type
	var otherDDLs []DDL

	for _, ddl := range ddls {
		if ct, ok := ddl.(*CreateTable); ok {
			createTables = append(createTables, ct)
		} else if typ, ok := ddl.(*Type); ok {
			createTypes = append(createTypes, typ)
		} else {
			otherDDLs = append(otherDDLs, ddl)
		}
	}

	// If there are no or only one CREATE TABLE, no sorting needed
	if len(createTables) <= 1 {
		return ddls
	}

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
		return ddls
	}

	// Rebuild the DDL list with CREATE TYPEs first, then sorted CREATE TABLEs, then other DDLs
	var result []DDL
	for _, typ := range createTypes {
		result = append(result, typ)
	}
	for _, ct := range sorted {
		result = append(result, ct)
	}
	result = append(result, otherDDLs...)

	return result
}

// generateDropTableDDLsWithDependencies generates DROP TABLE statements in the correct order
// considering foreign key dependencies. Tables that reference other tables are dropped first.
func (g *Generator) generateDropTableDDLsWithDependencies(tablesToDrop []*Table) []string {
	var sortedTablesToDrop []*Table

	if len(tablesToDrop) <= 1 {
		// If there are no or only one table to drop, no sorting needed.
		sortedTablesToDrop = tablesToDrop
	} else {
		// Build reverse dependency graph for drops
		// For drops: if table A references table B, then B depends on A (B can't be dropped until A is dropped)
		tableDependencies := make(map[string][]string)
		tableMap := make(map[string]*Table)

		// First, build a map of tables to be dropped for quick lookup
		for _, table := range tablesToDrop {
			tableMap[table.name] = table
			tableDependencies[table.name] = []string{}
		}

		// Now build reverse dependencies
		// For each table, find which other tables (in the drop list) reference it
		for _, table := range tablesToDrop {
			for _, fk := range table.foreignKeys {
				if fk.referenceName != "" && fk.referenceName != table.name {
					// If the referenced table is also being dropped
					if _, exists := tableMap[fk.referenceName]; exists {
						// The referenced table depends on this table being dropped first
						tableDependencies[fk.referenceName] = append(tableDependencies[fk.referenceName], table.name)
					}
				}
			}
		}

		sorted := topologicalSort(tablesToDrop, tableDependencies, func(t *Table) string {
			return t.name
		})

		if len(sorted) != 0 {
			sortedTablesToDrop = sorted
		} else {
			// If circular dependency is detected, fall back to the original order.
			sortedTablesToDrop = tablesToDrop
		}
	}

	var ddls []string
	for _, table := range sortedTablesToDrop {
		ddls = append(ddls, fmt.Sprintf("DROP TABLE %s", g.escapeTableName(table.name)))
	}
	return ddls
}

func splitTableName(table string, defaultSchema string) (string, string) {
	schemaTable := strings.SplitN(table, ".", 2)
	if len(schemaTable) == 2 {
		return schemaTable[0], schemaTable[1]
	} else {
		return defaultSchema, table
	}
}

func isValidAlgorithm(algorithm string) bool {
	switch strings.ToUpper(algorithm) {
	case "INPLACE", "COPY", "INSTANT":
		return true
	default:
		return false
	}
}

func isValidLock(lock string) bool {
	switch strings.ToUpper(lock) {
	case "DEFAULT", "NONE", "SHARED", "EXCLUSIVE":
		return true
	default:
		return false
	}
}

// Escape a string and add quotes to form a legal SQL string constant.
func StringConstant(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
