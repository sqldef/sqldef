package schema

import (
	"strings"

	"github.com/sqldef/sqldef/v3/parser"
)

// Ident represents an identifier with quote information.
// Used for quote-aware identifier handling throughout the schema package.
type Ident struct {
	Name   string
	Quoted bool
}

func (i Ident) String() string {
	return i.Name
}

// NewIdentFromGenerated creates an Ident for auto-generated identifier names
// (such as constraint names built from table and column names).
// The Quoted flag is inferred from case: if the name contains uppercase letters,
// it must have originated from a quoted identifier (PostgreSQL folds unquoted to lowercase).
func NewIdentFromGenerated(name string) Ident {
	return Ident{Name: name, Quoted: strings.ToLower(name) != name}
}

// QualifiedName represents a schema-qualified table name with quote information.
type QualifiedName struct {
	Schema Ident // empty if not specified (will use default schema)
	Name   Ident
}

// String returns the full qualified name as "schema.name" or just "name" if no schema.
func (q QualifiedName) String() string {
	if q.Schema.Name == "" {
		return q.Name.Name
	}
	return q.Schema.Name + "." + q.Name.Name
}

// identsEqual compares two Idents with quote-awareness based on database mode and legacyIgnoreQuotes.
// For non-PostgreSQL databases, always uses case-insensitive comparison.
// For PostgreSQL in quote-aware mode, unquoted identifiers are normalized to lowercase.
func identsEqual(a, b Ident, mode GeneratorMode, legacyIgnoreQuotes bool) bool {
	// For non-PostgreSQL databases, always use case-insensitive comparison
	if mode != GeneratorModePostgres {
		return strings.EqualFold(a.Name, b.Name)
	}
	if legacyIgnoreQuotes {
		return strings.EqualFold(a.Name, b.Name)
	}
	// Quote-aware comparison: normalize unquoted identifiers to lowercase
	aName := a.Name
	bName := b.Name
	if !a.Quoted {
		aName = strings.ToLower(aName)
	}
	if !b.Quoted {
		bName = strings.ToLower(bName)
	}
	return aName == bName
}

// qualifiedNamesEqual compares two QualifiedNames with quote-awareness.
func qualifiedNamesEqual(a, b QualifiedName, defaultSchema string, mode GeneratorMode, legacyIgnoreQuotes bool) bool {
	aSchema := a.Schema
	bSchema := b.Schema
	if aSchema.Name == "" && defaultSchema != "" {
		aSchema = Ident{Name: defaultSchema, Quoted: false}
	}
	if bSchema.Name == "" && defaultSchema != "" {
		bSchema = Ident{Name: defaultSchema, Quoted: false}
	}
	if !identsEqual(aSchema, bSchema, mode, legacyIgnoreQuotes) {
		return false
	}
	return identsEqual(a.Name, b.Name, mode, legacyIgnoreQuotes)
}

type DDL interface {
	Statement() string
}

type CreateTable struct {
	statement string
	table     Table
}

type CreateIndex struct {
	statement string
	tableName QualifiedName
	index     Index
}

type AddIndex struct {
	statement  string
	tableName  QualifiedName
	constraint bool
	index      Index
}

type AddPrimaryKey struct {
	statement string
	tableName QualifiedName
	index     Index
}

type AddForeignKey struct {
	statement  string
	tableName  QualifiedName
	foreignKey ForeignKey
}

type AddExclusion struct {
	statement string
	tableName QualifiedName
	exclusion Exclusion
}

type AddPolicy struct {
	statement string
	tableName QualifiedName
	policy    Policy
}

type GrantPrivilege struct {
	statement  string
	tableName  QualifiedName
	grantees   []string
	privileges []string
}

type RevokePrivilege struct {
	statement     string
	tableName     QualifiedName
	grantees      []string
	privileges    []string
	cascadeOption bool // CASCADE option for REVOKE
}

type Table struct {
	name        QualifiedName
	columns     map[string]*Column
	indexes     []Index
	checks      []CheckDefinition
	foreignKeys []ForeignKey
	exclusions  []Exclusion
	policies    []Policy
	privileges  []TablePrivilege
	options     map[string]string
	renamedFrom Ident // Previous table name if renamed via @renamed annotation
}

