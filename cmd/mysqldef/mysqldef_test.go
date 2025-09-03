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
	"strings"
	"testing"

	"github.com/sqldef/sqldef/v2/cmd/testutils"
	"github.com/sqldef/sqldef/v2/database"
	"github.com/sqldef/sqldef/v2/database/mysql"
	"github.com/sqldef/sqldef/v2/parser"
	"github.com/sqldef/sqldef/v2/schema"
)

const (
	nothingModified = "-- Nothing is modified --\n"
	applyPrefix     = "-- Apply --\n"
)

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

	version := strings.TrimSpace(testutils.MustExecute("mysql", "-uroot", "-h", "127.0.0.1", "-sN", "-e", "select version();"))
	sqlParser := database.NewParser(parser.ParserModeMysql)
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// Initialize the database with test.Current
			testutils.MustExecute("mysql", "-uroot", "-h", "127.0.0.1", "-e", "DROP DATABASE IF EXISTS mysqldef_test; CREATE DATABASE mysqldef_test;")

			testutils.RunTest(t, db, test, schema.GeneratorModeMysql, sqlParser, version)
		})
	}
}

// TODO: non-CLI tests should be migrated to TestApply

func TestMysqldefCreateTableSyntaxError(t *testing.T) {
	resetTestDatabase()
	assertApplyFailure(t, "CREATE TABLE users (id bigint,);", `found syntax error when parsing DDL "CREATE TABLE users (id bigint,)": syntax error at position 32`+"\n")
}


