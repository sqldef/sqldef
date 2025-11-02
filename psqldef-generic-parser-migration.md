# Generic Parser Migration

## Goal

We are implementing PostgreSQL syntaxes in the generic parser. Once the migration is complete, we will discard the `pgquery` parser.

## Current Status

- **DONE 1006 tests, 101 failures** (`PSQLDEF_PARSER=generic make test-psqldef`)

## Remaining Tasks

### Summary

The 101 test failures fall into these categories:

1. **Missing Parser Features** - 7 items causing ~28 syntax errors
2. **Type Normalization** - 3 items causing ~53 idempotency failures
3. **Expression Normalization** - 3 items causing ~21 idempotency failures
4. **View Normalization** - 1 item causing ~8 idempotency failures
5. **Keyword Case Sensitivity** - 1 item affecting ~12 managed roles tests (overlaps with GRANT REFERENCES)
6. **Auto-generated Constraint Names** - 1 item causing 1 failure
7. **Comment Statement Issues** - 2 items causing ~4 failures

Note: Many tests have multiple issues, so the counts overlap.

### Missing Parser Features

1. **Type reference resolution in foreign keys** - Schema-qualified types not handled correctly
   - Example: Generates `public.barsbigint` instead of `bigint`
   - Affects: `ForeignKeyDependenciesForCreateTables`
   - Location: Foreign key reference type resolution code

2. **GRANT statement with REFERENCES privilege** - Parser doesn't support REFERENCES keyword
   - Example: `GRANT DELETE, INSERT, REFERENCES, SELECT ...`
   - Error: `syntax error at line 1, column 34 near 'references'`
   - Affects: `ManagedRolesAllPrivileges` and related tests

3. **Multi-word type names in casts** - Two-word type names in cast expressions
   - Examples:
     - `((expression)::bigint)::double precision`
     - `''::character varying`
     - `'JPN'::bpchar`
   - Error: `syntax error at line 1, column 448 near 'double'`
   - Affects: `CreateViewWithCaseWhen`, `CreateTableWithDefault`

4. **CREATE EXTENSION parsing** - Extension names not recognized
   - Example: `CREATE EXTENSION citext;`
   - Error: `syntax error at line 1, column 25 near 'citext'`

5. **Reserved keyword handling** - Quoted identifiers using reserved keywords
   - Keywords: "level", "sku", "priority" (when quoted as identifiers)
   - Multiple CREATE TABLE statements failing

6. **Pattern matching operators** - PostgreSQL-specific operators not supported
   - Example: `~~` (LIKE), `~~*` (ILIKE), `!~~` (NOT LIKE), `!~~*` (NOT ILIKE)
   - Error: `syntax error at line 6, column 29` in CASE expressions
   - Affects: `CaseWithoutArgument`

7. **ADD UNIQUE syntax** - Incorrect UNIQUE constraint generation
   - Generated: `ADD UNIQUE "constraint_name" (columns)`
   - Expected: `ADD CONSTRAINT "constraint_name" UNIQUE (columns)`
   - Affects: `ConstraintCheckInAndUniqueAdd`, `ConstraintCheckInAndUniqueRemove`, `ConstraintUniqueAdd`, `ConstraintUniqueRemove`

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
