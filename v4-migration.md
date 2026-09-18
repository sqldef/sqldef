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

### 4. Explicit `FOR EACH` clause required for `CREATE TRIGGER` (psqldef)

**Change**: `CREATE TRIGGER` without a `FOR EACH` clause is now rejected. Write `FOR EACH ROW` or `FOR EACH STATEMENT` explicitly.

**Why this matters**: An omitted clause means different things to PostgreSQL and to v3. PostgreSQL defaults it to `FOR EACH STATEMENT`, while v3 created a row-level trigger:

```sql
-- Your schema file
CREATE TRIGGER notify_change AFTER INSERT ON users EXECUTE FUNCTION notify_func();

-- v3 output (does not match PostgreSQL's own default)
CREATE TRIGGER notify_change AFTER INSERT ON users FOR EACH ROW EXECUTE FUNCTION notify_func();
```

Adopting PostgreSQL's default in v4 would silently drop and recreate every such trigger as a statement-level one. A statement-level trigger fires once per statement and its function cannot reference `NEW` or `OLD`, so a function written for a row-level trigger would start failing at runtime. v4 rejects the ambiguous form instead of picking either meaning:

```
error: CREATE TRIGGER requires a FOR EACH clause: notify_change
```

**Migration**: Add the clause to every `CREATE TRIGGER` in your schema file. v3 warns for each trigger that omits it, and `--export` always writes the clause, so exported schemas need no change. To keep the v3 behavior, write `FOR EACH ROW`:

```sql
CREATE TRIGGER notify_change AFTER INSERT ON users FOR EACH ROW EXECUTE FUNCTION notify_func();
```

### 5. Explicit ownership management required (psqldef)

**Change**: In v4, ownership management requires `manage.owner`. Neither `manage.privilege` nor the deprecated `managed_roles` implicitly enables it. The two settings have independent responsibilities:

| Setting | Managed SQL | What `target` matches |
|---------|-------------|-----------------------|
| `manage.owner` | `ALTER ... OWNER TO` | Owner role names |
| `manage.privilege` | `GRANT` / `REVOKE` | Grantee role names |

Omitting `manage.owner` leaves ownership unmanaged: desired `OWNER TO` declarations are ignored, and `--export` omits ownership statements. `manage.privilege` continues to manage access rights; its `drop` option controls REVOKE operations.

**Before (v3)**: Configuring `manage.privilege` or `managed_roles` also enables ownership management for every owner role, unless explicit `manage.owner` rules override that behavior.

```yaml
manage:
  privilege:
    - target: readonly_user
      drop: true
```

**After (v4), preserving the v3 behavior**:

```yaml
manage:
  owner: []
  privilege:
    - target: readonly_user
      drop: true
```

**Migration**: Add `manage.owner: []` to continue managing every owner role. An empty array enables ownership management for all roles; omitting the key disables it in v4. To limit ownership management, specify owner-role targets instead. Copying privilege targets into `manage.owner` would narrow the previous ownership scope, since grantees and owners may be different roles.

Users of `managed_roles` also need an explicit `manage.owner` section to preserve ownership management. When migrating their access-rights configuration to `manage.privilege`, configure owner roles separately.

Explicit `manage.owner` is supported in v3 versions that include this feature, so this configuration can be prepared before upgrading. The implicit fallback remains in v3 for compatibility; its removal is a v4 change.

## Migration Checklist

Before upgrading to v4:

- [ ] Add `--apply` flag to all scripts and CI/CD pipelines
- [ ] Test with `--config-inline 'legacy_ignore_quotes: false'` to preview the new quoting behavior
- [ ] Test with `PSQLDEF_PARSER=generic` (for psqldef users)
- [ ] Add an explicit `FOR EACH ROW` / `FOR EACH STATEMENT` clause to every `CREATE TRIGGER` in your schema file (for psqldef users)
- [ ] Add `manage.owner: []` or explicit owner-role targets if you rely on ownership management enabled by `manage.privilege` or `managed_roles` (for psqldef users)
- [ ] If you have existing tables with mixed-case names created by v3, update your schema file to use quoted identifiers
