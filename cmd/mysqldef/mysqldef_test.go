// Integration test of mysqldef command.
//
// Test requirement:
//   - go command
//   - `mysql -uroot` must succeed
package main

import (
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/sqldef/sqldef/v3/cmd/testutils"
	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/database/mysql"
	"github.com/sqldef/sqldef/v3/parser"
	"github.com/sqldef/sqldef/v3/schema"
)

const (
	nothingModified = "-- Nothing is modified --\n"
	applyPrefix     = "-- Apply --\n"
)

func wrapWithTransaction(ddls string) string {
	return applyPrefix + "BEGIN;\n" + ddls + "COMMIT;\n"
}

func getMySQLPort() int {
	if portStr := os.Getenv("MYSQL_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			return port
		}
	}
	return 3306
}

func getMySQLArgs(dbName ...string) []string {
	port := getMySQLPort()
	args := []string{"-uroot", "-h", "127.0.0.1"}

	if port != 3306 {
		args = append(args, "-P", strconv.Itoa(port))
	}

	if len(dbName) > 0 {
		args = append(args, dbName[0])
	}

	return args
}

// adjustDDLForFlavor adjusts DDL strings to match the expected output format for MariaDB vs MySQL
func adjustDDLForFlavor(ddl string) string {
	mysqlFlavor := os.Getenv("MYSQL_FLAVOR")
	if mysqlFlavor == "mariadb" {
		// MariaDB includes collation in DEFAULT CHARSET=latin1 statements
		ddl = strings.ReplaceAll(ddl, "DEFAULT CHARSET=latin1;", "DEFAULT CHARSET=latin1 COLLATE=latin1_swedish_ci;")
	}
	return ddl
}

// assertedExecuteMySQLDef executes mysqldef with proper connection arguments and database name
func assertedExecuteMySQLDef(t *testing.T, dbName string, extraArgs ...string) string {
	t.Helper()
	args := append(getMySQLArgs(dbName), extraArgs...)
	return assertedExecute(t, "./mysqldef", args...)
}

// executeMySQLDef executes mysqldef and returns output and error (for failure testing)
func executeMySQLDef(dbName string, extraArgs ...string) (string, error) {
	args := append(getMySQLArgs(dbName), extraArgs...)
	return testutils.Execute("./mysqldef", args...)
}

func TestApply(t *testing.T) {
	db, err := connectDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tests, err := testutils.ReadTests("tests*.yml")
	if err != nil {
		t.Fatal(err)
	}

	args := append(getMySQLArgs(), "-sN", "-e", "select version();")
	version := strings.TrimSpace(testutils.MustExecute("mysql", args...))
	sqlParser := database.NewParser(parser.ParserModeMysql)

	// Get MySQL flavor for test filtering
	mysqlFlavor := os.Getenv("MYSQL_FLAVOR")

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// Initialize the database with test.Current
			args := append(getMySQLArgs(), "-e", "DROP DATABASE IF EXISTS mysqldef_test; CREATE DATABASE mysqldef_test;")
			testutils.MustExecute("mysql", args...)

			testutils.RunTest(t, db, test, schema.GeneratorModeMysql, sqlParser, version, mysqlFlavor)
		})
	}
}

func TestMysqldefCreateTableSyntaxError(t *testing.T) {
	resetTestDatabase()
	assertApplyFailure(t, "CREATE TABLE users (id bigint,);", `found syntax error when parsing DDL "CREATE TABLE users (id bigint,)": syntax error at position 32`+"\n")
}

//
// ----------------------- following tests are for CLI -----------------------
//

func TestMysqldefFileComparison(t *testing.T) {
	resetTestDatabase()
	writeFile("schema.sql", stripHeredoc(`
		CREATE TABLE users (
		  name varchar(40),
		  created_at datetime NOT NULL
		);`,
	))

	output := assertedExecute(t, "./mysqldef", "--file", "schema.sql", "schema.sql")
	assertEquals(t, output, nothingModified)
}

