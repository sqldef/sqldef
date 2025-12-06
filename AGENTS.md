# Development Guide

This project provides four schema management commands:

- **mysqldef** - MySQL schema management (mimics `mysql` CLI options)
- **psqldef** - PostgreSQL schema management (mimics `psql` CLI options)
- **mssqldef** - SQL Server schema management (mimics `sqlcmd` CLI options)
- **sqlite3def** - SQLite3 schema management (mimics `sqlite3` CLI options)

Each command follows the same pattern: it accepts connection parameters similar to those of the corresponding database CLI tool and applies schema changes idempotently.

## General Rules

* Never commit the changes unless the user asks for it
* Write comments to describe what is not obvious in the code. Explaining the "why" in comments is a recommended practice
* Format SQL statements in string literals
* Use `log/slog` to trace internal state of the code. Set `LOG_LEVEL=debug` to enable debug logging
* Use `panic` to assert that it is not reachable
* Be aware of the two escaping modes:
  * `legacy_ignore_quotes: true` (the default, backward compatible mode) generates SQL with always quoted identifiers
  * `legacy_ignore_quotes: false` (quote-aware mode) generates SQL with identifiers quoted only when they are quoted in the source SQL

## Build

Build all the sqldef commands (`mysqldef`, `psqldef`, `sqlite3def`, `mssqldef`):

```sh
make build
```

The executable binaries will be placed in the `build/$os-$arch/` directory.

### The Parser

To update the generic SQL parser, edit `parser/parser.y` and regenerate:

```sh
make parser    # generate parser/parser.go from parser/parser.y
make parser-v  # same as above, also writes a conflict report to y.output
```

Requirements:
- No reduce/reduce conflicts are allowed
- No more shift/reduce conflicts are allowed unless absolutely necessary
- To resolve conflicts, use `make parser-v` and inspect `y.output`

Usage notes:
- `psqldef` uses the **generic parser** by default with fallback to `go-pgquery` (native PostgreSQL parser)
- Always set `PSQLDEF_PARSER=generic` environment variable on development:
  - `PSQLDEF_PARSER=generic` - Use only the generic parser (no fallback to pgquery)
  - `PSQLDEF_PARSER=pgquery` - Use only the pgquery parser (no fallback to generic)
  - Not set (default) - Use generic parser with fallback to pgquery
- The generic parser builds ASTs, and the generator manipulates the ASTs for normalization and comparison. Do not parse strings with regular expressions
- No need to maintain the pgquery parser, which is obsolete and will be removed in the future
- Be careful to iterate a map because the iteration order is not deterministic. Use `util.CanonicalMapIter` to iterate maps in a deterministic order.

## Local Development

To have trial and error locally, you can use the following commands:

```sh
# psqldef - export current schema
build/$os-$arch/psqldef psqldef_test --export > schema.sql

# psqldef - dry run to preview changes
build/$os-$arch/psqldef psqldef_test --dry-run --file schema.sql

# psqldef - apply schema from file
build/$os-$arch/psqldef psqldef_test --apply --file schema.sql

# mysqldef - export current schema
build/$os-$arch/mysqldef mysqldef_test --export > schema.sql

# mysqldef - dry run to preview changes
build/$os-$arch/mysqldef mysqldef_test --dry-run --file schema.sql

# mysqldef - apply schema from file
build/$os-$arch/mysqldef mysqldef_test --apply --file schema.sql

# mssqldef - export current schema (password is mandatory)
build/$os-$arch/mssqldef -PPassw0rd mssqldef_test --export > schema.sql

# mssqldef - dry run to preview changes
build/$os-$arch/mssqldef -PPassw0rd mssqldef_test --dry-run --file schema.sql

# mssqldef - apply schema from file
build/$os-$arch/mssqldef -PPassw0rd mssqldef_test --apply --file schema.sql

# sqlite3def - export current schema
build/$os-$arch/sqlite3def sqlite3def.db --export > schema.sql

# sqlite3def - dry run to preview changes
build/$os-$arch/sqlite3def sqlite3def.db --dry-run --file schema.sql

# sqlite3def - apply schema from file
build/$os-$arch/sqlite3def sqlite3def.db --apply --file schema.sql
```

## Running Tests

For development iterations, use these commands to run tests:

