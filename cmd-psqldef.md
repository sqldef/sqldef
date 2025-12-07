# psqldef

`psqldef` works the same way as `psql` for setting connection information.

```
Usage:
  psqldef [OPTION]... DBNAME --export
  psqldef [OPTION]... DBNAME --apply < desired.sql
  psqldef [OPTION]... DBNAME --dry-run < desired.sql
  psqldef [OPTION]... current.sql < desired.sql

Application Options:
  -U, --user=USERNAME         PostgreSQL user name (default: postgres)
  -W, --password=PASSWORD     PostgreSQL user password, overridden by $PGPASSWORD
  -h, --host=HOSTNAME         Host or socket directory to connect to the PostgreSQL server (default: 127.0.0.1)
  -p, --port=PORT             Port used for the connection (default: 5432)
      --password-prompt       Force PostgreSQL user password prompt
  -f, --file=FILENAME         Read desired SQL from the file, rather than stdin (default: -)
      --dry-run               Don't run DDLs but just show them
      --apply                 Apply DDLs to the database (default, but will require this flag in future versions)
      --export                Just dump the current schema to stdout
      --enable-drop           Enable destructive changes such as DROP for TABLE, SCHEMA, ROLE, USER, FUNCTION, PROCEDURE, TRIGGER, VIEW, INDEX, SEQUENCE, TYPE
      --skip-view             Skip managing views/materialized views
      --skip-extension        Skip managing extensions
      --before-apply=SQL      Execute the given string before applying the regular DDLs
      --config=PATH           YAML configuration file (can be specified multiple times)
      --config-inline=YAML    YAML configuration as inline string (can be specified multiple times)
      --help                  Show this help
      --version               Show version information
```

Use `PGSSLMODE` environment variable to specify sslmode.

## Synopsis

```shell
# Verify PostgreSQL server connectivity
$ psql -U postgres test -c "select 1;"
 ?column?
----------
        1
(1 row)

# Export current schema
$ psqldef -U postgres test --export
CREATE TABLE public.users (
    id bigint NOT NULL,
    name text,
    age integer
);

CREATE TABLE public.bigdata (
    data bigint
);

# Save it to edit
$ psqldef -U postgres test --export > schema.sql
```

Update schema.sql as follows:

```diff
 CREATE TABLE users (
     id bigint NOT NULL PRIMARY KEY,
-    name text,
     age int
 );

-CREATE TABLE bigdata (
-    data bigint
-);
```

And then run:

```shell
# Preview migration plan (dry run)
$ psqldef -U postgres test --dry-run < schema.sql
-- dry run --
BEGIN;
DROP TABLE bigdata;
ALTER TABLE users DROP COLUMN name;
COMMIT;

# Apply DDLs
$ psqldef -U postgres test --apply < schema.sql
-- Apply --
BEGIN;
DROP TABLE bigdata;
ALTER TABLE users DROP COLUMN name;
COMMIT;

# Operations are idempotent - safe to run multiple times
$ psqldef -U postgres test --apply < schema.sql
-- Nothing is modified --

# Run without dropping tables and columns
$ psqldef -U postgres test --apply < schema.sql
-- Skipped: DROP TABLE users;

# Run with drop operations enabled
$ psqldef -U postgres test --apply --enable-drop < schema.sql
-- Apply --
BEGIN;
DROP TABLE users;
COMMIT;

# Managing table privileges for specific roles via config
$ cat schema.sql
CREATE TABLE users (
    id bigint NOT NULL PRIMARY KEY,
    name text
);
GRANT SELECT ON TABLE users TO readonly_user;
GRANT SELECT, INSERT, UPDATE ON TABLE users TO app_user;

# Use config file to filter tables and manage privileges
$ cat > config.yml <<EOF
target_tables: |
  public\.users
  public\.posts_\d+
skip_tables: |
  migrations
  temp_.*
skip_views: |
  materialized_view_.*
target_schema: |
  public
  app
managed_roles:
  - readonly_user
  - app_user
enable_drop: true
dump_concurrency: 4
create_index_concurrently: true
EOF
$ psqldef -U postgres test --apply --config=config.yml < schema.sql

# Use inline YAML configuration with managed roles
$ psqldef -U postgres test --apply --config-inline="managed_roles: [readonly_user, app_user]" < schema.sql

# Multiple configs (later values override earlier ones)
$ psqldef -U postgres test --apply --config=base.yml --config-inline="skip_tables: archived_.*" < schema.sql
```

