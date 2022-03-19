// Integration test of sqlite3def command.
//
// Test requirement:
//   - go command
//   - `sqlite3` must succeed
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/k0kubun/sqldef/adapter"
	"github.com/k0kubun/sqldef/adapter/sqlite3"
	"github.com/k0kubun/sqldef/cmd/testutils"
	"github.com/k0kubun/sqldef/schema"
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

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// Initialize the database with test.Current
			testutils.MustExecute("rm", "-f", "sqlite3def_test")
			db, err := connectDatabase() // re-connection seems needed after rm
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			testutils.RunTest(t, db, test, schema.GeneratorModeSQLite3, "")
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

func TestSQLite3defExportWithTargetFile(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(
		`CREATE TABLE users%d (
		    id integer NOT NULL
		);
		`)
	for i := 1; i <= 5; i++ {
		mustExecute("sqlite3", "sqlite3def_test", fmt.Sprintf(createTable, i))
	}

	writeFile("target-list", "users2\nusers4\nusers5\n")

	out := assertedExecute(t, "./sqlite3def", "sqlite3def_test", "--export", "--target-file", "target-list")
	assertEquals(t, out, fmt.Sprintf(createTable, 2)+"\n"+fmt.Sprintf(createTable, 4)+"\n"+fmt.Sprintf(createTable, 5))
}

func TestSQLite3defTargetFile(t *testing.T) {

	createTable := stripHeredoc(
		`CREATE TABLE users%d (
		    id integer NOT NULL
		);`)
	modifiedCreateTable := stripHeredoc(
		`CREATE TABLE users%d (
		    id integer NOT NULL,
		    name text
		);`)

	// Prepare the modified schema.sql
	resetTestDatabase()
	for i := 3; i <= 6; i++ {
		mustExecute("sqlite3", "sqlite3def_test", fmt.Sprintf(modifiedCreateTable, i))
	}
	out := assertedExecute(t, "./sqlite3def", "sqlite3def_test", "--export", "--file", "schema.sql")
	writeFile("schema.sql", out)

	// Run test
	resetTestDatabase()
	for i := 1; i <= 4; i++ {
		mustExecute("sqlite3", "sqlite3def_test", fmt.Sprintf(createTable, i))
	}

	writeFile("target-list", "users2\nusers4\nusers6\n")

	apply := assertedExecute(t, "./sqlite3def", "sqlite3def_test", "--target-file", "target-list", "--file", "schema.sql")
	assertEquals(t, apply,
		applyPrefix+
			"ALTER TABLE `users4` ADD COLUMN `name` text;\n"+
			fmt.Sprintf(modifiedCreateTable, 6)+"\n"+
			"DROP TABLE `users2`;\n")
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
	_ = os.Remove("target-list")
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

func connectDatabase() (adapter.Database, error) {
	return sqlite3.NewDatabase(adapter.Config{
		DbName: "sqlite3def_test",
	})
}
