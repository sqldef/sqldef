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

func TestSkipExtension(t *testing.T) {
	manageAllExtensions := &[]database.ManageObjectRule{{Target: ".*", Drop: true}}
	tests := []struct {
		name     string
		desired  string
		current  string
		config   database.GeneratorConfig
		expected []string
	}{
		{
			name:     "desired extension",
			desired:  "CREATE EXTENSION pgcrypto;",
			config:   database.GeneratorConfig{SkipExtension: true},
			expected: []string{},
		},
		{
			name:     "current extension",
			current:  "CREATE EXTENSION pgcrypto;",
			config:   database.GeneratorConfig{SkipExtension: true, EnableDrop: true},
			expected: []string{},
		},
		{
			name:    "manage.extension match",
			desired: "CREATE EXTENSION pgcrypto;",
			config: database.GeneratorConfig{
				SkipExtension:    true,
				ManageExtensions: manageAllExtensions,
			},
			expected: []string{},
		},
		{
			name:    "non-extension DDL remains",
			desired: "CREATE EXTENSION pgcrypto; CREATE TABLE users (id bigint);",
			config:  database.GeneratorConfig{SkipExtension: true},
			expected: []string{
				"CREATE TABLE users (id bigint)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.LegacyIgnoreQuotes = false
			ddls, err := GenerateIdempotentDDLs(
				GeneratorModePostgres,
				database.NewParser(parser.ParserModePostgres),
				tt.desired,
				tt.current,
				tt.config,
				"public",
			)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, ddls)
		})
	}
}

