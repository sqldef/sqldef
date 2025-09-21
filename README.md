# sqldef [![sqldef](https://github.com/sqldef/sqldef/actions/workflows/sqldef.yml/badge.svg)](https://github.com/sqldef/sqldef/actions/workflows/sqldef.yml)

The easiest idempotent schema management by SQL for MySQL / PostgreSQL / SQLite3 / SQL Server databases.

This is inspired by [Ridgepole](https://github.com/ridgepole/ridgepole) but using SQL,
so there's no need to remember Ruby DSL.

![demo](./demo.gif)

## Installation

Download the single-binary executable for your favorite database from:

https://github.com/sqldef/sqldef/releases

## Usage

There are `mysqldef`, `psqldef`, `sqlite3def`, and `mssqldef` provided for each database:

* [mysqldef](./mysqldef.md) for MySQL
* [psqldef](./psqldef.md) for PostgreSQL
* [sqlite3def](./sqlite3def.md) for SQLite3
* [mssqldef](./mssqldef.md) for SQL Server

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

This will generate appropriate rename commands for each database:
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

This will generate appropriate rename commands for each database:
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

This will generate appropriate rename commands for each database:
- MySQL: `ALTER TABLE users RENAME INDEX old_email_idx TO new_email_idx`
- PostgreSQL: `ALTER INDEX old_email_idx RENAME TO new_email_idx`
- SQL Server: `EXEC sp_rename 'users.old_email_idx', 'new_email_idx', 'INDEX'`
- SQLite: Drops and recreates the index (SQLite doesn't support index renaming)

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

## Distributions

### Linux

A debian package might be supported in the future, but for now it has not been implemented yet.

```shell
# mysqldef
wget -O - https://github.com/sqldef/sqldef/releases/latest/download/mysqldef_linux_amd64.tar.gz \
  | tar xvz

# psqldef
wget -O - https://github.com/sqldef/sqldef/releases/latest/download/psqldef_linux_amd64.tar.gz \
  | tar xvz
```

### macOS

[Homebrew tap](https://github.com/sqldef/homebrew-sqldef) is available.

```shell
# mysqldef
brew install sqldef/sqldef/mysqldef

# psqldef
brew install sqldef/sqldef/psqldef
```

## Development

If you update parser/parser.y, run:

```shell
$ make parser
```

You can use the following command to prepare command line tools and DB servers for running tests.

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

[parser](./parser) is distributed under the Apache Version 2.0 license found in the parser/LICENSE.md file.
