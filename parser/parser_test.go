package parser

import (
	"strings"
	"testing"
)

func TestErrorMessageSourcePosition(t *testing.T) {
	testCases := []struct {
		name        string
		sql         string
		mode        ParserMode
		expectedErr string // The exact expected error message
	}{
		// Single-line errors
		{
			name: "Typo in CREATE INDEX",
			sql:  "CREATE INDEXX idx_name ON users(name)",
			mode: ParserModeMysql,
			expectedErr: `found syntax error when parsing DDL "CREATE INDEXX idx_name ON users(name)": syntax error at line 1, column 15 near 'INDEXX'
  CREATE INDEXX idx_name ON users(name)
                ^`,
		},
		{
			name: "Missing comma between columns",
			sql:  "CREATE TABLE test (id INT name TEXT)",
			mode: ParserModeSQLite3,
			expectedErr: `found syntax error when parsing DDL "CREATE TABLE test (id INT name TEXT)": syntax error at line 1, column 32 near 'name'
  CREATE TABLE test (id INT name TEXT)
                                 ^`,
		},
		// Multi-line errors
		{
			name: "Error on second line",
			sql: `CREATE TABLE users (
    id INTEGER PRIMARY KEY
    name TEXT NOT NULL
)`,
			mode: ParserModeSQLite3,
			expectedErr: `found syntax error when parsing DDL "CREATE TABLE users (
    id INTEGER PRIMARY KEY
    name TEXT NOT NULL
)": syntax error at line 3, column 10 near 'name'
      name TEXT NOT NULL
           ^`,
		},
		{
			name: "Error on third line with LIKE",
			sql: `CREATE TABLE task (
    id INT PRIMARY KEY
);
CREATE TABLE task_log (LIKE task)`,
			mode: ParserModePostgres,
			expectedErr: `found syntax error when parsing DDL "CREATE TABLE task (
    id INT PRIMARY KEY
);
CREATE TABLE task_log (LIKE task)": syntax error at line 4, column 8 near 'create'
  CREATE TABLE task_log (LIKE task)
         ^`,
		},
		{
			name: "Multi-statement with error in second",
			sql: `CREATE TABLE users (id INT);
CREATE TABLEE posts (id INT)`,
			mode: ParserModeMysql,
			expectedErr: `found syntax error when parsing DDL "CREATE TABLE users (id INT);
CREATE TABLEE posts (id INT)": syntax error at line 2, column 8 near 'create'
  CREATE TABLEE posts (id INT)
         ^`,
		},
		// Trailing comma errors
		{
			name: "Trailing comma in CREATE TABLE",
			sql:  "CREATE TABLE test (id INT,)",
			mode: ParserModeSQLite3,
			expectedErr: `found syntax error when parsing DDL "CREATE TABLE test (id INT,)": trailing comma is not allowed in column definitions at line 1, column 28
  CREATE TABLE test (id INT,)
                             ^`,
		},
		{
			name: "Trailing comma with multiple columns",
			sql:  "CREATE TABLE test (id INT, name TEXT,)",
			mode: ParserModeMysql,
			expectedErr: `found syntax error when parsing DDL "CREATE TABLE test (id INT, name TEXT,)": trailing comma is not allowed in column definitions at line 1, column 39
  CREATE TABLE test (id INT, name TEXT,)
                                        ^`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDDL(tc.sql, tc.mode)
			if err == nil {
				t.Errorf("Expected error but got none")
				return
			}

			errMsg := err.Error()
			if errMsg != tc.expectedErr {
				t.Errorf("Error message mismatch.\nExpected:\n%s\n\nGot:\n%s", tc.expectedErr, errMsg)
			}
		})
	}
}

