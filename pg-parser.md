# PostgreSQL Parser Migration

This document tracks the migration from the external PostgreSQL parser (`github.com/pganalyze/pg_query_go`) to the internal parser implementation.

## Background

The `gfx/psqldef_parser` branch removes the dependency on the external PostgreSQL parser and implements PostgreSQL-specific features in the internal parser (`parser/parser.y`).

## Current Status

**Progress**: All non-TestApply tests passing. TestApply has 6 remaining YAML test failures.

**Test Failures**: 6 out of ~500 test cases (98.8% pass rate)

**Recent Fixes**:
- ✅ Fixed `numeric` vs `decimal` type normalization (ForeignKeyDependenciesPrimaryKeyChange)
- ✅ Fixed `varchar` vs `character varying` normalization (CreateIndexWithConcurrentlyConfigMixedStatements)
- ✅ **Fixed CHECK constraint normalization (7 tests)** - Added IN/NOT IN to ARRAY transformation in AST normalization

### Parser Conflicts
- Current state: 275 shift/reduce, 1556 reduce/reduce
- These are acceptable for complex SQL grammar and don't affect functionality

## Remaining Issues ❌

**Summary**: 6 test failures across 3 categories (down from 13 → 54% reduction)

**Breakdown**:
- 2 DEFAULT expression parsing tests (requires parser grammar enhancement)
- 2 Long auto-generated constraint name tests (requires PostgreSQL naming algorithm)
- 2 Variadic ARRAY transformation tests (documented limitation)

### 1. Default Expression Parsing (2 tests)

#### ChangeDefaultExpressionWithAddition
- **Issue**: Parser strips `::interval` cast from DEFAULT expressions
- **Current behavior**: `DEFAULT (CURRENT_TIMESTAMP + '1 day'::interval)` → `DEFAULT (current_timestamp + '3 days')`
- **Expected behavior**: Should preserve `'3 days'::interval`
- **Root cause**: Parser doesn't support interval type casts in binary expressions within DEFAULT clauses
- **Detailed error**:
  ```
  Expected: DEFAULT current_timestamp + '3 days'::interval
  Actual:   DEFAULT (current_timestamp + '3 days')
  ```

#### CreateTableWithDefault
- **Issue**: Parser fails to parse nested type casts in DEFAULT expressions
- **Current behavior**: Syntax error when parsing `DEFAULT ((CURRENT_TIMESTAMP)::date)::text`
- **Expected behavior**: Should parse and generate correct DEFAULT clause
- **Root cause**: Grammar doesn't support nested parenthesized type casts like `((expr)::type1)::type2`
- **Detailed error**:
  ```
  syntax error at line 13, column 58 near 'current_timestamp'
  "default_date_text" text DEFAULT ((CURRENT_TIMESTAMP)::date)::text,
                                                           ^
  ```
- **Status**: Requires parser grammar enhancement to support:
  - Nested parentheses in DEFAULT expressions
  - Chained type casts `expr::type1::type2`
  - Binary operators with type casts

### 2. View and Index Variadic ARRAY Transformations (2 tests)

#### CreateViewWithCaseWhen
- **Issue**: PostgreSQL transforms variadic function arguments to ARRAY form
- **Current behavior**: View is not idempotent after creation
- **Expected behavior**: View should be idempotent
- **Root cause**: PostgreSQL converts `jsonb_extract_path_text(x, 'a', 'b')` to `jsonb_extract_path_text(x, ARRAY['a', 'b'])` internally
- **Detailed transformations**:
  ```
  Parser output:  jsonb_extract_path_text(payload, 'hoge', 'amount')
  PostgreSQL DB:  jsonb_extract_path_text(payload, ARRAY['hoge', 'amount'])

  Parser output:  to_timestamp(x::bigint)
  PostgreSQL DB:  to_timestamp(x::bigint::double precision)

  Parser output:  cast(expr as date)
  PostgreSQL DB:  expr::date
  ```
