# mysqldef

`mysqldef` works the same way as `mysql` for setting connection information.

```
Usage:
  mysqldef [OPTION]... DATABASE --export
  mysqldef [OPTION]... DATABASE --apply < desired.sql
  mysqldef [OPTION]... DATABASE --dry-run < desired.sql
  mysqldef [OPTION]... current.sql < desired.sql

Application Options:
  -u, --user=USERNAME               MySQL user name (default: root)
  -p, --password=PASSWORD           MySQL user password, overridden by $MYSQL_PWD
  -h, --host=HOSTNAME               Host to connect to the MySQL server (default: 127.0.0.1)
  -P, --port=PORT                   Port used for the connection (default: 3306)
  -S, --socket=PATH                 The socket file to use for connection
      --ssl-mode=MODE               SSL connection mode(PREFERRED,REQUIRED,DISABLED). (default: PREFERRED)
      --ssl-ca=PATH                 File that contains list of trusted SSL Certificate Authorities
      --password-prompt             Force MySQL user password prompt
      --enable-cleartext-plugin     Enable/disable the clear text authentication plugin
      --file=FILENAME               Read desired SQL from the file, rather than stdin (default: -)
      --dry-run                     Don't run DDLs but just show them
      --apply                       Apply DDLs to the database (default, but will require this flag in future versions)
      --export                      Just dump the current schema to stdout
      --enable-drop                 Enable destructive changes such as DROP for TABLE, SCHEMA, ROLE, USER, FUNCTION, PROCEDURE, TRIGGER, VIEW, INDEX, SEQUENCE, TYPE
      --skip-view                   Skip managing views (temporary feature, to be removed later)
      --before-apply=SQL            Execute the given string before applying the regular DDLs
      --config=PATH                 YAML configuration file (can be specified multiple times)
      --config-inline=YAML          YAML configuration as inline string (can be specified multiple times)
      --help                        Show this help
      --version                     Show version information
```

## Synopsis

```shell
# Verify MySQL server connectivity
$ mysql -uroot test -e "select 1;"
+---+
| 1 |
+---+
| 1 |
+---+

# Export current schema
$ mysqldef -uroot test --export
CREATE TABLE `user` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  `name` varchar(191) DEFAULT 'k0kubun',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

# Save it to edit
$ mysqldef -uroot test --export > schema.sql
```

Update schema.sql as follows (instead of `ADD INDEX`, you can just add `KEY index_name (name)` in the `CREATE TABLE` as well):

```diff
 CREATE TABLE user (
   id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
   name VARCHAR(128) DEFAULT 'k0kubun',
+  created_at DATETIME NOT NULL
 ) Engine=InnoDB DEFAULT CHARSET=utf8mb4;
+
+ALTER TABLE user ADD INDEX index_name(name);
```

And then run:

```shell
# Preview migration plan (dry run)
$ mysqldef -uroot test --dry-run < schema.sql
-- dry run --
ALTER TABLE user ADD COLUMN created_at datetime NOT NULL ;
ALTER TABLE user ADD INDEX index_name(name);

# Apply DDLs
$ mysqldef -uroot test --apply < schema.sql
-- Apply --
ALTER TABLE user ADD COLUMN created_at datetime NOT NULL ;
ALTER TABLE user ADD INDEX index_name(name);

# Operations are idempotent - safe to run multiple times
$ mysqldef -uroot test --apply < schema.sql
-- Nothing is modified --

# By default, DROP operations are skipped (safe mode)
# To enable DROP TABLE, DROP COLUMN, etc., use --enable-drop
$ mysqldef -uroot test --apply --enable-drop < schema.sql
-- Apply --
DROP TABLE users;

# Use config file to control schema management
$ cat > config.yml <<EOF
target_tables: |
  users
  posts_\d+
skip_tables: |
  tmp_.*
algorithm: INPLACE
lock: NONE
dump_concurrency: 8
EOF
$ mysqldef -uroot test --apply --config=config.yml < schema.sql

# Use inline YAML configuration
$ mysqldef -uroot test --apply --config-inline="skip_tables: temp_.*" < schema.sql

# Multiple configs (later values override earlier ones)
$ mysqldef -uroot test --apply --config=config.yml --config-inline="algorithm: INSTANT" < schema.sql
```

## Offline Mode (File-to-File Comparison)

mysqldef can compare two schema files **without connecting to a database**. This is useful for CI/CD pipelines, schema validation, and generating migration scripts.

### How It Works

When the database argument ends with `.sql`, mysqldef operates in offline mode:

```shell
# Normal mode: connects to database
$ mysqldef -uroot mydb --apply < schema.sql

# Offline mode: compares two files (no database connection)
$ mysqldef current.sql < desired.sql
```

In offline mode:
- No database connection is established
- The tool compares two SQL files (current vs desired)
- DDL statements are generated to show what would change
- Changes are **always** shown in dry-run mode (not applied to any database)

