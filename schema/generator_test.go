package schema

import (
	"testing"

	"github.com/sqldef/sqldef/v3/parser"
	"github.com/stretchr/testify/assert"
)

func TestStringConstantSimple(t *testing.T) {
	assert.Equal(t, StringConstant(""), "''")
	assert.Equal(t, StringConstant("hello world"), "'hello world'")
}

func TestStringConstantContainingSingleQuote(t *testing.T) {
	assert.Equal(t, StringConstant("it's the bee's knees"), "'it''s the bee''s knees'")
	assert.Equal(t, StringConstant("'"), "''''")
	assert.Equal(t, StringConstant("''"), "''''''")
	assert.Equal(t, StringConstant("'example'"), "'''example'''")
}

func TestAreSamePrimaryKeyColumnsMutation(t *testing.T) {
	// Test that areSamePrimaryKeyColumns doesn't mutate the input indexes
	g := &Generator{mode: GeneratorModeMysql}

	// Create two indexes with empty directions
	indexA := Index{
		primary: true,
		columns: []IndexColumn{
			{column: "id", direction: ""},
			{column: "name", direction: ""},
		},
	}

	indexB := Index{
		primary: true,
		columns: []IndexColumn{
			{column: "id", direction: ""},
			{column: "name", direction: ""},
		},
	}

	// Store original direction values to check they weren't mutated
	originalBDirection0 := indexB.columns[0].direction
	originalBDirection1 := indexB.columns[1].direction

	// Call the function which currently mutates indexB
	result := g.areSamePrimaryKeyColumns(indexA, indexB)

	// The function should return true (they are the same)
	assert.True(t, result, "Indexes should be considered the same")

	// BUG: The directions should not have been mutated
	// This will FAIL with the current implementation
	assert.Equal(t, originalBDirection0, indexB.columns[0].direction, "indexB.columns[0].direction was mutated")
	assert.Equal(t, originalBDirection1, indexB.columns[1].direction, "indexB.columns[1].direction was mutated")
}

func TestAreSamePrimaryKeyColumnsWithDifferentDirections(t *testing.T) {
	// Test comparing primary keys with different explicit directions
	g := &Generator{mode: GeneratorModeMysql}

	indexA := Index{
		primary: true,
		columns: []IndexColumn{
			{column: "id", direction: AscScr},
			{column: "name", direction: DescScr},
		},
	}

	indexB := Index{
		primary: true,
		columns: []IndexColumn{
			{column: "id", direction: AscScr},
			{column: "name", direction: AscScr}, // Different direction
		},
	}

	// Store original values
	originalBDirection0 := indexB.columns[0].direction
	originalBDirection1 := indexB.columns[1].direction

	// Should return false due to different directions
	result := g.areSamePrimaryKeyColumns(indexA, indexB)
	assert.False(t, result, "Indexes with different directions should not be the same")

	// Verify no mutation occurred
	assert.Equal(t, originalBDirection0, indexB.columns[0].direction, "indexB.columns[0].direction should not be mutated")
	assert.Equal(t, originalBDirection1, indexB.columns[1].direction, "indexB.columns[1].direction should not be mutated")
}

