package mssql

import (
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	_ "github.com/denisenkom/go-mssqldb"
	"github.com/k0kubun/sqldef/adapter"
)

const indent = "    "

// names from: https://docs.microsoft.com/en-us/sql/t-sql/language-elements/reserved-keywords-transact-sql?view=sql-server-ver15
var reservedKeywordsArray = [...]string{
	// "sql server",
	"ADD",
	"EXTERNAL",
	"PROCEDURE",
	"ALL",
	"FETCH",
	"PUBLIC",
	"ALTER",
	"FILE",
	"RAISERROR",
	"AND",
	"FILLFACTOR",
	"READ",
	"ANY",
	"FOR",
	"READTEXT",
	"AS",
	"FOREIGN",
	"RECONFIGURE",
	"ASC",
	"FREETEXT",
	"REFERENCES",
	"AUTHORIZATION",
	"FREETEXTTABLE",
	"REPLICATION",
	"BACKUP",
	"FROM",
	"RESTORE",
	"BEGIN",
	"FULL",
	"RESTRICT",
	"BETWEEN",
	"FUNCTION",
	"RETURN",
	"BREAK",
	"GOTO",
	"REVERT",
	"BROWSE",
	"GRANT",
	"REVOKE",
	"BULK",
	"GROUP",
	"RIGHT",
	"BY",
	"HAVING",
	"ROLLBACK",
	"CASCADE",
	"HOLDLOCK",
	"ROWCOUNT",
	"CASE",
	"IDENTITY",
	"ROWGUIDCOL",
	"CHECK",
	"IDENTITY_INSERT",
	"RULE",
	"CHECKPOINT",
	"IDENTITYCOL",
	"SAVE",
	"CLOSE",
	"IF",
	"SCHEMA",
	"CLUSTERED",
	"IN",
	"SECURITYAUDIT",
	"COALESCE",
	"INDEX",
	"SELECT",
	"COLLATE",
	"INNER",
	"SEMANTICKEYPHRASETABLE",
	"COLUMN",
	"INSERT",
	"SEMANTICSIMILARITYDETAILSTABLE",
	"COMMIT",
	"INTERSECT",
	"SEMANTICSIMILARITYTABLE",
	"COMPUTE",
	"INTO",
	"SESSION_USER",
	"CONSTRAINT",
	"IS",
	"SET",
	"CONTAINS",
	"JOIN",
	"SETUSER",
	"CONTAINSTABLE",
	"KEY",
	"SHUTDOWN",
	"CONTINUE",
	"KILL",
	"SOME",
	"CONVERT",
	"LEFT",
	"STATISTICS",
	"CREATE",
	"LIKE",
	"SYSTEM_USER",
	"CROSS",
	"LINENO",
	"TABLE",
	"CURRENT",
	"LOAD",
	"TABLESAMPLE",
	"CURRENT_DATE",
	"MERGE",
	"TEXTSIZE",
	"CURRENT_TIME",
	"NATIONAL",
	"THEN",
	"CURRENT_TIMESTAMP",
	"NOCHECK",
	"TO",
	"CURRENT_USER",
	"NONCLUSTERED",
	"TOP",
	"CURSOR",
	"NOT",
	"TRAN",
	"DATABASE",
	"NULL",
	"TRANSACTION",
	"DBCC",
	"NULLIF",
	"TRIGGER",
	"DEALLOCATE",
	"OF",
	"TRUNCATE",
	"DECLARE",
	"OFF",
	"TRY_CONVERT",
	"DEFAULT",
	"OFFSETS",
	"TSEQUAL",
	"DELETE",
	"ON",
	"UNION",
	"DENY",
	"OPEN",
	"UNIQUE",
	"DESC",
	"OPENDATASOURCE",
	"UNPIVOT",
	"DISK",
	"OPENQUERY",
	"UPDATE",
	"DISTINCT",
	"OPENROWSET",
	"UPDATETEXT",
	"DISTRIBUTED",
	"OPENXML",
	"USE",
	"DOUBLE",
	"OPTION",
	"USER",
	"DROP",
	"OR",
	"VALUES",
	"DUMP",
	"ORDER",
	"VARYING",
	"ELSE",
	"OUTER",
	"VIEW",
	"END",
	"OVER",
	"WAITFOR",
	"ERRLVL",
	"PERCENT",
	"WHEN",
	"ESCAPE",
	"PIVOT",
	"WHERE",
	"EXCEPT",
	"PLAN",
	"WHILE",
	"EXEC",
	"PRECISION",
	"WITH",
	"EXECUTE",
	"PRIMARY",
	"WITHIN GROUP",
	"EXISTS",
	"PRINT",
	"WRITETEXT",
	"EXIT",
	"PROC",
	// "odbc",
	"ABSOLUTE",
	"EXEC",
	"OVERLAPS",
	"ACTION",
	"EXECUTE",
	"PAD",
	"ADA",
	"EXISTS",
	"PARTIAL",
	"ADD",
	"EXTERNAL",
	"PASCAL",
	"ALL",
	"EXTRACT",
	"POSITION",
	"ALLOCATE",
	"FALSE",
	"PRECISION",
	"ALTER",
	"FETCH",
	"PREPARE",
	"AND",
	"FIRST",
	"PRESERVE",
	"ANY",
	"FLOAT",
	"PRIMARY",
	"ARE",
	"FOR",
	"PRIOR",
	"AS",
	"FOREIGN",
	"PRIVILEGES",
	"ASC",
	"FORTRAN",
	"PROCEDURE",
	"ASSERTION",
	"FOUND",
	"PUBLIC",
	"AT",
	"FROM",
	"READ",
	"AUTHORIZATION",
	"FULL",
	"REAL",
	"AVG",
	"GET",
	"REFERENCES",
	"BEGIN",
	"GLOBAL",
	"RELATIVE",
	"BETWEEN",
	"GO",
	"RESTRICT",
	"BIT",
	"GOTO",
	"REVOKE",
	"BIT_LENGTH",
	"GRANT",
	"RIGHT",
	"BOTH",
	"GROUP",
	"ROLLBACK",
	"BY",
	"HAVING",
	"ROWS",
	"CASCADE",
	"HOUR",
	"SCHEMA",
	"CASCADED",
	"IDENTITY",
	"SCROLL",
	"CASE",
	"IMMEDIATE",
	"SECOND",
	"CAST",
	"IN",
	"SECTION",
	"CATALOG",
	"INCLUDE",
	"SELECT",
	"CHAR",
	"INDEX",
	"SESSION",
	"CHAR_LENGTH",
	"INDICATOR",
	"SESSION_USER",
	"CHARACTER",
	"INITIALLY",
	"SET",
	"CHARACTER_LENGTH",
	"INNER",
	"SIZE",
	"CHECK",
	"INPUT",
	"SMALLINT",
	"CLOSE",
	"INSENSITIVE",
	"SOME",
	"COALESCE",
	"INSERT",
	"SPACE",
	"COLLATE",
	"INT",
	"SQL",
	"COLLATION",
	"INTEGER",
	"SQLCA",
	"COLUMN",
	"INTERSECT",
	"SQLCODE",
	"COMMIT",
	"INTERVAL",
	"SQLERROR",
	"CONNECT",
	"INTO",
	"SQLSTATE",
	"CONNECTION",
	"IS",
	"SQLWARNING",
	"CONSTRAINT",
	"ISOLATION",
	"SUBSTRING",
	"CONSTRAINTS",
	"JOIN",
	"SUM",
	"CONTINUE",
	"KEY",
	"SYSTEM_USER",
	"CONVERT",
	"LANGUAGE",
	"TABLE",
	"CORRESPONDING",
	"LAST",
	"TEMPORARY",
	"COUNT",
	"LEADING",
	"THEN",
	"CREATE",
	"LEFT",
	"TIME",
	"CROSS",
	"LEVEL",
	"TIMESTAMP",
	"CURRENT",
	"LIKE",
	"TIMEZONE_HOUR",
	"CURRENT_DATE",
	"LOCAL",
	"TIMEZONE_MINUTE",
	"CURRENT_TIME",
	"LOWER",
	"TO",
	"CURRENT_TIMESTAMP",
	"MATCH",
	"TRAILING",
	"CURRENT_USER",
	"MAX",
	"TRANSACTION",
	"CURSOR",
	"MIN",
	"TRANSLATE",
	"DATE",
	"MINUTE",
	"TRANSLATION",
	"DAY",
	"MODULE",
	"TRIM",
	"DEALLOCATE",
	"MONTH",
	"TRUE",
	"DEC",
	"NAMES",
	"UNION",
	"DECIMAL",
	"NATIONAL",
	"UNIQUE",
	"DECLARE",
	"NATURAL",
	"UNKNOWN",
	"DEFAULT",
	"NCHAR",
	"UPDATE",
	"DEFERRABLE",
	"NEXT",
	"UPPER",
	"DEFERRED",
	"NO",
	"USAGE",
	"DELETE",
	"NONE",
	"USER",
	"DESC",
	"NOT",
	"USING",
	"DESCRIBE",
	"NULL",
	"VALUE",
	"DESCRIPTOR",
	"NULLIF",
	"VALUES",
	"DIAGNOSTICS",
	"NUMERIC",
	"VARCHAR",
	"DISCONNECT",
	"OCTET_LENGTH",
	"VARYING",
	"DISTINCT",
	"OF",
	"VIEW",
	"DOMAIN",
	"ON",
	"WHEN",
	"DOUBLE",
	"ONLY",
	"WHENEVER",
	"DROP",
	"OPEN",
	"WHERE",
	"ELSE",
	"OPTION",
	"WITH",
	"END",
	"OR",
	"WORK",
	"END-EXEC",
	"ORDER",
	"WRITE",
	"ESCAPE",
	"OUTER",
	"YEAR",
	"EXCEPT",
	"OUTPUT",
	"ZONE",
	"EXCEPTION",
	// "future keywords",
	"ABSOLUTE",
	"HOST",
	"RELATIVE",
	"ACTION",
	"HOUR",
	"RELEASE",
	"ADMIN",
	"IGNORE",
	"RESULT",
	"AFTER",
	"IMMEDIATE",
	"RETURNS",
	"AGGREGATE",
	"INDICATOR",
	"ROLE",
	"ALIAS",
	"INITIALIZE",
	"ROLLUP",
	"ALLOCATE",
	"INITIALLY",
	"ROUTINE",
	"ARE",
	"INOUT",
	"ROW",
	"ARRAY",
	"INPUT",
	"ROWS",
	"ASENSITIVE",
	"INT",
	"SAVEPOINT",
	"ASSERTION",
	"INTEGER",
	"SCROLL",
	"ASYMMETRIC",
	"INTERSECTION",
	"SCOPE",
	"AT",
	"INTERVAL",
	"SEARCH",
	"ATOMIC",
	"ISOLATION",
	"SECOND",
	"BEFORE",
	"ITERATE",
	"SECTION",
	"BINARY",
	"LANGUAGE",
	"SENSITIVE",
	"BIT",
	"LARGE",
	"SEQUENCE",
	"BLOB",
	"LAST",
	"SESSION",
	"BOOLEAN",
	"LATERAL",
	"SETS",
	"BOTH",
	"LEADING",
	"SIMILAR",
	"BREADTH",
	"LESS",
	"SIZE",
	"CALL",
	"LEVEL",
	"SMALLINT",
	"CALLED",
	"LIKE_REGEX",
	"SPACE",
	"CARDINALITY",
	"LIMIT",
	"SPECIFIC",
	"CASCADED",
	"LN",
	"SPECIFICTYPE",
	"CAST",
	"LOCAL",
	"SQL",
	"CATALOG",
	"LOCALTIME",
	"SQLEXCEPTION",
	"CHAR",
	"LOCALTIMESTAMP",
	"SQLSTATE",
	"CHARACTER",
	"LOCATOR",
	"SQLWARNING",
	"CLASS",
	"MAP",
	"START",
	"CLOB",
	"MATCH",
	"STATE",
	"COLLATION",
	"MEMBER",
	"STATEMENT",
	"COLLECT",
	"METHOD",
	"STATIC",
	"COMPLETION",
	"MINUTE",
	"STDDEV_POP",
	"CONDITION",
	"MOD",
	"STDDEV_SAMP",
	"CONNECT",
	"MODIFIES",
	"STRUCTURE",
	"CONNECTION",
	"MODIFY",
	"SUBMULTISET",
	"CONSTRAINTS",
	"MODULE",
	"SUBSTRING_REGEX",
	"CONSTRUCTOR",
	"MONTH",
	"SYMMETRIC",
	"CORR",
	"MULTISET",
	"SYSTEM",
	"CORRESPONDING",
	"NAMES",
	"TEMPORARY",
	"COVAR_POP",
	"NATURAL",
	"TERMINATE",
	"COVAR_SAMP",
	"NCHAR",
	"THAN",
	"CUBE",
	"NCLOB",
	"TIME",
	"CUME_DIST",
	"NEW",
	"TIMESTAMP",
	"CURRENT_CATALOG",
	"NEXT",
	"TIMEZONE_HOUR",
	"CURRENT_DEFAULT_TRANSFORM_GROUP",
	"NO",
	"TIMEZONE_MINUTE",
	"CURRENT_PATH",
	"NONE",
	"TRAILING",
	"CURRENT_ROLE",
	"NORMALIZE",
	"TRANSLATE_REGEX",
	"CURRENT_SCHEMA",
	"NUMERIC",
	"TRANSLATION",
	"CURRENT_TRANSFORM_GROUP_FOR_TYPE",
	"OBJECT",
	"TREAT",
	"CYCLE",
	"OCCURRENCES_REGEX",
	"TRUE",
	"DATA",
	"OLD",
	"UESCAPE",
	"DATE",
	"ONLY",
	"UNDER",
	"DAY",
	"OPERATION",
	"UNKNOWN",
	"DEC",
	"ORDINALITY",
	"UNNEST",
	"DECIMAL",
	"OUT",
	"USAGE",
	"DEFERRABLE",
	"OVERLAY",
	"USING",
	"DEFERRED",
	"OUTPUT",
	"VALUE",
	"DEPTH",
	"PAD",
	"VAR_POP",
	"DEREF",
	"PARAMETER",
	"VAR_SAMP",
	"DESCRIBE",
	"PARAMETERS",
	"VARCHAR",
	"DESCRIPTOR",
	"PARTIAL",
	"VARIABLE",
	"DESTROY",
	"PARTITION",
	"WHENEVER",
	"DESTRUCTOR",
	"PATH",
	"WIDTH_BUCKET",
	"DETERMINISTIC",
	"POSTFIX",
	"WITHOUT",
	"DICTIONARY",
	"PREFIX",
	"WINDOW",
	"DIAGNOSTICS",
	"PREORDER",
	"WITHIN",
	"DISCONNECT",
	"PREPARE",
	"WORK",
	"DOMAIN",
	"PERCENT_RANK",
	"WRITE",
	"DYNAMIC",
	"PERCENTILE_CONT",
	"XMLAGG",
	"EACH",
	"PERCENTILE_DISC",
	"XMLATTRIBUTES",
	"ELEMENT",
	"POSITION_REGEX",
	"XMLBINARY",
	"END-EXEC",
	"PRESERVE",
	"XMLCAST",
	"EQUALS",
	"PRIOR",
	"XMLCOMMENT",
	"EVERY",
	"PRIVILEGES",
	"XMLCONCAT",
	"EXCEPTION",
	"RANGE",
	"XMLDOCUMENT",
	"FALSE",
	"READS",
	"XMLELEMENT",
	"FILTER",
	"REAL",
	"XMLEXISTS",
	"FIRST",
	"RECURSIVE",
	"XMLFOREST",
	"FLOAT",
	"REF",
	"XMLITERATE",
	"FOUND",
	"REFERENCING",
	"XMLNAMESPACES",
	"FREE",
	"REGR_AVGX",
	"XMLPARSE",
	"FULLTEXTTABLE",
	"REGR_AVGY",
	"XMLPI",
	"FUSION",
	"REGR_COUNT",
	"XMLQUERY",
	"GENERAL",
	"REGR_INTERCEPT",
	"XMLSERIALIZE",
	"GET",
	"REGR_R2",
	"XMLTABLE",
	"GLOBAL",
	"REGR_SLOPE",
	"XMLTEXT",
	"GO",
	"REGR_SXX",
	"XMLVALIDATE",
	"GROUPING",
	"REGR_SXY",
	"YEAR",
	"HOLD",
	"REGR_SYY",
	"ZONE",

	// not acceptable names for sql parser
	"TYPE",
}

