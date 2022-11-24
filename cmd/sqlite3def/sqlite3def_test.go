// Integration test of sqlite3def command.
//
// Test requirement:
//   - go command
//   - `sqlite3` must succeed
package main

import (
	"github.com/k0kubun/sqldef"
	"github.com/k0kubun/sqldef/cmd/testutils"
	"github.com/k0kubun/sqldef/database"
	"github.com/k0kubun/sqldef/database/sqlite3"
	"github.com/k0kubun/sqldef/parser"
	"github.com/k0kubun/sqldef/schema"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

const (
	applyPrefix     = "-- Apply --\n"
	nothingModified = "-- Nothing is modified --\n"
)

func TestApply(t *testing.T) {
	defer testutils.MustExecute("rm", "-f", "sqlite3def_test") // after-test cleanup

	tests, err := testutils.ReadTests("tests.yml")
	if err != nil {
		t.Fatal(err)
	}

	sqlParser := database.NewParser(parser.ParserModeSQLite3)
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// Initialize the database with test.Current
			testutils.MustExecute("rm", "-f", "sqlite3def_test")
			db, err := connectDatabase() // re-connection seems needed after rm
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			testutils.RunTest(t, db, test, schema.GeneratorModeSQLite3, sqlParser, "")
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

	assertApplyOutput(t, createTable, applyPrefix+createTable)
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

func TestSQLite3defSkipDrop(t *testing.T) {
	resetTestDatabase()
	mustExecute("sqlite3", "sqlite3def_test", stripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    age integer
		);`,
	))

	writeFile("schema.sql", "")

	skipDrop := assertedExecute(t, "./sqlite3def", "sqlite3def_test", "--skip-drop", "--file", "schema.sql")
	apply := assertedExecute(t, "./sqlite3def", "sqlite3def_test", "--file", "schema.sql")
	assertEquals(t, skipDrop, strings.Replace(apply, "DROP", "-- Skipped: DROP", 1))
}

func TestSQLite3defExport(t *testing.T) {
	resetTestDatabase()
	out := assertedExecute(t, "./sqlite3def", "sqlite3def_test", "--export")
	assertEquals(t, out, "-- No table exists --\n")

	mustExecute("sqlite3", "sqlite3def_test", stripHeredoc(`
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
	mustExecute("sqlite3", "sqlite3def_test", usersTable+users1Table+users10Table)

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
	mustExecute("sqlite3", "sqlite3def_test", usersTable+users1Table+users10Table)

	writeFile("schema.sql", usersTable+users1Table)
	writeFile("config.yml", "skip_tables: |\n  users_10\n")

	apply := assertedExecute(t, "./sqlite3def", "--config", "config.yml", "--file", "schema.sql", "sqlite3def_test")
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

	writeFile("schema.sql", createTableFtsA+createTableFtsB+createTableRtreeA)
	// FTS is not available in modernc.org/sqlite v1.19.4 package
	writeFile("config.yml", stripHeredoc(`
		skip_tables: |
		  fts_a
		  fts_a_\w+
		  fts_b
		  fts_b_\w+
		  rtree_a_\w+
	`))
	actual := assertedExecute(t, "./sqlite3def", "--config", "config.yml", "--file", "schema.sql", "sqlite3def_test")
	assertEquals(t, actual, applyPrefix+createTableRtreeA)
	actual = assertedExecute(t, "./sqlite3def", "--config", "config.yml", "--file", "schema.sql", "sqlite3def_test")
	assertEquals(t, actual, nothingModified)
}

func TestSQLite3defFileSystem(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE bigdata (
		  data integer
		);
		`,
	)
	writeFile("schema.sql", createTable)

	createTable2 := stripHeredoc(`
		CREATE TABLE bigdata2 (
		  data integer
		);
		`,
	)

	db, err := connectDatabase()
	if err != nil {
		t.Fatal(err)
	}
	sqldef.Run(
		schema.GeneratorModeSQLite3,
		db,
		database.NewParser(parser.ParserModeSQLite3),
		&sqldef.Options{
			DesiredFile: "schema.sql",
			DesiredDDLs: createTable2,
		},
	)
	assertApplyOutput(t, createTable+createTable2, nothingModified)
}

func TestSQLite3defHelp(t *testing.T) {
	_, err := execute("./sqlite3def", "--help")
	if err != nil {
		t.Errorf("failed to run --help: %s", err)
	}

	out, err := execute("./sqlite3def")
	if err == nil {
		t.Errorf("no database must be error, but successfully got: %s", out)
	}
}

func TestMain(m *testing.M) {
	resetTestDatabase()
	mustExecute("go", "build")
	status := m.Run()
	_ = os.Remove("sqlite3def")
	_ = os.Remove("sqlite3def_test")
	_ = os.Remove("schema.sql")
	_ = os.Remove("config.yml")
	os.Exit(status)
}

func assertApplyOutput(t *testing.T, schema string, expected string) {
	t.Helper()
	writeFile("schema.sql", schema)
	actual := assertedExecute(t, "./sqlite3def", "sqlite3def_test", "--file", "schema.sql")
	assertEquals(t, actual, expected)
}

func mustExecute(command string, args ...string) {
	out, err := execute(command, args...)
	if err != nil {
		log.Printf("failed to execute '%s %s': `%s`", command, strings.Join(args, " "), out)
		log.Fatal(err)
	}
}

func assertedExecute(t *testing.T, command string, args ...string) string {
	t.Helper()
	out, err := execute(command, args...)
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

func execute(command string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
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

	file.Write(([]byte)(content))
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
