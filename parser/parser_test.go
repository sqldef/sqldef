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
