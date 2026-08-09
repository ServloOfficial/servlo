package sitefs

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListSortsDirectoriesFirstAndReportsModes(t *testing.T) {
	root := newRoot(t, map[string]string{
		"index.php":    "<?php",
		"README.md":    "# hi",
		"app/User.php": "<?php",
	})
	if err := os.Mkdir(filepath.Join(root.Path(), "storage"), 0o755); err != nil {
		t.Fatal(err)
	}

	listing, err := root.List("")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range listing.Entries {
		names = append(names, e.Name)
	}
	// Directories first, then case-insensitive by name: an operator looking for
	// a file does not think about which of two names is capitalised.
	want := []string{"app", "storage", "index.php", "README.md"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %v, want %v", names, want)
	}
	for _, e := range listing.Entries {
		if e.Mode == "" {
			t.Fatalf("%s has no mode", e.Name)
		}
	}
}

func TestListMarksASymlinkThatLeavesTheRootWithoutFollowingIt(t *testing.T) {
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
	root, err := Open(site)
	if err != nil {
		t.Fatal(err)
	}

	listing, err := root.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(listing.Entries))
	}
	e := listing.Entries[0]
	if !e.Symlink || !e.Escapes {
		t.Fatalf("entry = %+v, want a symlink marked as leaving the root", e)
	}
}

func TestListRefusesToLeaveTheRoot(t *testing.T) {
	root := newRoot(t, map[string]string{"index.php": "<?php"})
	if _, err := root.List("../"); err == nil {
		t.Fatal("listing above the root should be refused")
	}
}

