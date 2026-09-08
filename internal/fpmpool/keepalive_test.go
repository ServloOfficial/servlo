package fpmpool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The pool directory is bind-mounted over /usr/local/etc/php-fpm.d, which is
// where the image keeps its own pool, so the mount hides it. An empty directory
// therefore means php-fpm starts with no pool at all, exits, and systemd
// restarts it forever.
//
// That is not a hypothetical: a fresh `servlo install` has no sites, so it
// wrote no pools, so it left PHP-FPM crash-looping while reporting success.
// Every CI job before this one created a site immediately, which wrote a pool
// and hid the bug.
func TestWriteKeepaliveLeavesAPoolInAnOtherwiseEmptyDir(t *testing.T) {
	dir := t.TempDir()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("the fixture directory should start empty, has %d entries", len(entries))
	}

	path, err := WriteKeepalive(dir)
	if err != nil {
		t.Fatalf("WriteKeepalive: %v", err)
	}
	if path != KeepalivePath(dir) {
		t.Errorf("WriteKeepalive wrote %q, KeepalivePath says %q", path, KeepalivePath(dir))
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the keepalive: %v", err)
	}
	if !strings.Contains(string(body), "[servlo-keepalive]") {
		t.Errorf("the keepalive declares no pool section:\n%s", body)
	}
	// A pool with no listen is a pool php-fpm will not start with either.
	if !strings.Contains(string(body), "listen = ") {
		t.Errorf("the keepalive pool has no listen directive:\n%s", body)
	}
}

// It sorts ahead of a site's pool, so somebody reading the directory meets the
// explanation before the pools it stands in for.
func TestKeepaliveSortsBeforeASitePool(t *testing.T) {
	dir := "/pools"
	if filepath.Base(KeepalivePath(dir)) >= filepath.Base(Path(dir, "acme")) {
		t.Errorf("%q should sort before %q",
			filepath.Base(KeepalivePath(dir)), filepath.Base(Path(dir, "acme")))
	}
}

// Writing it twice is what a restart does, and it must not accumulate or fail.
func TestWriteKeepaliveIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		if _, err := WriteKeepalive(dir); err != nil {
			t.Fatalf("WriteKeepalive pass %d: %v", i, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("three writes left %d files, want 1", len(entries))
	}
}
