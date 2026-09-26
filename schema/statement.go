package schema

import (
	"fmt"
	"slices"
	"strings"

	"github.com/sqldef/sqldef/v3/parser"
	"github.com/sqldef/sqldef/v3/util"
)

// statement is one DDL statement the generator emits. It knows what it does, so nothing
// downstream has to recover that from the SQL text.
type statement interface {
	// Render returns the SQL, commented out when the statement is skipped.
	Render() string
	// Destructive reports whether enable_drop has to allow the statement.
	Destructive() bool
	// Transactional reports whether the statement may run inside a transaction.
	Transactional() bool
	// Skipped reports whether the statement is emitted only as a comment.
	Skipped() bool
}

// statementDefaults is what a statement is unless it says otherwise: not destructive,
// transactional and not skipped.
type statementDefaults struct{}

func (statementDefaults) Destructive() bool   { return false }
func (statementDefaults) Transactional() bool { return true }
func (statementDefaults) Skipped() bool       { return false }

// rawStatement is SQL the generator has not typed yet. The text-based enable_drop gate
// still runs on it after rendering.
type rawStatement string

func (s rawStatement) Render() string      { return string(s) }
func (s rawStatement) Destructive() bool   { return false }
func (s rawStatement) Transactional() bool { return true }
func (s rawStatement) Skipped() bool       { return false }

func rawStatements(ddls []string) []statement {
	return util.TransformSlice(ddls, func(ddl string) statement { return rawStatement(ddl) })
}

func renderStatements(statements []statement) []string {
	return util.TransformSlice(statements, statement.Render)
}

// skipped is a statement enable_drop holds back. It is still emitted, as a comment, so that
// --dry-run shows what --enable-drop would run.
type skipped struct {
	statement
}

func (s skipped) Render() string { return skippedStatement(s.statement.Render()) }
func (s skipped) Skipped() bool  { return true }

// recreate drops an object and brings it back. appendRecreateStatements emits it.
type recreate struct {
	statements []statement
}

// alterTarget is the table an ALTER TABLE changes. The key identifies the table however its
// name is quoted, so that the statements of one table can be bundled; the name renders it.
type alterTarget struct {
	name QualifiedName
	key  string
}

// algorithmLock renders the MySQL ALGORITHM and LOCK clauses an ALTER TABLE ends with.
type algorithmLock struct {
	algorithm string
	lock      string
}

func (a algorithmLock) render() string {
	var clauses string
	if isValidAlgorithm(a.algorithm) {
		clauses += ", ALGORITHM=" + strings.ToUpper(a.algorithm)
	}
	if isValidLock(a.lock) {
		clauses += ", LOCK=" + strings.ToUpper(a.lock)
	}
	return clauses
}

// alterTableStatement is an ALTER TABLE the generator builds. --bulk-alter fuses the
// actions of one table into a single statement.
type alterTableStatement struct {
	statementDefaults
	d       dialect
	table   alterTarget
	actions []alterTableAction
	algorithmLock
}

func (s *alterTableStatement) Render() string {
	actions := util.TransformSlice(s.actions, func(action alterTableAction) string { return action.render(s.d) })
	ddl := "ALTER TABLE " + s.d.escapeQualifiedName(s.table.name) + " " + strings.Join(actions, ", ")
	if !s.standalone() {
		ddl += s.algorithmLock.render()
	}
	return ddl
}

// standalone reports whether the statement holds an action that must be the only one of its
// ALTER TABLE.
func (s *alterTableStatement) standalone() bool {
	return slices.ContainsFunc(s.actions, alterTableAction.standalone)
}

func (s *alterTableStatement) Destructive() bool {
	for _, action := range s.actions {
		if action.destructive() {
			return true
		}
	}
	return false
}

// addsForeignKey reports whether the statement adds a foreign key. Those run after the
// indexes they may need and are never bundled.
func (s *alterTableStatement) addsForeignKey() bool {
	for _, action := range s.actions {
		if _, ok := action.(addForeignKeyAction); ok {
			return true
		}
	}
	return false
}

// inputAlterTableStatement is an ALTER TABLE from the desired schema, emitted as written.
type inputAlterTableStatement struct {
	statementDefaults
	input DDL
	algorithmLock
}

func (s inputAlterTableStatement) Render() string {
	switch s.input.(type) {
	case *SetTableOwner:
		// The same statement comes from ALTER VIEW and ALTER MATERIALIZED VIEW, and OWNER TO is
		// PostgreSQL syntax that takes no MySQL ALGORITHM or LOCK clause.
		return s.input.Statement()
	}
	return s.input.Statement() + s.algorithmLock.render()
}

