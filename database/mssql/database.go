package mssql

import (
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	_ "github.com/microsoft/go-mssqldb"
	"github.com/sqldef/sqldef/database"
)

const indent = "    "

type databaseInfo struct {
	tableName   []string
	columns     map[string][]column
	indexDefs   map[string][]*indexDef
	foreignDefs map[string][]string
}

type MssqlDatabase struct {
	config        database.Config
	db            *sql.DB
	defaultSchema *string
	info          databaseInfo
}

func NewDatabase(config database.Config) (database.Database, error) {
	db, err := sql.Open("sqlserver", mssqlBuildDSN(config))
	if err != nil {
		return nil, err
	}

	return &MssqlDatabase{
		db:     db,
		config: config,
	}, nil
}

func (d *MssqlDatabase) DumpDDLs() (string, error) {
	var ddls []string

	err := d.updateDatabaesInfo()
	if err != nil {
		return "", err
	}

	tableNames := d.tableNames()
	for _, tableName := range tableNames {
		ddl, err := d.dumpTableDDL(tableName)
		if err != nil {
			return "", err
		}

		ddls = append(ddls, ddl)
	}

	viewDDLs, err := d.views()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, viewDDLs...)

	triggerDDLs, err := d.triggers()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, triggerDDLs...)

	return strings.Join(ddls, "\n\n"), nil
}

func (d *MssqlDatabase) updateDatabaesInfo() error {
	var err error

	err = d.updateTableNames()
	if err != nil {
		return err
	}
	err = d.updateColumns()
	if err != nil {
		return err
	}
	err = d.updateIndexDefs()
	if err != nil {
		return err
	}
	err = d.updateForeignDefs()
	if err != nil {
		return err
	}

	return nil
}