## Offline Mode (File-to-File Comparison)

psqldef can compare two schema files **without connecting to a database**. This is useful for CI/CD pipelines, schema validation, and generating migration scripts.

### How It Works

When the database argument ends with `.sql`, psqldef operates in offline mode:

```shell
# Normal mode: connects to database
$ psqldef -U postgres mydb --apply < schema.sql

# Offline mode: compares two files (no database connection)
$ psqldef current.sql < desired.sql
```

In offline mode:
- No database connection is established
- The tool compares two SQL files (current vs desired)
- DDL statements are generated to show what would change
- Changes are **always** shown in dry-run mode (not applied to any database)

### Basic Usage

```shell
# Compare two schema files
$ psqldef current_schema.sql < desired_schema.sql
-- dry run --
BEGIN;
ALTER TABLE ""."users" ADD COLUMN "email" text NOT NULL;
CREATE INDEX idx_users_email ON users(email);
COMMIT;

# Using --file flag instead of stdin
$ psqldef --file desired_schema.sql current_schema.sql

# Verify idempotency (compare identical schemas)
$ psqldef desired_schema.sql < desired_schema.sql
-- Nothing is modified --
```

## Supported features

The following DDLs are generated by updating `CREATE TABLE`.
Some can also be used in the input schema.sql file.

- Tables: CREATE TABLE, DROP TABLE, ALTER TABLE RENAME TO, COMMENT ON TABLE
- Columns: ADD COLUMN, ALTER COLUMN, DROP COLUMN, ALTER COLUMN RENAME TO, GENERATED AS IDENTITY, COMMENT ON COLUMN
- Constraints: PRIMARY KEY, FOREIGN KEY, CHECK, UNIQUE, EXCLUDE, ADD CONSTRAINT, DROP CONSTRAINT
- Indexes: CREATE INDEX, CREATE UNIQUE INDEX, DROP INDEX, ALTER INDEX RENAME TO, CREATE INDEX CONCURRENTLY, WHERE
- Views: CREATE VIEW, CREATE OR REPLACE VIEW, DROP VIEW, CREATE MATERIALIZED VIEW, DROP MATERIALIZED VIEW
- Schemas: CREATE SCHEMA
- Extensions: CREATE EXTENSION, CREATE EXTENSION IF NOT EXISTS, DROP EXTENSION
- Types: CREATE TYPE, ENUM, ALTER TYPE ADD VALUE
- Privileges: GRANT, REVOKE (with managed_roles configuration)

## Example
### CREATE TABLE

```diff
+CREATE TABLE users (
+  id BIGINT PRIMARY KEY
+);
```

Remove the statement to DROP TABLE.

### ADD COLUMN

```diff
 CREATE TABLE users (
   id BIGINT PRIMARY KEY,
+  name VARCHAR(40)
 );
```

Remove the line to DROP COLUMN.

### CREATE INDEX

```diff
 CREATE TABLE users (
   id BIGINT PRIMARY KEY,
   name VARCHAR(40)
 );
+CREATE INDEX index_name on users (name);
```

Remove the line to DROP INDEX.

#### CREATE INDEX CONCURRENTLY

To create indexes without blocking writes, use the `create_index_concurrently` configuration:

```shell
# Using configuration file
$ cat > config.yml <<EOF
create_index_concurrently: true
EOF
$ psqldef -U postgres test --apply --config=config.yml < schema.sql

# Using inline configuration
$ psqldef -U postgres test --apply --config-inline="create_index_concurrently: true" < schema.sql

# Example output with create_index_concurrently enabled
-- Apply --
BEGIN;
ALTER TABLE users ADD COLUMN email VARCHAR(255);
COMMIT;
CREATE INDEX CONCURRENTLY idx_users_email ON users (email);  # Runs outside transaction
CREATE INDEX CONCURRENTLY idx_users_name ON users (name);    # Runs outside transaction
```

Note: CREATE INDEX CONCURRENTLY operations must run outside of transactions. When enabled, psqldef automatically separates these operations from the transaction block.

