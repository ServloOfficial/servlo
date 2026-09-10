package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A custom service is the one service definition nobody else has a copy of. The
// store presets can be fetched again; this is the image, the ports and the
// environment somebody typed into the panel once, and the YAML file is the only
// place it exists.
//
// os.WriteFile empties a file before it writes a byte, so a disk that fills in
// between leaves YAML that no longer parses, and the service is gone with
// nothing to restore it from. Replacing the file by rename costs the staging
// file instead.
func TestSaveCustomService_ReplacesTheFileRatherThanRewritingIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)

	svc := &CustomService{Name: "meilisearch", Image: "getmeili/meilisearch:v1.7", Ports: []string{"7700"}}
	if err := SaveCustomService(svc); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}
	path := filepath.Join(CustomServicesDir(), "meilisearch.yaml")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	svc.Description = "search"
	if err := SaveCustomService(svc); err != nil {
		t.Fatalf("SaveCustomService: %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Error("the service file was rewritten in place, so a write that runs out of disk loses a definition nothing else holds")
	}

	reloaded, err := LoadCustomService("meilisearch")
	if err != nil {
		t.Fatalf("LoadCustomService: %v", err)
	}
	if reloaded.Description != "search" {
		t.Errorf("description = %q, want %q", reloaded.Description, "search")
	}

	entries, err := os.ReadDir(CustomServicesDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".yaml" {
			t.Errorf("a staging file was left behind: %s", e.Name())
		}
	}
}
