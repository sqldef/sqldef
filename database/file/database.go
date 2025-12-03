package file

import (
	"database/sql"

	"github.com/sqldef/sqldef/v3"
	"github.com/sqldef/sqldef/v3/database"
)

// Pseudo database for comparison between files
type FileDatabase struct {
	file            string
	generatorConfig database.GeneratorConfig
}

func NewDatabase(file string) *FileDatabase {
	return &FileDatabase{
		file: file,
	}
}

func (f FileDatabase) ExportDDLs() (string, error) {
	return sqldef.ReadFile(f.file)
}

func (f FileDatabase) DB() *sql.DB {
	return nil
}

func (f FileDatabase) Close() error {
	return nil
}

func (f FileDatabase) GetDefaultSchema() string {
	return ""
}

func (d *FileDatabase) SetGeneratorConfig(config database.GeneratorConfig) {
	d.generatorConfig = config
}

func (d *FileDatabase) GetGeneratorConfig() database.GeneratorConfig {
	return d.generatorConfig
}

func (d *FileDatabase) GetTransactionQueries() database.TransactionQueries {
	return database.TransactionQueries{
		Begin:    "BEGIN",
		Commit:   "COMMIT",
		Rollback: "ROLLBACK",
	}
}

func (d *FileDatabase) GetConfig() database.Config {
	return database.Config{}
}
