# Development Guide

This project provides four schema management commands:

- **mysqldef** - MySQL schema management (mimics `mysql` CLI options)
- **psqldef** - PostgreSQL schema management (mimics `psql` CLI options)
- **mssqldef** - SQL Server schema management (mimics SQL Server CLI options)
- **sqlite3def** - SQLite3 schema management (mimics `sqlite3` CLI options)

Each command follows the same pattern: it accepts connection parameters similar to those of the corresponding database CLI tool and applies schema changes idempotently.

## Building Commands

Build all `*def` commands:

```bash
make build
```

Build individual commands:

```bash
make build-mysqldef
make build-psqldef
make build-sqlite3def
make build-mssqldef
```

The compiled binaries will be placed in the `build/<os>-<arch>/` directory.

## Running Tests

For development iterations, use these commands to run tests:

### Run all tests

```bash
make test
```

### Run tests for specific `*def` tools

```bash
go test ./cmd/mysqldef
go test ./cmd/psqldef
go test ./cmd/sqlite3def
go test ./cmd/mssqldef
```

### Run individual tests

Use the `-run` flag with a regex pattern to run specific test cases:

```bash
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
   ```bash
   # Example: Testing all index-related features
   go test ./cmd/psqldef -run=TestApply/Index.*
   ```

2. **Test both directions**: When testing schema changes, consider testing both:
   - Adding features (no `current`, only `desired`)
   - Modifying existing schemas (`current` → `desired`)

## General Rules

* Never commit the changes unless the user asks for it.

## Task Completion Checklist

Before considering any task complete, run these commands:

* [ ] `make build`      # Ensure it compiles
* [ ] `make test`       # Run all tests
* [ ] `gofmt -w .`      # Format the code
