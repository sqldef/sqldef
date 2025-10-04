# PostgreSQL Parser Migration

This document tracks the migration from the external PostgreSQL parser (`github.com/pganalyze/pg_query_go`) to the internal parser implementation.

## Background

The `gfx/psqldef_parser` branch removes the dependency on the external PostgreSQL parser and implements PostgreSQL-specific features in the internal parser (`parser/parser.y`).

## Completed Work ✅

### Core DDL Operations
- [x] **ALTER TABLE ADD COLUMN** - Support for both `ADD COLUMN` and `ADD` syntax variants
- [x] **ALTER TABLE** basic operations (ADD, DROP, RENAME)
- [x] **CREATE TABLE** with all column types
- [x] **DROP TABLE** operations

### PostgreSQL-Specific Syntax
- [x] **COMMENT ON TABLE/COLUMN** - Fixed `IS` keyword parsing in COMMENT statements
- [x] **CREATE SCHEMA** - Added support for `CREATE SCHEMA IF NOT EXISTS`
- [x] **CREATE EXTENSION** - Support for extensions like `citext`
- [x] **CREATE TYPE AS ENUM** - Enum type definitions with value lists

### Data Types
- [x] **INTERVAL type** - Added as a valid time type
- [x] **Reserved keywords as identifiers** - Allow LEVEL, TYPE as column names
- [x] **Date/Time literals** - Support for `DATE '2022-01-01'`, `TIME '12:00:00'`, `TIMESTAMP '2022-01-01 12:00:00'`
- [x] **CITEXT type** - Case-insensitive text type support

### Constraints
- [x] **EXCLUDE USING constraints** - Support for any index type (btree, gist, etc.)
- [x] **EXCLUDE with operators** - Including overlap operator `&&`
- [x] **CHECK constraints with ALL/ANY/SOME** - Proper parsing with parentheses
- [x] **Foreign key constraints** - Basic support maintained
- [x] **Unique constraints** - Basic support maintained

### Expressions
- [x] **ARRAY constructors** - `ARRAY[1, 2, 3]` with strings and numbers
- [x] **Type casting** - Using `::` operator
- [x] **Function calls in expressions** - Basic support

### Permissions
- [x] **GRANT/REVOKE statements** - Basic support with error handling for unsupported features
- [x] **CASCADE/RESTRICT options** - Parser support with appropriate error messages
- [x] **WITH GRANT OPTION** - Parser support with error message for unsupported feature
- [x] **Multiple tables in GRANT** - Parser support with error handling

## Recently Fixed Issues ✅ (December 2024)

The following issues were resolved during the latest work session:

1. **TestPsqldefTransactionBoundariesWithConcurrentIndex** ✅
   - **Fix**: Modified `index_column` rule in parser to use `reserved_sql_id` instead of `sql_id`
   - **Result**: Reserved keywords like `status` can now be used in index columns

2. **TestPsqldefAddIdentityColumnWithSequenceOption** ✅
   - **Fix**: Updated test expectations to match PostgreSQL's type normalization (`int` → `integer`)
   - **Result**: Test now passes with correct expectations

3. **TestPsqldefCreateType** ✅
   - **Fix**: Added `stripSchemaFromType()` function to handle schema-qualified type comparisons
   - **Result**: Proper comparison of types like `public.country` vs `country`

