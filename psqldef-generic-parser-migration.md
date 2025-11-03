# Generic Parser Migration

## Goal

We are implementing PostgreSQL syntaxes in the generic parser. Once the migration is complete, we will discard the `pgquery` parser.

## Current Status

- **1006 tests, 2 failures** (`PSQLDEF_PARSER=generic make test-psqldef`)

## Rules

* Keep this document up to date. Don't keep the completed tasks
* Do not modify tests in this migration process. No breaking changes are allowed
* Do not change the behavior by checking `PSQLDEF_PARSER`, which will be removed away after the migration
* `pgquery` parser might normalize AST in a wrong way, which should be fixed in this migration process (commit cadcee36b9ed3fbb1a185262cc8881ca53d409d4 for example)
* `make test` must pass (i.e. no regressions are allowed)

## Remaining Tasks

### Failing Tests (2 total)

1. `ChangeDefaultExpressionWithAddition`
2. `CreateTableWithDefault`

### Summary by Category

1. **Default expressions** - 2 failures

### Default Expressions

1. **Default expression with interval addition** - Missing `::interval` typecast
   - Affects: `ChangeDefaultExpressionWithAddition`
   - Issue: `current_timestamp + '3 days'` should generate `current_timestamp + '3 days'::interval`
   - Fix: Preserve type casts in binary expressions within DEFAULT clauses

2. **Array element typecast in defaults** - Parser fixes applied, normalization issue remains
   - Affects: `CreateTableWithDefault`
   - Parser fix: Added `tuple_expression` support in `array_element` grammar rule to parse `ARRAY[(CURRENT_DATE)::text]`
   - Remaining issue: PostgreSQL normalizes `ARRAY[current_date::text]::text[]` to `ARRAY[(CURRENT_DATE)::text]` (drops redundant array typecast when column type is already `text[]`)
   - This is a schema normalization issue, not a parser issue
   - Next step: Add normalization logic to strip redundant array typecasts when column type matches
