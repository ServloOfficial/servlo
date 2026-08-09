package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func withConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

// The key is generated once and then never changes. A second generation would
// silently orphan every backup taken before it, which is the one failure a
// backup system must not have.
func TestKey_IsCreatedOnceAndReused(t *testing.T) {
	withConfigHome(t)

	first, err := Key()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != KeySize {
		t.Fatalf("key is %d bytes, want %d", len(first), KeySize)
	}
	second, err := Key()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Error("a second call generated a new key, orphaning every backup taken with the first")
	}
}

// It is the one secret that opens every backup this server has ever written,
// so it is not readable by anyone else on the box.
func TestKey_IsWrittenPrivate(t *testing.T) {
	dir := withConfigHome(t)
	if _, err := Key(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "servlo", keyFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("key file is %04o, want 0600", perm)
	}
}

// A key file that is the wrong length is a corrupted or half-written one.
// Using it would encrypt with something nobody can reproduce, so it is refused
// rather than quietly replaced: replacing it is how the old backups become
// unopenable without anyone being told.
func TestKey_RefusesAKeyOfTheWrongLength(t *testing.T) {
	dir := withConfigHome(t)
	if err := os.MkdirAll(filepath.Join(dir, "servlo"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "servlo", keyFile), []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Key(); err == nil {
		t.Error("a truncated key file was accepted")
	}
}
