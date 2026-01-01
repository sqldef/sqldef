# Object Management

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

* Easy things should be easy, and hard things should be possible.
* Fine-grained control over which objects are managed and what operations are allowed
* Existing configurations continue to work; new features don't break current behavior

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
    - target: 'staging'
      drop: true

  table:
    - target: 'temp_.*'
      drop: true
    - schema: staging
      drop: true

  view:

  materialized_view:
    - target: 'cached_.*'
      drop: true

  index:
    - drop: true

  function:
    - schema: utils

  procedure:
    - target: 'sync_.*'

  trigger:
    - target: 'audit_.*'

  sequence:
    - target: '.*_seq'

  type:

  domain:

  policy:
    - target: 'tenant_.*'

  extension:
    - target: 'pgcrypto'
    - target: 'uuid-ossp'

  privilege:
    - target: 'readonly_.*'
    - target: 'temp_.*'
      drop: true  # allows REVOKE
```

### MySQL/SQLite Example

```yaml
manage:
  table:
    - target: 'temp_.*'
      drop: true

  index:
    - drop: true
```

## Field Reference

### Manage Block Fields

| Field | Description | Default |
|-------|-------------|---------|
| `default_schema` | Default schema for entries without `schema` field (psqldef/mssqldef only) | Database default (`public` / `dbo`) |

### Entry Fields

| Field | Description | Default |
|-------|-------------|---------|
| `schema` | Schema to match (regexp pattern); psqldef/mssqldef only | value of `default_schema` |
| `table` | Table to match (regexp pattern); trigger/policy only | (matches all tables in matched schema) |
| `target` | Regexp pattern to match object names | (matches all) |
| `drop` | Allow DROP of the object itself | `false` |
| `drop_column` | Allow ALTER TABLE ... DROP COLUMN (table only) | value of `drop` |
| `drop_constraint` | Allow ALTER TABLE ... DROP CONSTRAINT (table only) | value of `drop` |
| `drop_index` | Allow DROP INDEX on this table (table only) | value of `drop` |
| `partition` | Manage partition DDL for this table (table only); see Partition Handling | `true` |

## Pattern Syntax

The `target`, `schema`, and `table` fields use **regular expressions** with implicit anchoring.

Patterns are automatically wrapped with `^` and `$` anchors, so `users` matches exactly `users`, not `all_users` or `users_backup`. To match substrings, use `.*` explicitly (e.g., `.*users.*`).

| Pattern | Matches |
|---------|---------|
| `users` | Exactly `users` |
| `users_.*` | `users_`, `users_archive`, `users_backup`, etc. |
| `.*_users` | `app_users`, `admin_users`, etc. |
| `temp_\d+` | `temp_1`, `temp_123`, etc. |
| (omitted) | All objects |

### Schema Field (PostgreSQL/SQL Server)

For databases with schemas, use the `schema` field to specify which schema(s) to match:

| `schema` | `target` | Matches |
|----------|----------|---------|
| (omitted) | `users` | `{default_schema}.users` |
| `staging` | `users` | `staging.users` |
| `staging` | (omitted) | All objects in `staging` schema |
| `'.*'` | `temp_.*` | `/^temp_.*$/` tables in any schema |
| (omitted) | (omitted) | All objects in `default_schema` |

Rules:
- `schema` field is a regexp pattern (literal names work as-is since they match themselves)
- If `schema` is omitted, `default_schema` is used
- `target` matches against object names only (not qualified names)
- Using `schema` or `default_schema` with mysqldef/sqlite3def is an error

### Partition Handling

Partition tables are regular tables and follow normal matching rules. To manage a partitioned table and its partitions, both must match patterns:

```yaml
manage:
  table:
    - target: 'orders'
      partition: true
    - target: 'orders_\d{4}'  # partitions need explicit matching
