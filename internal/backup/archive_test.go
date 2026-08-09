package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realrashid/servlo/internal/config"
)

// siteTree lays down a small site: real content, a regenerable directory the
// exclude list should drop, and a nested file so the walk has to recurse.
func siteTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.php", "<?php echo 'live';")
	write(".env", "APP_KEY=secret")
	write("app/Models/User.php", "<?php class User {}")
	write("vendor/autoload.php", "regenerable")
	write("node_modules/left-pad/index.js", "regenerable")
	return root
}

func testOptions(t *testing.T, root string) Options {
	t.Helper()
	return Options{
		Site:     config.Site{Name: "acme", Domains: []string{"acme-supply.com"}, Path: root, PHPVersion: "8.4"},
		Excludes: []string{"vendor", "node_modules"},
		Taken:    time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
	}
}

// entries reads a written archive back and returns its manifest and the tar
// paths it carries, which is what every assertion below is about.
func entries(t *testing.T, sealed []byte, key []byte) (Manifest, map[string]string) {
	t.Helper()
	dec, err := NewDecryptor(bytes.NewReader(sealed), key)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(dec)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	files := map[string]string{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		files[h.Name] = string(body)
	}
	man, err := ParseManifest([]byte(files[ManifestName]))
	if err != nil {
		t.Fatalf("no readable manifest in the archive: %v", err)
	}
	return man, files
}

func TestCreate_CarriesTheSiteFilesAndTheDatabase(t *testing.T) {
	root := siteTree(t)
	key := testKey(t)
	opts := testOptions(t, root)
	opts.Database = func(w io.Writer) error {
		_, err := io.WriteString(w, "CREATE TABLE users (id int);")
		return err
	}
	opts.DatabaseName = "acme"

	var sealed bytes.Buffer
	if _, err := Create(&sealed, key, opts); err != nil {
		t.Fatal(err)
	}
	man, files := entries(t, sealed.Bytes(), key)

	for _, want := range []string{"files/index.php", "files/.env", "files/app/Models/User.php"} {
		if _, ok := files[want]; !ok {
			t.Errorf("the archive is missing %s", want)
		}
	}
	if got := files["files/index.php"]; got != "<?php echo 'live';" {
		t.Errorf("index.php came back as %q", got)
	}
	if got := files[DatabaseName]; got != "CREATE TABLE users (id int);" {
		t.Errorf("the dump came back as %q", got)
	}
	if !man.Database {
		t.Error("the manifest does not record that a database is in here")
	}
	if man.Site != "acme" || len(man.Domains) != 1 || man.Domains[0] != "acme-supply.com" {
		t.Errorf("manifest describes %q %v, not the site backed up", man.Site, man.Domains)
	}
}

// The exclude list is the difference between a backup and a disk image.
// vendor and node_modules come back from composer and npm, and carrying them
// makes every backup an order of magnitude larger for nothing.
func TestCreate_LeavesOutWhatTheExcludeListNames(t *testing.T) {
	root := siteTree(t)
	key := testKey(t)

	var sealed bytes.Buffer
	if _, err := Create(&sealed, key, testOptions(t, root)); err != nil {
		t.Fatal(err)
	}
	_, files := entries(t, sealed.Bytes(), key)

	for path := range files {
		if strings.HasPrefix(path, "files/vendor/") || strings.HasPrefix(path, "files/node_modules/") {
			t.Errorf("the archive carries %s, which the exclude list named", path)
		}
	}
	if _, ok := files["files/app/Models/User.php"]; !ok {
		t.Error("excluding vendor also dropped the application")
	}
}

// A site on SQLite, or one whose database could not be reached, still gets its
// files backed up. The manifest has to say the database is not in there, or a
// restore silently brings back a site with no data and no warning.
func TestCreate_SaysSoWhenThereIsNoDatabase(t *testing.T) {
	root := siteTree(t)
	key := testKey(t)

	var sealed bytes.Buffer
	if _, err := Create(&sealed, key, testOptions(t, root)); err != nil {
		t.Fatal(err)
	}
	man, files := entries(t, sealed.Bytes(), key)

	if man.Database {
		t.Error("the manifest claims a database that was never dumped")
	}
	if _, ok := files[DatabaseName]; ok {
		t.Error("there is a dump file in an archive that has no database")
	}
}

// A dump that fails halfway is the dangerous case: the files are already
// written, so the archive looks complete. It must fail rather than land as a
// backup missing the data.
func TestCreate_FailsWhenTheDumpFails(t *testing.T) {
	root := siteTree(t)
	opts := testOptions(t, root)
	opts.Database = func(io.Writer) error { return errors.New("mysqldump: connection lost") }
	opts.DatabaseName = "acme"

	var sealed bytes.Buffer
	_, err := Create(&sealed, testKey(t), opts)
	if err == nil {
		t.Fatal("a failed dump produced a backup")
	}
	if !strings.Contains(err.Error(), "connection lost") {
		t.Errorf("error = %q, does not carry what the engine said", err)
	}
}

// A symlink out of the site is followed by nothing: copying whatever it points
// at would pull the rest of the server into a site's backup, and restoring it
// would write outside the site.
func TestCreate_DoesNotFollowALinkOutOfTheSite(t *testing.T) {
	root := siteTree(t)
	outside := filepath.Join(t.TempDir(), "secrets")
	if err := os.WriteFile(outside, []byte("another site's data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	key := testKey(t)
	var sealed bytes.Buffer
	if _, err := Create(&sealed, key, testOptions(t, root)); err != nil {
		t.Fatal(err)
	}
	_, files := entries(t, sealed.Bytes(), key)

	for path, body := range files {
		if strings.Contains(body, "another site's data") {
			t.Errorf("%s carries content from outside the site", path)
		}
	}
}
