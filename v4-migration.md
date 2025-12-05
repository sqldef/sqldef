# Migrating to sqldef v4

This document describes the incompatible changes in sqldef v4 and how to migrate from v3.

## Breaking Changes

### 1. Explicit `--apply` flag required

**Change**: Commands now default to `--dry-run` mode. You must explicitly pass `--apply` to execute DDL statements.

**Before (v3)**:
```sh
psqldef mydb < schema.sql        # Applies changes immediately
psqldef mydb --dry-run < schema.sql  # Preview only
```

**After (v4)**:
```sh
psqldef mydb < schema.sql        # Preview only (dry-run by default)
psqldef mydb --apply < schema.sql    # Applies changes
```

**Migration**: Add `--apply` to all scripts and CI/CD pipelines that execute schema changes.

### 2. Quote-aware mode by default (`legacy_ignore_quotes: false`)

**Change**: Identifiers are now quoted only when they appear quoted in the source SQL. Previously, all identifiers were always quoted in the output.

**Why this matters**: In PostgreSQL (and most SQL databases), unquoted and quoted identifiers have different semantics:

- **Unquoted identifiers** are case-insensitive and normalized to lowercase. `users`, `Users`, and `USERS` all refer to the same table.
- **Quoted identifiers** are case-sensitive and preserve exact casing. `"users"` and `"Users"` are *different* tables.

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
-- Creates table "users" (lowercase), accessible as users, Users, or USERS

-- Quoted input (case-sensitive, exact casing preserved)
CREATE TABLE "Users" ("Id" int);
-- v4 output: CREATE TABLE "Users" ("Id" int);
-- Creates table "Users" (mixed case), accessible only as "Users"
```

**Migration**: If you rely on the old behavior, set `legacy_ignore_quotes: true` in your configuration file.

To restore v3 behavior, create a configuration file with:

```yaml
legacy_ignore_quotes: true
```

And pass it with `--config config.yml`.


### 3. Generic parser only for psqldef (no pgquery fallback)

**Change**: `psqldef` now uses only the generic SQL parser. The fallback to the `go-pgquery` (libpg_query) parser has been removed.

**Impact**: Some PostgreSQL-specific syntax that was previously handled by pgquery may require updates to the generic parser. If you encounter parsing errors, please report them as issues.

**Migration**: Test your schemas with `PSQLDEF_PARSER=generic` before upgrading to identify any parsing issues.

