package postgres

import "testing"

// A double-quoted PostgreSQL identifier escapes an embedded double quote by
// doubling it, so forceQuoteIdentifier must do the same. The scanner already
// collapses "" back to a single " when it reads a quoted identifier, which means
// a name such as a"b reaches DDL output with a raw " in it. Re-emitting that
// without doubling terminates the identifier early and produces invalid SQL.
func TestForceQuoteIdentifierEscapesDoubleQuote(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "plain", `"plain"`},
		{"embedded double quote", `a"b`, `"a""b"`},
		{"only a double quote", `"`, `""""`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := forceQuoteIdentifier(tc.in); got != tc.want {
				t.Errorf("forceQuoteIdentifier(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
