package schema

import (
	"testing"

	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/parser"
)

func grantPrivilege(statement string, grantees []string, privileges []string) *GrantPrivilege {
	privs := make([]parser.Privilege, len(privileges))
	for i, name := range privileges {
		privs[i] = parser.NewPrivilege(name, nil)
	}
	return &GrantPrivilege{
		statement:  statement,
		tableName:  QualifiedName{Schema: parser.NewIdent("public", true), Name: parser.NewIdent("users", true)},
		grantees:   grantees,
		privileges: privs,
		objectType: "TABLE",
	}
}

func filteredGrantStatements(t *testing.T, ddls []DDL, config database.GeneratorConfig) []string {
	t.Helper()
	var statements []string
	for _, ddl := range FilterPrivileges(ddls, config) {
		if grant, ok := ddl.(*GrantPrivilege); ok {
			statements = append(statements, grant.Statement())
		}
	}
	return statements
}

// Consolidating two grants must leave a statement that still names both
// grantees: --export prints the statement, and a grantee missing from it is
// revoked when the export is applied back.
func TestFilterPrivilegesKeepsMergedGranteesInStatement(t *testing.T) {
	config := database.GeneratorConfig{ManagedRoles: []string{"app_user", "readonly_user"}}

	tests := []struct {
		name string
		ddls []DDL
		want string
	}{
		{
			name: "quoted grantees",
			ddls: []DDL{
				grantPrivilege(`GRANT SELECT ON TABLE "public"."users" TO "app_user"`, []string{"app_user"}, []string{"SELECT"}),
				grantPrivilege(`GRANT SELECT ON TABLE "public"."users" TO "readonly_user"`, []string{"readonly_user"}, []string{"SELECT"}),
			},
			want: `GRANT SELECT ON TABLE "public"."users" TO "app_user", "readonly_user"`,
		},
		{
			name: "unquoted grantees",
			ddls: []DDL{
				grantPrivilege(`GRANT SELECT ON TABLE public.users TO app_user`, []string{"app_user"}, []string{"SELECT"}),
				grantPrivilege(`GRANT SELECT ON TABLE public.users TO readonly_user`, []string{"readonly_user"}, []string{"SELECT"}),
			},
			want: `GRANT SELECT ON TABLE public.users TO app_user, readonly_user`,
		},
		{
			name: "with grant option stays last",
			ddls: []DDL{
				grantPrivilege(`GRANT SELECT ON TABLE "public"."users" TO "app_user" WITH GRANT OPTION`, []string{"app_user"}, []string{"SELECT"}),
				grantPrivilege(`GRANT SELECT ON TABLE "public"."users" TO "readonly_user" WITH GRANT OPTION`, []string{"readonly_user"}, []string{"SELECT"}),
			},
			want: `GRANT SELECT ON TABLE "public"."users" TO "app_user", "readonly_user" WITH GRANT OPTION`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statements := filteredGrantStatements(t, tt.ddls, config)
			if len(statements) != 1 {
				t.Fatalf("expected the grants to consolidate into one statement, got %d: %v", len(statements), statements)
			}
			if statements[0] != tt.want {
				t.Errorf("statement = %q, want %q", statements[0], tt.want)
			}
		})
	}
}

// A grantee name may contain " TO ", so the grantee list cannot be located by
// searching for that keyword. The spelling is taken from the statement that is
// merged in, which is why both orders are covered.
func TestFilterPrivilegesGranteeNameContainingTo(t *testing.T) {
	config := database.GeneratorConfig{ManagedRoles: []string{"a TO b", "plain_user"}}

	forward := []DDL{
		grantPrivilege(`GRANT SELECT ON TABLE "public"."users" TO "a TO b"`, []string{"a TO b"}, []string{"SELECT"}),
		grantPrivilege(`GRANT SELECT ON TABLE "public"."users" TO "plain_user"`, []string{"plain_user"}, []string{"SELECT"}),
	}
	if got := filteredGrantStatements(t, forward, config); len(got) != 1 || got[0] != `GRANT SELECT ON TABLE "public"."users" TO "a TO b", "plain_user"` {
		t.Errorf("forward order: %v", got)
	}

	reverse := []DDL{
		grantPrivilege(`GRANT SELECT ON TABLE "public"."users" TO "plain_user"`, []string{"plain_user"}, []string{"SELECT"}),
		grantPrivilege(`GRANT SELECT ON TABLE "public"."users" TO "a TO b"`, []string{"a TO b"}, []string{"SELECT"}),
	}
	if got := filteredGrantStatements(t, reverse, config); len(got) != 1 || got[0] != `GRANT SELECT ON TABLE "public"."users" TO "plain_user", "a TO b"` {
		t.Errorf("reverse order: %v", got)
	}
}

// When the statement being merged in names grantees that the configuration
// excludes, its text cannot be reused, so the merge is refused and the previous
// statement is kept rather than naming an unmanaged grantee.
func TestFilterPrivilegesRefusesRewriteWithUnmanagedGrantee(t *testing.T) {
	config := database.GeneratorConfig{ManagedRoles: []string{"app_user", "readonly_user"}}
	ddls := []DDL{
		grantPrivilege(`GRANT SELECT ON TABLE "public"."users" TO "app_user"`, []string{"app_user"}, []string{"SELECT"}),
		grantPrivilege(`GRANT SELECT ON TABLE "public"."users" TO "readonly_user", "other_user"`, []string{"readonly_user", "other_user"}, []string{"SELECT"}),
	}

	statements := filteredGrantStatements(t, ddls, config)
	if len(statements) != 1 {
		t.Fatalf("expected one consolidated statement, got %v", statements)
	}
	if statements[0] != `GRANT SELECT ON TABLE "public"."users" TO "app_user"` {
		t.Errorf("statement = %q, want the original text kept", statements[0])
	}
}
