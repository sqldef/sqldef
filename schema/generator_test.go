package schema

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			{columnExpr: &parser.ColName{Name: parser.NewIdent("id", false)}, direction: ""},
			{columnExpr: &parser.ColName{Name: parser.NewIdent("name", false)}, direction: ""},
		},
	}

	indexB := Index{
		primary: true,
		columns: []IndexColumn{
			{columnExpr: &parser.ColName{Name: parser.NewIdent("id", false)}, direction: ""},
			{columnExpr: &parser.ColName{Name: parser.NewIdent("name", false)}, direction: ""},
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
			{columnExpr: &parser.ColName{Name: parser.NewIdent("id", false)}, direction: AscScr},
			{columnExpr: &parser.ColName{Name: parser.NewIdent("name", false)}, direction: DescScr},
		},
	}

	indexB := Index{
		primary: true,
		columns: []IndexColumn{
			{columnExpr: &parser.ColName{Name: parser.NewIdent("id", false)}, direction: AscScr},
			{columnExpr: &parser.ColName{Name: parser.NewIdent("name", false)}, direction: AscScr}, // Different direction
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

func TestPostgresCheckConstraintMatching(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		desired  string
		expected []string
	}{
		{
			name: "one current check is not reused",
			current: `CREATE TABLE pair_values (
				a integer,
				b integer,
				CONSTRAINT one_pair_positive CHECK (a > 0 AND b > 0) NO INHERIT
			);`,
			desired: `CREATE TABLE pair_values (
				a integer,
				b integer,
				CHECK (a > 0 AND b > 0) NO INHERIT,
				CHECK (a > 0 AND b > 0) NO INHERIT
			);`,
			expected: []string{
				"ALTER TABLE public.pair_values ADD CHECK (a > 0 AND b > 0) NO INHERIT",
			},
		},
		{
			name: "one desired check is not reused",
			current: `CREATE TABLE pair_values (
				a integer,
				b integer,
				CONSTRAINT first_pair_positive CHECK (a > 0 AND b > 0),
				CONSTRAINT second_pair_positive CHECK (a > 0 AND b > 0)
			);`,
			desired: `CREATE TABLE pair_values (
				a integer,
				b integer,
				CHECK (a > 0 AND b > 0)
			);`,
			expected: []string{
				"ALTER TABLE public.pair_values DROP CONSTRAINT second_pair_positive",
			},
		},
		{
			name: "named checks match before unnamed checks",
			current: `CREATE TABLE pair_values (
				a integer,
				b integer,
				CONSTRAINT first_pair_positive CHECK (a > 0 AND b > 0),
				CONSTRAINT second_pair_positive CHECK (a > 0 AND b > 0)
			);`,
			desired: `CREATE TABLE pair_values (
				a integer,
				b integer,
				CHECK (a > 0 AND b > 0),
				CONSTRAINT first_pair_positive CHECK (a > 0 AND b > 0)
			);`,
			expected: []string{},
		},
		{
			name: "matched check is not duplicated on a new column",
			current: `CREATE TABLE moved_check (
				a integer CONSTRAINT moved_check_a_check CHECK (a > 0)
			);`,
			desired: `CREATE TABLE moved_check (
				a integer,
				b integer CHECK (a > 0)
			);`,
			expected: []string{
				"ALTER TABLE public.moved_check ADD COLUMN b integer",
			},
		},
		{
			name:    "named check on a new column keeps its name",
			current: `CREATE TABLE named_new_column (a integer);`,
			desired: `CREATE TABLE named_new_column (
				a integer,
				b integer CONSTRAINT b_positive CHECK (b > 0)
			);`,
			expected: []string{
				"ALTER TABLE public.named_new_column ADD COLUMN b integer",
				"ALTER TABLE public.named_new_column ADD CONSTRAINT b_positive CHECK (b > 0)",
			},
		},
		{
			name: "matching unnamed check does not require a name",
			current: `CREATE TABLE measurements (
				amount integer CHECK (amount > 0)
			);`,
			desired: `CREATE TABLE measurements (
				amount integer CHECK (amount > 0)
			);`,
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ddls, err := GenerateIdempotentDDLs(
				GeneratorModePostgres,
				database.NewParser(parser.ParserModePostgres),
				tt.desired,
				tt.current,
				database.GeneratorConfig{EnableDrop: true, LegacyIgnoreQuotes: false},
				"public",
			)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, ddls)
		})
	}
}

