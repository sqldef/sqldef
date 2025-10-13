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

### Default Parser (generic primary, pgquery fallback) - ✅ ALL TESTS PASS
When running with default parser: All tests pass

### GENERIC Parser (PSQLDEF_PARSER=generic) - ✅ ALL TESTS PASS
When running with `PSQLDEF_PARSER=generic`: All tests pass

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
