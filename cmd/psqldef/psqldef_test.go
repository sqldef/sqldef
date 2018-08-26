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
	"regexp"
	"strings"
	"testing"
)

const (
	nothingModified = "Nothing is modified\n"
)

func TestPsqldefCreateTable(t *testing.T) {
	resetTestDatabase()

	createTable1 := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name text,
		  age integer
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

func TestPsqldefAddColumn(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name text
		);`,
	)
	assertApplyOutput(t, createTable, "Run: '"+createTable+"'\n")
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name text,
		  age integer
		);`,
	)
	assertApplyOutput(t, createTable, "Run: 'ALTER TABLE users ADD COLUMN age integer;'\n")
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  age integer
		);`,
	)
	assertApplyOutput(t, createTable, "Run: 'ALTER TABLE users DROP COLUMN name;'\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefCreateIndex(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name text,
		  age integer
		);`,
	)
	createIndex := "CREATE INDEX index_name on users (name);"
	assertApplyOutput(t, createTable+"\n"+createIndex, "Run: '"+createTable+"'\nRun: '"+createIndex+"'\n")
	//assertApplyOutput(t, createTable+"\n"+createIndex, nothingModified)
}

func TestPsqldefDataTypes(t *testing.T) {
	resetTestDatabase()

	// TODO:
	//   - int8
	//   - bigserial
	//   - serial8
	//   - bool
	//   - boolean
	//   - box
	//   - bytea
	//   - char
	//   - character
	//   - cidr
	//   - circle
	//   - double precision
	//   - float8
	//   - inet
	//   - int4
	//   - interval [ fields ] [ (p) ]
	//   - json
	//   - jsonb
	//   - line
	//   - lseg
	//   - macaddr
	//   - money
	//   - numeric [ (p, s) ]
	//   - decimal [ (p, s) ]
	//   - path
	//   - pg_lsn
	//   - point
	//   - polygon
	//   - real
	//   - float4
	//   - smallint
	//   - int2
	//   - smallserial
	//   - serial2
	//   - serial
	//   - serial4
	//   - time [ (p) ] [ without time zone ]
	//   - time [ (p) ] with time zone
	//   - timetz
	//   - timestamp [ (p) ] [ without time zone ]
	//   - timestamp [ (p) ] with time zone
	//   - timestamptz
	//   - tsquer
	//   - tsvector
	//   - txid_snapshot
	//   - uuid
	//   - xml
	//
	// Remaining SQL spec: bit varying, boolean, char, character, double precision, interval, numeric, decimal, real,
	//   smallint, time(with and without tz), timestamp(with and without tz), xml
	createTable := stripHeredoc(`
		CREATE TABLE users (
		  c_bigint bigint,
		  c_bit bit,
		  c_bit_2 bit(2),
		  c_character_varying_40 character varying(40),
		  c_date date,
		  c_int int,
		  c_integer integer,
		  c_text text,
		  c_varchar_30 varchar(30)
		);`,
	)

	assertApplyOutput(t, createTable, "Run: '"+createTable+"'\n")
	assertApplyOutput(t, createTable, nothingModified) // Label for column type may change. Type will be examined.
}

//
// ----------------------- following tests are for CLI -----------------------
//

func TestPsqldefDryRun(t *testing.T) {
	resetTestDatabase()
	writeFile("schema.sql", stripHeredoc(`
	    CREATE TABLE users (
	        id bigint NOT NULL PRIMARY KEY,
	        age int
	    );`,
	))

	dryRun := assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--dry-run", "--file", "schema.sql")
	apply := assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--file", "schema.sql")
	assertEquals(t, dryRun, "--- dry run ---\n"+apply)
}

func TestPsqldefExport(t *testing.T) {
	resetTestDatabase()
	out := assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--export")
	assertEquals(t, out, "-- No table exists\n")

	mustExecute("psql", "-Upostgres", "psqldef_test", "-c", stripHeredoc(`
	    CREATE TABLE users (
	        id bigint NOT NULL PRIMARY KEY,
	        age int
	    );`,
	))
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

func assertApply(t *testing.T, schema string) {
	writeFile("schema.sql", schema)
	assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--file", "schema.sql")
}

func assertApplyOutput(t *testing.T, schema string, expected string) {
	writeFile("schema.sql", schema)
	actual := assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--file", "schema.sql")
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
	out, err := execute(command, args...)
	if err != nil {
		t.Errorf("failed to execute '%s %s' (error: '%s'): `%s`", command, strings.Join(args, " "), err, out)
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

func stripHeredoc(heredoc string) string {
	heredoc = strings.TrimPrefix(heredoc, "\n")
	re := regexp.MustCompilePOSIX("^\t*")
	return re.ReplaceAllLiteralString(heredoc, "")
}
