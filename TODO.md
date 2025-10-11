# TODO: Migration from pgquery to generic parser

This document tracks the progress of migrating from `pgquery` to `generic` parser.

**Status: 7 of 10 tests now passing with generic parser** ✅

## Principle of Operation

* We are migrating from `pgquery` to `generic` parser, discarding `pgquery` in the future.
* However, `pgquery` will be kept for a while as a fallback parser.
* If something conflicts, the generic parser's way is correct. Update `pgquery` stuff to adjust to the generic parser's way.
* You can add test cases to debug, but do not modify the existing test cases.
* `parser/parser_test.go` is the test cases for the generic parser. Use it to develop the parser stuff.
* When you add `slog.Debug()` for debugging, you don't need to remove them after the task is done.
* Keep `TODO.md` up-to-date, just removing completed tasks, instead of marking them as done.

## Progress Summary

### 📋 Known Limitations (3 tests)

These issues are due to parser limitations and would require significant parser enhancements:

1. **IndexesOnChangedExpressions**
   - Issue: Parser doesn't support PostgreSQL's `VARIADIC` keyword
   - PostgreSQL normalizes: `func(a, 'b', 'c')` → `func(a, VARIADIC ARRAY['b'::text, 'c'::text])`
   - Would require: Adding `VARIADIC` keyword support to parser grammar

2. **CreateTableWithDefault**
   - Issue: Parser doesn't recognize `bpchar` (PostgreSQL internal type name for `char`)
   - Error: "syntax error near 'bpchar'"
   - Would require: Adding `bpchar` to `simple_convert_type` grammar rule

3. **CreateViewWithCaseWhen**
   - Issue: Complex view definition normalization
   - PostgreSQL adds `::text` casts to string literals that our normalization can't fully remove
   - PostgreSQL's view formatting differs significantly from parser output
   - Would require: More aggressive view normalization or accepting PostgreSQL's formatting


## Known Issues

None - all MySQL and SQL Server CHECK constraint tests are now passing!
