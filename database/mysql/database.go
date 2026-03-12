package mysql

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"

	driver "github.com/go-sql-driver/mysql"
	"github.com/sqldef/sqldef/v3/database"
)

type MysqlDatabase struct {
	config              database.Config
	db                  *sql.DB
	lowerCaseTableNames int // 0 = case-sensitive, 1 or 2 = case-insensitive
	generatorConfig     database.GeneratorConfig
	migrationScope      database.MigrationScope
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

func (d *MysqlDatabase) ExportDDLs() (string, error) {
	var ddls []string
	scope := d.GetMigrationScope()

	if scope.Table {
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
	}

	if scope.View {
		viewDDLs, err := d.views()
		if err != nil {
			return "", err
		}
		ddls = append(ddls, viewDDLs...)
	}

	if scope.Trigger {
		triggerDDLs, err := d.triggers()
		if err != nil {
			return "", err
		}
		ddls = append(ddls, triggerDDLs...)
	}

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

	return ddl + ";", nil
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

func (d *MysqlDatabase) SetMigrationScope(scope database.MigrationScope) {
	d.migrationScope = scope
}

func (d *MysqlDatabase) GetMigrationScope() database.MigrationScope {
	return d.migrationScope
}
