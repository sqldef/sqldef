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
			name: "CREATE TABLE LIKE syntax error",
			sql:  "CREATE TABLE task_log (LIKE task EXCLUDING CONSTRAINTS)",
			mode: ParserModePostgres,
			expectedErr: `found syntax error when parsing DDL "CREATE TABLE task_log (LIKE task EXCLUDING CONSTRAINTS)": syntax error at line 1, column 29 near 'like'
  CREATE TABLE task_log (LIKE task EXCLUDING CONSTRAINTS)
                              ^`,
		},
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
		// Long line truncation
		{
			name: "Very long line gets truncated",
			sql:  "CREATE TABLE very_long_table_name_that_exceeds_eighty_characters_for_testing_truncation (id INT, LIKE other)",
			mode: ParserModeSQLite3,
			expectedErr: `found syntax error when parsing DDL "CREATE TABLE very_long_table_name_that_exceeds_eighty_characters_for_testing_truncation (id INT, LIKE other)": syntax error at line 1, column 103 near 'like'
  ...ing_truncation (id INT, LIKE other)
                                  ^`,
		},
		{
			name: "Long line with invalid syntax",
			sql:  "CREATE TABLE short (id INT, name VARCHAR(255), email VARCHAR(255), address TEXT, phone VARCHAR(20), LIKE other)",
			mode: ParserModeSQLite3,
			expectedErr: `found syntax error when parsing DDL "CREATE TABLE short (id INT, name VARCHAR(255), email VARCHAR(255), address TEXT, phone VARCHAR(20), LIKE other)": syntax error at line 1, column 106 near 'like'
  ...EXT, phone VARCHAR(20), LIKE other)
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
