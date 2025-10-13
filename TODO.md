# TODO: Migration from pgquery to generic parser

This document tracks the progress of migrating from `pgquery` to `generic` parser.

## Principle of Operation

* We are migrating from `pgquery` to `generic` parser, discarding `pgquery` in the future.
* However, `pgquery` will be kept for a while as a fallback parser.
* If something conflicts, the generic parser's way is correct. Update `pgquery` stuff to adjust to the generic parser's way.
* You can add test cases to debug, but never modify the existing test cases.
* `parser/parser_test.go` is the test cases for the generic parser. Use it to develop the parser stuff.
* When you add `slog.Debug()` for debugging, you don't need to remove them after the task is done.
* Use AST as much as possible, instead of string manipulation.
* Add `TODO` comments when the solution may not be optimal.
* Keep `TODO.md` up-to-date, just removing completed tasks, instead of marking them as done.

## Test

`PSQLDEF_PARSER=generic` to use the generic parser. `PSQLDEF_PARSER=pgquery` to use the pgquery parser. Otherwise, `psqldef` uses `generic` as the primary parser, and the `pgquery` as the fallback.

Eventually, both `PSQLDEF_PARSER=generic make test-psqldef` and `PSQLDEF_PARSER=pgquery make test-psqldef` must pass, as well as `make test-mysqldef`, `make test-sqlite3def`, `make test-mssqldef`.

## Current Test Status

### All Databases - ✅ ALL TESTS PASS
- `make test-mysqldef`: All tests pass
- `make test-psqldef`: All tests pass (both default and `PSQLDEF_PARSER=generic`)
- `make test-sqlite3def`: All tests pass
- `make test-mssqldef`: All tests pass
- `make test`: Full test suite passes

## Completed Issues ✅

### High Priority - Generic Parser
1. **Foreign key constraint name generation** ✅ - Auto-generated names for long foreign keys
   - Implemented PostgreSQL's smart 63-character truncation algorithm in `parser/token.go`
   - Table names truncated to 33-len(suffix), column names to 28 characters
   - Matches PostgreSQL's exact behavior for `_fkey` suffix
2. **View idempotency** ✅ - CASE WHEN views get dropped/recreated on each apply
   - String literal ::text casts now normalized ✅
   - Parentheses around function calls and casts now normalized ✅
   - CAST vs :: syntax differences fixed ✅
   - Solution: Added `ConvertExpr` handling in `normalizeExprForView()` to convert `CAST(... AS type)` to `expr::type` syntax
   - Both `CastExpr` and `ConvertExpr` now properly normalized to `CastExpr` (:: syntax)
   - **Architecture improvement**: `CollateExpr` now only used for COLLATE expressions, not type casts (removed confusing overloading)
3. **CastExpr type consolidation** ✅ - Unified type representation across the system
   - Migrated `CastExpr.Type` from `*ConvertType` to `*ColumnType` for consistency
   - `ColumnType` is more generic and used throughout the codebase for column definitions
   - Updated parser grammar (`parser/parser.y`) to create `ColumnType` structures for TYPECAST productions
   - Updated all normalization code in `schema/generator.go` to use `ColumnType`
   - Updated pgquery fallback parser in `database/postgres/parser.go` to use `ColumnType`
   - Benefits: Reduced type duplication, improved consistency, eliminated conversion overhead
   - All view and cast normalization tests pass with the new type system
4. **Constraint options idempotency** ✅ - Fixed duplicate DROP constraint statements
   - Root cause: When an index/constraint definition changed, it was being dropped in two places
   - First loop (checking absent indexes) would drop if definition changed
   - Second loop (examining desired indexes) would also drop before adding
   - Fix: Modified first loop in `schema/generator.go` to skip dropping indexes that exist in desired
   - Now only the second loop handles drop+add for changed definitions
   - Eliminates duplicate DROP statements and ensures idempotent migrations
   - All tests pass with both generic and default parsers

### MySQL - Schema Comparison & Idempotency
5. **Foreign key auto-created indexes** ✅ - MySQL drops FK indexes unnecessarily
   - Root cause: MySQL auto-creates indexes for foreign keys with the constraint name
   - During idempotency check, code tried to drop these auto-created indexes even when FK exists
   - Fix: Added logic in `schema/generator.go` to skip dropping indexes that match FK constraint names
   - Check if index name exists in `desiredTable.foreignKeys` before dropping
   - Location: `schema/generator.go:406-412`

6. **Boolean value normalization** ✅ - MySQL boolean defaults cause non-idempotent changes
   - Root cause: MySQL treats `boolean` as `TINYINT(1)`, converts `false`/`true` to `0`/`1`
   - Original code only normalized desired values, not current values from database
   - Fix: Enhanced `areSameValue()` to normalize BOTH current and desired boolean values
   - Converts `false`→`0` and `true`→`1` in both directions for proper comparison
   - Location: `schema/generator.go:4751-4768`

### MSSQL - Foreign Key Handling
7. **Foreign key state tracking** ✅ - Duplicate FKs after drop/recreate operations
   - Root cause: In-memory state not updated when FKs dropped and recreated
   - Led to duplicate foreign key tracking, causing repeated drop/add cycles
   - Fix: Added state updates to remove old FKs and add new ones in `generateDDLsForCreateTable()`
   - Properly maintains `currentTable.foreignKeys` during modifications
   - Location: `schema/generator.go:1193-1218`

8. **Composite foreign key parsing** ✅ - Multi-column FKs parsed as multiple single-column FKs
   - Root cause: MSSQL query returns one row per FK column, code didn't group by constraint name
   - Composite FKs appeared as separate single-column constraints in exported schema
   - Fix: Rewrote `updateForeignDefs()` to group columns by constraint name before building definitions
   - Now properly handles multi-column FKs: `FOREIGN KEY (col1, col2) REFERENCES table (col1, col2)`
   - Location: `database/mssql/database.go:598-687`

## Priority Issues to Fix

### Medium Priority - Generic Parser
1. **CREATE EXTENSION** - Extension creation not supported (`CREATE EXTENSION citext;`)

## Needs Refactoring

### Normalization Functions (`normalizeExprForView()` vs `normalizeCheckExprAST()`)
**Problem**: Two similar normalization functions with ~150 lines of duplicated logic:

- `normalizeExprForView()` - Used for view expressions (from `pg_get_viewdef()`)
- `normalizeCheckExprAST()` - Used for CHECK constraint expressions (from `pg_get_constraintdef()`)

**Current inconsistency**: Exclusion constraint WHERE clauses use `normalizeExprForView()` but should use `normalizeCheckExprAST()` since they come from `pg_get_constraintdef()` like CHECK constraints.

**Key differences**:
- Cast handling: View normalizer aggressively removes casts; check normalizer is selective (keeps casts on columns, removes on literals)
- Operator normalization: Only check normalizer does this (<> → !=, etc.)
- IN clause: Check normalizer converts IN to ANY(ARRAY[...]) for PostgreSQL
- Mode awareness: Check normalizer handles MySQL/MSSQL/SQLite differences; view normalizer assumes PostgreSQL

**Recommendation**:
1. Short-term: Use `normalizeCheckExprAST()` for exclusion WHERE clauses instead of `normalizeExprForView()`
2. Long-term: Unify into a single `normalizeExprAST(expr, config)` with configuration-based behavior to eliminate duplication