func TestPostgresUnnamedCurrentCheckDropError(t *testing.T) {
	const expectedError = "cannot drop unnamed PostgreSQL CHECK constraint on table public.measurements: the current schema does not contain the constraint name required by DROP CONSTRAINT; export the current schema from a live database or specify the constraint name explicitly"

	tests := []struct {
		name    string
		current string
		desired string
	}{
		{
			name: "remove column check",
			current: `CREATE TABLE measurements (
				amount integer CHECK (amount > 0)
			);`,
			desired: `CREATE TABLE measurements (
				amount integer
			);`,
		},
		{
			name: "replace table check",
			current: `CREATE TABLE measurements (
				amount integer,
				CHECK (amount > 0)
			);`,
			desired: `CREATE TABLE measurements (
				amount integer,
				CHECK (amount > 1)
			);`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ddls, err := GenerateIdempotentDDLs(
				GeneratorModePostgres,
				database.NewParser(parser.ParserModePostgres),
				tt.desired,
				tt.current,
				database.GeneratorConfig{EnableDrop: true, LegacyIgnoreQuotes: false},
				"public",
			)

			require.EqualError(t, err, expectedError)
			assert.Nil(t, ddls)
		})
	}
}

func TestPostgresUnnamedCurrentCheckDoesNotBlockTableDrop(t *testing.T) {
	current := `CREATE TABLE measurements (
		amount integer CHECK (amount > 0)
	);`

	ddls, err := GenerateIdempotentDDLs(
		GeneratorModePostgres,
		database.NewParser(parser.ParserModePostgres),
		"",
		current,
		database.GeneratorConfig{EnableDrop: true, LegacyIgnoreQuotes: false},
		"public",
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"DROP TABLE public.measurements"}, ddls)
}

func newPostgresCheckGenerator(currentTable, desiredTable *Table) *Generator {
	return &Generator{
		mode:               GeneratorModePostgres,
		currentTables:      []*Table{currentTable},
		desiredTables:      []*Table{desiredTable},
		defaultSchema:      "public",
		config:             database.GeneratorConfig{EnableDrop: true, LegacyIgnoreQuotes: false},
		postgresCheckPlans: make(map[string]*postgresCheckMatchPlan),
	}
}

func TestPostgresCheckConstraintCleanup(t *testing.T) {
	tableName := QualifiedName{Schema: Ident{Name: "public"}, Name: Ident{Name: "measurements"}}

	t.Run("drop named check", func(t *testing.T) {
		currentTable := &Table{
			name:   tableName,
			checks: []CheckDefinition{{constraintName: Ident{Name: "amount_positive"}}},
		}
		desiredTable := &Table{name: tableName}
		generator := newPostgresCheckGenerator(currentTable, desiredTable)

		ddls, err := generator.generateDDLs(nil)

		require.NoError(t, err)
		assert.Equal(t, []string{
			"ALTER TABLE public.measurements DROP CONSTRAINT amount_positive",
		}, ddls)
	})

	t.Run("reject unnamed current check", func(t *testing.T) {
		currentTable := &Table{name: tableName, checks: []CheckDefinition{{}}}
		desiredTable := &Table{name: tableName}
		generator := newPostgresCheckGenerator(currentTable, desiredTable)

		ddls, err := generator.generateDDLs(nil)

		require.EqualError(t, err, "cannot drop unnamed PostgreSQL CHECK constraint on table public.measurements: the current schema does not contain the constraint name required by DROP CONSTRAINT; export the current schema from a live database or specify the constraint name explicitly")
		assert.Nil(t, ddls)
	})
}

func TestPostgresCheckConstraintInvariantPanics(t *testing.T) {
	t.Run("missing desired check", func(t *testing.T) {
		generator := &Generator{mode: GeneratorModePostgres}
		assert.PanicsWithValue(t, "PostgreSQL desired column CHECK constraint not found", func() {
			generator.postgresColumnCheckCanBeAddedInline(&postgresCheckMatchPlan{}, parser.NewIdent("amount", false))
		})
	})

	t.Run("missing desired column", func(t *testing.T) {
		tableName := QualifiedName{Schema: Ident{Name: "public"}, Name: Ident{Name: "measurements"}}
		currentTable := &Table{name: tableName}
		desiredTable := &Table{name: tableName}
		generator := newPostgresCheckGenerator(currentTable, desiredTable)
		plan := generator.postgresCheckMatchPlan(currentTable, desiredTable)
		plan.desired = []postgresCheckEntry{{
			check: new(CheckDefinition),
			location: postgresCheckLocation{
				columnName: Ident{Name: "missing"},
				isColumn:   true,
			},
		}}
		plan.desiredToCurrent = []int{-1}

		assert.PanicsWithValue(t, "PostgreSQL desired CHECK constraint column not found", func() {
			_, _ = generator.generatePostgresCheckDDLs(currentTable, desiredTable)
		})
	})
}

