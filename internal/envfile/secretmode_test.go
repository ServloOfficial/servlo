package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

// The file servlo writes a generated database password into cannot be left
// readable by every account on the machine. CLAUDE.md section 3.7 says 0600 and
// internal/sitefs repeats it as settled, but every writer here used to preserve
// whatever mode it found, so a .env created at 0644 stayed there for the life of
// the site with the password in it.
func TestApplyUpdates_NarrowsAnEnvItWroteCredentialsInto(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("APP_NAME=demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyUpdates(path, map[string]string{"DB_PASSWORD": "generated"}); err != nil {
		t.Fatalf("ApplyUpdates: %v", err)
	}
	assertMode(t, path, SecretMode)
}

// A sync that changes nothing is still servlo looking at the file, and a .env
// created before this was enforced holds the same password today.
func TestApplyUpdates_NarrowsEvenWhenNothingChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("DB_PASSWORD=generated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyUpdates(path, map[string]string{"DB_PASSWORD": "generated"}); err != nil {
		t.Fatalf("ApplyUpdates: %v", err)
	}
	assertMode(t, path, SecretMode)
}

// An operator who took the owner write bit off meant it, and servlo has no
// business handing it back on the way past.
func TestApplyUpdates_LeavesATighterModeAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("APP_NAME=demo\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := ApplyUpdates(path, map[string]string{"APP_NAME": "demo"}); err != nil {
		t.Fatalf("ApplyUpdates: %v", err)
	}
	assertMode(t, path, 0o400)
}

// The PHP-shaped config files carry the same credentials in a different syntax.
func TestApplyPhpConstUpdates_NarrowsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wp-config.php")
	if err := os.WriteFile(path, []byte("<?php\ndefine( 'DB_PASSWORD', 'old' );\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPhpConstUpdates(path, map[string]string{"DB_PASSWORD": "generated"}); err != nil {
		t.Fatalf("ApplyPhpConstUpdates: %v", err)
	}
	assertMode(t, path, SecretMode)
}

func TestApplyPhpArrayUpdates_NarrowsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.php")
	if err := os.WriteFile(path, []byte("<?php\nreturn [\n  'db' => [\n    'password' => 'old',\n  ],\n];\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPhpArrayUpdates(path, map[string]string{"db.password": "generated"}); err != nil {
		t.Fatalf("ApplyPhpArrayUpdates: %v", err)
	}
	assertMode(t, path, SecretMode)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s is %04o, want %04o", filepath.Base(path), got, want)
	}
}
