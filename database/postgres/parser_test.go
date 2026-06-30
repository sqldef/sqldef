package postgres

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	pgquery "github.com/pganalyze/pg_query_go/v6"
	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests, err := readTests("tests.yml")
	if err != nil {
		t.Fatal(err)
	}

	genericParser := database.NewParser(parser.ParserModePostgres)
	postgresParser := NewParserWithMode(PsqldefParserModePgquery)
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			psqlResult, err := postgresParser.Parse(test.SQL)
			if err != nil {
				t.Fatal(err)
			}

			if !test.CompareWithGenericParser {
				return
			}

			genericResult, err := genericParser.Parse(test.SQL)
			if err != nil {
				t.Fatal(err)
			}

			assert.Equal(t, genericResult, psqlResult)
		})
	}
}

type TestCase struct {
	SQL                      string
	CompareWithGenericParser bool `yaml:"compare_with_generic_parser"`
}

func readTests(file string) (map[string]TestCase, error) {
	buf, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var tests map[string]TestCase
	dec := yaml.NewDecoder(bytes.NewReader(buf), yaml.DisallowUnknownField())
	err = dec.Decode(&tests)
	if err != nil {
		return nil, err
	}

	return tests, nil
}

// TestParseIndexAsync tests parsing of CREATE INDEX ASYNC without database execution.
// This is a parse-only test since regular PostgreSQL doesn't support ASYNC (Aurora DSQL only).
// The parser uses testing=false to allow fallback to the generic parser.
func TestParseIndexAsync(t *testing.T) {
	t.Setenv("PSQLDEF_PARSER", "")
	sqlParser := NewParserWithMode(PsqldefParserModeAuto)

	// Test parsing CREATE INDEX ASYNC
	sql := `CREATE TABLE users (
  id BIGINT NOT NULL PRIMARY KEY,
  name VARCHAR(128) DEFAULT 'konsumer'
);
CREATE INDEX ASYNC username on users (name);`

	// Parse the schema - should not error (will use generic parser fallback)
	statements, err := sqlParser.Parse(sql)
	if err != nil {
		t.Fatalf("failed to parse CREATE INDEX ASYNC: %v", err)
	}

	// Verify we got 2 statements
	if len(statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(statements))
	}

	// Verify second statement is CREATE INDEX with Async flag
	indexStmt := statements[1].Statement
	ddl, ok := indexStmt.(*parser.DDL)
	if !ok {
		t.Fatalf("expected DDL statement, got %T", indexStmt)
	}

	if ddl.Action != parser.CreateIndex {
		t.Fatalf("expected CreateIndex action, got %v", ddl.Action)
	}

	if ddl.IndexSpec == nil {
		t.Fatal("expected IndexSpec to be non-nil")
	}

	if !ddl.IndexSpec.Async {
		t.Error("expected Async flag to be true")
	}

	// Verify the generated DDL string contains ASYNC
	generatedDDL := statements[1].DDL
	if !strings.Contains(strings.ToUpper(generatedDDL), "ASYNC") {
		t.Errorf("expected ASYNC in generated DDL, got: %s", generatedDDL)
	}
}

func TestCreateFunctionWithPgquery(t *testing.T) {
	t.Setenv("PSQLDEF_PARSER", "pgquery")
	postgresParser := NewParser()

	statements, err := postgresParser.Parse(`
    CREATE FUNCTION increment(i integer) RETURNS integer
    LANGUAGE plpgsql
    AS $$
      BEGIN
        RETURN i + 1;
      END;
    $$;
	`)

	if err != nil {
		t.Fatalf("failed to parse CREATE FUNCTION: %v", err)
	}

	if len(statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(statements))
	}

	funcStmt := statements[0].Statement
	_, ok := funcStmt.(*parser.Ignore)
	if !ok {
		t.Errorf("expected Ignore, got %T", funcStmt)
	}
}

