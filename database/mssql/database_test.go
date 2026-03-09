package mssql

import (
	"strings"
	"testing"

	"github.com/sqldef/sqldef/v3/database"
)

func TestMssqlQuoteIdentifier(t *testing.T) {
	db := &MssqlDatabase{}

	for _, legacyIgnoreQuotes := range []bool{true, false} {
		db.SetGeneratorConfig(database.GeneratorConfig{LegacyIgnoreQuotes: legacyIgnoreQuotes})
		for _, input := range []string{"PK_SystemUser", "SystemUserId", "Date", "has-dash"} {
			expected := "[" + strings.ReplaceAll(input, "]", "]]") + "]"
			if got := db.quoteIdentifier(input); got != expected {
				t.Fatalf("quoteIdentifier(%q) = %q, want %q", input, got, expected)
			}
		}
	}
}

func TestBuildExportTableDDLCanonicalPrimaryKey(t *testing.T) {
	db := &MssqlDatabase{}
	db.SetGeneratorConfig(database.GeneratorConfig{LegacyIgnoreQuotes: false})

	ddl := db.buildExportTableDDL(
		"dbo.SystemUser",
		[]column{
			{Name: "SystemUserId", dataType: "int", Nullable: false},
			{Name: "Date", dataType: "date", Nullable: false},
		},
		[]*indexDef{
			{
				name:       "PK_SystemUser",
				columns:    []string{"[SystemUserId]"},
				primary:    true,
				constraint: true,
				indexType:  "CLUSTERED",
			},
		},
		nil,
		nil,
	)

	if !strings.Contains(ddl, "CONSTRAINT [PK_SystemUser] PRIMARY KEY CLUSTERED ([SystemUserId])") {
		t.Fatalf("expected canonical bracketed PK constraint and column in export, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, "[Date] date NOT NULL") {
		t.Fatalf("expected reserved identifier to remain bracketed in export, got:\n%s", ddl)
	}
}