func TestMysqldefApply(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE friends (
		  data bigint
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefDryRun(t *testing.T) {
	resetTestDatabase()
	writeFile("schema.sql", stripHeredoc(`
		CREATE TABLE users (
		  name varchar(40),
		  created_at datetime NOT NULL
		);`,
	))

	dryRun := assertedExecuteMySQLDef(t, "mysqldef_test", "--dry-run", "--file", "schema.sql")
	expectedDryRun := stripHeredoc(`
		-- dry run --
		BEGIN;
		CREATE TABLE users (
		  name varchar(40),
		  created_at datetime NOT NULL
		);
		COMMIT;
	`)
	assertEquals(t, dryRun, expectedDryRun)

	apply := assertedExecuteMySQLDef(t, "mysqldef_test", "--file", "schema.sql")
	expectedApply := stripHeredoc(`
		-- Apply --
		BEGIN;
		CREATE TABLE users (
		  name varchar(40),
		  created_at datetime NOT NULL
		);
		COMMIT;
	`)
	assertEquals(t, apply, expectedApply)
}

func TestMysqldefExport(t *testing.T) {
	resetTestDatabase()
	out := assertedExecuteMySQLDef(t, "mysqldef_test", "--export")
	assertEquals(t, out, "-- No table exists --\n")

	ddls := "CREATE TABLE `users` (\n" +
		"  `name` varchar(40) DEFAULT NULL,\n" +
		"  `updated_at` datetime NOT NULL\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=latin1;\n" +
		"\n" +
		"CREATE TRIGGER test AFTER INSERT ON users FOR EACH ROW UPDATE users SET updated_at = current_timestamp();\n"
	args := append(getMySQLArgs("mysqldef_test"), "-e", ddls)
	testutils.MustExecute("mysql", args...)
	out = assertedExecuteMySQLDef(t, "mysqldef_test", "--export")
	expectedDDLs := adjustDDLForFlavor(ddls)
	assertEquals(t, out, expectedDDLs)
}

func TestMysqldefExportConcurrently(t *testing.T) {
	resetTestDatabase()

	ddls := "CREATE TABLE `users_1` (\n" +
		"  `name` varchar(40) DEFAULT NULL\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=latin1;\n" +
		"\n" +
		"CREATE TABLE `users_2` (\n" +
		"  `name` varchar(40) DEFAULT NULL\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=latin1;\n" +
		"\n" +
		"CREATE TABLE `users_3` (\n" +
		"  `name` varchar(40) DEFAULT NULL\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=latin1;\n"
	args := append(getMySQLArgs("mysqldef_test"), "-e", ddls)
	testutils.MustExecute("mysql", args...)

	outputDefault := assertedExecuteMySQLDef(t, "mysqldef_test", "--export")

	writeFile("config.yml", "dump_concurrency: 0")
	outputNoConcurrency := assertedExecuteMySQLDef(t, "mysqldef_test", "--export", "--config", "config.yml")

	writeFile("config.yml", "dump_concurrency: 1")
	outputConcurrency1 := assertedExecuteMySQLDef(t, "mysqldef_test", "--export", "--config", "config.yml")

	writeFile("config.yml", "dump_concurrency: 10")
	outputConcurrency10 := assertedExecuteMySQLDef(t, "mysqldef_test", "--export", "--config", "config.yml")

	writeFile("config.yml", "dump_concurrency: -1")
	outputConcurrencyNoLimit := assertedExecuteMySQLDef(t, "mysqldef_test", "--export", "--config", "config.yml")

	expectedDDLs := adjustDDLForFlavor(ddls)
	assertEquals(t, outputDefault, expectedDDLs)
	assertEquals(t, outputNoConcurrency, outputDefault)
	assertEquals(t, outputConcurrency1, outputDefault)
	assertEquals(t, outputConcurrency10, outputDefault)
	assertEquals(t, outputConcurrencyNoLimit, outputDefault)
}

func TestMysqldefDropTable(t *testing.T) {
	resetTestDatabase()
	ddl := stripHeredoc(`
               CREATE TABLE users (
                 name varchar(40),
                 created_at datetime NOT NULL
               ) DEFAULT CHARSET=latin1;`)
	args := append(getMySQLArgs("mysqldef_test"), "-e", ddl)
	testutils.MustExecute("mysql", args...)

	writeFile("schema.sql", "")

	dropTable := "DROP TABLE `users`;\n"
	out := assertedExecuteMySQLDef(t, "mysqldef_test", "--enable-drop", "--file", "schema.sql")
	assertEquals(t, out, wrapWithTransaction(dropTable))
}

func TestMysqldefConfigInlineEnableDrop(t *testing.T) {
	resetTestDatabase()
	ddl := stripHeredoc(`
               CREATE TABLE users (
                 name varchar(40),
                 created_at datetime NOT NULL
               ) DEFAULT CHARSET=latin1;`)
	args := append(getMySQLArgs("mysqldef_test"), "-e", ddl)
	testutils.MustExecute("mysql", args...)

	writeFile("schema.sql", "")

	dropTable := "DROP TABLE `users`;\n"
	expectedOutput := wrapWithTransaction(dropTable)

	outFlag := assertedExecuteMySQLDef(t, "mysqldef_test", "--enable-drop", "--file", "schema.sql")
	assertEquals(t, outFlag, expectedOutput)

	testutils.MustExecute("mysql", append(getMySQLArgs("mysqldef_test"), "-e", ddl)...)

	outConfigInline := assertedExecuteMySQLDef(t, "mysqldef_test", "--config-inline", "enable_drop: true", "--file", "schema.sql")
	assertEquals(t, outConfigInline, expectedOutput)
}

func TestMysqldefSkipView(t *testing.T) {
	resetTestDatabase()

	createTable := "CREATE TABLE users (id bigint(20));\n"
	createView := "CREATE VIEW user_views AS SELECT id from users;\n"

	args := append(getMySQLArgs("mysqldef_test"), "-e", createTable+createView)
	testutils.MustExecute("mysql", args...)

	writeFile("schema.sql", createTable)

	output := assertedExecuteMySQLDef(t, "mysqldef_test", "--skip-view", "--file", "schema.sql")
	assertEquals(t, output, nothingModified)
}

func TestMysqldefBeforeApply(t *testing.T) {
	resetTestDatabase()

	beforeApply := "SET FOREIGN_KEY_CHECKS = 0;"
	createTable := stripHeredoc(`
	CREATE TABLE a (
		id int(11) NOT NULL AUTO_INCREMENT,
		b_id int(11) NOT NULL,
		PRIMARY KEY (id),
		CONSTRAINT a FOREIGN KEY (b_id) REFERENCES b (id)
	) ENGINE = InnoDB DEFAULT CHARSET = utf8;
	CREATE TABLE b (
		id int(11) NOT NULL AUTO_INCREMENT,
		a_id int(11) NOT NULL,
		PRIMARY KEY (id)
	) ENGINE = InnoDB DEFAULT CHARSET = utf8;`,
	)
	writeFile("schema.sql", createTable)
	apply := assertedExecuteMySQLDef(t, "mysqldef_test", "--file", "schema.sql", "--before-apply", beforeApply)
	// Tables should be sorted by dependencies, so 'b' comes before 'a'
	sortedCreateTable := stripHeredoc(`
	CREATE TABLE b (
		id int(11) NOT NULL AUTO_INCREMENT,
		a_id int(11) NOT NULL,
		PRIMARY KEY (id)
	) ENGINE = InnoDB DEFAULT CHARSET = utf8;
	CREATE TABLE a (
		id int(11) NOT NULL AUTO_INCREMENT,
		b_id int(11) NOT NULL,
		PRIMARY KEY (id),
		CONSTRAINT a FOREIGN KEY (b_id) REFERENCES b (id)
	) ENGINE = InnoDB DEFAULT CHARSET = utf8;`,
	)
	assertEquals(t, apply, applyPrefix+"BEGIN;\n"+beforeApply+"\n"+sortedCreateTable+"\nCOMMIT;\n")
	apply = assertedExecuteMySQLDef(t, "mysqldef_test", "--file", "schema.sql", "--before-apply", beforeApply)
	assertEquals(t, apply, nothingModified)
}

func TestMysqldefConfigIncludesTargetTables(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	args := append(getMySQLArgs("mysqldef_test"), "-e", usersTable+users1Table+users10Table)
	testutils.MustExecute("mysql", args...)

	writeFile("schema.sql", usersTable+users1Table)
	writeFile("config.yml", "target_tables: |\n  users\n  users_\\d\n")

	apply := assertedExecuteMySQLDef(t, "mysqldef_test", "--config", "config.yml", "--file", "schema.sql")
	assertEquals(t, apply, nothingModified)
}

func TestMysqldefConfigIncludesSkipTables(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	args := append(getMySQLArgs("mysqldef_test"), "-e", usersTable+users1Table+users10Table)
	testutils.MustExecute("mysql", args...)

	writeFile("schema.sql", usersTable+users1Table)
	writeFile("config.yml", "skip_tables: |\n  users_10\n")

	apply := assertedExecuteMySQLDef(t, "mysqldef_test", "--config", "config.yml", "--file", "schema.sql")
	assertEquals(t, apply, nothingModified)
}

func TestMysqldefConfigInlineSkipTables(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	args := append(getMySQLArgs("mysqldef_test"), "-e", usersTable+users1Table+users10Table)
	testutils.MustExecute("mysql", args...)

	writeFile("schema.sql", usersTable+users1Table)

	apply := assertedExecuteMySQLDef(t, "mysqldef_test", "--config-inline", "skip_tables: users_10", "--file", "schema.sql")
	assertEquals(t, apply, nothingModified)
}

func TestMysqldefConfigIncludesAlgorithm(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id int UNSIGNED NOT NULL,
		  name varchar(255) COLLATE utf8mb4_bin DEFAULT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int UNSIGNED NOT NULL,
		  name varchar(1000) COLLATE utf8mb4_bin DEFAULT NULL
		);
		`,
	)

	writeFile("schema.sql", createTable)
	writeFile("config.yml", "algorithm: |\n  inplace\n")

	apply := assertedExecuteMySQLDef(t, "mysqldef_test", "--config", "config.yml", "--file", "schema.sql")
	assertEquals(t, apply, wrapWithTransaction(stripHeredoc(`
	ALTER TABLE `+"`users`"+` CHANGE COLUMN `+"`name` `name`"+` varchar(1000) COLLATE utf8mb4_bin DEFAULT null, ALGORITHM=INPLACE;
	`,
	)))
}

func TestMysqldefConfigIncludesLock(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id int UNSIGNED NOT NULL,
		  name varchar(255) COLLATE utf8mb4_bin DEFAULT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int UNSIGNED NOT NULL,
		  name varchar(255) COLLATE utf8mb4_bin DEFAULT NULL,
		  new_column varchar(255) COLLATE utf8mb4_bin DEFAULT NULL
		);
		`,
	)

	writeFile("schema.sql", createTable)
	writeFile("config.yml", "lock: none")

	apply := assertedExecuteMySQLDef(t, "mysqldef_test", "--config", "config.yml", "--file", "schema.sql")
	assertEquals(t, apply, wrapWithTransaction(stripHeredoc(`
	ALTER TABLE `+"`users`"+` ADD COLUMN `+"`new_column` "+`varchar(255) COLLATE utf8mb4_bin DEFAULT null `+"AFTER `name`, "+`LOCK=NONE;
	`)))

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int UNSIGNED NOT NULL,
		  name varchar(255) COLLATE utf8mb4_bin DEFAULT NULL,
		  new_column varchar(1000) COLLATE utf8mb4_bin DEFAULT NULL
		);
		`,
	)

	writeFile("schema.sql", createTable)
	writeFile("config.yml", "algorithm: inplace\nlock: none")

	apply = assertedExecuteMySQLDef(t, "mysqldef_test", "--config", "config.yml", "--file", "schema.sql")
	assertEquals(t, apply, wrapWithTransaction(stripHeredoc(`
	ALTER TABLE `+"`users`"+` CHANGE COLUMN `+"`new_column` `new_column` "+`varchar(1000) COLLATE utf8mb4_bin DEFAULT null, ALGORITHM=INPLACE, LOCK=NONE;
	`)))

}

func TestMysqldefHelp(t *testing.T) {
	_, err := testutils.Execute("./mysqldef", "--help")
	if err != nil {
		t.Errorf("failed to run --help: %s", err)
	}

	out, err := testutils.Execute("./mysqldef")
	if err == nil {
		t.Errorf("no database must be error, but successfully got: %s", out)
	}
}

func TestMain(m *testing.M) {
	if _, ok := os.LookupEnv("MYSQL_HOST"); !ok {
		os.Setenv("MYSQL_HOST", "127.0.0.1")
	}

	resetTestDatabase()
	testutils.MustExecute("go", "build")
	status := m.Run()
	os.Remove("mysqldef")
	os.Remove("schema.sql")
	os.Remove("config.yml")
	os.Exit(status)
}

func assertApply(t *testing.T, schema string) {
	t.Helper()
	writeFile("schema.sql", schema)
	assertedExecuteMySQLDef(t, "mysqldef_test", "--file", "schema.sql")
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

	sqlParser := database.NewParser(parser.ParserModeMysql)
	output, err := testutils.ApplyWithOutput(db, schema.GeneratorModeMysql, sqlParser, desiredSchema, config)
	if err != nil {
		t.Fatal(err)
	}

	return output
}

func assertApplyOptionsOutput(t *testing.T, schema string, expected string, options ...string) {
	t.Helper()
	writeFile("schema.sql", schema)
	args := append([]string{
		"-uroot", "mysqldef_test", "--file", "schema.sql",
	}, options...)

	actual := assertedExecute(t, "./mysqldef", args...)
	assertEquals(t, actual, expected)
}

func assertApplyFailure(t *testing.T, schema string, expected string) {
	t.Helper()
	writeFile("schema.sql", schema)
	actual, err := executeMySQLDef("mysqldef_test", "--file", "schema.sql")
	if err == nil {
		t.Errorf("expected 'mysqldef -uroot mysqldef_test --file schema.sql' to fail but succeeded with: %s", actual)
	}
	assertEquals(t, actual, expected)
}

func assertedExecute(t *testing.T, command string, args ...string) string {
	t.Helper()
	out, err := testutils.Execute(command, args...)
	if err != nil {
		t.Errorf("failed to execute '%s %s' (error: '%s'): `%s`", command, strings.Join(args, " "), err, out)
	}
	return out
}

func assertEquals(t *testing.T, actual string, expected string) {
	t.Helper()
	if expected != actual {
		t.Errorf("expected `%s` but got `%s`", expected, actual)
	}
}

func resetTestDatabase() {
	// Drop database if it exists (don't specify database name in connection)
	args1 := append(getMySQLArgs(), "-e", "DROP DATABASE IF EXISTS mysqldef_test;")
	testutils.MustExecute("mysql", args1...)

	// Then recreate the database
	args2 := append(getMySQLArgs(), "-e", "CREATE DATABASE mysqldef_test;")
	testutils.MustExecute("mysql", args2...)
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
	return mysql.NewDatabase(database.Config{
		User:   "root",
		Host:   "127.0.0.1",
		Port:   getMySQLPort(),
		DbName: "mysqldef_test",
	})
}