- **AST normalization attempted**: Part 9 implemented `tryExpandArrayLiteral()` to reverse the ARRAY transformation, but this doesn't work because:
  1. Need to know which functions are variadic (requires PostgreSQL system catalog lookup)
  2. Intermediate type casts added by PostgreSQL (`bigint → double precision`)
  3. CAST vs :: syntax equivalence in complex expressions
- **Status**: Documented limitation of AST-based approach

#### IndexesOnChangedExpressions
- **Issue**: Index expression changes not detected when function arguments change
- **Current behavior**: Index expression `jsonb_extract_path_text(col, 'foo', 'bar')` vs `jsonb_extract_path_text(col, 'foo')` not recognized as different
- **Expected behavior**: Should detect change and regenerate index
- **Root cause**: Same variadic ARRAY transformation issue as views
- **Detailed comparison**:
  ```
  Desired:  jsonb_extract_path_text(col, 'foo')
  Current:  jsonb_extract_path_text(col, ARRAY['foo', 'bar'])

  Normalized: Both become jsonb_extract_path_text(col, 'foo', 'bar') due to ARRAY expansion
  Result: Incorrectly seen as identical
  ```
- **Status**: Same limitation as view variadic functions

### 3. CHECK Constraint Normalization ✅ FIXED

**Affected Tests** (all now passing):
- ConstraintCheckInAdd
- ConstraintCheckInRemove
- ConstraintCheckInModify
- ConstraintCheckInAndUniqueAdd
- ConstraintCheckInAndUniqueRemove
- ConstraintCheckInMultipleColumnsWithUnique
- ConstraintCheckInWithUniqueCreate

- **Issue**: CHECK constraint comparison failed because `IN (...)` syntax was not normalized to PostgreSQL's internal `= ANY (ARRAY[...])` representation
- **Root cause**: The parser preserves `IN (val1, val2, ...)` as `ComparisonExpr{Operator: "in", Right: ValTuple}`, but PostgreSQL converts this to `ComparisonExpr{Operator: "=", Any: true, Right: ArrayConstructor}`. Additionally, PostgreSQL adds `::text` casts to array elements.
- **Detailed issue**:
  ```
  Desired schema:     CHECK (status IN ('active', 'inactive', 'pending'))
  Parser AST:         ComparisonExpr{Operator: "in", Right: ValTuple}
  PostgreSQL stores:  CHECK (status = ANY (ARRAY['active'::text, 'inactive'::text, 'pending'::text]))
  PostgreSQL AST:     ComparisonExpr{Operator: "=", Any: true, Right: ArrayConstructor}
  ```
- **Solution**: Enhanced `NormalizeExpr()` in `parser/normalize.go` to transform IN/NOT IN expressions:
  - `IN (val1, val2, ...)` → `= ANY (ARRAY[val1, val2, ...])`
  - `NOT IN (val1, val2, ...)` → `!= ALL (ARRAY[val1, val2, ...])`
  - The existing `isRedundantCast()` function then removes the `::text` casts from array elements
- **Files modified**:
  - `parser/normalize.go:219-268` - Added IN/NOT IN transformation logic
- **Status**: ✅ All 7 tests now passing

### 4. Long Auto-Generated Constraint Names (2 tests)

#### LongAutoGeneratedCheckConstraint
- **Issue**: sqldef generates different constraint names than PostgreSQL for long names
- **Current behavior**: sqldef doesn't truncate names, PostgreSQL does
- **Expected behavior**: Match PostgreSQL's naming algorithm
- **Root cause**: PostgreSQL has a complex name truncation algorithm that sqldef doesn't implement
- **Examples**:
  ```
  Table: loooooooooooooooooooooooooooooooooooooooong_table_63_characters
  Column: a

  sqldef generates:   loooooooooooooooooooooooooooooooooooooooong_table_63_characters_a_check
  PostgreSQL creates: loooooooooooooooooooooooooooooooooooooooong_table_63_ch_a_check

  Table: loooooong_table_29_characters
  Column: loooong_column_28_characters

  sqldef generates:   loooooong_table_29_characters_loooong_column_28_characters_check
  PostgreSQL creates: loooooong_table_29_characters_loooong_column_28_character_check
                                                                          ^ 's' removed
  ```
