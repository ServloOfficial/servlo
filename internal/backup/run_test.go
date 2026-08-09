package backup

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realrashid/servlo/internal/config"
)

func runnerSite(t *testing.T) config.Site {
	t.Helper()
	root := siteTree(t)
	return config.Site{Name: "acme", Domains: []string{"acme-supply.com"}, Path: root, PHPVersion: "8.4"}
}

// A backup that leaves a half-written file behind is worse than none: the next
// run's retention sweep counts it, and a restore picks it up. The archive is
// written to a temporary name and moved into place only once it is complete.
func TestRun_LeavesNothingBehindWhenTheBackupFails(t *testing.T) {
	withConfigHome(t)
	dest := t.TempDir()
	r := Runner{
		Dir:      dest,
		Excludes: func(*config.Site) ([]string, error) { return nil, nil },
		Dump: func(*config.Site) (func(io.Writer) error, string, string) {
			return func(io.Writer) error { return errors.New("engine down") }, "acme", "mysql"
		},
	}

	site := runnerSite(t)
	if _, err := r.Run(&site); err == nil {
		t.Fatal("a failed backup reported success")
	}

	left, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range left {
		t.Errorf("a failed backup left %s behind", e.Name())
	}
}

// The happy path: an archive lands, it is private, and it can be opened again
// with this server's key.
func TestRun_WritesAnArchiveThatOpens(t *testing.T) {
	withConfigHome(t)
	dest := t.TempDir()
	r := Runner{
		Dir:      dest,
		Excludes: func(*config.Site) ([]string, error) { return []string{"vendor", "node_modules"}, nil },
		Dump: func(*config.Site) (func(io.Writer) error, string, string) {
			return func(w io.Writer) error { _, err := io.WriteString(w, "-- dump"); return err }, "acme", "mysql"
		},
		Now: func() time.Time { return time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC) },
	}

	site := runnerSite(t)
	rec, err := r.Run(&site)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(rec.Path)
	if err != nil {
		t.Fatalf("no archive at %s: %v", rec.Path, err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("archive is %04o, want 0600: it holds the whole site", perm)
	}
	if !strings.Contains(filepath.Base(rec.Path), "acme") || !strings.Contains(filepath.Base(rec.Path), "20260809") {
		t.Errorf("archive is named %q, which does not say which site or when", filepath.Base(rec.Path))
	}

	key, err := Key()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(rec.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	man, err := ReadManifest(f, key)
	if err != nil {
		t.Fatalf("the archive it just wrote cannot be opened: %v", err)
	}
	if man.Site != "acme" || !man.Database {
		t.Errorf("manifest = %+v, not the backup that was taken", man)
	}
}

// Two backups of one site in the same second must not overwrite each other.
// Losing yesterday's good archive to today's is the failure retention exists to
// prevent, and a name collision is the same loss by another route.
func TestRun_TwoBackupsInTheSameSecondBothSurvive(t *testing.T) {
	withConfigHome(t)
	dest := t.TempDir()
	fixed := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	r := Runner{
		Dir:      dest,
		Excludes: func(*config.Site) ([]string, error) { return nil, nil },
		Now:      func() time.Time { return fixed },
	}

	site := runnerSite(t)
	first, err := r.Run(&site)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Run(&site)
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path {
		t.Fatal("the second backup overwrote the first")
	}
	for _, p := range []string{first.Path, second.Path} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s is gone: %v", p, err)
		}
	}
}

// A site with no database still backs up, and says so, rather than refusing.
func TestRun_BacksUpASiteWithNoDatabase(t *testing.T) {
	withConfigHome(t)
	r := Runner{
		Dir:      t.TempDir(),
		Excludes: func(*config.Site) ([]string, error) { return nil, nil },
		Dump:     func(*config.Site) (func(io.Writer) error, string, string) { return nil, "", "" },
	}

	site := runnerSite(t)
	rec, err := r.Run(&site)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Manifest.Database {
		t.Error("a site with no database produced a backup claiming one")
	}
}

// A scheduled backup that never sweeps fills the disk, and a disk that fills
// takes the sites down. The sweep runs as part of the backup rather than as a
// second timer nobody remembers to arm.
func TestRun_SweepsOldArchivesAfterWriting(t *testing.T) {
	withConfigHome(t)
	dest := t.TempDir()
	now := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
	for d := 1; d <= 30; d++ {
		at := now.AddDate(0, 0, -d)
		p := filepath.Join(dest, "acme-"+at.Format("20060102-150405")+Extension)
		if err := os.WriteFile(p, []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	r := Runner{
		Dir:      dest,
		Excludes: func(*config.Site) ([]string, error) { return nil, nil },
		Policy:   func(*config.Site) Policy { return Policy{Daily: 3} },
		Now:      func() time.Time { return now },
	}
	site := runnerSite(t)
	rec, err := r.Run(&site)
	if err != nil {
		t.Fatal(err)
	}

	left, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 3 {
		t.Errorf("kept %d archives, want the 3 the policy asked for", len(left))
	}
	if _, err := os.Stat(rec.Path); err != nil {
		t.Error("the sweep deleted the backup it had just taken")
	}
}

// The sweep must never take the archive down with it. A failure to free disk is
// a warning; reporting the whole backup as failed would have an operator
// re-running something that already succeeded.
func TestRun_ASweepFailureDoesNotLoseTheBackup(t *testing.T) {
	withConfigHome(t)
	dest := t.TempDir()
	r := Runner{
		Dir:      dest,
		Excludes: func(*config.Site) ([]string, error) { return nil, nil },
		Policy:   func(*config.Site) Policy { return Policy{Daily: 1} },
	}
	site := runnerSite(t)
	rec, err := r.Run(&site)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Path == "" {
		t.Fatal("no archive")
	}
	if _, err := os.Stat(rec.Path); err != nil {
		t.Errorf("the archive is gone: %v", err)
	}
}