func (s inputAlterTableStatement) Destructive() bool {
	if rls, ok := s.input.(*SetRowLevelSecurity); ok {
		return !rls.value
	}
	return false
}

// createIndexStatement is a CREATE INDEX the generator builds from the index.
type createIndexStatement struct {
	statementDefaults
	d     dialect
	table QualifiedName
	index Index
}

func (s createIndexStatement) Render() string {
	switch s.d.mode {
	case GeneratorModePostgres:
		return s.d.generateCreateIndexStatement(s.table, s.index)
	case GeneratorModeMssql:
		ddl := "CREATE"
		if s.index.unique {
			ddl += " UNIQUE"
		}
		ddl += mssqlClusteredOption(s.index)
		ddl += fmt.Sprintf(" INDEX %s ON %s", s.d.escapeSQLIdent(s.index.name), s.d.escapeQualifiedName(s.table))
		ddl += fmt.Sprintf(" (%s)%s", s.d.indexColumnList(s.index), s.d.generateIndexOptionDefinition(s.index.options))
		// definition of partition is valid only in the syntax `CREATE INDEX ...`
		if s.index.partition.partitionName != "" {
			ddl += fmt.Sprintf(" ON %s", s.d.forceEscapeSQLName(s.index.partition.partitionName))
			if s.index.partition.column != "" {
				ddl += fmt.Sprintf(" (%s)", s.d.forceEscapeSQLName(s.index.partition.column))
			}
		}
		return ddl
	case GeneratorModeSQLite3:
		ddl := "CREATE"
		if s.index.unique {
			ddl += " UNIQUE"
		}
		ddl += fmt.Sprintf(" INDEX %s ON %s", s.d.escapeSQLIdent(s.index.name), s.d.escapeQualifiedName(s.table))
		columns := util.TransformSlice(s.index.columns, func(column IndexColumn) string {
			return s.d.forceEscapeSQLName(parser.String(column.columnExpr))
		})
		ddl += fmt.Sprintf(" (%s)", strings.Join(columns, ", "))
		if s.index.where != nil {
			ddl += fmt.Sprintf(" WHERE %s", parser.String(s.index.where))
		}
		return ddl
	default:
		panic(fmt.Sprintf("CREATE INDEX is not generated for mode %d", s.d.mode))
	}
}

func (s createIndexStatement) Transactional() bool { return createsIndexInTransaction(s.index) }

// inputCreateIndexStatement is a CREATE INDEX from the desired schema, emitted as written.
type inputCreateIndexStatement struct {
	statementDefaults
	statement string
	index     Index
}

func (s inputCreateIndexStatement) Render() string      { return s.statement }
func (s inputCreateIndexStatement) Transactional() bool { return createsIndexInTransaction(s.index) }

// createsIndexInTransaction reports whether PostgreSQL and Aurora DSQL build the index inside a
// transaction; they refuse to build it CONCURRENTLY or ASYNC there.
func createsIndexInTransaction(index Index) bool {
	return !index.concurrently && !index.async
}

// mssqlClusteredOption renders whether a SQL Server index is clustered.
func mssqlClusteredOption(index Index) string {
	if index.clustered {
		return " CLUSTERED"
	}
	return " NONCLUSTERED"
}

func (d dialect) indexColumnList(index Index) string {
	return strings.Join(util.TransformSlice(index.columns, d.generateIndexColumnDefinition), ", ")
}

// dropIndexStatement is a standalone DROP INDEX.
type dropIndexStatement struct {
	statementDefaults
	d     dialect
	table QualifiedName
	name  Ident
}

func (s dropIndexStatement) Render() string {
	switch s.d.mode {
	case GeneratorModePostgres:
		return fmt.Sprintf("DROP INDEX %s.%s", s.d.escapeSQLIdent(s.d.normalizeDefaultSchema(s.table.Schema)), s.d.escapeSQLIdent(s.name))
	case GeneratorModeMssql:
		return fmt.Sprintf("DROP INDEX %s ON %s", s.d.escapeSQLIdent(s.name), s.d.escapeQualifiedName(s.table))
	case GeneratorModeSQLite3:
		return fmt.Sprintf("DROP INDEX %s", s.d.escapeSQLIdent(s.name))
	default:
		panic(fmt.Sprintf("DROP INDEX is not generated for mode %d", s.d.mode))
	}
}

