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
