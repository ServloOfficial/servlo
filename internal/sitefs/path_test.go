package sitefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The path jail is the whole security surface of the file manager, so it is
// tested the way an attacker reaches it: one case per shape of escape, and a
// case for each ordinary path that must keep working.

func TestOpenRefusesAPathThatIsNotADirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "index.php")
	writeFile(t, file, "<?php")

	if _, err := Open(file); err == nil {
		t.Fatal("opening a file as a site root should be refused")
	}
}

func TestOpenResolvesASymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	root, err := Open(link)
	if err != nil {
		t.Fatal(err)
	}
	// A root left unresolved would make every containment check compare a
	// resolved child against an unresolved prefix, and refuse everything.
	want, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	if root.Path() != want {
		t.Fatalf("root = %q, want the resolved %q", root.Path(), want)
	}
}

func TestResolveAcceptsPathsInsideTheRoot(t *testing.T) {
	root := newRoot(t, map[string]string{
		"index.php":           "<?php",
		"app/Models/User.php": "<?php",
	})

	for _, rel := range []string{"", ".", "index.php", "app", "app/Models/User.php", "./app/Models"} {
		got, err := root.ResolveExisting(rel)
		if err != nil {
			t.Fatalf("ResolveExisting(%q): %v", rel, err)
		}
		if !strings.HasPrefix(got, root.Path()) {
			t.Fatalf("ResolveExisting(%q) = %q, outside %q", rel, got, root.Path())
		}
	}
}

func TestResolveRefusesADotDotClimb(t *testing.T) {
	root := newRoot(t, map[string]string{"index.php": "<?php"})

	for _, rel := range []string{"..", "../", "../secrets", "app/../../secrets", "a/b/../../../c"} {
		if got, err := root.ResolveExisting(rel); err == nil {
			t.Fatalf("ResolveExisting(%q) = %q, want a refusal", rel, got)
		}
	}
}

func TestResolveRefusesAnAbsolutePath(t *testing.T) {
	root := newRoot(t, map[string]string{"index.php": "<?php"})

	for _, rel := range []string{"/etc/passwd", "/", "//etc/passwd"} {
		if got, err := root.ResolveExisting(rel); err == nil {
			t.Fatalf("ResolveExisting(%q) = %q, want a refusal", rel, got)
		}
	}
}

// A path that is only an escape once it is cleaned. "app/../.." is three
// harmless-looking segments; a check that looked at each segment on its own, or
// that cleaned first and then compared, would let it through.
func TestResolveRefusesAPathThatBecomesAnEscapeAfterCleaning(t *testing.T) {
	root := newRoot(t, map[string]string{"app/config.php": "<?php"})

	for _, rel := range []string{"app/../..", "app/./../../etc", "app/..//../etc/passwd"} {
		if got, err := root.ResolveExisting(rel); err == nil {
			t.Fatalf("ResolveExisting(%q) = %q, want a refusal", rel, got)
		}
	}
}

func TestResolveRefusesASymlinkPointingOutOfTheRoot(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(outside, "secrets.txt"), "top secret")

	site := filepath.Join(base, "site")
	if err := os.Mkdir(site, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secrets.txt"), filepath.Join(site, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(site, "escapedir")); err != nil {
		t.Fatal(err)
	}

	root, err := Open(site)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"escape.txt", "escapedir", "escapedir/secrets.txt"} {
		if got, err := root.ResolveExisting(rel); err == nil {
			t.Fatalf("ResolveExisting(%q) = %q, want a refusal", rel, got)
		}
	}
}

// A new file under a directory symlink that leaves the root: the final
// component does not exist, so only the parent can be resolved, and that is
// exactly the case a naive "resolve what exists" check gets wrong.
func TestResolveNewRefusesAParentThatLeavesTheRoot(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	site := filepath.Join(base, "site")
	if err := os.Mkdir(site, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(site, "escapedir")); err != nil {
		t.Fatal(err)
	}

	root, err := Open(site)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := root.ResolveNew("escapedir/planted.php"); err == nil {
		t.Fatalf("ResolveNew = %q, want a refusal", got)
	}
}

func TestResolveNewAcceptsAFileThatDoesNotExistYet(t *testing.T) {
	root := newRoot(t, map[string]string{"app/keep.txt": "x"})

	got, err := root.ResolveNew("app/new.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(root.Path(), "app", "new.txt") {
		t.Fatalf("ResolveNew = %q", got)
	}
}

func TestResolveRefusesANULByte(t *testing.T) {
	root := newRoot(t, map[string]string{"index.php": "<?php"})

	if _, err := root.ResolveExisting("index.php\x00.txt"); err == nil {
		t.Fatal("a path with a NUL byte should be refused")
	}
}

func TestResolveExistingRefusesSomethingThatIsNotThere(t *testing.T) {
	root := newRoot(t, map[string]string{"index.php": "<?php"})

	if _, err := root.ResolveExisting("missing.php"); err == nil {
		t.Fatal("a missing path should be refused by ResolveExisting")
	}
}

// A sibling directory whose name starts with the root's name is not inside the
// root, however much the two strings look alike. Without the separator in the
// containment check, "sites/acme-backup" reads as a child of "sites/acme".
func TestResolveRefusesASymlinkToASiblingWithTheSameNamePrefix(t *testing.T) {
	base := t.TempDir()
	site := filepath.Join(base, "acme")
	sibling := filepath.Join(base, "acme-backup")
	for _, dir := range []string{site, sibling} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(sibling, "dump.sql"), "-- secrets")
	if err := os.Symlink(filepath.Join(sibling, "dump.sql"), filepath.Join(site, "peek.sql")); err != nil {
		t.Fatal(err)
	}

	root, err := Open(site)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := root.ResolveExisting("peek.sql"); err == nil {
		t.Fatalf("ResolveExisting = %q, want a refusal", got)
	}
}

// A root whose own path contains a symlinked ancestor still has to accept its
// own children: the containment check compares resolved against resolved.
func TestResolveWorksUnderASymlinkedAncestor(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(filepath.Join(real, "site", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(real, "site", "app", "x.php"), "<?php")
	if err := os.Symlink(real, filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}

	root, err := Open(filepath.Join(base, "link", "site"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.ResolveExisting("app/x.php"); err != nil {
		t.Fatalf("ResolveExisting: %v", err)
	}
}

func newRoot(t *testing.T, files map[string]string) Root {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, full, body)
	}
	root, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
