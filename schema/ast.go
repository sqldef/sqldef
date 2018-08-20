package schema

type DDL interface {
	Statement() string
}

type CreateTable struct {
	statement string
	table     Table
}

type Table struct {
	name    string
	columns []Column
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
	// XXX: charset, collate, zerofill, scale?
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

func (c *CreateTable) Statement() string {
	return c.statement
}
