package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCountSitesUsingService_ServloYAML(t *testing.T) {
	setDataDir(t)

	siteDir := t.TempDir()
	servloYAML := `services:
  - postgres
`
	os.WriteFile(filepath.Join(siteDir, ".servlo.yaml"), []byte(servloYAML), 0644)

	if err := AddSite(Site{
		Name:    "goapp",
		Domains: []string{"goapp.test"},
		Path:    siteDir,
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	count := CountSitesUsingService("postgres")
	if count != 1 {
		t.Errorf("CountSitesUsingService(postgres) = %d, want 1", count)
	}

	count = CountSitesUsingService("mysql")
	if count != 0 {
		t.Errorf("CountSitesUsingService(mysql) = %d, want 0", count)
	}
}

func TestCountSitesUsingService_NoEnvNoYAML(t *testing.T) {
	setDataDir(t)

	siteDir := t.TempDir()
	if err := AddSite(Site{
		Name:    "bare",
		Domains: []string{"bare.test"},
		Path:    siteDir,
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	count := CountSitesUsingService("postgres")
	if count != 0 {
		t.Errorf("CountSitesUsingService(postgres) = %d, want 0", count)
	}
}

func TestCountSitesUsingService_EnvFallback(t *testing.T) {
	setDataDir(t)

	siteDir := t.TempDir()
	os.WriteFile(filepath.Join(siteDir, ".env"), []byte("DB_HOST=servlo-mysql\n"), 0644)

	if err := AddSite(Site{
		Name:    "phpapp",
		Domains: []string{"phpapp.test"},
		Path:    siteDir,
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	count := CountSitesUsingService("mysql")
	if count != 1 {
		t.Errorf("CountSitesUsingService(mysql) = %d, want 1", count)
	}
}

func TestCountSitesUsingService_ServloYAMLTakesPriority(t *testing.T) {
	setDataDir(t)

	siteDir := t.TempDir()
	// .servlo.yaml declares postgres, .env also references servlo-mysql.
	// The function should count the site for postgres via .servlo.yaml and
	// NOT double-count it (goto next after .servlo.yaml match).
	servloYAML := `services:
  - postgres
`
	os.WriteFile(filepath.Join(siteDir, ".servlo.yaml"), []byte(servloYAML), 0644)
	os.WriteFile(filepath.Join(siteDir, ".env"), []byte("DB_HOST=servlo-postgres\n"), 0644)

	if err := AddSite(Site{
		Name:    "dualapp",
		Domains: []string{"dualapp.test"},
		Path:    siteDir,
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	count := CountSitesUsingService("postgres")
	if count != 1 {
		t.Errorf("CountSitesUsingService(postgres) = %d, want 1 (not double-counted)", count)
	}
}

func TestCountSitesUsingService_IgnoredSiteSkipped(t *testing.T) {
	setDataDir(t)

	siteDir := t.TempDir()
	servloYAML := `services:
  - redis
`
	os.WriteFile(filepath.Join(siteDir, ".servlo.yaml"), []byte(servloYAML), 0644)

	if err := AddSite(Site{
		Name:    "ignored",
		Domains: []string{"ignored.test"},
		Path:    siteDir,
		Ignored: true,
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	count := CountSitesUsingService("redis")
	if count != 0 {
		t.Errorf("CountSitesUsingService(redis) = %d, want 0 for ignored site", count)
	}
}

func TestCountSitesUsingService_PausedSiteSkipped(t *testing.T) {
	setDataDir(t)

	siteDir := t.TempDir()
	servloYAML := `services:
  - redis
`
	os.WriteFile(filepath.Join(siteDir, ".servlo.yaml"), []byte(servloYAML), 0644)

	if err := AddSite(Site{
		Name:    "paused",
		Domains: []string{"paused.test"},
		Path:    siteDir,
		Paused:  true,
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	count := CountSitesUsingService("redis")
	if count != 0 {
		t.Errorf("CountSitesUsingService(redis) = %d, want 0 for paused site", count)
	}
}

func TestSitesUsingService_ReturnsLinkedSites(t *testing.T) {
	setDataDir(t)

	yamlSiteDir := t.TempDir()
	os.WriteFile(filepath.Join(yamlSiteDir, ".servlo.yaml"), []byte("services:\n  - mariadb\n"), 0644)
	if err := AddSite(Site{Name: "yaml-site", Domains: []string{"yaml.test"}, Path: yamlSiteDir}); err != nil {
		t.Fatalf("AddSite yaml: %v", err)
	}

	envSiteDir := t.TempDir()
	os.WriteFile(filepath.Join(envSiteDir, ".env"), []byte("DB_HOST=servlo-mariadb\n"), 0644)
	if err := AddSite(Site{Name: "env-site", Domains: []string{"env.test"}, Path: envSiteDir}); err != nil {
		t.Fatalf("AddSite env: %v", err)
	}

	otherSiteDir := t.TempDir()
	os.WriteFile(filepath.Join(otherSiteDir, ".env"), []byte("DB_HOST=servlo-postgres\n"), 0644)
	if err := AddSite(Site{Name: "other-site", Domains: []string{"other.test"}, Path: otherSiteDir}); err != nil {
		t.Fatalf("AddSite other: %v", err)
	}

	sites := SitesUsingService("mariadb")
	if len(sites) != 2 {
		t.Fatalf("SitesUsingService(mariadb) returned %d sites, want 2", len(sites))
	}
	names := map[string]bool{}
	for _, s := range sites {
		names[s.Name] = true
	}
	if !names["yaml-site"] || !names["env-site"] {
		t.Errorf("expected both yaml-site and env-site, got %v", names)
	}
	if names["other-site"] {
		t.Errorf("other-site (uses postgres) should not be in mariadb result")
	}
}

func TestSitesUsingService_SkipsIgnoredAndPaused(t *testing.T) {
	setDataDir(t)

	activeDir := t.TempDir()
	os.WriteFile(filepath.Join(activeDir, ".servlo.yaml"), []byte("services:\n  - redis\n"), 0644)
	if err := AddSite(Site{Name: "active", Domains: []string{"active.test"}, Path: activeDir}); err != nil {
		t.Fatalf("AddSite active: %v", err)
	}

	ignoredDir := t.TempDir()
	os.WriteFile(filepath.Join(ignoredDir, ".servlo.yaml"), []byte("services:\n  - redis\n"), 0644)
	if err := AddSite(Site{Name: "ignored", Domains: []string{"i.test"}, Path: ignoredDir, Ignored: true}); err != nil {
		t.Fatalf("AddSite ignored: %v", err)
	}

	pausedDir := t.TempDir()
	os.WriteFile(filepath.Join(pausedDir, ".servlo.yaml"), []byte("services:\n  - redis\n"), 0644)
	if err := AddSite(Site{Name: "paused", Domains: []string{"p.test"}, Path: pausedDir, Paused: true}); err != nil {
		t.Fatalf("AddSite paused: %v", err)
	}

	sites := SitesUsingService("redis")
	if len(sites) != 1 || sites[0].Name != "active" {
		t.Errorf("SitesUsingService(redis) = %v, want only [active]", sites)
	}
}

func TestSitesUsingService_ServloYAMLTakesPriorityOverEnv(t *testing.T) {
	setDataDir(t)

	siteDir := t.TempDir()
	os.WriteFile(filepath.Join(siteDir, ".servlo.yaml"), []byte("services:\n  - postgres\n"), 0644)
	os.WriteFile(filepath.Join(siteDir, ".env"), []byte("DB_HOST=servlo-postgres\n"), 0644)
	if err := AddSite(Site{Name: "dual", Domains: []string{"d.test"}, Path: siteDir}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	sites := SitesUsingService("postgres")
	if len(sites) != 1 {
		t.Errorf("SitesUsingService(postgres) returned %d sites, want 1 (no double-counting)", len(sites))
	}
}
