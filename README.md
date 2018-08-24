# sqldef

Idempotent MySQL/PostgreSQL schema management like [Ridgepole](https://github.com/winebarrel/ridgepole), but in SQL.

TODO: demo gif

## How it works

TODO: diagram

## Project Status

Proof of Concept. Not ready for production, but it's already playable.

## Installation

Download the single-binary executable for your favorite database from:

https://github.com/k0kubun/sqldef/releases

## Usage

### mysqldef

```
$ mysqldef --help
Usage:
  mysqldef [options] db_name

Application Options:
  -u, --user=user_name       MySQL user name (default: root)
  -p, --password=password    MySQL user password
  -h, --host=host_name       Host to connect to the MySQL server (default: 127.0.0.1)
  -P, --port=port_num        Port used for the connection (default: 3306)
      --file=sql_file        Read schema SQL from the file, rather than stdin (default: -)
      --dry-run              Don't run DDLs but just show them
      --export               Just dump the current schema to stdout
      --help                 Show this help
```

### psqldef

```
$ psqldef --help
Usage:
  psqldef [option...] db_name

Application Options:
  -U, --user=username        PostgreSQL user name (default: postgres)
  -W, --password=password    PostgreSQL user password
  -h, --host=hostname        Host to connect to the PostgreSQL server (default: 127.0.0.1)
  -p, --port=port            Port used for the connection (default: 5432)
  -f, --file=filename        Read schema SQL from the file, rather than stdin (default: -)
      --dry-run              Don't run DDLs but just show them
      --export               Just dump the current schema to stdout
      --help                 Show this help
```

## TODO

- [ ] Replace SQL parser
  - xwb1989/sqlparser was [not for parsing DDL](https://github.com/xwb1989/sqlparser/issues/35).
- [ ] Some important features
  - Changing type of column (including adding unique index) and type of index
  - Foreign key support
- [ ] Better PostgreSQL support
  - Basically this tool is tested/developed against MySQL. So psqldef has more unfixed bugs than mysqldef.
  - Drop `pg_dump` command dependency to dump schema?
- [ ] Drop dynamic link to libc from mysqldef binary
  - The golang library lib/pq is the cause, so psqldef can't be fixed

## License

MIT License
