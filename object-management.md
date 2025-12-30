# Object Management Configuration Specification

## Problem Statement

Currently, sqldef provides fragmented configuration options for filtering objects:
- `target_tables` / `skip_tables` - for tables
- `skip_views` - for views (no `target_views`)
- `target_schema` - for schemas
- `enable_drop` - global toggle for all DROP operations
- `--skip-view`, `--skip-extension`, `--skip-partition` flags

Limitations:
1. Inconsistent coverage across object types
2. No filtering for functions, procedures, triggers, sequences, types, policies
3. `enable_drop` is all-or-nothing; cannot allow DROP INDEX but forbid DROP TABLE
4. No per-pattern control (e.g., temp tables droppable, but users table not)

## Design Principles

This specification balances two goals:

1. **Development flexibility**: Fine-grained control over which objects are managed and what operations are allowed
2. **Backward compatibility**: Existing configurations continue to work; new features don't break current behavior

## Proposed Solution

Introduce a unified `manage:` configuration block with:
- **Per-object-type settings** (table, view, index, etc.)
- **Array of entries** with regexp patterns and drop permissions
- **First match wins** for overlapping patterns

## Configuration Schema

### PostgreSQL Example

```yaml
manage:
  default_schema: public

  schema:
    - target: 'public'
      drop: false
    - target: 'staging'
      drop: true

  table:
    - target: 'users'
      drop: false
    - target: 'temp_.*'
      drop: true
    - target: 'staging\..*'
      drop: true
    - drop: false

  view:
    - drop: false

  materialized_view:
    - target: 'cached_.*'
      drop: true

  index:
    - drop: true

  function:
    - target: 'utils\..*'
      drop: false

  procedure:
    - target: 'sync_.*'
      drop: false

  trigger:
    - target: '.*\.audit_.*'
      drop: false

  sequence:
    - target: '.*_seq'
      drop: false

  type:
    - drop: false

  policy:
    - target: '.*\.tenant_.*'
      drop: false

  extension:
    - target: 'pgcrypto'
      drop: false
    - target: 'uuid-ossp'
      drop: false
```

### MySQL/SQLite Example

```yaml
manage:
  table:
    - target: 'users'
      drop: false
    - target: 'temp_.*'
      drop: true
    - drop: false

  index:
    - drop: true
```

## Field Reference

### Manage Block Fields

| Field | Description | Default |
|-------|-------------|---------|
| `default_schema` | Schema for patterns without `\.` prefix (psqldef/mssqldef only) | Database default (`public` / `dbo`) |

### Entry Fields

| Field | Description | Default |
|-------|-------------|---------|
| `target` | Regexp pattern to match object names | (matches all) |
| `drop` | Allow DROP of the object itself | `false` |
| `drop_column` | Allow ALTER TABLE ... DROP COLUMN (table only) | value of `drop` |
| `drop_constraint` | Allow ALTER TABLE ... DROP CONSTRAINT (table only) | value of `drop` |
| `partition` | Manage partitions of this table (table only) | `true` |

## Pattern Syntax

Patterns use **regular expressions** with implicit anchoring (`^pattern$`):

| Pattern | Matches |
|---------|---------|
| `users` | Exactly `users` |
| `users_.*` | `users_`, `users_archive`, `users_backup`, etc. |
| `temp_\d+` | `temp_1`, `temp_123`, etc. |
| (omitted) | All objects |

### Schema Resolution (PostgreSQL/SQL Server)

For databases with schemas, patterns are matched against **fully qualified names** (`schema.name`):

| Pattern | Matches |
|---------|---------|
| `users` | `{default_schema}.users` |
| `staging\.users` | `staging.users` |
| `staging\..*` | All in `staging` schema |
| (omitted) | All objects in all schemas |

Rules:
- Patterns without `\.` are prefixed with `default_schema` before matching
- Patterns with `\.` match against the full `schema.name`
- Using `default_schema` with mysqldef/sqlite3def is an error

## Behavior

### When `manage:` is specified
- Only listed object types are managed (allow-list)
- Within each type, only objects matching patterns are managed
- Non-matching objects are ignored with NOTICE log (suppress with `--quiet`)

### When `manage:` is NOT specified
- All objects are managed (current default behavior)
- Existing options (`target_tables`, `skip_tables`, `enable_drop`, etc.) continue to work

### Pattern Matching
- **First match wins**: entries are evaluated in order
- Patterns match against fully qualified names:
  - Schemas: `name`
  - Tables, views, functions, etc.: `schema.name` (or `name` for MySQL/SQLite)
  - Triggers: `schema.table.trigger_name` (or `table.trigger_name`)
  - Policies: `schema.table.policy_name`
  - Extensions: `name`
- Case sensitivity follows the database's rules

### Pattern Ordering
List patterns from most specific to most general:

```yaml
manage:
  table:
    - target: 'users'
      drop: false
    - target: 'temp_.*'
      drop: true
    - drop: false
```

## Drop Control

The `drop`, `drop_column`, and `drop_constraint` fields control destructive operations:

