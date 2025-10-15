# Development Guide

This project provides four schema management commands:

- **mysqldef** - MySQL schema management (mimics `mysql` CLI options)
- **psqldef** - PostgreSQL schema management (mimics `psql` CLI options)
- **mssqldef** - SQL Server schema management (mimics `sqlcmd` CLI options)
- **sqlite3def** - SQLite3 schema management (mimics `sqlite3` CLI options)

Each command follows the same pattern: it accepts connection parameters similar to those of the corresponding database CLI tool and applies schema changes idempotently.

## General Rules

* Never commit the changes unless the user asks for it.
* Write comments to describe what is not obvious in the code. Describing the "why" is a recommended practice.
* Format queries in string literals.
* Use "log/slog" to trace internal flow of the code. `LOG_LEVEL=debug` to enable debug logging.

## Build

Build all the sqldef commands (`mysqldef`, `psqldef`, `sqlite3def`, `mssqldef`):

```sh
make build
```

The executable binaries will be placed in the `build/$os-$arch$/` directory.

### Build Parser

To maintain the parser, edit `parser/parser.y` and run:

```sh
make parser
# or
make regen-parser # force regeneration
```

For now, `psqldef` has two parsers: the primary parser is `generic` parser, and the fallback parser is `pgquery`, a native PostgreSQL parser.

## Local Development

To have trial and error locally, you can use the following commands:

```sh
# psqldef
build/$os-$arch$/psqldef psqldef_test [args...]

# mysqldef
build/$os-$arch$/mysqldef mysqldef_test [args...]

# mssqldef (password is mandatory)
build/$os-$arch$/mssqldef -PPassw0rd mssqldef_test [args...]

# sqlite3def
build/$os-$arch$/sqlite3def sqlite3def.db [args...]
```

## Running Tests

For development iterations, use these commands to run tests:

### Run all tests

```sh
make test # it will take 5 minutes to run
```

### Run tests for specific `*def` tools

```sh
go test ./cmd/mysqldef
go test ./cmd/psqldef
go test ./cmd/sqlite3def
go test ./cmd/mssqldef

# for testing with MariaDB
MYSQL_FLAVOR=mariadb MYSQL_PORT=3307 go test ./cmd/mysqldef
```

`make test* VERBOSE=1` sets `-v` to `go test`.

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

The test files use a YAML format where each top-level key is a test case name, and the value is a `TestCase` object with the following fields:

```yaml
TestCaseName:
  # Current schema state (optional, defaults to empty schema)
  current: |
    CREATE TABLE users (
      id bigint NOT NULL
    );

  # Desired schema state (optional, defaults to empty schema)
  desired: |
    CREATE TABLE users (
      id bigint NOT NULL,
      name text
    );

  # Expected DDL output (optional, defaults to 'desired' if not specified)
  output: |
    ALTER TABLE "public"."users" ADD COLUMN "name" text;

  # Expected error message (optional, defaults to no error)
  error: "specific error message"

  # Minimum database version required (optional)
  min_version: "10.0"

  # Maximum database version supported (optional)
  max_version: "14.0"

  # Database flavor requirement (optional, mysqldef only)
  # Either "mariadb" or "mysql"
  flavor: "mariadb"

  # User to run the test as (optional)
  user: "testuser"

  # Managed roles for privilege testing (optional, psqldef only)
  managed_roles:
    - readonly_user
    - app_user

  # Whether to enable DROP/REVOKE operations (optional, defaults to true)
  enable_drop: false

  # Only test that the schema applies successfully (optional, defaults to false)
  # When true, doesn't check exact DDL output, just that it applies without error
  apply_only: true

  # Configuration options for the test (optional)
  config:
    # Create indexes concurrently (psqldef only)
    create_index_concurrently: true
```

### Best Practices

1. **Use consistent prefixes**: When adding related test cases, use the same prefix for test names. This allows you to run all related tests with a simple pattern:
   ```sh
   # Example: Testing all index-related features
   go test ./cmd/psqldef -run='TestApply/Index.*'
   ```

2. **Test both directions**: When testing schema changes, consider testing both:
   - Adding features (no `current`, only `desired`)
   - Modifying existing schemas (`current` → `desired`)

## Documentation

There are markdown files to describe the usage of each command. Keep them up to date:

* `cmd-psqldef.md` for `psqldef`
* `cmd-mysqldef.md` for `mysqldef`
* `cmd-sqlite3def.md` for `sqlite3def`
* `cmd-mssqldef.md` for `mssqldef`

## Task Completion Checklist

Before considering any task complete, run these commands:

* [ ] `make build`  # Ensure all commands are compiled
* [ ] `make test`   # Ensure all tests pass
* [ ] `make lint`   # Ensure the code is linted
* [ ] `make format` # Ensure the code is formatted
