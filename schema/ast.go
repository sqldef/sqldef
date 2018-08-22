package schema

type DDL interface {
	Statement() string
}

type CreateTable struct {
	statement string
	table     Table
}

type AddIndex struct {
	statement string
	index     Index
}

type Table struct {
	name    string
	columns []Column
	indexes []Index
	// XXX: have options and alter on its change?
}

type Column struct {
	name          string
	typeName      string
	unsigned      bool
	notNull       bool
	autoIncrement bool
	defaultVal    *Value
	length        *Value
	scale         *Value
	keyOption     ColumnKeyOption
	// TODO: keyopt
	// XXX: charset, collate, zerofill?
}

type Index struct {
	name      string
	indexType string // Parsed in "create table" but not parsed in "add index". So actually not used yet. Just use primary/unique.
	columns   []IndexColumn
	primary   bool
	unique    bool
}

type IndexColumn struct {
	column string
	length *Value // Parsed in "create table" but not parsed in "add index". So actually not used yet.
}

type Value struct {
	valueType ValueType
	raw       []byte

	// ValueType-specific. Should be union?
	strVal   string  // ValueTypeStr
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

func (c *CreateTable) Statement() string {
	return c.statement
}

func (a *AddIndex) Statement() string {
	return a.statement
}
