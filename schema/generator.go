// TODO: Normalize implicit things in input first, and then compare
package schema

import (
	"fmt"
	"log"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/sqldef/sqldef/database"
)

type GeneratorMode int

const (
	GeneratorModeMysql = GeneratorMode(iota)
	GeneratorModePostgres
	GeneratorModeSQLite3
	GeneratorModeMssql
)

var (
	dataTypeAliases = map[string]string{
		"bool":    "boolean",
		"int":     "integer",
		"char":    "character",
		"varchar": "character varying",
	}
	mysqlDataTypeAliases = map[string]string{
		"boolean": "tinyint",
	}
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

	currentComments []*Comment

	desiredExtensions []*Extension
	currentExtensions []*Extension

	desiredSchemas []*Schema
	currentSchemas []*Schema

	defaultSchema string

	algorithm string
	lock      string
}

// Parse argument DDLs and call `generateDDLs()`
func GenerateIdempotentDDLs(mode GeneratorMode, sqlParser database.Parser, desiredSQL string, currentSQL string, config database.GeneratorConfig, defaultSchema string) ([]string, error) {
	// TODO: invalidate duplicated tables, columns
	desiredDDLs, err := ParseDDLs(mode, sqlParser, desiredSQL, defaultSchema)
	if err != nil {
		return nil, err
	}
	desiredDDLs = FilterTables(desiredDDLs, config)

	currentDDLs, err := ParseDDLs(mode, sqlParser, currentSQL, defaultSchema)
	if err != nil {
		return nil, err
	}
	currentDDLs = FilterTables(currentDDLs, config)

	tables, views, triggers, types, comments, extensions, schemas, err := aggregateDDLsToSchema(currentDDLs)
	if err != nil {
		return nil, err
	}

	generator := Generator{
		mode:              mode,
		desiredTables:     []*Table{},
		currentTables:     tables,
		desiredViews:      []*View{},
		currentViews:      views,
		desiredTriggers:   []*Trigger{},
		currentTriggers:   triggers,
		desiredTypes:      []*Type{},
		currentTypes:      types,
		currentComments:   comments,
		desiredExtensions: []*Extension{},
		currentExtensions: extensions,
		desiredSchemas:    []*Schema{},
		currentSchemas:    schemas,
		defaultSchema:     defaultSchema,
		algorithm:         config.Algorithm,
		lock:              config.Lock,
	}
	return generator.generateDDLs(desiredDDLs)
}

