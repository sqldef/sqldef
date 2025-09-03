# MySQL Test Migration Plan

## Overview
Migrate tests from `mysqldef_test.go` to YAML-based tests loaded by `TestApply`, organized into logical groups with separate `tests_xxx.yml` files.

## Migration Strategy

### 0. Existing tests.yml Reorganization
**SPLIT EXISTING**: The current tests.yml contains mixed categories that should be reorganized:

#### From existing tests.yml → `tests_tables.yml`:
- [ ] `CreateTable`
- [ ] `DropTable` 
- [ ] `CreateTableWithImplicitNotNull`
- [ ] `CreateTableDropPrimaryKey`
- [ ] `CreateTableAddPrimaryKeyInColumn`
- [ ] `CreateTableAddPrimaryKey`
- [ ] `CreateTableAddAutoIncrement`
- [ ] `CreateTableWithKeyBlockSize`
- [ ] `AlterTableAddSetTypeColumn`
- [ ] `AlterTableColumnFractionalSecondsPart`
- [ ] `TableComment`
- [ ] `RemoveTableComment`

#### From existing tests.yml → `tests_indices.yml`:
- [ ] `CreateTableUniqueIndex`

#### From existing tests.yml → `tests_constraints.yml`:
- [ ] `ConstraintCheck`
- [ ] `ColumnCheck`
- [ ] `ForeignKeyNormalizeRestrict`
- [ ] `AddForeignKeyWithAlter`

#### From existing tests.yml → `tests_datatypes.yml`:
- [ ] `BooleanValue`
- [ ] `AddColumnWithDefaultExpression`
- [ ] `AddDefaultExpression`
- [ ] `RemoveDefaultExpression`
- [ ] `CollateOnColumn`

#### From existing tests.yml → `tests_generated.yml`:
- [ ] `CreateTableGeneratedAlwaysAs80`
- [ ] `CreateTableGeneratedAlwaysAsChangeExpr80`
- [ ] `CreateTableGeneratedAlwaysAsAbbreviation80`
- [ ] `CreateTableGeneratedAlwaysAsAbbreviationChangeExpr80`

#### From existing tests.yml → `tests_views_triggers.yml`:
- [ ] `MysqlSecurityTypeView`
- [ ] `CreateTriggerWithComplexStatements`
- [ ] `CreateTriggerWithMultipleStatements`

#### From existing tests.yml → `tests_special.yml`:
- [ ] `MysqlComment`
- [ ] `SubstrExpression`
- [ ] `SubstringExpression`
- [ ] `NonReservedColumnName`
- [ ] `PartitionByRange`
- [ ] `CreateTableWithSpatialTypesAndSpatialKey`
- [ ] `CreateTableWithSpatialTypesSRIDSpecified`

#### From existing tests.yml → `tests_mysql57.yml`:
- [ ] `CreateTableRemoveAutoIncrement57`
- [ ] `CreateTableRemoveAutoIncrementPrimaryKey57`

#### From existing tests.yml → `tests_mysql80.yml`:
- [ ] `CreateTableRemoveAutoIncrement80`
- [ ] `CreateTableRemoveAutoIncrementPrimaryKey80`
- [ ] `CreateTableWithAutoIncrementPrimaryKeyAndAddMorePrimaryKey`
- [ ] `UUIDToBin`
- [ ] `MysqlViewUsingWindowFuncOnlyOver`
- [ ] `MysqlViewUsingWindowFuncPartitionBy`
- [ ] `MysqlViewUsingWindowFuncOrderBy`
- [ ] `MysqlViewUsingWindowFuncPartitionByAndOrderBy`
- [ ] `MysqlViewUsingWindowFuncPartitionByAndOrderByAndCoalesce`

### 1. Core Schema Operations → `tests_tables.yml`
**MIGRATE**: Table creation, modification, and column operations
- [ ] `TestMysqldefCreateTableChangePrimaryKey` → Add as `CreateTableChangePrimaryKey`
- [ ] `TestMysqldefCreateTableChangePrimaryKeyWithComment` → Add as `CreateTableChangePrimaryKeyWithComment`
- [ ] `TestMysqldefCreateTableAddAutoIncrementPrimaryKey` → Add as `CreateTableAddAutoIncrementPrimaryKey`
- [ ] `TestMysqldefCreateTableKeepAutoIncrement` → Add as `CreateTableKeepAutoIncrement`
- [ ] `TestMysqldefAddColumn` → Add as `AddColumn`
- [ ] `TestMysqldefAddColumnAfter` → Add as `AddColumnAfter`
- [ ] `TestMysqldefAddColumnWithNull` → Add as `AddColumnWithNull`
- [ ] `TestMysqldefChangeColumn` → Add as `ChangeColumn`
- [ ] `TestMysqldefChangeColumnLength` → Add as `ChangeColumnLength`
- [ ] `TestMysqldefChangeColumnBinary` → Add as `ChangeColumnBinary`
- [ ] `TestMysqldefChangeColumnCollate` → Add as `ChangeColumnCollate`
- [ ] `TestMysqldefChangeEnumColumn` → Add as `ChangeEnumColumn`
- [ ] `TestMysqldefChangeComment` → Add as `ChangeComment`
- [ ] `TestMysqldefSwapColumn` → Add as `SwapColumn`

