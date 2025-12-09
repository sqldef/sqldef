# User/Role Management Testing Problems

This document captures the problems encountered when running role management tests against a real PostgreSQL database.

## Core Problem

PostgreSQL roles are **cluster-wide** (not database-specific), and `CREATE ROLE` is **not transactional**. This creates fundamental challenges for testing role management features.

## Problem Categories

### 1. Role Already Exists Errors

**Symptom**: `pq: role "xxx" already exists`

**Affected Tests**:
- `ManagedRolesIgnoreUnmanagedRole` - role "readonly_user" already exists
- `ManagedRolesAllPrivileges` - role "admin_role" already exists
- `ManagedRolesMultipleRoles` - role "readonly_user" already exists
- `ManagedRolesMultipleTables` - role "app_user" already exists
- `ManagedRolesEmptyListIgnoresOrphans` - role "readonly_user" already exists
- `ManagedRolesErrorGrantOption` - role "readonly_user" already exists
- `ManagedRolesIdempotent` - role "readonly_user" already exists
- `ManagedRolesRevokeAll` - role "app_user" already exists
- `ManagedRolesPartialRevokeFromAll` - role "admin_role" already exists
- `ManagedRolesErrorCascade` - role "readonly_user" already exists
- `ManagedRolesAddPrivileges` - role "app_user" already exists
- `ManagedRolesSpecialCharacters` - role "user-with-dash" already exists
- `ManagedRolesOverlappingGrants` - role "user1" already exists
- `ManagedRolesMixedGrantRevoke` - role "app_user" already exists
- `ManagedRolesRevoke` - role "app_user" already exists
- `ManagedRolesBasicGrant` - role "readonly_user" already exists
- `ManagedRolesEmptyListNoChanges` - role "readonly_user" already exists
- `CreateRoleIdempotent` - role "existing_role" already exists
- `CreateRoleWithNologin` - role "test_role" already exists
- `GrantMultipleGranteesRevokeSkipped2` - role "grant_multi2_readonly" already exists
- `RevokePrivilegeSkipped` - role "revoke_priv_test_user" already exists
- `ManagedRolesDropRole` - role "orphan_role" already exists

**Root Causes**:
1. Test infrastructure pre-creates roles (`createAllTestRoles()`) for privilege testing
2. Tests run in parallel, attempting to create the same roles simultaneously
3. `CREATE ROLE` is non-transactional, so roles persist after ROLLBACK
4. Roles from previous test runs persist in the cluster

### 2. Pre-created Roles vs Schema Definitions Mismatch

**Symptom**: Tests expect `CREATE ROLE` but get `ALTER ROLE` because roles already exist

**Affected Tests**:
- `CreateRoleMultipleRoles` - Expected CREATE ROLE, got ALTER ROLE for role1, role2
- `CreateRoleWithSpecialCharacters` - Expected CREATE ROLE, got ALTER ROLE

**Root Cause**:
- Test infrastructure pre-creates roles (e.g., `role1`, `role2`, `user-with-dash`)
- When test runs, role already exists in cluster
- Database export shows role with current attributes
- Generator generates `ALTER ROLE` (modify existing) instead of `CREATE ROLE` (new)

### 3. Database Export vs Schema String Mismatch

**Symptom**: Idempotency check fails with unexpected DROP ROLE statements

**Example from CreateRoleMultipleRoles**:
```
Current schema is not idempotent. Expected no changes when reapplying current schema, but got:
DROP ROLE "role1";
DROP ROLE "role2";
```

**Root Cause**:
- Test's `current` schema string has no CREATE ROLE
- Database export includes roles that exist in cluster (pre-created by test infrastructure)
- Generator sees: current (from DB) has roles, desired (schema string) doesn't → generates DROP

### 4. VALID UNTIL Timestamp Formatting

**Symptom**: Idempotency check fails for roles with VALID UNTIL

**Affected Tests**:
- `CreateRoleWithValidUntil`

