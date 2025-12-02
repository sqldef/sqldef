# Identifier Case Sensitivity

| Aspect | PostgreSQL | MySQL | SQL Server | SQLite3 |
|--------|------------|-------|------------|---------|
| **Unquoted identifiers** | Folded to lowercase | Preserved as written | Preserved as written | Preserved as written |
| **Quoted identifiers** | Case-sensitive | Case-preserved | Depends on collation | Case-preserved but insensitive |
| **Quote character** | `"double quotes"` | ``` `backticks` ``` | `[brackets]` or `"double quotes"` | `"`, `` ` ``, or `[]` |
| **Table name comparison** | Case-sensitive | OS/setting dependent | Depends on collation | Case-insensitive |
| **Column name comparison** | Case-sensitive | Case-insensitive | Depends on collation | Case-insensitive |

## Notes

- **PostgreSQL**: Unquoted `FOO`, `Foo`, `foo` all become `"foo"`. Use quotes for mixed-case names.
- **MySQL**: Table name behavior depends on OS and `lower_case_table_names` setting (0=sensitive, 1=insensitive, 2=stored as-is but compared insensitively).
- **SQL Server**: Collation controls case sensitivity. Default `SQL_Latin1_General_CP1_CI_AS` is case-insensitive (`CI`).
- **SQLite3**: Always case-insensitive for ASCII, even with quoted identifiers.
