// This package has database driver layer. Never deal with SQL.
package driver

// Abstraction layer for multiple kinds of databases
type Database struct {
	config Config
}

func NewDatabase(config Config) *Database {
	return &Database{
		config: config,
	}
}

func (d *Database) TableNames() []string {
	return []string{}
}
