package mysql

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"

	driver "github.com/go-sql-driver/mysql"
	"github.com/sqldef/sqldef/v3/database"
)

type MysqlDatabase struct {
	config              database.Config
	db                  *sql.DB
	lowerCaseTableNames int // 0 = case-sensitive, 1 or 2 = case-insensitive
	generatorConfig     database.GeneratorConfig
}

func NewDatabase(config database.Config) (database.Database, error) {
	if config.SslMode == "custom" {
		err := registerTLSConfig(config.SslCa)
		if err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("mysql", mysqlBuildDSN(config))
	if err != nil {
		return nil, err
	}

	// Query MySQL version and lower_case_table_names for case sensitivity handling
	lowerCaseTableNames := queryMySQLServerInfo(db)

	return &MysqlDatabase{
		db:                  db,
		config:              config,
		lowerCaseTableNames: lowerCaseTableNames,
	}, nil
}

// queryMySQLServerInfo logs the MySQL version and returns the lower_case_table_names setting.
// This helps debug case sensitivity issues since MySQL behavior differs:
// - lower_case_table_names=0 (Linux default): Case-sensitive table names
// - lower_case_table_names=1 or 2 (macOS/Windows): Case-insensitive table names
func queryMySQLServerInfo(db *sql.DB) int {
	var version string
	if err := db.QueryRow("SELECT VERSION()").Scan(&version); err != nil {
		slog.Debug("Failed to get MySQL version", "error", err)
	} else {
		slog.Debug("MySQL server version", "version", version)
	}

	var varName, lowerCaseTableNames string
	if err := db.QueryRow("SHOW VARIABLES LIKE 'lower_case_table_names'").Scan(&varName, &lowerCaseTableNames); err != nil {
		slog.Debug("Failed to get lower_case_table_names", "error", err)
		return 0 // Default to case-sensitive
	}
	slog.Debug("MySQL lower_case_table_names", "value", lowerCaseTableNames)

	switch lowerCaseTableNames {
	case "1":
		return 1
	case "2":
		return 2
	default:
		return 0
	}
}

const databaseDistributionPolicyBindingsQuery = `
	SELECT o.schema_name, d.distribution_policy_name
	FROM information_schema.META_CLUSTER_DATA_OBJECTS o
	JOIN information_schema.META_CLUSTER_DPS d
	  ON d.distribution_policy_id = o.distribution_policy_id
	WHERE o.distribution_policy_id IS NOT NULL
	  AND (o.table_name IS NULL OR TRIM(o.table_name) = '')
	  AND o.schema_name IS NOT NULL
	  AND TRIM(o.schema_name) <> ''
	  AND d.distribution_policy_name IS NOT NULL
`

func (d *MysqlDatabase) databaseDDLs() ([]string, error) {
	policyRows, err := d.db.Query(databaseDistributionPolicyBindingsQuery)
	if err != nil {
		if isMissingTDSQLMetadata(err) {
			return d.databaseDDLsWithoutPolicies()
		}
		return nil, err
	}
	policies := make(map[string]string)
	for policyRows.Next() {
		var databaseName, policyName string
		if err := policyRows.Scan(&databaseName, &policyName); err != nil {
			policyRows.Close()
			return nil, err
		}
		if databaseName != "" && policyName != "" {
			policies[databaseName] = policyName
		}
	}
	if err := policyRows.Err(); err != nil {
		policyRows.Close()
		return nil, err
	}
	policyRows.Close()
	return d.databaseDDLsWithPolicies(policies)
}

func (d *MysqlDatabase) databaseDDLsWithoutPolicies() ([]string, error) {
	return d.databaseDDLsWithPolicies(nil)
}

func (d *MysqlDatabase) databaseDDLsWithPolicies(policies map[string]string) ([]string, error) {
	// Database DDLs are used only by ExportDDLsForDiff. The diff input must
	// include every user database, not just the database used for the current
	// connection; otherwise CREATE DATABASE for another desired database is
	// always considered missing and is emitted on every run.
	rows, err := d.db.Query(`
		SELECT SCHEMA_NAME
		FROM information_schema.SCHEMATA
		WHERE SCHEMA_NAME NOT IN ('information_schema', 'mysql', 'performance_schema', 'sys')
		ORDER BY SCHEMA_NAME
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
		statement := fmt.Sprintf("CREATE DATABASE %s", quoteBacktickIdentifier(name))
		if policyName := policies[name]; policyName != "" {
			statement += " USING DISTRIBUTION POLICY " + quoteDoubleIdentifier(policyName)
		}
		ddls = append(ddls, statement+";")
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ddls, nil
}

func (d *MysqlDatabase) ExportDDLs() (string, error) {
	policyDDLs, err := d.policyDDLs()
	if err != nil {
		return "", err
	}
	return d.exportObjectDDLs(policyDDLs)
}

// ExportDDLsForDiff adds the target database metadata needed to compare
// CREATE/ALTER DATABASE statements. It is deliberately separate from
// ExportDDLs so --export continues to emit only the selected database's
// objects and policy definitions.
func (d *MysqlDatabase) ExportDDLsForDiff() (string, error) {
	policyDDLs, err := d.policyDDLs()
	if err != nil {
		return "", err
	}
	databaseDDLs, err := d.databaseDDLs()
	if err != nil {
		return "", err
	}
	ddls := append(policyDDLs, databaseDDLs...)
	return d.exportObjectDDLs(ddls)
}

func (d *MysqlDatabase) exportObjectDDLs(ddls []string) (string, error) {
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

	triggerDDLs, err := d.triggers()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, triggerDDLs...)

	eventDDLs, err := d.events()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, eventDDLs...)

	return strings.Join(ddls, "\n\n"), nil
}

func (d *MysqlDatabase) tableNames() ([]string, error) {
	rows, err := d.db.Query(`
		SHOW FULL TABLES
		WHERE Table_Type != 'VIEW'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := []string{}
	for rows.Next() {
		var table string
		var tableType string
		if err := rows.Scan(&table, &tableType); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}
	return tables, nil
}

func (d *MysqlDatabase) exportTableDDL(table string) (string, error) {
	var ddl string
	sql := fmt.Sprintf(`
		SHOW CREATE TABLE `+"`%s`", table) // TODO: escape table name

	err := d.db.QueryRow(sql).Scan(&table, &ddl)
	if err != nil {
		return "", err
	}

	ddl = normalizeTTLArchiveTableDDL(ddl, d.config.DbName)
	ddl = regexp.MustCompile(`(?i)[[:space:]]+ENGINE[[:space:]]*=[[:space:]]*ROCKSDB`).ReplaceAllString(ddl, "")
	ddl = strings.TrimSpace(ddl) + ";"
	return d.appendPolicyBinding(table, ddl)
}

func (d *MysqlDatabase) views() ([]string, error) {
	if d.config.SkipView {
		return []string{}, nil
	}

	rows, err := d.db.Query(`
		SHOW FULL TABLES
		WHERE TABLE_TYPE = 'VIEW'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ddls []string
	for rows.Next() {
		var viewName, viewType, definition, security_type string
		if err = rows.Scan(&viewName, &viewType); err != nil {
			return nil, err
		}
		query := fmt.Sprintf(`
			SELECT VIEW_DEFINITION, SECURITY_TYPE
			FROM INFORMATION_SCHEMA.VIEWS
			WHERE TABLE_SCHEMA = '%s' AND TABLE_NAME = '%s'
		`, d.config.DbName, viewName)
		if err = d.db.QueryRow(query).Scan(&definition, &security_type); err != nil {
			return nil, err
		}
		ddls = append(ddls, fmt.Sprintf("CREATE SQL SECURITY %s VIEW %s AS %s;", security_type, viewName, definition))
	}
	return ddls, nil
}

func (d *MysqlDatabase) triggers() ([]string, error) {
	rows, err := d.db.Query(`
		SHOW TRIGGERS
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ddls []string
	for rows.Next() {
		var trigger, event, table, statement, timing, sqlMode, definer, characterSetClient, collationConnection, databaseCollation string
		var created *string // can be NULL when the trigger is migrated from MySQL 5.6 to 5.7
		if err = rows.Scan(&trigger, &event, &table, &statement, &timing, &created, &sqlMode, &definer, &characterSetClient, &collationConnection, &databaseCollation); err != nil {
			return nil, err
		}
		ddls = append(ddls, fmt.Sprintf("CREATE TRIGGER %s %s %s ON %s FOR EACH ROW %s;", trigger, timing, event, table, statement))
	}
	return ddls, nil
}

// events exports MySQL scheduled events via SHOW CREATE EVENT.
// SHOW CREATE EVENT returns 7 columns: Event, sql_mode, time_zone, Create Event,
// character_set_client, collation_connection, Database Collation.
func (d *MysqlDatabase) events() ([]string, error) {
	rows, err := d.db.Query(fmt.Sprintf(`
		SELECT EVENT_NAME FROM INFORMATION_SCHEMA.EVENTS
		WHERE EVENT_SCHEMA = '%s'
	`, d.config.DbName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ddls []string
	for rows.Next() {
		var eventName string
		if err = rows.Scan(&eventName); err != nil {
			return nil, err
		}

		var name, sqlMode, timeZone, createEvent, characterSetClient, collationConnection, databaseCollation string
		if err = d.db.QueryRow(fmt.Sprintf("SHOW CREATE EVENT `%s`", eventName)).Scan(&name, &sqlMode, &timeZone, &createEvent, &characterSetClient, &collationConnection, &databaseCollation); err != nil {
			return nil, err
		}
		ddls = append(ddls, createEvent+";")
	}
	return ddls, nil
}

func (d *MysqlDatabase) DB() *sql.DB {
	return d.db
}

func (d *MysqlDatabase) Close() error {
	return d.db.Close()
}

func (d *MysqlDatabase) GetDefaultSchema() string {
	return ""
}

func mysqlBuildDSN(config database.Config) string {
	c := driver.NewConfig()
	c.User = config.User
	c.Passwd = config.Password
	c.DBName = config.DbName
	c.AllowCleartextPasswords = config.MySQLEnableCleartextPlugin
	c.TLSConfig = config.SslMode
	if config.Socket == "" {
		c.Net = "tcp"
		c.Addr = fmt.Sprintf("%s:%d", config.Host, config.Port)
	} else {
		c.Net = "unix"
		c.Addr = config.Socket
	}
	return c.FormatDSN()
}

func registerTLSConfig(pemPath string) error {
	rootCertPool := x509.NewCertPool()
	pem, err := os.ReadFile(pemPath)
	if err != nil {
		return err
	}

	if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
		return fmt.Errorf("failed to append PEM")
	}

	if err := driver.RegisterTLSConfig("custom", &tls.Config{
		RootCAs: rootCertPool,
	}); err != nil {
		return err
	}

	return nil
}

func (d *MysqlDatabase) SetGeneratorConfig(config database.GeneratorConfig) {
	config.MysqlLowerCaseTableNames = d.lowerCaseTableNames
	d.generatorConfig = config
}

func (d *MysqlDatabase) GetGeneratorConfig() database.GeneratorConfig {
	return d.generatorConfig
}

func (d *MysqlDatabase) GetTransactionQueries() database.TransactionQueries {
	return database.TransactionQueries{
		Begin:    "BEGIN",
		Commit:   "COMMIT",
		Rollback: "ROLLBACK",
	}
}

func (d *MysqlDatabase) GetConfig() database.Config {
	return d.config
}

type distributionPolicyMetadata struct {
	Name string
	Desc string
}

type distributionPolicyDescription struct {
	Constraints []distributionPolicyConstraint `json:"constraints"`
}

type distributionPolicyConstraint struct {
	Key    string   `json:"key"`
	Op     string   `json:"op"`
	Values []string `json:"values"`
}

type partitionPolicyMetadata struct {
	Name        string
	Method      string
	Expression  string
	Hidden      string
	PrivateData string
	Partitions  int
}

func (d *MysqlDatabase) appendPolicyBinding(tableName, ddl string) (string, error) {
	bindings, err := d.distributionPolicyBindings(tableName)
	if err != nil {
		return "", err
	}
	partitionBindings, err := d.partitionPolicyBindings(tableName)
	if err != nil {
		return "", err
	}
	if len(bindings) > 1 || len(partitionBindings) > 1 {
		return ddl, nil
	}
	if len(bindings) == 1 {
		return appendUsingPolicy(ddl, "DISTRIBUTION POLICY", bindings[0]), nil
	}
	if len(partitionBindings) == 1 {
		return appendUsingPolicy(ddl, "PARTITION POLICY", partitionBindings[0]), nil
	}
	return ddl, nil
}

func appendUsingPolicy(ddl, kind, name string) string {
	if strings.Contains(strings.ToUpper(ddl), " USING ") {
		return ddl
	}
	return strings.TrimSuffix(strings.TrimSpace(ddl), ";") + " USING " + kind + " " + quoteDoubleIdentifier(name) + ";"
}

const distributionPolicyBindingsQuery = `
	SELECT DISTINCT d.distribution_policy_name
	FROM information_schema.META_CLUSTER_DATA_OBJECTS o
	JOIN information_schema.META_CLUSTER_DPS d
	  ON d.distribution_policy_id = o.distribution_policy_id
	WHERE o.schema_name = ?
	  AND o.table_name = ?
	  AND o.distribution_policy_id IS NOT NULL
`

func (d *MysqlDatabase) distributionPolicyBindings(tableName string) ([]string, error) {
	// META_CLUSTER_DATA_OBJECTS stores table bindings on the base-table or
	// primary-index row. TDSQL returns NULL for non-applicable partition and
	// sub-partition names, and PRIMARY for the primary-index name; filtering
	// those columns to empty strings can therefore hide a valid table binding.
	// Use the non-null policy ID as the authoritative binding signal and let the
	// joined DP catalog deduplicate rows for tables with partitions or indexes.
	rows, err := d.db.Query(distributionPolicyBindingsQuery, d.config.DbName, tableName)
	if err != nil {
		if isMissingTDSQLMetadata(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	return scanPolicyNames(rows)
}

const partitionPolicyBindingsQuery = `
	SELECT DISTINCT PARTITION_POLICY_NAME
	FROM information_schema.PARTITION_POLICY_AFFINITIES
	WHERE LOWER(TRIM(HIDDEN)) = 'explicit'
	  AND PARTITION_POLICY_NAME IS NOT NULL
	  AND TRIM(PARTITION_POLICY_NAME) <> ''
	  AND (
		DATA_OBJECT_NAME = CONCAT(?, '.', ?)
		OR DATA_OBJECT_NAME LIKE CONCAT(?, '.', ?, '.%')
	  )
`

func (d *MysqlDatabase) partitionPolicyBindings(tableName string) ([]string, error) {
	// PARTITION_POLICY_AFFINITIES names partition objects as
	// `<schema>.<table>.<partition>` (and base objects as `<schema>.<table>`),
	// not merely the table name returned by SHOW CREATE TABLE.
	rows, err := d.db.Query(partitionPolicyBindingsQuery,
		d.config.DbName, tableName, d.config.DbName, tableName,
	)
	if err != nil {
		if isMissingTDSQLMetadata(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	return scanPolicyNames(rows)
}

func scanPolicyNames(rows *sql.Rows) ([]string, error) {
	seen := map[string]struct{}{}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if name != "" {
			if _, ok := seen[name]; !ok {
				seen[name] = struct{}{}
				names = append(names, name)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return names, nil
}

func (d *MysqlDatabase) policyDDLs() ([]string, error) {
	distributionPolicies, err := d.distributionPolicyDDLs()
	if err != nil {
		return nil, err
	}
	partitionPolicies, err := d.partitionPolicyDDLs()
	if err != nil {
		return nil, err
	}
	return append(distributionPolicies, partitionPolicies...), nil
}

func (d *MysqlDatabase) distributionPolicyDDLs() ([]string, error) {
	rows, err := d.db.Query(`
		SELECT distribution_policy_name, distribution_policy_desc
		FROM information_schema.META_CLUSTER_DPS
		ORDER BY distribution_policy_name
	`)
	if err != nil {
		if isMissingTDSQLMetadata(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()

	var ddls []string
	for rows.Next() {
		var policy distributionPolicyMetadata
		if err := rows.Scan(&policy.Name, &policy.Desc); err != nil {
			return nil, err
		}
		ddl, err := formatDistributionPolicyDDL(policy)
		if err != nil {
			slog.Debug("Skipping unsupported TDSQL distribution policy in export", "name", policy.Name, "error", err)
			continue
		}
		ddls = append(ddls, ddl)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ddls, nil
}

func (d *MysqlDatabase) partitionPolicyDDLs() ([]string, error) {
	// Read the policy catalog itself instead of PARTITION_POLICY_PARTITION_BRIEF.
	// The brief view can omit standalone explicit policies such as pp2, even
	// though CREATE PARTITION POLICY has registered their names. Missing such a
	// policy makes the next apply attempt to create it again and breaks
	// idempotency.
	rows, err := d.db.Query(`
		SELECT
			p.NAME,
			p.PARTITION_TYPE,
			p.PARTITION_EXPRESSION,
			p.HIDDEN,
			p.SE_PRIVATE_DATA,
			COUNT(DISTINCT pp.PARTITION_ID)
		FROM information_schema.PARTITION_POLICIES p
		LEFT JOIN information_schema.PARTITION_POLICY_PARTITIONS pp
			ON pp.PARTITION_POLICY_ID = p.ID
		GROUP BY p.ID, p.NAME, p.PARTITION_TYPE, p.PARTITION_EXPRESSION, p.HIDDEN, p.SE_PRIVATE_DATA
		ORDER BY p.NAME
	`)
	if err != nil {
		if isMissingTDSQLMetadata(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()

	var policies []partitionPolicyMetadata
	for rows.Next() {
		var policy partitionPolicyMetadata
		var method, expression, hidden, privateData sql.NullString
		if err := rows.Scan(&policy.Name, &method, &expression, &hidden, &privateData, &policy.Partitions); err != nil {
			return nil, err
		}
		policy.Method = method.String
		policy.Expression = expression.String
		policy.Hidden = hidden.String
		policy.PrivateData = privateData.String
		policies = append(policies, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	ddls := make([]string, 0, len(policies))
	for _, policy := range policies {
		ddl, ok := formatPartitionPolicyDDL(policy)
		if !ok {
			slog.Debug("Skipping unsupported TDSQL partition policy in export", "name", policy.Name, "method", policy.Method)
			continue
		}
		ddls = append(ddls, ddl)
	}
	return ddls, nil
}

func formatDistributionPolicyDDL(policy distributionPolicyMetadata) (string, error) {
	var description distributionPolicyDescription
	if err := json.Unmarshal([]byte(policy.Desc), &description); err != nil {
		return "", fmt.Errorf("decode distribution policy %q metadata: %w", policy.Name, err)
	}
	if len(description.Constraints) == 0 {
		return "", fmt.Errorf("distribution policy %q has no constraints", policy.Name)
	}
	parts := make([]string, 0, len(description.Constraints))
	for index, constraint := range description.Constraints {
		part, err := formatDistributionPolicyConstraint(constraint)
		if err != nil {
			return "", fmt.Errorf("distribution policy %q: %w", policy.Name, err)
		}
		if index > 0 {
			part = strings.TrimPrefix(part, "SET ")
		}
		parts = append(parts, part)
	}
	return fmt.Sprintf("CREATE DISTRIBUTION POLICY %s %s;", quoteDoubleIdentifier(policy.Name), strings.Join(parts, " AND ")), nil
}

func formatDistributionPolicyConstraint(constraint distributionPolicyConstraint) (string, error) {
	key := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(constraint.Key), "-", "_"))
	if key == "" {
		return "", errors.New("constraint key is empty")
	}
	op := strings.ToUpper(strings.TrimSpace(constraint.Op))
	switch strings.ToLower(strings.TrimSpace(constraint.Op)) {
	case "in":
		op = "IN"
	case "notin", "not_in":
		op = "NOT IN"
	case "exists":
		op = "EXISTS"
	case "notexists", "not_exists":
		op = "NOT EXISTS"
	case "=":
		op = "="
	default:
		return "", fmt.Errorf("unsupported operator %q", constraint.Op)
	}
	if (op == "EXISTS" || op == "NOT EXISTS") && len(constraint.Values) != 0 {
		return "", fmt.Errorf("operator %s cannot have values", op)
	}
	if (op != "EXISTS" && op != "NOT EXISTS") && len(constraint.Values) == 0 {
		return "", fmt.Errorf("operator %s requires values", op)
	}
	if op == "EXISTS" || op == "NOT EXISTS" {
		return "SET " + key + " " + op, nil
	}
	values := make([]string, 0, len(constraint.Values))
	for _, value := range constraint.Values {
		value = strings.TrimSpace(value)
		if value == "" {
			return "", errors.New("constraint value is empty")
		}
		if op == "=" {
			if _, err := strconv.ParseInt(value, 10, 64); err == nil {
				values = append(values, value)
			} else {
				values = append(values, strconv.Quote(value))
			}
		} else {
			values = append(values, strconv.Quote(value))
		}
	}
	if op == "=" {
		return "SET " + key + " = " + values[0], nil
	}
	return "SET " + key + " " + op + " (" + strings.Join(values, ", ") + ")", nil
}

func formatPartitionPolicyDDL(policy partitionPolicyMetadata) (string, bool) {
	method := strings.ToUpper(strings.TrimSpace(policy.Method))
	if method == "" || strings.EqualFold(method, "NULL") {
		return fmt.Sprintf("CREATE PARTITION POLICY %s;", quoteBacktickIdentifier(policy.Name)), true
	}
	if method == "KEY_51" || method == "KEY_55" {
		columnCount, ok := partitionPolicyColumnCount(policy.PrivateData)
		if !ok {
			return "", false
		}
		return fmt.Sprintf("CREATE PARTITION POLICY %s PARTITION BY KEY COLUMNS %d PARTITIONS %d;", quoteBacktickIdentifier(policy.Name), columnCount, policy.Partitions), true
	}
	if method != "HASH" && method != "KEY" {
		return "", false
	}
	expression := strings.TrimSpace(policy.Expression)
	if expression == "" || strings.EqualFold(expression, "NULL") {
		return "", false
	}
	return fmt.Sprintf("CREATE PARTITION POLICY %s PARTITION BY %s(%s) PARTITIONS %d;", quoteBacktickIdentifier(policy.Name), method, expression, policy.Partitions), true
}

var partitionPolicyColumnCountPattern = regexp.MustCompile(`(?:^|;)\s*partition_columns_num\s*=\s*([0-9]+)\s*(?:;|$)`)

func partitionPolicyColumnCount(privateData string) (int, bool) {
	match := partitionPolicyColumnCountPattern.FindStringSubmatch(privateData)
	if len(match) != 2 {
		return 0, false
	}
	count, err := strconv.Atoi(match[1])
	return count, err == nil && count > 0
}

func quoteDoubleIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteBacktickIdentifier(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func normalizeTTLArchiveTableDDL(ddl, databaseName string) string {
	if databaseName == "" {
		return ddl
	}
	pattern := regexp.MustCompile(`(?i)(TTL_ARCHIVE_TABLE\s*=\s*')` + regexp.QuoteMeta(databaseName) + `\.`)
	return pattern.ReplaceAllString(ddl, `${1}`)
}

func isMissingTDSQLMetadata(err error) bool {
	mysqlErr, ok := errors.AsType[*driver.MySQLError](err)
	if ok {
		return mysqlErr.Number == 1109 || mysqlErr.Number == 1146
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unknown table") || strings.Contains(message, "doesn't exist")
}
