# Development Guide

This project provides four schema management commands:

- `mysqldef` - MySQL schema management (mimics `mysql` CLI options)
- `psqldef` - PostgreSQL schema management (mimics `psql` CLI options)
- `mssqldef` - SQL Server schema management (mimics `sqlcmd` CLI options)
- `sqlite3def` - SQLite3 schema management (mimics `sqlite3` CLI options)

Each command follows the same pattern: it accepts connection parameters similar to those of the corresponding database CLI tool and applies schema changes idempotently to match the desired state.

## General Rules

* Never commit changes unless the user explicitly requests it
* Only write comments to explain non-obvious code. Focus on explaining the "why" rather than the "what"
* Format SQL in string literals
* Use `log/slog` to trace internal state. Set `LOG_LEVEL=debug` to enable debug logging
* Use `panic` to assert unreachable code paths
* Be aware of the two escaping modes:
  * `legacy_ignore_quotes: true` (default; backward-compatible) generates SQL with identifiers always quoted
  * `legacy_ignore_quotes: false` (quote-aware) generates SQL with identifiers quoted only when they are quoted in the input SQL
* If you encounter an unsupported feature, don't rewrite tests to avoid it. Instead, comment out the test case and mark it as `FIXME`

## Build

Build all the sqldef commands (`mysqldef`, `psqldef`, `sqlite3def`, `mssqldef`):

```sh
make build
```

The executable binaries will be placed in the `build/$os-$arch/` directory, where `$os` is `go env GOOS` and `$arch` is `go env GOARCH`.

### The Parser

To update the generic SQL parser, edit `parser/parser.y` and regenerate:

```sh
make parser    # generate parser/parser.go from parser/parser.y
make parser-v  # same as above, also writes a conflict report to y.output
```

Requirements:
- No reduce/reduce conflicts are allowed
- Do not introduce new shift/reduce conflicts unless absolutely necessary
- To resolve conflicts, use `make parser-v` and inspect `y.output`

Usage notes:
- `psqldef` uses the **generic parser** by default with fallback to `go-pgquery` (native PostgreSQL parser)
- During development, always set `PSQLDEF_PARSER=generic`:
  - `PSQLDEF_PARSER=generic` - Use only the generic parser (no fallback to pgquery)
  - `PSQLDEF_PARSER=pgquery` - Use only the pgquery parser (no fallback to generic)
  - Not set (default) - Use generic parser with fallback to pgquery
- The generic parser builds ASTs that the generator uses for normalization and comparison. Do not parse SQL with regular expressions
- The pgquery parser is obsolete and will be removed in the future; no maintenance is needed
- Map iteration order is non-deterministic. Use `util.CanonicalMapIter` to iterate maps in a deterministic order

## Local Development

For local iteration, the typical workflow is:

```sh
# Pick a tool/target (where $os is `go env GOOS` and $arch is `go env GOARCH`):
TOOL="build/$(go env-$arch/psqldef psqldef_test"
# TOOL="build/$os-$arch/mysqldef mysqldef_test"
# TOOL="build/$os-$arch/sqlite3def sqlite3def.db"
# TOOL="build/$os-$arch/mssqldef -PPassw0rd mssqldef_test" # password is mandatory

# Export current schema
$TOOL --export > schema.sql

# Preview changes (dry run)
$TOOL --dry-run --file schema.sql

# Apply schema from file
$TOOL --apply --file schema.sql
```

## Running Tests

For development iterations, use these commands to run tests:

### Run all tests

```sh
make test  # Takes approximately 10 minutes to complete
```

### Run tests for a specific sqldef tool

The test runner is `gotestsum`, which is a wrapper around `go test` that provides a more readable output.

```sh
go test ./cmd/mysqldef
go test ./cmd/psqldef
go test ./cmd/sqlite3def
go test ./cmd/mssqldef
```

For pgvector testing:

```sh
PG_FLAVOR=pgvector PGPORT=55432 go test ./cmd/psqldef
```

For MariaDB testing:

```sh
MYSQL_FLAVOR=mariadb MYSQL_PORT=3307 go test ./cmd/mysqldef
```

For TiDB testing:

```sh
MYSQL_FLAVOR=tidb MYSQL_PORT=4000 go test ./cmd/mysqldef
```

If you encounter `tls: handshake failure` errors with MySQL 5.7, enable RSA key exchange:

```sh
GODEBUG=tlsrsakex=1 go test ./cmd/mysqldef
```

The tests for mssqldef are flaky due to SQL Server instance issues. In that case, restart it with `docker compose down mssql && docker compose up -d --wait mssql`, then run the tests again.

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

The test name pattern follows the format `TestApply/<TestCaseName>`, where `<TestCaseName>` matches the test case names defined in the YAML test files.

### YAML test files

For schema management tests, you typically only need to edit the YAML test files.

The test files use a YAML format where each top-level key is a test case name and the value is a `TestCase` object. A JSON schema is available at `./testutil/testcase.schema.json` for IDE autocomplete and validation.

Test case fields:

```yaml
# yaml-language-server: $schema=../../testutil/testcase.schema.json

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

  # Database flavor requirement for flavor-specific features
  # One of "mysql", "mariadb", "tidb" for mysqldef, and "pgvector" for psqldef
  # Supports positive and negative matching like "!tidb" to exclude TiDB
  # See "Flavor Behavior" section below for details
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

  # Quote handling mode
  # true = ignore quotes (incorrectly ignore case-sensitivity; legacy default)
  # false = preserve quotes (correctly handle case-sensitivity; future default)
  legacy_ignore_quotes: false

  # Configuration options for the test
  config:
    # Create indexes concurrently (psqldef only)
    create_index_concurrently: true
```

The `up` and `down` fields must both be specified or both be omitted:
- Both specified: asserts `current → desired` produces `up` and `desired → current` produces `down`
- Both omitted: idempotency-only test; DDLs are applied but not asserted (must be valid SQL unless `offline: true`)

#### Flavor Behavior

The `flavor` field controls flavor-specific test behavior, which validates that tests correctly fail on non-matching flavors:

| Scenario | Result |
|----------|--------|
| Flavor matches, test passes | PASS |
| Flavor matches, test fails | FAIL |
| Flavor doesn't match, test fails | SKIP |
| Flavor doesn't match, test passes | FAIL |

This design ensures that flavor annotations are accurate. If you add `flavor: mariadb` to a test, the test must actually fail on MySQL/TiDB. If it passes, the flavor annotation is wrong and should be removed.

#### Notes for Writing Tests

* YAML test cases default to `enable_drop: true`, which differs from the default behavior of sqldef tools
* Never use `offline: true` for databases tested in GitHub Actions:
  - MySQL, MariaDB, and TiDB
  - PostgreSQL
  - SQLite3
  - SQL Server
* Add `legacy_ignore_quotes: false` for new test cases. This is the default behavior in the future.
* Do not add trivial comments to test cases. Instead, describe what is tested in test case names.

## Documentation

Markdown files document the usage of each command. Keep them up to date:

* `cmd-psqldef.md` for `psqldef`
* `cmd-mysqldef.md` for `mysqldef`
* `cmd-sqlite3def.md` for `sqlite3def`
* `cmd-mssqldef.md` for `mssqldef`

## Task Completion Checklist

Before considering a task complete, run these commands to ensure the code is in a good state:

* `make build`
* `make test-all-flavors`
* `make format`
* `make lint`
