package postgres

import (
	"database/sql"
	"fmt"
	"iter"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"

	_ "github.com/lib/pq"
	"github.com/sqldef/sqldef/v3/database"
	schemaLib "github.com/sqldef/sqldef/v3/schema"
)

const indent = "    "

type PostgresDatabase struct {
	config          database.Config
	generatorConfig database.GeneratorConfig
	db              *sql.DB
	defaultSchema   *string
}

func NewDatabase(config database.Config) (database.Database, error) {
	db, err := sql.Open("postgres", postgresBuildDSN(config))
	if err != nil {
		return nil, err
	}

	return &PostgresDatabase{
		db:     db,
		config: config,
	}, nil
}

func (d *PostgresDatabase) SetGeneratorConfig(config database.GeneratorConfig) {
	d.generatorConfig = config
}

func (d *PostgresDatabase) GetTransactionQueries() database.TransactionQueries {
	return database.TransactionQueries{
		Begin:    "BEGIN",
		Commit:   "COMMIT",
		Rollback: "ROLLBACK",
	}
}

func (d *PostgresDatabase) ExportDDLs() (string, error) {
	var ddls []string

	schemaDDLs, err := d.schemas()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, schemaDDLs...)

	extensionDDLs, err := d.extensions()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, extensionDDLs...)

	typeDDLs, err := d.types()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, typeDDLs...)

	tableNames, err := d.tableNames()
	if err != nil {
		return "", err
	}

	tableDDLs, err := database.ConcurrentMapFuncWithError(
		tableNames,
		d.config.DumpConcurrency,
		func(tableName string) (string, error) {
			return d.exportTableDDL(tableName)
		})
	if err != nil {
		return "", err
	}
	ddls = append(ddls, tableDDLs...)

	viewDDLs, err := d.views()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, viewDDLs...)

	matViewDDLs, err := d.materializedViews()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, matViewDDLs...)

	return strings.Join(ddls, "\n\n"), nil
}

func (d *PostgresDatabase) tableNames() ([]string, error) {
	rows, err := d.db.Query(`
		select n.nspname as table_schema, relname as table_name from pg_catalog.pg_class c
		inner join pg_catalog.pg_namespace n on c.relnamespace = n.oid
		where n.nspname not in ('information_schema', 'pg_catalog')
		and c.relkind in ('r', 'p')
		and c.relpersistence in ('p', 'u')
		and c.relispartition = false
		and not exists (select * from pg_catalog.pg_depend d where c.oid = d.objid and d.deptype = 'e')
		order by relname asc;
	`)
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
		if d.config.TargetSchema != nil && !slices.Contains(d.config.TargetSchema, schema) {
			continue
		}
		tables = append(tables, schema+"."+name)
	}
	return tables, nil
}

var (
	suffixSemicolon = regexp.MustCompile(`;$`)
	spaces          = regexp.MustCompile(`[ ]+`)
)

