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
	applyPrefix     = "-- Apply --\n"
	nothingModified = "-- Nothing is modified --\n"
)

func TestPsqldefCreateTable(t *testing.T) {
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

	assertApplyOutput(t, createTable1, applyPrefix+"DROP TABLE \"bigdata\";\n")
	assertApplyOutput(t, createTable1, nothingModified)
}

func TestPsqldefCreateTablePrimaryKey(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL PRIMARY KEY,
		  name text
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  name text
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE users DROP CONSTRAINT users_pkey;\nALTER TABLE users DROP COLUMN id;\n")
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL PRIMARY KEY,
		  name text
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE users ADD COLUMN id bigint NOT NULL;\nALTER TABLE users ADD primary key (id);\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestCreateTableForeignKey(t *testing.T) {
	resetTestDatabase()

	createUsers := "CREATE TABLE users (id BIGINT PRIMARY KEY);\n"
	createPosts := stripHeredoc(`
			CREATE TABLE posts (
			  content text,
			  user_id bigint
			);
			`,
	)
	assertApplyOutput(t, createUsers+createPosts, applyPrefix+createUsers+createPosts)
	assertApplyOutput(t, createUsers+createPosts, nothingModified)

	createPosts = stripHeredoc(`
			CREATE TABLE posts (
			  content text,
			  user_id bigint,
			  CONSTRAINT posts_ibfk_1 FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL ON UPDATE RESTRICT
			);
			`,
	)
	assertApplyOutput(t, createUsers+createPosts, applyPrefix+"ALTER TABLE posts ADD CONSTRAINT posts_ibfk_1 FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL ON UPDATE RESTRICT;\n")
	assertApplyOutput(t, createUsers+createPosts, nothingModified)

	createPosts = stripHeredoc(`
			CREATE TABLE posts (
			  content text,
			  user_id bigint
			);
			`,
	)
	assertApplyOutput(t, createUsers+createPosts, applyPrefix+"ALTER TABLE posts DROP CONSTRAINT posts_ibfk_1;\n")
	assertApplyOutput(t, createUsers+createPosts, nothingModified)
}

func TestPsqldefDropPrimaryKey(t *testing.T) {
	t.Skip()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL PRIMARY KEY,
		  name text
		);`,
	)
	assertApply(t, createTable)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name text
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE users DROP PRIMARY KEY;\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefAddColumn(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name text
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name text,
		  age integer
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE users ADD COLUMN age integer;\n")
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  age integer
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE users DROP COLUMN name;\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefCreateIndex(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name text,
		  age integer
		);
		`,
	)
	createIndex1 := "CREATE INDEX index_name on users (name);\n"
	createIndex2 := "CREATE UNIQUE INDEX index_age on users (age);\n"
	assertApplyOutput(t, createTable+createIndex1+createIndex2, applyPrefix+createTable+createIndex1+createIndex2)
	assertApplyOutput(t, createTable+createIndex1+createIndex2, nothingModified)

	createIndex1 = "CREATE INDEX index_name on users (name, id);\n"
	assertApplyOutput(t, createTable+createIndex1+createIndex2, applyPrefix+"DROP INDEX index_name;\n"+createIndex1)
	assertApplyOutput(t, createTable+createIndex1+createIndex2, nothingModified)

	assertApplyOutput(t, createTable, applyPrefix+"DROP INDEX index_age;\nDROP INDEX index_name;\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefColumnLiteral(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  "id" bigint NOT NULL,
		  "name" text,
		  "age" integer
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestPsqldefDataTypes(t *testing.T) {
	resetTestDatabase()

	// TODO:
	//   - int8
	//   - bigserial
	//   - serial8
	//   - box
	//   - bytea
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
	//   - xml
	//
	// Remaining SQL spec: bit varying, double precision, interval, numeric, decimal, real,
	//   smallint, time(with and without tz), timestamp(with and without tz), xml
	createTable := stripHeredoc(`
		CREATE TABLE users (
		  c_bigint bigint,
		  c_bit bit,
		  c_bit_2 bit(2),
		  c_bool bool,
		  c_boolean boolean,
		  c_char_10 char(10),
		  c_character_20 character(20),
		  c_character_varying_30 character varying(30),
		  c_date date,
		  c_int int,
		  c_integer integer,
		  c_text text,
		  c_uuid uuid,
		  c_varchar_40 varchar(40)
		);
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+createTable)
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
	assertEquals(t, dryRun, strings.Replace(apply, "Apply", "dry run", 1))
}

func TestPsqldefExport(t *testing.T) {
	resetTestDatabase()
	out := assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--export")
	assertEquals(t, out, "-- No table exists --\n")

	mustExecute("psql", "-Upostgres", "psqldef_test", "-c", stripHeredoc(`
	    CREATE TABLE users (
	        id bigint NOT NULL PRIMARY KEY,
	        age int
	    );`,
	))
	out = assertedExecute(t, "psqldef", "-Upostgres", "psqldef_test", "--export")
	// workaround: local has `public.` but travis doesn't.
	assertEquals(t, strings.Replace(out, "public.users", "users", 2), stripHeredoc(`
		CREATE TABLE users (
		    id bigint NOT NULL,
		    age integer
		);
		ALTER TABLE ONLY users
		    ADD CONSTRAINT users_pkey PRIMARY KEY (id);
		`,
	))
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
