// Integration test of sqlite3def command.
//
// Test requirement:
//   - go command
//   - `sqlite3` must succeed
package main

import (
	"log"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	tu "github.com/sqldef/sqldef/v3/cmd/testutils"
	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/database/sqlite3"
	"github.com/sqldef/sqldef/v3/parser"
	"github.com/sqldef/sqldef/v3/schema"
)

const (
	applyPrefix     = "-- Apply --\n"
	nothingModified = "-- Nothing is modified --\n"
)

func wrapWithTransaction(ddls string) string {
	return applyPrefix + "BEGIN;\n" + ddls + "COMMIT;\n"
}

func TestApply(t *testing.T) {
	tests, err := tu.ReadTests("tests*.yml")
	if err != nil {
		t.Fatal(err)
	}

	sqlParser := database.NewParser(parser.ParserModeSQLite3)
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Use in-memory database for parallel testing
			// Each connection to :memory: creates a new, independent database
			db, err := connectDatabaseByName(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			tu.RunTest(t, db, test, schema.GeneratorModeSQLite3, sqlParser, "", "")
		})
	}
}

func TestSQLite3defApply(t *testing.T) {
	resetTestDatabase()

	createTable := tu.StripHeredoc(`
		CREATE TABLE bigdata (
		  data integer
		);
		`,
	)

	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestSQLite3defDryRun(t *testing.T) {
	resetTestDatabase()
	tu.WriteFile("schema.sql", tu.StripHeredoc(`
	    CREATE TABLE users (
	        id integer NOT NULL PRIMARY KEY,
	        age integer
	    );`,
	))

	dryRun := tu.MustExecute(t, "./sqlite3def", "sqlite3def_test", "--dry-run", "--file", "schema.sql")
	apply := tu.MustExecute(t, "./sqlite3def", "sqlite3def_test", "--file", "schema.sql")
	assert.Equal(t, strings.Replace(apply, "Apply", "dry run", 1), dryRun)
}

func TestSQLite3defDropTable(t *testing.T) {
	resetTestDatabase()
	mustSqlite3Exec("sqlite3def_test", tu.StripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    age integer
		);`,
	))

	tu.WriteFile("schema.sql", "")

	dropTable := "DROP TABLE \"users\";\n"
	out := tu.MustExecute(t, "./sqlite3def", "sqlite3def_test", "--enable-drop", "--file", "schema.sql")
	assert.Equal(t, wrapWithTransaction(dropTable), out)
}

func TestSQLite3defConfigInlineEnableDrop(t *testing.T) {
	resetTestDatabase()

	ddl := tu.StripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    age integer
		);`,
	)
	mustSqlite3Exec("sqlite3def_test", ddl)

	tu.WriteFile("schema.sql", "")

	dropTable := "DROP TABLE \"users\";\n"
	expectedOutput := wrapWithTransaction(dropTable)

	outFlag := tu.MustExecute(t, "./sqlite3def", "sqlite3def_test", "--enable-drop", "--file", "schema.sql")
	assert.Equal(t, expectedOutput, outFlag)

	mustSqlite3Exec("sqlite3def_test", ddl)

	outConfigInline := tu.MustExecute(t, "./sqlite3def", "sqlite3def_test", "--config-inline", "enable_drop: true", "--file", "schema.sql")
	assert.Equal(t, expectedOutput, outConfigInline)
}

func TestSQLite3defExport(t *testing.T) {
	resetTestDatabase()
	out := tu.MustExecute(t, "./sqlite3def", "sqlite3def_test", "--export")
	assert.Equal(t, "-- No table exists --\n", out)

	mustSqlite3Exec("sqlite3def_test", tu.StripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    age integer
		);`,
	))
	out = tu.MustExecute(t, "./sqlite3def", "sqlite3def_test", "--export")
	assert.Equal(t, tu.StripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    age integer
		);
		`,
	), out)
}

func TestSQLite3defConfigIncludesTargetTables(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	mustSqlite3Exec("sqlite3def_test", usersTable+users1Table+users10Table)

	tu.WriteFile("schema.sql", usersTable+users1Table)
	tu.WriteFile("config.yml", "target_tables: |\n  users\n  users_\\d\n")

	apply := tu.MustExecute(t, "./sqlite3def", "--config", "config.yml", "--file", "schema.sql", "sqlite3def_test")
	assert.Equal(t, nothingModified, apply)
}

func TestSQLite3defConfigIncludesSkipTables(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	mustSqlite3Exec("sqlite3def_test", usersTable+users1Table+users10Table)

	tu.WriteFile("schema.sql", usersTable+users1Table)
	tu.WriteFile("config.yml", "skip_tables: |\n  users_10\n")

	apply := tu.MustExecute(t, "./sqlite3def", "--config", "config.yml", "--file", "schema.sql", "sqlite3def_test")
	assert.Equal(t, nothingModified, apply)
}