func TestReadReturnsTextAndRefusesADirectory(t *testing.T) {
	root := newRoot(t, map[string]string{"index.php": "<?php echo 1;"})

	content, err := root.Read("index.php", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if content.Text != "<?php echo 1;" || content.Binary {
		t.Fatalf("content = %+v", content)
	}
	if _, err := root.Read("", 1<<20); err == nil {
		t.Fatal("reading a directory should be refused")
	}
}

func TestReadFlagsBinaryAndTruncates(t *testing.T) {
	root := newRoot(t, nil)
	writeFile(t, filepath.Join(root.Path(), "logo.png"), "\x89PNG\x00\x00binary")
	writeFile(t, filepath.Join(root.Path(), "big.txt"), strings.Repeat("a", 100))

	binary, err := root.Read("logo.png", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !binary.Binary || binary.Text != "" {
		t.Fatalf("binary read = %+v, want no text", binary)
	}
	truncated, err := root.Read("big.txt", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated.Truncated || len(truncated.Text) != 10 {
		t.Fatalf("truncated read = %+v", truncated)
	}
}

func TestWriteSavesAndKeepsSecretsOwnerOnly(t *testing.T) {
	root := newRoot(t, map[string]string{"index.php": "<?php old"})

	if err := root.Write("index.php", []byte("<?php new")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(root.Path(), "index.php")); got != "<?php new" {
		t.Fatalf("content = %q", got)
	}
	if err := root.Write(".env", []byte("APP_KEY=x")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root.Path(), ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf(".env mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestWriteRefusesToEscapeAndRefusesADirectory(t *testing.T) {
	root := newRoot(t, map[string]string{"app/x.php": "<?php"})

	if err := root.Write("../planted.php", []byte("x")); err == nil {
		t.Fatal("writing above the root should be refused")
	}
	if err := root.Write("app", []byte("x")); err == nil {
		t.Fatal("writing over a directory should be refused")
	}
}

func TestDeleteRemovesALinkRatherThanWhatItPointsAt(t *testing.T) {
	root := newRoot(t, map[string]string{"real.txt": "keep me"})
	if err := os.Symlink(filepath.Join(root.Path(), "real.txt"), filepath.Join(root.Path(), "link.txt")); err != nil {
		t.Fatal(err)
	}

	if err := root.Delete("link.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root.Path(), "link.txt")); !os.IsNotExist(err) {
		t.Fatal("the link should be gone")
	}
	if readFile(t, filepath.Join(root.Path(), "real.txt")) != "keep me" {
		t.Fatal("deleting a link must not delete what it points at")
	}
}

func TestDeleteRefusesTheSiteRootAndAnythingOutside(t *testing.T) {
	root := newRoot(t, map[string]string{"index.php": "<?php"})

	for _, rel := range []string{"", ".", "../", "/etc/passwd"} {
		if err := root.Delete(rel); err == nil {
			t.Fatalf("Delete(%q) should be refused", rel)
		}
	}
	if _, err := os.Stat(root.Path()); err != nil {
		t.Fatal("the site root should still be there")
	}
}

func TestUploadRefusesANameWithAPathInIt(t *testing.T) {
	root := newRoot(t, nil)

	for _, name := range []string{"../escape.php", "a/b.php", "", ".", "..", "x\x00.php"} {
		if _, err := root.Upload("", name, strings.NewReader("x"), 1<<20); err == nil {
			t.Fatalf("Upload(%q) should be refused", name)
		}
	}
}

// An upload names a file, not a place to put it. The directory comes from the
// separate path field, which has already been through the jail, so a name that
// carries its own directory is refused even when that directory is a perfectly
// ordinary one inside the site.
func TestUploadRefusesANameThatPicksItsOwnDirectory(t *testing.T) {
	root := newRoot(t, map[string]string{"public/keep": "x"})

	if _, err := root.Upload("", "public/app.js", strings.NewReader("x"), 1<<20); err == nil {
		t.Fatal("a filename carrying a directory should be refused")
	}
	if _, err := os.Stat(filepath.Join(root.Path(), "public", "app.js")); !os.IsNotExist(err) {
		t.Fatal("the refused upload was written anyway")
	}
}

func TestUploadStopsAtTheSizeCeiling(t *testing.T) {
	root := newRoot(t, nil)

	if _, err := root.Upload("", "big.bin", strings.NewReader(strings.Repeat("a", 50)), 10); err == nil {
		t.Fatal("an upload past the ceiling should be refused")
	}
	// Nothing half-written is left behind for the operator to clear up.
	if _, err := os.Stat(filepath.Join(root.Path(), "big.bin")); !os.IsNotExist(err) {
		t.Fatal("a refused upload should leave no file")
	}
}

func TestUploadWritesIntoASubdirectory(t *testing.T) {
	root := newRoot(t, map[string]string{"public/keep": "x"})

	written, err := root.Upload("public", "app.js", strings.NewReader("console.log(1)"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if written != 14 {
		t.Fatalf("written = %d", written)
	}
	if readFile(t, filepath.Join(root.Path(), "public", "app.js")) != "console.log(1)" {
		t.Fatal("upload did not land")
	}
}

func TestUnzipExtractsIntoADestinationThatIsAlreadyPopulated(t *testing.T) {
	root := newRoot(t, map[string]string{"plugins/existing.txt": "keep"})
	archive := buildZip(t, map[string]string{
		"acme/acme.php":      "<?php",
		"acme/assets/app.js": "console.log(1)",
	})

	res, err := root.Unzip("plugins", bytes.NewReader(archive), int64(len(archive)), UnzipLimits{MaxBytes: 1 << 20, MaxFiles: 100})
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 2 {
		t.Fatalf("files = %d, want 2", res.Files)
	}
	// The wrapper directory is kept: unzipping a plugin into wp-content/plugins
	// is the case, and flattening it would install the plugin wrong.
	if readFile(t, filepath.Join(root.Path(), "plugins", "acme", "acme.php")) != "<?php" {
		t.Fatal("archive did not extract with its wrapper directory")
	}
	if readFile(t, filepath.Join(root.Path(), "plugins", "existing.txt")) != "keep" {
		t.Fatal("extraction should not clear the destination")
	}
}

func TestUnzipRefusesAnEntryThatEscapesTheDestination(t *testing.T) {
	root := newRoot(t, map[string]string{"public/keep": "x"})
	for _, name := range []string{"../../planted.php", "/etc/planted.php", `..\planted.php`} {
		archive := buildZip(t, map[string]string{name: "<?php system($_GET[0]);"})
		if _, err := root.Unzip("public", bytes.NewReader(archive), int64(len(archive)), UnzipLimits{MaxBytes: 1 << 20, MaxFiles: 100}); err == nil {
			t.Fatalf("zip slip via %q should be refused", name)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root.Path()), "planted.php")); !os.IsNotExist(err) {
		t.Fatal("a refused archive must write nothing outside the root")
	}
}

func TestUnzipRefusesAnEntryThatEscapesThroughASymlinkedDestination(t *testing.T) {
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
	archive := buildZip(t, map[string]string{"payload.php": "<?php"})

	if _, err := root.Unzip("escapedir", bytes.NewReader(archive), int64(len(archive)), UnzipLimits{MaxBytes: 1 << 20, MaxFiles: 100}); err == nil {
		t.Fatal("extracting into a symlink that leaves the root should be refused")
	}
}

// The destination is a perfectly ordinary directory inside the site; the way
// out is a symlink one level down, reached by an entry name that is not itself
// a traversal. Only a check on where each entry actually lands catches this.
func TestUnzipRefusesAnEntryThatLandsThroughASymlinkInsideTheDestination(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	site := filepath.Join(base, "site")
	if err := os.MkdirAll(filepath.Join(site, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(site, "public", "uploads")); err != nil {
		t.Fatal(err)
	}
	root, err := Open(site)
	if err != nil {
		t.Fatal(err)
	}
	archive := buildZip(t, map[string]string{"uploads/payload.php": "<?php system($_GET[0]);"})

	if _, err := root.Unzip("public", bytes.NewReader(archive), int64(len(archive)), UnzipLimits{MaxBytes: 1 << 20, MaxFiles: 100}); err == nil {
		t.Fatal("an entry landing through a symlink inside the destination should be refused")
	}
	if _, err := os.Stat(filepath.Join(outside, "payload.php")); !os.IsNotExist(err) {
		t.Fatal("the entry was written outside the site")
	}
}

func TestSafeEntryNameRefusesEveryShapeOfZipSlip(t *testing.T) {
	for _, name := range []string{
		"", "\x00", "a\x00b", `..\evil.php`, `..\..\evil.php`,
		"/etc/passwd", "..", "../evil.php", "a/../../evil.php",
	} {
		if err := SafeEntryName(name); err == nil {
			t.Fatalf("SafeEntryName(%q) should be refused", name)
		}
	}
	for _, name := range []string{"a.php", "acme/a.php", "acme/", "./acme/a.php", "a..b/c.php"} {
		if err := SafeEntryName(name); err != nil {
			t.Fatalf("SafeEntryName(%q): %v", name, err)
		}
	}
}

func TestUnzipRefusesAnArchiveOverTheCeilings(t *testing.T) {
	root := newRoot(t, nil)
	archive := buildZip(t, map[string]string{"a.txt": strings.Repeat("a", 500), "b.txt": "b"})

	if _, err := root.Unzip("", bytes.NewReader(archive), int64(len(archive)), UnzipLimits{MaxBytes: 100, MaxFiles: 100}); err == nil {
		t.Fatal("an archive over the byte ceiling should be refused")
	}
	if _, err := root.Unzip("", bytes.NewReader(archive), int64(len(archive)), UnzipLimits{MaxBytes: 1 << 20, MaxFiles: 1}); err == nil {
		t.Fatal("an archive over the file ceiling should be refused")
	}
	if _, err := os.Stat(filepath.Join(root.Path(), "a.txt")); !os.IsNotExist(err) {
		t.Fatal("a refused archive should write nothing")
	}
}

func TestPermissionPlanNamesEveryRuleItWouldApply(t *testing.T) {
	root := newRoot(t, map[string]string{
		"index.php":   "<?php",
		".env":        "APP_KEY=x",
		"app/x.php":   "<?php",
		"artisan":     "#!/usr/bin/env php",
		"certs/a.pem": "-----BEGIN",
	})
	chmod(t, root, "index.php", 0o666)
	chmod(t, root, ".env", 0o644)
	chmod(t, root, "app", 0o777)
	chmod(t, root, "artisan", 0o744)
	chmod(t, root, "certs/a.pem", 0o644)

	plan, err := root.PermissionPlan()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Rule{}
	for _, rule := range plan.Rules {
		byName[rule.Name] = rule
	}
	for name, wantMode := range map[string]fs.FileMode{
		RuleDirectories: 0o755,
		RuleFiles:       0o644,
		RuleExecutables: 0o755,
		RuleSecrets:     0o600,
	} {
		rule, ok := byName[name]
		if !ok {
			t.Fatalf("plan has no %q rule", name)
		}
		if rule.Mode != wantMode {
			t.Fatalf("%s mode = %v, want %v", name, rule.Mode, wantMode)
		}
		if rule.Reason == "" {
			t.Fatalf("%s has no reason", name)
		}
	}
	if byName[RuleSecrets].Count != 2 {
		t.Fatalf("secrets count = %d, want .env and the pem", byName[RuleSecrets].Count)
	}
	if byName[RuleExecutables].Count != 1 {
		t.Fatalf("executables count = %d, want artisan", byName[RuleExecutables].Count)
	}
	if plan.Changes == 0 {
		t.Fatal("plan reports no changes but every file is wrong")
	}
}

func TestApplyPermissionsSetsExactlyWhatThePlanSaid(t *testing.T) {
	root := newRoot(t, map[string]string{
		"index.php": "<?php",
		".env":      "APP_KEY=x",
		"app/x.php": "<?php",
		"artisan":   "#!/usr/bin/env php",
	})
	chmod(t, root, "index.php", 0o666)
	chmod(t, root, ".env", 0o644)
	chmod(t, root, "app", 0o777)
	chmod(t, root, "artisan", 0o744)

	if _, err := root.ApplyPermissions(); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]fs.FileMode{
		"index.php": 0o644,
		".env":      0o600,
		"app":       0o755,
		"app/x.php": 0o644,
		"artisan":   0o755,
	} {
		info, err := os.Stat(filepath.Join(root.Path(), filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode = %v, want %v", rel, info.Mode().Perm(), want)
		}
	}
}

// Git keeps its own modes and a chmod pass over .git is how a repository ends
// up with hooks that no longer run. Symlinks are never chased, so nothing
// outside the root can be reached through one.
func TestPermissionsSkipsGitAndDoesNotFollowSymlinks(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(outside, "victim.sh"), "#!/bin/sh")
	if err := os.Chmod(filepath.Join(outside, "victim.sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	site := filepath.Join(base, "site")
	if err := os.MkdirAll(filepath.Join(site, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(site, ".git", "hooks", "pre-commit"), "#!/bin/sh")
	if err := os.Chmod(filepath.Join(site, ".git", "hooks", "pre-commit"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(site, "escapedir")); err != nil {
		t.Fatal(err)
	}
	root, err := Open(site)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := root.ApplyPermissions(); err != nil {
		t.Fatal(err)
	}
	hook, err := os.Stat(filepath.Join(site, ".git", "hooks", "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}
	if hook.Mode().Perm() != 0o700 {
		t.Fatalf("git hook mode = %v, want it left alone", hook.Mode().Perm())
	}
	victim, err := os.Stat(filepath.Join(outside, "victim.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if victim.Mode().Perm() != 0o700 {
		t.Fatalf("a file outside the root was chmodded to %v", victim.Mode().Perm())
	}
	// The directory the link points at is the one chmod would reach through
	// the link itself, so it is the mode that proves the link was not followed.
	outsideDir, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if outsideDir.Mode().Perm() != 0o700 {
		t.Fatalf("a directory outside the root was chmodded to %v", outsideDir.Mode().Perm())
	}
}

func chmod(t *testing.T, root Root, rel string, mode fs.FileMode) {
	t.Helper()
	if err := os.Chmod(filepath.Join(root.Path(), filepath.FromSlash(rel)), mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