| Field | Scope | Operations |
|-------|-------|------------|
| `drop` | Object | DROP TABLE, DROP VIEW, DROP INDEX, REVOKE, etc. |
| `drop_column` | Column | ALTER TABLE ... DROP COLUMN |
| `drop_constraint` | Constraint | ALTER TABLE ... DROP CONSTRAINT |

`drop_column` and `drop_constraint` inherit from `drop` by default:

```yaml
manage:
  table:
    - target: 'users'
      drop: false

    - target: 'orders'
      drop: false
      drop_column: true
      drop_constraint: true

    - target: 'temp_.*'
      drop: true
```

## Supported Object Types

| Object Type | psqldef | mysqldef | mssqldef | sqlite3def |
|-------------|---------|----------|----------|------------|
| `schema` | ✓ | - | ✓ | - |
| `table` | ✓ | ✓ | ✓ | ✓ |
| `view` | ✓ | ✓ | ✓ | ✓ |
| `materialized_view` | ✓ | - | - | - |
| `index` | ✓ | ✓ | ✓ | ✓ |
| `function` | ✓ | ✓ | ✓ | - |
| `procedure` | ✓ | ✓ | ✓ | - |
| `trigger` | ✓ | ✓ | ✓ | ✓ |
| `sequence` | ✓ | - | ✓ | - |
| `type` | ✓ | - | ✓ | - |
| `policy` | ✓ | - | - | - |
| `extension` | ✓ | - | - | - |

Using an unsupported object type is an error.

## Schema Management

The `schema:` section (psqldef/mssqldef only) controls CREATE/DROP SCHEMA:
- If omitted, schemas are not managed (objects within them still are)
- Useful for multitenancy where schemas are managed by application code

## Dependency Validation

If a managed object references an unmanaged object, sqldef **errors** and refuses to apply.

Examples:

**Table depending on function:**
```yaml
manage:
  table:
    - drop: false
  # function: not listed → error if any table uses a function in DEFAULT, CHECK, etc.
```

**Trigger on unmanaged table:**
```yaml
manage:
  trigger:
    - drop: false
  # table: not listed → error if any trigger references an unmanaged table
```

This ensures:
- No broken references at apply time
- Users explicitly manage all dependencies
- No orphaned or stale dependent objects

## Implicit Objects

Objects "owned" by a managed object are **implicitly managed**:

| Parent | Owned Objects |
|--------|---------------|
| Table with `PRIMARY KEY` | Index for the primary key |
| Table with `UNIQUE` | Index for the unique constraint |
| Table with `SERIAL`/`BIGSERIAL` | Sequence for the column |
| Table with `IDENTITY` | Sequence for the column |

Example:

```sql
CREATE TABLE users (
  id SERIAL PRIMARY KEY,
  email TEXT UNIQUE
);
```

**Default behavior** (object type section not listed):

```yaml
manage:
  table:
    - drop: false
  # index: not listed → owned indexes implicitly managed
```

`users_pkey` and `users_email_key` are implicitly managed with the table.

**Explicit override** (object type section listed):

```yaml
manage:
  table:
    - drop: false

  index:
    - target: 'custom_.*'
      drop: true
```

Listing `index:` opts into explicit control. Only indexes matching the patterns are managed.

Note: Owned objects are exempt from dependency validation. A table can have unmanaged owned indexes without triggering an error, because they are part of the table definition itself.

## Deprecation Path

| Deprecated | Replacement |
|------------|-------------|
| `enable_drop: true` | Set `drop: true` per entry |
| `target_tables` | `manage.table[].target` |
| `skip_tables` | Use allow-list instead |
| `skip_views` | Use allow-list instead |
| `target_schema` | Use `default_schema` or schema prefix |
| `--skip-view` | Omit `view` from `manage` |
| `--skip-extension` | Omit `extension` from `manage` |
| `--skip-partition` | Set `partition: false` on table entries |

Transition:
1. Both old and new options work
2. If `manage:` is specified, deprecated options are ignored
3. Emit deprecation warnings when mixing old and new options

## Examples

### Manage specific tables

```yaml
manage:
  table:
    - target: 'users'
    - target: 'orders'
```

### Full control over a schema

```yaml
manage:
  default_schema: myapp

  table:
    - drop: true

  index:
    - drop: true
```

### Per-pattern drop control

```yaml
manage:
  table:
    - target: 'users'
      drop: false
    - target: 'temp_.*'
      drop: true
    - drop: false
```

### Allow dropping indexes but not tables

```yaml
manage:
  table:
    - drop: false

  index:
    - drop: true
```

### Multi-schema setup

```yaml
manage:
  default_schema: app

  table:
    - target: 'staging\..*'
      drop: true
    - drop: false

  function:
    - drop: false
```

### Multitenancy (manage all schemas)

```yaml
manage:
  table:
    - target: '.*\..*'
      drop: false
```

Note: Pattern `.*\..*` matches any `schema.table`. Tables are matched against their fully qualified names.

### Complex patterns

```yaml
manage:
  table:
    - target: 'archive_\d{4}'
      drop: true
    - drop: false
```

### Skip partition management

```yaml
manage:
  table:
    - target: 'orders'
      drop: false
      partition: false
```
