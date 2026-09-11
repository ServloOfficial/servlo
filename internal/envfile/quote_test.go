package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

// What a .env value may carry unquoted was measured against phpdotenv 5.7,
// which is what Laravel, Statamic, Tempest, CodeIgniter and Grav read these
// files with. Three characters are not allowed through: a space or a tab makes
// it refuse the whole file, and a hash truncates the value at that point with
// nothing said.
//
// The first of those is the one that matters. An SMTP password from Google is
// four groups separated by spaces, and a from name is a company's, so both are
// ordinary answers to the panel's own mail form. Refusing the file is not one
// setting failing: the framework cannot boot, so every page of the site is a
// 500 from the moment the operator pressed Save.
func TestApplyUpdates_QuotesWhatDotenvCannotReadBare(t *testing.T) {
	cases := map[string]struct{ value, want string }{
		"a password from Google":      {"abcd efgh ijkl mnop", "MAIL_PASSWORD='abcd efgh ijkl mnop'"},
		"a hash in a password":        {"p@ss#word", "MAIL_PASSWORD='p@ss#word'"},
		"a tab":                       {"a\tb", "MAIL_PASSWORD='a\tb'"},
		"an apostrophe and a space":   {"o'brien & co", `MAIL_PASSWORD="o'brien & co"`},
		"a dollar, quoted for safety": {"a$b c", `MAIL_PASSWORD='a$b c'`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(path, []byte("APP_NAME=Shop\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := ApplyUpdates(path, map[string]string{"MAIL_PASSWORD": tc.value}); err != nil {
				t.Fatal(err)
			}
			body, _ := os.ReadFile(path)
			if !contains(string(body), tc.want) {
				t.Errorf("wrote:\n%s\nwant a line %s", body, tc.want)
			}
			// And servlo has to read back exactly what it was given, or the
			// panel shows the quotes it added.
			if got := ReadKey(path, "MAIL_PASSWORD"); got != tc.value {
				t.Errorf("read back %q, want %q", got, tc.value)
			}
			if got := ReadValues(path)["MAIL_PASSWORD"]; got != tc.value {
				t.Errorf("ReadValues gave %q, want %q", got, tc.value)
			}
		})
	}
}

// Everything else stays bare, which is what keeps this from rewriting every
// .env on the machine the first time servlo touches one.
func TestApplyUpdates_LeavesAnOrdinaryValueAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("APP_NAME=Shop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyUpdates(path, map[string]string{
		"DB_PASSWORD": "Xq7Lm2Rb9Tz4Kv1Nw8Jy6Hd3Fs5P",
		"DB_HOST":     "servlo-mysql",
		"MAIL_PORT":   "587",
		"APP_URL":     "https://acme-supply.com",
	}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	for _, want := range []string{
		"DB_PASSWORD=Xq7Lm2Rb9Tz4Kv1Nw8Jy6Hd3Fs5P",
		"DB_HOST=servlo-mysql",
		"MAIL_PORT=587",
		"APP_URL=https://acme-supply.com",
	} {
		if !contains(string(body), want) {
			t.Errorf("wrote:\n%s\nwant %s unquoted", body, want)
		}
	}
}

func contains(haystack, needle string) bool {
	for _, line := range splitLinesSimple(haystack) {
		if line == needle {
			return true
		}
	}
	return false
}

func splitLinesSimple(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
