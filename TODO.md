# Migration from pgquery to generic parser

This document tracks the progress of migrating from `pgquery` to `generic` parser.

## Principle of Operation

* We are migrating from `pgquery` to `generic` parser, discarding `pgquery` in the future.
* However, `pgquery` will be kept for a while as a fallback parser.
* If something conflicts, the generic parser's way is correct. Update `pgquery` stuff to adjust to the generic parser's way.
* You can add test cases to debug, but never modify the existing test cases.
* `parser/parser_test.go` is the test cases for the generic parser. Use it to develop the parser stuff.
* When you add `slog.Debug()` for debugging, you don't need to remove them after the task is done.
* Use AST as much as possible, instead of string manipulation.
* Add `TODO` comments when the solution may not be optimal.
* Keep `TODO.md` up-to-date, just removing completed tasks, instead of marking them as done.

## Test

`PSQLDEF_PARSER=generic` to use the generic parser. `PSQLDEF_PARSER=pgquery` to use the pgquery parser. Otherwise, `psqldef` uses `generic` as the primary parser, and the `pgquery` as the fallback.

Eventually, both `PSQLDEF_PARSER=generic make test-psqldef` and `PSQLDEF_PARSER=pgquery make test-psqldef` must pass, as well as `make test-mysqldef`, `make test-sqlite3def`, `make test-mssqldef`.

## Known Parser Warnings

When running `make regen-parser`, you'll see these warnings from goyacc:

### Grammar Conflicts (945 shift/reduce, 1914 reduce/reduce)

The parser has ~2,859 grammar conflicts, which is extremely high compared to typical parsers (0-5 for well-designed grammars, 50-200 for complex languages like C++).

**Why so many?**
- Supporting multiple SQL dialects (MySQL, PostgreSQL, SQLite, SQL Server)
- Allowing SQL keywords as identifiers in many contexts
- SQL syntax is inherently ambiguous

**Are they harmful?**
- **No** for tested cases - all tests pass, so goyacc's default conflict resolution (prefer shift, use first rule) works correctly for common patterns
- **Potentially yes** for edge cases - untested SQL patterns may parse incorrectly
- **Yes** for maintenance - adding new grammar rules is risky and hard to debug with so many conflicts

**Current status:** Accepted technical debt. The grammar has explicit TODOs acknowledging conflicts as "non-trivial" to fix (see parser.y:2049, 2169, 4446, 4454).

**Recommendation:** Accept as-is. Fixing would require complete grammar rewrite, which is impractical. The migration to generic parser accepts these tradeoffs in exchange for multi-dialect support. Document known limitations when discovered.

## Tasks

(nothing for now)