func (s dropIndexStatement) Destructive() bool { return true }

// renameIndexStatement is PostgreSQL's ALTER INDEX ... RENAME TO.
type renameIndexStatement struct {
	statementDefaults
	d     dialect
	table QualifiedName
	from  Ident
	to    Ident
}

func (s renameIndexStatement) Render() string {
	schema := s.d.normalizeDefaultSchema(s.table.Schema)
	return fmt.Sprintf("ALTER INDEX %s.%s RENAME TO %s", s.d.escapeSQLIdent(schema), s.d.escapeSQLIdent(s.from), s.d.escapeSQLIdent(s.to))
}

// spRenameStatement renames a SQL Server table, or a column or an index of it. sp_rename
// takes the names unquoted.
type spRenameStatement struct {
	statementDefaults
	d       dialect
	table   QualifiedName
	object  string // the column or index; empty to rename the table
	newName string
	kind    string // "COLUMN" or "INDEX"; empty for a table
}

func (s spRenameStatement) Render() string {
	if s.object == "" {
		return fmt.Sprintf("EXEC sp_rename '%s', '%s'", s.table.Name.Name, s.newName)
	}
	// The table is qualified only outside the default schema.
	table := s.table.Name.Name
	if schema := s.table.Schema.Name; schema != "" && schema != s.d.defaultSchema {
		table = schema + "." + table
	}
	return fmt.Sprintf("EXEC sp_rename '%s.%s', '%s', '%s'", table, s.object, s.newName, s.kind)
}

// alterSequenceStatement changes the type of the sequence behind a PostgreSQL serial column.
type alterSequenceStatement struct {
	statementDefaults
	d              dialect
	table          QualifiedName
	column         Ident
	underlyingType string
}

func (s alterSequenceStatement) Render() string {
	schema := s.d.normalizeDefaultSchema(s.table.Schema)
	sequence := Ident{Name: fmt.Sprintf("%s_%s_seq", s.table.Name.Name, s.column.Name), Quoted: false}
	return fmt.Sprintf("ALTER SEQUENCE %s.%s AS %s", s.d.escapeSQLIdent(schema), s.d.escapeSQLIdent(sequence), s.underlyingType)
}

// alterTableAction is one action of an ALTER TABLE.
type alterTableAction interface {
	render(d dialect) string
	destructive() bool
	// standalone reports whether the action has to be the only one of its ALTER TABLE, which
	// then takes no ALGORITHM or LOCK clause.
	standalone() bool
}

// additive is the default for an action enable_drop does not gate.
type additive struct{}

func (additive) destructive() bool { return false }
func (additive) standalone() bool  { return false }

// removal is the default for an action enable_drop gates.
type removal struct{}

func (removal) destructive() bool { return true }
func (removal) standalone() bool  { return false }

// mustRender renders a definition that its action's constructor already rendered without
// error.
func mustRender(ddl string, err error) string {
	if err != nil {
		panic(err)
	}
	return ddl
}

// columnPosition places a MySQL column. The zero value leaves the position unspecified.
type columnPosition struct {
	first bool
	after Ident
}

// mysqlColumnPosition places the column at position among columns.
func mysqlColumnPosition(columns []*Column, position int) columnPosition {
	if position == 0 {
		return columnPosition{first: true}
	}
	return columnPosition{after: columns[position-1].name}
}

func (p columnPosition) render(d dialect) string {
	if p.first {
		return " FIRST"
	}
	if !p.after.IsEmpty() {
		return " AFTER " + d.escapeSQLIdent(p.after)
	}
	return ""
}

type addColumnAction struct {
	additive
	column       Column
	enableUnique bool
	position     columnPosition
}

func (g *Generator) addColumn(column Column, enableUnique bool, position columnPosition) (addColumnAction, error) {
	_, err := g.generateColumnDefinition(column, enableUnique)
	return addColumnAction{column: column, enableUnique: enableUnique, position: position}, err
}

func (a addColumnAction) render(d dialect) string {
	keyword := "ADD COLUMN "
	if d.mode == GeneratorModeMssql {
		keyword = "ADD "
	}
	return keyword + mustRender(d.generateColumnDefinition(a.column, a.enableUnique)) + a.position.render(d)
}

type dropColumnAction struct {
	removal
	column Ident
}

func (a dropColumnAction) render(d dialect) string {
	return "DROP COLUMN " + d.escapeSQLIdent(a.column)
}

type renameColumnAction struct {
	additive
	from Ident
	to   Ident
}