// TestIntervalColumnType tests INTERVAL support as both a column type and expression
func TestIntervalColumnType(t *testing.T) {
	testCases := []struct {
		name        string
		sql         string
		shouldParse bool
		description string
	}{
		// INTERVAL as column type tests
		{
			name:        "INTERVAL as simple column type",
			sql:         "CREATE TABLE events (duration INTERVAL)",
			shouldParse: true,
			description: "INTERVAL should work as a basic column type",
		},
		{
			name:        "INTERVAL with precision",
			sql:         "CREATE TABLE events (duration INTERVAL(6))",
			shouldParse: true,
			description: "INTERVAL should accept optional precision",
		},
		{
			name:        "INTERVAL with NOT NULL constraint",
			sql:         "CREATE TABLE events (duration INTERVAL NOT NULL)",
			shouldParse: true,
			description: "INTERVAL columns should support constraints",
		},
		{
			name:        "Multiple INTERVAL columns",
			sql:         "CREATE TABLE events (start_time INTERVAL, end_time INTERVAL NOT NULL, break_time INTERVAL DEFAULT '15 minutes')",
			shouldParse: true,
			description: "Should support multiple INTERVAL columns with various options",
		},
		{
			name:        "INTERVAL with DEFAULT string literal",
			sql:         "CREATE TABLE events (break_time INTERVAL DEFAULT '15 minutes')",
			shouldParse: true,
			description: "INTERVAL should support DEFAULT with string literals",
		},
		{
			name:        "INTERVAL with type cast in DEFAULT",
			sql:         "CREATE TABLE events (break_time VARCHAR(50) DEFAULT '1 day'::interval)",
			shouldParse: true,
			description: "Should support casting to INTERVAL in DEFAULT expressions",
		},
		{
			name:        "ALTER TABLE ADD INTERVAL column",
			sql:         "ALTER TABLE events ADD break_time INTERVAL",
			shouldParse: false, // ALTER TABLE ADD COLUMN not yet implemented in parser
			description: "ALTER TABLE ADD COLUMN syntax not yet supported (separate from INTERVAL feature)",
		},
		{
			name:        "CREATE INDEX on INTERVAL column",
			sql:         "CREATE INDEX idx_duration ON events (duration)",
			shouldParse: true,
			description: "Should be able to create indexes on INTERVAL columns",
		},
		// Type casting tests
		{
			name:        "Cast string to INTERVAL using ::",
			sql:         "CREATE TABLE test (val VARCHAR(50) DEFAULT '1 hour'::interval)",
			shouldParse: true,
			description: "Should support :: casting to INTERVAL",
		},
		{
			name:        "Cast to various types including INTERVAL",
			sql:         "CREATE TABLE test (a VARCHAR(50) DEFAULT '1'::int, b VARCHAR(50) DEFAULT '1 day'::interval)",
			shouldParse: true,
			description: "INTERVAL casting should work alongside other type casts",
		},
		// Edge cases and complex scenarios
		{
			name: "INTERVAL in complex table definition",
			sql: `CREATE TABLE scheduled_events (
				id SERIAL PRIMARY KEY,
				name VARCHAR(255) NOT NULL,
				duration INTERVAL NOT NULL,
				break_time INTERVAL DEFAULT '10 minutes',
				total_time INTERVAL,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
			)`,
			shouldParse: true,
			description: "INTERVAL should work in complex real-world table definitions",
		},
		{
			name:        "INTERVAL array type",
			sql:         "CREATE TABLE events (durations INTERVAL[])",
			shouldParse: true,
			description: "Should support INTERVAL array types",
		},
		{
			name:        "INTERVAL with CHECK constraint referencing it",
			sql:         "CREATE TABLE events (duration INTERVAL, CONSTRAINT positive_duration CHECK (duration > '0'::interval))",
			shouldParse: true,
			description: "INTERVAL columns should work with CHECK constraints",
		},

		// Extended TYPECAST tests with parameters
		{
			name:        "TYPECAST to VARCHAR with length",
			sql:         "CREATE TABLE test (val VARCHAR(50) DEFAULT 'hello'::varchar(10))",
			shouldParse: true,
			description: "Should support ::varchar(n) casting",
		},
		{
			name:        "TYPECAST to CHARACTER VARYING with length",
			sql:         "CREATE TABLE test (val TEXT DEFAULT 'hello'::character varying(20))",
			shouldParse: true,
			description: "Should support ::character varying(n) casting",
		},
		{
			name:        "TYPECAST to CHAR with length",
			sql:         "CREATE TABLE test (val VARCHAR(50) DEFAULT 'A'::char(1))",
			shouldParse: true,
			description: "Should support ::char(n) casting",
		},
		{
			name:        "TYPECAST to CHARACTER with length",
			sql:         "CREATE TABLE test (val TEXT DEFAULT 'B'::character(5))",
			shouldParse: true,
			description: "Should support ::character(n) casting",
		},
		{
			name:        "TYPECAST to NUMERIC with precision",
			sql:         "CREATE TABLE test (val VARCHAR(50) DEFAULT '123.45'::numeric(5))",
			shouldParse: true,
			description: "Should support ::numeric(p) casting",
		},
		{
			name:        "TYPECAST to NUMERIC with precision and scale",
			sql:         "CREATE TABLE test (val TEXT DEFAULT '123.45'::numeric(10,2))",
			shouldParse: true,
			description: "Should support ::numeric(p,s) casting",
		},
		{
			name:        "TYPECAST to DECIMAL with precision",
			sql:         "CREATE TABLE test (val VARCHAR(50) DEFAULT '99.99'::decimal(4))",
			shouldParse: true,
			description: "Should support ::decimal(p) casting",
		},
		{
			name:        "TYPECAST to DECIMAL with precision and scale",
			sql:         "CREATE TABLE test (val TEXT DEFAULT '99.99'::decimal(5,2))",
			shouldParse: true,
			description: "Should support ::decimal(p,s) casting",
		},
		{
			name:        "TYPECAST to BIT with length",
			sql:         "CREATE TABLE test (val VARCHAR(50) DEFAULT '1010'::bit(4))",
			shouldParse: true,
			description: "Should support ::bit(n) casting",
		},
		{
			name:        "TYPECAST to TIMESTAMP with precision",
			sql:         "CREATE TABLE test (val TEXT DEFAULT '2024-01-01 12:00:00'::timestamp(3))",
			shouldParse: true,
			description: "Should support ::timestamp(p) casting",
		},
		{
			name:        "TYPECAST to TIME with precision",
			sql:         "CREATE TABLE test (val VARCHAR(50) DEFAULT '12:30:45.123'::time(3))",
			shouldParse: true,
			description: "Should support ::time(p) casting",
		},
		{
			name:        "TYPECAST in VIEW with numeric parameters",
			sql:         "CREATE VIEW test_view AS SELECT amount::numeric(10,2) AS amount_num FROM orders",
			shouldParse: true,
			description: "Should support parameterized TYPECAST in VIEW definitions",
		},
		{
			name:        "Multiple TYPECAST with parameters in same statement",
			sql:         "CREATE TABLE test (a VARCHAR(50) DEFAULT '10'::varchar(5), b TEXT DEFAULT '99.99'::numeric(4,2), c VARCHAR(100) DEFAULT 'X'::char(1))",
			shouldParse: true,
			description: "Should support multiple parameterized TYPECASTs",
		},
		{
			name:        "TYPECAST in CHECK constraint",
			sql:         "CREATE TABLE test (amount TEXT, CONSTRAINT valid_amount CHECK (amount::numeric(10,2) > 0))",
			shouldParse: true,
			description: "Should support parameterized TYPECAST in CHECK constraints",
		},
		{
			name:        "TYPECAST in function call",
			sql:         "CREATE TABLE test (val VARCHAR(50) DEFAULT COALESCE('123'::varchar(10), 'default'))",
			shouldParse: true,
			description: "Should support parameterized TYPECAST within function calls",
		},
		{
			name:        "TYPECAST with parentheses in expressions",
			sql:         "CREATE VIEW test_view AS SELECT (amount)::numeric(10,2) AS amount FROM orders",
			shouldParse: true,
			description: "Should handle parenthesized expressions before TYPECAST",
		},
		{
			name:        "Chained TYPECAST operations",
			sql:         "CREATE TABLE test (val TEXT DEFAULT '123'::varchar(10))",
			shouldParse: true,
			description: "Should support TYPECAST operations with varchar parameter",
		},
		{
			name:        "Chained TYPECAST - simple types",
			sql:         "CREATE TABLE test (val TEXT DEFAULT CURRENT_TIMESTAMP::date::text)",
			shouldParse: true,
			description: "Should support chained typecasts like value::type1::type2",
		},
		{
			name:        "CREATE EXTENSION with user-defined extension name",
			sql:         "CREATE EXTENSION my_extension",
			shouldParse: true,
			description: "Should support CREATE EXTENSION with user-defined extension name",
		},
		{
			name:        "CREATE EXTENSION with user-defined extension name (quoted)",
			sql:         "CREATE EXTENSION \"my extension\"",
			shouldParse: true,
			description: "Should support CREATE EXTENSION with user-defined extension name",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDDL(tc.sql, ParserModePostgres)

			if tc.shouldParse && err != nil {
				t.Errorf("%s\nSQL: %s\nError: %v", tc.description, tc.sql, err)
			} else if !tc.shouldParse && err == nil {
				t.Errorf("Expected parse error but got none.\n%s\nSQL: %s", tc.description, tc.sql)
			}
		})
	}
}

