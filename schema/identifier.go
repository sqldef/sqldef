package schema

import (
	"strings"

	"github.com/sqldef/sqldef/v3/parser"
)

// NormalizeIdentifierName normalizes an identifier name according to database-specific rules.
// This function handles the semantics of quoted vs unquoted identifiers for each database.
//
// Parameters:
// - name: the identifier name
// - quoted: whether the identifier was quoted in the source SQL
// - mode: the database mode (MySQL, PostgreSQL, MSSQL, SQLite)
// - legacyNameNormalization: nil (default: true for now), true (legacy mode), or false (quote-aware mode)
//
// Returns the normalized name that should be used for comparison.
func NormalizeIdentifierName(name string, quoted bool, mode GeneratorMode, legacyNameNormalization *bool) string {
	// Determine if we're using legacy mode
	useLegacy := true
	if legacyNameNormalization != nil {
		useLegacy = *legacyNameNormalization
	}

	// Legacy mode: just return the name as-is (preserving current behavior)
	if useLegacy {
		return name
	}

	// Quote-aware normalization based on database mode
	switch mode {
	case GeneratorModePostgres:
		// PostgreSQL: unquoted identifiers are case-insensitive (folded to lowercase)
		// quoted identifiers are case-sensitive (preserved as-is)
		if quoted {
			return name
		}
		return strings.ToLower(name)

	case GeneratorModeMysql:
		// MySQL: identifier case-sensitivity depends on lower_case_table_names setting
		// For now, we'll treat all as case-insensitive (lowercase) which is the default on most systems
		// TODO: Consider detecting lower_case_table_names from the server
		// When quoted with backticks, MySQL preserves case but compares case-insensitively on most systems
		return strings.ToLower(name)

	case GeneratorModeMssql:
		// SQL Server: identifiers are case-insensitive by default (depends on collation)
		// Most common collations (SQL_Latin1_General_CP1_CI_AS) are case-insensitive
		// Quoted identifiers preserve case but are still compared case-insensitively
		return strings.ToLower(name)

	case GeneratorModeSQLite3:
		// SQLite: identifiers are case-insensitive
		// Quoted identifiers preserve case but are compared case-insensitively
		return strings.ToLower(name)

	default:
		return name
	}
}

// NormalizeTableIdent normalizes a TableIdent for comparison.
func NormalizeTableIdent(ident parser.TableIdent, mode GeneratorMode, legacyNameNormalization *bool) string {
	return NormalizeIdentifierName(ident.String(), ident.Quoted(), mode, legacyNameNormalization)
}

// NormalizeColIdent normalizes a ColIdent for comparison.
func NormalizeColIdent(ident parser.ColIdent, mode GeneratorMode, legacyNameNormalization *bool) string {
	return NormalizeIdentifierName(ident.String(), ident.Quoted(), mode, legacyNameNormalization)
}
