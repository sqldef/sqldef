# TODO: Migration from pgquery to generic parser

This document tracks the failing tests when using `PSQLDEF_PARSER=generic`.

**Total failing tests: 74**

## Principle of Operation

* We are migrating from `pgquery` to `generic` parser, discarding `pgquery` in the future.
* However, `pgquery` will be kept for a while as a fallback parser.
* If something conflicts, the generic parser's way is correct. Update `pgquery` stuff to adjust to the generic parser's way.
* You can add test cases to debug, but do not modify the existing test cases.
* `parser/parser_test.go` is the test cases for the generic parser. Use it to develop the parser stuff.
* Wen you add `slog.Debug()` for debugging, you don't need to remove them after the task is done.
* Keep `TODO.md` up-to-date, just removing completed tasks, instead of marking them as done.

## Summary by Issue Type

- **Unsupported SQL features**: 58 tests (CREATE SCHEMA, CREATE EXTENSION, GRANT, COMMENT)
- **Idempotence issues**: 11 tests (schema not applying correctly on re-run)
- **Migration output mismatch**: 19 tests (generated DDL doesn't match expected)

## Recent Fixes (Non-Generic Parser Issues)

### CHECK Constraint Normalization (Cross-Database Fix)

**Fixed issue:**
- `normalizeCheckDefinitionForDDL` was applying PostgreSQL-specific normalization (IN to ANY/ARRAY conversion) to all databases
- This caused MySQL and SQL Server tests to fail with syntax errors
- **Fix**: Modified function to only apply PostgreSQL-specific normalization when `g.mode == GeneratorModePostgres`
- MySQL tests now pass (except 1 pre-existing OR-to-IN normalization test)

### Known Pre-Existing Issues (Not Related to Generic Parser)

**MySQL**:
- `CheckDuplicateValuesInOrChain` - expects OR chain deduplication and conversion to IN (not implemented)

**SQL Server**:
- 10 CHECK constraint tests failing due to SQL Server's own normalization differences (pre-existing)

## Categories

### View-Related Issues (1 test remaining)

**Solution implemented:**
- Migrated from regex-based string manipulation to AST-based view definition normalization
- Enhanced `normalizeViewDefinition` to return `*View` instead of `string`
- Merged `normalizeViewDefinitionAST` into `normalizeViewDefinition` using recursive helper
- Handles PostgreSQL's formatting differences:
  - Parenthesization of expressions in WHERE clauses
  - Table qualifiers in column names
  - Type cast normalization for literals
  - COLLATE expression normalization
  - CASE expression "ELSE NULL" normalization (PostgreSQL adds this automatically)
  - Function argument ARRAY conversion (e.g., jsonb_extract_path_text variadic args)
  - Whitespace and case normalization

**Remaining issues:**

- [ ] NullCast
  - Desired schema not idempotent
  - Not a view issue - DEFAULT NULL normalization problem

### FOREIGN KEY Constraint Issues (5 tests remaining)

**Fixed issues:**
- Added parser support for inline REFERENCES with DEFERRABLE/INITIALLY DEFERRED options
- Implemented conversion of inline REFERENCES to table-level foreign key constraints for PostgreSQL
- Fixed constraint options comparison to treat nil as default values (deferrable=false, initiallyDeferred=false)

**Remaining issues:**

- [ ] ForeignKeyDependenciesCascadeOptionsPreservation
  - Migration output mismatch

- [ ] ForeignKeyDependenciesCircular
  - Migration output mismatch
  - Type difference: `int` vs `integer`

- [ ] ForeignKeyDependenciesForCreateTables
  - Desired schema not idempotent

- [ ] ForeignKeyDependenciesMultipleToModifiedTable
  - Migration output mismatch

- [ ] ForeignKeyDependenciesPrimaryKeyChange
  - Both schemas not idempotent

### Unsupported SQL Features (58 tests)
These require parser extensions for CREATE SCHEMA, CREATE EXTENSION, GRANT, COMMENT:

#### CREATE SCHEMA (35 tests)
- [ ] AddBooleanColumnOnNonStandardDefaultSchema
- [ ] AddBooleanColumnWithExplicitSchema
- [ ] AddByteaColumnOnNonStandardDefaultSchema
- [ ] AddDoublePrecisionColumnOnNonStandardDefaultSchema
- [ ] AddEnumTypeColumn
- [ ] AddEnumTypeColumnOnNonStandardDefaultSchema
- [ ] AddEnumTypeColumnWithExplicitSchemaOnNonStandardDefaultSchema
- [ ] AddIntegerColumnOnNonStandardDefaultSchema
- [ ] AddJSONBColumnOnNonStandardDefaultSchema
- [ ] AddNumericColumnOnNonStandardDefaultSchema
- [ ] AddRealColumnOnNonStandardDefaultSchema
- [ ] AddTextColumnOnNonStandardDefaultSchema
- [ ] AddTimestampColumnOnNonStandardDefaultSchema
- [ ] AddTimestamptzColumnOnNonStandardDefaultSchema
- [ ] AddTimetzColumnOnNonStandardDefaultSchema
- [ ] AddUUIDColumnOnNonStandardDefaultSchema
- [ ] AddVarcharColumnOnNonStandardDefaultSchema
- [ ] AlterTypeAddValueWithSameTypeNameInDifferentSchema
- [ ] CommentOnNonStandardDefaultSchema
- [ ] CommentWithoutSchema
- [ ] CreateSchema
- [ ] CreateTableOnNonStandardDefaultSchema
- [ ] DropTableOnNonStandardDefaultSchema
- [ ] RenameIndexInNonDefaultSchema

#### CREATE EXTENSION (7 tests)
- [ ] AddColumnWithDefaultExpression
- [ ] AddDefaultExpression
- [ ] CreateExtension
- [ ] CreateExtensionIfNotExists
- [ ] CreateExtensionOrder
- [ ] DropExtension
- [ ] RemoveDefaultExpression
- [ ] RenameColumnQuotedDoubleQuotes

#### COMMENT (6 tests)
- [ ] Comment
- [ ] CommentContainingQuote
- [ ] CommentWithoutSchemaWithTableNameQuoted
- [ ] CommentWithoutSchemaWithoutTableNameQuoted
- [ ] MultipleComments
- [ ] UpdateComment

#### GRANT/REVOKE (20 tests)
- [ ] ManagedRolesAddPrivileges
- [ ] ManagedRolesAllPrivileges
- [ ] ManagedRolesBasicGrant
- [ ] ManagedRolesEmptyListNoChanges
- [ ] ManagedRolesErrorCascade
- [ ] ManagedRolesErrorGrantOption
- [ ] ManagedRolesIdempotent
- [ ] ManagedRolesIgnoreUnmanagedRole
- [ ] ManagedRolesMixedGrantRevoke
- [ ] ManagedRolesMultipleGrantees
- [ ] ManagedRolesMultipleRoles
- [ ] ManagedRolesMultipleTables
- [ ] ManagedRolesNoRevokeWithoutDrop
- [ ] ManagedRolesOverlapping
- [ ] ManagedRolesPartialGrantees
- [ ] ManagedRolesPartialRevokeFromAll
- [ ] ManagedRolesPublic
- [ ] ManagedRolesRevoke
- [ ] ManagedRolesRevokeAll
- [ ] ManagedRolesSpecialCharacters

### Low Priority: Other TestApply Failures (9 tests)

- [ ] ChangeDefaultExpressionWithAddition
  - Migration output mismatch

- [ ] ChangeTimezoneSyntax
  - Both schemas not idempotent
  - Timezone syntax not preserved

- [ ] CreateIndexWithBoolExpr
  - Desired schema not idempotent

- [ ] IndexesOnChangedExpressions
  - Migration output mismatch

### Non-TestApply Test Failures (16 tests)

- [ ] TestPsqldefAddUniqueConstraintToTableInNonpublicSchema
  - Issue: CREATE SCHEMA not supported

- [ ] TestPsqldefCheckConstraintInSchema
  - Issue: CREATE SCHEMA not supported

- [ ] TestPsqldefCitextExtension
  - Issue: CREATE EXTENSION syntax error

- [ ] TestPsqldefConfigIncludesTargetSchema
  - Issue: CREATE SCHEMA not supported

- [ ] TestPsqldefConfigIncludesTargetSchemaWithViews
  - Issue: CREATE SCHEMA not supported

- [ ] TestPsqldefCreateIndex
  - Subtests:
    - [ ] in_public_schema
    - [ ] in_non-public_schema

- [ ] TestPsqldefCreateMaterializedView
  - Subtests:
    - [ ] in_public_schema
    - [ ] in_non-public_schema

- [ ] TestPsqldefCreateMaterializedViewIndex

- [ ] TestPsqldefCreateTableInSchema
  - Issue: CREATE SCHEMA not supported

- [ ] TestPsqldefCreateTableWithConstraintReferences
  - Issue: CREATE SCHEMA not supported

- [ ] TestPsqldefCreateType

- [ ] TestPsqldefCreateView
  - Subtests:
    - [ ] in_public_schema
    - [ ] in_non-public_schema

- [ ] TestPsqldefFunctionAsDefault
  - Issue: CREATE SCHEMA not supported

- [ ] TestPsqldefIgnoreExtension
  - Issue: CREATE EXTENSION not supported

- [ ] TestPsqldefSameTableNameAmongSchemas
  - Issue: CREATE SCHEMA not supported

- [ ] TestPsqldefTableLevelCheckConstraintsWithAllAny
  - Issue: Likely fixed with ANY/ALL normalization (needs verification)
