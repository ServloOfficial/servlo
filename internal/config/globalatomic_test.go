package config

import (
	"os"
	"path/filepath"
	"testing"
)

// config.yaml is the machine: the parked directories, the panel's domain, the
// ACME contact, the service image overrides, the workspaces. os.WriteFile empties
// a file before it writes a byte, so a disk that fills in between leaves whatever
// fit, and a truncated config.yaml does not even fail to parse: it reads as
// defaults, so the parked directory silently becomes the built-in one and the
// panel's domain silently becomes none, which is a vhost removed at the next
// start.
//
// Replacing the file by rename is what makes a write that cannot finish cost the
// temporary file instead of the configuration. That is what this checks: the
// file the operator ends up with is a different file, not the old one rewritten
// in place.
func TestSaveGlobal_ReplacesTheFileRatherThanRewritingIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)

	cfg := &GlobalConfig{ParkedDirectories: []string{"/srv/sites"}}
	if err := SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	before, err := os.Stat(GlobalConfigFile())
	if err != nil {
		t.Fatal(err)
	}

	cfg.UI.Domain = "panel.example.com"
	if err := SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	after, err := os.Stat(GlobalConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Error("config.yaml was rewritten in place, so a write that runs out of disk truncates the machine's configuration")
	}

	// And no staging file left beside it.
	entries, err := os.ReadDir(ConfigDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("a staging file was left behind: %s", e.Name())
		}
	}

	reloaded, err := LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	if reloaded.UI.Domain != "panel.example.com" {
		t.Errorf("the configuration did not survive the write: domain = %q", reloaded.UI.Domain)
	}
}
