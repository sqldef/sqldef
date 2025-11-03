# Generic Parser Migration

## Goal

We are implementing PostgreSQL syntaxes in the generic parser. Once the migration is complete, we will discard the `pgquery` parser.

## Current Status

- **1006 tests, 3 failures** (`PSQLDEF_PARSER=generic make test-psqldef`)

## Rules

* Keep this document up to date. Don't keep the completed tasks
* Do not modify tests in this migration process. No breaking changes are allowed
* Do not change the behavior by checking `PSQLDEF_PARSER`, which will be removed away after the migration
* `pgquery` parser might normalize AST in a wrong way, which should be fixed in this migration process (commit cadcee36b9ed3fbb1a185262cc8881ca53d409d4 for example)

## Remaining Tasks

### Failing Tests (3 total)

1. `ChangeDefaultExpressionWithAddition`
2. `CreateTableWithConstraintOptions`
3. `CreateTableWithDefault`

### Summary by Category

1. **Miscellaneous** - 3 failures

### Miscellaneous

1. **Default expression with addition** - Expression not normalized
   - Affects: `ChangeDefaultExpressionWithAddition`
   - Fix: Normalize arithmetic expressions in defaults

2. **Default values in CREATE TABLE** - Not idempotent
   - Affects: `CreateTableWithDefault`
   - Fix: Normalize default value representation

3. **Constraint options** - DEFERRABLE/NOT DEFERRABLE not handled correctly
   - Affects: `CreateTableWithConstraintOptions`
   - Fix: Parse and generate constraint options correctly for non-FK constraints
