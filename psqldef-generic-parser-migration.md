# Generic Parser Migration

## Goal

We are implementing PostgreSQL syntaxes in the generic parser. Once the migration is complete, we will discard the `pgquery` parser.

## Current Status

- **690 tests PASSING, 8 tests SKIPPED** (98.9% success rate for generic parser tests)
- **5 unique test cases** affected by genuine parser limitations
- **0 reduce/reduce conflicts**
- **38 shift/reduce conflicts** (baseline)

## Running Tests

```sh
# Run parser tests only
go run gotest.tools/gotestsum@latest ./cmd/psqldef -run TestPsqldefYamlGenericParser

# Run all tests (takes ~5 minutes)
make test
```

## Rules

- **Must maintain zero reduce/reduce conflicts** for parser correctness
- **Must maintain baseline of 38 shift/reduce conflicts** to avoid regressions

## Notes

- The generic parser is a fallback - `psqldef` uses `pgquery` by default
- Use `PSQLDEF_PARSER=generic` environment variable to force generic parser
- Keep this document up to date

## Remaining Tasks

The analysis below is based on remaining skipped tests affecting 4 unique test cases.

### Remaining Parser Limitations

#### 1. Chained Type Casts (1 test case) - ✅ PARTIALLY FIXED

**Status:** Chained type casts now work (e.g., `CURRENT_TIMESTAMP::date::text`), but the test remains skipped due to unrelated issues with ARRAY constructors in the same test case.

**What works:**
```sql
-- This now parses successfully ✅
CREATE TABLE users (
  default_date_text text DEFAULT CURRENT_TIMESTAMP::date::text
);
```

**What still fails (unrelated issue):**
```sql
-- ARRAY constructors in DEFAULT not yet supported
CREATE TABLE users (
  arr int[] DEFAULT ARRAY[]::int[]
);
```

**Implementation:** Modified `default_expression` grammar in `parser/parser.y` (lines 2696-2715) to support recursive type casting.

**Tests affected:**
- CreateTableWithDefault (1 test - still skipped due to ARRAY constructor issue)

#### 2. Arithmetic Expressions in DEFAULT (1 test case) - ❌ BLOCKED

**Status:** Cannot be implemented without violating grammar conflict constraints.

**Problem:** Parser doesn't support arithmetic operations in DEFAULT expressions like `(CURRENT_TIMESTAMP + '1 day'::interval)`.

**Error Pattern:** `syntax error` when parsing binary operators in DEFAULT context

**Example:**
```sql
CREATE TABLE foo (
  expires_at timestamp DEFAULT (CURRENT_TIMESTAMP + '1 day'::interval)
);
```

**Root Cause - Fundamental Grammar Limitation:**

The parser has a dual-path structure for DEFAULT values:
```yacc
DEFAULT default_val          # Simple values → .Value field
DEFAULT default_expression   # Complex expressions → .Expr field
```

This design creates an inherent conflict when trying to add arithmetic operators:

1. **Adding literals to `default_expression`** (e.g., `default_expression: STRING`):
   - Creates reduce/reduce conflicts with `default_val: STRING`
   - Parser can't decide which path to take for `DEFAULT 'hello'`

2. **Using `value_expression`** (which has all operators):
   - Creates 248 reduce/reduce conflicts
   - Too broad, conflicts with other grammar rules

3. **Creating intermediate rules** (e.g., `default_val_expr`):
   - If used as base case: creates 97 reduce/reduce conflicts
   - If used only in operators: parser can't form complete expressions

**Why This Matters:**
- The dual-path design optimizes for simple literals vs. complex expressions
- Simple values print as `DEFAULT 5`, expressions print as `DEFAULT (expr)`
- Merging paths would require always printing parentheses, breaking diff generation

**Possible Solutions (all have trade-offs):**
1. Accept reduce/reduce conflicts (violates project rules)
2. Major grammar refactoring to single-path design (breaks compatibility)
3. Use GLR parsing instead of LALR (requires parser generator change)
4. Keep using pgquery parser for this syntax (current fallback works)

**Tests affected:**
- ChangeDefaultExpressionWithAddition (2 tests: current + desired) - SKIPPED ⏭️

**Decision:** This syntax remains unsupported in the generic parser. Users needing this feature should rely on the pgquery parser (default for psqldef).

#### 3. COALESCE in Index Expressions (1 test case)

**Problem:** Parser doesn't support function calls like COALESCE in CREATE INDEX expressions.

**Error Pattern:** `syntax error in CREATE INDEX ... (COALESCE(...))`

**Example:**
```sql
CREATE INDEX idx ON users (name, COALESCE(user_name, 'NO_NAME'::TEXT));
```

**Tests affected:**
- CreateIndexWithCoalesce (1 test)

#### 4. Type Cast to Numeric (1 test case)

**Problem:** Parser doesn't support casting to `numeric` type in expressions.

**Error Pattern:** `syntax error near '::numeric'`

**Example:**
```sql
CREATE VIEW v AS SELECT * FROM t WHERE (t.item = (0)::numeric);
```

**Tests affected:**
- NumericCast (1 test)

#### 5. Reserved Word "variables" as Table Name (1 test case)

**Problem:** Parser treats `variables` as a reserved keyword instead of allowing it as a table name.

**Error Pattern:** `syntax error near 'variables'`

**Example:**
```sql
CREATE TABLE IF NOT EXISTS variables (
  id VARCHAR(100) PRIMARY KEY
);
```

**Tests affected:**
- ForeignKeyOnReservedName (1 test)

#### 6. DEFERRABLE INITIALLY IMMEDIATE (1 test case)

**Problem:** Parser doesn't support `DEFERRABLE INITIALLY IMMEDIATE` constraint options on inline foreign key references.

**Error Pattern:** `syntax error near 'deferrable'`

**Example:**
```sql
CREATE TABLE bindings (
  image_id INT REFERENCES images(id) ON DELETE CASCADE DEFERRABLE INITIALLY IMMEDIATE
);
```

**Tests affected:**
- CreateTableWithConstraintOptions (2 tests: current + desired)
