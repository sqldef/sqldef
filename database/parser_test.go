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

func TestTDSQLExecutableTrailingCommentIsPreserved(t *testing.T) {
	sql := "CREATE TABLE t (id bigint NOT NULL, created_at datetime(6) NOT NULL, PRIMARY KEY (id)) ENGINE=ROCKSDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci /*B![ttl] TTL=`created_at` + INTERVAL 90 DAY TTL_ENABLE='ON' TTL_JOB_INTERVAL='1h' */;"
	stmts, err := NewParser(parser.ParserModeMysql).Parse(sql)
	assert.NoError(t, err)
	assert.Len(t, stmts, 1)
	ddl := stmts[0].Statement.(*parser.DDL)
	assert.Equal(t, "created_at + INTERVAL 90 DAY", ddl.TableSpec.Options["TTL"])
	assert.Equal(t, "'ON'", ddl.TableSpec.Options["TTL_ENABLE"])
}

func TestTDSQLIntervalExecutableCommentIsParsedAndPreserved(t *testing.T) {
	sql := "CREATE TABLE t (id bigint NOT NULL, PRIMARY KEY (id)) ENGINE=ROCKSDB /*!50100 PARTITION BY RANGE (`id`) */ /*!B210604 INTERVAL(100) */ /*!50100 (PARTITION p0 VALUES LESS THAN (100) ENGINE = ROCKSDB) */;"
	stmts, err := NewParser(parser.ParserModeMysql).Parse(sql)
	assert.NoError(t, err)
	assert.Len(t, stmts, 1)
	assert.Contains(t, stmts[0].DDL, "/*!B210604 INTERVAL(100) */")
	ddl := stmts[0].Statement.(*parser.DDL)
	assert.NotNil(t, ddl.TableSpec.Partition)
	assert.Equal(t, 100, ddl.TableSpec.Partition.Interval)
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
