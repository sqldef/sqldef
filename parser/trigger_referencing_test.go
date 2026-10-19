package parser

import "testing"

// https://www.postgresql.org/docs/current/sql-createtrigger.html
// REFERENCING { { OLD | NEW } TABLE [ AS ] transition_relation_name } [ ... ]
func TestCreateTriggerReferencing(t *testing.T) {
	testCases := []struct {
		name    string
		sql     string
		old     Ident
		new     Ident
		forEach string
		keyword string
	}{
		{
			name:    "OLD TABLE in pg_get_triggerdef form",
			sql:     "CREATE TRIGGER t AFTER DELETE ON public.x REFERENCING OLD TABLE AS deleted_rows FOR EACH STATEMENT EXECUTE FUNCTION f()",
			old:     NewIdent("deleted_rows", false),
			forEach: "STATEMENT",
			keyword: "FUNCTION",
		},
		{
			name:    "NEW TABLE",
			sql:     "CREATE TRIGGER t AFTER INSERT ON x REFERENCING NEW TABLE AS inserted_rows FOR EACH STATEMENT EXECUTE FUNCTION f()",
			new:     NewIdent("inserted_rows", false),
			forEach: "STATEMENT",
			keyword: "FUNCTION",
		},
		{
			name:    "OLD TABLE then NEW TABLE",
			sql:     "CREATE TRIGGER t AFTER UPDATE ON x REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION f()",
			old:     NewIdent("old_rows", false),
			new:     NewIdent("new_rows", false),
			forEach: "STATEMENT",
			keyword: "FUNCTION",
		},
		{
			name:    "NEW TABLE then OLD TABLE",
			sql:     "CREATE TRIGGER t AFTER UPDATE ON x REFERENCING NEW TABLE AS new_rows OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION f()",
			old:     NewIdent("old_rows", false),
			new:     NewIdent("new_rows", false),
			forEach: "STATEMENT",
			keyword: "FUNCTION",
		},
		{
			name:    "AS omitted and lowercase keywords",
			sql:     "create trigger t after update on x referencing old table old_rows new table new_rows for each statement execute function f()",
			old:     NewIdent("old_rows", false),
			new:     NewIdent("new_rows", false),
			forEach: "STATEMENT",
			keyword: "FUNCTION",
		},
		{
			name:    "quoted names",
			sql:     `CREATE TRIGGER t AFTER UPDATE ON x REFERENCING OLD TABLE AS "Old Rows" NEW TABLE AS "new" FOR EACH STATEMENT EXECUTE FUNCTION f()`,
			old:     NewIdent("Old Rows", true),
			new:     NewIdent("new", true),
			forEach: "STATEMENT",
			keyword: "FUNCTION",
		},
		{
			name:    "non-reserved keyword as a name",
			sql:     "CREATE TRIGGER t AFTER INSERT ON x REFERENCING NEW TABLE AS rows FOR EACH STATEMENT EXECUTE FUNCTION f()",
			new:     NewIdent("rows", false),
			forEach: "STATEMENT",
			keyword: "FUNCTION",
		},
		{
			name:    "FOR EACH omitted",
			sql:     "CREATE TRIGGER t AFTER INSERT ON x REFERENCING NEW TABLE AS inserted_rows EXECUTE FUNCTION f()",
			new:     NewIdent("inserted_rows", false),
			forEach: "",
			keyword: "FUNCTION",
		},
		{
			name:    "FOR EACH ROW with WHEN and EXECUTE PROCEDURE",
			sql:     "CREATE TRIGGER t AFTER UPDATE ON x REFERENCING OLD TABLE AS old_rows FOR EACH ROW WHEN (OLD.v IS DISTINCT FROM NEW.v) EXECUTE PROCEDURE f(1, 'a')",
			old:     NewIdent("old_rows", false),
			forEach: "ROW",
			keyword: "PROCEDURE",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stmt, err := ParseDDL(tc.sql, ParserModePostgres)
			if err != nil {
				t.Fatalf("ParseDDL(%q) failed: %v", tc.sql, err)
			}
			trigger := stmt.(*DDL).Trigger
			if trigger.TransitionTables.Old != tc.old || trigger.TransitionTables.New != tc.new {
				t.Errorf("TransitionTables = %+v, want Old=%+v New=%+v", trigger.TransitionTables, tc.old, tc.new)
			}
			if trigger.ForEach != tc.forEach {
				t.Errorf("ForEach = %q, want %q", trigger.ForEach, tc.forEach)
			}
			if keyword := trigger.Body[0].(*TriggerFuncExec).Keyword; keyword != tc.keyword {
				t.Errorf("Keyword = %q, want %q", keyword, tc.keyword)
			}
		})
	}
}

func TestCreateTriggerReferencingRejectsInvalidSyntax(t *testing.T) {
	testCases := []string{
		"CREATE TRIGGER t AFTER DELETE ON x REFERENCING older TABLE AS r FOR EACH STATEMENT EXECUTE FUNCTION f()",
		`CREATE TRIGGER t AFTER DELETE ON x REFERENCING "old" TABLE AS r FOR EACH STATEMENT EXECUTE FUNCTION f()`,
		"CREATE TRIGGER t AFTER DELETE ON x REFERENCING OLD AS r FOR EACH STATEMENT EXECUTE FUNCTION f()",
		"CREATE TRIGGER t AFTER DELETE ON x REFERENCING FOR EACH STATEMENT EXECUTE FUNCTION f()",
	}

	for _, sql := range testCases {
		t.Run(sql, func(t *testing.T) {
			if _, err := ParseDDL(sql, ParserModePostgres); err == nil {
				t.Errorf("Expected parse error but got none.\nSQL: %s", sql)
			}
		})
	}
}

func TestReferencingAndOldAsUnquotedIdentifiers(t *testing.T) {
	testCases := []string{
		"CREATE TABLE referencing (old int, referencing int)",
		"CREATE INDEX i ON t (old, referencing)",
	}

	for _, sql := range testCases {
		t.Run(sql, func(t *testing.T) {
			if _, err := ParseDDL(sql, ParserModePostgres); err != nil {
				t.Fatalf("ParseDDL(%q) failed: %v", sql, err)
			}
		})
	}
}

func TestReferencingKeepsCaseAsIdentifier(t *testing.T) {
	stmt, err := ParseDDL("CREATE TABLE REFERENCING (REFERENCING int)", ParserModeMysql)
	if err != nil {
		t.Fatalf("ParseDDL failed: %v", err)
	}
	ddl := stmt.(*DDL)
	if got := ddl.NewName.Name.Name; got != "REFERENCING" {
		t.Errorf("table name = %q, want %q", got, "REFERENCING")
	}
	if got := ddl.TableSpec.Columns[0].Name.Name; got != "REFERENCING" {
		t.Errorf("column name = %q, want %q", got, "REFERENCING")
	}
}
