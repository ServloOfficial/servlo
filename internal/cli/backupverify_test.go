package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ServloOfficial/servlo/internal/alerts"
	"github.com/ServloOfficial/servlo/internal/backup"
	"github.com/ServloOfficial/servlo/internal/config"
)

// The scheduled check has to report what it found, whichever way it ends.
//
// backup.ReportVerify is what raises the alert the panel and doctor read, and
// what clears it again. The CLI reported only on the path that restores a
// database: a site with no database servlo can name returned before reaching
// it, and a check that failed before reading the manifest returned before it
// too. Either way the result went nowhere but the systemd journal, which is
// exactly the failure the comment beside that call says the whole story exists
// to catch.
//
// The panel's own verify has always reported on both, which is how the two
// disagreed.
func TestBackupVerify_ReportsWhenItCannotEvenOpenTheArchive(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	err := runBackupVerify(filepath.Join(tmp, "not-an-archive.tar.gz.age"), "acme")
	if err == nil {
		t.Fatal("verifying an archive that is not there succeeded")
	}

	if !raisedFor(t, "acme") {
		t.Error("the failure never reached the alert the panel and doctor read, so it only ever appeared in the journal")
	}
}

// And a site with no database still gets a result, so an alert an earlier
// failure raised can be cleared by a later success.
func TestBackupVerify_AFilesOnlyArchiveStillReportsSuccess(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// An earlier failure, of the kind the weekly check would have raised.
	backup.ReportVerify("acme", "acme-old.tar.gz.age", errors.New("the dump would not load"))
	if !raisedFor(t, "acme") {
		t.Fatal("the fixture did not raise an alert, so this test would pass for the wrong reason")
	}

	path := writeFilesOnlyArchive(t, "acme")
	if err := runBackupVerify(path, "acme"); err != nil {
		t.Fatalf("verifying a files-only archive: %v", err)
	}

	if raisedFor(t, "acme") {
		t.Error("a files-only archive that verified did not clear the alert, so the site stays flagged for good")
	}
}

func raisedFor(t *testing.T, site string) bool {
	t.Helper()
	open, err := alerts.List()
	if err != nil {
		t.Fatalf("reading alerts: %v", err)
	}
	for _, a := range open {
		if a.Site == site && a.Kind == alerts.KindBackupUnverified {
			return true
		}
	}
	return false
}

// writeFilesOnlyArchive makes a real archive of a directory with no database,
// through the same writer a scheduled backup uses.
func writeFilesOnlyArchive(t *testing.T, site string) string {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("<h1>hi</h1>\n"), 0644); err != nil {
		t.Fatal(err)
	}
	key, err := backup.Key()
	if err != nil {
		t.Fatalf("backup key: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, site+"-20260101-000000"+backup.Extension)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	if _, err := backup.Create(f, key, backup.Options{
		Site: config.Site{Name: site, Path: src, Domains: []string{site + ".example.com"}},
	}); err != nil {
		t.Fatalf("writing the archive: %v", err)
	}
	return path
}