func TestMysqldefColumnLiteral(t *testing.T) {
	resetTestDatabase()

	createTable := "CREATE TABLE users (\n" +
		"  `id` bigint NOT NULL,\n" +
		"  `name` text\n" +
		"  );\n"
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefHyphenNames(t *testing.T) {
	resetTestDatabase()

	createTable := "CREATE TABLE `foo-bar_baz` (\n" +
		"  `id-bar_baz` bigint NOT NULL\n" +
		");\n"
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefKeywordIndexColumns(t *testing.T) {
	resetTestDatabase()

	createTable := "CREATE TABLE tools (\n" +
		"  `character` varchar(255) COLLATE utf8mb4_bin DEFAULT NULL\n" +
		");\n"
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = "CREATE TABLE tools (\n" +
		"  `character` varchar(255) COLLATE utf8mb4_bin DEFAULT NULL,\n" +
		"  KEY `index_character`(`character`)\n" +
		");\n"
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `tools` ADD KEY `index_character` (`character`);\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefMysqlDoubleDashComment(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users(
		  id bigint NOT NULL
		);
		`,
	)
	createTableWithComments := "-- comment 1\n" + createTable + "-- comment 2\n"
	assertApplyOutput(t, createTableWithComments, applyPrefix+createTable)
	assertApplyOutput(t, createTableWithComments, nothingModified)
}









func TestMysqldefView(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint(20) NOT NULL
		);
		CREATE TABLE posts (
		  id bigint(20) NOT NULL,
		  user_id bigint(20) NOT NULL,
		  is_deleted tinyint(1)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createView := stripHeredoc(`
		CREATE VIEW foo AS select u.id as id, p.id as post_id from  (mysqldef_test.users as u join mysqldef_test.posts as p on ((u.id = p.user_id)));
		`,
	)
	assertApplyOutput(t, createTable+createView, applyPrefix+createView)
	assertApplyOutput(t, createTable+createView, nothingModified)

	createView = stripHeredoc(`
		CREATE VIEW foo AS select u.id as id, p.id as post_id from (mysqldef_test.users as u join mysqldef_test.posts as p on (((u.id = p.user_id) and (p.is_deleted = 0))));
		`,
	)
	expected := stripHeredoc(`
		CREATE OR REPLACE VIEW ` + "`foo`" + ` AS select u.id as id, p.id as post_id from (mysqldef_test.users as u join mysqldef_test.posts as p on (((u.id = p.user_id) and (p.is_deleted = 0))));
		`,
	)
	assertApplyOutput(t, createTable+createView, applyPrefix+expected)
	assertApplyOutput(t, createTable+createView, nothingModified)

	assertApplyOutput(t, "", applyPrefix+"-- Skipped: DROP TABLE `posts`;\n-- Skipped: DROP TABLE `users`;\n-- Skipped: DROP VIEW `foo`;\n")
}

func TestMysqldefTriggerInsert(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name text
		);
		CREATE TABLE logs (
		  id bigint NOT NULL,
		  log varchar(20),
		  dt datetime
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTrigger := "CREATE TRIGGER `insert_log` after insert ON `users` FOR EACH ROW insert into log(log, dt) values ('insert', now());\n"
	assertApplyOutput(t, createTable+createTrigger, applyPrefix+createTrigger)
	assertApplyOutput(t, createTable+createTrigger, nothingModified)

	createTrigger = "CREATE TRIGGER `insert_log` after insert ON `users` FOR EACH ROW insert into log(log, dt) values ('insert_users', now());\n"
	assertApplyOptionsOutput(t, createTable+createTrigger, applyPrefix+
		"DROP TRIGGER `insert_log`;\n"+
		"CREATE TRIGGER `insert_log` after insert ON `users` FOR EACH ROW insert into log(log, dt) values ('insert_users', now());\n", "--enable-drop")
	assertApplyOutput(t, createTable+createTrigger, nothingModified)

	createTriggerForBeforeUpdate := "CREATE TRIGGER `insert_log_before_update` before update ON `users` FOR EACH ROW insert into log(log, dt) values ('insert', now());\n"
	assertApplyOutput(t, createTable+createTriggerForBeforeUpdate, applyPrefix+createTriggerForBeforeUpdate)
	assertApplyOutput(t, createTable+createTriggerForBeforeUpdate, nothingModified)

	createTriggerForBeforeUpdate = "CREATE TRIGGER `insert_log_before_update` before update ON `users` FOR EACH ROW insert into log(log, dt) values ('insert_users', now());\n"
	assertApplyOptionsOutput(t, createTable+createTriggerForBeforeUpdate, applyPrefix+
		"DROP TRIGGER `insert_log_before_update`;\n"+
		"CREATE TRIGGER `insert_log_before_update` before update ON `users` FOR EACH ROW insert into log(log, dt) values ('insert_users', now());\n", "--enable-drop")
	assertApplyOutput(t, createTable+createTriggerForBeforeUpdate, nothingModified)
}

func TestMysqldefTriggerSetNew(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id int unsigned NOT NULL AUTO_INCREMENT,
		  name varchar(255) NOT NULL,
		  deleted_at timestamp NULL DEFAULT NULL,
		  logical_uniqueness tinyint(1) DEFAULT '1',
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTrigger := "CREATE TRIGGER `set_logical_uniqueness_on_users` " +
		"before update ON `users` FOR EACH ROW set NEW.logical_uniqueness = 1;\n"
	assertApplyOutput(t, createTable+createTrigger, applyPrefix+createTrigger)
	assertApplyOutput(t, createTable+createTrigger, nothingModified)

	createTrigger = "CREATE TRIGGER `set_logical_uniqueness_on_users2` before update ON `users` FOR EACH ROW set NEW.logical_uniqueness = case when NEW.deleted_at is null then 1 when NEW.deleted_at is not null then null end;\n"
	assertApplyOutput(t, createTable+createTrigger, applyPrefix+createTrigger)
	assertApplyOutput(t, createTable+createTrigger, nothingModified)
}

func TestMysqldefTriggerBeginEnd(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE test_trigger (
		  id int(11) NOT NULL AUTO_INCREMENT,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTrigger := "CREATE TRIGGER `BEFORE_UPDATE_test_trigger` before insert ON `test_trigger` FOR EACH ROW begin\n" +
		"set NEW.id = NEW.id + 10;\n" +
		"end;\n"
	assertApplyOutput(t, createTable+createTrigger, applyPrefix+createTrigger)
	assertApplyOutput(t, createTable+createTrigger, nothingModified)
}

func TestMysqldefTriggerIf(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE test_trigger (
		  id int(11) NOT NULL AUTO_INCREMENT,
		  set_id int(11) NOT NULL,
		  sort_order int(11) NOT NULL,
		  PRIMARY KEY (id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8;
		CREATE TRIGGER ` + "`test_trigger_BEFORE_INSERT`" + ` before insert ON ` + "`test_trigger`" + ` FOR EACH ROW begin
		if NEW.sort_order is null or NEW.sort_order = 0 then
		set NEW.sort_order = (select COALESCE(MAX(sort_order) + 1, 1) from test_trigger where set_id = NEW.set_id);
		end if;
		end;
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
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
	assertApplyOutput(t, createTable, applyPrefix+createTable)
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

	dryRun := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--dry-run", "--file", "schema.sql")
	apply := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--file", "schema.sql")
	assertEquals(t, dryRun, strings.Replace(apply, "Apply", "dry run", 1))
}

func TestMysqldefExport(t *testing.T) {
	resetTestDatabase()
	out := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--export")
	assertEquals(t, out, "-- No table exists --\n")

	ddls := "CREATE TABLE `users` (\n" +
		"  `name` varchar(40) DEFAULT NULL,\n" +
		"  `updated_at` datetime NOT NULL\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=latin1;\n" +
		"\n" +
		"CREATE TRIGGER test AFTER INSERT ON users FOR EACH ROW UPDATE users SET updated_at = current_timestamp();\n"
	testutils.MustExecute("mysql", "-uroot", "mysqldef_test", "-e", ddls)
	out = assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--export")
	assertEquals(t, out, ddls)
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
	testutils.MustExecute("mysql", "-uroot", "mysqldef_test", "-e", ddls)

	outputDefault := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--export")

	writeFile("config.yml", "dump_concurrency: 0")
	outputNoConcurrency := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--export", "--config", "config.yml")

	writeFile("config.yml", "dump_concurrency: 1")
	outputConcurrency1 := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--export", "--config", "config.yml")

	writeFile("config.yml", "dump_concurrency: 10")
	outputConcurrency10 := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--export", "--config", "config.yml")

	writeFile("config.yml", "dump_concurrency: -1")
	outputConcurrencyNoLimit := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--export", "--config", "config.yml")

	assertEquals(t, outputDefault, ddls)
	assertEquals(t, outputNoConcurrency, outputDefault)
	assertEquals(t, outputConcurrency1, outputDefault)
	assertEquals(t, outputConcurrency10, outputDefault)
	assertEquals(t, outputConcurrencyNoLimit, outputDefault)
}

func TestMysqldefDropTable(t *testing.T) {
	resetTestDatabase()
	testutils.MustExecute("mysql", "-uroot", "mysqldef_test", "-e", stripHeredoc(`
               CREATE TABLE users (
                 name varchar(40),
                 created_at datetime NOT NULL
               ) DEFAULT CHARSET=latin1;`,
	))

	writeFile("schema.sql", "")

	dropTable := "DROP TABLE `users`;\n"
	out := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--enable-drop", "--file", "schema.sql")
	assertEquals(t, out, applyPrefix+dropTable)
}

func TestMysqldefSkipView(t *testing.T) {
	resetTestDatabase()

	createTable := "CREATE TABLE users (id bigint(20));\n"
	createView := "CREATE VIEW user_views AS SELECT id from users;\n"

	testutils.MustExecute("mysql", "-uroot", "mysqldef_test", "-e", createTable+createView)

	writeFile("schema.sql", createTable)

	output := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--skip-view", "--file", "schema.sql")
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
	apply := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--file", "schema.sql", "--before-apply", beforeApply)
	assertEquals(t, apply, applyPrefix+beforeApply+"\n"+createTable+"\n")
	apply = assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--file", "schema.sql", "--before-apply", beforeApply)
	assertEquals(t, apply, nothingModified)
}

func TestMysqldefConfigIncludesTargetTables(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	testutils.MustExecute("mysql", "-uroot", "mysqldef_test", "-e", usersTable+users1Table+users10Table)

	writeFile("schema.sql", usersTable+users1Table)
	writeFile("config.yml", "target_tables: |\n  users\n  users_\\d\n")

	apply := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--config", "config.yml", "--file", "schema.sql")
	assertEquals(t, apply, nothingModified)
}

func TestMysqldefConfigIncludesSkipTables(t *testing.T) {
	resetTestDatabase()

	usersTable := "CREATE TABLE users (id bigint);"
	users1Table := "CREATE TABLE users_1 (id bigint);"
	users10Table := "CREATE TABLE users_10 (id bigint);"
	testutils.MustExecute("mysql", "-uroot", "mysqldef_test", "-e", usersTable+users1Table+users10Table)

	writeFile("schema.sql", usersTable+users1Table)
	writeFile("config.yml", "skip_tables: |\n  users_10\n")

	apply := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--config", "config.yml", "--file", "schema.sql")
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
	assertApplyOutput(t, createTable, applyPrefix+createTable)
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

	apply := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--config", "config.yml", "--file", "schema.sql")
	assertEquals(t, apply, applyPrefix+stripHeredoc(`
	ALTER TABLE `+"`users`"+` CHANGE COLUMN `+"`name` `name`"+` varchar(1000) COLLATE utf8mb4_bin DEFAULT null, ALGORITHM=INPLACE;
	`,
	))
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
	assertApplyOutput(t, createTable, applyPrefix+createTable)
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

	apply := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--config", "config.yml", "--file", "schema.sql")
	assertEquals(t, apply, applyPrefix+stripHeredoc(`
	ALTER TABLE `+"`users`"+` ADD COLUMN `+"`new_column` "+`varchar(255) COLLATE utf8mb4_bin DEFAULT null `+"AFTER `name`, "+`LOCK=NONE;
	`))

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

	apply = assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--config", "config.yml", "--file", "schema.sql")
	assertEquals(t, apply, applyPrefix+stripHeredoc(`
	ALTER TABLE `+"`users`"+` CHANGE COLUMN `+"`new_column` `new_column` "+`varchar(1000) COLLATE utf8mb4_bin DEFAULT null, ALGORITHM=INPLACE, LOCK=NONE;
	`))

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
	assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--file", "schema.sql")
}

func assertApplyOutput(t *testing.T, schema string, expected string) {
	t.Helper()
	writeFile("schema.sql", schema)
	actual := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--file", "schema.sql")
	assertEquals(t, actual, expected)
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
	actual, err := testutils.Execute("./mysqldef", "-uroot", "mysqldef_test", "--file", "schema.sql")
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
	testutils.MustExecute("mysql", "-uroot", "-e", "DROP DATABASE IF EXISTS mysqldef_test;")
	testutils.MustExecute("mysql", "-uroot", "-e", "CREATE DATABASE mysqldef_test;")
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
	return mysql.NewDatabase(database.Config{
		User:   "root",
		Host:   "127.0.0.1",
		Port:   3306,
		DbName: "mysqldef_test",
	})
}
