// Integration test of mysqldef command.
//
// Test requirement:
//   - go command
//   - `mysql -uroot` must succeed
package main

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"

	tu "github.com/sqldef/sqldef/v3/cmd/testutils"
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

// executeMySQLDef executes mysqldef and returns output and error (for failure testing)
func executeMySQLDef(dbName string, extraArgs ...string) (string, error) {
	args := append(getMySQLArgs(dbName), extraArgs...)
	return tu.Execute("./mysqldef", args...)
}

// mustExecuteMySQLDef executes mysqldef with proper connection args and fails the test on error
func mustExecuteMySQLDef(t *testing.T, dbName string, extraArgs ...string) string {
	t.Helper()
	args := append(getMySQLArgs(dbName), extraArgs...)
	return tu.MustExecute(t, "./mysqldef", args...)
}

func TestApply(t *testing.T) {
	tests, err := tu.ReadTests("tests*.yml")
	if err != nil {
		t.Fatal(err)
	}

	version := mustGetMySQLVersion()
	sqlParser := database.NewParser(parser.ParserModeMysql)

	// Get MySQL flavor for test filtering
	mysqlFlavor := os.Getenv("MYSQL_FLAVOR")

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// Initialize the database with test.Current
			mustMysqlExec("", "DROP DATABASE IF EXISTS mysqldef_test")
			mustMysqlExec("", "CREATE DATABASE mysqldef_test")

			// Connect to the database after it's been recreated. This must be done
			// inside each subtest because the database is dropped and recreated for
			// each test, which would invalidate any connection created outside.
			db, err := connectDatabase()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()

			tu.RunTest(t, db, test, schema.GeneratorModeMysql, sqlParser, version, mysqlFlavor)
		})
	}
}

func TestMysqldefCreateTableSyntaxError(t *testing.T) {
	resetTestDatabase()
	assertApplyFailure(t,
		"CREATE TABLE users (id bigint,);",
		`found syntax error when parsing DDL "CREATE TABLE users (id bigint,)": syntax error at line 1, column 32
  CREATE TABLE users (id bigint,)
                                 ^
`)
}

//
// ----------------------- following tests are for CLI -----------------------
//

func TestMysqldefFileComparison(t *testing.T) {
	resetTestDatabase()
	tu.WriteFile("schema.sql", tu.StripHeredoc(`
		CREATE TABLE users (
		  name varchar(40),
		  created_at datetime NOT NULL
		);`,
	))

	output := tu.MustExecute(t, "./mysqldef", "--file", "schema.sql", "schema.sql")
	assert.Equal(t, nothingModified, output)
}

