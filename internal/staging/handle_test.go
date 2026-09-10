package staging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// A handle drops the TLD, so two staging domains that differ only there reduce
// to one, and config.AddSite treats a matching name as an update rather than a
// refusal. The replaced site keeps its FPM pool, its worker units and its backup
// timer, all now named for a site that is no longer in the registry.
func TestStagingHandle_DoesNotTakeAHandleAnotherSiteHolds(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	if err := config.AddSite(config.Site{
		Name: "staging-acme", Path: "/srv/sites/staging.acme.com", Domains: []string{"staging.acme.com"},
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	got := stagingHandleFor("staging.acme.net", "/srv/sites/staging.acme.net")
	if got == "staging-acme" {
		t.Fatal("the staging site took a handle another site holds, so registering it would replace that site")
	}
	if got != "staging-acme-2" {
		t.Errorf("handle = %q, want staging-acme-2", got)
	}
}

// Making the staging site again over the same directory keeps its handle.
func TestStagingHandle_KeepsTheHandleWhenThePathIsTheSame(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_DATA_HOME", dir)
	if err := config.AddSite(config.Site{
		Name: "staging-acme", Path: "/srv/sites/staging.acme.com", Domains: []string{"staging.acme.com"},
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}
	if got := stagingHandleFor("staging.acme.com", "/srv/sites/staging.acme.com"); got != "staging-acme" {
		t.Errorf("handle = %q, want the handle it already has", got)
	}
}

// And in one place, for the reason the same test gives in internal/appinstall.
func TestStagingHandle_IsDerivedInOnePlace(t *testing.T) {
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
				continue
			}
			t.Errorf("%s:%d derives a site handle without asking whether it is free:\n\t%s",
				e.Name(), i+1, strings.TrimSpace(line))
		}
	}
}
