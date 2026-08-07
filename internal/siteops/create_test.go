package siteops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareSiteDirectory_CreatesAMissingDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "example.com")

	res, err := PrepareSiteDirectory(target)
	if err != nil {
		t.Fatalf("PrepareSiteDirectory: %v", err)
	}
	if !res.Created {
		t.Error("Created = false, want true for a directory that did not exist")
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		t.Fatalf("the directory was not created: %v", err)
	}
	// The site's own files live here and the whole machine runs as one user, so
	// the only reader that matters is the one running servlo.
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Errorf("mode = %o, want 755", perm)
	}
}

// An add-site flow pointed at a project that is already on disk is the story's
// headline case, so an existing directory is not an error and is not touched.
func TestPrepareSiteDirectory_AcceptsAnExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := PrepareSiteDirectory(dir)
	if err != nil {
		t.Fatalf("PrepareSiteDirectory: %v", err)
	}
	if res.Created {
		t.Error("Created = true for a directory that already existed")
	}
	if !res.HasContent {
		t.Error("HasContent = false, want true when the directory holds files")
	}
	if _, err := os.Stat(filepath.Join(dir, "index.php")); err != nil {
		t.Errorf("an existing file was disturbed: %v", err)
	}
}

// Creating one missing level is convenience; creating a whole tree means a
// mistyped path silently becomes a new directory somewhere nobody looks.
func TestPrepareSiteDirectory_RefusesAMissingParent(t *testing.T) {
	target := filepath.Join(t.TempDir(), "no", "such", "parent", "example.com")

	_, err := PrepareSiteDirectory(target)
	if err == nil {
		t.Fatal("a path with a missing parent was accepted")
	}
	if !strings.Contains(err.Error(), "parent") {
		t.Errorf("error = %q, does not say the parent is missing", err)
	}
}

func TestPrepareSiteDirectory_RefusesAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := PrepareSiteDirectory(path); err == nil {
		t.Fatal("a regular file was accepted as a site directory")
	}
}

// A relative path resolves against whatever the process happens to be sitting
// in, which for a panel handler is not a place anybody chose.
func TestPrepareSiteDirectory_RefusesARelativePath(t *testing.T) {
	if _, err := PrepareSiteDirectory("sites/example.com"); err == nil {
		t.Fatal("a relative path was accepted")
	}
}

func TestInspectSiteDirectory_ReportsWhatTheFormShouldPrefill(t *testing.T) {
	dir := t.TempDir()
	public := filepath.Join(dir, "public")
	if err := os.MkdirAll(public, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(public, "index.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := InspectSiteDirectory(dir)
	if err != nil {
		t.Fatalf("InspectSiteDirectory: %v", err)
	}
	if !got.Exists {
		t.Error("Exists = false for a directory that is there")
	}
	if got.PublicDir != "public" {
		t.Errorf("PublicDir = %q, want public", got.PublicDir)
	}
}

// The form has to be able to ask about a path before it exists, which is the
// whole point of inspecting rather than linking: nothing is created here.
func TestInspectSiteDirectory_DescribesAPathThatIsNotThereYet(t *testing.T) {
	target := filepath.Join(t.TempDir(), "example.com")

	got, err := InspectSiteDirectory(target)
	if err != nil {
		t.Fatalf("InspectSiteDirectory: %v", err)
	}
	if got.Exists {
		t.Error("Exists = true for a path that does not exist")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("inspecting created the directory")
	}
	// Nothing on disk means nothing to detect, and the form should show empty
	// fields rather than a guess it cannot support.
	if got.Framework != "" {
		t.Errorf("Framework = %q, want empty for an absent directory", got.Framework)
	}
}
