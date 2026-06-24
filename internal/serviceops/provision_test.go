package serviceops

import "testing"

// These guard the DB identifier/literal quoting used by CreateDatabase and
// DropDatabase. A worktree DB name derives from a git branch, and git allows
// quotes and backticks in branch names, so a name reaching these sinks must not
// be able to terminate its quoting and inject SQL.
func TestEscapeIdentBacktick(t *testing.T) {
	cases := []struct{ in, want string }{
		{"acme_app", "acme_app"},
		{"a`b", "a``b"},
		{"`; DROP DATABASE x; --", "``; DROP DATABASE x; --"},
		{"plain", "plain"},
	}
	for _, tc := range cases {
		if got := escapeIdentBacktick(tc.in); got != tc.want {
			t.Errorf("escapeIdentBacktick(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEscapeIdentDQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"acme_app", "acme_app"},
		{`a"b`, `a""b`},
		{`x"; DROP DATABASE other; --`, `x""; DROP DATABASE other; --`},
	}
	for _, tc := range cases {
		if got := escapeIdentDQuote(tc.in); got != tc.want {
			t.Errorf("escapeIdentDQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEscapeSQLLiteral(t *testing.T) {
	cases := []struct{ in, want string }{
		{"acme_app", "acme_app"},
		{"a'b", "a''b"},
		{"' OR '1'='1", "'' OR ''1''=''1"},
	}
	for _, tc := range cases {
		if got := escapeSQLLiteral(tc.in); got != tc.want {
			t.Errorf("escapeSQLLiteral(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
