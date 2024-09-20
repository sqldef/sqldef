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

	"github.com/sqldef/sqldef/cmd/testutils"
	"github.com/sqldef/sqldef/database"
	"github.com/sqldef/sqldef/database/mysql"
	"github.com/sqldef/sqldef/parser"
	"github.com/sqldef/sqldef/schema"
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

	tests, err := testutils.ReadTests("tests.yml")
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

func TestMysqldefCreateTableChangePrimaryKey(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE friends (
		  user_id bigint NOT NULL PRIMARY KEY,
		  friend_id bigint NOT NULL,
		  created_at datetime NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE friends (
		  user_id bigint NOT NULL,
		  friend_id bigint NOT NULL,
		  created_at datetime NOT NULL,
		  PRIMARY KEY (user_id, friend_id)
		);
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `friends` DROP PRIMARY KEY;\n"+
		"ALTER TABLE `friends` ADD PRIMARY KEY (`user_id`, `friend_id`);\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefCreateTableAddAutoIncrementPrimaryKey(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  name varchar(20)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL AUTO_INCREMENT,
		  name varchar(20),
		  PRIMARY KEY (id)
		);
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` ADD COLUMN `id` bigint NOT NULL FIRST;\n"+
		"ALTER TABLE `users` ADD PRIMARY KEY (`id`);\n"+
		"ALTER TABLE `users` CHANGE COLUMN `id` `id` bigint NOT NULL AUTO_INCREMENT;\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefCreateTableKeepAutoIncrement(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL AUTO_INCREMENT,
		  password char(128) COLLATE utf8mb4_bin DEFAULT NULL,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT NOT NULL AUTO_INCREMENT,
		  password char(128) COLLATE utf8mb4_bin DEFAULT NULL,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` CHANGE COLUMN `id` `id` bigint NOT NULL AUTO_INCREMENT;\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefCreateTableAddIndexWithKeyLength(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint(20) UNSIGNED NOT NULL AUTO_INCREMENT,
		  name TEXT NOT NULL,
		  PRIMARY KEY (id)
		);`,
	)
	assertApply(t, createTable)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint(20) UNSIGNED NOT NULL AUTO_INCREMENT,
		  name TEXT NOT NULL,
		  PRIMARY KEY (id),
		  INDEX index_name(name(255))
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` ADD INDEX `index_name` (`name`(255));\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefAddColumn(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  name varchar(40) DEFAULT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  name varchar(40) DEFAULT NULL,
		  created_at datetime NOT NULL
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` ADD COLUMN `created_at` datetime NOT NULL AFTER `name`;\n")
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  created_at datetime NOT NULL
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` DROP COLUMN `name`;\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefAddColumnAfter(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  created_at datetime NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  name varchar(40) NOT NULL,
		  created_at datetime NOT NULL
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` ADD COLUMN `name` varchar(40) NOT NULL AFTER `id`;\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefAddColumnWithNull(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint PRIMARY KEY,
		  name varchar(40) DEFAULT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint PRIMARY KEY,
		  name varchar(40) DEFAULT NULL,
		  created_at timestamp NULL DEFAULT NULL
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` ADD COLUMN `created_at` timestamp NULL DEFAULT null AFTER `name`;\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefChangeColumn(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id int UNSIGNED NOT NULL,
		  name varchar(40)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint UNSIGNED NOT NULL,
		  name char(40)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+stripHeredoc(`
		ALTER TABLE `+"`users`"+` CHANGE COLUMN `+"`id` `id`"+` bigint UNSIGNED NOT NULL;
		ALTER TABLE `+"`users`"+` CHANGE COLUMN `+"`name` `name`"+` char(40);
		`,
	))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefChangeColumnLength(t *testing.T) {
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
	assertApplyOutput(t, createTable, applyPrefix+stripHeredoc(`
		ALTER TABLE `+"`users`"+` CHANGE COLUMN `+"`name` `name`"+` varchar(1000) COLLATE utf8mb4_bin DEFAULT null;
		`,
	))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefChangeColumnBinary(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  word varchar(64) NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  word varchar(64) BINARY NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` CHANGE COLUMN `word` `word` varchar(64) COLLATE utf8mb4_bin NOT NULL;\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefChangeColumnCollate(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  password char(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  password char(128) CHARACTER SET latin1 COLLATE latin1_bin
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` CHANGE COLUMN `password` `password` char(128) CHARACTER SET latin1 COLLATE latin1_bin;\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefChangeGenerateColumnGemerayedAlwaysAs(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE test_table (
		  id int(11) NOT NULL AUTO_INCREMENT,
		  test_value varchar(45) GENERATED ALWAYS AS ('test') VIRTUAL,
		  test_expr varchar(45) GENERATED ALWAYS AS (test_value / test_value) VIRTUAL,
  		  data json NOT NULL,
  		  name varchar(20) GENERATED ALWAYS AS (json_extract(data,'$.name1')) VIRTUAL,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE test_table (
		  id int(11) NOT NULL AUTO_INCREMENT,
		  test_value varchar(45) GENERATED ALWAYS AS ('test') VIRTUAL,
		  test_expr varchar(45) GENERATED ALWAYS AS (test_value / test_value) VIRTUAL,
  		  data json NOT NULL,
  		  name varchar(20) GENERATED ALWAYS AS (json_extract(data,'$.name2')) VIRTUAL,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+stripHeredoc(`
		ALTER TABLE `+"`test_table`"+` DROP COLUMN `+"`name`"+`;
		ALTER TABLE `+"`test_table`"+` ADD COLUMN `+"`name`"+` varchar(20) GENERATED ALWAYS AS (json_extract(data, '$.name2')) VIRTUAL AFTER `+"`data`"+`;
		`,
	))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE test_table (
		  id int(11) NOT NULL AUTO_INCREMENT,
		  test_value varchar(45) GENERATED ALWAYS AS ('test') VIRTUAL,
		  test_expr varchar(45) GENERATED ALWAYS AS (test_value / test_value) STORED,
  		  data json NOT NULL,
  		  name varchar(20) GENERATED ALWAYS AS (json_extract(data,'$.name2')) STORED,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+stripHeredoc(`
		ALTER TABLE `+"`test_table`"+` DROP COLUMN `+"`test_expr`"+`;
		ALTER TABLE `+"`test_table`"+` ADD COLUMN `+"`test_expr`"+` varchar(45) GENERATED ALWAYS AS (test_value / test_value) STORED AFTER `+"`test_value`"+`;
		ALTER TABLE `+"`test_table`"+` DROP COLUMN `+"`name`"+`;
		ALTER TABLE `+"`test_table`"+` ADD COLUMN `+"`name`"+` varchar(20) GENERATED ALWAYS AS (json_extract(data, '$.name2')) STORED AFTER `+"`data`"+`;
		`,
	))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE test_table (
		  id int(11) NOT NULL AUTO_INCREMENT,
		  test_value varchar(45) GENERATED ALWAYS AS ('test') VIRTUAL,
		  test_expr varchar(45) GENERATED ALWAYS AS ( test_value / test_value ) STORED,
  		  data json NOT NULL,
  		  name varchar(20) GENERATED ALWAYS AS (json_extract(data, '$.name2')) STORED,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE test_table (
		  id int(11) NOT NULL AUTO_INCREMENT,
		  test_value varchar(45) GENERATED ALWAYS AS ('test') VIRTUAL,
		  test_expr varchar(45) GENERATED ALWAYS AS (test_value / test_value) STORED NOT NULL,
  		  data json NOT NULL,
  		  name varchar(20) GENERATED ALWAYS AS (json_extract(data,'$.name2')) STORED NOT NULL,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+stripHeredoc(`
		ALTER TABLE `+"`test_table`"+` DROP COLUMN `+"`test_expr`"+`;
		ALTER TABLE `+"`test_table`"+` ADD COLUMN `+"`test_expr`"+` varchar(45) GENERATED ALWAYS AS (test_value / test_value) STORED NOT NULL AFTER `+"`test_value`"+`;
		ALTER TABLE `+"`test_table`"+` DROP COLUMN `+"`name`"+`;
		ALTER TABLE `+"`test_table`"+` ADD COLUMN `+"`name`"+` varchar(20) GENERATED ALWAYS AS (json_extract(data, '$.name2')) STORED NOT NULL AFTER `+"`data`"+`;
		`,
	))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE test_table (
		  id int(11) NOT NULL AUTO_INCREMENT,
		  test_value varchar(45) GENERATED ALWAYS AS ('test') VIRTUAL,
		  test_expr varchar(45) GENERATED ALWAYS AS ((test_value / test_value) * 2) STORED NOT NULL,
  		  data json NOT NULL,
  		  name varchar(20) GENERATED ALWAYS AS (json_extract(data,'$.name2')) STORED NOT NULL,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+stripHeredoc(`
		ALTER TABLE `+"`test_table`"+` DROP COLUMN `+"`test_expr`"+`;
		ALTER TABLE `+"`test_table`"+` ADD COLUMN `+"`test_expr`"+` varchar(45) GENERATED ALWAYS AS ((test_value / test_value) * 2) STORED NOT NULL AFTER `+"`test_value`"+`;
		`,
	))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE test_table (
		  id int(11) NOT NULL AUTO_INCREMENT,
		  test_value varchar(45) GENERATED ALWAYS AS ('test') VIRTUAL,
		  test_expr varchar(45) GENERATED ALWAYS AS ((test_value / test_value) * 2) STORED NOT NULL,
  		  data json NOT NULL,
  		  name varchar(20) GENERATED ALWAYS AS (substr(` + "'" + `test_value` + "'" + `, 1, 2)) STORED NOT NULL,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+stripHeredoc(`
		ALTER TABLE `+"`test_table`"+` DROP COLUMN `+"`name`"+`;
		ALTER TABLE `+"`test_table`"+` ADD COLUMN `+"`name`"+` varchar(20) GENERATED ALWAYS AS (substr(`+"'"+`test_value`+"'"+`, 1, 2)) STORED NOT NULL AFTER `+"`data`"+`;
		`,
	))
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE test_table (
		  id int(11) NOT NULL AUTO_INCREMENT,
		  test_value varchar(45) GENERATED ALWAYS AS ('test') VIRTUAL,
		  test_expr varchar(45) GENERATED ALWAYS AS ((test_value / test_value) * 2) STORED NOT NULL,
  		  data json NOT NULL,
  		  name varchar(20) GENERATED ALWAYS AS (substring(` + "'" + `test_value` + "'" + `, 2, 2)) STORED NOT NULL,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+stripHeredoc(`
		ALTER TABLE `+"`test_table`"+` DROP COLUMN `+"`name`"+`;
		ALTER TABLE `+"`test_table`"+` ADD COLUMN `+"`name`"+` varchar(20) GENERATED ALWAYS AS (substr(`+"'"+`test_value`+"'"+`, 2, 2)) STORED NOT NULL AFTER `+"`data`"+`;
		`,
	))
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefChangeEnumColumn(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  active enum("active")
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  active enum("active", "inactive")
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` CHANGE COLUMN `active` `active` enum('active', 'inactive');\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefChangeComment(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  created_at datetime COMMENT 'created at'
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  created_at datetime COMMENT 'created time'
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` CHANGE COLUMN `created_at` `created_at` datetime COMMENT 'created time';\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefSwapColumn(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(40) NOT NULL,
		  nickname varchar(20) NOT NULL,
		  created_at datetime NOT NULL,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  nickname varchar(20) NOT NULL,
		  name varchar(40) NOT NULL,
		  created_at datetime NOT NULL,
		  PRIMARY KEY (id)
		);`,
	)

	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` CHANGE COLUMN `nickname` `nickname` varchar(20) NOT NULL AFTER `id`;\n",
	)
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

	alterTable := "ALTER TABLE `users` ADD UNIQUE INDEX `index_name`(`name`);\n"
	assertApplyOutput(t, createTable+alterTable, applyPrefix+alterTable)
	assertApplyOutput(t, createTable+alterTable, nothingModified)

	alterTable = "ALTER TABLE `users` ADD INDEX `index_name`(`name`, `created_at`);\n"
	assertApplyOutput(t, createTable+alterTable, applyPrefix+"ALTER TABLE `users` DROP INDEX `index_name`;\n"+alterTable)
	assertApplyOutput(t, createTable+alterTable, nothingModified)

	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` DROP INDEX `index_name`;\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefAddIndexWithKeyLength(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint(20) UNSIGNED NOT NULL AUTO_INCREMENT,
		  name TEXT NOT NULL,
		  PRIMARY KEY (id)
		);`,
	)
	assertApply(t, createTable)

	alterTable := "ALTER TABLE `users` ADD INDEX `index_name`(`name`(255));\n"
	assertApplyOutput(t, createTable+alterTable, applyPrefix+alterTable)
	assertApplyOutput(t, createTable+alterTable, nothingModified)
}

func TestMysqldefIndexOption(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL
		);`,
	)
	assertApply(t, createTable)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL,
		  KEY index_id (id) USING BTREE
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` ADD KEY `index_id` (`id`) using BTREE;\n")
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL,
		  KEY index_id (id) using btree
		);`,
	)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL,
		  KEY index_id (id)
		);`,
	)
	assertApplyOutput(t, createTable, nothingModified)

	resetTestDatabase()
	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL
		);`,
	)
	assertApply(t, createTable)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL,
		  KEY index_id (id)
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` ADD KEY `index_id` (`id`);\n")

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL,
		  KEY index_id (id) USING BTREE
		);`,
	)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL,
		  KEY index_id (id) using btree
		);`,
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefMultipleColumnIndexesOption(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL,
		  registered_at datetime(6) NOT NULL,
		  role_type int(1) NOT NULL
		);`,
	)
	assertApply(t, createTable)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL,
		  registered_at datetime(6) NOT NULL,
		  role_type int(1) NOT NULL,
		  INDEX index_id (registered_at, role_type) USING BTREE
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` ADD INDEX `index_id` (`registered_at`, `role_type`) using BTREE;\n")
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL,
		  registered_at datetime(6) NOT NULL,
		  role_type int(1) NOT NULL,
		  INDEX index_id (registered_at, role_type) using btree
		);`,
	)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL,
		  registered_at datetime(6) NOT NULL,
		  role_type int(1) NOT NULL,
		  INDEX index_id (registered_at, role_type)
		);`,
	)
	assertApplyOutput(t, createTable, nothingModified)

	resetTestDatabase()
	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL,
		  registered_at datetime(6) NOT NULL,
		  role_type int(1) NOT NULL
		);`,
	)
	assertApply(t, createTable)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL,
		  registered_at datetime(6) NOT NULL,
		  role_type int(1) NOT NULL,
		  INDEX index_id (registered_at, role_type)
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` ADD INDEX `index_id` (`registered_at`, `role_type`);\n")

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL,
		  registered_at datetime(6) NOT NULL,
		  role_type int(1) NOT NULL,
		  INDEX index_id (registered_at, role_type) USING BTREE
		);`,
	)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id int(11) NOT NULL,
		  registered_at datetime(6) NOT NULL,
		  role_type int(1) NOT NULL,
		  INDEX index_id (registered_at, role_type) using btree
		);`,
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefFulltextIndex(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE posts (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  title varchar(40) DEFAULT NULL,
		  FULLTEXT KEY title_fulltext_index (title) /*!50100 WITH PARSER ngram */
		);
		`,
	)
	output := stripHeredoc(`
		CREATE TABLE posts (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  title varchar(40) DEFAULT NULL,
		  FULLTEXT KEY title_fulltext_index (title) /*!50100 WITH PARSER ngram */
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+output)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE posts (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  title varchar(40) DEFAULT NULL
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `posts` DROP INDEX `title_fulltext_index`;\n")
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE posts (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  title varchar(40) DEFAULT NULL,
		  FULLTEXT KEY title_fulltext_index (title) /*!50100 WITH PARSER ngram */
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `posts` ADD FULLTEXT KEY `title_fulltext_index` (`title`) WITH parser ngram;\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefCreateIndex(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  name varchar(40) DEFAULT NULL,
		  created_at datetime NOT NULL
		);`,
	)
	assertApply(t, createTable)

	createIndex1 := "CREATE INDEX index_name ON users (name);\n"
	createIndex2 := "CREATE UNIQUE INDEX index_created_at ON users (created_at);\n"
	assertApplyOutput(t, createTable+createIndex1+createIndex2, applyPrefix+createIndex1+createIndex2)
	assertApplyOutput(t, createTable+createIndex1+createIndex2, nothingModified)

	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` DROP INDEX `index_created_at`;\n"+
		"ALTER TABLE `users` DROP INDEX `index_name`;\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefCreateTableKey(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  name varchar(40) DEFAULT NULL,
		  created_at datetime NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  name varchar(40) DEFAULT NULL,
		  created_at datetime NOT NULL,
		  KEY index_name(name),
		  UNIQUE KEY index_created_at(created_at)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` ADD KEY `index_name` (`name`);\n"+
		"ALTER TABLE `users` ADD UNIQUE KEY `index_created_at` (`created_at`);\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefCreateTableWithUniqueColumn(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT PRIMARY KEY
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT PRIMARY KEY,
		  name varchar(40) UNIQUE
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` ADD COLUMN `name` varchar(40) UNIQUE AFTER `id`;\n",
	)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT PRIMARY KEY
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` DROP INDEX `name`;\n"+
		"ALTER TABLE `users` DROP COLUMN `name`;\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefCreateTableChangeUniqueColumn(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  name varchar(40)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  name varchar(40) UNIQUE
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` ADD UNIQUE KEY `name`(`name`);\n",
	)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  name varchar(40)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` DROP INDEX `name`;\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefCreateTableForeignKey(t *testing.T) {
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

	// Add a foreign key without options
	createPosts = stripHeredoc(`
		CREATE TABLE posts (
		  content text,
		  user_id bigint,
		  CONSTRAINT posts_ibfk_1 FOREIGN KEY (user_id) REFERENCES users (id)
		);
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, applyPrefix+"ALTER TABLE `posts` ADD CONSTRAINT `posts_ibfk_1` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`);\n")
	assertApplyOutput(t, createUsers+createPosts, nothingModified)

	// Add options to a foreign key
	createPosts = stripHeredoc(`
		CREATE TABLE posts (
		  content text,
		  user_id bigint,
		  CONSTRAINT posts_ibfk_1 FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
		);
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, applyPrefix+"ALTER TABLE `posts` DROP FOREIGN KEY `posts_ibfk_1`;\nALTER TABLE `posts` ADD CONSTRAINT `posts_ibfk_1` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE;\n")
	assertApplyOutput(t, createUsers+createPosts, nothingModified)

	// Drop a foreign key
	createPosts = stripHeredoc(`
		CREATE TABLE posts (
		  content text,
		  user_id bigint
		);
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, applyPrefix+
		"ALTER TABLE `posts` DROP FOREIGN KEY `posts_ibfk_1`;\n"+
		"ALTER TABLE `posts` DROP INDEX `posts_ibfk_1`;\n",
	)
	assertApplyOutput(t, createUsers+createPosts, nothingModified)

	// Add a foreign key with options
	createPosts = stripHeredoc(`
		CREATE TABLE posts (
		  content text,
		  user_id bigint,
		  CONSTRAINT posts_ibfk_1 FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL ON UPDATE CASCADE
		);
		`,
	)
	assertApplyOutput(t, createUsers+createPosts, applyPrefix+
		"ALTER TABLE `posts` ADD CONSTRAINT `posts_ibfk_1` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE SET NULL ON UPDATE CASCADE;\n")
	assertApplyOutput(t, createUsers+createPosts, nothingModified)
}

