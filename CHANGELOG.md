## v0.14.1

- sqlite3def: Add index support [#312](https://github.com/k0kubun/sqldef/issues/312)

## v0.14.0

- Drop support of Windows i386 [#310](https://github.com/k0kubun/sqldef/issues/310)
- Support virtual tables for sqlite3def [#310](https://github.com/k0kubun/sqldef/issues/310)

## v0.13.22

- Allow non-reserved keywords as column names for sqlite3def [#307](https://github.com/k0kubun/sqldef/issues/307)

## v0.13.21

- Support blob type for sqlite3def [#306](https://github.com/k0kubun/sqldef/issues/306)

## v0.13.20

- Add `--config` option to sqlite3def [#305](https://github.com/k0kubun/sqldef/issues/305)

## v0.13.19

- Add `skip_tables` option to `--config` for mysqldef and psqldef [#304](https://github.com/k0kubun/sqldef/issues/304)

## v0.13.18

- Update golang.org/x/text to v0.3.8 [#298](https://github.com/k0kubun/sqldef/issues/298)

## v0.13.17

- Add .exe extension to Windows executables [#294](https://github.com/k0kubun/sqldef/issues/294)

## v0.13.16

- Parse CREATE INDEX with cast expression for psqldef
  [#284](https://github.com/k0kubun/sqldef/issues/284)

## v0.13.15

- Parse CREATE VIEW with CASE WHEN and function calls for psqldef
  [#285](https://github.com/k0kubun/sqldef/issues/285)

## v0.13.14

- Filter primary keys, foreign keys, and indexes with `target_tables` of --config for psqldef
  [#290](https://github.com/k0kubun/sqldef/issues/290)

## v0.13.13

- Add --config option to psqldef as well [#289](https://github.com/k0kubun/sqldef/issues/289)

## v0.13.12

- Support extension for psqldef [#288](https://github.com/k0kubun/sqldef/issues/288)

## v0.13.11

- Add --ssl-ca option for mysqldef [#283](https://github.com/k0kubun/sqldef/issues/283)

## v0.13.10

- Stabilize create view comparison for psqldef [#278](https://github.com/k0kubun/sqldef/issues/278)

## v0.13.9

- Separate comment schema for each table for psqldef [#281](https://github.com/k0kubun/sqldef/issues/281)

## v0.13.8

- Add --ssl-mode option for mysqldef [#277](https://github.com/k0kubun/sqldef/issues/277)

## v0.13.7

- Stabilize default value comparison for mysqldef [#275](https://github.com/k0kubun/sqldef/issues/275)

## v0.13.6

- Support altering table comments for mysqldef [#271](https://github.com/k0kubun/sqldef/issues/271)

## v0.13.5

- Handle default values of "boolean" correctly [#274](https://github.com/k0kubun/sqldef/issues/274)

## v0.13.4

- Cross-compile psqldef releases for macOS using Xcode on the macOS runner of GitHub Actions

## v0.13.3

- Cross-compile psqldef releases for macOS using osxcross instead of Zig

## v0.13.2

- Initial support of comments for psqldef [#266](https://github.com/k0kubun/sqldef/issues/266)

## v0.13.1

- Switch the SQL parser of psqldef per statement
- Fix `psqldef --export` for policies

## v0.13.0

- Introduce a new SQL parser for psqldef [#241](https://github.com/k0kubun/sqldef/issues/241)
  - psqldef releases are now cross-compiled using Zig

## v0.12.8

- Support non-Linux operating systems in sqlite3def releases [#149](https://github.com/k0kubun/sqldef/issues/149)

## v0.12.7

- Initial support of materialized view indexes [#265](https://github.com/k0kubun/sqldef/issues/265)

## v0.12.6

- Parse INTERVAL and :: TIMESTAMP WITH TIME ZONE for psqldef [#263](https://github.com/k0kubun/sqldef/issues/263)

## v0.12.5

- Initial support of materialized views for psqldef [#262](https://github.com/k0kubun/sqldef/issues/262)

## v0.12.4

- Fix an error when a primary key with AUTO\_INCREMENT is modified [#258](https://github.com/k0kubun/sqldef/issues/258)
- Fix the output of composite foreign keys on `psqldef --export` [#260](https://github.com/k0kubun/sqldef/issues/260)

## v0.12.3

- Fix the type cast parser for psqldef [#257](https://github.com/k0kubun/sqldef/issues/257)

## v0.12.2

- Support changing precision and scale of numeric types [#256](https://github.com/k0kubun/sqldef/issues/256)

## v0.12.1

- Parse an expression in the first argument of `substr` for mysqldef [#254](https://github.com/k0kubun/sqldef/issues/254)

## v0.12.0

- Drop `--skip-file` option from mysqldef
- Add `--config` option to mysqldef to specify `target_tables` [#250](https://github.com/k0kubun/sqldef/issues/250)

## v0.11.62

- Support casting a default value to jsonb [#251](https://github.com/k0kubun/sqldef/issues/251)

## v0.11.61

- Fix the parser on reserved keywords for psqldef [#249](https://github.com/k0kubun/sqldef/issues/249)

## v0.11.60

- Support posix regexp on psqldef [#248](https://github.com/k0kubun/sqldef/issues/248)

## v0.11.59

- Add `--skip-file` option to `mysqldef` to skip tables specified with regexp
  [#242](https://github.com/k0kubun/sqldef/issues/242)

## v0.11.58

- Sort table names in `psqldef --export` [#240](https://github.com/k0kubun/sqldef/issues/240)

## v0.11.57

- Improve handling of SQL comments a little

## v0.11.56

- Parse `type` columns in VIEW definitions for psqldef [#235](https://github.com/k0kubun/sqldef/issues/235)

## v0.11.55

- Parse `CREATE INDEX` without an index name correctly for psqldef [#234](https://github.com/k0kubun/sqldef/issues/234)

## v0.11.54

- Support parsing function calls for psqldef [#233](https://github.com/k0kubun/sqldef/issues/233)

## v0.11.53

- Escape identifiers generated by `psqldef --export` [#232](https://github.com/k0kubun/sqldef/issues/232)

## v0.11.52

- Support `ALTER TABLE ADD VALUE` for psqldef [#228](https://github.com/k0kubun/sqldef/issues/228)

## v0.11.51

- Support parsing `CREATE INDEX CONCURRENTLY` for psqldef [#231](https://github.com/k0kubun/sqldef/issues/231)
- Run DDLs containing `CONCURRENTLY` outside a transaction

## v0.11.50

- Support parsing `::numeric` after an expression for psqldef [#227](https://github.com/k0kubun/sqldef/issues/227)

## v0.11.49

- Support parsing `DEFAULT NULL` with cast for psqldef [#226](https://github.com/k0kubun/sqldef/issues/226)

## v0.11.48

- Skip MySQL `/* */` comments [#222](https://github.com/k0kubun/sqldef/issues/222)

## v0.11.47

- Ignore `repack` schema in psqldef for `pg_repack` extension [#224](https://github.com/k0kubun/sqldef/issues/224)

## v0.11.46

- Support parsing UNIQUE INDEX in CREATE TABLE for mysqldef [#225](https://github.com/k0kubun/sqldef/issues/225)

## v0.11.45

- Improve cast handling of CHECK constraints in psqldef [#219](https://github.com/k0kubun/sqldef/issues/219)

## v0.11.44

- Add `--before-apply` to mysqldef [#217](https://github.com/k0kubun/sqldef/issues/217)

## v0.11.43

- Add `--skip-view` option to mysqldef as a temporary feature
  [#214](https://github.com/k0kubun/sqldef/issues/214)
  - This is expected to be removed once the view support is improved.

## v0.11.42

- Emulate mysql 5.7+'s TLS behavior by `tls=preferred` in mysqldef
  [#216](https://github.com/k0kubun/sqldef/issues/216)

## v0.11.41

- Emulate psql's `sslmode=prefer` in psqldef when `PGSSLMODE` isn't explicitly set

## v0.11.40

- Fix issues for nvarchar without size [#209](https://github.com/k0kubun/sqldef/issues/209)

## v0.11.39

- Parse `'string'::bpchar` for psqldef [#208](https://github.com/k0kubun/sqldef/pull/208)

## v0.11.38

- Consider ON RESTRICT and missing it as the same thing in mysqldef [#205](https://github.com/k0kubun/sqldef/pull/205)

## v0.11.37

- Parse string literal with character set for mysqldef [#204](https://github.com/k0kubun/sqldef/pull/204)
- Avoid unnecessary CHECK modification for mysqldef [#204](https://github.com/k0kubun/sqldef/pull/204)

## v0.11.36

- Support parsing IF THEN ... END IF for mysqldef [#203](https://github.com/k0kubun/sqldef/pull/203)

## v0.11.35

- Support creating indexes on expressions and using function as default [#199](https://github.com/k0kubun/sqldef/pull/199)

## v0.11.34

- Enable to add a unique constraint to tables in non-public schema [#197](https://github.com/k0kubun/sqldef/pull/197)

## v0.11.33

- Enable to drop and add CHECK constraints correctly for psqldef [#196](https://github.com/k0kubun/sqldef/pull/196)

## v0.11.32

- Add `--before-apply` option to psqldef to run commands before apply [#195](https://github.com/k0kubun/sqldef/pull/195)

## v0.11.31

- Fix issues in schema name handling on CONSTRAINT FOREIGN KEY REFERENCES for psqldef [#194](https://github.com/k0kubun/sqldef/pull/194)

## v0.11.30

- Handle the same table/column names in different schema names properly [#193](https://github.com/k0kubun/sqldef/pull/193)

## v0.11.29

- Handle constraints on the same table name but with different schema names for psqldef [#190](https://github.com/k0kubun/sqldef/pull/190)

## v0.11.28

- Support CHECK constraints on a table in a non-public schema [#188](https://github.com/k0kubun/sqldef/pull/188)

## v0.11.27

- Support parsing `GENERATED ALWAYS AS expr STORED` for psqldef [#184](https://github.com/k0kubun/sqldef/pull/184)
- Support parsing `text_pattern_ops` for psqldef [#184](https://github.com/k0kubun/sqldef/pull/184)

## v0.11.26

- Support parsing REFERENCES .. ON DELETE/UPDATE on a column for psqldef [#184](https://github.com/k0kubun/sqldef/pull/184)

## v0.11.25

- Fix schema handling of CREATE TABLE for psqldef [#187](https://github.com/k0kubun/sqldef/pull/187)

## v0.11.24

- Support `DEFERRABLE` options for psqldef [#186](https://github.com/k0kubun/sqldef/pull/186)

## v0.11.23

- Initial support of multi-column CHECK for psqldef [#183](https://github.com/k0kubun/sqldef/pull/183)

## v0.11.22

- Support dropping unique constraints for psqldef [#182](https://github.com/k0kubun/sqldef/pull/182)

## v0.11.21

- Allow an empty CREATE TABLE [#181](https://github.com/k0kubun/sqldef/pull/181)

## v0.11.20

- Support enum default values for psqldef [#180](https://github.com/k0kubun/sqldef/pull/180)

## v0.11.19

- Initial support of `ALTER TABLE ADD CONSTRAINT UNIQUE` for psqldef [#173](https://github.com/k0kubun/sqldef/pull/173)

## v0.11.18

- Support column types defined by `CREATE TYPE` for psqldef [#176](https://github.com/k0kubun/sqldef/pull/176)

## v0.11.17

- Support comparing two `--file` options [#179](https://github.com/k0kubun/sqldef/pull/179)

## v0.11.16

- Support altering a column with a boolean default value [#177](https://github.com/k0kubun/sqldef/pull/177)

## v0.11.15

- Fix a bug for retrieving views in mysqldef when there are multiple databases [#175](https://github.com/k0kubun/sqldef/pull/175)

## v0.11.14

- Initial support of `CREATE TYPE` for psqldef [#171](https://github.com/k0kubun/sqldef/pull/171)

## v0.11.13

- Initial support of `BEGIN END` in TRIGGER for mysqldef [#170](https://github.com/k0kubun/sqldef/pull/170)

## v0.11.12

- Support expressions for generated columns in mysqldef [#169](https://github.com/k0kubun/sqldef/pull/169)

## v0.11.11

- Avoid duplicating unique key definitions in `psqldef --export` [#167](https://github.com/k0kubun/sqldef/pull/167)

## v0.11.10

- Add `--enable-cleartext-plugin` option to in mysqldef [#166](https://github.com/k0kubun/sqldef/pull/166)

## v0.11.9

- Support triggers migrated from MySQL 5.6 to 5.7 in mysqldef [#157](https://github.com/k0kubun/sqldef/pull/157)
- Fix duplicated `;`s of triggers in `mysqldef --export`

## v0.11.8

- Support `NEW` keyword in an expression of triggers [#162](https://github.com/k0kubun/sqldef/pull/162)

## v0.11.7

- Support trigger assignment with `NEW` keyword in mysqldef [#158](https://github.com/k0kubun/sqldef/pull/158)

## v0.11.6

- Support a default value for JSON columns in psqldef [#161](https://github.com/k0kubun/sqldef/pull/161)

## v0.11.5

- Remove Windows and macOS binaries of sqlite3def releases that haven't been working
  [#149](https://github.com/k0kubun/sqldef/pull/149)
- Support updating comments of columns [#159](https://github.com/k0kubun/sqldef/pull/159)

## v0.11.4

- Support parsing table hint like `WITH(NOLOCK)` for mssqldef [#156](https://github.com/k0kubun/sqldef/pull/156)
- Fix parsers mysqldef and psqldef for TRIGGER time [#155](https://github.com/k0kubun/sqldef/pull/155)

## v0.11.3

- Support parsing `GENERATED ALWAYS AS` for mysqldef [#153](https://github.com/k0kubun/sqldef/pull/153)

## v0.11.2

- Fix mssqldef's parser for TRIGGER time [#152](https://github.com/k0kubun/sqldef/pull/152)

## v0.11.1

- Support `USING INDEX` for mysqldef properly [#150](https://github.com/k0kubun/sqldef/issues/150)
  - It has been crashing since v0.10.8

## v0.11.0

- Support `TRIGGER` for mssqldef and mysqldef [#135](https://github.com/k0kubun/sqldef/pull/135)
  - Support `DECLARE` [#137](https://github.com/k0kubun/sqldef/pull/137)
  - Support `CURSOR` [#138](https://github.com/k0kubun/sqldef/pull/138)
  - Support `WHILE` [#139](https://github.com/k0kubun/sqldef/pull/139)
  - Support `IF` [#141](https://github.com/k0kubun/sqldef/pull/141)
  - Support `SELECT` [#142](https://github.com/k0kubun/sqldef/pull/142)

## v0.10.15

- Support more `DEFAULT`-related features for mssqldef [#134](https://github.com/k0kubun/sqldef/issues/134)
  - Add and drop a default when the default constraint is changed
  - Support `GETDATE()`
  - Parse parenthesis in default constraints properly

## v0.10.14

- Support `NOT FOR REPLICATION` for mssqldef [#133](https://github.com/k0kubun/sqldef/issues/133)

## v0.10.13

- Support enum definition changes [#132](https://github.com/k0kubun/sqldef/issues/132)

## v0.10.12

- Support more index options for mssqldef [#131](https://github.com/k0kubun/sqldef/issues/131)

## v0.10.11

- Escape DSN for psqldef properly [#130](https://github.com/k0kubun/sqldef/issues/130)
- Support PGSSLPROTOCOL [#130](https://github.com/k0kubun/sqldef/issues/130)

## v0.10.10

- Support more value types for mssqldef [#129](https://github.com/k0kubun/sqldef/issues/129)

## v0.10.9

- Support CHECK for mssqldef [#128](https://github.com/k0kubun/sqldef/issues/128)

## v0.10.8

- Support indexes for mssqldef [#126](https://github.com/k0kubun/sqldef/issues/126)

## v0.10.7

- Support foreign keys for mssqldef [#127](https://github.com/k0kubun/sqldef/issues/127)

## v0.10.6

- Support index options for mssqldef [#125](https://github.com/k0kubun/sqldef/issues/125)

## v0.10.5

- Support PRIMARY KEY for mssqldef [#124](https://github.com/k0kubun/sqldef/issues/124)

## v0.10.4

- Support `DROP COLUMN` for mssqldef [#123](https://github.com/k0kubun/sqldef/issues/123)

## v0.10.3

- Support `ADD COLUMN` for mssqldef [#122](https://github.com/k0kubun/sqldef/issues/122)

## v0.10.2

- Add SQL Server support as `mssqldef` [#120](https://github.com/k0kubun/sqldef/issues/120)

## v0.10.1

- Support parsing and generating index lengths [#118](https://github.com/k0kubun/sqldef/issues/118)

## v0.10.0

- Accept `PGPASSWORD` instead of `PGPASS` in psqldef [#117](https://github.com/k0kubun/sqldef/issues/117)
- Support changing column defaults in psqldef [#116](https://github.com/k0kubun/sqldef/pull/116)
- Support more default values for psqldef: `CURRENT_DATE`, `CURRENT_TIME`, `text`, `bpchar` [#115](https://github.com/k0kubun/sqldef/pull/115)

## v0.9.2

- Support PostgreSQL Identity columns [#114](https://github.com/k0kubun/sqldef/issues/114)

## v0.9.1

- Support `"` to escape SQL identifiers in sqlite3def [#111](https://github.com/k0kubun/sqldef/issues/111)

## v0.9.0

- Drop darwin-i386 support to upgrade Go version

## v0.8.15

- Allow parsing `CURRENT_TIMESTAMP()` in addition to `CURRENT_TIMESTAMP` for MySQL [#59](https://github.com/k0kubun/sqldef/issues/59)

## v0.8.14

- Allow parsing index with non-escaped column name `key` for psqldef [#100](https://github.com/k0kubun/sqldef/issues/100)
- Prevent errors on `ADD CONSTRAINT FOREIGN KEY` for psqldef

## v0.8.13

- Support `SET NOT NULL` and `DROP NOT NULL` for psqldef `ALTER COLUMN`

## v0.8.12

- Support `CITEXT` data type for psqldef

## v0.8.11

- Fix CHECK handling of v0.8.9 to support PostgreSQL 12

## v0.8.10

- Support AUTOINCREMENT for sqlite3def [#99](https://github.com/k0kubun/sqldef/issues/99)

## v0.8.9

- Support CHECK option of CREATE TABLE for psqldef [#97](https://github.com/k0kubun/sqldef/issues/97)

## v0.8.8

- Generate composite primary keys properly in psqldef [#96](https://github.com/k0kubun/sqldef/issues/96)

## v0.8.7

- Make `CONSTRAINT foo PRIMARY KEY (bar)` work like `PRIMARY KEY (bar)` in psqldef [#88](https://github.com/k0kubun/sqldef/issues/88)

## v0.8.6

- All identifiers are escaped [#87](https://github.com/k0kubun/sqldef/issues/87)

## v0.8.5

- Improve comparison of decimal default values [#85](https://github.com/k0kubun/sqldef/issues/85)

## v0.8.4

- Support parsing columns names in a column's `REFERENCES` in psqldef [#84](https://github.com/k0kubun/sqldef/issues/84)

## v0.8.3

- Support parsing a column's `REFERENCES` in psqldef [#82](https://github.com/k0kubun/sqldef/issues/82)

## v0.8.2

- Support `CREATE POLICY` in psqldef [#77](https://github.com/k0kubun/sqldef/issues/77)

## v0.8.1

- Support more types of default values in psqldef [#80](https://github.com/k0kubun/sqldef/issues/80)

## v0.8.0

- Support `CREATE VIEW` and `DROP VIEW` [#78](https://github.com/k0kubun/sqldef/issues/78)

## v0.7.7

- Fix an error when adding `NOT NULL` [#71](https://github.com/k0kubun/sqldef/issues/71)
  - This fixed a bug introduced at v0.7.2

## v0.7.6

- Preserve AUTO\_INCREMENT when changing the column's data type in mysqldef [#70](https://github.com/k0kubun/sqldef/issues/70)
  - This fixed a bug introduced at v0.5.20.

## v0.7.5

- Fix ALTER with CHARACTER SET, COLLATE, and NOT NULL in mysqldef [#68](https://github.com/k0kubun/sqldef/issues/68)

## v0.7.4

- Support changing a DEFAULT value [#67](https://github.com/k0kubun/sqldef/issues/67)

## v0.7.3

- Allow a negative default value [#66](https://github.com/k0kubun/sqldef/issues/66)

## v0.7.2

- Generate `NULL` flag on a column definition of `ALTER TABLE` when it's explicitly specified [#63](https://github.com/k0kubun/sqldef/issues/63)

## v0.7.1

- Ignore `public.pg_buffercache` on psqldef when the extension is enabled [#65](https://github.com/k0kubun/sqldef/issues/65)

## v0.7.0

- Support sqlite3 by sqlite3def [#64](https://github.com/k0kubun/sqldef/issues/64)

## v0.6.4

- Support specifying non-public schema in psqldef [#62](https://github.com/k0kubun/sqldef/issues/62)

## v0.6.3

- Support changing column length [#61](https://github.com/k0kubun/sqldef/issues/61)

## v0.6.2

- Fully support having UNIQUE in a MySQL column [#60](https://github.com/k0kubun/sqldef/issues/60)

## v0.6.1

- Support BINARY attribute to specify collation in mysqldef [#47](https://github.com/k0kubun/sqldef/issues/47)

## v0.6.0

- Support changing types by `ALTER COLUMN` with psqldef

## v0.5.20

- Add AUTO\_INCREMENT after adding index or primary key
- Remove AUTO\_INCREMENT before removing index or primary key
- Allow a comment in the end of input schema

## v0.5.19

- Support altering a column for changing charset and collate [#60](https://github.com/k0kubun/sqldef/issues/60)

## v0.5.18

- Fix array type definition of `ADD COLUMN` for psqldef (a bugfix for v0.5.17)

## v0.5.17

- Support parsing a type with `ARRAY` or `[]` for psqldef [#58](https://github.com/k0kubun/sqldef/issues/58)

## v0.5.16

- Support CURRENT\_TIMESTAMP with precision [#59](https://github.com/k0kubun/sqldef/issues/59)

## v0.5.15

- Escape column names in index DDLs [#57](https://github.com/k0kubun/sqldef/issues/57)

## v0.5.14

- Support updating `ON UPDATE` / `ON DELETE` of foreign keys [#54](https://github.com/k0kubun/sqldef/issues/54)
- Fix a bug that foreign key is always exported as `ON UPDATE RESTRICT ON DELETE SET NULL` in psqldef

## v0.5.13

- Support JSONB type for psqldef [#55](https://github.com/k0kubun/sqldef/issues/55)

## v0.5.12

- DROP and ADD index if column combination is changed [#53](https://github.com/k0kubun/sqldef/issues/53)

## v0.5.11

- Escape index names generated in index DDLs [#51](https://github.com/k0kubun/sqldef/pull/51)

## v0.5.10

- Support adding/removing a default value to/from a column [#50](https://github.com/k0kubun/sqldef/pull/50)

## v0.5.9

- Avoid unnecessarily generating diff for `BOOLEAN` type on mysqldef [#49](https://github.com/k0kubun/sqldef/pull/49)

## v0.5.8

- Add `--skip-drop` option to skip `DROP` statements [#44](https://github.com/k0kubun/sqldef/pull/44)

## v0.5.7

- Support `double precision` for psqldef [#42](https://github.com/k0kubun/sqldef/pull/42)
- Support partial indexes syntax for psqldef [#41](https://github.com/k0kubun/sqldef/pull/41)

## v0.5.6

- Fix ordering between `NOT NULL` and `WITH TIME ZONE` for psqldef, related to v0.5.4 and v0.5.5
  [#40](https://github.com/k0kubun/sqldef/pull/40)

## v0.5.5

- Support `time` with and without timezone for psqldef [#39](https://github.com/k0kubun/sqldef/pull/39)

## v0.5.4

- Support `timestamp` with and without timezone for psqldef [#37](https://github.com/k0kubun/sqldef/pull/37)

## v0.5.3

- Fix output length bug of psqldef since v0.5.0 [#36](https://github.com/k0kubun/sqldef/pull/36)

## v0.5.2

- Support `timestamp` (without timezone) for psqldef [#34](https://github.com/k0kubun/sqldef/pull/34)

## v0.5.1

- Support `SMALLSERIAL`, `SERIAL`, `BIGSERIAL` for psqldef [#33](https://github.com/k0kubun/sqldef/pull/33)

## v0.5.0

- Remove `pg_dump` dependency for psqldef  [#32](https://github.com/k0kubun/sqldef/pull/32)

## v0.4.14

- Show `pg_dump` error output on failure [#30](https://github.com/k0kubun/sqldef/pull/30)

## v0.4.13

- Preserve line feeds when using stdin [#28](https://github.com/k0kubun/sqldef/pull/28)

## v0.4.12

- Support reordering columns with the same names [#27](https://github.com/k0kubun/sqldef/issues/27)

## v0.4.11

- Support enum [#25](https://github.com/k0kubun/sqldef/issues/25)

## v0.4.10

- Support `ON UPDATE CURRENT_TIMESTAMP` on MySQL

## v0.4.9

- Fix issues on handling primary key [#21](https://github.com/k0kubun/sqldef/issues/21)

## v0.4.8

- Add `--password-prompt` option to `mysqldef`/`psqldef`
  - This may be deprecated later once `--password` without value is properly implemented

## v0.4.7

- Add `-S`/`--socket` option of `mysqldef` to use unix domain socket
- Change `-h` option of `psqldef` to allow using unix domain socket

## v0.4.6

- Add support for fulltext index

## v0.4.5

- Support including hyphen in table names

## v0.4.4

- Support UUID data type for PostgreSQL and MySQL 8+

## v0.4.3

- Do not fail when view exists but just ignore views on mysqldef
  - Views may be supported later, but it's not managed by mysqldef for now

## v0.4.2

- Support generating `AFTER` or `FIRST` on `ADD COLUMN` on mysqldef

## v0.4.1

- Support `$PGSSLMODE` environment variable to specify `sslmode` on psqldef

## v0.4.0

- Support managing non-composite foreign key by changing CREATE TABLE
  - Note: Use `CONSTRAINT xxx FOREIGN KEY (yyy) REFERENCES zzz (vvv)` for both MySQL and PostgreSQL.
    In-column `REFERENCES` for PostgreSQL is not supported.
  - Note: Always specify constraint name, which is needed to identify foreign key name.
- Fix handling of DEFAULT NULL column

## v0.3.3

- Parse PostgreSQL's `"column"` literal properly
- Dump primary key with `--export` on PostgreSQL
- Prevent unexpected DDLs caused by data type aliases (bool, integer, char, varchar)

## v0.3.2

- Support `ADD PRIMARY KEY` / `DROP PRIMARY KEY` in MySQL
- Support parsing more data types for PostgreSQL: boolean, character
- Be aware of implicit `NOT NULL` on `PRIMARY KEY`
- Use `--schema-only` on `pg_dump` in psqldef

## v0.3.1

- Support `$MYSQL_PWD` environment variable to set password on mysqldef
- Support `$PGPASS` environment variable to set password on psqldef

## v0.3.0

- Support changing index on both MySQL and PostgreSQL
- Basic support of `CHANGE COLUMN` on MySQL
- All non-SQL outputs on apply/dry-run/export are formatted like `-- comment --`

## v0.2.0

- Support handling index on PostgreSQL
- Support `ADD INDEX` by modifying `CREATE TABLE` on MySQL

## v0.1.4

- Parse column definition more flexibly
  - ex) Both `NOT NULL AUTO_INCREMENT` and `AUTO_INCREMENT NOT NULL` are now valid
- Support parsing `character varying` for PostgreSQL
- Remove ` ` (space) before `;` on generated `ADD COLUMN`

## v0.1.3

- Fix SEGV and improve error message on parse error

## v0.1.2

- Drop all dynamic-link dependency from `mysqldef`
- "-- No table exists" is printed when no table exists on `--export`
- Improve error handling of unsupported features

## v0.1.1

- Release binaries for more architectures
  - New OS: Windows
  - New arch: 386, arm, arm64

## v0.1.0

- Initial release
  - OS: Linux, macOS
  - arch: amd64
- `mysqldef` for MySQL
  - Create table, drop table
  - Add column, drop column
  - Add index, drop index
- `psqldef` for PostgreSQL
  - Create table, drop table
  - Add column, drop column
