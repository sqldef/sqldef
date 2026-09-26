package schema

import (
	"fmt"
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

// rawStatement is SQL the generator has not typed yet. The text-based enable_drop gate
// still runs on it after rendering.
type rawStatement string

func (s rawStatement) Render() string      { return string(s) }
func (s rawStatement) Destructive() bool   { return false }
func (s rawStatement) Transactional() bool { return true }
func (s rawStatement) Skipped() bool       { return false }

func rawStatements(ddls []string) []statement {
	statements := make([]statement, len(ddls))
	for i, ddl := range ddls {
		statements[i] = rawStatement(ddl)
	}
	return statements
}

func renderStatements(statements []statement) []string {
	ddls := make([]string, len(statements))
	for i, s := range statements {
		ddls[i] = s.Render()
	}
	return ddls
}

// skipped is a statement enable_drop holds back. It is still emitted, as a comment, so that
// --dry-run shows what --enable-drop would run.
type skipped struct {
	statement
}

func (s skipped) Render() string { return skippedStatement(s.statement.Render()) }
func (s skipped) Skipped() bool  { return true }

// recreate drops an object and brings it back. Its statements run together or not at all:
// the ones after a held-back drop would run against the object that is still there.
type recreate struct {
	statements []statement
}

// tableName is the table a statement changes. The key identifies the table however its
// name is quoted, so that the statements of one table can be bundled; the name renders it.
type tableName struct {
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
	d       dialect
	table   tableName
	actions []alterTableAction
	algorithmLock
}

func (s *alterTableStatement) Render() string {
	actions := make([]string, len(s.actions))
	acceptsAlgorithmLock := true
	for i, action := range s.actions {
		actions[i] = action.render(s.d)
		acceptsAlgorithmLock = acceptsAlgorithmLock && action.acceptsAlgorithmLock()
	}
	ddl := "ALTER TABLE " + s.d.escapeQualifiedName(s.table.name) + " " + strings.Join(actions, ", ")
	if acceptsAlgorithmLock {
		ddl += s.algorithmLock.render()
	}
	return ddl
}

func (s *alterTableStatement) Destructive() bool {
	for _, action := range s.actions {
		if action.destructive() {
			return true
		}
	}
	return false
}

func (s *alterTableStatement) Transactional() bool { return true }
func (s *alterTableStatement) Skipped() bool       { return false }

// addsForeignKey reports whether the statement adds a foreign key. Those run after the
// indexes they may need and are never bundled.
func (s *alterTableStatement) addsForeignKey() bool {
	for _, action := range s.actions {
		switch action.(type) {
		case addForeignKeyAction, restoreForeignKeyAction:
			return true
		}
	}
	return false
}

// inputAlterTableStatement is an ALTER TABLE from the desired schema, emitted as written.
type inputAlterTableStatement struct {
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

func (s inputAlterTableStatement) Transactional() bool { return true }
func (s inputAlterTableStatement) Skipped() bool       { return false }

// createIndexStatement is a CREATE INDEX the generator builds from the index.
type createIndexStatement struct {
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
		if s.index.clustered {
			ddl += " CLUSTERED"
		} else {
			ddl += " NONCLUSTERED"
		}
		ddl += fmt.Sprintf(" INDEX %s ON %s", s.d.escapeSQLIdent(s.index.name), s.d.escapeQualifiedName(s.table))
		ddl += fmt.Sprintf(" (%s)%s", strings.Join(util.TransformSlice(s.index.columns, s.d.generateIndexColumnDefinition), ", "), s.d.generateIndexOptionDefinition(s.index.options))
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

func (s createIndexStatement) Destructive() bool   { return false }
func (s createIndexStatement) Transactional() bool { return !s.index.concurrently && !s.index.async }
func (s createIndexStatement) Skipped() bool       { return false }

// inputCreateIndexStatement is a CREATE INDEX from the desired schema, emitted as written.
type inputCreateIndexStatement struct {
	statement string
	index     Index
}

func (s inputCreateIndexStatement) Render() string    { return s.statement }
func (s inputCreateIndexStatement) Destructive() bool { return false }
func (s inputCreateIndexStatement) Transactional() bool {
	return !s.index.concurrently && !s.index.async
}
func (s inputCreateIndexStatement) Skipped() bool { return false }

// dropIndexStatement is a standalone DROP INDEX.
type dropIndexStatement struct {
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

func (s dropIndexStatement) Destructive() bool   { return true }
func (s dropIndexStatement) Transactional() bool { return true }
func (s dropIndexStatement) Skipped() bool       { return false }

// renameIndexStatement is PostgreSQL's ALTER INDEX ... RENAME TO.
type renameIndexStatement struct {
	d     dialect
	table QualifiedName
	from  Ident
	to    Ident
}

func (s renameIndexStatement) Render() string {
	schema := s.d.normalizeDefaultSchema(s.table.Schema)
	return fmt.Sprintf("ALTER INDEX %s.%s RENAME TO %s", s.d.escapeSQLIdent(schema), s.d.escapeSQLIdent(s.from), s.d.escapeSQLIdent(s.to))
}

func (s renameIndexStatement) Destructive() bool   { return false }
func (s renameIndexStatement) Transactional() bool { return true }
func (s renameIndexStatement) Skipped() bool       { return false }

// spRenameStatement renames a SQL Server object. sp_rename takes the names unquoted.
type spRenameStatement struct {
	object  string
	newName string
	kind    string // "COLUMN" or "INDEX"; empty for a table
}

func (s spRenameStatement) Render() string {
	ddl := fmt.Sprintf("EXEC sp_rename '%s', '%s'", s.object, s.newName)
	if s.kind != "" {
		ddl += fmt.Sprintf(", '%s'", s.kind)
	}
	return ddl
}

func (s spRenameStatement) Destructive() bool   { return false }
func (s spRenameStatement) Transactional() bool { return true }
func (s spRenameStatement) Skipped() bool       { return false }

// alterSequenceStatement changes the type of the sequence behind a PostgreSQL serial column.
type alterSequenceStatement struct {
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

func (s alterSequenceStatement) Destructive() bool   { return false }
func (s alterSequenceStatement) Transactional() bool { return true }
func (s alterSequenceStatement) Skipped() bool       { return false }

// alterTableAction is one action of an ALTER TABLE.
type alterTableAction interface {
	render(d dialect) string
	destructive() bool
	// acceptsAlgorithmLock reports whether MySQL accepts ALGORITHM and LOCK in the statement.
	acceptsAlgorithmLock() bool
}

// additive is the default for an action enable_drop does not gate.
type additive struct{}

func (additive) destructive() bool          { return false }
func (additive) acceptsAlgorithmLock() bool { return true }

// mustRender renders a definition that was already rendered once without error when its
// action was created.
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

func (a addColumnAction) render(d dialect) string {
	keyword := "ADD COLUMN "
	if d.mode == GeneratorModeMssql {
		keyword = "ADD "
	}
	return keyword + mustRender(d.generateColumnDefinition(a.column, a.enableUnique)) + a.position.render(d)
}

type dropColumnAction struct {
	column Ident
}

func (a dropColumnAction) render(d dialect) string {
	return "DROP COLUMN " + d.escapeSQLIdent(a.column)
}
func (dropColumnAction) destructive() bool          { return true }
func (dropColumnAction) acceptsAlgorithmLock() bool { return true }

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
	name Ident
}

func (a dropIndexAction) render(d dialect) string  { return "DROP INDEX " + d.escapeSQLIdent(a.name) }
func (dropIndexAction) destructive() bool          { return true }
func (dropIndexAction) acceptsAlgorithmLock() bool { return true }

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
	columns := strings.Join(util.TransformSlice(index.columns, d.generateIndexColumnDefinition), ", ")
	optionDefinition := d.generateIndexOptionDefinition(index.options)

	switch d.mode {
	case GeneratorModeMssql:
		ddl := "ADD"
		if index.name.Name != "PRIMARY" {
			ddl += fmt.Sprintf(" CONSTRAINT %s", d.escapeSQLIdent(index.name))
		}
		clusteredOption := " NONCLUSTERED"
		if index.clustered {
			clusteredOption = " CLUSTERED"
		}
		ddl += fmt.Sprintf(" %s%s", strings.ToUpper(index.indexType), clusteredOption)
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

// restoreForeignKeyAction adds back a foreign key that was dropped so that the primary key
// it references could change.
type restoreForeignKeyAction struct {
	additive
	foreignKey ForeignKey
}

func (a restoreForeignKeyAction) render(d dialect) string {
	fk := a.foreignKey
	ddl := fmt.Sprintf("ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
		d.escapeSQLIdent(fk.constraintName),
		d.escapeAndJoinNames(fk.indexColumns),
		d.escapeQualifiedName(fk.referenceTableName),
		d.escapeAndJoinNames(fk.referenceColumns))
	if fk.onDelete != "" {
		ddl += " ON DELETE " + fk.onDelete
	}
	if fk.onUpdate != "" {
		ddl += " ON UPDATE " + fk.onUpdate
	}
	return ddl
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

// MySQL rejects ALGORITHM and LOCK next to a partition operation as a syntax error.
func (addPartitionAction) acceptsAlgorithmLock() bool { return false }

type dropPartitionAction struct {
	name string
}

func (a dropPartitionAction) render(d dialect) string {
	return "DROP PARTITION " + d.escapePartitionName(a.name)
}

func (dropPartitionAction) destructive() bool { return true }

// MySQL rejects ALGORITHM and LOCK next to a partition operation as a syntax error.
func (dropPartitionAction) acceptsAlgorithmLock() bool { return false }

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
	force bool
}

func (a disableRowLevelSecurityAction) render(dialect) string {
	if a.force {
		return "NO FORCE ROW LEVEL SECURITY"
	}
	return "DISABLE ROW LEVEL SECURITY"
}

func (disableRowLevelSecurityAction) destructive() bool          { return true }
func (disableRowLevelSecurityAction) acceptsAlgorithmLock() bool { return true }
