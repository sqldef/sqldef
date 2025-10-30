# Generic Parser - Remaining Work

## Current Status
- **521/678 tests passing** (76.8% success rate)
- **0 reduce/reduce conflicts**
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

## Remaining Failures (157 tests)

The remaining failures are primarily due to PostgreSQL-specific syntax not yet implemented:

### 1. PostgreSQL-specific data types
- `tstzrange`, `tsrange`, `citext`
- Arrays and custom types

### 2. Advanced constraints
- `EXCLUDE` constraints with operators
- Complex CHECK constraints with expressions
- Constraint options like `DEFERRABLE`

### 3. Advanced expressions
- Type casting with `::`
- Operator classes in indexes
- Complex default expressions with operators

### 4. CREATE EXTENSION
- `CREATE EXTENSION IF NOT EXISTS` syntax

### 5. Reserved word handling
- Some PostgreSQL reserved words like `level` not properly handled

## Notes
- The generic parser is primarily a fallback - `psqldef` uses `go-pgquery` by default
- Use `PSQLDEF_PARSER=generic` environment variable to force generic parser