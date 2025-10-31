# Generic Parser TODO

## Current Status

- **651/677 parser tests passing** (96.2% success rate - aiming for 100%)
- **0 reduce/reduce conflicts** ✓
- **38 shift/reduce conflicts** (baseline maintained) ✓

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

## Remaining Features to Implement

### 1. PostgreSQL-specific data types
- Arrays with bracket syntax: `INTEGER[]`, `TEXT[][]` (array type definitions)

### 2. Advanced constraints
- Constraint options: `DEFERRABLE`, `INITIALLY DEFERRED`
- `NO INHERIT` on constraints (partial support)
- CHECK constraints with IN operator

### 3. Advanced expressions and operators
- Date/time literals with type prefix (e.g., `DATE '2022-01-01'`, `TIMESTAMP '2022-01-01'`)
  - Note: Adding these creates shift/reduce conflicts
- Operator classes in indexes (e.g., `text_pattern_ops`)
- Complex default expressions with operators
- PostgreSQL-specific operators in WHERE clauses
- Index expressions with functions (e.g., `COALESCE`)
- String literals with type casts in parentheses (e.g., `('text'::varchar)`)

### 4. GRANT/REVOKE edge cases
- CASCADE/RESTRICT options on REVOKE

### 5. Other PostgreSQL features
- Views with complex CASE/WHEN expressions
- Specialized index types and options
- Reserved words as identifiers in more contexts

## Implementation Constraints

### Parser Conflict Requirements
- **Must maintain zero reduce/reduce conflicts** for parser correctness
- **Must maintain baseline of 38 shift/reduce conflicts** to avoid regressions
- Adding certain features (like date literals) can introduce additional conflicts that are difficult to resolve

### Trade-offs
Some PostgreSQL-specific features cannot be added without breaking the conflict baseline:
- Date/time literals with type prefix introduce conflicts with existing grammar rules
- Parenthesized expressions with type casts have limited support through `tuple_expression`
- Complex operator precedence can create ambiguities

## Notes
- The generic parser is primarily a fallback - `psqldef` uses `go-pgquery` by default
- Use `PSQLDEF_PARSER=generic` environment variable to force generic parser
- The parser must maintain zero reduce/reduce conflicts for correctness
- Shift/reduce conflicts should stay at baseline (38) to avoid regressions