func TestSQLiteCheckConstraintModification(t *testing.T) {
	current := `CREATE TABLE measurements (
		amount integer,
		CONSTRAINT amount_positive CHECK (amount > 0)
	);`
	desired := `CREATE TABLE measurements (
		amount integer,
		CONSTRAINT amount_positive CHECK (amount > 1)
	);`

	ddls, err := GenerateIdempotentDDLs(
		GeneratorModeSQLite3,
		database.NewParser(parser.ParserModeSQLite3),
		desired,
		current,
		database.GeneratorConfig{EnableDrop: true, LegacyIgnoreQuotes: false},
		"",
	)

	require.NoError(t, err)
	assert.Empty(t, ddls)
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
			expected: `select id, (name collate "en_us") as name from users as u join orders as o on u.id = o.user_id`,
		},
		{
			name:     "PostgreSQL: preserve column names without prefixes",
			mode:     GeneratorModePostgres,
			input:    `select id, (name COLLATE "ja-JP-x-icu") as name from users`,
			expected: `select id, (name collate "ja-jp-x-icu") as name from users`,
		},
		{
			name:     "PostgreSQL: normalize ARRAY in function calls",
			mode:     GeneratorModePostgres,
			input:    `select jsonb_extract_path_text(payload, VARIADIC ARRAY['amount']) from events`,
			expected: `select jsonb_extract_path_text(payload, 'amount') from events`,
		},
		{
			name:     "PostgreSQL: normalize ARRAY with multiple elements in function calls",
			mode:     GeneratorModePostgres,
			input:    `select jsonb_extract_path_text(payload, VARIADIC ARRAY['data', 'user', 'name']) from events`,
			expected: `select jsonb_extract_path_text(payload, 'data', 'user', 'name') from events`,
		},
		{
			name:     "PostgreSQL: unwrap redundant set operand parentheses",
			mode:     GeneratorModePostgres,
			input:    `(SELECT 1 AS id) UNION ALL (SELECT 2 AS id)`,
			expected: `select 1 as id union all select 2 as id`,
		},
		{
			name:     "PostgreSQL: preserve ordered limited set operand parentheses",
			mode:     GeneratorModePostgres,
			input:    `(SELECT id FROM items ORDER BY id DESC LIMIT 1) UNION ALL SELECT id FROM items`,
			expected: `(select id from items order by id desc limit 1) union all select id from items`,
		},
		{
			name:     "PostgreSQL: preserve grouped set operation parentheses",
			mode:     GeneratorModePostgres,
			input:    `SELECT 1 AS id EXCEPT ((SELECT 2 AS id) UNION SELECT 3 AS id)`,
			expected: `select 1 as id except (select 2 as id union select 3 as id)`,
		},
		// MySQL should normalize column qualifiers (MySQL adds database.table.column when storing views)
		{
			name:     "MySQL: normalize table qualifiers in SELECT",
			mode:     GeneratorModeMysql,
			input:    `SELECT users.id, users.name FROM users`,
			expected: `select id, name from users`,
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

			// Parse the input SQL into a view definition
			viewSQL := fmt.Sprintf("CREATE VIEW test_view AS %s", tt.input)
			stmt, err := parser.ParseDDL(viewSQL, parser.ParserModePostgres)
			assert.NoError(t, err)

			ddl, ok := stmt.(*parser.DDL)
			assert.True(t, ok, "Statement is not a DDL")
			assert.Equal(t, parser.CreateView, ddl.Action)
			assert.NotNil(t, ddl.View.Definition, "Definition should not be nil")

			normalized := normalizeViewDefinition(ddl.View.Definition, g.mode, nil)
			actual := strings.ToLower(parser.String(normalized))

			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestNormalizeViewDefinitionInParenthesizedSetOperationSubquery(t *testing.T) {
	parseDefinition := func(sql string) parser.SelectStatement {
		t.Helper()
		stmt, err := parser.ParseDDL("CREATE VIEW v AS "+sql, parser.ParserModePostgres)
		assert.NoError(t, err)
		return stmt.(*parser.DDL).View.Definition
	}

	desired := parseDefinition(`SELECT * FROM (
  (SELECT a.id, a.name FROM a JOIN x USING (id))
  UNION ALL
  (SELECT b.id, b.name FROM b JOIN x USING (id))
) t`)
	current := parseDefinition(`SELECT t.id, t.name FROM (
  SELECT a.id, a.name FROM a JOIN x USING (id)
  UNION ALL
  SELECT b.id, b.name FROM b JOIN x USING (id)
) t`)
	tableLookup := func(QualifiedName) *Table { return nil }

	normalize := func(definition parser.SelectStatement) string {
		normalized := normalizeViewDefinition(definition, GeneratorModePostgres, tableLookup)
		return stripTableQualifiers(strings.ToLower(parser.String(normalized)))
	}

	assert.Equal(t, normalize(current), normalize(desired))
}

func TestNormalizeViewDefinitionExpandsStarFromTable(t *testing.T) {
	stmt := &parser.Select{
		SelectExprs: parser.SelectExprs{
			&parser.StarExpr{},
			&parser.AliasedExpr{Expr: parser.NewIntVal("3"), As: parser.NewIdent("marker", false)},
		},
		From: parser.TableExprs{
			&parser.AliasedTableExpr{
				Expr: parser.TableName{Name: parser.NewIdent("users", false)},
			},
		},
	}
	table := &Table{
		columns: map[string]*Column{
			"second": {name: parser.NewIdent("second", false), position: 2},
			"first":  {name: parser.NewIdent("first", false), position: 1},
		},
	}

	normalized := normalizeViewDefinition(stmt, GeneratorModePostgres, func(name QualifiedName) *Table {
		assert.Equal(t, "users", name.Name.Name)
		return table
	})

	assert.Equal(t, "select first, second, 3 as marker from users", parser.String(normalized))
}

func TestNormalizeTableExprParentheses(t *testing.T) {
	tableExpr := func(name string) *parser.AliasedTableExpr {
		return &parser.AliasedTableExpr{
			Expr: parser.TableName{Name: parser.NewIdent(name, false)},
		}
	}

	assert.Nil(t, normalizeTableExpr(nil, GeneratorModePostgres, nil))
	assert.Equal(t, tableExpr("a"), normalizeTableExpr(
		&parser.ParenTableExpr{Exprs: parser.TableExprs{tableExpr("a")}},
		GeneratorModePostgres,
		nil,
	))

	normalized := normalizeTableExpr(
		&parser.ParenTableExpr{Exprs: parser.TableExprs{tableExpr("a"), tableExpr("b")}},
		GeneratorModeSQLite3,
		nil,
	)
	paren, ok := normalized.(*parser.ParenTableExpr)
	assert.True(t, ok)
	assert.Len(t, paren.Exprs, 2)
}

func TestExtractSubqueryColumnsFromFrom(t *testing.T) {
	id := parser.NewIdent("id", false)
	alias := parser.NewIdent("alias", false)
	subquery := func(selectExprs parser.SelectExprs) *parser.AliasedTableExpr {
		return &parser.AliasedTableExpr{
			Expr: &parser.Subquery{
				Select: &parser.Select{SelectExprs: selectExprs},
			},
		}
	}

	tests := []struct {
		name     string
		from     parser.TableExprs
		expected parser.Columns
	}{
		{name: "empty FROM"},
		{
			name: "multiple FROM expressions",
			from: parser.TableExprs{subquery(nil), subquery(nil)},
		},
		{
			name: "non-aliased expression",
			from: parser.TableExprs{&parser.JoinTableExpr{}},
		},
		{
			name: "aliased table",
			from: parser.TableExprs{
				&parser.AliasedTableExpr{Expr: parser.TableName{Name: parser.NewIdent("users", false)}},
			},
		},
		{
			name: "explicit alias columns",
			from: parser.TableExprs{
				&parser.AliasedTableExpr{
					Expr:    &parser.Subquery{Select: &parser.Select{}},
					Columns: parser.Columns{id, alias},
				},
			},
			expected: parser.Columns{id, alias},
		},
		{
			name: "inferred columns",
			from: parser.TableExprs{subquery(parser.SelectExprs{
				&parser.AliasedExpr{Expr: &parser.ColName{Name: id}},
				&parser.AliasedExpr{Expr: parser.NewIntVal("1"), As: alias},
			})},
			expected: parser.Columns{id, alias},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractSubqueryColumnsFromFrom(tt.from))
		})
	}
}