**Root Cause**:
- PostgreSQL stores VALID UNTIL as full timestamp with timezone
- Schema specifies `VALID UNTIL '2025-12-31'`
- Database exports `VALID UNTIL '2025-12-31 00:00:00+00'`
- Generator sees difference → generates unnecessary ALTER ROLE

### 5. Privilege Consolidation Differences

**Symptom**: Different REVOKE statements generated depending on how privileges are read

**Affected Tests**:
- `GrantPartialGranteesRevokeSkipped` - Expected separate REVOKEs, got consolidated
- `GrantOverlappingRevokeSkipped` - Privilege order differs in REVOKE statements
- `RevokeMultiplePrivilegesSkipped` - Expected GRANT in down, got empty

**Root Cause**:
- Database exports actual privilege state from `information_schema.role_table_grants`
- Multiple GRANT statements to the same role get consolidated into one privilege set
- Privilege ordering differs between parsed schema and database export
- Reverse migration expectations don't match actual database state

### 6. Non-transactional CREATE ROLE with Parallel Tests

**Symptom**: Test creates role, test fails, role persists, next test fails

**Root Cause**:
- `CREATE ROLE` executes outside transaction (PostgreSQL limitation)
- Even with `BEGIN`/`ROLLBACK`, role creation is committed immediately
- Parallel tests compete for the same role names
- Failed tests leave roles behind, affecting subsequent tests

## Fundamental Architecture Issues

### Issue A: Cluster-wide vs Database-specific

PostgreSQL roles exist at the **cluster level**, not the database level:
- Each test creates its own database, but roles are shared
- Tests cannot be truly isolated when they involve role operations
- Role cleanup between tests affects all parallel tests

### Issue B: Non-transactional Role Operations

CREATE ROLE, ALTER ROLE, DROP ROLE are all non-transactional:
- Cannot rollback role changes on test failure
- Cannot use transaction isolation for test independence
- Roles created during test setup persist even if test fails

### Issue C: Test Infrastructure Pre-creates Roles

`createAllTestRoles()` pre-creates common roles for privilege testing:
- Creates roles like `readonly_user`, `app_user`, `admin_role`, etc.
- These roles then appear in database exports for ALL tests
- Tests expecting empty `current` state see pre-created roles
- Tests expecting to CREATE roles find them already existing

## Potential Solutions

### Solution 1: Unique Role Names Per Test

Generate unique role names for each test run (e.g., `test_12345_readonly_user`):
- Pros: Avoids conflicts between parallel tests
- Cons: Requires role cleanup, complicates test expectations

### Solution 2: Sequential Test Execution for Role Tests

Run role-related tests sequentially (not in parallel):
- Pros: Eliminates race conditions
- Cons: Slower test execution, still has cleanup issues

### Solution 3: Separate Test Suite for Role Management

Create a dedicated test suite that:
- Runs in isolation (no parallel tests)
- Uses a fresh PostgreSQL cluster or careful cleanup
- Handles the non-transactional nature explicitly

### Solution 4: Mock the Role Export

For tests that only need to verify DDL generation:
- Mock the database export to return controlled data
- Pros: Fast, isolated, predictable
- Cons: Doesn't test actual database interaction

### Solution 5: Use Docker Containers Per Test

Spin up a fresh PostgreSQL container per test:
- Pros: True isolation
- Cons: Very slow, resource intensive

## Recommendation

Use unique role names per test:

1. Generate test-specific role names using test name or UUID prefix
2. Clean up roles in test teardown
3. Run role tests sequentially to avoid race conditions
4. Normalize VALID UNTIL timestamps when comparing schemas

## Files Affected

- `cmd/psqldef/tests_create_role.yml` - CREATE/ALTER/DROP ROLE tests
- `cmd/psqldef/tests_managed_roles.yml` - Managed roles with privileges tests
- `cmd/psqldef/tests_enable_drop.yml` - REVOKE skipping tests with roles
- `cmd/psqldef/psqldef_test.go` - Test infrastructure (createAllTestRoles, cleanupTestRoles)