var reservedKeywords = make(map[string]bool)

func init() {
	// build reserved keyword map
	for _, keyword := range reservedKeywordsArray {
		reservedKeywords[strings.ToLower(keyword)] = true
	}
}

func getSafeColumnName(name string) string {
	if _, ok := reservedKeywords[strings.ToLower(name)]; ok {
		return "[" + name + "]"
	}
	return name
}

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

		// col.name check if reserved keywords or not
		var col_name string = getSafeColumnName(col.Name)

		fmt.Fprintf(&queryBuilder, "%s %s", col_name, col.dataType)
		if length, ok := col.getLength(); ok {
			fmt.Fprintf(&queryBuilder, "(%s)", length)
		}
		if !col.Nullable {
			fmt.Fprint(&queryBuilder, " NOT NULL")
		}
		if col.DefaultName != "" {
			fmt.Fprintf(&queryBuilder, " CONSTRAINT %s DEFAULT %s", col.DefaultName, col.DefaultVal)
		}
		if col.Identity != nil {
			fmt.Fprintf(&queryBuilder, " IDENTITY(%s,%s)", col.Identity.SeedValue, col.Identity.IncrementValue)
			if col.Identity.NotForReplication {
				fmt.Fprint(&queryBuilder, " NOT FOR REPLICATION")
			}
		}
		if col.Check != nil {
			fmt.Fprintf(&queryBuilder, " CONSTRAINT [%s] CHECK", col.Check.Name)
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
		fmt.Fprintf(&queryBuilder, "CONSTRAINT [%s] PRIMARY KEY", indexDef.name)

		if indexDef.indexType == "CLUSTERED" || indexDef.indexType == "NONCLUSTERED" {
			fmt.Fprintf(&queryBuilder, " %s", indexDef.indexType)
		}
		fmt.Fprintf(&queryBuilder, " (%s)", strings.Join(indexDef.columns, ", "))
		if len(indexDef.options) > 0 {
			fmt.Fprint(&queryBuilder, " WITH (")
			for i, option := range indexDef.options {
				// skip FILLFACTOR if value equal 0
				if option.name == "FILLFACTOR" && option.value == "0" {
					continue
				}
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

	// FIXME: reverse iteration for index
	// index comes reverse way, i dont know why
	var i int
	for i = len(indexDefs) - 1; i >= 0; i = i - 1 {
		var indexDef *indexDef = indexDefs[i]

		if indexDef.primary {
			continue
		}
		fmt.Fprint(&queryBuilder, ",\n"+indent)

		fmt.Fprintf(&queryBuilder, "INDEX [%s]", indexDef.name)
		if indexDef.unique {
			fmt.Fprint(&queryBuilder, " UNIQUE")
		}
		if indexDef.indexType == "CLUSTERED" || indexDef.indexType == "NONCLUSTERED" {
			fmt.Fprintf(&queryBuilder, " %s", indexDef.indexType)
		}

		// TODO: we don't have index ASC or DESC now
		fmt.Fprintf(&queryBuilder, " (%s)", strings.Join(indexDef.columns, ", "))
		if len(indexDef.included) > 0 {
			fmt.Fprintf(&queryBuilder, " INCLUDE (%s)", strings.Join(indexDef.included, ", "))
		}
		if len(indexDef.options) > 0 {
			fmt.Fprint(&queryBuilder, " WITH (")
			for i, option := range indexDef.options {
				// skip FILLFACTOR if value equal 0
				if option.name == "FILLFACTOR" && option.value == "0" {
					continue
				}
				if i > 0 {
					fmt.Fprint(&queryBuilder, ",")
				}

				fmt.Fprintf(&queryBuilder, " %s", fmt.Sprintf("%s = %s", option.name, option.value))
			}
			fmt.Fprint(&queryBuilder, " )")
		}
	}

	fmt.Fprintf(&queryBuilder, "\n);\n")

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
	case "datetimeoffset":
		if c.Scale == "7" {
			return "", false
		}
		return c.Scale, true
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

func (d *MssqlDatabase) getColumns(table string) ([]column, error) {
	schema, table := splitTableName(table)
	query := fmt.Sprintf(`SELECT
	c.name,
	[type_name] = tp.name,
	c.max_length,
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
FROM sys.columns c WITH(NOLOCK)
JOIN sys.types tp WITH(NOLOCK) ON c.user_type_id = tp.user_type_id
LEFT JOIN sys.check_constraints cc WITH(NOLOCK) ON c.[object_id] = cc.parent_object_id AND cc.parent_column_id = c.column_id
LEFT JOIN sys.identity_columns ic WITH(NOLOCK) ON c.[object_id] = ic.[object_id] AND ic.[column_id] = c.[column_id]
WHERE c.[object_id] = OBJECT_ID('%s.%s', 'U')`, schema, table)

	rows, err := d.db.Query(query)
	if err != nil {
		fmt.Println(err)
	}
	defer rows.Close()

	cols := []column{}
	for rows.Next() {
		col := column{}
		var colName, dataType, maxLen, scale, defaultId string
		var seedValue, incrementValue, defaultName, defaultVal, checkName, checkDefinition *string
		var isNullable, isIdentity bool
		var identityNotForReplication, checkNotForReplication *bool
		err = rows.Scan(&colName, &dataType, &maxLen, &scale, &isNullable, &isIdentity, &seedValue, &incrementValue, &identityNotForReplication, &defaultId, &defaultName, &defaultVal, &checkName, &checkDefinition, &checkNotForReplication)
		if err != nil {
			return nil, err
		}
		col.Name = colName
		col.MaxLength = maxLen
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
	included  []string
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
	ind.is_primary_key,
	ind.is_unique,
	ind.type_desc,
	ind.is_padded,
	ind.fill_factor,
	ind.ignore_dup_key,
	st.no_recompute,
	st.is_incremental,
	ind.allow_row_locks,
	ind.allow_page_locks
FROM sys.indexes ind
INNER JOIN sys.stats st ON ind.object_id = st.object_id AND ind.index_id = st.stats_id
WHERE ind.object_id = OBJECT_ID('[%s].[%s]')`, schema, table)

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}

	indexDefMap := make(map[string]*indexDef)
	var indexName, typeDesc, fillfactor string
	var isPrimary, isUnique, padIndex, ignoreDupKey, noRecompute, incremental, rowLocks, pageLocks bool
	for rows.Next() {
		err = rows.Scan(&indexName, &isPrimary, &isUnique, &typeDesc, &padIndex, &fillfactor, &ignoreDupKey, &noRecompute, &incremental, &rowLocks, &pageLocks)
		if err != nil {
			return nil, err
		}

		options := []indexOption{
			{name: "PAD_INDEX", value: boolToOnOff(padIndex)},
			{name: "FILLFACTOR", value: fillfactor},
			{name: "IGNORE_DUP_KEY", value: boolToOnOff(ignoreDupKey)},
			{name: "STATISTICS_NORECOMPUTE", value: boolToOnOff(noRecompute)},
			{name: "STATISTICS_INCREMENTAL", value: boolToOnOff(incremental)},
			{name: "ALLOW_ROW_LOCKS", value: boolToOnOff(rowLocks)},
			{name: "ALLOW_PAGE_LOCKS", value: boolToOnOff(pageLocks)},
		}

		definition := &indexDef{name: indexName, columns: []string{}, primary: isPrimary, unique: isUnique, indexType: typeDesc, included: []string{}, options: options}
		indexDefMap[indexName] = definition
	}

	rows.Close()

	query = fmt.Sprintf(`SELECT
	ind.name AS index_name,
	COL_NAME(ic.object_id, ic.column_id) AS column_name,
	ic.is_descending_key,
	ic.is_included_column
FROM sys.indexes ind
INNER JOIN sys.index_columns ic ON ind.object_id = ic.object_id AND ind.index_id = ic.index_id
WHERE ind.object_id = OBJECT_ID('[%s].[%s]')`, schema, table)

	rows, err = d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columnName string
	var isDescending, isIncluded bool
	for rows.Next() {
		err = rows.Scan(&indexName, &columnName, &isDescending, &isIncluded)
		if err != nil {
			return nil, err
		}

		columnDefinition := fmt.Sprintf("[%s]", columnName)

		if isIncluded {
			indexDefMap[indexName].included = append(indexDefMap[indexName].included, columnDefinition)
		} else {
			if isDescending {
				columnDefinition += " DESC"
			}
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
	f.delete_referential_action_desc,
	f.is_not_for_replication
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
		var notForReplication bool
		err = rows.Scan(&constraintName, &columnName, &foreignTableName, &foreignColumnName, &foreignUpdateRule, &foreignDeleteRule, &notForReplication)
		if err != nil {
			return nil, err
		}
		foreignUpdateRule = strings.Replace(foreignUpdateRule, "_", " ", -1)
		foreignDeleteRule = strings.Replace(foreignDeleteRule, "_", " ", -1)
		def := fmt.Sprintf("CONSTRAINT [%s] FOREIGN KEY ([%s]) REFERENCES [%s] ([%s]) ON UPDATE %s ON DELETE %s", constraintName, columnName, foreignTableName, foreignColumnName, foreignUpdateRule, foreignDeleteRule)
		if notForReplication {
			def += " NOT FOR REPLICATION"
		}
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
	// azure sql server has some system view only distinguished by 'is_ms_shipped = 0' check
	const sql = `SELECT
	sys.views.name as name,
	sys.sql_modules.definition as definition
FROM sys.views
INNER JOIN sys.objects ON
	sys.objects.object_id = sys.views.object_id and sys.objects.is_ms_shipped = 0
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
		ddls = append(ddls, definition+";")
	}
	return ddls, nil
}

func (d *MssqlDatabase) Triggers() ([]string, error) {
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
		triggers = append(triggers, definition)
	}

	return triggers, nil
}

func (d *MssqlDatabase) Types() ([]string, error) {
	return nil, nil
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
