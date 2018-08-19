// This package has SQL parser, its abstraction and SQL generator.
// Never touch database.
package schema

import (
	"fmt"
	"log"
	"strings"

	"github.com/xwb1989/sqlparser"
)

// Parse DDL like `CREATE TABLE` or `ALTER TABLE`.
// This doesn't support destructive DDL like `DROP TABLE`.
func ParseDDL(ddl string) (DDL, error) {
	stmt, err := sqlparser.Parse(ddl)
	if err != nil {
		log.Fatal(err)
	}

	switch stmt := stmt.(type) {
	case *sqlparser.DDL:
		if stmt.Action == "create" {
			log.Printf("action: %s", stmt.Action)
			return &CreateTable{statement: ddl}, nil
		} else {
			return nil, fmt.Errorf("unsupported type of DDL action (only 'create' is supported): %s", stmt.Action)
		}
	default:
		return nil, fmt.Errorf("unsupported type of SQL (only DDL is supported): %s", ddl)
	}
}

// Parse `ddls`, which is expected to `;`-concatenated DDLs
// and not to include destructive DDL.
func ParseDDLs(str string) ([]DDL, error) {
	ddls := strings.Split(str, ";")
	result := make([]DDL, len(ddls))

	for _, ddl := range ddls {
		ddl = strings.TrimSpace(ddl) // TODO: trim trailing comment as well?
		if len(ddl) == 0 {
			continue
		}

		parsed, err := ParseDDL(ddl)
		if err != nil {
			return result, err
		}
		result = append(result, parsed)
	}
	return result, nil
}
