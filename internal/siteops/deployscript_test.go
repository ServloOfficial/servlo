package siteops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"gopkg.in/yaml.v3"
)

func scriptHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
}

// seedStoreFramework puts a store definition in the local store the way `servlo
// link` does. Without it the framework resolves to the built-in Go definition,
// which carries no deploy profile: the profiles live in the store, and the
// store is what a linked site has.
func seedStoreFramework(t *testing.T, name, version string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "stores", "frameworks", name, version+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var fw config.Framework
	if err := yaml.Unmarshal(body, &fw); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveStoreFramework(&fw); err != nil {
		t.Fatal(err)
	}
}

// scriptSite builds a site on disk the way a real one looks, with its framework
// in the local store and the files its detection reads.
func scriptSite(t *testing.T, framework string) *config.Site {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if framework != "" {
		seedStoreFramework(t, framework, "12")
		if err := os.WriteFile(filepath.Join(path, "artisan"), []byte("#!/usr/bin/env php\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		composer := `{"require": {"laravel/framework": "^12.0"}}`
		if err := os.WriteFile(filepath.Join(path, "composer.json"), []byte(composer), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: path, PHPVersion: "8.4", Framework: framework,
	}
}

// A site that has never saved one reads its framework's template, so the editor
// opens on something worth editing rather than on an empty box.
func TestDeployScript_StartsFromTheFrameworkTemplate(t *testing.T) {
	scriptHome(t)
	site := scriptSite(t, "laravel")

	got, err := ReadDeployScript(site)
	if err != nil {
		t.Fatalf("ReadDeployScript: %v", err)
	}

	if got.Exists {
		t.Error("a site that never saved a script was reported as having one")
	}
	if !strings.Contains(got.Body, "composer install --no-dev") {
		t.Errorf("the template is not the framework's:\n%s", got.Body)
	}
	// Whoever opens this a year from now needs to know what runs it and where.
	for _, want := range []string{"git pull", "shop.example", "working directory"} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("the header does not mention %q:\n%s", want, got.Body)
		}
	}
}

// A framework with no profile still gets a usable file, not an error and not a
// blank one.
func TestDeployScript_ExplainsItselfWhenTheFrameworkHasNoProfile(t *testing.T) {
	scriptHome(t)
	site := scriptSite(t, "")

	got, err := ReadDeployScript(site)
	if err != nil {
		t.Fatalf("ReadDeployScript: %v", err)
	}

	if strings.TrimSpace(got.Body) == "" {
		t.Error("a site whose framework has no profile got an empty file with no explanation")
	}
	if !strings.Contains(got.Body, "git pull") {
		t.Errorf("the header is missing:\n%s", got.Body)
	}
}

// Saved is saved. The whole point of the template being a starting point is
// that servlo does not come back and overwrite the operator's version.
func TestDeployScript_SurvivesAndIsNeverRewritten(t *testing.T) {
	scriptHome(t)
	site := scriptSite(t, "laravel")

	res, err := SaveDeployScript(site, "#!/bin/sh\necho mine\n", false)
	if err != nil {
		t.Fatalf("SaveDeployScript: %v", err)
	}
	if !res.OK {
		t.Fatalf("save refused: %+v", res)
	}

	got, err := ReadDeployScript(site)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Exists || !strings.Contains(got.Body, "echo mine") {
		t.Errorf("the saved script did not come back:\n%+v", got)
	}
	if strings.Contains(got.Body, "composer install") {
		t.Errorf("the framework template came back over the operator's script:\n%s", got.Body)
	}
	// Reading it again changes nothing, which is what "never rewritten" means
	// in the only way a test can check it.
	again, _ := ReadDeployScript(site)
	if again.Body != got.Body {
		t.Error("reading the script twice returned two different scripts")
	}
}

// The script is not in the site directory, and that is deliberate: a deploy
// runs `git pull` first, and a script inside the working tree is a file the
// pull can rewrite while it is the thing running the deploy.
func TestDeployScript_LivesOutsideTheSiteDirectory(t *testing.T) {
	scriptHome(t)
	site := scriptSite(t, "laravel")

	if _, err := SaveDeployScript(site, "echo hi\n", false); err != nil {
		t.Fatal(err)
	}

	path := DeployScriptPath(site.Name)
	if strings.HasPrefix(path, site.Path) {
		t.Errorf("the script sits inside the site at %q, where a pull can rewrite it", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the script is not where it says it is: %v", err)
	}
	for _, e := range mustReadDir(t, site.Path) {
		if strings.Contains(e.Name(), "deploy") {
			t.Errorf("the site directory gained %q", e.Name())
		}
	}
}

// A script that runs a migration is what triggers the pre-deploy database
// backup, and it is read from what will actually run rather than from the
// framework's template.
func TestDeployScript_MigrationIsReadFromTheSavedScript(t *testing.T) {
	scriptHome(t)
	site := scriptSite(t, "laravel")

	// The template migrates, so an untouched site does.
	migrates, err := DeployScriptMigrates(site)
	if err != nil {
		t.Fatal(err)
	}
	if !migrates {
		t.Error("the framework template's migration was not recognised")
	}

	// An operator who took the migration out is not migrating, whatever the
	// framework's template says.
	if _, err := SaveDeployScript(site, "#!/bin/sh\ncomposer install --no-dev\n", false); err != nil {
		t.Fatal(err)
	}
	migrates, err = DeployScriptMigrates(site)
	if err != nil {
		t.Fatal(err)
	}
	if migrates {
		t.Error("a script with the migration removed was still treated as migrating, so every deploy snapshots the database")
	}

	// And one who added a migration the template never had is.
	if _, err := SaveDeployScript(site, "#!/bin/sh\nphp artisan migrate --force\n", false); err != nil {
		t.Fatal(err)
	}
	if migrates, _ = DeployScriptMigrates(site); !migrates {
		t.Error("a migration the operator added was not recognised")
	}
}

// Pasting over a working script is the mistake this protects against.
func TestDeployScript_KeepsABackupOfWhatItReplaced(t *testing.T) {
	scriptHome(t)
	site := scriptSite(t, "laravel")

	if _, err := SaveDeployScript(site, "#!/bin/sh\necho first\n", false); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveDeployScript(site, "#!/bin/sh\necho second\n", true); err != nil {
		t.Fatal(err)
	}

	backups, err := ListDeployScriptBackups(site)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("got %d backups, want the one it replaced", len(backups))
	}
	body, err := ReadDeployScriptBackup(site, backups[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "echo first") {
		t.Errorf("the backup is not what was replaced:\n%s", body)
	}

	if _, err := RestoreDeployScript(site, backups[0].Name); err != nil {
		t.Fatalf("RestoreDeployScript: %v", err)
	}
	got, _ := ReadDeployScript(site)
	if !strings.Contains(got.Body, "echo first") {
		t.Errorf("the restore did not bring the old script back:\n%s", got.Body)
	}
}

// Resetting goes back to the framework's template rather than to an empty file,
// which is what an operator asking for the default means.
func TestDeployScript_ResetGoesBackToTheTemplate(t *testing.T) {
	scriptHome(t)
	site := scriptSite(t, "laravel")
	if _, err := SaveDeployScript(site, "#!/bin/sh\necho mine\n", false); err != nil {
		t.Fatal(err)
	}

	if err := ResetDeployScript(site); err != nil {
		t.Fatalf("ResetDeployScript: %v", err)
	}

	got, _ := ReadDeployScript(site)
	if got.Exists {
		t.Error("the script still exists after a reset")
	}
	if !strings.Contains(got.Body, "composer install --no-dev") {
		t.Errorf("the reset did not go back to the framework template:\n%s", got.Body)
	}
}

// The site name decides a filename.
func TestDeployScript_RefusesASiteNameThatIsAPath(t *testing.T) {
	scriptHome(t)

	for _, bad := range []string{"../../etc/passwd", "a/b", "", ".."} {
		site := &config.Site{Name: bad, Domains: []string{"x.example"}, Path: t.TempDir()}
		if _, err := ReadDeployScript(site); err == nil {
			t.Errorf("site name %q was accepted", bad)
		}
		if _, err := SaveDeployScript(site, "echo hi\n", false); err == nil {
			t.Errorf("site name %q was accepted for a save", bad)
		}
	}
}

// A NUL makes the file unreadable by any shell, so it is refused rather than
// written and discovered at deploy time.
func TestDeployScript_RefusesANULByte(t *testing.T) {
	scriptHome(t)
	site := scriptSite(t, "laravel")

	res, err := SaveDeployScript(site, "echo hi\x00\n", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("a script containing a NUL was written")
	}
	if _, statErr := os.Stat(DeployScriptPath(site.Name)); statErr == nil {
		t.Error("the refused script was written to disk anyway")
	}
}

func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// The header is the only place an operator is told which shell runs this, and
// it has to be told plainly: servlo runs the body, not the file, so changing
// the first line changes nothing and a bash feature below it fails with
// something like "Illegal option -o pipefail" and no hint why.
func TestDeployScriptTemplate_SaysWhichShellActuallyRunsIt(t *testing.T) {
	site := &config.Site{Name: "acme", Domains: []string{"acme.com"}, Framework: "unknown"}
	header := deployScriptTemplate(site)
	for _, want := range []string{"#!/bin/sh", "/bin/sh", "first line"} {
		if !strings.Contains(header, want) {
			t.Errorf("the deploy script header never mentions %q:\n%s", want, header)
		}
	}
}

// §3.5: a database backup runs automatically before any deploy whose script
// contains a migration. The marker a migration is recognised by comes from the
// framework's profile, so a site that resolves to a definition carrying no
// profile has nothing to match and the backup silently never happens. That is
// the worst shape this guarantee can fail in: the deploy reports success, and
// the operator learns the snapshot was not taken when they go looking for it.
func TestDeployScriptMigrates_OnASiteWhoseVersionCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "cfg"))

	sitePath := filepath.Join(dir, "site")
	if err := os.MkdirAll(sitePath, 0o755); err != nil {
		t.Fatal(err)
	}
	// The marker file and nothing that says which version: no composer.json.
	if err := os.WriteFile(filepath.Join(sitePath, "artisan"), []byte("#!/usr/bin/env php\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	site := &config.Site{Name: "acme", Domains: []string{"acme.com"}, Path: sitePath, Framework: "laravel"}
	if _, err := SaveDeployScript(site, "#!/bin/sh\nset -eu\nphp artisan migrate --force\n", false); err != nil {
		t.Fatal(err)
	}

	migrates, err := DeployScriptMigrates(site)
	if err != nil {
		t.Fatal(err)
	}
	if !migrates {
		t.Error("a deploy script that plainly runs a migration was not treated as one, " +
			"so the deploy would have gone ahead with no database backup behind it")
	}
}
