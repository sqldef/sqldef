# Generic Parser - Remaining Work

## Current Status
- **528/678 tests passing** (77.9% success rate)
- **0 reduce/reduce conflicts** ✓
- **38 shift/reduce conflicts**

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

## Remaining Failures (150 tests)

The remaining failures are primarily due to PostgreSQL-specific syntax not yet implemented:

### 1. PostgreSQL-specific data types
- `tstzrange`, `tsrange`, `citext`
- Arrays and custom types
- `INTERVAL` as a column type (conflicts with INTERVAL expressions)

### 2. Advanced constraints
- `EXCLUDE` constraints with operators
- Complex CHECK constraints with `ALL`, `ANY`, `SOME` operators
- Constraint options like `DEFERRABLE`

### 3. Advanced expressions
- Type casting with `::` for complex types (e.g., `::numeric(10,2)`)
- Operator classes in indexes
- Complex default expressions with operators
- Functions like `gen_random_uuid()` (works as function calls)

### 4. COMMENT statements
- `COMMENT ON ... IS NULL` syntax (partial support)

### 5. Reserved word handling
- Some PostgreSQL reserved words like `level` not properly handled

### 6. Advanced GRANT/REVOKE
- Complex privilege management scenarios
- Role-based access control edge cases

## Implementation Challenges
Some features cannot be easily added without introducing grammar conflicts:
- **INTERVAL as column type**: Conflicts with `INTERVAL 'value'` expressions
- **Extended TYPECAST**: Supporting `::type(params)` causes reduce/reduce conflicts with convert_type
- **EXCLUDE keyword**: Would require significant grammar restructuring to avoid conflicts

## Notes
- The generic parser is primarily a fallback - `psqldef` uses `go-pgquery` by default
- Use `PSQLDEF_PARSER=generic` environment variable to force generic parser
- The parser must maintain zero reduce/reduce conflicts for correctness