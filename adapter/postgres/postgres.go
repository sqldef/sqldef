package postgres

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/k0kubun/sqldef/adapter"
	_ "github.com/lib/pq"
)

const indent = "    "

type PostgresDatabase struct {
	config adapter.Config
	db     *sql.DB
}

func NewDatabase(config adapter.Config) (adapter.Database, error) {
	db, err := sql.Open("postgres", postgresBuildDSN(config))
	if err != nil {
		return nil, err
	}

	return &PostgresDatabase{
		db:     db,
		config: config,
	}, nil
}

func (d *PostgresDatabase) TableNames() ([]string, error) {
	rows, err := d.db.Query("select table_name from information_schema.tables where table_schema='public';")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := []string{}
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}
	return tables, nil
}

func (d *PostgresDatabase) DumpTableDDL(table string) (string, error) {
	cols, err := d.getColumns(table)
	if err != nil {
		return "", err
	}
	primaryKeyDef, err := d.getPrimaryKeyDef(table)
	if err != nil {
		return "", err
	}
	indexDefs, err := d.getIndexDefs(table)
	if err != nil {
		return "", err
	}
	foreginDefs, err := d.getForeginDefs(table)
	if err != nil {
		return "", err
	}
	return buildDumpTableDDL(table, cols, primaryKeyDef, indexDefs, foreginDefs), nil
}

func buildDumpTableDDL(table string, columns []column, primaryKeyDef string, indexDefs, foreginDefs []string) string {
	var queryBuilder strings.Builder
	fmt.Fprintf(&queryBuilder, "CREATE TABLE public.%s (\n", table)
	for i, col := range columns {
		isLast := i == len(columns)-1
		fmt.Fprint(&queryBuilder, indent)
		fmt.Fprintf(&queryBuilder, "%s %s", col.Name, col.GetDataType())
		if col.Length > 0 {
			fmt.Fprintf(&queryBuilder, "(%d)", col.Length)
		}
		if col.IsUnique {
			fmt.Fprint(&queryBuilder, " UNIQUE")
		}
		if !col.Nullable {
			fmt.Fprint(&queryBuilder, " NOT NULL")
		}
		if col.Default != "" && !col.IsAutoIncrement {
			fmt.Fprintf(&queryBuilder, " DEFAULT %s", col.Default)
		}
		if isLast {
			fmt.Fprintln(&queryBuilder, "")
		} else {
			fmt.Fprintln(&queryBuilder, ",")
		}
	}
	fmt.Fprintf(&queryBuilder, ");\n")
	if primaryKeyDef != "" {
		fmt.Fprintf(&queryBuilder, "%s;\n", primaryKeyDef)
	}
	for _, v := range indexDefs {
		fmt.Fprintf(&queryBuilder, "%s;\n", v)
	}
	for _, v := range foreginDefs {
		fmt.Fprintf(&queryBuilder, "%s;\n", v)
	}
	return strings.TrimSuffix(queryBuilder.String(), ";\n")
}

type column struct {
	Name            string
	dataType        string
	Length          int
	Nullable        bool
	Default         string
	IsPrimaryKey    bool
	IsAutoIncrement bool
	IsUnique        bool
}

func (c *column) GetDataType() string {
	switch c.dataType {
	case "smallint":
		if c.IsAutoIncrement {
			return "smallserial"
		}
		return c.dataType
	case "integer":
		if c.IsAutoIncrement {
			return "serial"
		}
		return c.dataType
	case "bigint":
		if c.IsAutoIncrement {
			return "bigserial"
		}
		return c.dataType
	case "timestamp without time zone":
		// Note:
		// The SQL standard requires that writing just timestamp be equivalent to timestamp without time zone, and PostgreSQL honors that behavior.
		// timestamptz is accepted as an abbreviation for timestamp with time zone; this is a PostgreSQL extension.
		// https://www.postgresql.org/docs/9.6/datatype-datetime.html
		return "timestamp"
	case "time without time zone":
		return "time"
	default:
		return c.dataType
	}
}

func (d *PostgresDatabase) getColumns(table string) ([]column, error) {
	query := `SELECT column_name, column_default, is_nullable, data_type, character_maximum_length,
	CASE WHEN p.contype = 'p' THEN true ELSE false END AS primarykey,
	CASE WHEN p.contype = 'u' THEN true ELSE false END AS uniquekey
FROM pg_attribute f
	JOIN pg_class c ON c.oid = f.attrelid JOIN pg_type t ON t.oid = f.atttypid
	LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = f.attnum
	LEFT JOIN pg_namespace n ON n.oid = c.relnamespace
	LEFT JOIN pg_constraint p ON p.conrelid = c.oid AND f.attnum = ANY (p.conkey)
	LEFT JOIN pg_class AS g ON p.confrelid = g.oid
	LEFT JOIN INFORMATION_SCHEMA.COLUMNS s ON s.column_name=f.attname AND c.relname=s.table_name
WHERE c.relkind = 'r'::char AND c.relname = $1 AND f.attnum > 0 ORDER BY f.attnum;`

	rows, err := d.db.Query(query, table)
	if err != nil {
		fmt.Println(err)
	}
	defer rows.Close()

	cols := make([]column, 0)
	for rows.Next() {
		col := column{}
		var colName, isNullable, dataType string
		var maxLenStr, colDefault *string
		var isPK, isUnique bool
		err = rows.Scan(&colName, &colDefault, &isNullable, &dataType, &maxLenStr, &isPK, &isUnique)
		if err != nil {
			return nil, err
		}
		var maxLen int
		if maxLenStr != nil {
			maxLen, err = strconv.Atoi(*maxLenStr)
			if err != nil {
				return nil, err
			}
		}
		col.Name = strings.Trim(colName, `" `)
		if colDefault != nil || isPK {
			if isPK {
				col.IsPrimaryKey = true
			} else {
				col.Default = *colDefault
			}
		}
		col.IsUnique = isUnique
		if colDefault != nil && strings.HasPrefix(*colDefault, "nextval(") {
			col.IsAutoIncrement = true
		}
		col.Nullable = (isNullable == "YES")
		col.dataType = dataType
		col.Length = maxLen

		cols = append(cols, col)
	}
	return cols, nil
}