func (d *MssqlDatabase) updateTableNames() error {
	rows, err := d.db.Query(
		`select schema_name(schema_id) as table_schema, name from sys.objects where type = 'U';`,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	tables := []string{}
	for rows.Next() {
		var schema, name string
		if err := rows.Scan(&schema, &name); err != nil {
			return err
		}
		tables = append(tables, schema+"."+name)
	}
	d.info.tableName = tables
	return nil
}

func (d *MssqlDatabase) tableNames() []string {
	return d.info.tableName
}

func (d *MssqlDatabase) dumpTableDDL(table string) (string, error) {
	cols := d.getColumns(table)
	indexDefs := d.getIndexDefs(table)
	foreignDefs := d.getForeignDefs(table)
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
		fmt.Fprintf(&queryBuilder, "%s %s", quoteName(col.Name), col.dataType)
		if length, ok := col.getLength(); ok {
			fmt.Fprintf(&queryBuilder, "(%s)", length)
		}
		if !col.Nullable {
			fmt.Fprint(&queryBuilder, " NOT NULL")
		}
		if col.DefaultName != "" {
			fmt.Fprintf(&queryBuilder, " CONSTRAINT %s DEFAULT %s", quoteName(col.DefaultName), col.DefaultVal)
		}
		if col.Identity != nil {
			fmt.Fprintf(&queryBuilder, " IDENTITY(%s,%s)", col.Identity.SeedValue, col.Identity.IncrementValue)
			if col.Identity.NotForReplication {
				fmt.Fprint(&queryBuilder, " NOT FOR REPLICATION")
			}
		}
		if col.Check != nil {
			fmt.Fprintf(&queryBuilder, " CONSTRAINT %s CHECK", quoteName(col.Check.Name))
			if col.Check.NotForReplication {
				fmt.Fprint(&queryBuilder, " NOT FOR REPLICATION")
			}
			fmt.Fprintf(&queryBuilder, " %s", col.Check.Definition)
		}
	}

	// PRIMARY KEY
	for _, indexDef := range indexDefs {
		if !indexDef.primary {
			continue
		}
		fmt.Fprint(&queryBuilder, ",\n"+indent)
		fmt.Fprintf(&queryBuilder, "CONSTRAINT %s PRIMARY KEY", quoteName(indexDef.name))

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

	for _, indexDef := range indexDefs {
		if indexDef.primary {
			continue
		}
		fmt.Fprint(&queryBuilder, "CREATE")
		if indexDef.unique {
			fmt.Fprint(&queryBuilder, " UNIQUE")
		}
		switch indexDef.indexType {
		case "CLUSTERED", "NONCLUSTERED", "NONCLUSTERED COLUMNSTORE":
			fmt.Fprintf(&queryBuilder, " %s", indexDef.indexType)
		}
		if indexDef.indexType == "NONCLUSTERED COLUMNSTORE" {
			fmt.Fprintf(&queryBuilder, " INDEX [%s] ON %s (%s)", indexDef.name, table, strings.Join(indexDef.included, ", "))
		} else {
			fmt.Fprintf(&queryBuilder, " INDEX [%s] ON %s (%s)", indexDef.name, table, strings.Join(indexDef.columns, ", "))
			if len(indexDef.included) > 0 {
				fmt.Fprintf(&queryBuilder, " INCLUDE (%s)", strings.Join(indexDef.included, ", "))
			}
		}
		if indexDef.filter != nil {
			fmt.Fprintf(&queryBuilder, " WHERE %s", *indexDef.filter)
		}
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
		fmt.Fprintf(&queryBuilder, ";")
	}
	return strings.TrimSuffix(queryBuilder.String(), "\n")
}

type column struct {
	Name        string
	dataType    string
	MaxLength   string
	Scale       string
	Nullable    bool
	Identity    *identity
	DefaultName string
	DefaultVal  string
	Check       *check
}

func (c column) getLength() (string, bool) {
	switch c.dataType {
	case "char", "varchar", "binary", "varbinary":
		if c.MaxLength == "-1" {
			return "max", true
		}
		return c.MaxLength, true
	case "nvarchar", "nchar":
		if c.MaxLength == "-1" {
			return "max", true
		}
		maxLength, err := strconv.Atoi(c.MaxLength)
		if err != nil {
			return "", false
		}
		return strconv.Itoa(int(maxLength / 2)), true
	case "datetimeoffset", "datetime2":
		if c.Scale == "7" {
			return "", false
		}
		return c.Scale, true
	case "numeric", "decimal":
		if c.Scale == "0" {
			if c.MaxLength == "18" { // The default precision is 18.
				return "", false
			}
			return c.MaxLength, true
		}
		return c.MaxLength + ", " + c.Scale, true
	}
	return "", false
}

type identity struct {
	SeedValue         string
	IncrementValue    string
	NotForReplication bool
}

type check struct {
	Name              string
	Definition        string
	NotForReplication bool
}

func (d *MssqlDatabase) updateColumns() error {
	query := `SELECT
	schema_name = SCHEMA_NAME(o.schema_id),
	table_name = OBJECT_NAME(o.object_id),
	c.name,
	[type_name] = tp.name,
	c.max_length,
	c.precision,
	c.scale,
	c.is_nullable,
	c.is_identity,
	ic.seed_value,
	ic.increment_value,
	ic.is_not_for_replication,
	c.default_object_id,
	default_name = OBJECT_NAME(c.default_object_id),
	default_definition = OBJECT_DEFINITION(c.default_object_id),
	cc.name,
	cc.definition,
	cc.is_not_for_replication
FROM sys.objects o WITH(NOLOCK)
JOIN sys.columns c WITH(NOLOCK) on o.object_id = c.object_id
JOIN sys.types tp WITH(NOLOCK) ON c.user_type_id = tp.user_type_id
LEFT JOIN sys.check_constraints cc WITH(NOLOCK) ON c.[object_id] = cc.parent_object_id AND cc.parent_column_id = c.column_id
LEFT JOIN sys.identity_columns ic WITH(NOLOCK) ON c.[object_id] = ic.[object_id] AND ic.[column_id] = c.[column_id]
WHERE o.type = 'U'
ORDER BY c.object_id, COLUMNPROPERTY(c.object_id, c.name, 'ordinal')
`

	rows, err := d.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	allCols := make(map[string][]column)
	for rows.Next() {
		col := column{}
		var colName, dataType, maxLen, precision, scale, defaultId string
		var seedValue, incrementValue, defaultName, defaultVal, checkName, checkDefinition *string
		var schemaName, tableName *string
		var isNullable, isIdentity bool
		var identityNotForReplication, checkNotForReplication *bool
		err = rows.Scan(&schemaName, &tableName, &colName, &dataType, &maxLen, &precision, &scale, &isNullable, &isIdentity, &seedValue, &incrementValue, &identityNotForReplication, &defaultId, &defaultName, &defaultVal, &checkName, &checkDefinition, &checkNotForReplication)
		if err != nil {
			return err
		}
		col.Name = colName
		col.MaxLength = maxLen
		switch dataType {
		case "numeric", "decimal":
			col.MaxLength = precision
		}
		col.Scale = scale
		if defaultId != "0" {
			col.DefaultName = *defaultName
			col.DefaultVal = *defaultVal
		}
		col.Nullable = isNullable
		col.dataType = dataType
		if isIdentity {
			col.Identity = &identity{
				SeedValue:         *seedValue,
				IncrementValue:    *incrementValue,
				NotForReplication: *identityNotForReplication,
			}
		}
		if checkName != nil {
			col.Check = &check{
				Name:              *checkName,
				Definition:        *checkDefinition,
				NotForReplication: *checkNotForReplication,
			}
		}
		key := *schemaName + "." + *tableName
		_, ok := allCols[key]
		if !ok {
			allCols[key] = []column{}
		}
		allCols[key] = append(allCols[key], col)
	}

	d.info.columns = allCols
	return nil
}

func (d *MssqlDatabase) getColumns(table string) []column {
	schema, table := splitTableName(table, d.GetDefaultSchema())
	cols, ok := d.info.columns[schema+"."+table]

	if ok {
		return cols
	} else {
		return []column{}
	}
}

type indexDef struct {
	name      string
	columns   []string
	primary   bool
	unique    bool
	indexType string
	filter    *string
	included  []string
	options   []indexOption
}

type indexOption struct {
	name  string
	value string
}

func (d *MssqlDatabase) updateIndexDefs() error {
	query := `SELECT
	SCHEMA_NAME(obj.schema_id) as schema_name,
    obj.name as table_name,
	ind.name AS index_name,
	ind.is_primary_key,
	ind.is_unique,
	ind.type_desc,
	ind.filter_definition,
	ind.is_padded,
	ind.fill_factor,
	ind.ignore_dup_key,
	st.no_recompute,
	st.is_incremental,
	ind.allow_row_locks,
	ind.allow_page_locks,
    COL_NAME(ic.object_id, ic.column_id) as column_name,
    ic.is_descending_key,
    ic.is_included_column
FROM sys.objects obj
INNER JOIN sys.indexes ind ON obj.object_id = ind.object_id
INNER JOIN sys.stats st ON ind.object_id = st.object_id AND ind.index_id = st.stats_id
INNER JOIN sys.index_columns ic ON ind.index_id = ic.index_id AND ind.object_id = ic.object_id
WHERE obj.type = 'U'
ORDER BY obj.object_id, ind.index_id, ic.key_ordinal
`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil
	}

	indexMap := make(map[string]map[string]*indexDef)
	var schemaName, tableName, columnName, indexName, typeDesc, fillfactor string
	var filter *string
	var isPrimary, isUnique, padIndex, ignoreDupKey, noRecompute, incremental, rowLocks, pageLocks, isDescending, isIncluded bool

	for rows.Next() {
		err = rows.Scan(&schemaName, &tableName, &indexName, &isPrimary, &isUnique, &typeDesc, &filter, &padIndex, &fillfactor, &ignoreDupKey, &noRecompute, &incremental, &rowLocks, &pageLocks, &columnName, &isDescending, &isIncluded)
		if err != nil {
			return err
		}
		defer rows.Close()

		indexes, ok := indexMap[schemaName+"."+tableName]
		if !ok {
			indexes = make(map[string]*indexDef)
			indexMap[schemaName+"."+tableName] = indexes
		}

		definition, ok := indexes[indexName]

		if !ok {
			options := []indexOption{
				{name: "PAD_INDEX", value: boolToOnOff((padIndex))},
			}

			if padIndex {
				options = append(options, indexOption{name: "FILLFACTOR", value: fillfactor})
			}

			options = append(options, []indexOption{
				{name: "IGNORE_DUP_KEY", value: boolToOnOff(ignoreDupKey)},
				{name: "STATISTICS_NORECOMPUTE", value: boolToOnOff(noRecompute)},
				{name: "STATISTICS_INCREMENTAL", value: boolToOnOff(incremental)},
				{name: "ALLOW_ROW_LOCKS", value: boolToOnOff(rowLocks)},
				{name: "ALLOW_PAGE_LOCKS", value: boolToOnOff(pageLocks)},
			}...)

			definition = &indexDef{name: indexName, columns: []string{}, primary: isPrimary, unique: isUnique, indexType: typeDesc, filter: filter, included: []string{}, options: options}
			indexes[indexName] = definition
		}

		columnDefinition := quoteName(columnName)

		if isIncluded {
			definition.included = append(definition.included, columnDefinition)
		} else {
			if isDescending {
				columnDefinition += " DESC"
			}
			definition.columns = append(definition.columns, columnDefinition)
		}
	}

	indexDefs := make(map[string][]*indexDef)

	for tableName, indexes := range indexMap {
		tableIndexes := []*indexDef{}

		for _, definition := range indexes {
			tableIndexes = append(tableIndexes, definition)
		}

		indexDefs[tableName] = tableIndexes
	}

	d.info.indexDefs = indexDefs

	return nil
}

