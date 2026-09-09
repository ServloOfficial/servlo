package dbconn

import (
	"os"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// databases.yaml is where an external database's address and the password that
// opens it are written down, and there is no second copy: a site's .env carries
// the account servlo made for that site, not the administrator this file holds.
// os.WriteFile empties a file before it writes a byte, so a write that cannot
// finish would leave the operator with a registry that no longer names their
// managed database, and nothing to put back.
func TestSaveRegistry_ReplacesTheFileRatherThanRewritingIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)

	reg := &Registry{Connections: []Connection{
		External("managed", "mysql", "db.example.com", 25060, "doadmin", "secret"),
	}}
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
	path := registryFile()
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	reg.Default = "managed"
	if err := SaveRegistry(reg); err != nil {
		t.Fatalf("SaveRegistry: %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Error("databases.yaml was rewritten in place, so a write that runs out of disk takes the external database credentials with it")
	}
	// Still 0600: an external database's password is in it.
	if mode := after.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600", mode)
	}
	if _, err := config.LoadGlobal(); err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	reloaded, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if reloaded.Default != "managed" || len(reloaded.Connections) != 1 {
		t.Errorf("the registry did not survive the write: %+v", reloaded)
	}
}
