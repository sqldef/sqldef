// Integration test of mysqldef command.
//
// Test requirement:
//   - go command
//   - `mysql -uroot` must succeed
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
	nothingModified = "Nothing is modified\n"
)

func TestMysqldefCreateTable(t *testing.T) {
	resetTestDatabase()

	createTable1 := stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  name varchar(40) DEFAULT NULL,
		  created_at datetime NOT NULL
		);`,
	)
	createTable2 := stripHeredoc(`
		CREATE TABLE bigdata (
		  data bigint
		);`,
	)

	assertApplyOutput(t, createTable1+"\n"+createTable2, "Run: '"+createTable1+"'\n"+"Run: '"+createTable2+"'\n")
	assertApplyOutput(t, createTable1+"\n"+createTable2, nothingModified)

	assertApplyOutput(t, createTable1, "Run: 'DROP TABLE bigdata;'\n")
	assertApplyOutput(t, createTable1, nothingModified)
}

func TestMysqldefAddColumn(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  name varchar(40) DEFAULT NULL
		);`,
	)
	assertApplyOutput(t, createTable, "Run: '"+createTable+"'\n")
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  name varchar(40) DEFAULT NULL,
		  created_at datetime NOT NULL
		);`,
	)
	assertApplyOutput(t, createTable, "Run: 'ALTER TABLE users ADD COLUMN created_at datetime NOT NULL ;'\n")
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  created_at datetime NOT NULL
		);`,
	)
	assertApplyOutput(t, createTable, "Run: 'ALTER TABLE users DROP COLUMN name;'\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefAddIndex(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  name varchar(40) DEFAULT NULL,
		  created_at datetime NOT NULL
		);`,
	)
	assertApply(t, createTable)

	alterTable := "ALTER TABLE users ADD INDEX index_name(name);"
	assertApplyOutput(t, createTable+"\n"+alterTable, "Run: '"+alterTable+"'\n")
	assertApplyOutput(t, createTable+"\n"+alterTable, nothingModified)

	assertApplyOutput(t, createTable, "Run: 'ALTER TABLE users DROP INDEX index_name;'\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefCreateTableKey(t *testing.T) {
	t.Skip() // Nothing is modified, for now.
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  name varchar(40) DEFAULT NULL,
		  created_at datetime NOT NULL
		);`,
	)
	assertApply(t, createTable)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  name varchar(40) DEFAULT NULL,
		  created_at datetime NOT NULL,
		  KEY index_name(name)
		);`,
	)
	assertApplyOutput(t, createTable, "Run: 'ALTER TABLE users ADD INDEX index_name(name);'\n")
}

func TestMysqldefCreateTableSyntaxError(t *testing.T) {
	assertApplyFailure(t, "CREATE TABLE users (id bigint,);", `found syntax error when parsing DDL "CREATE TABLE users (id bigint,)": syntax error at position 32`+"\n")
}

//
// ----------------------- following tests are for CLI -----------------------
//

func TestMysqldefDryRun(t *testing.T) {
	resetTestDatabase()
	writeFile("schema.sql", stripHeredoc(`
		CREATE TABLE users (
		  name varchar(40),
		  created_at datetime NOT NULL
		);`,
	))

	dryRun := assertedExecute(t, "mysqldef", "-uroot", "mysqldef_test", "--dry-run", "--file", "schema.sql")
	apply := assertedExecute(t, "mysqldef", "-uroot", "mysqldef_test", "--file", "schema.sql")
	assertEquals(t, dryRun, "--- dry run ---\n"+apply)
}

func TestMysqldefExport(t *testing.T) {
	resetTestDatabase()
	out := assertedExecute(t, "mysqldef", "-uroot", "mysqldef_test", "--export")
	assertEquals(t, out, "-- No table exists\n")

	mustExecute("mysql", "-uroot", "mysqldef_test", "-e", stripHeredoc(`
		CREATE TABLE users (
		  name varchar(40),
		  created_at datetime NOT NULL
		);`,
	))
	out = assertedExecute(t, "mysqldef", "-uroot", "mysqldef_test", "--export")
	assertEquals(t, out,
		"CREATE TABLE `users` (\n"+
			"  `name` varchar(40) DEFAULT NULL,\n"+
			"  `created_at` datetime NOT NULL\n"+
			") ENGINE=InnoDB DEFAULT CHARSET=latin1;\n",
	)
}

func TestMysqldefHelp(t *testing.T) {
	_, err := execute("mysqldef", "--help")
	if err != nil {
		t.Errorf("failed to run --help: %s", err)
	}

	out, err := execute("mysqldef")
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
	writeFile("schema.sql", schema)
	assertedExecute(t, "mysqldef", "-uroot", "mysqldef_test", "--file", "schema.sql")
}

func assertApplyOutput(t *testing.T, schema string, expected string) {
	writeFile("schema.sql", schema)
	actual := assertedExecute(t, "mysqldef", "-uroot", "mysqldef_test", "--file", "schema.sql")
	assertEquals(t, actual, expected)
}

func assertApplyFailure(t *testing.T, schema string, expected string) {
	writeFile("schema.sql", schema)
	actual, err := execute("mysqldef", "-uroot", "mysqldef_test", "--file", "schema.sql")
	if err == nil {
		t.Errorf("expected 'mysqldef -uroot mysqldef_test --file schema.sql' to fail but succeeded with: %s", actual)
	}
	assertEquals(t, actual, expected)
}

func mustExecute(command string, args ...string) string {
	out, err := execute(command, args...)
	if err != nil {
		log.Printf("failed to execute '%s %s': `%s`", command, strings.Join(args, " "), out)
		log.Fatal(err)
	}
	return out
}

func assertedExecute(t *testing.T, command string, args ...string) string {
	out, err := execute(command, args...)
	if err != nil {
		t.Errorf("failed to execute '%s %s' (error: '%s'): `%s`", command, strings.Join(args, " "), err, out)
	}
	return out
}

func assertEquals(t *testing.T, actual string, expected string) {
	if expected != actual {
		t.Errorf("expected `%s` but got `%s`", expected, actual)
	}
}

func execute(command string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func resetTestDatabase() {
	mustExecute("mysql", "-uroot", "-e", "DROP DATABASE IF EXISTS mysqldef_test;")
	mustExecute("mysql", "-uroot", "-e", "CREATE DATABASE mysqldef_test;")
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
