# Generic Parser TODO

## Current Status

- **663/678 parser tests passing** (97.8% success rate)
- **0 reduce/reduce conflicts** ✓
- **38 shift/reduce conflicts** (baseline maintained) ✓
- **All command tests passing** ✓ (mysqldef, psqldef, sqlite3def, mssqldef)

## Running Tests

```sh
go test ./parser         # Run parser tests only
make test                # Run all tests (takes ~5 minutes)
```

## Failing Tests (15 failures in 6 test scenarios)

The following tests are still failing and represent features not yet fully supported:
- **CreateTableWithDefault** - Complex default expressions with type casts (e.g., `''::character varying`)
- **ChangeDefaultExpressionWithAddition** - Default expressions with arithmetic operations (e.g., `DEFAULT 1 + 1`)
- **ForeignKeyOnReservedName** - Foreign keys referencing reserved word columns
- **NumericCast** - Numeric type casting expressions
- **CreateIndexWithCoalesce** - Index expressions with COALESCE function
- **CreateTableWithConstraintOptions** - Constraint options like DEFERRABLE

**Note:** Adding support for type casts and arithmetic operations in default expressions creates significant grammar conflicts (337 reduce/reduce conflicts). These features would require major parser refactoring to implement without conflicts. Since the generic parser must support multiple SQL dialects and `psqldef` primarily uses `go-pgquery` anyway, these PostgreSQL-specific features remain unsupported in the generic parser.

## Remaining Features to Implement

### 1. PostgreSQL-specific data types
- Arrays with bracket syntax: `INTEGER[]`, `TEXT[][]` (array type definitions)

### 2. Advanced constraints
- Constraint options: `DEFERRABLE`, `INITIALLY DEFERRED`
- `NO INHERIT` on constraints (partial support)
- CHECK constraints with IN operator

### 3. Advanced expressions and operators
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
- Careful rule refactoring can often avoid conflict increases when adding new features

### Trade-offs
Some PostgreSQL-specific features have implementation constraints:
- Parenthesized expressions with type casts have limited support through `tuple_expression`
- Complex operator precedence can create ambiguities

## Notes
- The generic parser is primarily a fallback - `psqldef` uses `go-pgquery` by default
- Use `PSQLDEF_PARSER=generic` environment variable to force generic parser
- The parser must maintain zero reduce/reduce conflicts for correctness
- Shift/reduce conflicts should stay at baseline (38) to avoid regressions
