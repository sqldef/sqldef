// Integration test of mssqldef command.
//
// Test requirement:
//   - go command
//   - `sqlcmd -Usa -PPassw0rd` must succeed
package main

import (
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

func TestMssqldefColumnLiteral(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id integer NOT NULL,
		  name text,
		  age integer
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableQuotes(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE test_table (
		  id integer
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(
		"CREATE TABLE test_table (\n" +
			"  id integer\n" +
			");\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTable(t *testing.T) {
	resetTestDatabase()

	createTable1 := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name text,
		  age integer
		);
		`,
	)
	createTable2 := stripHeredoc(`
		CREATE TABLE bigdata (
		  data bigint
		);
		`,
	)

	assertApplyOutput(t, createTable1+createTable2, applyPrefix+createTable1+createTable2)
	assertApplyOutput(t, createTable1+createTable2, nothingModified)

	assertApplyOutput(t, createTable1, applyPrefix+"DROP TABLE [dbo].[bigdata];\n")
	assertApplyOutput(t, createTable1, nothingModified)
}

func TestMssqldefCreateTableWithDefault(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  profile varchar(50) NOT NULL DEFAULT '',
		  default_int int default 20,
		  default_bool bit default 1,
		  default_numeric numeric(5) default 42.195,
		  default_fixed_char varchar(3) default 'JPN',
		  default_text text default ''
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableWithIDENTITY(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id integer PRIMARY KEY IDENTITY(1,1),
		  name text,
		  age integer
		);
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateTableWithCLUSTERED(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id integer,
		  name text,
		  age integer,
			CONSTRAINT PK_users PRIMARY KEY CLUSTERED (id)
		);
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMssqldefCreateView(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE [dbo].[users] (
		  id integer NOT NULL,
		  name text,
		  age integer
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createView := stripHeredoc(`
		CREATE VIEW [dbo].[view_users] AS select id from dbo.users where age = 1;
		`,
	)
	assertApplyOutput(t, createTable+createView, applyPrefix+createView)
	assertApplyOutput(t, createTable+createView, nothingModified)

	createView = stripHeredoc(`
		CREATE VIEW [dbo].[view_users] AS select id from dbo.users where age = 2;
		`,
	)
	dropView := stripHeredoc(`
		DROP VIEW [dbo].[view_users];
		`,
	)
	assertApplyOutput(t, createTable+createView, applyPrefix+dropView+createView)
	assertApplyOutput(t, createTable+createView, nothingModified)

	assertApplyOutput(t, "", applyPrefix+"DROP TABLE [dbo].[users];\n"+dropView)
}

//
// ----------------------- following tests are for CLI -----------------------
//

func TestMssqldefDryRun(t *testing.T) {
	resetTestDatabase()
	writeFile("schema.sql", stripHeredoc(`
	    CREATE TABLE users (
	        id integer NOT NULL PRIMARY KEY,
	        age integer
	    );`,
	))

	dryRun := assertedExecute(t, "mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--dry-run", "--file", "schema.sql")
	apply := assertedExecute(t, "mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--file", "schema.sql")
	assertEquals(t, dryRun, strings.Replace(apply, "Apply", "dry run", 1))
}

func TestMssqldefSkipDrop(t *testing.T) {
	resetTestDatabase()
	mustExecute("sqlcmd", "-Usa", "-PPassw0rd", "-dmssqldef_test", "-Q", stripHeredoc(`
		CREATE TABLE users (
		    id integer NOT NULL PRIMARY KEY,
		    age integer
		);`,
	))

	writeFile("schema.sql", "")

	skipDrop := assertedExecute(t, "mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--skip-drop", "--file", "schema.sql")
	apply := assertedExecute(t, "mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--file", "schema.sql")
	assertEquals(t, skipDrop, strings.Replace(apply, "DROP", "-- Skipped: DROP", 1))
}

func TestMssqldefExport(t *testing.T) {
	resetTestDatabase()
	out := assertedExecute(t, "mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--export")
	assertEquals(t, out, "-- No table exists --\n")

	mustExecute("sqlcmd", "-Usa", "-PPassw0rd", "-dmssqldef_test", "-Q", stripHeredoc(`
		CREATE TABLE dbo.users (
		    id int NOT NULL,
		    age int
		);
		`,
	))
	out = assertedExecute(t, "mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--export")
	assertEquals(t, out, stripHeredoc(`
		CREATE TABLE dbo.users (
		    id int NOT NULL,
		    age int
		);
		`,
	))
}

func TestMssqldefHelp(t *testing.T) {
	_, err := execute("mssqldef", "--help")
	if err != nil {
		t.Errorf("failed to run --help: %s", err)
	}

	out, err := execute("mssqldef")
	if err == nil {
		t.Errorf("no database must be error, but successfully got: %s", out)
	}
}

func TestMain(m *testing.M) {
	resetTestDatabase()
	mustExecute("go", "build")
	status := m.Run()
	os.Exit(status)
}

func assertApply(t *testing.T, schema string) {
	t.Helper()
	writeFile("schema.sql", schema)
	assertedExecute(t, "mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--file", "schema.sql")
}

func assertApplyOutput(t *testing.T, schema string, expected string) {
	t.Helper()
	writeFile("schema.sql", schema)
	actual := assertedExecute(t, "mssqldef", "-Usa", "-PPassw0rd", "mssqldef_test", "--file", "schema.sql")
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
	mustExecute("sqlcmd", "-Usa", "-PPassw0rd", "-Q", "DROP DATABASE IF EXISTS mssqldef_test;")
	mustExecute("sqlcmd", "-Usa", "-PPassw0rd", "-Q", "CREATE DATABASE mssqldef_test;")
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
