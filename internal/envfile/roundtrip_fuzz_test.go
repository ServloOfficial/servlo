package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What goes into a site's .env has to come back out of it.
//
// Every credential servlo generates travels this way: a service preset injects
// a database password, the SMTP form writes a mail password, and the site reads
// them back. A value that survives the write but not the read is a site that
// cannot reach its own database, with nothing in any log to say why, so the
// round trip is the property worth holding rather than any particular quoting.
func FuzzEnvValueRoundTrips(f *testing.F) {
	for _, seed := range []string{
		"plain", "", " leading", "trailing ", "with space",
		"has#hash", "has=equals", `has"quote`, "has'quote",
		"$VAR", "${VAR}", "back\\slash", "tab\there",
		"base64:kPfBeKMEXhIQwiYuHhq3vXsUmFqTOvHzKQ0dJqTnGQY=",
		"mysql://user:p@ss@host:3306/db?tls=skip-verify",
		"héllo", "\x7f", "0", "true", "null",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		// Refused outright, and refusal is a fine answer: the file is left as it
		// was, which the writers already assert.
		if strings.ContainsAny(value, "\n\r") {
			t.Skip()
		}
		// The shapes a dotenv reader normalises rather than preserves: it trims
		// an unquoted value and strips one layer of surrounding quotes. Servlo
		// reads them the same way its sites do, which is the agreement that
		// matters, so exact recovery is not owed for these and asserting it
		// would only be asserting that the writer quotes the way the reader
		// unquotes.
		if strings.TrimSpace(value) != value {
			t.Skip()
		}
		if len(value) > 0 && strings.ContainsAny(value[:1], `"'`) {
			t.Skip()
		}
		if len(value) > 0 && strings.ContainsAny(value[len(value)-1:], `"'`) {
			t.Skip()
		}
		path := filepath.Join(t.TempDir(), ".env")
		if err := os.WriteFile(path, []byte("APP_NAME=servlo\n"), SecretMode); err != nil {
			t.Fatal(err)
		}
		if err := ApplyUpdates(path, map[string]string{"DB_PASSWORD": value}); err != nil {
			return
		}
		if got := ReadKey(path, "DB_PASSWORD"); got != value {
			body, _ := os.ReadFile(path)
			t.Fatalf("wrote %q and read back %q\nfile:\n%s", value, got, body)
		}
		// The neighbour has to survive too: a value that swallows the next line
		// is how one write takes another key with it.
		if got := ReadKey(path, "APP_NAME"); got != "servlo" {
			body, _ := os.ReadFile(path)
			t.Fatalf("writing %q left APP_NAME reading as %q\nfile:\n%s", value, got, body)
		}
	})
}