func TestCreateFunctionAutoModeFallbackRetry(t *testing.T) {
	// Force Auto mode regardless of an ambient PSQLDEF_PARSER (the dev/CI default
	// is generic, which would skip pgquery entirely and never exercise this path).
	t.Setenv("PSQLDEF_PARSER", "")
	postgresParser := NewParserWithMode(PsqldefParserModeAuto)

	// The table uses a storage parameter the generic parser rejects, forcing a
	// whole-file fallback to pgquery. The CREATE FUNCTION must survive that
	// fallback instead of being dropped as an Ignore, otherwise the diff engine
	// emits a spurious DROP FUNCTION for a function that is in the desired schema.
	statements, err := postgresParser.Parse(`
    CREATE TABLE t (id int) WITH (fillfactor = 70);
    CREATE FUNCTION increment(i integer) RETURNS integer
    LANGUAGE plpgsql
    AS $$
      BEGIN
        RETURN i + 1;
      END;
    $$;
	`)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	var foundFunction bool
	for _, stmt := range statements {
		if _, ok := stmt.Statement.(*parser.Ignore); ok {
			t.Errorf("statement was dropped as Ignore: %q", stmt.DDL)
		}
		if ddl, ok := stmt.Statement.(*parser.DDL); ok && ddl.Action == parser.CreateFunction {
			foundFunction = true
		}
	}
	if !foundFunction {
		t.Error("CREATE FUNCTION was not preserved across pgquery fallback in Auto mode")
	}
}

func TestRangeSubselectWithPgquery(t *testing.T) {
	t.Setenv("PSQLDEF_PARSER", "pgquery")
	postgresParser := NewParserWithMode(PsqldefParserModePgquery)

	statements, err := postgresParser.Parse(`
CREATE VIEW set_view AS
SELECT * FROM ((SELECT 1 AS id) UNION ALL (SELECT 2 AS id)) AS s;
`)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	ddl, ok := statements[0].Statement.(*parser.DDL)
	require.True(t, ok, "expected DDL statement, got %T", statements[0].Statement)
	require.NotNil(t, ddl.View)

	viewDefinition := parser.String(ddl.View.Definition)
	assert.Equal(t, "select * from (select 1 as id union all select 2 as id) as s", viewDefinition)
	assert.NotContains(t, viewDefinition, "from  ")
}

func TestSetOperationAutoFallback(t *testing.T) {
	t.Setenv("PSQLDEF_PARSER", "")
	postgresParser := NewParserWithMode(PsqldefParserModeAuto)

	statements, err := postgresParser.Parse(`
CREATE TABLE t (id int) WITH (fillfactor = 70);
CREATE VIEW set_view AS SELECT 1 AS id UNION ALL SELECT 2 AS id;
`)
	require.NoError(t, err)
	require.Len(t, statements, 2)

	ddl, ok := statements[1].Statement.(*parser.DDL)
	require.True(t, ok, "expected DDL statement, got %T", statements[1].Statement)
	require.NotNil(t, ddl.View)
	assert.Equal(t, "select 1 as id union all select 2 as id", parser.String(ddl.View.Definition))
}

func TestSetOperationVariantsWithPgquery(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "union",
			sql:  "SELECT 1 AS id UNION SELECT 2 AS id",
			want: "select 1 as id union select 2 as id",
		},
		{
			name: "intersect all",
			sql:  "SELECT 1 AS id INTERSECT ALL SELECT 1 AS id",
			want: "select 1 as id intersect all select 1 as id",
		},
		{
			name: "except all",
			sql:  "SELECT 1 AS id EXCEPT ALL SELECT 2 AS id",
			want: "select 1 as id except all select 2 as id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PSQLDEF_PARSER", "pgquery")
			postgresParser := NewParserWithMode(PsqldefParserModePgquery)

			statements, err := postgresParser.Parse("CREATE VIEW set_view AS " + tt.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			ddl, ok := statements[0].Statement.(*parser.DDL)
			require.True(t, ok, "expected DDL statement, got %T", statements[0].Statement)
			require.NotNil(t, ddl.View)
			assert.Equal(t, tt.want, parser.String(ddl.View.Definition))
		})
	}
}