```

| `partition` value | Behavior |
|-------------------|----------|
| `true` (default) | Partition DDL (ATTACH/DETACH PARTITION, etc.) is managed for this table |
| `false` | Partition DDL is ignored; useful when partitions are managed externally |

### Trigger/Policy Fields

For triggers and policies, use both `schema` and `table` fields:

| `schema` | `table` | `target` | Matches |
|----------|---------|----------|---------|
| (omitted) | (omitted) | (omitted) | All triggers/policies in `default_schema` |
| `public` | (omitted) | (omitted) | All triggers/policies in `public` schema |
| `staging` | `users` | (omitted) | All triggers/policies on `staging.users` |
| (omitted) | `foo_.*` | `audit_.*` | `/^audit_.*$/` triggers/policies on `/^foo_.*$/` tables in `{default_schema}` |
| `'.*'` | `'.*'` | (omitted) | All triggers/policies in all schemas |

Rules:
- `schema` defaults to `default_schema`
- `table` defaults to all tables in the matched schema
- `target` matches against trigger/policy names only

## Behavior

### When `manage:` is specified
- Object types not listed are ignored (allow-list model)
- Within each type, only objects matching patterns are managed
- Non-matching objects are skipped (see Skipped Object Notices)
- An empty section (e.g., `view:` with no entries) means all objects of that type are managed with `drop: false`
- Managing an object type includes CREATE and ALTER operations; DROP is controlled separately by the `drop` field

### When `manage:` is NOT specified
- All objects are managed (current default behavior)
- Existing options (`target_tables`, `skip_tables`, `enable_drop`, etc.) continue to work

### Pattern Matching
- **First match wins**: entries are evaluated in order
- `schema` field matches against schema names (PostgreSQL/SQL Server only)
- `table` field matches against table names (triggers/policies only)
- `target` field matches against object names
- Case sensitivity follows the database's rules

### Pattern Ordering
List patterns from most specific to most general:

```yaml
manage:
  table:
    - target: 'users'
    - target: 'temp_.*'
      drop: true
```

### Skipped Object Notices

When an object exists in the database but doesn't match any pattern, sqldef logs a notice and skips it. This helps catch unintentional configuration gaps. Suppress with `--quiet`.

## Drop Control

The `drop`, `drop_column`, `drop_constraint`, and `drop_index` fields control destructive operations:

| Field | Scope | Operations |
|-------|-------|------------|
| `drop` | Object | DROP TABLE, DROP VIEW, DROP INDEX, etc. |
| `drop` | Privilege | REVOKE (for `privilege:` entries only) |
| `drop_column` | Column | ALTER TABLE ... DROP COLUMN |
| `drop_constraint` | Constraint | ALTER TABLE ... DROP CONSTRAINT |
| `drop_index` | Index | DROP INDEX (on table entries) |

`drop_column`, `drop_constraint`, and `drop_index` inherit from `drop` by default:

```yaml
manage:
  table:
    - target: 'users'

    - target: 'orders'
      drop_column: true
      drop_constraint: true
      drop_index: true

    - target: 'temp_.*'
      drop: true
```

Note: These settings control explicit destructive operations only. Implicit drops may still occur as side effects of other schema changes (e.g., dropping a column implicitly drops its constraints, changing a column type may recreate constraints). sqldef does not block such implicit operations.

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
| `domain` | ✓ | - | - | - |
| `policy` | ✓ | - | - | - |
| `extension` | ✓ | - | - | - |
| `privilege` | ✓ | ✓ | ✓ | - |

Using an unsupported object type emits a warning and is ignored.

## Privilege Management

The `privilege:` section controls which roles/users' privileges are managed. sqldef manages only GRANT/REVOKE statements on managed objects; it does not create or drop roles/users.

```yaml
manage:
  privilege:
    - target: 'readonly_user'
    - target: 'app_.*'
      drop: true  # allows REVOKE

  table:
    - target: 'users'
