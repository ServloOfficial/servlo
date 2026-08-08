package siteops

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

// wordpressSite is a WordPress site with its definition in the local store, so
// the exclude list arrives from the store rather than from a Go builtin.
func wordpressSite(t *testing.T) *config.Site {
	t.Helper()
	scriptHome(t)
	seedStoreFramework(t, "wordpress", "6")

	path := filepath.Join(t.TempDir(), "shop")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"wp-login.php", "wp-config.php"} {
		if err := os.WriteFile(filepath.Join(path, f), []byte("<?php\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &config.Site{
		Name: "shop", Domains: []string{"shop.example"},
		Path: path, Framework: "wordpress", PHPVersion: "8.3",
	}
}

// The default is the framework's, which is where it belongs: a WordPress site
// protects uploads and plugins without anybody configuring it.
func TestDeployExcludes_FallsBackToTheFramework(t *testing.T) {
	site := wordpressSite(t)

	got, err := DeployExcludes(site)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"wp-content/uploads", "wp-content/plugins"} {
		if !slices.Contains(got, want) {
			t.Errorf("DeployExcludes() = %v, does not protect %q", got, want)
		}
	}
}

// The site's own list wins, because the framework cannot know what a particular
// application writes to.
func TestDeployExcludes_TheSitesOwnListWins(t *testing.T) {
	site := wordpressSite(t)
	site.DeployExclude = &[]string{"wp-content/uploads", "wp-content/languages"}

	got, err := DeployExcludes(site)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"wp-content/uploads", "wp-content/languages"}
	if !slices.Equal(got, want) {
		t.Errorf("DeployExcludes() = %v, want %v", got, want)
	}
}

// Clearing the list means clearing it. Falling back to the framework here would
// make the field impossible to turn off, and an operator who cleared it would
// watch the next deploy keep files anyway with nothing on screen saying why.
func TestDeployExcludes_AnEmptyListProtectsNothing(t *testing.T) {
	site := wordpressSite(t)
	site.DeployExclude = &[]string{}

	got, err := DeployExcludes(site)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 0 {
		t.Errorf("DeployExcludes() = %v, want nothing kept", got)
	}
}

// A site with no framework has nothing to inherit, and that is not an error:
// plain PHP has no directory servlo knows to protect.
func TestDeployExcludes_NoFrameworkIsNotAnError(t *testing.T) {
	scriptHome(t)
	path := filepath.Join(t.TempDir(), "plain")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	site := &config.Site{Name: "plain", Domains: []string{"plain.example"}, Path: path}

	got, err := DeployExcludes(site)
	if err != nil {
		t.Fatalf("DeployExcludes: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("DeployExcludes() = %v, want nothing", got)
	}
}

// The list comes out of a text box, so it arrives with the things a text box
// produces: blank lines, trailing slashes, the same path twice.
func TestDeployExcludes_TidiesWhatWasTypedIn(t *testing.T) {
	site := wordpressSite(t)
	site.DeployExclude = &[]string{
		" wp-content/uploads/ ", "", "   ",
		"./wp-content/plugins", "wp-content/plugins", ".",
	}

	got, err := DeployExcludes(site)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"wp-content/uploads", "wp-content/plugins"}
	if !slices.Equal(got, want) {
		t.Errorf("DeployExcludes() = %v, want %v", got, want)
	}
}
