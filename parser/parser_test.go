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

func TestAlterTableStatements(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		mode      ParserMode
		shouldErr bool
	}{
		{
			name: "ALTER TABLE ADD COLUMN basic",
			sql:  `ALTER TABLE test ADD COLUMN name varchar(100)`,
			mode: ParserModePostgres,
		},
		{
			name: "ALTER TABLE ADD COLUMN with schema",
			sql:  `ALTER TABLE "public"."test" ADD COLUMN "name" varchar(100)`,
			mode: ParserModePostgres,
		},
		{
			name: "ALTER TABLE DROP COLUMN",
			sql:  `ALTER TABLE "public"."test" DROP COLUMN "name"`,
			mode: ParserModePostgres,
		},
		{
			name: "ALTER TABLE ALTER COLUMN TYPE",
			sql:  `ALTER TABLE "public"."test" ALTER COLUMN "timestamp_wtz_plain" TYPE timestamp`,
			mode: ParserModePostgres,
		},
		{
			name: "ALTER TABLE ADD CONSTRAINT FOREIGN KEY",
			sql:  `ALTER TABLE "public"."t2" ADD CONSTRAINT "fk" FOREIGN KEY ("id1","id2") REFERENCES "public"."t1" ("id1","id2")`,
			mode: ParserModePostgres,
		},
		{
			name: "ALTER TABLE DROP CONSTRAINT",
			sql:  `ALTER TABLE "public"."test" DROP CONSTRAINT "test_pkey"`,
			mode: ParserModePostgres,
		},
		{
			name: "ALTER TABLE ADD CONSTRAINT CHECK simple",
			sql:  `ALTER TABLE "public"."products" ADD CONSTRAINT products_status_check CHECK (status = 'active')`,
			mode: ParserModePostgres,
		},
		{
			name: "ALTER TABLE ADD CONSTRAINT UNIQUE",
			sql:  `ALTER TABLE "public"."products" ADD CONSTRAINT "products_sku_key" UNIQUE ("sku")`,
			mode: ParserModePostgres,
		},
		{
			name: "ALTER TABLE ALTER COLUMN SET DEFAULT",
			sql:  `ALTER TABLE "public"."test" ALTER COLUMN "col" SET DEFAULT false`,
			mode: ParserModePostgres,
		},
		{
			name: "ALTER TABLE ALTER COLUMN SET NOT NULL",
			sql:  `ALTER TABLE "public"."test" ALTER COLUMN "col" SET NOT NULL`,
			mode: ParserModePostgres,
		},
		{
			name: "ALTER TABLE ALTER COLUMN DROP NOT NULL",
			sql:  `ALTER TABLE "public"."test" ALTER COLUMN "col" DROP NOT NULL`,
			mode: ParserModePostgres,
		},
		{
			name: "ALTER TABLE ADD PRIMARY KEY",
			sql:  `ALTER TABLE ONLY "public"."users" ADD CONSTRAINT "users_pkey" PRIMARY KEY ("id")`,
			mode: ParserModePostgres,
		},
		{
			name: "ALTER TYPE ADD VALUE",
			sql:  `ALTER TYPE status ADD VALUE 'pending'`,
			mode: ParserModePostgres,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt, err := ParseDDL(tt.sql, tt.mode)
			if tt.shouldErr {
				if err == nil {
					t.Errorf("Expected error but got none for %s", tt.name)
				}
			} else {
				if err != nil {
					t.Errorf("Failed to parse %s: %v", tt.name, err)
				} else {
					t.Logf("Successfully parsed %s: %T", tt.name, stmt)
				}
			}
		})
	}
}