// TestCTEParsing tests Common Table Expression (WITH clause) support
func TestCTEParsing(t *testing.T) {
	testCases := []struct {
		name        string
		sql         string
		shouldParse bool
		description string
	}{
		{
			name: "Simplest CTE",
			sql: `CREATE VIEW test_view AS
WITH cte AS (
  SELECT 1 as id
)
SELECT * FROM cte`,
			shouldParse: true,
			description: "Basic CTE should parse successfully",
		},
		{
			name: "CTE with comment after WITH",
			sql: `CREATE VIEW test_view AS
WITH
-- This is a comment
cte AS (
  SELECT 1 as id
)
SELECT * FROM cte`,
			shouldParse: true,
			description: "CTE with comment immediately after WITH should parse",
		},
		{
			name: "Multiple CTEs with comments",
			sql: `CREATE VIEW test_view AS
WITH
-- First CTE
cte1 AS (
  SELECT 1 as id
),
-- Second CTE
cte2 AS (
  SELECT 2 as id
)
SELECT * FROM cte1`,
			shouldParse: true,
			description: "Multiple CTEs with comments between them should parse",
		},
		{
			name: "CTE with CROSS JOIN",
			sql: `CREATE VIEW test_view AS
WITH cte AS (
  SELECT t1.id, t2.value
  FROM table1 t1
  CROSS JOIN table2 t2
)
SELECT * FROM cte`,
			shouldParse: true,
			description: "CTE containing CROSS JOIN should parse",
		},
		{
			name: "Materialized View with CTE",
			sql: `CREATE MATERIALIZED VIEW test_view AS
WITH cte AS (
  SELECT 1 as id
)
SELECT * FROM cte`,
			shouldParse: true,
			description: "Materialized view with CTE should parse",
		},
		{
			name: "WITH RECURSIVE - basic",
			sql: `CREATE VIEW test_view AS
WITH RECURSIVE cte AS (
  SELECT 1 as n
  UNION ALL
  SELECT n + 1 FROM cte WHERE n < 10
)
SELECT * FROM cte`,
			shouldParse: true,
			description: "WITH RECURSIVE should parse successfully",
		},
		{
			name: "WITH RECURSIVE - hierarchy traversal",
			sql: `CREATE VIEW employee_hierarchy AS
WITH RECURSIVE subordinates AS (
  SELECT employee_id, manager_id, 1 AS emp_level
  FROM employees
  WHERE manager_id IS NULL
  UNION ALL
  SELECT e.employee_id, e.manager_id, s.emp_level + 1
  FROM employees e
  INNER JOIN subordinates s ON s.employee_id = e.manager_id
)
SELECT * FROM subordinates`,
			shouldParse: true,
			description: "WITH RECURSIVE for hierarchical queries should parse",
		},
		{
			name: "WITH RECURSIVE in materialized view",
			sql: `CREATE MATERIALIZED VIEW numbers AS
WITH RECURSIVE cte AS (
  SELECT 1 as n
  UNION ALL
  SELECT n + 1 FROM cte WHERE n < 100
)
SELECT * FROM cte`,
			shouldParse: true,
			description: "WITH RECURSIVE in materialized view should parse",
		},
		{
			name: "Complex CTE with multiple CTEs and PostgreSQL functions",
			sql: `CREATE VIEW v_product_summary AS
WITH
-- Calculate counts
event_counts AS (
  SELECT
    p.id AS product_id,
    e.id AS event_id,
    COUNT(DISTINCT pe.event_id) AS count_until_event
  FROM products p
  CROSS JOIN events e
  LEFT JOIN product_events pe ON p.id = pe.product_id
  GROUP BY p.id, e.id
),
-- Calculate release date
release_dates AS (
  SELECT
    p.id AS product_id,
    e.id AS event_id,
    make_date(p.release_year, p.release_month, 1) AS release_date
  FROM products p
  CROSS JOIN events e
)
SELECT
  rd.product_id,
  rd.event_id,
  format('%s-%s', rd.release_date, ec.count_until_event) AS summary
FROM release_dates rd
LEFT JOIN event_counts ec ON rd.product_id = ec.product_id`,
			shouldParse: true,
			description: "Complex CTE with multiple CTEs, comments, CROSS JOIN, and PostgreSQL functions",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDDL(tc.sql, ParserModePostgres)

			if tc.shouldParse && err != nil {
				t.Errorf("%s\nSQL: %s\nError: %v", tc.description, tc.sql, err)
			} else if !tc.shouldParse && err == nil {
				t.Errorf("Expected parse error but got none.\n%s\nSQL: %s", tc.description, tc.sql)
			}
		})
	}
}

