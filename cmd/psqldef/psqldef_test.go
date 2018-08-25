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
		t.Errorf("failed to execute '%s %s' (out: '%s'): %s", command, strings.Join(args, " "), out, err)
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
	out, err := cmd.Output()
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