### Basic Usage

```shell
# Compare two schema files
$ mysqldef current_schema.sql < desired_schema.sql
-- dry run --
BEGIN;
ALTER TABLE `users` ADD COLUMN `email` varchar(255) NOT NULL AFTER `name`;
ALTER TABLE users ADD INDEX index_email(email);
COMMIT;

# Using --file flag instead of stdin
$ mysqldef --file desired_schema.sql current_schema.sql

# Verify idempotency (compare identical schemas)
$ mysqldef desired_schema.sql < desired_schema.sql
-- Nothing is modified --
```

## Supported features

The following DDLs are generated by updating `CREATE TABLE`.
Some can also be used in the input schema.sql file.

- Tables: CREATE TABLE, DROP TABLE, ALTER TABLE RENAME TO, COMMENT, PARTITION BY RANGE
- Columns: ADD COLUMN, CHANGE COLUMN, DROP COLUMN, ALTER TABLE CHANGE COLUMN, DEFAULT, ON UPDATE CURRENT_TIMESTAMP, GENERATED ALWAYS AS, STORED, VIRTUAL, COMMENT
- Constraints: PRIMARY KEY, FOREIGN KEY, CHECK, UNIQUE, ADD CONSTRAINT, DROP CONSTRAINT
- Indexes: ADD INDEX, ADD UNIQUE INDEX, CREATE INDEX, CREATE UNIQUE INDEX, DROP INDEX, ALTER TABLE RENAME INDEX, FULLTEXT, SPATIAL, VECTOR
- Views: CREATE VIEW, CREATE OR REPLACE VIEW, DROP VIEW
- Triggers: CREATE TRIGGER, DROP TRIGGER

## Example

### CREATE TABLE

```diff
+CREATE TABLE users (
+  name VARCHAR(40) DEFAULT NULL
+);
```

Remove the statement to DROP TABLE.

### ADD COLUMN

```diff
 CREATE TABLE users (
   name VARCHAR(40) DEFAULT NULL,
+  created_at DATETIME NOT NULL
 );
```

Remove the line to DROP COLUMN.

### CHANGE COLUMN

```diff
 CREATE TABLE users (
-  name VARCHAR(40) DEFAULT NULL,
+  name CHAR(40) DEFAULT NULL,
   created_at DATETIME NOT NULL
 );
```

### ADD INDEX

```diff
 CREATE TABLE users (
   name CHAR(40) DEFAULT NULL,
   created_at DATETIME NOT NULL,
+  UNIQUE KEY index_name(name)
 );
```

or

```diff
 CREATE TABLE users (
   name CHAR(40) DEFAULT NULL,
   created_at DATETIME NOT NULL
 );
+
+ALTER TABLE users ADD UNIQUE INDEX index_name(name);
```

Remove the line to DROP INDEX.

### ADD PRIMARY KEY

```diff
 CREATE TABLE users (
+  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
   name CHAR(40) DEFAULT NULL,
   created_at datetime NOT NULL,
   UNIQUE KEY index_name(name)
 );
```

Remove the line to DROP PRIMARY KEY.

Composite primary key may not work for now.

### ADD FOREIGN KEY

```diff
 CREATE TABLE users (
   id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
   name CHAR(40) DEFAULT NULL,
   created_at datetime NOT NULL,
   UNIQUE KEY index_name(name)
 );

 CREATE TABLE posts (
   user_id BIGINT UNSIGNED NOT NULL,
+  CONSTRAINT posts_ibfk_1 FOREIGN KEY (user_id) REFERENCES users (id)
 );
```

Remove the line to DROP FOREIGN KEY.

Composite foreign key may not work for now.

### CREATE (OR REPLACE) VIEW

```diff
 CREATE VIEW foo AS
   select u.id as id, p.id as post_id
   from  (
     mysqldef_test.users as u
     join mysqldef_test.posts as p on ((u.id = p.user_id))
   )
 ;
+ CREATE OR REPLACE VIEW foo AS select u.id as id, p.id as post_id from (mysqldef_test.users as u join mysqldef_test.posts as p on (((u.id = p.user_id) and (p.is_deleted = 0))));
```

Remove the line to DROP VIEW.

## Column, Table, and Index Renaming

### Column Renaming

mysqldef supports renaming columns using the `-- @renamed from=old_name` annotation:

```sql
CREATE TABLE users (
  id bigint NOT NULL,
  user_name text, -- @renamed from=username
  age integer
);
```

This generates:
```sql
ALTER TABLE users CHANGE COLUMN username user_name text;
```

For columns with special characters or spaces, use double quotes:

```sql
CREATE TABLE users (
  id bigint NOT NULL,
  column_with_underscore varchar(50), -- @renamed from="column-with-dash"
  normal_column text, -- @renamed from="special column"
);
```

