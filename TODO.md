# TODO for future improvements

This document includes TODOs for future improvements.

NOTE: do not keep the resolved issues here

## The `splitDDLs` function

The `splitDDLs()` function in `database/parser.go` has comments:

```go
// Right now, the parser isn't capable of splitting statements by itself.
// So we just attempt parsing until it succeeds. I'll let the parser do it in the future.
```

So once the parser is capable of splitting statements by itself, we can remove this function.

## DDL Generator Operation Ordering Bugs

**Status**: 4 tests failing due to DDL generator bugs (93% tests passing overall)

The migration from `output` to `up`/`down` test format revealed real bugs in the DDL generator where operations are generated in the wrong order, causing SQL errors when applied.

### MSSQL Foreign Key Ordering Issue (3 tests failing)

**Affected tests**: `ForeignKeyDependenciesPrimaryKeyChange`, `ForeignKeyDependenciesMultipleToModifiedTable`, `ForeignKeyDependenciesCascadeOptionsPreservation`

**Problem**: Column alterations are generated before dropping foreign keys that reference those columns.

**Error**: `ALTER TABLE ALTER COLUMN supplier_id failed because one or more objects access this column`

**Current buggy output**:
```sql
ALTER TABLE [dbo].[items] ALTER COLUMN [supplier_id] int;  -- ❌ FAILS
ALTER TABLE [dbo].[item_prices] DROP CONSTRAINT [prices_item_fk];  -- Should be first
```

**Required fix**: In `schema/generator.go`, before generating column ALTER statements, check for foreign keys referencing those columns and generate DROP CONSTRAINT DDLs first.

**Additional issue**: Duplicate DROP/ADD CONSTRAINT statements are generated for the same foreign key.

### MySQL CHECK Constraint Ordering Issue (1 test failing)

**Affected test**: `TypedLiteralsInCheck`

**Problem**: Columns are dropped before CHECK constraints that reference them.

**Error**: `Error 3959: Check constraint 'chk_date_range' uses column 'event_end_date', hence column cannot be dropped`

**Current buggy output**:
```sql
ALTER TABLE `events` DROP COLUMN `event_end_date`;  -- ❌ FAILS
ALTER TABLE `events` DROP CHECK `chk_date_range`;  -- Should be first
```

**Required fix**: In `schema/generator.go` (around lines 491-500), analyze CHECK constraint dependencies and drop constraints before dropping columns they reference.

