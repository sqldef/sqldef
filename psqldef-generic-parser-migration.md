# Generic Parser Migration

## Goal

We are implementing PostgreSQL syntaxes in the generic parser. Once the migration is complete, we will discard the `pgquery` parser.

## Current Status

- **710 tests PASSING, 0 tests SKIPPED** (100% success rate for generic parser tests) ✅
- **0 unique test cases** affected by parser limitations
- **0 reduce/reduce conflicts**
- **47 shift/reduce conflicts** (baseline, +2 from adding arithmetic operators in DEFAULT)

## Running Generic Parser Tests

```sh
# Run parser tests only
go run gotest.tools/gotestsum@latest ./cmd/psqldef -run TestPsqldefYamlGenericParser

# Run all tests (takes ~5 minutes)
make test
```

## Rules

- **Must maintain zero reduce/reduce conflicts** for parser correctness
- **Must maintain baseline of 47 shift/reduce conflicts** to avoid regressions

## Notes

- The generic parser is a fallback - `psqldef` uses `pgquery` by default
- Use `PSQLDEF_PARSER=generic` environment variable to force generic parser
- Keep this document up to date

## Recent Changes

### 2025-01-XX: Fixed Reserved Keywords in Foreign Key References ✅

**What was fixed:** The parser now accepts reserved keywords (like `TYPE`) as unquoted column names in foreign key references.

**Changes made:**
- Modified `column_list` rule to use `reserved_sql_id` instead of `sql_id`
- Modified `sql_id_list` rule to use `reserved_sql_id` instead of `sql_id`

**Impact:**
- All 710 generic parser tests now pass (previously 708 passing, 2 skipped)
- No new parser conflicts introduced (still 47 shift/reduce conflicts)
- CreateTableWithConstraintOptions test now passes

**Example that now works:**
```sql
CREATE TABLE image_owners (
  type VARCHAR(20) NOT NULL,
  PRIMARY KEY (type, id)
);

CREATE TABLE image_bindings (
  FOREIGN KEY (owner_type) REFERENCES image_owners(type, id)
  -- Now works! Parser accepts 'type' as column name
);
```