func TestMysqldefCreateTableSyntaxError(t *testing.T) {
	resetTestDatabase()
	assertApplyFailure(t, "CREATE TABLE users (id bigint,);", `found syntax error when parsing DDL "CREATE TABLE users (id bigint,)": syntax error at position 32`+"\n")
}

// Both `AUTO_INCREMENT NOT NULL` and `NOT NULL AUTO_INCREMENT` should work
func TestMysqldefAutoIncrementNotNull(t *testing.T) {
	resetTestDatabase()
	createTable1 := stripHeredoc(`
		CREATE TABLE users1 (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY
		);
		`,
	)
	createTable2 := stripHeredoc(`
		CREATE TABLE users2 (
		  id BIGINT UNSIGNED AUTO_INCREMENT NOT NULL PRIMARY KEY
		);
		`,
	)
	assertApplyOutput(t, createTable1+createTable2, applyPrefix+createTable1+createTable2)
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

func TestMysqldefTypeAliases(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  charv character(40),
		  varcharv character varying(40),
		  intv integer
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefBoolean(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  bool_flag BOOL,
		  boolean_flag BOOLEAN
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefDefaultNull(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint(20) NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint(20) NOT NULL,
		  name varchar(40) DEFAULT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` ADD COLUMN `name` varchar(40) DEFAULT null AFTER `id`;\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefAddNotNull(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  name varchar(255) DEFAULT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  name varchar(255) NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` CHANGE COLUMN `name` `name` varchar(255) NOT NULL;\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefCreateTableAddColumnWithCharsetAndNotNull(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name VARCHAR(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` ADD COLUMN `name` varchar(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL AFTER `id`;\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefOnUpdate(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  name varchar(40),
		  updated_at datetime DEFAULT current_timestamp ON UPDATE current_timestamp,
		  created_at datetime NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  name varchar(40),
		  updated_at datetime DEFAULT current_timestamp,
		  created_at datetime NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` CHANGE COLUMN `updated_at` `updated_at` datetime DEFAULT current_timestamp;\n")
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  name varchar(40),
		  updated_at datetime DEFAULT current_timestamp ON UPDATE current_timestamp,
		  created_at datetime NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` CHANGE COLUMN `updated_at` `updated_at` datetime DEFAULT current_timestamp ON UPDATE current_timestamp;\n")
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  name varchar(40),
		  updated_at datetime DEFAULT current_timestamp() ON UPDATE current_timestamp(),
		  created_at datetime NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefCurrentTimestampWithPrecision(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  created_at datetime(6) DEFAULT current_timestamp(6)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  created_at datetime(6) DEFAULT current_timestamp(6),
		  updated_at datetime(6) DEFAULT current_timestamp(6) ON UPDATE current_timestamp(6)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` ADD COLUMN `updated_at` datetime(6) DEFAULT current_timestamp(6) ON UPDATE current_timestamp(6) AFTER `created_at`;\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefEnumValues(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint(20) NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint(20) NOT NULL,
		  authorities enum('normal', 'admin') NOT NULL DEFAULT 'normal'
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` ADD COLUMN `authorities` enum('normal', 'admin') NOT NULL DEFAULT 'normal' AFTER `id`;\n")
	assertApplyOutput(t, createTable, nothingModified)
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

	assertApplyOutput(t, "", applyPrefix+"-- Skipped: DROP TABLE `posts`;\n-- Skipped: DROP TABLE `users`;\nDROP VIEW `foo`;\n")
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
	assertApplyOutput(t, createTable+createTrigger, applyPrefix+
		"DROP TRIGGER `insert_log`;\n"+
		"CREATE TRIGGER `insert_log` after insert ON `users` FOR EACH ROW insert into log(log, dt) values ('insert_users', now());\n")
	assertApplyOutput(t, createTable+createTrigger, nothingModified)

	createTriggerForBeforeUpdate := "CREATE TRIGGER `insert_log_before_update` before update ON `users` FOR EACH ROW insert into log(log, dt) values ('insert', now());\n"
	assertApplyOutput(t, createTable+createTriggerForBeforeUpdate, applyPrefix+createTriggerForBeforeUpdate)
	assertApplyOutput(t, createTable+createTriggerForBeforeUpdate, nothingModified)

	createTriggerForBeforeUpdate = "CREATE TRIGGER `insert_log_before_update` before update ON `users` FOR EACH ROW insert into log(log, dt) values ('insert_users', now());\n"
	assertApplyOutput(t, createTable+createTriggerForBeforeUpdate, applyPrefix+
		"DROP TRIGGER `insert_log_before_update`;\n"+
		"CREATE TRIGGER `insert_log_before_update` before update ON `users` FOR EACH ROW insert into log(log, dt) values ('insert_users', now());\n")
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

func TestMysqldefDefaultValue(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE tools (
		  id int(10) unsigned NOT NULL AUTO_INCREMENT,
		  name varchar(255) COLLATE utf8mb4_bin NOT NULL,
		  created_at datetime NOT NULL,
		  updated_at datetime NOT NULL,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE tools (
		  id int(10) unsigned NOT NULL AUTO_INCREMENT,
		  name varchar(255) COLLATE utf8mb4_bin NOT NULL,
		  created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  updated_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `tools` CHANGE COLUMN `created_at` `created_at` datetime NOT NULL DEFAULT current_timestamp;\n"+
		"ALTER TABLE `tools` CHANGE COLUMN `updated_at` `updated_at` datetime NOT NULL DEFAULT current_timestamp ON UPDATE current_timestamp;\n")
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE tools (
		  id int(10) unsigned NOT NULL AUTO_INCREMENT,
		  name varchar(255) COLLATE utf8mb4_bin NOT NULL,
		  created_at datetime NOT NULL,
		  updated_at datetime NOT NULL,
		  PRIMARY KEY (id)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `tools` CHANGE COLUMN `created_at` `created_at` datetime NOT NULL;\n"+
		"ALTER TABLE `tools` CHANGE COLUMN `updated_at` `updated_at` datetime NOT NULL;\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefNegativeDefault(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE items (
		  position float DEFAULT -20
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE items (
		  position float DEFAULT 100
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `items` CHANGE COLUMN `position` `position` float DEFAULT 100;\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefDecimalDefault(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE some_table (
		  some_value decimal(5, 2) DEFAULT 0.0
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefIndexWithDot(t *testing.T) {
	resetTestDatabase()

	createTable := "CREATE TABLE users (\n" +
		"  `id` BIGINT,\n" +
		"  `account_id` BIGINT\n" +
		");\n"
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = "CREATE TABLE users (\n" +
		"  `id` BIGINT,\n" +
		"  `account_id` BIGINT,\n" +
		"  KEY `account.id`(account_id)\n" +
		");\n"
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` ADD KEY `account.id` (`account_id`);\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefChangeIndexCombination(t *testing.T) {
	resetTestDatabase()

	createTable := "CREATE TABLE users (\n" +
		"  `id` BIGINT,\n" +
		"  `name` varchar(255),\n" +
		"  `account_id` BIGINT,\n" +
		"  KEY `index_users1`(account_id),\n" +
		"  KEY `index_users2`(account_id, name)\n" +
		");\n"
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = "CREATE TABLE users (\n" +
		"  `id` BIGINT,\n" +
		"  `name` varchar(255),\n" +
		"  `account_id` BIGINT,\n" +
		"  KEY `index_users1`(account_id, name),\n" +
		"  KEY `index_users2`(account_id)\n" +
		");\n"
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` DROP INDEX `index_users1`;\n"+
		"ALTER TABLE `users` ADD KEY `index_users1` (`account_id`, `name`);\n"+
		"ALTER TABLE `users` DROP INDEX `index_users2`;\n"+
		"ALTER TABLE `users` ADD KEY `index_users2` (`account_id`);\n")
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
	out := assertedExecute(t, "./mysqldef", "-uroot", "mysqldef_test", "--enable-drop-table", "--file", "schema.sql")
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