func TestExtractSelectOutputColumns(t *testing.T) {
	id := parser.NewIdent("id", false)
	alias := parser.NewIdent("alias", false)
	selectWithColumns := &parser.Select{SelectExprs: parser.SelectExprs{
		&parser.AliasedExpr{Expr: &parser.ColName{Name: id}},
		&parser.AliasedExpr{Expr: parser.NewIntVal("1"), As: alias},
	}}

	tests := []struct {
		name     string
		stmt     parser.SelectStatement
		expected parser.Columns
	}{
		{name: "nil statement"},
		{
			name:     "select",
			stmt:     selectWithColumns,
			expected: parser.Columns{id, alias},
		},
		{
			name: "non-aliased select expression",
			stmt: &parser.Select{SelectExprs: parser.SelectExprs{&parser.StarExpr{}}},
		},
		{
			name: "anonymous non-column expression",
			stmt: &parser.Select{SelectExprs: parser.SelectExprs{
				&parser.AliasedExpr{Expr: parser.NewIntVal("1")},
			}},
		},
		{
			name: "union uses left output",
			stmt: &parser.Union{
				Left:  selectWithColumns,
				Right: &parser.Select{},
			},
			expected: parser.Columns{id, alias},
		},
		{
			name:     "parenthesized select",
			stmt:     &parser.ParenSelect{Select: selectWithColumns},
			expected: parser.Columns{id, alias},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractSelectOutputColumns(tt.stmt))
		})
	}
}