func (a renameColumnAction) render(d dialect) string {
	return fmt.Sprintf("RENAME COLUMN %s TO %s", d.escapeSQLIdent(a.from), d.escapeSQLIdent(a.to))
}

// changeColumnAction is MySQL's CHANGE COLUMN.
type changeColumnAction struct {
	additive
	from         Ident
	column       Column
	enableUnique bool
	position     columnPosition
}

func (g *Generator) changeColumn(from Ident, column Column, enableUnique bool, position columnPosition) (changeColumnAction, error) {
	_, err := g.generateColumnDefinition(column, enableUnique)
	return changeColumnAction{from: from, column: column, enableUnique: enableUnique, position: position}, err
}

func (a changeColumnAction) render(d dialect) string {
	return fmt.Sprintf("CHANGE COLUMN %s %s", d.escapeSQLIdent(a.from), mustRender(d.generateColumnDefinition(a.column, a.enableUnique))) + a.position.render(d)
}

// alterColumnTypeAction is PostgreSQL's ALTER COLUMN ... TYPE.
type alterColumnTypeAction struct {
	additive
	column Column
	// serialType replaces the column's type when a serial column changes to another serial.
	serialType string
	using      string
}

func (a alterColumnTypeAction) render(d dialect) string {
	dataType := a.serialType
	if dataType == "" {
		dataType = d.generateDataType(a.column)
	}
	ddl := fmt.Sprintf("ALTER COLUMN %s TYPE %s", d.escapeSQLIdent(a.column.name), dataType)
	if a.using != "" {
		ddl += " USING " + a.using
	}
	return ddl
}

type alterColumnNotNullAction struct {
	additive
	column  Ident
	notNull bool
}

func (a alterColumnNotNullAction) render(d dialect) string {
	if a.notNull {
		return fmt.Sprintf("ALTER COLUMN %s SET NOT NULL", d.escapeSQLIdent(a.column))
	}
	return fmt.Sprintf("ALTER COLUMN %s DROP NOT NULL", d.escapeSQLIdent(a.column))
}

// alterColumnDefaultAction sets the default, or drops it when there is none.
type alterColumnDefaultAction struct {
	additive
	column     Ident
	defaultDef *DefaultDefinition
}

func (g *Generator) alterColumnDefault(column Ident, defaultDef *DefaultDefinition) (alterColumnDefaultAction, error) {
	var err error
	if defaultDef != nil {
		_, err = g.generateDefaultDefinition(*defaultDef)
	}
	return alterColumnDefaultAction{column: column, defaultDef: defaultDef}, err
}

func (a alterColumnDefaultAction) render(d dialect) string {
	if a.defaultDef == nil {
		return fmt.Sprintf("ALTER COLUMN %s DROP DEFAULT", d.escapeSQLIdent(a.column))
	}
	return fmt.Sprintf("ALTER COLUMN %s SET %s", d.escapeSQLIdent(a.column), mustRender(d.generateDefaultDefinition(*a.defaultDef)))
}

type dropIdentityAction struct {
	additive
	column Ident
}

func (a dropIdentityAction) render(d dialect) string {
	return fmt.Sprintf("ALTER COLUMN %s DROP IDENTITY IF EXISTS", d.escapeSQLIdent(a.column))
}

type addIdentityAction struct {
	additive
	column   Ident
	behavior string
	sequence *Sequence
}

func (a addIdentityAction) render(d dialect) string {
	ddl := fmt.Sprintf("ALTER COLUMN %s ADD GENERATED %s AS IDENTITY", d.escapeSQLIdent(a.column), a.behavior)
	if a.sequence != nil {
		ddl += " (" + generateSequenceClause(a.sequence) + ")"
	}
	return ddl
}

type setIdentityAction struct {
	additive
	column   Ident
	behavior string
}

func (a setIdentityAction) render(d dialect) string {
	return fmt.Sprintf("ALTER COLUMN %s SET GENERATED %s", d.escapeSQLIdent(a.column), a.behavior)
}

// alterColumnDefinitionAction is SQL Server's ALTER COLUMN with the whole definition.
type alterColumnDefinitionAction struct {
	additive
	column Column
}

func (g *Generator) alterColumnDefinition(column Column) (alterColumnDefinitionAction, error) {
	_, err := g.generateColumnDefinition(column, false)
	return alterColumnDefinitionAction{column: column}, err
}

func (a alterColumnDefinitionAction) render(d dialect) string {
	return "ALTER COLUMN " + mustRender(d.generateColumnDefinition(a.column, false))
}

