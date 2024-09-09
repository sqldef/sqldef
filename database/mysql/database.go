package mysql

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"os"
	"strings"

	driver "github.com/go-sql-driver/mysql"
	"github.com/sqldef/sqldef/database"
)

type MysqlDatabase struct {
	config database.Config
	db     *sql.DB
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

	return &MysqlDatabase{
		db:     db,
		config: config,
	}, nil
}

func (d *MysqlDatabase) DumpDDLs() (string, error) {
	var ddls []string

	tableNames, err := d.tableNames()
	if err != nil {
		return "", err
	}
	tableDDLs, err := database.ConcurrentMapFuncWithError(
		tableNames,
		d.config.DumpConcurrency,
		func(tableName string) (string, error) {
			return d.dumpTableDDL(tableName)
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

	return strings.Join(ddls, "\n\n"), nil
}

func (d *MysqlDatabase) tableNames() ([]string, error) {
	rows, err := d.db.Query("show full tables where Table_Type != 'VIEW'")
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

func (d *MysqlDatabase) dumpTableDDL(table string) (string, error) {
	var ddl string
	sql := fmt.Sprintf("show create table `%s`;", table) // TODO: escape table name

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

	rows, err := d.db.Query("show full tables where TABLE_TYPE = 'VIEW'")
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
		query := fmt.Sprintf("select VIEW_DEFINITION,SECURITY_TYPE from INFORMATION_SCHEMA.VIEWS where TABLE_SCHEMA = '%s' AND TABLE_NAME = '%s';", d.config.DbName, viewName)
		if err = d.db.QueryRow(query).Scan(&definition, &security_type); err != nil {
			return nil, err
		}
		ddls = append(ddls, fmt.Sprintf("CREATE SQL SECURITY %s VIEW %s AS %s;", security_type, viewName, definition))
	}
	return ddls, nil
}

func (d *MysqlDatabase) triggers() ([]string, error) {
	rows, err := d.db.Query("show triggers")
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

	driver.RegisterTLSConfig("custom", &tls.Config{
		RootCAs: rootCertPool,
	})

	return nil
}
