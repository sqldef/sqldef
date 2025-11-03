package util

import "fmt"

// BuildPostgresConstraintName generates a constraint name following PostgreSQL's naming convention.
// It automatically truncates names to 63 characters (NAMEDATALEN - 1) using PostgreSQL's algorithm:
// - If column > 28 chars: reduce column to 28 first, then apply remaining overflow to table
// - If column == 28 chars and table <= 29 chars: truncate table
// - If column == 28 chars and table > 29 chars: truncate table
// - If column < 28 chars: truncate table
// In summary: when column <= 28, always truncate the table first
func BuildPostgresConstraintName(tableName, columnName, suffix string) string {
	fullName := fmt.Sprintf("%s_%s_%s", tableName, columnName, suffix)
	if len(fullName) <= 63 {
		return fullName
	}

	overflow := len(fullName) - 63
	tableLen := len(tableName)
	columnLen := len(columnName)

	tableRemove := 0
	columnRemove := 0

	if columnLen > 28 {
		// Column exceeds 28: reduce to 28 first, then put remaining overflow on table
		columnRemove = overflow
		if columnRemove > columnLen-28 {
			// Column can only be reduced to 28, put the rest on table
			tableRemove = columnRemove - (columnLen - 28)
			columnRemove = columnLen - 28
		}
	} else {
		// Column <= 28: always truncate table
		tableRemove = overflow
	}

	truncatedTable := tableName[:tableLen-tableRemove]
	truncatedColumn := columnName[:columnLen-columnRemove]

	return fmt.Sprintf("%s_%s_%s", truncatedTable, truncatedColumn, suffix)
}