func (d *MssqlDatabase) getIndexDefs(table string) []*indexDef {
	schema, table := splitTableName(table, d.GetDefaultSchema())

	indexDefs, ok := d.info.indexDefs[schema+"."+table]
	if ok {
		return indexDefs
	} else {
		return []*indexDef{}
	}
}

func (d *MssqlDatabase) updateForeignDefs() error {
	query := `SELECT
	SCHEMA_NAME(obj.schema_id),
    obj.name as table_name,
    f.name as constraint_name,
    COL_NAME(obj.object_id, fc.parent_column_id) as column_name,
    OBJECT_NAME(f.referenced_object_id) as ref_table_name,
    COL_NAME(f.referenced_object_id, fc.referenced_column_id) as ref_column_name,
    f.update_referential_action_desc,
	f.delete_referential_action_desc,
	f.is_not_for_replication
FROM sys.objects obj
INNER JOIN sys.foreign_keys f ON f.parent_object_id = obj.object_id
INNER JOIN sys.foreign_key_columns fc ON f.object_id = fc.constraint_object_id
WHERE obj.type = 'U'`

	rows, err := d.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	defs := make(map[string][]string)

	for rows.Next() {
		var schemaName, tableName, constraintName, columnName, foreignTableName, foreignColumnName, foreignUpdateRule, foreignDeleteRule string
		var notForReplication bool

		err = rows.Scan(&schemaName, &tableName, &constraintName, &columnName, &foreignTableName, &foreignColumnName, &foreignUpdateRule, &foreignDeleteRule, &notForReplication)
		if err != nil {
			return err
		}
		foreignUpdateRule = strings.Replace(foreignUpdateRule, "_", " ", -1)
		foreignDeleteRule = strings.Replace(foreignDeleteRule, "_", " ", -1)

		def := fmt.Sprintf("CONSTRAINT [%s] FOREIGN KEY ([%s]) REFERENCES [%s] ([%s]) ON UPDATE %s ON DELETE %s", constraintName, columnName, foreignTableName, foreignColumnName, foreignUpdateRule, foreignDeleteRule)
		if notForReplication {
			def += " NOT FOR REPLICATION"
		}

		defs[schemaName+"."+tableName] = append(defs[schemaName+"."+tableName], def)
	}

	d.info.foreignDefs = defs

	return nil
}

