package database

import (
	"strings"
	"testing"

	"github.com/sqldef/sqldef/v3/parser"
	"github.com/stretchr/testify/assert"
)

func TestParseErrorScopedToOffendingStatement(t *testing.T) {
	sql := `CREATE TABLE foo (
    id bigint NOT NULL
) WITH (some_bogus_totally_unsupported_syntax = true);

CREATE TABLE bar (
    id bigint NOT NULL
);

CREATE TABLE baz (
    id bigint NOT NULL
);
`
	p := NewParser(parser.ParserModePostgres)
	_, err := p.Parse(sql)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "some_bogus_totally_unsupported_syntax")
	assert.NotContains(t, err.Error(), "CREATE TABLE bar")
	assert.NotContains(t, err.Error(), "CREATE TABLE baz")
	assert.Less(t, len(err.Error()), 500, "error message should be scoped to the offending statement, not the rest of the input")
}

func TestParseResyncAcrossEmbeddedSemicolons(t *testing.T) {
	sql := `CREATE TABLE foo (
    id bigint NOT NULL
);

CREATE TABLE bar (
    id bigint NOT NULL
);
`
	p := NewParser(parser.ParserModePostgres)
	stmts, err := p.Parse(sql)
	assert.NoError(t, err)
	assert.Len(t, stmts, 2)
	assert.True(t, strings.Contains(stmts[0].DDL, "foo"))
	assert.True(t, strings.Contains(stmts[1].DDL, "bar"))
}