func TestMultipleFromEntriesWithPgquery(t *testing.T) {
	t.Setenv("PSQLDEF_PARSER", "pgquery")
	postgresParser := NewParserWithMode(PsqldefParserModePgquery)

	_, err := postgresParser.Parse(`CREATE VIEW v AS SELECT * FROM a, b;`)
	require.ErrorContains(t, err, "unhandled multiple FROM entries in parseSelectStmt")
}

func TestUnsupportedLateralSubselectWithPgquery(t *testing.T) {
	t.Setenv("PSQLDEF_PARSER", "pgquery")
	postgresParser := NewParserWithMode(PsqldefParserModePgquery)

	_, err := postgresParser.Parse(`CREATE VIEW v AS SELECT * FROM LATERAL (SELECT 1 AS id) AS s;`)
	require.ErrorContains(t, err, "unhandled lateral subquery in parseSelectStmt")
}

func TestUnsupportedSubselectSortWithPgquery(t *testing.T) {
	t.Setenv("PSQLDEF_PARSER", "pgquery")
	postgresParser := NewParserWithMode(PsqldefParserModePgquery)

	_, err := postgresParser.Parse(`CREATE VIEW v AS SELECT * FROM (SELECT 1 AS id ORDER BY id) AS s;`)
	require.ErrorContains(t, err, "unhandled node in parseSelectStmt")
}

func TestAliasColumnNamesWithPgquery(t *testing.T) {
	t.Setenv("PSQLDEF_PARSER", "pgquery")
	postgresParser := NewParserWithMode(PsqldefParserModePgquery)

	statements, err := postgresParser.Parse(`
CREATE VIEW v AS
SELECT * FROM (SELECT 1 AS id, 2 AS other) AS s(a, b);
`)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	ddl, ok := statements[0].Statement.(*parser.DDL)
	require.True(t, ok, "expected DDL statement, got %T", statements[0].Statement)
	require.NotNil(t, ddl.View)

	assert.Equal(t, "select * from (select 1 as id, 2 as other) as s(a, b)", parser.String(ddl.View.Definition))
}

func TestParseCheckConstraintMultiArgBoolExprWithPgquery(t *testing.T) {
	t.Setenv("PSQLDEF_PARSER", "pgquery")
	postgresParser := NewParserWithMode(PsqldefParserModePgquery)

	statements, err := postgresParser.Parse(`
CREATE TABLE test (
  status text NOT NULL,
  quantity integer,
  price integer,
  CONSTRAINT chk CHECK (
    (status = 'active' AND quantity > 0 AND price IS NOT NULL) OR
    (status = 'archived' AND quantity = 0 AND price IS NULL) OR
    (status = 'deleted' AND quantity IS NULL AND price IS NULL)
  )
);
`)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	ddl, ok := statements[0].Statement.(*parser.DDL)
	require.True(t, ok)
	require.Len(t, ddl.TableSpec.Checks, 1)

	checkExpr := parser.String(ddl.TableSpec.Checks[0].Where.Expr)
	expected := "status = 'active' and quantity > 0 and price is not null" +
		" or status = 'archived' and quantity = 0 and price is null" +
		" or status = 'deleted' and quantity is null and price is null"
	assert.Equal(t, expected, checkExpr)
}