func TestSpecialConstructs(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		mode      ParserMode
		shouldErr bool
	}{
		{
			name: "Type casting with :: in default",
			sql:  `CREATE TABLE test (col text DEFAULT 'value'::text)`,
			mode: ParserModePostgres,
		},
		{
			name: "CURRENT_TIMESTAMP plus interval",
			sql:  `CREATE TABLE test (expires_at timestamp DEFAULT (CURRENT_TIMESTAMP + '1 day'::interval))`,
			mode: ParserModePostgres,
		},
		{
			name: "Simple expression addition",
			sql:  `CREATE TABLE test (num int DEFAULT (1 + 2))`,
			mode: ParserModePostgres,
		},
		{
			name: "String plus interval",
			sql:  `CREATE TABLE test (expires_at timestamp DEFAULT ('2024-01-01'::timestamp + '1 day'::interval))`,
			mode: ParserModePostgres,
		},
		{
			name: "CURRENT_TIMESTAMP standalone",
			sql:  `CREATE TABLE test (created_at timestamp DEFAULT CURRENT_TIMESTAMP)`,
			mode: ParserModePostgres,
		},
		{
			name: "COALESCE in index",
			sql:  `CREATE INDEX idx ON users (COALESCE(user_name, 'default'))`,
			mode: ParserModePostgres,
		},
		{
			name: "COALESCE in index with multiple columns",
			sql:  `CREATE INDEX create_index_with_function_call ON users (name, COALESCE(user_name, 'NO_NAME'::TEXT))`,
			mode: ParserModePostgres,
		},
		{
			name: "Type casting with :: timestamp",
			sql:  `CREATE TABLE test (col text DEFAULT CURRENT_TIMESTAMP::date::text)`,
			mode: ParserModePostgres,
		},
		{
			name: "Array constructor simple",
			sql:  `CREATE TABLE test (col int[] DEFAULT '{}'::int[])`,
			mode: ParserModePostgres,
		},
		{
			name: "Array constructor with ARRAY keyword",
			sql:  `CREATE TABLE test (col int[] DEFAULT ARRAY[]::int[])`,
			mode: ParserModePostgres,
		},
		{
			name: "Array with elements",
			sql:  `CREATE TABLE test (state TEXT CHECK (state = ANY (ARRAY['active', 'pending'])))`,
			mode: ParserModePostgres,
		},
		{
			name: "GRANT simple",
			sql:  `GRANT SELECT ON TABLE users TO readonly_user`,
			mode: ParserModePostgres,
		},
		{
			name: "GRANT ALL PRIVILEGES",
			sql:  `GRANT ALL PRIVILEGES ON TABLE users TO admin_role`,
			mode: ParserModePostgres,
		},
		{
			name: "GRANT WITH GRANT OPTION",
			sql:  `GRANT SELECT ON TABLE users TO readonly_user WITH GRANT OPTION`,
			mode: ParserModePostgres,
		},
		{
			name: "REVOKE simple",
			sql:  `REVOKE SELECT ON TABLE users FROM readonly_user`,
			mode: ParserModePostgres,
		},
		{
			name: "REVOKE with CASCADE",
			sql:  `REVOKE SELECT ON TABLE users FROM readonly_user CASCADE`,
			mode: ParserModePostgres,
		},
		{
			name: "COMMENT ON COLUMN",
			sql:  `COMMENT ON COLUMN users.id IS 'User primary key'`,
			mode: ParserModePostgres,
		},
		{
			name: "EXCLUDE constraint in CREATE TABLE",
			sql:  `CREATE TABLE test (id int, CONSTRAINT ex1 EXCLUDE (name WITH =))`,
			mode: ParserModePostgres,
		},
		{
			name: "ALTER TABLE ADD EXCLUDE constraint",
			sql:  `ALTER TABLE test ADD CONSTRAINT ex1 EXCLUDE USING GIST (event_start WITH &&, event_end WITH &&)`,
			mode: ParserModePostgres,
		},
		{
			name: "WITH TIME ZONE",
			sql:  `CREATE TABLE test (ts timestamp WITH TIME ZONE)`,
			mode: ParserModePostgres,
		},
		{
			name: "WITHOUT TIME ZONE",
			sql:  `CREATE TABLE test (ts timestamp WITHOUT TIME ZONE)`,
			mode: ParserModePostgres,
		},
		{
			name: "Posix regex operators",
			sql:  `CREATE TABLE test (val text CHECK (val ~ '[a-z]+'))`,
			mode: ParserModePostgres,
		},
		{
			name: "Posix regex not match",
			sql:  `CREATE TABLE test (val text CHECK (val !~ '[0-9]+'))`,
			mode: ParserModePostgres,
		},
		{
			name: "Posix regex case insensitive",
			sql:  `CREATE TABLE test (val text CHECK (val ~* '[A-Z]+'))`,
			mode: ParserModePostgres,
		},
		{
			name: "Posix regex not match case insensitive",
			sql:  `CREATE TABLE test (val text CHECK (val !~* '[A-Z]+'))`,
			mode: ParserModePostgres,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stmt, err := ParseDDL(tt.sql, tt.mode)
			if tt.shouldErr {
				if err == nil {
					t.Errorf("Expected error but got none for %s", tt.name)
				}
			} else {
				if err != nil {
					t.Errorf("Failed to parse %s: %v", tt.name, err)
				} else {
					t.Logf("Successfully parsed %s: %T", tt.name, stmt)
				}
			}
		})
	}
}