func TestNormalizeViewDefinition(t *testing.T) {
	tests := []struct {
		name     string
		mode     GeneratorMode
		input    string
		expected string
	}{
		// PostgreSQL specific tests
		{
			name:     "PostgreSQL: normalize table prefix with COLLATE",
			mode:     GeneratorModePostgres,
			input:    `select users.id, (users.name COLLATE "ja-JP-x-icu") as name from users`,
			expected: `select id, (name collate "ja-jp-x-icu") as name from users`,
		},
		{
			name:     "PostgreSQL: normalize multiple table prefixes",
			mode:     GeneratorModePostgres,
			input:    `select users.id, users.name, users.email from users`,
			expected: `select id, name, email from users`,
		},
		{
			name:     "PostgreSQL: normalize with lowercase collate",
			mode:     GeneratorModePostgres,
			input:    `select users.id, (users.name collate "ja-JP-x-icu") as name from users`,
			expected: `select id, (name collate "ja-jp-x-icu") as name from users`,
		},
		{
			name:     "PostgreSQL: normalize spaces",
			mode:     GeneratorModePostgres,
			input:    `select   users.id,    (users.name   COLLATE   "ja-JP-x-icu")   as   name   from   users`,
			expected: `select id, (name collate "ja-jp-x-icu") as name from users`,
		},
		{
			name:     "PostgreSQL: normalize with joins",
			mode:     GeneratorModePostgres,
			input:    `select u.id, (u.name COLLATE "en_US") as name from users u join orders o on u.id = o.user_id`,
			expected: `select id, (name collate "en_us") as name from users u join orders o on id = user_id`,
		},
		{
			name:     "PostgreSQL: preserve column names without prefixes",
			mode:     GeneratorModePostgres,
			input:    `select id, (name COLLATE "ja-JP-x-icu") as name from users`,
			expected: `select id, (name collate "ja-jp-x-icu") as name from users`,
		},
		{
			name:     "PostgreSQL: normalize array syntax",
			mode:     GeneratorModePostgres,
			input:    `select array[1, 2, 3] as nums`,
			expected: `select 1, 2, 3 as nums`,
		},
		// Non-PostgreSQL modes should not normalize
		{
			name:     "MySQL: no normalization",
			mode:     GeneratorModeMysql,
			input:    `SELECT users.id, users.name FROM users`,
			expected: `select users.id, users.name from users`,
		},
		{
			name:     "SQLite3: no normalization",
			mode:     GeneratorModeSQLite3,
			input:    `SELECT users.id, users.name FROM users`,
			expected: `select users.id, users.name from users`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &Generator{mode: tt.mode}
			actual := g.normalizeViewDefinition(tt.input)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestNormalizeCheckExprAST(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Remove ::text cast from string literal",
			input:    "status = 'active'::text",
			expected: "status = 'active'",
		},
		{
			name:     "Remove ::text cast from ARRAY elements",
			input:    "status = ANY(ARRAY['active'::text, 'pending'::text])",
			expected: "status = ANY(ARRAY['active', 'pending'])",
		},
		{
			name:     "Remove ::character varying cast",
			input:    "name = 'test'::character varying",
			expected: "name = 'test'",
		},
		{
			name:     "Remove ::character varying(255) cast",
			input:    "name = 'test'::character varying(255)",
			expected: "name = 'test'",
		},
		{
			name:     "Remove double parentheses",
			input:    "((status = 'active'))",
			expected: "(status = 'active')",
		},
		{
			name:     "Handle AND expression with casts",
			input:    "status = 'active'::text and name = 'test'::text",
			expected: "status = 'active' and name = 'test'",
		},
		{
			name:     "Handle OR expression with casts",
			input:    "status = 'active'::text or status = 'pending'::text",
			expected: "status in ('active', 'pending')",
		},
		{
			name:     "Handle NOT expression with cast",
			input:    "not status = 'inactive'::text",
			expected: "not status = 'inactive'",
		},
		{
			name:     "Handle complex comparison with casts",
			input:    "status = ANY(ARRAY['active'::text, 'pending'::text, 'processing'::text])",
			expected: "status = ANY(ARRAY['active', 'pending', 'processing'])",
		},
		{
			name:     "Handle IS NULL with cast",
			input:    "status::text is null",
			expected: "status is null",
		},
		{
			name:     "Handle BETWEEN with casts",
			input:    "created_at between '2020-01-01'::text and '2020-12-31'::text",
			expected: "created_at between '2020-01-01' and '2020-12-31'",
		},
		{
			name:     "Handle function call with cast arguments",
			input:    "upper(status::text) = 'ACTIVE'",
			expected: "upper(status) = 'ACTIVE'",
		},
		{
			name:     "No changes for expression without casts",
			input:    "status = 'active' and amount > 100",
			expected: "status = 'active' and amount > 100",
		},
		{
			name:     "Handle nested expressions with casts",
			input:    "(status = 'active'::text and (priority = 'high'::text or priority = 'urgent'::text))",
			expected: "(status = 'active' and priority in ('high', 'urgent'))",
		},
		{
			name:     "Handle ValTuple in IN clause",
			input:    "status IN ('a', 'c', 'b')",
			expected: "status in ('a', 'b', 'c')",
		},
		{
			name:     "Handle ValTuple with charset prefix",
			input:    "status in (_utf8mb4'a', _utf8mb4'b')",
			expected: "status in ('a', 'b')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the input expression as a CHECK constraint
			stmt, err := parser.ParseDDL("create table t (id int, check("+tt.input+"))", parser.ParserModePostgres)
			assert.NoError(t, err, "Failed to parse input")
			assert.NotNil(t, stmt, "Parsed statement is nil")

			ddl, ok := stmt.(*parser.DDL)
			assert.True(t, ok, "Statement is not a DDL")
			assert.NotNil(t, ddl.TableSpec, "TableSpec is nil")
			assert.Greater(t, len(ddl.TableSpec.Checks), 0, "No check constraints found")

			check := ddl.TableSpec.Checks[0]
			assert.NotNil(t, check.Where.Expr, "Check expression is nil")

			// Normalize the expression
			normalized := normalizeCheckExprAST(check.Where.Expr)

			// Convert normalized expression to string
			actual := parser.String(normalized)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestNormalizeCheckExprASTNilInput(t *testing.T) {
	result := normalizeCheckExprAST(nil)
	assert.Nil(t, result)
}

func TestCheckConstraintComparisonWithDifferentInValues(t *testing.T) {
	// Test that CHECK constraints with different IN clause values are detected as different

	// Parse current state (from DB with charset prefix)
	stmt1, err := parser.ParseDDL("create table t (id int, check(status IN (_utf8mb4'todo',_utf8mb4'in_progress')))", parser.ParserModeMysql)
	assert.NoError(t, err)
	ddl1 := stmt1.(*parser.DDL)
	check1 := ddl1.TableSpec.Checks[0]

	// Parse desired state (from user, no charset prefix)
	stmt2, err := parser.ParseDDL("create table t (id int, check(status IN ('todo', 'in_progress', 'done')))", parser.ParserModeMysql)
	assert.NoError(t, err)
	ddl2 := stmt2.(*parser.DDL)
	check2 := ddl2.TableSpec.Checks[0]

	// Normalize both
	normalized1 := normalizeCheckExprAST(check1.Where.Expr)
	normalized2 := normalizeCheckExprAST(check2.Where.Expr)

	// Convert to strings
	str1 := parser.String(normalized1)
	str2 := parser.String(normalized2)

	t.Logf("Normalized 1: %s", str1)
	t.Logf("Normalized 2: %s", str2)

	// They should be different
	assert.NotEqual(t, str1, str2, "CHECK constraints with different IN values should be detected as different")
}

func TestCheckConstraintIdempotencyWithMySQLFormat(t *testing.T) {
	// Test that CHECK constraints are idempotent when MySQL returns them with extra parens and charset

	// Parse as user would write it
	stmt1, err := parser.ParseDDL("create table t (id int, check(`status` IN ('todo', 'in_progress')))", parser.ParserModeMysql)
	assert.NoError(t, err)
	ddl1 := stmt1.(*parser.DDL)
	check1 := ddl1.TableSpec.Checks[0]

	// Parse as MySQL would return it (extra parens, charset prefix, lowercase)
	stmt2, err := parser.ParseDDL("create table t (id int, check((`status` in (_utf8mb4'todo',_utf8mb4'in_progress'))))", parser.ParserModeMysql)
	assert.NoError(t, err)
	ddl2 := stmt2.(*parser.DDL)
	check2 := ddl2.TableSpec.Checks[0]

	// Normalize both
	normalized1 := normalizeCheckExprAST(check1.Where.Expr)
	normalized2 := normalizeCheckExprAST(check2.Where.Expr)

	// Unwrap outermost parentheses (as done in areSameCheckDefinition)
	normalized1 = unwrapOutermostParenExpr(normalized1)
	normalized2 = unwrapOutermostParenExpr(normalized2)

	// Convert to strings
	str1 := parser.String(normalized1)
	str2 := parser.String(normalized2)

	t.Logf("Normalized 1 (user format): %s", str1)
	t.Logf("Normalized 2 (MySQL format): %s", str2)

	// They should be the same (idempotent)
	assert.Equal(t, str1, str2, "CHECK constraints should be idempotent despite MySQL's formatting")
}

func TestCheckConstraintMSSQLInVsOrNormalization(t *testing.T) {
	// Test that MSSQL's OR chain is normalized to IN and matches user's IN clause

	// Parse user's IN format as table-level CHECK (what user writes)
	stmtUser, err := parser.ParseDDL("CREATE TABLE t (c varchar(20), CONSTRAINT c_chk CHECK (c IN ('todo', 'in_progress')))", parser.ParserModeMssql)
	assert.NoError(t, err)
	ddlUser := stmtUser.(*parser.DDL)
	checkUser := ddlUser.TableSpec.Checks[0]

	// Parse MSSQL's OR format as column-level CHECK (what DB returns after MSSQL converts it)
	stmtDB, err := parser.ParseDDL("CREATE TABLE t (c varchar(20) CONSTRAINT [c_chk] CHECK ([c]='in_progress' OR [c]='todo'))", parser.ParserModeMssql)
	assert.NoError(t, err)
	ddlDB := stmtDB.(*parser.DDL)
	// This should be a column-level CHECK
	colDB := ddlDB.TableSpec.Columns[0] // First column is 'c'
	assert.NotNil(t, colDB.Type.Check, "Expected column-level CHECK")
	checkDB := colDB.Type.Check

	// Normalize both
	normalizedUser := normalizeCheckExprAST(checkUser.Where.Expr)
	normalizedDB := normalizeCheckExprAST(checkDB.Where.Expr)

	// Unwrap outermost parens
	normalizedUser = unwrapOutermostParenExpr(normalizedUser)
	normalizedDB = unwrapOutermostParenExpr(normalizedDB)

	// Convert to strings
	strUser := parser.String(normalizedUser)
	strDB := parser.String(normalizedDB)

	t.Logf("Normalized user (table-level IN): %s", strUser)
	t.Logf("Normalized DB (column-level OR):  %s", strDB)

	// They should be equal
	assert.Equal(t, strUser, strDB, "CHECK constraints should normalize to the same format")
}