```

Behavior:
- `target` matches role/user names (regexp pattern)
- Roles/users must already exist (created externally)
- `drop: false` (default): only GRANT operations
- `drop: true`: both GRANT and REVOKE operations
- Privileges are managed only on objects listed in other `manage:` sections
- Roles are cluster-global in PostgreSQL; sqldef manages privileges per-database

## Schema Management

The `schema:` section (psqldef/mssqldef only) controls CREATE/DROP SCHEMA:
- If omitted, schemas are not managed (objects within them still are)
- Useful for multitenancy where schemas are managed by application code

## Dependency Validation

If a managed object references an unmanaged object, sqldef **errors** and refuses to apply. This validation runs in both `--dry-run` and `--apply` modes.

Examples:

**Table depending on function:**
```yaml
manage:
  table:
  # function: not listed → error if any table uses a function in DEFAULT, CHECK, etc.
```

**Trigger on unmanaged table:**
```yaml
manage:
  trigger:
  # table: not listed → error if any trigger references an unmanaged table
```

**Explicit index on managed table:**
```yaml
manage:
  table:
  # index: not listed → error if CREATE INDEX exists on a managed table
```

Note: Indexes created implicitly by constraints (PRIMARY KEY, UNIQUE) are part of the table definition and don't require separate declaration in `index:`.

Error messages include suggestions for resolving the issue:

```
error: managed table "users" references unmanaged function "generate_uuid"

To fix this, add the function to your configuration:

  manage:
    table:
      ...
    function:
      - target: 'generate_uuid'
```

This ensures:
- No broken references at apply time
- Users explicitly manage all dependencies
- No orphaned or stale dependent objects

### Cross-Schema References

When a managed object references an object in an unmanaged schema (e.g., a foreign key from `public.orders` to `staging.users` where only `public` is managed), sqldef emits a **warning** but proceeds. This allows schemas to be managed independently while alerting users to external dependencies.

## Deprecation Path

| Deprecated | Replacement |
|------------|-------------|
| `enable_drop: true` | Set `drop: true` per entry |
| `target_tables` | `manage.table[].target` |
| `skip_tables` | Use allow-list instead |
| `skip_views` | Use allow-list instead |
| `target_schema` | Use `default_schema` or `schema` field |
| `--skip-view` | Omit `view` from `manage` |
| `--skip-extension` | Omit `extension` from `manage` |
| `--skip-partition` | Set `partition: false` on table entries |
| `managed_roles` | `manage.privilege[].target` |

Transition:
1. Both old and new options work
2. If `manage:` is specified, deprecated options are ignored
3. Emit deprecation warnings when mixing old and new options

## Configuration Generation

sqldef provides a way to generate a `manage:` configuration listing all supported object types for the tool. This helps users:

1. **Migrate from deprecated options** - start with a complete configuration and customize
2. **Discover available object types** - see what can be managed
3. **Bootstrap new projects** - get a working configuration quickly

Example output for psqldef:

```yaml
manage:
  schema:
  table:
  view:
  materialized_view:
  index:
  function:
  procedure:
  trigger:
  sequence:
  type:
  domain:
  policy:
  extension:
  privilege:
```

Users can then:
- Remove object types they don't want managed
- Add `target:` patterns to filter specific objects
- Add `drop: true` to entries where destructive operations are allowed
- Add `privilege:` entries for roles/users whose privileges should be managed

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
    - target: 'temp_.*'
      drop: true
```

### Allow dropping indexes but not tables

```yaml
manage:
  table:

  index:
    - drop: true
```

### Multi-schema setup

```yaml
manage:
  default_schema: app

  table:
    - schema: staging
      drop: true

  function:
```

### Schema-based multi-tenancy (manage all schemas)

```yaml
manage:
  table:
    - schema: '.*'
```

Note: `schema: '.*'` matches any schema. Combined with omitted `target`, this matches all tables in all schemas.

### Complex patterns

```yaml
manage:
  table:
    - target: 'archive_\d{4}'
      drop: true
```

### Skip partition management

```yaml
manage:
  table:
    - target: 'orders'
      partition: false
```
