# TODO: Migration from pgquery to generic parser

This document tracks the failing tests when using `PSQLDEF_PARSER=generic`.

**Total failing tests: 79**

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
- **Idempotence issues**: 16 tests (schema not applying correctly on re-run) - was 33, fixed 17!
- **Migration output mismatch**: 23 tests (generated DDL doesn't match expected)
- **SQL syntax errors**: 0 tests (all fixed!)

## Categories

### View-Related Issues (6 tests remaining)

**Partial solution implemented:**
- Enhanced view definition normalization to handle PostgreSQL's parenthesization of expressions
- Added type cast normalization for literals (e.g., `(2)::bigint` → `2::bigint` → `2`)

**Remaining issues:**
These require more complex view definition normalization:

- [ ] CreateViewWithCaseWhen
  - Desired schema not idempotent
  - CASE WHEN expressions not preserved

- [ ] NullCast
  - Desired schema not idempotent

- [ ] ReplaceViewWithChangeCondition
  - Desired schema not idempotent

- [ ] ViewDDLsAreEmittedLastWithChangingDefinition
  - Desired schema not idempotent

- [ ] ViewDDLsAreEmittedLastWithoutChangingDefinition
  - Desired schema not idempotent
  - CREATE OR REPLACE generated when not needed

- [ ] ViewWithGroupByAndHaving
  - Desired schema not idempotent

### FOREIGN KEY Constraint Issues (9 tests)
Issues with foreign key constraint handling:

- [ ] CreateTableWithConstraintOptions
  - Foreign key with DEFERRABLE options not detected (missing DDL for inline REFERENCES)
  - Expected to update inline FK with DEFERRABLE options but doesn't generate DDL

- [ ] CreateTableAddAbsentForeignKey
  - Foreign key not being added (empty migration)

- [ ] ForeignKeyConstraintsAreEmittedLast
  - Migration output mismatch

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

- [ ] ForeignKeyOnReservedName
  - Migration output mismatch

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