func TestNowFunctionInDefaultExpression(t *testing.T) {
	sql := "CREATE TABLE test (pk timestamp primary key default now())"

	statement, err := ParseDDL(sql, ParserModePostgres)
	if err != nil {
		t.Fatalf("failed to parse NOW() default expression: %v", err)
	}

	got := String(statement)
	if got != "create table test (\n\tpk timestamp default(now()) primary key\n)" {
		t.Fatalf("unexpected normalized SQL:\n%s", got)
	}
}

// TestTypeKeywordsAsIndexColumns tests that type keywords (uuid, int, bigint, etc.)
// can be used as unquoted column names in index definitions
func TestTypeKeywordsAsIndexColumns(t *testing.T) {
	testCases := []struct {
		name        string
		sql         string
		shouldParse bool
		description string
	}{
		// UNIQUE constraint with type keywords as column names
		{
			name:        "UNIQUE constraint with uuid column",
			sql:         `ALTER TABLE "test" ADD CONSTRAINT "test_uuid_key" UNIQUE (uuid)`,
			shouldParse: true,
			description: "uuid should be usable as unquoted column name in UNIQUE constraint",
		},
		{
			name:        "UNIQUE constraint with int column",
			sql:         `ALTER TABLE "test" ADD CONSTRAINT "test_int_key" UNIQUE (int)`,
			shouldParse: true,
			description: "int should be usable as unquoted column name in UNIQUE constraint",
		},
		{
			name:        "UNIQUE constraint with bigint column",
			sql:         `ALTER TABLE "test" ADD CONSTRAINT "test_bigint_key" UNIQUE (bigint)`,
			shouldParse: true,
			description: "bigint should be usable as unquoted column name in UNIQUE constraint",
		},
		{
			name:        "UNIQUE constraint with json column",
			sql:         `ALTER TABLE "test" ADD CONSTRAINT "test_json_key" UNIQUE (json)`,
			shouldParse: true,
			description: "json should be usable as unquoted column name in UNIQUE constraint",
		},
		{
			name:        "UNIQUE constraint with varchar column",
			sql:         `ALTER TABLE "test" ADD CONSTRAINT "test_varchar_key" UNIQUE (varchar)`,
			shouldParse: true,
			description: "varchar should be usable as unquoted column name in UNIQUE constraint",
		},
		// PRIMARY KEY with type keywords
		{
			name:        "PRIMARY KEY with uuid column",
			sql:         `ALTER TABLE ONLY "test" ADD CONSTRAINT "test_pkey" PRIMARY KEY (uuid)`,
			shouldParse: true,
			description: "uuid should be usable as unquoted column name in PRIMARY KEY",
		},
		// CREATE INDEX with type keywords
		{
			name:        "CREATE INDEX on uuid column",
			sql:         `CREATE INDEX idx_uuid ON test (uuid)`,
			shouldParse: true,
			description: "uuid should be usable as unquoted column name in CREATE INDEX",
		},
		{
			name:        "CREATE UNIQUE INDEX on int column",
			sql:         `CREATE UNIQUE INDEX idx_int ON test (int)`,
			shouldParse: true,
			description: "int should be usable as unquoted column name in CREATE UNIQUE INDEX",
		},
		// Multiple columns including type keywords
		{
			name:        "UNIQUE constraint with multiple columns including uuid",
			sql:         `ALTER TABLE "test" ADD CONSTRAINT "test_multi_key" UNIQUE (name, uuid)`,
			shouldParse: true,
			description: "uuid should work alongside regular column names",
		},
		{
			name:        "CREATE INDEX with multiple type keyword columns",
			sql:         `CREATE INDEX idx_multi ON test (uuid, int, bigint)`,
			shouldParse: true,
			description: "Multiple type keywords should be usable together",
		},
		// Quoted versions should also work
		{
			name:        "UNIQUE constraint with quoted uuid column",
			sql:         `ALTER TABLE "test" ADD CONSTRAINT "test_uuid_key" UNIQUE ("uuid")`,
			shouldParse: true,
			description: "Quoted uuid should also parse successfully",
		},
		// CREATE TABLE with inline UNIQUE constraint using quoted uuid column name
		{
			name:        "CREATE TABLE with inline UNIQUE on quoted uuid column",
			sql:         `CREATE TABLE test ("uuid" UUID NOT NULL, CONSTRAINT test_uuid_key UNIQUE (uuid))`,
			shouldParse: true,
			description: "Inline UNIQUE constraint should work with uuid column name",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDDL(tc.sql, ParserModePostgres)

			if tc.shouldParse && err != nil {
				t.Errorf("%s\nSQL: %s\nError: %v", tc.description, tc.sql, err)
			} else if !tc.shouldParse && err == nil {
				t.Errorf("Expected parse error but got none.\n%s\nSQL: %s", tc.description, tc.sql)
			}
		})
	}
}

