# Generic Parser Migration

## Goal

We are implementing PostgreSQL syntaxes in the generic parser. Once the migration is complete, we will discard the `pgquery` parser.

## Current Status

- **1006 tests, 1 failure** (`PSQLDEF_PARSER=generic make test-psqldef`)

## Rules

* Keep this document up to date. Don't keep the completed tasks
* Do not modify tests in this migration process. No breaking changes are allowed
* Do not change the behavior by checking `PSQLDEF_PARSER`, which will be removed away after the migration
* `pgquery` parser might normalize AST in a wrong way, which should be fixed in this migration process (commit cadcee36b9ed3fbb1a185262cc8881ca53d409d4 for example)
* `make test` must pass (i.e. no regressions are allowed)

## Remaining Tasks

### Failing Tests (1 total)

1. `ChangeDefaultExpressionWithAddition`

### Summary by Category

1. **Default expressions** - 1 failure

### Default Expressions

1. **Default expression with interval addition** - Missing `::interval` typecast
   - Affects: `ChangeDefaultExpressionWithAddition`
   - Issue: `current_timestamp + '3 days'` should generate `current_timestamp + '3 days'::interval`
   - Fix: Preserve type casts in binary expressions within DEFAULT clauses