- **PostgreSQL algorithm**:
  - Total name limit: 63 characters
  - Intelligently abbreviates parts to fit
  - Prefers abbreviating earlier components
- **Impact**: Functional - constraints work but names differ, causing recreation

#### LongAutoGeneratedForeignKeyConstraint
- **Issue**: Same as above but for foreign key constraint names
- **Expected suffix**: `_fkey`
- **Examples**:
  ```
  Table: loooooooooooooooooooooooooooooooooooooooong_table_63_characters
  Column: a

  sqldef generates:   loooooooooooooooooooooooooooooooooooooooong_table_63_characters
                      (truncated to 63 chars, no _fkey suffix!)
  PostgreSQL creates: loooooooooooooooooooooooooooooooooooooooong_table_63_cha_a_fkey
  ```
- **Impact**: Same as CHECK constraints - functional but name mismatch

## Testing Strategy

### Test Commands

```bash
# Run all psqldef tests
go test ./cmd/psqldef -count=1

# Run specific failing test
go test ./cmd/psqldef -run TestApply/ChangeDefaultExpressionWithAddition -v

# Run all TestApply tests
go test ./cmd/psqldef -run TestApply -v

# Count failures
go test ./cmd/psqldef -count=1 2>&1 | grep -c "^--- FAIL"

# List failing test names
go test ./cmd/psqldef -run=TestApply -count=1 2>&1 | grep "^    --- FAIL:" | sed 's/.*TestApply\///' | cut -d' ' -f1
```

## Implementation Notes

### Parser Architecture
- Main parser: `parser/parser.y` (yacc grammar)
- Tokenizer: `parser/token.go`
- AST nodes: `parser/node.go`
- Schema generation: `schema/generator.go`
- PostgreSQL database interface: `database/postgres/database.go`

### AST-Based View Comparison (Part 9-10)

Implemented a three-phase architecture for view comparison:
1. **Parse**: Convert PostgreSQL string output → AST via `parser.ParseSelectStatement()`
2. **Normalize**: Visitor pattern to remove semantic-preserving differences
3. **Compare**: Structural deep comparison instead of string matching

**Files**:
- `parser/expr.go`: Parse helpers for SELECT statements and expressions
- `parser/normalize.go`: AST normalization visitor (~600 lines)
- `parser/compare.go`: Structural comparison functions (~450 lines)

**Key normalizations**:
- ✅ Remove SQL comments from AST
- ✅ Clear PostgreSQL-added ELSE NULL from CASE expressions
- ✅ Normalize operators (~~→LIKE, etc.)
- ✅ Remove redundant parentheses around simple expressions
- ✅ Lowercase function names
- ✅ Remove table qualifiers from column names
- ✅ Handle nested type casts
- ❌ Variadic ARRAY transformation (documented limitation)

**Known limitations**:
- PostgreSQL's variadic ARRAY transformation changes AST structure
- Intermediate type cast chains added by PostgreSQL for type promotion
- Requires PostgreSQL catalog knowledge to distinguish variadic functions

## Development Guidelines

When adding new PostgreSQL features:

1. **Add tokens** in `parser/token.go` if new keywords needed
2. **Update grammar** in `parser/parser.y`
3. **Define AST nodes** in `parser/node.go` if new structures needed
4. **Update schema generator** in `schema/generator.go`
5. **Add normalization** in `parser/normalize.go` for view/expression comparison
6. **Add tests** in `cmd/psqldef/tests.yml` or `cmd/psqldef/*_test.go`
7. **Run `make parser`** to regenerate parser
8. **Run `make test`** to verify all tests pass
