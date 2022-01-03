package file

import (
	"database/sql"
	"github.com/k0kubun/sqldef"
)

// Pseudo adapter for comparison between files
type FileDatabase struct {
	file string
}

func NewDatabase(file string) FileDatabase {
	return FileDatabase{
		file: file,
	}
}

func (f FileDatabase) TableNames() ([]string, error) {
	return []string{f.file}, nil
}

func (f FileDatabase) DumpTableDDL(file string) (string, error) {
	return sqldef.ReadFile(file)
}

func (f FileDatabase) Views() ([]string, error) {
	return nil, nil
}

func (f FileDatabase) Triggers() ([]string, error) {
	return nil, nil
}

func (f FileDatabase) Types() ([]string, error) {
	return nil, nil
}

func (f FileDatabase) DB() *sql.DB {
	return nil
}

func (f FileDatabase) Close() error {
	return nil
}
