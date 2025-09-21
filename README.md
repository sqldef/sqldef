# sqldef [![sqldef](https://github.com/sqldef/sqldef/actions/workflows/sqldef.yml/badge.svg)](https://github.com/sqldef/sqldef/actions/workflows/sqldef.yml)

**sqldef** is the easiest idempotent schema management tool for MySQL, PostgreSQL, SQLite3, and SQL Server that uses plain SQL DDLs. Define your desired schema in SQL, and sqldef generates and applies the migrations to update your database.

With sqldef, you maintain a single SQL file with your complete schema. To modify your schema - add columns, change constraints, or create indexes - simply edit this file. sqldef compares desired against current schema and generates the appropriate DDLs, ensuring your database reaches the desired state from any starting point.

Each database gets its own command (`mysqldef`, `psqldef`, `sqlite3def`, `mssqldef`) that mimics the connection options of the native database client, making it familiar and easy to integrate into existing workflows. The tool comes as a single binary with no dependencies, and provides idempotent operations that are safe to run multiple times.

This is inspired by [Ridgepole](https://github.com/ridgepole/ridgepole) but using SQL,
so there's no need to remember Ruby DSL.

![demo](./demo.gif)

## Usage

### Basic Workflow

This is the basic workflow, which is identical across all databases - only the connection options differ between commands.

**Note:** Replace `$sqldef` with the appropriate command for your database:

- `mysqldef` for MySQL
- `psqldef` for PostgreSQL
- `sqlite3def` for SQLite
- `mssqldef` for SQL Server

#### 1. Export Current Schema

```shell
$sqldef [connection-options] --export > schema.sql
```

Export the existing database schema to review your starting point.

#### 2. Modify the Schema

Edit `schema.sql` to add, remove, or change columns/tables/indexes:

```sql
CREATE TABLE users (
  id BIGINT PRIMARY KEY,
  name VARCHAR(100),
  age INTEGER,  -- Added new column
  created_at TIMESTAMP
);
```

#### 3. Preview Changes

```shell
$sqldef [connection-options] --dry-run < schema.sql
```

Show the migrations that will be applied without executing them (e.g., `ALTER TABLE users ADD COLUMN age INTEGER`).

#### 4. Apply Changes

```shell
$sqldef [connection-options] < schema.sql
```

Apply the necessary DDLs to transform current schema to desired state.

Running again shows no changes needed - operations are idempotent.

### Command Documentation

* [mysqldef](./cmd-mysqldef.md)
* [psqldef](./cmd-psqldef.md)
* [sqlite3def](./cmd-sqlite3def.md)
* [mssqldef](./cmd-mssqldef.md)

## Column, Table, and Index Renaming

### Column Renaming

sqldef supports renaming columns using the `-- @rename from=old_name` annotation:

```sql
CREATE TABLE users (
  id bigint NOT NULL,
  user_name text, -- @rename from=username
  age integer
);
```

This generates appropriate rename commands for each database:
- MySQL: `ALTER TABLE users CHANGE COLUMN username user_name text`
- PostgreSQL: `ALTER TABLE users RENAME COLUMN username TO user_name`
- SQL Server: `EXEC sp_rename 'users.username', 'user_name', 'COLUMN'`
- SQLite: `ALTER TABLE users RENAME COLUMN username TO user_name`

For columns with special characters or spaces, use double quotes:

```sql
CREATE TABLE users (
  id bigint NOT NULL,
  column_with_underscore varchar(50), -- @rename from="column-with-dash"
  normal_column text, -- @rename from="special column"
);
```

### Table Renaming

sqldef supports renaming tables using the `-- @rename from=old_name` annotation on the CREATE TABLE line:

```sql
CREATE TABLE users ( -- @rename from=user_accounts
  id bigint NOT NULL,
  username text,
  age integer
);
```

You can also use the block comment style:

```sql
CREATE TABLE users /* @rename from=user_accounts */ (
  id bigint NOT NULL,
  username text,
  age integer
);
```

This generates appropriate rename commands for each database:
- MySQL: `ALTER TABLE user_accounts RENAME TO users`
- PostgreSQL: `ALTER TABLE user_accounts RENAME TO users`
- SQL Server: `EXEC sp_rename 'user_accounts', 'users'`
- SQLite: `ALTER TABLE user_accounts RENAME TO users`

For tables with special characters or spaces, use double quotes:

```sql
CREATE TABLE user_profiles ( -- @rename from="user accounts"
  id bigint NOT NULL,
  name text
);
```

You can combine table renaming with column renaming and other schema changes:

```sql
CREATE TABLE accounts ( -- @rename from=old_accounts
  id bigint NOT NULL PRIMARY KEY,
  username varchar(100) NOT NULL, -- @rename from=user_name
  is_active boolean DEFAULT true
);
```

### Index Renaming

sqldef supports renaming indexes using the `-- @rename from=old_name` or `/* @rename from=old_name */` annotation:

```sql
CREATE TABLE users (
  id bigint NOT NULL,
  email varchar(255),
  username varchar(100),
  INDEX new_email_idx (email) -- @rename from=old_email_idx
);
```

Or with standalone index creation:

```sql
CREATE INDEX new_email_idx /* @rename from=old_email_idx */ ON users (email);
```

This generates appropriate rename commands for each database:
- MySQL: `ALTER TABLE users RENAME INDEX old_email_idx TO new_email_idx`
- PostgreSQL: `ALTER INDEX old_email_idx RENAME TO new_email_idx`
- SQL Server: `EXEC sp_rename 'users.old_email_idx', 'new_email_idx', 'INDEX'`
- SQLite: Drops and recreates the index (doesn't support index renaming)

You can rename multiple indexes:

```sql
CREATE TABLE users (
  id bigint NOT NULL,
  email varchar(255),
  username varchar(100),
  INDEX email_idx (email), -- @rename from=idx_email
  INDEX username_idx (username) -- @rename from=idx_username
);
```

The rename annotation also works for unique indexes and constraints:

```sql
CREATE TABLE users (
  id bigint NOT NULL,
  email varchar(255),
  UNIQUE KEY unique_email (email) -- @rename from=old_unique_email
);
```

## Installation

### Pre-built binaries

Download the single-binary executable for your favorite database from:

https://github.com/sqldef/sqldef/releases

### Linux

Debian packages might be supported in the future, but for now they have not been implemented yet.

```shell
# mysqldef
wget -O - https://github.com/sqldef/sqldef/releases/latest/download/mysqldef_linux_amd64.tar.gz \
  | tar xvz

# psqldef
wget -O - https://github.com/sqldef/sqldef/releases/latest/download/psqldef_linux_amd64.tar.gz \
  | tar xvz

# sqlite3def
wget -O - https://github.com/sqldef/sqldef/releases/latest/download/sqlite3def_linux_amd64.tar.gz \
  | tar xvz

# mssqldef
wget -O - https://github.com/sqldef/sqldef/releases/latest/download/mssqldef_linux_amd64.tar.gz \
  | tar xvz
```

### macOS

[Homebrew tap](https://github.com/sqldef/homebrew-sqldef) is available.

```shell
# mysqldef
brew install sqldef/sqldef/mysqldef

# psqldef
brew install sqldef/sqldef/psqldef

# sqlite3def
brew install sqldef/sqldef/sqlite3def

# mssqldef
brew install sqldef/sqldef/mssqldef
```

## Development

If you update `parser/parser.y`, run:

```shell
$ make parser
```

Use the following commands to prepare command line tools and DB servers for running tests.

```shell
# Linux
$ sudo apt install mysql-client postgresql-client sqlite3
$ curl https://packages.microsoft.com/keys/microsoft.asc | sudo apt-key add -
$ curl https://packages.microsoft.com/config/ubuntu/22.04/prod.list | sudo tee /etc/apt/sources.list.d/msprod.list
$ sudo apt-get update && sudo apt-get install mssql-tools # then add: export PATH="$PATH:/opt/mssql-tools/bin"

# macOS
$ brew install libpq && brew link --force libpq
$ brew install microsoft/mssql-release/mssql-tools

# Start database
$ docker-compose up

# Run all tests
$ make test

# Run *def tests
$ go test ./cmd/*def

# Run a single test
$ go test ./cmd/mysqldef -run=TestApply/CreateTable
```

## Contributing

Please file a pull request if you have a feature request.

If you're unsure what to do, you may file a "Feature requests" ticket on [Discussions](https://github.com/sqldef/sqldef/discussions)
and discuss how to implement that with the community.

## Releasing

The `tagpr` and `sqldef` workflows are used to release sqldef.

1. (optional) A maintainer labels a pull request (PR) with `minor` or `major` to manage the next version.
2. When a PR is merged to the default branch, `tagpr` creates a PR to bump the version and update the CHANGELOG.md ("release PR").
3. **A maintainer reviews the release PR and merges it.**
4. `tagpr` creates and pushes a release tag, which triggers the next workflow.
5. `sqldef` workflows creates a GitHub release, build artifacts, upload them to the GitHub release.

Unless it's a pretty big change that needs a discussion, we encourage sqldef maintainers to merge and release
their own Pull Requests without asking/waiting for reviews.

We also expect them to release sqldef as frequently as possible.
When there's a behavior change, sqldef should have at least one release on that day.

## Maintainers

* **@k0kubun**
* **@knaka** (sqlite3def)
* **@odz** (mssqldef)
* **@hokaccha** (psqldef)
* **@gfx** (psqldef)

These are the component they were contributing to when they became a maintainer,
but they're allowed to maintain every part of sqldef.

### Alumni

* **@ytakaya** (mssqldef)

## License

Unless otherwise noted, the sqldef source files are distributed under the MIT License found in the LICENSE file.

[parser](./parser) is distributed under the Apache Version 2.0 license found in the [parser/LICENSE.md](./parser/LICENSE.md) file.
