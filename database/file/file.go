package file

import (
	"database/sql"
	"github.com/k0kubun/sqldef"
	"strings"
)

// Pseudo database for comparison between files
type FileDatabase struct {
	file string
}

func NewDatabase(file string) FileDatabase {
	return FileDatabase{
		file: file,
	}
}

func (d FileDatabase) DumpDDLs() (string, error) {
	var ddls []string

	typeDDLs, err := d.Types()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, typeDDLs...)

	tableNames, err := d.TableNames()
	if err != nil {
		return "", err
	}
	for _, tableName := range tableNames {
		ddl, err := d.DumpTableDDL(tableName)
		if err != nil {
			return "", err
		}

		ddls = append(ddls, ddl)
	}

	viewDDLs, err := d.Views()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, viewDDLs...)

	triggerDDLs, err := d.Triggers()
	if err != nil {
		return "", err
	}
	ddls = append(ddls, triggerDDLs...)

	return strings.Join(ddls, "\n\n"), nil
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
