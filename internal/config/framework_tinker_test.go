package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetTinkerForDir_LaravelBuiltin(t *testing.T) {
	setDataDir(t)
	setConfigDir(t)

	site := t.TempDir()
	if err := os.WriteFile(filepath.Join(site, "artisan"), []byte("#!/usr/bin/env php\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(site, "vendor", "laravel", "tinker"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := AddSite(Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: site, Framework: "laravel"}); err != nil {
		t.Fatal(err)
	}

	got := GetTinkerForDir(site)
	if got == nil {
		t.Fatal("expected tinker spec for Laravel site")
	}
	if len(got.Command) == 0 || got.Command[0] != "artisan" {
		t.Errorf("expected first command to be 'artisan', got %v", got.Command)
	}
	if got.ExecuteFlag != "--execute" {
		t.Errorf("ExecuteFlag = %q, want --execute", got.ExecuteFlag)
	}
}

func TestGetTinkerForDir_LaravelMissingTinkerPackage(t *testing.T) {
	setDataDir(t)
	setConfigDir(t)

	site := t.TempDir()
	if err := os.WriteFile(filepath.Join(site, "artisan"), []byte("#!/usr/bin/env php\n"), 0755); err != nil {
		t.Fatal(err)
	}
	// no vendor/laravel/tinker on purpose
	if err := AddSite(Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: site, Framework: "laravel"}); err != nil {
		t.Fatal(err)
	}

	if got := GetTinkerForDir(site); got != nil {
		t.Errorf("expected nil (requires_package not satisfied), got %+v", got)
	}
}

func TestGetTinkerForDir_NoSite(t *testing.T) {
	setDataDir(t)
	setConfigDir(t)

	if got := GetTinkerForDir(t.TempDir()); got != nil {
		t.Errorf("expected nil for unregistered path, got %+v", got)
	}
}

func TestGetTinkerForDir_RoundtripsPositional(t *testing.T) {
	// Resolver-level check that ExecutePositional survives load/merge,
	// using a custom user-defined framework so we don't depend on the
	// store-fetched Drupal YAML being present in the test env.
	setDataDir(t)
	setConfigDir(t)

	site := t.TempDir()
	if err := os.WriteFile(filepath.Join(site, "marker"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := AddSite(Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: site, Framework: "drupalish"}); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(FrameworksDir(), 0755); err != nil {
		t.Fatal(err)
	}
	yaml := `name: drupalish
public_dir: web
tinker:
  command: ["fake-cli", "eval"]
  execute_positional: true
  requires_file: marker
`
	if err := os.WriteFile(filepath.Join(FrameworksDir(), "drupalish.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	got := GetTinkerForDir(site)
	if got == nil {
		t.Fatal("expected resolver to return tinker spec")
	}
	if !got.ExecutePositional {
		t.Errorf("ExecutePositional did not roundtrip, got %+v", got)
	}
	if got.ExecuteFlag != "" {
		t.Errorf("ExecuteFlag should be empty, got %q", got.ExecuteFlag)
	}
}

func TestGetTinkerForDir_NoFrameworkAssigned(t *testing.T) {
	setDataDir(t)
	setConfigDir(t)

	site := t.TempDir()
	if err := AddSite(Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: site}); err != nil {
		t.Fatal(err)
	}

	if got := GetTinkerForDir(site); got != nil {
		t.Errorf("expected nil for site with no framework, got %+v", got)
	}
}
