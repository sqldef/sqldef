# Generic Parser Migration

## Goal

We are implementing PostgreSQL syntaxes in the generic parser. Once the migration is complete, we will discard the `pgquery` parser.

## Current Status

- **✅ Integration tests**: 1528 tests, 0 failures (`make test`) - **ALL PASS!**
- **✅ Generic parser tests**: 1006 tests, 0 failures (`PSQLDEF_PARSER=generic make test-psqldef`) - **ALL PASS!**

**Migration completed successfully!** All tests now pass with the generic parser.

## Rules

* Keep this document up to date. Don't keep the completed tasks
* Do not modify tests in this migration process. No breaking changes are allowed
* Do not change the behavior by checking `PSQLDEF_PARSER`, which will be removed away after the migration
* `pgquery` parser might normalize AST in a wrong way, which should be fixed in this migration process (commit cadcee36b9ed3fbb1a185262cc8881ca53d409d4 for example)
* `make test` must pass (i.e. no regressions are allowed)

## Completed Tasks

### Parser Comparison Unit Tests ✅

Fixed the `CreateViewWithCast` test failure by adding support for the `REAL` type in type cast expressions.

**Solution:**
- Added `REAL` to the `simple_convert_type` rule in `parser/parser.y`
- The generic parser was missing support for `::real` type casts used in view definitions
- After regenerating the parser, all 1006 generic parser tests now pass

**Technical Details:**
- Modified: `parser/parser.y` (added REAL to simple_convert_type rule around line 5321)
- The issue was that `REAL` was defined as a token and used in column types, but was missing from the type cast conversion rules
- Test case involved views with cast expressions: `b::bool`, `s::smallint`, `r::real`, etc.