func TestAreSamePrimaryKeyColumnsMutation(t *testing.T) {
	// Test that areSamePrimaryKeyColumns doesn't mutate the input indexes
	g := &Generator{dialect: dialect{mode: GeneratorModeMysql}}

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
	g := &Generator{dialect: dialect{mode: GeneratorModeMysql}}

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

func TestPostgresIndexMatching(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		desired  string
		expected []string
	}{
		{
			name: "one current index is not reused",
			current: `CREATE TABLE t (a integer);
				CREATE INDEX t_a_idx ON t (a);`,
			desired: `CREATE TABLE t (a integer);
				CREATE INDEX ON t (a);
				CREATE INDEX ON t (a);`,
			expected: []string{
				"CREATE INDEX ON public.t (a)",
			},
		},
		{
			name: "one desired index is not reused",
			current: `CREATE TABLE t (a integer);
				CREATE INDEX t_a_idx ON t (a);
				CREATE INDEX t_a_idx1 ON t (a);`,
			desired: `CREATE TABLE t (a integer);
				CREATE INDEX ON t (a);`,
			expected: []string{
				"DROP INDEX public.t_a_idx1",
			},
		},
		{
			name: "named indexes match before unnamed indexes",
			current: `CREATE TABLE t (a integer);
				CREATE INDEX my_idx ON t (a);`,
			desired: `CREATE TABLE t (a integer);
				CREATE INDEX ON t (a);
				CREATE INDEX my_idx ON t (a);`,
			expected: []string{
				"CREATE INDEX ON public.t (a)",
			},
		},
		{
			name: "current indexes are matched in name order",
			current: `CREATE TABLE t (a integer);
				CREATE INDEX t_a_idx1 ON t (a);
				CREATE INDEX t_a_idx ON t (a);`,
			desired: `CREATE TABLE t (a integer);
				CREATE INDEX ON t (a);`,
			expected: []string{
				"DROP INDEX public.t_a_idx1",
			},
		},
		{
			name: "unnamed current index matches unnamed desired index",
			current: `CREATE TABLE t (a integer);
				CREATE INDEX ON t (a);`,
			desired: `CREATE TABLE t (a integer);
				CREATE INDEX ON t (a);`,
			expected: []string{},
		},
		{
			name: "one current unique constraint is not reused",
			current: `CREATE TABLE t (a integer);
				ALTER TABLE t ADD CONSTRAINT t_a_key UNIQUE (a);`,
			desired: `CREATE TABLE t (a integer);
				ALTER TABLE t ADD UNIQUE (a);
				ALTER TABLE t ADD UNIQUE (a);`,
			expected: []string{
				"ALTER TABLE t ADD UNIQUE (a)",
			},
		},
		{
			name: "unnamed current unique constraints declared twice in CREATE TABLE are one",
			current: `CREATE TABLE t (
				a integer,
				UNIQUE (a),
				UNIQUE (a)
			);`,
			desired: `CREATE TABLE t (
				a integer,
				UNIQUE (a)
			);`,
			expected: []string{},
		},
		{
			name: "unnamed unique constraints declared twice in CREATE TABLE are one",
			current: `CREATE TABLE t (
				a integer,
				CONSTRAINT t_a_key UNIQUE (a)
			);`,
			desired: `CREATE TABLE t (
				a integer,
				UNIQUE (a),
				UNIQUE (a)
			);`,
			expected: []string{},
		},
		{
			name: "one desired unique constraint is not reused",
			current: `CREATE TABLE t (
				a integer,
				CONSTRAINT t_a_key UNIQUE (a),
				CONSTRAINT t_a_key1 UNIQUE (a)
			);`,
			desired: `CREATE TABLE t (
				a integer,
				UNIQUE (a)
			);`,
			expected: []string{
				"ALTER TABLE public.t DROP CONSTRAINT t_a_key1",
			},
		},
		{
			name: "named unique constraints match before unnamed unique constraints",
			current: `CREATE TABLE t (a integer);
				ALTER TABLE t ADD CONSTRAINT my_key UNIQUE (a);`,
			desired: `CREATE TABLE t (a integer);
				ALTER TABLE t ADD UNIQUE (a);
				ALTER TABLE t ADD CONSTRAINT my_key UNIQUE (a);`,
			expected: []string{
				"ALTER TABLE t ADD UNIQUE (a)",
			},
		},
		{
			name: "current unique constraints are matched in name order",
			current: `CREATE TABLE t (
				a integer,
				CONSTRAINT t_a_key1 UNIQUE (a),
				CONSTRAINT t_a_key UNIQUE (a)
			);`,
			desired: `CREATE TABLE t (
				a integer,
				UNIQUE (a)
			);`,
			expected: []string{
				"ALTER TABLE public.t DROP CONSTRAINT t_a_key1",
			},
		},
		{
			name: "unnamed current unique constraint matches unnamed desired unique constraint",
			current: `CREATE TABLE t (
				a integer,
				UNIQUE (a)
			);`,
			desired: `CREATE TABLE t (
				a integer,
				UNIQUE (a)
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
			require.NoError(t, err)
			assert.Equal(t, tt.expected, ddls)
		})
	}
}

func TestPostgresIndexMatchInvariantPanics(t *testing.T) {
	generator := &Generator{dialect: dialect{mode: GeneratorModePostgres}}
	assert.PanicsWithValue(t, "PostgreSQL desired index not found", func() {
		generator.claimPostgresIndex(&postgresIndexMatchPlan{}, Index{})
	})
}

func TestPostgresUnnamedCurrentIndexDropError(t *testing.T) {
	tests := []struct {
		name          string
		current       string
		desired       string
		expectedError string
	}{
		{
			name: "remove index",
			current: `CREATE TABLE t (a integer);
				CREATE INDEX ON t (a);`,
			desired:       `CREATE TABLE t (a integer);`,
			expectedError: "cannot drop unnamed PostgreSQL index on table public.t: the current schema does not contain the index name required by DROP INDEX; export the current schema from a live database or specify the index name explicitly",
		},
		{
			name: "remove unique constraint",
			current: `CREATE TABLE t (
				a integer,
				UNIQUE (a)
			);`,
			desired:       `CREATE TABLE t (a integer);`,
			expectedError: "cannot drop unnamed PostgreSQL UNIQUE constraint on table public.t: the current schema does not contain the constraint name required by DROP CONSTRAINT; export the current schema from a live database or specify the constraint name explicitly",
		},
		{
			name: "remove index on materialized view",
			current: `CREATE TABLE t (a integer);
				CREATE MATERIALIZED VIEW mv AS SELECT a FROM t;
				CREATE INDEX ON mv (a);`,
			desired: `CREATE TABLE t (a integer);
				CREATE MATERIALIZED VIEW mv AS SELECT a FROM t;`,
			expectedError: "cannot drop unnamed PostgreSQL index on table public.mv: the current schema does not contain the index name required by DROP INDEX; export the current schema from a live database or specify the index name explicitly",
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

			require.EqualError(t, err, tt.expectedError)
			assert.Nil(t, ddls)
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
		dialect:            dialect{mode: GeneratorModePostgres, defaultSchema: "public"},
		currentTables:      []*Table{currentTable},
		desiredTables:      []*Table{desiredTable},
		config:             database.GeneratorConfig{EnableDrop: true},
		postgresCheckPlans: make(map[string]*postgresCheckMatchPlan),
		postgresIndexPlans: make(map[string]*postgresIndexMatchPlan),
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
		generator := &Generator{dialect: dialect{mode: GeneratorModePostgres}}
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
			g := &Generator{dialect: dialect{mode: tt.mode}}

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
	g := &Generator{dialect: dialect{mode: GeneratorModePostgres}}

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

func TestDestructiveStatements(t *testing.T) {
	users := QualifiedName{Name: Ident{Name: "users"}}
	mysql := &Generator{dialect: dialect{mode: GeneratorModeMysql}}
	postgres := &Generator{dialect: dialect{mode: GeneratorModePostgres, defaultSchema: "public"}}

	// Dropping data, an index or a partition, or turning row level security off, is destructive.
	assert.True(t, mysql.alterTable(users, dropColumnAction{column: Ident{Name: "name"}}).Destructive())
	assert.True(t, mysql.alterTable(users, dropIndexAction{name: Ident{Name: "idx_name"}}).Destructive())
	assert.True(t, mysql.alterTable(users, dropPartitionAction{name: "p2024"}).Destructive())
	assert.True(t, postgres.alterTable(users, disableRowLevelSecurityAction{}).Destructive())
	assert.True(t, postgres.alterTable(users, disableRowLevelSecurityAction{force: true}).Destructive())
	assert.True(t, postgres.generateDropIndex(users, Ident{Name: "idx_name"}, false).Destructive())
	assert.True(t, postgres.inputAlterTable(&SetRowLevelSecurity{tableName: users, value: false}).Destructive())

	// Dropping an object or revoking a privilege is destructive.
	assert.True(t, dropObjectStatement{d: postgres.dialect, kind: "TABLE", name: users}.Destructive())
	assert.True(t, dropTriggerStatement{d: postgres.dialect, name: QualifiedName{Name: Ident{Name: "t"}}, table: users}.Destructive())
	assert.True(t, dropPolicyStatement{d: postgres.dialect, name: Ident{Name: "p"}, table: users}.Destructive())
	assert.True(t, dropFunctionStatement{d: postgres.dialect, name: QualifiedName{Name: Ident{Name: "f"}}}.Destructive())
	assert.True(t, dropExtensionStatement{d: postgres.dialect, name: Ident{Name: "pgcrypto"}}.Destructive())
	assert.True(t, revokeStatement{d: postgres.dialect, privileges: []Privilege{{Name: "SELECT"}}, object: users, grantee: "app_user"}.Destructive())

	// One destructive action makes the whole statement destructive.
	assert.True(t, mysql.alterTable(users,
		addColumnAction{column: Column{name: Ident{Name: "a"}, typeName: "int"}},
		dropColumnAction{column: Ident{Name: "name"}},
	).Destructive())

	// Drops that non-destructive changes need are not destructive.
	assert.False(t, postgres.alterTable(users, dropConstraintAction{name: Ident{Name: "users_check"}}).Destructive())
	assert.False(t, postgres.generateDropIndex(users, Ident{Name: "users_key"}, true).Destructive())
	assert.False(t, postgres.alterTable(users, alterColumnDefaultAction{column: Ident{Name: "c"}}).Destructive())
	assert.False(t, mysql.alterTable(users, dropForeignKeyAction{name: Ident{Name: "fk_users"}}).Destructive())
	assert.False(t, mysql.alterTable(users, dropPrimaryKeyAction{}).Destructive())
	assert.False(t, postgres.inputAlterTable(&SetRowLevelSecurity{tableName: users, value: true}).Destructive())

	// Creating or changing an object is not destructive, even when its body, comment or
	// literal spells a destructive statement.
	assert.False(t, inputStatement{statement: "CREATE FUNCTION intercept_ddl() RETURNS event_trigger AS $$\nBEGIN\n  IF tg_tag = 'DROP TABLE' THEN RAISE NOTICE 'x'; END IF;\nEND;\n$$ LANGUAGE plpgsql;"}.Destructive())
	assert.False(t, inputStatement{statement: "CREATE OR REPLACE FUNCTION f() RETURNS void AS $$\n-- REVOKE and DROP TABLE only appear in this comment\nBEGIN END;\n$$ LANGUAGE plpgsql;"}.Destructive())
	assert.False(t, rawStatement(`COMMENT ON TABLE "public"."audit_log" IS 'rows written when a DROP TABLE happens'`).Destructive())
	assert.False(t, alterEventStatement{d: mysql.dialect, event: Event{name: QualifiedName{Name: Ident{Name: "cleanup"}}, schedule: "EVERY 1 DAY", body: []string{"ALTER TABLE logs DROP PARTITION p2024"}}}.Destructive())
	assert.False(t, alterDomainStatement{d: postgres.dialect, domain: users, action: dropDomainConstraintAction{name: "users_check"}}.Destructive())
}

func TestSkippedRender(t *testing.T) {
	d := dialect{mode: GeneratorModePostgres, defaultSchema: "public", legacyIgnoreQuotes: true}
	users := QualifiedName{Schema: Ident{Name: "public"}, Name: Ident{Name: "users"}}

	assert.Equal(t, `-- Skipped: DROP TABLE "public"."users"`, skipped{dropObjectStatement{d: d, kind: "TABLE", name: users}}.Render())
	revoke := revokeStatement{d: d, privileges: []Privilege{{Name: "SELECT"}}, spellAll: true, objectType: "TABLE", object: users, grantee: "PUBLIC", cascade: true}
	assert.Equal(t, `-- Skipped: REVOKE SELECT ON TABLE "public"."users" FROM PUBLIC CASCADE`, skipped{revoke}.Render())

	// Every line is commented, so that no executable SQL can leak after the first one.
	assert.Equal(t,
		"-- Skipped: CREATE TRIGGER t\n-- BEGIN\n-- DELETE FROM users;\n-- END",
		skipped{inputStatement{statement: "CREATE TRIGGER t\nBEGIN\nDELETE FROM users;\nEND"}}.Render(),
	)
}

func TestHeldBack(t *testing.T) {
	d := dialect{mode: GeneratorModePostgres, defaultSchema: "public"}
	users := QualifiedName{Name: Ident{Name: "users"}}
	dropTable := dropObjectStatement{d: d, kind: "TABLE", name: users}
	dropEvent := dropObjectStatement{d: d, kind: "EVENT", name: QualifiedName{Name: Ident{Name: "cleanup"}}}
	dropFunction := dropFunctionStatement{d: d, name: QualifiedName{Schema: Ident{Name: "public"}, Name: Ident{Name: "f"}}}
	dropExtension := dropExtensionStatement{d: d, name: Ident{Name: "pgcrypto"}}
	revoke := revokeStatement{d: d, privileges: []Privilege{{Name: "SELECT"}}, object: users, grantee: "app_user"}
	rules := func(rules ...database.ManageObjectRule) *[]database.ManageObjectRule { return &rules }

	tests := []struct {
		name     string
		config   database.GeneratorConfig
		s        statement
		heldBack bool
	}{
		{"DropTableWithEnableDrop", database.GeneratorConfig{EnableDrop: true}, dropTable, false},
		{"DropTableWithoutEnableDrop", database.GeneratorConfig{}, dropTable, true},
		{"DropEventWithoutEnableDrop", database.GeneratorConfig{}, dropEvent, true},
		{"CreateWithoutEnableDrop", database.GeneratorConfig{}, inputStatement{statement: "CREATE TABLE users (id bigint)"}, false},
		{"AlreadySkipped", database.GeneratorConfig{}, skipped{dropTable}, false},

		{"RevokeWithoutEnableDrop", database.GeneratorConfig{}, revoke, true},
		{"RevokeManagedRolesWithoutEnableDrop", database.GeneratorConfig{ManagedRoles: []string{"app_user"}}, revoke, true},
		{"RevokeManagedRolesWithEnableDrop", database.GeneratorConfig{EnableDrop: true, ManagedRoles: []string{"app_user"}}, revoke, false},
		{"RevokeManagePrivilegeDropWithoutEnableDrop", database.GeneratorConfig{ManagePrivileges: rules(database.ManageObjectRule{Target: "app_user", Drop: true})}, revoke, false},
		{"RevokeManagePrivilegeNoDropWithEnableDrop", database.GeneratorConfig{EnableDrop: true, ManagePrivileges: rules(database.ManageObjectRule{Target: "app_user"})}, revoke, true},
		{"RevokeManagePrivilegeUnmatched", database.GeneratorConfig{EnableDrop: true, ManagePrivileges: rules(database.ManageObjectRule{Target: "other", Drop: true})}, revoke, true},

		{"DropFunctionWithoutEnableDrop", database.GeneratorConfig{}, dropFunction, true},
		{"DropFunctionManageFunctionDropWithoutEnableDrop", database.GeneratorConfig{ManageFunctions: rules(database.ManageObjectRule{Target: "f", Drop: true})}, dropFunction, false},
		{"DropFunctionManageFunctionNoDropWithEnableDrop", database.GeneratorConfig{EnableDrop: true, ManageFunctions: rules(database.ManageObjectRule{Target: "f"})}, dropFunction, true},
		{"DropFunctionManageFunctionUnmatched", database.GeneratorConfig{EnableDrop: true, ManageFunctions: rules(database.ManageObjectRule{Target: "other", Drop: true})}, dropFunction, true},

		{"DropExtensionManageExtensionDropWithEnableDrop", database.GeneratorConfig{EnableDrop: true, ManageExtensions: rules(database.ManageObjectRule{Target: "pgcrypto", Drop: true})}, dropExtension, false},
		{"DropExtensionManageExtensionDropWithoutEnableDrop", database.GeneratorConfig{ManageExtensions: rules(database.ManageObjectRule{Target: "pgcrypto", Drop: true})}, dropExtension, true},
		{"DropExtensionManageExtensionNoDropWithEnableDrop", database.GeneratorConfig{EnableDrop: true, ManageExtensions: rules(database.ManageObjectRule{Target: "pgcrypto"})}, dropExtension, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.heldBack, heldBack(tt.config, tt.s))
		})
	}
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
	g := &Generator{dialect: dialect{mode: GeneratorModePostgres}}
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

	// An omitted RETURNS is derived from the output parameters, so a function
	// written in the PostgreSQL-native form still matches the exported one.
	// Without this the signature check falls back to DROP + CREATE, which the
	// default flags gate away and leave the CREATE failing with 42723.
	outInt := FunctionArg{mode: "OUT", name: parser.NewIdent("b", false), typ: "int"}
	assert.True(t, g.areSameFunctionSignature(
		fn("integer", FunctionArg{mode: "OUT", name: parser.NewIdent("b", false), typ: "integer"}),
		fn("", outInt)))
}

func TestAreSameFunctionDefinition(t *testing.T) {
	g := &Generator{dialect: dialect{mode: GeneratorModePostgres}}
	mssql := &Generator{dialect: dialect{mode: GeneratorModeMssql}}
	fn := func(returnType string, args ...FunctionArg) *Function {
		return &Function{returnType: returnType, body: " SELECT 1 ", language: "sql", args: args}
	}
	out := func(name, typ string) FunctionArg {
		return FunctionArg{mode: "OUT", name: parser.NewIdent(name, false), typ: typ}
	}

	// Type aliases equal their canonical spelling: the current side comes from
	// pg_get_functiondef, which always prints the canonical name.
	assert.True(t, g.areSameFunctionDefinition(fn("integer"), fn("int")))
	assert.True(t, g.areSameFunctionDefinition(fn("boolean"), fn("bool")))
	assert.True(t, g.areSameFunctionDefinition(fn("character varying"), fn("varchar")))
	assert.True(t, g.areSameFunctionDefinition(fn("setof integer"), fn("SETOF int")))
	assert.True(t, g.areSameFunctionDefinition(fn("double precision"), fn("float")))
	assert.False(t, g.areSameFunctionDefinition(fn("integer"), fn("bigint")))

	// The aliases are PostgreSQL-specific; other dialects keep the raw compare.
	assert.False(t, mssql.areSameFunctionDefinition(fn("integer"), fn("int")))
	assert.True(t, mssql.areSameFunctionDefinition(fn("int"), fn("int")))

	// An omitted RETURNS is derived from the output parameters.
	assert.True(t, g.areSameFunctionDefinition(
		fn("integer", out("b", "integer")),
		fn("", out("b", "int"))))
	assert.True(t, g.areSameFunctionDefinition(
		fn("integer", FunctionArg{mode: "INOUT", name: parser.NewIdent("a", false), typ: "integer"}),
		fn("", FunctionArg{mode: "INOUT", name: parser.NewIdent("a", false), typ: "int"})))
	assert.True(t, g.areSameFunctionDefinition(
		fn("record", out("b", "integer"), out("c", "integer")),
		fn("", out("b", "int"), out("c", "int"))))

	assert.True(t, g.areSameFunctionDefinition(
		fn("timestamp with time zone", out("b", "timestamp with time zone")),
		fn("", out("b", "timestamptz"))))

	// VARIADIC and IN are not output parameters, so nothing is derived and the
	// mismatch against the exported RETURNS stays visible.
	assert.False(t, g.areSameFunctionDefinition(
		fn("integer", FunctionArg{mode: "VARIADIC", name: parser.NewIdent("a", false), typ: "integer[]"}),
		fn("", FunctionArg{mode: "VARIADIC", name: parser.NewIdent("a", false), typ: "int[]"})))

	// Body and language still participate in the comparison.
	changedBody := fn("integer")
	changedBody.body = " SELECT 2 "
	assert.False(t, g.areSameFunctionDefinition(fn("int"), changedBody))
}

func TestNormalizePGFunctionType(t *testing.T) {
	assert.Equal(t, "integer", normalizePGFunctionType("INT"))
	assert.Equal(t, "integer[]", normalizePGFunctionType("int4[]"))
	assert.Equal(t, "timestamp with time zone", normalizePGFunctionType("timestamptz"))

	// SETOF is a modifier on the return type, not part of the type name, so it
	// has to be stripped before the alias lookup.
	assert.Equal(t, "setof integer", normalizePGFunctionType("SETOF int"))
	assert.Equal(t, "setof integer", normalizePGFunctionType("setof  integer"))
	assert.Equal(t, "setof integer[]", normalizePGFunctionType("SETOF int[]"))
	assert.Equal(t, "double precision", normalizePGFunctionType("float"))
	assert.Equal(t, "setof", normalizePGFunctionType("setof"))
}

func TestDropFunction(t *testing.T) {
	g := &Generator{dialect: dialect{mode: GeneratorModePostgres, defaultSchema: "public"}}
	name := database.QualifiedName{Schema: parser.NewIdent("public", false), Name: parser.NewIdent("f", false)}

	// Identity argument types are appended so overloads stay unambiguous.
	fn := &Function{name: name, args: []FunctionArg{
		{name: parser.NewIdent("x", false), typ: "integer"},
		{mode: "VARIADIC", name: parser.NewIdent("rest", false), typ: "text[]"},
	}}
	assert.Equal(t, "DROP FUNCTION "+g.escapeQualifiedName(name)+"(integer, VARIADIC text[])", g.dropFunction(fn).Render())

	// Zero arguments.
	assert.Equal(t, "DROP FUNCTION "+g.escapeQualifiedName(name)+"()", g.dropFunction(&Function{name: name}).Render())

	// OUT parameters are not part of the identity: keep the bare form.
	outFn := &Function{name: name, args: []FunctionArg{{mode: "OUT", name: parser.NewIdent("x", false), typ: "integer"}}}
	assert.Equal(t, "DROP FUNCTION "+g.escapeQualifiedName(name), g.dropFunction(outFn).Render())
}

func TestGenerateIndexColumnDefinitionOperatorClassPrecedesDirection(t *testing.T) {
	// PostgreSQL parses an index key part as `expr [opclass] [ASC|DESC]`, so the operator class
	// has to be emitted before the direction.
	g := &Generator{dialect: dialect{mode: GeneratorModePostgres}}

	tests := []struct {
		name        string
		indexColumn IndexColumn
		expected    string
	}{
		{
			name: "column",
			indexColumn: IndexColumn{
				columnExpr:    &parser.ColName{Name: parser.NewIdent("name", false)},
				operatorClass: "text_pattern_ops",
				direction:     DescScr,
			},
			expected: "name text_pattern_ops desc",
		},
		{
			name: "expression",
			indexColumn: IndexColumn{
				columnExpr: &parser.BinaryExpr{
					Operator: "||",
					Left:     &parser.ColName{Name: parser.NewIdent("a", false)},
					Right:    &parser.ColName{Name: parser.NewIdent("b", false)},
				},
				operatorClass: "text_pattern_ops",
				direction:     DescScr,
			},
			expected: "(a || b) text_pattern_ops desc",
		},
		{
			name: "no direction",
			indexColumn: IndexColumn{
				columnExpr:    &parser.ColName{Name: parser.NewIdent("name", false)},
				operatorClass: "text_pattern_ops",
			},
			expected: "name text_pattern_ops",
		},
		{
			name: "no operator class",
			indexColumn: IndexColumn{
				columnExpr: &parser.ColName{Name: parser.NewIdent("name", false)},
				direction:  DescScr,
			},
			expected: "name desc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, g.generateIndexColumnDefinition(tt.indexColumn))
		})
	}
}

// generateAddIndex has no PostgreSQL-reachable path carrying an operator class today, so the
// key-part ordering of both index generators is asserted here instead of in cmd/psqldef.
func TestIndexGeneratorsEmitOperatorClassBeforeDirection(t *testing.T) {
	g := &Generator{dialect: dialect{mode: GeneratorModePostgres}}
	table := QualifiedName{Schema: Ident{Name: "public"}, Name: Ident{Name: "products"}}
	index := Index{
		name:      Ident{Name: "idx_name"},
		indexType: "INDEX",
		columns: []IndexColumn{{
			columnExpr:    &parser.ColName{Name: parser.NewIdent("name", false)},
			operatorClass: "text_pattern_ops",
			direction:     DescScr,
		}},
	}

	assert.Contains(t, g.generateCreateIndexStatement(table, index), "(name text_pattern_ops desc)")
	assert.Contains(t, g.generateAddIndex(table, index).Render(), "(name text_pattern_ops desc)")
}

// TestCreateIndexStatementRoundTrip guards the clause loss that quote-aware mode is prone to:
// it regenerates a CREATE INDEX from the parsed index, so any clause the model does not carry
// disappears from the statement — and, because both sides of a comparison go through the same
// model, disappears from the diff as well. Parsing the regenerated statement back has to yield
// the same index.
func TestCreateIndexStatementRoundTrip(t *testing.T) {
	statements := []string{
		`CREATE INDEX i ON public.t USING btree (a)`,
		`CREATE UNIQUE INDEX i ON public.t USING btree (a, b)`,
		`CREATE INDEX i ON public.t USING btree (a) INCLUDE (b)`,
		`CREATE INDEX i ON public.t USING btree (a) INCLUDE (b, "C")`,
		`CREATE UNIQUE INDEX i ON public.t USING btree (a) INCLUDE (b) NULLS NOT DISTINCT`,
		`CREATE INDEX i ON public.t USING btree (a DESC NULLS LAST, b NULLS FIRST)`,
		`CREATE INDEX i ON public.t USING btree (b COLLATE "C")`,
		`CREATE INDEX i ON public.t USING btree (b text_pattern_ops)`,
		`CREATE INDEX i ON public.t USING gin (b gin_trgm_ops)`,
		`CREATE INDEX i ON public.t USING btree (lower(b))`,
		`CREATE INDEX i ON public.t USING btree (a) WHERE a > 0`,
		`CREATE INDEX i ON public.t USING btree (a) WHERE "isActive"`,
		`CREATE INDEX i ON public.t USING btree (a) WITH (fillfactor = 70)`,
		`CREATE INDEX i ON public."T" USING btree ("A") INCLUDE ("B")`,
	}

	sqlParser := database.NewParser(parser.ParserModePostgres)
	g := &Generator{dialect: dialect{mode: GeneratorModePostgres}}

	parseIndexOf := func(t *testing.T, statement string) (QualifiedName, Index) {
		t.Helper()
		ddls, err := ParseDDLs(GeneratorModePostgres, sqlParser, statement+";", "public")
		require.NoError(t, err)
		require.Len(t, ddls, 1)
		createIndex, ok := ddls[0].(*CreateIndex)
		require.True(t, ok)
		return createIndex.tableName, createIndex.index
	}

	for _, statement := range statements {
		t.Run(statement, func(t *testing.T) {
			tableName, index := parseIndexOf(t, statement)

			generated := g.generateCreateIndexStatement(tableName, index)
			_, regenerated := parseIndexOf(t, generated)

			assert.Equal(t, index.name, regenerated.name)
			assert.Equal(t, index.indexType, regenerated.indexType)
			assert.Equal(t, index.options, regenerated.options)
			assert.Equal(t, index.included, regenerated.included)
			assert.True(t, g.areSameIndexes(nil, index, regenerated),
				"regenerated statement describes a different index:\n%s\n%s", statement, generated)
		})
	}
}

// TestAutoIndexName covers the names MySQL gives an index or constraint declared without one. A
// name that does not match what the server chose makes the desired schema differ from the
// exported one on every run, so the index is dropped and recreated each time.
func TestAutoIndexName(t *testing.T) {
	nameOf := func(t *testing.T, statement string) string {
		t.Helper()
		ddls, err := ParseDDLs(GeneratorModeMysql, database.NewParser(parser.ParserModeMysql), statement+";", "public")
		require.NoError(t, err)
		require.Len(t, ddls, 1)
		switch ddl := ddls[0].(type) {
		case *CreateIndex:
			return ddl.index.name.Name
		case *AddIndex:
			return ddl.index.name.Name
		case *AddPrimaryKey:
			return ddl.index.name.Name
		default:
			t.Fatalf("unexpected DDL type %T", ddl)
			return ""
		}
	}

	mysql := []struct {
		statement string
		expected  string
	}{
		{`ALTER TABLE t ADD UNIQUE (a, b)`, "a"},
		{`ALTER TABLE t ADD PRIMARY KEY (a)`, "PRIMARY"},
	}
	for _, tt := range mysql {
		t.Run("mysql "+tt.statement, func(t *testing.T) {
			assert.Equal(t, tt.expected, nameOf(t, tt.statement))
		})
	}
}

func TestFilterObjectsOwnerStatements(t *testing.T) {
	const sql = `
		CREATE TABLE users (id bigint);
		CREATE VIEW v_users AS SELECT id FROM users;
		ALTER TABLE users OWNER TO app_user;
		ALTER TABLE v_users OWNER TO app_user;
	`
	parse := func(t *testing.T) []DDL {
		t.Helper()
		ddls, err := ParseDDLs(GeneratorModePostgres, database.NewParser(parser.ParserModePostgres), sql, "public")
		if err != nil {
			t.Fatal(err)
		}
		return ddls
	}
	owners := func(ddls []DDL) []string {
		var names []string
		for _, ddl := range ddls {
			if stmt, ok := ddl.(*SetTableOwner); ok {
				names = append(names, stmt.tableName.RawString())
			}
		}
		return names
	}

	// target_tables is about tables and does not filter views, so neither owner goes away.
	filtered := FilterObjects(parse(t), database.GeneratorConfig{TargetTables: []string{"public.users"}})
	assert.Equal(t, []string{"public.users", "public.v_users"}, owners(filtered))

	// skip_views is about views, so a regexp that happens to match a table name must not reach
	// the table's owner.
	filtered = FilterObjects(parse(t), database.GeneratorConfig{SkipViews: []string{"public.users"}})
	assert.Equal(t, []string{"public.users", "public.v_users"}, owners(filtered))

	// The owner of a filtered object goes with it.
	filtered = FilterObjects(parse(t), database.GeneratorConfig{SkipTables: []string{"public.users"}})
	assert.Equal(t, []string{"public.v_users"}, owners(filtered))

	filtered = FilterObjects(parse(t), database.GeneratorConfig{SkipViews: []string{"public.v_users"}})
	assert.Equal(t, []string{"public.users"}, owners(filtered))
}

func TestDependentViewOwnerManagementDisabled(t *testing.T) {
	current := `
		CREATE TABLE users (id bigint, name text);
		CREATE VIEW v_base AS SELECT id, name FROM users;
		CREATE VIEW v_dep AS SELECT id FROM v_base;
	`
	desired := `
		CREATE TABLE users (id bigint, name text);
		CREATE VIEW v_base AS SELECT id FROM users;
		CREATE VIEW v_dep AS SELECT id FROM v_base;
		ALTER VIEW v_dep OWNER TO outside_role;
	`
	ddls, err := GenerateIdempotentDDLs(GeneratorModePostgres, database.NewParser(parser.ParserModePostgres), desired, current,
		database.GeneratorConfig{EnableDrop: true}, "public")
	assert.NoError(t, err)
	for _, ddl := range ddls {
		assert.NotContains(t, ddl, "OWNER TO")
	}
}

func TestFilterObjectsOwnerIdentity(t *testing.T) {
	for _, test := range []struct {
		name   string
		sql    string
		config database.GeneratorConfig
	}{
		{"legacy case folding", `CREATE TABLE "Users" (id bigint); ALTER TABLE users OWNER TO app_user;`, database.GeneratorConfig{LegacyIgnoreQuotes: true, SkipTables: []string{`public\.Users`}}},
		{"quoted identifiers", `CREATE TABLE "public"."users" (id bigint); ALTER TABLE users OWNER TO app_user;`, database.GeneratorConfig{SkipTables: []string{`public\.users`}}},
		{"partition child", `CREATE TABLE logs_2024 PARTITION OF logs FOR VALUES FROM (1) TO (2); ALTER TABLE logs_2024 OWNER TO app_user;`, database.GeneratorConfig{SkipTables: []string{`public\.logs_2024`}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ddls, err := ParseDDLs(GeneratorModePostgres, database.NewParser(parser.ParserModePostgres), test.sql, "public")
			assert.NoError(t, err)
			assert.Empty(t, FilterObjects(ddls, test.config))
		})
	}
}

func TestFilterPrivilegesMergesGranteesOnce(t *testing.T) {
	sql := `
		GRANT SELECT ON TABLE users TO app_user, readonly_user WITH GRANT OPTION;
		GRANT SELECT ON TABLE users TO app_user WITH GRANT OPTION;
	`
	ddls, err := ParseDDLs(GeneratorModePostgres, database.NewParser(parser.ParserModePostgres), sql, "public")
	require.NoError(t, err)
	rules := []database.ManageObjectRule{{Target: "app_user|readonly_user"}}

	filtered := FilterPrivileges(ddls, database.GeneratorConfig{ManagePrivileges: &rules})

	require.Len(t, filtered, 1)
	assert.Equal(t, []string{"app_user", "readonly_user"}, filtered[0].(*GrantPrivilege).grantees)
}

func TestRecreatedViewOwnerEscaping(t *testing.T) {
	rules := []database.ManageObjectRule{}
	current := `
		CREATE VIEW v AS SELECT 1 AS id, 2 AS extra;
		ALTER VIEW v OWNER TO "role;with""quote";
	`
	ddls, err := GenerateIdempotentDDLs(GeneratorModePostgres, database.NewParser(parser.ParserModePostgres),
		`CREATE VIEW v AS SELECT 1 AS id;`, current,
		database.GeneratorConfig{EnableDrop: true, ManagePrivileges: &rules}, "public")
	assert.NoError(t, err)
	assert.Contains(t, ddls, `ALTER VIEW public.v OWNER TO "role;with""quote"`)
}

func TestUnmanagedOwnerWithoutObject(t *testing.T) {
	ddls, err := GenerateIdempotentDDLs(GeneratorModePostgres, database.NewParser(parser.ParserModePostgres),
		`ALTER TABLE absent OWNER TO outside_role;`, "", database.GeneratorConfig{}, "public")
	assert.NoError(t, err)
	assert.Empty(t, ddls)
}

func TestHeldBackViewRecreationKeepsIndexState(t *testing.T) {
	current := `
		CREATE VIEW v AS SELECT 1 AS id, 2 AS extra;
		CREATE MATERIALIZED VIEW mv AS SELECT id FROM v;
		CREATE UNIQUE INDEX mv_id ON mv (id);
	`
	desired := `
		CREATE VIEW v AS SELECT 1 AS id;
		CREATE MATERIALIZED VIEW mv AS SELECT id FROM v;
		CREATE UNIQUE INDEX mv_id ON mv (id);
	`
	ddls, err := GenerateIdempotentDDLs(GeneratorModePostgres, database.NewParser(parser.ParserModePostgres),
		desired, current, database.GeneratorConfig{EnableDrop: false}, "public")
	assert.NoError(t, err)
	for _, ddl := range ddls {
		assert.Contains(t, ddl, "-- Skipped:")
	}
}

func TestRenamePrivilegeColumn(t *testing.T) {
	g := &Generator{dialect: dialect{mode: GeneratorModePostgres}}

	tests := []struct {
		privilege Privilege
		oldColumn Ident
		newColumn Ident
		expected  string
	}{
		{
			parser.NewPrivilege("SELECT", []Ident{{Name: "id"}, {Name: "secret"}}),
			Ident{Name: "secret"}, Ident{Name: "secret_v2"},
			"SELECT (id, secret_v2)",
		},
		// The column list is re-sorted, and a name is quoted only where it has to be
		{
			parser.NewPrivilege("SELECT", []Ident{{Name: "id"}, {Name: "secret"}}),
			Ident{Name: "id"}, Ident{Name: "Key"},
			`SELECT ("Key", secret)`,
		},
		// A quoted name is matched case-sensitively
		{
			parser.NewPrivilege("UPDATE", []Ident{{Name: "Odd, \"Name", Quoted: true}}),
			Ident{Name: `Odd, "Name`, Quoted: true}, Ident{Name: "plain"},
			"UPDATE (plain)",
		},
		// A column the rename does not touch keeps its exact name, whitespace
		// and all, because the column list is never re-parsed out of the SQL
		{
			parser.NewPrivilege("SELECT", []Ident{{Name: " secret ", Quoted: true}, {Name: "id"}}),
			Ident{Name: "id"}, Ident{Name: "id_v2"},
			`SELECT (" secret ", id_v2)`,
		},
		// A quoted name may begin or end with a space
		{
			parser.NewPrivilege("SELECT", []Ident{{Name: " secret ", Quoted: true}, {Name: "id"}}),
			Ident{Name: " secret ", Quoted: true}, Ident{Name: "secret_v2"},
			"SELECT (id, secret_v2)",
		},
		// Privileges that are not column-level, or do not mention the column
		{
			parser.NewPrivilege("SELECT", nil),
			Ident{Name: "secret"}, Ident{Name: "secret_v2"},
			"SELECT",
		},
		{
			parser.NewPrivilege("SELECT", []Ident{{Name: "id"}}),
			Ident{Name: "secret"}, Ident{Name: "secret_v2"},
			"SELECT (id)",
		},
	}

	for _, test := range tests {
		assert.Equal(t, test.expected, g.renamePrivilegeColumn(test.privilege, test.oldColumn, test.newColumn).String())
	}
}

func TestRenameColumnOfGrantOnMultipleTables(t *testing.T) {
	current := `
		CREATE TABLE t1 (id integer, a integer);
		CREATE TABLE t2 (id integer, a integer);
		GRANT SELECT (a) ON t1, t2 TO app_user;
	`
	desired := `
		CREATE TABLE t1 (
		  id integer,
		  b integer -- @renamed from=a
		);
		CREATE TABLE t2 (id integer, a integer);
		GRANT SELECT (b) ON t1 TO app_user;
		GRANT SELECT (a) ON t2 TO app_user;
	`

	ddls, err := GenerateIdempotentDDLs(
		GeneratorModePostgres,
		database.NewParser(parser.ParserModePostgres),
		desired,
		current,
		database.GeneratorConfig{EnableDrop: true, LegacyIgnoreQuotes: false, ManagedRoles: []string{"app_user"}},
		"public",
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"ALTER TABLE public.t1 RENAME COLUMN a TO b"}, ddls)
}
