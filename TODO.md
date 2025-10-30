# Generic Parser Improvements

## Current Status

- **593/685 tests passing** (86.6% success rate)
- **0 reduce/reduce conflicts**
- **38 shift/reduce conflicts**

## Running Tests

```sh
go test ./parser
```

## TODO: Remove splitDDLs Workaround
The generic parser now natively supports multiple statements. The `splitDDLs()` workaround should be removed:

**Location:** `database/parser.go:49-82` (GenericParser.splitDDLs method)

**Current flow:**
1. `GenericParser.Parse()` calls `splitDDLs()`
2. `splitDDLs()` manually splits by semicolons
3. Each fragment is parsed individually

**New flow should be:**
1. `GenericParser.Parse()` calls `parser.ParseDDL()` once
2. Handle `MultiStatement` result if multiple statements exist
3. Return all statements

This will make the parser more robust for complex SQL with embedded semicolons (e.g., stored procedures, triggers)

## Remaining Failures (92 tests)

The remaining failures are primarily due to PostgreSQL-specific syntax not yet implemented:

### 1. PostgreSQL-specific data types
- Arrays with bracket syntax: `INTEGER[]`, `TEXT[][]` (array type definitions)
- `INTERVAL` as a column type (conflicts with INTERVAL expressions)

### 2. Advanced constraints
- Complex `EXCLUDE` constraints with USING GIST and multiple operators
- Constraint options: `DEFERRABLE`, `INITIALLY DEFERRED`
- `NO INHERIT` on constraints (partial support)

### 3. Advanced expressions and operators
- Type casting with `::` for more complex types
- Operator classes in indexes (e.g., `text_pattern_ops`)
- Complex default expressions with operators
- PostgreSQL-specific operators in WHERE clauses

### 4. GRANT/REVOKE edge cases
- Complex privilege management with multiple grantees
- `WITH GRANT OPTION` support
- CASCADE/RESTRICT options
- Role-based access control with special characters

### 5. Reserved word handling
- Reserved words as identifiers (e.g., `level`, `select` as column names)
- Context-sensitive keywords

### 6. Other PostgreSQL features
- `CREATE TYPE ... AS ENUM` with complex usage patterns
- Views with complex CASE/WHEN expressions
- Index expressions with functions (e.g., `COALESCE`)
- Specialized index types and options

## Implementation Challenges
Some features cannot be easily added without introducing grammar conflicts:
- **INTERVAL as column type**: Conflicts with `INTERVAL 'value'` expressions (attempted, causes 111 reduce/reduce conflicts)
- **Extended TYPECAST**: Supporting all `::type(params)` patterns may cause reduce/reduce conflicts
- **Complex EXCLUDE constraints**: Basic structure added, but full USING GIST support needs more work

## Notes
- The generic parser is primarily a fallback - `psqldef` uses `go-pgquery` by default
- Use `PSQLDEF_PARSER=generic` environment variable to force generic parser
- The parser must maintain zero reduce/reduce conflicts for correctness