func TestSQLite3defConfigInlineSkipTables(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	mustSqlite3Exec("sqlite3def_test", usersTable+users1Table+users10Table)

	tu.WriteFile("schema.sql", usersTable+users1Table)

	apply := tu.MustExecute(t, "./sqlite3def", "--config-inline", "skip_tables: users_10", "--file", "schema.sql", "sqlite3def_test")
	assert.Equal(t, nothingModified, apply)
}

func TestSQLite3defConfigMerge(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	postsTable := "CREATE TABLE posts (id bigint);"
	mustSqlite3Exec("sqlite3def_test", usersTable+users1Table+users10Table+postsTable)

	tu.WriteFile("schema.sql", usersTable+users1Table+postsTable)

	// Config file says to skip users_10, but inline config overrides to skip posts
	tu.WriteFile("config.yml", "skip_tables: users_10")

	// inline config should override file config, so posts will be skipped instead of users_10
	// This means users_10 will be dropped (skipped without --enable-drop) and posts will be kept
	apply := tu.MustExecute(t, "./sqlite3def", "--config", "config.yml", "--config-inline", "skip_tables: posts", "--file", "schema.sql", "sqlite3def_test")
	assert.Equal(t, wrapWithTransaction("-- Skipped: DROP TABLE \"users_10\";\n"), apply)
}

func TestSQLite3defMultipleConfigs(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	postsTable := "CREATE TABLE posts (id bigint);"
	commentsTable := "CREATE TABLE comments (id bigint);"
	mustSqlite3Exec("sqlite3def_test", usersTable+users1Table+users10Table+postsTable+commentsTable)

	tu.WriteFile("schema.sql", usersTable+users1Table+postsTable)

	// First config skips users_10
	tu.WriteFile("config1.yml", "skip_tables: users_10")
	// Second config skips posts
	tu.WriteFile("config2.yml", "skip_tables: posts")
	// Third config skips comments (this should win)
	tu.WriteFile("config3.yml", "skip_tables: comments")

	// The last config (config3.yml) should win, so only comments will be skipped
	// users_10 is NOT in the final skip list, so it will be dropped
	// comments IS in the final skip list, so it won't be touched (even though it's not in schema.sql)
	apply := tu.MustExecute(t, "./sqlite3def", "--config", "config1.yml", "--config", "config2.yml", "--config", "config3.yml", "--file", "schema.sql", "sqlite3def_test")
	assert.Equal(t, wrapWithTransaction("-- Skipped: DROP TABLE \"users_10\";\n"), apply)
}

func TestSQLite3defMultipleInlineConfigs(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	postsTable := "CREATE TABLE posts (id bigint);"
	mustSqlite3Exec("sqlite3def_test", usersTable+users1Table+users10Table+postsTable)

	tu.WriteFile("schema.sql", usersTable+users1Table+postsTable)

	// Multiple inline configs - the last one should win
	apply := tu.MustExecute(t, "./sqlite3def",
		"--config-inline", "skip_tables: posts",
		"--config-inline", "skip_tables: users_1",
		"--config-inline", "skip_tables: users_10",
		"--file", "schema.sql", "sqlite3def_test")
	assert.Equal(t, nothingModified, apply)
}

func TestSQLite3defVirtualTable(t *testing.T) {
	resetTestDatabase()

	// SQLite FTS3 and FTS4 Extensions https://www.sqlite.org/fts3.html
	createTableFtsA := tu.StripHeredoc(`
		CREATE VIRTUAL TABLE fts_a USING fts4(
		  body TEXT,
		  tokenize=unicode61 "tokenchars=.=" "separators=X"
		);
	`)
	createTableFtsB := tu.StripHeredoc(`
		CREATE VIRTUAL TABLE fts_b USING fts3(
		  subject VARCHAR(256) NOT NULL,
		  body TEXT CHECK(length(body) < 10240),
		  tokenize=icu en_AU
		);
	`)
	// SQLite FTS5 Extension https://www.sqlite.org/fts5.html
	createTableFts5 := tu.StripHeredoc(`
		CREATE VIRTUAL TABLE fts5tbl USING fts5(
			x,
			tokenize = 'porter ascii'
		);
	`)
	// The SQLite R*Tree Module https://www.sqlite.org/rtree.html
	createTableRtreeA := tu.StripHeredoc(`
		CREATE VIRTUAL TABLE rtree_a USING rtree(
		  id,            -- Integer primary key
		  minX, maxX,    -- Minimum and maximum X coordinate
		  minY, maxY,    -- Minimum and maximum Y coordinate
		  +objname TEXT, -- name of the object
		  +objtype TEXT, -- object type
		  +boundary BLOB -- detailed boundary of object
		);
	`)

	tu.WriteFile("schema.sql", createTableFtsA+createTableFtsB+createTableFts5+createTableRtreeA)
	// FTS is not available in modernc.org/sqlite v1.19.4 package
	tu.WriteFile("config.yml", tu.StripHeredoc(`
		skip_tables: |
		  fts_a
		  fts_a_\w+
		  fts_b
		  fts_b_\w+
		  rtree_a_\w+
		  fts5tbl_\w+
	`))
	actual := tu.MustExecute(t, "./sqlite3def", "--config", "config.yml", "--file", "schema.sql", "sqlite3def_test")
	assert.Equal(t, wrapWithTransaction(createTableFts5+createTableRtreeA), actual)
	actual = tu.MustExecute(t, "./sqlite3def", "--config", "config.yml", "--file", "schema.sql", "sqlite3def_test")
	assert.Equal(t, nothingModified, actual)
}

