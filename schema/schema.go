package schema

type DDL interface {
	Statement() string
}

type CreateTable struct {
	statement string
}

func (c *CreateTable) Statement() string {
	return c.statement
}