func TestAutoRandom(t *testing.T) {
	testCases := []struct {
		name      string
		sql       string
		shardBits int
		rangeBits int
	}{
		{"bare", "CREATE TABLE t (id bigint AUTO_RANDOM, PRIMARY KEY (id))", 0, 0},
		{"shard bits", "CREATE TABLE t (id bigint AUTO_RANDOM(5), PRIMARY KEY (id))", 5, 0},
		{"shard and range", "CREATE TABLE t (id bigint AUTO_RANDOM(5, 54), PRIMARY KEY (id))", 5, 54},
		{"tidb comment", "CREATE TABLE t (id bigint /*T![auto_rand] AUTO_RANDOM(5) */ NOT NULL, PRIMARY KEY (id))", 5, 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tree, err := ParseDDL(tc.sql, ParserModeMysql)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			ddl := tree.(*DDL)
			col := ddl.TableSpec.Columns[0]
			if !bool(col.Type.AutoRandom) {
				t.Error("expected AutoRandom=true")
			}
			if col.Type.AutoRandomShardBits != tc.shardBits {
				t.Errorf("expected ShardBits=%d, got %d", tc.shardBits, col.Type.AutoRandomShardBits)
			}
			if col.Type.AutoRandomRange != tc.rangeBits {
				t.Errorf("expected Range=%d, got %d", tc.rangeBits, col.Type.AutoRandomRange)
			}
		})
	}
}

func TestTiDBComments(t *testing.T) {
	testCases := []struct {
		name    string
		sql     string
		options map[string]string
	}{
		{
			"clustered_index",
			"CREATE TABLE t (id bigint NOT NULL, PRIMARY KEY (id) /*T![clustered_index] CLUSTERED */)",
			nil,
		},
		{
			"nonclustered_index",
			"CREATE TABLE t (id bigint NOT NULL AUTO_INCREMENT, PRIMARY KEY (id) /*T![clustered_index] NONCLUSTERED */)",
			nil,
		},
		{
			"auto_id_cache",
			"CREATE TABLE t (id bigint NOT NULL, PRIMARY KEY (id)) /*T![auto_id_cache] AUTO_ID_CACHE=1 */",
			map[string]string{"AUTO_ID_CACHE": "1"},
		},
		{
			"shard_row_id_bits",
			"CREATE TABLE t (a int, b int) /*T! SHARD_ROW_ID_BITS=4 PRE_SPLIT_REGIONS=3 */",
			map[string]string{"SHARD_ROW_ID_BITS": "4", "PRE_SPLIT_REGIONS": "3"},
		},
		{
			"empty_ungated_comment",
			"CREATE TABLE t (a int) /*T! */",
			nil,
		},
		{
			"unclosed_feature_bracket",
			"CREATE TABLE t (a int) /*T![unclosed */",
			nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tree, err := ParseDDL(tc.sql, ParserModeMysql)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			ddl := tree.(*DDL)
			if ddl.TableSpec == nil {
				t.Fatal("expected TableSpec")
			}
			for key, expected := range tc.options {
				if actual := ddl.TableSpec.Options[key]; actual != expected {
					t.Errorf("option %q: expected %q, got %q", key, expected, actual)
				}
			}
		})
	}
}

// TestInvalidCustomOperators tests that invalid PostgreSQL custom operators produce errors
func TestInvalidCustomOperators(t *testing.T) {
	testCases := []struct {
		name        string
		sql         string
		description string
	}{
		{
			name:        "Operator containing --",
			sql:         "CREATE VIEW v AS SELECT a <-- b FROM t",
			description: "Operators cannot contain -- (comment sequence)",
		},
		{
			name:        "Operator containing /*",
			sql:         "CREATE VIEW v AS SELECT a </* b FROM t",
			description: "Operators cannot contain /* (comment sequence)",
		},
		{
			name:        "Operator ending in - without special char",
			sql:         "CREATE VIEW v AS SELECT a <- b FROM t",
			description: "Multi-char operators ending in - must contain ~ ! @ # % ^ & | ` ?",
		},
		{
			name:        "Operator ending in + without special char",
			sql:         "CREATE VIEW v AS SELECT a <+ b FROM t",
			description: "Multi-char operators ending in + must contain ~ ! @ # % ^ & | ` ?",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDDL(tc.sql, ParserModePostgres)
			if err == nil {
				t.Errorf("Expected parse error but got none.\n%s\nSQL: %s", tc.description, tc.sql)
			}
		})
	}
}

func TestDefaultFunctionExpressions(t *testing.T) {
	testCases := []struct {
		name string
		sql  string
		mode ParserMode
	}{
		{
			name: "MySQL JSON_ARRAY default wrapped in parentheses",
			sql:  "CREATE TABLE t (id bigint, friend_ids JSON DEFAULT(JSON_ARRAY()))",
			mode: ParserModeMysql,
		},
		{
			name: "Postgres uuid_generate_v4 default",
			sql:  "CREATE TABLE t (id uuid DEFAULT uuid_generate_v4())",
			mode: ParserModePostgres,
		},
		{
			name: "Postgres gen_random_uuid default",
			sql:  "CREATE TABLE t (id uuid DEFAULT gen_random_uuid())",
			mode: ParserModePostgres,
		},
		{
			name: "Postgres now default stays a function call",
			sql:  "CREATE TABLE t (created_at timestamp DEFAULT now())",
			mode: ParserModePostgres,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseDDL(tc.sql, tc.mode); err != nil {
				t.Fatalf("ParseDDL(%q) failed: %v", tc.sql, err)
			}
		})
	}
}

