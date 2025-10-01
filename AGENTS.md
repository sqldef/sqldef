# Development Guide

This project provides four schema management commands:

- **mysqldef** - MySQL schema management (mimics `mysql` CLI options)
- **psqldef** - PostgreSQL schema management (mimics `psql` CLI options)
- **mssqldef** - SQL Server schema management (mimics `sqlcmd` CLI options)
- **sqlite3def** - SQLite3 schema management (mimics `sqlite3` CLI options)

Each command follows the same pattern: it accepts connection parameters similar to those of the corresponding database CLI tool and applies schema changes idempotently.

## Building Commands

Build all `*def` commands:

```sh
make build
```

The compiled binaries will be placed in the `build/$os-$arch$/` directory.

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
make test
```

### Run tests for specific `*def` tools

```sh
go test ./cmd/mysqldef
go test ./cmd/psqldef
go test ./cmd/sqlite3def
go test ./cmd/mssqldef
```

For MariaDB testing locally:

```sh
MYSQL_FLAVOR=mariadb MYSQL_PORT=3307 go test ./cmd/mysqldef
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

### Best Practices

1. **Use consistent prefixes**: When adding related test cases, use the same prefix for test names. This allows you to run all related tests with a simple pattern:
   ```sh
   # Example: Testing all index-related features
   go test ./cmd/psqldef -run='TestApply/Index.*'
   ```

2. **Test both directions**: When testing schema changes, consider testing both:
   - Adding features (no `current`, only `desired`)
   - Modifying existing schemas (`current` → `desired`)

## General Rules

* Never commit the changes unless the user asks for it.
* Write comments to describe what is not obvious in the code. Describing the "why" is a recommended practice.
* Keep the documents up to date:
  * `cmd-psqldef.md` describes all the features of `psqldef`
  * `cmd-mysqldef.md` describes all the features of `mysqldef`
  * `cmd-sqlite3def.md` describes all the features of `sqlite3def`
  * `cmd-mssqldef.md` describes all the features of `mssqldef`

## Task Completion Checklist

Before considering any task complete, run these commands:

* [ ] `make build`      # Ensure it compiles
* [ ] `make test`       # Run all tests
* [ ] `gofmt -w .`      # Format the code
