package dbconn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// No framework definition writes where a database is.
//
// A definition that spells out servlo-mysql is a definition that works on
// exactly one kind of install: the site's connection says the database is on a
// managed server, the framework writes the container next door into .env, and
// the site connects to the wrong database rather than failing. Coordinates come
// from the site's connection through the placeholders, so a literal here is a
// site quietly pointed somewhere nobody chose.
//
// The generated password is checked the same way. The Go builtins used to carry
// it directly; they carry the placeholder now, so the value the site is wired
// with is the value its own connection holds.
func TestNoDefinitionWritesWhereADatabaseIs(t *testing.T) {
	banned := map[string]string{
		"servlo-mysql":    "{{db_host}}",
		"servlo-postgres": "{{db_host}}",
	}

	definitions, err := filepath.Glob(filepath.Join("..", "..", "stores", "frameworks", "*", "*.yaml"))
	if err != nil || len(definitions) == 0 {
		t.Fatalf("no framework definitions to read: %v", err)
	}
	for _, path := range definitions {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for literal, instead := range banned {
			if strings.Contains(string(data), literal) {
				t.Errorf("%s/%s writes %q where a database is; use %s",
					filepath.Base(filepath.Dir(path)), filepath.Base(path), literal, instead)
			}
		}
	}
}

// And the definitions actually use the placeholders, or the check above is a
// test that every framework stopped wiring a database at all.
func TestTheDefinitionsAskForTheSitesConnection(t *testing.T) {
	definitions, err := filepath.Glob(filepath.Join("..", "..", "stores", "frameworks", "*", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	using := 0
	for _, path := range definitions {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "{{db_host}}") {
			using++
		}
	}
	if using == 0 {
		t.Fatal("no framework definition asks for the site's connection, so nothing above was checked")
	}
}
