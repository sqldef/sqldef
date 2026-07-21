package parser

import "testing"

// TestParseSetTableOwner covers ALTER TABLE ... OWNER TO parsing and verifies
// that promoting OWNER to a keyword keeps it usable as a regular identifier.
func TestParseSetTableOwner(t *testing.T) {
	owners := map[string]string{
		"ALTER TABLE users OWNER TO app_user":                  "app_user",
		"ALTER TABLE ONLY dip_owner.tbf_acnt OWNER TO dip_own": "dip_own",
		`ALTER TABLE "public"."users" OWNER TO "my-role"`:      "my-role",
	}
	for sql, want := range owners {
		stmt, err := ParseDDL(sql, ParserModePostgres)
		if err != nil {
			t.Errorf("%q: %v", sql, err)
			continue
		}
		ddl, ok := stmt.(*DDL)
		if !ok || ddl.Action != SetTableOwner {
			t.Errorf("%q: expected SetTableOwner, got %#v", sql, stmt)
			continue
		}
		if ddl.OwnerRole.Name != want {
			t.Errorf("%q: owner = %q, want %q", sql, ddl.OwnerRole.Name, want)
		}
	}

	// OWNER stays usable as an identifier (PostgreSQL unreserved keyword).
	identifierCompat := []string{
		"CREATE TABLE t (owner text, id bigint)",
		"CREATE TABLE owner (id bigint)",
		"CREATE INDEX idx ON t (owner)",
		"GRANT SELECT (owner) ON TABLE t TO r",
	}
	for _, sql := range identifierCompat {
		if _, err := ParseDDL(sql, ParserModePostgres); err != nil {
			t.Errorf("%q: %v", sql, err)
		}
	}
}
