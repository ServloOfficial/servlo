package appstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installable parses the sample definition and then points it at a local test
// server. The source is set afterwards rather than in the YAML because Parse
// refuses a non-https URL, correctly, and httptest speaks http. These tests are
// about Install; the refusal has its own test.
func installable(t *testing.T, archive []byte) App {
	t.Helper()
	app, err := Parse([]byte(sampleApp))
	if err != nil {
		t.Fatal(err)
	}
	app.Source.URL = serving(t, archive)
	app.Source.SHA256 = digestOf(archive)
	app.Source.StripPrefix = ""
	return app
}

func fakeConnection(name string) (Connection, error) {
	return Connection{Name: name, User: name + "_u", Password: "generated-password", Host: "servlo-mysql"}, nil
}

func TestInstall_WritesTheConfigFileAtItsDeclaredMode(t *testing.T) {
	app := installable(t, releaseZip(t, map[string]string{"index.php": "<?php"}))
	dir := t.TempDir()

	res, err := app.Install(context.Background(),
		Request{Dir: dir, SiteURL: "https://example.com", DatabaseName: "example"},
		Deps{CreateDatabase: fakeConnection})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	info, err := os.Stat(res.ConfigWritten)
	if err != nil {
		t.Fatalf("no config file: %v", err)
	}
	// It holds the database password, and every site here runs as one user.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config mode = %o, want 600", perm)
	}
	body, _ := os.ReadFile(res.ConfigWritten)
	for _, want := range []string{"example", "generated-password", "servlo-mysql", "https://example.com"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("config missing %q:\n%s", want, body)
		}
	}
}

// An app that says it needs a database and is given no way to make one would
// otherwise write a config file pointing at nothing and report success.
func TestInstall_RefusesWhenItCannotMakeTheDatabaseTheAppNeeds(t *testing.T) {
	app := installable(t, releaseZip(t, map[string]string{"index.php": "<?php"}))

	_, err := app.Install(context.Background(),
		Request{Dir: t.TempDir(), SiteURL: "https://example.com"}, Deps{})
	if err == nil {
		t.Fatal("an app needing a database installed without one")
	}
	if !strings.Contains(err.Error(), "database") {
		t.Errorf("error = %q, does not name the database", err)
	}
}

// The release is fetched and verified before the database is touched, so a bad
// checksum does not leave an orphaned database behind.
func TestInstall_DoesNotCreateADatabaseForAReleaseItRejects(t *testing.T) {
	app := installable(t, releaseZip(t, map[string]string{"index.php": "<?php"}))
	app.Source.SHA256 = digestOf([]byte("a different release"))

	called := false
	_, err := app.Install(context.Background(),
		Request{Dir: t.TempDir(), SiteURL: "https://example.com", DatabaseName: "example"},
		Deps{CreateDatabase: func(n string) (Connection, error) { called = true; return fakeConnection(n) }})

	if err == nil {
		t.Fatal("a release that failed its checksum was installed")
	}
	if called {
		t.Error("a database was created for a release that was rejected")
	}
}

// Two installs must not share salts, or every site servlo creates has the same
// session keys as every other.
func TestInstall_GivesEachSiteItsOwnSecrets(t *testing.T) {
	archive := releaseZip(t, map[string]string{"index.php": "<?php"})

	read := func() string {
		app := installable(t, archive)
		app.Secrets = []Secret{{Name: "salt", Length: 64}}
		app.ConfigFile.Template = "<?php define('S','{{salt}}'); define('D','{{db_name}}');"
		dir := t.TempDir()
		res, err := app.Install(context.Background(),
			Request{Dir: dir, SiteURL: "https://example.com", DatabaseName: "example"},
			Deps{CreateDatabase: fakeConnection})
		if err != nil {
			t.Fatal(err)
		}
		body, _ := os.ReadFile(res.ConfigWritten)
		return string(body)
	}

	if read() == read() {
		t.Error("two installs produced identical config files, so they share a salt")
	}
}

// The config file goes inside the site and nowhere else, whatever the
// definition's path spelled. Parse refuses an escaping path, and this is the
// second half of that promise.
func TestInstall_WritesTheConfigInsideTheSite(t *testing.T) {
	app := installable(t, releaseZip(t, map[string]string{"index.php": "<?php"}))
	dir := t.TempDir()

	res, err := app.Install(context.Background(),
		Request{Dir: dir, SiteURL: "https://example.com", DatabaseName: "example"},
		Deps{CreateDatabase: fakeConnection})
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(dir, res.ConfigWritten)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("config written to %q, outside %q", res.ConfigWritten, dir)
	}
}