### Table Renaming

mysqldef supports renaming tables using the `-- @renamed from=old_name` annotation on the CREATE TABLE line:

```sql
CREATE TABLE users ( -- @renamed from=user_accounts
  id bigint NOT NULL,
  username text,
  age integer
);
```

You can also use the block comment style:

```sql
CREATE TABLE users /* @renamed from=user_accounts */ (
  id bigint NOT NULL,
  username text,
  age integer
);
```

This generates:
```sql
ALTER TABLE user_accounts RENAME TO users;
```

For tables with special characters or spaces, use double quotes:

```sql
CREATE TABLE user_profiles ( -- @renamed from="user accounts"
  id bigint NOT NULL,
  name text
);
```

You can combine table renaming with column renaming and other schema changes:

```sql
CREATE TABLE accounts ( -- @renamed from=old_accounts
  id bigint NOT NULL PRIMARY KEY,
  username varchar(100) NOT NULL, -- @renamed from=user_name
  is_active boolean DEFAULT true
);
```

### Index Renaming

mysqldef supports renaming indexes using the `-- @renamed from=old_name` or `/* @renamed from=old_name */` annotation:

```sql
CREATE TABLE users (
  id bigint NOT NULL,
  email varchar(255),
  username varchar(100),
  KEY new_email_idx (email) -- @renamed from=old_email_idx
);
```

Or with standalone index creation:

```sql
CREATE INDEX new_email_idx /* @renamed from=old_email_idx */ ON users (email);
```

This generates:
```sql
ALTER TABLE users RENAME INDEX old_email_idx TO new_email_idx;
```

You can rename multiple indexes:

```sql
CREATE TABLE users (
  id bigint NOT NULL,
  email varchar(255),
  username varchar(100),
  KEY email_idx (email), -- @renamed from=idx_email
  KEY username_idx (username) -- @renamed from=idx_username
);
```

The rename annotation also works for unique indexes and constraints:

```sql
CREATE TABLE users (
  id bigint NOT NULL,
  email varchar(255),
  UNIQUE KEY unique_email (email) -- @renamed from=old_unique_email
);
```

## Configuration

Configuration can be provided through YAML files (`--config`) or inline YAML strings (`--config-inline`). Multiple configurations can be specified and will be merged in order.

### Using Configuration Files

```shell
$ mysqldef -uroot dbname --apply --config config.yml < schema.sql
```

### Using Inline Configuration

```shell
$ mysqldef -uroot dbname --apply --config-inline 'enable_drop: true' < schema.sql
```

### Combining Multiple Configurations

```shell
$ mysqldef -uroot dbname --apply \
  --config base.yml \
  --config-inline 'skip_tables: [logs, temp_data]' \
  --config-inline 'enable_drop: true' \
  < schema.sql
```

### Available Configuration Options

| Field | Type | Description |
|-------|------|-------------|
| `enable_drop` | boolean | Enable destructive changes (DROP statements). Equivalent to `--enable-drop` flag. |
| `target_tables` | string | Regular expression patterns (one per line) to specify which tables to manage. Only tables matching these patterns will be processed. |
| `skip_tables` | string | Regular expression patterns (one per line) to specify which tables to skip. Tables matching these patterns will be ignored. |
| `skip_views` | string | Regular expression patterns (one per line) to specify which views to skip. |
| `algorithm` | string | Algorithm to use for ALTER TABLE operations (e.g., INPLACE, INSTANT, COPY). Controls how MySQL performs the schema change. |
| `lock` | string | Lock level to use for ALTER TABLE operations (e.g., NONE, SHARED, EXCLUSIVE). Controls concurrent access during schema changes. |
| `dump_concurrency` | integer | Number of parallel connections to use when exporting the schema. Improves performance for large schemas. Default is 1. |
| `legacy_ignore_quotes` | boolean | Controls identifier quoting behavior. When `true` (default), all identifiers are quoted in output. When `false`, identifiers preserve their original quoting from the source SQL. Default is `true` but will change to `false` in the next major version. |

## MariaDB Compatibility

mysqldef is compatible with [MariaDB](https://mariadb.org/), a MySQL-compatible database.

## TiDB Compatibility

mysqldef is compatible with [TiDB](https://www.pingcap.com/tidb/), a MySQL-compatible distributed database. TiDB uses port 4000 by default.

### Caveats: CHECK Constraints

TiDB disables CHECK constraint support by default. To use CHECK constraints with mysqldef, enable them in TiDB:

```sql
SET GLOBAL tidb_enable_check_constraint = ON;
```

This setting:
- Is a global-only variable (cannot be set per session)
- Requires SUPER privilege to change
- Does not persist across TiDB restarts (must be set in TiDB configuration for persistence)

Without this setting, CHECK constraints in your schema will be silently ignored by TiDB.
