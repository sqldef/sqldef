package schema

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