func TestSQLiteTableOptions(t *testing.T) {
	testCases := []struct {
		name string
		sql  string
		want string
	}{
		{
			"strict",
			"CREATE TABLE t (id integer PRIMARY KEY) STRICT",
			"create table t (\n\tid integer primary key\n) STRICT",
		},
		{
			"without rowid",
			"CREATE TABLE t (id integer PRIMARY KEY) WITHOUT ROWID",
			"create table t (\n\tid integer primary key\n) WITHOUT ROWID",
		},
		{
			"strict and without rowid",
			"CREATE TABLE t (id integer PRIMARY KEY, data text) STRICT, WITHOUT ROWID",
			"create table t (\n\tid integer primary key,\n\tdata text\n) STRICT, WITHOUT ROWID",
		},
		{
			"any type in strict table",
			"CREATE TABLE t (id integer PRIMARY KEY, data ANY) STRICT",
			"create table t (\n\tid integer primary key,\n\tdata ANY\n) STRICT",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tree, err := ParseDDL(tc.sql, ParserModeSQLite3)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got := String(tree)
			if got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestCreatePolicyPredicates(t *testing.T) {
	testCases := []string{
		"CREATE POLICY p ON t AS PERMISSIVE FOR ALL TO public USING (current_schema() = current_database())",
		"CREATE POLICY p ON t AS PERMISSIVE FOR ALL TO public USING (current_schema()::uuid = current_database()::uuid)",
	}

	for _, sql := range testCases {
		t.Run(sql, func(t *testing.T) {
			if _, err := ParseDDL(sql, ParserModePostgres); err != nil {
				t.Fatalf("ParseDDL(%q) failed: %v", sql, err)
			}
		})
	}
}

func TestStringConcatOperator(t *testing.T) {
	t.Run("postgres mode emits || without substituting or", func(t *testing.T) {
		testCases := []struct {
			name string
			sql  string
			// checkShape, when set, asserts AST shape after round-trip text
			// checks. Used to lock down precedence/associativity beyond the
			// emitter, which is the layer the text checks above cover.
			checkShape func(t *testing.T, stmt Statement)
		}{
			{
				name: "DEFAULT with two string literals",
				sql:  "CREATE TABLE t (s varchar(64) NOT NULL DEFAULT ('a' || 'b'))",
			},
			{
				name: "DEFAULT with literal and function call",
				sql:  "CREATE TABLE t (public_id varchar(64) NOT NULL DEFAULT ('usr_' || nanoid()))",
			},
			{
				name: "column-level CHECK with concat and comparison",
				sql:  "CREATE TABLE t (s text CHECK (s || 'x' <> ''))",
			},
			{
				name: "chained concat is left-associative",
				sql:  "CREATE TABLE t (s text NOT NULL DEFAULT ('a' || 'b' || 'c'))",
				// Expected AST: ParenExpr(ConcatExpr{Left: ConcatExpr{'a','b'}, Right: 'c'}).
				// Left-leaning tree confirms %left CONCAT is applied.
				checkShape: func(t *testing.T, stmt Statement) {
					t.Helper()
					ddl := stmt.(*DDL)
					expr := ddl.TableSpec.Columns[0].Type.Default.Expression.Expr
					paren, ok := expr.(*ParenExpr)
					if !ok {
						t.Fatalf("expected outer *ParenExpr, got %T", expr)
					}
					outer, ok := paren.Expr.(*ConcatExpr)
					if !ok {
						t.Fatalf("expected *ConcatExpr inside parens, got %T", paren.Expr)
					}
					if _, ok := outer.Left.(*ConcatExpr); !ok {
						t.Errorf("expected left-associative shape ConcatExpr{ConcatExpr,X}, got %T on Left", outer.Left)
					}
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				stmt, err := ParseDDL(tc.sql, ParserModePostgres)
				if err != nil {
					t.Fatalf("ParseDDL(%q) failed: %v", tc.sql, err)
				}
				got := String(stmt)
				if !strings.Contains(got, "||") {
					t.Errorf("expected || in round-trip output, got:\n%s", got)
				}
				if strings.Contains(got, " or ") {
					t.Errorf("expected no ' or ' substitution for ||, got:\n%s", got)
				}
				if tc.checkShape != nil {
					tc.checkShape(t, stmt)
				}
			})
		}
	})

	t.Run("postgres mode: concat coexists with explicit OR", func(t *testing.T) {
		// Expected AST: OrExpr{Left: ComparisonExpr{Left: ConcatExpr{...}, ...},
		// Right: IsExpr{s, "is null"}}. The shape assertion locks down both
		// (a) || binds tighter than comparison, and (b) OR is at the outermost
		// boolean level — independent of how the emitter formats the text.
		sql := "CREATE TABLE t (s text CHECK ('a' || 'b' = 'ab' OR s IS NULL))"
		stmt, err := ParseDDL(sql, ParserModePostgres)
		if err != nil {
			t.Fatalf("ParseDDL failed: %v", err)
		}
		got := String(stmt)
		if !strings.Contains(got, "||") {
			t.Errorf("expected || in round-trip output, got:\n%s", got)
		}
		if !strings.Contains(got, " or ") {
			t.Errorf("expected explicit OR to round-trip as ' or ', got:\n%s", got)
		}

		ddl := stmt.(*DDL)
		expr := ddl.TableSpec.Columns[0].Type.Check.Where.Expr
		or, ok := expr.(*OrExpr)
		if !ok {
			t.Fatalf("expected *OrExpr at top, got %T", expr)
		}
		cmp, ok := or.Left.(*ComparisonExpr)
		if !ok {
			t.Fatalf("expected *ComparisonExpr on OR.Left, got %T", or.Left)
		}
		if _, ok := cmp.Left.(*ConcatExpr); !ok {
			t.Errorf("expected *ConcatExpr on ComparisonExpr.Left, got %T", cmp.Left)
		}
	})

	t.Run("mysql mode still treats || as OR", func(t *testing.T) {
		sql := "CREATE TABLE t (s varchar(64) DEFAULT ('a' || 'b'))"
		stmt, err := ParseDDL(sql, ParserModeMysql)
		if err != nil {
			t.Fatalf("ParseDDL failed: %v", err)
		}
		got := String(stmt)
		if !strings.Contains(got, " or ") {
			t.Errorf("expected MySQL mode to emit ' or ' for ||, got:\n%s", got)
		}
		if strings.Contains(got, "||") {
			t.Errorf("expected MySQL mode to not emit '||', got:\n%s", got)
		}
	})

	t.Run("|| binds tighter than comparison (precedence)", func(t *testing.T) {
		// Per PostgreSQL spec, || is "any other operator" tier: tighter than
		// comparison (=/<>/etc), looser than additive (+/-). So:
		//   'a' || 'b' = 'ab'   parses as ('a' || 'b') = 'ab'
		//   1 + 2 || 3          parses as (1 + 2) || 3
		// Asserting AST shape (not text round-trip) since PG would re-parse
		// the round-tripped text correctly even with the wrong AST.
		cases := []struct {
			name string
			sql  string
			// rootIsConcat=true means top-level expression is ConcatExpr.
			// rootIsConcat=false means top-level is ComparisonExpr with ConcatExpr on the Left.
			rootIsComparison bool
			rootIsConcat     bool
		}{
			{
				name:             "concat vs equal: equal is outer, concat is inner-left",
				sql:              `CREATE TABLE t (s text, CHECK ('a' || 'b' = 'ab'))`,
				rootIsComparison: true,
			},
			{
				name:             "concat vs not-equal: not-equal is outer, concat is inner-left",
				sql:              `CREATE TABLE t (s text, CHECK ('a' || 'b' <> 'ab'))`,
				rootIsComparison: true,
			},
			{
				name:         "concat vs additive: concat is outer, plus is inner-left",
				sql:          `CREATE TABLE t (s text, CHECK (1 + 2 || 3))`,
				rootIsConcat: true,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				stmt, err := ParseDDL(tc.sql, ParserModePostgres)
				if err != nil {
					t.Fatalf("ParseDDL failed: %v", err)
				}
				ddl := stmt.(*DDL)
				expr := ddl.TableSpec.Checks[0].Where.Expr
				if tc.rootIsComparison {
					cmp, ok := expr.(*ComparisonExpr)
					if !ok {
						t.Fatalf("expected *ComparisonExpr at top, got %T (expr=%s)", expr, String(expr))
					}
					if _, ok := cmp.Left.(*ConcatExpr); !ok {
						t.Errorf("expected *ConcatExpr on Left of comparison, got %T (expr=%s)", cmp.Left, String(expr))
					}
				}
				if tc.rootIsConcat {
					concat, ok := expr.(*ConcatExpr)
					if !ok {
						t.Fatalf("expected *ConcatExpr at top, got %T (expr=%s)", expr, String(expr))
					}
					if _, ok := concat.Left.(*BinaryExpr); !ok {
						t.Errorf("expected *BinaryExpr on Left of concat, got %T (expr=%s)", concat.Left, String(expr))
					}
				}
			})
		}
	})

	t.Run("exclusion constraint with || operator preserves operator string", func(t *testing.T) {
		sql := "CREATE TABLE t (a text, CONSTRAINT ex EXCLUDE USING gist (a WITH ||))"
		stmt, err := ParseDDL(sql, ParserModePostgres)
		if err != nil {
			t.Fatalf("ParseDDL failed: %v", err)
		}
		ddl, ok := stmt.(*DDL)
		if !ok {
			t.Fatalf("expected *DDL, got %T", stmt)
		}
		if len(ddl.TableSpec.Exclusions) != 1 {
			t.Fatalf("expected one exclusion, got %d", len(ddl.TableSpec.Exclusions))
		}
		ex := ddl.TableSpec.Exclusions[0]
		if len(ex.Exclusions) != 1 {
			t.Fatalf("expected one exclusion pair, got %d", len(ex.Exclusions))
		}
		if got := ex.Exclusions[0].Operator; got != "||" {
			t.Errorf("expected Operator %q, got %q", "||", got)
		}
	})
}

func TestCreateFunctionArgDefaultsAndModes(t *testing.T) {
	type expectedArg struct {
		Mode       string
		Name       string
		Type       string
		HasDefault bool
		Default    string
	}

	testCases := []struct {
		name string
		sql  string
		want []expectedArg
	}{
		{
			name: "DEFAULT integer literal",
			sql:  "CREATE FUNCTION f(size int DEFAULT 16) RETURNS text AS $$ SELECT '' $$ LANGUAGE sql",
			want: []expectedArg{
				{Name: "size", Type: "int", HasDefault: true, Default: "16"},
			},
		},
		{
			name: "DEFAULT string literal",
			sql:  "CREATE FUNCTION f(s text DEFAULT 'abc') RETURNS text AS $$ SELECT s $$ LANGUAGE sql",
			want: []expectedArg{
				{Name: "s", Type: "text", HasDefault: true, Default: "'abc'"},
			},
		},
		{
			name: "mixed required and default arg",
			sql:  "CREATE FUNCTION f(a int, b int DEFAULT 0) RETURNS int AS $$ SELECT a + b $$ LANGUAGE sql",
			want: []expectedArg{
				{Name: "a", Type: "int"},
				{Name: "b", Type: "int", HasDefault: true, Default: "0"},
			},
		},
		{
			name: "IN and OUT argument modes",
			sql:  "CREATE FUNCTION f(IN a int, OUT b int) RETURNS int AS $$ SELECT a $$ LANGUAGE sql",
			want: []expectedArg{
				{Mode: "IN", Name: "a", Type: "int"},
				{Mode: "OUT", Name: "b", Type: "int"},
			},
		},
		{
			name: "VARIADIC argument mode",
			sql:  "CREATE FUNCTION f(VARIADIC a int[]) RETURNS int AS $$ SELECT 0 $$ LANGUAGE sql",
			want: []expectedArg{
				{Mode: "VARIADIC", Name: "a", Type: "int[]"},
			},
		},
		{
			name: "equals-sign default form",
			sql:  "CREATE FUNCTION f(a int = 5) RETURNS int AS $$ SELECT a $$ LANGUAGE sql",
			want: []expectedArg{
				{Name: "a", Type: "int", HasDefault: true, Default: "5"},
			},
		},
		{
			name: "multi-arg CREATE OR REPLACE with DEFAULTs",
			sql: `CREATE OR REPLACE FUNCTION concat_strings(
    count int DEFAULT 1,
    sep text DEFAULT '-'
) RETURNS text AS $$
DECLARE
    result text := '';
BEGIN
    RETURN result;
END
$$ LANGUAGE plpgsql VOLATILE`,
			want: []expectedArg{
				{Name: "count", Type: "int", HasDefault: true, Default: "1"},
				{Name: "sep", Type: "text", HasDefault: true, Default: "'-'"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stmt, err := ParseDDL(tc.sql, ParserModePostgres)
			if err != nil {
				t.Fatalf("ParseDDL failed: %v", err)
			}
			ddl, ok := stmt.(*DDL)
			if !ok {
				t.Fatalf("expected *DDL, got %T", stmt)
			}
			if ddl.Action != CreateFunction {
				t.Fatalf("expected CreateFunction action, got %v", ddl.Action)
			}
			if ddl.Function == nil {
				t.Fatalf("expected non-nil Function")
			}
			if got, want := len(ddl.Function.Args), len(tc.want); got != want {
				t.Fatalf("expected %d args, got %d", want, got)
			}
			for i, want := range tc.want {
				got := ddl.Function.Args[i]
				if got.Mode != want.Mode {
					t.Errorf("arg[%d] Mode: got %q, want %q", i, got.Mode, want.Mode)
				}
				if got.Name.Name != want.Name {
					t.Errorf("arg[%d] Name: got %q, want %q", i, got.Name.Name, want.Name)
				}
				if got.Type != want.Type {
					t.Errorf("arg[%d] Type: got %q, want %q", i, got.Type, want.Type)
				}
				if want.HasDefault {
					if got.Default == nil {
						t.Errorf("arg[%d] Default: got nil, want %q", i, want.Default)
					} else if s := String(got.Default); s != want.Default {
						t.Errorf("arg[%d] Default: got %q, want %q", i, s, want.Default)
					}
				} else if got.Default != nil {
					t.Errorf("arg[%d] Default: got %q, want nil", i, String(got.Default))
				}
			}
		})
	}
}

func TestTableSpecRoundTripConstraints(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		mode ParserMode
	}{
		{
			"unnamed table-level CHECK",
			"CREATE TABLE t (s text, CHECK (s > 0))",
			ParserModePostgres,
		},
		{
			"named table-level CHECK",
			"CREATE TABLE t (s text, CONSTRAINT c CHECK (s > 0))",
			ParserModePostgres,
		},
		{
			"CHECK NO INHERIT",
			"CREATE TABLE t (s text, CONSTRAINT c CHECK (s > 0) NO INHERIT)",
			ParserModePostgres,
		},
		{
			"FK basic",
			"CREATE TABLE t (a int, CONSTRAINT fk FOREIGN KEY (a) REFERENCES u (id))",
			ParserModePostgres,
		},
		{
			"FK with ON DELETE CASCADE",
			"CREATE TABLE t (a int, CONSTRAINT fk FOREIGN KEY (a) REFERENCES u (id) ON DELETE CASCADE)",
			ParserModePostgres,
		},
		{
			"FK DEFERRABLE INITIALLY DEFERRED",
			"CREATE TABLE t (a int, CONSTRAINT fk FOREIGN KEY (a) REFERENCES u (id) DEFERRABLE INITIALLY DEFERRED)",
			ParserModePostgres,
		},
		{
			"FK composite columns",
			"CREATE TABLE t (a int, b int, CONSTRAINT fk FOREIGN KEY (a, b) REFERENCES u (x, y))",
			ParserModePostgres,
		},
		{
			"FK NOT FOR REPLICATION (MSSQL)",
			"CREATE TABLE t (a int, CONSTRAINT fk FOREIGN KEY (a) REFERENCES u (id) NOT FOR REPLICATION)",
			ParserModeMssql,
		},
		{
			"EXCLUDE without USING",
			"CREATE TABLE t (a text, CONSTRAINT ex EXCLUDE (a WITH =))",
			ParserModePostgres,
		},
		{
			"EXCLUDE USING gist with &&",
			"CREATE TABLE t (a text, CONSTRAINT ex EXCLUDE USING gist (a WITH &&))",
			ParserModePostgres,
		},
		{
			"EXCLUDE with WHERE",
			"CREATE TABLE t (a int, CONSTRAINT ex EXCLUDE (a WITH =) WHERE (a > 0))",
			ParserModePostgres,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stmt1, err := ParseDDL(tc.sql, tc.mode)
			if err != nil {
				t.Fatalf("first parse failed: %v", err)
			}
			text1 := String(stmt1)

			stmt2, err := ParseDDL(text1, tc.mode)
			if err != nil {
				t.Fatalf("re-parse failed for %q: %v", text1, err)
			}
			text2 := String(stmt2)

			if text1 != text2 {
				t.Errorf("round-trip not idempotent\nfirst:  %s\nsecond: %s", text1, text2)
			}
		})
	}
}

func TestExcludeWhereGrammar(t *testing.T) {
	t.Run("WHERE (predicate) yields non-ParenExpr AST", func(t *testing.T) {
		sql := "CREATE TABLE t (a int, CONSTRAINT ex EXCLUDE (a WITH =) WHERE (a > 0))"
		stmt, err := ParseDDL(sql, ParserModePostgres)
		if err != nil {
			t.Fatalf("parse failed: %v", err)
		}
		ex := stmt.(*DDL).TableSpec.Exclusions[0]
		if pe, ok := ex.Where.Expr.(*ParenExpr); ok {
			t.Errorf("Where.Expr should be the raw predicate, got *ParenExpr wrapping %T", pe.Expr)
		}
	})

	t.Run("WHERE without parens is rejected", func(t *testing.T) {
		sql := "CREATE TABLE t (a int, CONSTRAINT ex EXCLUDE (a WITH =) WHERE a > 0)"
		if _, err := ParseDDL(sql, ParserModePostgres); err == nil {
			t.Errorf("expected parse error for WHERE without parens, got nil")
		}
	})
}