type Column struct {
	name                       Ident
	position                   int
	typeName                   string
	typeIdent                  Ident // Type name with quote information (for custom types like domains)
	unsigned                   bool
	notNull                    *bool
	autoIncrement              bool
	array                      bool
	defaultDef                 *DefaultDefinition
	sridDef                    *SridDefinition
	length                     *Value
	scale                      *Value
	displayWidth               *Value
	check                      *CheckDefinition
	charset                    string
	collate                    string
	timezone                   bool // for Postgres `with time zone`
	keyOption                  ColumnKeyOption
	onUpdate                   *Value
	comment                    *Value
	enumValues                 []string
	references                 Ident
	referenceDeferrable        *bool // for Postgres: DEFERRABLE, NOT DEFERRABLE, or nil
	referenceInitiallyDeferred *bool // for Postgres: INITIALLY DEFERRED, INITIALLY IMMEDIATE, or nil
	identity                   *Identity
	sequence                   *Sequence
	generated                  *Generated
	renamedFrom                Ident // Previous column name if renamed via @renamed annotation
	// TODO: keyopt
	// XXX: zerofill?
}

type Index struct {
	name              Ident
	indexType         string // Parsed only in "create table" but not parsed in "add index". Only used inside `generateDDLsForCreateTable`.
	columns           []IndexColumn
	primary           bool
	unique            bool
	vector            bool // for MariaDB vector indexes
	constraint        bool // for Postgres/MSSQL `ADD CONSTRAINT UNIQUE`
	async             bool // for Aurora DSQL
	concurrently      bool // for PostgreSQL
	constraintOptions *ConstraintOptions
	where             string         // for Postgres `Partial Indexes`
	included          []string       // for MSSQL
	clustered         bool           // for MSSQL
	partition         IndexPartition // for MSSQL
	options           []IndexOption
	renamedFrom       Ident // Previous index name if renamed via @renamed annotation
}

type IndexColumn struct {
	columnExpr parser.Expr // never nil as it's always initialized in the parser
	length     *int
	direction  string
}

// ColumnName returns the column name if this is a simple column reference.
// For functional indexes or expressions, it returns the string representation.
func (ic IndexColumn) ColumnName() string {
	// Check if it's a simple column reference (ColName)
	if colName, ok := ic.columnExpr.(*parser.ColName); ok {
		return colName.Name.String()
	}
	// For expressions, return the full expression string
	return parser.String(ic.columnExpr)
}

// IndexColumn.direction
const (
	AscScr  = "asc"
	DescScr = "desc"
)

type IndexOption struct {
	optionName string
	value      *Value
}

type IndexPartition struct {
	partitionName string
	column        string
}

type ConstraintOptions struct {
	deferrable        bool
	initiallyDeferred bool
}

type ForeignKey struct {
	constraintName     Ident
	indexName          string
	indexColumns       []Ident
	referenceTableName QualifiedName
	referenceColumns   []Ident
	onDelete           string
	onUpdate           string
	notForReplication  bool
	constraintOptions  *ConstraintOptions
}

type Exclusion struct {
	constraintName Ident
	indexType      string
	where          string
	exclusions     []ExclusionPair
}

type ExclusionPair struct {
	expression string
	operator   string
}

type Policy struct {
	name          Ident
	referenceName string
	permissive    string
	scope         string
	roles         []string
	using         parser.Expr
	withCheck     parser.Expr
}

type TablePrivilege struct {
	tableName       string
	grantee         string
	privileges      []string
	withGrantOption bool
}

type View struct {
	statement    string
	viewType     string
	securityType string
	name         QualifiedName
	definition   parser.SelectStatement
	indexes      []Index
	columns      []string
	withData     bool // true for "WITH DATA"
	withNoData   bool // true for "WITH NO DATA"
}

type Trigger struct {
	statement string
	name      Ident
	tableName QualifiedName
	time      string
	event     []string
	body      []string
}

