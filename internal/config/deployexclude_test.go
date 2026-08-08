package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The three states have to survive a write and a read, because they mean
// different things: unset inherits the framework's list, an empty list protects
// nothing, and a list is the site's own.
func TestSiteDeployExclude_RoundTripsAllThreeStates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	none := []string{}
	sites := []Site{
		{Name: "inherits", Domains: []string{"a.example"}, Path: dir},
		{Name: "protects-nothing", Domains: []string{"b.example"}, Path: dir, DeployExclude: &none},
		{Name: "own-list", Domains: []string{"c.example"}, Path: dir, DeployExclude: &[]string{"storage/app"}},
	}
	for _, s := range sites {
		if err := AddSite(s); err != nil {
			t.Fatal(err)
		}
	}

	reg, err := LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	back := map[string]*[]string{}
	for _, s := range reg.Sites {
		back[s.Name] = s.DeployExclude
	}

	if back["inherits"] != nil {
		t.Errorf("a site that never set a list came back with %v, so it can no longer inherit its framework's", *back["inherits"])
	}
	if back["protects-nothing"] == nil {
		t.Error("a site that cleared its list came back unset, so it silently went back to inheriting")
	} else if len(*back["protects-nothing"]) != 0 {
		t.Errorf("cleared list came back as %v", *back["protects-nothing"])
	}
	if back["own-list"] == nil || len(*back["own-list"]) != 1 || (*back["own-list"])[0] != "storage/app" {
		t.Errorf("own list came back as %v", back["own-list"])
	}
}

// An exclude path names something inside the site. One that climbs out would
// have servlo restoring files over the rest of the filesystem.
func TestValidateDeployExclude_RefusesAPathThatEscapesTheSite(t *testing.T) {
	for _, bad := range []string{"../../etc", "/etc/passwd", "wp-content/../../..", "", "  ", "a\x00b"} {
		s := &Site{Name: "shop", DeployExclude: &[]string{bad}}
		if err := s.ValidateDeployExclude(); err == nil {
			t.Errorf("exclude %q was accepted", bad)
		}
	}

	ok := &Site{Name: "shop", DeployExclude: &[]string{"wp-content/uploads", "wp-content/plugins"}}
	if err := ok.ValidateDeployExclude(); err != nil {
		t.Errorf("a normal list was refused: %v", err)
	}

	// Unset and empty are both fine: they are the two ways of saying nothing
	// about this site.
	if err := (&Site{Name: "shop"}).ValidateDeployExclude(); err != nil {
		t.Errorf("an unset list was refused: %v", err)
	}
	if err := (&Site{Name: "shop", DeployExclude: &[]string{}}).ValidateDeployExclude(); err != nil {
		t.Errorf("an empty list was refused: %v", err)
	}
}

// The default has to reach a WordPress site from the store rather than from Go,
// because §2 says a framework's behaviour is data.
func TestWordPressDefinition_ExcludesUploadsAndPlugins(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "stores", "frameworks", "wordpress", "6.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	fw := parseFramework(t, string(b))

	want := map[string]bool{"wp-content/uploads": false, "wp-content/plugins": false}
	for _, p := range fw.DeployExcludes() {
		if _, ok := want[p]; ok {
			want[p] = true
		}
	}
	for p, found := range want {
		if !found {
			t.Errorf("the WordPress definition does not protect %q: %v", p, fw.DeployExcludes())
		}
	}
}