func TestNormalizeViewDefinitionPreservesTableAliasColumns(t *testing.T) {
	stmt := &parser.Select{
		SelectExprs: parser.SelectExprs{&parser.StarExpr{}},
		From: parser.TableExprs{
			&parser.AliasedTableExpr{
				Expr: &parser.Subquery{
					Select: &parser.Select{
						SelectExprs: parser.SelectExprs{
							&parser.AliasedExpr{
								Expr: parser.NewIntVal("1"),
								As:   parser.NewIdent("id", false),
							},
						},
					},
				},
				As: parser.NewIdent("s", false),
				Columns: parser.Columns{
					parser.NewIdent("a", false),
					parser.NewIdent("b", false),
				},
			},
		},
	}

	normalized := normalizeViewDefinition(stmt, GeneratorModePostgres, nil)

	assert.Equal(t, "select * from (select 1 as id) as s(a, b)", parser.String(normalized))
}

func TestNormalizeCheckExpr(t *testing.T) {
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
			input:    "status = ANY (ARRAY['active'::text, 'pending'::text])",
			expected: "status = ANY (ARRAY['active', 'pending'])",
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
			input:    "status = ANY (ARRAY['active'::text, 'pending'::text, 'processing'::text])",
			expected: "status = ANY (ARRAY['active', 'pending', 'processing'])",
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
		// Test unwrapOutermostParenExpr behavior in AND/OR contexts
		// Note: outermost parens are preserved by normalizeCheckExpr,
		// they're only unwrapped in areSameCheckDefinition
		{
			name:     "Unwrap unnecessary parens in AND operands",
			input:    "(a = 1) and (b = 2)",
			expected: "a = 1 and b = 2",
		},
		{
			name:     "Preserve parens around OR in AND expression",
			input:    "a = 1 and (b = 2 or c = 3)",
			expected: "a = 1 and (b = 2 or c = 3)",
		},
		{
			name:     "Preserve parens around OR in both AND operands",
			input:    "(a = 1 or b = 2) and (c = 3 or d = 4)",
			expected: "(a = 1 or b = 2) and (c = 3 or d = 4)",
		},
		{
			name:     "Mixed: unwrap AND but preserve OR",
			input:    "(a = 1 and b = 2) and (c = 3 or d = 4)",
			expected: "a = 1 and b = 2 and (c = 3 or d = 4)",
		},
		{
			name:     "Unwrap parens in OR operands",
			input:    "(a = 1) or (b = 2)",
			expected: "a = 1 or b = 2",
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
			normalized := normalizeCheckExpr(check.Where.Expr, GeneratorModeMysql)

			// Convert normalized expression to string
			actual := parser.String(normalized)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestNormalizeCheckExprNilInput(t *testing.T) {
	result := normalizeCheckExpr(nil, GeneratorModeMysql)
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
	normalized1 := normalizeCheckExpr(check1.Where.Expr, GeneratorModeMysql)
	normalized2 := normalizeCheckExpr(check2.Where.Expr, GeneratorModeMysql)

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
	normalized1 := normalizeCheckExpr(check1.Where.Expr, GeneratorModeMysql)
	normalized2 := normalizeCheckExpr(check2.Where.Expr, GeneratorModeMysql)

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

func TestAreSameForeignKeysConstraintOptionsNilVsDefault(t *testing.T) {
	g := &Generator{mode: GeneratorModePostgres}

	fkNil := ForeignKey{
		constraintName:     Ident{Name: "fk_test"},
		indexColumns:       []Ident{{Name: "user_id"}},
		referenceTableName: QualifiedName{Schema: Ident{Name: "public"}, Name: Ident{Name: "users"}},
		referenceColumns:   []Ident{{Name: "user_id"}},
		onDelete:           "RESTRICT",
		onUpdate:           "NO ACTION",
		constraintOptions:  nil,
	}

	fkDefault := ForeignKey{
		constraintName:     Ident{Name: "fk_test"},
		indexColumns:       []Ident{{Name: "user_id"}},
		referenceTableName: QualifiedName{Schema: Ident{Name: "public"}, Name: Ident{Name: "users"}},
		referenceColumns:   []Ident{{Name: "user_id"}},
		onDelete:           "RESTRICT",
		onUpdate:           "NO ACTION",
		constraintOptions:  &ConstraintOptions{deferrable: false, initiallyDeferred: false},
	}

	assert.True(t, g.areSameForeignKeys(fkNil, fkDefault),
		"FK with nil ConstraintOptions and FK with default ConstraintOptions{false, false} should be considered the same")
	assert.True(t, g.areSameForeignKeys(fkDefault, fkNil),
		"FK with default ConstraintOptions{false, false} and FK with nil ConstraintOptions should be considered the same")
}

func TestAlterBundler(t *testing.T) {
	g := &Generator{mode: GeneratorModeMysql}
	tableA := &Table{name: QualifiedName{Name: Ident{Name: "a"}}}
	tableB := &Table{name: QualifiedName{Name: Ident{Name: "b"}}}

	bundler := newAlterBundler(g, true)

	slotA := bundler.emit(tableA, "ALTER TABLE a ADD COLUMN x int")
	assert.NotEqual(t, "ALTER TABLE a ADD COLUMN x int", slotA, "first action should be replaced by a placeholder")

	slotB := bundler.emit(tableB, "ALTER TABLE b ADD COLUMN z int")
	assert.NotEqual(t, slotA, slotB, "each table gets its own placeholder")

	folded := bundler.emit(tableA, "ALTER TABLE a DROP COLUMN y")
	assert.Equal(t, "", folded, "subsequent same-table action should fold, not emit")

	other := bundler.emit(tableA, "DROP INDEX idx ON a")
	assert.Equal(t, "DROP INDEX idx ON a", other, "non-ALTER statement should pass through")

	ddls := bundler.finalize([]string{slotA, slotB, "DROP INDEX idx ON a"})
	assert.Equal(t, []string{
		"ALTER TABLE a ADD COLUMN x int, DROP COLUMN y",
		"ALTER TABLE b ADD COLUMN z int",
		"DROP INDEX idx ON a",
	}, ddls)
}

func TestAlterBundlerDisabledPassesThrough(t *testing.T) {
	g := &Generator{mode: GeneratorModeMysql}
	table := &Table{name: QualifiedName{Name: Ident{Name: "a"}}}
	bundler := newAlterBundler(g, false)

	stmt := bundler.emit(table, "ALTER TABLE a ADD COLUMN x int")
	assert.Equal(t, "ALTER TABLE a ADD COLUMN x int", stmt)

	ddls := bundler.finalize([]string{stmt})
	assert.Equal(t, []string{"ALTER TABLE a ADD COLUMN x int"}, ddls)
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

	// Normalize both (use MySQL mode since this test is for MSSQL/MySQL behavior)
	normalizedUser := normalizeCheckExpr(checkUser.Where.Expr, GeneratorModeMysql)
	normalizedDB := normalizeCheckExpr(checkDB.Where.Expr, GeneratorModeMysql)

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

func TestIsDropStatement(t *testing.T) {
	// Destructive statements are detected by their leading keyword.
	assert.True(t, isDropStatement(`DROP TABLE "public"."users"`))
	assert.True(t, isDropStatement("DROP FUNCTION public.add_one"))
	assert.True(t, isDropStatement("DROP EVENT `cleanup`"))
	assert.True(t, isDropStatement(`REVOKE SELECT ON TABLE users FROM app_user`))

	// Destructive clauses embedded in ALTER TABLE are detected.
	assert.True(t, isDropStatement(`ALTER TABLE "public"."users" DROP COLUMN "name"`))
	assert.True(t, isDropStatement("ALTER TABLE `users` DROP INDEX `idx_name`"))
	assert.True(t, isDropStatement("ALTER TABLE `logs` DROP PARTITION p2024"))
	assert.True(t, isDropStatement(`ALTER TABLE public.users DISABLE ROW LEVEL SECURITY`))
	assert.True(t, isDropStatement(`ALTER TABLE public.users NO FORCE ROW LEVEL SECURITY`))

	// Non-destructive ALTER clauses stay allowed (needed for non-destructive
	// schema changes).
	assert.False(t, isDropStatement(`ALTER TABLE users DROP CONSTRAINT users_check`))
	assert.False(t, isDropStatement(`ALTER TABLE users ALTER COLUMN c DROP DEFAULT`))
	assert.False(t, isDropStatement("ALTER TABLE `users` DROP FOREIGN KEY `fk_users`"))

	// Additive statements are never destructive, even when their payload
	// mentions destructive keywords (function bodies, comment text, literals).
	assert.False(t, isDropStatement("CREATE FUNCTION intercept_ddl() RETURNS event_trigger AS $$\nBEGIN\n  IF tg_tag = 'DROP TABLE' THEN RAISE NOTICE 'x'; END IF;\nEND;\n$$ LANGUAGE plpgsql;"))
	assert.False(t, isDropStatement("CREATE OR REPLACE FUNCTION f() RETURNS void AS $$\n-- REVOKE and DROP TABLE only appear in this comment\nBEGIN END;\n$$ LANGUAGE plpgsql;"))
	assert.False(t, isDropStatement(`COMMENT ON TABLE "public"."audit_log" IS 'rows written when a DROP TABLE happens'`))
	assert.False(t, isDropStatement(`COMMENT ON COLUMN "public"."users"."flags" IS 'set after REVOKE runs'`))
	assert.False(t, isDropStatement(`ALTER TABLE audit ADD CONSTRAINT note_ck CHECK (note <> 'DROP TABLE')`))
	assert.False(t, isDropStatement(`ALTER TABLE users ALTER COLUMN c SET DEFAULT 'DROP TABLE'`))
	assert.False(t, isDropStatement("ALTER EVENT `cleanup` ON SCHEDULE EVERY 1 DAY DO\nDELETE FROM logs WHERE note = 'DROP TABLE'"))
	assert.False(t, isDropStatement("-- audit helper\nCREATE FUNCTION f() RETURNS void AS $$ SELECT 'DROP TABLE' $$ LANGUAGE sql;"))
}

func TestCommentOutDropStatements(t *testing.T) {
	none := database.GeneratorConfig{}
	withPrivileges := database.GeneratorConfig{ManagePrivileges: &[]database.ManageObjectRule{}}
	withFunctions := database.GeneratorConfig{ManageFunctions: &[]database.ManageObjectRule{}}

	// Single-line drops keep the existing format.
	assert.Equal(t,
		[]string{`-- Skipped: DROP TABLE "public"."users"`},
		commentOutDropStatements([]string{`DROP TABLE "public"."users"`}, none),
	)
	// Non-drop statements pass through unchanged.
	assert.Equal(t,
		[]string{"CREATE TABLE users (id bigint)"},
		commentOutDropStatements([]string{"CREATE TABLE users (id bigint)"}, none),
	)
	// manage.privilege keeps REVOKE statements executable (revoke gating is
	// already decided per grantee at emission time).
	assert.Equal(t,
		[]string{`REVOKE SELECT ON TABLE users FROM app_user`},
		commentOutDropStatements([]string{`REVOKE SELECT ON TABLE users FROM app_user`}, withPrivileges),
	)
	// manage.function keeps DROP FUNCTION executable (drop gating is already
	// decided per function; a forbidden one already carries "-- Skipped: ").
	assert.Equal(t,
		[]string{`DROP FUNCTION "public"."managed_fn"`},
		commentOutDropStatements([]string{`DROP FUNCTION "public"."managed_fn"`}, withFunctions),
	)
	// Without manage.function, DROP FUNCTION is still gated by enable_drop.
	assert.Equal(t,
		[]string{`-- Skipped: DROP FUNCTION "public"."f"`},
		commentOutDropStatements([]string{`DROP FUNCTION "public"."f"`}, none),
	)
	// Every line of a multi-line statement is commented out so no executable
	// SQL can leak after the first line.
	assert.Equal(t,
		[]string{"-- Skipped: DROP TABLE users;\n-- DROP TABLE orders;"},
		commentOutDropStatements([]string{"DROP TABLE users;\nDROP TABLE orders;"}, none),
	)
}

func TestGateFunctionDropDDL(t *testing.T) {
	drop := `DROP FUNCTION "public"."f"`

	// Legacy mode (manage.function unset): unchanged; the global enable_drop
	// pass handles it.
	assert.Equal(t, drop, gateFunctionDropDDL(database.GeneratorConfig{}, "f", drop))

	// Matched rule with drop:true → allowed (bare DDL).
	allow := database.GeneratorConfig{ManageFunctions: &[]database.ManageObjectRule{{Target: "f", Drop: true}}}
	assert.Equal(t, drop, gateFunctionDropDDL(allow, "f", drop))

	// Matched rule with drop:false → skipped.
	forbid := database.GeneratorConfig{ManageFunctions: &[]database.ManageObjectRule{{Target: "f"}}}
	assert.Equal(t, "-- Skipped: "+drop, gateFunctionDropDDL(forbid, "f", drop))

	// No rule matches → skipped (unmanaged functions are never dropped).
	other := database.GeneratorConfig{ManageFunctions: &[]database.ManageObjectRule{{Target: "other", Drop: true}}}
	assert.Equal(t, "-- Skipped: "+drop, gateFunctionDropDDL(other, "f", drop))
}

func TestIsManagedFunction(t *testing.T) {
	config := database.GeneratorConfig{ManageFunctions: &[]database.ManageObjectRule{{Target: "app_.*"}}}

	// Default schema (empty or explicit "public") + matching name → managed.
	assert.True(t, isManagedFunction(config, "public", "", "app_touch"))
	assert.True(t, isManagedFunction(config, "public", "public", "app_touch"))

	// Matching name but a non-default schema → not managed (scoped to default).
	assert.False(t, isManagedFunction(config, "public", "s2", "app_touch"))

	// Default schema but no rule matches → not managed.
	assert.False(t, isManagedFunction(config, "public", "public", "other_fn"))
}

func TestInsertOrReplaceIntoCreateFunction(t *testing.T) {
	// OR REPLACE is spliced after CREATE, preserving the original casing.
	got, ok := insertOrReplaceIntoCreateFunction("CREATE FUNCTION f() RETURNS integer AS $$ SELECT 1 $$ LANGUAGE sql;")
	assert.True(t, ok)
	assert.Equal(t, "CREATE OR REPLACE FUNCTION f() RETURNS integer AS $$ SELECT 1 $$ LANGUAGE sql;", got)

	got, ok = insertOrReplaceIntoCreateFunction("create function f() returns integer as $$ select 1 $$ language sql;")
	assert.True(t, ok)
	assert.Equal(t, "create OR REPLACE function f() returns integer as $$ select 1 $$ language sql;", got)

	// A comment between CREATE and FUNCTION stays in place and the result is
	// still valid SQL.
	got, ok = insertOrReplaceIntoCreateFunction("CREATE /* c */ FUNCTION f() RETURNS integer AS $$ SELECT 1 $$ LANGUAGE sql;")
	assert.True(t, ok)
	assert.Equal(t, "CREATE OR REPLACE /* c */ FUNCTION f() RETURNS integer AS $$ SELECT 1 $$ LANGUAGE sql;", got)

	_, ok = insertOrReplaceIntoCreateFunction("  CREATE FUNCTION f() RETURNS integer AS $$ SELECT 1 $$ LANGUAGE sql;")
	assert.True(t, ok)

	// A leading comment defeats the splice; callers fall back to DROP+CREATE.
	_, ok = insertOrReplaceIntoCreateFunction("-- note\nCREATE FUNCTION f() RETURNS integer AS $$ SELECT 1 $$ LANGUAGE sql;")
	assert.False(t, ok)
}

func TestAreSameFunctionSignature(t *testing.T) {
	g := &Generator{mode: GeneratorModePostgres}
	fn := func(returnType string, args ...FunctionArg) *Function {
		return &Function{returnType: returnType, args: args}
	}
	arg := func(name, typ string) FunctionArg {
		return FunctionArg{name: parser.NewIdent(name, false), typ: typ}
	}

	base := fn("integer", arg("x", "integer"), arg("y", "text"))
	assert.True(t, g.areSameFunctionSignature(base, fn("integer", arg("x", "integer"), arg("y", "text"))))

	// Common type aliases equal their canonical spelling (the current side
	// always comes from pg_get_functiondef, which prints canonical names).
	assert.True(t, g.areSameFunctionSignature(base, fn("int", arg("x", "int4"), arg("y", "text"))))
	assert.True(t, g.areSameFunctionSignature(
		fn("character varying", arg("v", "character varying")),
		fn("varchar", arg("v", "varchar"))))
	assert.True(t, g.areSameFunctionSignature(
		fn("integer[]", arg("xs", "integer[]")),
		fn("int[]", arg("xs", "int[]"))))

	// Unquoted argument names fold to lower case; quoted ones keep their case.
	assert.True(t, g.areSameFunctionSignature(base, fn("integer", arg("X", "integer"), arg("y", "text"))))
	quoted := fn("integer", FunctionArg{name: parser.NewIdent("X", true), typ: "integer"}, arg("y", "text"))
	assert.False(t, g.areSameFunctionSignature(quoted, base))

	// Adding a name to an unnamed parameter is allowed; renaming is not.
	unnamed := fn("integer", arg("", "integer"), arg("y", "text"))
	assert.True(t, g.areSameFunctionSignature(unnamed, base))
	assert.False(t, g.areSameFunctionSignature(base, fn("integer", arg("z", "integer"), arg("y", "text"))))

	// Changed return type / arg type / arity are never replaceable.
	assert.False(t, g.areSameFunctionSignature(base, fn("text", arg("x", "integer"), arg("y", "text"))))
	assert.False(t, g.areSameFunctionSignature(base, fn("integer", arg("x", "bigint"), arg("y", "text"))))
	assert.False(t, g.areSameFunctionSignature(base, fn("integer", arg("x", "integer"))))

	// Argument modes are part of the identity ("" means IN).
	outArg := fn("integer", FunctionArg{mode: "OUT", name: parser.NewIdent("x", false), typ: "integer"}, arg("y", "text"))
	assert.False(t, g.areSameFunctionSignature(base, outArg))
	inExplicit := fn("integer", FunctionArg{mode: "IN", name: parser.NewIdent("x", false), typ: "integer"}, arg("y", "text"))
	assert.True(t, g.areSameFunctionSignature(base, inExplicit))

	// RETURNS TABLE(...) loses its column list in parsing, so it is never
	// considered replaceable.
	assert.False(t, g.areSameFunctionSignature(fn("TABLE"), fn("TABLE")))
}

func TestDropFunctionDDL(t *testing.T) {
	g := &Generator{mode: GeneratorModePostgres, defaultSchema: "public"}
	name := database.QualifiedName{Schema: parser.NewIdent("public", false), Name: parser.NewIdent("f", false)}

	// Identity argument types are appended so overloads stay unambiguous.
	fn := &Function{name: name, args: []FunctionArg{
		{name: parser.NewIdent("x", false), typ: "integer"},
		{mode: "VARIADIC", name: parser.NewIdent("rest", false), typ: "text[]"},
	}}
	assert.Equal(t, "DROP FUNCTION "+g.escapeQualifiedName(name)+"(integer, VARIADIC text[])", g.dropFunctionDDL(fn))

	// Zero arguments.
	assert.Equal(t, "DROP FUNCTION "+g.escapeQualifiedName(name)+"()", g.dropFunctionDDL(&Function{name: name}))

	// OUT parameters are not part of the identity: keep the bare form.
	outFn := &Function{name: name, args: []FunctionArg{{mode: "OUT", name: parser.NewIdent("x", false), typ: "integer"}}}
	assert.Equal(t, "DROP FUNCTION "+g.escapeQualifiedName(name), g.dropFunctionDDL(outFn))
}