func (d *PostgresDatabase) getIndexDefs(table string) ([]string, error) {
	query := "SELECT indexName, indexdef FROM pg_indexes WHERE tablename=$1"
	rows, err := d.db.Query(query, table)
	if err != nil {
		fmt.Println(err)
	}
	defer rows.Close()

	indexes := make([]string, 0)
	for rows.Next() {
		var indexName, indexdef string
		err = rows.Scan(&indexName, &indexdef)
		if err != nil {
			return nil, err
		}
		indexName = strings.Trim(indexName, `" `)
		if strings.HasSuffix(indexName, "_pkey") {
			continue
		}
		indexes = append(indexes, indexdef)
	}
	return indexes, nil
}

func (d *PostgresDatabase) getPrimaryKeyDef(table string) (string, error) {
	query := `SELECT
	tc.table_schema, tc.constraint_name, tc.table_name, kcu.column_name
FROM 
	information_schema.table_constraints AS tc 
	JOIN information_schema.key_column_usage AS kcu
		ON tc.constraint_name = kcu.constraint_name
WHERE constraint_type = 'PRIMARY KEY' AND tc.table_name=$1 ORDER BY kcu.ordinal_position`
	rows, err := d.db.Query(query, table)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	columnNames := make([]string, 0)
	var tableSchema, constraintName, tableName string
	for rows.Next() {
		var columnName string
		err = rows.Scan(&tableSchema, &constraintName, &tableName, &columnName)
		if err != nil {
			return "", err
		}
		columnNames = append(columnNames, columnName)
	}
	if len(columnNames) == 0 {
		return "", nil
	}
	return fmt.Sprintf("ALTER TABLE ONLY %s.%s\n%sADD CONSTRAINT %s PRIMARY KEY (%s)",
		tableSchema, tableName, indent, constraintName, strings.Join(columnNames, ","),
	), nil
}

// refs: https://gist.github.com/PickledDragon/dd41f4e72b428175354d
func (d *PostgresDatabase) getForeginDefs(table string) ([]string, error) {
	query := `SELECT
	tc.table_schema, tc.constraint_name, tc.table_name, kcu.column_name, 
	ccu.table_schema AS foreign_table_schema,
	ccu.table_name AS foreign_table_name,
	ccu.column_name AS foreign_column_name 
FROM 
	information_schema.table_constraints AS tc 
	JOIN information_schema.key_column_usage AS kcu
		ON tc.constraint_name = kcu.constraint_name
	JOIN information_schema.constraint_column_usage AS ccu
		ON ccu.constraint_name = tc.constraint_name
WHERE constraint_type = 'FOREIGN KEY' AND tc.table_name=$1`
	rows, err := d.db.Query(query, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	defs := make([]string, 0)
	for rows.Next() {
		var tableSchema, constraintName, tableName, columnName, foreignTableSchema, foreignTableName, foreignColumnName string
		err = rows.Scan(&tableSchema, &constraintName, &tableName, &columnName, &foreignTableSchema, &foreignTableName, &foreignColumnName)
		if err != nil {
			return nil, err
		}
		def := fmt.Sprintf(
			"ALTER TABLE ONLY %s.%s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s.%s(%s) ON UPDATE RESTRICT ON DELETE SET NULL",
			tableSchema, tableName, constraintName, columnName, foreignTableSchema, foreignTableName, foreignColumnName,
		)
		defs = append(defs, def)
	}
	return defs, nil
}

func (d *PostgresDatabase) DB() *sql.DB {
	return d.db
}

func (d *PostgresDatabase) Close() error {
	return d.db.Close()
}

func postgresBuildDSN(config adapter.Config) string {
	user := config.User
	password := config.Password
	database := config.DbName
	host := ""
	if config.Socket == "" {
		host = fmt.Sprintf("%s:%d", config.Host, config.Port)
	} else {
		host = config.Socket
	}

	options := ""
	if sslmode, ok := os.LookupEnv("PGSSLMODE"); ok { // TODO: have this in adapter.Config, or standardize config with DSN?
		options = fmt.Sprintf("?sslmode=%s", sslmode) // TODO: uri escape
	}

	// TODO: uri escape
	return fmt.Sprintf("postgres://%s:%s@%s/%s%s", user, password, host, database, options)
}
