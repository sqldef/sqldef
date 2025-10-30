package parser

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/goccy/go-yaml"
)

// TestCase represents a test case from the psqldef YAML files
type TestCase struct {
	Current string // Current schema state
	Desired string // Desired schema state
	// Other fields are not needed for this test
}

// readPsqldefTests reads all psqldef YAML test files
func readPsqldefTests() (map[string]TestCase, error) {
	// Find all YAML test files in cmd/psqldef/
	pattern := filepath.Join("..", "cmd", "psqldef", "tests*.yml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	ret := map[string]TestCase{}
	for _, file := range files {
		var tests map[string]*TestCase

		buf, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}

		dec := yaml.NewDecoder(bytes.NewReader(buf))
		err = dec.Decode(&tests)
		if err != nil {
			return nil, err
		}

		for name, test := range tests {
			ret[name] = *test
		}
	}

	return ret, nil
}

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

// TestPsqldefYamlGenericParser tests that the generic parser can parse all SQL statements
// from the psqldef YAML test files. This validates that the generic parser works as a fallback
// for PostgreSQL statements.
func TestPsqldefYamlGenericParser(t *testing.T) {
	// Read all YAML test files from cmd/psqldef/*.yml
	tests, err := readPsqldefTests()
	if err != nil {
		t.Fatalf("Failed to read psqldef YAML tests: %v", err)
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			// Test parsing the 'current' schema if present
			if testCase.Current != "" {
				t.Run("current", func(t *testing.T) {
					stmt, err := ParseDDL(testCase.Current, ParserModePostgres)
					if err != nil {
						t.Errorf("Failed to parse 'current' schema: %v\nSQL:\n%s", err, testCase.Current)
						return
					}
					if stmt == nil {
						t.Errorf("ParseDDL returned nil statement for 'current' schema\nSQL:\n%s", testCase.Current)
					}
				})
			}

			// Test parsing the 'desired' schema if present
			if testCase.Desired != "" {
				t.Run("desired", func(t *testing.T) {
					stmt, err := ParseDDL(testCase.Desired, ParserModePostgres)
					if err != nil {
						t.Errorf("Failed to parse 'desired' schema: %v\nSQL:\n%s", err, testCase.Desired)
						return
					}
					if stmt == nil {
						t.Errorf("ParseDDL returned nil statement for 'desired' schema\nSQL:\n%s", testCase.Desired)
					}
				})
			}
		})
	}
}
