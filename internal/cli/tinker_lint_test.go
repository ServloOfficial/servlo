package cli

import "testing"

func TestParsePHPLintOutput(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantLen  int
		wantLine int
		wantSev  string
	}{
		{
			"clean output",
			"No syntax errors detected in /dev/stdin\n",
			0, 0, "",
		},
		{
			"parse error",
			`PHP Parse error:  syntax error, unexpected token ";" in /dev/stdin on line 3
Errors parsing /dev/stdin
`,
			1, 3, "error",
		},
		{
			"fatal error",
			`PHP Fatal error:  Cannot redeclare foo() in /dev/stdin on line 5
`,
			1, 5, "error",
		},
		{
			"warning",
			`PHP Warning:  Use of undefined constant FOO in /dev/stdin on line 2
`,
			1, 2, "warning",
		},
		{
			"deprecation downgrades to warning",
			`PHP Deprecated: foo is deprecated in /dev/stdin on line 7
`,
			1, 7, "warning",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parsePHPLintOutput(tc.in)
			if len(got) != tc.wantLen {
				t.Fatalf("got %d diagnostics, want %d: %#v", len(got), tc.wantLen, got)
			}
			if tc.wantLen == 0 {
				return
			}
			if got[0].Line != tc.wantLine {
				t.Errorf("line: got %d want %d", got[0].Line, tc.wantLine)
			}
			if got[0].Severity != tc.wantSev {
				t.Errorf("severity: got %s want %s", got[0].Severity, tc.wantSev)
			}
		})
	}
}
