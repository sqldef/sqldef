package postgres

import (
	"cmp"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"

	_ "github.com/lib/pq"
	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/parser"
	schemaLib "github.com/sqldef/sqldef/v3/schema"
	"github.com/sqldef/sqldef/v3/util"
)

type (
	Ident         = database.Ident
	QualifiedName = database.QualifiedName
)

var (
	NewIdentWithQuoteDetected = database.NewIdentWithQuoteDetected
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
	// Sync TargetSchema to d.config for backward compatibility
	// (other methods read from d.config.TargetSchema)
	d.config.TargetSchema = config.TargetSchema
}

func (d *PostgresDatabase) GetGeneratorConfig() database.GeneratorConfig {
	return d.generatorConfig
}

func (d *PostgresDatabase) GetTransactionQueries() database.TransactionQueries {
	return database.TransactionQueries{
		Begin:    "BEGIN",
		Commit:   "COMMIT",
		Rollback: "ROLLBACK",
	}
}

func (d *PostgresDatabase) GetConfig() database.Config {
	return d.config
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

	domainDDLs, err := d.domains()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, domainDDLs...)

	functionDDLs, err := d.functions()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, functionDDLs...)

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

	triggerDDLs, err := d.triggers()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, triggerDDLs...)

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
		order by n.nspname asc, relname asc;
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
		definition = strings.ReplaceAll(definition, "\n", " ")
		definition = suffixSemicolon.ReplaceAllString(definition, "")
		definition = spaces.ReplaceAllString(definition, " ")
		// Normalize PostgreSQL-specific syntax for generic parser compatibility
		definition = normalizeDatePartToExtract(definition)
		ddls = append(
			ddls, fmt.Sprintf(
				"CREATE VIEW %s.%s AS %s;", d.escapeIdentifier(schema), d.escapeIdentifier(name), definition,
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
		definition = strings.ReplaceAll(definition, "\n", " ")
		definition = suffixSemicolon.ReplaceAllString(definition, "")
		definition = spaces.ReplaceAllString(definition, " ")
		// Normalize PostgreSQL-specific syntax for generic parser compatibility
		definition = normalizeDatePartToExtract(definition)
		ddls = append(
			ddls, fmt.Sprintf(
				"CREATE MATERIALIZED VIEW %s.%s AS %s;", d.escapeIdentifier(schema), d.escapeIdentifier(name), definition,
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
	// Use quote_literal() to properly escape enum labels containing special characters
	// (single quotes, spaces, etc.) and preserve the order with enumsortorder
	rows, err := d.db.Query(`
		SELECT n.nspname AS type_schema,
		       t.typname,
		       string_agg(quote_literal(e.enumlabel), ', ' ORDER BY e.enumsortorder)
		FROM pg_enum e
		JOIN pg_type t ON e.enumtypid = t.oid
		INNER JOIN pg_catalog.pg_namespace n ON t.typnamespace = n.oid
		WHERE NOT EXISTS (SELECT * FROM pg_depend d WHERE d.objid = t.oid AND d.deptype = 'e')
		GROUP BY n.nspname, t.typname;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ddls []string
	for rows.Next() {
		var typeSchema, typeName, enumLabels string
		if err := rows.Scan(&typeSchema, &typeName, &enumLabels); err != nil {
			return nil, err
		}
		if d.config.TargetSchema != nil && !slices.Contains(d.config.TargetSchema, typeSchema) {
			continue
		}
		ddls = append(
			ddls, fmt.Sprintf(
				"CREATE TYPE %s.%s AS ENUM (%s);", d.escapeIdentifier(typeSchema), d.escapeIdentifier(typeName), enumLabels,
			),
		)
	}
	return ddls, nil
}

func (d *PostgresDatabase) domains() ([]string, error) {
	rows, err := d.db.Query(`
		SELECT n.nspname AS domain_schema,
		       t.typname AS domain_name,
		       pg_catalog.format_type(t.typbasetype, t.typtypmod) AS data_type,
		       t.typdefault AS default_value,
		       t.typnotnull AS not_null,
		       c.collname AS collation
		FROM pg_catalog.pg_type t
		INNER JOIN pg_catalog.pg_namespace n ON t.typnamespace = n.oid
		LEFT JOIN pg_catalog.pg_collation c ON t.typcollation = c.oid AND t.typcollation <> 0
		WHERE t.typtype = 'd'
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = t.oid AND d.deptype = 'e')
		ORDER BY n.nspname, t.typname;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type domainInfo struct {
		schema, name, dataType  string
		defaultValue, collation sql.NullString
		notNull                 bool
	}

	var domains []domainInfo
	for rows.Next() {
		var di domainInfo
		if err := rows.Scan(&di.schema, &di.name, &di.dataType, &di.defaultValue, &di.notNull, &di.collation); err != nil {
			return nil, err
		}
		if d.config.TargetSchema != nil && !slices.Contains(d.config.TargetSchema, di.schema) {
			continue
		}
		domains = append(domains, di)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Now fetch constraints for domains, applying the same TargetSchema filter
	constraintRows, err := d.db.Query(`
		SELECT n.nspname AS domain_schema,
		       t.typname AS domain_name,
		       con.conname AS constraint_name,
		       pg_catalog.pg_get_constraintdef(con.oid, true) AS constraint_def
		FROM pg_catalog.pg_constraint con
		INNER JOIN pg_catalog.pg_type t ON con.contypid = t.oid
		INNER JOIN pg_catalog.pg_namespace n ON t.typnamespace = n.oid
		WHERE t.typtype = 'd'
		  AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		ORDER BY n.nspname, t.typname, con.conname;
	`)
	if err != nil {
		return nil, err
	}
	defer constraintRows.Close()

	// Map of domain (schema.name) to list of constraint definitions
	constraintsMap := make(map[string][]string)
	for constraintRows.Next() {
		var domainSchema, domainName, constraintName string
		var constraintDef string
		if err := constraintRows.Scan(&domainSchema, &domainName, &constraintName, &constraintDef); err != nil {
			return nil, err
		}
		// Apply TargetSchema filter to constraints as well
		if d.config.TargetSchema != nil && !slices.Contains(d.config.TargetSchema, domainSchema) {
			continue
		}
		key := domainSchema + "." + domainName
		constraintsMap[key] = append(constraintsMap[key], constraintDef)
	}
	if err := constraintRows.Err(); err != nil {
		return nil, err
	}

	// Build CREATE DOMAIN statements
	var ddls []string
	for _, di := range domains {
		ddl := fmt.Sprintf("CREATE DOMAIN %s.%s AS %s", d.escapeIdentifier(di.schema), d.escapeIdentifier(di.name), di.dataType)

		if di.collation.Valid && di.collation.String != "" && di.collation.String != "default" {
			ddl += fmt.Sprintf(" COLLATE %s", d.escapeIdentifier(di.collation.String))
		}

		if di.defaultValue.Valid {
			ddl += fmt.Sprintf(" DEFAULT %s", di.defaultValue.String)
		}

		if di.notNull {
			ddl += " NOT NULL"
		}

		// Add all CHECK constraints
		key := di.schema + "." + di.name
		if constraints, ok := constraintsMap[key]; ok {
			for _, constraintDef := range constraints {
				ddl += fmt.Sprintf(" %s", constraintDef)
			}
		}

		ddl += ";"
		ddls = append(ddls, ddl)
	}
	return ddls, nil
}

// functions fetches user-defined functions from the database
func (d *PostgresDatabase) functions() ([]string, error) {
	// Query to get user-defined functions (excluding system functions and extension functions)
	// We use pg_get_functiondef to get the complete function definition
	rows, err := d.db.Query(`
		SELECT n.nspname AS func_schema,
		       p.proname AS func_name,
		       pg_get_functiondef(p.oid) AS func_def
		FROM pg_catalog.pg_proc p
		INNER JOIN pg_catalog.pg_namespace n ON p.pronamespace = n.oid
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND p.prokind = 'f'
		  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = p.oid AND d.deptype = 'e')
		ORDER BY n.nspname, p.proname;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ddls []string
	for rows.Next() {
		var funcSchema, funcName, funcDef string
		if err := rows.Scan(&funcSchema, &funcName, &funcDef); err != nil {
			return nil, err
		}
		if d.config.TargetSchema != nil && !slices.Contains(d.config.TargetSchema, funcSchema) {
			continue
		}
		// pg_get_functiondef returns the complete CREATE FUNCTION statement
		// We just need to ensure it ends with a semicolon
		funcDef = strings.TrimSpace(funcDef)
		if !strings.HasSuffix(funcDef, ";") {
			funcDef += ";"
		}
		ddls = append(ddls, funcDef)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ddls, nil
}

// triggers fetches user-defined triggers from the database
func (d *PostgresDatabase) triggers() ([]string, error) {
	// Query to get user-defined triggers (excluding internal triggers and extension triggers)
	// We use pg_get_triggerdef to get the complete trigger definition
	rows, err := d.db.Query(`
		SELECT n.nspname AS trigger_schema,
		       c.relname AS table_name,
		       t.tgname AS trigger_name,
		       pg_get_triggerdef(t.oid) AS trigger_def
		FROM pg_catalog.pg_trigger t
		INNER JOIN pg_catalog.pg_class c ON t.tgrelid = c.oid
		INNER JOIN pg_catalog.pg_namespace n ON c.relnamespace = n.oid
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND NOT t.tgisinternal
		  AND NOT EXISTS (SELECT 1 FROM pg_depend d WHERE d.objid = t.oid AND d.deptype = 'e')
		ORDER BY n.nspname, c.relname, t.tgname;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ddls []string
	for rows.Next() {
		var triggerSchema, tableName, triggerName, triggerDef string
		if err := rows.Scan(&triggerSchema, &tableName, &triggerName, &triggerDef); err != nil {
			return nil, err
		}
		if d.config.TargetSchema != nil && !slices.Contains(d.config.TargetSchema, triggerSchema) {
			continue
		}
		// pg_get_triggerdef returns the complete CREATE TRIGGER statement
		// We just need to ensure it ends with a semicolon
		triggerDef = strings.TrimSpace(triggerDef)
		if !strings.HasSuffix(triggerDef, ";") {
			triggerDef += ";"
		}
		ddls = append(ddls, triggerDef)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ddls, nil
}

// CheckConstraint holds a CHECK constraint's name and definition.
type CheckConstraint struct {
	Name       Ident
	Definition string
}

type TableDDLComponents struct {
	TableName         string
	Columns           []column
	PrimaryKeyName    Ident
	PrimaryKeyCols    []string
	IndexDefs         []string
	ForeignDefs       []string
	ExclusionDefs     []string
	PolicyDefs        []string
	Comments          []string
	CheckConstraints  []CheckConstraint
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
	return d.buildExportTableDDL(components), nil
}

func (d *PostgresDatabase) buildExportTableDDL(components TableDDLComponents) string {
	var queryBuilder strings.Builder
	schema, table := splitTableName(components.TableName, components.DefaultSchema)
	fmt.Fprintf(&queryBuilder, "CREATE TABLE %s.%s (", d.escapeIdentifier(schema), d.escapeIdentifier(table))
	for i, col := range components.Columns {
		if i > 0 {
			fmt.Fprint(&queryBuilder, ",")
		}
		fmt.Fprint(&queryBuilder, "\n"+indent)
		fmt.Fprintf(&queryBuilder, "%s %s", d.escapeIdentifier(col.Name), d.escapeDataTypeName(col.GetDataType()))
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
			fmt.Fprintf(&queryBuilder, " CONSTRAINT %s %s", d.escapeConstraintName(col.Check.name), col.Check.definition)
		}
	}
	if len(components.PrimaryKeyCols) > 0 {
		fmt.Fprint(&queryBuilder, ",\n"+indent)
		fmt.Fprintf(&queryBuilder, "CONSTRAINT %s PRIMARY KEY (\"%s\")", d.escapeConstraintName(components.PrimaryKeyName), strings.Join(components.PrimaryKeyCols, "\", \""))
	}

	for _, check := range components.CheckConstraints {
		fmt.Fprint(&queryBuilder, ",\n"+indent)
		fmt.Fprintf(&queryBuilder, "CONSTRAINT %s %s", d.escapeConstraintName(check.Name), check.Definition)
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

	for _, constraintDef := range util.CanonicalMapIter(components.UniqueConstraints) {
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
	name       Ident
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
	case "time without time zone":
		return strings.TrimSuffix(c.formattedDataType, " without time zone")
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
	      -- Domain types ('d') and enum types ('e'): return the type name with schema prefix
	      WHEN t.typtype IN ('d', 'e') THEN
	        CASE
	          WHEN tn.nspname = 'public' THEN t.typname
	          ELSE tn.nspname || '.' || t.typname
	        END
	      WHEN s.data_type IN ('ARRAY', 'USER-DEFINED') THEN format_type(f.atttypid, f.atttypmod)
	      ELSE s.data_type
	      END,
	      -- formattedDataType: also return type name for domain and enum types
	      CASE
	      WHEN t.typtype IN ('d', 'e') THEN
	        CASE
	          WHEN tn.nspname = 'public' THEN t.typname
	          ELSE tn.nspname || '.' || t.typname
	        END
	      ELSE format_type(f.atttypid, f.atttypmod)
	      END,
	      s.identity_generation
	    FROM pg_attribute f
	    JOIN pg_class c ON c.oid = f.attrelid JOIN pg_type t ON t.oid = f.atttypid
	    LEFT JOIN pg_namespace tn ON tn.oid = t.typnamespace
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
			// Normalize type casts for generic parser compatibility
			normalizedDef := normalizePostgresTypeCasts(*checkDefinition)
			col.Check = &columnConstraint{
				definition: normalizedDef,
				name:       NewIdentWithQuoteDetected(*checkName),
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
	ORDER BY indexdef
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

// normalizeDatePartToExtract converts PostgreSQL's date_part() function calls to EXTRACT() expressions
// PostgreSQL stores EXTRACT(field FROM source) as date_part('field'::text, source) internally.
// The generic parser handles EXTRACT natively but parses date_part as a generic function call,
// so we need to convert it back to EXTRACT for idempotent schema comparisons.
func normalizeDatePartToExtract(sql string) string {
	// Match date_part('field'::text, ...) or date_part('field', ...)
	// The field can be: year, month, day, hour, minute, second, epoch, dow, doy, week, quarter, etc.
	// We need to handle nested function calls and complex expressions as the second argument

	// Use a regex that captures the field name and finds the matching closing parenthesis
	// Pattern: date_part('field'::text, source) or date_part('field', source)
	re := regexp.MustCompile(`date_part\('([^']+)'(?:::text)?,\s*([^)]+)\)`)

	// Replace with EXTRACT(field FROM source)
	sql = re.ReplaceAllStringFunc(sql, func(match string) string {
		submatches := re.FindStringSubmatch(match)
		if len(submatches) == 3 {
			field := submatches[1]
			source := strings.TrimSpace(submatches[2])
			return fmt.Sprintf("EXTRACT(%s FROM %s)", field, source)
		}
		return match
	})

	return sql
}

// normalizePostgresTypeCasts normalizes PostgreSQL's verbose type cast syntax for generic parser compatibility.
// The generic parser has difficulty parsing ::time casts, so we convert them to TypedLiteral format (time 'value').
func normalizePostgresTypeCasts(sql string) string {
	// Convert ::time without time zone casts to typed literal format
	// PostgreSQL returns: '09:00:00'::time without time zone
	// Convert to: time '09:00:00' (which the generic parser can handle)
	re := regexp.MustCompile(`'([^']+)'::time without time zone`)
	sql = re.ReplaceAllString(sql, "time '$1'")

	re = regexp.MustCompile(`'([^']+)'::timestamp without time zone`)
	sql = re.ReplaceAllString(sql, "timestamp '$1'")

	// For with time zone variants, keep as cast since TypedLiteral doesn't support them well
	sql = strings.ReplaceAll(sql, "::timestamp with time zone", "::timestamptz")
	sql = strings.ReplaceAll(sql, "::time with time zone", "::timetz")

	return sql
}

func (d *PostgresDatabase) getTableCheckConstraints(tableName string) ([]CheckConstraint, error) {
	const query = `SELECT con.conname, pg_get_constraintdef(con.oid, true)
	FROM   pg_constraint con
	JOIN   pg_namespace nsp ON nsp.oid = con.connamespace
	JOIN   pg_class cls ON cls.oid = con.conrelid
	WHERE  con.contype = 'c'
	AND    nsp.nspname = $1
	AND    cls.relname = $2
	AND    array_length(con.conkey, 1) > 1;`

	var result []CheckConstraint
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
		// Normalize type casts for generic parser compatibility
		// PostgreSQL returns "::time without time zone" but the generic parser expects "::time"
		constraintDef = normalizePostgresTypeCasts(constraintDef)
		result = append(result, CheckConstraint{
			Name:       NewIdentWithQuoteDetected(constraintName),
			Definition: constraintDef,
		})
	}

	return result, nil
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

func (d *PostgresDatabase) getPrimaryKeyName(table string) (Ident, error) {
	schema, table := splitTableName(table, d.GetDefaultSchema())
	tableWithSchema := fmt.Sprintf("%s.%s", escapeSQLName(schema), escapeSQLName(table))
	query := fmt.Sprintf(`
		SELECT conname
		FROM pg_constraint
		WHERE conrelid = '%s'::regclass AND contype = 'p'
	`, tableWithSchema)
	rows, err := d.db.Query(query)
	if err != nil {
		return Ident{}, err
	}
	defer rows.Close()

	var keyName string
	if rows.Next() {
		err = rows.Scan(&keyName)
		if err != nil {
			return Ident{}, err
		}
	} else {
		return Ident{}, err
	}
	return NewIdentWithQuoteDetected(keyName), nil
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
	slices.SortFunc(keys, func(a, b identifier) int {
		if c := cmp.Compare(a.schema, b.schema); c != 0 {
			return c
		}
		return cmp.Compare(a.name, b.name)
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
	const query = `
		SELECT policyname, permissive, roles, cmd, qual, with_check
		FROM pg_policies
		WHERE schemaname = $1 AND tablename = $2
	`
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
		ddls = append(ddls, fmt.Sprintf("COMMENT ON TABLE %s.%s IS %s;", d.escapeIdentifier(schema), d.escapeIdentifier(table), schemaLib.StringConstant(comment)))
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
		ddls = append(ddls, fmt.Sprintf("COMMENT ON COLUMN %s.%s.%s IS %s;", d.escapeIdentifier(schema), d.escapeIdentifier(table), d.escapeIdentifier(columnName), schemaLib.StringConstant(comment)))
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
	query := `
		SELECT current_schema()
	`

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
	var options []string

	if config.Socket == "" {
		host = fmt.Sprintf("%s:%d", config.Host, config.Port)
	} else {
		// We want to use either:
		// - postgres://user:@%2Fvar%2Frun%2Fpostgresql/dbname
		// - postgres://user:@/dbname?host=/var/run/postgresql
		// As the first form would be rejected by the URL parser,
		// we resort to the second form.
		options = append(options, fmt.Sprintf("host=%s", config.Socket))
	}

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

// escapeConstraintName quotes a constraint name for DDL output.
// In legacy mode (LegacyIgnoreQuotes=true): don't quote (original behavior).
// In quote-aware mode (LegacyIgnoreQuotes=false): respect the Ident's Quoted field.
func (d *PostgresDatabase) escapeConstraintName(ident Ident) string {
	if d.generatorConfig.LegacyIgnoreQuotes {
		return ident.Name
	}
	if ident.Quoted {
		return escapeSQLName(ident.Name)
	}
	return ident.Name
}

// escapeIdentifier quotes an identifier for DDL output.
// In legacy mode: always quote to preserve exact case.
// In quote-aware mode: use case detection and keyword check.
func (d *PostgresDatabase) escapeIdentifier(name string) string {
	if d.generatorConfig.LegacyIgnoreQuotes {
		return escapeSQLName(name)
	}
	// Quote-aware mode: quote if name has uppercase letters or is a keyword
	if strings.ToLower(name) != name || parser.IsKeyword(name) {
		return escapeSQLName(name)
	}
	return name
}

// escapeDataTypeName quotes a data type name appropriately.
// Handles array types and schema-qualified names.
// Uses case detection to determine if quoting is needed:
//   - All lowercase names (built-in types or unquoted custom types) are not quoted
//   - Names with uppercase letters (quoted custom types) are quoted to preserve case
func (d *PostgresDatabase) escapeDataTypeName(typeName string) string {
	// Handle array types: preserve the [] suffix
	arraySuffix := ""
	if strings.HasSuffix(typeName, "[]") {
		arraySuffix = "[]"
		typeName = strings.TrimSuffix(typeName, "[]")
	}

	// If already quoted (from format_type()), return as-is
	if strings.HasPrefix(typeName, "\"") && strings.HasSuffix(typeName, "\"") {
		return typeName + arraySuffix
	}

	// Handle schema-qualified types (e.g., "public.my_type")
	if idx := strings.Index(typeName, "."); idx > 0 {
		schema := typeName[:idx]
		baseType := typeName[idx+1:]
		// Quote each part only if it has uppercase letters
		escapedSchema := schema
		if strings.ToLower(schema) != schema {
			escapedSchema = d.escapeIdentifier(schema)
		}
		escapedType := baseType
		if strings.ToLower(baseType) != baseType {
			escapedType = d.escapeIdentifier(baseType)
		}
		return escapedSchema + "." + escapedType + arraySuffix
	}

	// For simple type names, use case detection:
	// - All lowercase: don't quote (built-in types like "integer", or custom types created without quotes)
	// - Has uppercase: quote to preserve case (custom types created with quotes like "UserStatus")
	if strings.ToLower(typeName) == typeName {
		return typeName + arraySuffix
	}

	return d.escapeIdentifier(typeName) + arraySuffix
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

func (d *PostgresDatabase) getPrivilegeDefs(table string) ([]string, error) {
	// If no roles are specified to include, don't query privileges at all
	if len(d.generatorConfig.ManagedRoles) == 0 {
		return []string{}, nil
	}

	schema, tableName := splitTableName(table, d.GetDefaultSchema())

	rolePlaceholders := make([]string, len(d.generatorConfig.ManagedRoles))
	queryArgs := []any{schema, tableName}
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
