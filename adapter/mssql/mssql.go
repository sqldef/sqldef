package mssql

import (
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/k0kubun/sqldef/adapter"
)

const indent = "    "

type MssqlDatabase struct {
	config adapter.Config
	db     *sql.DB
}

func NewDatabase(config adapter.Config) (adapter.Database, error) {
	db, err := sql.Open("sqlserver", mssqlBuildDSN(config))
	if err != nil {
		return nil, err
	}

	return &MssqlDatabase{
		db:     db,
		config: config,
	}, nil
}

func (d *MssqlDatabase) TableNames() ([]string, error) {
	rows, err := d.db.Query(
		`select schema_name(schema_id) as table_schema, name from sys.objects where type = 'U';`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := []string{}
	for rows.Next() {
		var schema, name string
		if err := rows.Scan(&schema, &name); err != nil {
			return nil, err
		}
		tables = append(tables, schema+"."+name)
	}
	return tables, nil
}

func (d *MssqlDatabase) DumpTableDDL(table string) (string, error) {
	cols, err := d.getColumns(table)
	if err != nil {
		return "", err
	}
	indexDefs, err := d.getIndexDefs(table)
	if err != nil {
		return "", err
	}
	foreignDefs, err := d.getForeignDefs(table)
	if err != nil {
		return "", err
	}
	return buildDumpTableDDL(table, cols, indexDefs, foreignDefs), nil
}

func buildDumpTableDDL(table string, columns []column, indexDefs []*indexDef, foreignDefs []string) string {
	var queryBuilder strings.Builder
	fmt.Fprintf(&queryBuilder, "CREATE TABLE %s (", table)
	for i, col := range columns {
		if i > 0 {
			fmt.Fprint(&queryBuilder, ",")
		}
		fmt.Fprint(&queryBuilder, "\n"+indent)
		fmt.Fprintf(&queryBuilder, "%s %s", col.Name, col.dataType)
		if col.dataType == "char" || col.dataType == "varchar" || col.dataType == "binary" || col.dataType == "varbinary" {
			fmt.Fprintf(&queryBuilder, "(%s)", col.Length)
		}
		if !col.Nullable {
			fmt.Fprint(&queryBuilder, " NOT NULL")
		}
		if col.DefaultName != "" {
			fmt.Fprintf(&queryBuilder, " CONSTRAINT %s DEFAULT %s", col.DefaultName, col.DefaultVal)
		}
		if col.IsIdentity {
			fmt.Fprintf(&queryBuilder, " IDENTITY(%s,%s)", col.SeedValue, col.IncrementValue)
		}
		if col.CheckName != "" {
			fmt.Fprintf(&queryBuilder, " CONSTRAINT [%s] CHECK %s", col.CheckName, col.CheckDefinition)
		}
	}

	for _, indexDef := range indexDefs {
		fmt.Fprint(&queryBuilder, ",\n"+indent)
		if indexDef.primary {
			fmt.Fprintf(&queryBuilder, "CONSTRAINT [%s] PRIMARY KEY", indexDef.name)
		} else {
			fmt.Fprintf(&queryBuilder, "INDEX [%s]", indexDef.name)
			if indexDef.unique {
				fmt.Fprint(&queryBuilder, " UNIQUE")
			}
		}
		if indexDef.indexType == "CLUSTERED" || indexDef.indexType == "NONCLUSTERED" {
			fmt.Fprintf(&queryBuilder, " %s", indexDef.indexType)
		}
		fmt.Fprintf(&queryBuilder, " (%s)", strings.Join(indexDef.columns, ", "))
		if len(indexDef.options) > 0 {
			fmt.Fprint(&queryBuilder, " WITH (")
			for i, option := range indexDef.options {
				if i > 0 {
					fmt.Fprint(&queryBuilder, ",")
				}
				fmt.Fprintf(&queryBuilder, " %s", fmt.Sprintf("%s = %s", option.name, option.value))
			}
			fmt.Fprint(&queryBuilder, " )")
		}
	}

	for _, v := range foreignDefs {
		fmt.Fprint(&queryBuilder, ",\n"+indent)
		fmt.Fprint(&queryBuilder, v)
	}

	fmt.Fprintf(&queryBuilder, "\n);\n")
	return strings.TrimSuffix(queryBuilder.String(), ";\n")
}

type column struct {
	Name            string
	dataType        string
	Length          string
	Nullable        bool
	IsIdentity      bool
	SeedValue       string
	IncrementValue  string
	DefaultName     string
	DefaultVal      string
	CheckName       string
	CheckDefinition string
}

func (d *MssqlDatabase) getColumns(table string) ([]column, error) {
	schema, table := splitTableName(table)
	query := fmt.Sprintf(`SELECT
	c.name,
	[type_name] = tp.name,
	c.max_length,
	c.is_nullable,
	c.is_identity,
	seed_value = CASE WHEN c.is_identity = 1 THEN IDENTITYPROPERTY(c.[object_id], 'SeedValue') END,
	increment_value = CASE WHEN c.is_identity = 1 THEN IDENTITYPROPERTY(c.[object_id], 'IncrementValue') END,
	c.default_object_id,
	default_name = OBJECT_NAME(c.default_object_id),
	default_definition = OBJECT_DEFINITION(c.default_object_id),
	cc.name,
	cc.definition
FROM sys.columns c WITH(NOLOCK)
	JOIN sys.types tp WITH(NOLOCK) ON c.user_type_id = tp.user_type_id
	LEFT JOIN sys.check_constraints cc WITH(NOLOCK) ON c.[object_id] = cc.parent_object_id
		AND cc.parent_column_id = c.column_id
WHERE c.[object_id] = OBJECT_ID('%s.%s', 'U')`, schema, table)

	rows, err := d.db.Query(query)
	if err != nil {
		fmt.Println(err)
	}
	defer rows.Close()

	cols := []column{}
	for rows.Next() {
		col := column{}
		var colName, dataType, maxLen, defaultId string
		var seedValue, incrementValue, defaultName, defaultVal, checkName, checkDefinition *string
		var isNullable, isIdentity bool
		err = rows.Scan(&colName, &dataType, &maxLen, &isNullable, &isIdentity, &seedValue, &incrementValue, &defaultId, &defaultName, &defaultVal, &checkName, &checkDefinition)
		if err != nil {
			return nil, err
		}
		col.Name = colName
		col.Length = maxLen
		if defaultId != "0" {
			col.DefaultName = *defaultName
			col.DefaultVal = removeBrace(*defaultVal)
		}
		col.Nullable = isNullable
		col.dataType = dataType
		col.IsIdentity = isIdentity
		if isIdentity {
			col.SeedValue = *seedValue
			col.IncrementValue = *incrementValue
		}
		if checkName != nil {
			col.CheckName = *checkName
			col.CheckDefinition = *checkDefinition
		}
		cols = append(cols, col)
	}
	return cols, nil
}

type indexDef struct {
	name      string
	columns   []string
	primary   bool
	unique    bool
	indexType string
	options   []indexOption
}

type indexOption struct {
	name  string
	value string
}

func (d *MssqlDatabase) getIndexDefs(table string) ([]*indexDef, error) {
	schema, table := splitTableName(table)
	query := fmt.Sprintf(`SELECT
	ind.name AS index_name,
	COL_NAME(ic.object_id, ic.column_id) AS column_name,
	ind.is_primary_key,
	ind.is_unique,
	ind.type_desc,
	ic.is_descending_key,
	ind.is_padded,
	ind.fill_factor,
	ind.ignore_dup_key,
	st.no_recompute,
	st.is_incremental,
	ind.allow_row_locks,
	ind.allow_page_locks
FROM sys.indexes ind
INNER JOIN sys.index_columns ic ON ind.object_id = ic.object_id AND ind.index_id = ic.index_id
INNER JOIN sys.stats st ON ind.object_id = st.object_id AND ind.index_id = st.stats_id
WHERE ind.object_id = OBJECT_ID('[%s].[%s]')`, schema, table)

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	indexDefMap := make(map[string]*indexDef)
	var indexName, columnName, typeDesc, fillfactor string
	var isPrimary, isUnique, isDescending, padIndex, ignoreDupKey, noRecompute, incremental, rowLocks, pageLocks bool
	for rows.Next() {
		err = rows.Scan(&indexName, &columnName, &isPrimary, &isUnique, &typeDesc, &isDescending, &padIndex, &fillfactor, &ignoreDupKey, &noRecompute, &incremental, &rowLocks, &pageLocks)
		if err != nil {
			return nil, err
		}

		columnDefinition := fmt.Sprintf("[%s]", columnName)
		if isDescending {
			columnDefinition += " DESC"
		}

		if _, ok := indexDefMap[indexName]; !ok {
			options := []indexOption{
				{name: "PAD_INDEX", value: boolToOnOff(padIndex)},
				{name: "FILLFACTOR", value: fillfactor},
				{name: "IGNORE_DUP_KEY", value: boolToOnOff(ignoreDupKey)},
				{name: "STATISTICS_NORECOMPUTE", value: boolToOnOff(noRecompute)},
				{name: "STATISTICS_INCREMENTAL", value: boolToOnOff(incremental)},
				{name: "ALLOW_ROW_LOCKS", value: boolToOnOff(rowLocks)},
				{name: "ALLOW_PAGE_LOCKS", value: boolToOnOff(pageLocks)},
			}

			definition := &indexDef{name: indexName, columns: []string{columnDefinition}, primary: isPrimary, unique: isUnique, indexType: typeDesc, options: options}
			indexDefMap[indexName] = definition
		} else {
			indexDefMap[indexName].columns = append(indexDefMap[indexName].columns, columnDefinition)
		}
	}

	indexDefs := make([]*indexDef, 0)
	for _, definition := range indexDefMap {
		indexDefs = append(indexDefs, definition)
	}
	return indexDefs, nil
}

func (d *MssqlDatabase) getForeignDefs(table string) ([]string, error) {
	schema, table := splitTableName(table)
	query := fmt.Sprintf(`SELECT
	f.name,
	COL_NAME(f.parent_object_id, fc.parent_column_id),
	OBJECT_NAME(f.referenced_object_id)_id,
	COL_NAME(f.referenced_object_id, fc.referenced_column_id),
	f.update_referential_action_desc,
	f.delete_referential_action_desc
FROM sys.foreign_keys f INNER JOIN sys.foreign_key_columns fc ON f.OBJECT_ID = fc.constraint_object_id
WHERE f.parent_object_id = OBJECT_ID('[%s].[%s]')`, schema, table)

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	defs := make([]string, 0)
	for rows.Next() {
		var constraintName, columnName, foreignTableName, foreignColumnName, foreignUpdateRule, foreignDeleteRule string
		err = rows.Scan(&constraintName, &columnName, &foreignTableName, &foreignColumnName, &foreignUpdateRule, &foreignDeleteRule)
		if err != nil {
			return nil, err
		}
		foreignUpdateRule = strings.Replace(foreignUpdateRule, "_", " ", -1)
		foreignDeleteRule = strings.Replace(foreignDeleteRule, "_", " ", -1)
		def := fmt.Sprintf("CONSTRAINT [%s] FOREIGN KEY ([%s]) REFERENCES [%s] ([%s]) ON UPDATE %s ON DELETE %s", constraintName, columnName, foreignTableName, foreignColumnName, foreignUpdateRule, foreignDeleteRule)
		defs = append(defs, def)
	}

	return defs, nil
}

func boolToOnOff(in bool) string {
	if in {
		return "ON"
	} else {
		return "OFF"
	}
}

var (
	suffixSemicolon = regexp.MustCompile(`;$`)
	spaces          = regexp.MustCompile(`[ ]+`)
)

func (d *MssqlDatabase) Views() ([]string, error) {
	const sql = `SELECT
	sys.views.name as name,
	sys.sql_modules.definition as definition
FROM sys.views
INNER JOIN sys.objects ON
	sys.objects.object_id = sys.views.object_id
INNER JOIN sys.schemas ON
	sys.schemas.schema_id = sys.objects.schema_id
INNER JOIN sys.sql_modules
	ON sys.sql_modules.object_id = sys.objects.object_id
`

	rows, err := d.db.Query(sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ddls []string
	for rows.Next() {
		var name, definition string
		if err := rows.Scan(&name, &definition); err != nil {
			return nil, err
		}
		definition = strings.TrimSpace(definition)
		definition = strings.ReplaceAll(definition, "\n", "")
		definition = suffixSemicolon.ReplaceAllString(definition, "")
		definition = spaces.ReplaceAllString(definition, " ")
		ddls = append(ddls, definition)
	}
	return ddls, nil
}

func (d *MssqlDatabase) DB() *sql.DB {
	return d.db
}

func (d *MssqlDatabase) Close() error {
	return d.db.Close()
}

func mssqlBuildDSN(config adapter.Config) string {
	query := url.Values{}
	query.Add("database", config.DbName)

	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(config.User, config.Password),
		Host:     fmt.Sprintf("%s:%d", config.Host, config.Port),
		RawQuery: query.Encode(),
	}
	return u.String()
}

func splitTableName(table string) (string, string) {
	schema := "dbo"
	schemaTable := strings.SplitN(table, ".", 2)
	if len(schemaTable) == 2 {
		schema = schemaTable[0]
		table = schemaTable[1]
	}
	return schema, table
}

func removeBrace(str string) string {
	return strings.Replace(strings.Replace(str, "(", "", -1), ")", "", -1)
}
