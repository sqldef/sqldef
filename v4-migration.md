# Migrating to sqldef v4

This document describes the breaking changes in sqldef v4 and how to migrate from v3.

**Before upgrading**, test your schemas in a local environment to identify any issues.

## Breaking Changes

### 1. Explicit `--apply` flag required

**Change**: Commands now default to `--dry-run` mode. You must explicitly pass `--apply` to execute DDL statements.

**Before (v3)**:
```sh
psqldef mydb < schema.sql            # Applies changes immediately
psqldef mydb --apply < schema.sql    # ditto
psqldef mydb --dry-run < schema.sql  # Preview only
```

**After (v4)**:
```sh
psqldef mydb < schema.sql            # Preview only (dry-run by default)
psqldef mydb --dry-run < schema.sql  # Preview only (recommended)
psqldef mydb --apply < schema.sql    # Applies changes
```

**Migration**: Add `--apply` to all scripts and CI/CD pipelines that execute schema changes. The `--apply` flag is already available in v3, so you can update your scripts before upgrading.

### 2. Quote-aware mode by default (`legacy_ignore_quotes: false`)

**Change**: Identifiers are now quoted only when they appear quoted in the source SQL. Previously, all identifiers were always quoted in the output.

**Why this matters**: In PostgreSQL, unquoted and quoted identifiers have different semantics:

- **Unquoted identifiers** are case-insensitive and stored as lowercase. `users`, `Users`, and `USERS` all refer to the same table.
- **Quoted identifiers** are case-sensitive and stored with exact casing. `"users"` and `"Users"` are *different* tables.

The v3 behavior of always quoting identifiers could cause schema drift and unexpected behavior:

```sql
-- Your schema file
CREATE TABLE Users (Id int);

-- v3 output (problematic)
CREATE TABLE "Users" ("Id" int);
-- This creates a case-sensitive table "Users", not the intended lowercase table "users"!

-- If your application queries: SELECT * FROM users;
-- PostgreSQL looks for lowercase "users", but the table is actually "Users"
-- Result: ERROR: relation "users" does not exist
```

With v4, sqldef preserves your original intent:

```sql
-- Unquoted input (case-insensitive, stored as lowercase)
CREATE TABLE Users (Id int);
-- v4 output: CREATE TABLE Users (Id int);
-- Creates table stored as "users", accessible as users, Users, or USERS

-- Quoted input (case-sensitive, exact casing preserved)
CREATE TABLE "Users" ("Id" int);
-- v4 output: CREATE TABLE "Users" ("Id" int);
-- Creates table "Users" (mixed case), accessible only as "Users"
```

**Migrating existing databases**: If you have tables created by v3 using unquoted identifiers, upgrading to v4 can cause issues:

```sql
-- Your schema file (unchanged)
CREATE TABLE Users (Id int);

-- What v3 created in the database
CREATE TABLE "Users" ("Id" int);  -- stored as mixed-case "Users"

-- What v4 now generates
CREATE TABLE Users (Id int);  -- refers to lowercase "users"

-- Problem: sqldef sees "Users" and "users" as different tables!
-- It may try to drop "Users" and create "users", causing data loss.
```

**Migration options** (in order of recommendation):

1. **Update your schema file** to use quoted identifiers matching the database:
   ```sql
   CREATE TABLE "Users" ("Id" int);
   ```
2. **Rename tables** in the database to lowercase (requires data migration)
3. **Use `legacy_ignore_quotes: true`** as a temporary workaround:
   ```yaml
   # config.yml
   legacy_ignore_quotes: true
   ```
   Then pass it with `--config config.yml`.

   **Note**: This option will be removed in a future major release.

### 3. Generic parser only for psqldef (no pgquery fallback)

**Change**: `psqldef` now uses only the generic SQL parser. The fallback to the `go-pgquery` (libpg_query) parser has been removed.

**Impact**: Some PostgreSQL-specific syntax previously handled by pgquery may not yet be supported by the generic parser. If you encounter parsing errors, please report them as issues.

**Migration**: Test your schemas before upgrading by setting `PSQLDEF_PARSER=generic`:

```sh
PSQLDEF_PARSER=generic psqldef mydb --dry-run < schema.sql
```

## Migration Checklist

Before upgrading to v4:

- [ ] Add `--apply` flag to all scripts and CI/CD pipelines
- [ ] Test with `--config-inline 'legacy_ignore_quotes: false'` to preview the new quoting behavior
- [ ] Test with `PSQLDEF_PARSER=generic` (for psqldef users)
- [ ] If you have existing tables with mixed-case names created by v3, update your schema file to use quoted identifiers
