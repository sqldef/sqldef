# Generic Parser Migration

## Goal

We are implementing PostgreSQL syntaxes in the generic parser. Once the migration is complete, we will discard the `pgquery` parser.

## Current Status

- **1006 tests, 14 failures** (`PSQLDEF_PARSER=generic make test-psqldef`)

## Rules

* Keep this document up to date. Don't keep the completed tasks
* Do not modify tests in this migration process. No breaking changes are allowed
* Do not change the behavior by checking `PSQLDEF_PARSER`, which will be removed away after the migration
* `pgquery` parser might normalize AST in a wrong way, which should be fixed in this migration process (commit cadcee36b9ed3fbb1a185262cc8881ca53d409d4 for example)

## Remaining Tasks

### Failing Tests (14 total)

1. `ChangeDefaultExpressionWithAddition`
2. `ChangeMultiDimensionalArrayDefault`
3. `CheckConstraint`
4. `CommentUnset`
5. `CommentWithoutSchema`
6. `CommentWithoutSchemaWithoutTableNameQuoted`
7. `CommentWithoutSchemaWithTableNameQuoted`
8. `CreateTableWithConstraintOptions`
9. `CreateTableWithDefault`
10. `ManagedRolesErrorCascade`
11. `ManagedRolesErrorGrantOption`
12. `MultiDimensionalArrayWithDefaultEmpty`
13. `NegativeDefaultNumbers`
14. `TypedLiteralsInCheckWithCast`

### Summary by Category

1. **Comment Statement Issues** - 4 failures
2. **Array Default Issues** - 2 failures
3. **Check Constraint Issues** - 2 failures
4. **Managed Roles Issues** - 2 failures
5. **Miscellaneous** - 4 failures

### Comment Statement Issues

1. **COMMENT schema qualification** - Missing schema prefix in COMMENT statements
   - Generated: `COMMENT ON TABLE users IS ...`
   - Expected: `COMMENT ON TABLE public.users IS ...`
   - Affects: `CommentUnset`, `CommentWithoutSchema`, `CommentWithoutSchemaWithoutTableNameQuoted`, `CommentWithoutSchemaWithTableNameQuoted`
   - Fix: Always include schema prefix in COMMENT statements

2. **COMMENT IS NULL not generated** - Comment removal statements not output
   - When comments need to be removed, should generate: `COMMENT ON TABLE table IS NULL;`
   - Currently generates: nothing (empty string)
   - Affects: `CommentUnset`
   - Fix: Detect when comments are removed and generate COMMENT ... IS NULL statements

### Array Default Issues

1. **Multi-dimensional array defaults** - Not handled correctly
   - Array defaults with multiple dimensions or empty arrays
   - Affects: `ChangeMultiDimensionalArrayDefault`, `MultiDimensionalArrayWithDefaultEmpty`
   - Fix: Parse and generate multi-dimensional array defaults correctly

### Check Constraint Issues

1. **Check constraint with typed literals and casts** - Not handled correctly
   - Check constraints with complex type casts and literal formats
   - Affects: `CheckConstraint`, `TypedLiteralsInCheckWithCast`
   - Fix: Normalize typed literals and cast expressions in CHECK constraints

### Managed Roles Issues

1. **Managed roles error handling** - CASCADE and GRANT OPTION not properly validated
   - When using managed roles, certain grant operations should error
   - Affects: `ManagedRolesErrorCascade`, `ManagedRolesErrorGrantOption`
   - Fix: Implement proper validation for managed roles feature

### Miscellaneous

1. **Default expression with addition** - Expression not normalized
   - Affects: `ChangeDefaultExpressionWithAddition`
   - Fix: Normalize arithmetic expressions in defaults

2. **Negative default numbers** - Not parsed correctly
   - Affects: `NegativeDefaultNumbers`
   - Fix: Handle negative numbers in default values

3. **Default values in CREATE TABLE** - Not idempotent
   - Affects: `CreateTableWithDefault`
   - Fix: Normalize default value representation

4. **Constraint options** - DEFERRABLE/NOT DEFERRABLE not handled correctly
   - Affects: `CreateTableWithConstraintOptions`
   - Fix: Parse and generate constraint options correctly for non-FK constraints
