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

## Parser Refactoring Plan

Based on deep analysis of `goyacc -v output.txt`, here's the concrete plan to reduce conflicts from 2,859 to a manageable level.

### Root Causes Analysis

**Top Conflict Sources (from state machine analysis):**

1. **State 1619** (298 conflicts): Ambiguity between `sql_id` as `column_type`, `reserved_sql_id`, or part of `module_arguments`
   - Parser cannot decide if identifier is a type name, keyword, or function argument
   - Affects nearly every token (UNION, SELECT, FROM, WHERE, etc.)

2. **Optional Rule Proliferation**: 50+ `*_opt` rules with empty productions cause cascading ambiguities
   - Each empty production creates shift/reduce or reduce/reduce conflicts
   - Most problematic: `length_opt` (rule 375), `deferrable_opt` (rule 937), `if_not_exists_opt` (rule 921)

3. **Type Name Ambiguity**: SQL keywords double as type names and identifiers
   - `int_type` vs `sql_id` creates 191 conflicts (rules 818 vs 375)
   - `column_type` definitions conflict with column options (191 conflicts, rules 710 vs 829)

4. **Reduce/Reduce Hotspots**:
   - Rules 818 ↔ 375: 191 conflicts (`int_type` vs `length_opt` empty)
   - Rules 710 ↔ 829: 191 conflicts (type chain ambiguity)
   - Rules 261 ↔ 937: 157 conflicts (`REFERENCES` vs `deferrable_opt` empty)
   - Rules 337 ↔ 1058: 156 conflicts
   - Multiple column definition rules ↔ `deferrable_opt`: 156 conflicts each

### Refactoring Strategy

#### Phase 1: Low-Hanging Fruit (Target: -300 conflicts)

**1.1 Merge Related Optional Rules**
- Combine `deferrable_opt` + `initially_deferred_opt` into single `constraint_timing_opt`
- Eliminate empty productions by using explicit lists with | /* empty */ only when necessary
- Impact: ~200 reduce/reduce conflicts

**1.2 Disambiguate Type Names**
- Create separate lexer states or precedence for type contexts
- Use `%prec` directives for ambiguous type/identifier cases
- Add explicit `typename_or_id` non-terminal for dual-use tokens
- Impact: ~100 shift/reduce conflicts

**1.3 Factor Column Definition Rules**
- Consolidate the 8+ `column_definition` alternatives that differ only in optional clauses
- Use single rule with optional components: `column_definition_type option_list`
- Impact: ~156 reduce/reduce conflicts with `deferrable_opt`

#### Phase 2: Structural Refactoring (Target: -800 conflicts)

**2.1 Rewrite sql_id/reserved_sql_id Hierarchy**
- Current: Massive ambiguity in state 1619 (298 conflicts)
- Solution: Split contexts explicitly:
  - `typename`: For column types
  - `identifier`: For names
  - `keyword_as_id`: For keywords-as-identifiers
- Use GLR parsing hints or context-dependent lexing
- Impact: ~300 conflicts

**2.2 Separate DDL vs DML Grammar Paths**
- Create distinct entry points for CREATE/ALTER (DDL) vs SELECT/INSERT (DML)
- Reduces cross-contamination of production rules
- Impact: ~200 conflicts

**2.3 Refactor Expression Grammar**
- `value_expression` has 40+ alternatives, many ambiguous
- Group by operator precedence explicitly
- Use %left/%right precedence declarations consistently
- Impact: ~300 conflicts

#### Phase 3: Optional Rules Cleanup (Target: -500 conflicts)

**3.1 Replace Empty Productions with Lists**
- Pattern: `foo_opt: /* empty */ | FOO` → `foo_list: /* empty */ | foo_list FOO`
- Apply to: `index_option_opt`, `where_expression_opt`, `order_by_opt`, etc.
- Impact: ~200 conflicts

**3.2 Make Optionals More Specific**
- Instead of generic `length_opt` everywhere, use context-specific versions:
  - `int_length_opt`, `string_length_opt`, `decimal_precision_opt`
- Reduces cross-contamination between rules
- Impact: ~150 conflicts

**3.3 Inline Trivial Optionals**
- Rules used only once → inline them directly
- Example: `semicolon_opt` → just `';'` | /* empty */ inline
- Impact: ~150 conflicts

#### Phase 4: Advanced Techniques (Target: -500 conflicts)

**4.1 Use Parser Hints**
- Add `%expect` directives to document intentional conflicts
- Use `%prec` to explicitly resolve known ambiguities
- Example: INTERVAL as keyword vs function name
- Impact: Document remaining ~800 acceptable conflicts

**4.2 Consider Split Parsers**
- Separate grammars for PostgreSQL vs MySQL vs SQLite vs MSSQL
- Each dialect shares common base but has own extensions
- Trade-off: Code duplication vs conflict reduction
- Impact: Could eliminate ~500 dialect-mixing conflicts

**4.3 Lexer-Level Disambiguation**
- Move keyword/identifier decision to lexer based on context
- PostgreSQL does this - identifiers in quotes vs unquoted
- Requires stateful lexer (current is stateless)
- Impact: ~200 conflicts

### Implementation Roadmap

**Milestone 1**: Phase 1 complete (2-3 days)
- Target: 2,859 → 2,559 conflicts (-300)
- All tests pass
- No behavioral changes to user-facing features

**Milestone 2**: Phases 1-2 complete (1-2 weeks)
- Target: 2,859 → 1,759 conflicts (-1,100)
- May require test updates for new grammar structure
- Backward compatible DDL output

**Milestone 3**: Phases 1-3 complete (2-3 weeks)
- Target: 2,859 → 1,259 conflicts (-1,600)
- Comprehensive test coverage
- Document remaining conflicts

**Milestone 4**: Phase 4 exploration (3-4 weeks)
- Target: 2,859 → 759 conflicts (-2,100)
- Evaluate trade-offs of advanced techniques
- Decision point: Accept remaining conflicts or pursue split parsers

### Risk Assessment

**High Risk**:
- Phase 2.1 (sql_id hierarchy rewrite) - touches 1000+ grammar rules
- Phase 4.2 (split parsers) - major architectural change

**Medium Risk**:
- Phase 2.3 (expression refactoring) - complex precedence interactions
- Phase 3 (optional rules) - widespread changes, easy to break tests

**Low Risk**:
- Phase 1.1 (merge optional rules) - localized changes
- Phase 1.2 (type name precedence) - uses existing yacc features
- Phase 4.1 (parser hints) - documentation only, no behavior change

### Success Criteria

**Minimum Viable**: Reduce to <1,500 conflicts (Phase 1-2)
- Eliminates most reduce/reduce conflicts
- Makes grammar maintainable
- Future additions less risky

**Stretch Goal**: Reduce to <800 conflicts (Phase 1-3)
- Industry standard for complex parsers (similar to C++)
- Clear remaining conflicts documented
- High confidence in parser behavior

**Aspirational**: Reduce to <200 conflicts (Phase 1-4)
- Requires architectural changes (split parsers or GLR)
- May not be worth the complexity cost
- Decision after Milestone 3

### Next Steps

1. ✅ **DONE**: Generate and analyze `goyacc -v output.txt`
2. **TODO**: Create branch `refactor/reduce-conflicts`
3. **TODO**: Implement Phase 1.1 (merge optional rules)
4. **TODO**: Run full test suite, benchmark performance
5. **TODO**: Document conflicts before/after each phase

## Tasks

(nothing for now)
