package staging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// two sites on disk and in the registry: a live one with content, and a staging
// copy of it.
func pair(t *testing.T) (live, stage *config.Site) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))

	live = &config.Site{Name: "acme", Domains: []string{"acme.example"}, Path: filepath.Join(root, "acme")}
	stage = &config.Site{
		Name: "staging-acme", Domains: []string{"staging.acme.example"},
		Path:    filepath.Join(root, "staging"),
		Staging: &config.SiteStaging{Origin: "acme", User: "staging", Hash: "$2a$10$abc"},
	}
	if err := os.MkdirAll(stage.Path, 0755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(live.Path, "public/index.php"), "<?php // live\n")
	write(t, filepath.Join(live.Path, "app/Model.php"), "live model\n")
	write(t, filepath.Join(live.Path, ".env"), "APP_ENV=production\nDB_DATABASE=acme\n")

	for _, s := range []*config.Site{live, stage} {
		if err := config.AddSite(*s); err != nil {
			t.Fatal(err)
		}
	}
	return live, stage
}

// The refresh brings the live code across.
func TestRefresh_CopiesTheLiveFilesOntoStaging(t *testing.T) {
	live, stage := pair(t)

	res, err := Refresh(stage, Bring{Files: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Files < 2 {
		t.Errorf("copied %d files", res.Files)
	}
	if got := read(t, filepath.Join(stage.Path, "public/index.php")); !strings.Contains(got, "live") {
		t.Errorf("the live index did not arrive: %q", got)
	}
	if got := read(t, filepath.Join(live.Path, "public/index.php")); !strings.Contains(got, "live") {
		t.Error("the live site was modified by a refresh")
	}
}

// The .env is the one file that must not cross. Overwriting staging's would
// point it at the production database, which is the same accident as copying
// the wrong way round arriving by a different route.
func TestRefresh_NeverBringsTheEnvAcross(t *testing.T) {
	live, stage := pair(t)
	write(t, filepath.Join(stage.Path, ".env"), "APP_ENV=staging\nDB_DATABASE=acme_staging\n")
	write(t, filepath.Join(live.Path, ".env.before_servlo"), "old production\n")

	if _, err := Refresh(stage, Bring{Files: true}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(stage.Path, ".env")); !strings.Contains(got, "acme_staging") {
		t.Errorf("staging's own .env was overwritten with live's:\n%s", got)
	}
	if exists(filepath.Join(stage.Path, ".env.before_servlo")) {
		t.Error("a live env backup was copied across")
	}
}

// A file somebody added on staging survives. A refresh that deleted it would be
// deleting work that was not committed anywhere.
func TestRefresh_LeavesWhatIsOnlyOnStaging(t *testing.T) {
	_, stage := pair(t)
	write(t, filepath.Join(stage.Path, "notes.md"), "half-finished\n")

	if _, err := Refresh(stage, Bring{Files: true}); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(stage.Path, "notes.md")) {
		t.Error("a file that only existed on staging was deleted")
	}
}

// This is the guard the whole design is arranged around. Nothing that is not a
// staging site can be the target of a copy from another site.
func TestRefresh_RefusesASiteThatIsNotStaging(t *testing.T) {
	live, _ := pair(t)

	if _, err := Refresh(live, Everything()); err == nil {
		t.Fatal("a live site was refreshed from another site")
	}
	if exists(filepath.Join(live.Path, "notes.md")) {
		t.Error("something was written anyway")
	}
}

// An origin that is the site itself would walk a directory while writing into
// it and report a successful refresh that copied nothing.
//
// Checked files-only on purpose. With the database included the same mistake is
// caught further down, by the check that refuses to load a site's own database
// into itself, and a test that let that one answer would pass with this guard
// deleted.
func TestRefresh_RefusesASiteThatIsItsOwnOrigin(t *testing.T) {
	_, stage := pair(t)
	stage.Staging.Origin = stage.Name
	if err := config.AddSite(*stage); err != nil {
		t.Fatal(err)
	}

	_, err := Refresh(stage, Bring{Files: true})
	if err == nil {
		t.Fatal("a site was refreshed from itself")
	}
	if !strings.Contains(err.Error(), "own origin") {
		t.Errorf("the error does not say what is wrong: %v", err)
	}
}

// The database half has its own version of the same guard. Two sites resolving
// to one database on one connection would have a refresh load it into itself
// while somebody is using it.
//
// Checked directly rather than through a refresh: servlo derives a database
// name from the site's own name, so nothing reachable can currently produce the
// collision, and a test that tried to arrange one would be testing the naming
// rather than the guard.
func TestCheckDatabases_RefusesLoadingADatabaseIntoItself(t *testing.T) {
	if _, err := checkDatabases("acme", "acme", true); err == nil {
		t.Error("a refresh into the same database on the same connection was allowed")
	}
	// Two databases of the same name on different connections are two
	// databases, and copying between them is the ordinary case for a staging
	// site on a separate server.
	if _, err := checkDatabases("acme", "acme", false); err != nil {
		t.Errorf("a copy between connections was refused: %v", err)
	}
	if _, err := checkDatabases("acme", "acme_staging", true); err != nil {
		t.Errorf("an ordinary refresh was refused: %v", err)
	}
}

// A site with no database is not a failure. It is a site on SQLite, or one
// whose data lives somewhere servlo does not manage, and its files still copy.
func TestCheckDatabases_SaysSoWhenThereIsNothingToCopy(t *testing.T) {
	note, err := checkDatabases("", "acme_staging", false)
	if err != nil || note == "" {
		t.Errorf("a live site with no database gave %q, %v", note, err)
	}
	if note, err := checkDatabases("acme", "", false); err != nil || note == "" {
		t.Errorf("a staging site with no database gave %q, %v", note, err)
	}
}

// An origin that has been removed says so, rather than copying nothing and
// reporting success.
func TestRefresh_SaysSoWhenTheOriginIsGone(t *testing.T) {
	_, stage := pair(t)
	stage.Staging.Origin = "a-site-that-was-deleted"

	_, err := Refresh(stage, Everything())
	if err == nil {
		t.Fatal("a refresh from a site that does not exist reported success")
	}
	if !strings.Contains(err.Error(), "a-site-that-was-deleted") {
		t.Errorf("the error does not name what is missing: %v", err)
	}
}

// A symlink out of the live site is recreated, not followed. Following one
// would copy whatever it points at, which may be the rest of the server.
func TestRefresh_RecreatesASymlinkRatherThanFollowingIt(t *testing.T) {
	live, stage := pair(t)
	outside := filepath.Join(t.TempDir(), "secrets")
	write(t, outside, "not the site's\n")
	if err := os.Symlink(outside, filepath.Join(live.Path, "link")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	if _, err := Refresh(stage, Bring{Files: true}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(stage.Path, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was followed and its target copied into the site")
	}
}

// The refresh is stamped, because the question anybody asks about a staging
// site is how old it is.
func TestRefresh_RecordsWhenItHappened(t *testing.T) {
	_, stage := pair(t)

	if _, err := Refresh(stage, Bring{Files: true}); err != nil {
		t.Fatal(err)
	}
	back, err := config.FindSite(stage.Name)
	if err != nil {
		t.Fatal(err)
	}
	if back.Staging == nil || back.Staging.RefreshedAt == "" {
		t.Errorf("nothing recorded when the refresh happened: %+v", back.Staging)
	}
}