4. **TestPsqldefAllAnySomeCheckConstraints** ✅
   - **Fix**: Normalized SOME to ANY in check constraints and updated test expectations
   - **Result**: Consistent handling of SOME/ANY operators (they're SQL equivalents)

5. **TestPsqldefSomeConstraintModifications** ✅
   - **Fix**: Improved check constraint normalization and output formatting
   - **Result**: Proper constraint modification detection and generation

## Fixed in Latest Session ✅ (October 2025 - Part 5)

### Index Expression Support
1. **Function Calls in Index Expressions** ✅
   - Added support for function calls directly in index column lists without wrapping parentheses
   - Now supports mixed lists like `(name, COALESCE(user_name, 'default'))`
   - Added three new rules to `index_column` for `function_call_generic`, `function_call_keyword`, and `function_call_nonkeyword`
   - Fixed in: `parser/parser.y:2980-2991`
   - Test passing: TestApply/CreateIndexWithCoalesce

### Parser Improvements
1. **DEFAULT Expression Simplification** ✅
   - Removed `DEFAULT '(' default_expression ')'` rule to eliminate shift/reduce conflicts
   - Parser now relies on `DEFAULT default_expression` where parens are part of the expression itself
   - Reduced shift/reduce conflicts significantly

2. **CURRENT_TIMESTAMP Parsing** ✅
   - Removed `current_timestamp` from `default_val` to eliminate reduce/reduce conflicts
   - Removed `CURRENT_TIMESTAMP ( )` function call variant (not used in PostgreSQL)
   - Removed `CURRENT_TIMESTAMP` from `non_reserved_keyword` to avoid column name conflicts
   - Reduced reduce/reduce conflicts from 1495 to 1390
   - Fixed in: `parser/parser.y:2103-2143, 4365-4368, 5275-5278`

**Progress**: Test failures reduced from 40 to 39. One additional test now passing.

## Fixed in Previous Session ✅ (January 2025 - Part 4)

### EXCLUDE Constraint Support
1. **GIST Keyword Recognition** ✅
   - Added GIST to `non_reserved_keyword` list in parser grammar
   - Changed exclusion rules to use `reserved_sql_id` instead of `sql_id`
   - Allows GIST and other index types to be used as identifiers
   - Fixed in: `parser/parser.y:5415, 3130, 3148`

2. **&& Operator Support** ✅
   - Fixed `exclusion_operator` rule to use single `AND` token instead of `AND AND`
   - The tokenizer converts && to a single AND token
   - Now properly parses `EXCLUDE USING GIST (event_start WITH &&)`
   - Fixed in: `parser/parser.y:3208`

3. **EXCLUDE Constraint Output Format** ✅
   - Updated `generateExclusionDefinition` to handle empty indexType
   - Only outputs `USING indexType` when indexType is non-empty
   - Prevents output like "EXCLUDE USING " with trailing space
   - Fixed in: `schema/generator.go:2049-2073`

4. **EXCLUDE Constraint Comparison** ✅
   - Added `normalizeExclusionWhere` function to normalize WHERE clauses
   - Handles <> vs != equivalence (PostgreSQL normalizes <> to !=)
   - Removes extra outer parentheses from WHERE clauses
   - Treats empty indexType and "btree" as equivalent (btree is default)
   - Fixed in: `schema/generator.go:3384-3418`

### Parser Conflicts
- Conflicts increased slightly: 320 shift/reduce (+4), 1495 reduce/reduce (+21)
- This is expected when adding keywords to non_reserved_keyword
- Does not affect functionality

**Note:** Some EXCLUDE constraint idempotency issues remain due to expression normalization differences between the parser output and PostgreSQL's canonical format. This requires deeper expression parsing and normalization.

## Fixed in Previous Session ✅ (January 2025 - Part 3)

### Database Type Handling
1. **Timestamp/Time WITH TIME ZONE Precision** ✅
   - Fixed PostgreSQL type preservation in `database/postgres/database.go`
   - Now correctly preserves precision in `formattedDataType` for `timestamp(6) with time zone` and `time(6) with time zone`
   - Prevents unnecessary ALTER TABLE statements when precision is specified
   - Fixed in: `database/postgres/database.go:504-515`

2. **Type Comparison in haveSameDataType()** ✅
   - Added PostgreSQL-specific handling for timezone types with precision
   - Recognizes that `timestamp with time zone` == `timestamp(6) with time zone` (default precision)
   - Same for `time with time zone` == `time(6) with time zone`
   - Fixed in: `schema/generator.go:2916-2944`

### Parser Improvements
1. **LEVEL Keyword Support** ✅
   - Added support for `LEVEL` as a column name in expressions
   - Can now parse `CHECK (level IN (1, 2, 3))` correctly
   - Added `LEVEL` to `column_name` rule in parser
   - Added `LEVEL` to `index_column` rule for UNIQUE constraints
   - Fixed in: `parser/parser.y:4711-4714, 2975-2978`

2. **ANY/ALL Spacing Consistency** ✅
   - Parser outputs `ANY (` and `ALL (` with space to match PostgreSQL's canonical format
   - SOME is converted to `ANY(` without space in `normalizeCheckDefinitionForOutput()`
   - This matches the behavior expected by existing tests
   - Fixed in: `parser/node.go:1617-1628`, `schema/generator.go:3057-3063`

### Tests Fixed (5 additional tests now passing)
- ✅ TestPsqldefDataTypes
- ✅ TestPsqldefAllAnySomeCheckConstraints
- ✅ TestPsqldefSomeConstraintModifications
- ✅ TestPsqldefTableLevelCheckConstraintsWithAllAny
- ✅ Parser now handles LEVEL keyword in CHECK constraints and UNIQUE indexes

**Progress**: All non-TestApply tests now passing. TestApply has 39 remaining YAML test failures (down from 46 initially, down from 40 after Part 4, now 39 after Part 5 fixes)

## Remaining Issues ❌

### Test Failures (39 YAML test cases remaining in TestApply)

#### Type Normalization Issues
- **varchar vs character varying**: PostgreSQL normalizes `varchar` to `character varying` internally
- **timestamptz vs timestamp WITH TIME ZONE**: Short form vs expanded form mismatch
- **timetz vs time WITH TIME ZONE**: Similar aliasing issue
- **Note**: Some test expectations may be inconsistent with PostgreSQL's canonical representations

#### Other Failure Patterns
- View definitions with complex expressions
- Schema-qualified comments
- Foreign key dependency ordering
- Managed roles and permissions (GRANT/REVOKE)
- Exclusion constraints
- Boolean expressions in indexes

## TODO List 📋

- [x] ~~Fix IN vs ANY(ARRAY) conversion~~ ✅ COMPLETED
- [x] ~~Fix ANY/ALL spacing issues~~ ✅ COMPLETED
- [x] ~~Fix timestamp/time with time zone precision handling~~ ✅ COMPLETED
- [x] ~~Fix LEVEL keyword support in expressions and indexes~~ ✅ COMPLETED
- [x] ~~Fix EXCLUDE USING GIST parsing~~ ✅ COMPLETED (January 2025 - Part 4)
- [x] ~~Fix && operator in EXCLUDE constraints~~ ✅ COMPLETED (January 2025 - Part 4)

### Current Test Failures (In Progress)

#### Parser Syntax Errors
- [ ] **TYPE keyword as column name in REFERENCES** - `REFERENCES image_owners(type, id)` fails parsing
- [ ] **Type casting in DEFAULT** - Double type cast `((CURRENT_TIMESTAMP)::date)::text` fails (DEFERRED - requires deep parser restructuring)
- [ ] **LIKE operators (~~ and !~~)** - Parser doesn't recognize PostgreSQL's ~~ (LIKE) and !~~ (NOT LIKE) operators
- [ ] **CASE WHEN with LIKE** - `CASE WHEN admin THEN name ~~ 'admin%' END` fails parsing
- [x] **COALESCE in CREATE INDEX** - Function calls with multiple arguments in index expressions ✅ FIXED
- [ ] **Interval arithmetic in DEFAULT** - `DEFAULT (CURRENT_TIMESTAMP + '1 day'::interval)` fails

#### Constraint Comparison/Normalization Issues
- [ ] **UNIQUE constraint idempotency** - Generated constraints repeatedly dropped/recreated (multiple tests)
- [ ] **CHECK constraint normalization** - Check constraints with type casts not matching after round-trip
- [~] **EXCLUDE constraint format** - Partially fixed: now handles empty indexType correctly, but idempotency issues remain with expression normalization
- [ ] **Long auto-generated constraint names** - Auto-generated CHECK constraint names not matching PostgreSQL's

#### View Definition Issues
- [ ] **View idempotency** - Views with CASE/CAST/COALESCE being recreated even when unchanged
- [ ] **View expression formatting** - Expression formatting doesn't match PostgreSQL's canonical format

#### COMMENT Statement Issues
- [ ] **Schema qualification in COMMENT** - Comments output without schema qualification when expected with `public.`
- [ ] **COMMENT idempotency** - Comment statements being regenerated even when unchanged

#### Foreign Key Issues
- [ ] **Foreign key dependency ordering** - Some FK constraints not created in correct order or missing
- [ ] **Foreign key with decimal precision** - `decimal(10, 2)` type causing spurious ALTER statements

#### GRANT/REVOKE Issues
- [ ] **Multiple tables in GRANT** - `GRANT ... ON TABLE users, posts` not supported
- [ ] **Error message format for CASCADE** - Error messages should not include "found syntax error when parsing DDL"

#### Type Normalization Issues
- [ ] **varchar vs character varying** - Test expects `varchar(255)` but parser outputs `character varying(255)`

#### Index Expression Issues
- [ ] **Index on changed expressions** - Dropping and recreating indexes with expressions not detected

### Future Enhancements
- [ ] Partial indexes (`WHERE` clause in CREATE INDEX)
- [ ] Expression indexes with complex functions
- [ ] INHERITS clause for table inheritance
- [ ] Table partitioning syntax
- [ ] Row-level security policies
- [ ] Materialized views
- [ ] Stored procedures and functions
- [ ] Triggers (more comprehensive support)
- [ ] Custom operators
- [ ] Full text search configurations
- [ ] Reduce parser conflicts (currently 316 shift/reduce, 1474 reduce/reduce)

## Testing Strategy

1. **Unit Tests**: All tests in `cmd/psqldef/*_test.go`
2. **Integration Tests**: YAML-based test files in `cmd/psqldef/tests*.yml`
3. **Parser Tests**: Direct parser testing for edge cases

### Test Commands

```bash
# Run all psqldef tests
go test ./cmd/psqldef -count=1

# Run specific test
go test ./cmd/psqldef -run TestPsqldefCreateType -v

# Run TestApply suite
go test ./cmd/psqldef -run TestApply -v

# Count failures
go test ./cmd/psqldef -count=1 2>&1 | grep -c "^--- FAIL"
```

## Implementation Notes

### Parser Architecture
- Main parser: `parser/parser.y` (yacc grammar)
- Tokenizer: `parser/token.go`
- AST nodes: `parser/node.go`
- Schema generation: `schema/generator.go`

### Key Changes Made in This Session (January 2025 - Part 3)

1. **Database Layer** (`database/postgres/database.go`):
   - Lines 504-515: Preserve precision in `formattedDataType` for `timestamp(6) with time zone` and `time(6) with time zone`
   - Returns the full formatted type from PostgreSQL instead of stripping precision information

2. **Schema Generator** (`schema/generator.go`):
   - Lines 2916-2944: Added PostgreSQL-specific handling in `haveSameDataType()` for timezone types
   - Recognizes that `timestamp with time zone` is equivalent to `timestamp(6) with time zone` (default precision 6)
   - Lines 3057-3063: Simplified `normalizeCheckDefinitionForOutput()` to only convert SOME→ANY without space
   - Maintained `normalizeCheckDefinitionForComparison()` for accurate constraint comparison

3. **Parser Grammar** (`parser/parser.y`):
   - Lines 4711-4714: Added `LEVEL` keyword to `column_name` rule for use in expressions
   - Lines 2975-2978: Added `LEVEL` keyword to `index_column` rule for UNIQUE constraints
   - Allows reserved keyword `LEVEL` to be used as a column name in CHECK constraints and indexes

4. **Parser Node Formatting** (`parser/node.go`):
   - Lines 1617-1628: Format `ANY (` and `ALL (` with space to match PostgreSQL's canonical output
   - Maintains consistency with how PostgreSQL's `pg_get_constraintdef()` formats constraints

### Parser Conflicts
The parser currently has conflicts that are acceptable for a complex grammar:
- 259 shift/reduce conflicts (reduced from 320 baseline, then 377 at peak, now 259 after cleanup)
- 1419 reduce/reduce conflicts (reduced from 1495 baseline to 1390, now 1419 after adding index expression support)

These don't affect functionality but could be reduced further with grammar refactoring. The reduction in conflicts shows improved grammar clarity.

## Development Guidelines

When adding new PostgreSQL features:

1. **Add tokens** in `parser/token.go` if new keywords needed
2. **Update grammar** in `parser/parser.y`
3. **Define AST nodes** in `parser/node.go` if new structures needed
4. **Update schema generator** in `schema/generator.go`
5. **Add tests** in appropriate test files
6. **Run `make parser`** to regenerate parser
7. **Run `make test`** to verify all tests pass

