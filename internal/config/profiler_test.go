package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrGenerateProfilerKey_GeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))

	key, err := LoadOrGenerateProfilerKey()
	if err != nil {
		t.Fatalf("LoadOrGenerateProfilerKey: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("expected 32 hex chars, got %d: %q", len(key), key)
	}
	if _, err := os.Stat(SpxKeyFile()); err != nil {
		t.Errorf("key file not persisted: %v", err)
	}

	key2, err := LoadOrGenerateProfilerKey()
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if key2 != key {
		t.Errorf("key changed between calls: %q != %q", key, key2)
	}
}