// Main part of DDL genearation
func (g *Generator) generateDDLs(desiredDDLs []DDL) ([]string, error) {
	// These variables are used to control the output order of the DDL.
	// `CREATE SCHEMA` should execute first, and DDLs that add indexes and foreign keys should execute last.
	// Other ddls are stored in interDDLs.
	createExtensionDDLs := []string{}
	createSchemaDDLs := []string{}
	interDDLs := []string{}
	indexDDLs := []string{}
	foreignKeyDDLs := []string{}

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
				interDDLs = append(interDDLs, tableDDLs...)
				mergeTable(currentTable, desired.table)
			} else {
				// Table not found, create table.
				interDDLs = append(interDDLs, desired.statement)
				table := desired.table // copy table
				g.currentTables = append(g.currentTables, &table)
			}
			table := desired.table // copy table
			g.desiredTables = append(g.desiredTables, &table)
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
		case *AddPolicy:
			policyDDLs, err := g.generateDDLsForCreatePolicy(desired.tableName, desired.policy, "CREATE POLICY", ddl.Statement())
			if err != nil {
				return nil, err
			}
			interDDLs = append(interDDLs, policyDDLs...)
		case *View:
			viewDDLs, err := g.generateDDLsForCreateView(desired.name, desired)
			if err != nil {
				return nil, err
			}
			interDDLs = append(interDDLs, viewDDLs...)
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
		default:
			return nil, fmt.Errorf("unexpected ddl type in generateDDLs: %v", desired)
		}
	}

	ddls := []string{}
	ddls = append(ddls, createExtensionDDLs...)
	ddls = append(ddls, createSchemaDDLs...)
	ddls = append(ddls, interDDLs...)
	ddls = append(ddls, indexDDLs...)
	ddls = append(ddls, foreignKeyDDLs...)

	// Clean up obsoleted tables, indexes, columns
	for _, currentTable := range g.currentTables {
		desiredTable := findTableByName(g.desiredTables, currentTable.name)
		if desiredTable == nil {
			// Obsoleted table found. Drop table.
			ddls = append(ddls, fmt.Sprintf("DROP TABLE %s", g.escapeTableName(currentTable.name)))
			g.currentTables = removeTableByName(g.currentTables, currentTable.name)
			continue
		}

		// Table is expected to exist. Drop foreign keys prior to index deletion
		for _, foreignKey := range currentTable.foreignKeys {
			if containsString(convertForeignKeysToConstraintNames(desiredTable.foreignKeys), foreignKey.constraintName) {
				continue // Foreign key is expected to exist.
			}

			// The foreign key seems obsoleted. Check and drop it as needed.
			foreignKeyDDLs := g.generateDDLsForAbsentForeignKey(foreignKey, *currentTable, *desiredTable)
			ddls = append(ddls, foreignKeyDDLs...)
			// TODO: simulate to remove foreign key from `currentTable.foreignKeys`?
		}

		// Check indexes
		for _, index := range currentTable.indexes {

			// Alter statement for primary key index should be generated above.
			if index.primary {
				continue
			}

			if containsString(convertIndexesToIndexNames(desiredTable.indexes), index.name) ||
				containsString(convertForeignKeysToIndexNames(desiredTable.foreignKeys), index.name) {
				continue // Index is expected to exist.
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
			if containsString(convertColumnsToColumnNames(desiredTable.columns), column.name) {
				continue // Column is expected to exist.
			}

			// Column is obsoleted. Drop column.
			columnDDLs := g.generateDDLsForAbsentColumn(currentTable, column.name)
			ddls = append(ddls, columnDDLs...)
			// TODO: simulate to remove column from `currentTable.columns`?
		}

		// Check policies.
		for _, policy := range currentTable.policies {
			if containsString(convertPolicyNames(desiredTable.policies), policy.name) {
				continue
			}
			ddls = append(ddls, fmt.Sprintf("DROP POLICY %s ON %s", g.escapeSQLName(policy.name), g.escapeTableName(currentTable.name)))
		}

		// Check checks.
		for _, check := range currentTable.checks {
			if containsString(convertCheckConstraintNames(desiredTable.checks), check.constraintName) {
				continue
			}
			if g.mode != GeneratorModeMysql { // workaround. inline CHECK should be converted to out-of-place CONSTRAINT to fix this.
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(currentTable.name), g.escapeSQLName(check.constraintName)))
			}
		}
	}

	// Clean up obsoleted views
	for _, currentView := range g.currentViews {
		if containsString(convertViewNames(g.desiredViews), currentView.name) {
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
		if containsString(convertExtensionNames(g.desiredExtensions), currentExtension.extension.Name) {
			continue
		}
		ddls = append(ddls, fmt.Sprintf("DROP EXTENSION %s", g.escapeSQLName(currentExtension.extension.Name)))
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

	// Examine each column
	for i, desiredColumn := range desired.table.columns {
		currentColumn := findColumnByName(currentTable.columns, desiredColumn.name)
		if currentColumn == nil || !currentColumn.autoIncrement {
			// We may not be able to add AUTO_INCREMENT yet. It will be added after adding keys (primary or not) at the "Add new AUTO_INCREMENT" place.
			desiredColumn.autoIncrement = false
		}
		if currentColumn == nil {
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
				if i > 0 {
					after = " AFTER " + g.escapeSQLName(desired.table.columns[i-1].name)
				}
				ddl += after
			}

			ddls = append(ddls, ddl)
		} else {
			// Change column data type or order as needed.
			switch g.mode {
			case GeneratorModeMysql:
				currentPos := currentColumn.position
				desiredPos := desiredColumn.position
				changeOrder := currentPos > desiredPos && currentPos-desiredPos > len(currentTable.columns)-len(desired.table.columns)

				// Change column type and orders, *except* AUTO_INCREMENT and UNIQUE KEY.
				if !g.haveSameColumnDefinition(*currentColumn, desiredColumn) || !g.areSameDefaultValue(currentColumn.defaultDef, desiredColumn.defaultDef) || !g.areSameGenerated(currentColumn.generated, desiredColumn.generated) || changeOrder {
					definition, err := g.generateColumnDefinition(desiredColumn, false)
					if err != nil {
						return ddls, err
					}

					if desiredColumn.generated != nil {
						ddl1 := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentColumn.name))
						ddl2 := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", g.escapeTableName(desired.table.name), definition)
						after := " FIRST"
						if i > 0 {
							after = " AFTER " + g.escapeSQLName(desired.table.columns[i-1].name)
						}
						ddl2 += after
						ddls = append(ddls, ddl1, ddl2)
					} else {
						ddl := fmt.Sprintf("ALTER TABLE %s CHANGE COLUMN %s %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentColumn.name), definition)
						if changeOrder {
							after := " FIRST"
							if i > 0 {
								after = " AFTER " + g.escapeSQLName(desired.table.columns[i-1].name)
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
					ddl := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentColumn.name), generateDataType(desiredColumn))
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
				if !g.areSameDefaultValue(currentColumn.defaultDef, desiredColumn.defaultDef) {
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

				_, tableName := splitTableName(desired.table.name, g.defaultSchema)
				constraintName := fmt.Sprintf("%s_%s_check", tableName, desiredColumn.name)
				if desiredColumn.check != nil && desiredColumn.check.constraintName != "" {
					constraintName = desiredColumn.check.constraintName
				}

				columnChecks := []CheckDefinition{}
				for _, column := range currentTable.columns {
					if column.check != nil {
						columnChecks = append(columnChecks, *column.check)
					}
				}

				currentCheck := findCheckByName(columnChecks, constraintName)
				if !areSameCheckDefinition(currentCheck, desiredColumn.check) { // || currentColumn.checkNoInherit != desiredColumn.checkNoInherit {
					if currentCheck != nil {
						ddl := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(desired.table.name), constraintName)
						ddls = append(ddls, ddl)
					}
					if desiredColumn.check != nil {
						ddl := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)", g.escapeTableName(desired.table.name), constraintName, desiredColumn.check.definition)
						if desiredColumn.check.noInherit {
							ddl += " NO INHERIT"
						}
						ddls = append(ddls, ddl)
					}
				}

				// TODO: support adding a column's `references`
			case GeneratorModeMssql:
				if !g.haveSameDataType(*currentColumn, desiredColumn) {
					// Change type
					ddl := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentColumn.name), generateDataType(desiredColumn))
					ddls = append(ddls, ddl)
				}

				if !areSameCheckDefinition(currentColumn.check, desiredColumn.check) {
					_, tableName := splitTableName(desired.table.name, g.defaultSchema)
					constraintName := fmt.Sprintf("%s_%s_check", tableName, desiredColumn.name)
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
						ddl := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK%s (%s)", g.escapeTableName(desired.table.name), desiredConstraintName, replicationDefinition, desiredColumn.check.definition)
						ddls = append(ddls, ddl)
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
				if !g.areSameDefaultValue(currentColumn.defaultDef, desiredColumn.defaultDef) {
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
				definition, err := g.generateColumnDefinition(currentColumn, false)
				if err != nil {
					return ddls, err
				}
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s CHANGE COLUMN %s %s", g.escapeTableName(currentTable.name), g.escapeSQLName(currentColumn.name), definition))
			}
		}
	}

	// Examine primary key
	if primaryKeysChanged {
		if currentPrimaryKey != nil {
			switch g.mode {
			case GeneratorModeMysql:
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP PRIMARY KEY", g.escapeTableName(desired.table.name)))
			case GeneratorModePostgres:
				tableName := strings.SplitN(desired.table.name, ".", 2)[1] // without schema
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(desired.table.name), g.escapeSQLName(tableName+"_pkey")))
			case GeneratorModeMssql:
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentPrimaryKey.name)))
			default:
			}
		}
		if desiredPrimaryKey != nil {
			ddls = append(ddls, g.generateAddIndex(desired.table.name, *desiredPrimaryKey))
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
			// Index not found, add index.
			ddls = append(ddls, g.generateAddIndex(desired.table.name, desiredIndex))
		}
	}

	// Add new AUTO_INCREMENT after adding index and primary key
	if g.mode == GeneratorModeMysql {
		for _, desiredColumn := range desired.table.columns {
			currentColumn := findColumnByName(currentTable.columns, desiredColumn.name)
			if desiredColumn.autoIncrement && (primaryKeysChanged || currentColumn == nil || !currentColumn.autoIncrement) {
				definition, err := g.generateColumnDefinition(desiredColumn, false)
				if err != nil {
					return ddls, err
				}
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s CHANGE COLUMN %s %s", g.escapeTableName(currentTable.name), g.escapeSQLName(desiredColumn.name), definition))
			}
		}
	}

	// Examine each foreign key
	for _, desiredForeignKey := range desired.table.foreignKeys {
		if len(desiredForeignKey.constraintName) == 0 && g.mode != GeneratorModeSQLite3 {
			return ddls, fmt.Errorf(
				"Foreign key without constraint symbol was found in table '%s' (index name: '%s', columns: %v). "+
					"Specify the constraint symbol to identify the foreign key.",
				desired.table.name, desiredForeignKey.indexName, desiredForeignKey.indexColumns,
			)
		}

		if currentForeignKey := findForeignKeyByName(currentTable.foreignKeys, desiredForeignKey.constraintName); currentForeignKey != nil {
			// Drop and add foreign key as needed.
			if !g.areSameForeignKeys(*currentForeignKey, desiredForeignKey) {
				var dropDDL string
				switch g.mode {
				case GeneratorModeMysql:
					dropDDL = fmt.Sprintf("ALTER TABLE %s DROP FOREIGN KEY %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentForeignKey.constraintName))
				case GeneratorModePostgres, GeneratorModeMssql:
					dropDDL = fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentForeignKey.constraintName))
				default:
				}
				if dropDDL != "" {
					ddls = append(ddls, dropDDL, fmt.Sprintf("ALTER TABLE %s ADD %s%s", g.escapeTableName(desired.table.name), g.generateForeignKeyDefinition(desiredForeignKey), g.generateConstraintOptions(desiredForeignKey.constraintOptions)))
				}
			}
		} else {
			// Foreign key not found, add foreign key.
			definition := g.generateForeignKeyDefinition(desiredForeignKey)
			ddl := fmt.Sprintf("ALTER TABLE %s ADD %s", g.escapeTableName(desired.table.name), definition)
			ddls = append(ddls, ddl)
		}
	}

	// Examine each check
	for _, desiredCheck := range desired.table.checks {
		if currentCheck := findCheckByName(currentTable.checks, desiredCheck.constraintName); currentCheck != nil {
			if !areSameCheckDefinition(currentCheck, &desiredCheck) {
				switch g.mode {
				case GeneratorModePostgres:
					ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(desired.table.name), g.escapeSQLName(currentCheck.constraintName)))
					ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)", g.escapeTableName(desired.table.name), g.escapeSQLName(desiredCheck.constraintName), desiredCheck.definition))
				default:
				}
			}
		} else {
			ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)", g.escapeTableName(desired.table.name), g.escapeSQLName(desiredCheck.constraintName), desiredCheck.definition))
		}
	}

	// Examine table comment
	if currentTable.options["comment"] != desired.table.options["comment"] {
		ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s COMMENT = %s", g.escapeTableName(desired.table.name), desired.table.options["comment"]))
	}

	return ddls, nil
}

