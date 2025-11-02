# Generic Parser Migration

## Goal

We are implementing PostgreSQL syntaxes in the generic parser. Once the migration is complete, we will discard the `pgquery` parser.

## Current Status

- **690 tests PASSING, 8 tests SKIPPED** (98.9% success rate)
- **5 unique test cases** affected by genuine parser limitations
- **0 reduce/reduce conflicts**
- **38 shift/reduce conflicts** (baseline maintained)

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

The analysis below is based on 8 skipped tests affecting 5 unique test cases.

### Remaining Parser Limitations (1.1% of tests)

#### 1. Chained Type Casts (1 test case)

**Problem:** PostgreSQL allows `value::type1::type2` but parser doesn't support it.

**Error Pattern:** `syntax error at line N, column M near '::'`

**Example:**
```sql
CREATE TABLE users (
  default_date_text text DEFAULT CURRENT_TIMESTAMP::date::text
);
```

**Tests affected:**
- CreateTableWithDefault (1 test)

#### 2. Arithmetic Expressions in DEFAULT (1 test case)

**Problem:** Parser doesn't support arithmetic operations like `+` in DEFAULT expressions.

**Error Pattern:** `syntax error at DEFAULT (CURRENT_TIMESTAMP + '1 day'::interval)`

**Example:**
```sql
CREATE TABLE foo (
  expires_at timestamp DEFAULT (CURRENT_TIMESTAMP + '1 day'::interval)
);
```

**Tests affected:**
- ChangeDefaultExpressionWithAddition (2 tests: current + desired)

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