func TestCreatePolicyWithPgquery(t *testing.T) {
	t.Setenv("PSQLDEF_PARSER", "pgquery")

	tests := []struct {
		name           string
		sql            string
		wantTable      string
		wantPermissive parser.Permissive
		wantScope      string
		wantRoles      []string
		wantUsing      string
		wantWithCheck  string
	}{
		{
			name: "permissive policy with public role and predicates",
			sql: `
CREATE POLICY tenant_isolation_policy ON public.test_table AS PERMISSIVE FOR ALL TO public
USING ((tenant_id)::uuid = tenant_uuid)
WITH CHECK (tenant_id > 0);
`,
			wantTable:      "public.test_table",
			wantPermissive: parser.Permissive("PERMISSIVE"),
			wantScope:      "ALL",
			wantRoles:      []string{"public"},
			wantUsing:      "tenant_id::uuid = tenant_uuid",
			wantWithCheck:  "tenant_id > 0",
		},
		{
			name: "restrictive policy with named role and using only",
			sql: `
CREATE POLICY p_users ON users AS RESTRICTIVE FOR SELECT TO postgres
USING (id = 1);
`,
			wantTable:      "users",
			wantPermissive: parser.Permissive("RESTRICTIVE"),
			wantScope:      "SELECT",
			wantRoles:      []string{"postgres"},
			wantUsing:      "id = 1",
		},
		{
			name: "policy without roles or predicates",
			sql: `
CREATE POLICY p_all ON users;
`,
			wantTable:      "users",
			wantPermissive: parser.Permissive("PERMISSIVE"),
			wantScope:      "ALL",
			wantRoles:      []string{"public"},
		},
	}

	sqlParser := NewParserWithMode(PsqldefParserModePgquery)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statements, err := sqlParser.Parse(tt.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			ddl, ok := statements[0].Statement.(*parser.DDL)
			require.True(t, ok, "expected DDL statement, got %T", statements[0].Statement)
			require.Equal(t, parser.CreatePolicy, ddl.Action)
			require.NotNil(t, ddl.Policy)

			assert.Equal(t, tt.wantTable, parser.String(ddl.Table))
			assert.Equal(t, tt.wantPermissive, ddl.Policy.Permissive)
			assert.Equal(t, tt.wantScope, ddl.Policy.Scope)
			assert.Equal(t, tt.wantRoles, identNames(ddl.Policy.To))

			if tt.wantUsing == "" {
				assert.Nil(t, ddl.Policy.Using)
			} else {
				require.NotNil(t, ddl.Policy.Using)
				assert.Equal(t, tt.wantUsing, parser.String(ddl.Policy.Using.Expr))
			}

			if tt.wantWithCheck == "" {
				assert.Nil(t, ddl.Policy.WithCheck)
			} else {
				require.NotNil(t, ddl.Policy.WithCheck)
				assert.Equal(t, tt.wantWithCheck, parser.String(ddl.Policy.WithCheck.Expr))
			}
		})
	}
}

func TestCreatePolicyWithPgqueryRejectsUnexpectedRoleNode(t *testing.T) {
	sqlParser := NewParserWithMode(PsqldefParserModePgquery)
	stmt := &pgquery.CreatePolicyStmt{
		Table:      &pgquery.RangeVar{Relname: "users"},
		PolicyName: "p_users",
		CmdName:    "all",
		Roles: []*pgquery.Node{
			{Node: &pgquery.Node_String_{String_: &pgquery.String{Sval: "postgres"}}},
		},
	}

	_, err := sqlParser.parseCreatePolicyStmt(stmt)
	require.EqualError(t, err, "unexpected role type in create policy statement")
}

func TestCreatePolicyWithPgqueryRejectsUnsupportedRoleType(t *testing.T) {
	sqlParser := NewParserWithMode(PsqldefParserModePgquery)
	stmt := &pgquery.CreatePolicyStmt{
		Table:      &pgquery.RangeVar{Relname: "users"},
		PolicyName: "p_users",
		CmdName:    "all",
		Roles: []*pgquery.Node{
			{
				Node: &pgquery.Node_RoleSpec{
					RoleSpec: &pgquery.RoleSpec{Roletype: pgquery.RoleSpecType_ROLESPEC_CURRENT_ROLE},
				},
			},
		},
	}

	_, err := sqlParser.parseCreatePolicyStmt(stmt)
	require.EqualError(t, err, "unsupported role type in create policy statement")
}

func identNames(idents []parser.Ident) []string {
	names := make([]string, 0, len(idents))
	for _, ident := range idents {
		names = append(names, ident.Name)
	}
	return names
}
