// Integration test of sqlite3def command.
//
// Test requirement:
//   - go command
//   - `sqlite3` must succeed
package main

import (
	"log"
	"os"
	"regexp"
	"strings"
	"testing"

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

	createTable := stripHeredoc(`
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
	writeFile("schema.sql", stripHeredoc(`
	    CREATE TABLE users (
	        id integer NOT NULL PRIMARY KEY,
	        age integer
	    );`,
	))

	dryRun := assertedExecute(t, "./sqlite3def", "sqlite3def_test", "--dry-run", "--file", "schema.sql")
	apply := assertedExecute(t, "./sqlite3def", "sqlite3def_test", "--file", "schema.sql")
	assertEquals(t, dryRun, strings.Replace(apply, "Apply", "dry run", 1))
}

func TestSQLite3defDropTable(t *testing.T) {
	resetTestDatabase()
	mustSqlite3Exec("sqlite3def_test", stripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    age integer
		);`,
	))

	writeFile("schema.sql", "")

	dropTable := "DROP TABLE `users`;\n"
	out := assertedExecute(t, "./sqlite3def", "sqlite3def_test", "--enable-drop", "--file", "schema.sql")
	assertEquals(t, out, wrapWithTransaction(dropTable))
}

func TestSQLite3defConfigInlineEnableDrop(t *testing.T) {
	resetTestDatabase()

	ddl := stripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    age integer
		);`,
	)
	mustSqlite3Exec("sqlite3def_test", ddl)

	writeFile("schema.sql", "")

	dropTable := "DROP TABLE `users`;\n"
	expectedOutput := wrapWithTransaction(dropTable)

	outFlag := assertedExecute(t, "./sqlite3def", "sqlite3def_test", "--enable-drop", "--file", "schema.sql")
	assertEquals(t, outFlag, expectedOutput)

	mustSqlite3Exec("sqlite3def_test", ddl)

	outConfigInline := assertedExecute(t, "./sqlite3def", "sqlite3def_test", "--config-inline", "enable_drop: true", "--file", "schema.sql")
	assertEquals(t, outConfigInline, expectedOutput)
}

func TestSQLite3defExport(t *testing.T) {
	resetTestDatabase()
	out := assertedExecute(t, "./sqlite3def", "sqlite3def_test", "--export")
	assertEquals(t, out, "-- No table exists --\n")

	mustSqlite3Exec("sqlite3def_test", stripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    age integer
		);`,
	))
	out = assertedExecute(t, "./sqlite3def", "sqlite3def_test", "--export")
	assertEquals(t, out, stripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    age integer
		);
		`,
	))
}

func TestSQLite3defConfigIncludesTargetTables(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	mustSqlite3Exec("sqlite3def_test", usersTable+users1Table+users10Table)

	writeFile("schema.sql", usersTable+users1Table)
	writeFile("config.yml", "target_tables: |\n  users\n  users_\\d\n")

	apply := assertedExecute(t, "./sqlite3def", "--config", "config.yml", "--file", "schema.sql", "sqlite3def_test")
	assertEquals(t, apply, nothingModified)
}

func TestSQLite3defConfigIncludesSkipTables(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	mustSqlite3Exec("sqlite3def_test", usersTable+users1Table+users10Table)

	writeFile("schema.sql", usersTable+users1Table)
	writeFile("config.yml", "skip_tables: |\n  users_10\n")

	apply := assertedExecute(t, "./sqlite3def", "--config", "config.yml", "--file", "schema.sql", "sqlite3def_test")
	assertEquals(t, apply, nothingModified)
}

func TestSQLite3defConfigInlineSkipTables(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	mustSqlite3Exec("sqlite3def_test", usersTable+users1Table+users10Table)

	writeFile("schema.sql", usersTable+users1Table)

	apply := assertedExecute(t, "./sqlite3def", "--config-inline", "skip_tables: users_10", "--file", "schema.sql", "sqlite3def_test")
	assertEquals(t, apply, nothingModified)
}

func TestSQLite3defConfigMerge(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	postsTable := "CREATE TABLE posts (id bigint);"
	mustSqlite3Exec("sqlite3def_test", usersTable+users1Table+users10Table+postsTable)

	writeFile("schema.sql", usersTable+users1Table+postsTable)

	// Config file says to skip users_10, but inline config overrides to skip posts
	writeFile("config.yml", "skip_tables: users_10")

	// inline config should override file config, so posts will be skipped instead of users_10
	// This means users_10 will be dropped (skipped without --enable-drop) and posts will be kept
	apply := assertedExecute(t, "./sqlite3def", "--config", "config.yml", "--config-inline", "skip_tables: posts", "--file", "schema.sql", "sqlite3def_test")
	assertEquals(t, apply, wrapWithTransaction("-- Skipped: DROP TABLE `users_10`;\n"))
}

func TestSQLite3defMultipleConfigs(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	postsTable := "CREATE TABLE posts (id bigint);"
	commentsTable := "CREATE TABLE comments (id bigint);"
	mustSqlite3Exec("sqlite3def_test", usersTable+users1Table+users10Table+postsTable+commentsTable)

	writeFile("schema.sql", usersTable+users1Table+postsTable)

	// First config skips users_10
	writeFile("config1.yml", "skip_tables: users_10")
	// Second config skips posts
	writeFile("config2.yml", "skip_tables: posts")
	// Third config skips comments (this should win)
	writeFile("config3.yml", "skip_tables: comments")

	// The last config (config3.yml) should win, so only comments will be skipped
	// users_10 is NOT in the final skip list, so it will be dropped
	// comments IS in the final skip list, so it won't be touched (even though it's not in schema.sql)
	apply := assertedExecute(t, "./sqlite3def", "--config", "config1.yml", "--config", "config2.yml", "--config", "config3.yml", "--file", "schema.sql", "sqlite3def_test")
	assertEquals(t, apply, wrapWithTransaction("-- Skipped: DROP TABLE `users_10`;\n"))
}

func TestSQLite3defMultipleInlineConfigs(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	postsTable := "CREATE TABLE posts (id bigint);"
	mustSqlite3Exec("sqlite3def_test", usersTable+users1Table+users10Table+postsTable)

	writeFile("schema.sql", usersTable+users1Table+postsTable)

	// Multiple inline configs - the last one should win
	apply := assertedExecute(t, "./sqlite3def",
		"--config-inline", "skip_tables: posts",
		"--config-inline", "skip_tables: users_1",
		"--config-inline", "skip_tables: users_10",
		"--file", "schema.sql", "sqlite3def_test")
	assertEquals(t, apply, nothingModified)
}

func TestSQLite3defVirtualTable(t *testing.T) {
	resetTestDatabase()

	// SQLite FTS3 and FTS4 Extensions https://www.sqlite.org/fts3.html
	createTableFtsA := stripHeredoc(`
		CREATE VIRTUAL TABLE fts_a USING fts4(
		  body TEXT,
		  tokenize=unicode61 "tokenchars=.=" "separators=X"
		);
	`)
	createTableFtsB := stripHeredoc(`
		CREATE VIRTUAL TABLE fts_b USING fts3(
		  subject VARCHAR(256) NOT NULL,
		  body TEXT CHECK(length(body) < 10240),
		  tokenize=icu en_AU
		);
	`)
	// SQLite FTS5 Extension https://www.sqlite.org/fts5.html
	createTableFts5 := stripHeredoc(`
		CREATE VIRTUAL TABLE fts5tbl USING fts5(
			x,
			tokenize = 'porter ascii'
		);
	`)
	// The SQLite R*Tree Module https://www.sqlite.org/rtree.html
	createTableRtreeA := stripHeredoc(`
		CREATE VIRTUAL TABLE rtree_a USING rtree(
		  id,            -- Integer primary key
		  minX, maxX,    -- Minimum and maximum X coordinate
		  minY, maxY,    -- Minimum and maximum Y coordinate
		  +objname TEXT, -- name of the object
		  +objtype TEXT, -- object type
		  +boundary BLOB -- detailed boundary of object
		);
	`)

	writeFile("schema.sql", createTableFtsA+createTableFtsB+createTableFts5+createTableRtreeA)
	// FTS is not available in modernc.org/sqlite v1.19.4 package
	writeFile("config.yml", stripHeredoc(`
		skip_tables: |
		  fts_a
		  fts_a_\w+
		  fts_b
		  fts_b_\w+
		  rtree_a_\w+
		  fts5tbl_\w+
	`))
	actual := assertedExecute(t, "./sqlite3def", "--config", "config.yml", "--file", "schema.sql", "sqlite3def_test")
	assertEquals(t, actual, wrapWithTransaction(createTableFts5+createTableRtreeA))
	actual = assertedExecute(t, "./sqlite3def", "--config", "config.yml", "--file", "schema.sql", "sqlite3def_test")
	assertEquals(t, actual, nothingModified)
}

// https://www.sqlite.org/lang_createtrigger.html
func TestSQLite3defCreateTrigger(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id integer NOT NULL PRIMARY KEY,
		  age integer NOT NULL
		);
		CREATE TABLE logs (
		  id integer NOT NULL PRIMARY KEY,
		  typ TEXT NOT NULL,
		  typ_id integer NOT NULL,
		  body TEXT NOT NULL,
		  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		create view user_view as select * from users;
	`)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	queries := map[string]string{
		"before, delete": `
			CREATE TRIGGER users_delete BEFORE DELETE ON users
			BEGIN
				delete from logs where typ = 'user' and typ_id = OLD.id;
			END;
		`,
		"after, update": `
			CREATE TRIGGER users_update AFTER UPDATE ON users
			BEGIN
				delete from logs where typ = 'user' and typ_id = OLD.id;
				insert into logs(typ, typ_id, body) values ('user', NEW.id, 'updated user');
			END;
		`,
		"instead of, insert": `
			CREATE TRIGGER user_view_update INSTEAD OF INSERT ON user_view
			BEGIN
				insert into users(id, age) values (NEW.id, NEW.age);
			END;
		`,
		"update of the single column": `
			CREATE TRIGGER users_update_of_id AFTER UPDATE OF id ON users
			BEGIN
				delete from logs where typ = 'user' and typ_id = OLD.id;
				insert into logs(typ, typ_id, body) values ('user', NEW.id, 'updated user');
			END;
		`,
		"update of multiple columns": `
			CREATE TRIGGER users_update_of_id_and_age AFTER UPDATE OF id,age ON users
			BEGIN
				delete from logs where typ = 'user' and typ_id = OLD.id;
				insert into logs(typ, typ_id, body) values ('user', NEW.id, 'updated user');
			END;
		`,
		"for each row": `
			CREATE TRIGGER users_delete_for_each_row BEFORE DELETE ON users FOR EACH ROW
			BEGIN
				delete from logs where typ = 'user' and typ_id = OLD.id;
			END;
		`,
		"when": `
			CREATE TRIGGER users_delete_when BEFORE DELETE ON users
			WHEN OLD.age > 20
			BEGIN
				delete from logs where typ = 'user' and typ_id = OLD.id;
			END;
		`,
		"for each row, when": `
			CREATE TRIGGER users_delete_for_each_row_and_when BEFORE DELETE ON users FOR EACH ROW
			WHEN OLD.age > 20
			BEGIN
				delete from logs where typ = 'user' and typ_id = OLD.id;
			END;
		`,
	}

	var createTrigger string
	for _, q := range queries {
		createTrigger += stripHeredoc(q)
	}

	// The iteration order of a map is random,
	// so SQL that needs guaranteed order should be written separately.
	createTrigger += stripHeredoc(`
		CREATE TRIGGER IF NOT EXISTS users_insert after insert ON users
		BEGIN
			insert into logs(typ, typ_id, body) values ('user', NEW.id, 'inserted user');
		END;
	`)

	assertApplyOutput(t, createTable+createTrigger, wrapWithTransaction(createTrigger))
	assertApplyOutput(t, createTable+createTrigger, nothingModified)
}

