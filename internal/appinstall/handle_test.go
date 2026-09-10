package appinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// stageSite puts a site in the registry the handle check reads.
func stageSite(t *testing.T, name, path string, domains ...string) {
	t.Helper()
	if err := config.AddSite(config.Site{Name: name, Path: path, Domains: domains}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}
}

// A site's handle drops the TLD, so acme.com, acme.net and acme.org all reduce
// to "acme". The handle names the FPM pool, the worker units, the backup timer,
// the deploy script and the per-site PHP settings, and AddSite treats a matching
// name as an update: installing an app on the second domain would replace the
// first site's registry entry and take its database name with it.
//
// `servlo link` has taken a free handle since the linker was written. This is
// the same question asked of the one-click install, which did not.
func TestSiteHandle_DoesNotTakeAHandleAnotherSiteHolds(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	stageSite(t, "acme", "/srv/sites/acme.com", "acme.com")

	got := siteHandleFor("acme.net", "/srv/sites/acme.net")
	if got == "acme" {
		t.Fatal("the install took the handle acme.com already holds, so registering it would replace that site")
	}
	if got != "acme-2" {
		t.Errorf("handle = %q, want acme-2", got)
	}
}

// Re-running an install over the same directory is not a collision: it is the
// same site, and giving it a new handle each time would strand its units.
func TestSiteHandle_KeepsTheHandleWhenThePathIsTheSame(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	stageSite(t, "acme", "/srv/sites/acme.com", "acme.com")

	if got := siteHandleFor("acme.com", "/srv/sites/acme.com"); got != "acme" {
		t.Errorf("handle = %q, want the handle it already has", got)
	}
}

// And the handle is chosen in one place. A second caller deriving it straight
// from the domain is how this came back, so the package is read for one.
func TestSiteHandle_IsDerivedInOnePlace(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(".", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, "siteops.SiteName(") {
				continue
			}
			if strings.Contains(line, "return linker.FreeSiteName(") {
				continue // the one place, which asks the registry
			}
			t.Errorf("%s:%d derives a site handle without asking whether it is free:\n\t%s",
				e.Name(), i+1, strings.TrimSpace(line))
		}
	}
}
