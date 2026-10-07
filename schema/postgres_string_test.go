package schema

import (
	"strings"
	"testing"

	"github.com/sqldef/sqldef/v3/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckRegexKeepsOneBackslash(t *testing.T) {
	stmt, err := parser.ParseDDL(`CREATE TABLE users (
		email text,
		CONSTRAINT users_email_check CHECK (email ~ '^[^@\s]+\.[^@\s]+$')
	)`, parser.ParserModePostgres)
	require.NoError(t, err)

	expr := stmt.(*parser.DDL).TableSpec.Checks[0].Where.Expr
	for _, legacy := range []bool{true, false} {
		d := dialect{mode: GeneratorModePostgres, legacyIgnoreQuotes: legacy}
		got := d.normalizeCheckExprString(expr)
		assert.Contains(t, got, `'^[^@\s]+\.[^@\s]+$'`)
		assert.Equal(t, 3, strings.Count(got, "\\"), got)
	}
}