### 2. Index Operations → `tests_indices.yml`
**MIGRATE**: All index-related operations
- [ ] `TestMysqldefCreateTableAddIndexWithKeyLength` → Add as `CreateTableAddIndexWithKeyLength`
- [ ] `TestMysqldefAddIndex` → Add as `AddIndex`
- [ ] `TestMysqldefAddIndexWithKeyLength` → Add as `AddIndexWithKeyLength`
- [ ] `TestMysqldefIndexOption` → Add as `IndexOption`
- [ ] `TestMysqldefMultipleColumnIndexesOption` → Add as `MultipleColumnIndexesOption`
- [ ] `TestMysqldefFulltextIndex` → Add as `FulltextIndex`
- [ ] `TestMysqldefCreateIndex` → Add as `CreateIndex`
- [ ] `TestMysqldefCreateTableKey` → Add as `CreateTableKey`
- [ ] `TestMysqldefCreateTableWithUniqueColumn` → Add as `CreateTableWithUniqueColumn`
- [ ] `TestMysqldefCreateTableChangeUniqueColumn` → Add as `CreateTableChangeUniqueColumn`
- [ ] `TestMysqldefIndexWithDot` → Add as `IndexWithDot`
- [ ] `TestMysqldefChangeIndexCombination` → Add as `ChangeIndexCombination`

### 3. Foreign Keys & Constraints → `tests_constraints.yml`
**MIGRATE**: Foreign key and constraint operations
- [ ] `TestMysqldefCreateTableForeignKey` → Add as `CreateTableForeignKey`

### 4. Data Types & Defaults → `tests_datatypes.yml`
**MIGRATE**: Data type and default value operations
- [ ] `TestMysqldefAutoIncrementNotNull` → Add as `AutoIncrementNotNull`
- [ ] `TestMysqldefTypeAliases` → Add as `TypeAliases`
- [ ] `TestMysqldefBoolean` → Add as `Boolean`
- [ ] `TestMysqldefDefaultNull` → Add as `DefaultNull`
- [ ] `TestMysqldefAddNotNull` → Add as `AddNotNull`
- [ ] `TestMysqldefCreateTableAddColumnWithCharsetAndNotNull` → Add as `CreateTableAddColumnWithCharsetAndNotNull`
- [ ] `TestMysqldefOnUpdate` → Add as `OnUpdate`
- [ ] `TestMysqldefCurrentTimestampWithPrecision` → Add as `CurrentTimestampWithPrecision`
- [ ] `TestMysqldefEnumValues` → Add as `EnumValues`
- [ ] `TestMysqldefDefaultValue` → Add as `DefaultValue`
- [ ] `TestMysqldefNegativeDefault` → Add as `NegativeDefault`
- [ ] `TestMysqldefDecimalDefault` → Add as `DecimalDefault`

### 5. Generated Columns → `tests_generated.yml`
**MIGRATE**: Generated column operations
- [ ] `TestMysqldefChangeGenerateColumnGemerayedAlwaysAs` → Add as `ChangeGenerateColumnGemerayedAlwaysAs`

### 6. Views & Triggers → `tests_views_triggers.yml`
**MIGRATE**: Views and triggers operations
- [ ] `TestMysqldefView` → Add as `View`
- [ ] `TestMysqldefTriggerInsert` → Add as `TriggerInsert`
- [ ] `TestMysqldefTriggerSetNew` → Add as `TriggerSetNew`
- [ ] `TestMysqldefTriggerBeginEnd` → Add as `TriggerBeginEnd`
- [ ] `TestMysqldefTriggerIf` → Add as `TriggerIf`

### 7. Special Cases → `tests_special.yml`
**MIGRATE**: Special syntax and edge cases
- [ ] `TestMysqldefColumnLiteral` → Add as `ColumnLiteral`
- [ ] `TestMysqldefHyphenNames` → Add as `HyphenNames`
- [ ] `TestMysqldefKeywordIndexColumns` → Add as `KeywordIndexColumns`
- [ ] `TestMysqldefMysqlDoubleDashComment` → Add as `MysqlDoubleDashComment`

### 8. Version-Constrained Tests (Split by Version)