func TestSQLite3defHelp(t *testing.T) {
	_, err := tu.Execute("./sqlite3def", "--help")
	if err != nil {
		t.Errorf("failed to run --help: %s", err)
	}

	out, err := tu.Execute("./sqlite3def")
	if err == nil {
		t.Errorf("no database must be error, but successfully got: %s", out)
	}
}

func TestDeprecatedRenameAnnotation(t *testing.T) {
	resetTestDatabase()

	// Create initial table with old column name
	mustSqlite3Exec("sqlite3def_test", tu.StripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    username text NOT NULL
		);`,
	))

	// Define schema using deprecated @rename annotation
	schemaWithDeprecatedRename := tu.StripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    user_name text NOT NULL -- @rename from=username
		);`,
	)

	tu.WriteFile("schema.sql", schemaWithDeprecatedRename)

	// Execute sqlite3def and capture combined output (stdout + stderr)
	out, err := tu.Execute("./sqlite3def", "sqlite3def_test", "--file", "schema.sql")
	if err != nil {
		t.Fatalf("sqlite3def execution failed: %s\nOutput: %s", err, out)
	}

	// Check that the deprecation warning is present
	if !strings.Contains(out, "-- WARNING: @rename is deprecated. Please use @renamed instead.") {
		t.Errorf("Expected deprecation warning not found in output:\n%s", out)
	}

	// Verify that the rename operation actually worked
	if !strings.Contains(out, "ALTER TABLE \"users\" RENAME COLUMN \"username\" TO \"user_name\";") {
		t.Errorf("Expected rename operation not found in output:\n%s", out)
	}

	// Verify the table structure is correct after rename
	export := tu.MustExecute(t, "./sqlite3def", "sqlite3def_test", "--export")
	if !strings.Contains(export, "\"user_name\" text NOT NULL") && !strings.Contains(export, "user_name text NOT NULL") {
		t.Errorf("Column rename didn't work correctly. Export output:\n%s", export)
	}

	// Now test with @renamed (no warning expected)
	mustSqlite3Exec("sqlite3def_test", tu.StripHeredoc(`
		DROP TABLE users;
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    old_name text NOT NULL
		);`,
	))

	schemaWithRenamed := tu.StripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    new_name text NOT NULL -- @renamed from=old_name
		);`,
	)

	tu.WriteFile("schema.sql", schemaWithRenamed)
	out, err = tu.Execute("./sqlite3def", "sqlite3def_test", "--file", "schema.sql")
	if err != nil {
		t.Fatalf("sqlite3def execution failed: %s\nOutput: %s", err, out)
	}

	// Should NOT have warning for @renamed
	if strings.Contains(out, "-- WARNING: @rename is deprecated") {
		t.Errorf("Unexpected deprecation warning for @renamed in output:\n%s", out)
	}

	// Should still perform the rename
	if !strings.Contains(out, "ALTER TABLE \"users\" RENAME COLUMN \"old_name\" TO \"new_name\";") {
		t.Errorf("Expected rename operation not found for @renamed annotation:\n%s", out)
	}
}

func TestMain(m *testing.M) {
	resetTestDatabase()
	tu.BuildForTest()
	status := m.Run()
	_ = os.Remove("sqlite3def")
	_ = os.Remove("sqlite3def_test")
	_ = os.Remove("schema.sql")
	_ = os.Remove("config.yml")
	os.Exit(status)
}

func assertApplyOutput(t *testing.T, schema string, expected string) {
	t.Helper()
	actual := assertApplyOutputWithConfig(t, schema, database.GeneratorConfig{EnableDrop: false, LegacyIgnoreQuotes: true})
	assert.Equal(t, expected, actual)
}

func assertApplyOutputWithConfig(t *testing.T, desiredSchema string, config database.GeneratorConfig) string {
	t.Helper()

	db, err := connectDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sqlParser := database.NewParser(parser.ParserModeSQLite3)
	output, err := tu.ApplyWithOutput(db, schema.GeneratorModeSQLite3, sqlParser, desiredSchema, config)
	if err != nil {
		t.Fatal(err)
	}

	return output
}

func assertApplyOptionsOutput(t *testing.T, schema string, expected string, options ...string) {
	t.Helper()
	tu.WriteFile("schema.sql", schema)
	args := append([]string{
		"sqlite3def_test", "--file", "schema.sql",
	}, options...)

	actual := tu.MustExecute(t, "./sqlite3def", args...)
	assert.Equal(t, expected, actual)
}

func resetTestDatabase() {
	_, err := os.Stat("sqlite3def_test")
	if err == nil {
		err := os.Remove("sqlite3def_test")
		if err != nil {
			log.Fatal(err)
		}
	}
}

func connectDatabase() (database.Database, error) {
	return sqlite3.NewDatabase(database.Config{
		DbName: "sqlite3def_test",
	})
}

// connectDatabaseByName connects to a SQLite database with the given name.
// Use ":memory:" for in-memory databases (useful for parallel testing).
func connectDatabaseByName(dbName string) (database.Database, error) {
	return sqlite3.NewDatabase(database.Config{
		DbName: dbName,
	})
}

// sqlite3Query executes a query against the database and returns rows as string
func sqlite3Query(dbName string, query string) (string, error) {
	db, err := sqlite3.NewDatabase(database.Config{
		DbName: dbName,
	})
	if err != nil {
		return "", err
	}
	defer db.Close()

	return tu.QueryRows(db, query)
}

// sqlite3Exec executes a statement against the database (doesn't return rows)
func sqlite3Exec(dbName string, statement string) error {
	db, err := sqlite3.NewDatabase(database.Config{
		DbName: dbName,
	})
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.DB().Exec(statement)
	return err
}

// mustSqlite3Exec executes a statement against the database and panics on error
func mustSqlite3Exec(dbName string, statement string) {
	if err := sqlite3Exec(dbName, statement); err != nil {
		panic(err)
	}
}

func TestSQLite3defConfigOrderPreserved(t *testing.T) {
	resetTestDatabase()

	// Create tables
	createTable := tu.StripHeredoc(`
		CREATE TABLE users (id integer primary key);
		CREATE TABLE posts (id integer primary key);
		CREATE TABLE comments (id integer primary key);
	`)
	assertApplyOutput(t, createTable, wrapWithTransaction(
		"CREATE TABLE users (id integer primary key);\n"+
			"CREATE TABLE posts (id integer primary key);\n"+
			"CREATE TABLE comments (id integer primary key);\n"))

	// Create config files
	config1 := "config1.yml"
	config2 := "config2.yml"
	tu.WriteFile(config1, "skip_tables: users") // Skip users
	tu.WriteFile(config2, "skip_tables: posts") // Skip posts
	defer os.Remove(config1)
	defer os.Remove(config2)

	// Test: file, inline, file - the last file should win
	// This tests that the order is preserved: config1, inline(comments), config2
	// Final result: posts should be skipped (from config2)
	out := tu.MustExecute(t, "./sqlite3def",
		"--config", config1,
		"--config-inline", "skip_tables: comments",
		"--config", config2,
		"--export", "sqlite3def_test")

	// Should export only users and comments (posts is skipped by the last config)
	expectedContent := "CREATE TABLE users (id integer primary key);\n\nCREATE TABLE comments (id integer primary key);\n"
	if out != expectedContent {
		t.Errorf("Expected export with config order preserved (last config2 skipping posts):\n%s\nGot:\n%s", expectedContent, out)
	}

	// Test: inline, file, inline - the last inline should win
	// This tests: inline(users), config2(posts), inline(comments)
	// Final result: comments should be skipped
	out2 := tu.MustExecute(t, "./sqlite3def",
		"--config-inline", "skip_tables: users",
		"--config", config2,
		"--config-inline", "skip_tables: comments",
		"--export", "sqlite3def_test")

	// Should export only users and posts (comments is skipped by the last inline)
	expectedContent2 := "CREATE TABLE users (id integer primary key);\n\nCREATE TABLE posts (id integer primary key);\n"
	if out2 != expectedContent2 {
		t.Errorf("Expected export with last inline winning (skipping comments):\n%s\nGot:\n%s", expectedContent2, out2)
	}
}