// Shared by `CREATE INDEX` and `ALTER TABLE ADD INDEX`.
// This manages `g.currentTables` unlike `generateDDLsForCreateTable`...
func (g *Generator) generateDDLsForCreateIndex(tableName string, desiredIndex Index, action string, statement string) ([]string, error) {
	ddls := []string{}

	currentTable := findTableByName(g.currentTables, tableName)
	if currentTable == nil { // Views
		currentView := findViewByName(g.currentViews, tableName)
		currentIndex := findIndexByName(currentView.indexes, desiredIndex.name)
		if currentIndex == nil {
			// Index not found, add index.
			ddls = append(ddls, statement)
			currentView.indexes = append(currentView.indexes, desiredIndex)
		}
		return ddls, nil
	}

	currentIndex := findIndexByName(currentTable.indexes, desiredIndex.name)
	if currentIndex == nil {
		// Index not found, add index.
		ddls = append(ddls, statement)
		currentTable.indexes = append(currentTable.indexes, desiredIndex)
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
	desiredTable.indexes = append(desiredTable.indexes, desiredIndex)

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
	if containsString(convertForeignKeysToConstraintNames(desiredTable.foreignKeys), desiredForeignKey.constraintName) {
		return nil, fmt.Errorf("index '%s' is doubly created against table '%s': '%s'", desiredForeignKey.constraintName, tableName, statement)
	}
	desiredTable.foreignKeys = append(desiredTable.foreignKeys, desiredForeignKey)

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
	if containsString(convertPolicyNames(desiredTable.policies), desiredPolicy.name) {
		return nil, fmt.Errorf("policy '%s' is doubly created against table '%s': '%s'", desiredPolicy.name, tableName, statement)
	}
	desiredTable.policies = append(desiredTable.policies, desiredPolicy)

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
		// If columns are added, be sure to DROP and CREATE.
		if len(currentView.columns) > len(desiredView.columns) {
			return true
		}

		// If all existing columns are identical and only a new column is added, use REPLACE; otherwise, execute DROP and CREATE.
		return !reflect.DeepEqual(currentView.columns, desiredView.columns[:len(currentView.columns)])
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
		g.currentViews = append(g.currentViews, &view)
	} else if desiredView.viewType == "VIEW" { // TODO: Fix the definition comparison for materialized views and enable this
		// View found. If it's different, create or replace view.
		if g.normalizeViewDefinition(currentView.definition) != g.normalizeViewDefinition(desiredView.definition) {
			if g.shouldDropAndCreateView(currentView, desiredView) {
				ddls = append(ddls, fmt.Sprintf("DROP %s %s", desiredView.viewType, g.escapeTableName(viewName)))
				ddls = append(ddls, fmt.Sprintf("CREATE %s %s AS %s", desiredView.viewType, g.escapeTableName(viewName), desiredView.definition))
			} else {
				ddls = append(ddls, fmt.Sprintf("CREATE OR REPLACE %s %s AS %s", desiredView.viewType, g.escapeTableName(viewName), desiredView.definition))
			}
		}
	} else if desiredView.viewType == "SQL SECURITY" {
		// VIEW with the specified security type found. If it's different, create or replace view.
		if g.normalizeViewDefinition(currentView.securityType) != g.normalizeViewDefinition(desiredView.securityType) {
			ddls = append(ddls, fmt.Sprintf("CREATE OR REPLACE SQL SECURITY %s VIEW %s AS %s", desiredView.securityType, g.escapeTableName(viewName), desiredView.definition))
		}
	}

	// Examine policies in desiredTable to delete obsoleted policies later
	if containsString(convertViewNames(g.desiredViews), desiredView.name) {
		return nil, fmt.Errorf("view '%s' is doubly created: '%s'", desiredView.name, desiredView.statement)
	}
	g.desiredViews = append(g.desiredViews, desiredView)

	return ddls, nil
}