#### 8a. MySQL 5.7 Tests → `tests_mysql57.yml`
**MIGRATE**: Tests that only work on MySQL 5.7 and below (`max_version: '5.7'`)
- [ ] Legacy AUTO_INCREMENT removal behavior tests
- [ ] MySQL 5.7-specific syntax variations

#### 8b. MySQL 8.0+ Tests → `tests_mysql80.yml`
**MIGRATE**: Tests that require MySQL 8.0+ (`min_version: '8.0'`)
- [ ] Generated columns tests (abbreviation forms)
- [ ] CHECK constraints tests  
- [ ] JSON default expressions tests
- [ ] Window function tests in views
- [ ] UUID_TO_BIN tests
- [ ] Complex AUTO_INCREMENT + PRIMARY KEY changes
- [ ] MySQL 8.0-specific features

### 9. Tests to Keep in Go File
**KEEP**: CLI-specific functionality that requires complex logic
- [ ] `TestApply` (main YAML test runner - **UPDATE** to load multiple files)
- [ ] `TestMysqldefFileComparison`
- [ ] `TestMysqldefApply`
- [ ] `TestMysqldefDryRun`
- [ ] `TestMysqldefExport`
- [ ] `TestMysqldefExportConcurrently`
- [ ] `TestMysqldefDropTable`
- [ ] `TestMysqldefSkipView`
- [ ] `TestMysqldefBeforeApply`
- [ ] `TestMysqldefConfigIncludesTargetTables`
- [ ] `TestMysqldefConfigIncludesSkipTables`
- [ ] `TestMysqldefConfigIncludesAlgorithm`
- [ ] `TestMysqldefConfigIncludesLock`
- [ ] `TestMysqldefHelp`
- [ ] `TestMain`
- [ ] `TestMysqldefCreateTableSyntaxError` (expects failure)

## Implementation Steps

**IMPORTANT**: Remember to check off completed tasks in this file before committing!

### Phase 1: Initial Setup & First Split
- [x] **Commit 1**: Update TestApply to load multiple YAML files + Create tests_tables.yml from existing tests.yml
  - [x] Update TestApply function to load all test_*.yml files 
  - [x] Create tests_tables.yml with table-related tests from existing tests.yml
  - [x] Run `make test-mysqldef` to verify
  - [x] Commit changes

**IMPORTANT**: All new YAML files must end with a newline character to avoid test formatting issues.

### Phase 2: Split Existing tests.yml (One commit per file)
- [x] **Commit 2**: Create tests_indices.yml 
  - [x] Move index-related tests from tests.yml
  - [x] Run `make test-mysqldef` to verify
  - [x] Commit changes
- [x] **Commit 3**: Create tests_constraints.yml
  - [x] Move constraint-related tests from tests.yml  
  - [x] Run `make test-mysqldef` to verify
  - [x] Commit changes
- [x] **Commit 4**: Create tests_datatypes.yml
  - [x] Move datatype-related tests from tests.yml
  - [x] Run `make test-mysqldef` to verify  
  - [x] Commit changes
- [x] **Commit 5**: Create tests_generated.yml
  - [x] Move generated column tests from tests.yml
  - [x] Run `make test-mysqldef` to verify
  - [x] Commit changes
- [x] **Commit 6**: Create tests_views_triggers.yml
  - [x] Move view/trigger tests from tests.yml
  - [x] Run `make test-mysqldef` to verify
  - [x] Commit changes
- [x] **Commit 7**: Create tests_special.yml  
  - [x] Move special case tests from tests.yml
  - [x] Run `make test-mysqldef` to verify
  - [x] Commit changes
- [x] **Commit 8**: Create tests_mysql57.yml
  - [x] Move MySQL 5.7 specific tests from tests.yml
  - [x] Run `make test-mysqldef` to verify
  - [x] Commit changes
- [x] **Commit 9**: Create tests_mysql80.yml
  - [x] Move MySQL 8.0+ specific tests from tests.yml
  - [x] Run `make test-mysqldef` to verify
  - [x] Commit changes
- [x] **Commit 10**: Remove/rename original tests.yml
  - [x] Move remaining test to tests_tables.yml
  - [x] Remove original tests.yml (now empty)
  - [x] Run `make test-mysqldef` to verify
  - [x] Commit changes