### ADD FOREIGN KEY

```diff
 CREATE TABLE users (
   id BIGINT PRIMARY KEY,
   name VARCHAR(40)
 );
 CREATE INDEX index_name on users (name);

 CREATE TABLE posts (
   user_id BIGINT,
+  CONSTRAINT fk_posts_user_id FOREIGN KEY (user_id) REFERENCES users (id)
 )
```

Remove the line to DROP CONSTRAINT.

### ADD POLICY

```diff
 CREATE TABLE users (
   id BIGINT PRIMARY KEY,
   name VARCHAR(40)
 );

+CREATE POLICY p_users ON users AS PERMISSIVE FOR ALL TO PUBLIC USING (id = (current_user)::integer) WITH CHECK ((name)::text = current_user)
```

Remove the line to DROP POLICY.

### CREATE (OR REPLACE) VIEW

```diff
 CREATE VIEW foo AS
   select u.id as id, p.id as post_id
   from  (
     mysqldef_test.users as u
     join mysqldef_test.posts as p on ((u.id = p.user_id))
   )
 ;
+ CREATE OR REPLACE VIEW foo AS select u.id as id, p.id as post_id from (users as u join posts as p on (((u.id = p.user_id) and (p.is_deleted = 0))));
```

Remove the line to DROP VIEW.

## Column, Table, and Index Renaming

### Column Renaming

psqldef supports renaming columns using the `-- @renamed from=old_name` annotation:

```sql
CREATE TABLE users (
  id bigint NOT NULL,
  user_name text, -- @renamed from=username
  age integer
);
```

This generates:
```sql
ALTER TABLE users RENAME COLUMN username TO user_name;
```

For columns with special characters or spaces, use double quotes:

```sql
CREATE TABLE users (
  id bigint NOT NULL,
  column_with_underscore varchar(50), -- @renamed from="column-with-dash"
  normal_column text, -- @renamed from="special column"
);
```

### Table Renaming

psqldef supports renaming tables using the `-- @renamed from=old_name` annotation on the CREATE TABLE line:

```sql
CREATE TABLE users ( -- @renamed from=user_accounts
  id bigint NOT NULL,
  username text,
  age integer
);
```

You can also use the block comment style:

```sql
CREATE TABLE users /* @renamed from=user_accounts */ (
  id bigint NOT NULL,
  username text,
  age integer
);
```

This generates:
```sql
ALTER TABLE user_accounts RENAME TO users;
```

For tables with special characters or spaces, use double quotes:

```sql
CREATE TABLE user_profiles ( -- @renamed from="user accounts"
  id bigint NOT NULL,
  name text
);
```

You can combine table renaming with column renaming and other schema changes:

```sql
CREATE TABLE accounts ( -- @renamed from=old_accounts
  id bigint NOT NULL PRIMARY KEY,
  username varchar(100) NOT NULL, -- @renamed from=user_name
  is_active boolean DEFAULT true
);
```

### Index Renaming

psqldef supports renaming indexes using the `-- @renamed from=old_name` or `/* @renamed from=old_name */` annotation:

```sql
CREATE INDEX new_email_idx /* @renamed from=old_email_idx */ ON users (email);
```

This generates:
```sql
ALTER INDEX old_email_idx RENAME TO new_email_idx;
```

You can rename multiple indexes:

```sql
CREATE INDEX email_idx ON users (email); -- @renamed from=idx_email
CREATE INDEX username_idx ON users (username); -- @renamed from=idx_username
```

The rename annotation also works for unique indexes:

```sql
CREATE UNIQUE INDEX unique_email /* @renamed from=old_unique_email */ ON users (email);
```

## Configuration

Configuration can be provided through YAML files (`--config`) or inline YAML strings (`--config-inline`). Multiple configurations can be specified and will be merged in order.

### Using Configuration Files

```shell
$ psqldef -U postgres dbname --apply --config config.yml < schema.sql
```

### Using Inline Configuration

```shell
$ psqldef -U postgres dbname --apply --config-inline 'enable_drop: true' < schema.sql
```

### Combining Multiple Configurations

```shell
$ psqldef -U postgres dbname --apply \
  --config base.yml \
  --config-inline 'managed_roles: [app_user, readonly]' \
  --config-inline 'enable_drop: true' \
  < schema.sql
```

