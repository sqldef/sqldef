// Integration test of psqldef command.
//
// Test requirement:
//   - go command
//   - `psql -Upostgres` must succeed
package main

import (
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPsqldefCreateTable(t *testing.T) {
	resetTestDatabase()

	createTable1 := "CREATE TABLE users (\n" +
		"  id bigint NOT NULL,\n" +
		"  name text,\n" +
		"  age integer\n" +
		");"
	createTable2 := "CREATE TABLE bigdata (\n" +
		"  data bigint\n" +
		");"

	writeFile("schema.sql", createTable1+"\n"+createTable2)
	result := assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--file", "schema.sql")
	assertEquals(t, result, "Run: '"+createTable1+"'\n"+"Run: '"+createTable2+"'\n")

	writeFile("schema.sql", createTable1)
	result = assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--file", "schema.sql")
	assertEquals(t, result, "Run: 'DROP TABLE bigdata;'\n")
}

func TestPsqldefAddColumn(t *testing.T) {
	resetTestDatabase()

	createTable := "CREATE TABLE users (\n" +
		"  id bigint NOT NULL,\n" +
		"  name text\n" +
		");"
	writeFile("schema.sql", createTable)
	result := assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--file", "schema.sql")
	assertEquals(t, result, "Run: '"+createTable+"'\n")

	createTable = "CREATE TABLE users (\n" +
		"  id bigint NOT NULL,\n" +
		"  name text,\n" +
		"  age integer\n" +
		");"
	writeFile("schema.sql", createTable)
	result = assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--file", "schema.sql")
	assertEquals(t, result, "Run: 'ALTER TABLE users ADD COLUMN age integer ;'\n")

	createTable = "CREATE TABLE users (\n" +
		"  id bigint NOT NULL,\n" +
		"  age integer\n" +
		");"
	writeFile("schema.sql", createTable)
	result = assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--file", "schema.sql")
	assertEquals(t, result, "Run: 'ALTER TABLE users DROP COLUMN name;'\n")
}

func TestPsqldefDryRun(t *testing.T) {
	resetTestDatabase()
	writeFile("schema.sql", `
	    CREATE TABLE users (
	        id bigint NOT NULL PRIMARY KEY,
	        age int
	    );
	`)

	dryRun := assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--dry-run", "--file", "schema.sql")
	apply := assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--file", "schema.sql")
	assertEquals(t, dryRun, "--- dry run ---\n"+apply)
}

func TestPsqldefExport(t *testing.T) {
	resetTestDatabase()
	out := assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--export")
	assertEquals(t, out, "-- No table exists\n")

	mustExecute("psql", "-Upostgres", "psqldef_test", "-c", `
	    CREATE TABLE users (
	        id bigint NOT NULL PRIMARY KEY,
	        age int
	    );
	`)
	out = assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--export")
	assertEquals(t, strings.Replace(out, "public.users", "users", 1), // workaround: local has `public.` but travis doesn't.
		"CREATE TABLE users (\n"+
			"    id bigint NOT NULL,\n"+
			"    age integer\n"+
			");\n",
	)
}

func TestPsqldefHelp(t *testing.T) {
	_, err := execute("psqldef", "--help")
	if err != nil {
		t.Errorf("failed to run --help: %s", err)
	}

	out, err := execute("psqldef")
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

func mustExecute(command string, args ...string) {
	out, err := execute(command, args...)
	if err != nil {
		log.Printf("command: '%s %s'", command, strings.Join(args, " "))
		log.Printf("out: '%s'", out)
		log.Fatal(err)
	}
}

func assertedExecute(t *testing.T, command string, args ...string) string {
	out, err := execute(command, args...)
	if err != nil {
		t.Errorf("failed to execute '%s %s' (error: '%s'): %s", command, strings.Join(args, " "), err, out)
	}
	return out
}

func assertEquals(t *testing.T, actual string, expected string) {
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
	mustExecute("psql", "-Upostgres", "-c", "DROP DATABASE IF EXISTS psqldef_test;")
	mustExecute("psql", "-Upostgres", "-c", "CREATE DATABASE psqldef_test;")
}

func writeFile(path string, content string) {
	file, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	file.Write(([]byte)(content))
}
