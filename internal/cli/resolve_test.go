package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

func TestErrNotLinkedMentionsLink(t *testing.T) {
	msg := errNotLinked().Error()
	if !strings.Contains(msg, "run 'servlo link' first") {
		t.Errorf("errNotLinked message changed, callers rely on it: %q", msg)
	}
}

// In a test process stdin is not a terminal, so ensureSiteForCwd must take the
// non-interactive branch and return the consistent error without prompting.
// The package source dir it runs in is not a registered site.
func TestEnsureSiteForCwdNonInteractiveErrors(t *testing.T) {
	_, err := ensureSiteForCwd()
	if err == nil {
		t.Fatal("expected an error for an unlinked directory in non-interactive mode")
	}
	if !strings.Contains(err.Error(), "servlo link") {
		t.Errorf("error should point the user at servlo link, got: %v", err)
	}
}

// chdir moves into dir for the test and restores the old cwd afterwards.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) }) //nolint:errcheck
}

// writeSitesYAML writes a minimal sites.yaml into the current XDG_DATA_HOME so
// config.LoadSites returns the supplied sites.
func writeSitesYAML(t *testing.T, sites []config.Site) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_DATA_HOME"), "servlo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := "sites:\n"
	for _, s := range sites {
		body += "  - name: " + s.Name + "\n"
		body += "    path: " + s.Path + "\n"
		body += "    domains:\n      - " + s.Name + ".test\n"
		body += "    php_version: \"8.4\"\n    node_version: \"22\"\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "sites.yaml"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

// A registered site resolves with no branch.
func TestEnsureSiteAndBranchForCwd_registeredSiteHasNoBranch(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	parent := t.TempDir()
	writeSitesYAML(t, []config.Site{{Name: "rapids", Path: parent}})
	chdir(t, parent)

	site, branch, err := ensureSiteAndBranchForCwd()
	if err != nil {
		t.Fatalf("ensureSiteAndBranchForCwd: %v", err)
	}
	if site.Name != "rapids" {
		t.Errorf("site = %q, want rapids", site.Name)
	}
	if branch != "" {
		t.Errorf("branch = %q, want empty for the parent checkout", branch)
	}
}
