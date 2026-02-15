package database

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
)

type DryRunDatabase struct {
	wrapped         Database
	dryRunDB        *sql.DB
	generatorConfig GeneratorConfig
}

func NewDryRunDatabase(db Database) (*DryRunDatabase, error) {
	txQueries := db.GetTransactionQueries()

	dryRunDriverName := fmt.Sprintf("dry-run-%p", db) // Unique name per database instance
	sql.Register(dryRunDriverName, &dryRunDriver{txQueries: txQueries})

	dryRunDB, err := sql.Open(dryRunDriverName, "dry-run")
	if err != nil {
		return nil, err
	}

	return &DryRunDatabase{
		wrapped:  db,
		dryRunDB: dryRunDB,
	}, nil
}

func (d *DryRunDatabase) ExportDDLs() (string, error) {
	return d.wrapped.ExportDDLs()
}

func (d *DryRunDatabase) DB() *sql.DB {
	return d.dryRunDB
}

func (d *DryRunDatabase) Close() error {
	if err := d.dryRunDB.Close(); err != nil {
		return err
	}
	return d.wrapped.Close()
}

func (d *DryRunDatabase) GetDefaultSchema() string {
	return d.wrapped.GetDefaultSchema()
}

func (d *DryRunDatabase) SetGeneratorConfig(config GeneratorConfig) {
	d.wrapped.SetGeneratorConfig(config)
	// Get the config back from wrapped in case it was modified (e.g., MySQL adds lowerCaseTableNames)
	d.generatorConfig = d.wrapped.GetGeneratorConfig()
}

func (d *DryRunDatabase) GetGeneratorConfig() GeneratorConfig {
	return d.generatorConfig
}

func (d *DryRunDatabase) GetTransactionQueries() TransactionQueries {
	return d.wrapped.GetTransactionQueries()
}

func (d *DryRunDatabase) GetConfig() Config {
	return d.wrapped.GetConfig()
}

func (d *DryRunDatabase) SetMigrationScope(scope MigrationScope) {
	d.wrapped.SetMigrationScope(scope)
}

func (d *DryRunDatabase) GetMigrationScope() MigrationScope {
	return d.wrapped.GetMigrationScope()
}

type dryRunDriver struct {
	txQueries TransactionQueries
}

func (d *dryRunDriver) Open(name string) (driver.Conn, error) {
	return &dryRunConn{txQueries: d.txQueries}, nil
}

type dryRunConn struct {
	txQueries TransactionQueries
}

func (c *dryRunConn) Prepare(query string) (driver.Stmt, error) {
	return &dryRunStmt{query: query}, nil
}

func (c *dryRunConn) Close() error {
	return nil
}

func (c *dryRunConn) Begin() (driver.Tx, error) {
	return &dryRunTx{txQueries: c.txQueries}, nil
}

type dryRunTx struct {
	txQueries TransactionQueries
}

func (tx *dryRunTx) Commit() error {
	return nil
}

func (tx *dryRunTx) Rollback() error {
	return nil
}

type dryRunStmt struct {
	query string
}

func (s *dryRunStmt) Close() error {
	return nil
}

func (s *dryRunStmt) NumInput() int {
	return -1
}

func (s *dryRunStmt) Exec(args []driver.Value) (driver.Result, error) {
	return &dryRunResult{}, nil
}

func (s *dryRunStmt) Query(args []driver.Value) (driver.Rows, error) {
	return &dryRunRows{closed: false}, nil
}

type dryRunResult struct{}

func (r *dryRunResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (r *dryRunResult) RowsAffected() (int64, error) {
	return 0, nil
}

type dryRunRows struct {
	closed bool
}

func (r *dryRunRows) Columns() []string {
	return []string{}
}

func (r *dryRunRows) Close() error {
	r.closed = true
	return nil
}

func (r *dryRunRows) Next(dest []driver.Value) error {
	return fmt.Errorf("EOF")
}