### Available Configuration Options

| Field | Type | Description |
|-------|------|-------------|
| `enable_drop` | boolean | Enable destructive changes (DROP statements). Equivalent to `--enable-drop` flag. Default is false. |
| `target_tables` | string | Regular expression patterns (one per line) to specify which tables to manage. Only tables matching these patterns will be processed. |
| `skip_tables` | string | Regular expression patterns (one per line) to specify which tables to skip. Tables matching these patterns will be ignored. |
| `skip_views` | string | Regular expression patterns (one per line) to specify which views/materialized views to skip. |
| `target_schema` | string | Schema names (one per line) to specify which schemas to manage. Only objects in these schemas will be processed. |
| `managed_roles` | array | List of role names whose privileges (GRANT/REVOKE) should be managed. Only privileges for these roles will be applied. |
| `dump_concurrency` | integer | Number of parallel connections to use when exporting the schema. Improves performance for large schemas. Default is 1. |
| `create_index_concurrently` | boolean | When true, adds CONCURRENTLY to all CREATE INDEX statements. Default is false. |
| `legacy_ignore_quotes` | boolean | Controls identifier quoting behavior. When `true` (default), all identifiers are quoted in output. When `false`, identifiers preserve their original quoting from the source SQL. Default is `true` but will change to `false` in the next major version. See [Identifier Quoting](#identifier-quoting) for details. |

## Identifier Quoting

PostgreSQL distinguishes between quoted and unquoted identifiers:
- **Unquoted identifiers** are normalized to lowercase (e.g., `Users` becomes `users`)
- **Quoted identifiers** preserve their exact case (e.g., `"Users"` remains `Users`)

The `legacy_ignore_quotes` configuration option controls how psqldef handles this distinction.

### Quote-Aware Mode (`legacy_ignore_quotes: false`)

When set to `false`, psqldef preserves the quoting from your source SQL:

```sql
-- Source schema (desired.sql)
CREATE TABLE users (
    id bigint NOT NULL PRIMARY KEY,
    name text
);
```

```shell
$ psqldef -U postgres mydb --apply --config-inline="legacy_ignore_quotes: false" < desired.sql
-- Apply --
ALTER TABLE public.users ADD COLUMN name text;
```

In this mode:
- Unquoted identifiers remain unquoted in generated DDL
- Quoted identifiers remain quoted in generated DDL
- This matches PostgreSQL's actual case-sensitivity semantics

### Legacy Mode (`legacy_ignore_quotes: true`)

When set to `true` (current default), all identifiers are quoted in output:

```shell
$ psqldef -U postgres mydb --apply --config-inline="legacy_ignore_quotes: true" < desired.sql
-- Apply --
ALTER TABLE "public"."users" ADD COLUMN "name" text;
```

**Problem with legacy mode:** It loses the semantic distinction between quoted and unquoted identifiers. Consider this schema with mixed quoting:

```sql
-- desired.sql: intentionally mixed quoting
CREATE TABLE users (           -- unquoted: normalizes to lowercase
    id bigint PRIMARY KEY
);
CREATE TABLE "AuditLog" (      -- quoted: preserves exact case
    id bigint PRIMARY KEY
);
```

In legacy mode, the output quotes everything uniformly:

```shell
$ psqldef -U postgres mydb --export
CREATE TABLE "public"."users" (
    "id" bigint NOT NULL
);
CREATE TABLE "public"."AuditLog" (
    "id" bigint NOT NULL
);
```

This makes it impossible to tell which identifiers were originally quoted (case-sensitive) vs unquoted (case-insensitive). If you use this exported schema as input, you lose the original intent—`users` is now explicitly quoted when it didn't need to be.

### Migration Guide

The default will change from `true` to `false` in the next major version. To prepare:

1. **Test with `legacy_ignore_quotes: false`** to see if your workflow is affected
2. **Explicitly set the option** in your configuration to avoid surprises during upgrade:

```yaml
# config.yml - explicitly set to maintain current behavior
legacy_ignore_quotes: true
```

Or to adopt the new behavior early:

```yaml
# config.yml - opt-in to quote-aware mode
legacy_ignore_quotes: false
```