type Value struct {
	valueType ValueType
	raw       string

	// ValueType-specific (behaves like a union)
	strVal   string  // ValueTypeStr
	intVal   int     // ValueTypeInt
	floatVal float64 // ValueTypeFloat
	bitVal   bool    // ValueTypeBit, ValueTypeBool
}

type ValueType int

const (
	ValueTypeStr = ValueType(iota)
	ValueTypeInt
	ValueTypeFloat
	ValueTypeHexNum
	ValueTypeHex
	ValueTypeValArg
	ValueTypeBit
	ValueTypeBool
)

type ColumnKeyOption int

const (
	ColumnKeyNone = ColumnKeyOption(iota)
	ColumnKeyPrimary
	ColumnKeySpatialKey
	ColumnKeyUnique
	ColumnKeyUniqueKey
	ColumnKey
)

type Identity struct {
	behavior          string
	notForReplication bool
}

type Sequence struct {
	Name        string
	IfNotExists bool
	Type        string
	IncrementBy *int
	MinValue    *int
	NoMinValue  bool
	MaxValue    *int
	NoMaxValue  bool
	StartWith   *int
	Cache       *int
	Cycle       bool
	NoCycle     bool
	OwnedBy     string
}

type DefaultDefinition struct {
	expression     parser.Expr
	constraintName Ident // only for MSSQL
}

type SridDefinition struct {
	value *Value
}

type CheckDefinition struct {
	definition        parser.Expr
	constraintName    Ident
	notForReplication bool
	noInherit         bool
}

// TODO: include type information
type Type struct {
	name       QualifiedName
	statement  string
	enumValues []string
}

type Domain struct {
	name         QualifiedName
	statement    string
	dataType     string
	defaultValue *DefaultDefinition
	notNull      bool
	collation    string
	constraints  []DomainConstraint
}

type DomainConstraint struct {
	name       string
	expression parser.Expr
}

type Generated struct {
	expr          string
	generatedType GeneratedType
}

type GeneratedType int

const (
	GeneratedTypeVirtual = GeneratedType(iota)
	GeneratedTypeStored
)

type Comment struct {
	statement string
	comment   parser.Comment
}

type Extension struct {
	statement string
	extension parser.Extension
}

type Schema struct {
	statement string
	schema    parser.Schema
}

func (c *CreateTable) Statement() string {
	return c.statement
}

func (c *CreateIndex) Statement() string {
	return c.statement
}

func (a *AddIndex) Statement() string {
	return a.statement
}

func (a *AddPrimaryKey) Statement() string {
	return a.statement
}

func (a *AddForeignKey) Statement() string {
	return a.statement
}

func (a *AddExclusion) Statement() string {
	return a.statement
}

func (a *AddPolicy) Statement() string {
	return a.statement
}

func (g *GrantPrivilege) Statement() string {
	return g.statement
}

func (r *RevokePrivilege) Statement() string {
	return r.statement
}

func (v *View) Statement() string {
	return v.statement
}

func (t *Trigger) Statement() string {
	return t.statement
}

func (t *Type) Statement() string {
	return t.statement
}

func (d *Domain) Statement() string {
	return d.statement
}

func (t *Comment) Statement() string {
	return t.statement
}

func (t *Extension) Statement() string {
	return t.statement
}

func (t *Schema) Statement() string {
	return t.statement
}

func (t *Table) PrimaryKey() *Index {
	for _, index := range t.indexes {
		if index.primary {
			return &index
		}
	}

	primaryColumns := []IndexColumn{}
	for _, column := range t.columns {
		if column.keyOption == ColumnKeyPrimary {
			primaryColumns = append(primaryColumns, IndexColumn{
				columnExpr: &parser.ColName{Name: parser.NewIdent(column.name.Name, column.name.Quoted)},
			})
		}
	}

	if len(primaryColumns) == 0 {
		return nil
	}

	return &Index{
		name:      Ident{Name: "PRIMARY", Quoted: false},
		indexType: "primary key",
		columns:   primaryColumns,
		primary:   true,
		unique:    true,
		clustered: true,
	}
}

func (keyOption ColumnKeyOption) isUnique() bool {
	return keyOption == ColumnKeyUnique || keyOption == ColumnKeyUniqueKey
}
