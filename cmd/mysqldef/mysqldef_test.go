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
	nothingModified = "-- Nothing is modified --\n"
	applyPrefix     = "-- Apply --\n"
)

func TestMysqldefCreateTable(t *testing.T) {
	resetTestDatabase()

	createTable1 := stripHeredoc(`
		CREATE TABLE users (
		  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  name varchar(40) DEFAULT NULL,
		  created_at datetime NOT NULL
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

	assertApplyOutput(t, createTable1, applyPrefix+"DROP TABLE `bigdata`;\n")
	assertApplyOutput(t, createTable1, nothingModified)
}

func TestMysqldefCreateTableWithImplicitNotNull(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint PRIMARY KEY,
		  name varchar(40) DEFAULT NULL,
		  created_at datetime NOT NULL
		);
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified) // `NOT NULL` appears on `id`
}

func TestMysqldefCreateTableDropPrimaryKey(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL PRIMARY KEY,
		  name varchar(20)
		);`,
	)
	assertApply(t, createTable)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20)
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `users` DROP PRIMARY KEY;\n")
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20) PRIMARY KEY
		);`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` CHANGE COLUMN `name` `name` varchar(20) NOT NULL;\n"+
		"ALTER TABLE `users` ADD primary key (`name`);\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefCreateTableAddPrimaryKey(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint NOT NULL,
		  name varchar(20),
		  PRIMARY KEY (id)
		);
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` ADD primary key (`id`);\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

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
		"ALTER TABLE `friends` ADD primary key (`user_id`, `friend_id`);\n",
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
		"ALTER TABLE `users` ADD primary key (`id`);\n"+
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

func TestMysqldefCreateTableChangeAutoIncrement(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users (
		  id bigint(20) NOT NULL PRIMARY KEY,
		  name varchar(20)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint(20) NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  name varchar(20)
		);
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` CHANGE COLUMN `id` `id` bigint(20) NOT NULL AUTO_INCREMENT;\n",
	)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE users (
		  id bigint(20) NOT NULL PRIMARY KEY,
		  name varchar(20)
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `users` CHANGE COLUMN `id` `id` bigint(20) NOT NULL;\n",
	)
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefCreateTableRemoveAutoIncrementPrimaryKey(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE friends (
		  id bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
		  created_at datetime NOT NULL
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
	assertApplyOutput(t, createTable, nothingModified)

	createTable = stripHeredoc(`
		CREATE TABLE friends (
		  created_at datetime NOT NULL
		);
		`,
	)

	assertApplyOutput(t, createTable, applyPrefix+
		"ALTER TABLE `friends` CHANGE COLUMN `id` `id` bigint(20) NOT NULL;\n"+
		"ALTER TABLE `friends` DROP PRIMARY KEY;\n"+
		"ALTER TABLE `friends` DROP COLUMN `id`;\n",
	)
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
	assertApplyOutput(t, createTable, applyPrefix+createTable)
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
	assertApplyOutput(t, createTable, applyPrefix+"ALTER TABLE `posts` ADD fulltext key `title_fulltext_index`(`title`);\n")
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
		"ALTER TABLE `users` ADD key `index_name`(`name`);\n"+
		"ALTER TABLE `users` ADD unique key `index_created_at`(`created_at`);\n",
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
		"ALTER TABLE `tools` ADD key `index_character`(`character`);\n")
	assertApplyOutput(t, createTable, nothingModified)
}

func TestMysqldefMysqlComment(t *testing.T) {
	resetTestDatabase()

	createTable := stripHeredoc(`
		CREATE TABLE users(
		  id bigint NOT NULL /* comment */
		);
		`,
	)
	assertApplyOutput(t, createTable, applyPrefix+createTable)
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

	assertApplyOutput(t, "", applyPrefix+"DROP TABLE `posts`;\nDROP TABLE `users`;\nDROP VIEW `foo`;\n")
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
		"ALTER TABLE `users` ADD key `account.id`(`account_id`);\n")
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
		"ALTER TABLE `users` ADD key `index_users1`(`account_id`, `name`);\n"+
		"ALTER TABLE `users` DROP INDEX `index_users2`;\n"+
		"ALTER TABLE `users` ADD key `index_users2`(`account_id`);\n")
	assertApplyOutput(t, createTable, nothingModified)
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
	assertEquals(t, dryRun, strings.Replace(apply, "Apply", "dry run", 1))
}

func TestMysqldefExport(t *testing.T) {
	resetTestDatabase()
	out := assertedExecute(t, "mysqldef", "-uroot", "mysqldef_test", "--export")
	assertEquals(t, out, "-- No table exists --\n")

	mustExecute("mysql", "-uroot", "mysqldef_test", "-e", stripHeredoc(`
		CREATE TABLE users (
		  name varchar(40),
		  created_at datetime NOT NULL
		) DEFAULT CHARSET=latin1;`,
	))
	out = assertedExecute(t, "mysqldef", "-uroot", "mysqldef_test", "--export")
	assertEquals(t, out,
		"CREATE TABLE `users` (\n"+
			"  `name` varchar(40) DEFAULT NULL,\n"+
			"  `created_at` datetime NOT NULL\n"+
			") ENGINE=InnoDB DEFAULT CHARSET=latin1;\n",
	)
}

func TestMysqldefSkipDrop(t *testing.T) {
	resetTestDatabase()
	mustExecute("mysql", "-uroot", "mysqldef_test", "-e", stripHeredoc(`
		CREATE TABLE users (
		  name varchar(40),
		  created_at datetime NOT NULL
		) DEFAULT CHARSET=latin1;`,
	))

	writeFile("schema.sql", "")

	skipDrop := assertedExecute(t, "mysqldef", "-uroot", "mysqldef_test", "--skip-drop", "--file", "schema.sql")
	apply := assertedExecute(t, "mysqldef", "-uroot", "mysqldef_test", "--file", "schema.sql")
	assertEquals(t, skipDrop, strings.Replace(apply, "DROP", "-- Skipped: DROP", 1))
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
	t.Helper()
	writeFile("schema.sql", schema)
	assertedExecute(t, "mysqldef", "-uroot", "mysqldef_test", "--file", "schema.sql")
}

func assertApplyOutput(t *testing.T, schema string, expected string) {
	t.Helper()
	writeFile("schema.sql", schema)
	actual := assertedExecute(t, "mysqldef", "-uroot", "mysqldef_test", "--file", "schema.sql")
	assertEquals(t, actual, expected)
}

func assertApplyFailure(t *testing.T, schema string, expected string) {
	t.Helper()
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