### Run all tests

```sh
make test # it will take 5 minutes to run
```

### Run tests for specific `*def` tools

The test runner is `gotestsum`, which is a wrapper around `go test` that provides a more readable output.

```sh
go test ./cmd/mysqldef
go test ./cmd/psqldef
go test ./cmd/sqlite3def
go test ./cmd/mssqldef
```

For MariaDB testing:

```sh
MYSQL_FLAVOR=mariadb MYSQL_PORT=3307 go test ./cmd/mysqldef
```

For test coverage:

```sh
make test-cov     # shows a plain text report
make test-cov-xml # generates coverage.xml
```

### Run individual tests

Use the `-run` flag with a regex pattern to run specific test cases:

```sh
# Run a specific test (runs test cases matching CreateTable* defined in the YAML test files)
go test ./cmd/mysqldef -run=TestApply/CreateTable

# Run tests for a specific feature across all tools
go test ./cmd/*def -run=TestApply/AddColumn
```

The test name pattern follows the format `TestApply/<TestCaseName>`, where `<TestCaseName>` corresponds to the test scenarios defined in the YAML test files.

### How to Write Tests

For schema management tests, in most cases you only need to edit the YAML test files.

#### YAML Test Schema

The test files use a YAML format where each top-level key is a test case name, and the value is a `TestCase` object. A JSON schema is available at `./cmd/testutils/testcase.schema.json` for IDE support.

Test case fields:

```yaml
# yaml-language-server: $schema=../testutils/testcase.schema.json
TestCaseName:
  # Current schema state (defaults to empty schema)
  current: |
    CREATE TABLE users (
      id bigint NOT NULL
    );

  # Desired schema state (defaults to empty schema)
  desired: |
    CREATE TABLE users (
      id bigint NOT NULL,
      name text
    );

  # Expected DDL for forward migration: current → desired
  # If specified, 'down' must also be specified
  up: |
    ALTER TABLE "public"."users" ADD COLUMN "name" text;

  # Expected DDL for reverse migration: desired → current
  # Required if 'up' is specified (empty string is allowed for empty DDL)
  down: |
    ALTER TABLE "public"."users" DROP COLUMN "name";

  # Expected error message (defaults to no error)
  error: "specific error message"

  # Minimum database version required
  min_version: "10.0"

  # Maximum database version supported
  max_version: "14.0"

  # Database flavor requirement (mysqldef only)
  # Either "mariadb" or "mysql"
  flavor: "mariadb"

  # User to run the test as
  user: "testuser"

  # Managed roles for privilege testing
  managed_roles:
    - readonly_user
    - app_user

  # Whether to enable DROP/REVOKE operations (defaults to true)
  enable_drop: false

  # Use offline testing only for proprietary SQL dialects such as Aurora DSQL (defaults to false)
  offline: true

  # Configuration options for the test
  config:
    # Create indexes concurrently (psqldef only)
    create_index_concurrently: true
```

The `up` and `down` fields work together:
- If neither is specified: idempotency-only test (verifies `desired` schema is stable)
- If `up` is specified: `down` must also be specified (bidirectional migration test)

When both are specified, the test runner validates:
1. `current` → `desired` produces `up`
2. `desired` → `current` produces `down`

NOTE: Never use `offline: true` for databases that are tested in GitHub Actions:
- MySQL (including MariaDB)
- PostgreSQL
- SQLite3
- SQL Server

### Best Practices

* **Use consistent prefixes**: When adding related test cases, use the same prefix for test names. This allows you to run all related tests with a simple pattern:
   ```sh
   # Example: Testing all index-related features
   go test ./cmd/psqldef -run='TestApply/Index.*'
   ```
* **Check test coverage**: When you edit source code, check the coverage report to ensure the code is covered by tests.

## Documentation

There are markdown files to describe the usage of each command. Keep them up to date:

* `cmd-psqldef.md` for `psqldef`
* `cmd-mysqldef.md` for `mysqldef`
* `cmd-sqlite3def.md` for `sqlite3def`
* `cmd-mssqldef.md` for `mssqldef`

## Task Completion Checklist

Before considering any task complete, run these commands to ensure the code is in a good state:

* `make build`
* `make test`
* `make modernize`
* `make lint`
* `make vulncheck`
* `make format`
