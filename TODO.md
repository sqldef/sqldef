# TODO for future improvements

## The `splitDDLs` function

The `splitDDLs()` function in `database/parser.go` has comments:

```go
// Right now, the parser isn't capable of splitting statements by itself.
// So we just attempt parsing until it succeeds. I'll let the parser do it in the future.
```

So once the parser is capable of splitting statements by itself, we can remove this function.

## Tool: fix-tests (Automated Test Expectation Updater)

**Location:** `cmd/fix-tests/main.go`

**Purpose:** Automatically fixes failing bidirectional migration tests by updating test expectations to match actual DDL output.

**Usage:**
```bash
# Build the tool
cd cmd/fix-tests && go build -o ../../build/fix-tests

# Run from project root
./build/fix-tests
```

**How it works:**
1. Runs `go test ./cmd/psqldef -json` to get structured test output
2. Parses test failures to extract expected vs actual DDL
3. For failures in "Phase 3: Reverse Migration" (down migrations):
   - Automatically updates the YAML test file's `down:` field
   - Replaces expected DDL with actual DDL
   - Preserves YAML formatting and structure
4. Categorizes failures (statement ordering, missing statements, etc.)
5. Reports which tests were fixed

**When to use:**
- After fixing DDL generation bugs - run this to update test expectations
- When statement ordering changes but semantics are correct
- To batch-update many tests after a systematic fix

**Limitations:**
- Only fixes "Phase 3" failures (reverse migration expectation mismatches)
- Doesn't fix Phase 1, 2, or 4 failures (those indicate real bugs)
- Doesn't fix DDL application errors (like "relation does not exist")
- Manual review recommended after running

## Remaining Test Failures (psqldef)

After migrating to bidirectional test format (up/down) and fixing major bugs, psqldef has **555/563 tests passing (98.6%)**.

**Remaining 8 failing tests:**

1. **AddIdentityWithSequenceToExistingColumn** - Cannot properly reverse IDENTITY column modifications
2. **ForeignKeyConstraintsAreEmittedLast** - Complex FK dependency ordering edge case
3. **TypedLiterals** - Statement ordering difference in reverse migration
4. **TypedLiteralsInCheckWithCast** - Statement ordering difference in reverse migration
5. **QuotedSchemaVsUnquotedSchemaModifyBoth** - Statement ordering difference
6. **ViewDDLsAreEmittedLastWithChangingDefinition** - View references non-existent table in reverse
7. **ViewDependsOnView_UpdateBaseViewAffectingDependentViews** - Complex view-on-view dependency changes
8. **ForeignKeyDependenciesMultipleToModifiedTable** - FK recreation order issue

These are complex edge cases that will require more sophisticated fixes.
