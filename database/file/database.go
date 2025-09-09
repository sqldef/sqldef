package file

import (
	"database/sql"

	"github.com/sqldef/sqldef/v2"
	"github.com/sqldef/sqldef/v2/database"
)

// Pseudo database for comparison between files
type FileDatabase struct {
	file string
}

func NewDatabase(file string) *FileDatabase {
	return &FileDatabase{
		file: file,
	}
}

func (f FileDatabase) DumpDDLs() (string, error) {
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
	// Not implemented for file - privileges not supported yet
}
