# Code Review: psqldef Generic Parser Changes

## Low-Quality Code

### Duplicated Normalization Calls

**File**: `database/postgres/database.go:580, 676`

The same `normalizePostgresTypeCasts` function is called in two places:

1. In `getColumns` (line 580):
```go
normalizedDef := normalizePostgresTypeCasts(*checkDefinition)
col.Check = &columnConstraint{
    definition: normalizedDef,
    name:       *checkName,
}
```

2. In `getTableCheckConstraints` (line 676):
```go
constraintDef = normalizePostgresTypeCasts(constraintDef)
result[constraintName] = constraintDef
```

**Problem**: The same normalization is applied to different data sources, suggesting:
- The normalization should happen at a common layer
- Or the database queries should return consistent formats

**Recommendation**: Consider normalizing at the database layer or creating a dedicated normalization layer for check constraints.

---

### Inconsistent Logging Levels

**File**: `database/postgres/parser.go:52, 83, 118`

Different logging levels for similar situations:

1. Environment variable set → `Debug` (line 52):
```go
slog.Debug("Using generic parser only mode (PSQLDEF_PARSER=generic)")
```

2. Generic parser fails → `Warn` with "unexpected behavior" (line 83):
```go
slog.Warn("Generic parser failed, falling back to pgquery (unexpected behavior)", ...)
```

3. pgquery parseStmt fails → `Warn` (line 118):
```go
slog.Warn("pgquery parseStmt failed, falling back to generic parser for statement", ...)
```

**Problems**:
- If fallback is "unexpected behavior", it should be `Error` or `Warn`, but then why implement it?
- Logging at `Debug` for mode selection but `Warn` for failures creates inconsistent signal-to-noise
- The message "unexpected behavior" in production code is unprofessional

**Recommendation**:
- If fallback is normal: use `Debug` consistently
- If fallback is exceptional: use `Warn` and remove "unexpected behavior" text
- Clarify in comments when each parser should be used

---

### Complex Nested Logic in Parse Method

**File**: `database/postgres/parser.go:64-136`

The `Parse` method has multiple levels of conditional logic with three different modes and two different parsers. The control flow is hard to follow:

```go
func (p PostgresParser) Parse(sql string) ([]database.DDLStatement, error) {
    if p.mode == PsqldefParserModeGeneric {
        return p.parser.Parse(sql)
    }

    if p.mode == PsqldefParserModePgquery {
        return p.parsePgquery(sql)
    }

    // Auto mode
    statements, err := p.parser.Parse(sql)
    if err != nil {
        return p.parsePgquery(sql)
    }

    return statements, nil
}

func (p PostgresParser) parsePgquery(sql string) ([]database.DDLStatement, error) {
    // ... parsing logic ...
    if err != nil {
        if p.mode == PsqldefParserModeAuto {
            stmts, err = p.parser.Parse(ddl)
        }
        // ...
    }
    // ...
}
```

**Recommendation**: Extract the fallback logic into a separate method, add flow diagram in comments, or simplify by removing one of the parsers.

---

### Poor Variable Naming

**File**: `database/postgres/parser.go:684`

```go
rawTypeName := p.getRawTypeName(node.TypeCast.TypeName)
typeName := rawTypeName
if typeName == "" {
    typeName = columnType.Type
}
```

Both `rawTypeName` and `typeName` are used, but the distinction between "raw" and "normalized" is unclear. Better names would be:
- `rawTypeName` → `typeNameFromPgquery`
- `typeName` → `finalTypeName`
