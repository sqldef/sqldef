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
	pkeyDef, err := d.getPrimaryKeyColumns(table)
	if err != nil {
		return "", err
	}
	return buildDumpTableDDL(table, cols, pkeyDef), nil
}

func buildDumpTableDDL(table string, columns []column, pkeyDef *pkeyDef) string {
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
		if col.Default != "" {
			fmt.Fprintf(&queryBuilder, " DEFAULT %s", col.Default)
		}
		if col.IsIdentity {
			fmt.Fprintf(&queryBuilder, " IDENTITY(%s,%s)", col.SeedValue, col.IncrementValue)
		}
	}
	if len(pkeyDef.columnNames) > 0 {
		var clusterDef string
		if pkeyDef.indexID == "1" {
			clusterDef = "CLUSTERED"
		} else {
			clusterDef = "NONCLUSTERED"
		}
		fmt.Fprint(&queryBuilder, ",\n"+indent)
		fmt.Fprintf(&queryBuilder, "CONSTRAINT %s PRIMARY KEY %s ([%s])", pkeyDef.constraintName, clusterDef, strings.Join(pkeyDef.columnNames, ", "))
	}
	fmt.Fprintf(&queryBuilder, "\n);\n")
	return strings.TrimSuffix(queryBuilder.String(), ";\n")
}

type column struct {
	Name           string
	dataType       string
	Length         string
	Nullable       bool
	IsIdentity     bool
	SeedValue      string
	IncrementValue string
	Default        string
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
	default_definition = OBJECT_DEFINITION(c.default_object_id)
FROM sys.columns c WITH(NOLOCK)
	JOIN sys.types tp WITH(NOLOCK) ON c.user_type_id = tp.user_type_id
WHERE c.[object_id] = OBJECT_ID('%s.%s', 'U')`, schema, table)

	rows, err := d.db.Query(query)
	if err != nil {
		fmt.Println(err)
	}
	defer rows.Close()

	cols := []column{}
	for rows.Next() {
		col := column{}
		var colName, dataType, maxLen string
		var seedValue, incrementValue, colDefault *string
		var isNullable, isIdentity bool
		err = rows.Scan(&colName, &dataType, &maxLen, &isNullable, &isIdentity, &seedValue, &incrementValue, &colDefault)
		if err != nil {
			return nil, err
		}
		col.Name = colName
		col.Length = maxLen
		if colDefault != nil {
			col.Default = removeBrace(*colDefault)
		}
		col.Nullable = isNullable
		col.dataType = dataType
		col.IsIdentity = isIdentity
		if isIdentity {
			col.SeedValue = *seedValue
			col.IncrementValue = *incrementValue
		}
		cols = append(cols, col)
	}
	return cols, nil
}

type pkeyDef struct {
	constraintName string
	columnNames    []string
	indexID        string
}

func (d *MssqlDatabase) getPrimaryKeyColumns(table string) (*pkeyDef, error) {
	schema, table := splitTableName(table)
	query := fmt.Sprintf(`SELECT
	pk_name = kc.name,
	column_name = c.name,
	ic.index_id,
	ic.is_descending_key
FROM sys.key_constraints kc WITH(NOLOCK)
JOIN sys.index_columns ic WITH(NOLOCK) ON
	kc.parent_object_id = ic.object_id
AND ic.index_id = kc.unique_index_id
JOIN sys.columns c WITH(NOLOCK) ON
	ic.[object_id] = c.[object_id]
AND ic.column_id = c.column_id
WHERE kc.parent_object_id = OBJECT_ID('[%s].[%s]', 'U')
AND kc.[type] = 'PK'`, schema, table)

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columnNames := make([]string, 0)
	var constraintName, indexID string
	var isDescending bool
	for rows.Next() {
		var columnName string
		err = rows.Scan(&constraintName, &columnName, &indexID, &isDescending)
		if err != nil {
			return nil, err
		}
		columnNames = append(columnNames, columnName)
	}

	return &pkeyDef{constraintName: constraintName, columnNames: columnNames, indexID: indexID}, nil
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