func TestSQLite3defDropTrigger(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id integer NOT NULL PRIMARY KEY,
		  age integer NOT NULL
		);
		CREATE TABLE logs (
		  id integer NOT NULL PRIMARY KEY,
		  body TEXT NOT NULL,
		  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	createTrigger := stripHeredoc(`
		CREATE TRIGGER ` + "`users_insert`" + ` after insert ON ` + "`users`" + `
		BEGIN
			insert into logs(typ, typ_id, body) values ('user', NEW.id, 'inserted user');
		END;
	`)
	assertApplyOutput(t, createTable+createTrigger, wrapWithTransaction(createTable+createTrigger))
	assertApplyOutput(t, createTable+createTrigger, nothingModified)

	assertApplyOutput(t, createTable, wrapWithTransaction("-- Skipped: DROP TRIGGER `users_insert`;\n"))
	assertApplyOptionsOutput(t, createTable, wrapWithTransaction("DROP TRIGGER `users_insert`;\n"), "--enable-drop")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestSQLite3defChangeTrigger(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE IF NOT EXISTS users (
		  id integer NOT NULL PRIMARY KEY,
		  age integer NOT NULL
		);
		CREATE TABLE logs (
		  id integer NOT NULL PRIMARY KEY,
		  typ TEXT NOT NULL,
		  typ_id integer NOT NULL,
		  body TEXT NOT NULL,
		  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)
	createTrigger := stripHeredoc(`
		CREATE TRIGGER ` + "`users_insert`" + ` after insert ON ` + "`users`" + `
		BEGIN
			insert into logs(typ, typ_id, body) values ('user', NEW.id, 'inserted user');
		END;
	`)
	assertApplyOutput(t, createTable+createTrigger, wrapWithTransaction(createTable+createTrigger))
	assertApplyOutput(t, createTable+createTrigger, nothingModified)

	changeTrigger := stripHeredoc(`
		CREATE TRIGGER ` + "`users_insert`" + ` after insert ON ` + "`users`" + `
		BEGIN
			insert into logs(typ, typ_id, body) values ('user', NEW.id, 'user inserted');
		END;
	`)
	assertApplyOptionsOutput(t, createTable+changeTrigger, wrapWithTransaction("DROP TRIGGER `users_insert`;\n"+changeTrigger), "--enable-drop")
	assertApplyOutput(t, createTable+changeTrigger, nothingModified)
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
	mustSqlite3Exec("sqlite3def_test", stripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    username text NOT NULL
		);`,
	))

	// Define schema using deprecated @rename annotation
	schemaWithDeprecatedRename := stripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    user_name text NOT NULL -- @rename from=username
		);`,
	)

	writeFile("schema.sql", schemaWithDeprecatedRename)

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
	if !strings.Contains(out, "ALTER TABLE `users` RENAME COLUMN `username` TO `user_name`;") {
		t.Errorf("Expected rename operation not found in output:\n%s", out)
	}

	// Verify the table structure is correct after rename
	export := assertedExecute(t, "./sqlite3def", "sqlite3def_test", "--export")
	if !strings.Contains(export, "\"user_name\" text NOT NULL") && !strings.Contains(export, "user_name text NOT NULL") {
		t.Errorf("Column rename didn't work correctly. Export output:\n%s", export)
	}

	// Now test with @renamed (no warning expected)
	mustSqlite3Exec("sqlite3def_test", stripHeredoc(`
		DROP TABLE users;
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    old_name text NOT NULL
		);`,
	))

	schemaWithRenamed := stripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    new_name text NOT NULL -- @renamed from=old_name
		);`,
	)

	writeFile("schema.sql", schemaWithRenamed)
	out, err = tu.Execute("./sqlite3def", "sqlite3def_test", "--file", "schema.sql")
	if err != nil {
		t.Fatalf("sqlite3def execution failed: %s\nOutput: %s", err, out)
	}

	// Should NOT have warning for @renamed
	if strings.Contains(out, "-- WARNING: @rename is deprecated") {
		t.Errorf("Unexpected deprecation warning for @renamed in output:\n%s", out)
	}

	// Should still perform the rename
	if !strings.Contains(out, "ALTER TABLE `users` RENAME COLUMN `old_name` TO `new_name`;") {
		t.Errorf("Expected rename operation not found for @renamed annotation:\n%s", out)
	}
}

func TestMain(m *testing.M) {
	resetTestDatabase()
	tu.MustExecute("go", "build")
	status := m.Run()
	_ = os.Remove("sqlite3def")
	_ = os.Remove("sqlite3def_test")
	_ = os.Remove("schema.sql")
	_ = os.Remove("config.yml")
	os.Exit(status)
}

func assertApplyOutput(t *testing.T, schema string, expected string) {
	t.Helper()
	actual := assertApplyOutputWithConfig(t, schema, database.GeneratorConfig{EnableDrop: false})
	assertEquals(t, actual, expected)
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
	writeFile("schema.sql", schema)
	args := append([]string{
		"sqlite3def_test", "--file", "schema.sql",
	}, options...)

	actual := assertedExecute(t, "./sqlite3def", args...)
	assertEquals(t, actual, expected)
}

func assertedExecute(t *testing.T, command string, args ...string) string {
	t.Helper()
	out, err := tu.Execute(command, args...)
	if err != nil {
		t.Errorf("failed to execute '%s %s' (error: '%s'): `%s`", command, strings.Join(args, " "), err, out)
	}
	return out
}

func assertEquals(t *testing.T, actual string, expected string) {
	t.Helper()
	if expected != actual {
		t.Errorf("expected '%s' but got '%s'", expected, actual)
	}
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

func writeFile(path string, content string) {
	file, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	if _, err := file.Write(([]byte)(content)); err != nil {
		log.Fatal(err)
	}
}

func stripHeredoc(heredoc string) string {
	heredoc = strings.TrimPrefix(heredoc, "\n")
	re := regexp.MustCompilePOSIX("^\t*")
	return re.ReplaceAllLiteralString(heredoc, "")
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
	createTable := stripHeredoc(`
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
	writeFile(config1, "skip_tables: users") // Skip users
	writeFile(config2, "skip_tables: posts") // Skip posts
	defer os.Remove(config1)
	defer os.Remove(config2)

	// Test: file, inline, file - the last file should win
	// This tests that the order is preserved: config1, inline(comments), config2
	// Final result: posts should be skipped (from config2)
	out := assertedExecute(t, "./sqlite3def",
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
	out2 := assertedExecute(t, "./sqlite3def",
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
