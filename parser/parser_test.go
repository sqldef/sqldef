package parser

import (
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