type addCheckAction struct {
	additive
	name              Ident
	expr              parser.Expr
	notForReplication bool
	noInherit         bool
}

func (a addCheckAction) render(d dialect) string {
	ddl := "ADD "
	// PostgreSQL names an unnamed CHECK itself.
	if d.mode != GeneratorModePostgres || !a.name.IsEmpty() {
		ddl += "CONSTRAINT " + d.escapeSQLIdent(a.name) + " "
	}
	ddl += "CHECK"
	if a.notForReplication {
		ddl += " NOT FOR REPLICATION"
	}
	ddl += " (" + d.normalizeCheckExprString(a.expr) + ")"
	if a.noInherit {
		ddl += " NO INHERIT"
	}
	return ddl
}

// dropConstraintAction is not destructive: dropping a constraint loses no data, and
// changing one takes a drop.
type dropConstraintAction struct {
	additive
	name Ident
}

func (a dropConstraintAction) render(d dialect) string {
	return "DROP CONSTRAINT " + d.escapeSQLIdent(a.name)
}

// addDefaultConstraintAction is SQL Server's ADD [CONSTRAINT name] DEFAULT ... FOR column.
type addDefaultConstraintAction struct {
	additive
	name       Ident
	defaultDef DefaultDefinition
	column     Ident
}

func (g *Generator) addDefaultConstraint(name Ident, defaultDef DefaultDefinition, column Ident) (addDefaultConstraintAction, error) {
	_, err := g.generateDefaultDefinition(defaultDef)
	return addDefaultConstraintAction{name: name, defaultDef: defaultDef, column: column}, err
}

func (a addDefaultConstraintAction) render(d dialect) string {
	definition := mustRender(d.generateDefaultDefinition(a.defaultDef))
	if a.name.IsEmpty() {
		return fmt.Sprintf("ADD %s FOR %s", definition, d.escapeSQLIdent(a.column))
	}
	return fmt.Sprintf("ADD CONSTRAINT %s %s FOR %s", d.escapeSQLIdent(a.name), definition, d.escapeSQLIdent(a.column))
}

type dropForeignKeyAction struct {
	additive
	name Ident
}

func (a dropForeignKeyAction) render(d dialect) string {
	return "DROP FOREIGN KEY " + d.escapeSQLIdent(a.name)
}

// dropIndexAction is MySQL's DROP INDEX clause.
type dropIndexAction struct {
	removal
	name Ident
}

func (a dropIndexAction) render(d dialect) string { return "DROP INDEX " + d.escapeSQLIdent(a.name) }

type dropPrimaryKeyAction struct {
	additive
}

func (dropPrimaryKeyAction) render(dialect) string { return "DROP PRIMARY KEY" }

// addIndexAction adds an index, a primary key or a unique constraint.
type addIndexAction struct {
	additive
	index Index
}

func (a addIndexAction) render(d dialect) string {
	index := a.index
	columns := d.indexColumnList(index)
	optionDefinition := d.generateIndexOptionDefinition(index.options)

	switch d.mode {
	case GeneratorModeMssql:
		ddl := "ADD"
		if index.name.Name != "PRIMARY" {
			ddl += fmt.Sprintf(" CONSTRAINT %s", d.escapeSQLIdent(index.name))
		}
		ddl += " " + strings.ToUpper(index.indexType) + mssqlClusteredOption(index)
		return ddl + fmt.Sprintf(" (%s)%s", columns, optionDefinition)
	case GeneratorModePostgres:
		ddl := "ADD "
		if strings.EqualFold(index.indexType, "PRIMARY KEY") && index.primary &&
			(!index.name.IsEmpty() && index.name.Name != "PRIMARY" && index.name.Name != index.columns[0].ColumnName()) {
			ddl += fmt.Sprintf("CONSTRAINT %s ", d.escapeSQLIdent(index.name))
		}
		if strings.EqualFold(index.indexType, "UNIQUE") {
			if !index.name.IsEmpty() {
				ddl += fmt.Sprintf("CONSTRAINT %s ", d.escapeSQLIdent(index.name))
			}
			ddl += "UNIQUE"
			if index.nullsNotDistinct {
				ddl += " NULLS NOT DISTINCT"
			}
		} else {
			ddl += strings.ToUpper(index.indexType)
			if !index.primary {
				ddl += fmt.Sprintf(" %s", d.escapeSQLIdent(index.name))
			}
		}
		ddl += fmt.Sprintf(" (%s)", columns)
		if len(index.included) > 0 {
			ddl += fmt.Sprintf(" INCLUDE (%s)", d.escapeAndJoinNames(index.included))
		}
		return ddl + optionDefinition + d.generateConstraintOptions(index.constraintOptions)
	default:
		// Construct index type with optional VECTOR keyword for MariaDB vector indexes
		indexType := strings.ToUpper(index.indexType)
		if index.vector {
			indexType = "VECTOR INDEX"
		}
		ddl := "ADD " + indexType
		if !index.primary {
			ddl += fmt.Sprintf(" %s", d.escapeSQLIdent(index.name))
		}
		return ddl + fmt.Sprintf(" (%s)%s%s", columns, optionDefinition, d.generateConstraintOptions(index.constraintOptions))
	}
}