func TestMysqldefApply(t *testing.T) {
	resetTestDatabase()

	createTable := tu.StripHeredoc(`
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
	tu.WriteFile("schema.sql", tu.StripHeredoc(`
		CREATE TABLE users (
		  name varchar(40),
		  created_at datetime NOT NULL
		);`,
	))

	dryRun := mustExecuteMySQLDef(t, "mysqldef_test", "--dry-run", "--file", "schema.sql")
	expectedDryRun := tu.StripHeredoc(`
		-- dry run --
		BEGIN;
		CREATE TABLE users (
		  name varchar(40),
		  created_at datetime NOT NULL
		);
		COMMIT;
	`)
	assert.Equal(t, expectedDryRun, dryRun)

	apply := mustExecuteMySQLDef(t, "mysqldef_test", "--file", "schema.sql")
	expectedApply := tu.StripHeredoc(`
		-- Apply --
		BEGIN;
		CREATE TABLE users (
		  name varchar(40),
		  created_at datetime NOT NULL
		);
		COMMIT;
	`)
	assert.Equal(t, expectedApply, apply)
}

func TestMysqldefExport(t *testing.T) {
	resetTestDatabase()
	out := mustExecuteMySQLDef(t, "mysqldef_test", "--export")
	assert.Equal(t, "-- No table exists --\n", out)

	ddls := "CREATE TABLE `users` (\n" +
		"  `name` varchar(40) DEFAULT NULL,\n" +
		"  `updated_at` datetime NOT NULL\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=latin1;\n" +
		"\n" +
		"CREATE TRIGGER test AFTER INSERT ON users FOR EACH ROW UPDATE users SET updated_at = current_timestamp();\n"
	mustMysqlExec("mysqldef_test", ddls)
	out = mustExecuteMySQLDef(t, "mysqldef_test", "--export")
	expectedDDLs := adjustDDLForFlavor(ddls)
	assert.Equal(t, expectedDDLs, out)
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
	mustMysqlExec("mysqldef_test", ddls)

	outputDefault := mustExecuteMySQLDef(t, "mysqldef_test", "--export")

	tu.WriteFile("config.yml", "dump_concurrency: 0")
	outputNoConcurrency := mustExecuteMySQLDef(t, "mysqldef_test", "--export", "--config", "config.yml")

	tu.WriteFile("config.yml", "dump_concurrency: 1")
	outputConcurrency1 := mustExecuteMySQLDef(t, "mysqldef_test", "--export", "--config", "config.yml")

	tu.WriteFile("config.yml", "dump_concurrency: 10")
	outputConcurrency10 := mustExecuteMySQLDef(t, "mysqldef_test", "--export", "--config", "config.yml")

	tu.WriteFile("config.yml", "dump_concurrency: -1")
	outputConcurrencyNoLimit := mustExecuteMySQLDef(t, "mysqldef_test", "--export", "--config", "config.yml")

	expectedDDLs := adjustDDLForFlavor(ddls)
	assert.Equal(t, expectedDDLs, outputDefault)
	assert.Equal(t, outputDefault, outputNoConcurrency)
	assert.Equal(t, outputDefault, outputConcurrency1)
	assert.Equal(t, outputDefault, outputConcurrency10)
	assert.Equal(t, outputDefault, outputConcurrencyNoLimit)
}

func TestMysqldefDropTable(t *testing.T) {
	resetTestDatabase()
	ddl := tu.StripHeredoc(`
               CREATE TABLE users (
                 name varchar(40),
                 created_at datetime NOT NULL
               ) DEFAULT CHARSET=latin1;`)
	mustMysqlExec("mysqldef_test", ddl)

	tu.WriteFile("schema.sql", "")

	dropTable := "DROP TABLE `users`;\n"
	out := mustExecuteMySQLDef(t, "mysqldef_test", "--enable-drop", "--file", "schema.sql")
	assert.Equal(t, wrapWithTransaction(dropTable), out)
}

func TestMysqldefConfigInlineEnableDrop(t *testing.T) {
	resetTestDatabase()
	ddl := tu.StripHeredoc(`
               CREATE TABLE users (
                 name varchar(40),
                 created_at datetime NOT NULL
               ) DEFAULT CHARSET=latin1;`)
	mustMysqlExec("mysqldef_test", ddl)

	tu.WriteFile("schema.sql", "")

	dropTable := "DROP TABLE `users`;\n"
	expectedOutput := wrapWithTransaction(dropTable)

	outFlag := mustExecuteMySQLDef(t, "mysqldef_test", "--enable-drop", "--file", "schema.sql")
	assert.Equal(t, expectedOutput, outFlag)

	mustMysqlExec("mysqldef_test", ddl)

	outConfigInline := mustExecuteMySQLDef(t, "mysqldef_test", "--config-inline", "enable_drop: true", "--file", "schema.sql")
	assert.Equal(t, expectedOutput, outConfigInline)
}

func TestMysqldefSkipView(t *testing.T) {
	resetTestDatabase()

	createTable := "CREATE TABLE users (id bigint(20));\n"
	createView := "CREATE VIEW user_views AS SELECT id from users;\n"

	mustMysqlExec("mysqldef_test", createTable+createView)

	tu.WriteFile("schema.sql", createTable)

	output := mustExecuteMySQLDef(t, "mysqldef_test", "--skip-view", "--file", "schema.sql")
	assert.Equal(t, nothingModified, output)
}

func TestMysqldefBeforeApply(t *testing.T) {
	resetTestDatabase()

	beforeApply := "SET FOREIGN_KEY_CHECKS = 0;"
	createTable := tu.StripHeredoc(`
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
	tu.WriteFile("schema.sql", createTable)
	apply := mustExecuteMySQLDef(t, "mysqldef_test", "--file", "schema.sql", "--before-apply", beforeApply)
	// Tables should be sorted by dependencies, so 'b' comes before 'a'
	sortedCreateTable := tu.StripHeredoc(`
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
	assert.Equal(t, applyPrefix+"BEGIN;\n"+beforeApply+"\n"+sortedCreateTable+"\nCOMMIT;\n", apply)
	apply = mustExecuteMySQLDef(t, "mysqldef_test", "--file", "schema.sql", "--before-apply", beforeApply)
	assert.Equal(t, nothingModified, apply)
}

func TestMysqldefConfigIncludesTargetTables(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	mustMysqlExec("mysqldef_test", usersTable+users1Table+users10Table)

	tu.WriteFile("schema.sql", usersTable+users1Table)
	tu.WriteFile("config.yml", "target_tables: |\n  users\n  users_\\d\n")

	apply := mustExecuteMySQLDef(t, "mysqldef_test", "--config", "config.yml", "--file", "schema.sql")
	assert.Equal(t, nothingModified, apply)
}

func TestMysqldefConfigIncludesSkipTables(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	mustMysqlExec("mysqldef_test", usersTable+users1Table+users10Table)

	tu.WriteFile("schema.sql", usersTable+users1Table)
	tu.WriteFile("config.yml", "skip_tables: |\n  users_10\n")

	apply := mustExecuteMySQLDef(t, "mysqldef_test", "--config", "config.yml", "--file", "schema.sql")
	assert.Equal(t, nothingModified, apply)
}

func TestMysqldefConfigInlineSkipTables(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	mustMysqlExec("mysqldef_test", usersTable+users1Table+users10Table)

	tu.WriteFile("schema.sql", usersTable+users1Table)

	apply := mustExecuteMySQLDef(t, "mysqldef_test", "--config-inline", "skip_tables: users_10", "--file", "schema.sql")
	assert.Equal(t, nothingModified, apply)
}

func TestMysqldefConfigIncludesAlgorithm(t *testing.T) {
	resetTestDatabase()

	createTable := tu.StripHeredoc(`
		CREATE TABLE users (
		  id int UNSIGNED NOT NULL,
		  name varchar(255) COLLATE utf8mb4_bin DEFAULT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = tu.StripHeredoc(`
		CREATE TABLE users (
		  id int UNSIGNED NOT NULL,
		  name varchar(1000) COLLATE utf8mb4_bin DEFAULT NULL
		);
		`,
	)

	tu.WriteFile("schema.sql", createTable)
	tu.WriteFile("config.yml", "algorithm: |\n  inplace\n")

	apply := mustExecuteMySQLDef(t, "mysqldef_test", "--config", "config.yml", "--file", "schema.sql")
	assert.Equal(t, wrapWithTransaction(tu.StripHeredoc(`
	ALTER TABLE `+"`users`"+` CHANGE COLUMN `+"`name` `name`"+` varchar(1000) COLLATE utf8mb4_bin DEFAULT null, ALGORITHM=INPLACE;
	`,
	)), apply)
}

func TestMysqldefConfigIncludesLock(t *testing.T) {
	resetTestDatabase()

	createTable := tu.StripHeredoc(`
		CREATE TABLE users (
		  id int UNSIGNED NOT NULL,
		  name varchar(255) COLLATE utf8mb4_bin DEFAULT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, wrapWithTransaction(createTable))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = tu.StripHeredoc(`
		CREATE TABLE users (
		  id int UNSIGNED NOT NULL,
		  name varchar(255) COLLATE utf8mb4_bin DEFAULT NULL,
		  new_column varchar(255) COLLATE utf8mb4_bin DEFAULT NULL
		);
		`,
	)

	tu.WriteFile("schema.sql", createTable)
	tu.WriteFile("config.yml", "lock: none")

	apply := mustExecuteMySQLDef(t, "mysqldef_test", "--config", "config.yml", "--file", "schema.sql")
	assert.Equal(t, wrapWithTransaction(tu.StripHeredoc(`
	ALTER TABLE `+"`users`"+` ADD COLUMN `+"`new_column` "+`varchar(255) COLLATE utf8mb4_bin DEFAULT null `+"AFTER `name`, "+`LOCK=NONE;
	`)), apply)

	createTable = tu.StripHeredoc(`
		CREATE TABLE users (
		  id int UNSIGNED NOT NULL,
		  name varchar(255) COLLATE utf8mb4_bin DEFAULT NULL,
		  new_column varchar(1000) COLLATE utf8mb4_bin DEFAULT NULL
		);
		`,
	)

	tu.WriteFile("schema.sql", createTable)
	tu.WriteFile("config.yml", "algorithm: inplace\nlock: none")

	apply = mustExecuteMySQLDef(t, "mysqldef_test", "--config", "config.yml", "--file", "schema.sql")
	assert.Equal(t, wrapWithTransaction(tu.StripHeredoc(`
	ALTER TABLE `+"`users`"+` CHANGE COLUMN `+"`new_column` `new_column` "+`varchar(1000) COLLATE utf8mb4_bin DEFAULT null, ALGORITHM=INPLACE, LOCK=NONE;
	`)), apply)

}

func TestMysqldefHelp(t *testing.T) {
	_, err := tu.Execute("./mysqldef", "--help")
	if err != nil {
		t.Errorf("failed to run --help: %s", err)
	}

	out, err := tu.Execute("./mysqldef")
	if err == nil {
		t.Errorf("no database must be error, but successfully got: %s", out)
	}
}

// waitForMySQL waits for MySQL to be ready for connections with retry logic.
// MySQL initialization can take several seconds, especially in CI environments.
func waitForMySQL() {
	maxRetries := 30
	retryDelay := 500 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		if PingToMySQL() == nil {
			return
		}

		// Connection failed, wait and retry
		time.Sleep(retryDelay)
	}

	// If we get here, MySQL never became ready
	panic(fmt.Sprintf("MySQL did not become ready after %d retries", maxRetries))
}

func TestMain(m *testing.M) {
	if _, ok := os.LookupEnv("MYSQL_HOST"); !ok {
		os.Setenv("MYSQL_HOST", "127.0.0.1")
	}

	waitForMySQL()
	resetTestDatabase()
	tu.BuildForTest()
	status := m.Run()
	os.Remove("mysqldef")
	os.Remove("schema.sql")
	os.Remove("config.yml")
	os.Exit(status)
}

func assertApply(t *testing.T, schema string) {
	t.Helper()
	tu.WriteFile("schema.sql", schema)
	mustExecuteMySQLDef(t, "mysqldef_test", "--file", "schema.sql")
}

func assertApplyOutput(t *testing.T, schema string, expected string) {
	t.Helper()
	actual := assertApplyOutputWithConfig(t, schema, database.GeneratorConfig{EnableDrop: false})
	assert.Equal(t, expected, actual)
}

func assertApplyOutputWithConfig(t *testing.T, desiredSchema string, config database.GeneratorConfig) string {
	t.Helper()

	db, err := connectDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sqlParser := database.NewParser(parser.ParserModeMysql)
	output, err := tu.ApplyWithOutput(db, schema.GeneratorModeMysql, sqlParser, desiredSchema, config)
	if err != nil {
		t.Fatal(err)
	}

	return output
}

func assertApplyOptionsOutput(t *testing.T, schema string, expected string, options ...string) {
	t.Helper()
	tu.WriteFile("schema.sql", schema)
	args := append(getMySQLArgs("mysqldef_test"), "--file", "schema.sql")
	args = append(args, options...)

	actual := tu.MustExecute(t, "./mysqldef", args...)
	assert.Equal(t, expected, actual)
}

func assertApplyFailure(t *testing.T, schema string, expected string) {
	t.Helper()
	tu.WriteFile("schema.sql", schema)
	actual, err := executeMySQLDef("mysqldef_test", "--file", "schema.sql")
	if err == nil {
		t.Errorf("expected 'mysqldef -uroot mysqldef_test --file schema.sql' to fail but succeeded with: %s", actual)
	}
	assert.Equal(t, expected, actual)
}

func resetTestDatabase() {
	// Drop database if it exists (don't specify database name in connection)
	mustMysqlExec("", "DROP DATABASE IF EXISTS mysqldef_test")

	// Then recreate the database
	mustMysqlExec("", "CREATE DATABASE mysqldef_test")
}

func connectDatabase() (database.Database, error) {
	return mysql.NewDatabase(database.Config{
		User:   "root",
		Host:   "127.0.0.1",
		Port:   getMySQLPort(),
		DbName: "mysqldef_test",
	})
}

// mysqlQuery executes a query against the database and returns rows as string
func mysqlQuery(dbName string, query string) (string, error) {
	db, err := mysql.NewDatabase(database.Config{
		User:   "root",
		Host:   "127.0.0.1",
		Port:   getMySQLPort(),
		DbName: dbName,
	})
	if err != nil {
		return "", err
	}
	defer db.Close()

	return tu.QueryRows(db, query)
}

// mysqlExec executes a statement (or multiple statements) against the database
func mysqlExec(dbName string, statement string) error {
	// Build DSN with multiStatements=true to support executing multiple statements
	dsn := fmt.Sprintf("root@tcp(127.0.0.1:%d)/%s?multiStatements=true", getMySQLPort(), dbName)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(statement)
	return err
}

// mustMysqlExec executes a statement against the database and panics on error
func mustMysqlExec(dbName string, statement string) {
	if err := mysqlExec(dbName, statement); err != nil {
		panic(err)
	}
}

// PingToMySQL checks if MySQL is ready for connections
func PingToMySQL() error {
	dsn := fmt.Sprintf("root@tcp(127.0.0.1:%d)/", getMySQLPort())
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	return db.Ping()
}

// mustGetMySQLVersion retrieves the MySQL server version and panics on error
func mustGetMySQLVersion() string {
	// Connect without specifying a database since SELECT version() is a server-level function.
	// This avoids dependency on mysqldef_test database existing and reduces timing issues.
	db, err := mysql.NewDatabase(database.Config{
		User:   "root",
		Host:   "127.0.0.1",
		Port:   getMySQLPort(),
		DbName: "",
	})
	if err != nil {
		panic(err)
	}
	defer db.Close()

	var version string
	err = db.DB().QueryRow("SELECT version()").Scan(&version)
	if err != nil {
		panic(err)
	}

	return strings.TrimSpace(version)
}
