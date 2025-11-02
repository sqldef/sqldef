# Generic Parser Migration

## Goal

We are implementing PostgreSQL syntaxes in the generic parser. Once the migration is complete, we will discard the `pgquery` parser.

## Current Status

- **DONE 1006 tests, 100 failures** (`PSQLDEF_PARSER=generic make test-psqldef`)

## Rules

* Keep this document up to date. Don't keep the completed tasks
* Do not modify tests in this migration process. No breaking changes are allowed
* Do not change the behavior by checking `PSQLDEF_PARSER`, which will be removed away after the migration

## Remaining Tasks

### Summary

The 100 test failures fall into these categories:

1. **Missing Parser Features** - 2 items causing ~22 syntax errors
2. **Type Normalization** - 3 items causing ~53 idempotency failures
3. **Expression Normalization** - 3 items causing ~21 idempotency failures
4. **View Normalization** - 1 item causing ~8 idempotency failures
5. **Keyword Case Sensitivity** - 1 item affecting ~12 managed roles tests
6. **Auto-generated Constraint Names** - 2 items causing ~3 failures
7. **Comment Statement Issues** - 2 items causing ~4 failures

Note: Many tests have multiple issues, so the counts overlap.

### Missing Parser Features

1. **Type reference resolution in foreign keys** - Schema-qualified types not handled correctly
   - Example: Generates `public.barsbigint` instead of `bigint`
   - Affects: `ForeignKeyDependenciesForCreateTables`
   - Location: Foreign key reference type resolution code

2. **Reserved keyword handling** - Quoted identifiers using reserved keywords
   - Keywords: "level", "sku", "priority" (when quoted as identifiers)
   - Multiple CREATE TABLE statements failing

### Type Normalization (53 idempotency failures)

1. **Integer type aliases** - Generic parser outputs "int" but PostgreSQL normalizes to "integer"
   - All ADD COLUMN statements with int/integer type
   - Affects: `ForeignKeyDependenciesMultipleToModifiedTable`, etc.
   - Fix: Normalize "int" → "integer" in generator output

2. **Timestamp and time type aliases** - Generic parser outputs short forms
   - Parser outputs: `timestamptz`, `timetz`
   - PostgreSQL expects: `timestamp WITH TIME ZONE`, `time WITH TIME ZONE`
   - Also affects: `timestamp` vs `timestamp WITHOUT TIME ZONE`
   - Affects: `ChangeTimezoneSyntax`, `AddTimestamptzColumnOnNonStandardDefaultSchema`, `AddTimetzColumnOnNonStandardDefaultSchema`
   - Fix: Always output long form with explicit WITH/WITHOUT TIME ZONE

3. **Enum type schema qualification** - Missing or incorrect schema prefix
   - Example: `country` vs `public.country` vs `foo.lang`
   - Parser doesn't preserve or normalize schema qualification
   - Affects: `CreateTypeEnum`, `AddEnumTypeColumnWithExplicitSchemaOnNonStandardDefaultSchema`
   - Fix: Always include schema prefix when outputting enum types

### Expression Normalization

1. **CHECK constraints with IN/ANY/SOME/ALL** - Different representations not normalized
   - PostgreSQL converts: `IN (1,2,3)` → `= ANY (ARRAY[1,2,3])`
   - PostgreSQL preserves: `= SOME (ARRAY[...])`, `= ALL (ARRAY[...])`
   - Generic parser outputs original form, causing mismatch
   - Affects: ~15 tests including `ConstraintCheckInModify`, `ParseAllAnyCheckConstraint`, `SomeConstraintModificationsSomeToAll`
   - Fix: Normalize all forms consistently or parse PostgreSQL's normalized form

2. **Typed literal defaults** - Type prefix in default values
   - PostgreSQL adds type prefix: `date '2024-01-01'` instead of `'2024-01-01'`
   - Affects: `TypedLiterals`, `TypedLiteralsChangeDefault`, `TypedLiteralsIdempotency`
   - Fix: Parse and normalize typed literals (date/time/timestamp prefixes)

3. **EXCLUDE constraint normalization** - Missing USING clause or other parts
   - Example: `EXCLUDE (name WITH =)` vs `EXCLUDE USING GIST (name WITH =)`
   - PostgreSQL adds default USING method
   - Affects: `ExcludeConstraintDropAndAdd`, `ExcludeConstraintChange`, `ExcludeConstraintWithAlterTable`
   - Fix: Parse and output complete EXCLUDE constraint syntax

### View Normalization

1. **VIEW definition normalization** - Views not idempotent after creation
   - PostgreSQL normalizes view definitions (adds casts, rewrites expressions, etc.)
   - Generic parser doesn't normalize to match PostgreSQL's output
   - Affects: ~8 tests including `ViewDDLsAreEmittedLastWithoutChangingDefinition`, `ViewWithGroupByAndHaving`, `CreateViewCast`
   - Fix: Either normalize views in generic parser or fetch normalized definition from database

### Keyword Case Sensitivity

1. **GRANT privilege keywords** - Case mismatch in output
   - Expected: `GRANT SELECT ...` (uppercase)
   - Actual: `GRANT select ...` (lowercase)
   - Affects: `ManagedRolesSpecialCharacters` and ~12 other managed roles tests
   - Fix: Normalize privilege keywords to uppercase in generator

### Auto-generated Constraint Names

1. **Long constraint names** - PostgreSQL truncates to 63 characters differently
   - Parser generates different constraint names than PostgreSQL
   - Affects: `LongAutoGeneratedCheckConstraint`
   - Fix: Implement PostgreSQL's constraint name generation algorithm

2. **UNIQUE constraint naming convention** - Generic parser uses wrong naming convention
   - Generated: `ADD CONSTRAINT "sku" UNIQUE ("sku")`
   - Expected: `ADD CONSTRAINT "products_sku_key" UNIQUE ("sku")`
   - PostgreSQL naming convention: `tablename_columnname_key` for single-column UNIQUE constraints
   - Affects: `ConstraintUniqueAdd`, `ConstraintUniqueRemove`, `ConstraintCheckInAndUniqueAdd`, `ConstraintCheckInAndUniqueRemove` (partially)
   - Note: ADD UNIQUE syntax was fixed in commit 5d1d335 (`ADD CONSTRAINT ... UNIQUE` now correct)
   - Fix: Auto-generate UNIQUE constraint names following PostgreSQL's convention when using generic parser

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