func (d *PostgresDatabase) views() ([]string, error) {
	if d.config.SkipView {
		return []string{}, nil
	}

	rows, err := d.db.Query(`
		select n.nspname as table_schema, c.relname as table_name, pg_get_viewdef(c.oid) as definition
		from pg_catalog.pg_class c inner join pg_catalog.pg_namespace n on c.relnamespace = n.oid
		where n.nspname not in ('information_schema', 'pg_catalog')
		and c.relkind = 'v'
		and not exists (select * from pg_catalog.pg_depend d where c.oid = d.objid and d.deptype = 'e')
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ddls []string
	for rows.Next() {
		var schema, name, definition string
		if err := rows.Scan(&schema, &name, &definition); err != nil {
			return nil, err
		}
		if d.config.TargetSchema != nil && !slices.Contains(d.config.TargetSchema, schema) {
			continue
		}
		definition = strings.TrimSpace(definition)
		definition = strings.ReplaceAll(definition, "\n", "")
		definition = suffixSemicolon.ReplaceAllString(definition, "")
		definition = spaces.ReplaceAllString(definition, " ")
		ddls = append(
			ddls, fmt.Sprintf(
				"CREATE VIEW %s AS %s;", schema+"."+name, definition,
			),
		)
	}
	return ddls, nil
}

func (d *PostgresDatabase) materializedViews() ([]string, error) {
	if d.config.SkipView {
		return []string{}, nil
	}

	rows, err := d.db.Query(`
		select n.nspname as schemaname, c.relname as matviewname, pg_get_viewdef(c.oid) as definition
		from pg_catalog.pg_class c inner join pg_catalog.pg_namespace n on c.relnamespace = n.oid
		where c.relkind = 'm'
		and not exists (select * from pg_catalog.pg_depend d where c.oid = d.objid and d.deptype = 'e')
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ddls []string
	for rows.Next() {
		var schema, name, definition string
		if err := rows.Scan(&schema, &name, &definition); err != nil {
			return nil, err
		}
		if d.config.TargetSchema != nil && !slices.Contains(d.config.TargetSchema, schema) {
			continue
		}
		definition = strings.TrimSpace(definition)
		definition = strings.ReplaceAll(definition, "\n", "")
		definition = suffixSemicolon.ReplaceAllString(definition, "")
		definition = spaces.ReplaceAllString(definition, " ")
		ddls = append(
			ddls, fmt.Sprintf(
				"CREATE MATERIALIZED VIEW %s AS %s;", schema+"."+name, definition,
			),
		)

		indexDefs, err := d.getIndexDefs(schema + "." + name)
		if err != nil {
			return ddls, err
		}
		for _, indexDef := range indexDefs {
			ddls = append(ddls, fmt.Sprintf("%s;", indexDef))
		}
	}
	return ddls, nil
}

func (d *PostgresDatabase) schemas() ([]string, error) {
	rows, err := d.db.Query(`
		SELECT schema_name
		FROM information_schema.schemata
		WHERE schema_name NOT LIKE 'pg_%%'
		AND schema_name not in ('information_schema', 'public');
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ddls []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		ddls = append(
			ddls, fmt.Sprintf(
				"CREATE SCHEMA %s;", escapeSQLName(name),
			),
		)
	}
	return ddls, nil
}

func (d *PostgresDatabase) extensions() ([]string, error) {
	if d.config.SkipExtension {
		return []string{}, nil
	}

	rows, err := d.db.Query(`
		SELECT extname FROM pg_extension
		WHERE extname != 'plpgsql';
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ddls []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		ddls = append(
			ddls, fmt.Sprintf(
				"CREATE EXTENSION %s;", escapeSQLName(name),
			),
		)
	}
	return ddls, nil
}

func (d *PostgresDatabase) types() ([]string, error) {
	rows, err := d.db.Query(`
		select n.nspname as type_schema, t.typname, string_agg(e.enumlabel, ' ')
		from pg_enum e
		join pg_type t on e.enumtypid = t.oid
		inner join pg_catalog.pg_namespace n on t.typnamespace = n.oid
		where not exists (select * from pg_depend d where d.objid = t.oid and d.deptype = 'e')
		group by n.nspname, t.typname;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ddls []string
	for rows.Next() {
		var typeSchema, typeName, labels string
		if err := rows.Scan(&typeSchema, &typeName, &labels); err != nil {
			return nil, err
		}
		if d.config.TargetSchema != nil && !slices.Contains(d.config.TargetSchema, typeSchema) {
			continue
		}
		enumLabels := []string{}
		for _, label := range strings.Split(labels, " ") {
			enumLabels = append(enumLabels, fmt.Sprintf("'%s'", label))
		}
		ddls = append(
			ddls, fmt.Sprintf(
				"CREATE TYPE %s.%s AS ENUM (%s);", escapeSQLName(typeSchema), escapeSQLName(typeName), strings.Join(enumLabels, ", "),
			),
		)
	}
	return ddls, nil
}

type TableDDLComponents struct {
	TableName         string
	Columns           []column
	PrimaryKeyName    string
	PrimaryKeyCols    []string
	IndexDefs         []string
	ForeignDefs       []string
	ExclusionDefs     []string
	PolicyDefs        []string
	Comments          []string
	CheckConstraints  map[string]string
	UniqueConstraints map[string]string
	PrivilegeDefs     []string
	DefaultSchema     string
}

func (d *PostgresDatabase) exportTableDDL(table string) (string, error) {
	components := TableDDLComponents{
		TableName:     table,
		DefaultSchema: d.GetDefaultSchema(),
	}

	var err error
	components.Columns, err = d.getColumns(table)
	if err != nil {
		return "", fmt.Errorf("failed to get columns for table %s: %w", table, err)
	}
	components.PrimaryKeyCols, err = d.getPrimaryKeyColumns(table)
	if err != nil {
		return "", fmt.Errorf("failed to get primary key columns for table %s: %w", table, err)
	}
	// if pkey cols exist, retrieve the pkey name
	if len(components.PrimaryKeyCols) > 0 {
		components.PrimaryKeyName, err = d.getPrimaryKeyName(table)
		if err != nil {
			return "", fmt.Errorf("failed to get primary key name for table %s: %w", table, err)
		}
	}
	components.IndexDefs, err = d.getIndexDefs(table)
	if err != nil {
		return "", fmt.Errorf("failed to get index definitions for table %s: %w", table, err)
	}
	components.ForeignDefs, err = d.getForeignDefs(table)
	if err != nil {
		return "", fmt.Errorf("failed to get foreign key definitions for table %s: %w", table, err)
	}
	components.PolicyDefs, err = d.getPolicyDefs(table)
	if err != nil {
		return "", fmt.Errorf("failed to get policy definitions for table %s: %w", table, err)
	}
	components.CheckConstraints, err = d.getTableCheckConstraints(table)
	if err != nil {
		return "", fmt.Errorf("failed to get check constraints for table %s: %w", table, err)
	}
	components.UniqueConstraints, err = d.getUniqueConstraints(table)
	if err != nil {
		return "", fmt.Errorf("failed to get unique constraints for table %s: %w", table, err)
	}
	components.ExclusionDefs, err = d.getExclusionDefs(table)
	if err != nil {
		return "", fmt.Errorf("failed to get exclusion definitions for table %s: %w", table, err)
	}
	components.Comments, err = d.getComments(table)
	if err != nil {
		return "", fmt.Errorf("failed to get comments for table %s: %w", table, err)
	}
	components.PrivilegeDefs, err = d.getPrivilegeDefs(table)
	if err != nil {
		return "", fmt.Errorf("failed to get privilege definitions for table %s: %w", table, err)
	}
	return buildExportTableDDL(components), nil
}

func canonicalMapIter[T any](m map[string]T) iter.Seq2[string, T] {
	return func(yield func(string, T) bool) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			if !yield(k, m[k]) {
				return
			}
		}
	}
}

func buildExportTableDDL(components TableDDLComponents) string {
	var queryBuilder strings.Builder
	schema, table := splitTableName(components.TableName, components.DefaultSchema)
	fmt.Fprintf(&queryBuilder, "CREATE TABLE %s.%s (", escapeSQLName(schema), escapeSQLName(table))
	for i, col := range components.Columns {
		if i > 0 {
			fmt.Fprint(&queryBuilder, ",")
		}
		fmt.Fprint(&queryBuilder, "\n"+indent)
		fmt.Fprintf(&queryBuilder, "\"%s\" %s", col.Name, col.GetDataType())
		if !col.Nullable {
			fmt.Fprint(&queryBuilder, " NOT NULL")
		}
		if col.Default != "" && !col.IsAutoIncrement {
			fmt.Fprintf(&queryBuilder, " DEFAULT %s", col.Default)
		}
		if col.IdentityGeneration != "" {
			fmt.Fprintf(&queryBuilder, " GENERATED %s AS IDENTITY", col.IdentityGeneration)
		}
		if col.Check != nil {
			fmt.Fprintf(&queryBuilder, " CONSTRAINT %s %s", col.Check.name, col.Check.definition)
		}
	}
	if len(components.PrimaryKeyCols) > 0 {
		fmt.Fprint(&queryBuilder, ",\n"+indent)
		fmt.Fprintf(&queryBuilder, "CONSTRAINT %s PRIMARY KEY (\"%s\")", components.PrimaryKeyName, strings.Join(components.PrimaryKeyCols, "\", \""))
	}

	for constraintName, constraintDef := range canonicalMapIter(components.CheckConstraints) {
		fmt.Fprint(&queryBuilder, ",\n"+indent)
		fmt.Fprintf(&queryBuilder, "CONSTRAINT %s %s", constraintName, constraintDef)
	}

	fmt.Fprintf(&queryBuilder, "\n);\n")
	for _, v := range components.IndexDefs {
		fmt.Fprintf(&queryBuilder, "%s;\n", v)
	}
	for _, v := range components.ForeignDefs {
		fmt.Fprintf(&queryBuilder, "%s;\n", v)
	}
	for _, v := range components.ExclusionDefs {
		fmt.Fprintf(&queryBuilder, "%s;\n", v)
	}
	for _, v := range components.PolicyDefs {
		fmt.Fprintf(&queryBuilder, "%s;\n", v)
	}

	for _, constraintDef := range canonicalMapIter(components.UniqueConstraints) {
		fmt.Fprintf(&queryBuilder, "%s;\n", constraintDef)
	}

	for _, v := range components.Comments {
		fmt.Fprintf(&queryBuilder, "%s\n", v)
	}
	for _, v := range components.PrivilegeDefs {
		fmt.Fprintf(&queryBuilder, "%s;\n", v)
	}
	return strings.TrimSuffix(queryBuilder.String(), "\n")
}

type columnConstraint struct {
	definition string
	name       string
}

type column struct {
	Name               string
	dataType           string
	formattedDataType  string
	Nullable           bool
	Default            string
	IsAutoIncrement    bool
	IdentityGeneration string
	Check              *columnConstraint
}

func (c *column) GetDataType() string {
	switch c.dataType {
	case "smallint":
		if c.IsAutoIncrement {
			return "smallserial" + strings.TrimPrefix(c.formattedDataType, "smallint")
		}
		return c.dataType
	case "integer":
		if c.IsAutoIncrement {
			return "serial" + strings.TrimPrefix(c.formattedDataType, "integer")
		}
		return c.dataType
	case "bigint":
		if c.IsAutoIncrement {
			return "bigserial" + strings.TrimPrefix(c.formattedDataType, "bigint")
		}
		return c.dataType
	case "timestamp without time zone":
		// Note:
		// The SQL standard requires that writing just timestamp be equivalent to timestamp without time zone, and PostgreSQL honors that behavior.
		// timestamptz is accepted as an abbreviation for timestamp with time zone; this is a PostgreSQL extension.
		// https://www.postgresql.org/docs/9.6/datatype-datetime.html
		return strings.TrimSuffix(c.formattedDataType, " without time zone")
	case "timestamp with time zone":
		// PostgreSQL returns "timestamp with time zone" but we preserve the precision if specified
		// formattedDataType contains the precision: "timestamp(6) with time zone"
		// Note: timestamptz is a PostgreSQL extension abbreviation for timestamp with time zone
		return c.formattedDataType
	case "time without time zone":
		return strings.TrimSuffix(c.formattedDataType, " without time zone")
	case "time with time zone":
		// PostgreSQL returns "time with time zone" but we preserve the precision if specified
		// formattedDataType contains the precision: "time(6) with time zone"
		// Note: timetz is a PostgreSQL extension abbreviation for time with time zone
		return c.formattedDataType
	default:
		return c.formattedDataType
	}
}

func (d *PostgresDatabase) getColumns(table string) ([]column, error) {
	const query = `WITH
	  columns AS (
	    SELECT
	      s.column_name,
	      s.column_default,
	      s.is_nullable,
	      CASE
	      WHEN s.data_type IN ('ARRAY', 'USER-DEFINED') THEN format_type(f.atttypid, f.atttypmod)
	      ELSE s.data_type
	      END,
	      format_type(f.atttypid, f.atttypmod),
	      s.identity_generation
	    FROM pg_attribute f
	    JOIN pg_class c ON c.oid = f.attrelid JOIN pg_type t ON t.oid = f.atttypid
	    LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = f.attnum
	    LEFT JOIN pg_namespace n ON n.oid = c.relnamespace
	    LEFT JOIN information_schema.columns s ON s.column_name = f.attname AND s.table_name = c.relname AND s.table_schema = n.nspname
	    WHERE c.relkind in ('r', 'p')
	    AND n.nspname = $1
	    AND c.relname = $2
	    AND f.attnum > 0
	    ORDER BY f.attnum
	  ),
	  column_constraints AS (
	    SELECT att.attname column_name, tmp.name, tmp.type , tmp.definition
	    FROM (
	      SELECT unnest(con.conkey) AS conkey,
	             pg_get_constraintdef(con.oid, true) AS definition,
	             cls.oid AS relid,
	             con.conname AS name,
	             con.contype AS type
	      FROM   pg_constraint con
	      JOIN   pg_namespace nsp ON nsp.oid = con.connamespace
	      JOIN   pg_class cls ON cls.oid = con.conrelid
	      WHERE  nsp.nspname = $1
	      AND    cls.relname = $2
	      AND    array_length(con.conkey, 1) = 1
	    ) tmp
	    JOIN pg_attribute att ON tmp.conkey = att.attnum AND tmp.relid = att.attrelid
	  ),
	  check_constraints AS (
	    SELECT column_name, name, definition
	    FROM   column_constraints
	    WHERE  type = 'c'
	  )
	SELECT    columns.*, checks.name, checks.definition
	FROM      columns
	LEFT JOIN check_constraints checks USING (column_name);`

	schema, table := splitTableName(table, d.GetDefaultSchema())
	rows, err := d.db.Query(query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := make([]column, 0)
	for rows.Next() {
		col := column{}
		var colName, isNullable, dataType, formattedDataType string
		var colDefault, idGen, checkName, checkDefinition *string
		err = rows.Scan(&colName, &colDefault, &isNullable, &dataType, &formattedDataType, &idGen, &checkName, &checkDefinition)
		if err != nil {
			return nil, err
		}
		col.Name = strings.Trim(colName, `" `)
		if colDefault != nil {
			col.Default = *colDefault
		}
		if colDefault != nil && strings.HasPrefix(*colDefault, "nextval(") {
			col.IsAutoIncrement = true
		}
		col.Nullable = isNullable == "YES"
		col.dataType = dataType
		col.formattedDataType = formattedDataType
		if idGen != nil {
			col.IdentityGeneration = *idGen
		}
		if checkName != nil && checkDefinition != nil {
			col.Check = &columnConstraint{
				definition: normalizeCheckConstraintDefinition(*checkDefinition),
				name:       *checkName,
			}
		}
		cols = append(cols, col)
	}
	return cols, nil
}

func (d *PostgresDatabase) getIndexDefs(table string) ([]string, error) {
	// Exclude indexes that are implicitly created for primary keys or unique constraints or exclusion constraints.
	const query = `WITH
	  exclude_constraints AS (
	    SELECT con.conname AS name
	    FROM   pg_constraint con
	    JOIN   pg_namespace nsp ON nsp.oid = con.connamespace
	    JOIN   pg_class cls ON cls.oid = con.conrelid
	    WHERE  con.contype IN ('p', 'u', 'x')
	    AND    nsp.nspname = $1
	    AND    cls.relname = $2
	  )
	SELECT indexName, indexdef
	FROM   pg_indexes
	WHERE  schemaname = $1
	AND    tablename = $2
	AND    indexName NOT IN (SELECT name FROM exclude_constraints)
	`
	schema, table := splitTableName(table, d.GetDefaultSchema())
	rows, err := d.db.Query(query, schema, table)
	if err != nil {
		return nil, err
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

		indexes = append(indexes, indexdef)
	}
	return indexes, nil
}

func (d *PostgresDatabase) getTableCheckConstraints(tableName string) (map[string]string, error) {
	const query = `SELECT con.conname, pg_get_constraintdef(con.oid, true)
	FROM   pg_constraint con
	JOIN   pg_namespace nsp ON nsp.oid = con.connamespace
	JOIN   pg_class cls ON cls.oid = con.conrelid
	WHERE  con.contype = 'c'
	AND    nsp.nspname = $1
	AND    cls.relname = $2
	AND    array_length(con.conkey, 1) > 1;`

	result := map[string]string{}
	schema, table := splitTableName(tableName, d.GetDefaultSchema())
	rows, err := d.db.Query(query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var constraintName, constraintDef string
		err = rows.Scan(&constraintName, &constraintDef)
		if err != nil {
			return nil, err
		}
		// Normalize constraint definition to handle PostgreSQL's automatic type casting
		normalizedDef := normalizeCheckConstraintDefinition(constraintDef)
		result[constraintName] = normalizedDef
	}

	return result, nil
}

// normalizeCheckConstraintDefinition removes redundant type casts that PostgreSQL automatically adds
// and normalizes the format to make constraint comparison work correctly. Specifically handles:
// - ARRAY['active'::text, 'pending'::text] -> ARRAY['active', 'pending']
// - '[0-9]'::text -> '[0-9]'
// - Uppercase AND/OR -> lowercase and/or
// - Spacing normalization
func normalizeCheckConstraintDefinition(def string) string {
	// pg_get_constraintdef returns "CHECK (...)" so we need to preserve that format
	// but normalize the content inside

	// Remove ::text type casts from string literals in ARRAY expressions
	// This handles the pattern: 'string'::text within ARRAY[...]
	result := regexp.MustCompile(`'([^']*)'::text`).ReplaceAllString(def, "'$1'")

	// Remove ::character varying type casts similarly
	result = regexp.MustCompile(`'([^']*)'::character varying(\([^)]*\))?`).ReplaceAllString(result, "'$1'")

	// Normalize AND/OR to lowercase
	result = regexp.MustCompile(`\bAND\b`).ReplaceAllString(result, "and")
	result = regexp.MustCompile(`\bOR\b`).ReplaceAllString(result, "or")

	// Remove spaces between function names and parentheses
	result = regexp.MustCompile(`ANY\s+\(`).ReplaceAllString(result, "ANY(")
	result = regexp.MustCompile(`ALL\s+\(`).ReplaceAllString(result, "ALL(")

	return result
}

func (d *PostgresDatabase) getUniqueConstraints(tableName string) (map[string]string, error) {
	const query = `SELECT con.conname, pg_get_constraintdef(con.oid)
	FROM   pg_constraint con
	JOIN   pg_namespace nsp ON nsp.oid = con.connamespace
	JOIN   pg_class cls ON cls.oid = con.conrelid
	WHERE  con.contype = 'u'
	AND    nsp.nspname = $1
	AND    cls.relname = $2;`

	result := map[string]string{}
	schema, table := splitTableName(tableName, d.GetDefaultSchema())
	rows, err := d.db.Query(query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var constraintName, constraintDef string
		err = rows.Scan(&constraintName, &constraintDef)
		if err != nil {
			return nil, err
		}

		result[constraintName] = fmt.Sprintf("ALTER TABLE %s.%s ADD CONSTRAINT %s %s",
			escapeSQLName(schema), escapeSQLName(table),
			escapeSQLName(constraintName), constraintDef,
		)
	}

	return result, nil
}

func (d *PostgresDatabase) getExclusionDefs(tableName string) ([]string, error) {
	const query = `SELECT con.conname, pg_get_constraintdef(con.oid, true)
	FROM   pg_constraint con
	JOIN   pg_namespace nsp ON nsp.oid = con.connamespace
	JOIN   pg_class cls ON cls.oid = con.conrelid
	WHERE  con.contype = 'x'
	AND    nsp.nspname = $1
	AND    cls.relname = $2;`

	result := []string{}
	schema, table := splitTableName(tableName, d.GetDefaultSchema())
	rows, err := d.db.Query(query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var constraintName, constraintDef string
		err = rows.Scan(&constraintName, &constraintDef)
		if err != nil {
			return nil, err
		}
		result = append(result, fmt.Sprintf("ALTER TABLE %s.%s ADD CONSTRAINT %s %s", schema, table, constraintName, constraintDef))
	}

	return result, nil
}

func (d *PostgresDatabase) getPrimaryKeyColumns(table string) ([]string, error) {
	const query = `SELECT
	tc.table_schema, tc.constraint_name, tc.table_name, kcu.column_name
FROM
	information_schema.table_constraints AS tc
	JOIN information_schema.key_column_usage AS kcu
		USING (table_schema, table_name, constraint_name)
WHERE constraint_type = 'PRIMARY KEY' AND tc.table_schema=$1 AND tc.table_name=$2 ORDER BY kcu.ordinal_position`
	schema, table := splitTableName(table, d.GetDefaultSchema())
	rows, err := d.db.Query(query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columnNames := make([]string, 0)
	var tableSchema, constraintName, tableName string
	for rows.Next() {
		var columnName string
		err = rows.Scan(&tableSchema, &constraintName, &tableName, &columnName)
		if err != nil {
			return nil, err
		}
		columnNames = append(columnNames, columnName)
	}
	return columnNames, nil
}

func (d *PostgresDatabase) getPrimaryKeyName(table string) (string, error) {
	schema, table := splitTableName(table, d.GetDefaultSchema())
	tableWithSchema := fmt.Sprintf("%s.%s", escapeSQLName(schema), escapeSQLName(table))
	query := fmt.Sprintf(`SELECT
	conname from pg_constraint where conrelid = '%s'::regclass and contype = 'p'`, tableWithSchema)
	rows, err := d.db.Query(query)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var keyName string
	if rows.Next() {
		err = rows.Scan(&keyName)
		if err != nil {
			return "", err
		}
	} else {
		return "", err
	}
	return keyName, nil
}

// refs: https://gist.github.com/PickledDragon/dd41f4e72b428175354d
func (d *PostgresDatabase) getForeignDefs(table string) ([]string, error) {
	const query = `SELECT
		nc.nspname AS constraint_schema,
		n1.nspname AS table_schema,
		c.conname  AS constraint_name,
		r1.relname AS table_name,
		a1.attname AS column_name,
		n2.nspname AS foreign_table_schema,
		r2.relname AS foreign_table_name,
		a2.attname AS foreign_column_name,
		CASE c.confupdtype
			WHEN 'c' THEN 'CASCADE'
			WHEN 'n' THEN 'SET NULL'
			WHEN 'd' THEN 'SET DEFAULT'
			WHEN 'r' THEN 'RESTRICT'
			WHEN 'a' THEN 'NO ACTION'
		END AS foreign_update_rule,
		CASE c.confdeltype
			WHEN 'c' THEN 'CASCADE'
			WHEN 'n' THEN 'SET NULL'
			WHEN 'd' THEN 'SET DEFAULT'
			WHEN 'r' THEN 'RESTRICT'
			WHEN 'a' THEN 'NO ACTION'
		END AS foreign_delete_rule,
		c.condeferrable AS deferrable,
		c.condeferred AS initially_deferred
	FROM pg_constraint      AS c
	INNER JOIN pg_namespace AS nc ON nc.oid = c.connamespace
	INNER JOIN pg_class     AS r1 ON r1.oid = c.conrelid
	INNER JOIN pg_class     AS r2 ON r2.oid = c.confrelid
	INNER JOIN pg_namespace AS n1 ON n1.oid = r1.relnamespace
	INNER JOIN pg_namespace AS n2 ON n2.oid = r2.relnamespace
	CROSS JOIN UNNEST(c.conkey, c.confkey) with ordinality AS k(key1, key2, ordinality)
	INNER JOIN pg_attribute AS a1
		ON  a1.attrelid = c.conrelid
		AND a1.attnum   = k.key1
	INNER JOIN pg_attribute AS a2
		ON  a2.attrelid = c.confrelid
		AND a2.attnum   = k.key2
	WHERE c.contype = 'f' AND n1.nspname = $1 AND r1.relname = $2
	ORDER BY constraint_schema, constraint_name, k.ordinality
	`
	schema, table := splitTableName(table, d.GetDefaultSchema())
	rows, err := d.db.Query(query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type identifier struct {
		schema, name string
	}
	type constraint struct {
		tableSchema, constraintName, tableName, foreignTableSchema, foreignTableName, foreignUpdateRule, foreignDeleteRule string
		columns, foreignColumns                                                                                            []string
		deferrable, initiallyDeferred                                                                                      bool
	}

	constraints := make(map[identifier]constraint)

	for rows.Next() {
		var constraintSchema, tableSchema, constraintName, tableName, columnName, foreignTableSchema, foreignTableName, foreignColumnName, foreignUpdateRule, foreignDeleteRule string
		var deferrable, initiallyDeferred bool
		err = rows.Scan(&constraintSchema, &tableSchema, &constraintName, &tableName, &columnName, &foreignTableSchema, &foreignTableName, &foreignColumnName, &foreignUpdateRule, &foreignDeleteRule, &deferrable, &initiallyDeferred)
		if err != nil {
			return nil, err
		}
		key := identifier{constraintSchema, constraintName}
		if _, exist := constraints[key]; !exist {
			constraints[key] = constraint{
				tableSchema, constraintName, tableName, foreignTableSchema, foreignTableName, foreignUpdateRule, foreignDeleteRule,
				[]string{}, []string{},
				deferrable, initiallyDeferred,
			}
		}
		c := constraints[key]
		c.columns = append(c.columns, columnName)
		c.foreignColumns = append(c.foreignColumns, foreignColumnName)
		constraints[key] = c
	}

	var keys []identifier
	for key := range constraints {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].schema < keys[j].schema || keys[i].name < keys[j].name
	})

	defs := make([]string, 0)
	for _, key := range keys {
		c := constraints[key]
		var escapedColumns []string
		for i := range c.columns {
			escapedColumns = append(escapedColumns, escapeSQLName(c.columns[i]))
		}
		var escapedForeignColumns []string
		for i := range c.foreignColumns {
			escapedForeignColumns = append(escapedForeignColumns, escapeSQLName(c.foreignColumns[i]))
		}
		var constraintOptions string
		if c.deferrable {
			if c.initiallyDeferred {
				constraintOptions = " DEFERRABLE INITIALLY DEFERRED"
			} else {
				constraintOptions = " DEFERRABLE INITIALLY IMMEDIATE"
			}
		}
		def := fmt.Sprintf(
			"ALTER TABLE ONLY %s.%s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s.%s (%s) ON UPDATE %s ON DELETE %s%s",
			escapeSQLName(c.tableSchema), escapeSQLName(c.tableName), escapeSQLName(c.constraintName), strings.Join(escapedColumns, ", "),
			escapeSQLName(c.foreignTableSchema), escapeSQLName(c.foreignTableName), strings.Join(escapedForeignColumns, ", "), c.foreignUpdateRule, c.foreignDeleteRule,
			constraintOptions,
		)
		defs = append(defs, def)
	}
	return defs, nil
}

var (
	policyRolesPrefixRegex = regexp.MustCompile(`^{`)
	policyRolesSuffixRegex = regexp.MustCompile(`}$`)
)

func (d *PostgresDatabase) getPolicyDefs(table string) ([]string, error) {
	const query = "SELECT policyname, permissive, roles, cmd, qual, with_check FROM pg_policies WHERE schemaname = $1 AND tablename = $2;"
	schema, table := splitTableName(table, d.GetDefaultSchema())
	rows, err := d.db.Query(query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	defs := make([]string, 0)
	for rows.Next() {
		var (
			policyName, permissive, roles, cmd string
			using, withCheck                   sql.NullString
		)
		err = rows.Scan(&policyName, &permissive, &roles, &cmd, &using, &withCheck)
		if err != nil {
			return nil, err
		}
		roles = policyRolesPrefixRegex.ReplaceAllString(roles, "")
		roles = policyRolesSuffixRegex.ReplaceAllString(roles, "")
		def := fmt.Sprintf(
			"CREATE POLICY %s ON %s AS %s FOR %s TO %s",
			policyName, table, permissive, cmd, roles,
		)
		if using.Valid {
			def += fmt.Sprintf(" USING (%s)", using.String)
		}
		if withCheck.Valid {
			def += fmt.Sprintf(" WITH CHECK %s", withCheck.String)
		}
		defs = append(defs, def+";")
	}
	return defs, nil
}

func (d *PostgresDatabase) getComments(table string) ([]string, error) {
	schema, table := splitTableName(table, d.GetDefaultSchema())
	var ddls []string

	// Table comments
	tableRows, err := d.db.Query(`
		SELECT obj_description(c.oid)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind in ('r', 'p')
		AND obj_description(c.oid) IS NOT NULL
		AND n.nspname = $1
		AND c.relname = $2
	`, schema, table)
	if err != nil {
		return nil, err
	}
	defer tableRows.Close()
	for tableRows.Next() {
		var comment string
		if err := tableRows.Scan(&comment); err != nil {
			return nil, err
		}
		ddls = append(ddls, fmt.Sprintf("COMMENT ON TABLE \"%s\".\"%s\" IS %s;", schema, table, schemaLib.StringConstant(comment)))
	}

	// Column comments
	columnRows, err := d.db.Query(`
		select
			c.column_name, pgd.description
		from pg_catalog.pg_statio_all_tables as st
		inner join pg_catalog.pg_description pgd on (
			pgd.objoid = st.relid
		)
		inner join information_schema.columns c on (
			pgd.objsubid   = c.ordinal_position and
			c.table_schema = st.schemaname and
			c.table_name   = st.relname and
			c.table_schema = $1 and
			st.relname = $2
		);
	`, schema, table)
	if err != nil {
		return nil, err
	}
	defer columnRows.Close()
	for columnRows.Next() {
		var columnName, comment string
		if err := columnRows.Scan(&columnName, &comment); err != nil {
			return nil, err
		}
		ddls = append(ddls, fmt.Sprintf("COMMENT ON COLUMN \"%s\".\"%s\".\"%s\" IS %s;", schema, table, columnName, schemaLib.StringConstant(comment)))
	}

	return ddls, nil
}

func (d *PostgresDatabase) DB() *sql.DB {
	return d.db
}

func (d *PostgresDatabase) Close() error {
	return d.db.Close()
}

func (d *PostgresDatabase) GetDefaultSchema() string {
	if d.defaultSchema != nil {
		return *d.defaultSchema
	}

	var defaultSchema string
	query := "SELECT current_schema();"

	err := d.db.QueryRow(query).Scan(&defaultSchema)
	if err != nil {
		return ""
	}

	d.defaultSchema = &defaultSchema

	return defaultSchema
}

func postgresBuildDSN(config database.Config) string {
	user := config.User
	password := config.Password
	database := config.DbName
	host := ""
	if config.Socket == "" {
		host = fmt.Sprintf("%s:%d", config.Host, config.Port)
	} else {
		host = config.Socket
	}

	var options []string
	// Use config.SslMode if set, otherwise check environment variable
	if config.SslMode != "" {
		options = append(options, fmt.Sprintf("sslmode=%s", config.SslMode))
	} else if sslmode, ok := os.LookupEnv("PGSSLMODE"); ok {
		options = append(options, fmt.Sprintf("sslmode=%s", sslmode))
	}

	if sslrootcert, ok := os.LookupEnv("PGSSLROOTCERT"); ok { // TODO: have this in database.Config, or standardize config with DSN?
		options = append(options, fmt.Sprintf("sslrootcert=%s", sslrootcert))
	}

	if sslcert, ok := os.LookupEnv("PGSSLCERT"); ok { // TODO: have this in database.Config, or standardize config with DSN?
		options = append(options, fmt.Sprintf("sslcert=%s", sslcert))
	}

	if sslkey, ok := os.LookupEnv("PGSSLKEY"); ok { // TODO: have this in database.Config, or standardize config with DSN?
		options = append(options, fmt.Sprintf("sslkey=%s", sslkey))
	}

	// `QueryEscape` instead of `PathEscape` so that colon can be escaped.
	return fmt.Sprintf("postgres://%s:%s@%s/%s?%s", url.QueryEscape(user), url.QueryEscape(password), host, database, strings.Join(options, "&"))
}

func escapeSQLName(name string) string {
	return fmt.Sprintf("\"%s\"", name)
}

func splitTableName(table string, defaultSchema string) (string, string) {
	schema := defaultSchema
	schemaTable := strings.SplitN(table, ".", 2)
	if len(schemaTable) == 2 {
		schema = schemaTable[0]
		table = schemaTable[1]
	}
	return schema, table
}

// postgresTablePrivilegeList contains all possible table privileges for PostgreSQL
// Ordered alphabetically as PostgreSQL returns them
var postgresTablePrivilegeList = []string{
	"DELETE",
	"INSERT",
	"REFERENCES",
	"SELECT",
	"TRIGGER",
	"TRUNCATE",
	"UPDATE",
}

// normalizePrivileges converts a privilege list to "ALL PRIVILEGES" if it contains all table privileges
func normalizePrivileges(privileges string) string {
	privList := strings.Split(privileges, ", ")
	if len(privList) != len(postgresTablePrivilegeList) {
		return privileges
	}

	privMap := make(map[string]bool)
	for _, priv := range privList {
		privMap[priv] = true
	}

	for _, requiredPriv := range postgresTablePrivilegeList {
		if !privMap[requiredPriv] {
			return privileges
		}
	}

	return "ALL PRIVILEGES"
}

func (d *PostgresDatabase) getPrivilegeDefs(table string) ([]string, error) {
	// If no roles are specified to include, don't query privileges at all
	if len(d.generatorConfig.ManagedRoles) == 0 {
		return []string{}, nil
	}

	schema, tableName := splitTableName(table, d.GetDefaultSchema())

	rolePlaceholders := make([]string, len(d.generatorConfig.ManagedRoles))
	queryArgs := []interface{}{schema, tableName}
	for i, role := range d.generatorConfig.ManagedRoles {
		rolePlaceholders[i] = fmt.Sprintf("$%d", i+3)
		queryArgs = append(queryArgs, role)
	}
	roleFilter := strings.Join(rolePlaceholders, ", ")

	query := fmt.Sprintf(`
		SELECT
			grantee,
			string_agg(privilege_type, ', ' ORDER BY privilege_type) as privileges
		FROM information_schema.table_privileges
		WHERE table_schema = $1
		AND table_name = $2
		AND grantee IN (%s)
		AND grantee != (
			SELECT tableowner FROM pg_tables
			WHERE schemaname = $1 AND tablename = $2
		)
		GROUP BY grantee
		ORDER BY grantee
	`, roleFilter)

	rows, err := d.db.Query(query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to query privileges for table %s.%s: %w", schema, tableName, err)
	}
	defer rows.Close()

	var privilegeDefs []string
	for rows.Next() {
		var grantee, privileges string
		if err := rows.Scan(&grantee, &privileges); err != nil {
			return nil, fmt.Errorf("failed to scan privilege row: %w", err)
		}

		privileges = normalizePrivileges(privileges)

		escapedGrantee := grantee
		if grantee != "PUBLIC" {
			// PUBLIC is a special keyword and should not be quoted
			escapedGrantee = escapeSQLName(grantee)
		}

		grant := fmt.Sprintf("GRANT %s ON TABLE %s TO %s", privileges, table, escapedGrantee)
		privilegeDefs = append(privilegeDefs, grant)
	}

	return privilegeDefs, nil
}