// addColumnUniqueKeyAction adds the UNIQUE KEY a MySQL column declares inline.
type addColumnUniqueKeyAction struct {
	additive
	column Ident
}

func (a addColumnUniqueKeyAction) render(d dialect) string {
	return fmt.Sprintf("ADD UNIQUE KEY %s(%s)", d.escapeSQLIdent(a.column), d.escapeSQLIdent(a.column))
}

type addForeignKeyAction struct {
	additive
	foreignKey ForeignKey
}

func (a addForeignKeyAction) render(d dialect) string {
	return "ADD " + d.generateForeignKeyDefinition(a.foreignKey) + d.generateConstraintOptions(a.foreignKey.constraintOptions)
}

type addExclusionAction struct {
	additive
	exclusion Exclusion
}

func (a addExclusionAction) render(d dialect) string {
	return "ADD " + d.generateExclusionDefinition(a.exclusion)
}

// tableCommentAction sets a MySQL table comment. The comment is the literal as written.
type tableCommentAction struct {
	additive
	comment string
}

func (a tableCommentAction) render(dialect) string {
	if a.comment == "" {
		return "COMMENT = ''"
	}
	return "COMMENT = " + a.comment
}

// tableOptionAction sets a TiDB table option.
type tableOptionAction struct {
	additive
	key   string
	value string
}

func (a tableOptionAction) render(dialect) string {
	return fmt.Sprintf("%s = %s", a.key, a.value)
}

type addPartitionAction struct {
	partition PartitionDefinition
}

func (a addPartitionAction) render(d dialect) string {
	part := a.partition
	name := d.escapePartitionName(part.Name.Name)
	switch {
	case part.In != nil:
		return fmt.Sprintf("ADD PARTITION (PARTITION %s VALUES IN (%s))", name, d.formatExprs(part.In))
	case part.Maxvalue:
		return fmt.Sprintf("ADD PARTITION (PARTITION %s VALUES LESS THAN MAXVALUE)", name)
	case part.LessThan != nil:
		return fmt.Sprintf("ADD PARTITION (PARTITION %s VALUES LESS THAN (%s))", name, d.formatExprs(part.LessThan))
	default:
		panic(fmt.Sprintf("partition %s has no bound", part.Name.Name))
	}
}

func (addPartitionAction) destructive() bool { return false }

// MySQL takes a partition operation only on its own, without ALGORITHM or LOCK.
func (addPartitionAction) standalone() bool { return true }

type dropPartitionAction struct {
	name string
}

func (a dropPartitionAction) render(d dialect) string {
	return "DROP PARTITION " + d.escapePartitionName(a.name)
}

func (dropPartitionAction) destructive() bool { return true }

// MySQL takes a partition operation only on its own, without ALGORITHM or LOCK.
func (dropPartitionAction) standalone() bool { return true }

type renameTableAction struct {
	additive
	to Ident
}

func (a renameTableAction) render(d dialect) string {
	return "RENAME TO " + d.escapeSQLIdent(a.to)
}

// renameIndexAction is MySQL's RENAME INDEX clause.
type renameIndexAction struct {
	additive
	from Ident
	to   Ident
}

func (a renameIndexAction) render(d dialect) string {
	return fmt.Sprintf("RENAME INDEX %s TO %s", d.escapeSQLIdent(a.from), d.escapeSQLIdent(a.to))
}

// disableRowLevelSecurityAction turns row level security off, or stops forcing it on the
// table owner.
type disableRowLevelSecurityAction struct {
	removal
	force bool
}

func (a disableRowLevelSecurityAction) render(dialect) string {
	if a.force {
		return "NO FORCE ROW LEVEL SECURITY"
	}
	return "DISABLE ROW LEVEL SECURITY"
}