### Phase 3: Migrate Go Tests (One commit per test function)
- [ ] **Migrate tables tests** (14 commits):
  - [x] Commit: `TestMysqldefCreateTableChangePrimaryKey` → tests_tables.yml
  - [x] Commit: `TestMysqldefCreateTableChangePrimaryKeyWithComment` → tests_tables.yml
  - [x] Commit: `TestMysqldefCreateTableAddAutoIncrementPrimaryKey` → tests_tables.yml
  - [x] Commit: `TestMysqldefCreateTableKeepAutoIncrement` → tests_tables.yml
  - [x] Commit: `TestMysqldefAddColumn` → tests_tables.yml
  - [x] Commit: `TestMysqldefAddColumnAfter` → tests_tables.yml
  - [x] Commit: `TestMysqldefAddColumnWithNull` → tests_tables.yml
  - [x] Commit: `TestMysqldefChangeColumn` → tests_tables.yml
  - [x] Commit: `TestMysqldefChangeColumnLength` → tests_tables.yml
  - [x] Commit: `TestMysqldefChangeColumnBinary` → tests_tables.yml
  - [x] Commit: `TestMysqldefChangeColumnCollate` → tests_tables.yml
  - [x] Commit: `TestMysqldefChangeEnumColumn` → tests_tables.yml
  - [x] Commit: `TestMysqldefChangeComment` → tests_tables.yml
  - [x] Commit: `TestMysqldefSwapColumn` → tests_tables.yml

- [ ] **Migrate indices tests** (12 commits):
  - [x] Commit: `TestMysqldefCreateTableAddIndexWithKeyLength` → tests_indices.yml
  - [x] Commit: `TestMysqldefAddIndex` → tests_indices.yml
  - [x] Commit: `TestMysqldefAddIndexWithKeyLength` → tests_indices.yml
  - [x] Commit: `TestMysqldefIndexOption` → tests_indices.yml
  - [x] Commit: `TestMysqldefMultipleColumnIndexesOption` → tests_indices.yml
  - [x] Commit: `TestMysqldefFulltextIndex` → tests_indices.yml
  - [x] Commit: `TestMysqldefCreateIndex` → tests_indices.yml
  - [x] Commit: `TestMysqldefCreateTableKey` → tests_indices.yml
  - [x] Commit: `TestMysqldefCreateTableWithUniqueColumn` → tests_indices.yml
  - [x] Commit: `TestMysqldefCreateTableChangeUniqueColumn` → tests_indices.yml
  - [x] Commit: `TestMysqldefIndexWithDot` → tests_indices.yml
  - [x] Commit: `TestMysqldefChangeIndexCombination` → tests_indices.yml

- [ ] **Migrate constraints tests** (1 commit):
  - [ ] Commit: `TestMysqldefCreateTableForeignKey` → tests_constraints.yml

- [ ] **Migrate datatypes tests** (12 commits):
  - [ ] Commit: `TestMysqldefAutoIncrementNotNull` → tests_datatypes.yml
  - [ ] Commit: `TestMysqldefTypeAliases` → tests_datatypes.yml
  - [ ] Commit: `TestMysqldefBoolean` → tests_datatypes.yml
  - [ ] Commit: `TestMysqldefDefaultNull` → tests_datatypes.yml
  - [ ] Commit: `TestMysqldefAddNotNull` → tests_datatypes.yml
  - [ ] Commit: `TestMysqldefCreateTableAddColumnWithCharsetAndNotNull` → tests_datatypes.yml
  - [ ] Commit: `TestMysqldefOnUpdate` → tests_datatypes.yml
  - [ ] Commit: `TestMysqldefCurrentTimestampWithPrecision` → tests_datatypes.yml
  - [ ] Commit: `TestMysqldefEnumValues` → tests_datatypes.yml
  - [ ] Commit: `TestMysqldefDefaultValue` → tests_datatypes.yml
  - [ ] Commit: `TestMysqldefNegativeDefault` → tests_datatypes.yml
  - [ ] Commit: `TestMysqldefDecimalDefault` → tests_datatypes.yml

- [ ] **Migrate generated tests** (1 commit):
  - [ ] Commit: `TestMysqldefChangeGenerateColumnGemerayedAlwaysAs` → tests_generated.yml

- [ ] **Migrate views/triggers tests** (5 commits):
  - [ ] Commit: `TestMysqldefView` → tests_views_triggers.yml
  - [ ] Commit: `TestMysqldefTriggerInsert` → tests_views_triggers.yml
  - [ ] Commit: `TestMysqldefTriggerSetNew` → tests_views_triggers.yml
  - [ ] Commit: `TestMysqldefTriggerBeginEnd` → tests_views_triggers.yml
  - [ ] Commit: `TestMysqldefTriggerIf` → tests_views_triggers.yml

- [ ] **Migrate special tests** (4 commits):
  - [ ] Commit: `TestMysqldefColumnLiteral` → tests_special.yml
  - [ ] Commit: `TestMysqldefHyphenNames` → tests_special.yml
  - [ ] Commit: `TestMysqldefKeywordIndexColumns` → tests_special.yml
  - [ ] Commit: `TestMysqldefMysqlDoubleDashComment` → tests_special.yml

### Phase 4: Final Cleanup
- [ ] **Final verification**: Run `make test-mysqldef` to ensure all tests pass
- [ ] **Update documentation** if needed

