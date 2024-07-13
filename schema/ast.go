package schema

import "github.com/sqldef/sqldef/parser"

type DDL interface {
	Statement() string
}

type CreateTable struct {
	statement string
	table     Table
}

type CreateIndex struct {
	statement string
	tableName string
	index     Index
}

type AddIndex struct {
	statement  string
	tableName  string
	constraint bool
	index      Index
}

type AddPrimaryKey struct {
	statement string
	tableName string
	index     Index
}

type AddForeignKey struct {
	statement  string
	tableName  string
	foreignKey ForeignKey
}

type AddPolicy struct {
	statement string
	tableName string
	policy    Policy
}

type Table struct {
	name        string
	columns     []Column
	indexes     []Index
	checks      []CheckDefinition
	foreignKeys []ForeignKey
	policies    []Policy
	options     map[string]string
}

type Column struct {
	name          string
	position      int
	typeName      string
	unsigned      bool
	notNull       *bool
	autoIncrement bool
	array         bool
	defaultDef    *DefaultDefinition
	sridDef       *SridDefinition
	length        *Value
	scale         *Value
	displayWidth  *Value
	check         *CheckDefinition
	charset       string
	collate       string
	timezone      bool // for Postgres `with time zone`
	keyOption     ColumnKeyOption
	onUpdate      *Value
	comment       *Value
	enumValues    []string
	references    string
	identity      *Identity
	sequence      *Sequence
	generated     *Generated
	// TODO: keyopt
	// XXX: zerofill?
}

type Index struct {
	name              string
	indexType         string // Parsed only in "create table" but not parsed in "add index". Only used inside `generateDDLsForCreateTable`.
	columns           []IndexColumn
	primary           bool
	unique            bool
	constraint        bool // for Postgres `ADD CONSTRAINT UNIQUE`
	constraintOptions *ConstraintOptions
	where             string         // for Postgres `Partial Indexes`
	included          []string       // for MSSQL
	clustered         bool           // for MSSQL
	partition         IndexPartition // for MSSQL
	options           []IndexOption
}

type IndexColumn struct {
	column    string
	length    *int
	direction string
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
	constraintName    string
	indexName         string
	indexColumns      []string
	referenceName     string
	referenceColumns  []string
	onDelete          string
	onUpdate          string
	notForReplication bool
	constraintOptions *ConstraintOptions
}

type Policy struct {
	name          string
	referenceName string
	permissive    string
	scope         string
	roles         []string
	using         string
	withCheck     string
}

type View struct {
	statement    string
	viewType     string
	securityType string
	name         string
	definition   string
	indexes      []Index
	columns      []string
}

type Trigger struct {
	statement string
	name      string
	tableName string
	time      string
	event     []string
	body      []string
}

type Value struct {
	valueType ValueType
	raw       []byte

	// ValueType-specific. Should be union?
	strVal   string  // ValueTypeStr, ValueTypeBool
	intVal   int     // ValueTypeInt
	floatVal float64 // ValueTypeFloat
	bitVal   bool    // ValueTypeBit
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
	value          *Value
	expression     string
	constraintName string // only for MSSQL
}

type SridDefinition struct {
	value *Value
}

type CheckDefinition struct {
	definition        string
	constraintName    string
	notForReplication bool
	noInherit         bool
}

// TODO: include type information
type Type struct {
	name       string
	statement  string
	enumValues []string
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

func (a *AddPolicy) Statement() string {
	return a.statement
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
				column: column.name,
			})
		}
	}

	if len(primaryColumns) == 0 {
		return nil
	}

	return &Index{
		name:      "PRIMARY",
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