func (d *MssqlDatabase) getForeignDefs(table string) []string {
	schema, table := splitTableName(table, d.GetDefaultSchema())

	if defs, ok := d.info.foreignDefs[schema+"."+table]; ok {
		return defs
	} else {
		return make([]string, 0)
	}
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

func (d *MssqlDatabase) views() ([]string, error) {
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
		definition = strings.ReplaceAll(definition, "\n", " ")
		definition = suffixSemicolon.ReplaceAllString(definition, "")
		definition = spaces.ReplaceAllString(definition, " ")
		ddls = append(ddls, definition+";")
	}
	return ddls, nil
}

func (d *MssqlDatabase) triggers() ([]string, error) {
	query := `SELECT
	s.definition
FROM sys.triggers tr
INNER JOIN sys.all_sql_modules s ON s.object_id = tr.object_id`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	triggers := make([]string, 0)
	for rows.Next() {
		var definition string
		err = rows.Scan(&definition)
		if err != nil {
			return nil, err
		}
		triggers = append(triggers, definition+";")
	}

	return triggers, nil
}

func (d *MssqlDatabase) DB() *sql.DB {
	return d.db
}

func (d *MssqlDatabase) Close() error {
	return d.db.Close()
}

func (d *MssqlDatabase) GetDefaultSchema() string {
	if d.defaultSchema != nil {
		return *d.defaultSchema
	}

	var defaultSchema string
	query := "SELECT schema_name();"

	err := d.db.QueryRow(query).Scan(&defaultSchema)
	if err != nil {
		return ""
	}

	d.defaultSchema = &defaultSchema

	return defaultSchema
}

func mssqlBuildDSN(config database.Config) string {
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

func splitTableName(table string, defaultSchmea string) (string, string) {
	schema := defaultSchmea
	schemaTable := strings.SplitN(table, ".", 2)
	if len(schemaTable) == 2 {
		schema = schemaTable[0]
		table = schemaTable[1]
	}
	return schema, table
}

func quoteName(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}