// Workaround for: jsonb_extract_path_text(payload, array['amount'])
// generated by jsonb_extract_path_text(payload, 'amount')
// and collate, etc.
func (g *Generator) normalizeViewDefinition(definition string) string {
	definition = strings.ToLower(definition)
	if g.mode == GeneratorModePostgres {
		definition = strings.ReplaceAll(definition, "array[", "")
		definition = strings.ReplaceAll(definition, "]", "")
		definition = strings.ReplaceAll(definition, "collate", "")
		definition = strings.ReplaceAll(definition, "  ", "")
	}
	return definition
}

func (g *Generator) generateDDLsForCreateTrigger(triggerName string, desiredTrigger *Trigger) ([]string, error) {
	var ddls []string
	currentTrigger := findTriggerByName(g.currentTriggers, triggerName)

	var triggerDefinition string
	switch g.mode {
	case GeneratorModeMssql:
		triggerDefinition += fmt.Sprintf("TRIGGER %s ON %s %s %s AS\n%s", g.escapeSQLName(desiredTrigger.name), g.escapeTableName(desiredTrigger.tableName), desiredTrigger.time, strings.Join(desiredTrigger.event, ", "), strings.Join(desiredTrigger.body, "\n"))
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

	g.desiredTriggers = append(g.desiredTriggers, desiredTrigger)

	return ddls, nil
}

func (g *Generator) generateDDLsForCreateType(desired *Type) ([]string, error) {
	ddls := []string{}

	if currentType := findTypeByName(g.currentTypes, desired.name); currentType != nil {
		// Type found. Add values if not present.
		if currentType.enumValues != nil && len(currentType.enumValues) < len(desired.enumValues) {
			for _, enumValue := range desired.enumValues {
				if !containsString(currentType.enumValues, enumValue) {
					ddl := fmt.Sprintf("ALTER TYPE %s ADD VALUE %s", currentType.name, enumValue)
					ddls = append(ddls, ddl)
				}
			}
		}
	} else {
		// Type not found, add type.
		ddls = append(ddls, desired.statement)
	}
	g.desiredTypes = append(g.desiredTypes, desired)

	return ddls, nil
}

func (g *Generator) generateDDLsForComment(desired *Comment) ([]string, error) {
	ddls := []string{}

	currentComment := findCommentByObject(g.currentComments, desired.comment.Object)
	if currentComment == nil || currentComment.comment.Comment != desired.comment.Comment {
		// Comment not found, add comment.
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

	g.desiredExtensions = append(g.desiredExtensions, desired)

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

	g.desiredSchemas = append(g.desiredSchemas, desired)

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
				referencesColumn = &column
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
				primaryKeyColumn = &column
				break
			}
		}

		if primaryKeyColumn == nil {
			// If nil, it will be `DROP COLUMN`-ed and we can usually ignore it.
			// However, it seems like you need to explicitly drop it first for MSSQL.
			if g.mode == GeneratorModeMssql && (primaryKeyColumn == nil || primaryKeyColumn.name != currentIndex.columns[0].column) {
				ddls = append(ddls, fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", g.escapeTableName(currentTable.name), g.escapeSQLName(currentIndex.name)))
			}
		} else if primaryKeyColumn.name != currentIndex.columns[0].column { // TODO: check length of currentIndex.columns
			// TODO: handle this. Rename primary key column...?
			return ddls, fmt.Errorf(
				"primary key column name of '%s' should be '%s' but currently '%s'. This is not handled yet.",
				currentTable.name, primaryKeyColumn.name, currentIndex.columns[0].column,
			)
		}
	} else if currentIndex.unique {
		var uniqueKeyColumn *Column
		// Columns become empty if the index is a PostgreSQL's expression index.
		if len(currentIndex.columns) > 0 {
			for _, column := range desiredTable.columns {
				if column.name == currentIndex.columns[0].column && column.keyOption.isUnique() {
					uniqueKeyColumn = &column
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

func generateDataType(column Column) string {
	suffix := ""
	if column.timezone {
		suffix += " WITH TIME ZONE"
	}
	if column.array {
		suffix += "[]"
	}

	if column.displayWidth != nil {
		return fmt.Sprintf("%s(%s)%s", column.typeName, string(column.displayWidth.raw), suffix)
	} else if column.length != nil {
		if column.scale != nil {
			return fmt.Sprintf("%s(%s, %s)%s", column.typeName, string(column.length.raw), string(column.scale.raw), suffix)
		} else {
			return fmt.Sprintf("%s(%s)%s", column.typeName, string(column.length.raw), suffix)
		}
	} else {
		switch column.typeName {
		case "enum", "set":
			return fmt.Sprintf("%s(%s)%s", column.typeName, strings.Join(column.enumValues, ", "), suffix)
		default:
			return fmt.Sprintf("%s%s", column.typeName, suffix)
		}
	}
}

func (g *Generator) generateColumnDefinition(column Column, enableUnique bool) (string, error) {
	// TODO: make string concatenation faster?

	definition := fmt.Sprintf("%s %s ", g.escapeSQLName(column.name), generateDataType(column))

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
		definition += fmt.Sprintf("ON UPDATE %s ", string(column.onUpdate.raw))
	}

	if column.comment != nil {
		// TODO: Should this use StringConstant?
		definition += fmt.Sprintf("COMMENT '%s' ", string(column.comment.raw))
	}

	if column.check != nil {
		definition += "CHECK "
		if column.check.notForReplication {
			definition += "NOT FOR REPLICATION "
		}
		definition += fmt.Sprintf("(%s) ", column.check.definition)
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
		column := g.escapeSQLName(indexColumn.column)
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
		if strings.ToUpper(index.indexType) == "UNIQUE KEY" {
			ddl += "CONSTRAINT"
		} else {
			ddl += strings.ToUpper(index.indexType)
		}
		if !index.primary {
			ddl += fmt.Sprintf(" %s", g.escapeSQLName(index.name))
		}
		if strings.ToUpper(index.indexType) == "UNIQUE KEY" {
			ddl += " UNIQUE"
		}
		constraintOptions := g.generateConstraintOptions(index.constraintOptions)
		ddl += fmt.Sprintf(" (%s)%s%s", strings.Join(columns, ", "), optionDefinition, constraintOptions)
		return ddl
	default:
		ddl := fmt.Sprintf(
			"ALTER TABLE %s ADD %s",
			g.escapeTableName(table),
			strings.ToUpper(index.indexType),
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
			indexOption := indexOptions[0]
			if indexOption.optionName == "parser" {
				indexOption.optionName = "WITH " + indexOption.optionName
			}
			optionDefinition = fmt.Sprintf(" %s %s", indexOption.optionName, string(indexOption.value.raw))
		case GeneratorModeMssql:
			options := []string{}
			for _, indexOption := range indexOptions {
				var optionValue string
				switch indexOption.value.valueType {
				case ValueTypeBool:
					if string(indexOption.value.raw) == "true" {
						optionValue = "ON"
					} else {
						optionValue = "OFF"
					}
				default:
					optionValue = string(indexOption.value.raw)
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
		return fmt.Sprintf("DROP INDEX %s ON %s", g.escapeSQLName(indexName), g.escapeTableName(tableName))
	case GeneratorModeSQLite3:
		return fmt.Sprintf("DROP INDEX %s", g.escapeSQLName(indexName))
	default:
		return ""
	}
}

func (g *Generator) escapeTableName(name string) string {
	switch g.mode {
	case GeneratorModePostgres, GeneratorModeMssql:
		schemaTable := strings.SplitN(name, ".", 2)
		var schemaName, tableName string
		if len(schemaTable) == 1 {
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
		return fmt.Sprintf("\"%s\"", name)
	case GeneratorModeMssql:
		return fmt.Sprintf("[%s]", name)
	default:
		return fmt.Sprintf("`%s`", name)
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

func isPrimaryKey(column Column, table Table) bool {
	if column.keyOption == ColumnKeyPrimary {
		return true
	}

	for _, index := range table.indexes {
		if index.primary {
			for _, indexColumn := range index.columns {
				if indexColumn.column == column.name {
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

func aggregateDDLsToSchema(ddls []DDL) ([]*Table, []*View, []*Trigger, []*Type, []*Comment, []*Extension, []*Schema, error) {
	var tables []*Table
	var views []*View
	var triggers []*Trigger
	var types []*Type
	var comments []*Comment
	var extensions []*Extension
	var schemas []*Schema
	for _, ddl := range ddls {
		switch stmt := ddl.(type) {
		case *CreateTable:
			table := stmt.table // copy table
			tables = append(tables, &table)
		case *CreateIndex:
			table := findTableByName(tables, stmt.tableName)
			if table == nil {
				view := findViewByName(views, stmt.tableName)
				if view == nil {
					return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("CREATE INDEX is performed before CREATE TABLE: %s", ddl.Statement())
				}
				// TODO: check duplicated creation
				view.indexes = append(view.indexes, stmt.index)
			} else {
				// TODO: check duplicated creation
				table.indexes = append(table.indexes, stmt.index)
			}
		case *AddIndex:
			table := findTableByName(tables, stmt.tableName)
			if table == nil {
				return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("ADD INDEX is performed before CREATE TABLE: %s", ddl.Statement())
			}
			// TODO: check duplicated creation
			table.indexes = append(table.indexes, stmt.index)
		case *AddPrimaryKey:
			table := findTableByName(tables, stmt.tableName)
			if table == nil {
				return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("ADD PRIMARY KEY is performed before CREATE TABLE: %s", ddl.Statement())
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
				return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("ADD FOREIGN KEY is performed before CREATE TABLE: %s", ddl.Statement())
			}

			table.foreignKeys = append(table.foreignKeys, stmt.foreignKey)
		case *AddPolicy:
			table := findTableByName(tables, stmt.tableName)
			if table == nil {
				return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("ADD POLICY performed before CREATE TABLE: %s", ddl.Statement())
			}

			table.policies = append(table.policies, stmt.policy)
		case *View:
			views = append(views, stmt)
		case *Trigger:
			triggers = append(triggers, stmt)
		case *Type:
			types = append(types, stmt)
		case *Comment:
			comments = append(comments, stmt)
		case *Extension:
			extensions = append(extensions, stmt)
		case *Schema:
			schemas = append(schemas, stmt)
		default:
			return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("unexpected ddl type in convertDDLsToTablesAndViews: %#v", stmt)
		}
	}
	return tables, views, triggers, types, comments, extensions, schemas, nil
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

func findIndexOptionByName(options []IndexOption, name string) *IndexOption {
	for _, option := range options {
		if option.optionName == name {
			return &option
		}
	}
	return nil
}

func findCheckByName(checks []CheckDefinition, name string) *CheckDefinition {
	for _, check := range checks {
		if check.constraintName == name {
			return &check
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
	if g.normalizeDataType(current.typeName) != g.normalizeDataType(desired.typeName) {
		return false
	}
	if !reflect.DeepEqual(current.enumValues, desired.enumValues) {
		return false
	}
	if current.length == nil && desired.length != nil || current.length != nil && desired.length == nil {
		return false
	}
	if current.length != nil && desired.length != nil && current.length.intVal != desired.length.intVal {
		return false
	}
	if current.scale == nil && (desired.scale != nil && desired.scale.intVal != 0) || (current.scale != nil && current.scale.intVal != 0) && desired.scale == nil {
		return false
	}
	if current.scale != nil && desired.scale != nil && current.scale.intVal != desired.scale.intVal {
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

func areSameCheckDefinition(checkA *CheckDefinition, checkB *CheckDefinition) bool {
	if checkA == nil && checkB == nil {
		return true
	}
	if checkA == nil || checkB == nil {
		return false
	}
	return checkA.definition == checkB.definition &&
		checkA.notForReplication == checkB.notForReplication &&
		checkA.noInherit == checkB.noInherit
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

func (g *Generator) areSameDefaultValue(currentDefault *DefaultDefinition, desiredDefault *DefaultDefinition) bool {
	var currentVal *Value
	var desiredVal *Value
	if currentDefault != nil && !isNullValue(currentDefault.value) {
		currentVal = currentDefault.value
	}
	if desiredDefault != nil && !isNullValue(desiredDefault.value) {
		desiredVal = desiredDefault.value
	}
	if !g.areSameValue(currentVal, desiredVal) {
		return false
	}

	var currentExprSchema, currentExpr string
	var desiredExprSchema, desiredExpr string
	if currentDefault != nil {
		currentExprSchema, currentExpr = splitTableName(currentDefault.expression, g.defaultSchema)
	}
	if desiredDefault != nil {
		desiredExprSchema, desiredExpr = splitTableName(desiredDefault.expression, g.defaultSchema)
	}
	return strings.ToLower(currentExprSchema) == strings.ToLower(desiredExprSchema) && strings.ToLower(currentExpr) == strings.ToLower(desiredExpr)
}

func (g *Generator) areSameValue(current, desired *Value) bool {
	if current == nil && desired == nil {
		return true
	}
	if current == nil || desired == nil {
		return false
	}

	// NOTE: -1 can be changed to '-1' in show create table and valueType is not reliable
	currentRaw := strings.ToLower(string(current.raw))
	desiredRaw := strings.ToLower(string(desired.raw))
	if desired.valueType == ValueTypeFloat && len(currentRaw) > len(desiredRaw) {
		// Round "0.00" to "0.0" for comparison with desired.
		// Ideally we should do this seeing precision in a data type.
		currentRaw = currentRaw[0:len(desiredRaw)]
	}

	// NOTE: Boolean constants is evaluated as TINYINT(1) value in MySQL.
	if g.mode == GeneratorModeMysql {
		if desired.valueType == ValueTypeBool {
			if strings.ToLower(string(desired.raw)) == "false" {
				desiredRaw = "0"
			} else if strings.ToLower(string(desired.raw)) == "true" {
				desiredRaw = "1"
			}
		}
	}

	return currentRaw == desiredRaw
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
		bodyA := strings.ToLower(strings.Replace(triggerA.body[i], " ", "", -1))
		bodyB := strings.ToLower(strings.Replace(triggerB.body[i], " ", "", -1))
		if bodyA != bodyB {
			return false
		}
	}
	return true
}

func isNullValue(value *Value) bool {
	return value != nil && value.valueType == ValueTypeValArg && string(value.raw) == "null"
}

func (g *Generator) normalizeDataType(dataType string) string {
	alias, ok := dataTypeAliases[dataType]
	if ok {
		dataType = alias
	}
	if g.mode == GeneratorModeMysql {
		alias, ok = mysqlDataTypeAliases[dataType]
		if ok {
			dataType = alias
		}
	}
	return dataType
}

func (g *Generator) areSamePrimaryKeys(primaryKeyA *Index, primaryKeyB *Index) bool {
	if primaryKeyA != nil && primaryKeyB != nil {
		return g.areSameIndexes(*primaryKeyA, *primaryKeyB)
	} else {
		return primaryKeyA == nil && primaryKeyB == nil
	}
}

func (g *Generator) areSameIndexes(indexA Index, indexB Index) bool {
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
		if indexAColumn.direction == "" {
			indexAColumn.direction = AscScr
		}
		if indexB.columns[i].direction == "" {
			indexB.columns[i].direction = AscScr
		}
		// TODO: check length?
		if g.normalizeIndexColumn(indexA.columns[i].column) != g.normalizeIndexColumn(indexB.columns[i].column) ||
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

	indexAOptions := indexA.options
	indexBOptions := indexB.options
	// Mysql: Default Index B-Tree
	if g.mode == GeneratorModeMysql {
		if len(indexAOptions) == 0 {
			indexAOptions = []IndexOption{{optionName: "using", value: &Value{valueType: ValueTypeStr, raw: []byte("btree"), strVal: "btree"}}}
		}
		if len(indexBOptions) == 0 {
			indexBOptions = []IndexOption{{optionName: "using", value: &Value{valueType: ValueTypeStr, raw: []byte("btree"), strVal: "btree"}}}
		}
	}
	for _, optionB := range indexBOptions {
		if optionA := findIndexOptionByName(indexAOptions, optionB.optionName); optionA != nil {
			if !g.areSameValue(optionA.value, optionB.value) {
				return false
			}
		} else {
			return false
		}
	}

	// Specific to unique constraints
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

	return true
}

// jsonb_extract_path_text(col, ARRAY['foo', 'bar']) => jsonb_extract_path_text(col, 'foo', 'bar')
func (g *Generator) normalizeIndexColumn(column string) string {
	column = strings.ToLower(column)
	if g.mode == GeneratorModePostgres {
		column = strings.ReplaceAll(column, "array[", "")
		column = strings.ReplaceAll(column, "]", "")
	}
	return column
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
	// TODO: check index, reference
	return true
}

func areSamePolicies(policyA, policyB Policy) bool {
	if strings.ToLower(policyA.scope) != strings.ToLower(policyB.scope) {
		return false
	}
	if strings.ToLower(policyA.permissive) != strings.ToLower(policyB.permissive) {
		return false
	}
	if normalizeUsing(policyA.using) != normalizeUsing(policyB.using) {
		return fmt.Sprintf("(%s)", policyA.using) == policyB.using
	}
	if strings.ToLower(policyA.withCheck) != strings.ToLower(policyB.withCheck) {
		return fmt.Sprintf("(%s)", policyA.withCheck) == policyB.withCheck
	}
	if len(policyA.roles) != len(policyB.roles) {
		return false
	}
	sort.Slice(policyA.roles, func(i, j int) bool {
		return policyA.roles[i] <= policyA.roles[j]
	})
	sort.Slice(policyB.roles, func(i, j int) bool {
		return policyB.roles[i] <= policyB.roles[j]
	})
	for i := range policyA.roles {
		if strings.ToLower(policyA.roles[i]) != strings.ToLower(policyB.roles[i]) {
			return false
		}
	}
	return true
}

// Workaround for: ((current_schema())::uuid = (current_database())::uuid)
// generated by (current_schema()::uuid = current_database()::uuid)
func normalizeUsing(expr string) string {
	expr = strings.ToLower(expr)
	expr = strings.ReplaceAll(expr, "(", "")
	expr = strings.ReplaceAll(expr, ")", "")
	return expr
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

func convertPolicyNames(policies []Policy) []string {
	policyNames := make([]string, len(policies))
	for i, policy := range policies {
		policyNames[i] = policy.name
	}
	return policyNames
}

func convertCheckConstraintNames(checks []CheckDefinition) []string {
	checkConstraintNames := make([]string, len(checks))
	for i, check := range checks {
		checkConstraintNames[i] = check.constraintName
	}
	return checkConstraintNames
}

func convertViewNames(views []*View) []string {
	viewNames := make([]string, len(views))
	for i, view := range views {
		viewNames[i] = view.name
	}
	return viewNames
}

func convertExtensionNames(extensions []*Extension) []string {
	extensionNames := make([]string, len(extensions))
	for i, extension := range extensions {
		extensionNames[i] = extension.extension.Name
	}
	return extensionNames
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
	if defaultDefinition.value != nil {
		defaultVal := defaultDefinition.value
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
			return fmt.Sprintf("DEFAULT %s", string(defaultVal.raw)), nil
		default:
			return "", fmt.Errorf("unsupported default value type (valueType: '%d')", defaultVal.valueType)
		}
	} else if defaultDefinition.expression != "" {
		if g.mode == GeneratorModeMysql || g.mode == GeneratorModeSQLite3 {
			// Enclose expression with parentheses to avoid syntax error
			// https://dev.mysql.com/doc/refman/8.0/en/data-type-defaults.html#data-type-defaults-explicit
			// https://www.sqlite.org/syntax/column-constraint.html
			return fmt.Sprintf("DEFAULT(%s)", defaultDefinition.expression), nil
		} else {
			return fmt.Sprintf("DEFAULT %s", defaultDefinition.expression), nil
		}
	}
	return "", fmt.Errorf("default value is not set")
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

func containsRegexpString(strs []string, str string) bool {
	for _, s := range strs {
		if regexp.MustCompile("^" + s + "$").MatchString(str) {
			return true
		}
	}
	return false
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
