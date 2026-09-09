package sitefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fuzzRoot builds a site directory with everything a caller could try to walk
// out through: a real subtree, a symlink to a directory outside the site, a
// symlink to a file outside it, and one pointing at the root of the machine.
func fuzzRoot(t *testing.T) (Root, string) {
	t.Helper()
	base := t.TempDir()
	site := filepath.Join(base, "site")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{
		filepath.Join(site, "app", "Http"),
		filepath.Join(site, "storage"),
		filepath.Join(outside, "secrets"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secrets", "key.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "app", "index.php"), []byte("<?php"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, link := range []struct{ from, to string }{
		{filepath.Join(site, "escape"), outside},
		{filepath.Join(site, "escape-file"), filepath.Join(outside, "secrets", "key.txt")},
		{filepath.Join(site, "storage", "up"), ".."},
		{filepath.Join(site, "root"), string(os.PathSeparator)},
	} {
		if err := os.Symlink(link.to, link.from); err != nil {
			t.Fatal(err)
		}
	}
	r, err := Open(site)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(site)
	if err != nil {
		t.Fatal(err)
	}
	return r, resolved
}

// inside is the invariant every resolver owes its caller: whatever it hands
// back is the site directory or something under it. A path that escapes is the
// file manager reading or writing somebody else's files, and on a machine where
// every site runs as one Linux user that is every other site on it.
func inside(root, abs string) bool {
	return abs == root || strings.HasPrefix(abs, root+string(os.PathSeparator))
}

func FuzzResolveStaysInsideTheSite(f *testing.F) {
	for _, seed := range []string{
		"", ".", "/", "app", "app/index.php", "app/../storage",
		"..", "../..", "../outside/secrets/key.txt",
		"escape", "escape/secrets/key.txt", "escape-file", "root/etc/passwd",
		"storage/up/../outside", "storage/up/up/outside",
		"app/./../../outside", "//outside", `app\..\..\outside`,
		"app/\x00/../outside", " ..", "...", "....//outside",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, rel string) {
		r, root := fuzzRoot(t)
		for _, c := range []struct {
			name string
			fn   func(string) (string, error)
		}{
			{"ResolveExisting", r.ResolveExisting},
			{"ResolveNew", r.ResolveNew},
			{"ResolveLeaf", r.ResolveLeaf},
		} {
			got, err := c.fn(rel)
			if err != nil {
				continue
			}
			if !inside(root, got) {
				t.Fatalf("%s(%q) resolved to %q, which is outside %q", c.name, rel, got, root)
			}
		}
	})
}
