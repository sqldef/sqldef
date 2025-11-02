# Generic Parser Migration

## Goal

We are implementing PostgreSQL syntaxes in the generic parser. Once the migration is complete, we will discard the `pgquery` parser.

## Current Status

- **708 tests PASSING, 2 tests SKIPPED** (99.7% success rate for generic parser tests)
- **1 unique test case** affected by parser limitations (reserved keyword as column name)
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

## Parser Limitations

### Reserved Keywords as Column Names in Foreign Key References (1 test case) - ⚠️ PARSER LIMITATION

**Status:** Cannot parse reserved keywords (like `TYPE`) as unquoted column names in foreign key references.

**Problem:** Parser fails when reserved keywords are used as column names without quotes in `REFERENCES` clauses.

**Error Pattern:** `syntax error` when parsing reserved keywords in foreign key column lists

**Example:**
```sql
CREATE TABLE image_owners (
  type VARCHAR(20) NOT NULL,
  PRIMARY KEY (type, id)
);

CREATE TABLE image_bindings (
  FOREIGN KEY (owner_type) REFERENCES image_owners(type, id)
  -- ERROR: Parser treats 'type' as keyword, not column name
);
```

**Workaround:** Use quoted identifiers:
```sql
REFERENCES image_owners("type", id)  -- Works with quotes
```

**Root Cause:**
The parser's grammar doesn't allow reserved keywords in all contexts where column names are valid. The `REFERENCES` clause expects column identifiers, but the parser's keyword precedence prevents `TYPE` from being recognized as an identifier in this context.

**Tests affected:**
- CreateTableWithConstraintOptions (2 tests: current + desired) - SKIPPED ⏭️

**Note:** This test also covers `DEFERRABLE`/`INITIALLY IMMEDIATE`/`INITIALLY DEFERRED` constraint options, which are implemented and working. The test is skipped due to the reserved keyword issue, not the constraint options feature.
